package cmd

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Rivil/dross/internal/phase"
)

func Phase() *cobra.Command {
	c := &cobra.Command{
		Use:   "phase",
		Short: "Manage phase directories under .dross/phases/",
	}
	c.AddCommand(phaseList(), phaseCreate(), phaseShow())
	return c
}

func phaseList() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List phases",
		RunE: func(_ *cobra.Command, _ []string) error {
			root, err := FindRoot()
			if err != nil {
				return err
			}
			ids, err := phase.List(root)
			if err != nil {
				return err
			}
			if len(ids) == 0 {
				Print("(no phases)")
				return nil
			}
			for _, id := range ids {
				Print(id)
			}
			return nil
		},
	}
}

// phaseCreate makes the directory NN-slug and (when the repo has git
// and --no-branch isn't passed) switches to a phase/<id> branch so all
// phase work commits land off main. Keeping phase work off main means
// the squash-merge on origin doesn't diverge from local main — the
// reason every prior ship needed an explicit reconcile commit.
//
// Spec/plan are written by /dross-spec and /dross-plan slash commands.
func phaseCreate() *cobra.Command {
	var noBranch bool
	c := &cobra.Command{
		Use:   "create <title>",
		Short: "Create the next phase directory and switch to phase/<id>",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			title := strings.Join(args, " ")
			root, err := FindRoot()
			if err != nil {
				return err
			}
			repoDir := filepath.Dir(root)

			n, err := nextPhaseNumber(root)
			if err != nil {
				return err
			}
			id := fmt.Sprintf("%02d-%s", n, phase.Slugify(title))
			branchName := "phase/" + id

			// Pre-flight git checks before any side effects. We only do
			// these when the repo has git AND the user didn't opt out;
			// `dross init` runs cleanly in non-git dirs and we keep that
			// property here.
			hasGit := isDir(filepath.Join(repoDir, ".git"))
			if hasGit && !noBranch {
				if err := preflightPhaseBranch(repoDir, branchName); err != nil {
					return err
				}
			}

			dir := phase.Dir(root, id)
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return err
			}

			if hasGit && !noBranch {
				if out, err := gitCombined(repoDir, "checkout", "-b", branchName); err != nil {
					// Roll back the directory we just made so a retry
					// after fixing the git issue doesn't leak a phase
					// number. dir is empty so plain Remove suffices.
					_ = os.Remove(dir)
					return fmt.Errorf("git checkout -b %s: %w\n%s", branchName, err, out)
				}
				Printf("created %s\n", dir)
				Printf("checked out %s\n", branchName)
			} else {
				Printf("created %s\n", dir)
				if !hasGit {
					Print("(no .git/ found — skipping phase branch creation)")
				}
			}
			Print("Next: /dross-spec to write spec.toml, then /dross-plan")
			RecordOutcomeEvent("phase_create", map[string]int{"ordinal": n}, nil, nil)
			return nil
		},
	}
	c.Flags().BoolVar(&noBranch, "no-branch", false,
		"skip creating/checking out the phase/<id> git branch (advanced)")
	return c
}

// preflightPhaseBranch enforces the invariants required for a clean
// phase start: on the configured main branch, clean working tree, and
// no existing phase/<id> branch ref. Returns a user-facing error.
func preflightPhaseBranch(repoDir, branchName string) error {
	p, _, err := loadProject()
	if err != nil {
		return err
	}
	mainBranch := p.Repo.GitMainBranch
	if mainBranch == "" {
		mainBranch = "main"
	}

	cur, err := gitTrim(repoDir, "symbolic-ref", "--short", "HEAD")
	if err != nil {
		return fmt.Errorf("read current branch: %w", err)
	}
	if cur != mainBranch {
		return fmt.Errorf("must be on %s to start a phase (currently on %s); switch back or use --no-branch", mainBranch, cur)
	}

	status, err := gitTrim(repoDir, "status", "--porcelain")
	if err != nil {
		return fmt.Errorf("git status: %w", err)
	}
	if status != "" {
		return errors.New("working tree is dirty; commit or stash before starting a phase")
	}

	if err := gitNoOut(repoDir, "rev-parse", "--verify", "refs/heads/"+branchName); err == nil {
		return fmt.Errorf("branch %s already exists locally; delete it first or pass --no-branch", branchName)
	}
	return nil
}

// gitNoOut runs git silently, discarding output. Used when only the
// exit status matters (e.g. ref-exists probes).
func gitNoOut(repoDir string, args ...string) error {
	full := append([]string{"-C", repoDir}, args...)
	return exec.Command("git", full...).Run()
}

// isDir reports whether path exists and is a directory.
func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func phaseShow() *cobra.Command {
	return &cobra.Command{
		Use:   "show <phase-id>",
		Short: "Print the spec.toml and plan.toml for a phase",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			root, err := FindRoot()
			if err != nil {
				return err
			}
			dir := phase.Dir(root, args[0])
			for _, name := range []string{"spec.toml", "plan.toml"} {
				path := filepath.Join(dir, name)
				b, err := os.ReadFile(path)
				if err != nil {
					Printf("# %s — (missing)\n\n", path)
					continue
				}
				Printf("# %s\n%s\n", path, string(b))
			}
			return nil
		},
	}
}

func nextPhaseNumber(root string) (int, error) {
	entries, err := os.ReadDir(filepath.Join(root, "phases"))
	if err != nil && !os.IsNotExist(err) {
		return 0, err
	}
	max := 0
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		parts := strings.SplitN(e.Name(), "-", 2)
		if len(parts) < 1 {
			continue
		}
		n, err := strconv.Atoi(parts[0])
		if err != nil {
			continue
		}
		if n > max {
			max = n
		}
	}
	sort.Ints([]int{max})
	return max + 1, nil
}
