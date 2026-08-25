package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// backlogCloseFake is a YouTrack stand-in that additionally models resolution:
// each issue has a resolved flag, a State write flips it, and every close is
// counted. newFakeBoard models summaries and tags but has no notion of an issue
// being done, which is the whole subject here.
type backlogCloseFake struct {
	mu       sync.Mutex
	t        *testing.T
	srv      *httptest.Server
	summary  map[string]string   // key -> summary
	tags     map[string][]string // key -> tag names
	resolved map[string]bool
	// refuse names issues whose State write is accepted and changes nothing —
	// the shape a workflow that forbids the transition has.
	refuse  map[string]bool
	closes  map[string]int
	nextNum int
}

func newBacklogCloseFake(t *testing.T) *backlogCloseFake {
	t.Helper()
	f := &backlogCloseFake{
		t:        t,
		summary:  map[string]string{},
		tags:     map[string][]string{},
		resolved: map[string]bool{},
		refuse:   map[string]bool{},
		closes:   map[string]int{},
		nextNum:  200,
	}
	f.srv = httptest.NewServer(http.HandlerFunc(f.handle))
	t.Cleanup(f.srv.Close)
	return f
}

// seed puts an issue on the board directly — the state a previous sync left.
func (f *backlogCloseFake) seed(key, summary string, tags ...string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.summary[key] = summary
	f.tags[key] = tags
}

func (f *backlogCloseFake) closeCount(key string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.closes[key]
}

func (f *backlogCloseFake) totalCloses() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.closes {
		n += c
	}
	return n
}

func (f *backlogCloseFake) keyBySummary(summary string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	for k, s := range f.summary {
		if s == summary {
			return k
		}
	}
	return ""
}

func (f *backlogCloseFake) body(key string) string {
	var tagJSON []string
	for _, n := range f.tags[key] {
		tagJSON = append(tagJSON, fmt.Sprintf(`{"id":"tid-%s","name":%q}`, n, n))
	}
	res := "null"
	if f.resolved[key] {
		res = "1700000000000"
	}
	return fmt.Sprintf(`{"idReadable":%q,"summary":%q,"resolved":%s,"tags":[%s]}`,
		key, f.summary[key], res, strings.Join(tagJSON, ","))
}

func (f *backlogCloseFake) handle(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	path := r.URL.Path

	switch {
	case strings.HasSuffix(path, "/customFields") && r.Method == "GET":
		_, _ = io.WriteString(w, `[{"field":{"name":"Fix versions"},"bundle":{"id":"B1","$type":"VersionBundle","values":[]}}]`)
	case strings.Contains(path, "/bundles/version/B1/values") && r.Method == "POST":
		_, _ = io.WriteString(w, `{"name":"v0.1"}`)

	case path == "/api/issueTags" && r.Method == "GET":
		seen := map[string]bool{}
		var out []string
		for _, tags := range f.tags {
			for _, n := range tags {
				if n != "" && !seen[n] {
					seen[n] = true
					out = append(out, fmt.Sprintf(`{"id":"tid-%s","name":%q}`, n, n))
				}
			}
		}
		_, _ = io.WriteString(w, "["+strings.Join(out, ",")+"]")
	case path == "/api/issueTags" && r.Method == "POST":
		var b struct {
			Name string `json:"name"`
		}
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &b)
		_, _ = io.WriteString(w, fmt.Sprintf(`{"id":"tid-%s"}`, b.Name))
	case strings.HasSuffix(path, "/tags") && r.Method == "POST":
		key := strings.TrimSuffix(strings.TrimPrefix(path, "/api/issues/"), "/tags")
		var b struct {
			ID string `json:"id"`
		}
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &b)
		f.tags[key] = append(f.tags[key], strings.TrimPrefix(b.ID, "tid-"))
		_, _ = io.WriteString(w, `{}`)
	case strings.Contains(path, "/tags/") && r.Method == "DELETE":
		_, _ = io.WriteString(w, `{}`)

	case path == "/api/issues" && r.Method == "POST":
		f.nextNum++
		key := fmt.Sprintf("PROJ-%d", f.nextNum)
		var b map[string]any
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &b)
		f.summary[key], _ = b["summary"].(string)
		_, _ = io.WriteString(w, f.body(key))

	case path == "/api/issues" && r.Method == "GET":
		query := r.URL.Query().Get("query")
		var out []string
		for key := range f.summary {
			for _, n := range f.tags[key] {
				if n != "" && strings.Contains(query, n) {
					out = append(out, f.body(key))
					break
				}
			}
		}
		_, _ = io.WriteString(w, "["+strings.Join(out, ",")+"]")

	case strings.HasPrefix(path, "/api/issues/") && r.Method == "GET":
		key := strings.TrimPrefix(path, "/api/issues/")
		if _, ok := f.summary[key]; !ok {
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, `{"error":"not found"}`)
			return
		}
		_, _ = io.WriteString(w, f.body(key))

	case strings.HasPrefix(path, "/api/issues/") && r.Method == "POST":
		key := strings.TrimPrefix(path, "/api/issues/")
		var b map[string]any
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &b)
		if _, isState := b["customFields"]; isState {
			f.closes[key]++
			if !f.refuse[key] {
				f.resolved[key] = true
			}
		}
		if summary, ok := b["summary"].(string); ok {
			f.summary[key] = summary
		}
		_, _ = io.WriteString(w, f.body(key))

	case path == "/api/commands" && r.Method == "POST":
		_, _ = io.WriteString(w, `{}`)

	default:
		f.t.Errorf("unexpected %s %s", r.Method, path)
		_, _ = io.WriteString(w, `{}`)
	}
}

// backlogCloseRepo builds a milestone with the given roadmap slugs and a host
// phase carrying the given deferred TOML blocks.
func backlogCloseRepo(t *testing.T, apiBase string, slugs []string, deferredTOML string) string {
	t.Helper()
	dir := youtrackBoardRepo(t, apiBase)
	quoted := make([]string, 0, len(slugs))
	for _, s := range slugs {
		quoted = append(quoted, fmt.Sprintf("%q", s))
	}
	mustWrite(t, filepath.Join(dir, ".dross", "milestones", "v0.1.toml"), fmt.Sprintf(`
phases = [%s]

[milestone]
version = "v0.1"
title = "First cut"

[scope]
success_criteria = ["ships"]
`, strings.Join(quoted, ", ")))
	writeSpec(t, dir, "host", `
[phase]
id = "host"
title = "Host"
milestone = "v0.1"

[[criteria]]
id = "c1"
text = "works"
`+deferredTOML)
	return dir
}

// scaffoldPhase creates a phase directory, which is the proof a roadmap slug
// stopped being backlog and became real work.
func scaffoldBacklogPhase(t *testing.T, dir, slug string) {
	t.Helper()
	writeSpec(t, dir, slug, fmt.Sprintf("[phase]\nid = %q\ntitle = %q\nmilestone = \"v0.1\"\n", slug, slug))
}

// TestBacklogClosesScaffoldedSlugOnly is c-6's first half. A roadmap slug that
// has been scaffolded is no longer backlog — its own phase issue tracks it now —
// so its backlog mirror must resolve. A slug still waiting keeps its mirror.
func TestBacklogClosesScaffoldedSlugOnly(t *testing.T) {
	f := newBacklogCloseFake(t)
	dir := backlogCloseRepo(t, f.srv.URL, []string{"grown-up", "still-waiting"}, "")

	// First sync while both slugs are backlog.
	if err := runCmd(t, Issue(), "backlog", "sync", "v0.1"); err != nil {
		t.Fatalf("first sync: %v", err)
	}
	grown := f.keyBySummary("[backlog] grown-up")
	waiting := f.keyBySummary("[backlog] still-waiting")
	if grown == "" || waiting == "" {
		t.Fatalf("both slugs should have mirrors: %v", f.summary)
	}

	// One of them gets scaffolded, and the phase ships.
	scaffoldBacklogPhase(t, dir, "grown-up")

	_ = captureStdout(t, func() {
		_ = captureStderr(t, func() {
			if err := runCmd(t, Issue(), "backlog", "sync", "v0.1"); err != nil {
				t.Fatalf("reconcile sync: %v", err)
			}
		})
	})
	if got := f.closeCount(grown); got != 1 {
		t.Errorf("scaffolded slug's mirror closes = %d, want 1", got)
	}
	if got := f.closeCount(waiting); got != 0 {
		t.Errorf("an unscaffolded slug's mirror was closed (%d times) — that work has not happened", got)
	}
	bd := loadBoardFile(t, dir)
	if _, ok := bd.BacklogID("slug:grown-up"); ok {
		t.Error("the closed mirror's board.json key should be dropped after a verified close")
	}
	if _, ok := bd.BacklogID("slug:still-waiting"); !ok {
		t.Error("a live backlog item must keep its link")
	}

	// Idempotence: a third run has nothing left to close.
	before := f.totalCloses()
	_ = captureStdout(t, func() {
		_ = captureStderr(t, func() {
			if err := runCmd(t, Issue(), "backlog", "sync", "v0.1"); err != nil {
				t.Fatalf("third sync: %v", err)
			}
		})
	})
	if got := f.totalCloses() - before; got != 0 {
		t.Errorf("a repeat reconcile issued %d further closes, want 0", got)
	}
}

// TestRoutedBacklogClosesOnlyWhenTargetResolved is c-6's other half. A routed
// item's work lands in its TARGET phase, so the target's own issue is the only
// honest signal that the idea is done. The two cases differ in nothing but that
// read-back.
func TestRoutedBacklogClosesOnlyWhenTargetResolved(t *testing.T) {
	for _, tc := range []struct {
		name           string
		targetResolved bool
		wantCloses     int
	}{
		{"target shipped", true, 1},
		{"target still open", false, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newBacklogCloseFake(t)
			dir := backlogCloseRepo(t, f.srv.URL, nil, `
[[deferred]]
text = "routed idea"
target = "destination"
`)
			// The target phase and its board issue already exist.
			scaffoldBacklogPhase(t, dir, "destination")
			f.seed("PROJ-900", "destination — Destination", labelMarker, phaseLabel("destination"))
			f.mu.Lock()
			f.resolved["PROJ-900"] = tc.targetResolved
			f.mu.Unlock()
			mustWrite(t, filepath.Join(dir, ".dross", "board.json"),
				`{"phases":{"destination":"PROJ-900"},"quicks":{},"milestones":{}}`)

			_ = captureStdout(t, func() {
				_ = captureStderr(t, func() {
					if err := runCmd(t, Issue(), "backlog", "sync", "v0.1"); err != nil {
						t.Fatalf("backlog-sync: %v", err)
					}
				})
			})
			mirror := f.keyBySummary("[routed] routed idea")
			if mirror == "" {
				t.Fatalf("the routed item was never mirrored: %v", f.summary)
			}
			if got := f.closeCount(mirror); got != tc.wantCloses {
				t.Errorf("routed mirror closes = %d, want %d", got, tc.wantCloses)
			}

			// A routed item stays in the LIVE set after its target ships — it
			// is still a backlog entry — so its board.json key is kept and the
			// next sync sees it again. Idempotence therefore rests on reading
			// the mirror back rather than on the key going away, and this
			// second run is the only place that distinction is visible.
			_ = captureStdout(t, func() {
				_ = captureStderr(t, func() {
					if err := runCmd(t, Issue(), "backlog", "sync", "v0.1"); err != nil {
						t.Fatalf("second backlog-sync: %v", err)
					}
				})
			})
			if got := f.closeCount(mirror); got != tc.wantCloses {
				t.Errorf("after a second run, routed mirror closes = %d, want %d — the reconcile is not idempotent", got, tc.wantCloses)
			}
		})
	}
}

// TestDismissedBacklogItemIsClosedAndUnlinked: dismissing an idea is a decision
// someone made, not a loose end. The mirror follows it out of the live set the
// same way a scaffolded slug does.
func TestDismissedBacklogItemIsClosedAndUnlinked(t *testing.T) {
	f := newBacklogCloseFake(t)
	dir := backlogCloseRepo(t, f.srv.URL, nil, `
[[deferred]]
text = "an idea"
`)
	if err := runCmd(t, Issue(), "backlog", "sync", "v0.1"); err != nil {
		t.Fatalf("first sync: %v", err)
	}
	mirror := f.keyBySummary("[someday] an idea")
	if mirror == "" {
		t.Fatalf("the idea was never mirrored: %v", f.summary)
	}
	bd := loadBoardFile(t, dir)
	var key string
	for _, k := range bd.BacklogKeys() {
		if id, _ := bd.BacklogID(k); id == mirror {
			key = k
		}
	}
	if key == "" {
		t.Fatal("no board.json key recorded for the mirrored idea")
	}

	// Dismiss it at source.
	specPath := filepath.Join(dir, ".dross", "phases", "host", "spec.toml")
	mustWrite(t, specPath, mustRead(t, specPath)+"\ndismissed = true\n")

	_ = captureStdout(t, func() {
		_ = captureStderr(t, func() {
			if err := runCmd(t, Issue(), "backlog", "sync", "v0.1"); err != nil {
				t.Fatalf("reconcile sync: %v", err)
			}
		})
	})
	if got := f.closeCount(mirror); got != 1 {
		t.Errorf("dismissed item's mirror closes = %d, want 1", got)
	}
	if _, ok := loadBoardFile(t, dir).BacklogID(key); ok {
		t.Error("a closed mirror's key should be dropped")
	}
}

// TestUnattributableBacklogKeyIsNeverClosed is the guard against closing by
// set-difference. A recorded key this milestone's live set does not explain is
// not thereby finished — it may belong to another milestone, or to a slug
// someone renamed. Closing it would resolve work nobody did.
func TestUnattributableBacklogKeyIsNeverClosed(t *testing.T) {
	f := newBacklogCloseFake(t)
	dir := backlogCloseRepo(t, f.srv.URL, []string{"still-waiting"}, "")
	f.seed("PROJ-901", "[backlog] someone-elses-slug", labelMarker)
	mustWrite(t, filepath.Join(dir, ".dross", "board.json"),
		`{"phases":{},"quicks":{},"milestones":{},"backlog":{"slug:someone-elses-slug":"PROJ-901"}}`)

	var errOut string
	_ = captureStdout(t, func() {
		errOut = captureStderr(t, func() {
			if err := runCmd(t, Issue(), "backlog", "sync", "v0.1"); err != nil {
				t.Fatalf("backlog-sync: %v", err)
			}
		})
	})
	if got := f.closeCount("PROJ-901"); got != 0 {
		t.Errorf("an unattributable mirror was closed %d time(s) — nothing shows that work is done", got)
	}
	if !strings.Contains(errOut, "PROJ-901") {
		t.Errorf("stderr = %q, want the unattributable mirror named rather than silently skipped", errOut)
	}
	if _, ok := loadBoardFile(t, dir).BacklogID("slug:someone-elses-slug"); !ok {
		t.Error("an unattributable key must be kept — dropping it would orphan a live issue")
	}
}

// TestFailedBacklogCloseKeepsTheLink: a mirror whose close the tracker refuses
// must keep its board.json key. Unlinked AND open is the one unrecoverable
// state — no later run could find it again.
func TestFailedBacklogCloseKeepsTheLink(t *testing.T) {
	f := newBacklogCloseFake(t)
	dir := backlogCloseRepo(t, f.srv.URL, []string{"grown-up"}, "")
	if err := runCmd(t, Issue(), "backlog", "sync", "v0.1"); err != nil {
		t.Fatalf("first sync: %v", err)
	}
	mirror := f.keyBySummary("[backlog] grown-up")
	f.mu.Lock()
	f.refuse[mirror] = true
	f.mu.Unlock()
	scaffoldBacklogPhase(t, dir, "grown-up")

	var errOut string
	_ = captureStdout(t, func() {
		errOut = captureStderr(t, func() {
			if err := runCmd(t, Issue(), "backlog", "sync", "v0.1"); err != nil {
				t.Fatalf("a refused mirror close must not fail the sync: %v", err)
			}
		})
	})
	if f.closeCount(mirror) == 0 {
		t.Fatal("no close was attempted")
	}
	if !strings.Contains(errOut, mirror) {
		t.Errorf("stderr = %q, want the refused close named", errOut)
	}
	if _, ok := loadBoardFile(t, dir).BacklogID("slug:grown-up"); !ok {
		t.Error("a refused close must keep the link so the next run retries")
	}
}

// TestBacklogSyncResolvesVersionFromThePhaseSpec covers the --phase form
// ship.md uses. The finalize steps carry only a phase id, and resolving the
// version in Go keeps it greppable and testable where prose in a prompt is
// neither.
func TestBacklogSyncResolvesVersionFromThePhaseSpec(t *testing.T) {
	t.Run("phase in a milestone", func(t *testing.T) {
		f := newBacklogCloseFake(t)
		backlogCloseRepo(t, f.srv.URL, []string{"still-waiting"}, "")
		if err := runCmd(t, Issue(), "backlog", "sync", "--phase", "host"); err != nil {
			t.Fatalf("backlog-sync --phase: %v", err)
		}
		if f.keyBySummary("[backlog] still-waiting") == "" {
			t.Errorf("v0.1's backlog was not reconciled: %v", f.summary)
		}
	})

	t.Run("phase in no milestone", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			t.Errorf("a milestone-less phase must reach the tracker not at all, but got %s %s", r.Method, r.URL.Path)
			_, _ = w.Write([]byte(`{}`))
		}))
		t.Cleanup(srv.Close)
		dir := youtrackBoardRepo(t, srv.URL)
		writeSpec(t, dir, "loner", "[phase]\nid = \"loner\"\ntitle = \"Loner\"\n")

		_ = captureStdout(t, func() {
			if err := runCmd(t, Issue(), "backlog", "sync", "--phase", "loner"); err != nil {
				t.Fatalf("a phase outside any milestone should exit 0: %v", err)
			}
		})
	})

	t.Run("neither form", func(t *testing.T) {
		f := newBacklogCloseFake(t)
		backlogCloseRepo(t, f.srv.URL, nil, "")
		if err := runCmd(t, Issue(), "backlog", "sync"); err == nil {
			t.Error("backlog-sync with no version and no --phase should be refused")
		}
	})
}

// TestShipPromptReconcilesTheBacklog: the verb existed and nothing in
// assets/prompts called it, which is why backlog mirrors accumulated. A
// capability no prompt edge invokes closes nothing.
func TestShipPromptReconcilesTheBacklog(t *testing.T) {
	content := shipPromptContent(t)
	if !strings.Contains(content, "dross issue backlog sync") {
		t.Fatal("ship.md never calls backlog sync — backlog mirrors would keep accumulating")
	}
	if !strings.Contains(content, "--phase <phase-id>") {
		t.Error("ship.md's backlog sync call must use the --phase form: the finalize steps carry only a phase id")
	}
}
