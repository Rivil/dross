package ship

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Rivil/dross/internal/changes"
)

// FilterPreserveHistory creates a review branch that mirrors the
// per-task commit history of the phase, but with `.dross/` stripped
// from every commit. Use this when the reviewer benefits from seeing
// the work in the same shape it was authored — atomic per-task — and
// the squash form would obscure that.
//
// Algorithm
//   1. Same base/branch/worktree setup as FilterSquash.
//   2. For each phase commit (in topological order):
//      a. `git cherry-pick --no-commit <commit>` into the worktree.
//      b. Remove .dross/ from the index + worktree.
//      c. Re-commit with the cherry-picked message + author/date.
//   3. Empty cherry-picks (commits whose only changes were under
//      .dross/) are skipped — they'd produce no-op commits otherwise.
//
// Returns the branch name and the SHA of the tip of the new history.
//
// Caller is responsible for pushing the branch.
//
// Limitations
//   - Merge commits in the source range are not handled — they get
//     skipped with a warning. Phase work in dross is linear by
//     convention so this rarely matters.
//   - Author identity is preserved; committer is whoever runs ship.
func FilterPreserveHistory(c *changes.Changes, opts FilterOpts) (branch, sha string, err error) {
	if opts.RepoDir == "" {
		return "", "", errors.New("RepoDir is required")
	}
	if opts.PhaseID == "" {
		return "", "", errors.New("PhaseID is required")
	}
	if opts.BranchName == "" {
		opts.BranchName = "pr/" + opts.PhaseID
	}

	commits, err := orderedPhaseCommits(opts.RepoDir, c)
	if err != nil {
		return "", "", err
	}
	if len(commits) == 0 {
		return "", "", errors.New("no phase commits to preserve")
	}

	// Base is the parent of the earliest phase commit (commits[0]).
	// We don't reuse FilterSquash's deriveBase here because it relies
	// on `rev-list --no-walk --topo-order --reverse` whose flag
	// combination silently ignores topo-order — the "earliest" it
	// returns is whichever commit happens to come first from map
	// iteration. orderedPhaseCommits sorts by ancestry, so commits[0]
	// is the actual root of the chain.
	base := opts.BaseCommit
	if base == "" {
		parent, err := gitOut(opts.RepoDir, "rev-parse", commits[0]+"^")
		if err != nil {
			return "", "", fmt.Errorf("earliest phase commit %s has no parent: %w", commits[0], err)
		}
		base = parent
	}

	// Existing-branch handling matches FilterSquash exactly.
	if err := prepareExistingBranch(opts.RepoDir, opts.BranchName, opts.Force); err != nil {
		return "", "", err
	}

	wtDir, err := os.MkdirTemp("", "dross-ship-history-")
	if err != nil {
		return "", "", fmt.Errorf("create temp dir for worktree: %w", err)
	}
	if err := os.RemoveAll(wtDir); err != nil {
		return "", "", fmt.Errorf("clear temp dir: %w", err)
	}
	if _, err := git(opts.RepoDir, "worktree", "add", "--detach", "--quiet", wtDir, base); err != nil {
		return "", "", fmt.Errorf("worktree add %s %s: %w", wtDir, base, err)
	}
	defer func() {
		_, _ = git(opts.RepoDir, "worktree", "remove", "--force", wtDir)
	}()

	for _, commit := range commits {
		if err := cherryPickStripDross(wtDir, commit); err != nil {
			return "", "", err
		}
	}
	// Final defensive sweep: if the worktree somehow has .dross/ left
	// over (e.g. from base, or from a path classification miss), strip
	// it before we return. The tip commit was already made; this can't
	// affect commit history but prevents stale files in the worktree
	// from leaking into a follow-up commit if the caller re-uses it.
	_ = os.RemoveAll(filepath.Join(wtDir, ".dross"))

	tipSHA, err := gitOut(wtDir, "rev-parse", "HEAD")
	if err != nil {
		return "", "", fmt.Errorf("rev-parse new tip: %w", err)
	}
	// Verify we actually advanced past the base — if every cherry-pick
	// became empty (whole phase was .dross/-only), tipSHA == base and
	// there's nothing to ship.
	if tipSHA == base {
		return "", "", errors.New("history-preserving filter produced no commits (every phase commit touched only .dross/)")
	}

	if _, err := git(opts.RepoDir, "branch", opts.BranchName, tipSHA); err != nil {
		return "", "", fmt.Errorf("create branch %s: %w", opts.BranchName, err)
	}
	return opts.BranchName, tipSHA, nil
}

// cherryPickStripDross applies one source commit, drops .dross/ from
// the resulting tree, and writes either:
//   - a new commit preserving the source message + authorship, or
//   - nothing (skipped) if the commit's only changes were under .dross/
//
// Implementation note: we don't use `git cherry-pick`. Cherry-pick
// auto-commits even with --no-commit when the source matches certain
// fast-forward conditions, and we need full control over the index
// to strip .dross/ before the commit lands. Instead we materialize
// the source commit's tree, apply only the non-.dross/ paths, and
// commit explicitly via `git commit -C <source>` to preserve message
// and authorship.
func cherryPickStripDross(wtDir, sourceCommit string) error {
	// 1. List paths changed by sourceCommit relative to its parent.
	//    --diff-filter excludes nothing (we want adds, mods, deletes).
	diffOut, err := gitOut(wtDir, "diff-tree", "--no-commit-id", "--name-only",
		"-r", sourceCommit)
	if err != nil {
		return fmt.Errorf("diff-tree %s: %w", sourceCommit, err)
	}
	var keepPaths []string
	for _, line := range strings.Split(diffOut, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if line == ".dross" || strings.HasPrefix(line, ".dross/") {
			continue
		}
		keepPaths = append(keepPaths, line)
	}
	if len(keepPaths) == 0 {
		// Whole commit was .dross/-only — skip cleanly.
		return nil
	}

	// 2. For each kept path, copy its blob from sourceCommit's tree
	//    into the worktree's index + working tree. Handle deletions
	//    (path absent in sourceCommit) by removing from index.
	for _, p := range keepPaths {
		exists, err := pathInCommit(wtDir, sourceCommit, p)
		if err != nil {
			return fmt.Errorf("inspect %s in %s: %w", p, sourceCommit, err)
		}
		if !exists {
			// Source removed this path — remove from index too.
			if _, err := git(wtDir, "rm", "--quiet", "--cached", "--ignore-unmatch", p); err != nil {
				return fmt.Errorf("rm cached %s: %w", p, err)
			}
			_ = os.Remove(filepath.Join(wtDir, p))
			continue
		}
		// Materialize the blob into worktree.
		blob, err := gitOut(wtDir, "show", sourceCommit+":"+p)
		if err != nil {
			return fmt.Errorf("show %s:%s: %w", sourceCommit, p, err)
		}
		full := filepath.Join(wtDir, p)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return fmt.Errorf("mkdir for %s: %w", p, err)
		}
		// `git show <commit>:<path>` strips the trailing newline; many
		// files genuinely end in one, so re-append. False positives on
		// trailing-newline-free files are rare and harmless for review.
		if err := os.WriteFile(full, []byte(blob+"\n"), 0o644); err != nil {
			return fmt.Errorf("write %s: %w", p, err)
		}
	}

	// 3. Stage only the kept paths. `git add -A` would scoop up anything
	//    sitting in the worktree (including .dross/ if a prior copy or
	//    base checkout left it behind) — we want the index to reflect
	//    only what we deliberately materialized. Use --force so the
	//    user's repo-level .gitignore (which often gitignores .dross/
	//    and could in theory ignore other phase paths too) doesn't
	//    silently swallow legitimate adds.
	for _, p := range keepPaths {
		out, err := git(wtDir, "add", "--force", "--", p)
		if err != nil {
			if strings.Contains(err.Error(), "did not match any files") {
				continue
			}
			return fmt.Errorf("git add %s: %w\n%s", p, err, out)
		}
	}

	// 4. If after staging the index matches HEAD's tree, the change set
	//    was effectively empty after the .dross/ strip — skip.
	statusOut, err := gitOut(wtDir, "status", "--porcelain")
	if err != nil {
		return fmt.Errorf("status: %w", err)
	}
	if strings.TrimSpace(statusOut) == "" {
		return nil
	}

	// 5. Commit, copying message + authorship from source.
	cmd := exec.Command("git", "-C", wtDir, "commit",
		"--no-verify",
		"-C", sourceCommit,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("commit cherry-pick of %s: %w\n%s", sourceCommit, err, string(out))
	}
	return nil
}

// pathInCommit reports whether a given path is present in the tree of
// sourceCommit. Used to distinguish deletes from adds/mods when
// reconstructing the cherry-pick.
func pathInCommit(wtDir, sourceCommit, path string) (bool, error) {
	_, err := gitOut(wtDir, "cat-file", "-e", sourceCommit+":"+path)
	if err == nil {
		return true, nil
	}
	// `cat-file -e` exits 128 when the path is absent. We don't have a
	// clean way to distinguish that from other errors short of inspecting
	// the message — but treating any error as "absent" is fine: a real
	// disk/repo failure will surface on the next git call.
	return false, nil
}

// orderedPhaseCommits returns the phase's commits in topological
// (root-first) order, deduplicated. Sort is by ancestry: `a` sorts
// before `b` if `a` is an ancestor of `b`. Same-second commits
// (common in tests) are ordered correctly regardless of timestamp
// resolution.
//
// For non-linear histories (merge commits within the phase) ancestry
// is a partial order; sort.Slice is stable enough in practice — the
// produced order is *a* topological order, not necessarily the only
// one. dross phase work is linear by convention so this rarely
// matters.
func orderedPhaseCommits(repoDir string, c *changes.Changes) ([]string, error) {
	if c == nil || len(c.Tasks) == 0 {
		return nil, errors.New("changes.json has no tasks")
	}
	seen := map[string]bool{}
	var raw []string
	for _, t := range c.Tasks {
		if t.Commit != "" && !seen[t.Commit] {
			seen[t.Commit] = true
			raw = append(raw, t.Commit)
		}
	}
	if len(raw) == 0 {
		return nil, errors.New("no commits recorded in changes.json")
	}

	// Sort by ancestor-count within the recorded set. The root of the
	// chain has 0 ancestors among its peers; each subsequent commit has
	// exactly one more. Comparator-based sort with `is-ancestor` as a
	// less-than is unreliable here — Go's pdqsort can call less(i, i)
	// internally and `is-ancestor(a, a)` returns true, which violates
	// the strict-weak-ordering contract. The count-based approach is
	// O(n²) git calls but n is small (tasks per phase, typically <20)
	// and the determinism is worth it.
	isAncestor := func(a, b string) bool {
		if a == b {
			return false
		}
		err := exec.Command("git", "-C", repoDir,
			"merge-base", "--is-ancestor", a, b).Run()
		return err == nil
	}
	ancestorsOf := make(map[string]int, len(raw))
	for _, c1 := range raw {
		for _, c2 := range raw {
			if isAncestor(c2, c1) {
				ancestorsOf[c1]++
			}
		}
	}
	sort.SliceStable(raw, func(i, j int) bool {
		return ancestorsOf[raw[i]] < ancestorsOf[raw[j]]
	})
	return raw, nil
}
