package ship

import (
	"errors"
	"fmt"
	"os/exec"
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
//  2. Branching off the base.
//  3. Copying the current working tree onto that branch.
//  4. Removing .dross/ from the index + working tree.
//  5. Committing the cumulative diff as one squash with Opts.Message.
//
// Returns the branch name and the new commit SHA. The caller is
// responsible for pushing the branch and restoring the user's prior
// HEAD afterwards.
//
// v1 is squash-only. A history-preserving variant (per-commit cherry-
// pick + drop .dross/ per commit) is a follow-up.
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
	originalRef, err := gitOut(opts.RepoDir, "symbolic-ref", "--quiet", "--short", "HEAD")
	if err != nil {
		// Detached HEAD — fall back to SHA.
		originalRef = currentTip
	}

	// Verify branch doesn't already exist (unless --force).
	if exists, _ := branchExists(opts.RepoDir, opts.BranchName); exists {
		if !opts.Force {
			return "", "", fmt.Errorf("branch %s already exists (use Force to overwrite)", opts.BranchName)
		}
		if _, err := git(opts.RepoDir, "branch", "-D", opts.BranchName); err != nil {
			return "", "", fmt.Errorf("delete existing %s: %w", opts.BranchName, err)
		}
	}

	// 1) checkout fresh branch at base
	if _, err := git(opts.RepoDir, "checkout", "-b", opts.BranchName, base); err != nil {
		return "", "", fmt.Errorf("checkout -b %s %s: %w", opts.BranchName, base, err)
	}
	// Best-effort restore on any later error.
	defer func() {
		if err != nil {
			_, _ = git(opts.RepoDir, "checkout", originalRef)
		}
	}()

	// 2) overlay the tip's tree
	if _, err := git(opts.RepoDir, "checkout", currentTip, "--", "."); err != nil {
		return "", "", fmt.Errorf("checkout tip tree: %w", err)
	}

	// 3) drop .dross/ from index + working tree (ignore error if absent)
	_, _ = git(opts.RepoDir, "rm", "-r", "--quiet", "--cached", "--ignore-unmatch", ".dross")
	_, _ = exec.Command("rm", "-rf", opts.RepoDir+"/.dross").CombinedOutput()

	// 4) stage everything (handles deletions, additions, renames in working tree)
	if _, err := git(opts.RepoDir, "add", "-A"); err != nil {
		return "", "", fmt.Errorf("git add -A: %w", err)
	}

	// 5) commit (amend the implicit branch checkout if there's nothing to add — git complains)
	cmd := exec.Command("git", "-C", opts.RepoDir, "commit",
		"-m", opts.Message,
		"--no-verify",
	)
	out, cerr := cmd.CombinedOutput()
	if cerr != nil {
		return "", "", fmt.Errorf("commit: %w\n%s", cerr, string(out))
	}

	newSHA, err := gitOut(opts.RepoDir, "rev-parse", "HEAD")
	if err != nil {
		return "", "", fmt.Errorf("rev-parse new HEAD: %w", err)
	}

	// Return user to their original branch.
	if _, e := git(opts.RepoDir, "checkout", originalRef); e != nil {
		return opts.BranchName, newSHA, fmt.Errorf("created %s but failed to restore %s: %w", opts.BranchName, originalRef, e)
	}
	return opts.BranchName, newSHA, nil
}

// deriveBase returns the parent of the earliest phase commit per
// topological order. Falls back to "<main-branch>" — but main branch
// is unknown here, so caller passes BaseCommit explicitly when there
// are no recorded changes.
func deriveBase(repoDir string, c *changes.Changes) (string, error) {
	if c == nil || len(c.Tasks) == 0 {
		return "", errors.New("changes.json has no tasks; pass BaseCommit explicitly")
	}
	seen := map[string]bool{}
	var commits []string
	for _, t := range c.Tasks {
		if t.Commit != "" && !seen[t.Commit] {
			seen[t.Commit] = true
			commits = append(commits, t.Commit)
		}
	}
	if len(commits) == 0 {
		return "", errors.New("no commits recorded in changes.json")
	}
	args := append([]string{"rev-list", "--reverse", "--topo-order", "--no-walk"}, commits...)
	out, err := gitOut(repoDir, args...)
	if err != nil {
		return "", err
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) == 0 || lines[0] == "" {
		return "", errors.New("rev-list produced no output")
	}
	earliest := lines[0]
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
