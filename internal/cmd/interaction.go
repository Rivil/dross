package cmd

import (
	"github.com/spf13/cobra"

	"github.com/Rivil/dross/assets"
	"github.com/Rivil/dross/internal/gatestate"
)

// Interaction registers `dross interaction {show}`.
func Interaction() *cobra.Command {
	c := &cobra.Command{
		Use:   "interaction",
		Short: "The interaction playbook injected into interactive command prompts",
	}
	c.AddCommand(interactionShow())
	return c
}

// interactionShow prints the propose-and-react playbook verbatim from the binary,
// mirroring `dross rule show`. Interactive command prompts call this in pre-flight
// so the contract text reaches the model — the snippet_delivery decision, after the
// pilot proved nested @-include does not expand.
//
// A repeat in the same session context prints PlaybookUnchanged instead
// (showOnce); --full always prints the playbook.
func interactionShow() *cobra.Command {
	var full bool
	c := &cobra.Command{
		Use:   "show",
		Short: "Render the interaction playbook for prompt injection",
		RunE: func(_ *cobra.Command, _ []string) error {
			return showOnce(gatestate.ShowPlaybook, assets.InteractionPlaybook, full)
		},
	}
	c.Flags().BoolVar(&full, "full", false, "print the whole playbook even when this session already has it")
	return c
}
