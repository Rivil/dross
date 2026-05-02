package cmd

import (
	"fmt"
	"os"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/spf13/cobra"

	"github.com/rivil/dross/internal/milestone"
)

func Milestone() *cobra.Command {
	c := &cobra.Command{
		Use:   "milestone",
		Short: "Manage milestones under .dross/milestones/",
	}
	c.AddCommand(milestoneList(), milestoneCreate(), milestoneShow())
	return c
}

func milestoneList() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List milestones",
		RunE: func(_ *cobra.Command, _ []string) error {
			root, err := FindRoot()
			if err != nil {
				return err
			}
			versions, err := milestone.List(root)
			if err != nil {
				return err
			}
			if len(versions) == 0 {
				Print("(no milestones)")
				return nil
			}
			for _, v := range versions {
				Print(v)
			}
			return nil
		},
	}
}

func milestoneCreate() *cobra.Command {
	return &cobra.Command{
		Use:   "create <version>",
		Short: "Create a new milestone (e.g. v0.1)",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			root, err := FindRoot()
			if err != nil {
				return err
			}
			path := milestone.FilePath(root, args[0])
			if _, err := os.Stat(path); err == nil {
				return fmt.Errorf("%s already exists", path)
			}
			m := &milestone.Milestone{
				Milestone: milestone.Meta{
					Version: args[0],
					Status:  "planning",
					Started: time.Now().UTC().Format("2006-01-02"),
				},
			}
			if err := m.Save(path); err != nil {
				return err
			}
			Printf("created %s\n", path)
			return nil
		},
	}
}

func milestoneShow() *cobra.Command {
	return &cobra.Command{
		Use:   "show <version>",
		Short: "Print a milestone toml",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			root, err := FindRoot()
			if err != nil {
				return err
			}
			path := milestone.FilePath(root, args[0])
			m, err := milestone.Load(path)
			if err != nil {
				return err
			}
			Printf("# %s\n", path)
			return toml.NewEncoder(os.Stdout).Encode(m)
		},
	}
}
