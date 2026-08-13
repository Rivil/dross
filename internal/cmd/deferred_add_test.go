package cmd

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/phase"
)

// addFixture is setupTargetFixture with a current phase set, so the default
// home is a real phase spec.
func addFixture(t *testing.T, currentPhase string) string {
	t.Helper()
	dir := setupTargetFixture(t)
	if currentPhase != "" {
		if err := runCmd(t, State(), "set", "current_phase", currentPhase); err != nil {
			t.Fatalf("set current_phase: %v", err)
		}
	}
	return dir
}

// findAdded returns the listed entry with the given text, or fails.
func findAdded(t *testing.T, text string, args ...string) deferredEntry {
	t.Helper()
	for _, e := range listJSON(t, append(args, "--json")...) {
		if e.Text == text {
			return e
		}
	}
	t.Fatalf("item %q not found in `deferred list %v`", text, args)
	return deferredEntry{}
}

// TestDeferredAddLandsInCurrentPhase is c-1 on its primary path plus the first
// arm of storage_home: with a current phase whose spec loads, the item appends
// to that spec and is listed immediately.
func TestDeferredAddLandsInCurrentPhase(t *testing.T) {
	dir := addFixture(t, "alpha")
	if err := runCmd(t, Deferred(), "add", "a fresh finding"); err != nil {
		t.Fatalf("add: %v", err)
	}
	got := findAdded(t, "a fresh finding")
	if got.Source != "alpha" {
		t.Errorf("source = %q, want alpha — a project-store write means the current phase was ignored", got.Source)
	}
	// The fixture's alpha already holds two items, so the new one is index 2.
	if got.Index != 2 {
		t.Errorf("index = %d, want 2 (appended after the existing items)", got.Index)
	}
	if !strings.Contains(mustRead(t, filepath.Join(dir, ".dross", "phases", "alpha", "spec.toml")), "a fresh finding") {
		t.Error("item not written to alpha/spec.toml")
	}
}

// TestDeferredAddWithNoCurrentPhase is c-2: the command never refuses for want
// of a home. With nothing scaffolded as current, the item lands in the store.
func TestDeferredAddWithNoCurrentPhase(t *testing.T) {
	addFixture(t, "")
	if err := runCmd(t, Deferred(), "add", "homeless finding"); err != nil {
		t.Fatalf("add with no current phase must not error: %v", err)
	}
	if got := findAdded(t, "homeless finding"); got.Source != projectStoreSlug {
		t.Errorf("source = %q, want %q", got.Source, projectStoreSlug)
	}
}

// TestDeferredAddFallsBackFromUnusableHome is the unusable-home arm the locked
// storage_home decision spells out: a current_phase whose spec.toml is missing
// falls back to the store rather than erroring, and says so.
func TestDeferredAddFallsBackFromUnusableHome(t *testing.T) {
	dir := addFixture(t, "ghost")
	var out string
	if err := runCmdCapturing(t, &out, Deferred(), "add", "orphaned finding"); err != nil {
		t.Fatalf("add with an unusable home must not error: %v", err)
	}
	if got := findAdded(t, "orphaned finding"); got.Source != projectStoreSlug {
		t.Errorf("source = %q, want %q", got.Source, projectStoreSlug)
	}
	if !strings.Contains(out, projectStoreSlug) {
		t.Errorf("stdout should name the fallback home:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(dir, ".dross", "phases", "ghost", "spec.toml")); err == nil {
		t.Error("add materialised a spec.toml for the unusable phase; it must fall back, not create")
	}
}

// TestDeferredAddWhyRoundTrips: --why is optional and must not leave an empty
// `why =` line behind when omitted.
func TestDeferredAddWhyRoundTrips(t *testing.T) {
	dir := addFixture(t, "alpha")
	if err := runCmd(t, Deferred(), "add", "bare item"); err != nil {
		t.Fatalf("add: %v", err)
	}
	body := mustRead(t, filepath.Join(dir, ".dross", "phases", "alpha", "spec.toml"))
	if strings.Contains(body, `why = ""`) {
		t.Errorf("empty why emitted:\n%s", body)
	}
	if err := runCmd(t, Deferred(), "add", "explained item", "--why", "because reasons"); err != nil {
		t.Fatalf("add --why: %v", err)
	}
	if got := findAdded(t, "explained item"); got.Why != "because reasons" {
		t.Errorf("why = %q, want %q", got.Why, "because reasons")
	}
}

// TestDeferredAddTargetRoutesInOneCommand is c-3: --target files and routes in
// one go, and omitting it leaves the item someday. Both halves are asserted so
// an implementation that defaults the target to anything non-empty fails.
func TestDeferredAddTargetRoutesInOneCommand(t *testing.T) {
	addFixture(t, "alpha")

	if err := runCmd(t, Deferred(), "add", "routed on arrival", "--target", "beta"); err != nil {
		t.Fatalf("add --target: %v", err)
	}
	findAdded(t, "routed on arrival", "--routed", "--target", "beta")
	for _, e := range listJSON(t, "--someday", "--json") {
		if e.Text == "routed on arrival" {
			t.Error("a routed item must not appear in --someday")
		}
	}

	if err := runCmd(t, Deferred(), "add", "parked for later"); err != nil {
		t.Fatalf("add: %v", err)
	}
	findAdded(t, "parked for later", "--someday")
	for _, e := range listJSON(t, "--routed", "--json") {
		if e.Text == "parked for later" {
			t.Error("an unrouted item must not appear in --routed")
		}
	}
}

// TestDeferredAddRejectsBadTargetIdenticallyToRoute is c-7 reaching the new
// verb. The two rejections are compared byte-for-byte in one assertion, so the
// paths cannot drift into two different rules with two different messages.
func TestDeferredAddRejectsBadTargetIdenticallyToRoute(t *testing.T) {
	dir := addFixture(t, "alpha")
	specPath := filepath.Join(dir, ".dross", "phases", "alpha", "spec.toml")
	before := mustRead(t, specPath)

	addErr := runCmd(t, Deferred(), "add", "doomed", "--target", "typo-slug")
	routeErr := runCmd(t, Deferred(), "route", "alpha", "0", "--target", "typo-slug")
	if addErr == nil {
		t.Fatal("add --target typo-slug should be rejected")
	}
	if routeErr == nil {
		t.Fatal("route --target typo-slug should be rejected")
	}
	if addErr.Error() != routeErr.Error() {
		t.Errorf("add and route rejections have drifted:\n add:   %s\n route: %s", addErr, routeErr)
	}
	if got := mustRead(t, specPath); got != before {
		t.Errorf("a rejected add wrote to the spec — no orphan item with a dead target may be left:\n%s", got)
	}
}

// TestDeferredAddRejectedTargetMakesNoBoardCall proves validation precedes the
// board mirror as well as the local write: the server errors the test on ANY
// request, so a single call fails here.
func TestDeferredAddRejectedTargetMakesNoBoardCall(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		t.Errorf("a rejected add must make no board call, got %s %s", r.Method, r.URL.Path)
	}))
	t.Cleanup(srv.Close)

	dir := youtrackBoardRepo(t, srv.URL)
	writeSpec(t, dir, "alpha", "[phase]\nid = \"alpha\"\ntitle = \"Alpha\"\n\n[[criteria]]\nid = \"c-1\"\ntext = \"x\"\n")
	if err := runCmd(t, State(), "set", "current_phase", "alpha"); err != nil {
		t.Fatal(err)
	}
	if err := runCmd(t, Deferred(), "add", "doomed", "--target", "typo-slug"); err == nil {
		t.Fatal("add --target typo-slug should be rejected")
	}
}

// TestDeferredAddRejectsEmptyText: a blank [[deferred]] records nothing about
// what was found, so it is refused rather than appended.
func TestDeferredAddRejectsEmptyText(t *testing.T) {
	dir := addFixture(t, "alpha")
	before := mustRead(t, filepath.Join(dir, ".dross", "phases", "alpha", "spec.toml"))
	if err := runCmd(t, Deferred(), "add", ""); err == nil {
		t.Fatal("add with empty text should be rejected")
	}
	if got := mustRead(t, filepath.Join(dir, ".dross", "phases", "alpha", "spec.toml")); got != before {
		t.Error("a rejected add appended a blank item")
	}
}

// TestDeferredAddMintsDistinctIDs: two adds into the same phase must not share
// an id — a collision would silently merge their board links.
func TestDeferredAddMintsDistinctIDs(t *testing.T) {
	dir := addFixture(t, "alpha")
	if err := runCmd(t, Deferred(), "add", "first add"); err != nil {
		t.Fatal(err)
	}
	if err := runCmd(t, Deferred(), "add", "second add"); err != nil {
		t.Fatal(err)
	}
	// Read the spec, not `list --json`: the id is deliberately absent from the
	// JSON wire (locked deferred_identity), so on-disk is the only observer.
	spec, err := phase.LoadSpec(filepath.Join(dir, ".dross", "phases", "alpha", "spec.toml"))
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]string{}
	for _, d := range spec.Deferred {
		if d.Text != "first add" && d.Text != "second add" {
			continue
		}
		if d.ID == "" {
			t.Fatalf("no id assigned on add for %q", d.Text)
		}
		if prev, dup := ids[d.ID]; dup {
			t.Errorf("%q and %q share id %q", prev, d.Text, d.ID)
		}
		ids[d.ID] = d.Text
	}
	if len(ids) != 2 {
		t.Errorf("want 2 distinct ids, got %d: %v", len(ids), ids)
	}
}

// TestDeferredAddedItemIsIndistinguishable is c-4 on its primary path: the
// `<phase> <idx>` handle an add returns round-trips through route, unroute and
// dismiss exactly as a spec-authored item's does. (t-5 proves the same for the
// project store.)
func TestDeferredAddedItemIsIndistinguishable(t *testing.T) {
	addFixture(t, "alpha")
	if err := runCmd(t, Deferred(), "add", "addressable item"); err != nil {
		t.Fatal(err)
	}
	e := findAdded(t, "addressable item")
	idx := itoa(e.Index)

	if err := runCmd(t, Deferred(), "route", e.Source, idx, "--target", "beta"); err != nil {
		t.Fatalf("route an added item: %v", err)
	}
	if got := findAdded(t, "addressable item"); got.Target != "beta" {
		t.Errorf("target = %q, want beta", got.Target)
	}
	if err := runCmd(t, Deferred(), "unroute", e.Source, idx); err != nil {
		t.Fatalf("unroute an added item: %v", err)
	}
	if got := findAdded(t, "addressable item"); got.Target != "" {
		t.Errorf("target = %q after unroute, want empty", got.Target)
	}
	if err := runCmd(t, Deferred(), "dismiss", e.Source, idx); err != nil {
		t.Fatalf("dismiss an added item: %v", err)
	}
	if got := findAdded(t, "addressable item", "--dismissed"); !got.Dismissed {
		t.Error("added item not dismissed")
	}
}

// TestReadmeListsDeferredAdd: the command table is the surface a user reads to
// learn the verb exists. A shipped command absent from it is undiscoverable.
func TestReadmeListsDeferredAdd(t *testing.T) {
	if !strings.Contains(docText(t, "README.md"), "dross deferred {list,route,unroute,dismiss,add}") {
		t.Error("README's deferred row does not list the `add` verb")
	}
}

// TestDeferredAddReplaysHomelessFindings replays the two findings c-5 names, on
// a fixture, in exactly the shapes they were filed in: one bare `add` that must
// land as someday, and one `add --target` at a slug that exists ONLY in an
// earlier milestone's phases array — the sole end-to-end exercise of t-2's
// milestone-array branch through the `add` verb.
func TestDeferredAddReplaysHomelessFindings(t *testing.T) {
	addFixture(t, "alpha")

	const (
		hermeticity = "test-suite hermeticity gap: a test read the real repo's gitignored state.json"
		vocabulary  = "verify stdout and verify.toml disagree on survivor vocabulary"
	)

	if err := runCmd(t, Deferred(), "add", hermeticity,
		"--why", "found by CI, not by dross"); err != nil {
		t.Fatalf("bare add: %v", err)
	}
	// watch-pr-ci-status stands in for mutation-score-truth: a slug parked in an
	// earlier milestone's array with no phase directory, which is precisely the
	// shape the real filing used.
	if err := runCmd(t, Deferred(), "add", vocabulary,
		"--target", "watch-pr-ci-status"); err != nil {
		t.Fatalf("routed add at a milestone-array-only slug: %v", err)
	}

	if got := findAdded(t, hermeticity, "--someday"); got.Target != "" {
		t.Errorf("the bare filing should be someday, got target %q", got.Target)
	}
	if got := findAdded(t, vocabulary, "--target", "watch-pr-ci-status"); got.Target != "watch-pr-ci-status" {
		t.Errorf("target = %q, want watch-pr-ci-status", got.Target)
	}
	// Neither filing may leave a dangling destination behind.
	if err := runCmd(t, Validate()); err != nil {
		t.Errorf("validate should stay green after both filings: %v", err)
	}
}
