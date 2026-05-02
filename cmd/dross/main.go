package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/Rivil/dross/internal/cmd"
)

var version = "0.1.0.0"

func main() {
	root := &cobra.Command{
		Use:           "dross",
		Short:         "Dross — refine intent into verified code",
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: false,
	}

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
		cmd.Codex(),
		cmd.Profile(),
	)

	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "dross:", err)
		os.Exit(1)
	}
}
