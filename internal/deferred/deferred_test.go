package deferred

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/phase"
)

// fixtureRoot builds a .dross root with phases alpha (two items: one routed
// to beta, one someday) and beta (one someday item) and returns the root.
func fixtureRoot(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), ".dross")
	write := func(rel, body string) {
		t.Helper()
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("phases/alpha/spec.toml", `[phase]
id = "alpha"
title = "Alpha"

[[criteria]]
id = "c-1"
text = "x"

[[deferred]]
text = "alpha routed idea"
target = "beta"

[[deferred]]
text = "alpha someday idea"
`)
	write("phases/beta/spec.toml", `[phase]
id = "beta"
title = "Beta"

[[criteria]]
id = "c-1"
text = "x"

[[deferred]]
text = "beta someday idea"
`)
	return root
}

func mustRead(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// TestDeferredStoreRoundTrip proves the project store is a real phase.Spec on
// disk: an item written with id+text+target reloads identically through
// phase.LoadSpec. Dropping the `id` toml tag from phase.Deferred loses the id
// across the reload and fails here.
func TestDeferredStoreRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "deferred.toml")

	want := phase.Deferred{ID: "a1b2c3d4", Text: "hermeticity gap", Target: "mutation-score-truth"}
	spec := &phase.Spec{
		Phase:    phase.SpecPhase{ID: ProjectStoreSlug, Title: "project-level deferred store"},
		Deferred: []phase.Deferred{want},
	}
	if err := spec.Save(path); err != nil {
		t.Fatalf("save store: %v", err)
	}

	got, err := phase.LoadSpec(path)
	if err != nil {
		t.Fatalf("reload store: %v", err)
	}
	if len(got.Deferred) != 1 {
		t.Fatalf("want 1 deferred item, got %d", len(got.Deferred))
	}
	if got.Deferred[0] != want {
		t.Errorf("round-trip mismatch:\n got %+v\nwant %+v", got.Deferred[0], want)
	}
}

// TestDeferredIDOmitEmpty pins the omitempty half of the id tag: a spec written
// without an id must not grow an `id = ""` line. Every pre-existing spec in
// every dross repo is id-less, and a non-omitempty tag would rewrite all of
// them on the next save.
func TestDeferredIDOmitEmpty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "spec.toml")

	spec := &phase.Spec{
		Phase:    phase.SpecPhase{ID: "alpha", Title: "Alpha"},
		Deferred: []phase.Deferred{{Text: "no id here"}},
	}
	if err := spec.Save(path); err != nil {
		t.Fatalf("save: %v", err)
	}
	if strings.Contains(mustRead(t, path), "id = \"\"") {
		t.Errorf("id emitted for an id-less deferred item; omitempty lost:\n%s", mustRead(t, path))
	}
	// Guard against the assertion above passing vacuously: an item WITH an id
	// must still emit one.
	spec.Deferred[0].ID = "deadbeef"
	if err := spec.Save(path); err != nil {
		t.Fatalf("save: %v", err)
	}
	if !strings.Contains(mustRead(t, path), `id = "deadbeef"`) {
		t.Errorf("id not emitted for an item that has one:\n%s", mustRead(t, path))
	}
}

// TestDeferredStoreResolvesPaths pins the one helper every verb routes its path
// building through. The `_project` arm must NOT fall through to phase.Dir — a
// store living at .dross/phases/_project/spec.toml would be shadowable by a
// real phase directory and would collide with the source slug.
func TestDeferredStoreResolvesPaths(t *testing.T) {
	root := fixtureRoot(t)

	t.Run("_project resolves to .dross/deferred.toml", func(t *testing.T) {
		got, err := Store(root, ProjectStoreSlug)
		if err != nil {
			t.Fatalf("Store(_project): %v", err)
		}
		want := filepath.Join(root, "deferred.toml")
		if got != want {
			t.Errorf("store path = %q, want %q", got, want)
		}
		if got != StorePath(root) {
			t.Errorf("Store(_project) = %q disagrees with StorePath = %q", got, StorePath(root))
		}
	})

	t.Run("a real phase resolves to its spec.toml", func(t *testing.T) {
		got, err := Store(root, "alpha")
		if err != nil {
			t.Fatalf("Store(alpha): %v", err)
		}
		want := filepath.Join(root, "phases", "alpha", "spec.toml")
		if got != want {
			t.Errorf("store path = %q, want %q", got, want)
		}
	})

	t.Run("an unknown slug errors instead of returning a creatable path", func(t *testing.T) {
		got, err := Store(root, "no-such-phase")
		if err == nil {
			t.Fatalf("want an error for a slug with no phase dir, got path %q", got)
		}
		for _, want := range []string{"no-such-phase", ProjectStoreSlug, "only non-phase source"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("error should mention %q, got %q", want, err)
			}
		}
	})
}

// TestDeferredStoreSlugIsUnreachableByPhases is the shadowing guard: the
// reserved slug is safe only because phase.Slugify can never emit it. If
// slugify ever starts preserving leading underscores, a phase titled "_project"
// would shadow the store — this fails first, before any data can be lost.
func TestDeferredStoreSlugIsUnreachableByPhases(t *testing.T) {
	for _, title := range []string{"_project", "_Project", " _project ", "__project__", "_ project"} {
		if got := phase.Slugify(title); got == ProjectStoreSlug {
			t.Errorf("Slugify(%q) = %q — a real phase can now shadow the reserved store", title, got)
		}
	}
}

// TestLoadStoreEmptyUntilWritten: a project that never files a homeless item
// never grows the file — LoadStore hands back an empty, correctly-titled spec
// and creates nothing.
func TestLoadStoreEmptyUntilWritten(t *testing.T) {
	root := fixtureRoot(t)
	spec, err := LoadStore(root)
	if err != nil {
		t.Fatalf("LoadStore on a missing file: %v", err)
	}
	if spec.Phase.ID != ProjectStoreSlug || len(spec.Deferred) != 0 {
		t.Errorf("empty store = %+v, want id %q and no items", spec.Phase, ProjectStoreSlug)
	}
	if _, err := os.Stat(StorePath(root)); err == nil {
		t.Error("LoadStore materialised deferred.toml on read")
	}
}

// TestCollectSkipsProjectPhaseDir covers the ambiguity a hand-made
// phases/_project directory would create: two sources sharing one slug, so
// `_project 0` names two different items. Collect skips the directory and
// lists the real store under the reserved slug instead.
func TestCollectSkipsProjectPhaseDir(t *testing.T) {
	root := fixtureRoot(t)
	impostor := filepath.Join(root, "phases", ProjectStoreSlug)
	if err := os.MkdirAll(impostor, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(impostor, "spec.toml"), []byte(`[phase]
id = "_project"
title = "Impostor"

[[deferred]]
text = "impostor item from a phase dir"
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(StorePath(root), []byte(`[phase]
  id = "_project"
  title = "project-level deferred store"

[[deferred]]
  text = "homeless finding"
`), 0o644); err != nil {
		t.Fatal(err)
	}

	entries, err := Collect(root)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	var store int
	for _, e := range entries {
		if strings.Contains(e.Text, "impostor") {
			t.Fatalf("phases/_project item leaked into the collection as source %q idx %d", e.Source, e.Index)
		}
		if e.Source == ProjectStoreSlug {
			store++
			if e.Text != "homeless finding" || e.Index != 0 {
				t.Errorf("store entry = %+v, want the real store item at index 0", e)
			}
		}
	}
	if store != 1 {
		t.Errorf("want exactly 1 store entry, got %d", store)
	}
	// Provenance of the phase-sourced items is per-source indexed.
	want := map[string]int{"alpha": 2, "beta": 1}
	got := map[string]int{}
	for _, e := range entries {
		if e.Source != ProjectStoreSlug {
			got[e.Source]++
			if e.Index >= want[e.Source] {
				t.Errorf("entry %+v has an index outside its source's array", e)
			}
		}
	}
	for src, n := range want {
		if got[src] != n {
			t.Errorf("source %s: %d entries, want %d", src, got[src], n)
		}
	}
}

var hexID = regexp.MustCompile(`^[0-9a-f]{16}$`)

// TestEnsureIDsIsIdempotent: a spec with two id-less items gets two distinct
// 16-hex ids, a second call rewrites nothing (bytes unchanged), and both ids
// are readable on a second Collect.
func TestEnsureIDsIsIdempotent(t *testing.T) {
	root := fixtureRoot(t)
	alpha := filepath.Join(root, "phases", "alpha", "spec.toml")

	entries, err := EnsureIDs(root)
	if err != nil {
		t.Fatalf("EnsureIDs: %v", err)
	}
	ids := map[string]string{}
	for _, e := range entries {
		if !hexID.MatchString(e.ID) {
			t.Errorf("entry %s[%d] id %q is not 16 hex chars", e.Source, e.Index, e.ID)
		}
		if prev, dup := ids[e.ID]; dup {
			t.Errorf("id %q assigned twice (%s and %s[%d])", e.ID, prev, e.Source, e.Index)
		}
		ids[e.ID] = e.Source
	}
	if len(ids) != 3 {
		t.Fatalf("want 3 distinct ids across alpha+beta, got %d", len(ids))
	}

	before := mustRead(t, alpha)
	if _, err := EnsureIDs(root); err != nil {
		t.Fatalf("second EnsureIDs: %v", err)
	}
	if got := mustRead(t, alpha); got != before {
		t.Errorf("second EnsureIDs rewrote alpha/spec.toml:\n--- before\n%s\n--- after\n%s", before, got)
	}

	again, err := Collect(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range again {
		if ids[e.ID] != e.Source {
			t.Errorf("after a fresh Collect, %s[%d] carries id %q which was minted for %q", e.Source, e.Index, e.ID, ids[e.ID])
		}
	}
}

// TestNewIDAvoidsCollisions: an id already in use is never handed out again.
func TestNewIDAvoidsCollisions(t *testing.T) {
	used := map[string]bool{}
	for i := 0; i < 50; i++ {
		id, err := NewID(used)
		if err != nil {
			t.Fatal(err)
		}
		if used[id] {
			t.Fatalf("NewID returned an id already in use: %q", id)
		}
		if !hexID.MatchString(id) {
			t.Fatalf("id %q is not 16 hex chars", id)
		}
		used[id] = true
	}
}

// TestResolveSourceAndIndex: route/unroute/dismiss share one resolver, and the
// bounds error reads the same for a phase and for the store.
func TestResolveSourceAndIndex(t *testing.T) {
	root := fixtureRoot(t)
	spec, path, err := ResolveSource(root, "alpha")
	if err != nil {
		t.Fatalf("ResolveSource(alpha): %v", err)
	}
	if path != filepath.Join(root, "phases", "alpha", "spec.toml") || len(spec.Deferred) != 2 {
		t.Errorf("alpha resolved to %q with %d items", path, len(spec.Deferred))
	}
	if err := Index("alpha", spec, 1); err != nil {
		t.Errorf("index 1 of 2 rejected: %v", err)
	}
	err = Index("alpha", spec, 2)
	if err == nil || !strings.Contains(err.Error(), "index 2 out of range") || !strings.Contains(err.Error(), "alpha has 2 deferred item(s)") {
		t.Errorf("out-of-range error = %v", err)
	}
	if err := Index("alpha", spec, -1); err == nil {
		t.Error("negative index accepted")
	}

	store, spath, err := ResolveSource(root, ProjectStoreSlug)
	if err != nil {
		t.Fatalf("ResolveSource(_project): %v", err)
	}
	if spath != StorePath(root) || len(store.Deferred) != 0 {
		t.Errorf("store resolved to %q with %d items", spath, len(store.Deferred))
	}
	if _, _, err := ResolveSource(root, "nope"); err == nil {
		t.Error("unknown source resolved")
	}
}

// TestFilterPreservesOrder: Filter keeps accepted entries in input order and
// returns a non-nil empty slice when nothing matches (JSON `[]`, not `null`).
func TestFilterPreservesOrder(t *testing.T) {
	in := []Entry{{Text: "a", Target: "x"}, {Text: "b"}, {Text: "c", Target: "x"}}
	got := Filter(in, func(e Entry) bool { return e.Target == "x" })
	if len(got) != 2 || got[0].Text != "a" || got[1].Text != "c" {
		t.Errorf("Filter = %+v", got)
	}
	none := Filter(in, func(Entry) bool { return false })
	if none == nil || len(none) != 0 {
		t.Errorf("empty Filter = %#v, want a non-nil empty slice", none)
	}
}

// TestRepointTargetWalksSpecsAndStore: a rename re-points every entry aimed at
// the old slug — in phase specs AND the project store — and leaves every other
// target and every untouched file byte-identical.
func TestRepointTargetWalksSpecsAndStore(t *testing.T) {
	root := fixtureRoot(t)
	if err := os.WriteFile(StorePath(root), []byte(`[phase]
  id = "_project"
  title = "project-level deferred store"

[[deferred]]
  text = "store item aimed at beta"
  target = "beta"

[[deferred]]
  text = "store item aimed elsewhere"
  target = "gamma"
`), 0o644); err != nil {
		t.Fatal(err)
	}
	betaBefore := mustRead(t, filepath.Join(root, "phases", "beta", "spec.toml"))

	if err := RepointTarget(root, "beta", "beta-renamed"); err != nil {
		t.Fatalf("RepointTarget: %v", err)
	}
	entries, err := Collect(root)
	if err != nil {
		t.Fatal(err)
	}
	targets := map[string]string{}
	for _, e := range entries {
		targets[e.Text] = e.Target
	}
	if targets["alpha routed idea"] != "beta-renamed" {
		t.Errorf("alpha's routed item target = %q, want beta-renamed", targets["alpha routed idea"])
	}
	if targets["store item aimed at beta"] != "beta-renamed" {
		t.Errorf("store item target = %q, want beta-renamed — the store was not walked", targets["store item aimed at beta"])
	}
	if targets["store item aimed elsewhere"] != "gamma" {
		t.Errorf("unrelated target rewritten to %q", targets["store item aimed elsewhere"])
	}
	if got := mustRead(t, filepath.Join(root, "phases", "beta", "spec.toml")); got != betaBefore {
		t.Error("beta/spec.toml (no matching target) was rewritten")
	}
}

// TestLegacyBacklogKeyShape pins the positional key pre-id boards were linked
// under; changing it orphans every legacy link on upgrade.
func TestLegacyBacklogKeyShape(t *testing.T) {
	if got := LegacyBacklogKey("alpha", 3); got != "someday:alpha#3" {
		t.Errorf("LegacyBacklogKey = %q", got)
	}
}
