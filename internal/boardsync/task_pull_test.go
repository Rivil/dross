package boardsync

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/board"
	"github.com/Rivil/dross/internal/forge"
	"github.com/Rivil/dross/internal/phase"
	"github.com/Rivil/dross/internal/project"
)

func projectWithProvider(provider string) *project.Project {
	p := &project.Project{}
	p.Board.Provider = provider
	return p
}

// pullFixture writes plan.toml for phase p1 under a fresh .dross root and
// returns a Ctx over an empty board narrating into out.
func pullFixture(t *testing.T, planBody string, out *bytes.Buffer) (*Ctx, *phase.Plan, string) {
	t.Helper()
	root := filepath.Join(t.TempDir(), ".dross")
	dir := filepath.Join(root, "phases", "p1")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	planPath := filepath.Join(dir, "plan.toml")
	if err := os.WriteFile(planPath, []byte(planBody), 0o644); err != nil {
		t.Fatal(err)
	}
	plan, err := phase.LoadPlan(planPath)
	if err != nil {
		t.Fatal(err)
	}
	return &Ctx{
		Board:     board.New(),
		Proj:      projectWithProvider("youtrack"),
		Root:      root,
		BoardPath: filepath.Join(root, board.File),
		Out:       out,
	}, plan, planPath
}

func mustRead(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func planTask(t *testing.T, planPath, id string) phase.Task {
	t.Helper()
	plan, err := phase.LoadPlan(planPath)
	if err != nil {
		t.Fatal(err)
	}
	task := plan.FindTask(id)
	if task == nil {
		t.Fatalf("task %s missing from %s", id, planPath)
	}
	return *task
}

func assertPlanUnchanged(t *testing.T, planPath, before string) {
	t.Helper()
	if after := mustRead(t, planPath); after != before {
		t.Errorf("plan.toml was mutated:\n--- before ---\n%s\n--- after ---\n%s", before, after)
	}
}

const pullPlan = `[phase]
id = "p1"
[[task]]
id = "t-1"
wave = 1
title = "first"
status = "in_progress"
[[task]]
id = "t-2"
wave = 1
title = "second"
status = "in_progress"
`

// pullFakeClient is a BoardClient that answers GetIssue and nothing else — the
// only method the inbound path uses.
type pullFakeClient struct {
	t      *testing.T
	issues map[string]*forge.Issue
	err    error
}

func (f *pullFakeClient) GetIssue(key string) (*forge.Issue, error) {
	if f.err != nil {
		return nil, f.err
	}
	if iss, ok := f.issues[key]; ok {
		return iss, nil
	}
	return nil, errors.New("no such issue: " + key)
}
func (f *pullFakeClient) EnsureMilestone(string, string) (string, error) { return "", nil }
func (f *pullFakeClient) CreateIssue(forge.IssueInput) (*forge.Issue, error) {
	f.t.Fatal("task-pull must not create issues")
	return nil, nil
}
func (f *pullFakeClient) UpdateIssue(string, forge.IssuePatch) (*forge.Issue, error) {
	f.t.Fatal("task-pull must not write to the board — it is the inbound direction")
	return nil, nil
}
func (f *pullFakeClient) CloseIssue(string) error { return nil }
func (f *pullFakeClient) ListIssues(forge.IssueFilter) ([]forge.Issue, error) {
	return nil, nil
}

func statusLabels(lifecycle string) []string {
	l := []string{LabelMarker}
	if lifecycle != "" {
		l = append(l, StatusLabel(lifecycle))
	}
	return l
}

// TestClassifyBoardMoved: the board moved and the plan did not, so the plan
// follows. This is the feature.
func TestClassifyBoardMoved(t *testing.T) {
	task := &phase.Task{ID: "t-1", Status: phase.StatusInProgress}
	link := board.TaskLink{Issue: "PROJ-7", PlanStatus: phase.StatusInProgress, BoardState: StatusTaskInProgress}
	iss := &forge.Issue{Key: "PROJ-7", Labels: statusLabels(StatusTaskInReview)}

	got := ClassifyTaskMove(task, link, iss)
	if got.Kind != TaskBoardMoved {
		t.Fatalf("kind = %v, want TaskBoardMoved", got.Kind)
	}
	if got.NewStatus != phase.StatusDone {
		t.Errorf("new status = %q, want %q — task-in-review is what done looks like on a board", got.NewStatus, phase.StatusDone)
	}
}

// TestClassifyConflict is the criterion the user chose: both sides moved since
// they last agreed, so dross refuses rather than picking a winner.
func TestClassifyConflict(t *testing.T) {
	task := &phase.Task{ID: "t-1", Status: phase.StatusDone} // the plan moved
	link := board.TaskLink{Issue: "PROJ-7", PlanStatus: phase.StatusInProgress, BoardState: StatusTaskInProgress}
	iss := &forge.Issue{Key: "PROJ-7", Labels: statusLabels(StatusTaskInReview)} // and so did the board

	got := ClassifyTaskMove(task, link, iss)
	if got.Kind != TaskConflict {
		t.Fatalf("kind = %v, want TaskConflict", got.Kind)
	}
	if got.PlanStatus != phase.StatusDone || got.BoardState != StatusTaskInReview {
		t.Errorf("the verdict must carry BOTH current values for the refusal to name them: %+v", got)
	}
	if got.WasPlan != phase.StatusInProgress || got.WasBoard != StatusTaskInProgress {
		t.Errorf("the verdict must carry the agreement point it was judged against: %+v", got)
	}
}

// TestClassifyPlanMoved: outbound is task-sync's job, so this reports rather
// than pushing — but it must NOT be mistaken for a board move and reverted.
func TestClassifyPlanMoved(t *testing.T) {
	task := &phase.Task{ID: "t-1", Status: phase.StatusDone}
	link := board.TaskLink{Issue: "PROJ-7", PlanStatus: phase.StatusInProgress, BoardState: StatusTaskInProgress}
	iss := &forge.Issue{Key: "PROJ-7", Labels: statusLabels(StatusTaskInProgress)}

	if got := ClassifyTaskMove(task, link, iss); got.Kind != TaskPlanMoved {
		t.Errorf("kind = %v, want TaskPlanMoved — the local change must not be reverted from the board", got.Kind)
	}
}

func TestClassifyUnchanged(t *testing.T) {
	task := &phase.Task{ID: "t-1", Status: phase.StatusInProgress}
	link := board.TaskLink{Issue: "PROJ-7", PlanStatus: phase.StatusInProgress, BoardState: StatusTaskInProgress}
	iss := &forge.Issue{Key: "PROJ-7", Labels: statusLabels(StatusTaskInProgress)}

	if got := ClassifyTaskMove(task, link, iss); got.Kind != TaskUnchanged {
		t.Errorf("kind = %v, want TaskUnchanged", got.Kind)
	}
}

// TestClassifyUnsyncedIsNotAMove is the migration's runtime half: a link
// carrying no agreement point cannot attribute a difference, so it must not be
// applied as though the board had moved.
func TestClassifyUnsyncedIsNotAMove(t *testing.T) {
	task := &phase.Task{ID: "t-1", Status: phase.StatusPending}
	link := board.TaskLink{Issue: "PROJ-7"} // migrated from the string shape
	iss := &forge.Issue{Key: "PROJ-7", Labels: statusLabels(StatusTaskInReview)}

	got := ClassifyTaskMove(task, link, iss)
	if got.Kind != TaskUnsynced {
		t.Fatalf("kind = %v, want TaskUnsynced — with no agreement point, a difference cannot be attributed", got.Kind)
	}
	if got.NewStatus != "" {
		t.Errorf("an unsynced task proposed a write (%q) — that is a guess, not a move", got.NewStatus)
	}
}

// TestClassifyIgnoresAColumnDrossDoesNotMirror: a card dragged to some other
// column must not be invented into a plan status that means something else.
func TestClassifyIgnoresAColumnDrossDoesNotMirror(t *testing.T) {
	task := &phase.Task{ID: "t-1", Status: phase.StatusInProgress}
	link := board.TaskLink{Issue: "PROJ-7", PlanStatus: phase.StatusInProgress, BoardState: StatusTaskInProgress}
	iss := &forge.Issue{Key: "PROJ-7", Labels: statusLabels("uat")} // a phase-level status

	got := ClassifyTaskMove(task, link, iss)
	if got.Kind == TaskBoardMoved {
		t.Errorf("a column dross does not mirror was applied as %q", got.NewStatus)
	}
}

// TestLifecycleMappingRoundTrips pins the vocabulary conversion that used to
// exist only as prose in execute.md.
func TestLifecycleMappingRoundTrips(t *testing.T) {
	for _, planStatus := range []string{phase.StatusInProgress, phase.StatusDone} {
		lifecycle, ok := LifecycleForPlanStatus(planStatus)
		if !ok {
			t.Fatalf("%s has no board lifecycle status", planStatus)
		}
		back, ok := PlanStatusForLifecycle(lifecycle)
		if !ok || back != planStatus {
			t.Errorf("%s -> %s -> %q; want it back", planStatus, lifecycle, back)
		}
	}
	// pending and failed are deliberately unmirrored.
	if _, ok := LifecycleForPlanStatus(phase.StatusPending); ok {
		t.Error("pending must not assert a board status — every sync would relabel to say nothing changed")
	}
	if _, ok := LifecycleForPlanStatus(phase.StatusFailed); ok {
		t.Error("failed is a local judgement about a run, not a board column")
	}
}

func TestProviderWorkflowStateSupport(t *testing.T) {
	for _, p := range []string{"youtrack", "jira", "YouTrack"} {
		if !ProviderHasWorkflowState(p) {
			t.Errorf("%s can express a workflow state", p)
		}
	}
	for _, p := range []string{"forgejo", "gitea", "gitlab", "github", ""} {
		if ProviderHasWorkflowState(p) {
			t.Errorf("%s has no workflow field — claiming otherwise would report 'nothing moved' for a board that cannot answer", p)
		}
	}
}

func TestLifecycleFromLabels(t *testing.T) {
	if got := LifecycleFromLabels([]string{LabelMarker, "dross/status:task-in-review", "other"}); got != StatusTaskInReview {
		t.Errorf("got %q, want task-in-review", got)
	}
	if got := LifecycleFromLabels([]string{LabelMarker}); got != "" {
		t.Errorf("got %q, want empty when no status label is present", got)
	}
}

// TestReportAppliesABoardMove is the write path: --apply must change plan.toml
// AND advance the ledger, or the next run re-applies the same move forever.
func TestReportAppliesABoardMove(t *testing.T) {
	var buf bytes.Buffer
	ctx, plan, planPath := pullFixture(t, pullPlan, &buf)
	moves := []TaskMoveVerdict{{
		TaskID: "t-1", Issue: "PROJ-1", Kind: TaskBoardMoved,
		PlanStatus: phase.StatusInProgress, BoardState: StatusTaskInReview,
		NewStatus: phase.StatusDone,
	}}
	if err := ReportTaskMoves(ctx, "p1", plan, planPath, moves, true); err != nil {
		t.Fatalf("apply: %v", err)
	}
	out := buf.String()
	if got := planTask(t, planPath, "t-1").Status; got != phase.StatusDone {
		t.Errorf("plan status = %q, want done — the board move was not written", got)
	}
	link, ok := ctx.Board.TaskLinkFor("p1", "t-1")
	if !ok || link.PlanStatus != phase.StatusDone || link.BoardState != StatusTaskInReview {
		t.Errorf("ledger not advanced: %+v — the next run would re-apply this move", link)
	}
	if !strings.Contains(out, "applied 1") {
		t.Errorf("the write must be reported: %s", out)
	}
}

// TestReportDryRunWritesNothing: the default must never surprise anyone with a
// plan.toml mutation.
func TestReportDryRunWritesNothing(t *testing.T) {
	var buf bytes.Buffer
	ctx, plan, planPath := pullFixture(t, pullPlan, &buf)
	before := mustRead(t, planPath)
	moves := []TaskMoveVerdict{{
		TaskID: "t-1", Issue: "PROJ-1", Kind: TaskBoardMoved,
		PlanStatus: phase.StatusInProgress, BoardState: StatusTaskInReview,
		NewStatus: phase.StatusDone,
	}}
	if err := ReportTaskMoves(ctx, "p1", plan, planPath, moves, false); err != nil {
		t.Fatalf("dry run: %v", err)
	}
	out := buf.String()
	assertPlanUnchanged(t, planPath, before)
	if !strings.Contains(out, "would move") || !strings.Contains(out, "--apply") {
		t.Errorf("a dry run must say what it would do and how to do it: %s", out)
	}
}

// TestReportConflictExitsNonZeroAfterApplyingTheRest: a contested task must not
// hide the clean moves around it.
func TestReportConflictExitsNonZeroAfterApplyingTheRest(t *testing.T) {
	var buf bytes.Buffer
	ctx, plan, planPath := pullFixture(t, pullPlan, &buf)
	moves := []TaskMoveVerdict{
		{TaskID: "t-1", Issue: "PROJ-1", Kind: TaskBoardMoved,
			PlanStatus: phase.StatusInProgress, BoardState: StatusTaskInReview, NewStatus: phase.StatusDone},
		{TaskID: "t-2", Issue: "PROJ-2", Kind: TaskConflict,
			PlanStatus: phase.StatusDone, BoardState: StatusTaskInProgress,
			WasPlan: phase.StatusInProgress, WasBoard: StatusTaskInReview},
	}
	var err error
	err = ReportTaskMoves(ctx, "p1", plan, planPath, moves, true)
	out := buf.String()
	if err == nil {
		t.Fatal("a conflict must exit non-zero")
	}
	if !strings.Contains(err.Error(), "both sides") {
		t.Errorf("unexpected error: %v", err)
	}
	// The clean move still landed.
	if got := planTask(t, planPath, "t-1").Status; got != phase.StatusDone {
		t.Errorf("t-1 = %q — the conflict on t-2 suppressed an unrelated clean move", got)
	}
	// And the refusal named both values.
	if !strings.Contains(out, "CONFLICT") || !strings.Contains(out, StatusTaskInProgress) {
		t.Errorf("the conflict must name what each side holds: %s", out)
	}
}

func TestReportNothingToDo(t *testing.T) {
	var buf bytes.Buffer
	ctx, plan, planPath := pullFixture(t, pullPlan, &buf)
	if err := ReportTaskMoves(ctx, "p1", plan, planPath, []TaskMoveVerdict{
		{TaskID: "t-1", Kind: TaskUnchanged},
	}, true); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "no task moves") {
		t.Errorf("an unchanged board must say so plainly: %s", out)
	}
}

// TestReportNarratesPlanMovedAndUnsynced: both are reported rather than
// silently skipped — a task the tool declined to touch must say why.
func TestReportNarratesPlanMovedAndUnsynced(t *testing.T) {
	var buf bytes.Buffer
	ctx, plan, planPath := pullFixture(t, pullPlan, &buf)
	if err := ReportTaskMoves(ctx, "p1", plan, planPath, []TaskMoveVerdict{
		{TaskID: "t-1", Kind: TaskPlanMoved},
		{TaskID: "t-2", Kind: TaskUnsynced},
	}, false); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "task sync p1 t-1") {
		t.Errorf("a plan-side move must name the command that pushes it: %s", out)
	}
	if !strings.Contains(out, "no agreement point") {
		t.Errorf("an unsynced task must explain why it was skipped: %s", out)
	}
}

// TestCollectTaskMovesSkipsUnmirroredTasks: a task with no issue is task-sync's
// job, and asking the board about it would be a round trip for nothing.
func TestCollectTaskMovesSkipsUnmirroredTasks(t *testing.T) {
	ctx, plan, _ := pullFixture(t, pullPlan, new(bytes.Buffer))
	ctx.Client = &pullFakeClient{t: t, issues: map[string]*forge.Issue{
		"PROJ-1": {Key: "PROJ-1", Labels: statusLabels(StatusTaskInReview)},
	}}
	ctx.Board.SetTaskSynced("p1", "t-1", "PROJ-1", phase.StatusInProgress, StatusTaskInProgress)
	// t-2 deliberately has no mapping.
	moves, err := CollectTaskMoves(ctx, "p1", plan)
	if err != nil {
		t.Fatal(err)
	}
	if len(moves) != 1 || moves[0].TaskID != "t-1" {
		t.Fatalf("moves = %+v, want only the mirrored task", moves)
	}
	if moves[0].Kind != TaskBoardMoved {
		t.Errorf("kind = %v, want TaskBoardMoved", moves[0].Kind)
	}
}

// TestCollectTaskMovesSurfacesABoardFailure: a board that cannot be read must
// fail the run rather than report an empty set of moves.
func TestCollectTaskMovesSurfacesABoardFailure(t *testing.T) {
	ctx, plan, _ := pullFixture(t, pullPlan, new(bytes.Buffer))
	ctx.Client = &pullFakeClient{t: t, err: errors.New("500 from the tracker")}
	ctx.Board.SetTaskSynced("p1", "t-1", "PROJ-1", phase.StatusInProgress, StatusTaskInProgress)
	if _, err := CollectTaskMoves(ctx, "p1", plan); err == nil {
		t.Fatal("an unreadable board must not read as 'no moves'")
	}
}

// TestCollectTaskMovesSortsByTaskID pins the ordering. Unsorted output makes
// two runs of an unchanged board produce different text, which is the
// difference between a diff you can read and one you re-read every time.
func TestCollectTaskMovesSortsByTaskID(t *testing.T) {
	ctx, plan, _ := pullFixture(t, pullPlan, new(bytes.Buffer))
	ctx.Client = &pullFakeClient{t: t, issues: map[string]*forge.Issue{
		"PROJ-1": {Key: "PROJ-1", Labels: statusLabels(StatusTaskInProgress)},
		"PROJ-2": {Key: "PROJ-2", Labels: statusLabels(StatusTaskInProgress)},
	}}
	// Recorded t-2 first, so an unsorted collect would emit t-2 before t-1.
	ctx.Board.SetTaskSynced("p1", "t-2", "PROJ-2", phase.StatusInProgress, StatusTaskInProgress)
	ctx.Board.SetTaskSynced("p1", "t-1", "PROJ-1", phase.StatusInProgress, StatusTaskInProgress)

	moves, err := CollectTaskMoves(ctx, "p1", plan)
	if err != nil {
		t.Fatal(err)
	}
	if len(moves) != 2 {
		t.Fatalf("moves = %+v, want both mirrored tasks", moves)
	}
	if moves[0].TaskID != "t-1" || moves[1].TaskID != "t-2" {
		t.Errorf("order = %s,%s — want t-1 then t-2", moves[0].TaskID, moves[1].TaskID)
	}
}
