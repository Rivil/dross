package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/phase"
)

// fakeBoard is a YouTrack stand-in that remembers each issue's summary, so a
// GET can answer "what does this issue currently say?" — the question the
// legacy-key migration asks before adopting a link.
type fakeBoard struct {
	t        *testing.T
	srv      *httptest.Server
	issues   map[string]string   // issue key -> current summary
	tags     map[string][]string // issue key -> tag names currently on it
	tagIndex []string            // tag entities the instance knows
	creates  []string            // summaries, in create order
	patches  []patchCall         // every UpdateIssue, with its target key
	links    []linkCall          // every relates-to link, in order
	nextItem int
}

type linkCall struct{ from, to string }

// knownTags is the instance's tag vocabulary: everything created plus
// everything any issue carries. FilterKnownLabels checks queries against it.
func (f *fakeBoard) knownTags() []string {
	seen := map[string]bool{}
	var out []string
	add := func(n string) {
		if n != "" && !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	for _, n := range f.tagIndex {
		add(n)
	}
	for _, tags := range f.tags {
		for _, n := range tags {
			add(n)
		}
	}
	return out
}

// issueBody renders an issue with its tags, the shape the resolvers read.
func (f *fakeBoard) issueBody(key string) string {
	var tagJSON []string
	for _, n := range f.tags[key] {
		tagJSON = append(tagJSON, fmt.Sprintf(`{"id":"tid-%s","name":%q}`, n, n))
	}
	return fmt.Sprintf(`{"idReadable":%q,"summary":%q,"tags":[%s]}`,
		key, f.issues[key], strings.Join(tagJSON, ","))
}

type patchCall struct{ key, summary string }

func newFakeBoard(t *testing.T) *fakeBoard {
	t.Helper()
	f := &fakeBoard{t: t, issues: map[string]string{}, tags: map[string][]string{}}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/customFields") && r.Method == "GET":
			_, _ = io.WriteString(w, `[{"field":{"name":"Fix versions"},"bundle":{"id":"B1","$type":"VersionBundle","values":[]}}]`)
		case strings.Contains(r.URL.Path, "/bundles/version/B1/values") && r.Method == "POST":
			_, _ = io.WriteString(w, `{"name":"v0.1"}`)
		case r.URL.Path == "/api/issues" && r.Method == "POST":
			var b map[string]any
			raw, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(raw, &b)
			f.nextItem++
			key := fmt.Sprintf("PROJ-%d", 200+f.nextItem)
			summary, _ := b["summary"].(string)
			f.issues[key] = summary
			f.creates = append(f.creates, summary)
			_, _ = io.WriteString(w, fmt.Sprintf(`{"idReadable":%q,"summary":%q}`, key, summary))
		case strings.HasPrefix(r.URL.Path, "/api/issues/") && r.Method == "GET":
			key := strings.TrimPrefix(r.URL.Path, "/api/issues/")
			if _, ok := f.issues[key]; !ok {
				w.WriteHeader(http.StatusNotFound)
				_, _ = io.WriteString(w, `{"error":"not found"}`)
				return
			}
			_, _ = io.WriteString(w, f.issueBody(key))
		// --- tags (labels are entity writes on YouTrack) ---
		case r.URL.Path == "/api/issueTags" && r.Method == "GET":
			var out []string
			for _, n := range f.knownTags() {
				out = append(out, fmt.Sprintf(`{"id":"tid-%s","name":%q}`, n, n))
			}
			_, _ = io.WriteString(w, "["+strings.Join(out, ",")+"]")
		case r.URL.Path == "/api/issueTags" && r.Method == "POST":
			var b struct {
				Name string `json:"name"`
			}
			raw, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(raw, &b)
			f.tagIndex = append(f.tagIndex, b.Name)
			_, _ = io.WriteString(w, fmt.Sprintf(`{"id":"tid-%s"}`, b.Name))
		case strings.HasSuffix(r.URL.Path, "/tags") && r.Method == "POST":
			key := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/issues/"), "/tags")
			var b struct {
				ID string `json:"id"`
			}
			raw, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(raw, &b)
			f.tags[key] = append(f.tags[key], strings.TrimPrefix(b.ID, "tid-"))
			_, _ = io.WriteString(w, `{}`)

		case strings.HasPrefix(r.URL.Path, "/api/issues/") && r.Method == "POST":
			key := strings.TrimPrefix(r.URL.Path, "/api/issues/")
			var b map[string]any
			raw, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(raw, &b)
			// A labels-only patch carries no summary; it is tag traffic, not an
			// issue-body update, and counting it would double every patch.
			if summary, ok := b["summary"].(string); ok {
				f.issues[key] = summary
				f.patches = append(f.patches, patchCall{key: key, summary: summary})
			}
			_, _ = io.WriteString(w, f.issueBody(key))

		case strings.Contains(r.URL.Path, "/tags/") && r.Method == "DELETE":
			rest := strings.TrimPrefix(r.URL.Path, "/api/issues/")
			key, id, _ := strings.Cut(rest, "/tags/")
			drop := strings.TrimPrefix(id, "tid-")
			var kept []string
			for _, n := range f.tags[key] {
				if n != drop {
					kept = append(kept, n)
				}
			}
			f.tags[key] = kept
			_, _ = io.WriteString(w, `{}`)

		// The resolvers' label query.
		case r.URL.Path == "/api/issues" && r.Method == "GET":
			query := r.URL.Query().Get("query")
			var out []string
			for key := range f.issues {
				for _, n := range f.tags[key] {
					if n != "" && strings.Contains(query, n) {
						out = append(out, f.issueBody(key))
						break
					}
				}
			}
			_, _ = io.WriteString(w, "["+strings.Join(out, ",")+"]")

		case r.URL.Path == "/api/commands" && r.Method == "POST":
			var b struct {
				Query  string `json:"query"`
				Issues []struct {
					IDReadable string `json:"idReadable"`
				} `json:"issues"`
			}
			raw, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(raw, &b)
			if len(b.Issues) > 0 && strings.HasPrefix(b.Query, "relates to ") {
				f.links = append(f.links, linkCall{
					from: b.Issues[0].IDReadable,
					to:   strings.TrimPrefix(b.Query, "relates to "),
				})
			}
			_, _ = io.WriteString(w, `{}`)

		default:
			f.t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	t.Cleanup(f.srv.Close)
	return f
}

// twoSomedayRepo builds a repo whose single phase carries two someday items,
// plus a milestone for backlog-sync to attach to.
func twoSomedayRepo(t *testing.T, apiBase string) string {
	t.Helper()
	dir := youtrackBoardRepo(t, apiBase)
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

[[deferred]]
text = "first idea"

[[deferred]]
text = "second idea"
`)
	return dir
}

// TestBacklogSurvivesNeighbourRemoval is c-8. Two board-linked someday items;
// delete the first; re-sync. Under positional keys the survivor slides into
// index 0 and inherits the DELETED item's issue, which is then re-titled to the
// survivor's text — one live issue silently re-pointed at different work.
func TestBacklogSurvivesNeighbourRemoval(t *testing.T) {
	f := newFakeBoard(t)
	dir := twoSomedayRepo(t, f.srv.URL)

	if err := runCmd(t, Issue(), "backlog-sync", "v0.1"); err != nil {
		t.Fatalf("first sync: %v", err)
	}
	if len(f.creates) != 2 {
		t.Fatalf("first sync should create 2 items, got %d (%v)", len(f.creates), f.creates)
	}
	// Which issue key belongs to the survivor ("second idea")?
	var survivorKey string
	for k, summary := range f.issues {
		if summary == "[someday] second idea" {
			survivorKey = k
		}
	}
	if survivorKey == "" {
		t.Fatalf("no issue created for the survivor: %v", f.issues)
	}
	var deletedKey string
	for k, summary := range f.issues {
		if summary == "[someday] first idea" {
			deletedKey = k
		}
	}

	// Remove the FIRST item; the survivor now sits at index 0.
	specPath := filepath.Join(dir, ".dross", "phases", "host", "spec.toml")
	spec, err := phase.LoadSpec(specPath)
	if err != nil {
		t.Fatal(err)
	}
	spec.Deferred = spec.Deferred[1:]
	if err := spec.Save(specPath); err != nil {
		t.Fatal(err)
	}

	f.patches = nil
	if err := runCmd(t, Issue(), "backlog-sync", "v0.1"); err != nil {
		t.Fatalf("second sync: %v", err)
	}

	var sawSurvivorPatch bool
	for _, p := range f.patches {
		if p.summary == "[someday] second idea" {
			if p.key == deletedKey {
				t.Errorf("survivor's text was written onto the DELETED item's issue %s — positional keying re-pointed a live issue", deletedKey)
			}
			if p.key == survivorKey {
				sawSurvivorPatch = true
			}
		}
	}
	if !sawSurvivorPatch {
		t.Errorf("no update against the survivor's own issue %s; patches=%v", survivorKey, f.patches)
	}
	if len(f.creates) != 2 {
		t.Errorf("survivor should have been updated, not re-created: creates=%v", f.creates)
	}
}

// TestBacklogMigratesLegacyPositionalKey: a board.json written before ids
// existed holds `someday:<phase>#0`. The upgrade run must re-key that link onto
// the item's id, not orphan the live issue and create a duplicate.
func TestBacklogMigratesLegacyPositionalKey(t *testing.T) {
	f := newFakeBoard(t)
	dir := youtrackBoardRepo(t, f.srv.URL)
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

[[deferred]]
text = "legacy idea"
`)
	// A live issue, linked under the pre-id positional key.
	f.issues["PROJ-99"] = "[someday] legacy idea"
	mustWrite(t, filepath.Join(dir, ".dross", "board.json"),
		`{"milestones":{"v0.1":"v0.1"},"backlog":{"someday:host#0":"PROJ-99"}}`)

	if err := runCmd(t, Issue(), "backlog-sync", "v0.1"); err != nil {
		t.Fatalf("sync: %v", err)
	}
	if len(f.creates) != 0 {
		t.Errorf("legacy link should be migrated, not duplicated; creates=%v", f.creates)
	}

	bj, err := readBoardJSON(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, stale := bj.Backlog["someday:host#0"]; stale {
		t.Errorf("legacy key not retired after re-keying: %v", bj.Backlog)
	}
	var found bool
	for k, v := range bj.Backlog {
		if strings.HasPrefix(k, "someday:id:") && v == "PROJ-99" {
			found = true
		}
	}
	if !found {
		t.Errorf("link not re-keyed onto the item's id: %v", bj.Backlog)
	}
}

// TestBacklogDoesNotAdoptShiftedLegacyKey is the case that makes the migration
// safe rather than dangerous. The legacy board.json records `someday:host#0`
// for an item deleted BEFORE the upgrade run — so index 0 now names a different
// finding. Adopting on position alone would inherit c-8's own fault permanently.
// The title check catches it: not mine, so create fresh.
func TestBacklogDoesNotAdoptShiftedLegacyKey(t *testing.T) {
	f := newFakeBoard(t)
	dir := youtrackBoardRepo(t, f.srv.URL)
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

[[deferred]]
text = "survivor idea"
`)
	// PROJ-99 is the DELETED item's issue; the recorded index now names the
	// survivor instead.
	f.issues["PROJ-99"] = "[someday] deleted idea"
	mustWrite(t, filepath.Join(dir, ".dross", "board.json"),
		`{"milestones":{"v0.1":"v0.1"},"backlog":{"someday:host#0":"PROJ-99"}}`)

	if err := runCmd(t, Issue(), "backlog-sync", "v0.1"); err != nil {
		t.Fatalf("sync: %v", err)
	}
	if got := f.issues["PROJ-99"]; got != "[someday] deleted idea" {
		t.Errorf("the deleted item's issue was re-titled to %q — a shifted legacy key was adopted", got)
	}
	if len(f.creates) != 1 || f.creates[0] != "[someday] survivor idea" {
		t.Errorf("survivor should get its own new issue; creates=%v", f.creates)
	}
}

// TestBacklogSyncBackfillsIDsIdempotently: an id-less spec-authored item gains
// an id on first sync and keeps it on the second — no churn, no second issue.
// Two items backfilled in the same run must get DIFFERENT ids; a collision
// would silently merge their board links.
func TestBacklogSyncBackfillsIDsIdempotently(t *testing.T) {
	f := newFakeBoard(t)
	dir := twoSomedayRepo(t, f.srv.URL)
	specPath := filepath.Join(dir, ".dross", "phases", "host", "spec.toml")

	if err := runCmd(t, Issue(), "backlog-sync", "v0.1"); err != nil {
		t.Fatalf("first sync: %v", err)
	}
	spec, err := phase.LoadSpec(specPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(spec.Deferred) != 2 {
		t.Fatalf("want 2 deferred items, got %d", len(spec.Deferred))
	}
	first, second := spec.Deferred[0].ID, spec.Deferred[1].ID
	if first == "" || second == "" {
		t.Fatalf("ids not backfilled: %q / %q", first, second)
	}
	if first == second {
		t.Errorf("two items share id %q — their board links would silently merge", first)
	}

	if err := runCmd(t, Issue(), "backlog-sync", "v0.1"); err != nil {
		t.Fatalf("second sync: %v", err)
	}
	spec, err = phase.LoadSpec(specPath)
	if err != nil {
		t.Fatal(err)
	}
	if spec.Deferred[0].ID != first || spec.Deferred[1].ID != second {
		t.Errorf("ids churned on re-run: %q/%q -> %q/%q", first, second, spec.Deferred[0].ID, spec.Deferred[1].ID)
	}
	if len(f.creates) != 2 {
		t.Errorf("re-run created new issues; creates=%v", f.creates)
	}
}

// TestBacklogSyncCoversRoutedButNotDismissed pins c-6's scope change: a routed
// item IS mirrored (it has a destination, not an absence of one), while a
// dismissed item still is not. The earlier `Target != ""` skip is what left a
// routed item's issue frozen at its filed text — the staleness c-6 closes.
func TestBacklogSyncCoversRoutedButNotDismissed(t *testing.T) {
	f := newFakeBoard(t)
	dir := youtrackBoardRepo(t, f.srv.URL)
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

[[deferred]]
text = "routed idea"
target = "host"

[[deferred]]
text = "dismissed idea"
dismissed = true
`)
	if err := runCmd(t, Issue(), "backlog-sync", "v0.1"); err != nil {
		t.Fatalf("sync: %v", err)
	}
	if len(f.creates) != 1 {
		t.Fatalf("creates = %v, want exactly the routed item", f.creates)
	}
	if !strings.Contains(f.creates[0], "routed idea") {
		t.Errorf("created %q, want the routed item", f.creates[0])
	}
	for _, got := range f.creates {
		if strings.Contains(got, "dismissed idea") {
			t.Error("a dismissed item reached the board — the skip removal must be scoped to routed items")
		}
	}
}
