package forge

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/configenum"
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
		Hosts:    allowingSelf(srv.URL),
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
		{"unset token", Config{Provider: "forgejo", URL: "https://x/o/r", APIBase: "https://x", AuthEnv: "DROSS_DEFINITELY_UNSET", Hosts: allowingSelf("https://x")}, "is not set", false},
		{"bad url", Config{Provider: "forgejo", URL: "not-a-url", APIBase: "https://x", AuthEnv: tokenEnv, Hosts: allowingSelf("https://x")}, "bad repo URL", false},
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
	if id != "7" {
		t.Errorf("id = %q, want 7", id)
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
	if id != "42" {
		t.Errorf("id = %q, want 42", id)
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
	if _, err := c.UpdateIssue("12", IssuePatch{Body: &newBody, Labels: &labels}); err != nil {
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
	if err := c.CloseIssue("12"); err != nil {
		t.Fatalf("CloseIssue: %v", err)
	}
	if patchBody["state"] != "closed" {
		t.Errorf("state = %v, want closed", patchBody["state"])
	}
}

func TestListIssuesExcludesPRsAndPassesFilters(t *testing.T) {
	var queries []string
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/labels") {
			_, _ = w.Write([]byte(`[{"id":1,"name":"bug"},{"id":2,"name":"enhancement"}]`))
			return
		}
		queries = append(queries, r.URL.RawQuery)
		if strings.Contains(r.URL.RawQuery, "labels=bug") {
			_, _ = w.Write([]byte(`[
				{"number":1,"title":"a bug","state":"open","labels":[{"name":"bug"}]},
				{"number":2,"title":"a PR","state":"open","pull_request":{"merged":false}}
			]`))
			return
		}
		_, _ = w.Write([]byte(`[]`))
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
	// One request per label — a comma-joined `labels=bug,enhancement` is an
	// intersection on these APIs, which is the c-1 bug.
	if len(queries) != 2 {
		t.Fatalf("issued %d issue queries (%v), want one per label", len(queries), queries)
	}
	for _, want := range []string{"state=open", "type=issues", "labels=bug"} {
		if !strings.Contains(queries[0], want) {
			t.Errorf("query %q missing %q", queries[0], want)
		}
	}
	if strings.Contains(queries[0], "labels=bug%2Cenhancement") {
		t.Errorf("query %q comma-joins the labels — that ANDs them", queries[0])
	}
}

func TestGetIssue(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/issues/5") || r.Method != "GET" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"number":5,"title":"t","state":"closed","milestone":{"title":"v0.2"}}`))
	})
	iss, err := c.GetIssue("5")
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
	_, err := c.GetIssue("1")
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
	if _, err := c.GetIssue("1"); err != nil {
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

// --- GitLab backend ---

const gitlabTokenEnv = "MOCK_GITLAB_TOKEN"

// newGitLabTestClient points a gitlab Client at an httptest server.
func newGitLabTestClient(t *testing.T, h http.HandlerFunc) *Client {
	t.Helper()
	t.Setenv(gitlabTokenEnv, "glsecret")
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c, err := New(Config{
		Provider: "gitlab",
		URL:      "https://gitlab.example/me/proj",
		APIBase:  srv.URL,
		AuthEnv:  gitlabTokenEnv,
		Hosts:    allowingSelf(srv.URL),
	})
	if err != nil {
		t.Fatalf("New gitlab: %v", err)
	}
	return c
}

// TestNewAcceptsGitLab proves New returns a constructed backend for gitlab
// instead of the ErrNotImplemented sentinel (the github path still returns it).
func TestNewAcceptsGitLab(t *testing.T) {
	t.Setenv(gitlabTokenEnv, "x")
	c, err := New(Config{
		Provider: "gitlab",
		URL:      "https://gitlab.example/me/proj",
		APIBase:  "https://gitlab.example/api/v4",
		AuthEnv:  gitlabTokenEnv,
		Hosts:    allowingSelf("https://gitlab.example"),
	})
	if err != nil {
		t.Fatalf("gitlab should construct a backend, got %v", err)
	}
	if c == nil || !c.isGitLab() {
		t.Fatalf("expected a gitlab client, got %+v", c)
	}
}

func TestGitLabCreateIssue(t *testing.T) {
	var (
		gotPath, gotPrivTok string
		gotBody             map[string]any
	)
	c := newGitLabTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotPrivTok = r.Header.Get("PRIVATE-TOKEN")
		if r.Method == "POST" && strings.HasSuffix(r.URL.EscapedPath(), "/issues") {
			gotPath = r.URL.EscapedPath()
			b, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(b, &gotBody)
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"iid":12,"web_url":"https://gitlab.example/me/proj/-/issues/12","state":"opened"}`))
			return
		}
		t.Errorf("unexpected %s %s", r.Method, r.URL.EscapedPath())
	})

	iss, err := c.CreateIssue(IssueInput{Title: "t", Body: "desc", Labels: []string{"dross", "phase"}, Milestone: 7})
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	if iss.Number != 12 || iss.URL == "" {
		t.Errorf("iid->Number / web_url->URL mapping wrong: %+v", iss)
	}
	if !strings.Contains(gotPath, "/projects/me%2Fproj/issues") {
		t.Errorf("path not URL-encoded owner/repo: %q", gotPath)
	}
	if gotBody["description"] != "desc" {
		t.Errorf("body must use description, got %v", gotBody)
	}
	if gotBody["labels"] != "dross,phase" {
		t.Errorf("labels must be a comma-joined string, got %v", gotBody["labels"])
	}
	if gotBody["milestone_id"] != float64(7) {
		t.Errorf("milestone_id = %v", gotBody["milestone_id"])
	}
	if gotPrivTok != "glsecret" {
		t.Errorf("PRIVATE-TOKEN header = %q", gotPrivTok)
	}
}

func TestGitLabCloseIssue(t *testing.T) {
	var (
		gotPath string
		gotBody map[string]any
	)
	c := newGitLabTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "PUT" {
			t.Errorf("want PUT, got %s %s", r.Method, r.URL.EscapedPath())
			return
		}
		gotPath = r.URL.EscapedPath()
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &gotBody)
		_, _ = w.Write([]byte(`{"iid":12,"state":"closed"}`))
	})
	if err := c.CloseIssue("12"); err != nil {
		t.Fatalf("CloseIssue: %v", err)
	}
	if !strings.Contains(gotPath, "/projects/me%2Fproj/issues/12") {
		t.Errorf("close path = %q", gotPath)
	}
	if gotBody["state_event"] != "close" {
		t.Errorf("close must send state_event=close, got %v", gotBody["state_event"])
	}
}

func TestGitLabGetIssue(t *testing.T) {
	c := newGitLabTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || !strings.Contains(r.URL.EscapedPath(), "/projects/me%2Fproj/issues/5") {
			t.Errorf("unexpected %s %s", r.Method, r.URL.EscapedPath())
		}
		_, _ = w.Write([]byte(`{"iid":5,"title":"t","description":"d","state":"opened","web_url":"u","milestone":{"title":"v0.2"}}`))
	})
	iss, err := c.GetIssue("5")
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	if iss.Number != 5 || iss.Body != "d" || iss.State != "open" || iss.Milestone != "v0.2" {
		t.Errorf("iid/description/state(opened->open)/milestone mapping wrong: %+v", iss)
	}
}

func TestGitLabListIssuesMapsOpenedState(t *testing.T) {
	var gotQuery string
	c := newGitLabTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/labels") {
			_, _ = w.Write([]byte(`[{"id":1,"name":"bug"}]`))
			return
		}
		gotQuery = r.URL.RawQuery
		_, _ = w.Write([]byte(`[{"iid":1,"title":"a","state":"opened","labels":["bug"]}]`))
	})
	got, err := c.ListIssues(IssueFilter{State: "open", Labels: []string{"bug"}})
	if err != nil {
		t.Fatalf("ListIssues: %v", err)
	}
	if len(got) != 1 || got[0].Number != 1 || got[0].State != "open" || got[0].Labels[0] != "bug" {
		t.Fatalf("list mapping wrong: %+v", got)
	}
	if !strings.Contains(gotQuery, "state=opened") {
		t.Errorf("gitlab must map open->opened in the query, got %q", gotQuery)
	}
}

func TestGitLabEnsureMilestoneOmitsStateAll(t *testing.T) {
	var listQuery, gotPath string
	c := newGitLabTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			t.Errorf("milestone already exists — unexpected %s", r.Method)
			return
		}
		listQuery = r.URL.RawQuery
		gotPath = r.URL.EscapedPath()
		_, _ = w.Write([]byte(`[{"id":7,"title":"v0.6"}]`))
	})
	id, err := c.EnsureMilestone("v0.6", "d")
	if err != nil {
		t.Fatalf("EnsureMilestone: %v", err)
	}
	if id != "7" {
		t.Errorf("id = %q, want 7", id)
	}
	if strings.Contains(listQuery, "state=all") {
		t.Errorf("gitlab must not send state=all (rejected by GitLab), got %q", listQuery)
	}
	if !strings.Contains(gotPath, "/projects/me%2Fproj/milestones") {
		t.Errorf("milestone path = %q", gotPath)
	}
}

func TestGitLabBearerAuthHeader(t *testing.T) {
	t.Setenv(gitlabTokenEnv, "tok")
	var gotAuth, gotPriv string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPriv = r.Header.Get("PRIVATE-TOKEN")
		_, _ = w.Write([]byte(`{"iid":1}`))
	}))
	t.Cleanup(srv.Close)
	c, err := New(Config{
		Provider:   "gitlab",
		URL:        "https://gitlab.example/me/proj",
		APIBase:    srv.URL,
		AuthEnv:    gitlabTokenEnv,
		AuthScheme: "bearer",
		Hosts:      allowingSelf(srv.URL),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := c.GetIssue("1"); err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	if gotAuth != "Bearer tok" {
		t.Errorf("bearer scheme: Authorization = %q", gotAuth)
	}
	if gotPriv != "" {
		t.Errorf("bearer scheme: PRIVATE-TOKEN should be empty, got %q", gotPriv)
	}
}

// --- configenum-backed dispatch + basic auth (phase validator-truth) ---

// forgeSourcePath locates a file in this package for the source-scan guards.
func forgeSourcePath(t *testing.T, name string) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(dir, name)
}

// Bitbucket Cloud's credential — and any future Basic-auth forge — is
// user:token. It must be the only header sent: a PRIVATE-TOKEN riding alongside
// would present two credentials on one request.
func TestClientDoSendsBasicAuth(t *testing.T) {
	t.Setenv(tokenEnv, "secret")
	var gotAuth, gotPrivate string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPrivate = r.Header.Get("PRIVATE-TOKEN")
		_, _ = w.Write([]byte(`{"number":1}`))
	}))
	t.Cleanup(srv.Close)

	for _, scheme := range []string{"basic", " Basic ", "BASIC"} {
		c, err := New(Config{
			Provider: "gitlab", URL: "https://forge.example/me/proj", APIBase: srv.URL,
			AuthEnv: tokenEnv, AuthScheme: scheme, AuthUser: "someone@example.com",
			Hosts: allowingSelf(srv.URL),
		})
		if err != nil {
			t.Fatalf("New(scheme=%q): %v", scheme, err)
		}
		if _, err := c.GetIssue("1"); err != nil {
			t.Fatalf("GetIssue: %v", err)
		}
		want := "Basic " + base64.StdEncoding.EncodeToString([]byte("someone@example.com:secret"))
		if gotAuth != want {
			t.Errorf("scheme %q: Authorization = %q, want %q", scheme, gotAuth, want)
		}
		if gotPrivate != "" {
			t.Errorf("scheme %q: PRIVATE-TOKEN sent alongside Basic: %q", scheme, gotPrivate)
		}
	}
}

// basic without a username is half a credential. Sending Basic base64(:token)
// would 401 with nothing actionable in the message.
func TestNewRejectsBasicWithoutAuthUser(t *testing.T) {
	t.Setenv(tokenEnv, "secret")
	_, err := New(Config{
		Provider: "gitlab", URL: "https://forge.example/me/proj", APIBase: "https://x",
		AuthEnv: tokenEnv, AuthScheme: "basic", // AuthUser deliberately empty
	})
	if err == nil {
		t.Fatal("expected a construction error for basic with no auth_user")
	}
	if !strings.Contains(err.Error(), "auth_user") {
		t.Errorf("error must name the missing setting, got: %v", err)
	}
}

// A provider doctor accepts must dispatch. NewBoard has no default arm — it
// falls through to New — so a padded value that fails to match reaches New and
// errors there instead of building the right backend.
func TestNewBoardNormalisesProvider(t *testing.T) {
	t.Setenv(tokenEnv, "secret")
	for _, provider := range []string{" jira", "Jira", "JIRA\t"} {
		c, err := NewBoard(Config{
			Provider: provider, URL: "https://board.local/PROJ", APIBase: "https://jira.example",
			AuthEnv: tokenEnv, AuthUser: "me@example.com", Project: "PROJ",
			Hosts: allowingSelf("https://jira.example"),
		})
		if err != nil {
			t.Fatalf("NewBoard(%q): %v", provider, err)
		}
		if _, ok := c.(*JiraClient); !ok {
			t.Errorf("NewBoard(%q) built %T, want *JiraClient — the padded value fell through to New", provider, c)
		}
	}
	for _, provider := range []string{" youtrack", "YouTrack"} {
		c, err := NewBoard(Config{
			Provider: provider, URL: "https://board.local/PROJ", APIBase: "https://yt.example",
			AuthEnv: tokenEnv, Project: "PROJ",
			Hosts: allowingSelf("https://yt.example"),
		})
		if err != nil {
			t.Fatalf("NewBoard(%q): %v", provider, err)
		}
		if _, ok := c.(*YouTrackClient); !ok {
			t.Errorf("NewBoard(%q) built %T, want *YouTrackClient", provider, c)
		}
	}
}

func TestNewMessageListsForgeRESTProviders(t *testing.T) {
	_, err := New(Config{Provider: "perforce", URL: "https://x/y/z"})
	if err == nil {
		t.Fatal("expected error for an unsupported provider")
	}
	if !strings.Contains(err.Error(), configenum.ForgeRESTProviders.List()) {
		t.Errorf("message must derive from ForgeRESTProviders, got: %v", err)
	}
}

// milestone_mode is the one enum with a consumer in each of two files. If
// either keeps its own ToLower(TrimSpace(...)), the normalisation can drift
// away from what doctor validates.
func TestMilestoneModeConsumersUseConfigenum(t *testing.T) {
	for _, name := range []string{"jira.go", "youtrack.go"} {
		raw, err := os.ReadFile(forgeSourcePath(t, name))
		if err != nil {
			t.Fatal(err)
		}
		src := string(raw)
		if strings.Contains(src, "strings.ToLower(strings.TrimSpace(mode))") {
			t.Errorf("%s hand-rolls the milestone_mode normalisation; use configenum.Normalize", name)
		}
		if !strings.Contains(src, "configenum.Normalize(mode)") {
			t.Errorf("%s does not normalise milestone_mode through configenum", name)
		}
	}
}

// The behaviour behind the source scan: a padded mode must reach the same arm
// as the bare one.
func TestEnsureMilestoneEntityNormalisesMode(t *testing.T) {
	jira := &JiraClient{}
	if _, err := jira.EnsureMilestoneEntity(" EPIC ", "v1", ""); err == nil {
		t.Error("jira must reject epic in any casing")
	}
	yt := &YouTrackClient{}
	// " Bogus " must land in the default arm, naming the mode set.
	if _, err := yt.EnsureMilestoneEntity(" bogus ", "v1", ""); err == nil {
		t.Error("youtrack must reject an unknown mode")
	}
}

// TestFilterKnownLabels pins the partition itself. Returning everything
// reintroduces the unknown-label failure; returning nothing silently drops a
// working filter — both halves have to be exact.
func TestFilterKnownLabels(t *testing.T) {
	cases := []struct {
		name               string
		requested, known   []string
		wantKept, wantDrop []string
	}{
		{"one known, one unknown", []string{"bug", "typo"}, []string{"bug"}, []string{"bug"}, []string{"typo"}},
		{"all known", []string{"bug", "dross"}, []string{"dross", "bug"}, []string{"bug", "dross"}, nil},
		{"none known", []string{"typo"}, []string{"bug"}, nil, []string{"typo"}},
		{"empty index drops everything", []string{"bug"}, nil, nil, []string{"bug"}},
		{"no request keeps nothing", nil, []string{"bug"}, nil, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			kept, dropped := FilterKnownLabels(tc.requested, tc.known)
			if strings.Join(kept, ",") != strings.Join(tc.wantKept, ",") {
				t.Errorf("kept = %v, want %v", kept, tc.wantKept)
			}
			if strings.Join(dropped, ",") != strings.Join(tc.wantDrop, ",") {
				t.Errorf("dropped = %v, want %v", dropped, tc.wantDrop)
			}
		})
	}
}

// TestWarnDroppedLabelsNamesThem pins that the drop is audible. A silent drop
// reads as "nothing matched" — the same zero-versus-failure confusion c-2
// exists to kill.
func TestWarnDroppedLabelsNamesThem(t *testing.T) {
	t.Run("names each dropped label", func(t *testing.T) {
		warn := captureForgeStderr(t, func() { WarnDroppedLabels("youtrack", []string{"typo", "gone"}) })
		for _, want := range []string{"typo", "gone"} {
			if !strings.Contains(warn, want) {
				t.Errorf("stderr = %q, want it to name %q", warn, want)
			}
		}
	})

	t.Run("silent when nothing was dropped", func(t *testing.T) {
		if warn := captureForgeStderr(t, func() { WarnDroppedLabels("youtrack", nil) }); warn != "" {
			t.Errorf("stderr = %q, want silence", warn)
		}
	})
}

// captureForgeStderr runs fn with os.Stderr redirected and returns what it wrote.
func captureForgeStderr(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stderr
	rd, wr, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stderr = wr
	fn()
	_ = wr.Close()
	os.Stderr = old
	out, _ := io.ReadAll(rd)
	return string(out)
}

// TestRESTListIssuesOrsLabels pins the fan-out on the shared REST backend.
// forgejo, gitea and gitlab all treat a comma-joined `labels=` param as an
// intersection, so the old single request matched only issues carrying every
// label — c-1's bug in its third form.
func TestRESTListIssuesOrsLabels(t *testing.T) {
	t.Run("forgejo unions one request per label", func(t *testing.T) {
		var queried []string
		c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
			if strings.HasSuffix(r.URL.Path, "/labels") {
				_, _ = w.Write([]byte(`[{"id":1,"name":"bug"},{"id":2,"name":"enhancement"}]`))
				return
			}
			label := r.URL.Query().Get("labels")
			queried = append(queried, label)
			switch label {
			case "bug":
				_, _ = w.Write([]byte(`[{"number":1,"title":"a bug","state":"open"}]`))
			case "enhancement":
				_, _ = w.Write([]byte(`[{"number":2,"title":"an idea","state":"open"}]`))
			default:
				t.Errorf("unexpected labels param %q", label)
				_, _ = w.Write([]byte(`[]`))
			}
		})
		got, err := c.ListIssues(IssueFilter{Labels: []string{"bug", "enhancement"}})
		if err != nil {
			t.Fatalf("ListIssues: %v", err)
		}
		if len(queried) != 2 {
			t.Errorf("issued %d queries (%v), want one per label", len(queried), queried)
		}
		if len(got) != 2 || got[0].Number != 1 || got[1].Number != 2 {
			t.Errorf("union = %+v, want both issues", got)
		}
	})

	t.Run("an issue under both labels appears once", func(t *testing.T) {
		c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
			if strings.HasSuffix(r.URL.Path, "/labels") {
				_, _ = w.Write([]byte(`[{"id":1,"name":"bug"},{"id":2,"name":"enhancement"}]`))
				return
			}
			_, _ = w.Write([]byte(`[{"number":5,"title":"both","state":"open"}]`))
		})
		got, err := c.ListIssues(IssueFilter{Labels: []string{"bug", "enhancement"}})
		if err != nil {
			t.Fatalf("ListIssues: %v", err)
		}
		if len(got) != 1 || got[0].Number != 5 {
			t.Errorf("union = %+v, want #5 exactly once", got)
		}
	})

	// GitLab is asserted against the fake only — this backend is wired but not
	// dogfooded, so no live-server verification is claimed for it.
	t.Run("gitlab returns issues carrying either label", func(t *testing.T) {
		c := newGitLabTestClient(t, func(w http.ResponseWriter, r *http.Request) {
			if strings.HasSuffix(r.URL.Path, "/labels") {
				_, _ = w.Write([]byte(`[{"id":1,"name":"bug"},{"id":2,"name":"enhancement"}]`))
				return
			}
			switch r.URL.Query().Get("labels") {
			case "bug":
				_, _ = w.Write([]byte(`[{"iid":1,"title":"a","state":"opened"}]`))
			default:
				_, _ = w.Write([]byte(`[{"iid":2,"title":"b","state":"opened"}]`))
			}
		})
		got, err := c.ListIssues(IssueFilter{Labels: []string{"bug", "enhancement"}})
		if err != nil {
			t.Fatalf("ListIssues: %v", err)
		}
		if len(got) != 2 {
			t.Errorf("union = %+v, want an issue for each label", got)
		}
	})
}

// TestRESTListIssuesDropsUnknownLabels pins the same label-index gate the
// YouTrack/Jira/GitHub backends carry, fed by the existing loadLabels index.
func TestRESTListIssuesDropsUnknownLabels(t *testing.T) {
	t.Run("unknown label dropped and named", func(t *testing.T) {
		var queried []string
		c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
			if strings.HasSuffix(r.URL.Path, "/labels") {
				_, _ = w.Write([]byte(`[{"id":1,"name":"bug"}]`))
				return
			}
			queried = append(queried, r.URL.Query().Get("labels"))
			_, _ = w.Write([]byte(`[{"number":1,"title":"a","state":"open"}]`))
		})

		warn := captureForgeStderr(t, func() {
			if _, err := c.ListIssues(IssueFilter{Labels: []string{"bug", "typo"}}); err != nil {
				t.Fatalf("ListIssues: %v", err)
			}
		})
		if len(queried) != 1 || queried[0] != "bug" {
			t.Errorf("queried %v, want only the known label", queried)
		}
		if !strings.Contains(warn, "typo") {
			t.Errorf("stderr = %q, want the dropped label named", warn)
		}
	})

	t.Run("a failing label index refuses the query", func(t *testing.T) {
		var issueCalls int
		c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
			if strings.HasSuffix(r.URL.Path, "/labels") {
				w.WriteHeader(500)
				return
			}
			issueCalls++
			_, _ = w.Write([]byte(`[]`))
		})
		if _, err := c.ListIssues(IssueFilter{Labels: []string{"bug"}}); err == nil {
			t.Fatal("a 500 from /labels returned no error")
		}
		if issueCalls != 0 {
			t.Errorf("issued %d issue queries after the index failed, want 0", issueCalls)
		}
	})

	t.Run("an empty filter reads no label index at all", func(t *testing.T) {
		var labelCalls int
		c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
			if strings.HasSuffix(r.URL.Path, "/labels") {
				labelCalls++
			}
			_, _ = w.Write([]byte(`[]`))
		})
		if _, err := c.ListIssues(IssueFilter{}); err != nil {
			t.Fatalf("ListIssues: %v", err)
		}
		if labelCalls != 0 {
			t.Errorf("unlabelled list read the label index %d times — a blip must not fail `dross watch`", labelCalls)
		}
	})
}
