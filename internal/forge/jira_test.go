package forge

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/configenum"
)

const jiraTokenEnv = "MOCK_JIRA_TOKEN"

// newTestJiraClient spins up an httptest server and points a JiraClient at it.
// The token env is set for the test's lifetime.
func newTestJiraClient(t *testing.T, h http.HandlerFunc) (*JiraClient, *httptest.Server) {
	t.Helper()
	t.Setenv(jiraTokenEnv, "secret")
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c, err := NewJira(Config{APIBase: srv.URL, AuthEnv: jiraTokenEnv, Project: "PROJ", AuthUser: "me@example.com", Hosts: allowingSelf(srv.URL)})
	if err != nil {
		t.Fatalf("NewJira: %v", err)
	}
	return c, srv
}

// TestNewAcceptsJira pins that the Jira board client constructs from a valid
// config and rejects the same shapes of bad config the sibling backends do.
func TestNewAcceptsJira(t *testing.T) {
	t.Setenv(jiraTokenEnv, "secret")
	good := Config{APIBase: "https://x.atlassian.net", AuthEnv: jiraTokenEnv, Project: "PROJ", AuthUser: "me@example.com"}
	c, err := NewJira(good)
	if err != nil {
		t.Fatalf("NewJira(good): %v", err)
	}
	if c == nil {
		t.Fatal("NewJira(good): nil client")
	}

	bad := []struct {
		name string
		cfg  Config
		want string
	}{
		{"missing base", Config{AuthEnv: jiraTokenEnv, Project: "PROJ", AuthUser: "me@example.com"}, "needs APIBase"},
		{"missing authenv", Config{APIBase: "https://x", Project: "PROJ", AuthUser: "me@example.com"}, "needs AuthEnv"},
		{"missing project", Config{APIBase: "https://x", AuthEnv: jiraTokenEnv, AuthUser: "me@example.com"}, "needs Project"},
		{"missing authuser", Config{APIBase: "https://x", AuthEnv: jiraTokenEnv, Project: "PROJ"}, "needs AuthUser"},
		{"unset token", Config{APIBase: "https://x", AuthEnv: "DROSS_DEFINITELY_UNSET", Project: "PROJ", AuthUser: "me@example.com", Hosts: allowingSelf("https://x")}, "is not set"},
	}
	for _, tc := range bad {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := NewJira(tc.cfg); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want error containing %q, got %v", tc.want, err)
			}
		})
	}
}

func TestJiraCreateIssue(t *testing.T) {
	var gotPath, gotMethod string
	var gotBody map[string]any
	c, _ := newTestJiraClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		_, _ = io.WriteString(w, `{"id":"10000","key":"PROJ-24","self":"https://x/rest/api/3/issue/10000"}`)
	})

	iss, err := c.CreateIssue(IssueInput{Title: "Hi", Body: "the body", Labels: []string{"dross"}, Milestone: 10001})
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	if gotMethod != "POST" || gotPath != "/rest/api/3/issue" {
		t.Fatalf("create hit %s %s, want POST /rest/api/3/issue", gotMethod, gotPath)
	}
	fields, ok := gotBody["fields"].(map[string]any)
	if !ok {
		t.Fatalf("create body missing fields object: %v", gotBody)
	}
	proj, _ := fields["project"].(map[string]any)
	if proj["key"] != "PROJ" {
		t.Errorf("create body project.key = %v, want PROJ", proj["key"])
	}
	if fields["summary"] != "Hi" {
		t.Errorf("create body summary = %v, want Hi", fields["summary"])
	}
	if _, ok := fields["description"]; !ok {
		t.Errorf("create body missing ADF description: %v", fields)
	}
	fv, _ := fields["fixVersions"].([]any)
	if len(fv) == 0 {
		t.Errorf("create body missing fixVersions for milestone: %v", fields)
	} else if m, _ := fv[0].(map[string]any); m["id"] != "10001" {
		t.Errorf("fixVersions[0].id = %v, want 10001", m["id"])
	}
	if iss.Key != "PROJ-24" {
		t.Errorf("Issue.Key = %q, want PROJ-24", iss.Key)
	}
}

func TestJiraGetIssue(t *testing.T) {
	var gotPath string
	c, _ := newTestJiraClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = io.WriteString(w, `{"key":"PROJ-7","fields":{"summary":"Hi","description":{"type":"doc","version":1,"content":[{"type":"paragraph","content":[{"type":"text","text":"hello world"}]}]},"labels":["dross"],"status":{"name":"Done","statusCategory":{"key":"done"}}}}`)
	})

	iss, err := c.GetIssue("PROJ-7")
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	if gotPath != "/rest/api/3/issue/PROJ-7" {
		t.Fatalf("get hit %s, want /rest/api/3/issue/PROJ-7", gotPath)
	}
	if iss.Key != "PROJ-7" || iss.Title != "Hi" {
		t.Errorf("get returned %+v", iss)
	}
	if iss.Body != "hello world" {
		t.Errorf("ADF description not flattened: got %q", iss.Body)
	}
	if iss.State != "closed" {
		t.Errorf("done statusCategory should map to closed, got %q", iss.State)
	}
}

func TestJiraUpdateIssue(t *testing.T) {
	var gotPath, gotMethod string
	var gotBody map[string]any
	c, _ := newTestJiraClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		w.WriteHeader(http.StatusNoContent)
	})

	newTitle := "New"
	iss, err := c.UpdateIssue("PROJ-7", IssuePatch{Title: &newTitle})
	if err != nil {
		t.Fatalf("UpdateIssue: %v", err)
	}
	if gotMethod != "PUT" || gotPath != "/rest/api/3/issue/PROJ-7" {
		t.Fatalf("update hit %s %s, want PUT /rest/api/3/issue/PROJ-7", gotMethod, gotPath)
	}
	fields, _ := gotBody["fields"].(map[string]any)
	if fields["summary"] != "New" {
		t.Errorf("update body summary = %v, want New", fields["summary"])
	}
	if iss.Key != "PROJ-7" {
		t.Errorf("Issue.Key = %q, want PROJ-7", iss.Key)
	}
}

func TestJiraCloseIssueTransitions(t *testing.T) {
	var listHit, postHit bool
	var postBody map[string]any
	c, _ := newTestJiraClient(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/transitions") {
			t.Errorf("unexpected path %s", r.URL.Path)
			return
		}
		switch r.Method {
		case "GET":
			listHit = true
			_, _ = io.WriteString(w, `{"transitions":[{"id":"11","name":"Start","to":{"name":"In Progress","statusCategory":{"key":"indeterminate"}}},{"id":"31","name":"Done","to":{"name":"Done","statusCategory":{"key":"done"}}}]}`)
		case "POST":
			postHit = true
			raw, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(raw, &postBody)
			w.WriteHeader(http.StatusNoContent)
		}
	})

	if err := c.CloseIssue("PROJ-7"); err != nil {
		t.Fatalf("CloseIssue: %v", err)
	}
	if !listHit || !postHit {
		t.Fatalf("close should list then POST a transition (list=%v post=%v)", listHit, postHit)
	}
	tr, _ := postBody["transition"].(map[string]any)
	if tr["id"] != "31" {
		t.Errorf("closed via transition id %v, want 31 (the done-category one)", tr["id"])
	}
}

func TestJiraListIssuesJQL(t *testing.T) {
	var gotPath, gotJQL, gotFields string
	c, _ := newTestJiraClient(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/label") {
			_, _ = io.WriteString(w, `{"values":["dross"]}`)
			return
		}
		gotPath = r.URL.Path
		gotJQL = r.URL.Query().Get("jql")
		gotFields = r.URL.Query().Get("fields")
		_, _ = io.WriteString(w, `{"issues":[{"key":"PROJ-7","fields":{"summary":"a"}},{"key":"PROJ-8","fields":{"summary":"b"}}]}`)
	})

	issues, err := c.ListIssues(IssueFilter{State: "open", Labels: []string{"dross"}})
	if err != nil {
		t.Fatalf("ListIssues: %v", err)
	}
	// Must be the current /search/jql endpoint — the legacy /search was
	// removed by Jira Cloud (HTTP 410, CHANGE-2046). Pinning the old path
	// here is what let the removed-endpoint bug ship unnoticed.
	if gotPath != "/rest/api/3/search/jql" {
		t.Fatalf("list hit %s, want /rest/api/3/search/jql", gotPath)
	}
	if !strings.Contains(gotJQL, "project = ") {
		t.Errorf("JQL %q missing project scope", gotJQL)
	}
	if !strings.Contains(gotJQL, `labels IN ("dross")`) {
		t.Errorf("JQL %q missing label clause", gotJQL)
	}
	if gotFields == "" {
		t.Errorf("list dropped the fields projection")
	}
	if len(issues) != 2 || issues[0].Key != "PROJ-7" || issues[1].Key != "PROJ-8" {
		t.Errorf("list returned %+v", issues)
	}
}

// TestJiraBuildJQLOrsLabels pins the OR semantics. AND-joining one
// `labels = ` clause per label matches only issues carrying every label at
// once, so a two-label pull returns near-nothing — the c-1 bug.
func TestJiraBuildJQLOrsLabels(t *testing.T) {
	c, _ := newTestJiraClient(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) })

	t.Run("labels share one IN clause", func(t *testing.T) {
		got := c.buildJQL(IssueFilter{Labels: []string{"bug", "enhancement"}})
		if !strings.Contains(got, `labels IN ("bug","enhancement")`) {
			t.Errorf("JQL = %q, want a single `labels IN (...)` clause", got)
		}
		if strings.Contains(got, " AND labels = ") {
			t.Errorf("JQL = %q, want no AND-joined `labels = ` clause", got)
		}
	})

	t.Run("the state clause stays AND-joined", func(t *testing.T) {
		got := c.buildJQL(IssueFilter{Labels: []string{"bug", "enhancement"}})
		if !strings.Contains(got, "statusCategory != Done") {
			t.Fatalf("JQL = %q, want the open-state clause preserved", got)
		}
		if !strings.Contains(got, "AND statusCategory != Done") {
			t.Errorf("JQL = %q, want statusCategory still AND-joined — the OR must not widen the state scope", got)
		}
	})

	t.Run("no labels emits no label clause", func(t *testing.T) {
		if got := c.buildJQL(IssueFilter{}); strings.Contains(got, "labels") {
			t.Errorf("JQL = %q, want no label clause at all", got)
		}
	})
}

func TestJiraEnsureMilestoneVersion(t *testing.T) {
	t.Run("creates when absent", func(t *testing.T) {
		var gotProjectGet, gotVersionPost string
		var postBody map[string]any
		c, _ := newTestJiraClient(t, func(w http.ResponseWriter, r *http.Request) {
			switch {
			case strings.HasPrefix(r.URL.Path, "/rest/api/3/project/") && r.Method == "GET":
				gotProjectGet = r.URL.Path
				_, _ = io.WriteString(w, `{"id":"10000","key":"PROJ","versions":[{"id":"10001","name":"v0.5"}]}`)
			case r.URL.Path == "/rest/api/3/version" && r.Method == "POST":
				gotVersionPost = r.URL.Path
				raw, _ := io.ReadAll(r.Body)
				_ = json.Unmarshal(raw, &postBody)
				_, _ = io.WriteString(w, `{"id":"10002","name":"v0.6"}`)
			default:
				t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			}
		})

		id, err := c.EnsureMilestone("v0.6", "the milestone")
		if err != nil {
			t.Fatalf("EnsureMilestone: %v", err)
		}
		if gotProjectGet != "/rest/api/3/project/PROJ" {
			t.Errorf("project discovery GET = %q", gotProjectGet)
		}
		if gotVersionPost == "" {
			t.Errorf("version not POSTed")
		}
		if pid, _ := postBody["projectId"].(float64); int(pid) != 10000 {
			t.Errorf("version body projectId = %v, want 10000", postBody["projectId"])
		}
		if postBody["name"] != "v0.6" {
			t.Errorf("version body name = %v, want v0.6", postBody["name"])
		}
		if id != "10002" {
			t.Errorf("id = %q, want 10002", id)
		}
	})

	t.Run("reuses when present", func(t *testing.T) {
		posted := false
		c, _ := newTestJiraClient(t, func(w http.ResponseWriter, r *http.Request) {
			if r.Method == "POST" {
				posted = true
			}
			_, _ = io.WriteString(w, `{"id":"10000","key":"PROJ","versions":[{"id":"10001","name":"v0.6"}]}`)
		})
		id, err := c.EnsureMilestoneEntity("version", "v0.6", "")
		if err != nil {
			t.Fatalf("EnsureMilestoneEntity: %v", err)
		}
		if posted {
			t.Error("should not POST a version that already exists")
		}
		if id != "10001" {
			t.Errorf("id = %q, want 10001", id)
		}
	})
}

func TestJiraSetStateTransitions(t *testing.T) {
	t.Run("maps and applies the matching transition", func(t *testing.T) {
		var postBody map[string]any
		c, _ := newTestJiraClient(t, func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case "GET":
				_, _ = io.WriteString(w, `{"transitions":[{"id":"21","name":"In Progress","to":{"name":"In Progress","statusCategory":{"key":"indeterminate"}}}]}`)
			case "POST":
				raw, _ := io.ReadAll(r.Body)
				_ = json.Unmarshal(raw, &postBody)
				w.WriteHeader(http.StatusNoContent)
			}
		})
		if err := c.SetState("PROJ-7", "in-progress", nil); err != nil {
			t.Fatalf("SetState: %v", err)
		}
		tr, _ := postBody["transition"].(map[string]any)
		if tr["id"] != "21" {
			t.Errorf("applied transition id %v, want 21", tr["id"])
		}
	})

	t.Run("override wins", func(t *testing.T) {
		var postBody map[string]any
		c, _ := newTestJiraClient(t, func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case "GET":
				_, _ = io.WriteString(w, `{"transitions":[{"id":"41","name":"Ship It","to":{"name":"Shipped","statusCategory":{"key":"done"}}}]}`)
			case "POST":
				raw, _ := io.ReadAll(r.Body)
				_ = json.Unmarshal(raw, &postBody)
				w.WriteHeader(http.StatusNoContent)
			}
		})
		if err := c.SetState("PROJ-7", "shipped", map[string]string{"shipped": "Shipped"}); err != nil {
			t.Fatalf("SetState: %v", err)
		}
		tr, _ := postBody["transition"].(map[string]any)
		if tr["id"] != "41" {
			t.Errorf("applied transition id %v, want 41", tr["id"])
		}
	})

	t.Run("unmapped state warns and skips", func(t *testing.T) {
		posted := false
		c, _ := newTestJiraClient(t, func(w http.ResponseWriter, r *http.Request) {
			if r.Method == "POST" {
				posted = true
			}
			_, _ = io.WriteString(w, `{"transitions":[]}`)
		})
		if err := c.SetState("PROJ-7", "no-such-state", nil); err != nil {
			t.Fatalf("unmapped state must not fail the sync: %v", err)
		}
		if posted {
			t.Error("unmapped state must not POST a transition")
		}
	})

	t.Run("no matching transition warns and skips", func(t *testing.T) {
		posted := false
		c, _ := newTestJiraClient(t, func(w http.ResponseWriter, r *http.Request) {
			if r.Method == "POST" {
				posted = true
			}
			_, _ = io.WriteString(w, `{"transitions":[{"id":"11","name":"Start","to":{"name":"Backlog","statusCategory":{"key":"new"}}}]}`)
		})
		if err := c.SetState("PROJ-7", "shipped", nil); err != nil {
			t.Fatalf("no-match must not fail the sync: %v", err)
		}
		if posted {
			t.Error("should not POST when no transition matches the target status")
		}
	})
}

// TestStateMapsResolveTheEmittedLifecycle pins both default maps against the
// vocabulary dross emits.
//
// resolveYouTrackState is asserted here rather than in youtrack_test.go on
// purpose: the failure this phase exists to close is the two providers
// answering the same question differently, and a shared table is where a key
// added to one map and not the other shows up as a diff instead of as a passing
// test in a file nobody opened.
func TestStateMapsResolveTheEmittedLifecycle(t *testing.T) {
	if got, ok := resolveJiraState("planned", nil); !ok || got != "To Do" {
		t.Errorf(`resolveJiraState("planned", nil) = (%q, %v), want ("To Do", true)`, got, ok)
	}
	if got, ok := resolveYouTrackState("planned", nil); !ok || got != "Open" {
		t.Errorf(`resolveYouTrackState("planned", nil) = (%q, %v), want ("Open", true)`, got, ok)
	}
	// "planning" is what issue.go emitted before the rename. It must map to
	// nothing on both providers — if either map ever grows it back as an alias,
	// the producer can drift again without anything failing.
	if v, ok := resolveJiraState("planning", nil); ok {
		t.Errorf(`resolveJiraState("planning", nil) = (%q, true), want ok=false`, v)
	}
	if v, ok := resolveYouTrackState("planning", nil); ok {
		t.Errorf(`resolveYouTrackState("planning", nil) = (%q, true), want ok=false`, v)
	}

	// Every status dross can emit resolves on both providers. The reverse
	// direction — a map key nothing emits — is gated by
	// internal/cmd/board_lifecycle_divergence_test.go, which can also see the
	// prompt call sites this package cannot.
	for _, s := range configenum.LifecycleStatuses.Values() {
		if _, ok := resolveJiraState(s, nil); !ok {
			t.Errorf("lifecycle status %q has no defaultJiraStateMap entry", s)
		}
		if _, ok := resolveYouTrackState(s, nil); !ok {
			t.Errorf("lifecycle status %q has no defaultYouTrackStateMap entry", s)
		}
	}
}

func TestJiraBasicAuth(t *testing.T) {
	var gotAuth string
	c, _ := newTestJiraClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		_, _ = io.WriteString(w, `{"key":"PROJ-7","fields":{"summary":"x"}}`)
	})
	if _, err := c.GetIssue("PROJ-7"); err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	// base64("me@example.com:secret")
	if !strings.HasPrefix(gotAuth, "Basic ") {
		t.Fatalf("Authorization = %q, want a Basic credential", gotAuth)
	}
	want := "Basic bWVAZXhhbXBsZS5jb206c2VjcmV0"
	if gotAuth != want {
		t.Errorf("Authorization = %q, want %q", gotAuth, want)
	}
}

// TestJiraUpdateIssueStateTransitions covers UpdateIssue's State-patch branch:
// State="closed" drives a done transition; any other State reopens via the
// indeterminate transition. Without this, the close-vs-reopen branch is untested.
func TestJiraUpdateIssueStateTransitions(t *testing.T) {
	var postedID string
	c, _ := newTestJiraClient(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/transitions") {
			t.Errorf("unexpected path %s", r.URL.Path)
			return
		}
		switch r.Method {
		case "GET":
			_, _ = io.WriteString(w, `{"transitions":[{"id":"11","name":"Start","to":{"name":"In Progress","statusCategory":{"key":"indeterminate"}}},{"id":"31","name":"Done","to":{"name":"Done","statusCategory":{"key":"done"}}}]}`)
		case "POST":
			var body map[string]any
			raw, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(raw, &body)
			tr, _ := body["transition"].(map[string]any)
			postedID, _ = tr["id"].(string)
			w.WriteHeader(http.StatusNoContent)
		}
	})

	closed := "closed"
	postedID = ""
	if _, err := c.UpdateIssue("PROJ-7", IssuePatch{State: &closed}); err != nil {
		t.Fatalf("UpdateIssue(closed): %v", err)
	}
	if postedID != "31" {
		t.Errorf("State=closed fired transition %q, want 31 (done)", postedID)
	}

	open := "open"
	postedID = ""
	if _, err := c.UpdateIssue("PROJ-7", IssuePatch{State: &open}); err != nil {
		t.Fatalf("UpdateIssue(open): %v", err)
	}
	if postedID != "11" {
		t.Errorf("State=open fired transition %q, want 11 (reopen/indeterminate)", postedID)
	}
}

// TestJiraErrorSnippetTruncatesAndHints is the Jira twin of the GitHub and
// YouTrack error-path tests. Jira's 401 hint is the one that names TWO config
// keys — the token env and [board].auth_user — because Jira Cloud authenticates
// with an email/token pair and getting the email wrong looks identical to a bad
// token from the outside.
func TestJiraErrorSnippetTruncatesAndHints(t *testing.T) {
	cases := []struct {
		name    string
		status  int
		body    string
		wantIn  []string
		wantOut []string
	}{
		{
			name:    "oversized body is truncated to the cap",
			status:  500,
			body:    bigBody(),
			wantOut: []string{"TAIL-MARKER"},
		},
		{
			name:   "a short body is passed through whole",
			status: 500,
			body:   "boom",
			wantIn: []string{"boom"},
		},
		{
			name:   "401 names both the token env and the auth user",
			status: 401,
			body:   "unauthorized",
			wantIn: []string{jiraTokenEnv, "auth_user"},
		},
		{
			name:   "403 names the permission problem",
			status: 403,
			body:   "forbidden",
			wantIn: []string{"lacks permission"},
		},
		{
			name:   "404 names the project and the config keys",
			status: 404,
			body:   "missing",
			wantIn: []string{"PROJ", "base_url"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := newTestJiraClient(t, func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			})

			_, err := c.GetIssue("PROJ-1")
			if err == nil {
				t.Fatalf("status %d returned no error", tc.status)
			}
			msg := err.Error()
			for _, want := range tc.wantIn {
				if !strings.Contains(msg, want) {
					t.Errorf("error %q does not mention %q", msg, want)
				}
			}
			for _, unwanted := range tc.wantOut {
				if strings.Contains(msg, unwanted) {
					t.Errorf("error %q still carries %q — the snippet was not truncated", msg, unwanted)
				}
			}
		})
	}
}

// TestJiraListIssuesDropsUnknownLabels covers the label-index gate — same
// contract as YouTrack's, against Jira's /label index.
func TestJiraListIssuesDropsUnknownLabels(t *testing.T) {
	t.Run("unknown label dropped and named, known one still queried", func(t *testing.T) {
		var gotJQL string
		var searchCalls int
		c, _ := newTestJiraClient(t, func(w http.ResponseWriter, r *http.Request) {
			if strings.HasSuffix(r.URL.Path, "/label") {
				_, _ = io.WriteString(w, `{"values":["bug"]}`)
				return
			}
			searchCalls++
			gotJQL = r.URL.Query().Get("jql")
			_, _ = io.WriteString(w, `{"issues":[{"key":"PROJ-1","fields":{"summary":"a"}}]}`)
		})

		warn := captureForgeStderr(t, func() {
			if _, err := c.ListIssues(IssueFilter{Labels: []string{"bug", "typo"}}); err != nil {
				t.Fatalf("ListIssues: %v", err)
			}
		})

		if searchCalls != 1 {
			t.Fatalf("issued %d searches, want 1", searchCalls)
		}
		if !strings.Contains(gotJQL, "bug") {
			t.Errorf("JQL %q lost the known label", gotJQL)
		}
		if strings.Contains(gotJQL, "typo") {
			t.Errorf("JQL %q carried the unknown label to the wire", gotJQL)
		}
		if !strings.Contains(warn, "typo") {
			t.Errorf("stderr = %q, want the dropped label named", warn)
		}
	})

	t.Run("every label unknown returns nothing, never the whole project", func(t *testing.T) {
		var searchCalls int
		c, _ := newTestJiraClient(t, func(w http.ResponseWriter, r *http.Request) {
			if strings.HasSuffix(r.URL.Path, "/label") {
				_, _ = io.WriteString(w, `{"values":["bug"]}`)
				return
			}
			searchCalls++
			_, _ = io.WriteString(w, `{"issues":[{"key":"PROJ-1","fields":{"summary":"everything"}}]}`)
		})

		var issues []Issue
		_ = captureForgeStderr(t, func() {
			var err error
			if issues, err = c.ListIssues(IssueFilter{Labels: []string{"typo"}}); err != nil {
				t.Fatalf("ListIssues: %v", err)
			}
		})
		if len(issues) != 0 || searchCalls != 0 {
			t.Errorf("returned %+v via %d searches, want zero of each", issues, searchCalls)
		}
	})

	t.Run("a failing label index refuses the query", func(t *testing.T) {
		var searchCalls int
		c, _ := newTestJiraClient(t, func(w http.ResponseWriter, r *http.Request) {
			if strings.HasSuffix(r.URL.Path, "/label") {
				w.WriteHeader(500)
				return
			}
			searchCalls++
			_, _ = io.WriteString(w, `{"issues":[]}`)
		})
		if _, err := c.ListIssues(IssueFilter{Labels: []string{"bug"}}); err == nil {
			t.Fatal("a 500 from the label index returned no error")
		}
		if searchCalls != 0 {
			t.Errorf("issued %d searches after the index failed, want 0", searchCalls)
		}
	})

	t.Run("an empty filter reads no label index at all", func(t *testing.T) {
		var labelCalls int
		c, _ := newTestJiraClient(t, func(w http.ResponseWriter, r *http.Request) {
			if strings.HasSuffix(r.URL.Path, "/label") {
				labelCalls++
			}
			_, _ = io.WriteString(w, `{"issues":[]}`)
		})
		if _, err := c.ListIssues(IssueFilter{}); err != nil {
			t.Fatalf("ListIssues: %v", err)
		}
		if labelCalls != 0 {
			t.Errorf("unlabelled list read the label index %d times — a blip must not fail `dross watch`", labelCalls)
		}
	})
}

// TestJiraLinkIssues covers the Jira half of c-7: a real link where the
// instance can express one, no duplicate on re-sync, and a distinguishable
// capability gap where it cannot.
func TestJiraLinkIssues(t *testing.T) {
	t.Run("links a new pair exactly once", func(t *testing.T) {
		var posts int
		var gotBody map[string]any
		c, _ := newTestJiraClient(t, func(w http.ResponseWriter, r *http.Request) {
			switch {
			case strings.HasSuffix(r.URL.Path, "/issueLinkType"):
				_, _ = io.WriteString(w, `{"issueLinkTypes":[{"id":"10000","name":"Blocks"},{"id":"10003","name":"Relates"}]}`)
			case strings.HasSuffix(r.URL.Path, "/issueLink") && r.Method == "POST":
				posts++
				raw, _ := io.ReadAll(r.Body)
				_ = json.Unmarshal(raw, &gotBody)
				w.WriteHeader(http.StatusCreated)
			default: // the issuelinks read
				_, _ = io.WriteString(w, `{"fields":{"issuelinks":[]}}`)
			}
		})

		if err := c.LinkIssues("PROJ-9", "PROJ-3"); err != nil {
			t.Fatalf("LinkIssues: %v", err)
		}
		if posts != 1 {
			t.Fatalf("issued %d link POSTs, want exactly 1", posts)
		}
		inward, _ := gotBody["inwardIssue"].(map[string]any)
		outward, _ := gotBody["outwardIssue"].(map[string]any)
		if inward["key"] != "PROJ-9" || outward["key"] != "PROJ-3" {
			t.Errorf("link body = %v, want it to name both issues", gotBody)
		}
		linkType, _ := gotBody["type"].(map[string]any)
		if name, _ := linkType["name"].(string); name == "" {
			t.Errorf("link body carries no type: %v", gotBody)
		}
	})

	t.Run("an existing pair sends no POST", func(t *testing.T) {
		var posts int
		c, _ := newTestJiraClient(t, func(w http.ResponseWriter, r *http.Request) {
			switch {
			case strings.HasSuffix(r.URL.Path, "/issueLinkType"):
				_, _ = io.WriteString(w, `{"issueLinkTypes":[{"name":"Relates"}]}`)
			case strings.HasSuffix(r.URL.Path, "/issueLink") && r.Method == "POST":
				posts++
				w.WriteHeader(http.StatusCreated)
			default:
				_, _ = io.WriteString(w, `{"fields":{"issuelinks":[{"outwardIssue":{"key":"PROJ-3"}}]}}`)
			}
		})
		if err := c.LinkIssues("PROJ-9", "PROJ-3"); err != nil {
			t.Fatalf("LinkIssues: %v", err)
		}
		if posts != 0 {
			t.Errorf("re-linking an existing pair issued %d POSTs, want 0", posts)
		}
	})

	t.Run("no link type yields ErrNoLinkType, not a generic failure", func(t *testing.T) {
		for _, tc := range []struct {
			name  string
			serve func(w http.ResponseWriter)
		}{
			{"empty list", func(w http.ResponseWriter) { _, _ = io.WriteString(w, `{"issueLinkTypes":[]}`) }},
			{"404", func(w http.ResponseWriter) { w.WriteHeader(404) }},
		} {
			t.Run(tc.name, func(t *testing.T) {
				c, _ := newTestJiraClient(t, func(w http.ResponseWriter, r *http.Request) {
					if strings.HasSuffix(r.URL.Path, "/issueLinkType") {
						tc.serve(w)
						return
					}
					_, _ = io.WriteString(w, `{"fields":{"issuelinks":[]}}`)
				})
				err := c.LinkIssues("PROJ-9", "PROJ-3")
				if !errors.Is(err, ErrNoLinkType) {
					t.Errorf("err = %v, want ErrNoLinkType — the caller must tell a capability gap from an outage", err)
				}
			})
		}
	})
}

// TestJiraDoneCategoryIsResolved pins the Jira half of the read-back signal:
// the done status-category is the tracker's own verdict that the issue is
// finished, and it has to reach Issue.Resolved as well as Issue.State.
func TestJiraDoneCategoryIsResolved(t *testing.T) {
	cases := []struct {
		name         string
		body         string
		wantResolved bool
		wantState    string
	}{
		{
			"done category",
			`{"key":"PROJ-7","fields":{"summary":"a","status":{"name":"Done","statusCategory":{"key":"done"}}}}`,
			true, "closed",
		},
		{
			"in progress",
			`{"key":"PROJ-7","fields":{"summary":"a","status":{"name":"In Progress","statusCategory":{"key":"indeterminate"}}}}`,
			false, "open",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := newTestJiraClient(t, func(w http.ResponseWriter, _ *http.Request) {
				_, _ = io.WriteString(w, tc.body)
			})
			iss, err := c.GetIssue("PROJ-7")
			if err != nil {
				t.Fatalf("GetIssue: %v", err)
			}
			if iss.Resolved != tc.wantResolved {
				t.Errorf("Resolved = %v, want %v", iss.Resolved, tc.wantResolved)
			}
			if iss.State != tc.wantState {
				t.Errorf("State = %q, want %q", iss.State, tc.wantState)
			}
		})
	}
}

// TestJiraCloseWithoutADoneTransitionErrors pins that a workflow with no path
// to the done category is a failure, not a shrug. Warning here would let ship
// print "(closed)" for an issue nothing moved.
func TestJiraCloseWithoutADoneTransitionErrors(t *testing.T) {
	c, _ := newTestJiraClient(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/transitions") && r.Method == "GET" {
			_, _ = io.WriteString(w, `{"transitions":[{"id":"11","name":"Start","to":{"name":"In Progress","statusCategory":{"key":"indeterminate"}}}]}`)
			return
		}
		t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
	})
	if err := c.CloseIssue("PROJ-7"); err == nil {
		t.Fatal("a board with no done-category transition closed without error")
	}
}
