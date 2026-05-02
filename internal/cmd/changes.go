package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Rivil/dross/internal/changes"
)

// Changes registers `dross changes {record,show}`.
func Changes() *cobra.Command {
	c := &cobra.Command{
		Use:   "changes",
		Short: "Append-only record of files touched per task during execute",
	}
	c.AddCommand(changesRecord(), changesShow())
	return c
}

func changesRecord() *cobra.Command {
	var filesCSV, commit, notes string
	c := &cobra.Command{
		Use:   "record <phase-id> <task-id>",
		Short: "Record files (and optionally commit + notes) for a task",
		Args:  cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			files := splitCSV(filesCSV)
			if len(files) == 0 {
				return fmt.Errorf("at least one --files entry is required")
			}
			root, err := FindRoot()
			if err != nil {
				return err
			}
			path := changes.FilePath(root, args[0])
			c, err := changes.Load(path, args[0])
			if err != nil {
				return err
			}
			c.Record(args[1], files, commit, notes)
			if err := c.Save(path); err != nil {
				return err
			}
			Printf("recorded %s/%s (%d files%s)\n", args[0], args[1], len(files),
				func() string {
					if commit != "" {
						return ", commit " + commit
					}
					return ""
				}())
			return nil
		},
	}
	c.Flags().StringVar(&filesCSV, "files", "", "comma-separated list of files touched")
	c.Flags().StringVar(&commit, "commit", "", "commit SHA for this task")
	c.Flags().StringVar(&notes, "notes", "", "free-form notes")
	_ = c.MarkFlagRequired("files")
	return c
}

func changesShow() *cobra.Command {
	return &cobra.Command{
		Use:   "show <phase-id>",
		Short: "Print changes.json (or empty record if none yet)",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			root, err := FindRoot()
			if err != nil {
				return err
			}
			path := changes.FilePath(root, args[0])
			c, err := changes.Load(path, args[0])
			if err != nil {
				return err
			}
			b, _ := json.MarshalIndent(c, "", "  ")
			os.Stdout.Write(b)
			fmt.Println()
			return nil
		},
	}
}

func splitCSV(s string) []string {
	out := []string{}
	for _, x := range strings.Split(s, ",") {
		x = strings.TrimSpace(x)
		if x != "" {
			out = append(out, x)
		}
	}
	return out
}
