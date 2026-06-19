package cmd

import (
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Rivil/dross/internal/state"
)

// shipRecover heals legacy repos where phase commits accumulated on
// main and the squash-merge then created a parallel history on origin.
// The phase-branch model (phase/<id> branches via `dross phase create`,
// `dross phase complete` after merge) prevents this from happening on
// new work, but pre-existing repos need a one-shot reset.
//
// Two distinct legacy cases this heals:
//  1. Old strip-filter era: `.dross/` was filtered out of the PR branch,
//     so origin/main lost it on every squash-merge. Recovery restores
//     the full `.dross/` tree from the pre-reset HEAD.
//  2. Pre-phase-branch era: phase commits lived on main (no phase/<id>
//     branch), so the squash-merge diverged main from itself.
//     Recovery resets main to origin and re-attaches `.dross/`.
//
// Either way: fetch + reset + restore `.dross/` + commit, atomically.
func shipRecover() *cobra.Command {
	var preMergeSHA string
	c := &cobra.Command{
		Use:   "recover [phase-id]",
		Short: "Heal main after a legacy squash-merge (one-shot migration)",
		Long: `One-shot migration tool. New dross workflows don't need this —
phase work lives on phase/<id> branches, ` + "`dross phase complete`" + `
handles post-merge sync, and main never diverges.

Use this only when a previously-shipped repo is stuck because:

  - phase commits were made directly on main (no phase/<id>), so the
    squash-merge created a parallel history on origin; or
  - an older dross version stripped .dross/ from the PR branch, leaving
    origin/main without planning artefacts.

The recovery is the same in both cases:

  1. fetch origin
  2. reset --hard origin/<main>
  3. checkout <pre-merge-sha> -- .dross/   (default: current HEAD)
  4. update state.json (records the merge)
  5. one atomic commit with the restored .dross/ tree

Pass --pre-merge-sha if you've already manually reset main and HEAD no
longer holds the pre-merge .dross/ tree:

  dross ship recover --pre-merge-sha=$(git rev-parse HEAD@{1}) <phase-id>`,
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

			// Refuse to run on the wrong branch — reset is destructive.
			cur, err := gitTrim(repoDir, "symbolic-ref", "--short", "HEAD")
			if err != nil {
				return fmt.Errorf("read current branch: %w", err)
			}
			if cur != mainBranch {
				return fmt.Errorf("must be on %s before recovering (currently on %s)", mainBranch, cur)
			}

			// Refuse to run on a dirty tree — reset would silently destroy work.
			status, err := gitTrim(repoDir, "status", "--porcelain")
			if err != nil {
				return fmt.Errorf("git status: %w", err)
			}
			if status != "" {
				return errors.New("working tree is dirty; commit or stash before recovering")
			}

			// Capture the SHA holding the pre-merge .dross/ tree *before*
			// we mutate anything. Default: current HEAD (which still has
			// the phase commits, as the divergent steady state requires).
			// Override: --pre-merge-sha for the case where the user has
			// already manually reset main and lost current HEAD.
			sha := preMergeSHA
			if sha == "" {
				sha, err = gitTrim(repoDir, "rev-parse", "HEAD")
				if err != nil {
					return fmt.Errorf("rev-parse HEAD: %w", err)
				}
			}

			// Pre-check: SHA must actually contain a .dross/ tree, or the
			// checkout step would fail with an unhelpful pathspec error.
			if err := exec.Command("git", "-C", repoDir, "rev-parse", "--verify",
				sha+":.dross").Run(); err != nil {
				return fmt.Errorf("commit %s has no .dross/ tree — nothing to restore. "+
					"If you've already reset main, pass "+
					"--pre-merge-sha=$(git rev-parse HEAD@{1})", short(sha))
			}

			if out, err := gitCombined(repoDir, "fetch", "origin"); err != nil {
				return fmt.Errorf("git fetch: %w\n%s", err, out)
			}
			if out, err := gitCombined(repoDir, "reset", "--hard", "origin/"+mainBranch); err != nil {
				return fmt.Errorf("git reset --hard origin/%s: %w\n%s", mainBranch, err, out)
			}
			if out, err := gitCombined(repoDir, "checkout", sha, "--", ".dross/"); err != nil {
				return fmt.Errorf("git checkout %s -- .dross/: %w\n%s", short(sha), err, out)
			}

			// Touch state.json so the recovery commit captures the merge
			// in the history log. The earlier ship-time `Touch("shipped …")`
			// is preserved because the reset to origin/main doesn't touch
			// state.json (it's on local main pre-reset, but state.json is
			// inside .dross/ which we just restored from the pre-reset SHA).
			s.Touch(fmt.Sprintf("merged %s", phaseID))
			if err := s.Save(filepath.Join(root, state.File)); err != nil {
				return fmt.Errorf("save state: %w", err)
			}

			if out, err := gitCombined(repoDir, "add", ".dross/"); err != nil {
				return fmt.Errorf("git add: %w\n%s", err, out)
			}
			msg := fmt.Sprintf("chore(dross): restore .dross/ after squash-merge for %s + merge", phaseID)
			if out, err := gitCombined(repoDir, "commit", "-m", msg); err != nil {
				return fmt.Errorf("git commit: %w\n%s", err, out)
			}

			RecordOutcomeEvent("ship_recover",
				map[string]int{},
				nil,
				map[string]string{"result": "recovered"},
			)
			Printf("Restored .dross/ from %s and recorded merge for %s\n", short(sha), phaseID)
			return nil
		},
	}
	c.Flags().StringVar(&preMergeSHA, "pre-merge-sha", "",
		"commit holding the pre-merge .dross/ tree (default: current HEAD)")
	return c
}

func gitTrim(repoDir string, args ...string) (string, error) {
	full := append([]string{"-C", repoDir}, args...)
	out, err := exec.Command("git", full...).Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func gitCombined(repoDir string, args ...string) (string, error) {
	full := append([]string{"-C", repoDir}, args...)
	out, err := exec.Command("git", full...).CombinedOutput()
	return string(out), err
}

func short(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}
