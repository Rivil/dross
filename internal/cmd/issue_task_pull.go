package cmd

import (
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Rivil/dross/internal/board"
	"github.com/Rivil/dross/internal/configenum"
	"github.com/Rivil/dross/internal/forge"
	"github.com/Rivil/dross/internal/phase"
)

// `dross issue task-pull` is the inbound half of the task mirror: someone drags
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

// taskMoveKind is what a comparison concluded.
type taskMoveKind int

const (
	taskUnchanged taskMoveKind = iota
	taskBoardMoved
	taskPlanMoved
	taskConflict
	// taskUnsynced is a task whose agreement point was never recorded — a
	// mapping migrated from the pre-ledger board.json, or one synced before a
	// status was ever asserted. Nothing is known about what either side held,
	// so a difference cannot be attributed and applying would be a guess.
	taskUnsynced
)

// taskMoveVerdict is one task's verdict.
type taskMoveVerdict struct {
	TaskID     string
	Issue      string
	Kind       taskMoveKind
	PlanStatus string // what plan.toml holds now
	BoardState string // the lifecycle status the board's label asserts now
	WasPlan    string // the snapshot
	WasBoard   string
	NewStatus  string // the plan status to write, when Kind is taskBoardMoved
}

func issueTaskPull() *cobra.Command {
	var apply bool
	c := &cobra.Command{
		Use:   "task-pull [phase-id]",
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

func taskPull(ctx *boardCtx, phaseID string, apply bool) error {
	// Refused up front, by name. A board with no workflow field cannot express
	// a task's column, so reporting "no changes" for it would be a lie of
	// exactly the shape this repo has already fixed once for a failed pull.
	if !providerHasWorkflowState(ctx.proj.Board.Provider) {
		return fmt.Errorf("board provider %q has no workflow state — its issues are open/closed only, "+
			"so there is no column for a card to move between.\n\n"+
			"Task-state pull needs youtrack or jira. The outbound mirror (`dross issue task-sync`) "+
			"still works here; only the inbound direction is unavailable.", ctx.proj.Board.Provider)
	}

	plan, _, planPath, err := loadPhasePlanAndSpec(phaseID)
	if err != nil {
		return err
	}

	moves, err := collectTaskMoves(ctx, phaseID, plan)
	if err != nil {
		return err
	}
	return reportTaskMoves(ctx, phaseID, plan, planPath, moves, apply)
}

// providerHasWorkflowState reports whether the backend can say which column an
// issue is in. Kept as an explicit list rather than probing an issue, so the
// refusal is immediate and does not depend on there being an issue to look at.
func providerHasWorkflowState(provider string) bool {
	switch configenum.Normalize(provider) {
	case "youtrack", "jira":
		return true
	}
	return false
}

func collectTaskMoves(ctx *boardCtx, phaseID string, plan *phase.Plan) ([]taskMoveVerdict, error) {
	var moves []taskMoveVerdict
	for i := range plan.Task {
		t := &plan.Task[i]
		link, ok := ctx.board.TaskLinkFor(phaseID, t.ID)
		if !ok || link.Issue == "" {
			continue // never mirrored; task-sync's job, not ours
		}
		iss, err := ctx.client.GetIssue(link.Issue)
		if err != nil {
			return nil, wrapBoard(fmt.Errorf("read %s for %s: %w", link.Issue, t.ID, err))
		}
		moves = append(moves, classifyTaskMove(t, link, iss))
	}
	sort.Slice(moves, func(i, j int) bool { return moves[i].TaskID < moves[j].TaskID })
	return moves, nil
}

// classifyTaskMove compares one task's three values: what the plan holds, what
// the board asserts, and what they agreed at the last sync.
func classifyTaskMove(t *phase.Task, link board.TaskLink, iss *forge.Issue) taskMoveVerdict {
	boardStatus := lifecycleFromLabels(iss.Labels)
	m := taskMoveVerdict{
		TaskID:     t.ID,
		Issue:      link.Issue,
		PlanStatus: t.Status,
		BoardState: boardStatus,
		WasPlan:    link.PlanStatus,
		WasBoard:   link.BoardState,
	}
	if link.PlanStatus == "" && link.BoardState == "" {
		m.Kind = taskUnsynced
		return m
	}
	planMoved := t.Status != link.PlanStatus
	boardMoved := boardStatus != link.BoardState
	switch {
	case planMoved && boardMoved:
		m.Kind = taskConflict
	case boardMoved:
		want, ok := planStatusForLifecycle(boardStatus)
		if !ok {
			// The board moved to a column dross does not mirror. Not a
			// conflict and not applicable — reported as unchanged rather than
			// invented into a plan status that means something else.
			m.Kind = taskUnchanged
			return m
		}
		m.Kind = taskBoardMoved
		m.NewStatus = want
	case planMoved:
		m.Kind = taskPlanMoved
	default:
		m.Kind = taskUnchanged
	}
	return m
}

// lifecycleFromLabels reads the dross/status:<lifecycle> label — what dross
// last asserted, or what a human moved the card to if the tracker relabels.
//
// The LABEL rather than the tracker's own state name, because both provider
// state maps are non-injective: Jira sends in-progress and task-in-progress to
// the same "In Progress". Inverting a state name is ambiguous by construction;
// the label names the exact lifecycle status.
func lifecycleFromLabels(labels []string) string {
	const prefix = "dross/status:"
	for _, l := range labels {
		if rest, ok := strings.CutPrefix(l, prefix); ok {
			return rest
		}
	}
	return ""
}

// reportTaskMoves prints the verdicts and, under --apply, writes the ones that
// are unambiguous.
//
// A conflict never blocks the tasks around it: each is reported, the clean ones
// still apply, and the command exits non-zero at the END. Aborting on the first
// conflict would make a single contested task hide every other move.
func reportTaskMoves(ctx *boardCtx, phaseID string, plan *phase.Plan, planPath string, moves []taskMoveVerdict, apply bool) error {
	var applied, conflicts int
	for _, m := range moves {
		switch m.Kind {
		case taskBoardMoved:
			if apply {
				if !plan.SetTaskStatus(m.TaskID, m.NewStatus) {
					return fmt.Errorf("task %s vanished from the plan mid-run", m.TaskID)
				}
				ctx.board.SetTaskSynced(phaseID, m.TaskID, m.Issue, m.NewStatus, m.BoardState)
				Printf("  %s  %s -> %s (from the board)\n", m.TaskID, m.PlanStatus, m.NewStatus)
			} else {
				Printf("  %s  would move %s -> %s (board says %s)\n", m.TaskID, m.PlanStatus, m.NewStatus, m.BoardState)
			}
			applied++
		case taskConflict:
			conflicts++
			Printf("  %s  CONFLICT: plan says %q, board says %q — both changed since they last agreed (%q/%q)\n",
				m.TaskID, m.PlanStatus, m.BoardState, m.WasPlan, m.WasBoard)
		case taskPlanMoved:
			Printf("  %s  the plan moved; run `dross issue task-sync %s %s` to push it\n", m.TaskID, phaseID, m.TaskID)
		case taskUnsynced:
			Printf("  %s  no agreement point recorded — run task-sync once to establish one\n", m.TaskID)
		}
	}

	if applied == 0 && conflicts == 0 {
		Print("no task moves on the board")
		return nil
	}
	if apply && applied > 0 {
		if err := plan.Save(planPath); err != nil {
			return err
		}
		if err := ctx.board.Save(ctx.boardPath); err != nil {
			return err
		}
		Printf("applied %d move(s) to %s\n", applied, planPath)
	}
	if !apply && applied > 0 {
		Printf("\n%d move(s) to apply — re-run with --apply\n", applied)
	}
	if conflicts > 0 {
		// Non-zero, but only after every clean move has been reported and (with
		// --apply) written. A contested task must not hide the rest.
		return &ExitCodeError{
			Code: 1,
			Err: fmt.Errorf("%d task(s) changed on both sides since the last sync — resolve them by hand, "+
				"then re-run: set the plan with `dross task status`, or move the card back", conflicts),
		}
	}
	return nil
}
