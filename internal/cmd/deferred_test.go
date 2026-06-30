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

// TestDeferredListDismissedHidden: a dismissed item drops out of --someday and
// the default listing, and is reachable only via --dismissed (with the field
// carried in JSON).
func TestDeferredListDismissedHidden(t *testing.T) {
	setupDeferredFixture(t)

	// gamma[0] starts as someday; dismiss it.
	if err := runCmd(t, Deferred(), "dismiss", "gamma", "0"); err != nil {
		t.Fatalf("dismiss: %v", err)
	}

	for _, e := range listJSON(t, "--someday", "--json") {
		if e.Source == "gamma" {
			t.Errorf("--someday surfaced a dismissed item: %+v", e)
		}
	}
	for _, e := range listJSON(t, "--json") {
		if e.Source == "gamma" {
			t.Errorf("default listing surfaced a dismissed item: %+v", e)
		}
	}

	dismissed := listJSON(t, "--dismissed", "--json")
	if len(dismissed) != 1 {
		t.Fatalf("--dismissed should return exactly the 1 dismissed entry, got %d: %+v", len(dismissed), dismissed)
	}
	if dismissed[0].Source != "gamma" || !dismissed[0].Dismissed {
		t.Errorf("--dismissed entry wrong: %+v (want gamma, Dismissed=true)", dismissed[0])
	}
}

// TestDeferredListDismissedMarker: the table renders (dismissed) in the TARGET
// column for a dismissed item, not (someday).
func TestDeferredListDismissedMarker(t *testing.T) {
	setupDeferredFixture(t)

	if err := runCmd(t, Deferred(), "dismiss", "gamma", "0"); err != nil {
		t.Fatalf("dismiss: %v", err)
	}

	var out string
	if err := runCmdCapturing(t, &out, Deferred(), "list", "--dismissed"); err != nil {
		t.Fatalf("list --dismissed: %v", err)
	}
	if !strings.Contains(out, "(dismissed)") {
		t.Errorf("dismissed item should render (dismissed) in TARGET column, got:\n%s", out)
	}
	if strings.Contains(out, "(someday)") {
		t.Errorf("dismissed item must not render as (someday), got:\n%s", out)
	}
}

// TestDeferredUnrouteRoundTrip: route an item, then unroute it — the target is
// cleared on disk and the entry reappears under --someday.
func TestDeferredUnrouteRoundTrip(t *testing.T) {
	dir := setupDeferredFixture(t)

	if err := runCmd(t, Deferred(), "route", "gamma", "0", "--target", "alpha"); err != nil {
		t.Fatalf("route: %v", err)
	}
	if err := runCmd(t, Deferred(), "unroute", "gamma", "0"); err != nil {
		t.Fatalf("unroute: %v", err)
	}
	spec, err := phase.LoadSpec(filepath.Join(dir, ".dross", "phases", "gamma", "spec.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if spec.Deferred[0].Target != "" {
		t.Errorf("unroute did not clear target: got %q want \"\"", spec.Deferred[0].Target)
	}

	found := false
	for _, e := range listJSON(t, "--someday", "--json") {
		if e.Source == "gamma" && e.Index == 0 {
			found = true
		}
	}
	if !found {
		t.Error("un-routed item should reappear under --someday")
	}
}

// TestDeferredUnrouteOutOfRange: an out-of-range idx errors (naming the range)
// rather than panicking, matching dismiss.
func TestDeferredUnrouteOutOfRange(t *testing.T) {
	setupDeferredFixture(t)

	err := runCmd(t, Deferred(), "unroute", "gamma", "5")
	if err == nil {
		t.Fatal("unroute with out-of-range idx should error, got nil")
	}
	if !strings.Contains(err.Error(), "out of range") {
		t.Errorf("out-of-range error should name the range, got %q", err)
	}
}

// TestDeferredUnrouteMissingPhase: a missing phase spec surfaces LoadSpec's error
// rather than panicking.
func TestDeferredUnrouteMissingPhase(t *testing.T) {
	setupDeferredFixture(t)

	if err := runCmd(t, Deferred(), "unroute", "no-such-phase", "0"); err == nil {
		t.Error("unroute on a missing phase should error, got nil")
	}
}

// TestDeferredUnrouteIdempotentSomeday: un-routing an already-someday item is a
// no-op success that prints the "already someday" message.
func TestDeferredUnrouteIdempotentSomeday(t *testing.T) {
	dir := setupDeferredFixture(t)

	// gamma[0] starts as someday (no target).
	var out string
	if err := runCmdCapturing(t, &out, Deferred(), "unroute", "gamma", "0"); err != nil {
		t.Fatalf("unroute on someday item should succeed, got %v", err)
	}
	if !strings.Contains(out, "already someday") {
		t.Errorf("idempotent path should print the \"already someday\" message, got:\n%s", out)
	}
	spec, err := phase.LoadSpec(filepath.Join(dir, ".dross", "phases", "gamma", "spec.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if spec.Deferred[0].Target != "" {
		t.Errorf("someday item must stay someday after unroute: got target %q", spec.Deferred[0].Target)
	}
}

// TestDeferredUnrouteDismissedGuard: un-routing a dismissed item is refused with a
// pointer to `dismiss --undo`, and leaves the item dismissed.
func TestDeferredUnrouteDismissedGuard(t *testing.T) {
	dir := setupDeferredFixture(t)

	// gamma[0] is someday; dismiss it so it carries Dismissed (and empty Target).
	if err := runCmd(t, Deferred(), "dismiss", "gamma", "0"); err != nil {
		t.Fatalf("dismiss: %v", err)
	}
	err := runCmd(t, Deferred(), "unroute", "gamma", "0")
	if err == nil {
		t.Fatal("unroute on a dismissed item should error, got nil")
	}
	if !strings.Contains(err.Error(), "dismiss --undo") {
		t.Errorf("dismissed-guard error should point to `dismiss --undo`, got %q", err)
	}
	spec, lerr := phase.LoadSpec(filepath.Join(dir, ".dross", "phases", "gamma", "spec.toml"))
	if lerr != nil {
		t.Fatal(lerr)
	}
	if !spec.Deferred[0].Dismissed {
		t.Error("dismissed item must remain dismissed after a rejected unroute")
	}
}

func intToStr(i int) string {
	return strconv.Itoa(i)
}
