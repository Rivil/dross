package boardsync

import (
	"fmt"
	"sort"
	"strings"

	"github.com/Rivil/dross/internal/board"
	"github.com/Rivil/dross/internal/configenum"
	"github.com/Rivil/dross/internal/forge"
	"github.com/Rivil/dross/internal/phase"
)

// TaskMoveKind is what a comparison concluded.
type TaskMoveKind int

const (
	TaskUnchanged TaskMoveKind = iota
	TaskBoardMoved
	TaskPlanMoved
	TaskConflict
	// TaskUnsynced is a task whose agreement point was never recorded — a
	// mapping migrated from the pre-ledger board.json, or one synced before a
	// status was ever asserted. Nothing is known about what either side held,
	// so a difference cannot be attributed and applying would be a guess.
	TaskUnsynced
)

// TaskMoveVerdict is one task's verdict.
type TaskMoveVerdict struct {
	TaskID     string
	Issue      string
	Kind       TaskMoveKind
	PlanStatus string // what plan.toml holds now
	BoardState string // the lifecycle status the board's label asserts now
	WasPlan    string // the snapshot
	WasBoard   string
	NewStatus  string // the plan status to write, when Kind is TaskBoardMoved
}

// providerHasWorkflowState reports whether the backend can say which column an
// issue is in. Kept as an explicit list rather than probing an issue, so the
// refusal is immediate and does not depend on there being an issue to look at.
// TaskPull applies board task-state moves back into plan — reporting only
// unless apply is set. The caller resolved the phase and loaded plan from
// planPath; the workflow-state refusal is repeated here so a direct caller
// cannot skip it.
func TaskPull(ctx *Ctx, phaseID string, plan *phase.Plan, planPath string, apply bool) error {
	if err := RequireWorkflowState(ctx.Proj.Board.Provider); err != nil {
		return err
	}
	moves, err := CollectTaskMoves(ctx, phaseID, plan)
	if err != nil {
		return err
	}
	return ReportTaskMoves(ctx, phaseID, plan, planPath, moves, apply)
}

// RequireWorkflowState refuses, by name, a board with no workflow field: it
// cannot express a task's column, so reporting "no changes" for it would be a
// lie of exactly the shape this repo has already fixed once for a failed pull.
func RequireWorkflowState(provider string) error {
	if ProviderHasWorkflowState(provider) {
		return nil
	}
	return fmt.Errorf("board provider %q has no workflow state — its issues are open/closed only, "+
		"so there is no column for a card to move between.\n\n"+
		"Task-state pull needs youtrack or jira. The outbound mirror (`dross issue task sync`) "+
		"still works here; only the inbound direction is unavailable.", provider)
}

func ProviderHasWorkflowState(provider string) bool {
	switch configenum.Normalize(provider) {
	case "youtrack", "jira":
		return true
	}
	return false
}

func CollectTaskMoves(ctx *Ctx, phaseID string, plan *phase.Plan) ([]TaskMoveVerdict, error) {
	var moves []TaskMoveVerdict
	for i := range plan.Task {
		t := &plan.Task[i]
		link, ok := ctx.Board.TaskLinkFor(phaseID, t.ID)
		if !ok || link.Issue == "" {
			continue // never mirrored; task-sync's job, not ours
		}
		iss, err := ctx.Client.GetIssue(link.Issue)
		if err != nil {
			return nil, Wrap(fmt.Errorf("read %s for %s: %w", link.Issue, t.ID, err))
		}
		moves = append(moves, ClassifyTaskMove(t, link, iss))
	}
	sort.Slice(moves, func(i, j int) bool { return moves[i].TaskID < moves[j].TaskID })
	return moves, nil
}

// classifyTaskMove compares one task's three values: what the plan holds, what
// the board asserts, and what they agreed at the last sync.
func ClassifyTaskMove(t *phase.Task, link board.TaskLink, iss *forge.Issue) TaskMoveVerdict {
	boardStatus := LifecycleFromLabels(iss.Labels)
	m := TaskMoveVerdict{
		TaskID:     t.ID,
		Issue:      link.Issue,
		PlanStatus: t.Status,
		BoardState: boardStatus,
		WasPlan:    link.PlanStatus,
		WasBoard:   link.BoardState,
	}
	if link.PlanStatus == "" && link.BoardState == "" {
		m.Kind = TaskUnsynced
		return m
	}
	planMoved := t.Status != link.PlanStatus
	boardMoved := boardStatus != link.BoardState
	switch {
	case planMoved && boardMoved:
		m.Kind = TaskConflict
	case boardMoved:
		want, ok := PlanStatusForLifecycle(boardStatus)
		if !ok {
			// The board moved to a column dross does not mirror. Not a
			// conflict and not applicable — reported as unchanged rather than
			// invented into a plan status that means something else.
			m.Kind = TaskUnchanged
			return m
		}
		m.Kind = TaskBoardMoved
		m.NewStatus = want
	case planMoved:
		m.Kind = TaskPlanMoved
	default:
		m.Kind = TaskUnchanged
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
func LifecycleFromLabels(labels []string) string {
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
func ReportTaskMoves(ctx *Ctx, phaseID string, plan *phase.Plan, planPath string, moves []TaskMoveVerdict, apply bool) error {
	var applied, conflicts int
	for _, m := range moves {
		switch m.Kind {
		case TaskBoardMoved:
			if apply {
				if !plan.SetTaskStatus(m.TaskID, m.NewStatus) {
					return fmt.Errorf("task %s vanished from the plan mid-run", m.TaskID)
				}
				ctx.Board.SetTaskSynced(phaseID, m.TaskID, m.Issue, m.NewStatus, m.BoardState)
				fmt.Fprintf(ctx.out(), "  %s  %s -> %s (from the board)\n", m.TaskID, m.PlanStatus, m.NewStatus)
			} else {
				fmt.Fprintf(ctx.out(), "  %s  would move %s -> %s (board says %s)\n", m.TaskID, m.PlanStatus, m.NewStatus, m.BoardState)
			}
			applied++
		case TaskConflict:
			conflicts++
			fmt.Fprintf(ctx.out(), "  %s  CONFLICT: plan says %q, board says %q — both changed since they last agreed (%q/%q)\n",
				m.TaskID, m.PlanStatus, m.BoardState, m.WasPlan, m.WasBoard)
		case TaskPlanMoved:
			fmt.Fprintf(ctx.out(), "  %s  the plan moved; run `dross issue task sync %s %s` to push it\n", m.TaskID, phaseID, m.TaskID)
		case TaskUnsynced:
			fmt.Fprintf(ctx.out(), "  %s  no agreement point recorded — run task-sync once to establish one\n", m.TaskID)
		}
	}

	if applied == 0 && conflicts == 0 {
		fmt.Fprintln(ctx.out(), "no task moves on the board")
		return nil
	}
	if apply && applied > 0 {
		if err := plan.Save(planPath); err != nil {
			return err
		}
		if err := ctx.Board.Save(ctx.BoardPath); err != nil {
			return err
		}
		fmt.Fprintf(ctx.out(), "applied %d move(s) to %s\n", applied, planPath)
	}
	if !apply && applied > 0 {
		fmt.Fprintf(ctx.out(), "\n%d move(s) to apply — re-run with --apply\n", applied)
	}
	if conflicts > 0 {
		// Non-zero, but only after every clean move has been reported and (with
		// --apply) written. A contested task must not hide the rest.
		return fmt.Errorf("%d task(s) changed on both sides since the last sync — resolve them by hand, "+
			"then re-run: set the plan with `dross task status`, or move the card back", conflicts)
	}
	return nil
}
