package cmd

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/deferred"
)

// The store's unit tests — round-trip, id omitempty, path resolution and the
// reserved-slug guard — live with the ledger in internal/deferred. What stays
// here drives the `deferred` cobra tree end to end.

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
// the id is carried in Go (boardsync.SyncBacklog keys on it) but never reaches the JSON a
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
	entries, err := deferred.Collect(filepath.Join(dir, ".dross"))
	if err != nil {
		t.Fatalf("deferred.Collect: %v", err)
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
