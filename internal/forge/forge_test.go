package forge

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const tokenEnv = "MOCK_FORGE_TOKEN"

// newTestClient spins up an httptest server, points a forgejo Client at it,
// and returns both. The token env is set for the test's lifetime.
func newTestClient(t *testing.T, h http.HandlerFunc) (*Client, *httptest.Server) {
	t.Helper()
	t.Setenv(tokenEnv, "secret")
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c, err := New(Config{
		Provider: "forgejo",
		URL:      "https://forge.example/me/proj",
		APIBase:  srv.URL,
		AuthEnv:  tokenEnv,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c, srv
}

func TestNewValidation(t *testing.T) {
	t.Setenv(tokenEnv, "secret")
	tests := []struct {
		name    string
		cfg     Config
		wantErr string // substring; "" means expect ErrNotImplemented sentinel
		notImpl bool
	}{
		{"github not implemented", Config{Provider: "github", URL: "https://github.com/o/r", APIBase: "x", AuthEnv: tokenEnv}, "", true},
		{"unsupported provider", Config{Provider: "bitbucket", URL: "https://x/o/r", APIBase: "x", AuthEnv: tokenEnv}, "unsupported provider", false},
		{"missing apibase", Config{Provider: "forgejo", URL: "https://x/o/r", AuthEnv: tokenEnv}, "needs APIBase", false},
		{"missing authenv", Config{Provider: "forgejo", URL: "https://x/o/r", APIBase: "x"}, "needs AuthEnv", false},
		{"unset token", Config{Provider: "forgejo", URL: "https://x/o/r", APIBase: "x", AuthEnv: "DROSS_DEFINITELY_UNSET"}, "is not set", false},
		{"bad url", Config{Provider: "forgejo", URL: "not-a-url", APIBase: "x", AuthEnv: tokenEnv}, "bad repo URL", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := New(tc.cfg)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if tc.notImpl {
				if !errors.Is(err, ErrNotImplemented) {
					t.Errorf("want ErrNotImplemented, got %v", err)
				}
				return
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("want error containing %q, got %v", tc.wantErr, err)
			}
		})
	}
}

func TestEnsureMilestoneExisting(t *testing.T) {
	posted := false
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			posted = true
		}
		if strings.HasSuffix(r.URL.Path, "/milestones") && r.Method == "GET" {
			_, _ = w.Write([]byte(`[{"id":7,"title":"v0.2"},{"id":8,"title":"v0.3"}]`))
			return
		}
		t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
	})
	id, err := c.EnsureMilestone("v0.2", "desc")
	if err != nil {
		t.Fatalf("EnsureMilestone: %v", err)
	}
	if id != 7 {
		t.Errorf("id = %d, want 7", id)
	}
	if posted {
		t.Error("should not POST when milestone already exists")
	}
}

func TestEnsureMilestoneCreates(t *testing.T) {
	var gotBody map[string]any
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case "GET":
			_, _ = w.Write([]byte(`[]`))
		case "POST":
			b, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(b, &gotBody)
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":42}`))
		}
	})
	id, err := c.EnsureMilestone("v1.0", "the desc")
	if err != nil {
		t.Fatalf("EnsureMilestone: %v", err)
	}
	if id != 42 {
		t.Errorf("id = %d, want 42", id)
	}
	if gotBody["title"] != "v1.0" || gotBody["description"] != "the desc" {
		t.Errorf("create body = %v", gotBody)
	}
}

func TestCreateIssueWithLabelsAndMilestone(t *testing.T) {
	var (
		labelsListed bool
		createdLabel string
		issueBody    map[string]any
	)
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/labels") && r.Method == "GET":
			labelsListed = true
			// "dross" exists with id 1; "dross/status:planning" does not.
			_, _ = w.Write([]byte(`[{"id":1,"name":"dross"}]`))
		case strings.HasSuffix(r.URL.Path, "/labels") && r.Method == "POST":
			b, _ := io.ReadAll(r.Body)
			var lb map[string]any
			_ = json.Unmarshal(b, &lb)
			createdLabel, _ = lb["name"].(string)
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":9}`))
		case strings.HasSuffix(r.URL.Path, "/issues") && r.Method == "POST":
			b, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(b, &issueBody)
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"number":12,"html_url":"https://forge.example/me/proj/issues/12","state":"open"}`))
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
	})

	iss, err := c.CreateIssue(IssueInput{
		Title:     "Phase 02 — auth",
		Body:      "## tasks\n- [ ] task 01",
		Labels:    []string{"dross", "dross/status:planning"},
		Milestone: 7,
	})
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	if iss.Number != 12 || iss.URL == "" {
		t.Errorf("issue = %+v", iss)
	}
	if !labelsListed {
		t.Error("labels were not listed before resolving ids")
	}
	if createdLabel != "dross/status:planning" {
		t.Errorf("expected missing status label to be created, created = %q", createdLabel)
	}
	// label ids: dross=1 (existing), status=9 (created)
	ids, _ := issueBody["labels"].([]any)
	if len(ids) != 2 || ids[0] != float64(1) || ids[1] != float64(9) {
		t.Errorf("issue label ids = %v", issueBody["labels"])
	}
	if issueBody["milestone"] != float64(7) {
		t.Errorf("milestone = %v", issueBody["milestone"])
	}
}

func TestUpdateIssueBodyAndLabels(t *testing.T) {
	var (
		patchBody    map[string]any
		labelPutBody map[string]any
	)
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/labels") && r.Method == "GET":
			_, _ = w.Write([]byte(`[{"id":1,"name":"dross"},{"id":2,"name":"dross/status:in-progress"}]`))
		case strings.Contains(r.URL.Path, "/issues/12/labels") && r.Method == "PUT":
			b, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(b, &labelPutBody)
			// Forgejo/Gitea returns the resulting LabelList (not the issue);
			// dross must not try to decode this into issueResponse.
			_, _ = w.Write([]byte(`[{"id":1,"name":"dross"},{"id":2,"name":"dross/status:in-progress"}]`))
		case strings.HasSuffix(r.URL.Path, "/issues/12") && r.Method == "PATCH":
			b, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(b, &patchBody)
			_, _ = w.Write([]byte(`{"number":12,"state":"open"}`))
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
	})

	newBody := "updated body"
	labels := []string{"dross", "dross/status:in-progress"}
	if _, err := c.UpdateIssue(12, IssuePatch{Body: &newBody, Labels: &labels}); err != nil {
		t.Fatalf("UpdateIssue: %v", err)
	}
	if patchBody["body"] != "updated body" {
		t.Errorf("patch body = %v", patchBody)
	}
	ids, _ := labelPutBody["labels"].([]any)
	if len(ids) != 2 || ids[0] != float64(1) || ids[1] != float64(2) {
		t.Errorf("label PUT ids = %v", labelPutBody["labels"])
	}
}

func TestCloseIssue(t *testing.T) {
	var patchBody map[string]any
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "PATCH" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &patchBody)
		_, _ = w.Write([]byte(`{"number":12,"state":"closed"}`))
	})
	if err := c.CloseIssue(12); err != nil {
		t.Fatalf("CloseIssue: %v", err)
	}
	if patchBody["state"] != "closed" {
		t.Errorf("state = %v, want closed", patchBody["state"])
	}
}

func TestListIssuesExcludesPRsAndPassesFilters(t *testing.T) {
	var gotQuery string
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		_, _ = w.Write([]byte(`[
			{"number":1,"title":"a bug","state":"open","labels":[{"name":"bug"}]},
			{"number":2,"title":"a PR","state":"open","pull_request":{"merged":false}}
		]`))
	})
	got, err := c.ListIssues(IssueFilter{State: "open", Labels: []string{"bug", "enhancement"}})
	if err != nil {
		t.Fatalf("ListIssues: %v", err)
	}
	if len(got) != 1 || got[0].Number != 1 {
		t.Fatalf("expected only the non-PR issue, got %+v", got)
	}
	if got[0].Labels[0] != "bug" {
		t.Errorf("labels = %v", got[0].Labels)
	}
	for _, want := range []string{"state=open", "type=issues", "labels=bug%2Cenhancement"} {
		if !strings.Contains(gotQuery, want) {
			t.Errorf("query %q missing %q", gotQuery, want)
		}
	}
}

func TestGetIssue(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/issues/5") || r.Method != "GET" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"number":5,"title":"t","state":"closed","milestone":{"title":"v0.2"}}`))
	})
	iss, err := c.GetIssue(5)
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	if iss.Number != 5 || iss.State != "closed" || iss.Milestone != "v0.2" {
		t.Errorf("issue = %+v", iss)
	}
}

func TestDoSurfacesHTTPError(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"message":"token required"}`))
	})
	_, err := c.GetIssue(1)
	if err == nil || !strings.Contains(err.Error(), "HTTP 401") {
		t.Errorf("expected HTTP 401 error, got %v", err)
	}
}

func TestSendsTokenHeader(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "token secret" {
			t.Errorf("auth header = %q", got)
		}
		_, _ = w.Write([]byte(`{"number":1}`))
	})
	if _, err := c.GetIssue(1); err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
}

func TestSplitOwnerRepo(t *testing.T) {
	tests := []struct {
		url         string
		owner, repo string
		wantErr     bool
	}{
		{"https://forge.example/me/proj", "me", "proj", false},
		{"https://github.com/Rivil/dross.git", "Rivil", "dross", false},
		{"https://forge.example/", "", "", true},
		{"https://forge.example/lonely", "", "", true},
		{"not-a-url", "", "", true},
	}
	for _, tc := range tests {
		owner, repo, err := splitOwnerRepo(tc.url)
		if (err != nil) != tc.wantErr {
			t.Errorf("%q: err = %v, wantErr %v", tc.url, err, tc.wantErr)
		}
		if owner != tc.owner || repo != tc.repo {
			t.Errorf("%q: got %q/%q want %q/%q", tc.url, owner, repo, tc.owner, tc.repo)
		}
	}
}
