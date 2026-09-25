package cmd

import (
	"github.com/spf13/cobra"

	"github.com/Rivil/dross/internal/update"
)

// Update registers `dross update` — a thin cobra wrapper over update.Apply,
// which fetches the latest GitHub release, verifies it (minisign signature over
// checksums.txt, then the archive's SHA-256), swaps the running binary and
// re-syncs the embedded assets from the freshly-swapped one. This only maps the
// flags and the running build's version onto update.Options.
func Update() *cobra.Command {
	var o update.Options
	c := &cobra.Command{
		Use:   "update",
		Short: "Update dross to the latest GitHub release",
		RunE: func(cmd *cobra.Command, _ []string) error {
			o.Out = cmd.OutOrStdout()
			o.Version = Version
			o.Commit = Commit
			return update.Apply(cmd.Context(), o)
		},
	}
	c.Flags().BoolVar(&o.Check, "check", false, "report the available version without updating")
	c.Flags().BoolVar(&o.Force, "force", false, "reinstall the latest release even if it is not newer")
	c.Flags().StringVar(&o.APIBase, "api-base", "", "override the GitHub API base URL (testing)")
	_ = c.Flags().MarkHidden("api-base")
	return c
}
