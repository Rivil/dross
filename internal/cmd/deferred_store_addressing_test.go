package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// storeFixture is the target fixture with one item already filed in the project
// store, addressable as `_project 0`.
func storeFixture(t *testing.T) string {
	t.Helper()
	dir := setupTargetFixture(t)
	mustWrite(t, filepath.Join(dir, ".dross", "deferred.toml"),
		`[phase]
  id = "_project"
  title = "project-level deferred store"

[[deferred]]
  id = "store0000"
  text = "store item"
`)
	return dir
}

// specSnapshot records every phases/*/spec.toml so a store operation can be
// proved not to have touched any of them.
func specSnapshot(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	phasesDir := filepath.Join(dir, ".dross", "phases")
	entries, err := os.ReadDir(phasesDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		p := filepath.Join(phasesDir, e.Name(), "spec.toml")
		if _, err := os.Stat(p); err == nil {
			out[p] = mustRead(t, p)
		}
	}
	return out
}

// TestDeferredRouteAddressesStore is c-4 for the store: `route _project 0`
// rewrites .dross/deferred.toml and no phase spec. A verb still building its
// path from phase.Dir would look for .dross/phases/_project/spec.toml and fail.
func TestDeferredRouteAddressesStore(t *testing.T) {
	dir := storeFixture(t)
	before := specSnapshot(t, dir)

	if err := runCmd(t, Deferred(), "route", projectStoreSlug, "0", "--target", "beta"); err != nil {
		t.Fatalf("route a store item: %v", err)
	}
	storePath := filepath.Join(dir, ".dross", "deferred.toml")
	if !strings.Contains(mustRead(t, storePath), `target = "beta"`) {
		t.Errorf("target not stamped in the store:\n%s", mustRead(t, storePath))
	}
	if _, err := os.Stat(filepath.Join(dir, ".dross", "phases", projectStoreSlug, "spec.toml")); err == nil {
		t.Error("route materialised .dross/phases/_project/spec.toml — the store is not a phase dir")
	}
	for path, body := range before {
		if got := mustRead(t, path); got != body {
			t.Errorf("%s was rewritten by a store-scoped route", path)
		}
	}
}

// TestDeferredUnrouteDismissAddressStore: the rest of c-4's four verbs. The
// dismiss guard behaves identically too — a routed store item is refused with
// the same "un-route before dismissing" message a phase-sourced item gets.
func TestDeferredUnrouteDismissAddressStore(t *testing.T) {
	dir := storeFixture(t)

	if err := runCmd(t, Deferred(), "route", projectStoreSlug, "0", "--target", "beta"); err != nil {
		t.Fatalf("route: %v", err)
	}
	storeErr := runCmd(t, Deferred(), "dismiss", projectStoreSlug, "0")
	if storeErr == nil {
		t.Fatal("dismissing a routed store item should be refused")
	}
	if !strings.Contains(storeErr.Error(), "un-route before dismissing") {
		t.Errorf("store dismiss guard differs from the phase one: %v", storeErr)
	}
	// The same refusal for a phase-sourced routed item (alpha[0] → beta).
	phaseErr := runCmd(t, Deferred(), "dismiss", "alpha", "0")
	if phaseErr == nil || !strings.Contains(phaseErr.Error(), "un-route before dismissing") {
		t.Errorf("phase dismiss guard: %v", phaseErr)
	}

	if err := runCmd(t, Deferred(), "unroute", projectStoreSlug, "0"); err != nil {
		t.Fatalf("unroute a store item: %v", err)
	}
	if strings.Contains(mustRead(t, filepath.Join(dir, ".dross", "deferred.toml")), `target = "beta"`) {
		t.Error("unroute did not clear the store item's target")
	}
	if err := runCmd(t, Deferred(), "dismiss", projectStoreSlug, "0"); err != nil {
		t.Fatalf("dismiss an un-routed store item: %v", err)
	}
}

// TestDeferredStoreIndexOutOfRange: an out-of-range store index errors in the
// same shape a phase source does — naming the source and its item count — and
// writes nothing. No panic, no silent append at the end.
func TestDeferredStoreIndexOutOfRange(t *testing.T) {
	dir := storeFixture(t)
	storePath := filepath.Join(dir, ".dross", "deferred.toml")
	before := mustRead(t, storePath)

	err := runCmd(t, Deferred(), "dismiss", projectStoreSlug, "7")
	if err == nil {
		t.Fatal("dismiss _project 7 against a 1-item store should error")
	}
	for _, want := range []string{"index 7", "out of range", projectStoreSlug, "1 deferred item"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error missing %q: %v", want, err)
		}
	}
	if got := mustRead(t, storePath); got != before {
		t.Errorf("an out-of-range dismiss rewrote the store:\n%s", got)
	}
}

// TestDeferredListMilestoneExcludesStore: --milestone scopes by source phase,
// and the store has no source phase, so store items fall outside every
// milestone while still showing in the default listing.
func TestDeferredListMilestoneExcludesStore(t *testing.T) {
	storeFixture(t)

	for _, e := range listJSON(t, "--milestone", "v0.5", "--json") {
		if e.Source == projectStoreSlug {
			t.Errorf("store item leaked into --milestone v0.5: %+v", e)
		}
	}
	var found bool
	for _, e := range listJSON(t, "--json") {
		if e.Source == projectStoreSlug {
			found = true
		}
	}
	if !found {
		t.Error("store item missing from the default listing")
	}
}

// TestDeferredStoreObeysThreeStateFilter: the store is subject to the same
// someday/routed/dismissed filtering a spec is — a dismissed store item is
// hidden by default and by --someday, and shown by --dismissed.
func TestDeferredStoreObeysThreeStateFilter(t *testing.T) {
	storeFixture(t)
	if err := runCmd(t, Deferred(), "dismiss", projectStoreSlug, "0"); err != nil {
		t.Fatalf("dismiss: %v", err)
	}

	for _, args := range [][]string{{"--json"}, {"--someday", "--json"}} {
		for _, e := range listJSON(t, args...) {
			if e.Source == projectStoreSlug {
				t.Errorf("dismissed store item shown by `list %v`", args)
			}
		}
	}
	var shown bool
	for _, e := range listJSON(t, "--dismissed", "--json") {
		if e.Source == projectStoreSlug && e.Dismissed {
			shown = true
		}
	}
	if !shown {
		t.Error("dismissed store item not shown by --dismissed")
	}
}
