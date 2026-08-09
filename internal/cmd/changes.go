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
	var landmarkFlags []string
	c := &cobra.Command{
		Use:   "record <phase-id> <task-id>",
		Short: "Record files (and optionally commit, notes, landmarks) for a task",
		Args:  cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			// A loop-step boundary — see execGatedCommands.
			if err := requireExecConsent(); err != nil {
				return err
			}
			files := splitCSV(filesCSV)
			if len(files) == 0 {
				return fmt.Errorf("at least one --files entry is required")
			}
			landmarks := make([]changes.Landmark, 0, len(landmarkFlags))
			for _, raw := range landmarkFlags {
				lm, err := changes.ParseLandmark(raw)
				if err != nil {
					return err
				}
				landmarks = append(landmarks, lm)
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
			c.Record(args[1], files, commit, notes, landmarks)
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
	// StringArray (not StringSlice): each --landmark is one landmark whose value
	// is comma-separated key=value pairs — cobra must NOT split it on commas.
	c.Flags().StringArrayVar(&landmarkFlags, "landmark", nil,
		`typed landmark "feature=…, symbol=…, loc=file:line, what=…" (repeatable; values may contain commas — a new pair starts only at a recognised key=)`)
	_ = c.MarkFlagRequired("files")
	return c
}

func changesShow() *cobra.Command {
	var asJSON bool
	c := &cobra.Command{
		Use:   "show <phase-id>",
		Short: "Print changes.json (or empty record if none yet)",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			root, err := FindRoot()
			if err != nil {
				return err
			}
			path := changes.FilePath(root, args[0])
			rec, err := changes.Load(path, args[0])
			if err != nil {
				return err
			}
			// This show has always emitted JSON — the record is a .json file,
			// not a toml document. --json is accepted for symmetry so a caller
			// can pass it uniformly across every `show`, exactly as `state
			// show` already does; the output is identical either way.
			b, _ := json.MarshalIndent(rec, "", "  ")
			os.Stdout.Write(b)
			fmt.Println()
			return nil
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "accepted for symmetry; changes show always emits JSON")
	return c
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
