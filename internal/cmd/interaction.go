package cmd

import (
	"github.com/spf13/cobra"

	"github.com/Rivil/dross/assets"
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
func interactionShow() *cobra.Command {
	return &cobra.Command{
		Use:   "show",
		Short: "Render the interaction playbook for prompt injection",
		RunE: func(_ *cobra.Command, _ []string) error {
			Printf("%s", assets.InteractionPlaybook)
			return nil
		},
	}
}
