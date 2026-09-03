package cmd

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Rivil/dross/internal/project"
	"github.com/Rivil/dross/internal/remote"
)

// remoteExecFn is the install seam.
//
// A package-level var for the same reason remoteProbeFn is one: bootstrap is a
// command that CHANGES another machine, and every test of it has to be able to
// assert what it would have run without running it. Production never reassigns
// it, and it goes through remote.Exec so the argv gets the transport's own
// quoting rather than a shell line built here (locked install_transport).
var remoteExecFn = remote.Exec

// remoteBootstrap registers `dross remote bootstrap`.
//
// Dry-run by default, --apply writes. The default is the safe one because the
// command's whole job is to modify a machine that is not this one: a verb that
// installed on sight would make "let me see what it would do" impossible to ask.
func remoteBootstrap() *cobra.Command {
	var apply bool
	c := &cobra.Command{
		Use:   "bootstrap",
		Short: "Install the mutation and lane toolchains the granted host is missing",
		Long: "Probes the granted host for the tools this repo's configured mutation\n" +
			"adapters AND its declared test lanes need, and installs the ones it can.\n" +
			"One probe, one plan, one vocabulary: a host's readiness for lanes and for\n" +
			"mutation is never two separate answers.\n\n" +
			"It installs adapter PACKAGES into a runtime that is already there —\n" +
			"gremlins via `go install`, pinned. It never installs a language runtime:\n" +
			"a host missing go, node or dotnet is reported by name so its owner can\n" +
			"install it, because that is version policy and PATH ownership, not a\n" +
			"decision a mutation run gets to make.\n\n" +
			"Dry-run by default: it prints what it would run and changes nothing.\n" +
			"Pass --apply to install. Re-running over a provisioned host does nothing.",
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			root, err := FindRoot()
			if err != nil {
				return err
			}
			p, err := project.Load(filepath.Join(root, project.File))
			if err != nil {
				return err
			}
			// The probe set comes from the same derivation the plan uses.
			// Asking a different question is how a surface ends up agreeing
			// about the host and disagreeing about what it holds.
			tools, _, _ := remoteProbeTools(p)
			if len(tools) == 0 {
				// Names BOTH sources, because a repo can be empty for two
				// reasons and a message naming only adapters would tell a
				// lanes-only repo it has no lanes.
				//
				// Answered before any probe: a repo with nothing to install has
				// no question to ask a host, and asking anyway would make an
				// unreachable pool fail a command that had no work to do.
				Print("no mutation adapters and no test lanes configured — nothing to bootstrap")
				return nil
			}
			targets, err := readRemoteGrants(root, filepath.Dir(root))
			if err != nil {
				return err
			}
			if len(targets) == 0 {
				// Named, not merely reported: the user's next move is the grant,
				// and an error that only says "no remote" sends them looking.
				return fmt.Errorf("no remote granted — nothing to bootstrap.\nGrant one with `dross remote grant <host> <workdir>`")
			}
			// EVERY candidate, not the one a run would choose (c-3). Per-lane
			// routing assumes the pool is provisioned; bringing up only the
			// first host leaves the rest exactly as they were, and the next
			// run's lane still comes home for a tool nothing installed.
			pool, err := probeEveryCandidate(targets, bootstrapProbeSet(tools))
			if err != nil {
				return err
			}
			if len(pool.Candidates) == 0 {
				// Unreachable is not ungranted, and the remedies differ. An
				// error that said "no remote granted" would send the user to
				// re-grant hosts that are already authorized and merely down.
				return fmt.Errorf("no granted host answered — nothing to bootstrap.\n%s", pool.Why)
			}
			return bootstrapPool(p, targets, pool, apply)
		},
	}
	c.Flags().BoolVar(&apply, "apply", false, "actually install (default is a dry run that changes nothing)")
	return c
}

// bootstrapPool provisions every granted candidate, in declared order, and
// reports each one on its own.
//
// The per-host report is the point of the verb once a pool exists (c-3). A
// single merged outcome cannot say what the user has to act on: a tool that
// installed on alpha and was refused on beta is two different situations with
// two different remedies, and folding them into one line hides whichever is
// worse behind whichever is louder.
//
// An unreachable candidate is REPORTED and skipped, never fatal. Aborting on it
// would leave the live hosts exactly as unprovisioned as the dead one, which is
// the opposite of what the user asked for — but it still moves the exit code,
// because a pool with a host nobody could reach is not provisioned.
//
// Each candidate is planned from ITS OWN readiness, taken by the walk above.
// One plan reused across hosts would install onto a machine that already had
// the tool and skip one that did not.
func bootstrapPool(p *project.Project, targets []*remote.Target, pool remotePool, apply bool) error {
	ready := map[string]remote.Readiness{}
	for _, c := range pool.Candidates {
		ready[c.Target.Host] = c.Ready
	}
	why := map[string]string{}
	for _, m := range pool.Unreached {
		why[m.Target.Host] = m.Why
	}

	var failures []string
	first := true
	for _, t := range targets {
		if !first {
			Print("")
		}
		first = false
		if reason, missed := why[t.Host]; missed {
			// Named with its reason, in the same list as the hosts that
			// answered: a candidate silently absent from the report reads as
			// one that had nothing to do.
			Printf("remote %s — could not be reached: %s\n", t.Host, reason)
			failures = append(failures, t.Host)
			continue
		}
		steps, err := planRemoteBootstrap(p, ready[t.Host])
		if err != nil {
			return err
		}
		if len(steps) == 0 {
			Printf("remote %s — nothing to bootstrap\n", t.Host)
			continue
		}
		if err := reportBootstrap(*t, steps, apply); err != nil {
			// Recorded and carried, not returned: one host's refusals must not
			// stop the pool, and a candidate never attempted is a candidate
			// still missing the toolchain the next run assumes.
			failures = append(failures, t.Host)
		}
	}
	if len(failures) == 0 {
		return nil
	}
	return &ExitCodeError{Code: 1, Err: fmt.Errorf(
		"the pool is not fully provisioned — %s", strings.Join(failures, ", "))}
}

// reportBootstrap prints each step and, under apply, runs the installable ones.
//
// One failure never aborts the rest. The steps are independent tools, and
// stopping at the first would leave a host half-provisioned for reasons that had
// nothing to do with the tools that were never attempted — while still exiting
// non-zero so nothing reads the run as clean.
func reportBootstrap(target remote.Target, steps []bootstrapStep, apply bool) error {
	Printf("remote %s — %s\n", target.Host, bootstrapMode(apply))
	installed, refused, failed, unreported := 0, 0, 0, 0

	for _, s := range steps {
		switch {
		case s.Present:
			Printf("  ✓ %s (%s) already installed\n", s.Tool, s.origin())
		case s.Refusal != "":
			// A refusal is not an install failure, and the two must not be
			// reported as one thing: the remedies are different and only one of
			// them is dross's to perform.
			Printf("  ✗ %s (%s) refused — %s\n", s.Tool, s.origin(), s.Refusal)
			refused++
		case s.Unknown:
			// Printed, never counted (locked undeclared_exit). A declared lane
			// with no recipe and no install line is a gap in THIS repo's
			// configuration rather than a failure of the host, and exiting 1 on
			// it would make every repo with lanes start failing a command that
			// passed the day before.
			Printf("  · %s (%s) — %s\n", s.Tool, s.origin(), s.Note)
			unreported++
		case !apply:
			Printf("  → %s (%s) would install: %s\n", s.Tool, s.origin(), shellPreview(s.Argv))
		default:
			Printf("  → %s (%s) installing: %s\n", s.Tool, s.origin(), shellPreview(s.Argv))
			if _, err := remoteExecFn(target, s.Argv); err != nil {
				Printf("    ✗ %v\n", err)
				failed++
				continue
			}
			installed++
		}
	}

	switch {
	case !apply && refused == 0:
		Print("\nDry run — nothing was changed. Re-run with --apply to install.")
	case !apply:
		Print("\nDry run — nothing was changed. The refusals above need the host's owner.")
	default:
		Printf("\n%d installed, %d refused, %d failed.\n", installed, refused, failed)
	}
	if unreported > 0 {
		Printf("%d lane tool(s) have no install recipe and no install line — add one with `dross test lane edit <name> --install \"<cmd>\"`.\n", unreported)
	}
	if refused > 0 || failed > 0 {
		return &ExitCodeError{Code: 1, Err: fmt.Errorf("%d tool(s) refused, %d failed — the host is not fully provisioned", refused, failed)}
	}
	return nil
}

func bootstrapMode(apply bool) string {
	if apply {
		return "installing"
	}
	return "dry run (pass --apply to install)"
}

// shellPreview renders an argv for reading. It is display only — the argv the
// transport receives is the slice, never this string.
func shellPreview(argv []string) string {
	out := ""
	for i, a := range argv {
		if i > 0 {
			out += " "
		}
		out += a
	}
	return out
}
