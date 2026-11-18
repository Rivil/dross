package cmd

import (
	"github.com/spf13/cobra"
)

// NudgeHook is the retired PostToolUse context-checkpoint nudge. `dross hooks
// ensure` prunes it from settings.json (retiredHooks) and doctor warns while it
// is still wired.
const NudgeHook = "dross hooks nudge"

// hooksNudge keeps the retired verb as a hidden no-op: a settings.json wired
// before the nudge was retired still runs it on every tool call until `dross
// hooks ensure` prunes it, and a missing verb there would be a hook error on
// each one. It reads nothing, prints nothing and always exits 0.
func hooksNudge() *cobra.Command {
	return &cobra.Command{
		Use:           "nudge",
		Short:         "Retired PostToolUse hook: a silent no-op until `dross hooks ensure` prunes it",
		Hidden:        true,
		Args:          cobra.ArbitraryArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE:          func(*cobra.Command, []string) error { return nil },
	}
}
