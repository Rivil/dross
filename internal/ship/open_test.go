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
		url           string
		owner, repo   string
		wantErr       bool
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
