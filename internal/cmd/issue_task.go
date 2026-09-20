package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/Rivil/dross/internal/boardsync"
	"github.com/Rivil/dross/internal/configenum"
)

// `dross issue task sync` — the board mirror at task granularity.
//
// Before it, the tracker saw a phase and a rendered checklist inside its body.
// Nothing moved when one task of eight was picked up, so a board that claimed
// to show where work was could only ever show which phase it was in. This gives
// each plan task its own issue, related to the phase's, and drives it through
// the tracker's OWN workflow field rather than a dross label — a label is
// dross talking to itself; the state field is what the tracker's columns read.
//
// It is a no-op when board sync is off, like every other `dross issue` verb, so
// the loop prompts call it unconditionally.

func issueTaskSync() *cobra.Command {
	var status string
	var doClose bool
	c := &cobra.Command{
		Use:   "sync <phase-id> [task-id]",
		Short: "Mirror a phase's plan tasks as issues, one per task",
		Long: `Create or update one board issue per plan task, related to the phase's issue.

With a task id, only that task is synced — which is what the execute loop
does at each edge, so a commit does not rewrite the other seven issues.

--status drives the tracker's own workflow field (YouTrack's State, a Jira
transition), not a dross label. A backend that cannot relate issues, or has
no workflow field at all, warns once per run and continues with the label.

A no-op when board sync is off.`,
		Args: cobra.RangeArgs(1, 2),
		RunE: func(_ *cobra.Command, args []string) error {
			// Validated ahead of openBoard, exactly as phase-sync does: the
			// enabled check below returns nil, so a typo'd --status on a repo
			// that has not opted into board sync would exit 0 as a silent
			// no-op and only surface on the machine where sync is on. The
			// asymmetry with phase-sync was harmless until `task-complete`
			// existed; from here a mistyped terminal status is a card that
			// never leaves the review column.
			if status != "" {
				if !configenum.LifecycleStatuses.Has(status) {
					return fmt.Errorf("unknown --status %q; expected %s", status, configenum.LifecycleStatuses.List())
				}
				// Reassign the normalized form: status is passed raw to
				// statusLabel and to the state-map lookups, so validating
				// without normalizing would accept " Task-In-Review", emit the
				// label "dross/status: Task-In-Review" and then miss the map.
				status = configenum.Normalize(status)
			}
			// --close REQUIRES --status. boardsync.CloseIssue defaults an empty
			// status to `complete` — the PHASE lane's terminal state — and
			// writing that onto task cards is the label collision the
			// task_terminal_status decision exists to prevent.
			if doClose && status == "" {
				return fmt.Errorf("--close requires --status: without one the close would write the phase lane's terminal state onto every task card (expected %s)",
					configenum.LifecycleStatuses.List())
			}
			ctx, enabled, err := openBoard()
			if err != nil {
				return err
			}
			if !enabled {
				return nil
			}
			phaseID := args[0]
			only := ""
			if len(args) == 2 {
				only = args[1]
			}
			return boardsync.SyncTasks(ctx, phaseID, only, status, doClose)
		},
	}
	c.Flags().StringVar(&status, "status", "", "lifecycle status to drive the task's state ("+configenum.LifecycleStatuses.List()+")")
	c.Flags().BoolVar(&doClose, "close", false, "resolve each synced task's issue (use at ship finalize; requires --status)")
	return c
}
