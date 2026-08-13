package cmd

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ytCloseFake models the close round-trip: it records the State value written
// and answers reads with whatever `resolved` the test wants, so a write that
// the workflow silently refuses is expressible.
type ytCloseFake struct {
	stateWrites []string
	readBacks   int
	resolved    bool
}

func (f *ytCloseFake) handler(t *testing.T) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		switch {
		case path == "/api/issueTags" && r.Method == "GET":
			_, _ = io.WriteString(w, `[]`)
		case path == "/api/issueTags" && r.Method == "POST":
			_, _ = io.WriteString(w, `{"id":"tid-x"}`)
		case strings.HasSuffix(path, "/tags") && r.Method == "POST":
			_, _ = io.WriteString(w, `{}`)
		case strings.Contains(path, "/tags/") && r.Method == "DELETE":
			_, _ = io.WriteString(w, `{}`)

		case path == "/api/issues" && r.Method == "GET":
			_, _ = io.WriteString(w, `[]`)
		case path == "/api/issues" && r.Method == "POST":
			_, _ = io.WriteString(w, `{"idReadable":"PROJ-7"}`)

		case strings.HasPrefix(path, "/api/issues/") && r.Method == "GET":
			f.readBacks++
			resolved := "null"
			if f.resolved {
				resolved = "1700000000000"
			}
			_, _ = io.WriteString(w, `{"idReadable":"PROJ-7","resolved":`+resolved+`,"tags":[{"id":"tid-dross","name":"dross"}]}`)

		case strings.HasPrefix(path, "/api/issues/") && r.Method == "POST":
			var body struct {
				CustomFields []struct {
					Name  string `json:"name"`
					Value struct {
						Name string `json:"name"`
					} `json:"value"`
				} `json:"customFields"`
			}
			raw, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(raw, &body)
			for _, cf := range body.CustomFields {
				f.stateWrites = append(f.stateWrites, cf.Value.Name)
			}
			_, _ = io.WriteString(w, `{"idReadable":"PROJ-7"}`)

		default:
			t.Errorf("unexpected %s %s", r.Method, path)
			_, _ = io.WriteString(w, `{}`)
		}
	}
}

// TestPhaseSyncCloseWritesStateAndVerifies proves c-5's happy path end to end:
// the mapped resolved state reaches the tracker, the command reads the issue
// back, and only then reports it closed.
func TestPhaseSyncCloseWritesStateAndVerifies(t *testing.T) {
	f := &ytCloseFake{resolved: true}
	srv := httptest.NewServer(f.handler(t))
	t.Cleanup(srv.Close)

	phaseSyncRepo(t, srv.URL, "01-x", "X", emptyBoardJSON)

	out := captureStdout(t, func() {
		if err := runCmd(t, Issue(), "phase-sync", "01-x", "--status", "complete", "--close"); err != nil {
			t.Fatalf("phase-sync --close: %v", err)
		}
	})
	if !slicesHas(f.stateWrites, "Verified") {
		t.Errorf("state writes = %v, want the mapped Verified", f.stateWrites)
	}
	if f.readBacks == 0 {
		t.Error("no read-back GET — without it a refused transition is indistinguishable from a close")
	}
	if !strings.Contains(out, "(closed)") {
		t.Errorf("output = %q, want the closed line on a verified close", out)
	}
}

// TestPhaseSyncCloseWithUnmappedStatusFails is the c-5 inversion: today's
// warn-and-continue exits 0 and prints "(closed)" for an issue nothing moved.
func TestPhaseSyncCloseWithUnmappedStatusFails(t *testing.T) {
	f := &ytCloseFake{resolved: false}
	srv := httptest.NewServer(f.handler(t))
	t.Cleanup(srv.Close)

	phaseSyncRepo(t, srv.URL, "01-x", "X", emptyBoardJSON)
	mustRunSet(t, "board.state_map.complete", "")

	var err error
	out := captureStdout(t, func() {
		_ = captureStderr(t, func() {
			err = runCmd(t, Issue(), "phase-sync", "01-x", "--status", "complete", "--close")
		})
	})
	if err == nil {
		t.Fatal("an unmapped close status exited 0 — a close that did nothing must not read as success")
	}
	if !strings.Contains(err.Error(), "complete") {
		t.Errorf("err = %q, want it to name the status", err)
	}
	if strings.Contains(out, "closed") {
		t.Errorf("output = %q, want no closed line when the close failed", out)
	}
}

// TestPhaseSyncCloseFailsWhenTheIssueStaysUnresolved: the fake accepts the
// State write but reports resolved:null. Without the read-back this test
// cannot fail.
func TestPhaseSyncCloseFailsWhenTheIssueStaysUnresolved(t *testing.T) {
	f := &ytCloseFake{resolved: false}
	srv := httptest.NewServer(f.handler(t))
	t.Cleanup(srv.Close)

	phaseSyncRepo(t, srv.URL, "01-x", "X", emptyBoardJSON)

	var err error
	out := captureStdout(t, func() {
		err = runCmd(t, Issue(), "phase-sync", "01-x", "--status", "complete", "--close")
	})
	if err == nil {
		t.Fatal("the issue read back unresolved and the command still succeeded")
	}
	if len(f.stateWrites) == 0 {
		t.Error("no state write at all — the close must at least try")
	}
	if strings.Contains(out, "closed") {
		t.Errorf("output = %q, want no closed line", out)
	}
}

// TestPhaseSyncCloseOnCreateTakesTheSamePath: the issue is minted and closed in
// one run. That branch is the one that regressed, so it gets its own assertion
// rather than riding on the update path's.
func TestPhaseSyncCloseOnCreateTakesTheSamePath(t *testing.T) {
	f := &ytCloseFake{resolved: true}
	srv := httptest.NewServer(f.handler(t))
	t.Cleanup(srv.Close)

	// No board.json entry at all, and nothing on the tracker: this run creates.
	phaseSyncRepo(t, srv.URL, "01-x", "X", emptyBoardJSON)

	out := captureStdout(t, func() {
		if err := runCmd(t, Issue(), "phase-sync", "01-x", "--status", "complete", "--close"); err != nil {
			t.Fatalf("phase-sync --close: %v", err)
		}
	})
	if !slicesHas(f.stateWrites, "Verified") {
		t.Errorf("state writes = %v — the close-on-create edge must resolve too", f.stateWrites)
	}
	if f.readBacks == 0 {
		t.Error("the close-on-create edge skipped the read-back")
	}
	if !strings.Contains(out, "(closed)") {
		t.Errorf("output = %q, want the closed line", out)
	}
}

// TestPhaseSyncWithoutCloseKeepsTheLenientPath: an unmapped status is only
// fatal when the caller asserts the work is done. A plain sync still warns and
// exits 0 — a missing status label is cosmetic, a false close is not.
func TestPhaseSyncWithoutCloseKeepsTheLenientPath(t *testing.T) {
	f := &ytCloseFake{resolved: false}
	srv := httptest.NewServer(f.handler(t))
	t.Cleanup(srv.Close)

	phaseSyncRepo(t, srv.URL, "01-x", "X", emptyBoardJSON)
	mustRunSet(t, "board.state_map.complete", "")

	var err error
	warn := captureStderr(t, func() {
		_ = captureStdout(t, func() {
			err = runCmd(t, Issue(), "phase-sync", "01-x", "--status", "complete")
		})
	})
	if err != nil {
		t.Fatalf("a plain sync with an unmapped status must still exit 0, got %v", err)
	}
	if !strings.Contains(warn, "no YouTrack State mapping") {
		t.Errorf("stderr = %q, want the skip warning", warn)
	}
}

// TestJiraCloseWithoutDoneTransitionFailsTheCommand is the Jira arm: a board
// whose workflow offers no done-category transition must fail the command
// rather than print the closed line.
func TestJiraCloseWithoutDoneTransitionFailsTheCommand(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/rest/api/3/label" && r.Method == "GET":
			_, _ = io.WriteString(w, `{"values":[]}`)
		case r.URL.Path == "/rest/api/3/search/jql" && r.Method == "GET":
			_, _ = io.WriteString(w, `{"issues":[]}`)
		case r.URL.Path == "/rest/api/3/issue" && r.Method == "POST":
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, `{"id":"1000","key":"PROJ-1"}`)
		case strings.HasSuffix(r.URL.Path, "/transitions") && r.Method == "GET":
			// Only a non-done transition is offered.
			_, _ = io.WriteString(w, `{"transitions":[{"id":"11","name":"Start","to":{"name":"In Progress","statusCategory":{"key":"indeterminate"}}}]}`)
		case strings.HasSuffix(r.URL.Path, "/transitions") && r.Method == "POST":
			w.WriteHeader(http.StatusNoContent)
		default:
			_, _ = io.WriteString(w, `{"key":"PROJ-1","fields":{"labels":["dross"]}}`)
		}
	}))
	t.Cleanup(srv.Close)

	dir := jiraBoardRepo(t, srv.URL)
	writeSpec(t, dir, "01-x", "[phase]\nid=\"01-x\"\ntitle=\"X\"\n")

	var err error
	out := captureStdout(t, func() {
		_ = captureStderr(t, func() {
			err = runCmd(t, Issue(), "phase-sync", "01-x", "--status", "complete", "--close")
		})
	})
	if err == nil {
		t.Fatal("a Jira board with no done-category transition closed without error")
	}
	if strings.Contains(out, "closed") {
		t.Errorf("output = %q, want no closed line", out)
	}
}
