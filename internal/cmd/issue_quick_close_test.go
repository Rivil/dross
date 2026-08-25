package cmd

import (
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

// quickCloseRepo scaffolds a YouTrack-backed repo whose board.json links the
// quick ref "myref" to PROJ-7, ready for `issue quick myref --close`.
func quickCloseRepo(t *testing.T, srvURL string) string {
	t.Helper()
	dir := youtrackBoardRepo(t, srvURL)
	mustWrite(t, filepath.Join(dir, ".dross", "board.json"),
		`{"phases":{},"quicks":{"myref":"PROJ-7"},"milestones":{}}`)
	return dir
}

// TestQuickCloseFailsWhenTheIssueStaysUnresolved pins c-4 on the quick lane: a
// read-back that still reads unresolved fails the command and prints no closed
// line.
//
// On YouTrack specifically this already held before the reroute —
// YouTrackClient.CloseIssue delegates to CloseIssueAs("complete", nil), which
// verifies — so this is a regression pin, not a proof of new behaviour. What
// the reroute actually buys is in TestQuickCloseHonoursStateMap (the mapped
// write) and TestFlatBoardCloseVerifiesOnStateNotResolved (the boards where
// CloseIssue verified nothing at all).
func TestQuickCloseFailsWhenTheIssueStaysUnresolved(t *testing.T) {
	f := &ytCloseFake{resolved: false}
	srv := httptest.NewServer(f.handler(t))
	t.Cleanup(srv.Close)
	quickCloseRepo(t, srv.URL)

	var err error
	out := captureStdout(t, func() {
		_ = captureStderr(t, func() {
			err = runCmd(t, Issue(), "quick", "myref", "--close")
		})
	})
	if err == nil {
		t.Fatal("quick --close reported success over an issue the tracker still reads unresolved")
	}
	if strings.Contains(out, "(closed)") {
		t.Errorf("output = %q, want no closed line for a refused close", out)
	}
	if f.readBacks == 0 {
		t.Error("no read-back GET — without it a refused write is indistinguishable from a close")
	}
}

// TestQuickCloseHonoursStateMap proves the quick lane goes through the mapped
// write rather than the nil-override CloseIssue path: a [board].state_map entry
// for `complete` — the status closeBoardIssue defaults to — must be the value
// that reaches the tracker, not the built-in "Verified".
func TestQuickCloseHonoursStateMap(t *testing.T) {
	f := &ytCloseFake{resolved: true}
	srv := httptest.NewServer(f.handler(t))
	t.Cleanup(srv.Close)
	quickCloseRepo(t, srv.URL)
	mustRunSet(t, "board.state_map.complete", "Shipped To Prod")

	out := captureStdout(t, func() {
		if err := runCmd(t, Issue(), "quick", "myref", "--close"); err != nil {
			t.Fatalf("quick --close: %v", err)
		}
	})
	if !slicesHas(f.stateWrites, "Shipped To Prod") {
		t.Errorf("state writes = %v, want the [board].state_map override — the quick lane is still on the unmapped CloseIssue path", f.stateWrites)
	}
	if slicesHas(f.stateWrites, "Verified") {
		t.Errorf("state writes = %v, want the override to REPLACE the built-in default", f.stateWrites)
	}
	if !strings.Contains(out, "quick myref -> board PROJ-7 (closed)") {
		t.Errorf("output = %q, want the closed line on a verified close", out)
	}
}
