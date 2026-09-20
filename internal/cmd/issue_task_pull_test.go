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
	"github.com/Rivil/dross/internal/boardsync"
	"github.com/Rivil/dross/internal/forge"
	"github.com/Rivil/dross/internal/phase"
	"github.com/Rivil/dross/internal/project"
)

// TestTaskPullRefusesAProviderWithoutWorkflowState: reporting "no changes" for
// a board that cannot answer is the silent-zero fault in a new place.
func TestTaskPullRefusesAProviderWithoutWorkflowState(t *testing.T) {
	ctx := &boardCtx{Proj: projectWithProvider("forgejo")}
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
		Board:     bd,
		Proj:      projectWithProvider("youtrack"),
		Root:      root,
		BoardPath: filepath.Join(root, board.File),
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

// TestTaskPullSurfacesAMissingPhase: a phase id that names nothing must fail,
// not report an empty board.
func TestTaskPullSurfacesAMissingPhase(t *testing.T) {
	ctx, _, _ := pullFixture(t, pullPlan)
	ctx.Client = &pullFakeClient{t: t}
	if err := taskPull(ctx, "no-such-phase", false); err == nil {
		t.Fatal("an unknown phase must be refused, not read as 'no moves'")
	}
}

// TestTaskPullSurfacesABoardFailure: the same at the taskPull boundary — an
// unreadable board must reach the caller rather than being reported as clean.
func TestTaskPullSurfacesABoardFailure(t *testing.T) {
	ctx, _, _ := pullFixture(t, pullPlan)
	ctx.Client = &pullFakeClient{t: t, err: errors.New("502 from the tracker")}
	ctx.Board.SetTaskSynced("p1", "t-1", "PROJ-1", phase.StatusInProgress, boardsync.StatusTaskInProgress)
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
