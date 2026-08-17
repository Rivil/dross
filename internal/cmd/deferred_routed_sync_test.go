package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// routedRepo scaffolds a milestone plus one phase carrying the given [[deferred]]
// block, on a YouTrack-backed board.
func routedRepo(t *testing.T, srvURL, deferredBlock string) string {
	t.Helper()
	dir := youtrackBoardRepo(t, srvURL)
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
`+deferredBlock)
	return dir
}

// tagsOn returns the tag names the fake currently holds for an issue.
func tagsOn(f *fakeBoard, key string) []string { return f.tags[key] }

// issueCarrying returns the key of the single issue whose summary contains
// `want`, failing if there is not exactly one.
func issueCarrying(t *testing.T, f *fakeBoard, want string) string {
	t.Helper()
	var found []string
	for key, summary := range f.issues {
		if strings.Contains(summary, want) {
			found = append(found, key)
		}
	}
	if len(found) != 1 {
		t.Fatalf("issues matching %q = %v, want exactly one", want, found)
	}
	return found[0]
}

// TestRoutedDeferredGetsItsOwnIssue is c-6's first half: a routed item is
// mirrored, not skipped. The old `Target != ""` skip produced one issue for the
// two items below; the fix produces two.
func TestRoutedDeferredGetsItsOwnIssue(t *testing.T) {
	f := newFakeBoard(t)
	routedRepo(t, f.srv.URL, `
[[deferred]]
text = "routed idea"
target = "host"

[[deferred]]
text = "someday idea"
`)
	if err := runCmd(t, Issue(), "backlog-sync", "v0.1"); err != nil {
		t.Fatalf("backlog-sync: %v", err)
	}
	if len(f.creates) != 2 {
		t.Fatalf("creates = %v, want one per deferred item", f.creates)
	}

	key := issueCarrying(t, f, "routed idea")
	got := tagsOn(f, key)
	if !slicesHas(got, "dross/target:host") {
		t.Errorf("tags on the routed item = %v, want dross/target:host", got)
	}
	var carriesIdentity bool
	for _, n := range got {
		if strings.HasPrefix(n, "dross/deferred:") {
			carriesIdentity = true
		}
	}
	if !carriesIdentity {
		t.Errorf("tags = %v, want a dross/deferred:<id> identity label", got)
	}
}

// TestRoutedDeferredAdoptsAnExistingIssue is the post-ship case: the issue
// mirrorDeferredAdd minted is alive on the tracker, but the board.json that
// recorded it died with the phase branch. A create here is the duplicate t-9
// closes for phases, in its deferred form.
func TestRoutedDeferredAdoptsAnExistingIssue(t *testing.T) {
	f := newFakeBoard(t)
	dir := routedRepo(t, f.srv.URL, `
[[deferred]]
id = "abc123"
text = "routed idea"
target = "host"
`)
	// The live issue, tagged as `deferred add` would have left it — and no
	// board.json entry pointing at it.
	f.issues["PROJ-77"] = "[routed] routed idea"
	f.tags["PROJ-77"] = []string{"dross", "dross/deferred:abc123", "dross/target:host"}
	mustWrite(t, filepath.Join(dir, ".dross", "board.json"), emptyBoardJSON)

	if err := runCmd(t, Issue(), "backlog-sync", "v0.1"); err != nil {
		t.Fatalf("backlog-sync: %v", err)
	}
	if len(f.creates) != 0 {
		t.Fatalf("creates = %v, want 0 — the labelled issue must be adopted", f.creates)
	}
	if len(f.patches) != 1 || f.patches[0].key != "PROJ-77" {
		t.Errorf("patches = %+v, want exactly one update to PROJ-77", f.patches)
	}
}

// TestRoutedDeferredIgnoresAnUnmarkedMatch pins the same marker post-filter the
// phase resolver applies: a hand-made issue carrying the identity label is not
// dross's to take over.
func TestRoutedDeferredIgnoresAnUnmarkedMatch(t *testing.T) {
	f := newFakeBoard(t)
	dir := routedRepo(t, f.srv.URL, `
[[deferred]]
id = "abc123"
text = "routed idea"
target = "host"
`)
	f.issues["PROJ-77"] = "someone else's issue"
	f.tags["PROJ-77"] = []string{"dross/deferred:abc123"} // no marker
	mustWrite(t, filepath.Join(dir, ".dross", "board.json"), emptyBoardJSON)

	if err := runCmd(t, Issue(), "backlog-sync", "v0.1"); err != nil {
		t.Fatalf("backlog-sync: %v", err)
	}
	if len(f.creates) != 1 {
		t.Errorf("creates = %v, want 1 — an unmarked match must not be adopted", f.creates)
	}
	for _, p := range f.patches {
		if p.key == "PROJ-77" {
			t.Error("backlog-sync wrote to an issue it does not own")
		}
	}
}

// TestRoutedDeferredStaysCurrent is c-6's "stays current" half: editing the
// item's text updates the same issue rather than minting a second one.
func TestRoutedDeferredStaysCurrent(t *testing.T) {
	f := newFakeBoard(t)
	dir := routedRepo(t, f.srv.URL, `
[[deferred]]
id = "abc123"
text = "routed idea"
target = "host"
`)
	if err := runCmd(t, Issue(), "backlog-sync", "v0.1"); err != nil {
		t.Fatalf("first sync: %v", err)
	}
	key := issueCarrying(t, f, "routed idea")

	specPath := filepath.Join(dir, ".dross", "phases", "host", "spec.toml")
	body, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatalf("read spec: %v", err)
	}
	mustWrite(t, specPath, strings.ReplaceAll(string(body), "routed idea", "routed idea, reworded"))

	before := len(f.creates)
	f.patches = nil
	if err := runCmd(t, Issue(), "backlog-sync", "v0.1"); err != nil {
		t.Fatalf("second sync: %v", err)
	}
	if len(f.creates) != before {
		t.Errorf("re-sync created %d new issue(s); an edit must update, not duplicate", len(f.creates)-before)
	}
	if len(f.patches) != 1 || f.patches[0].key != key {
		t.Fatalf("patches = %+v, want exactly one update to %s", f.patches, key)
	}
	if !strings.Contains(f.patches[0].summary, "reworded") {
		t.Errorf("update summary = %q, want the edited text", f.patches[0].summary)
	}
}

// TestReroutedDeferredSwapsItsTargetLabel relies on t-6's replace semantics: an
// additive tag write would leave the issue claiming both destinations, and
// every dross/target: filter would then return it twice.
func TestReroutedDeferredSwapsItsTargetLabel(t *testing.T) {
	f := newFakeBoard(t)
	dir := routedRepo(t, f.srv.URL, `
[[deferred]]
id = "abc123"
text = "routed idea"
target = "host"
`)
	if err := runCmd(t, Issue(), "backlog-sync", "v0.1"); err != nil {
		t.Fatalf("first sync: %v", err)
	}
	key := issueCarrying(t, f, "routed idea")
	if !slicesHas(tagsOn(f, key), "dross/target:host") {
		t.Fatalf("tags = %v, want the initial target label", tagsOn(f, key))
	}

	specPath := filepath.Join(dir, ".dross", "phases", "host", "spec.toml")
	body, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatalf("read spec: %v", err)
	}
	mustWrite(t, specPath, strings.ReplaceAll(string(body), `target = "host"`, `target = "other"`))

	if err := runCmd(t, Issue(), "backlog-sync", "v0.1"); err != nil {
		t.Fatalf("second sync: %v", err)
	}
	got := tagsOn(f, key)
	if !slicesHas(got, "dross/target:other") {
		t.Errorf("tags = %v, want the new target label", got)
	}
	if slicesHas(got, "dross/target:host") {
		t.Errorf("tags = %v, still claim the old destination — removals must remove", got)
	}
}

// TestDismissedRoutedItemProducesNoIssue proves the skip removal was scoped:
// dismissing an item still keeps it off the board, routed or not.
func TestDismissedRoutedItemProducesNoIssue(t *testing.T) {
	f := newFakeBoard(t)
	routedRepo(t, f.srv.URL, `
[[deferred]]
text = "routed but dismissed"
target = "host"
dismissed = true
`)
	if err := runCmd(t, Issue(), "backlog-sync", "v0.1"); err != nil {
		t.Fatalf("backlog-sync: %v", err)
	}
	for _, got := range f.creates {
		if strings.Contains(got, "routed but dismissed") {
			t.Errorf("creates = %v, want the dismissed item skipped", f.creates)
		}
	}
}

// TestMirrorDeferredAddNoteIsGone pins the doc-comment cleanup: the note said
// the staleness was "left to board-sync-truth", which is this phase. Leaving it
// would send the next reader looking for work that is already done.
func TestMirrorDeferredAddNoteIsGone(t *testing.T) {
	root := repoRootFromTest(t)
	b, err := os.ReadFile(filepath.Join(root, "internal", "cmd", "deferred_add.go"))
	if err != nil {
		t.Fatalf("read deferred_add.go: %v", err)
	}
	if strings.Contains(string(b), "left to board-sync-truth") {
		t.Error("mirrorDeferredAdd still defers the staleness to this phase")
	}
	if strings.Contains(string(b), "backlog-sync, which skips routed items") {
		t.Error("mirrorDeferredAdd still describes backlog-sync as skipping routed items")
	}
}
