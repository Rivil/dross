package forge

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const ytTokenEnv = "MOCK_YOUTRACK_TOKEN"

// newTestYTClient spins up an httptest server and points a YouTrackClient at
// it. The token env is set for the test's lifetime.
func newTestYTClient(t *testing.T, h http.HandlerFunc) (*YouTrackClient, *httptest.Server) {
	t.Helper()
	t.Setenv(ytTokenEnv, "secret")
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c, err := NewYouTrack(Config{APIBase: srv.URL, AuthEnv: ytTokenEnv, Project: "PROJ"})
	if err != nil {
		t.Fatalf("NewYouTrack: %v", err)
	}
	return c, srv
}

// TestNewAcceptsYouTrack pins that the YouTrack board client constructs from a
// valid config (a working client, never ErrNotImplemented) and rejects the
// same shape of bad config the forge backend does. The forge.New dispatch to
// this backend lands in the string-id migration (plan t-5).
func TestNewAcceptsYouTrack(t *testing.T) {
	t.Setenv(ytTokenEnv, "secret")
	good := Config{APIBase: "https://yt.example.com", AuthEnv: ytTokenEnv, Project: "PROJ"}
	c, err := NewYouTrack(good)
	if err != nil {
		t.Fatalf("NewYouTrack(good): %v", err)
	}
	if c == nil {
		t.Fatal("NewYouTrack(good): nil client")
	}

	bad := []struct {
		name string
		cfg  Config
		want string
	}{
		{"missing base", Config{AuthEnv: ytTokenEnv, Project: "PROJ"}, "needs APIBase"},
		{"missing authenv", Config{APIBase: "https://x", Project: "PROJ"}, "needs AuthEnv"},
		{"missing project", Config{APIBase: "https://x", AuthEnv: ytTokenEnv}, "needs Project"},
		{"unset token", Config{APIBase: "https://x", AuthEnv: "DROSS_DEFINITELY_UNSET", Project: "PROJ"}, "is not set"},
	}
	for _, tc := range bad {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := NewYouTrack(tc.cfg); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want error containing %q, got %v", tc.want, err)
			}
		})
	}
}

func TestYouTrackCreateIssue(t *testing.T) {
	var gotPath, gotMethod string
	var gotBody map[string]any
	c, _ := newTestYTClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		_, _ = io.WriteString(w, `{"idReadable":"PROJ-7","summary":"Hi","description":"body","$type":"Issue"}`)
	})

	iss, err := c.CreateIssue(IssueInput{Title: "Hi", Body: "body"})
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	if gotMethod != "POST" || gotPath != "/api/issues" {
		t.Fatalf("create hit %s %s, want POST /api/issues", gotMethod, gotPath)
	}
	if _, ok := gotBody["project"]; !ok {
		t.Errorf("create body missing project: %v", gotBody)
	}
	if gotBody["summary"] != "Hi" || gotBody["description"] != "body" {
		t.Errorf("create body summary/description wrong: %v", gotBody)
	}
	if iss.Key != "PROJ-7" {
		t.Errorf("Issue.Key = %q, want PROJ-7", iss.Key)
	}
}

func TestYouTrackListIssues(t *testing.T) {
	var gotPath, gotQuery, gotFields string
	c, _ := newTestYTClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.Query().Get("query")
		gotFields = r.URL.Query().Get("fields")
		_, _ = io.WriteString(w, `[{"idReadable":"PROJ-7"},{"idReadable":"PROJ-8"}]`)
	})

	issues, err := c.ListIssues(IssueFilter{State: "open"})
	if err != nil {
		t.Fatalf("ListIssues: %v", err)
	}
	if gotPath != "/api/issues" {
		t.Fatalf("list hit %s, want /api/issues", gotPath)
	}
	if !strings.Contains(gotQuery, "project:") {
		t.Errorf("list query %q missing project scope", gotQuery)
	}
	if gotFields == "" {
		t.Errorf("list dropped the fields projection")
	}
	if len(issues) != 2 || issues[0].Key != "PROJ-7" || issues[1].Key != "PROJ-8" {
		t.Errorf("list returned %+v", issues)
	}
}

func TestYouTrackGetIssue(t *testing.T) {
	var gotPath string
	c, _ := newTestYTClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = io.WriteString(w, `{"idReadable":"PROJ-7","summary":"Hi","customFields":[{"name":"State","value":{"name":"Open"}}]}`)
	})

	iss, err := c.GetIssue("PROJ-7")
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	if gotPath != "/api/issues/PROJ-7" {
		t.Fatalf("get hit %s, want /api/issues/PROJ-7", gotPath)
	}
	if iss.Key != "PROJ-7" || iss.Title != "Hi" || iss.State != "Open" {
		t.Errorf("get returned %+v", iss)
	}
}

func TestYouTrackUpdateIssue(t *testing.T) {
	var gotPath, gotMethod string
	var gotBody map[string]any
	c, _ := newTestYTClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		_, _ = io.WriteString(w, `{"idReadable":"PROJ-7","summary":"New"}`)
	})

	newTitle := "New"
	iss, err := c.UpdateIssue("PROJ-7", IssuePatch{Title: &newTitle})
	if err != nil {
		t.Fatalf("UpdateIssue: %v", err)
	}
	if gotMethod != "POST" || gotPath != "/api/issues/PROJ-7" {
		t.Fatalf("update hit %s %s, want POST /api/issues/PROJ-7", gotMethod, gotPath)
	}
	if gotBody["summary"] != "New" {
		t.Errorf("update body summary = %v, want New", gotBody["summary"])
	}
	if iss.Key != "PROJ-7" {
		t.Errorf("Issue.Key = %q, want PROJ-7", iss.Key)
	}
}

func TestYouTrackBearerAuth(t *testing.T) {
	var gotAuth, gotPath string
	c, _ := newTestYTClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		_, _ = io.WriteString(w, `{"idReadable":"PROJ-7"}`)
	})

	if _, err := c.GetIssue("PROJ-7"); err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	if gotAuth != "Bearer secret" {
		t.Errorf("Authorization = %q, want \"Bearer secret\"", gotAuth)
	}
	// The readable id, never an internal database id, addresses the issue.
	if !strings.HasSuffix(gotPath, "/PROJ-7") {
		t.Errorf("issue path %q is not addressed by readable id", gotPath)
	}
}
