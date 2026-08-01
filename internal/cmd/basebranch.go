package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

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

// pushBaseIfAheadDrossOnly is the ship / phase-complete pre-flight safety net
// (the chore_push locked decision): .dross-only chores committed to the local
// base branch — a pause auto-snapshot, a gate auto-commit, a recovery restore —
// sit unpushed and re-seed base divergence at the next squash-merge. Local-only
// writers like pause never push; the commands already doing network absorb it.
//
// After a fetch it examines rev-list origin/<base>..<base>:
//   - empty (or origin/<base> doesn't exist yet) → no-op
//   - every ahead commit touches only .dross/ paths → git push origin <base>
//   - any ahead commit touches a non-.dross path → refuse and push nothing;
//     unpushed code on the base is a real divergence the user must reconcile
//
// A failed push is a hard error (the push_failure locked decision):
// proceeding past it would re-seed the exact divergence this exists to kill.
func pushBaseIfAheadDrossOnly(repoDir, base string) (pushed bool, err error) {
	if out, err := gitCombined(repoDir, "fetch", "origin"); err != nil {
		return false, fmt.Errorf("git fetch: %w\n%s", err, out)
	}
	if gitNoOut(repoDir, "rev-parse", "--verify", "refs/remotes/origin/"+base) != nil {
		return false, nil // no origin/<base> to be ahead of
	}
	ahead, err := gitTrim(repoDir, "rev-list", "origin/"+base+".."+base)
	if err != nil {
		return false, fmt.Errorf("git rev-list origin/%s..%s: %w", base, base, err)
	}
	if ahead == "" {
		return false, nil
	}
	behind, err := gitTrim(repoDir, "rev-list", base+"..origin/"+base)
	if err != nil {
		return false, fmt.Errorf("git rev-list %s..origin/%s: %w", base, base, err)
	}
	if behind != "" {
		// True divergence (ahead AND behind): a push can't fast-forward, and
		// the ff-only / --recover machinery downstream owns this state with
		// its own guided errors — the safety net stays out of it. It only
		// handles the purely-ahead base, where origin/<base> is an ancestor.
		return false, nil
	}
	for _, sha := range strings.Fields(ahead) {
		files, err := gitTrim(repoDir, "diff-tree", "--no-commit-id", "--name-only", "-r", "--root", sha)
		if err != nil {
			return false, fmt.Errorf("git diff-tree %s: %w", sha, err)
		}
		for _, f := range strings.Split(files, "\n") {
			if f == "" {
				continue
			}
			if !underDross(unquotePath(f)) {
				return false, fmt.Errorf("local %s is ahead of origin/%s with commits touching non-.dross paths (e.g. %s in %.7s) — "+
					"push or reconcile %s manually before continuing; refusing to push it for you",
					base, base, f, sha, base)
			}
		}
	}
	if out, err := gitCombined(repoDir, "push", "origin", base); err != nil {
		return false, fmt.Errorf("safety-net push of .dross chores on %s failed: %w\n%s\n"+
			"Refusing to continue — proceeding would leave %s diverged from origin again.",
			base, err, out, base)
	}
	return true, nil
}

// pushQuickBaseIfRecorded extends the chore_push safety net to the OTHER
// branch a repo can accumulate unpushed .dross chores on: the one a standalone
// `/dross-quick` committed to, recorded as quick_base in .dross/local.toml.
// The phase base is handled by the caller's own pushBaseIfAheadDrossOnly; this
// covers a quick task that landed on a different branch (main, while the phase
// was forked off a milestone branch), whose chores would otherwise sit unpushed
// and re-seed divergence at the next squash-merge.
//
// It is driven by the record, never by inference — the whole point of storing
// the branch quick actually used. Three no-ops: no record, a record equal to
// the phase base (already pushed, and pushing twice is pointless), and a record
// naming a branch with no local ref (a stale machine-local value, which is
// expected: the store is gitignored and is simply overwritten by the next
// standalone quick rather than cleared on success).
//
// A refusal from the underlying net is wrapped so it names the record as the
// source — otherwise the user sees a branch they never mentioned to this
// command and has no idea where it came from.
func pushQuickBaseIfRecorded(repoDir, root, phaseBase string) (pushed bool, branch string, err error) {
	qb := readLocalKey(root, "quick_base")
	if qb == "" || qb == phaseBase {
		return false, "", nil
	}
	if gitNoOut(repoDir, "rev-parse", "--verify", "refs/heads/"+qb) != nil {
		return false, "", nil
	}
	pushed, err = pushBaseIfAheadDrossOnly(repoDir, qb)
	if err != nil {
		return false, qb, fmt.Errorf("%w\n(%s is the quick_base recorded in .dross/local.toml — the branch a standalone quick task committed to)", err, qb)
	}
	return pushed, qb, nil
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
