package ship

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestForgejoPRStatusUsesMergedFlag proves merged comes from the "merged"
// boolean, never derived from state — Gitea reports a merged PR as state
// "closed", so an impl reading state alone would call a landed PR unmerged.
func TestForgejoPRStatusUsesMergedFlag(t *testing.T) {
	t.Setenv("MOCK_FORGEJO_TOKEN", "secret")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"merged":true,"state":"closed","base":{"ref":"main"}}`))
	}))
	t.Cleanup(server.Close)

	status, err := GetPRStatus(OpenOpts{
		Provider: "forgejo", URL: "https://forge.example/me/p", APIBase: server.URL,
		AuthEnv: "MOCK_FORGEJO_TOKEN", PRNumber: 5,
	})
	if err != nil {
		t.Fatalf("GetPRStatus: %v", err)
	}
	if !status.Merged || status.BaseRef != "main" {
		t.Errorf("got %+v, want {Merged:true BaseRef:main}", status)
	}
}

// TestForgejoPRStatusClosedUnmergedIsNotMerged proves a declined PR
// (merged=false, state=closed) reports Merged false, so completion refuses
// rather than false-completing discarded work.
func TestForgejoPRStatusClosedUnmergedIsNotMerged(t *testing.T) {
	t.Setenv("MOCK_FORGEJO_TOKEN", "secret")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"merged":false,"state":"closed","base":{"ref":"main"}}`))
	}))
	t.Cleanup(server.Close)

	status, err := GetPRStatus(OpenOpts{
		Provider: "forgejo", URL: "https://forge.example/me/p", APIBase: server.URL,
		AuthEnv: "MOCK_FORGEJO_TOKEN", PRNumber: 5,
	})
	if err != nil {
		t.Fatalf("GetPRStatus: %v", err)
	}
	if status.Merged {
		t.Error("a declined PR must not report Merged true")
	}
}

// TestForgejoPRStatusOpenPopulatesBaseRef proves an open PR reports Merged
// false with BaseRef still read from base.ref — this is what makes the
// retarget check work on an unmerged PR.
func TestForgejoPRStatusOpenPopulatesBaseRef(t *testing.T) {
	t.Setenv("MOCK_FORGEJO_TOKEN", "secret")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"merged":false,"state":"open","base":{"ref":"milestone/v1.2"}}`))
	}))
	t.Cleanup(server.Close)

	status, err := GetPRStatus(OpenOpts{
		Provider: "forgejo", URL: "https://forge.example/me/p", APIBase: server.URL,
		AuthEnv: "MOCK_FORGEJO_TOKEN", PRNumber: 5,
	})
	if err != nil {
		t.Fatalf("GetPRStatus: %v", err)
	}
	if status.Merged {
		t.Error("an open PR must not report Merged true")
	}
	if status.BaseRef != "milestone/v1.2" {
		t.Errorf("BaseRef = %q, want milestone/v1.2", status.BaseRef)
	}
}

// TestForgejoPRStatusGiteaAlias proves "gitea" and "Gitea" reach the same
// implementation.
func TestForgejoPRStatusGiteaAlias(t *testing.T) {
	t.Setenv("MOCK_FORGEJO_TOKEN", "secret")
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"merged":true,"state":"closed","base":{"ref":"main"}}`))
	}))
	t.Cleanup(server.Close)

	for _, prov := range []string{"gitea", "Gitea"} {
		status, err := GetPRStatus(OpenOpts{
			Provider: prov, URL: "https://forge.example/me/p", APIBase: server.URL,
			AuthEnv: "MOCK_FORGEJO_TOKEN", PRNumber: 5,
		})
		if err != nil {
			t.Fatalf("provider %q: GetPRStatus: %v", prov, err)
		}
		if !status.Merged {
			t.Errorf("provider %q: expected Merged true", prov)
		}
	}
	if calls != 2 {
		t.Errorf("got %d handler calls, want 2 (gitea + Gitea)", calls)
	}
}

// TestForgejoPRStatusEndpointAndAuth asserts the request path and the
// Authorization: token header.
func TestForgejoPRStatusEndpointAndAuth(t *testing.T) {
	t.Setenv("MOCK_FORGEJO_TOKEN", "secret")
	var gotPath, gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"merged":true,"state":"closed","base":{"ref":"main"}}`))
	}))
	t.Cleanup(server.Close)

	if _, err := GetPRStatus(OpenOpts{
		Provider: "forgejo", URL: "https://forge.example/owner/repo", APIBase: server.URL,
		AuthEnv: "MOCK_FORGEJO_TOKEN", PRNumber: 5,
	}); err != nil {
		t.Fatalf("GetPRStatus: %v", err)
	}
	if want := "/repos/owner/repo/pulls/5"; gotPath != want {
		t.Errorf("path = %q, want %q", gotPath, want)
	}
	if gotAuth != "token secret" {
		t.Errorf("Authorization = %q, want %q", gotAuth, "token secret")
	}
}

// TestForgejoPRStatusHTTP500IsError proves a non-2xx response yields an
// error, not (false, nil).
func TestForgejoPRStatusHTTP500IsError(t *testing.T) {
	t.Setenv("MOCK_FORGEJO_TOKEN", "secret")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	t.Cleanup(server.Close)

	status, err := GetPRStatus(OpenOpts{
		Provider: "forgejo", URL: "https://forge.example/me/p", APIBase: server.URL,
		AuthEnv: "MOCK_FORGEJO_TOKEN", PRNumber: 5,
	})
	if err == nil {
		t.Fatalf("expected an error for a 500, got %+v", status)
	}
}

// TestForgejoOpenPRsTargetingFiltersByBaseRef proves only the PRs whose
// base.ref matches survive — Gitea's list-pulls endpoint has no base filter,
// so an impl trusting a `base=` query param would return the "dev" PR too.
func TestForgejoOpenPRsTargetingFiltersByBaseRef(t *testing.T) {
	t.Setenv("MOCK_FORGEJO_TOKEN", "secret")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			{"number":1,"title":"a","html_url":"u1","head":{"ref":"h1"},"base":{"ref":"main"}},
			{"number":2,"title":"b","html_url":"u2","head":{"ref":"h2"},"base":{"ref":"dev"}},
			{"number":3,"title":"c","html_url":"u3","head":{"ref":"h3"},"base":{"ref":"main"}}
		]`))
	}))
	t.Cleanup(server.Close)

	prs, err := OpenPRsTargeting(OpenOpts{
		Provider: "forgejo", URL: "https://forge.example/me/p", APIBase: server.URL,
		AuthEnv: "MOCK_FORGEJO_TOKEN",
	}, "main")
	if err != nil {
		t.Fatalf("OpenPRsTargeting: %v", err)
	}
	if len(prs) != 2 {
		t.Fatalf("got %d PRs, want 2 (base=dev must be filtered out): %+v", len(prs), prs)
	}
	for _, pr := range prs {
		if pr.Number != 1 && pr.Number != 3 {
			t.Errorf("unexpected PR in result: %+v", pr)
		}
	}
}

// TestForgejoOpenPRsTargetingFieldMapping proves number->Number,
// html_url->URL, and head.ref (not head.label, which carries an owner
// prefix)->HeadRefName.
func TestForgejoOpenPRsTargetingFieldMapping(t *testing.T) {
	t.Setenv("MOCK_FORGEJO_TOKEN", "secret")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"number":9,"title":"t","html_url":"https://forge/me/p/pulls/9","head":{"ref":"phase/x","label":"someone:phase/x"},"base":{"ref":"main"}}]`))
	}))
	t.Cleanup(server.Close)

	prs, err := OpenPRsTargeting(OpenOpts{
		Provider: "forgejo", URL: "https://forge.example/me/p", APIBase: server.URL,
		AuthEnv: "MOCK_FORGEJO_TOKEN",
	}, "main")
	if err != nil {
		t.Fatalf("OpenPRsTargeting: %v", err)
	}
	if len(prs) != 1 {
		t.Fatalf("got %d PRs, want 1", len(prs))
	}
	if prs[0].Number != 9 {
		t.Errorf("Number = %d, want 9", prs[0].Number)
	}
	if prs[0].URL != "https://forge/me/p/pulls/9" {
		t.Errorf("URL = %q", prs[0].URL)
	}
	if prs[0].HeadRefName != "phase/x" {
		t.Errorf("HeadRefName = %q, want head.ref (\"phase/x\"), not head.label", prs[0].HeadRefName)
	}
}

// TestForgejoOpenPRsTargetingAuthHeader asserts the Authorization: token
// header and a state=open query.
func TestForgejoOpenPRsTargetingAuthHeader(t *testing.T) {
	t.Setenv("MOCK_FORGEJO_TOKEN", "secret")
	var gotAuth, gotQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
	}))
	t.Cleanup(server.Close)

	if _, err := OpenPRsTargeting(OpenOpts{
		Provider: "forgejo", URL: "https://forge.example/me/p", APIBase: server.URL,
		AuthEnv: "MOCK_FORGEJO_TOKEN",
	}, "main"); err != nil {
		t.Fatalf("OpenPRsTargeting: %v", err)
	}
	if gotAuth != "token secret" {
		t.Errorf("Authorization = %q, want %q", gotAuth, "token secret")
	}
	if !strings.Contains(gotQuery, "state=open") {
		t.Errorf("query missing state=open: %q", gotQuery)
	}
}

// TestForgejoOpenPRsPaginates proves a full page followed by a short page
// both contribute their PRs. Gitea defaults to 30/page.
func TestForgejoOpenPRsPaginates(t *testing.T) {
	t.Setenv("MOCK_FORGEJO_TOKEN", "secret")

	fullPage := make([]map[string]any, 50)
	for i := range fullPage {
		fullPage[i] = map[string]any{
			"number": i + 1, "title": "t", "html_url": "u",
			"head": map[string]any{"ref": "h"}, "base": map[string]any{"ref": "main"},
		}
	}
	fullPageJSON, _ := json.Marshal(fullPage)
	shortPageJSON := []byte(`[{"number":200,"title":"t","html_url":"u","head":{"ref":"h"},"base":{"ref":"main"}}]`)

	var pages []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
		Provider: "forgejo", URL: "https://forge.example/me/p", APIBase: server.URL,
		AuthEnv: "MOCK_FORGEJO_TOKEN",
	}, "main")
	if err != nil {
		t.Fatalf("OpenPRsTargeting: %v", err)
	}
	if len(prs) != 51 {
		t.Fatalf("got %d PRs, want 51 (50 from page 1 + 1 from page 2)", len(prs))
	}
	if len(pages) != 2 {
		t.Fatalf("got %d page requests, want 2", len(pages))
	}
}

// TestForgejoOpenPRsErrorNeverEmptySlice proves a 404/500, including
// mid-listing, yields an error and a nil slice, never a partial result.
func TestForgejoOpenPRsErrorNeverEmptySlice(t *testing.T) {
	t.Setenv("MOCK_FORGEJO_TOKEN", "secret")

	fullPage := make([]map[string]any, 50)
	for i := range fullPage {
		fullPage[i] = map[string]any{
			"number": i + 1, "title": "t", "html_url": "u",
			"head": map[string]any{"ref": "h"}, "base": map[string]any{"ref": "main"},
		}
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
		Provider: "forgejo", URL: "https://forge.example/me/p", APIBase: server.URL,
		AuthEnv: "MOCK_FORGEJO_TOKEN",
	}, "main")
	if err == nil {
		t.Fatalf("expected an error from the page-2 500, got %+v", prs)
	}
	if prs != nil {
		t.Errorf("expected no PRs alongside the error, got %+v", prs)
	}
}

// TestForgejoOpenPRsTargetingGiteaAlias proves Provider "gitea" and "Gitea"
// reach the same handler, exercising configenum.Normalize rather than a
// hardcoded "forgejo" literal.
func TestForgejoOpenPRsTargetingGiteaAlias(t *testing.T) {
	t.Setenv("MOCK_FORGEJO_TOKEN", "secret")
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
	}))
	t.Cleanup(server.Close)

	for _, prov := range []string{"gitea", "Gitea"} {
		if _, err := OpenPRsTargeting(OpenOpts{
			Provider: prov, URL: "https://forge.example/me/p", APIBase: server.URL,
			AuthEnv: "MOCK_FORGEJO_TOKEN",
		}, "main"); err != nil {
			t.Fatalf("provider %q: OpenPRsTargeting: %v", prov, err)
		}
	}
	if calls != 2 {
		t.Errorf("got %d handler calls, want 2 (gitea + Gitea)", calls)
	}
}
