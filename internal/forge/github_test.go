package forge

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const ghTokenEnv = "MOCK_GITHUB_TOKEN"

// newTestGitHubClient spins up an httptest server and points a GitHubClient at
// it. The token env is set for the test's lifetime. boardID, when non-empty,
// enables the Projects v2 add-to-board step.
func newTestGitHubClient(t *testing.T, boardID string, h http.HandlerFunc) (*GitHubClient, *httptest.Server) {
	t.Helper()
	t.Setenv(ghTokenEnv, "secret")
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c, err := NewGitHubProjects(Config{APIBase: srv.URL, AuthEnv: ghTokenEnv, Project: "octo/repo", BoardID: boardID})
	if err != nil {
		t.Fatalf("NewGitHubProjects: %v", err)
	}
	return c, srv
}

// TestNewAcceptsGitHubProjects pins that the GitHub board client constructs
// from a valid config (never ErrNotImplemented) and rejects bad config.
func TestNewAcceptsGitHubProjects(t *testing.T) {
	t.Setenv(ghTokenEnv, "secret")
	good := Config{AuthEnv: ghTokenEnv, Project: "octo/repo"}
	c, err := NewGitHubProjects(good)
	if err != nil {
		t.Fatalf("NewGitHubProjects(good): %v", err)
	}
	if c == nil {
		t.Fatal("NewGitHubProjects(good): nil client")
	}
	// APIBase defaults to the public API when unset.
	if c.apiBase != "https://api.github.com" {
		t.Errorf("default apiBase = %q, want https://api.github.com", c.apiBase)
	}

	bad := []struct {
		name string
		cfg  Config
		want string
	}{
		{"missing authenv", Config{Project: "octo/repo"}, "needs AuthEnv"},
		{"missing project", Config{AuthEnv: ghTokenEnv}, "needs Project"},
		{"malformed project", Config{AuthEnv: ghTokenEnv, Project: "justrepo"}, "owner/repo"},
		{"unset token", Config{AuthEnv: "DROSS_DEFINITELY_UNSET", Project: "octo/repo"}, "is not set"},
	}
	for _, tc := range bad {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := NewGitHubProjects(tc.cfg); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want error containing %q, got %v", tc.want, err)
			}
		})
	}
}

func TestGitHubCreateIssueNoProject(t *testing.T) {
	var gotPath, gotMethod, gotAuth, gotAccept string
	var gotBody map[string]any
	graphqlHit := false
	c, _ := newTestGitHubClient(t, "", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/graphql" {
			graphqlHit = true
			return
		}
		gotMethod, gotPath = r.Method, r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotAccept = r.Header.Get("Accept")
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"number":42,"node_id":"I_kwABCD","html_url":"https://github.com/octo/repo/issues/42","state":"open","title":"Hi","body":"body"}`)
	})

	iss, err := c.CreateIssue(IssueInput{Title: "Hi", Body: "body", Labels: []string{"dross"}, Milestone: 3})
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	if gotMethod != "POST" || gotPath != "/repos/octo/repo/issues" {
		t.Fatalf("create hit %s %s, want POST /repos/octo/repo/issues", gotMethod, gotPath)
	}
	if gotBody["title"] != "Hi" || gotBody["body"] != "body" {
		t.Errorf("create body wrong: %v", gotBody)
	}
	if ms, _ := gotBody["milestone"].(float64); int(ms) != 3 {
		t.Errorf("create body milestone = %v, want 3", gotBody["milestone"])
	}
	if gotAuth != "Bearer secret" {
		t.Errorf("Authorization = %q, want \"Bearer secret\"", gotAuth)
	}
	if gotAccept != "application/vnd.github+json" {
		t.Errorf("Accept = %q, want application/vnd.github+json", gotAccept)
	}
	if iss.Number != 42 || iss.Key != "42" {
		t.Errorf("issue number/key wrong: %+v", iss)
	}
	if graphqlHit {
		t.Error("no project configured — GraphQL must not fire")
	}
}

func TestGitHubCreateIssueAddsToProject(t *testing.T) {
	var gqlBody map[string]any
	graphqlHit := false
	c, _ := newTestGitHubClient(t, "PVT_kwDOABCD", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/graphql" {
			graphqlHit = true
			raw, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(raw, &gqlBody)
			_, _ = io.WriteString(w, `{"data":{"addProjectV2ItemById":{"item":{"id":"PVTI_lADO"}}}}`)
			return
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"number":42,"node_id":"I_kwABCD","state":"open","title":"Hi"}`)
	})

	if _, err := c.CreateIssue(IssueInput{Title: "Hi"}); err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	if !graphqlHit {
		t.Fatal("with a project configured, CreateIssue must POST the addProjectV2ItemById mutation")
	}
	query, _ := gqlBody["query"].(string)
	if !strings.Contains(query, "addProjectV2ItemById") {
		t.Errorf("graphql query missing the mutation: %q", query)
	}
	vars, _ := gqlBody["variables"].(map[string]any)
	if vars["projectId"] != "PVT_kwDOABCD" {
		t.Errorf("graphql projectId = %v, want PVT_kwDOABCD", vars["projectId"])
	}
	if vars["contentId"] != "I_kwABCD" {
		t.Errorf("graphql contentId = %v, want the issue node_id I_kwABCD", vars["contentId"])
	}
}

func TestGitHubCreateIssueProjectFailureIsBestEffort(t *testing.T) {
	// A GraphQL error must not fail the issue create — the issue still returns.
	c, _ := newTestGitHubClient(t, "PVT_kwDOABCD", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/graphql" {
			_, _ = io.WriteString(w, `{"errors":[{"message":"boom"}]}`)
			return
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"number":42,"node_id":"I_kwABCD"}`)
	})
	iss, err := c.CreateIssue(IssueInput{Title: "Hi"})
	if err != nil {
		t.Fatalf("CreateIssue must not fail when the project add fails: %v", err)
	}
	if iss.Key != "42" {
		t.Errorf("issue still returns: got %+v", iss)
	}
}

func TestGitHubGetUpdateCloseList(t *testing.T) {
	t.Run("get", func(t *testing.T) {
		var gotPath string
		c, _ := newTestGitHubClient(t, "", func(w http.ResponseWriter, r *http.Request) {
			gotPath = r.URL.Path
			_, _ = io.WriteString(w, `{"number":42,"title":"Hi","state":"open","labels":[{"name":"dross"}],"milestone":{"title":"v0.6"}}`)
		})
		iss, err := c.GetIssue("42")
		if err != nil {
			t.Fatalf("GetIssue: %v", err)
		}
		if gotPath != "/repos/octo/repo/issues/42" {
			t.Fatalf("get hit %s", gotPath)
		}
		if iss.Milestone != "v0.6" || len(iss.Labels) != 1 || iss.Labels[0] != "dross" {
			t.Errorf("get returned %+v", iss)
		}
	})

	t.Run("update patches labels as names", func(t *testing.T) {
		var gotMethod, gotPath string
		var gotBody map[string]any
		c, _ := newTestGitHubClient(t, "", func(w http.ResponseWriter, r *http.Request) {
			gotMethod, gotPath = r.Method, r.URL.Path
			raw, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(raw, &gotBody)
			_, _ = io.WriteString(w, `{"number":42}`)
		})
		title := "New"
		labels := []string{"dross", "bug"}
		if _, err := c.UpdateIssue("42", IssuePatch{Title: &title, Labels: &labels}); err != nil {
			t.Fatalf("UpdateIssue: %v", err)
		}
		if gotMethod != "PATCH" || gotPath != "/repos/octo/repo/issues/42" {
			t.Fatalf("update hit %s %s", gotMethod, gotPath)
		}
		gotLabels, _ := gotBody["labels"].([]any)
		if len(gotLabels) != 2 || gotLabels[0] != "dross" {
			t.Errorf("labels not sent as names: %v", gotBody["labels"])
		}
	})

	t.Run("close patches state=closed", func(t *testing.T) {
		var gotBody map[string]any
		c, _ := newTestGitHubClient(t, "", func(w http.ResponseWriter, r *http.Request) {
			raw, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(raw, &gotBody)
			_, _ = io.WriteString(w, `{"number":42}`)
		})
		if err := c.CloseIssue("42"); err != nil {
			t.Fatalf("CloseIssue: %v", err)
		}
		if gotBody["state"] != "closed" {
			t.Errorf("close body state = %v, want closed", gotBody["state"])
		}
	})

	t.Run("list excludes PRs and passes filters", func(t *testing.T) {
		var gotState, gotLabels string
		c, _ := newTestGitHubClient(t, "", func(w http.ResponseWriter, r *http.Request) {
			gotState = r.URL.Query().Get("state")
			gotLabels = r.URL.Query().Get("labels")
			_, _ = io.WriteString(w, `[{"number":1,"title":"issue"},{"number":2,"title":"a pr","pull_request":{"url":"x"}}]`)
		})
		issues, err := c.ListIssues(IssueFilter{State: "open", Labels: []string{"dross"}})
		if err != nil {
			t.Fatalf("ListIssues: %v", err)
		}
		if gotState != "open" || gotLabels != "dross" {
			t.Errorf("list filters state=%q labels=%q", gotState, gotLabels)
		}
		if len(issues) != 1 || issues[0].Number != 1 {
			t.Errorf("list should exclude the PR, got %+v", issues)
		}
	})
}

func TestGitHubEnsureMilestone(t *testing.T) {
	t.Run("reuses existing", func(t *testing.T) {
		posted := false
		c, _ := newTestGitHubClient(t, "", func(w http.ResponseWriter, r *http.Request) {
			if r.Method == "POST" {
				posted = true
			}
			if strings.HasSuffix(r.URL.Path, "/milestones") && r.Method == "GET" {
				_, _ = io.WriteString(w, `[{"number":7,"title":"v0.6"}]`)
				return
			}
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		})
		id, err := c.EnsureMilestone("v0.6", "desc")
		if err != nil {
			t.Fatalf("EnsureMilestone: %v", err)
		}
		if id != "7" {
			t.Errorf("id = %q, want 7 (the integer milestone number as a string)", id)
		}
		if posted {
			t.Error("should not POST when the milestone exists")
		}
	})

	t.Run("creates when absent", func(t *testing.T) {
		var gotBody map[string]any
		c, _ := newTestGitHubClient(t, "", func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case "GET":
				_, _ = io.WriteString(w, `[]`)
			case "POST":
				raw, _ := io.ReadAll(r.Body)
				_ = json.Unmarshal(raw, &gotBody)
				w.WriteHeader(http.StatusCreated)
				_, _ = io.WriteString(w, `{"number":9}`)
			}
		})
		id, err := c.EnsureMilestone("v0.7", "desc")
		if err != nil {
			t.Fatalf("EnsureMilestone: %v", err)
		}
		if gotBody["title"] != "v0.7" {
			t.Errorf("create milestone body title = %v", gotBody["title"])
		}
		if id != "9" {
			t.Errorf("id = %q, want 9", id)
		}
	})
}
