package ship

import (
	"encoding/json"
	"fmt"
	"github.com/Rivil/dross/internal/hostallow"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestGitLabPRStatusMerged proves a merged MR reports Merged true and its
// target_branch as BaseRef, replacing today's sentinel.
func TestGitLabPRStatusMerged(t *testing.T) {
	t.Setenv("MOCK_GITLAB_TOKEN", "secret")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"state":"merged","target_branch":"main"}`))
	}))
	t.Cleanup(server.Close)

	status, err := GetPRStatus(OpenOpts{
		Provider: "gitlab", URL: "https://gitlab.example/me/p", APIBase: server.URL, Hosts: hostallow.Derive(server.URL, nil),
		AuthEnv: "MOCK_GITLAB_TOKEN", PRNumber: 5,
	})
	if err != nil {
		t.Fatalf("GetPRStatus: %v", err)
	}
	if !status.Merged || status.BaseRef != "main" {
		t.Errorf("got %+v, want {Merged:true BaseRef:main}", status)
	}
}

// TestGitLabPRStatusClosedIsNotMerged proves both "closed" (declined without
// landing) and "locked" report Merged false with no error. A `state !=
// "opened"` impl would pass the "opened" test but this one catches it.
func TestGitLabPRStatusClosedIsNotMerged(t *testing.T) {
	for _, state := range []string{"closed", "locked"} {
		t.Run(state, func(t *testing.T) {
			t.Setenv("MOCK_GITLAB_TOKEN", "secret")
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(fmt.Sprintf(`{"state":%q,"target_branch":"main"}`, state)))
			}))
			t.Cleanup(server.Close)

			status, err := GetPRStatus(OpenOpts{
				Provider: "gitlab", URL: "https://gitlab.example/me/p", APIBase: server.URL, Hosts: hostallow.Derive(server.URL, nil),
				AuthEnv: "MOCK_GITLAB_TOKEN", PRNumber: 5,
			})
			if err != nil {
				t.Fatalf("GetPRStatus: %v", err)
			}
			if status.Merged {
				t.Errorf("state %q must report Merged false", state)
			}
		})
	}
}

// TestGitLabPRStatusOpenedIsNotMerged proves an open MR reports Merged false.
func TestGitLabPRStatusOpenedIsNotMerged(t *testing.T) {
	t.Setenv("MOCK_GITLAB_TOKEN", "secret")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"state":"opened","target_branch":"main"}`))
	}))
	t.Cleanup(server.Close)

	status, err := GetPRStatus(OpenOpts{
		Provider: "gitlab", URL: "https://gitlab.example/me/p", APIBase: server.URL, Hosts: hostallow.Derive(server.URL, nil),
		AuthEnv: "MOCK_GITLAB_TOKEN", PRNumber: 5,
	})
	if err != nil {
		t.Fatalf("GetPRStatus: %v", err)
	}
	if status.Merged {
		t.Error("an opened MR must not report Merged true")
	}
}

// TestGitLabPRStatusReportsTargetBranch proves BaseRef comes straight from
// target_branch — this is the exact field a later retarget check consumes.
func TestGitLabPRStatusReportsTargetBranch(t *testing.T) {
	t.Setenv("MOCK_GITLAB_TOKEN", "secret")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"state":"opened","target_branch":"milestone/v1.2"}`))
	}))
	t.Cleanup(server.Close)

	status, err := GetPRStatus(OpenOpts{
		Provider: "gitlab", URL: "https://gitlab.example/me/p", APIBase: server.URL, Hosts: hostallow.Derive(server.URL, nil),
		AuthEnv: "MOCK_GITLAB_TOKEN", PRNumber: 5,
	})
	if err != nil {
		t.Fatalf("GetPRStatus: %v", err)
	}
	if status.BaseRef != "milestone/v1.2" {
		t.Errorf("BaseRef = %q, want milestone/v1.2", status.BaseRef)
	}
}

// TestGitLabPRStatusEndpoint asserts the request path, both by derived
// owner/repo and by a numeric ProjectID override.
func TestGitLabPRStatusEndpoint(t *testing.T) {
	t.Setenv("MOCK_GITLAB_TOKEN", "secret")
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.EscapedPath()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"state":"opened","target_branch":"main"}`))
	}))
	t.Cleanup(server.Close)

	if _, err := GetPRStatus(OpenOpts{
		Provider: "gitlab", URL: "https://gitlab.example/owner/repo", APIBase: server.URL, Hosts: hostallow.Derive(server.URL, nil),
		AuthEnv: "MOCK_GITLAB_TOKEN", PRNumber: 5,
	}); err != nil {
		t.Fatalf("GetPRStatus: %v", err)
	}
	if want := "/projects/owner%2Frepo/merge_requests/5"; gotPath != want {
		t.Errorf("path = %q, want %q", gotPath, want)
	}

	if _, err := GetPRStatus(OpenOpts{
		Provider: "gitlab", URL: "https://gitlab.example/owner/repo", APIBase: server.URL, Hosts: hostallow.Derive(server.URL, nil),
		AuthEnv: "MOCK_GITLAB_TOKEN", ProjectID: "123", PRNumber: 5,
	}); err != nil {
		t.Fatalf("GetPRStatus (ProjectID): %v", err)
	}
	if want := "/projects/123/merge_requests/5"; gotPath != want {
		t.Errorf("path = %q, want %q", gotPath, want)
	}
}

// TestGitLabPRStatusAuthHeader proves PRIVATE-TOKEN by default and Bearer
// when AuthScheme is "bearer".
func TestGitLabPRStatusAuthHeader(t *testing.T) {
	t.Setenv("MOCK_GITLAB_TOKEN", "secret")
	var gotAuth, gotPrivate string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPrivate = r.Header.Get("PRIVATE-TOKEN")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"state":"opened","target_branch":"main"}`))
	}))
	t.Cleanup(server.Close)

	if _, err := GetPRStatus(OpenOpts{
		Provider: "gitlab", URL: "https://gitlab.example/o/r", APIBase: server.URL, Hosts: hostallow.Derive(server.URL, nil),
		AuthEnv: "MOCK_GITLAB_TOKEN", PRNumber: 5,
	}); err != nil {
		t.Fatalf("GetPRStatus: %v", err)
	}
	if gotPrivate != "secret" || gotAuth != "" {
		t.Errorf("default scheme: PRIVATE-TOKEN=%q Authorization=%q", gotPrivate, gotAuth)
	}

	if _, err := GetPRStatus(OpenOpts{
		Provider: "gitlab", URL: "https://gitlab.example/o/r", APIBase: server.URL, Hosts: hostallow.Derive(server.URL, nil),
		AuthEnv: "MOCK_GITLAB_TOKEN", AuthScheme: "bearer", PRNumber: 5,
	}); err != nil {
		t.Fatalf("GetPRStatus (bearer): %v", err)
	}
	if gotAuth != "Bearer secret" || gotPrivate != "" {
		t.Errorf("bearer scheme: Authorization=%q PRIVATE-TOKEN=%q", gotAuth, gotPrivate)
	}
}

// TestGitLabPRStatusHTTP404IsError proves a 404 yields an error rather than
// (false, nil) — mergeGate must announce and fall back, not read a lookup
// failure as "not merged".
func TestGitLabPRStatusHTTP404IsError(t *testing.T) {
	t.Setenv("MOCK_GITLAB_TOKEN", "secret")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	t.Cleanup(server.Close)

	status, err := GetPRStatus(OpenOpts{
		Provider: "gitlab", URL: "https://gitlab.example/o/r", APIBase: server.URL, Hosts: hostallow.Derive(server.URL, nil),
		AuthEnv: "MOCK_GITLAB_TOKEN", PRNumber: 5,
	})
	if err == nil {
		t.Fatalf("expected an error for a 404, got %+v", status)
	}
}

// TestGitLabPRStatusNeedsPRNumber proves a missing PR number is an error
// before any request is made.
func TestGitLabPRStatusNeedsPRNumber(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
	}))
	t.Cleanup(server.Close)

	if _, err := GetPRStatus(OpenOpts{
		Provider: "gitlab", URL: "https://gitlab.example/o/r", APIBase: server.URL, Hosts: hostallow.Derive(server.URL, nil),
		AuthEnv: "MOCK_GITLAB_TOKEN",
	}); err == nil {
		t.Error("expected an error when PRNumber is unset")
	}
	if requests != 0 {
		t.Errorf("%d request(s) made before the PR-number check", requests)
	}
}

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
		Provider: "gitlab", URL: "https://gitlab.example/me/p", APIBase: server.URL, Hosts: hostallow.Derive(server.URL, nil),
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
		Provider: "gitlab", URL: "https://gitlab.example/me/p", APIBase: server.URL, Hosts: hostallow.Derive(server.URL, nil),
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
		Provider: "gitlab", URL: "https://gitlab.example/me/p", APIBase: server.URL, Hosts: hostallow.Derive(server.URL, nil),
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
		Provider: "gitlab", URL: "https://gitlab.example/me/p", APIBase: server.URL, Hosts: hostallow.Derive(server.URL, nil),
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
		Provider: "gitlab", URL: "https://gitlab.example/me/p", APIBase: server.URL, Hosts: hostallow.Derive(server.URL, nil),
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
		Provider: "gitlab", URL: "https://gitlab.example/me/p", APIBase: server.URL, Hosts: hostallow.Derive(server.URL, nil),
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
		Provider: "gitlab", URL: "https://gitlab.example/me/p", APIBase: server.URL, Hosts: hostallow.Derive(server.URL, nil),
		AuthEnv: "MOCK_GITLAB_TOKEN_UNSET",
	}, "main")
	if err == nil || !strings.Contains(err.Error(), "dross env set") {
		t.Fatalf("expected a `dross env set` error, got: %v", err)
	}
	if requests != 0 {
		t.Errorf("%d request(s) made before the token check", requests)
	}
}
