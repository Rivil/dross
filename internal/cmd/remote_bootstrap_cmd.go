package cmd

import (
	"fmt"
	"path/filepath"

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
			// Resolved by walking the pool, exactly as a run does (c-2): a
			// bootstrap that provisioned the host config names while runs go
			// somewhere else would install onto a machine nothing measures on.
			//
			// The walk asks bootstrap's OWN probe set, through the same
			// derivation the plan uses. Resolving on a different question is
			// how a surface ends up agreeing about the host and disagreeing
			// about what it holds — and it is what lets the pool walk skip a
			// candidate for a reason bootstrap would not recognise.
			tools, _, _ := remoteProbeTools(p)
			target, pool, err := resolveRemoteHost(root, filepath.Dir(root), bootstrapProbeSet(tools))
			if err != nil {
				return err
			}
			if pool.Fallback {
				// Unreachable is not ungranted, and the remedies differ. An
				// error that said "no remote granted" would send the user to
				// re-grant a host that is already authorized and merely down.
				return fmt.Errorf("no granted host answered — nothing to bootstrap.\n%s", pool.Why)
			}
			if target == nil {
				// Named, not merely reported: the user's next move is the grant,
				// and an error that only says "no remote" sends them looking.
				return fmt.Errorf("no remote granted — nothing to bootstrap.\nGrant one with `dross remote grant <host> <workdir>`")
			}
			for _, n := range pool.Notices {
				Printf("remote: %s\n", n)
			}

			// The plan reads the readiness the WALK already took, so the host
			// is asked once. Re-probing here would be a second round trip and
			// a second chance for it to change underneath the plan.
			steps, err := planRemoteBootstrap(p, pool.Candidates[0].Ready)
			if err != nil {
				return err
			}
			if len(steps) == 0 {
				// Names BOTH sources, because a repo can now be empty for two
				// reasons and a message naming only adapters would tell a
				// lanes-only repo it has no lanes.
				Print("no mutation adapters and no test lanes configured — nothing to bootstrap")
				return nil
			}
			return reportBootstrap(*target, steps, apply)
		},
	}
	c.Flags().BoolVar(&apply, "apply", false, "actually install (default is a dry run that changes nothing)")
	return c
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
