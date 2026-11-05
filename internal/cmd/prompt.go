package cmd

import (
	"io/fs"

	"github.com/spf13/cobra"

	"github.com/Rivil/dross/assets"
)

// promptFS is the tree `dross prompt` reads partials from: the binary's own
// embedded assets, swapped for a MapFS in tests.
var promptFS fs.FS = assets.FS

// Prompt registers `dross prompt {partial}`.
func Prompt() *cobra.Command {
	c := &cobra.Command{
		Use:   "prompt",
		Short: "Slash-command prompt text the binary carries",
	}
	c.AddCommand(promptPartial())
	return c
}

// promptPartial prints one prompt partial verbatim — the branch a base prompt
// moved out of its common path, fetched at the fetch line that names its
// trigger. Unlike `interaction show` it never dedupes per session: a re-fetch
// after a compaction must print the whole partial again.
func promptPartial() *cobra.Command {
	return &cobra.Command{
		Use:   "partial <prompt> <name>",
		Short: "Print a prompt partial verbatim (a branch fetched only when its trigger holds)",
		Args:  cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			b, err := assets.Partial(promptFS, args[0], args[1])
			if err != nil {
				return err
			}
			Printf("%s", b)
			return nil
		},
	}
}
