package cmd

import (
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

const routedItemSpec = `
[[deferred]]
id = "abc123"
text = "routed idea"
target = "host"
`

// TestRoutedItemLinksToItsTargetPhaseIssue is c-7's happy path: the target
// phase already has an issue, so the routed item's issue is related to it.
func TestRoutedItemLinksToItsTargetPhaseIssue(t *testing.T) {
	f := newFakeBoard(t)
	dir := routedRepo(t, f.srv.URL, routedItemSpec)

	// The target phase's issue, already on the tracker and in the cache.
	f.issues["PROJ-9"] = "host — Host"
	f.tags["PROJ-9"] = []string{"dross", "dross/phase:host"}
	mustWrite(t, filepath.Join(dir, ".dross", "board.json"),
		`{"phases":{"host":"PROJ-9"},"quicks":{},"milestones":{},"backlog":{},"dismissed":[]}`)

	if err := runCmd(t, Issue(), "backlog-sync", "v0.1"); err != nil {
		t.Fatalf("backlog-sync: %v", err)
	}
	if len(f.links) != 1 {
		t.Fatalf("links = %+v, want exactly one", f.links)
	}
	itemKey := issueCarrying(t, f, "routed idea")
	if f.links[0].from != itemKey || f.links[0].to != "PROJ-9" {
		t.Errorf("link = %+v, want %s -> PROJ-9", f.links[0], itemKey)
	}
}

// TestRoutedItemWithoutATargetIssueWarnsAndContinues: erroring would fail a
// sync over an enrichment; silence would hide that the link is still owed.
func TestRoutedItemWithoutATargetIssueWarnsAndContinues(t *testing.T) {
	f := newFakeBoard(t)
	routedRepo(t, f.srv.URL, routedItemSpec)

	var err error
	warn := captureStderr(t, func() {
		_ = captureStdout(t, func() {
			err = runCmd(t, Issue(), "backlog-sync", "v0.1")
		})
	})
	if err != nil {
		t.Fatalf("a missing target issue must not fail the sync, got %v", err)
	}
	if len(f.links) != 0 {
		t.Errorf("links = %+v, want none — there is nothing to link to yet", f.links)
	}
	if !strings.Contains(warn, "host") {
		t.Errorf("stderr = %q, want the target slug named", warn)
	}
	itemKey := issueCarrying(t, f, "routed idea")
	if !slicesHas(tagsOn(f, itemKey), "dross/target:host") {
		t.Errorf("tags = %v, want the target label kept as the relationship", tagsOn(f, itemKey))
	}
}

// TestRoutedItemLinksOnALaterSync is c-7's "established on a later sync"
// clause. It cannot pass unless the warn path leaves the item relinkable.
func TestRoutedItemLinksOnALaterSync(t *testing.T) {
	f := newFakeBoard(t)
	dir := routedRepo(t, f.srv.URL, routedItemSpec)

	_ = captureStderr(t, func() {
		_ = captureStdout(t, func() {
			if err := runCmd(t, Issue(), "backlog-sync", "v0.1"); err != nil {
				t.Fatalf("first sync: %v", err)
			}
		})
	})
	if len(f.links) != 0 {
		t.Fatalf("first sync linked %+v, want nothing yet", f.links)
	}

	// The target phase gets its issue.
	f.issues["PROJ-9"] = "host — Host"
	f.tags["PROJ-9"] = []string{"dross", "dross/phase:host"}
	mustWrite(t, filepath.Join(dir, ".dross", "board.json"),
		`{"phases":{"host":"PROJ-9"},"quicks":{},"milestones":{},"backlog":{},"dismissed":[]}`)

	if err := runCmd(t, Issue(), "backlog-sync", "v0.1"); err != nil {
		t.Fatalf("second sync: %v", err)
	}
	if len(f.links) != 1 || f.links[0].to != "PROJ-9" {
		t.Errorf("links = %+v, want exactly one to PROJ-9 once the target exists", f.links)
	}
}

// TestGitHubBoardRecordsNoLinkAttempts: the capability check must be an
// interface assertion, not a provider-string switch — a no-op stub satisfying
// IssueLinker would silence this whole arm.
func TestGitHubBoardRecordsNoLinkAttempts(t *testing.T) {
	var linkAttempts int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "issueLink") || r.URL.Path == "/api/commands":
			linkAttempts++
			_, _ = io.WriteString(w, `{}`)
		case strings.HasSuffix(r.URL.Path, "/labels"):
			_, _ = io.WriteString(w, `[{"name":"dross"}]`)
		case strings.HasSuffix(r.URL.Path, "/milestones") && r.Method == "GET":
			_, _ = io.WriteString(w, `[]`)
		case strings.HasSuffix(r.URL.Path, "/milestones") && r.Method == "POST":
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, `{"number":5}`)
		case strings.HasSuffix(r.URL.Path, "/issues") && r.Method == "GET":
			_, _ = io.WriteString(w, `[]`)
		case strings.HasSuffix(r.URL.Path, "/issues") && r.Method == "POST":
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, `{"number":42,"node_id":"I_kw","state":"open"}`)
		default:
			_, _ = io.WriteString(w, `{"number":42,"labels":[{"name":"dross"}]}`)
		}
	}))
	t.Cleanup(srv.Close)

	dir := t.TempDir()
	chdir(t, dir)
	t.Setenv("MOCK_TOKEN", "secret")
	if err := runCmd(t, Init()); err != nil {
		t.Fatalf("init: %v", err)
	}
	mustRunSet(t, "remote.url", srv.URL)
	mustRunSet(t, "board.provider", "github")
	mustRunSet(t, "board.base_url", srv.URL)
	mustRunSet(t, "board.auth_env", "MOCK_TOKEN")
	mustRunSet(t, "board.project", "octo/repo")
	mustRunSet(t, "board.enabled", "true")

	mustWrite(t, filepath.Join(dir, ".dross", "milestones", "v0.1.toml"), `
phases = ["host"]

[milestone]
version = "v0.1"
title = "First cut"

[scope]
success_criteria = ["ships"]
`)
	writeSpec(t, dir, "host", `
[phase]
id = "host"
title = "Host"

[[criteria]]
id = "c1"
text = "works"
`+routedItemSpec)

	var err error
	warn := captureStderr(t, func() {
		_ = captureStdout(t, func() {
			err = runCmd(t, Issue(), "backlog-sync", "v0.1")
		})
	})
	if err != nil {
		t.Fatalf("a provider without links must not fail the sync, got %v", err)
	}
	if linkAttempts != 0 {
		t.Errorf("a GitHub board made %d link attempt(s), want 0", linkAttempts)
	}
	if strings.Count(warn, "cannot link issues") != 1 {
		t.Errorf("stderr = %q, want exactly one capability warning per run", warn)
	}
}

// TestLinkOutageDoesNotFailTheBacklogSync: the link call 500s. The items are
// the deliverable; the link is an enrichment, so the sync still exits 0 with
// the issue created and labelled.
func TestLinkOutageDoesNotFailTheBacklogSync(t *testing.T) {
	f := newFakeBoard(t)
	// Intercept the link command with a 500.
	base := f.srv.Config.Handler
	f.srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/commands" {
			w.WriteHeader(500)
			return
		}
		base.ServeHTTP(w, r)
	})

	dir := routedRepo(t, f.srv.URL, routedItemSpec)
	f.issues["PROJ-9"] = "host — Host"
	f.tags["PROJ-9"] = []string{"dross", "dross/phase:host"}
	mustWrite(t, filepath.Join(dir, ".dross", "board.json"),
		`{"phases":{"host":"PROJ-9"},"quicks":{},"milestones":{},"backlog":{},"dismissed":[]}`)

	var err error
	_ = captureStderr(t, func() {
		_ = captureStdout(t, func() {
			err = runCmd(t, Issue(), "backlog-sync", "v0.1")
		})
	})
	if err != nil {
		t.Fatalf("a failing link must not fail the sync, got %v", err)
	}
	itemKey := issueCarrying(t, f, "routed idea")
	if !slicesHas(tagsOn(f, itemKey), "dross/target:host") {
		t.Errorf("tags = %v, want the item still created and labelled", tagsOn(f, itemKey))
	}
}

// TestNoLinkTypeWarnsAndContinues is the capability-gap arm, against Jira —
// the only backend that can report ErrNoLinkType. An instance with no link
// types must warn and continue, not abort the whole backlog sync.
func TestNoLinkTypeWarnsAndContinues(t *testing.T) {
	var linkPosts, itemCreates int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/issueLinkType"):
			w.WriteHeader(404) // this instance cannot express links
		case strings.HasSuffix(r.URL.Path, "/issueLink") && r.Method == "POST":
			linkPosts++
			w.WriteHeader(http.StatusCreated)
		case r.URL.Path == "/rest/api/3/label":
			_, _ = io.WriteString(w, `{"values":[]}`)
		case strings.HasSuffix(r.URL.Path, "/search/jql"):
			_, _ = io.WriteString(w, `{"issues":[]}`)
		case r.URL.Path == "/rest/api/3/project/PROJ" && r.Method == "GET":
			_, _ = io.WriteString(w, `{"id":"10000","versions":[{"id":"10001","name":"v0.1"}]}`)
		case r.URL.Path == "/rest/api/3/issue" && r.Method == "POST":
			itemCreates++
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, `{"id":"1000","key":"PROJ-1"}`)
		default:
			_, _ = io.WriteString(w, `{"key":"PROJ-9","fields":{"summary":"host — Host","labels":["dross"],"issuelinks":[]}}`)
		}
	}))
	t.Cleanup(srv.Close)

	dir := jiraBoardRepo(t, srv.URL)
	mustWrite(t, filepath.Join(dir, ".dross", "milestones", "v0.1.toml"), `
phases = ["host"]

[milestone]
version = "v0.1"
title = "First cut"

[scope]
success_criteria = ["ships"]
`)
	writeSpec(t, dir, "host", `
[phase]
id = "host"
title = "Host"

[[criteria]]
id = "c1"
text = "works"
`+routedItemSpec)
	mustWrite(t, filepath.Join(dir, ".dross", "board.json"),
		`{"phases":{"host":"PROJ-9"},"quicks":{},"milestones":{},"backlog":{},"dismissed":[]}`)

	var err error
	warn := captureStderr(t, func() {
		_ = captureStdout(t, func() {
			err = runCmd(t, Issue(), "backlog-sync", "v0.1")
		})
	})
	if err != nil {
		t.Fatalf("an instance with no link type must not abort the sync, got %v", err)
	}
	if linkPosts != 0 {
		t.Errorf("issued %d link POSTs against an instance with no link type, want 0", linkPosts)
	}
	if itemCreates == 0 {
		t.Error("the backlog item was never created — the capability gap aborted real work")
	}
	// The specific wording, not just "link": passing on a generic HTTP failure
	// would mean the ErrNoLinkType arm is never actually exercised.
	if !strings.Contains(warn, "no issue-link type") {
		t.Errorf("stderr = %q, want the ErrNoLinkType capability gap surfaced", warn)
	}
}
