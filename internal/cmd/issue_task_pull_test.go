package cmd

import (
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
	if !strings.Contains(err.Error(), "task-sync") {
		t.Errorf("the refusal must say what still works: %v", err)
	}
}

func projectWithProvider(provider string) *project.Project {
	p := &project.Project{}
	p.Board.Provider = provider
	p.Board.Enabled = true
	return p
}
