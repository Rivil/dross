package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/Rivil/dross/internal/cmd"
)

func main() {
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
		cmd.Rule(),
		cmd.Validate(),
		cmd.Phase(),
		cmd.Milestone(),
		cmd.Task(),
		cmd.Changes(),
		cmd.Verify(),
		cmd.Status(),
		cmd.Codex(),
		cmd.Profile(),
		cmd.VersionCmd(),
		cmd.Doctor(),
		cmd.Defaults(),
		cmd.Env(),
		cmd.Ship(),
	)

	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "dross:", err)
		os.Exit(1)
	}
}
