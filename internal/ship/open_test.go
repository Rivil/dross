package ship

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSplitOwnerRepo(t *testing.T) {
	tests := []struct {
		url         string
		owner, repo string
		wantErr     bool
	}{
		{"https://github.com/Rivil/dross", "Rivil", "dross", false},
		{"https://github.com/Rivil/dross.git", "Rivil", "dross", false},
		{"https://forge.example/me/p", "me", "p", false},
		{"https://github.com/", "", "", true},
		{"not-a-url", "", "", true},
	}
	for _, tc := range tests {
		owner, repo, err := splitOwnerRepo(tc.url)
		if tc.wantErr && err == nil {
			t.Errorf("%q: expected error", tc.url)
		}
		if !tc.wantErr && err != nil {
			t.Errorf("%q: %v", tc.url, err)
		}
		if owner != tc.owner || repo != tc.repo {
			t.Errorf("%q: owner=%q repo=%q want %q/%q", tc.url, owner, repo, tc.owner, tc.repo)
		}
	}
}

func TestParsePRNumber(t *testing.T) {
	tests := map[string]int{
		"https://github.com/o/r/pull/123": 123,
		"https://forge/me/p/pulls/7":      7,
		"":                                0,
		"not-a-url":                       0,
		"https://x/y/pull/abc":            0,
	}
	for url, want := range tests {
		if got := parsePRNumber(url); got != want {
			t.Errorf("parsePRNumber(%q) = %d want %d", url, got, want)
		}
	}
}

func TestOpenForgejoPRHappyPath(t *testing.T) {
	t.Setenv("MOCK_FORGEJO_TOKEN", "secret")

	var (
		pullsCalled, reviewersCalled bool
		gotPullsBody                 map[string]any
		gotReviewersBody             map[string]any
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "token secret" {
			t.Errorf("auth header: %q", got)
		}
		body, _ := io.ReadAll(r.Body)
		switch {
		case strings.HasSuffix(r.URL.Path, "/pulls") && r.Method == "POST":
			pullsCalled = true
			_ = json.Unmarshal(body, &gotPullsBody)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"number":42,"html_url":"https://forge.example/me/p/pulls/42"}`))
		case strings.HasSuffix(r.URL.Path, "/requested_reviewers") && r.Method == "POST":
			reviewersCalled = true
			_ = json.Unmarshal(body, &gotReviewersBody)
			w.WriteHeader(http.StatusCreated)
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	t.Cleanup(server.Close)

	res, err := OpenPR(OpenOpts{
		Provider:   "forgejo",
		URL:        "https://forge.example/me/p",
		APIBase:    server.URL,
		AuthEnv:    "MOCK_FORGEJO_TOKEN",
		HeadBranch: "pr/01-x",
		BaseBranch: "main",
		Title:      "phase 01-x: tagging",
		Body:       "## body",
		Reviewers:  []string{"alice", "bob"},
	})
	if err != nil {
		t.Fatalf("OpenPR: %v", err)
	}
	if !pullsCalled {
		t.Error("/pulls endpoint not called")
	}
	if !reviewersCalled {
		t.Error("/requested_reviewers endpoint not called")
	}
	if res.Number != 42 || res.URL != "https://forge.example/me/p/pulls/42" {
		t.Errorf("OpenResult: %+v", res)
	}
	if gotPullsBody["title"] != "phase 01-x: tagging" {
		t.Errorf("pulls body title: %+v", gotPullsBody)
	}
	if gotPullsBody["head"] != "pr/01-x" || gotPullsBody["base"] != "main" {
		t.Errorf("pulls head/base: %+v", gotPullsBody)
	}
	revs, _ := gotReviewersBody["reviewers"].([]any)
	if len(revs) != 2 || revs[0] != "alice" || revs[1] != "bob" {
		t.Errorf("reviewers: %+v", gotReviewersBody)
	}
}

func TestOpenForgejoPRDraftPrefix(t *testing.T) {
	t.Setenv("MOCK_FORGEJO_TOKEN", "x")
	var got map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &got)
		_, _ = w.Write([]byte(`{"number":1,"html_url":"u"}`))
	}))
	t.Cleanup(server.Close)

	_, _ = OpenPR(OpenOpts{
		Provider: "forgejo", URL: "https://x/o/r", APIBase: server.URL,
		AuthEnv: "MOCK_FORGEJO_TOKEN", HeadBranch: "pr", BaseBranch: "main",
		Title: "real title", Draft: true,
	})
	if got["title"] != "Draft: real title" {
		t.Errorf("draft prefix not applied: %v", got["title"])
	}
}

func TestOpenForgejoPRMissingToken(t *testing.T) {
	_, err := OpenPR(OpenOpts{
		Provider: "forgejo", URL: "https://x/o/r", APIBase: "https://api",
		AuthEnv: "DROSS_TEST_DEFINITELY_UNSET_TOKEN", HeadBranch: "pr", BaseBranch: "main",
		Title: "x",
	})
	if err == nil {
		t.Error("expected error for missing token env")
	}
	if !strings.Contains(err.Error(), "is not set") {
		t.Errorf("error should mention token not set: %v", err)
	}
}

func TestOpenForgejoPRReviewerFailureNonFatal(t *testing.T) {
	t.Setenv("MOCK_FORGEJO_TOKEN", "x")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/pulls") && r.Method == "POST":
			_, _ = w.Write([]byte(`{"number":7,"html_url":"https://forge/o/r/pulls/7"}`))
		case strings.HasSuffix(r.URL.Path, "/requested_reviewers"):
			http.Error(w, "user not found", http.StatusUnprocessableEntity)
		}
	}))
	t.Cleanup(server.Close)

	res, err := OpenPR(OpenOpts{
		Provider: "forgejo", URL: "https://forge/o/r", APIBase: server.URL,
		AuthEnv: "MOCK_FORGEJO_TOKEN", HeadBranch: "pr", BaseBranch: "main",
		Title: "x", Reviewers: []string{"ghost"},
	})
	if res == nil || res.Number != 7 {
		t.Errorf("PR should be reported even when reviewer add fails: res=%+v err=%v", res, err)
	}
	if err == nil {
		t.Error("expected non-fatal error mentioning reviewer failure")
	}
}

func TestOpenPRRejectsUnknownProvider(t *testing.T) {
	_, err := OpenPR(OpenOpts{Provider: "perforce", URL: "x"})
	if err == nil {
		t.Error("expected error for unknown provider")
	}
}

func TestGitLabProjectRef(t *testing.T) {
	if got := gitlabProjectRef("me", "p", 0); got != "me%2Fp" {
		t.Errorf("derive path: got %q want %q", got, "me%2Fp")
	}
	if got := gitlabProjectRef("me", "p", 123); got != "123" {
		t.Errorf("numeric project_id should win: got %q want %q", got, "123")
	}
}

func TestGitLabAuthHeader(t *testing.T) {
	for _, scheme := range []string{"", "private-token"} {
		req, _ := http.NewRequest("GET", "https://x", nil)
		gitlabAuthHeader(req, scheme, "tok")
		if got := req.Header.Get("PRIVATE-TOKEN"); got != "tok" {
			t.Errorf("scheme %q: PRIVATE-TOKEN got %q want %q", scheme, got, "tok")
		}
		if got := req.Header.Get("Authorization"); got != "" {
			t.Errorf("scheme %q: Authorization should be empty, got %q", scheme, got)
		}
	}
	req, _ := http.NewRequest("GET", "https://x", nil)
	gitlabAuthHeader(req, "bearer", "tok")
	if got := req.Header.Get("Authorization"); got != "Bearer tok" {
		t.Errorf("bearer: Authorization got %q want %q", got, "Bearer tok")
	}
	if got := req.Header.Get("PRIVATE-TOKEN"); got != "" {
		t.Errorf("bearer: PRIVATE-TOKEN should be empty, got %q", got)
	}
}

func TestOpenGitLabPRHappyPath(t *testing.T) {
	t.Setenv("MOCK_GITLAB_TOKEN", "secret")

	var (
		createCalled, usersCalled, assignCalled bool
		gotCreateBody, gotAssignBody            map[string]any
		gotCreatePath                           string
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("PRIVATE-TOKEN"); got != "secret" {
			t.Errorf("auth header: PRIVATE-TOKEN=%q", got)
		}
		body, _ := io.ReadAll(r.Body)
		switch {
		case r.Method == "POST" && strings.HasSuffix(r.URL.EscapedPath(), "/merge_requests"):
			createCalled = true
			gotCreatePath = r.URL.EscapedPath()
			_ = json.Unmarshal(body, &gotCreateBody)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"iid":42,"web_url":"https://gitlab.example/me/p/-/merge_requests/42"}`))
		case r.Method == "GET" && strings.Contains(r.URL.Path, "/users"):
			usersCalled = true
			_, _ = w.Write([]byte(`[{"id":7}]`))
		case r.Method == "PUT" && strings.Contains(r.URL.EscapedPath(), "/merge_requests/42"):
			assignCalled = true
			_ = json.Unmarshal(body, &gotAssignBody)
			_, _ = w.Write([]byte(`{"iid":42}`))
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.EscapedPath())
		}
	}))
	t.Cleanup(server.Close)

	res, err := OpenPR(OpenOpts{
		Provider:   "gitlab",
		URL:        "https://gitlab.example/me/p",
		APIBase:    server.URL,
		AuthEnv:    "MOCK_GITLAB_TOKEN",
		HeadBranch: "phase/x",
		BaseBranch: "main",
		Title:      "phase x: feature",
		Body:       "## body",
		Reviewers:  []string{"alice"},
	})
	if err != nil {
		t.Fatalf("OpenPR: %v", err)
	}
	if !createCalled || !usersCalled || !assignCalled {
		t.Errorf("endpoints: create=%v users=%v assign=%v", createCalled, usersCalled, assignCalled)
	}
	if !strings.Contains(gotCreatePath, "/projects/me%2Fp/merge_requests") {
		t.Errorf("create path not URL-encoded owner/repo: %q", gotCreatePath)
	}
	if gotCreateBody["source_branch"] != "phase/x" || gotCreateBody["target_branch"] != "main" {
		t.Errorf("create body uses source_branch/target_branch, got: %+v", gotCreateBody)
	}
	if gotCreateBody["title"] != "phase x: feature" || gotCreateBody["description"] != "## body" {
		t.Errorf("create body title/description: %+v", gotCreateBody)
	}
	if res.Number != 42 || res.URL != "https://gitlab.example/me/p/-/merge_requests/42" {
		t.Errorf("OpenResult (iid->Number, web_url->URL): %+v", res)
	}
	ids, _ := gotAssignBody["reviewer_ids"].([]any)
	if len(ids) != 1 || ids[0] != float64(7) {
		t.Errorf("reviewer_ids: %+v", gotAssignBody)
	}
}

func TestOpenGitLabPRDraftPrefix(t *testing.T) {
	t.Setenv("MOCK_GITLAB_TOKEN", "x")
	var got map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &got)
		_, _ = w.Write([]byte(`{"iid":1,"web_url":"u"}`))
	}))
	t.Cleanup(server.Close)

	_, _ = OpenPR(OpenOpts{
		Provider: "gitlab", URL: "https://x/o/r", APIBase: server.URL,
		AuthEnv: "MOCK_GITLAB_TOKEN", HeadBranch: "phase/x", BaseBranch: "main",
		Title: "real title", Draft: true,
	})
	if got["title"] != "Draft: real title" {
		t.Errorf("draft prefix not applied: %v", got["title"])
	}
}

func TestOpenGitLabPRReviewerFailureNonFatal(t *testing.T) {
	t.Setenv("MOCK_GITLAB_TOKEN", "x")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "POST" && strings.HasSuffix(r.URL.EscapedPath(), "/merge_requests"):
			_, _ = w.Write([]byte(`{"iid":7,"web_url":"https://gitlab/o/r/-/merge_requests/7"}`))
		case strings.Contains(r.URL.Path, "/users"):
			http.Error(w, "boom", http.StatusInternalServerError)
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.EscapedPath())
		}
	}))
	t.Cleanup(server.Close)

	res, err := OpenPR(OpenOpts{
		Provider: "gitlab", URL: "https://gitlab/o/r", APIBase: server.URL,
		AuthEnv: "MOCK_GITLAB_TOKEN", HeadBranch: "phase/x", BaseBranch: "main",
		Title: "x", Reviewers: []string{"ghost"},
	})
	if res == nil || res.Number != 7 {
		t.Errorf("MR should be reported even when reviewer lookup fails: res=%+v err=%v", res, err)
	}
	if err == nil {
		t.Error("expected non-fatal error mentioning reviewer failure")
	}
}
