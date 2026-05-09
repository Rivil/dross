package ship

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/Rivil/dross/internal/changes"
)

// FilterOpts configures the squash-into-clean-branch flow.
type FilterOpts struct {
	RepoDir    string // working tree root (where .git lives)
	PhaseID    string // e.g. "01-meal-tagging"
	BranchName string // defaults to "pr/<PhaseID>"
	Message    string // commit message for the squash
	BaseCommit string // optional; if empty, derived from changes.json
	Force      bool   // recreate BranchName if it already exists
}

// FilterSquash creates a clean review branch by:
//  1. Computing the base commit (parent of earliest phase commit, from
//     changes.json — unless explicitly overridden).
//  2. Adding an ephemeral git worktree at that base commit.
//  3. Overlaying the current tip's tree inside the worktree.
//  4. Removing .dross/ from the worktree's index + files.
//  5. Committing the cumulative diff as one squash with Opts.Message.
//  6. Creating the PR branch in the main repo at the new commit SHA.
//  7. Removing the ephemeral worktree.
//
// Returns the branch name and the new commit SHA. The caller is
// responsible for pushing the branch.
//
// All destructive work happens inside the ephemeral worktree — the
// user's working tree is never touched. This is critical because
// .dross/ is gitignored: once removed from the user's working tree,
// git cannot restore it on branch checkout (untracked files don't
// participate in checkout).
//
// Use FilterPreserveHistory in the same package when reviewers benefit
// from seeing the per-task commit shape; FilterSquash is the default
// because most PRs read better as one cumulative diff.
func FilterSquash(c *changes.Changes, opts FilterOpts) (branch, sha string, err error) {
	if opts.RepoDir == "" {
		return "", "", errors.New("RepoDir is required")
	}
	if opts.PhaseID == "" {
		return "", "", errors.New("PhaseID is required")
	}
	if opts.Message == "" {
		opts.Message = "phase " + opts.PhaseID
	}
	if opts.BranchName == "" {
		opts.BranchName = "pr/" + opts.PhaseID
	}

	base := opts.BaseCommit
	if base == "" {
		b, err := deriveBase(opts.RepoDir, c)
		if err != nil {
			return "", "", fmt.Errorf("derive base: %w", err)
		}
		base = b
	}

	currentTip, err := gitOut(opts.RepoDir, "rev-parse", "HEAD")
	if err != nil {
		return "", "", fmt.Errorf("rev-parse HEAD: %w", err)
	}

	if err := prepareExistingBranch(opts.RepoDir, opts.BranchName, opts.Force); err != nil {
		return "", "", err
	}

	// Create an ephemeral worktree path. MkdirTemp gives us a unique name we
	// own; remove it so `git worktree add` can create it fresh.
	wtDir, err := os.MkdirTemp("", "dross-ship-")
	if err != nil {
		return "", "", fmt.Errorf("create temp dir for worktree: %w", err)
	}
	if err := os.RemoveAll(wtDir); err != nil {
		return "", "", fmt.Errorf("clear temp dir for worktree: %w", err)
	}

	if _, err := git(opts.RepoDir, "worktree", "add", "--detach", "--quiet", wtDir, base); err != nil {
		return "", "", fmt.Errorf("worktree add %s %s: %w", wtDir, base, err)
	}
	// Best-effort cleanup. --force handles the case where commit failed and
	// the worktree has uncommitted state.
	defer func() {
		_, _ = git(opts.RepoDir, "worktree", "remove", "--force", wtDir)
	}()

	// 1) overlay the tip's tree onto the worktree
	if _, err := git(wtDir, "checkout", currentTip, "--", "."); err != nil {
		return "", "", fmt.Errorf("checkout tip tree in worktree: %w", err)
	}

	// 2) drop .dross/ from index + worktree (defensive; safe because we're
	//    operating on the ephemeral worktree, not the user's checkout)
	_, _ = git(wtDir, "rm", "-r", "--quiet", "--cached", "--ignore-unmatch", ".dross")
	_ = os.RemoveAll(filepath.Join(wtDir, ".dross"))

	// 3) stage everything (handles deletions, additions, renames)
	if _, err := git(wtDir, "add", "-A"); err != nil {
		return "", "", fmt.Errorf("git add -A in worktree: %w", err)
	}

	// 4) commit
	cmd := exec.Command("git", "-C", wtDir, "commit",
		"-m", opts.Message,
		"--no-verify",
	)
	out, cerr := cmd.CombinedOutput()
	if cerr != nil {
		return "", "", fmt.Errorf("commit in worktree: %w\n%s", cerr, string(out))
	}

	newSHA, err := gitOut(wtDir, "rev-parse", "HEAD")
	if err != nil {
		return "", "", fmt.Errorf("rev-parse new HEAD in worktree: %w", err)
	}

	// 5) create the PR branch in the main repo at the new commit. Worktrees
	//    share the object database with the main repo, so the new SHA is
	//    immediately reachable here without an explicit fetch.
	if _, err := git(opts.RepoDir, "branch", opts.BranchName, newSHA); err != nil {
		return "", "", fmt.Errorf("create branch %s at %s: %w", opts.BranchName, newSHA, err)
	}

	return opts.BranchName, newSHA, nil
}

// deriveBase returns the parent of the earliest phase commit per
// ancestry order — the first commit such that no other recorded
// commit is its ancestor. `git rev-list --topo-order --no-walk`
// silently ignores --topo-order and emits commits in argument
// order, which is whatever Go's map iteration produces; that bug
// caused FilterSquash to occasionally pick a base partway through
// the phase chain. Now we use ancestry directly.
func deriveBase(repoDir string, c *changes.Changes) (string, error) {
	ordered, err := orderedPhaseCommits(repoDir, c)
	if err != nil {
		return "", err
	}
	earliest := ordered[0]
	parent, err := gitOut(repoDir, "rev-parse", earliest+"^")
	if err != nil {
		return "", fmt.Errorf("earliest commit %s has no parent: %w", earliest, err)
	}
	return parent, nil
}

func branchExists(repoDir, name string) (bool, error) {
	_, err := gitOut(repoDir, "show-ref", "--verify", "--quiet", "refs/heads/"+name)
	if err != nil {
		// non-zero exit just means "doesn't exist"
		return false, nil
	}
	return true, nil
}

// prepareExistingBranch is the auto-overwrite handler for the squash
// branch. dross owns the `pr/*` namespace by convention — the user
// re-running ship for the same phase clearly intends to rebuild it,
// which previously required a `--force-branch` retry. We now drop
// stale local-only branches and fully-in-sync branches without asking.
//
// We *do* still gate on diverged branches (local commits beyond the
// upstream that wouldn't be reproduced by the upcoming filter) — that
// is the only case where auto-overwrite could lose user work. Force
// remains the explicit override.
func prepareExistingBranch(repoDir, name string, force bool) error {
	exists, _ := branchExists(repoDir, name)
	if !exists {
		return nil
	}
	if force {
		if _, err := git(repoDir, "branch", "-D", name); err != nil {
			return fmt.Errorf("delete existing %s: %w", name, err)
		}
		return nil
	}
	if !branchHasUnpushedWork(repoDir, name) {
		if _, err := git(repoDir, "branch", "-D", name); err != nil {
			return fmt.Errorf("delete existing %s: %w", name, err)
		}
		// Surface the auto-replacement so the user sees what we did.
		// stderr keeps the squash branch's stdout (printed by the
		// caller as "Built ...") clean for piping.
		fmt.Fprintf(os.Stderr, "ship: replacing stale local %s\n", name)
		return nil
	}
	return fmt.Errorf("branch %s already exists with unpushed local commits — "+
		"run `git log origin/%s..%s` to inspect, then re-run with --force-branch "+
		"to discard and rebuild from current HEAD", name, name, name)
}

// branchHasUnpushedWork returns true only when the branch has commits
// beyond what's on its tracked upstream. Three "safe" cases:
//   - no upstream tracked → branch was never pushed (stale local artefact)
//   - upstream exists, branch is fully in sync or purely behind
//
// On any git error we fail closed (return true) so the caller falls
// back to the explicit force gate. Better to make the user re-run with
// --force-branch than to silently drop work because git refused to
// answer.
func branchHasUnpushedWork(repoDir, name string) bool {
	upstream, err := gitOut(repoDir, "rev-parse", "--abbrev-ref", name+"@{upstream}")
	if err != nil {
		// no upstream — never pushed; safe to drop
		return false
	}
	if upstream == "" {
		return false
	}
	ahead, err := gitOut(repoDir, "rev-list", "--count", upstream+".."+name)
	if err != nil {
		return true
	}
	return ahead != "0"
}

func git(repoDir string, args ...string) (string, error) {
	full := append([]string{"-C", repoDir}, args...)
	out, err := exec.Command("git", full...).CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("git %s: %w\n%s", strings.Join(args, " "), err, string(out))
	}
	return strings.TrimSpace(string(out)), nil
}

func gitOut(repoDir string, args ...string) (string, error) {
	full := append([]string{"-C", repoDir}, args...)
	out, err := exec.Command("git", full...).Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
