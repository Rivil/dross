package cmd

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Rivil/dross/internal/project"
	"github.com/Rivil/dross/internal/state"
)

// Repair wires t-1/t-2/t-3's detectors together into one top-level command:
// dry-run by default (report only), --apply writes the restores and commits
// them (locked decision repair_invocation_mode). Reconstructing state.json is
// inferred/lossy, so the plan is always shown before it can become the new
// truth on disk.
func Repair() *cobra.Command {
	var apply bool
	c := &cobra.Command{
		Use:   "repair",
		Short: "Detect and restore clobbered or missing tracked .dross/ files",
		Long: `Diffs the working tree against git history for tracked .dross/ files
(project.toml, rules.toml, milestone tomls, phase spec.toml/plan.toml, whole
phase directories wiped by a checkout) and reports what's missing or
diverged. Also reconstructs state.json when it's missing or clearly stale
(its current_phase disagrees with the checked-out phase/<id> branch).

Dry-run by default — reports findings without writing anything. Pass --apply
to write the restores and commit the result.`,
		RunE: func(_ *cobra.Command, _ []string) error {
			// LocateRoot, not FindRoot: a missing/clobbered project.toml is
			// exactly what repair exists to detect and fix, so it must reach
			// here rather than being turned away as not-a-dross-repo.
			root, _, err := LocateRoot()
			if err != nil {
				return err
			}
			repoDir := filepath.Dir(root)

			mainBranch := "main"
			if p, perr := project.Load(filepath.Join(root, project.File)); perr == nil && p.Repo.GitMainBranch != "" {
				mainBranch = p.Repo.GitMainBranch
			}

			clobbered, err := detectModifiedOrMissingTracked(repoDir)
			if err != nil {
				return fmt.Errorf("detect clobbered files: %w", err)
			}
			missingDirs, err := detectMissingPhaseDirs(repoDir, root, mainBranch)
			if err != nil {
				return fmt.Errorf("detect missing phase dirs: %w", err)
			}
			statePath := filepath.Join(root, state.File)
			stale, reconstructed, err := checkStaleState(repoDir, root, statePath, mainBranch)
			if err != nil {
				return fmt.Errorf("check state.json: %w", err)
			}

			if len(clobbered) == 0 && len(missingDirs) == 0 && !stale {
				Print("nothing to repair")
				return nil
			}

			reportRepairFindings(clobbered, missingDirs, stale)

			if !apply {
				Print("\ndry-run — pass --apply to write these fixes")
				return nil
			}

			for _, f := range clobbered {
				if err := restorePathFromRef(repoDir, "HEAD", f.Path); err != nil {
					return fmt.Errorf("restore %s: %w", f.Path, err)
				}
			}
			for _, id := range missingDirs {
				dirPath := RootDirName + "/phases/" + id
				if err := restorePathFromRef(repoDir, "origin/"+mainBranch, dirPath); err != nil {
					return fmt.Errorf("restore phase dir %s: %w", id, err)
				}
			}
			if stale {
				if err := reconstructed.Save(statePath); err != nil {
					return fmt.Errorf("save %s: %w", statePath, err)
				}
			}

			if out, err := gitCombined(repoDir, "add", RootDirName); err != nil {
				return fmt.Errorf("git add %s: %w\n%s", RootDirName, err, out)
			}
			// Empty-commit guard. Restoring a clobbered tracked file to HEAD's
			// blob can never itself produce a diff against HEAD — the working
			// tree simply returns to what git already had, same as before the
			// clobber. state.json is gitignored and never stages at all. A
			// real diff only arises when a restored phase dir carries content
			// local HEAD never had (t-3's origin-sourced restore).
			if gitNoOut(repoDir, "diff", "--cached", "--quiet") == nil {
				Print("\nrestored — nothing further to commit (already matches git history)")
				return nil
			}
			msg := "chore(dross): repair clobbered .dross/ artefacts"
			if out, err := gitCombined(repoDir, "commit", "-m", msg); err != nil {
				return fmt.Errorf("git commit: %w\n%s", err, out)
			}
			Print("\nrepaired and committed")
			return nil
		},
	}
	c.Flags().BoolVar(&apply, "apply", false, "write the restores and commit (default: dry-run report only)")
	return c
}

// checkStaleState reports whether state.json at statePath is missing or
// clearly stale — its current_phase disagrees with the checked-out
// phase/<id> branch — and, when it is, the reconstructed replacement to
// write. A detached HEAD or a branch not named phase/<id> carries no
// staleness signal of its own; an unparseable or absent state.json is
// always stale regardless.
func checkStaleState(repoDir, root, statePath, mainBranch string) (stale bool, reconstructed *state.State, err error) {
	var branchPhase string
	if branch, berr := gitTrim(repoDir, "symbolic-ref", "--short", "HEAD"); berr == nil {
		if id, ok := strings.CutPrefix(branch, "phase/"); ok {
			branchPhase = id
		}
	}

	existing, loadErr := state.Load(statePath)
	switch {
	case loadErr != nil:
		stale = true
	case branchPhase != "" && existing.CurrentPhase != branchPhase:
		stale = true
	}
	if !stale {
		return false, nil, nil
	}
	s, rerr := reconstructState(repoDir, root, mainBranch)
	if rerr != nil {
		return false, nil, rerr
	}
	return true, s, nil
}

// reportRepairFindings prints one line per finding, matching doctor's ✗
// remediation-hint style.
func reportRepairFindings(clobbered []ClobberedFile, missingDirs []string, stale bool) {
	Print("Findings:")
	for _, f := range clobbered {
		if f.Missing {
			Printf("  ✗ %s — missing\n", f.Path)
		} else {
			Printf("  ✗ %s — diverged from HEAD\n", f.Path)
		}
	}
	for _, id := range missingDirs {
		Printf("  ✗ %s — phase dir known to origin but absent from working tree\n", RootDirName+"/phases/"+id)
	}
	if stale {
		Printf("  ✗ %s — missing or stale, will be reconstructed from git history\n", RootDirName+"/"+state.File)
	}
}
