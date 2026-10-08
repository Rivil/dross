package cmd

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/Rivil/dross/internal/gatestate"
	"github.com/Rivil/dross/internal/pathfence"
	"github.com/Rivil/dross/internal/phase"
)

// Execute is `dross execute`: the state /dross-execute keeps through the CLI.
func Execute() *cobra.Command {
	c := &cobra.Command{
		Use:   "execute",
		Short: "Bookkeeping /dross-execute keeps through the CLI",
	}
	c.AddCommand(executeBegin())
	return c
}

// executeBegin records the mode a phase is executed in, in the machine-local
// .dross/gate/execute.json the pair-approval gate reads: pair unless --solo.
// It is the only writer of that record, and besides it only clears a stale
// quick marker — never state.json, never the plan — so beginning a run can
// never clobber either. A same-mode re-run leaves the record byte-for-byte
// alone. A solo begin refuses unless the shipped reviewer is installed.
func executeBegin() *cobra.Command {
	var solo bool
	c := &cobra.Command{
		Use:   "begin <phase-id>",
		Short: "Record the mode /dross-execute runs a phase in (pair unless --solo)",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			id := args[0]
			if err := pathfence.Segment("phase id", id); err != nil {
				return err
			}
			root, err := FindRoot()
			if err != nil {
				return err
			}
			if !isFile(filepath.Join(phase.Dir(root, id), "plan.toml")) {
				return fmt.Errorf("phase %q has no plan.toml — plan it with /dross-plan before executing it", id)
			}
			repoDir := filepath.Dir(root)
			mode := "pair"
			if solo {
				mode = "solo"
				if err := requireReviewer(repoDir); err != nil {
					return err
				}
			}
			// A phase run supersedes any quick marker left behind: a quick
			// that ended without `dross quick end` must not go on arming its
			// review inside the phase run.
			if err := gatestate.RemoveQuick(repoDir); err != nil {
				return err
			}
			rec, err := gatestate.LoadExecute(repoDir)
			if err != nil {
				return err
			}
			if rec != nil && rec.Phase == id && rec.Mode == mode {
				Printf("execute %s: %s (already recorded)\n", id, mode)
				return nil
			}
			if err := gatestate.SaveExecute(repoDir, gatestate.Execute{Phase: id, Mode: mode, At: time.Now().UTC()}); err != nil {
				return err
			}
			Printf("execute %s: %s (recorded)\n", id, mode)
			return nil
		},
	}
	c.Flags().BoolVar(&solo, "solo", false, "record solo mode: the run proceeds without per-task approvals")
	return c
}
