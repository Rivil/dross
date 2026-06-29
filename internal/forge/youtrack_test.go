package forge

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
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

func TestYouTrackMilestoneVersionMode(t *testing.T) {
	// Create path: bundle exists with other values, target absent → POST it.
	t.Run("creates when absent", func(t *testing.T) {
		var gotFieldsGet, gotValuePost string
		var postBody map[string]any
		c, _ := newTestYTClient(t, func(w http.ResponseWriter, r *http.Request) {
			switch {
			case strings.HasSuffix(r.URL.Path, "/customFields") && r.Method == "GET":
				gotFieldsGet = r.URL.Path
				_, _ = io.WriteString(w, `[{"field":{"name":"Fix versions"},"bundle":{"id":"B1","$type":"VersionBundle","values":[{"name":"v0.5"}]}}]`)
			case strings.Contains(r.URL.Path, "/bundles/version/B1/values") && r.Method == "POST":
				gotValuePost = r.URL.Path
				raw, _ := io.ReadAll(r.Body)
				_ = json.Unmarshal(raw, &postBody)
				_, _ = io.WriteString(w, `{"name":"v0.6"}`)
			default:
				t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			}
		})

		id, err := c.EnsureMilestoneEntity("version", "v0.6", "")
		if err != nil {
			t.Fatalf("EnsureMilestoneEntity: %v", err)
		}
		if gotFieldsGet != "/api/admin/projects/PROJ/customFields" {
			t.Errorf("bundle discovery GET = %q", gotFieldsGet)
		}
		if gotValuePost == "" || postBody["name"] != "v0.6" {
			t.Errorf("version value not POSTed: path=%q body=%v", gotValuePost, postBody)
		}
		if id != "v0.6" {
			t.Errorf("id = %q, want v0.6", id)
		}
	})

	// Idempotent: target already in the bundle → no POST.
	t.Run("reuses when present", func(t *testing.T) {
		posted := false
		c, _ := newTestYTClient(t, func(w http.ResponseWriter, r *http.Request) {
			if r.Method == "POST" {
				posted = true
			}
			_, _ = io.WriteString(w, `[{"field":{"name":"Fix versions"},"bundle":{"id":"B1","$type":"VersionBundle","values":[{"name":"v0.6"}]}}]`)
		})
		id, err := c.EnsureMilestoneEntity("version", "v0.6", "")
		if err != nil {
			t.Fatalf("EnsureMilestoneEntity: %v", err)
		}
		if posted {
			t.Error("should not POST a version that already exists")
		}
		if id != "v0.6" {
			t.Errorf("id = %q, want v0.6", id)
		}
	})
}

func TestYouTrackMilestoneAgileMode(t *testing.T) {
	t.Run("missing board warns and skips", func(t *testing.T) {
		var gotPath string
		c, _ := newTestYTClient(t, func(w http.ResponseWriter, r *http.Request) {
			gotPath = r.URL.Path
			_, _ = io.WriteString(w, `[{"id":"108-1","name":"Other board"}]`)
		})
		id, err := c.EnsureMilestoneEntity("agile", "v0.6 board", "")
		if err != nil {
			t.Fatalf("agile mode must not fail on a missing board: %v", err)
		}
		if gotPath != "/api/agiles" {
			t.Errorf("agile lookup hit %q, want /api/agiles", gotPath)
		}
		if id != "" {
			t.Errorf("missing board should skip (empty id), got %q", id)
		}
	})

	t.Run("present board returns its id", func(t *testing.T) {
		c, _ := newTestYTClient(t, func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.WriteString(w, `[{"id":"108-23","name":"v0.6 board"}]`)
		})
		id, err := c.EnsureMilestoneEntity("agile", "v0.6 board", "")
		if err != nil {
			t.Fatalf("EnsureMilestoneEntity: %v", err)
		}
		if id != "108-23" {
			t.Errorf("id = %q, want 108-23", id)
		}
	})
}

func TestYouTrackMilestoneEpicMode(t *testing.T) {
	t.Run("creates when absent", func(t *testing.T) {
		var postBody map[string]any
		created := false
		c, _ := newTestYTClient(t, func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case "GET":
				_, _ = io.WriteString(w, `[]`)
			case "POST":
				created = true
				raw, _ := io.ReadAll(r.Body)
				_ = json.Unmarshal(raw, &postBody)
				_, _ = io.WriteString(w, `{"idReadable":"PROJ-50","summary":"v0.6"}`)
			}
		})
		id, err := c.EnsureMilestoneEntity("epic", "v0.6", "the milestone")
		if err != nil {
			t.Fatalf("EnsureMilestoneEntity: %v", err)
		}
		if !created {
			t.Error("epic should be created when absent")
		}
		cfs, _ := postBody["customFields"].([]any)
		if len(cfs) == 0 {
			t.Errorf("create epic body missing Type custom field: %v", postBody)
		}
		if id != "PROJ-50" {
			t.Errorf("id = %q, want PROJ-50", id)
		}
	})

	t.Run("reuses when present", func(t *testing.T) {
		posted := false
		c, _ := newTestYTClient(t, func(w http.ResponseWriter, r *http.Request) {
			if r.Method == "POST" {
				posted = true
			}
			_, _ = io.WriteString(w, `[{"idReadable":"PROJ-50","summary":"v0.6"}]`)
		})
		id, err := c.EnsureMilestoneEntity("epic", "v0.6", "the milestone")
		if err != nil {
			t.Fatalf("EnsureMilestoneEntity: %v", err)
		}
		if posted {
			t.Error("should not create a duplicate epic")
		}
		if id != "PROJ-50" {
			t.Errorf("id = %q, want PROJ-50", id)
		}
	})
}

func TestYouTrackSetStateMapsAndUpdates(t *testing.T) {
	post := func(t *testing.T, status string, override map[string]string) map[string]any {
		t.Helper()
		var gotPath, gotMethod string
		var body map[string]any
		c, _ := newTestYTClient(t, func(w http.ResponseWriter, r *http.Request) {
			gotMethod, gotPath = r.Method, r.URL.Path
			raw, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(raw, &body)
			_, _ = io.WriteString(w, `{"idReadable":"PROJ-7"}`)
		})
		if err := c.SetState("PROJ-7", status, override); err != nil {
			t.Fatalf("SetState: %v", err)
		}
		if gotMethod != "POST" || gotPath != "/api/issues/PROJ-7" {
			t.Fatalf("SetState hit %s %s, want POST /api/issues/PROJ-7", gotMethod, gotPath)
		}
		return body
	}

	stateValue := func(body map[string]any) string {
		cfs, _ := body["customFields"].([]any)
		for _, cf := range cfs {
			m, _ := cf.(map[string]any)
			if m["name"] == "State" {
				v, _ := m["value"].(map[string]any)
				s, _ := v["name"].(string)
				return s
			}
		}
		return ""
	}

	t.Run("override wins", func(t *testing.T) {
		if got := stateValue(post(t, "shipped", map[string]string{"shipped": "Fixed"})); got != "Fixed" {
			t.Errorf("State value = %q, want Fixed", got)
		}
	})
	t.Run("default map when override empty", func(t *testing.T) {
		if got := stateValue(post(t, "shipped", nil)); got != "Fixed" {
			t.Errorf("State value = %q, want Fixed (default map)", got)
		}
	})
}

func TestYouTrackSetStateUnmappedWarnsSkips(t *testing.T) {
	posted := false
	c, _ := newTestYTClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			posted = true
		}
		_, _ = io.WriteString(w, `{}`)
	})

	// Capture stderr to confirm the warning.
	oldStderr := os.Stderr
	rd, wr, _ := os.Pipe()
	os.Stderr = wr
	err := c.SetState("PROJ-7", "no-such-state", nil)
	_ = wr.Close()
	os.Stderr = oldStderr
	warn, _ := io.ReadAll(rd)

	if err != nil {
		t.Fatalf("unmapped state must not fail the sync, got %v", err)
	}
	if posted {
		t.Error("unmapped state must not write the State field")
	}
	if !strings.Contains(string(warn), "no YouTrack State mapping") {
		t.Errorf("expected a skip warning, got %q", string(warn))
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
