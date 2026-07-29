package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

// EnforceSubcommandKnown walks a cobra command tree and installs an
// error-returning RunE on every "parent" command that has subcommands but
// no Run/RunE of its own. Without this, cobra silently prints help and
// exits 0 when a user types an unknown subcommand — e.g. `dross phase add`
// shows phase's help instead of saying `add` is not a real subcommand. The
// shell exit is 0, and the telemetry event lands as a successful no-op,
// hiding the typo from both the user and stats.
func EnforceSubcommandKnown(root *cobra.Command) {
	if root == nil {
		return
	}
	// Unknown-flag errors never reach a RunE — cobra fails during parsing —
	// so they need their own hook. Installed once at the true root: cobra
	// walks up to the nearest ancestor with a FlagErrorFunc, so one
	// registration covers the whole tree (D3 — no main.go edit).
	if root.Parent() == nil {
		InstallFlagHints(root)
	}
	for _, c := range root.Commands() {
		EnforceSubcommandKnown(c)
	}
	if len(root.Commands()) == 0 || root.Run != nil || root.RunE != nil {
		return
	}
	if root.SuggestionsMinimumDistance == 0 {
		root.SuggestionsMinimumDistance = 2
	}
	root.RunE = func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			return cmd.Help()
		}
		typed := args[0]
		msg := fmt.Sprintf("unknown subcommand %q for %q", typed, cmd.CommandPath())
		avail := availableSubcommandNames(cmd)
		hinted := false
		// The curated table is consulted first: these are semantic remaps, and
		// a distance match would at best name a different verb with different
		// arity. Cobra's own suggestions come next, then our wider-distance
		// fallback for typos cobra gives up on.
		if hint, ok := CuratedHint(cmd.CommandPath(), typed); ok {
			msg += "\n\n" + hintLines(hint)
			hinted = true
		} else if sug := cmd.SuggestionsFor(typed); len(sug) > 0 {
			msg += "\n\nDid you mean this?\n\t" + strings.Join(sug, "\n\t")
			hinted = true
		} else if near := Nearest(typed, avail); len(near) > 0 {
			msg += "\n\nDid you mean this?\n\t" + strings.Join(near, "\n\t")
			hinted = true
		}
		if !hinted && len(avail) > 0 {
			// No close match — show what IS valid so a far-off guess (the
			// shape that produces most unknown_subcommand telemetry) doesn't
			// have to round-trip through --help to discover the command set.
			msg += "\n\nAvailable subcommands:\n\t" + strings.Join(avail, "\n\t")
		}
		msg += fmt.Sprintf("\n\nRun '%s --help' for available subcommands.", cmd.CommandPath())
		return fmt.Errorf("%s", msg)
	}
}

// availableSubcommandNames returns the names of cmd's immediate, user-facing
// subcommands, skipping hidden ones and cobra's auto-added help/completion.
func availableSubcommandNames(cmd *cobra.Command) []string {
	var names []string
	for _, sub := range cmd.Commands() {
		if !sub.IsAvailableCommand() || sub.Name() == "help" || sub.Name() == "completion" {
			continue
		}
		names = append(names, sub.Name())
	}
	return names
}
