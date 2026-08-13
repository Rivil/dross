package cmd

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/forge"
)

// pullResult mirrors the envelope `issue pull --json` promises. Declaring it
// here rather than reusing the production type keeps the test honest about the
// wire shape a prompt actually parses.
type pullResult struct {
	Issues []forge.Issue `json:"issues"`
	Error  *string       `json:"error"`
}

func decodePull(t *testing.T, out string) pullResult {
	t.Helper()
	var got pullResult
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &got); err != nil {
		t.Fatalf("pull --json is not an {issues, error} envelope: %v\n%s", err, out)
	}
	return got
}

// TestIssuePullEnvelopeOnSuccess pins the success shape. A bare array — what
// pull emitted before — fails to unmarshal into the envelope at all, which is
// the point: the shape breaks once, visibly, in one place.
func TestIssuePullEnvelopeOnSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/issueTags") {
			_, _ = io.WriteString(w, `[{"name":"bug"}]`)
			return
		}
		_, _ = io.WriteString(w, `[{"idReadable":"PROJ-21","summary":"a real bug"}]`)
	}))
	t.Cleanup(srv.Close)

	youtrackBoardRepo(t, srv.URL)

	out := captureStdout(t, func() {
		if err := runCmd(t, Issue(), "pull", "--json"); err != nil {
			t.Fatalf("pull: %v", err)
		}
	})
	got := decodePull(t, out)
	if got.Error != nil {
		t.Errorf("Error = %q on a healthy board, want null", *got.Error)
	}
	if len(got.Issues) != 1 || got.Issues[0].Key != "PROJ-21" {
		t.Errorf("Issues = %+v, want the one inbound issue", got.Issues)
	}
}

// TestIssuePullEnvelopeOnBoardFailure is the c-2 case: a board that cannot be
// reached must be distinguishable from a board with nothing on it, without
// breaking the "calling dross issue … is always safe" contract the workflow
// prompts rely on. So: exit 0, and the failure travels in the payload.
func TestIssuePullEnvelopeOnBoardFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(500)
	}))
	t.Cleanup(srv.Close)

	youtrackBoardRepo(t, srv.URL)

	var runErr error
	out := captureStdout(t, func() {
		runErr = runCmd(t, Issue(), "pull", "--json")
	})
	if runErr != nil {
		t.Fatalf("a board failure must still exit 0, got %v", runErr)
	}
	got := decodePull(t, out)
	if got.Error == nil || *got.Error == "" {
		t.Fatalf("Error is empty on a 500 — the failure reads as an empty board: %s", out)
	}
	if got.Issues == nil {
		t.Error("Issues is null; want an empty array so consumers can index it")
	}
	if len(got.Issues) != 0 {
		t.Errorf("Issues = %+v on a failed fetch, want empty", got.Issues)
	}
}

// TestIssuePullEnvelopeWhenBoardDisabled pins that the off-board case emits the
// same envelope. Emitting a bare `[]` here would leave one consumer path still
// parsing an array.
func TestIssuePullEnvelopeWhenBoardDisabled(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	if err := runCmd(t, Init()); err != nil {
		t.Fatalf("init: %v", err)
	}

	out := captureStdout(t, func() {
		if err := runCmd(t, Issue(), "pull", "--json"); err != nil {
			t.Fatalf("pull: %v", err)
		}
	})
	if trimmed := strings.TrimSpace(out); trimmed == "[]" {
		t.Fatalf("board-off emitted a bare array %q, want the {issues, error} envelope", trimmed)
	}
	got := decodePull(t, out)
	if got.Error != nil {
		t.Errorf("Error = %q with board sync off, want null — off is not a failure", *got.Error)
	}
	if got.Issues == nil || len(got.Issues) != 0 {
		t.Errorf("Issues = %+v, want an empty array", got.Issues)
	}
}

// TestIssuePullHumanModeReportsUnreachable covers the non-JSON half of c-2.
// "no new issues on the board" on an unreachable board is the exact lie the
// criterion exists to kill.
func TestIssuePullHumanModeReportsUnreachable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(500)
	}))
	t.Cleanup(srv.Close)

	youtrackBoardRepo(t, srv.URL)

	var runErr error
	out := captureStdout(t, func() {
		runErr = runCmd(t, Issue(), "pull")
	})
	if runErr != nil {
		t.Fatalf("a board failure must still exit 0, got %v", runErr)
	}
	if !strings.Contains(out, "unreachable") {
		t.Errorf("stdout = %q, want it to report the board unreachable", out)
	}
	if strings.Contains(out, "no new issues on the board") {
		t.Errorf("stdout = %q — an unreachable board must not read as an empty one", out)
	}
}

// TestIssuePullMarkDoesNotStampAFailedPull pins the mark gate. Stamping
// last_pull for a fetch that never happened makes the next run skip issues it
// never saw — the silent-zero fault wearing a different hat.
func TestIssuePullMarkDoesNotStampAFailedPull(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(500)
	}))
	t.Cleanup(srv.Close)

	dir := youtrackBoardRepo(t, srv.URL)
	boardPath := filepath.Join(dir, ".dross", "board.json")
	mustWrite(t, boardPath, `{"phases":{},"quicks":{},"milestones":{},"dismissed":[]}`)
	before, err := os.ReadFile(boardPath)
	if err != nil {
		t.Fatalf("read board.json: %v", err)
	}

	var runErr error
	_ = captureStdout(t, func() {
		runErr = runCmd(t, Issue(), "pull", "--mark", "--json")
	})
	if runErr != nil {
		t.Fatalf("a board failure must still exit 0, got %v", runErr)
	}

	after, err := os.ReadFile(boardPath)
	if err != nil {
		t.Fatalf("read board.json: %v", err)
	}
	if string(before) != string(after) {
		t.Errorf("--mark stamped a pull that never happened:\nbefore %s\nafter  %s", before, after)
	}
}
