package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// The forge fake in issue_task_test.go cannot relate issues and has no workflow
// field, so it exercises only the fallback halves of task-sync. These tests use
// backends that CAN do both — a tracker where the link lands and the state field
// moves — which is the path the execute loop actually takes on a real board.

// taskStateForge is a YouTrack stand-in for task-sync: it records the relates-to
// links and the State value written to each issue, and can be told to fail
// either one, which is how the "keep going" and "stop" arms get driven.
type taskStateForge struct {
	mu       sync.Mutex
	t        *testing.T
	srv      *httptest.Server
	issues   map[string]string   // key -> summary
	tags     map[string][]string // key -> tag names
	tagIndex []string
	links    []linkCall
	states   map[string]string // key -> last State value written
	nextItem int

	// failLink makes the commands API reject, which is the capability gap a
	// tracker reports at call time rather than through the interface.
	failLink bool
	// failState makes the State write reject. The value is already in the
	// project's bundle, so there is nothing to add and retry — the failure is
	// real and must reach the caller.
	failState bool
}

func newTaskStateForge(t *testing.T) *taskStateForge {
	t.Helper()
	f := &taskStateForge{
		t:      t,
		issues: map[string]string{},
		tags:   map[string][]string{},
		states: map[string]string{},
	}
	f.srv = httptest.NewServer(http.HandlerFunc(f.handle))
	t.Cleanup(f.srv.Close)
	return f
}

// issueBody renders an issue with its tags, the shape the resolvers read.
func (f *taskStateForge) issueBody(key string) string {
	var tagJSON []string
	for _, n := range f.tags[key] {
		tagJSON = append(tagJSON, fmt.Sprintf(`{"id":"tid-%s","name":%q}`, n, n))
	}
	return fmt.Sprintf(`{"idReadable":%q,"summary":%q,"tags":[%s]}`,
		key, f.issues[key], strings.Join(tagJSON, ","))
}

// knownTags is the instance's tag vocabulary: a query naming a tag the tracker
// has never heard of is dropped before it is sent.
func (f *taskStateForge) knownTags() []string {
	seen := map[string]bool{}
	var out []string
	add := func(n string) {
		if n != "" && !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	for _, n := range f.tagIndex {
		add(n)
	}
	for _, tags := range f.tags {
		for _, n := range tags {
			add(n)
		}
	}
	return out
}

func (f *taskStateForge) handle(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()

	switch {
	// The project's State bundle. It already holds every value dross writes
	// here, so a failed State write is never masked by an add-and-retry.
	case strings.Contains(r.URL.Path, "/admin/projects/") && strings.HasSuffix(r.URL.Path, "/customFields"):
		_, _ = io.WriteString(w, `[{"field":{"name":"State"},"bundle":{"id":"SB1","values":[{"name":"In Progress"},{"name":"In Review"}],"projects":[{"shortName":"PROJ"}]}}]`)

	// --- tags (labels are entity writes on YouTrack) ---
	case r.URL.Path == "/api/issueTags" && r.Method == "GET":
		var out []string
		for _, n := range f.knownTags() {
			out = append(out, fmt.Sprintf(`{"id":"tid-%s","name":%q}`, n, n))
		}
		_, _ = io.WriteString(w, "["+strings.Join(out, ",")+"]")
	case r.URL.Path == "/api/issueTags" && r.Method == "POST":
		var b struct {
			Name string `json:"name"`
		}
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &b)
		f.tagIndex = append(f.tagIndex, b.Name)
		_, _ = io.WriteString(w, fmt.Sprintf(`{"id":"tid-%s"}`, b.Name))
	case strings.HasSuffix(r.URL.Path, "/tags") && r.Method == "POST":
		key := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/issues/"), "/tags")
		var b struct {
			ID string `json:"id"`
		}
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &b)
		f.tags[key] = append(f.tags[key], strings.TrimPrefix(b.ID, "tid-"))
		_, _ = io.WriteString(w, `{}`)
	case strings.Contains(r.URL.Path, "/tags/") && r.Method == "DELETE":
		rest := strings.TrimPrefix(r.URL.Path, "/api/issues/")
		key, id, _ := strings.Cut(rest, "/tags/")
		drop := strings.TrimPrefix(id, "tid-")
		var kept []string
		for _, n := range f.tags[key] {
			if n != drop {
				kept = append(kept, n)
			}
		}
		f.tags[key] = kept
		_, _ = io.WriteString(w, `{}`)

	// --- issues ---
	case r.URL.Path == "/api/issues" && r.Method == "POST":
		var b map[string]any
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &b)
		f.nextItem++
		key := fmt.Sprintf("PROJ-%d", 300+f.nextItem)
		summary, _ := b["summary"].(string)
		f.issues[key] = summary
		_, _ = io.WriteString(w, fmt.Sprintf(`{"idReadable":%q,"summary":%q}`, key, summary))

	case r.URL.Path == "/api/issues" && r.Method == "GET":
		query := r.URL.Query().Get("query")
		var out []string
		for key := range f.issues {
			for _, n := range f.tags[key] {
				if n != "" && strings.Contains(query, n) {
					out = append(out, f.issueBody(key))
					break
				}
			}
		}
		_, _ = io.WriteString(w, "["+strings.Join(out, ",")+"]")

	case strings.HasPrefix(r.URL.Path, "/api/issues/") && r.Method == "GET":
		key := strings.TrimPrefix(r.URL.Path, "/api/issues/")
		if _, ok := f.issues[key]; !ok {
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, `{"error":"not found"}`)
			return
		}
		_, _ = io.WriteString(w, f.issueBody(key))

	case strings.HasPrefix(r.URL.Path, "/api/issues/") && r.Method == "POST":
		key := strings.TrimPrefix(r.URL.Path, "/api/issues/")
		var b map[string]any
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &b)
		// A State write and a body patch are the same verb on the same path;
		// only the payload tells them apart.
		if cf, ok := b["customFields"].([]any); ok {
			if f.failState {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = io.WriteString(w, `{"error":"workflow rejected the state change"}`)
				return
			}
			for _, raw := range cf {
				m, _ := raw.(map[string]any)
				v, _ := m["value"].(map[string]any)
				if name, ok := v["name"].(string); ok {
					f.states[key] = name
				}
			}
			_, _ = io.WriteString(w, f.issueBody(key))
			return
		}
		if summary, ok := b["summary"].(string); ok {
			f.issues[key] = summary
		}
		_, _ = io.WriteString(w, f.issueBody(key))

	case r.URL.Path == "/api/commands" && r.Method == "POST":
		if f.failLink {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"error":"no such link type"}`)
			return
		}
		var b struct {
			Query  string `json:"query"`
			Issues []struct {
				IDReadable string `json:"idReadable"`
			} `json:"issues"`
		}
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &b)
		if len(b.Issues) > 0 && strings.HasPrefix(b.Query, "relates to ") {
			f.links = append(f.links, linkCall{
				from: b.Issues[0].IDReadable,
				to:   strings.TrimPrefix(b.Query, "relates to "),
			})
		}
		_, _ = io.WriteString(w, `{}`)

	default:
		f.t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		_, _ = io.WriteString(w, `{}`)
	}
}

const twoTaskAuthPlan = `task_seq = 2

[phase]
  id = "01-auth"

[[task]]
  id = "t-1"
  wave = 1
  title = "First task"
  files = ["a.go"]
  status = "pending"

[[task]]
  id = "t-2"
  wave = 1
  title = "Second task"
  files = ["b.go"]
  status = "pending"
`

// linkingTaskRepo is a YouTrack-backed repo whose phase issue already exists —
// the state the execute loop is in when it calls task-sync. The phase issue is
// seeded into board.json rather than synced so these tests exercise the task
// path only.
func linkingTaskRepo(t *testing.T, f *taskStateForge) string {
	t.Helper()
	dir := youtrackBoardRepo(t, f.srv.URL)
	writeSpec(t, dir, "01-auth", "[phase]\n  id = \"01-auth\"\n  title = \"Auth\"\n")
	writePlan(t, dir, "01-auth", twoTaskAuthPlan)
	mustWrite(t, filepath.Join(dir, ".dross", "board.json"),
		`{"phases":{"01-auth":"PROJ-9"},"tasks":{},"quicks":{},"milestones":{},"backlog":{},"dismissed":[]}`)
	f.mu.Lock()
	f.issues["PROJ-9"] = "01-auth — Auth"
	f.tags["PROJ-9"] = []string{labelMarker, phaseLabel("01-auth")}
	f.mu.Unlock()
	return dir
}

// TestTaskSyncLinksAndMovesTheStateOnACapableTracker is the whole point of
// mirroring a task: the issue is related to its phase's, and the tracker's OWN
// State field moves — not a dross label, which is dross talking to itself.
func TestTaskSyncLinksAndMovesTheStateOnACapableTracker(t *testing.T) {
	f := newTaskStateForge(t)
	dir := linkingTaskRepo(t, f)

	stderr := captureStderr(t, func() {
		_ = captureStdout(t, func() {
			if err := runCmd(t, Issue(), "task-sync", "01-auth", "t-1", "--status", "task-in-progress"); err != nil {
				t.Fatalf("task-sync: %v", err)
			}
		})
	})

	if strings.Contains(stderr, "relate") {
		t.Errorf("a tracker that CAN relate issues still warned:\n%s", stderr)
	}
	key, ok := loadBoardFile(t, dir).TaskIssue("01-auth", "t-1")
	if !ok || key == "" {
		t.Fatalf("t-1 mapping = %q,%v — no issue was recorded", key, ok)
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.links) != 1 {
		t.Fatalf("links = %+v, want exactly one", f.links)
	}
	// `to` is the key syncOneTask returned. An empty or wrong one here means
	// the task issue was created but the link points at nothing.
	if f.links[0].from != "PROJ-9" || f.links[0].to != key {
		t.Errorf("link = %+v, want PROJ-9 -> %s", f.links[0], key)
	}
	if got := f.states[key]; got != "In Progress" {
		t.Errorf("State on %s = %q, want %q — the tracker's own field did not move", key, got, "In Progress")
	}
}

// TestLinkFailureWarnsOnceAndKeepsTheIssues: a tracker that advertises the
// capability but rejects the call at runtime must not lose the issues over it.
// The issues are the substance; the link is only how they are grouped — and an
// eight-task phase must not print eight identical lines about it.
func TestLinkFailureWarnsOnceAndKeepsTheIssues(t *testing.T) {
	f := newTaskStateForge(t)
	f.failLink = true
	dir := linkingTaskRepo(t, f)

	var err error
	stderr := captureStderr(t, func() {
		_ = captureStdout(t, func() {
			err = runCmd(t, Issue(), "task-sync", "01-auth")
		})
	})
	if err != nil {
		t.Fatalf("a rejected link must not fail the sync, got %v", err)
	}
	if n := strings.Count(stderr, "could not relate"); n != 1 {
		t.Errorf("the link failure warned %d times for a 2-task phase, want exactly 1:\n%s", n, stderr)
	}

	b := loadBoardFile(t, dir)
	for _, id := range []string{"t-1", "t-2"} {
		if key, ok := b.TaskIssue("01-auth", id); !ok || key == "" {
			t.Errorf("%s mapping = %q,%v — a failed link dropped the issue", id, key, ok)
		}
	}
}

// TestStateFailureFailsTheTaskSync: driving the state IS the deliverable at a
// task edge. A rejected State write that the sync swallowed would leave the
// board claiming the task had not been picked up while the loop moved on — so
// it has to reach the caller.
func TestStateFailureFailsTheTaskSync(t *testing.T) {
	f := newTaskStateForge(t)
	f.failState = true
	linkingTaskRepo(t, f)

	var err error
	_ = captureStderr(t, func() {
		_ = captureStdout(t, func() {
			err = runCmd(t, Issue(), "task-sync", "01-auth", "t-1", "--status", "task-in-progress")
		})
	})
	if err == nil {
		t.Fatal("a rejected State write reported success")
	}
	if !strings.Contains(err.Error(), "state") {
		t.Errorf("error = %v, want the failed state write named", err)
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.links) != 0 {
		t.Errorf("links = %+v, want none — the task never reached the link step", f.links)
	}
}

// TestJiraStateFailureFailsTheTaskSync is the Jira arm of the same property.
// The two provider branches are separate arms of one switch, and an error
// swallowed in either one is the same silent lie about where the work is.
func TestJiraStateFailureFailsTheTaskSync(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/rest/api/3/label" && r.Method == "GET":
			_, _ = io.WriteString(w, `{"values":[]}`)
		case strings.HasPrefix(r.URL.Path, "/rest/api/3/search") && r.Method == "GET":
			_, _ = io.WriteString(w, `{"issues":[]}`)
		case r.URL.Path == "/rest/api/3/issue" && r.Method == "POST":
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, `{"id":"1000","key":"PROJ-1"}`)
		case strings.HasSuffix(r.URL.Path, "/transitions") && r.Method == "GET":
			// The workflow endpoint is down: dross cannot know what states are
			// reachable, so it cannot claim the card moved.
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = io.WriteString(w, `{"errorMessages":["workflow unavailable"]}`)
		default:
			_, _ = io.WriteString(w, `{"key":"PROJ-1","fields":{"labels":["dross"]}}`)
		}
	}))
	t.Cleanup(srv.Close)

	dir := jiraBoardRepo(t, srv.URL)
	writeSpec(t, dir, "01-auth", "[phase]\n  id = \"01-auth\"\n  title = \"Auth\"\n")
	writePlan(t, dir, "01-auth", twoTaskAuthPlan)
	mustWrite(t, filepath.Join(dir, ".dross", "board.json"),
		`{"phases":{"01-auth":"PROJ-9"},"tasks":{},"quicks":{},"milestones":{},"backlog":{},"dismissed":[]}`)

	var err error
	_ = captureStderr(t, func() {
		_ = captureStdout(t, func() {
			err = runCmd(t, Issue(), "task-sync", "01-auth", "t-1", "--status", "task-in-progress")
		})
	})
	if err == nil {
		t.Fatal("a Jira board whose workflow endpoint failed reported a successful state change")
	}
}
