package forge

import (
	"encoding/json"
	"fmt"
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
	c, err := NewYouTrack(Config{APIBase: srv.URL, AuthEnv: ytTokenEnv, Project: "PROJ", Hosts: allowingSelf(srv.URL)})
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
	good := Config{APIBase: "https://yt.example.com", AuthEnv: ytTokenEnv, Project: "PROJ", Hosts: allowingSelf("https://yt.example.com")}
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
		{"unset token", Config{APIBase: "https://x", AuthEnv: "DROSS_DEFINITELY_UNSET", Project: "PROJ", Hosts: allowingSelf("https://x")}, "is not set"},
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

// bigBody returns a response body longer than the 500-byte snippet cap, with a
// recognisable tail so truncation is provable rather than merely a length check.
func bigBody() string {
	return strings.Repeat("x", 600) + "TAIL-MARKER"
}

// TestYouTrackErrorSnippetTruncatesAndHints covers the error branch of the
// YouTrack transport, which the happy-path fakes never reach. Both halves
// matter operationally: an untruncated body dumps a whole HTML error page into
// the user's terminal, and the per-status hint is the difference between "403"
// and knowing which token to fix.
func TestYouTrackErrorSnippetTruncatesAndHints(t *testing.T) {
	cases := []struct {
		name     string
		status   int
		body     string
		wantIn   []string
		wantOut  []string
		truncate bool
	}{
		{
			name:     "oversized body is truncated to the cap",
			status:   500,
			body:     bigBody(),
			wantOut:  []string{"TAIL-MARKER"},
			truncate: true,
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
			wantIn: []string{ytTokenEnv, "expired"},
		},
		{
			name:   "403 names the permission problem",
			status: 403,
			body:   "forbidden",
			wantIn: []string{"lacks permission"},
		},
		{
			name:   "404 names the project and the config keys to check",
			status: 404,
			body:   "missing",
			wantIn: []string{"PROJ", "base_url"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := newTestYTClient(t, func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			})

			_, err := c.GetIssue("PROJ-1")
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
			if tc.truncate && strings.Count(msg, "x") > 520 {
				t.Errorf("snippet is %d x's long, want it capped near 500", strings.Count(msg, "x"))
			}
		})
	}
}

// TestYouTrackCreateBacklogItem covers both arms of the fix-version guard and
// the create path's own error branch. Attaching the version is how a backlog
// item lands in the right milestone bundle; skipping it when none is given is
// what keeps an unversioned item from being filed against an empty version.
func TestYouTrackCreateBacklogItem(t *testing.T) {
	capture := func(t *testing.T, fixVersion string) map[string]any {
		t.Helper()
		var got map[string]any
		c, _ := newTestYTClient(t, func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewDecoder(r.Body).Decode(&got)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"idReadable":"PROJ-9","summary":"s"}`))
		})
		issue, err := c.CreateBacklogItem("s", "d", fixVersion)
		if err != nil {
			t.Fatalf("CreateBacklogItem: %v", err)
		}
		if issue.Key != "PROJ-9" {
			t.Errorf("Key = %q, want PROJ-9", issue.Key)
		}
		return got
	}

	t.Run("with a fix version it is attached", func(t *testing.T) {
		body := capture(t, "v1.3")
		fields, ok := body["customFields"]
		if !ok {
			t.Fatalf("no customFields sent: %v", body)
		}
		if !strings.Contains(fmt.Sprint(fields), "v1.3") {
			t.Errorf("customFields does not carry the version: %v", fields)
		}
	})

	t.Run("without one, no customFields are sent", func(t *testing.T) {
		body := capture(t, "")
		if _, ok := body["customFields"]; ok {
			t.Errorf("an empty fix version still sent customFields: %v", body)
		}
	})

	t.Run("a transport failure is wrapped", func(t *testing.T) {
		c, _ := newTestYTClient(t, func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(500)
			_, _ = w.Write([]byte("nope"))
		})
		if _, err := c.CreateBacklogItem("s", "d", ""); err == nil {
			t.Fatal("a failing create returned no error")
		} else if !strings.Contains(err.Error(), "create backlog item") {
			t.Errorf("err = %q, want the create context", err)
		}
	})
}

// TestYouTrackLinkSubtask covers the commands-API link path and its error arm.
// Re-applying an existing link is a documented no-op, so this is called on
// every re-sync — a silently failing link leaves an orphaned child issue.
func TestYouTrackLinkSubtask(t *testing.T) {
	t.Run("applies the subtask command to the child", func(t *testing.T) {
		var got map[string]any
		c, _ := newTestYTClient(t, func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewDecoder(r.Body).Decode(&got)
			w.WriteHeader(200)
		})
		if err := c.LinkSubtask("PROJ-1", "PROJ-2"); err != nil {
			t.Fatalf("LinkSubtask: %v", err)
		}
		if q, _ := got["query"].(string); q != "subtask of PROJ-1" {
			t.Errorf("query = %q, want the parent in the command", q)
		}
		if !strings.Contains(fmt.Sprint(got["issues"]), "PROJ-2") {
			t.Errorf("the child issue is not the command's target: %v", got["issues"])
		}
	})

	t.Run("a failure names both issues", func(t *testing.T) {
		c, _ := newTestYTClient(t, func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(500)
		})
		err := c.LinkSubtask("PROJ-1", "PROJ-2")
		if err == nil {
			t.Fatal("a failing link returned no error")
		}
		for _, want := range []string{"PROJ-1", "PROJ-2"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("err = %q, want it to name %q", err, want)
			}
		}
	})
}

// TestYouTrackBuildQueryFoldsStateAndLabels covers the label loop, which no
// existing test reaches — every ListIssues fixture filters by state only. The
// query is what scopes a sync to this project's issues, so a dropped label
// clause silently widens a sync to the whole board.
func TestYouTrackBuildQueryFoldsStateAndLabels(t *testing.T) {
	c, _ := newTestYTClient(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) })

	cases := []struct {
		name   string
		filter IssueFilter
		want   string
	}{
		{"default state is open", IssueFilter{}, "project: PROJ #Unresolved"},
		{"explicit open", IssueFilter{State: "open"}, "project: PROJ #Unresolved"},
		{"closed", IssueFilter{State: "closed"}, "project: PROJ #Resolved"},
		{"one label", IssueFilter{Labels: []string{"bug"}}, "project: PROJ #Unresolved tag: bug"},
		{
			"several labels share one OR'd clause",
			IssueFilter{State: "closed", Labels: []string{"bug", "dross"}},
			"project: PROJ #Resolved tag: bug, dross",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := c.buildQuery(tc.filter); got != tc.want {
				t.Errorf("buildQuery = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestYouTrackBuildQueryOrsLabels pins the OR semantics themselves. Repeating
// the `tag:` token per label intersects them, so a two-label pull would return
// only issues carrying both — the AND bug c-1 exists to kill.
func TestYouTrackBuildQueryOrsLabels(t *testing.T) {
	c, _ := newTestYTClient(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) })

	t.Run("exactly one tag token for several labels", func(t *testing.T) {
		got := c.buildQuery(IssueFilter{Labels: []string{"bug", "enhancement"}})
		if n := strings.Count(got, "tag:"); n != 1 {
			t.Errorf("query %q carries %d `tag:` tokens, want exactly 1 (OR, not AND)", got, n)
		}
		if !strings.Contains(got, "tag: bug, enhancement") {
			t.Errorf("query = %q, want it to carry `tag: bug, enhancement`", got)
		}
	})

	t.Run("a structured label is brace-wrapped", func(t *testing.T) {
		got := c.buildQuery(IssueFilter{Labels: []string{"dross/phase:01-x"}})
		if !strings.Contains(got, "tag: {dross/phase:01-x}") {
			t.Errorf("query = %q, want the label brace-wrapped — `/`, `:` and `-` are query syntax", got)
		}
	})

	t.Run("the state clause is untouched by the label change", func(t *testing.T) {
		got := c.buildQuery(IssueFilter{Labels: []string{"bug", "enhancement"}})
		if !strings.Contains(got, "#Unresolved") {
			t.Errorf("query = %q, want the #Unresolved state clause still scoped in", got)
		}
	})

	t.Run("no labels emits no tag token", func(t *testing.T) {
		if got := c.buildQuery(IssueFilter{}); strings.Contains(got, "tag:") {
			t.Errorf("query = %q, want no `tag:` token at all", got)
		}
	})
}

// TestYouTrackListIssuesDropsUnknownLabels covers the label-index gate. An
// unknown tag cannot match anything, so it is dropped and named — but the query
// must never degrade into an unfiltered whole-board list, and an unreadable
// index must refuse rather than widen.
func TestYouTrackListIssuesDropsUnknownLabels(t *testing.T) {
	t.Run("unknown label dropped and named, known one still queried", func(t *testing.T) {
		var gotQuery string
		var issueCalls int
		c, _ := newTestYTClient(t, func(w http.ResponseWriter, r *http.Request) {
			if strings.HasSuffix(r.URL.Path, "/issueTags") {
				_, _ = io.WriteString(w, `[{"name":"bug"}]`)
				return
			}
			issueCalls++
			gotQuery = r.URL.Query().Get("query")
			_, _ = io.WriteString(w, `[{"idReadable":"PROJ-1","summary":"a"}]`)
		})

		warn := captureForgeStderr(t, func() {
			if _, err := c.ListIssues(IssueFilter{Labels: []string{"bug", "typo"}}); err != nil {
				t.Fatalf("ListIssues: %v", err)
			}
		})

		if issueCalls != 1 {
			t.Fatalf("issued %d issue queries, want 1", issueCalls)
		}
		if !strings.Contains(gotQuery, "bug") {
			t.Errorf("query %q lost the known label", gotQuery)
		}
		if strings.Contains(gotQuery, "typo") {
			t.Errorf("query %q carried the unknown label to the wire", gotQuery)
		}
		if !strings.Contains(warn, "typo") {
			t.Errorf("stderr = %q, want the dropped label named", warn)
		}
	})

	t.Run("every label unknown returns nothing, never the whole board", func(t *testing.T) {
		var issueCalls int
		c, _ := newTestYTClient(t, func(w http.ResponseWriter, r *http.Request) {
			if strings.HasSuffix(r.URL.Path, "/issueTags") {
				_, _ = io.WriteString(w, `[{"name":"bug"}]`)
				return
			}
			issueCalls++
			_, _ = io.WriteString(w, `[{"idReadable":"PROJ-1","summary":"everything"}]`)
		})

		var issues []Issue
		_ = captureForgeStderr(t, func() {
			var err error
			if issues, err = c.ListIssues(IssueFilter{Labels: []string{"typo"}}); err != nil {
				t.Fatalf("ListIssues: %v", err)
			}
		})
		if len(issues) != 0 {
			t.Errorf("returned %+v, want zero issues", issues)
		}
		if issueCalls != 0 {
			t.Errorf("issued %d unfiltered issue queries, want 0", issueCalls)
		}
	})

	t.Run("a failing tag index refuses the query", func(t *testing.T) {
		var issueCalls int
		c, _ := newTestYTClient(t, func(w http.ResponseWriter, r *http.Request) {
			if strings.HasSuffix(r.URL.Path, "/issueTags") {
				w.WriteHeader(500)
				return
			}
			issueCalls++
			_, _ = io.WriteString(w, `[]`)
		})
		if _, err := c.ListIssues(IssueFilter{Labels: []string{"bug"}}); err == nil {
			t.Fatal("a 500 from the tag index returned no error")
		}
		if issueCalls != 0 {
			t.Errorf("issued %d issue queries after the index failed, want 0", issueCalls)
		}
	})

	t.Run("an empty filter reads no tag index at all", func(t *testing.T) {
		var tagCalls int
		c, _ := newTestYTClient(t, func(w http.ResponseWriter, r *http.Request) {
			if strings.HasSuffix(r.URL.Path, "/issueTags") {
				tagCalls++
			}
			_, _ = io.WriteString(w, `[]`)
		})
		if _, err := c.ListIssues(IssueFilter{}); err != nil {
			t.Fatalf("ListIssues: %v", err)
		}
		if tagCalls != 0 {
			t.Errorf("unlabelled list read the tag index %d times — a blip must not fail `dross watch`", tagCalls)
		}
	})
}

// ytTagFake is a YouTrack fake that models tags as real entities: a global tag
// index with ids, plus a per-issue tag set that POST/DELETE mutate. Assertions
// can then read the issue's tags back by NAME, which is what the dross
// resolvers actually query by.
type ytTagFake struct {
	index    map[string]string   // tag name -> id
	onIssue  map[string][]string // issue key -> tag names
	created  []string            // tag names created via POST /api/issueTags
	nextID   int
	tagWrite func(w http.ResponseWriter) bool // optional: intercept a tag write
}

func newYTTagFake(known ...string) *ytTagFake {
	f := &ytTagFake{index: map[string]string{}, onIssue: map[string][]string{}}
	for _, n := range known {
		f.nextID++
		f.index[n] = fmt.Sprintf("t-%d", f.nextID)
	}
	return f
}

func (f *ytTagFake) nameFor(id string) string {
	for n, i := range f.index {
		if i == id {
			return n
		}
	}
	return ""
}

func (f *ytTagFake) handler(t *testing.T) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		switch {
		case path == "/api/issueTags" && r.Method == "GET":
			var out []string
			for n, id := range f.index {
				out = append(out, fmt.Sprintf(`{"id":%q,"name":%q}`, id, n))
			}
			_, _ = io.WriteString(w, "["+strings.Join(out, ",")+"]")

		case path == "/api/issueTags" && r.Method == "POST":
			var body struct {
				Name string `json:"name"`
			}
			raw, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(raw, &body)
			f.nextID++
			id := fmt.Sprintf("t-%d", f.nextID)
			f.index[body.Name] = id
			f.created = append(f.created, body.Name)
			_, _ = io.WriteString(w, fmt.Sprintf(`{"id":%q,"name":%q}`, id, body.Name))

		case strings.HasSuffix(path, "/tags") && r.Method == "POST":
			if f.tagWrite != nil && f.tagWrite(w) {
				return
			}
			key := strings.TrimSuffix(strings.TrimPrefix(path, "/api/issues/"), "/tags")
			var body struct {
				ID string `json:"id"`
			}
			raw, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(raw, &body)
			f.onIssue[key] = append(f.onIssue[key], f.nameFor(body.ID))
			_, _ = io.WriteString(w, `{}`)

		case strings.Contains(path, "/tags/") && r.Method == "DELETE":
			rest := strings.TrimPrefix(path, "/api/issues/")
			key, id, _ := strings.Cut(rest, "/tags/")
			name := f.nameFor(id)
			var kept []string
			for _, n := range f.onIssue[key] {
				if n != name {
					kept = append(kept, n)
				}
			}
			f.onIssue[key] = kept
			_, _ = io.WriteString(w, `{}`)

		case strings.HasPrefix(path, "/api/issues/") && r.Method == "GET":
			key := strings.TrimPrefix(path, "/api/issues/")
			var tags []string
			for _, n := range f.onIssue[key] {
				tags = append(tags, fmt.Sprintf(`{"id":%q,"name":%q}`, f.index[n], n))
			}
			_, _ = io.WriteString(w, fmt.Sprintf(`{"idReadable":%q,"tags":[%s]}`, key, strings.Join(tags, ",")))

		case path == "/api/issues" && r.Method == "POST":
			_, _ = io.WriteString(w, `{"idReadable":"PROJ-7","summary":"Hi"}`)

		case strings.HasPrefix(path, "/api/issues/") && r.Method == "POST":
			key := strings.TrimPrefix(path, "/api/issues/")
			_, _ = io.WriteString(w, fmt.Sprintf(`{"idReadable":%q}`, key))

		default:
			t.Errorf("unexpected %s %s", r.Method, path)
			_, _ = io.WriteString(w, `{}`)
		}
	}
}

// TestYouTrackCreateAppliesTags is the c-4/c-6 foundation: CreateIssue sent no
// tags at all before this, so no dross marker had ever reached a YouTrack
// issue — and every resolver that queries by that marker was dead on arrival.
func TestYouTrackCreateAppliesTags(t *testing.T) {
	f := newYTTagFake("dross", "dross/phase:01-x")
	c, _ := newTestYTClient(t, f.handler(t))

	iss, err := c.CreateIssue(IssueInput{Title: "Hi", Labels: []string{"dross", "dross/phase:01-x"}})
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	if iss.Key != "PROJ-7" {
		t.Fatalf("Issue.Key = %q, want PROJ-7", iss.Key)
	}
	got := f.onIssue["PROJ-7"]
	for _, want := range []string{"dross", "dross/phase:01-x"} {
		if !slicesContain(got, want) {
			t.Errorf("issue tags = %v, missing %q", got, want)
		}
	}
}

// TestYouTrackTagsReplaceRatherThanAccumulate pins replace semantics. An
// additive implementation leaves a re-routed item claiming both destinations,
// and every `dross/target:` filter then returns it twice.
func TestYouTrackTagsReplaceRatherThanAccumulate(t *testing.T) {
	f := newYTTagFake("dross", "dross/target:a", "dross/target:b")
	f.onIssue["PROJ-7"] = []string{"dross", "dross/target:a"}
	c, _ := newTestYTClient(t, f.handler(t))

	labels := []string{"dross", "dross/target:b"}
	if _, err := c.UpdateIssue("PROJ-7", IssuePatch{Labels: &labels}); err != nil {
		t.Fatalf("UpdateIssue: %v", err)
	}
	got := f.onIssue["PROJ-7"]
	if len(got) != 2 || !slicesContain(got, "dross") || !slicesContain(got, "dross/target:b") {
		t.Fatalf("tags = %v, want exactly [dross dross/target:b]", got)
	}
	if slicesContain(got, "dross/target:a") {
		t.Error("the replaced target tag survived — removals must remove")
	}
}

// TestYouTrackCreatesMissingTagEntityOnce pins the ensure step: a name the
// instance has never seen is created before the issue write, and a name it
// already knows costs no create.
func TestYouTrackCreatesMissingTagEntityOnce(t *testing.T) {
	t.Run("absent name is created once", func(t *testing.T) {
		f := newYTTagFake("dross")
		c, _ := newTestYTClient(t, f.handler(t))
		if _, err := c.CreateIssue(IssueInput{Title: "Hi", Labels: []string{"dross", "dross/phase:01-x"}}); err != nil {
			t.Fatalf("CreateIssue: %v", err)
		}
		if len(f.created) != 1 || f.created[0] != "dross/phase:01-x" {
			t.Errorf("created tags = %v, want exactly [dross/phase:01-x]", f.created)
		}
	})

	t.Run("known names create nothing", func(t *testing.T) {
		f := newYTTagFake("dross", "dross/phase:01-x")
		c, _ := newTestYTClient(t, f.handler(t))
		if _, err := c.CreateIssue(IssueInput{Title: "Hi", Labels: []string{"dross", "dross/phase:01-x"}}); err != nil {
			t.Fatalf("CreateIssue: %v", err)
		}
		if len(f.created) != 0 {
			t.Errorf("created %v, want no tag creates for names already in the index", f.created)
		}
	})
}

// TestYouTrackUpdateWithoutLabelsSendsNoTagWrite pins that a title-only patch
// costs no tag traffic — and, more importantly, cannot strip an issue's tags
// by treating "not mentioned" as "empty".
func TestYouTrackUpdateWithoutLabelsSendsNoTagWrite(t *testing.T) {
	var tagRequests int
	c, _ := newTestYTClient(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/tags") || strings.HasSuffix(r.URL.Path, "/issueTags") {
			tagRequests++
		}
		_, _ = io.WriteString(w, `{"idReadable":"PROJ-7","summary":"New"}`)
	})
	title := "New"
	if _, err := c.UpdateIssue("PROJ-7", IssuePatch{Title: &title}); err != nil {
		t.Fatalf("UpdateIssue: %v", err)
	}
	if tagRequests != 0 {
		t.Errorf("a labels-nil patch made %d tag request(s), want 0", tagRequests)
	}
}

// TestYouTrackCreateSurvivesATagBlip pins that a failed tag write does not
// orphan a real issue: the caller still gets the key it has to record, plus a
// warning naming the failure.
func TestYouTrackCreateSurvivesATagBlip(t *testing.T) {
	f := newYTTagFake("dross")
	f.tagWrite = func(w http.ResponseWriter) bool {
		w.WriteHeader(500)
		return true
	}
	c, _ := newTestYTClient(t, f.handler(t))

	var iss *Issue
	var err error
	warn := captureForgeStderr(t, func() {
		iss, err = c.CreateIssue(IssueInput{Title: "Hi", Labels: []string{"dross"}})
	})
	if err != nil {
		t.Fatalf("a tagging blip must not fail the create: %v", err)
	}
	if iss == nil || iss.Key != "PROJ-7" {
		t.Fatalf("created issue = %+v, want its key returned so the caller can record it", iss)
	}
	if !strings.Contains(warn, "PROJ-7") {
		t.Errorf("stderr = %q, want a warning naming the issue", warn)
	}
}

// TestYouTrackGetIssueMapsTagsToLabels pins the read side. The phase and
// deferred resolvers post-filter on Issue.Labels, so a GetIssue that dropped
// tags would make every adoption check fail open.
func TestYouTrackGetIssueMapsTagsToLabels(t *testing.T) {
	c, _ := newTestYTClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"idReadable":"PROJ-7","summary":"Hi","tags":[{"name":"dross"},{"name":"dross/phase:01-x"}]}`)
	})
	iss, err := c.GetIssue("PROJ-7")
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	if len(iss.Labels) != 2 || !slicesContain(iss.Labels, "dross") || !slicesContain(iss.Labels, "dross/phase:01-x") {
		t.Errorf("Labels = %v, want both tags mapped through", iss.Labels)
	}
}

func slicesContain(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}

// TestYouTrackLinkIssuesRelates pins the relates-to command. It is a peer
// relation, deliberately not LinkSubtask's hierarchy — a routed backlog item
// and its destination phase are not parent and child.
func TestYouTrackLinkIssuesRelates(t *testing.T) {
	var gotPath string
	var gotBody map[string]any
	c, _ := newTestYTClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		_, _ = io.WriteString(w, `{}`)
	})

	if err := c.LinkIssues("PROJ-9", "PROJ-3"); err != nil {
		t.Fatalf("LinkIssues: %v", err)
	}
	if gotPath != "/api/commands" {
		t.Fatalf("link hit %s, want /api/commands", gotPath)
	}
	query, _ := gotBody["query"].(string)
	if !strings.Contains(query, "PROJ-3") {
		t.Errorf("command query = %q, want it to name the target PROJ-3", query)
	}
	if strings.Contains(query, "subtask") {
		t.Errorf("command query = %q, want a peer relation, not a subtask hierarchy", query)
	}
	issues, _ := gotBody["issues"].([]any)
	if len(issues) != 1 {
		t.Fatalf("issues = %v, want exactly the source issue", gotBody["issues"])
	}
	first, _ := issues[0].(map[string]any)
	if first["idReadable"] != "PROJ-9" {
		t.Errorf("issues[0].idReadable = %v, want PROJ-9", first["idReadable"])
	}
}

// TestIssueLinkerCapabilityIsAssertable pins that the capability is a real
// interface boundary. A future no-op stub on GitHub would satisfy the
// assertion and silence c-7's warn arm undetectably — so the negative half is
// asserted too.
func TestIssueLinkerCapabilityIsAssertable(t *testing.T) {
	if _, ok := any((*YouTrackClient)(nil)).(IssueLinker); !ok {
		t.Error("YouTrackClient must satisfy IssueLinker")
	}
	if _, ok := any((*JiraClient)(nil)).(IssueLinker); !ok {
		t.Error("JiraClient must satisfy IssueLinker")
	}
	if _, ok := any((*GitHubClient)(nil)).(IssueLinker); ok {
		t.Error("GitHubClient must NOT satisfy IssueLinker — a no-op stub hides the fallback path")
	}
}

// newTestYTClientFields is newTestYTClient with [board.fields] overrides
// applied — the whole point of c-3 is that these travel from config into the
// wire payloads.
func newTestYTClientFields(t *testing.T, fields Fields, h http.HandlerFunc) *YouTrackClient {
	t.Helper()
	t.Setenv(ytTokenEnv, "secret")
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c, err := NewYouTrack(Config{APIBase: srv.URL, AuthEnv: ytTokenEnv, Project: "PROJ", Fields: fields, Hosts: allowingSelf(srv.URL)})
	if err != nil {
		t.Fatalf("NewYouTrack: %v", err)
	}
	return c
}

// TestYouTrackStateFieldIsConfigurable proves the state half of c-3: a project
// that renamed its State field syncs without a code change, and one that did
// not is byte-identical to before.
func TestYouTrackStateFieldIsConfigurable(t *testing.T) {
	postedFieldName := func(t *testing.T, fields Fields) string {
		t.Helper()
		var got string
		c := newTestYTClientFields(t, fields, func(w http.ResponseWriter, r *http.Request) {
			var body struct {
				CustomFields []struct {
					Name string `json:"name"`
				} `json:"customFields"`
			}
			raw, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(raw, &body)
			if len(body.CustomFields) > 0 {
				got = body.CustomFields[0].Name
			}
			_, _ = io.WriteString(w, `{}`)
		})
		if err := c.SetState("PROJ-7", "complete", nil); err != nil {
			t.Fatalf("SetState: %v", err)
		}
		return got
	}

	if got := postedFieldName(t, Fields{State: "Статус"}); got != "Статус" {
		t.Errorf("customFields[0].name = %q, want the configured Статус", got)
	}
	if got := postedFieldName(t, Fields{}); got != "State" {
		t.Errorf("customFields[0].name = %q with Fields unset, want the default State", got)
	}
}

// TestYouTrackFixVersionsFieldIsConfigurable proves the milestone half: the
// backlog item's version attach goes to the configured field.
func TestYouTrackFixVersionsFieldIsConfigurable(t *testing.T) {
	var got string
	c := newTestYTClientFields(t, Fields{FixVersions: "Release"}, func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			CustomFields []struct {
				Name string `json:"name"`
			} `json:"customFields"`
		}
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		if len(body.CustomFields) > 0 {
			got = body.CustomFields[0].Name
		}
		_, _ = io.WriteString(w, `{"idReadable":"PROJ-7"}`)
	})
	if _, err := c.CreateBacklogItem("s", "d", "v1.4"); err != nil {
		t.Fatalf("CreateBacklogItem: %v", err)
	}
	if got != "Release" {
		t.Errorf("fix-versions field = %q, want the configured Release", got)
	}
}

// TestYouTrackTypeFieldMovesQueryAndPayloadTogether pins that the epic search
// and the epic create use the SAME field name. If only one moved, ensureEpic
// would create an epic it can then never find — one new epic per milestone
// sync, forever.
func TestYouTrackTypeFieldMovesQueryAndPayloadTogether(t *testing.T) {
	var gotQuery, gotFieldName string
	c := newTestYTClientFields(t, Fields{Type: "Kind"}, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			gotQuery = r.URL.Query().Get("query")
			_, _ = io.WriteString(w, `[]`)
			return
		}
		var body struct {
			CustomFields []struct {
				Name string `json:"name"`
			} `json:"customFields"`
		}
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		if len(body.CustomFields) > 0 {
			gotFieldName = body.CustomFields[0].Name
		}
		_, _ = io.WriteString(w, `{"idReadable":"PROJ-9","summary":"v1.4"}`)
	})

	if _, err := c.EnsureMilestoneEntity("epic", "v1.4", ""); err != nil {
		t.Fatalf("EnsureMilestoneEntity: %v", err)
	}
	if !strings.Contains(gotQuery, "Kind: Epic") {
		t.Errorf("epic search query = %q, want it to filter on the configured Kind field", gotQuery)
	}
	if gotFieldName != "Kind" {
		t.Errorf("epic create field = %q, want Kind — query and payload must move together", gotFieldName)
	}
}

// TestYouTrackEnsureVersionPicksTheConfiguredBundle pins bundle selection. A
// project with two VersionBundle-typed fields would otherwise take whichever
// came back first, writing the milestone into the wrong bundle — silently,
// because that write succeeds.
func TestYouTrackEnsureVersionPicksTheConfiguredBundle(t *testing.T) {
	var gotBundlePath string
	c := newTestYTClientFields(t, Fields{FixVersions: "Release"}, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/customFields") {
			_, _ = io.WriteString(w, `[
				{"field":{"name":"Sprint versions"},"bundle":{"id":"B-WRONG","$type":"VersionBundle","values":[]}},
				{"field":{"name":"Release"},"bundle":{"id":"B-RIGHT","$type":"VersionBundle","values":[]}}
			]`)
			return
		}
		gotBundlePath = r.URL.Path
		_, _ = io.WriteString(w, `{"name":"v1.4"}`)
	})

	if _, err := c.EnsureMilestoneEntity("version", "v1.4", ""); err != nil {
		t.Fatalf("EnsureMilestoneEntity: %v", err)
	}
	if !strings.Contains(gotBundlePath, "B-RIGHT") {
		t.Errorf("wrote to %q, want the bundle behind the configured Release field", gotBundlePath)
	}
}

// TestYouTrackZeroConfigKeepsTodaysLiterals is the no-regression guard: an
// existing project, which has no [board.fields] at all, must sync exactly as
// it did before this task.
func TestYouTrackZeroConfigKeepsTodaysLiterals(t *testing.T) {
	c, _ := newTestYTClient(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, `{}`) })
	if got := c.stateField(); got != "State" {
		t.Errorf("stateField() = %q, want State", got)
	}
	if got := c.typeField(); got != "Type" {
		t.Errorf("typeField() = %q, want Type", got)
	}
	if got := c.fixVersionsField(); got != "Fix versions" {
		t.Errorf("fixVersionsField() = %q, want \"Fix versions\"", got)
	}
}

// TestYouTrackReadsStateFromTheConfiguredField pins the read side. A rename
// that moved only the write would leave every issue reading back stateless —
// and t-11's close read-back then cannot tell resolved from unresolved.
func TestYouTrackReadsStateFromTheConfiguredField(t *testing.T) {
	c := newTestYTClientFields(t, Fields{State: "Статус"}, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"idReadable":"PROJ-7","customFields":[{"name":"Статус","value":{"name":"Done"}}]}`)
	})
	iss, err := c.GetIssue("PROJ-7")
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	if iss.State != "Done" {
		t.Errorf("State = %q, want Done read out of the configured field", iss.State)
	}
}

// TestYouTrackGetIssueReadsResolved pins the read-back signal. Without a field
// the tracker fills in, "did the close take?" can only be answered by trusting
// the write — which is exactly the assumption c-5 exists to remove.
func TestYouTrackGetIssueReadsResolved(t *testing.T) {
	cases := []struct {
		name string
		body string
		want bool
	}{
		{"a resolved timestamp", `{"idReadable":"PROJ-7","resolved":1700000000000}`, true},
		{"null", `{"idReadable":"PROJ-7","resolved":null}`, false},
		{"absent", `{"idReadable":"PROJ-7"}`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := newTestYTClient(t, func(w http.ResponseWriter, _ *http.Request) {
				_, _ = io.WriteString(w, tc.body)
			})
			iss, err := c.GetIssue("PROJ-7")
			if err != nil {
				t.Fatalf("GetIssue: %v", err)
			}
			if iss.Resolved != tc.want {
				t.Errorf("Resolved = %v, want %v", iss.Resolved, tc.want)
			}
		})
	}
}

// TestYouTrackCloseIssueResolvesForReal is the c-5 core. CloseIssue used to be
// `return nil`: ship printed "(closed)" for every YouTrack phase while the
// issue stayed open, and nothing could tell the difference.
func TestYouTrackCloseIssueResolvesForReal(t *testing.T) {
	t.Run("writes the mapped state and verifies the read-back", func(t *testing.T) {
		var stateWrite string
		var readBacks int
		c, _ := newTestYTClient(t, func(w http.ResponseWriter, r *http.Request) {
			if r.Method == "GET" {
				readBacks++
				_, _ = io.WriteString(w, `{"idReadable":"PROJ-7","resolved":1700000000000}`)
				return
			}
			var body struct {
				CustomFields []struct {
					Name  string `json:"name"`
					Value struct {
						Name string `json:"name"`
					} `json:"value"`
				} `json:"customFields"`
			}
			raw, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(raw, &body)
			if len(body.CustomFields) > 0 {
				stateWrite = body.CustomFields[0].Value.Name
			}
			_, _ = io.WriteString(w, `{}`)
		})

		if err := c.CloseIssueAs("PROJ-7", "complete", nil); err != nil {
			t.Fatalf("CloseIssueAs: %v", err)
		}
		if stateWrite != "Verified" {
			t.Errorf("wrote State %q, want the mapped Verified — a bare nil write nothing at all", stateWrite)
		}
		if readBacks != 1 {
			t.Errorf("read the issue back %d times, want exactly 1", readBacks)
		}
	})

	t.Run("an accepted write that does not resolve is an error", func(t *testing.T) {
		c, _ := newTestYTClient(t, func(w http.ResponseWriter, r *http.Request) {
			if r.Method == "GET" {
				// The workflow refused the transition; the POST still 200'd.
				_, _ = io.WriteString(w, `{"idReadable":"PROJ-7","resolved":null}`)
				return
			}
			_, _ = io.WriteString(w, `{}`)
		})
		err := c.CloseIssueAs("PROJ-7", "complete", nil)
		if err == nil {
			t.Fatal("a state write that left the issue unresolved returned no error")
		}
		if !strings.Contains(err.Error(), "unresolved") {
			t.Errorf("err = %q, want it to say the issue still reads unresolved", err)
		}
	})

	t.Run("an unmapped status errors instead of silently doing nothing", func(t *testing.T) {
		var writes int
		c, _ := newTestYTClient(t, func(w http.ResponseWriter, r *http.Request) {
			if r.Method == "POST" {
				writes++
			}
			_, _ = io.WriteString(w, `{"idReadable":"PROJ-7","resolved":null}`)
		})
		err := c.CloseIssueAs("PROJ-7", "complete", map[string]string{"complete": ""})
		if err == nil {
			t.Fatal("an unmapped status returned no error — a close that does nothing must not read as success")
		}
		if !strings.Contains(err.Error(), "complete") {
			t.Errorf("err = %q, want it to name the status", err)
		}
		if writes != 0 {
			t.Errorf("wrote state %d times for an unmapped status, want 0", writes)
		}
	})

	t.Run("the BoardClient CloseIssue is no longer a bare nil", func(t *testing.T) {
		var writes int
		c, _ := newTestYTClient(t, func(w http.ResponseWriter, r *http.Request) {
			if r.Method == "POST" {
				writes++
				_, _ = io.WriteString(w, `{}`)
				return
			}
			_, _ = io.WriteString(w, `{"idReadable":"PROJ-7","resolved":1700000000000}`)
		})
		if err := c.CloseIssue("PROJ-7"); err != nil {
			t.Fatalf("CloseIssue: %v", err)
		}
		if writes == 0 {
			t.Error("CloseIssue made no state write at all — that silent nil IS the c-5 bug")
		}
	})
}
