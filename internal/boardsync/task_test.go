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

// refusingBoard is a fakeBoard whose CloseIssue refuses one key.
type refusingBoard struct {
	*fakeBoard
	refuse string
}

func (r *refusingBoard) CloseIssue(key string) error {
	if key == r.refuse {
		return errors.New("tracker refused the transition")
	}
	return r.fakeBoard.CloseIssue(key)
}

// taskFixture is a phase with two plan tasks and a synced phase issue.
func taskFixture(t *testing.T) (*Ctx, *fakeBoard) {
	t.Helper()
	root := filepath.Join(t.TempDir(), ".dross")
	dir := filepath.Join(root, "phases", "auth")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	plan := "[phase]\nid = \"auth\"\n\n[[task]]\nid = \"t-1\"\nwave = 1\ntitle = \"first\"\nfiles = [\"a.go\"]\ncovers = [\"c-1\"]\nstatus = \"pending\"\n\n[[task]]\nid = \"t-2\"\nwave = 1\ntitle = \"second\"\nfiles = [\"b.go\"]\ncovers = [\"c-1\"]\nstatus = \"pending\"\n"
	if err := os.WriteFile(filepath.Join(dir, "plan.toml"), []byte(plan), 0o644); err != nil {
		t.Fatal(err)
	}
	client := newFakeBoard()
	bd := board.New()
	parent, _ := client.CreateIssue(forge.IssueInput{Title: "auth — Auth", Labels: []string{LabelMarker, PhaseLabel("auth")}})
	bd.SetPhase("auth", parent.Key)
	return &Ctx{Client: client, Board: bd, Proj: &project.Project{}, Root: root, BoardPath: filepath.Join(root, board.File), Out: new(bytes.Buffer)}, client
}

// TestSyncTasksMintsOneIssuePerTask: every task gets its own labelled issue,
// linked in board.json; a second run updates rather than re-creates; `only`
// narrows to one task.
func TestSyncTasksMintsOneIssuePerTask(t *testing.T) {
	ctx, client := taskFixture(t)
	if err := SyncTasks(ctx, "auth", "", StatusTaskInProgress, false); err != nil {
		t.Fatalf("SyncTasks: %v", err)
	}
	if len(client.created) != 3 { // the phase issue + two tasks
		t.Fatalf("created %d issues, want 3", len(client.created))
	}
	for _, id := range []string{"t-1", "t-2"} {
		key, ok := ctx.Board.TaskIssue("auth", id)
		if !ok {
			t.Fatalf("board.json has no issue for %s", id)
		}
		labels := client.issues[key].Labels
		for _, want := range []string{LabelMarker, TaskLabel("auth", id), StatusLabel(StatusTaskInProgress)} {
			if !contains(labels, want) {
				t.Errorf("%s labels %v lack %s", id, labels, want)
			}
		}
	}
	if err := SyncTasks(ctx, "auth", "t-2", StatusTaskInReview, false); err != nil {
		t.Fatal(err)
	}
	if len(client.created) != 3 {
		t.Errorf("a re-sync minted a duplicate (%d issues)", len(client.created))
	}
	key1, _ := ctx.Board.TaskIssue("auth", "t-1")
	key2, _ := ctx.Board.TaskIssue("auth", "t-2")
	if contains(client.issues[key1].Labels, StatusLabel(StatusTaskInReview)) {
		t.Error("`only` t-2 touched t-1's status")
	}
	if !contains(client.issues[key2].Labels, StatusLabel(StatusTaskInReview)) || contains(client.issues[key2].Labels, StatusLabel(StatusTaskInProgress)) {
		t.Errorf("t-2's status label was not replaced: %v", client.issues[key2].Labels)
	}
}

// TestSyncTasksNeedsThePhaseIssue: without a synced phase there is nothing to
// relate the task cards to, and the run refuses by name.
func TestSyncTasksNeedsThePhaseIssue(t *testing.T) {
	ctx, _ := taskFixture(t)
	ctx.Board = board.New()
	err := SyncTasks(ctx, "auth", "", "", false)
	if err == nil || !strings.Contains(err.Error(), "dross issue phase sync auth") {
		t.Errorf("missing phase issue: %v", err)
	}
}

// TestSyncTasksPartialCloseNamesTheTaskAndKeepsGoing: a close the tracker
// refuses is a TaskCloseError for that card; the remaining cards still close,
// board.json is still saved, and the run fails at the END naming the card.
func TestSyncTasksPartialCloseNamesTheTaskAndKeepsGoing(t *testing.T) {
	ctx, client := taskFixture(t)
	if err := SyncTasks(ctx, "auth", "", StatusTaskInReview, false); err != nil {
		t.Fatal(err)
	}
	key1, _ := ctx.Board.TaskIssue("auth", "t-1")
	key2, _ := ctx.Board.TaskIssue("auth", "t-2")
	ctx.Client = &refusingBoard{fakeBoard: client, refuse: key1}

	err := SyncTasks(ctx, "auth", "", StatusTaskComplete, true)
	if err == nil {
		t.Fatal("a refused close did not fail the run")
	}
	if !strings.Contains(err.Error(), "refused 1 task close") || !strings.Contains(err.Error(), "task t-1") {
		t.Errorf("error = %v", err)
	}
	if client.issues[key2].State != "closed" {
		t.Error("the refusal on t-1 stopped t-2 from closing")
	}
	if client.issues[key1].State == "closed" {
		t.Error("the refused card reads closed")
	}
	if _, err := os.Stat(ctx.BoardPath); err != nil {
		t.Error("board.json was not saved on a partial run — the run is not resumable")
	}
}

// TestRenderTaskBodyShape: the body names the parent, the task, its title,
// files and test contract, and says edits do not travel back — the
// human-readable half of the card.
func TestRenderTaskBodyShape(t *testing.T) {
	body := RenderTaskBody("auth", "PROJ-1", phase.Task{ID: "t-1", Wave: 2, Title: "do it", Files: []string{"a.go", "b.go"}, TestContract: []string{"if x then y"}})
	for _, want := range []string{"PROJ-1", "`auth`", "`t-1`", "**do it**", "Files: a.go, b.go", "- if x then y", "do not travel back"} {
		if !strings.Contains(body, want) {
			t.Errorf("body lacks %q:\n%s", want, body)
		}
	}
}
