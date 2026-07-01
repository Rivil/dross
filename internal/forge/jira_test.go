package forge

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const jiraTokenEnv = "MOCK_JIRA_TOKEN"

// newTestJiraClient spins up an httptest server and points a JiraClient at it.
// The token env is set for the test's lifetime.
func newTestJiraClient(t *testing.T, h http.HandlerFunc) (*JiraClient, *httptest.Server) {
	t.Helper()
	t.Setenv(jiraTokenEnv, "secret")
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c, err := NewJira(Config{APIBase: srv.URL, AuthEnv: jiraTokenEnv, Project: "PROJ", AuthUser: "me@example.com"})
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
		{"unset token", Config{APIBase: "https://x", AuthEnv: "DROSS_DEFINITELY_UNSET", Project: "PROJ", AuthUser: "me@example.com"}, "is not set"},
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
		gotPath = r.URL.Path
		gotJQL = r.URL.Query().Get("jql")
		gotFields = r.URL.Query().Get("fields")
		_, _ = io.WriteString(w, `{"issues":[{"key":"PROJ-7","fields":{"summary":"a"}},{"key":"PROJ-8","fields":{"summary":"b"}}]}`)
	})

	issues, err := c.ListIssues(IssueFilter{State: "open", Labels: []string{"dross"}})
	if err != nil {
		t.Fatalf("ListIssues: %v", err)
	}
	if gotPath != "/rest/api/3/search" {
		t.Fatalf("list hit %s, want /rest/api/3/search", gotPath)
	}
	if !strings.Contains(gotJQL, "project = ") {
		t.Errorf("JQL %q missing project scope", gotJQL)
	}
	if !strings.Contains(gotJQL, "labels = ") {
		t.Errorf("JQL %q missing label clause", gotJQL)
	}
	if gotFields == "" {
		t.Errorf("list dropped the fields projection")
	}
	if len(issues) != 2 || issues[0].Key != "PROJ-7" || issues[1].Key != "PROJ-8" {
		t.Errorf("list returned %+v", issues)
	}
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
