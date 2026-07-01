package cmd

import (
	"path/filepath"

	"github.com/Rivil/dross/internal/project"
	"github.com/Rivil/dross/internal/state"
)

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
