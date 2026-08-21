package cmd

import (
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// writeStrandedFixture seeds a repo whose board.json is stranded in all five
// mirror namespaces, and returns the fixture directory.
//
// One fixture for every lane on purpose: a plan that covers four lanes and
// silently drops the fifth is the exact failure the sweep exists to prevent,
// and a per-lane fixture would never catch it.
func writeStrandedFixture(t *testing.T, dir string) {
	t.Helper()
	writeChanges(t, dir, "01-auth", "complete") // phase + task cards
	writeChanges(t, dir, "built", "complete")   // the scaffolded backlog slug
	writeMilestoneToml(t, filepath.Join(dir, ".dross"), "v1.5", "complete", "")
}

const strandedBoard = `{
  "phases": {"01-auth": "PROJ-1"},
  "tasks": {"01-auth/t-1": {"issue": "PROJ-2"}},
  "milestones": {"v1.5": "PROJ-7"},
  "quicks": {"1.0.0.1": "PROJ-40"},
  "backlog": {"slug:built": "PROJ-20"}
}`

// reapCmdRepo scaffolds the fixture and returns the repo dir. Unlike the
// classifier fixtures this one drives the real cobra command, so it does not
// hold a boardCtx.
func reapCmdRepo(t *testing.T, f *readOnlyYT, boardJSON string) string {
	t.Helper()
	srv := httptest.NewServer(f.handler(t))
	t.Cleanup(srv.Close)
	dir := youtrackBoardRepo(t, srv.URL)
	mustRunSet(t, "board.milestone_mode", "epic")
	mustWrite(t, filepath.Join(dir, ".dross", "board.json"), boardJSON)
	return dir
}

// writingIsFatalYT fails the test on ANY request that is not a GET, and counts
// the reads, so a dry run that reached for a write is caught at the wire rather
// than by inspecting what it printed.
type writingIsFatalYT struct {
	readOnlyYT
	writes int
}

func (f *writingIsFatalYT) handler(t *testing.T) http.HandlerFunc {
	t.Helper()
	inner := f.readOnlyYT.handler(t)
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case "POST", "PUT", "PATCH", "DELETE":
			f.writes++
			t.Errorf("dry run issued %s %s — reap without --apply must write nothing", r.Method, r.URL.Path)
			_, _ = io.WriteString(w, `{}`)
		default:
			inner(w, r)
		}
	}
}

func reapCmdRepoStrict(t *testing.T, f *writingIsFatalYT, boardJSON string) string {
	t.Helper()
	srv := httptest.NewServer(f.handler(t))
	t.Cleanup(srv.Close)
	dir := youtrackBoardRepo(t, srv.URL)
	mustRunSet(t, "board.milestone_mode", "epic")
	mustWrite(t, filepath.Join(dir, ".dross", "board.json"), boardJSON)
	return dir
}

// TestReapWithoutApplyNeverWrites is the dry-run-by-default contract (c-2) held
// at the wire, not at the output: the fake fails the test on any POST, PUT,
// PATCH or DELETE.
func TestReapWithoutApplyNeverWrites(t *testing.T) {
	f := &writingIsFatalYT{readOnlyYT: readOnlyYT{resolved: map[string]bool{}}}
	dir := reapCmdRepoStrict(t, f, strandedBoard)
	writeStrandedFixture(t, dir)

	var err error
	out := captureStdout(t, func() {
		err = runCmd(t, Issue(), "reap")
	})
	if err != nil {
		t.Fatalf("reap: %v", err)
	}
	if f.writes != 0 {
		t.Errorf("%d write requests issued during a dry run", f.writes)
	}
	if !strings.Contains(out, "PROJ-1") {
		t.Errorf("dry run printed no plan:\n%s", out)
	}
}

// TestReapPlanNamesTheJustifyingRecord: a plan that lists only card ids asks
// the reader to take ninety closes on trust.
func TestReapPlanNamesTheJustifyingRecord(t *testing.T) {
	f := &readOnlyYT{resolved: map[string]bool{}}
	dir := reapCmdRepo(t, f, strandedBoard)
	writeStrandedFixture(t, dir)

	out := captureStdout(t, func() {
		if err := runCmd(t, Issue(), "reap"); err != nil {
			t.Fatalf("reap: %v", err)
		}
	})
	for _, want := range []string{
		"phases/01-auth/changes.json", // the phase and task cards' record
		"milestones/v1.5.toml",        // the epic's record
		"phases/built/",               // the backlog slug's record
	} {
		if !strings.Contains(out, want) {
			t.Errorf("plan never names %q — a line with only a card id is unauditable:\n%s", want, out)
		}
	}
}

// TestReapWholeBoardCoversFiveLanes: the plan's lane set is compared against
// reflection over board.Board, so a namespace added there with no reap path
// fails here rather than being silently skipped on a live board.
func TestReapWholeBoardCoversFiveLanes(t *testing.T) {
	f := &readOnlyYT{resolved: map[string]bool{}}
	dir := reapCmdRepo(t, f, strandedBoard)
	writeStrandedFixture(t, dir)

	out := captureStdout(t, func() {
		if err := runCmd(t, Issue(), "reap"); err != nil {
			t.Fatalf("reap: %v", err)
		}
	})
	want := boardNamespaceFields(t)
	if len(want) == 0 {
		t.Fatal("reflection found no namespaces — the guard would pass vacuously")
	}
	for _, lane := range want {
		if !strings.Contains(out, lane+" (") {
			t.Errorf("lane %s is absent from the whole-board plan:\n%s", lane, out)
		}
	}
}

// TestReapNamespaceFilterScopesThePlan: the filter is what lets the first live
// run land in reviewable batches by class instead of ninety writes at once.
func TestReapNamespaceFilterScopesThePlan(t *testing.T) {
	seed := func(t *testing.T) *readOnlyYT {
		t.Helper()
		f := &readOnlyYT{resolved: map[string]bool{}}
		dir := reapCmdRepo(t, f, strandedBoard)
		writeStrandedFixture(t, dir)
		return f
	}

	t.Run("one namespace", func(t *testing.T) {
		seed(t)
		out := captureStdout(t, func() {
			if err := runCmd(t, Issue(), "reap", "--namespace", "tasks"); err != nil {
				t.Fatalf("reap: %v", err)
			}
		})
		if !strings.Contains(out, "PROJ-2") {
			t.Errorf("the task card is missing from a --namespace tasks plan:\n%s", out)
		}
		for _, other := range []string{"PROJ-1", "PROJ-7", "PROJ-20", "PROJ-40"} {
			if strings.Contains(out, other) {
				t.Errorf("--namespace tasks printed %s from another lane:\n%s", other, out)
			}
		}
		if !strings.Contains(out, "1 lane") {
			t.Errorf("footer does not match the scoped plan:\n%s", out)
		}
	})

	t.Run("two namespaces", func(t *testing.T) {
		seed(t)
		out := captureStdout(t, func() {
			if err := runCmd(t, Issue(), "reap", "--namespace", "tasks", "--namespace", "phases"); err != nil {
				t.Fatalf("reap: %v", err)
			}
		})
		if !strings.Contains(out, "PROJ-1") || !strings.Contains(out, "PROJ-2") {
			t.Errorf("both requested lanes are not in the plan:\n%s", out)
		}
		if strings.Contains(out, "PROJ-7") {
			t.Errorf("an unrequested lane leaked into the plan:\n%s", out)
		}
	})

	t.Run("unknown namespace names the valid set", func(t *testing.T) {
		seed(t)
		err := runCmd(t, Issue(), "reap", "--namespace", "bogus")
		if err == nil {
			t.Fatal("an unknown --namespace was accepted")
		}
		for _, lane := range boardNamespaceFields(t) {
			if !strings.Contains(err.Error(), lane) {
				t.Errorf("error %q does not name the valid namespace %s", err, lane)
			}
		}
	})
}

// TestReapNamespaceSetIsReadOffBoardNotTranscribed: the flag's valid values are
// derived from board.Board by reflection. A literal list beside the flag would
// go stale the moment a namespace is added, and the error a typo produced would
// name a set that no longer matches reality.
func TestReapNamespaceSetIsReadOffBoardNotTranscribed(t *testing.T) {
	got := boardNamespaceNames()
	want := boardNamespaceFields(t)
	sort.Strings(want)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("boardNamespaceNames() = %v, want board.Board's map fields %v", got, want)
	}
}

// TestReapIsANoOpWhenBoardSyncIsOff: every `dross issue` verb is safe to call
// unconditionally on a repo that never opted in.
func TestReapIsANoOpWhenBoardSyncIsOff(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("board API called with sync disabled: %s %s", r.Method, r.URL.Path)
	}))
	t.Cleanup(srv.Close)
	boardRepo(t, srv.URL, false)

	if err := runCmd(t, Issue(), "reap"); err != nil {
		t.Fatalf("reap with board sync off must be a silent no-op: %v", err)
	}
}
