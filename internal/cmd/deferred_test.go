package cmd

import (
	"encoding/json"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/phase"
)

// setupDeferredFixture builds a temp project with three phases and a milestone.
// Flattened (phase.List sorts): alpha[0]→beta(routed), alpha[1]→someday,
// beta[0]→alpha(routed), gamma[0]→someday. Milestone v0.5 = [alpha, beta]
// (gamma is outside it). Returns the project dir.
func setupDeferredFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	chdir(t, dir)
	if err := runCmd(t, Init()); err != nil {
		t.Fatal(err)
	}
	mustRunSet(t, "project.name", "test-app")
	mustRunSet(t, "runtime.mode", "native")

	mustWrite(t, filepath.Join(dir, ".dross", "milestones", "v0.5.toml"),
		"phases = [\"alpha\", \"beta\"]\n\n[milestone]\n  version = \"v0.5\"\n  title = \"X\"\n")

	mustWrite(t, filepath.Join(dir, ".dross", "phases", "alpha", "spec.toml"),
		`[phase]
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
	mustWrite(t, filepath.Join(dir, ".dross", "phases", "beta", "spec.toml"),
		`[phase]
id = "beta"
title = "Beta"

[[criteria]]
id = "c-1"
text = "x"

[[deferred]]
text = "beta routed idea"
target = "alpha"
`)
	mustWrite(t, filepath.Join(dir, ".dross", "phases", "gamma", "spec.toml"),
		`[phase]
id = "gamma"
title = "Gamma"

[[criteria]]
id = "c-1"
text = "x"

[[deferred]]
text = "gamma someday idea"
`)
	return dir
}

func listJSON(t *testing.T, args ...string) []deferredEntry {
	t.Helper()
	var out string
	if err := runCmdCapturing(t, &out, Deferred(), append([]string{"list"}, args...)...); err != nil {
		t.Fatalf("deferred list %v: %v", args, err)
	}
	var entries []deferredEntry
	if err := json.Unmarshal([]byte(out), &entries); err != nil {
		t.Fatalf("unmarshal %q: %v", out, err)
	}
	return entries
}

// TestDeferredListSomedayRoutedComplement: --someday and --routed partition the
// full set — disjoint, and together they cover every entry.
func TestDeferredListSomedayRoutedComplement(t *testing.T) {
	setupDeferredFixture(t)

	all := listJSON(t, "--json")
	someday := listJSON(t, "--someday", "--json")
	routed := listJSON(t, "--routed", "--json")

	if len(all) != 4 {
		t.Fatalf("expected 4 deferred entries total, got %d", len(all))
	}
	for _, e := range someday {
		if e.Target != "" {
			t.Errorf("--someday returned a routed row: %+v", e)
		}
	}
	for _, e := range routed {
		if e.Target == "" {
			t.Errorf("--routed returned a someday row: %+v", e)
		}
	}
	if len(someday)+len(routed) != len(all) {
		t.Errorf("--someday (%d) + --routed (%d) should equal all (%d) — not exact complements", len(someday), len(routed), len(all))
	}
}

// TestDeferredListJSONSourceIndex: --target filters to the routed entry and the
// JSON carries its originating phase (source) and per-phase index.
func TestDeferredListJSONSourceIndex(t *testing.T) {
	setupDeferredFixture(t)

	got := listJSON(t, "--target", "beta", "--json")
	if len(got) != 1 {
		t.Fatalf("expected exactly 1 entry routed to beta, got %d: %+v", len(got), got)
	}
	e := got[0]
	if e.Source != "alpha" {
		t.Errorf("source field wrong: got %q want alpha", e.Source)
	}
	if e.Index != 0 {
		t.Errorf("index handle wrong: got %d want 0", e.Index)
	}
	if e.Target != "beta" {
		t.Errorf("target wrong: got %q want beta", e.Target)
	}
}

// TestDeferredListMilestoneScope: --milestone restricts to entries whose source
// phase is in that milestone's phases array (gamma is excluded).
func TestDeferredListMilestoneScope(t *testing.T) {
	setupDeferredFixture(t)

	got := listJSON(t, "--milestone", "v0.5", "--json")
	if len(got) != 3 {
		t.Fatalf("expected 3 entries from alpha+beta, got %d: %+v", len(got), got)
	}
	for _, e := range got {
		if e.Source == "gamma" {
			t.Errorf("--milestone v0.5 leaked an entry from gamma (outside its phases array): %+v", e)
		}
	}
}

// TestDeferredRouteRoundTrip: route stamps target on disk; reload reflects it.
func TestDeferredRouteRoundTrip(t *testing.T) {
	dir := setupDeferredFixture(t)

	if err := runCmd(t, Deferred(), "route", "gamma", "0", "--target", "alpha"); err != nil {
		t.Fatalf("route: %v", err)
	}
	spec, err := phase.LoadSpec(filepath.Join(dir, ".dross", "phases", "gamma", "spec.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if spec.Deferred[0].Target != "alpha" {
		t.Errorf("route did not persist target: got %q want alpha", spec.Deferred[0].Target)
	}
}

// TestDeferredListRouteHandoff: the idx surfaced by `list --json` is exactly the
// handle `route` consumes — take a someday entry's source+index from JSON, route
// it, reload, and assert that exact entry now carries the target.
func TestDeferredListRouteHandoff(t *testing.T) {
	dir := setupDeferredFixture(t)

	someday := listJSON(t, "--someday", "--json")
	var pick *deferredEntry
	for i := range someday {
		if someday[i].Source == "gamma" {
			pick = &someday[i]
			break
		}
	}
	if pick == nil {
		t.Fatal("fixture should expose a someday entry from gamma")
	}

	if err := runCmd(t, Deferred(), "route", pick.Source, intToStr(pick.Index), "--target", "alpha"); err != nil {
		t.Fatalf("route via list-supplied idx: %v", err)
	}
	spec, err := phase.LoadSpec(filepath.Join(dir, ".dross", "phases", pick.Source, "spec.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if spec.Deferred[pick.Index].Target != "alpha" {
		t.Errorf("list→route handoff broken: entry %s[%d] target=%q want alpha", pick.Source, pick.Index, spec.Deferred[pick.Index].Target)
	}
}

// TestDeferredDismissRoundTrip: dismissing a someday item persists Dismissed on
// disk; reload reflects it.
func TestDeferredDismissRoundTrip(t *testing.T) {
	dir := setupDeferredFixture(t)

	if err := runCmd(t, Deferred(), "dismiss", "gamma", "0"); err != nil {
		t.Fatalf("dismiss: %v", err)
	}
	spec, err := phase.LoadSpec(filepath.Join(dir, ".dross", "phases", "gamma", "spec.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if !spec.Deferred[0].Dismissed {
		t.Errorf("dismiss did not persist: gamma[0] Dismissed=%v want true", spec.Deferred[0].Dismissed)
	}
}

// TestDeferredDismissUndo: --undo clears the dismissed state back to someday.
func TestDeferredDismissUndo(t *testing.T) {
	dir := setupDeferredFixture(t)

	if err := runCmd(t, Deferred(), "dismiss", "gamma", "0"); err != nil {
		t.Fatalf("dismiss: %v", err)
	}
	if err := runCmd(t, Deferred(), "dismiss", "--undo", "gamma", "0"); err != nil {
		t.Fatalf("dismiss --undo: %v", err)
	}
	spec, err := phase.LoadSpec(filepath.Join(dir, ".dross", "phases", "gamma", "spec.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if spec.Deferred[0].Dismissed {
		t.Errorf("--undo did not clear dismissed: gamma[0] Dismissed=%v want false", spec.Deferred[0].Dismissed)
	}
}

// TestDeferredDismissOutOfRange: an out-of-range idx errors rather than silently
// succeeding.
func TestDeferredDismissOutOfRange(t *testing.T) {
	setupDeferredFixture(t)

	if err := runCmd(t, Deferred(), "dismiss", "gamma", "5"); err == nil {
		t.Error("dismiss with out-of-range idx should error, got nil")
	}
}

// TestDeferredDismissRoutedGuard: dismiss is someday-only — a routed item errors
// and is left untouched.
func TestDeferredDismissRoutedGuard(t *testing.T) {
	dir := setupDeferredFixture(t)

	// alpha[0] is routed to beta.
	err := runCmd(t, Deferred(), "dismiss", "alpha", "0")
	if err == nil {
		t.Fatal("dismiss on a routed item should error, got nil")
	}
	if !strings.Contains(err.Error(), "un-route") {
		t.Errorf("routed-guard error should mention un-routing, got %q", err)
	}
	spec, lerr := phase.LoadSpec(filepath.Join(dir, ".dross", "phases", "alpha", "spec.toml"))
	if lerr != nil {
		t.Fatal(lerr)
	}
	if spec.Deferred[0].Dismissed {
		t.Error("routed item must not be dismissed after a rejected dismiss")
	}
}

func intToStr(i int) string {
	return strconv.Itoa(i)
}
