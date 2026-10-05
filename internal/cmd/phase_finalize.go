package cmd

import (
	"errors"
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/Rivil/dross/internal/boardsync"
	"github.com/Rivil/dross/internal/gitrun"
	"github.com/Rivil/dross/internal/state"
)

// finalizeBoard closes a completed phase's board: every task card at
// task-complete and the phase card at complete (boardsync.FinalizePhase), then
// the board.json it wrote is committed and published like any other .dross
// chore on the base, so complete still hands back a clean tree level with
// origin. With board sync off it builds no client and makes no call.
//
// `dross phase complete` runs it last, after the completion record and the
// branch teardown (locked board_failure_posture): a board that cannot be
// reached never rolls back a completion that is already true.
// `dross issue phase finalize` runs the same function as the retry.
func finalizeBoard(repoDir, phaseID, base string) error {
	ctx, enabled, err := openBoard()
	if err != nil {
		return err
	}
	if !enabled {
		return nil
	}
	finalizeErr := boardsync.FinalizePhase(ctx, phaseID)
	// What the finalizer did write is committed and routed even when a card
	// failed: those links let the retry resume instead of starting over.
	if _, err := autoCommitDrossDirt(repoDir, "finalizing the board"); err != nil {
		return errors.Join(finalizeErr, fmt.Errorf("commit board.json: %w", err))
	}
	route, err := routeBaseChores(repoDir, base)
	if err != nil {
		return errors.Join(finalizeErr, fmt.Errorf("publish board.json: %w", err))
	}
	if c := route.ChorePR; c != nil {
		Printf("board record: %s\n", c.narrate())
	}
	return finalizeErr
}

// boardNotFinalized is the error complete returns when the completion stood but
// the board did not follow. It names the one command that finishes the job.
func boardNotFinalized(phaseID string, err error) error {
	return fmt.Errorf("%s is complete, but its board was not finalized: %w\n"+
		"Re-run `dross issue phase finalize %s` once the board is reachable — the completion record is already written", phaseID, err, phaseID)
}

// issuePhaseFinalize is the retry for a board `dross phase complete` could not
// finish: the same finalizer, run from the base the phase was merged into.
func issuePhaseFinalize() *cobra.Command {
	var baseFlag string
	c := &cobra.Command{
		Use:   "finalize <phase-id>",
		Short: "Close a completed phase's board cards: tasks at task-complete, the phase at complete",
		Long: "Bring a completed phase's board to its terminal state — the same step `dross phase complete` " +
			"runs last. Every task card ends at task-complete and the phase card at complete, all closed; " +
			"missing cards are created closed; a card already there is left alone, so a re-run only reads. " +
			"Refuses unless the phase's changes.json reads complete and HEAD is on the phase's recorded base " +
			"(or --base, for a phase completed with --base). With board sync off it does nothing.",
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			phaseID := args[0]
			proj, _, err := loadProject()
			if err != nil {
				return err
			}
			if !proj.Board.Enabled {
				return nil
			}
			root, err := FindRoot()
			if err != nil {
				return err
			}
			repoDir := filepath.Dir(root)
			s, err := state.Load(filepath.Join(root, state.File))
			if err != nil {
				return err
			}
			base, err := resolveCompleteBase(repoDir, root, proj, s, phaseID, baseFlag)
			if err != nil {
				return err
			}
			cur, err := gitrun.Trim(repoDir, "symbolic-ref", "--short", "HEAD")
			if err != nil {
				return fmt.Errorf("read the current branch: %w", err)
			}
			if cur != base {
				return fmt.Errorf("finalize %s from %s, the branch it was merged into — HEAD is on %s", phaseID, base, cur)
			}
			return finalizeBoard(repoDir, phaseID, base)
		},
	}
	c.Flags().StringVar(&baseFlag, "base", "",
		"the branch the phase was merged into, when it records none (a phase completed with --base)")
	return c
}
