package cmd

// `dross phase red-proof repoint` — the repair verb for a rotted pin.
//
// A sibling of `red-proof set` rather than a `doctor --fix`, per the locked
// repoint_surface decision: writer verbs for the same noun belong together, and
// a diagnostic that repairs is a diagnostic people stop trusting to be
// read-only. Doctor's hint names this command; doctor still writes nothing.
//
// Dry-run by default (c-4). The repair rewrites a tracked prose doc, and a verb
// that edits committed prose the first time it is typed is one people run once
// and then work around. `--apply` is the whole difference.
//
// It does not commit (locked repoint_commit): the caller does. The rewrite is
// an ordinary source-tree change and belongs in a reviewed diff.

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

func phaseRedProofRepoint() *cobra.Command {
	var apply bool
	c := &cobra.Command{
		Use:   "repoint [phase-id]",
		Short: "Repair red-proof pins whose commit origin can no longer reach",
		Long: "Rewrites a rotted pin — one naming a commit no refs/remotes/origin/* ref\n" +
			"contains — to the owning phase's fork point, updating both the record and\n" +
			"the replay doc so they keep agreeing. A sound pin is left alone.\n\n" +
			"With no argument every discovered pin is scanned; a phase-id narrows it to\n" +
			"one. Dry-run by default: pass --apply to write. Where a replay command is\n" +
			"recorded and consented, it is re-run at the proposed commit and the repair\n" +
			"is refused unless the proof still goes red.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			root, err := FindRoot()
			if err != nil {
				return err
			}
			repoDir := filepath.Dir(root)

			pins, err := discoverRedProofPins(root)
			if err != nil {
				return err
			}
			if len(args) == 1 {
				pins = pinsForPhase(pins, args[0])
				if len(pins) == 0 {
					return fmt.Errorf("phase %q records no red proof to repoint", args[0])
				}
			}
			if len(pins) == 0 {
				Print("no red proofs recorded — nothing to repoint")
				return nil
			}

			var repaired, refused int
			for _, pin := range pins {
				if repointOnePin(root, repoDir, pin, apply) {
					repaired++
				} else {
					refused++
				}
			}
			// Reported after the loop, not instead of it: one refusal must not
			// cost the other pins their repair (a blanket scan that aborted on
			// the first bad pin would leave the rest rotted for no reason).
			if refused > 0 {
				return fmt.Errorf("%d of %d red-proof pins could not be repaired — see the refusals above", refused, len(pins))
			}
			if !apply && repaired > 0 {
				Print("\ndry-run — pass --apply to write these repairs")
			}
			return nil
		},
	}
	c.Flags().BoolVar(&apply, "apply", false, "write the repairs; without it the run only reports what it would do")
	return c
}

// repointOnePin handles one pin end to end, printing its outcome. It returns
// false only for a REFUSAL — something that should move the exit code. A
// nothing-to-do pin returns true: leaving a sound pin alone is a success.
func repointOnePin(root, repoDir string, pin redProofPin, apply bool) bool {
	plan, err := planRedProofRepoint(root, repoDir, pin, nil)
	if err != nil {
		Printf("%s: refused — %v\n", pin.Phase, err)
		return false
	}
	if plan.Verdict != repointRepair {
		Printf("%s: nothing to do — %s\n", pin.Phase, plan.Why)
		return true
	}

	Printf("%s: %s pins %s, which has rotted — %s\n", pin.Phase, plan.Doc, plan.OldSHA, plan.Why)
	Printf("  proposed: %s (%s's fork point)\n", plan.NewSHA, pin.Phase)
	verb := "would write"
	if apply {
		verb = "writes"
	}
	Printf("  %s:\n", verb)
	for _, f := range plan.Files {
		Printf("    %s\n", f)
	}

	if !apply {
		// A dry run must not spawn anything: reporting a proposal is not
		// consent to execute a command on the strength of it.
		if plan.Replay != "" {
			Printf("  replay: %s (not run in a dry run)\n", plan.Replay)
		} else {
			Print("  replay: none recorded — an --apply run would report the repair unverified")
		}
		return true
	}

	note, ok := checkReplayBeforeRepoint(root, repoDir, plan)
	if !ok {
		Printf("%s: refused — %s\n", pin.Phase, note)
		return false
	}
	if err := applyRedProofRepoint(plan); err != nil {
		Printf("%s: refused — %v\n", pin.Phase, err)
		return false
	}
	Printf("  repointed %s -> %s\n", short(plan.OldSHA), short(plan.NewSHA))
	Printf("  %s\n", note)
	return true
}

// checkReplayBeforeRepoint runs the recorded replay at the proposed commit and
// decides whether the repair may proceed, returning the line to print about it.
//
// The three states are the locked replay_consent_states decision:
//
//   - recorded AND consented: run it. Red proceeds; GREEN refuses, because a
//     proof that no longer reproduces at the proposed commit is not the proof
//     being moved.
//   - recorded but not consented, or not recorded at all: proceed, and say the
//     repair is UNVERIFIED. Refusing here would make a rotted pin unrepairable
//     on a fresh clone until the operator granted a command they may not want
//     to run — which would make the single-command promise false exactly where
//     it matters most.
//   - could not be run (worktree failure, spawn error, timeout): REFUSE. An
//     error is not evidence the proof went red, and treating it as one is how a
//     repair gets recorded for a proof nobody checked.
func checkReplayBeforeRepoint(root, repoDir string, plan redProofRepointPlan) (string, bool) {
	if strings.TrimSpace(plan.Replay) == "" {
		return "unverified: no replay command is recorded for this phase, so nothing re-checked the proof at the proposed commit", true
	}
	res, err := runRedProofReplay(root, repoDir, plan.NewSHA, plan.Replay)
	switch {
	case errors.Is(err, ErrNoReplayConsent):
		return fmt.Sprintf("unverified: this machine has not consented to the recorded replay, so nothing re-checked the proof — grant it with `dross trust --replay %s` and re-run", plan.Phase), true
	case err != nil:
		return fmt.Sprintf("the replay could not be run at %s, and an error is not evidence the proof went red: %v", short(plan.NewSHA), err), false
	case !res.Red:
		return fmt.Sprintf("the replay did NOT go red at %s (exit 0) — the proof does not reproduce there, so moving the pin onto it would record a proof that no longer exists:\n%s", short(plan.NewSHA), res.Tail), false
	}
	return fmt.Sprintf("verified: the replay went red at %s (exit %d)", short(plan.NewSHA), res.ExitCode), true
}

// pinsForPhase narrows a scan to one phase.
func pinsForPhase(pins []redProofPin, phaseID string) []redProofPin {
	var out []redProofPin
	for _, p := range pins {
		if p.Phase == phaseID {
			out = append(out, p)
		}
	}
	return out
}
