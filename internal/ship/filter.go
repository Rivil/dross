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

	// Verify branch doesn't already exist (unless --force).
	if exists, _ := branchExists(opts.RepoDir, opts.BranchName); exists {
		if !opts.Force {
			return "", "", fmt.Errorf("branch %s already exists (use Force to overwrite)", opts.BranchName)
		}
		if _, err := git(opts.RepoDir, "branch", "-D", opts.BranchName); err != nil {
			return "", "", fmt.Errorf("delete existing %s: %w", opts.BranchName, err)
		}
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
