package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/phase"
)

// TestDeferredStoreRoundTrip proves the project store is a real phase.Spec on
// disk: an item written with id+text+target reloads identically through
// phase.LoadSpec. Dropping the `id` toml tag from phase.Deferred loses the id
// across the reload and fails here.
func TestDeferredStoreRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "deferred.toml")

	want := phase.Deferred{ID: "a1b2c3d4", Text: "hermeticity gap", Target: "mutation-score-truth"}
	spec := &phase.Spec{
		Phase:    phase.SpecPhase{ID: projectStoreSlug, Title: "project-level deferred store"},
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
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "id = \"\"") {
		t.Errorf("id emitted for an id-less deferred item; omitempty lost:\n%s", body)
	}
	// Guard against the assertion above passing vacuously: an item WITH an id
	// must still emit one.
	spec.Deferred[0].ID = "deadbeef"
	if err := spec.Save(path); err != nil {
		t.Fatalf("save: %v", err)
	}
	body, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `id = "deadbeef"`) {
		t.Errorf("id not emitted for an item that has one:\n%s", body)
	}
}

// TestDeferredStoreResolvesPaths pins the one helper every verb routes its path
// building through. The `_project` arm must NOT fall through to phase.Dir — a
// store living at .dross/phases/_project/spec.toml would be shadowable by a
// real phase directory and would collide with the source slug.
func TestDeferredStoreResolvesPaths(t *testing.T) {
	dir := setupDeferredFixture(t)
	root := filepath.Join(dir, ".dross")

	t.Run("_project resolves to .dross/deferred.toml", func(t *testing.T) {
		got, err := deferredStore(root, projectStoreSlug)
		if err != nil {
			t.Fatalf("deferredStore(_project): %v", err)
		}
		want := filepath.Join(root, "deferred.toml")
		if got != want {
			t.Errorf("store path = %q, want %q", got, want)
		}
	})

	t.Run("a real phase resolves to its spec.toml", func(t *testing.T) {
		got, err := deferredStore(root, "alpha")
		if err != nil {
			t.Fatalf("deferredStore(alpha): %v", err)
		}
		want := filepath.Join(root, "phases", "alpha", "spec.toml")
		if got != want {
			t.Errorf("store path = %q, want %q", got, want)
		}
	})

	t.Run("an unknown slug errors instead of returning a creatable path", func(t *testing.T) {
		got, err := deferredStore(root, "nope")
		if err == nil {
			t.Fatalf("want an error for a slug with no phase dir, got path %q", got)
		}
		if !strings.Contains(err.Error(), "nope") {
			t.Errorf("error should name the bad slug, got %q", err)
		}
	})
}

// TestDeferredStoreSlugIsUnreachableByPhases is the shadowing guard: the
// reserved slug is safe only because phase.Slugify can never emit it. If
// slugify ever starts preserving leading underscores, a phase titled "_project"
// would shadow the store — this fails first, before any data can be lost.
func TestDeferredStoreSlugIsUnreachableByPhases(t *testing.T) {
	for _, title := range []string{"_project", "_Project", " _project ", "__project__", "_ project"} {
		if got := phase.Slugify(title); got == projectStoreSlug {
			t.Errorf("Slugify(%q) = %q — a real phase can now shadow the reserved store", title, got)
		}
	}
}

// TestDeferredListSkipsProjectPhaseDir covers the ambiguity a hand-made
// phases/_project directory would create: two sources sharing one slug, so
// `_project 0` names two different items. The lister skips the directory; the
// validate half of this guard is t-8's.
func TestDeferredListSkipsProjectPhaseDir(t *testing.T) {
	dir := setupDeferredFixture(t)
	mustWrite(t, filepath.Join(dir, ".dross", "phases", projectStoreSlug, "spec.toml"),
		`[phase]
id = "_project"
title = "Impostor"

[[criteria]]
id = "c-1"
text = "x"

[[deferred]]
text = "impostor item from a phase dir"
`)

	for _, e := range listJSON(t, "--json") {
		if strings.Contains(e.Text, "impostor") {
			t.Fatalf("phases/_project item leaked into the listing as source %q idx %d", e.Source, e.Index)
		}
	}
}

// TestDeferredListIncludesProjectStore is the positive arm: an item in the real
// store lists under the reserved slug at its own index.
func TestDeferredListIncludesProjectStore(t *testing.T) {
	dir := setupDeferredFixture(t)
	mustWrite(t, filepath.Join(dir, ".dross", "deferred.toml"),
		`[phase]
  id = "_project"
  title = "project-level deferred store"

[[deferred]]
  text = "homeless finding"
`)

	var found bool
	for _, e := range listJSON(t, "--json") {
		if e.Text == "homeless finding" {
			found = true
			if e.Source != projectStoreSlug {
				t.Errorf("store item source = %q, want %q", e.Source, projectStoreSlug)
			}
			if e.Index != 0 {
				t.Errorf("store item index = %d, want 0", e.Index)
			}
		}
	}
	if !found {
		t.Error("store item missing from `deferred list`")
	}
}

// TestDeferredEntryIDStaysInternal pins the locked deferred_identity decision:
// the id is carried in Go (syncBacklog keys on it) but never reaches the JSON a
// prompt reads, where `<source> <idx>` remains the only handle. Removing the
// json:"-" tag leaks an `id` key and fails here.
func TestDeferredEntryIDStaysInternal(t *testing.T) {
	dir := setupDeferredFixture(t)
	mustWrite(t, filepath.Join(dir, ".dross", "phases", "gamma", "spec.toml"),
		`[phase]
id = "gamma"
title = "Gamma"

[[criteria]]
id = "c-1"
text = "x"

[[deferred]]
id = "cafef00d"
text = "gamma someday idea"
`)

	// In Go, the id is present.
	entries, err := collectDeferred(filepath.Join(dir, ".dross"))
	if err != nil {
		t.Fatalf("collectDeferred: %v", err)
	}
	var carried bool
	for _, e := range entries {
		if e.Text == "gamma someday idea" {
			carried = e.ID == "cafef00d"
		}
	}
	if !carried {
		t.Error("deferredEntry did not carry the spec's id into the collector")
	}

	// Over the JSON wire, it is absent.
	var out string
	if err := runCmdCapturing(t, &out, Deferred(), "list", "--json"); err != nil {
		t.Fatalf("deferred list --json: %v", err)
	}
	var raw []map[string]any
	if err := json.Unmarshal([]byte(out), &raw); err != nil {
		t.Fatalf("unmarshal list --json: %v\n%s", err, out)
	}
	if len(raw) == 0 {
		t.Fatal("no entries in list --json")
	}
	for _, m := range raw {
		if _, ok := m["id"]; ok {
			t.Errorf("`id` leaked into deferred list --json: %v", m)
		}
	}
	if strings.Contains(out, "cafef00d") {
		t.Errorf("id value leaked into deferred list --json output: %s", out)
	}
}
