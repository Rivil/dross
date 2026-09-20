package cmd

import (
	"github.com/spf13/cobra"

	"github.com/Rivil/dross/internal/boardsync"
)

// `dross issue task pull` is the inbound half of the task mirror: someone drags
// a card on the tracker, and the plan follows.
//
// Every other `dross issue` verb pushes. This one lets a remote system write a
// local planning artefact, which is why it is a separate verb, dry by default,
// and refuses rather than guesses.
//
// THE HARD PART is not reading the board — it is knowing whether a difference
// means the board moved. Two current values cannot tell you: plan=done beside
// board="In Progress" is identical whether the board moved, the plan moved, or
// both did. So each sync records the pair both sides held when they last
// agreed (board.TaskLink), and a difference is read against THAT:
//
//   - board differs from the snapshot, plan does not  → the board moved, apply
//   - plan differs from the snapshot, board does not  → the plan moved, push
//   - both differ                                     → refuse, name both
//   - neither differs                                 → nothing to do
//
// No timestamps. No provider surfaces an issue's updated_at, and comparing a
// laptop's clock against a SaaS tracker's would be a race dressed up as a fact.

func issueTaskPull() *cobra.Command {
	var apply bool
	c := &cobra.Command{
		Use:   "pull [phase-id]",
		Short: "Apply board task-state moves back into plan.toml",
		Long: "Reads each mirrored task's issue and applies a move made on the board.\n\n" +
			"Reports without writing unless --apply is passed. A task changed on both\n" +
			"sides since the last sync is refused, naming both values — dross does not\n" +
			"pick a winner for you.\n\n" +
			"Needs a board whose issues carry a workflow state. Forgejo, Gitea and\n" +
			"GitLab boards are open/closed only and are refused by name.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			ctx, enabled, err := openBoard()
			if err != nil {
				return err
			}
			if !enabled {
				Print("board sync is off — nothing to pull")
				return nil
			}
			phaseID, err := resolveTaskPhaseID(args)
			if err != nil {
				return err
			}
			return taskPull(ctx, phaseID, apply)
		},
	}
	c.Flags().BoolVar(&apply, "apply", false,
		"write the moves into plan.toml (default: report only)")
	return c
}

// taskPull resolves the phase's plan and hands it to boardsync.TaskPull.
// Refused up front, by name, BEFORE the plan is read: a board with no
// workflow field cannot express a task's column, so reporting "no changes"
// for it would be a lie of exactly the shape this repo has already fixed once
// for a failed pull.
func taskPull(ctx *boardCtx, phaseID string, apply bool) error {
	if err := boardsync.RequireWorkflowState(ctx.Proj.Board.Provider); err != nil {
		return err
	}
	plan, _, planPath, err := loadPhasePlanAndSpec(phaseID)
	if err != nil {
		return err
	}
	return boardsync.TaskPull(ctx, phaseID, plan, planPath, apply)
}
