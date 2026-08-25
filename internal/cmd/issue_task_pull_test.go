package cmd

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/board"
	"github.com/Rivil/dross/internal/forge"
	"github.com/Rivil/dross/internal/phase"
	"github.com/Rivil/dross/internal/project"
)

func statusLabels(lifecycle string) []string {
	l := []string{labelMarker}
	if lifecycle != "" {
		l = append(l, statusLabel(lifecycle))
	}
	return l
}

// TestClassifyBoardMoved: the board moved and the plan did not, so the plan
// follows. This is the feature.
func TestClassifyBoardMoved(t *testing.T) {
	task := &phase.Task{ID: "t-1", Status: phase.StatusInProgress}
	link := board.TaskLink{Issue: "PROJ-7", PlanStatus: phase.StatusInProgress, BoardState: statusTaskInProgress}
	iss := &forge.Issue{Key: "PROJ-7", Labels: statusLabels(statusTaskInReview)}

	got := classifyTaskMove(task, link, iss)
	if got.Kind != taskBoardMoved {
		t.Fatalf("kind = %v, want taskBoardMoved", got.Kind)
	}
	if got.NewStatus != phase.StatusDone {
		t.Errorf("new status = %q, want %q — task-in-review is what done looks like on a board", got.NewStatus, phase.StatusDone)
	}
}

// TestClassifyConflict is the criterion the user chose: both sides moved since
// they last agreed, so dross refuses rather than picking a winner.
func TestClassifyConflict(t *testing.T) {
	task := &phase.Task{ID: "t-1", Status: phase.StatusDone} // the plan moved
	link := board.TaskLink{Issue: "PROJ-7", PlanStatus: phase.StatusInProgress, BoardState: statusTaskInProgress}
	iss := &forge.Issue{Key: "PROJ-7", Labels: statusLabels(statusTaskInReview)} // and so did the board

	got := classifyTaskMove(task, link, iss)
	if got.Kind != taskConflict {
		t.Fatalf("kind = %v, want taskConflict", got.Kind)
	}
	if got.PlanStatus != phase.StatusDone || got.BoardState != statusTaskInReview {
		t.Errorf("the verdict must carry BOTH current values for the refusal to name them: %+v", got)
	}
	if got.WasPlan != phase.StatusInProgress || got.WasBoard != statusTaskInProgress {
		t.Errorf("the verdict must carry the agreement point it was judged against: %+v", got)
	}
}

// TestClassifyPlanMoved: outbound is task-sync's job, so this reports rather
// than pushing — but it must NOT be mistaken for a board move and reverted.
func TestClassifyPlanMoved(t *testing.T) {
	task := &phase.Task{ID: "t-1", Status: phase.StatusDone}
	link := board.TaskLink{Issue: "PROJ-7", PlanStatus: phase.StatusInProgress, BoardState: statusTaskInProgress}
	iss := &forge.Issue{Key: "PROJ-7", Labels: statusLabels(statusTaskInProgress)}

	if got := classifyTaskMove(task, link, iss); got.Kind != taskPlanMoved {
		t.Errorf("kind = %v, want taskPlanMoved — the local change must not be reverted from the board", got.Kind)
	}
}

func TestClassifyUnchanged(t *testing.T) {
	task := &phase.Task{ID: "t-1", Status: phase.StatusInProgress}
	link := board.TaskLink{Issue: "PROJ-7", PlanStatus: phase.StatusInProgress, BoardState: statusTaskInProgress}
	iss := &forge.Issue{Key: "PROJ-7", Labels: statusLabels(statusTaskInProgress)}

	if got := classifyTaskMove(task, link, iss); got.Kind != taskUnchanged {
		t.Errorf("kind = %v, want taskUnchanged", got.Kind)
	}
}

// TestClassifyUnsyncedIsNotAMove is the migration's runtime half: a link
// carrying no agreement point cannot attribute a difference, so it must not be
// applied as though the board had moved.
func TestClassifyUnsyncedIsNotAMove(t *testing.T) {
	task := &phase.Task{ID: "t-1", Status: phase.StatusPending}
	link := board.TaskLink{Issue: "PROJ-7"} // migrated from the string shape
	iss := &forge.Issue{Key: "PROJ-7", Labels: statusLabels(statusTaskInReview)}

	got := classifyTaskMove(task, link, iss)
	if got.Kind != taskUnsynced {
		t.Fatalf("kind = %v, want taskUnsynced — with no agreement point, a difference cannot be attributed", got.Kind)
	}
	if got.NewStatus != "" {
		t.Errorf("an unsynced task proposed a write (%q) — that is a guess, not a move", got.NewStatus)
	}
}

// TestClassifyIgnoresAColumnDrossDoesNotMirror: a card dragged to some other
// column must not be invented into a plan status that means something else.
func TestClassifyIgnoresAColumnDrossDoesNotMirror(t *testing.T) {
	task := &phase.Task{ID: "t-1", Status: phase.StatusInProgress}
	link := board.TaskLink{Issue: "PROJ-7", PlanStatus: phase.StatusInProgress, BoardState: statusTaskInProgress}
	iss := &forge.Issue{Key: "PROJ-7", Labels: statusLabels("uat")} // a phase-level status

	got := classifyTaskMove(task, link, iss)
	if got.Kind == taskBoardMoved {
		t.Errorf("a column dross does not mirror was applied as %q", got.NewStatus)
	}
}

// TestLifecycleMappingRoundTrips pins the vocabulary conversion that used to
// exist only as prose in execute.md.
func TestLifecycleMappingRoundTrips(t *testing.T) {
	for _, planStatus := range []string{phase.StatusInProgress, phase.StatusDone} {
		lifecycle, ok := lifecycleForPlanStatus(planStatus)
		if !ok {
			t.Fatalf("%s has no board lifecycle status", planStatus)
		}
		back, ok := planStatusForLifecycle(lifecycle)
		if !ok || back != planStatus {
			t.Errorf("%s -> %s -> %q; want it back", planStatus, lifecycle, back)
		}
	}
	// pending and failed are deliberately unmirrored.
	if _, ok := lifecycleForPlanStatus(phase.StatusPending); ok {
		t.Error("pending must not assert a board status — every sync would relabel to say nothing changed")
	}
	if _, ok := lifecycleForPlanStatus(phase.StatusFailed); ok {
		t.Error("failed is a local judgement about a run, not a board column")
	}
}

func TestProviderWorkflowStateSupport(t *testing.T) {
	for _, p := range []string{"youtrack", "jira", "YouTrack"} {
		if !providerHasWorkflowState(p) {
			t.Errorf("%s can express a workflow state", p)
		}
	}
	for _, p := range []string{"forgejo", "gitea", "gitlab", "github", ""} {
		if providerHasWorkflowState(p) {
			t.Errorf("%s has no workflow field — claiming otherwise would report 'nothing moved' for a board that cannot answer", p)
		}
	}
}

func TestLifecycleFromLabels(t *testing.T) {
	if got := lifecycleFromLabels([]string{labelMarker, "dross/status:task-in-review", "other"}); got != statusTaskInReview {
		t.Errorf("got %q, want task-in-review", got)
	}
	if got := lifecycleFromLabels([]string{labelMarker}); got != "" {
		t.Errorf("got %q, want empty when no status label is present", got)
	}
}

// TestTaskPullRefusesAProviderWithoutWorkflowState: reporting "no changes" for
// a board that cannot answer is the silent-zero fault in a new place.
func TestTaskPullRefusesAProviderWithoutWorkflowState(t *testing.T) {
	ctx := &boardCtx{proj: projectWithProvider("forgejo")}
	err := taskPull(ctx, "p1", false)
	if err == nil {
		t.Fatal("a board with no workflow field must be refused")
	}
	if !strings.Contains(err.Error(), "workflow state") {
		t.Errorf("the refusal must say why: %v", err)
	}
	if !strings.Contains(err.Error(), "task sync") {
		t.Errorf("the refusal must say what still works: %v", err)
	}
}

func projectWithProvider(provider string) *project.Project {
	p := &project.Project{}
	p.Board.Provider = provider
	p.Board.Enabled = true
	return p
}

// pullFixture is a repo with a phase plan and a board.json ledger, ready for
// reportTaskMoves to write into.
func pullFixture(t *testing.T, planBody string) (*boardCtx, *phase.Plan, string) {
	t.Helper()
	dir := t.TempDir()
	chdir(t, dir)
	scaffoldPhaseWithPlan(t, "p1", planBody) // runs Init itself
	plan, _, planPath, err := loadPhasePlanAndSpec("p1")
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(dir, ".dross")
	bd := board.New()
	ctx := &boardCtx{
		board:     bd,
		proj:      projectWithProvider("youtrack"),
		root:      root,
		boardPath: filepath.Join(root, board.File),
	}
	return ctx, plan, planPath
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

// TestReportAppliesABoardMove is the write path: --apply must change plan.toml
// AND advance the ledger, or the next run re-applies the same move forever.
func TestReportAppliesABoardMove(t *testing.T) {
	ctx, plan, planPath := pullFixture(t, pullPlan)
	moves := []taskMoveVerdict{{
		TaskID: "t-1", Issue: "PROJ-1", Kind: taskBoardMoved,
		PlanStatus: phase.StatusInProgress, BoardState: statusTaskInReview,
		NewStatus: phase.StatusDone,
	}}
	out := captureStdout(t, func() {
		if err := reportTaskMoves(ctx, "p1", plan, planPath, moves, true); err != nil {
			t.Fatalf("apply: %v", err)
		}
	})
	if got := planTask(t, planPath, "t-1").Status; got != phase.StatusDone {
		t.Errorf("plan status = %q, want done — the board move was not written", got)
	}
	link, ok := ctx.board.TaskLinkFor("p1", "t-1")
	if !ok || link.PlanStatus != phase.StatusDone || link.BoardState != statusTaskInReview {
		t.Errorf("ledger not advanced: %+v — the next run would re-apply this move", link)
	}
	if !strings.Contains(out, "applied 1") {
		t.Errorf("the write must be reported: %s", out)
	}
}

// TestReportDryRunWritesNothing: the default must never surprise anyone with a
// plan.toml mutation.
func TestReportDryRunWritesNothing(t *testing.T) {
	ctx, plan, planPath := pullFixture(t, pullPlan)
	before := mustRead(t, planPath)
	moves := []taskMoveVerdict{{
		TaskID: "t-1", Issue: "PROJ-1", Kind: taskBoardMoved,
		PlanStatus: phase.StatusInProgress, BoardState: statusTaskInReview,
		NewStatus: phase.StatusDone,
	}}
	out := captureStdout(t, func() {
		if err := reportTaskMoves(ctx, "p1", plan, planPath, moves, false); err != nil {
			t.Fatalf("dry run: %v", err)
		}
	})
	assertPlanUnchanged(t, planPath, before)
	if !strings.Contains(out, "would move") || !strings.Contains(out, "--apply") {
		t.Errorf("a dry run must say what it would do and how to do it: %s", out)
	}
}

// TestReportConflictExitsNonZeroAfterApplyingTheRest: a contested task must not
// hide the clean moves around it.
func TestReportConflictExitsNonZeroAfterApplyingTheRest(t *testing.T) {
	ctx, plan, planPath := pullFixture(t, pullPlan)
	moves := []taskMoveVerdict{
		{TaskID: "t-1", Issue: "PROJ-1", Kind: taskBoardMoved,
			PlanStatus: phase.StatusInProgress, BoardState: statusTaskInReview, NewStatus: phase.StatusDone},
		{TaskID: "t-2", Issue: "PROJ-2", Kind: taskConflict,
			PlanStatus: phase.StatusDone, BoardState: statusTaskInProgress,
			WasPlan: phase.StatusInProgress, WasBoard: statusTaskInReview},
	}
	var err error
	out := captureStdout(t, func() {
		err = reportTaskMoves(ctx, "p1", plan, planPath, moves, true)
	})
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
	if !strings.Contains(out, "CONFLICT") || !strings.Contains(out, statusTaskInProgress) {
		t.Errorf("the conflict must name what each side holds: %s", out)
	}
}

func TestReportNothingToDo(t *testing.T) {
	ctx, plan, planPath := pullFixture(t, pullPlan)
	out := captureStdout(t, func() {
		if err := reportTaskMoves(ctx, "p1", plan, planPath, []taskMoveVerdict{
			{TaskID: "t-1", Kind: taskUnchanged},
		}, true); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(out, "no task moves") {
		t.Errorf("an unchanged board must say so plainly: %s", out)
	}
}

// TestReportNarratesPlanMovedAndUnsynced: both are reported rather than
// silently skipped — a task the tool declined to touch must say why.
func TestReportNarratesPlanMovedAndUnsynced(t *testing.T) {
	ctx, plan, planPath := pullFixture(t, pullPlan)
	out := captureStdout(t, func() {
		if err := reportTaskMoves(ctx, "p1", plan, planPath, []taskMoveVerdict{
			{TaskID: "t-1", Kind: taskPlanMoved},
			{TaskID: "t-2", Kind: taskUnsynced},
		}, false); err != nil {
			t.Fatal(err)
		}
	})
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
	ctx, plan, _ := pullFixture(t, pullPlan)
	ctx.client = &pullFakeClient{t: t, issues: map[string]*forge.Issue{
		"PROJ-1": {Key: "PROJ-1", Labels: statusLabels(statusTaskInReview)},
	}}
	ctx.board.SetTaskSynced("p1", "t-1", "PROJ-1", phase.StatusInProgress, statusTaskInProgress)
	// t-2 deliberately has no mapping.
	moves, err := collectTaskMoves(ctx, "p1", plan)
	if err != nil {
		t.Fatal(err)
	}
	if len(moves) != 1 || moves[0].TaskID != "t-1" {
		t.Fatalf("moves = %+v, want only the mirrored task", moves)
	}
	if moves[0].Kind != taskBoardMoved {
		t.Errorf("kind = %v, want taskBoardMoved", moves[0].Kind)
	}
}

// TestCollectTaskMovesSurfacesABoardFailure: a board that cannot be read must
// fail the run rather than report an empty set of moves.
func TestCollectTaskMovesSurfacesABoardFailure(t *testing.T) {
	ctx, plan, _ := pullFixture(t, pullPlan)
	ctx.client = &pullFakeClient{t: t, err: errors.New("500 from the tracker")}
	ctx.board.SetTaskSynced("p1", "t-1", "PROJ-1", phase.StatusInProgress, statusTaskInProgress)
	if _, err := collectTaskMoves(ctx, "p1", plan); err == nil {
		t.Fatal("an unreadable board must not read as 'no moves'")
	}
}

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

// TestCollectTaskMovesSortsByTaskID pins the ordering. Unsorted output makes
// two runs of an unchanged board produce different text, which is the
// difference between a diff you can read and one you re-read every time.
func TestCollectTaskMovesSortsByTaskID(t *testing.T) {
	ctx, plan, _ := pullFixture(t, pullPlan)
	ctx.client = &pullFakeClient{t: t, issues: map[string]*forge.Issue{
		"PROJ-1": {Key: "PROJ-1", Labels: statusLabels(statusTaskInProgress)},
		"PROJ-2": {Key: "PROJ-2", Labels: statusLabels(statusTaskInProgress)},
	}}
	// Recorded t-2 first, so an unsorted collect would emit t-2 before t-1.
	ctx.board.SetTaskSynced("p1", "t-2", "PROJ-2", phase.StatusInProgress, statusTaskInProgress)
	ctx.board.SetTaskSynced("p1", "t-1", "PROJ-1", phase.StatusInProgress, statusTaskInProgress)

	moves, err := collectTaskMoves(ctx, "p1", plan)
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

// TestTaskPullSurfacesAMissingPhase: a phase id that names nothing must fail,
// not report an empty board.
func TestTaskPullSurfacesAMissingPhase(t *testing.T) {
	ctx, _, _ := pullFixture(t, pullPlan)
	ctx.client = &pullFakeClient{t: t}
	if err := taskPull(ctx, "no-such-phase", false); err == nil {
		t.Fatal("an unknown phase must be refused, not read as 'no moves'")
	}
}

// TestTaskPullSurfacesABoardFailure: the same at the taskPull boundary — an
// unreadable board must reach the caller rather than being reported as clean.
func TestTaskPullSurfacesABoardFailure(t *testing.T) {
	ctx, _, _ := pullFixture(t, pullPlan)
	ctx.client = &pullFakeClient{t: t, err: errors.New("502 from the tracker")}
	ctx.board.SetTaskSynced("p1", "t-1", "PROJ-1", phase.StatusInProgress, statusTaskInProgress)
	err := taskPull(ctx, "p1", false)
	if err == nil {
		t.Fatal("a board failure must fail the pull")
	}
	if !strings.Contains(err.Error(), "502") {
		t.Errorf("the cause must survive to the caller: %v", err)
	}
}

// TestTaskPullCommandNoOpsWhenBoardIsOff drives the command itself, which the
// direct taskPull tests never reach: board sync off is a silent no-op, because
// every workflow prompt calls `dross issue …` unconditionally on that promise.
func TestTaskPullCommandNoOpsWhenBoardIsOff(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	if err := runCmd(t, Init()); err != nil {
		t.Fatal(err)
	}
	out := captureStdout(t, func() {
		if err := runCmd(t, Issue(), "task", "pull", "p1"); err != nil {
			t.Fatalf("board-off must be a no-op, got %v", err)
		}
	})
	if !strings.Contains(out, "board sync is off") {
		t.Errorf("the no-op must say why it did nothing: %s", out)
	}
}

// TestTaskPullCommandNeedsAPhase: with no argument and no current_phase there
// is nothing to resolve, and guessing would pull against the wrong plan.
func TestTaskPullCommandNeedsAPhase(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `[]`)
	}))
	t.Cleanup(srv.Close)
	youtrackBoardRepo(t, srv.URL)

	if err := runCmd(t, Issue(), "task", "pull"); err == nil {
		t.Fatal("no phase id and no current_phase must be refused")
	}
}

// TestTaskPullCommandSurfacesASetupFailure drives the openBoard error arm: a
// board configured but unusable must fail loudly here, unlike `issue pull`
// whose --json contract deliberately routes failures into its envelope.
func TestTaskPullCommandSurfacesASetupFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	t.Cleanup(srv.Close)
	youtrackBoardRepo(t, srv.URL)
	mustRunSet(t, "board.provider", "not-a-real-tracker")

	if err := runCmd(t, Issue(), "task", "pull", "p1"); err == nil {
		t.Fatal("an unusable board must fail the command")
	}
}
