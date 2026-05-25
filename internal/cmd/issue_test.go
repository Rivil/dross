package cmd

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// boardRepo scaffolds a .dross repo wired to a forge at apiBase, with board
// sync toggled per `enabled`. Token env is set. Caller has already chdir'd
// nowhere — boardRepo does the chdir.
func boardRepo(t *testing.T, apiBase string, enabled bool) string {
	t.Helper()
	dir := t.TempDir()
	chdir(t, dir)
	t.Setenv("MOCK_TOKEN", "secret")
	if err := runCmd(t, Init()); err != nil {
		t.Fatalf("init: %v", err)
	}
	mustRunSet(t, "remote.provider", "forgejo")
	mustRunSet(t, "remote.url", "https://forge.example/me/proj")
	mustRunSet(t, "remote.api_base", apiBase)
	mustRunSet(t, "remote.auth_env", "MOCK_TOKEN")
	if enabled {
		mustRunSet(t, "remote.board_sync", "true")
	}
	return dir
}

func writeSpec(t *testing.T, dir, phaseID, body string) {
	t.Helper()
	mustWrite(t, filepath.Join(dir, ".dross", "phases", phaseID, "spec.toml"), body)
}

func writePlan(t *testing.T, dir, phaseID, body string) {
	t.Helper()
	mustWrite(t, filepath.Join(dir, ".dross", "phases", phaseID, "plan.toml"), body)
}

func TestIssueEnableDisableTogglesConfig(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	if err := runCmd(t, Init()); err != nil {
		t.Fatal(err)
	}
	if err := runCmd(t, Issue(), "enable"); err != nil {
		t.Fatalf("enable: %v", err)
	}
	if !strings.Contains(mustRead(t, filepath.Join(dir, ".dross", "project.toml")), "board_sync = true") {
		t.Error("board_sync not set true after enable")
	}
	if err := runCmd(t, Issue(), "disable"); err != nil {
		t.Fatalf("disable: %v", err)
	}
	body := mustRead(t, filepath.Join(dir, ".dross", "project.toml"))
	if strings.Contains(body, "board_sync = true") {
		t.Error("board_sync still true after disable")
	}
}

func TestIssuePhaseSyncNoOpWhenDisabled(t *testing.T) {
	// Point at a server that fails the test if touched.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("board API must not be called when sync is disabled: %s %s", r.Method, r.URL.Path)
	}))
	t.Cleanup(srv.Close)

	dir := boardRepo(t, srv.URL, false) // disabled
	writeSpec(t, dir, "01-auth", "[phase]\nid=\"01-auth\"\ntitle=\"Auth\"\n")

	if err := runCmd(t, Issue(), "phase-sync", "01-auth"); err != nil {
		t.Fatalf("phase-sync should no-op (nil) when disabled: %v", err)
	}
	// No board.json should have been written.
	if _, err := readBoardJSON(dir); err == nil {
		t.Error("board.json should not exist after a disabled no-op")
	}
}

func TestIssuePhaseSyncCreatesThenUpdates(t *testing.T) {
	var (
		issuePosts  int
		issuePatch  int
		labelPut    int
		createdBody map[string]any
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/labels") && r.Method == "GET":
			_, _ = w.Write([]byte(`[{"id":1,"name":"dross"}]`))
		case strings.HasSuffix(r.URL.Path, "/labels") && r.Method == "POST":
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":2}`))
		case strings.HasSuffix(r.URL.Path, "/issues") && r.Method == "POST":
			issuePosts++
			b, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(b, &createdBody)
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"number":12,"html_url":"https://forge.example/me/proj/issues/12","state":"open"}`))
		case strings.Contains(r.URL.Path, "/issues/12/labels") && r.Method == "PUT":
			labelPut++
			_, _ = w.Write([]byte(`{"number":12}`))
		case strings.HasSuffix(r.URL.Path, "/issues/12") && r.Method == "PATCH":
			issuePatch++
			_, _ = w.Write([]byte(`{"number":12}`))
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	t.Cleanup(srv.Close)

	dir := boardRepo(t, srv.URL, true)
	writeSpec(t, dir, "01-auth", `
[phase]
id = "01-auth"
title = "Auth middleware"

[[criteria]]
id = "c1"
text = "login works"
`)
	writePlan(t, dir, "01-auth", `
[phase]
id = "01-auth"

[[task]]
id = "t1"
title = "schema"
wave = 1
status = "done"

[[task]]
id = "t2"
title = "handler"
wave = 1
`)

	// First sync — creates the issue.
	out := captureStdout(t, func() {
		if err := runCmd(t, Issue(), "phase-sync", "01-auth"); err != nil {
			t.Fatalf("phase-sync create: %v", err)
		}
	})
	if issuePosts != 1 {
		t.Errorf("expected 1 issue POST, got %d", issuePosts)
	}
	if !strings.Contains(out, "#12") || !strings.Contains(out, "in-progress") {
		t.Errorf("output = %q (want #12 + in-progress, one task is done)", out)
	}
	body, _ := createdBody["body"].(string)
	if !strings.Contains(body, "- [x] t1 — schema") || !strings.Contains(body, "- [ ] t2 — handler") {
		t.Errorf("issue body checklist wrong:\n%s", body)
	}
	bj, err := readBoardJSON(dir)
	if err != nil || bj.Phases["01-auth"] != 12 {
		t.Errorf("board link not stored: %+v err=%v", bj, err)
	}

	// Second sync — link exists, so it PATCHes + PUTs labels, no new POST.
	if err := runCmd(t, Issue(), "phase-sync", "01-auth", "--status", "verifying"); err != nil {
		t.Fatalf("phase-sync update: %v", err)
	}
	if issuePosts != 1 {
		t.Errorf("second sync should not POST a new issue, posts=%d", issuePosts)
	}
	if issuePatch != 1 || labelPut != 1 {
		t.Errorf("update should PATCH+PUT once each, patch=%d put=%d", issuePatch, labelPut)
	}
}

func TestIssueMilestoneSyncCreatesAndLinks(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case "GET": // list milestones
			_, _ = w.Write([]byte(`[]`))
		case "POST": // create
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":5}`))
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	t.Cleanup(srv.Close)

	dir := boardRepo(t, srv.URL, true)
	mustWrite(t, filepath.Join(dir, ".dross", "milestones", "v0.1.toml"), `
[milestone]
version = "v0.1"
title = "First cut"

[scope]
success_criteria = ["ships"]
`)
	if err := runCmd(t, Issue(), "milestone-sync", "v0.1"); err != nil {
		t.Fatalf("milestone-sync: %v", err)
	}
	bj, err := readBoardJSON(dir)
	if err != nil || bj.Milestones["v0.1"] != 5 {
		t.Errorf("milestone link not stored: %+v err=%v", bj, err)
	}
}

func TestIssuePullFiltersLinkedAndDismissed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// list issues: #12 is linked (a phase), #20 dismissed, #21 is new.
		_, _ = w.Write([]byte(`[
			{"number":12,"title":"phase issue","state":"open"},
			{"number":20,"title":"dismissed one","state":"open"},
			{"number":21,"title":"a real bug","state":"open","labels":[{"name":"bug"}]}
		]`))
	}))
	t.Cleanup(srv.Close)

	dir := boardRepo(t, srv.URL, true)
	// Seed board.json with a phase link (#12) and a dismissal (#20).
	writeSpec(t, dir, "01-x", "[phase]\nid=\"01-x\"\ntitle=\"X\"\n")
	mustWrite(t, filepath.Join(dir, ".dross", "board.json"),
		`{"phases":{"01-x":12},"quicks":{},"milestones":{},"dismissed":[20]}`)

	out := captureStdout(t, func() {
		if err := runCmd(t, Issue(), "pull", "--json"); err != nil {
			t.Fatalf("pull: %v", err)
		}
	})
	var got []map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &got); err != nil {
		t.Fatalf("pull --json not valid JSON: %v\n%s", err, out)
	}
	if len(got) != 1 || got[0]["Number"].(float64) != 21 {
		t.Errorf("expected only #21 inbound, got %v", got)
	}
}

func TestIssueDismissPersists(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	if err := runCmd(t, Init()); err != nil {
		t.Fatal(err)
	}
	if err := runCmd(t, Issue(), "dismiss", "42"); err != nil {
		t.Fatalf("dismiss: %v", err)
	}
	bj, err := readBoardJSON(dir)
	if err != nil {
		t.Fatalf("board.json: %v", err)
	}
	found := false
	for _, n := range bj.Dismissed {
		if n == 42 {
			found = true
		}
	}
	if !found {
		t.Errorf("42 not dismissed: %+v", bj.Dismissed)
	}
}

func TestIssueDismissRejectsNonInteger(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	if err := runCmd(t, Init()); err != nil {
		t.Fatal(err)
	}
	if err := runCmd(t, Issue(), "dismiss", "abc"); err == nil {
		t.Fatal("expected error for non-integer issue number")
	}
}

// readBoardJSON decodes .dross/board.json for assertions.
func readBoardJSON(dir string) (*struct {
	Milestones map[string]int `json:"milestones"`
	Phases     map[string]int `json:"phases"`
	Quicks     map[string]int `json:"quicks"`
	Dismissed  []int          `json:"dismissed"`
}, error) {
	b, err := os.ReadFile(filepath.Join(dir, ".dross", "board.json"))
	if err != nil {
		return nil, err
	}
	var out struct {
		Milestones map[string]int `json:"milestones"`
		Phases     map[string]int `json:"phases"`
		Quicks     map[string]int `json:"quicks"`
		Dismissed  []int          `json:"dismissed"`
	}
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
