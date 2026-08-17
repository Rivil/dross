package cmd

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

// boardAddRepo is a repo with board sync on, a current milestone, and a current
// phase whose spec is a valid home for an added item.
func boardAddRepo(t *testing.T, apiBase string) string {
	t.Helper()
	dir := youtrackBoardRepo(t, apiBase)
	mustWrite(t, filepath.Join(dir, ".dross", "milestones", "v0.1.toml"), `
phases = ["host"]

[milestone]
version = "v0.1"
title = "First cut"

[scope]
success_criteria = ["ships"]
`)
	writeSpec(t, dir, "host", "[phase]\nid = \"host\"\ntitle = \"Host\"\n\n[[criteria]]\nid = \"c-1\"\ntext = \"x\"\n")
	if err := runCmd(t, State(), "set", "current_phase", "host"); err != nil {
		t.Fatal(err)
	}
	if err := runCmd(t, State(), "set", "current_milestone", "v0.1"); err != nil {
		t.Fatal(err)
	}
	return dir
}

func storedText(t *testing.T, dir string) string {
	t.Helper()
	return mustRead(t, filepath.Join(dir, ".dross", "phases", "host", "spec.toml"))
}

// TestDeferredAddMirrorsToBoard is c-6: filing and mirroring are one command.
// Exactly one create — zero means the mirror never happened, more than one means
// add triggered a whole-milestone sync instead of pushing its own item.
func TestDeferredAddMirrorsToBoard(t *testing.T) {
	f := newFakeBoard(t)
	dir := boardAddRepo(t, f.srv.URL)

	var out string
	if err := runCmdCapturing(t, &out, Deferred(), "add", "mirrored finding"); err != nil {
		t.Fatalf("add: %v", err)
	}
	if len(f.creates) != 1 {
		t.Fatalf("want exactly 1 create, got %d: %v", len(f.creates), f.creates)
	}
	if f.creates[0] != "[someday] mirrored finding" {
		t.Errorf("created title = %q", f.creates[0])
	}
	// The mirror must announce the issue it produced: a silent success is
	// indistinguishable from the warn-and-continue path at a glance.
	bj, err := readBoardJSON(dir)
	if err != nil {
		t.Fatal(err)
	}
	var linked string
	for k, v := range bj.Backlog {
		if strings.HasPrefix(k, "someday:id:") {
			linked = v
		}
	}
	if linked == "" {
		t.Fatalf("no id-keyed board link recorded: %v", bj.Backlog)
	}
	if !strings.Contains(out, linked) {
		t.Errorf("output does not name the created issue key %q:\n%s", linked, out)
	}
}

// TestDeferredAddMirrorsRoutedItem: the push is unconditional on --target, so a
// routed add is mirrored too. A `Target != ""` skip copied from syncBacklog
// fails here.
func TestDeferredAddMirrorsRoutedItem(t *testing.T) {
	f := newFakeBoard(t)
	boardAddRepo(t, f.srv.URL)

	if err := runCmd(t, Deferred(), "add", "routed finding", "--target", "host"); err != nil {
		t.Fatalf("add --target: %v", err)
	}
	if len(f.creates) != 1 {
		t.Errorf("a routed add must mirror too; creates=%v", f.creates)
	}
}

// TestDeferredAddSurvivesBoardFailure is the locked board_push decision: a board
// 500 warns and leaves the local write standing. The item's presence in the TOML
// with a failing server is also what proves the local write precedes the push.
func TestDeferredAddSurvivesBoardFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	dir := boardAddRepo(t, srv.URL)

	var out string
	if err := runCmdCapturing(t, &out, Deferred(), "add", "resilient finding"); err != nil {
		t.Fatalf("a board failure must not fail the command: %v", err)
	}
	if !strings.Contains(strings.ToLower(out), "board") || !strings.Contains(strings.ToLower(out), "warning") {
		t.Errorf("no warning naming the board failure:\n%s", out)
	}
	if !strings.Contains(storedText(t, dir), "resilient finding") {
		t.Error("item missing from the local spec — the local write must precede and survive the push")
	}
}

// TestDeferredAddSkipsDisabledBoard: with board sync off, the mirror is a silent
// no-op. The server errors the test on ANY request.
func TestDeferredAddSkipsDisabledBoard(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		t.Errorf("disabled board must make no request, got %s %s", r.Method, r.URL.Path)
	}))
	t.Cleanup(srv.Close)
	dir := boardAddRepo(t, srv.URL)
	mustRunSet(t, "board.enabled", "false")

	if err := runCmd(t, Deferred(), "add", "offline finding"); err != nil {
		t.Fatalf("add: %v", err)
	}
	if !strings.Contains(storedText(t, dir), "offline finding") {
		t.Error("item not written locally with the board disabled")
	}
}

// TestDeferredAddSkipsWithoutCurrentMilestone: there is no milestone entity to
// attach a backlog item to, so the mirror no-ops — quietly. An absent milestone
// is not a failure and must not read like one.
func TestDeferredAddSkipsWithoutCurrentMilestone(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		calls++
		t.Errorf("no current milestone must make no request, got %s %s", r.Method, r.URL.Path)
	}))
	t.Cleanup(srv.Close)
	dir := boardAddRepo(t, srv.URL)
	if err := runCmd(t, State(), "set", "current_milestone", ""); err != nil {
		t.Fatal(err)
	}

	var out string
	if err := runCmdCapturing(t, &out, Deferred(), "add", "milestone-less finding"); err != nil {
		t.Fatalf("add: %v", err)
	}
	if calls != 0 {
		t.Errorf("want 0 board calls, got %d", calls)
	}
	for _, bad := range []string{"warning", "error", "failed"} {
		if strings.Contains(strings.ToLower(out), bad) {
			t.Errorf("output reads like a failure (%q):\n%s", bad, out)
		}
	}
	if !strings.Contains(storedText(t, dir), "milestone-less finding") {
		t.Error("item not written locally")
	}
}

// TestDeferredAddSharesKeySpaceWithSync: add and backlog-sync must key on the
// same id, so the next sync UPDATES the issue add created rather than creating a
// second one for the same finding.
func TestDeferredAddSharesKeySpaceWithSync(t *testing.T) {
	f := newFakeBoard(t)
	dir := boardAddRepo(t, f.srv.URL)

	if err := runCmd(t, Deferred(), "add", "shared-key finding"); err != nil {
		t.Fatalf("add: %v", err)
	}
	bj, err := readBoardJSON(dir)
	if err != nil {
		t.Fatal(err)
	}
	var linked string
	for k, v := range bj.Backlog {
		if strings.HasPrefix(k, "someday:id:") {
			linked = v
		}
	}
	if linked == "" {
		t.Fatalf("add recorded no id-keyed board link: %v", bj.Backlog)
	}

	f.patches = nil
	if err := runCmd(t, Issue(), "backlog-sync", "v0.1"); err != nil {
		t.Fatalf("backlog-sync: %v", err)
	}
	if len(f.creates) != 1 {
		t.Errorf("sync created a duplicate issue for an added item; creates=%v", f.creates)
	}
	var patched bool
	for _, p := range f.patches {
		if p.key == linked && p.summary == "[someday] shared-key finding" {
			patched = true
		}
	}
	if !patched {
		t.Errorf("sync did not update the issue add created (%s); patches=%v", linked, f.patches)
	}
}
