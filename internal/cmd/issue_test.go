package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// boardRepo scaffolds a .dross repo whose [board] points at a forgejo tracker
// at apiBase, with board sync toggled per `enabled`. Token env is set; boardRepo
// does the chdir. Board sync is resolved SOLELY from [board].
func boardRepo(t *testing.T, apiBase string, enabled bool) string {
	t.Helper()
	dir := t.TempDir()
	chdir(t, dir)
	t.Setenv("MOCK_TOKEN", "secret")
	if err := runCmd(t, Init()); err != nil {
		t.Fatalf("init: %v", err)
	}
	mustRunSet(t, "board.provider", "forgejo")
	mustRunSet(t, "board.base_url", apiBase)
	mustRunSet(t, "board.auth_env", "MOCK_TOKEN")
	mustRunSet(t, "board.project", "me/proj")
	if enabled {
		mustRunSet(t, "board.enabled", "true")
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
	if !strings.Contains(mustRead(t, filepath.Join(dir, ".dross", "project.toml")), "enabled = true") {
		t.Error("[board].enabled not set true after enable")
	}
	if err := runCmd(t, Issue(), "disable"); err != nil {
		t.Fatalf("disable: %v", err)
	}
	body := mustRead(t, filepath.Join(dir, ".dross", "project.toml"))
	if strings.Contains(body, "enabled = true") {
		t.Error("[board].enabled still true after disable")
	}
}

// TestOpenBoardResolvesFromBoardBlock proves c-1: board ops resolve their
// client solely from [board], independent of [remote]. A repo with
// [remote].provider=github (no board backend) but an enabled forgejo [board]
// must hit the BOARD server; a disabled [board] is a silent no-op.
func TestOpenBoardResolvesFromBoardBlock(t *testing.T) {
	hit := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit = true
		_, _ = w.Write([]byte(`[]`))
	}))
	t.Cleanup(srv.Close)

	dir := t.TempDir()
	chdir(t, dir)
	t.Setenv("MOCK_TOKEN", "secret")
	if err := runCmd(t, Init()); err != nil {
		t.Fatal(err)
	}
	// [remote] is a code host with no board backend — must NOT drive board ops.
	mustRunSet(t, "remote.provider", "github")
	mustRunSet(t, "remote.url", "https://github.com/me/proj")
	// [board] is the single board source.
	mustRunSet(t, "board.provider", "forgejo")
	mustRunSet(t, "board.base_url", srv.URL)
	mustRunSet(t, "board.auth_env", "MOCK_TOKEN")
	mustRunSet(t, "board.project", "me/proj")
	mustRunSet(t, "board.enabled", "true")

	if err := runCmd(t, Issue(), "pull", "--json"); err != nil {
		t.Fatalf("pull (board enabled): %v", err)
	}
	if !hit {
		t.Error("board op did not hit the [board] server — openBoard must resolve from [board], not [remote]")
	}
	_ = dir

	// Disabled [board] + populated [remote] → silent no-op (no server hit).
	hit = false
	mustRunSet(t, "board.enabled", "false")
	if err := runCmd(t, Issue(), "pull", "--json"); err != nil {
		t.Fatalf("pull (board disabled): %v", err)
	}
	if hit {
		t.Error("disabled [board] must be a no-op, but the board server was hit")
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
	if !strings.Contains(out, "board 12") || !strings.Contains(out, "in-progress") {
		t.Errorf("output = %q (want board 12 + in-progress, one task is done)", out)
	}
	body, _ := createdBody["body"].(string)
	if !strings.Contains(body, "- [x] t1 — schema") || !strings.Contains(body, "- [ ] t2 — handler") {
		t.Errorf("issue body checklist wrong:\n%s", body)
	}
	bj, err := readBoardJSON(dir)
	if err != nil || bj.Phases["01-auth"] != "12" {
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
	if err != nil || bj.Milestones["v0.1"] != "5" {
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
	// Seed board.json with a phase link (#12) and a dismissal (#20), string-keyed.
	writeSpec(t, dir, "01-x", "[phase]\nid=\"01-x\"\ntitle=\"X\"\n")
	mustWrite(t, filepath.Join(dir, ".dross", "board.json"),
		`{"phases":{"01-x":"12"},"quicks":{},"milestones":{},"dismissed":["20"]}`)

	out := captureStdout(t, func() {
		if err := runCmd(t, Issue(), "pull", "--json"); err != nil {
			t.Fatalf("pull: %v", err)
		}
	})
	var got []map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &got); err != nil {
		t.Fatalf("pull --json not valid JSON: %v\n%s", err, out)
	}
	if len(got) != 1 || got[0]["Key"].(string) != "21" {
		t.Errorf("expected only #21 inbound, got %v", got)
	}
}

// youtrackBoardRepo scaffolds a .dross repo whose [board] points at a YouTrack
// instance at apiBase, board sync enabled. Token env is set; does the chdir.
func youtrackBoardRepo(t *testing.T, apiBase string) string {
	t.Helper()
	dir := t.TempDir()
	chdir(t, dir)
	t.Setenv("MOCK_TOKEN", "secret")
	if err := runCmd(t, Init()); err != nil {
		t.Fatalf("init: %v", err)
	}
	mustRunSet(t, "board.provider", "youtrack")
	mustRunSet(t, "board.base_url", apiBase)
	mustRunSet(t, "board.auth_env", "MOCK_TOKEN")
	mustRunSet(t, "board.project", "PROJ")
	mustRunSet(t, "board.enabled", "true")
	return dir
}

// TestIssuePhaseSyncYouTrackCreatesThenUpdates proves c-4 for YouTrack:
// phase-sync creates a YouTrack issue (criteria + task checklist) and links it
// by readable id; a second sync updates that issue, never creating a duplicate.
func TestIssuePhaseSyncYouTrackCreatesThenUpdates(t *testing.T) {
	var creates, updates int
	var createdBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/issues" && r.Method == "POST":
			creates++
			raw, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(raw, &createdBody)
			_, _ = io.WriteString(w, `{"idReadable":"PROJ-7"}`)
		case r.URL.Path == "/api/issues/PROJ-7" && r.Method == "POST":
			updates++
			_, _ = io.WriteString(w, `{"idReadable":"PROJ-7"}`)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	t.Cleanup(srv.Close)

	dir := youtrackBoardRepo(t, srv.URL)
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
	if err := runCmd(t, Issue(), "phase-sync", "01-auth"); err != nil {
		t.Fatalf("phase-sync create: %v", err)
	}
	if creates != 1 {
		t.Errorf("expected 1 create POST to /api/issues, got %d", creates)
	}
	body, _ := createdBody["description"].(string)
	if !strings.Contains(body, "login works") || !strings.Contains(body, "- [x] t1 — schema") || !strings.Contains(body, "- [ ] t2 — handler") {
		t.Errorf("issue description missing criteria/checklist:\n%s", body)
	}
	bj, _ := readBoardJSON(dir)
	if bj.Phases["01-auth"] != "PROJ-7" {
		t.Errorf("phase link = %q, want PROJ-7", bj.Phases["01-auth"])
	}

	// Second sync — link exists, so it updates /api/issues/PROJ-7, no new create.
	if err := runCmd(t, Issue(), "phase-sync", "01-auth", "--status", "in-progress"); err != nil {
		t.Fatalf("phase-sync update: %v", err)
	}
	if creates != 1 {
		t.Errorf("second sync must not create a new issue, creates=%d", creates)
	}
	if updates < 1 {
		t.Errorf("second sync should POST an update to /api/issues/PROJ-7, got %d", updates)
	}
}

// TestIssueMilestoneSyncYouTrack proves c-4: milestone-sync ensures the YouTrack
// milestone entity per milestone_mode (version bundle by default) and links it
// in board.json.
func TestIssueMilestoneSyncYouTrack(t *testing.T) {
	posted := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/customFields") && r.Method == "GET":
			_, _ = io.WriteString(w, `[{"field":{"name":"Fix versions"},"bundle":{"id":"B1","$type":"VersionBundle","values":[]}}]`)
		case strings.Contains(r.URL.Path, "/bundles/version/B1/values") && r.Method == "POST":
			posted = true
			_, _ = io.WriteString(w, `{"name":"v0.1"}`)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	t.Cleanup(srv.Close)

	dir := youtrackBoardRepo(t, srv.URL)
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
	if !posted {
		t.Error("milestone entity not ensured (no version-bundle POST)")
	}
	bj, err := readBoardJSON(dir)
	if err != nil || bj.Milestones["v0.1"] != "v0.1" {
		t.Errorf("milestone entity not linked in board.json: %+v err=%v", bj, err)
	}
}

// TestIssuePullYouTrackFiltersLinkedAndDismissed proves c-3 for YouTrack: pull
// emits only open issues not linked and not dismissed (by readable id), with the
// label filter passed through to the upstream query as a tag clause.
func TestIssuePullYouTrackFiltersLinkedAndDismissed(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query().Get("query")
		// PROJ-12 is linked (a phase), PROJ-20 dismissed, PROJ-21 is new.
		_, _ = io.WriteString(w, `[
			{"idReadable":"PROJ-12","summary":"phase issue"},
			{"idReadable":"PROJ-20","summary":"dismissed one"},
			{"idReadable":"PROJ-21","summary":"a real bug","tags":[{"name":"bug"}]}
		]`)
	}))
	t.Cleanup(srv.Close)

	dir := youtrackBoardRepo(t, srv.URL)
	writeSpec(t, dir, "01-x", "[phase]\nid=\"01-x\"\ntitle=\"X\"\n")
	mustWrite(t, filepath.Join(dir, ".dross", "board.json"),
		`{"phases":{"01-x":"PROJ-12"},"quicks":{},"milestones":{},"dismissed":["PROJ-20"]}`)

	out := captureStdout(t, func() {
		if err := runCmd(t, Issue(), "pull", "--labels", "bug", "--json"); err != nil {
			t.Fatalf("pull: %v", err)
		}
	})
	var got []map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &got); err != nil {
		t.Fatalf("pull --json not valid JSON: %v\n%s", err, out)
	}
	if len(got) != 1 || got[0]["Key"].(string) != "PROJ-21" {
		t.Errorf("expected only PROJ-21 inbound, got %v", got)
	}
	if !strings.Contains(gotQuery, "bug") {
		t.Errorf("upstream YouTrack query %q must carry the bug tag filter", gotQuery)
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
		if n == "42" {
			found = true
		}
	}
	if !found {
		t.Errorf("42 not dismissed: %+v", bj.Dismissed)
	}
}

func TestIssueLinkAdoptsExistingIssue(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	if err := runCmd(t, Init()); err != nil {
		t.Fatal(err)
	}
	if err := runCmd(t, Issue(), "link", "04-rate-limit", "37"); err != nil {
		t.Fatalf("link: %v", err)
	}
	bj, err := readBoardJSON(dir)
	if err != nil || bj.Phases["04-rate-limit"] != "37" {
		t.Errorf("link not stored: %+v err=%v", bj, err)
	}
}

// TestIssueDismissAcceptsReadableID proves the dismiss CLI takes a readable
// string issue id (e.g. a YouTrack "PROJ-300"), not just an integer.
func TestIssueDismissAcceptsReadableID(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	if err := runCmd(t, Init()); err != nil {
		t.Fatal(err)
	}
	if err := runCmd(t, Issue(), "dismiss", "PROJ-300"); err != nil {
		t.Fatalf("dismiss readable id: %v", err)
	}
	bj, err := readBoardJSON(dir)
	if err != nil {
		t.Fatalf("board.json: %v", err)
	}
	found := false
	for _, n := range bj.Dismissed {
		if n == "PROJ-300" {
			found = true
		}
	}
	if !found {
		t.Errorf("PROJ-300 not dismissed: %+v", bj.Dismissed)
	}
}

// TestIssueBacklogSyncYouTrackIdempotent proves c-6: backlog-sync mirrors the
// milestone's unscaffolded slugs + unrouted someday ideas as backlog items
// attached to the milestone entity (Fix versions), idempotently.
func TestIssueBacklogSyncYouTrackIdempotent(t *testing.T) {
	var creates, updates int
	var createBodies []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/customFields") && r.Method == "GET":
			_, _ = io.WriteString(w, `[{"field":{"name":"Fix versions"},"bundle":{"id":"B1","$type":"VersionBundle","values":[]}}]`)
		case strings.Contains(r.URL.Path, "/bundles/version/B1/values") && r.Method == "POST":
			_, _ = io.WriteString(w, `{"name":"v0.1"}`)
		case r.URL.Path == "/api/issues" && r.Method == "POST":
			creates++
			raw, _ := io.ReadAll(r.Body)
			var b map[string]any
			_ = json.Unmarshal(raw, &b)
			createBodies = append(createBodies, b)
			_, _ = io.WriteString(w, fmt.Sprintf(`{"idReadable":"PROJ-%d"}`, 200+creates))
		case strings.HasPrefix(r.URL.Path, "/api/issues/") && r.Method == "POST":
			updates++
			_, _ = io.WriteString(w, `{"idReadable":"PROJ-upd"}`)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	t.Cleanup(srv.Close)

	dir := youtrackBoardRepo(t, srv.URL)
	mustWrite(t, filepath.Join(dir, ".dross", "milestones", "v0.1.toml"), `
phases = ["01-done", "future-x"]

[milestone]
version = "v0.1"
title = "First cut"

[scope]
success_criteria = ["ships"]
`)
	// 01-done is scaffolded (has a phase dir) and carries a someday deferred idea.
	writeSpec(t, dir, "01-done", `
[phase]
id = "01-done"
title = "Done phase"

[[criteria]]
id = "c1"
text = "works"

[[deferred]]
text = "a future idea"
why = "later"
`)

	// First run: future-x (unscaffolded slug) + the someday idea → 2 creates.
	if err := runCmd(t, Issue(), "backlog-sync", "v0.1"); err != nil {
		t.Fatalf("backlog-sync: %v", err)
	}
	if creates != 2 {
		t.Fatalf("expected exactly 2 backlog item creates, got %d", creates)
	}
	for i, b := range createBodies {
		if !hasFixVersion(b, "v0.1") {
			t.Errorf("backlog item %d not attached to milestone entity (Fix versions v0.1): %v", i, b)
		}
	}
	bj, err := readBoardJSON(dir)
	if err != nil || len(bj.Backlog) != 2 {
		t.Fatalf("backlog map should have 2 links: %+v err=%v", bj, err)
	}

	// Second run: same items → 0 new creates, updated by readable-id link.
	if err := runCmd(t, Issue(), "backlog-sync", "v0.1"); err != nil {
		t.Fatalf("backlog-sync rerun: %v", err)
	}
	if creates != 2 {
		t.Errorf("rerun must not create new items, total creates=%d", creates)
	}
	if updates != 2 {
		t.Errorf("rerun should update the 2 linked items, updates=%d", updates)
	}
}

// hasFixVersion reports whether a create body sets the Fix versions field to v.
func hasFixVersion(b map[string]any, v string) bool {
	cfs, _ := b["customFields"].([]any)
	for _, cf := range cfs {
		m, _ := cf.(map[string]any)
		if m["name"] != "Fix versions" {
			continue
		}
		vals, _ := m["value"].([]any)
		for _, val := range vals {
			vm, _ := val.(map[string]any)
			if vm["name"] == v {
				return true
			}
		}
	}
	return false
}

// readBoardJSON decodes .dross/board.json for assertions.
func readBoardJSON(dir string) (*struct {
	Milestones map[string]string `json:"milestones"`
	Phases     map[string]string `json:"phases"`
	Quicks     map[string]string `json:"quicks"`
	Backlog    map[string]string `json:"backlog"`
	Dismissed  []string          `json:"dismissed"`
}, error) {
	b, err := os.ReadFile(filepath.Join(dir, ".dross", "board.json"))
	if err != nil {
		return nil, err
	}
	var out struct {
		Milestones map[string]string `json:"milestones"`
		Phases     map[string]string `json:"phases"`
		Quicks     map[string]string `json:"quicks"`
		Backlog    map[string]string `json:"backlog"`
		Dismissed  []string          `json:"dismissed"`
	}
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
