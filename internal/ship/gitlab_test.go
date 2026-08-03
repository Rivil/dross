package ship

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestGitLabOpenPRsTargetingUsesIID proves BasePR.Number comes from iid, not
// GitLab's internal numeric id — reading "id" would point completion-time
// dependent checks at the wrong MR.
func TestGitLabOpenPRsTargetingUsesIID(t *testing.T) {
	t.Setenv("MOCK_GITLAB_TOKEN", "secret")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"iid":7,"id":901,"title":"t","web_url":"u","source_branch":"s"}]`))
	}))
	t.Cleanup(server.Close)

	prs, err := OpenPRsTargeting(OpenOpts{
		Provider: "gitlab", URL: "https://gitlab.example/me/p", APIBase: server.URL,
		AuthEnv: "MOCK_GITLAB_TOKEN",
	}, "main")
	if err != nil {
		t.Fatalf("OpenPRsTargeting: %v", err)
	}
	if len(prs) != 1 || prs[0].Number != 7 {
		t.Errorf("got %+v, want Number 7 (from iid, not id)", prs)
	}
}

// TestGitLabOpenPRsTargetingFieldMapping proves web_url->URL and
// source_branch->HeadRefName.
func TestGitLabOpenPRsTargetingFieldMapping(t *testing.T) {
	t.Setenv("MOCK_GITLAB_TOKEN", "secret")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"iid":1,"title":"t","web_url":"https://gitlab/me/p/-/merge_requests/1","source_branch":"phase/x"}]`))
	}))
	t.Cleanup(server.Close)

	prs, err := OpenPRsTargeting(OpenOpts{
		Provider: "gitlab", URL: "https://gitlab.example/me/p", APIBase: server.URL,
		AuthEnv: "MOCK_GITLAB_TOKEN",
	}, "main")
	if err != nil {
		t.Fatalf("OpenPRsTargeting: %v", err)
	}
	if len(prs) != 1 {
		t.Fatalf("got %d PRs, want 1", len(prs))
	}
	if prs[0].URL != "https://gitlab/me/p/-/merge_requests/1" {
		t.Errorf("URL = %q", prs[0].URL)
	}
	if prs[0].HeadRefName != "phase/x" {
		t.Errorf("HeadRefName = %q", prs[0].HeadRefName)
	}
}

// TestGitLabOpenPRsTargetingQueryAndAuth asserts the query carries
// state=opened and target_branch=<base>, plus the scheme-appropriate auth
// header.
func TestGitLabOpenPRsTargetingQueryAndAuth(t *testing.T) {
	t.Setenv("MOCK_GITLAB_TOKEN", "secret")
	var gotQuery, gotAuth, gotPrivate string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		gotAuth = r.Header.Get("Authorization")
		gotPrivate = r.Header.Get("PRIVATE-TOKEN")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
	}))
	t.Cleanup(server.Close)

	if _, err := OpenPRsTargeting(OpenOpts{
		Provider: "gitlab", URL: "https://gitlab.example/me/p", APIBase: server.URL,
		AuthEnv: "MOCK_GITLAB_TOKEN",
	}, "milestone/v1.2"); err != nil {
		t.Fatalf("OpenPRsTargeting: %v", err)
	}
	if !strings.Contains(gotQuery, "state=opened") {
		t.Errorf("query missing state=opened: %q", gotQuery)
	}
	if !strings.Contains(gotQuery, "target_branch=milestone%2Fv1.2") {
		t.Errorf("query missing target_branch=milestone/v1.2: %q", gotQuery)
	}
	if gotPrivate != "secret" {
		t.Errorf("PRIVATE-TOKEN = %q, want secret", gotPrivate)
	}
	if gotAuth != "" {
		t.Errorf("Authorization should be empty for the default scheme, got %q", gotAuth)
	}

	// Bearer sub-case: Authorization: Bearer, no PRIVATE-TOKEN.
	if _, err := OpenPRsTargeting(OpenOpts{
		Provider: "gitlab", URL: "https://gitlab.example/me/p", APIBase: server.URL,
		AuthEnv: "MOCK_GITLAB_TOKEN", AuthScheme: "bearer",
	}, "milestone/v1.2"); err != nil {
		t.Fatalf("OpenPRsTargeting (bearer): %v", err)
	}
	if gotAuth != "Bearer secret" {
		t.Errorf("Authorization = %q, want Bearer secret", gotAuth)
	}
	if gotPrivate != "" {
		t.Errorf("PRIVATE-TOKEN should be empty under bearer, got %q", gotPrivate)
	}
}

// TestGitLabOpenMRsPaginates proves a full page followed by a short page both
// contribute their MRs, and every page's request carries target_branch=<base>.
// GitLab defaults to 20/page, so a first-page-only impl would let
// `milestone prune` delete a branch with live dependents still on page 2.
func TestGitLabOpenMRsPaginates(t *testing.T) {
	t.Setenv("MOCK_GITLAB_TOKEN", "secret")

	fullPage := make([]map[string]any, 100)
	for i := range fullPage {
		fullPage[i] = map[string]any{"iid": i + 1, "title": "t", "web_url": "u", "source_branch": "s"}
	}
	fullPageJSON, _ := json.Marshal(fullPage)
	shortPageJSON := []byte(`[{"iid":200,"title":"t","web_url":"u","source_branch":"s"}]`)

	var pages []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.RawQuery, "target_branch=main") {
			t.Errorf("page request missing target_branch=main: %q", r.URL.RawQuery)
		}
		pages = append(pages, r.URL.Query().Get("page"))
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("page") == "1" {
			_, _ = w.Write(fullPageJSON)
			return
		}
		_, _ = w.Write(shortPageJSON)
	}))
	t.Cleanup(server.Close)

	prs, err := OpenPRsTargeting(OpenOpts{
		Provider: "gitlab", URL: "https://gitlab.example/me/p", APIBase: server.URL,
		AuthEnv: "MOCK_GITLAB_TOKEN",
	}, "main")
	if err != nil {
		t.Fatalf("OpenPRsTargeting: %v", err)
	}
	if len(prs) != 101 {
		t.Fatalf("got %d PRs, want 101 (100 from page 1 + 1 from page 2)", len(prs))
	}
	if len(pages) != 2 {
		t.Fatalf("got %d page requests, want 2", len(pages))
	}
}

// TestGitLabOpenPRsTargetingHTTP500IsError proves a 500 on any page yields a
// non-nil error and a nil slice, never an empty or partial slice — an empty
// slice reads as "no dependents" and authorizes an irreversible branch delete.
func TestGitLabOpenPRsTargetingHTTP500IsError(t *testing.T) {
	t.Setenv("MOCK_GITLAB_TOKEN", "secret")

	fullPage := make([]map[string]any, 100)
	for i := range fullPage {
		fullPage[i] = map[string]any{"iid": i + 1, "title": "t", "web_url": "u", "source_branch": "s"}
	}
	fullPageJSON, _ := json.Marshal(fullPage)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page") == "1" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(fullPageJSON)
			return
		}
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	t.Cleanup(server.Close)

	prs, err := OpenPRsTargeting(OpenOpts{
		Provider: "gitlab", URL: "https://gitlab.example/me/p", APIBase: server.URL,
		AuthEnv: "MOCK_GITLAB_TOKEN",
	}, "main")
	if err == nil {
		t.Fatalf("expected an error from the page-2 500, got %+v", prs)
	}
	if prs != nil {
		t.Errorf("expected no PRs alongside the error, got %+v", prs)
	}
}

// TestGitLabOpenPRsTargetingMissingToken proves a missing $AuthEnv value
// fails before any request, with an error mentioning `dross env set`.
func TestGitLabOpenPRsTargetingMissingToken(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.Write([]byte(`[]`))
	}))
	t.Cleanup(server.Close)

	_, err := OpenPRsTargeting(OpenOpts{
		Provider: "gitlab", URL: "https://gitlab.example/me/p", APIBase: server.URL,
		AuthEnv: "MOCK_GITLAB_TOKEN_UNSET",
	}, "main")
	if err == nil || !strings.Contains(err.Error(), "dross env set") {
		t.Fatalf("expected a `dross env set` error, got: %v", err)
	}
	if requests != 0 {
		t.Errorf("%d request(s) made before the token check", requests)
	}
}
