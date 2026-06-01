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
	"github.com/Rivil/dross/internal/state"
)

func Phase() *cobra.Command {
	c := &cobra.Command{
		Use:   "phase",
		Short: "Manage phase directories under .dross/phases/",
	}
	c.AddCommand(phaseList(), phaseCreate(), phaseShow(), phaseComplete())
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

			// Mark this as the active phase so downstream commands can
			// resolve "no args = current phase" cleanly. Done after the
			// branch op so a failed checkout doesn't leave state pointing
			// at a phase whose branch wasn't created.
			s, err := state.Load(filepath.Join(root, state.File))
			if err != nil {
				return err
			}
			s.CurrentPhase = id
			s.CurrentPhaseStatus = "created"
			s.Touch(fmt.Sprintf("created %s", id))
			if err := s.Save(filepath.Join(root, state.File)); err != nil {
				return fmt.Errorf("save state: %w", err)
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

// phaseComplete finalizes a phase after its PR has been squash-merged
// upstream. It switches back to main, fast-forwards from origin, deletes
// the local phase branch, and clears state.CurrentPhase with an audit
// entry. This is the post-merge counterpart to `dross phase create` —
// together they keep phase work fully off main.
func phaseComplete() *cobra.Command {
	return &cobra.Command{
		Use:   "complete [phase-id]",
		Short: "Finalize a phase after squash-merge: ff main, delete phase/<id>",
		Long: `Run after the PR for this phase has been squash-merged upstream.

  1. switch to the configured main branch (if not already there)
  2. fetch origin
  3. fast-forward main from origin/<main>
  4. delete the local phase/<id> branch
  5. clear state.current_phase + record a "completed <id>" history
     entry, then commit state.json as a chore

Refuses on a dirty tree or when the fast-forward isn't clean — both
usually mean the upstream merge hasn't actually happened yet.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			root, err := FindRoot()
			if err != nil {
				return err
			}
			repoDir := filepath.Dir(root)
			p, _, err := loadProject()
			if err != nil {
				return err
			}
			s, err := state.Load(filepath.Join(root, state.File))
			if err != nil {
				return err
			}

			phaseID := ""
			if len(args) == 1 {
				phaseID = args[0]
			} else {
				phaseID = s.CurrentPhase
			}
			if phaseID == "" {
				return errors.New("no phase id given and state has no current_phase")
			}

			mainBranch := p.Repo.GitMainBranch
			if mainBranch == "" {
				mainBranch = "main"
			}
			phaseBranch := "phase/" + phaseID

			// Working tree must be clean — checkout and branch -D both
			// behave better on a clean tree, and a dirty one usually
			// signals the user hasn't finished the previous step.
			status, err := gitTrim(repoDir, "status", "--porcelain")
			if err != nil {
				return fmt.Errorf("git status: %w", err)
			}
			if status != "" {
				return errors.New("working tree is dirty; commit or stash before completing")
			}

			// Switch to main if we aren't already there.
			cur, err := gitTrim(repoDir, "symbolic-ref", "--short", "HEAD")
			if err != nil {
				return fmt.Errorf("git symbolic-ref failed (read current branch): %w", err)
			}
			if cur != mainBranch {
				if out, err := gitCombined(repoDir, "checkout", mainBranch); err != nil {
					return fmt.Errorf("git checkout %s: %w\n%s", mainBranch, err, out)
				}
			}

			if out, err := gitCombined(repoDir, "fetch", "origin"); err != nil {
				return fmt.Errorf("git fetch: %w\n%s", err, out)
			}

			// Guard against the "branch deleted but never merged" trap:
			// ff-only happily no-ops when origin/main hasn't advanced,
			// then we'd delete phase/<id> and lose the work. Verify
			// origin/main is strictly past the merge-base with phase/<id>
			// (i.e. the upstream merge actually happened) before
			// touching anything destructive.
			if err := gitNoOut(repoDir, "rev-parse", "--verify", "refs/heads/"+phaseBranch); err == nil {
				mergeBase, err := gitTrim(repoDir, "merge-base", "origin/"+mainBranch, phaseBranch)
				if err != nil {
					return fmt.Errorf("git merge-base origin/%s %s: %w", mainBranch, phaseBranch, err)
				}
				originHead, err := gitTrim(repoDir, "rev-parse", "origin/"+mainBranch)
				if err != nil {
					return fmt.Errorf("git rev-parse origin/%s: %w", mainBranch, err)
				}
				if originHead == mergeBase {
					return fmt.Errorf("origin/%s hasn't advanced past %s's base — has the PR actually merged upstream?",
						mainBranch, phaseBranch)
				}
			}

			if out, err := gitCombined(repoDir, "merge", "--ff-only", "origin/"+mainBranch); err != nil {
				return fmt.Errorf("fast-forward of %s from origin failed: %w\n%s",
					mainBranch, err, out)
			}

			// Delete the local phase branch (best-effort: only if it exists).
			if err := gitNoOut(repoDir, "rev-parse", "--verify", "refs/heads/"+phaseBranch); err == nil {
				if out, err := gitCombined(repoDir, "branch", "-D", phaseBranch); err != nil {
					return fmt.Errorf("git branch -D %s: %w\n%s", phaseBranch, err, out)
				}
			}

			// Clear current_phase and audit. Then commit state.json so
			// next dross commands don't start on a dirty tree.
			s.CurrentPhase = ""
			s.CurrentPhaseStatus = ""
			s.Touch(fmt.Sprintf("completed %s", phaseID))
			if err := s.Save(filepath.Join(root, state.File)); err != nil {
				return fmt.Errorf("save state: %w", err)
			}
			if out, err := gitCombined(repoDir, "add", filepath.Join(".dross", state.File)); err != nil {
				return fmt.Errorf("git add state.json: %w\n%s", err, out)
			}
			msg := fmt.Sprintf("chore(dross): complete %s", phaseID)
			if out, err := gitCombined(repoDir, "commit", "-m", msg); err != nil {
				return fmt.Errorf("git commit: %w\n%s", err, out)
			}

			RecordOutcomeEvent("phase_complete",
				map[string]int{},
				nil,
				map[string]string{"result": "completed"},
			)
			Printf("completed %s — main is at origin, phase/%s deleted\n", phaseID, phaseID)
			return nil
		},
	}
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
		return fmt.Errorf("git symbolic-ref failed (read current branch): %w", err)
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
