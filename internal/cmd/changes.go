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
				if err := requireFilelessTaskInPlan(args[0], args[1]); err != nil {
					return err
				}
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
			Printf("recorded %s/%s (%s%s)\n", args[0], args[1],
				func() string {
					if len(files) == 0 {
						return "no files — plan declares files = []"
					}
					return fmt.Sprintf("%d files", len(files))
				}(),
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
	// Deliberately NOT MarkFlagRequired: a task whose plan entry declares
	// files = [] — its artifact is a repo setting, a dashboard toggle, a
	// provider-side change — has no file to name, and its changes record is
	// then the only durable trace it leaves. The guard moves into RunE, where
	// plan.toml can be consulted; see requireFilelessTaskInPlan.
	return c
}

// requireFilelessTaskInPlan decides whether `changes record` may proceed with
// no --files. The blanket "at least one file" guard existed to catch a silent
// no-op record, and that risk is real for a task that does touch files — so
// zero files is accepted only when plan.toml's own entry for the task declares
// files = []. A task that declares files must record them, and an id absent
// from the plan is not a way past the guard: an unrecognised id is exactly the
// typo the original check was there to catch.
func requireFilelessTaskInPlan(phaseID, taskID string) error {
	plan, _, err := loadPhasePlan(phaseID)
	if err != nil {
		return fmt.Errorf("at least one --files entry is required: zero files is only allowed for a task whose plan.toml entry declares files = [], and %s's plan could not be read: %w", phaseID, err)
	}
	for _, t := range plan.Task {
		if t.ID != taskID {
			continue
		}
		if len(t.Files) > 0 {
			return fmt.Errorf("at least one --files entry is required: %s/%s declares %d file(s) in plan.toml", phaseID, taskID, len(t.Files))
		}
		return nil
	}
	return fmt.Errorf("at least one --files entry is required: %s/%s is not in plan.toml, so nothing declares it fileless", phaseID, taskID)
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
