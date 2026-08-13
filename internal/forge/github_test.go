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
	c, err := NewGitHubProjects(Config{APIBase: srv.URL, AuthEnv: ghTokenEnv, Project: "octo/repo", BoardID: boardID, Hosts: allowingSelf(srv.URL)})
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
			if strings.HasSuffix(r.URL.Path, "/labels") {
				_, _ = io.WriteString(w, `[{"name":"dross"}]`)
				return
			}
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

// TestGitHubListIssuesOrsLabels pins the fan-out. GitHub's `labels=` param
// ANDs the names it is handed, so a comma-joined filter returns only issues
// carrying every label — c-1's bug in its GitHub form. The fix is one request
// per label, unioned by number.
func TestGitHubListIssuesOrsLabels(t *testing.T) {
	t.Run("one request per label, results unioned", func(t *testing.T) {
		var gotLabels []string
		c, _ := newTestGitHubClient(t, "", func(w http.ResponseWriter, r *http.Request) {
			if strings.HasSuffix(r.URL.Path, "/labels") {
				_, _ = io.WriteString(w, `[{"name":"bug"},{"name":"enhancement"}]`)
				return
			}
			l := r.URL.Query().Get("labels")
			gotLabels = append(gotLabels, l)
			switch l {
			case "bug":
				_, _ = io.WriteString(w, `[{"number":1,"title":"a bug"}]`)
			case "enhancement":
				_, _ = io.WriteString(w, `[{"number":2,"title":"an enhancement"}]`)
			default:
				t.Errorf("unexpected labels param %q", l)
				_, _ = io.WriteString(w, `[]`)
			}
		})
		issues, err := c.ListIssues(IssueFilter{Labels: []string{"bug", "enhancement"}})
		if err != nil {
			t.Fatalf("ListIssues: %v", err)
		}
		if len(gotLabels) != 2 {
			t.Errorf("issued %d requests (%v), want exactly 2 — one per label", len(gotLabels), gotLabels)
		}
		if len(issues) != 2 {
			t.Fatalf("union returned %+v, want both issues", issues)
		}
		if issues[0].Number != 1 || issues[1].Number != 2 {
			t.Errorf("union = %+v, want #1 then #2", issues)
		}
	})

	t.Run("an issue under both labels appears once", func(t *testing.T) {
		c, _ := newTestGitHubClient(t, "", func(w http.ResponseWriter, r *http.Request) {
			if strings.HasSuffix(r.URL.Path, "/labels") {
				_, _ = io.WriteString(w, `[{"name":"bug"},{"name":"enhancement"}]`)
				return
			}
			_, _ = io.WriteString(w, `[{"number":5,"title":"both"}]`)
		})
		issues, err := c.ListIssues(IssueFilter{Labels: []string{"bug", "enhancement"}})
		if err != nil {
			t.Fatalf("ListIssues: %v", err)
		}
		if len(issues) != 1 || issues[0].Number != 5 {
			t.Errorf("union = %+v, want #5 exactly once", issues)
		}
	})

	t.Run("an unlabelled filter issues one request with no labels param", func(t *testing.T) {
		var requests int
		var sawLabels bool
		c, _ := newTestGitHubClient(t, "", func(w http.ResponseWriter, r *http.Request) {
			requests++
			if _, ok := r.URL.Query()["labels"]; ok {
				sawLabels = true
			}
			_, _ = io.WriteString(w, `[{"number":1,"title":"issue"}]`)
		})
		if _, err := c.ListIssues(IssueFilter{}); err != nil {
			t.Fatalf("ListIssues: %v", err)
		}
		if requests != 1 {
			t.Errorf("issued %d requests, want exactly 1 for an unlabelled filter", requests)
		}
		if sawLabels {
			t.Error("an unlabelled filter sent a labels param")
		}
	})

	t.Run("a PR in one label's response stays out of the union", func(t *testing.T) {
		c, _ := newTestGitHubClient(t, "", func(w http.ResponseWriter, r *http.Request) {
			if strings.HasSuffix(r.URL.Path, "/labels") {
				_, _ = io.WriteString(w, `[{"name":"bug"},{"name":"enhancement"}]`)
				return
			}
			if r.URL.Query().Get("labels") == "bug" {
				_, _ = io.WriteString(w, `[{"number":1,"title":"issue"},{"number":9,"title":"a pr","pull_request":{"url":"x"}}]`)
				return
			}
			_, _ = io.WriteString(w, `[{"number":2,"title":"other"}]`)
		})
		issues, err := c.ListIssues(IssueFilter{Labels: []string{"bug", "enhancement"}})
		if err != nil {
			t.Fatalf("ListIssues: %v", err)
		}
		for _, iss := range issues {
			if iss.Number == 9 {
				t.Fatalf("union carried the PR: %+v", issues)
			}
		}
		if len(issues) != 2 {
			t.Errorf("union = %+v, want the two real issues", issues)
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

// TestGitHubErrorSnippetTruncatesAndHints is the GitHub twin of the YouTrack
// and Jira error-path tests. All three transports share this shape and each is
// pinned in its own package file, because the hint text differs per provider
// and that text is the whole point: it turns a bare 401 into "which token".
func TestGitHubErrorSnippetTruncatesAndHints(t *testing.T) {
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
			name:   "401 names the token env",
			status: 401,
			body:   "unauthorized",
			wantIn: []string{ghTokenEnv, "expired"},
		},
		{
			name:   "403 mentions permission or rate limiting",
			status: 403,
			body:   "forbidden",
			wantIn: []string{"lacks permission"},
		},
		{
			name:   "404 names the repo and the config key",
			status: 404,
			body:   "missing",
			wantIn: []string{"octo", "repo", "project"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := newTestGitHubClient(t, "", func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			})

			_, err := c.GetIssue("1")
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

// TestGitHubListIssuesDropsUnknownLabels covers the label-index gate — same
// contract as YouTrack's and Jira's, against the repo's /labels index.
func TestGitHubListIssuesDropsUnknownLabels(t *testing.T) {
	t.Run("unknown label dropped and named, known one still queried", func(t *testing.T) {
		var queried []string
		c, _ := newTestGitHubClient(t, "", func(w http.ResponseWriter, r *http.Request) {
			if strings.HasSuffix(r.URL.Path, "/labels") {
				_, _ = io.WriteString(w, `[{"name":"bug"}]`)
				return
			}
			queried = append(queried, r.URL.Query().Get("labels"))
			_, _ = io.WriteString(w, `[{"number":1,"title":"a"}]`)
		})

		warn := captureForgeStderr(t, func() {
			if _, err := c.ListIssues(IssueFilter{Labels: []string{"bug", "typo"}}); err != nil {
				t.Fatalf("ListIssues: %v", err)
			}
		})

		if len(queried) != 1 || queried[0] != "bug" {
			t.Errorf("queried %v, want exactly the known label", queried)
		}
		if !strings.Contains(warn, "typo") {
			t.Errorf("stderr = %q, want the dropped label named", warn)
		}
	})

	t.Run("every label unknown returns nothing, never the whole repo", func(t *testing.T) {
		var issueCalls int
		c, _ := newTestGitHubClient(t, "", func(w http.ResponseWriter, r *http.Request) {
			if strings.HasSuffix(r.URL.Path, "/labels") {
				_, _ = io.WriteString(w, `[{"name":"bug"}]`)
				return
			}
			issueCalls++
			_, _ = io.WriteString(w, `[{"number":1,"title":"everything"}]`)
		})

		var issues []Issue
		_ = captureForgeStderr(t, func() {
			var err error
			if issues, err = c.ListIssues(IssueFilter{Labels: []string{"typo"}}); err != nil {
				t.Fatalf("ListIssues: %v", err)
			}
		})
		if len(issues) != 0 || issueCalls != 0 {
			t.Errorf("returned %+v via %d queries, want zero of each", issues, issueCalls)
		}
	})

	t.Run("a failing label index refuses the query", func(t *testing.T) {
		var issueCalls int
		c, _ := newTestGitHubClient(t, "", func(w http.ResponseWriter, r *http.Request) {
			if strings.HasSuffix(r.URL.Path, "/labels") {
				w.WriteHeader(500)
				return
			}
			issueCalls++
			_, _ = io.WriteString(w, `[]`)
		})
		if _, err := c.ListIssues(IssueFilter{Labels: []string{"bug"}}); err == nil {
			t.Fatal("a 500 from the label index returned no error")
		}
		if issueCalls != 0 {
			t.Errorf("issued %d issue queries after the index failed, want 0", issueCalls)
		}
	})

	t.Run("an empty filter reads no label index at all", func(t *testing.T) {
		var labelCalls int
		c, _ := newTestGitHubClient(t, "", func(w http.ResponseWriter, r *http.Request) {
			if strings.HasSuffix(r.URL.Path, "/labels") {
				labelCalls++
			}
			_, _ = io.WriteString(w, `[]`)
		})
		if _, err := c.ListIssues(IssueFilter{}); err != nil {
			t.Fatalf("ListIssues: %v", err)
		}
		if labelCalls != 0 {
			t.Errorf("unlabelled list read the label index %d times — a blip must not fail `dross watch`", labelCalls)
		}
	})
}
