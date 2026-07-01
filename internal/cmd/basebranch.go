package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/Rivil/dross/internal/project"
	"github.com/Rivil/dross/internal/state"
)

// BaseBranch prints the branch that new phase/quick work should fork off (and
// that ship should target): the active milestone's integration branch when it
// exists, else the configured main branch. stdout carries only the bare branch
// name so callers can consume `$(dross base-branch)`; the scope-a-milestone
// nudge (no active milestone) goes to stderr to keep stdout clean.
func BaseBranch() *cobra.Command {
	return &cobra.Command{
		Use:   "base-branch",
		Short: "Print the branch new phase/quick work should fork off",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			root, err := FindRoot()
			if err != nil {
				return err
			}
			base, milestoneActive, err := resolveNewWorkBase(filepath.Dir(root), root)
			if err != nil {
				return err
			}
			Print(base)
			// Nudge only when there's no active milestone at all — never in
			// the cutover case (a milestone is set but predates the branch
			// model), matching phase create's fallback nudge.
			if !milestoneActive {
				if s, err := state.Load(filepath.Join(root, state.File)); err == nil && s.CurrentMilestone == "" {
					fmt.Fprintf(os.Stderr, "no milestone active — rooted on %s; scope one with `dross milestone <version>` for integration branching\n", base)
				}
			}
			return nil
		},
	}
}

// resolveNewWorkBase decides the branch that new phase/quick work should fork
// off (and that ship should target). It returns the active milestone's
// integration branch — milestone/<current_milestone> — but only when that ref
// actually exists in repoDir; otherwise it falls back to the configured main
// branch.
//
// This single existence check is where two locked v0.7 decisions live:
//   - rollout_cutover: a milestone scoped before the branch model shipped has
//     no milestone/<version> ref, so it transparently falls back to main. The
//     branch's existence IS the switch — no retrofit, no stored flag/date.
//   - no_milestone_fallback: with no current_milestone at all, work roots on
//     main.
//
// milestoneActive reports whether the milestone branch was chosen, so callers
// can tailor messaging (e.g. the scope-a-milestone nudge on the fallback path).
func resolveNewWorkBase(repoDir, root string) (base string, milestoneActive bool, err error) {
	p, err := project.Load(filepath.Join(root, project.File))
	if err != nil {
		return "", false, err
	}
	main := p.Repo.GitMainBranch
	if main == "" {
		main = "main"
	}

	s, err := state.Load(filepath.Join(root, state.File))
	if err != nil {
		return "", false, err
	}
	if s.CurrentMilestone == "" {
		return main, false, nil
	}

	branch := "milestone/" + s.CurrentMilestone
	// The ref-existence probe is the cutover mechanism: a pre-cutover
	// milestone (or a non-git dir) has no such ref, so we fall back to main.
	if gitNoOut(repoDir, "rev-parse", "--verify", "refs/heads/"+branch) != nil {
		return main, false, nil
	}
	return branch, true, nil
}
