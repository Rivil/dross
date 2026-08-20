package cmd

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

// epicCloseRepo scaffolds a YouTrack board in epic mode whose board.json
// already links v1.5 to the epic PROJ-7, which is the state a finalize run is
// in: the epic was ensured long ago, and all that is left is to resolve it.
func epicCloseRepo(t *testing.T, srvURL string) string {
	t.Helper()
	dir := youtrackBoardRepo(t, srvURL)
	mustRunSet(t, "board.milestone_mode", "epic")
	mustWrite(t, filepath.Join(dir, ".dross", "board.json"),
		`{"phases":{},"quicks":{},"milestones":{"v1.5":"PROJ-7"}}`)
	return dir
}

// TestEpicCloseResolvesForReal is c-3: before this, no verb could close a
// milestone epic at all, so every finished milestone left its epic open.
func TestEpicCloseResolvesForReal(t *testing.T) {
	f := &ytCloseFake{resolved: true}
	srv := httptest.NewServer(f.handler(t))
	t.Cleanup(srv.Close)
	epicCloseRepo(t, srv.URL)

	out := captureStdout(t, func() {
		if err := runCmd(t, Issue(), "milestone-sync", "v1.5", "--close"); err != nil {
			t.Fatalf("milestone-sync --close: %v", err)
		}
	})
	if !slicesHas(f.stateWrites, "Verified") {
		t.Errorf("state writes = %v, want the mapped resolved state on the epic", f.stateWrites)
	}
	if f.readBacks == 0 {
		t.Error("no read-back — the epic close must verify like every other close")
	}
	if !strings.Contains(out, "(closed)") {
		t.Errorf("output = %q, want the closed line", out)
	}
}

// TestEpicCloseFailsWhenUnresolved is the inversion: a tracker that accepted
// the State write and resolved nothing must not produce a closed line.
func TestEpicCloseFailsWhenUnresolved(t *testing.T) {
	f := &ytCloseFake{resolved: false}
	srv := httptest.NewServer(f.handler(t))
	t.Cleanup(srv.Close)
	epicCloseRepo(t, srv.URL)

	var err error
	out := captureStdout(t, func() {
		_ = captureStderr(t, func() {
			err = runCmd(t, Issue(), "milestone-sync", "v1.5", "--close")
		})
	})
	if err == nil {
		t.Fatal("an epic that still reads unresolved was reported closed")
	}
	if strings.Contains(out, "closed") {
		t.Errorf("output = %q, want no closed line", out)
	}
}

// TestMilestoneSyncWithoutCloseNeverWritesState guards the ensure path: adding
// --close must not turn the plain sync into a close. Nothing about ensuring a
// link should touch the epic's state field.
func TestMilestoneSyncWithoutCloseNeverWritesState(t *testing.T) {
	f := &ytCloseFake{resolved: true}
	srv := httptest.NewServer(f.handler(t))
	t.Cleanup(srv.Close)
	epicCloseRepo(t, srv.URL)

	if err := runCmd(t, Issue(), "milestone-sync", "v1.5"); err != nil {
		t.Fatalf("milestone-sync: %v", err)
	}
	if len(f.stateWrites) != 0 {
		t.Errorf("state writes = %v, want none on the plain ensure path", f.stateWrites)
	}
}

// TestMilestoneCloseRefusesANonIssueEntity is the entity gate. Only YouTrack's
// epic mode stores an ISSUE in the milestones slot.
//
// The forgejo case is the dangerous one, not merely the inert one: a forge
// milestone id and a forge issue number are the same id space, so an ungated
// close of milestone 7 would resolve human issue #7. The fake fails the test if
// ANY request reaches it, which is stricter than "no close request" and pins
// the gate ahead of the ensure.
func TestMilestoneCloseRefusesANonIssueEntity(t *testing.T) {
	for _, tc := range []struct {
		name      string
		setup     func(t *testing.T, srvURL string)
		wantInErr string
	}{
		{
			name: "youtrack version bundle",
			setup: func(t *testing.T, srvURL string) {
				dir := youtrackBoardRepo(t, srvURL)
				mustRunSet(t, "board.milestone_mode", "version")
				mustWrite(t, filepath.Join(dir, ".dross", "board.json"),
					`{"phases":{},"quicks":{},"milestones":{"v1.5":"v1.5"}}`)
			},
			wantInErr: "version",
		},
		{
			name: "youtrack agile board",
			setup: func(t *testing.T, srvURL string) {
				dir := youtrackBoardRepo(t, srvURL)
				mustRunSet(t, "board.milestone_mode", "agile")
				mustWrite(t, filepath.Join(dir, ".dross", "board.json"),
					`{"phases":{},"quicks":{},"milestones":{"v1.5":"Dross Board"}}`)
			},
			wantInErr: "agile",
		},
		{
			name: "forgejo numeric milestone id",
			setup: func(t *testing.T, srvURL string) {
				dir := boardRepo(t, srvURL, true)
				mustWrite(t, filepath.Join(dir, ".dross", "board.json"),
					`{"phases":{},"quicks":{},"milestones":{"v1.5":"7"}}`)
			},
			wantInErr: "forgejo",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				t.Errorf("a refused --close must reach the tracker not at all, but got %s %s", r.Method, r.URL.Path)
				_, _ = w.Write([]byte(`{}`))
			}))
			t.Cleanup(srv.Close)
			tc.setup(t, srv.URL)

			var err error
			out := captureStdout(t, func() {
				_ = captureStderr(t, func() {
					err = runCmd(t, Issue(), "milestone-sync", "v1.5", "--close")
				})
			})
			if err == nil {
				t.Fatal("--close was accepted for an entity that is not an issue")
			}
			if !strings.Contains(err.Error(), tc.wantInErr) {
				t.Errorf("error = %v, want it to name %q", err, tc.wantInErr)
			}
			if strings.Contains(out, "closed") {
				t.Errorf("output = %q, want no closed line", out)
			}
		})
	}
}

// TestMilestonePromptClosesTheEpicAtFinalize pins the emission. A verb nothing
// calls closes nothing: the c-3 gap was never a missing capability on the
// tracker, it was that no step in the loop ever asked.
func TestMilestonePromptClosesTheEpicAtFinalize(t *testing.T) {
	content := promptContent(t, "milestone.md")

	const emit = "dross issue milestone-sync <version> --close"
	at := strings.Index(content, emit)
	if at < 0 {
		t.Fatalf("milestone.md never emits %q — the epic would stay open after every finalize", emit)
	}
	finalize := strings.Index(content, "dross milestone complete <version> --finalize")
	if finalize < 0 {
		t.Fatal("milestone.md no longer carries the --finalize step this emission is anchored to")
	}
	if at < finalize {
		t.Error("the epic close is emitted BEFORE --finalize; the milestone is not finished until finalize has run")
	}
}
