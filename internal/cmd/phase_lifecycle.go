package cmd

import (
	"errors"
	"fmt"
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
