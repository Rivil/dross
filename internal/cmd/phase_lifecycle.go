package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Rivil/dross/internal/milestone"
	"github.com/Rivil/dross/internal/phase"
	"github.com/Rivil/dross/internal/state"
)

// resolveAnchor enforces the locked input_validation decision: exactly one of
// --after / --before. It returns the anchor slug and whether placement is
// before it.
func resolveAnchor(after, before string) (anchor string, isBefore bool, err error) {
	switch {
	case after != "" && before != "":
		return "", false, errors.New("pass exactly one of --after or --before, not both")
	case after == "" && before == "":
		return "", false, errors.New("pass exactly one of --after or --before")
	case before != "":
		return before, true, nil
	default:
		return after, false, nil
	}
}

// loadCurrentMilestone resolves the current milestone from state and loads it.
// Lifecycle commands operate within a single milestone's phases array, so a
// missing current milestone is a usage error.
func loadCurrentMilestone(root string) (version string, m *milestone.Milestone, path string, err error) {
	s, err := state.Load(filepath.Join(root, state.File))
	if err != nil {
		return "", nil, "", err
	}
	if s.CurrentMilestone == "" {
		return "", nil, "", errors.New("no current milestone set; lifecycle commands operate within a milestone's phases array")
	}
	path = milestone.FilePath(root, s.CurrentMilestone)
	m, err = milestone.Load(path)
	if err != nil {
		return "", nil, "", fmt.Errorf("load milestone %s: %w", s.CurrentMilestone, err)
	}
	return s.CurrentMilestone, m, path, nil
}

// refuseIfShipped blocks a lifecycle mutation when the phase has a live origin
// phase/<slug> branch — the open-PR window of the locked inflight_guard
// decision. It mirrors phaseComplete's ls-remote model: unstarted/planning
// phases have no remote branch, so this only fires once a phase is shipped and
// not yet merged+branch-deleted. A repo with no git or no reachable origin
// can't be shipped, so it's a no-op there.
func refuseIfShipped(repoDir, slug string) error {
	if !isDir(filepath.Join(repoDir, ".git")) {
		return nil
	}
	branch := "phase/" + slug
	out, err := gitTrim(repoDir, "ls-remote", "--heads", "origin", branch)
	if err != nil {
		return nil // no origin / unreachable — can't prove it's shipped, don't block
	}
	if strings.TrimSpace(out) != "" {
		return fmt.Errorf("phase %q has a live origin branch %s (open PR) — merge or close it before move/rename", slug, branch)
	}
	return nil
}

// phaseMove reorders an existing phase within the current milestone's phases
// array. Only the array changes — no phase directory, id, branch, or artifact
// is touched.
func phaseMove() *cobra.Command {
	var after, before string
	c := &cobra.Command{
		Use:   "move <slug>",
		Short: "Reorder a phase within its milestone's phases array",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			slug := args[0]
			anchor, isBefore, err := resolveAnchor(after, before)
			if err != nil {
				return err
			}
			root, err := FindRoot()
			if err != nil {
				return err
			}
			repoDir := filepath.Dir(root)

			version, m, mPath, err := loadCurrentMilestone(root)
			if err != nil {
				return err
			}
			if !slices.Contains(m.Phases, slug) {
				return fmt.Errorf("phase %q is not in milestone %s", slug, version)
			}
			if err := refuseIfShipped(repoDir, slug); err != nil {
				return err
			}

			next, err := phase.MoveRelative(m.Phases, slug, anchor, isBefore)
			if err != nil {
				if errors.Is(err, phase.ErrAnchorNotFound) {
					return fmt.Errorf("anchor %q is not in milestone %s", anchor, version)
				}
				return err
			}
			// No-op check, ordered before any write: MoveRelative returns the
			// input unchanged when slug is already in place.
			if slices.Equal(next, m.Phases) {
				Printf("%s already in place — nothing to do\n", slug)
				return nil
			}
			m.Phases = next
			if err := m.Save(mPath); err != nil {
				return err
			}
			Printf("moved %s → position %d in %s\n", slug, phase.DisplayNumber(m.Phases, slug), version)
			return nil
		},
	}
	c.Flags().StringVar(&after, "after", "", "place <slug> immediately after this phase slug")
	c.Flags().StringVar(&before, "before", "", "place <slug> immediately before this phase slug")
	return c
}

// phaseInsert scaffolds a new phase (directory + phase/<slug> branch + active
// state) and splices it into the current milestone's array at the anchor.
// Unlike phase create it uses a STRICT slug (phase.Slugify, no UniqueSlug
// auto-suffix — the locked input_validation decision) and refuses a collision,
// and it splices at the anchor instead of appending at the tail.
func phaseInsert() *cobra.Command {
	var after, before string
	c := &cobra.Command{
		Use:   "insert <title>",
		Short: "Scaffold a phase and place it at a position in the milestone array",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			title := strings.Join(args, " ")
			anchor, isBefore, err := resolveAnchor(after, before)
			if err != nil {
				return err
			}
			root, err := FindRoot()
			if err != nil {
				return err
			}
			repoDir := filepath.Dir(root)

			version, m, mPath, err := loadCurrentMilestone(root)
			if err != nil {
				return err
			}

			id := phase.Slugify(title)
			if id == "" {
				return fmt.Errorf("title %q produces an empty slug", title)
			}
			// Strict collision refusal (no auto-suffix), checked BEFORE any
			// scaffolding so a refusal leaves no stray directory.
			if isDir(phase.Dir(root, id)) {
				return fmt.Errorf("phase %q already exists (phases/%s) — choose another title or rename it first", id, id)
			}
			if slices.Contains(m.Phases, id) {
				return fmt.Errorf("phase %q is already in milestone %s", id, version)
			}
			if !slices.Contains(m.Phases, anchor) {
				return fmt.Errorf("anchor %q is not in milestone %s", anchor, version)
			}

			// Scaffold: reuse phase create's preflight + mkdir + checkout-b steps
			// (NOT its tail-append). Side effects only after every check passes.
			branchName := "phase/" + id
			hasGit := isDir(filepath.Join(repoDir, ".git"))
			if hasGit {
				if err := preflightPhaseBranch(repoDir, branchName); err != nil {
					return err
				}
			}
			dir := phase.Dir(root, id)
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return err
			}
			if hasGit {
				if out, err := gitCombined(repoDir, "checkout", "-b", branchName); err != nil {
					_ = os.Remove(dir)
					return fmt.Errorf("git checkout -b %s: %w\n%s", branchName, err, out)
				}
			}

			// Splice into the array at the anchor (existence already checked).
			next, err := phase.InsertRelative(m.Phases, id, anchor, isBefore)
			if err != nil {
				return err
			}
			m.Phases = next
			if err := m.Save(mPath); err != nil {
				return err
			}

			sPath := filepath.Join(root, state.File)
			s, err := state.Load(sPath)
			if err != nil {
				return err
			}
			s.CurrentPhase = id
			s.CurrentPhaseStatus = "created"
			s.Touch(fmt.Sprintf("inserted %s", id))
			if err := s.Save(sPath); err != nil {
				return err
			}

			Printf("inserted %s at position %d in %s\n", id, phase.DisplayNumber(m.Phases, id), version)
			if hasGit {
				Printf("checked out %s\n", branchName)
			}
			Print("Next: /dross-spec to write spec.toml, then /dross-plan")
			return nil
		},
	}
	c.Flags().StringVar(&after, "after", "", "place the new phase immediately after this phase slug")
	c.Flags().StringVar(&before, "before", "", "place the new phase immediately before this phase slug")
	return c
}

// phaseRename renames a phase end to end: its directory, spec/plan id, milestone
// array entry, any deferred targets pointing at it, and the local git branch.
// Other phases are left byte-for-byte untouched.
func phaseRename() *cobra.Command {
	c := &cobra.Command{
		Use:   "rename <old-slug> <new-slug>",
		Short: "Rename a phase: directory, spec id, milestone entry, deferred targets, branch",
		Args:  cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			oldSlug, newSlug := args[0], args[1]
			root, err := FindRoot()
			if err != nil {
				return err
			}
			repoDir := filepath.Dir(root)

			version, m, mPath, err := loadCurrentMilestone(root)
			if err != nil {
				return err
			}
			if !slices.Contains(m.Phases, oldSlug) {
				return fmt.Errorf("phase %q is not in milestone %s", oldSlug, version)
			}
			// No-op, special-cased BEFORE the target-exists check so renaming a
			// phase to its own name succeeds quietly and writes nothing.
			if newSlug == oldSlug {
				Printf("%s already named that — nothing to do\n", oldSlug)
				return nil
			}
			if newSlug == "" || phase.Slugify(newSlug) != newSlug {
				return fmt.Errorf("new slug %q is not a valid slug", newSlug)
			}
			if err := refuseIfShipped(repoDir, oldSlug); err != nil {
				return err
			}
			// Target-exists check BEFORE the directory move: if it ran after, a
			// collision would leave phases/<old> already gone (a partial rename).
			if isDir(phase.Dir(root, newSlug)) || slices.Contains(m.Phases, newSlug) {
				return fmt.Errorf("phase %q already exists — choose another name", newSlug)
			}

			oldDir := phase.Dir(root, oldSlug)
			newDir := filepath.Join(root, "phases", newSlug)
			if err := os.Rename(oldDir, newDir); err != nil {
				return fmt.Errorf("rename %s → %s: %w", oldDir, newDir, err)
			}
			if err := rewritePhaseID(newDir, newSlug); err != nil {
				return err
			}
			m.Phases = phase.RenameInArray(m.Phases, oldSlug, newSlug)
			if err := m.Save(mPath); err != nil {
				return err
			}
			if err := repointDeferredTarget(root, oldSlug, newSlug); err != nil {
				return err
			}

			// Rename the local branch when it exists; never touch remotes.
			branchOld, branchNew := "phase/"+oldSlug, "phase/"+newSlug
			if isDir(filepath.Join(repoDir, ".git")) {
				if err := gitNoOut(repoDir, "rev-parse", "--verify", "refs/heads/"+branchOld); err == nil {
					if out, err := gitCombined(repoDir, "branch", "-m", branchOld, branchNew); err != nil {
						return fmt.Errorf("git branch -m %s %s: %w\n%s", branchOld, branchNew, err, out)
					}
				}
			}

			// Follow the rename in state when the renamed phase was current.
			sPath := filepath.Join(root, state.File)
			if s, err := state.Load(sPath); err == nil && s.CurrentPhase == oldSlug {
				s.CurrentPhase = newSlug
				s.Touch(fmt.Sprintf("renamed %s → %s", oldSlug, newSlug))
				if err := s.Save(sPath); err != nil {
					return err
				}
			}

			Printf("renamed %s → %s in %s\n", oldSlug, newSlug, version)
			return nil
		},
	}
	return c
}
