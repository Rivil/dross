package main

import (
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/Rivil/dross/internal/cmd"
)

// newRoot assembles the full dross command tree. Extracted from main so
// tests (e.g. the README↔command parity check) can inspect the real set
// of commands without duplicating the AddCommand list.
func newRoot() *cobra.Command {
	root := &cobra.Command{
		Use:           "dross",
		Short:         "Dross — refine intent into verified code",
		Version:       cmd.VersionString(),
		SilenceUsage:  true,
		SilenceErrors: false,
	}
	root.SetVersionTemplate("{{.Version}}\n")

	root.AddCommand(
		cmd.Init(),
		cmd.Onboard(),
		cmd.Project(),
		cmd.State(),
		cmd.Local(),
		cmd.Rule(),
		cmd.Interaction(),
		cmd.Validate(),
		cmd.Phase(),
		cmd.Checkout(),
		cmd.Milestone(),
		cmd.Task(),
		cmd.Changes(),
		cmd.Verify(),
		cmd.Status(),
		cmd.Watch(),
		cmd.Codex(),
		cmd.Profile(),
		cmd.VersionCmd(),
		cmd.Doctor(),
		cmd.Defaults(),
		cmd.Env(),
		cmd.Ship(),
		cmd.Stats(),
		cmd.Issue(),
		cmd.Security(),
		cmd.Quality(),
		cmd.Techdebt(),
		cmd.Stack(),
		cmd.Deferred(),
		cmd.Pause(),
		cmd.Reentry(),
		cmd.Hooks(),
		cmd.Install(),
		cmd.Update(),
		cmd.Statusline(),
		cmd.Architecture(),
		cmd.BaseBranch(),
	)

	// Without this, `dross phase add` (or any unknown subcommand under a
	// parent that has no Run of its own) silently prints help and exits 0.
	cmd.EnforceSubcommandKnown(root)
	return root
}

func main() {
	root := newRoot()

	// Telemetry: capture resolved subcommand at PreRun, write the event
	// after Execute returns so we get duration + final error class.
	// Failures here are swallowed — telemetry never breaks the user's
	// workflow.
	start := time.Now()
	var resolvedCmd *cobra.Command
	root.PersistentPreRun = func(c *cobra.Command, _ []string) {
		resolvedCmd = c
	}

	err := root.Execute()

	// When Execute fails before PreRun (unknown subcommand, flag parse
	// error, --help/--version short-circuit), resolvedCmd is nil and the
	// event would land with no `cmd` field — telemetry then can't say
	// which command was attempted. Fall back to a Find() over os.Args so
	// the event at least records the deepest cobra match.
	if resolvedCmd == nil {
		resolvedCmd = cmd.ResolveCmdForTelemetry(root, os.Args[1:])
	}

	cmd.RecordCLIEvent(resolvedCmd, time.Since(start), err)

	if err != nil {
		fmt.Fprintln(os.Stderr, "dross:", err)
		os.Exit(1)
	}
}
