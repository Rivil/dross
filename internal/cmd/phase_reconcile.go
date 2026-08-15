package cmd

import (
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Rivil/dross/internal/phase"
	"github.com/Rivil/dross/internal/state"
)

// `dross phase reconcile` — clear every merged-but-uncompleted phase in one
// verb.
//
// It exists because `dross status` and the watch digest were leaking a chore
// list: N lines of `dross phase complete <id>` for the user to hand-execute,
// one per phase whose PR had merged while nobody ran the completion. A tool
// that can name the work can do the work.
//
// It is a CONVENIENCE over the existing gate, never a way around it. Each phase
// goes through the same `dross phase complete` — same merge confirmation, same
// recorded-base resolution, same guarded branch switches — and a phase whose
// merge cannot be confirmed is reported and skipped rather than completed. A
// batch verb that relaxed the gate would be worse than the chore list it
// replaces, because it would do the unsafe thing at N times the rate.
func phaseReconcile() *cobra.Command {
	return &cobra.Command{
		Use:   "reconcile",
		Short: "Complete every merged-but-uncompleted phase",
		Long: `Run ` + "`dross phase complete`" + ` for each phase that looks unfinished.

A phase is a candidate when its phase/<id> branch still exists locally and
state history carries no "completed <id>" record. Whether it is actually
finished is decided by the same merge gate a single completion uses — this
verb only saves you typing the ids.

A phase whose merge cannot be confirmed is reported and skipped; the rest
still complete. Exits 0 when there is nothing to do.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			root, err := FindRoot()
			if err != nil {
				return err
			}
			repoDir := filepath.Dir(root)
			s, err := state.Load(filepath.Join(root, state.File))
			if err != nil {
				return err
			}

			ids, err := reconcilablePhases(root, repoDir, s)
			if err != nil {
				return err
			}
			if len(ids) == 0 {
				// The ordinary state of a tidy repo. Not a failure.
				Print("nothing to reconcile — no phase branch is waiting on a completion")
				return nil
			}

			Printf("%d phase(s) to reconcile: %s\n\n", len(ids), strings.Join(ids, ", "))
			completed, skipped := 0, 0
			for _, id := range ids {
				Printf("--- %s\n", id)
				// A fresh command per phase: completion reloads state and the
				// phase's own records, and the previous iteration moved HEAD.
				if err := runSubcommand(cmd, phaseComplete(), id); err != nil {
					// Reported and survived, not fatal. One phase whose PR is
					// still open would otherwise block the verb forever, which
					// is exactly the state that produces the chore list.
					Printf("    skipped: %v\n", err)
					skipped++
					continue
				}
				completed++
			}
			Printf("\nreconciled %d, skipped %d\n", completed, skipped)
			return nil
		},
	}
}

// runSubcommand executes a freshly built command with args, wired to the
// parent's streams. Kept as a seam so the reconcile loop's own behaviour —
// continue-past-failure, per-phase reporting — is testable without a live
// forge.
var runSubcommand = func(parent *cobra.Command, sub *cobra.Command, args ...string) error {
	sub.SetArgs(args)
	sub.SetOut(parent.OutOrStdout())
	sub.SetErr(parent.ErrOrStderr())
	sub.SilenceUsage = true
	sub.SilenceErrors = true
	return sub.Execute()
}

// reconcilablePhases lists phases that still have a local branch and no
// completion record, in phase order.
//
// Deliberately permissive: this is a CANDIDATE list, not a verdict. Asking
// "has its PR merged?" here would duplicate the gate — and a second
// implementation of a merge check is exactly how the two come to disagree.
// `dross phase complete` answers that question, once, for real.
func reconcilablePhases(root, repoDir string, s *state.State) ([]string, error) {
	ids, err := phase.List(root)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, id := range ids {
		if stateHasCompletedPhase(s, id) {
			continue
		}
		if gitNoOut(repoDir, gitRefArgs("rev-parse", []string{"--verify", "--quiet"}, "refs/heads/phase/"+id)...) != nil {
			continue // no branch left: nothing to tear down
		}
		out = append(out, id)
	}
	return out, nil
}

// stateHasCompletedPhase reports whether state history carries the
// "completed <id>" record `dross phase complete` writes once a merge is
// confirmed. Same string the watch drift classifier keys on.
func stateHasCompletedPhase(s *state.State, id string) bool {
	if s == nil {
		return false
	}
	for _, a := range s.History {
		if strings.Contains(a.Action, "completed "+id) {
			return true
		}
	}
	return false
}
