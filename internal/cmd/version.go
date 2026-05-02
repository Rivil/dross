package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

// Build metadata. Overridden at link time via:
//   -ldflags "-X github.com/Rivil/dross/internal/cmd.Version=...
//             -X github.com/Rivil/dross/internal/cmd.Commit=...
//             -X github.com/Rivil/dross/internal/cmd.Date=..."
// GoReleaser sets all three; `make build` sets Commit and Date.
var (
	Version = "0.1.0.0"
	Commit  = "unknown"
	Date    = "unknown"
)

// VersionString is the single-line form printed by `dross --version` and
// `dross version`. Stable format so doctor/scripts can parse it.
func VersionString() string {
	return fmt.Sprintf("dross %s (commit %s, built %s)", Version, Commit, Date)
}

func VersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version, commit, and build date",
		Run: func(cmd *cobra.Command, _ []string) {
			fmt.Fprintln(cmd.OutOrStdout(), VersionString())
		},
	}
}
