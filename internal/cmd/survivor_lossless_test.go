package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"

	"github.com/Rivil/dross/internal/survivor"
)

// synthGitHubToken is GitHub-PAT-shaped (ghp_ + 36 alphanumerics) but built at
// runtime, so this source file never holds one for the repo's own scan to find.
func synthGitHubToken() string {
	return "ghp_" + strings.Repeat("Ab1", 12)
}

// markedStore is a survivors.toml whose one acceptance carries token-shaped
// source text, allowed by the trailing marker — the shape feastahead's 130
// accepts stripped down to nothing.
func markedStore() string {
	return "[[accepted]]\n  key = \"seeded\"\n  file = \"internal/x.go\"\n  op = \"OP\"\n  text = \"" +
		synthGitHubToken() + "\"  # dross:allow-secret\n  reason = \"seeded by the test\"\n"
}

// TestSurvivorAcceptKeepsAllowSecretMarkers: `dross survivor accept` adds an
// entry and leaves every original line — the marker included — byte-identical,
// so `dross validate` stays green. The negative control re-encodes the same
// store whole and shows validate then reports the secret: the fixture does trip
// the scanner, and the green is the marker surviving, not a blind scan.
//
// validate's artifact scan walks .dross on disk outside a git work tree, which
// is where this fixture lives, so the store needs no commit to be seen.
func TestSurvivorAcceptKeepsAllowSecretMarkers(t *testing.T) {
	dir := setupSurvivorFixture(t)
	store := storeFileOf(dir)
	src := markedStore()
	mustWrite(t, store, src)

	if err := runCmd(t, Survivor(), "accept", "internal/x.go:4",
		"--op", "CONDITIONALS_BOUNDARY", "--reason", "accepted by the test"); err != nil {
		t.Fatalf("accept: %v", err)
	}
	after := mustRead(t, store)
	if !strings.HasPrefix(after, src) {
		t.Fatalf("accept rewrote the existing entry:\n%s", after)
	}
	if out, err := runValidate(t); err != nil {
		t.Fatalf("validate failed after an accept: %v\n%s", err, out)
	}

	var s survivor.Store
	if _, err := toml.Decode(after, &s); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(s); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, store, buf.String())
	if out, err := runValidate(t); err == nil || !strings.Contains(out, "secret:") {
		t.Fatalf("a re-encoded store passed validate (err %v) — the fixture does not trip the scanner:\n%s", err, out)
	}
}

// TestSurvivorRouteLeavesStoreUntouched: route writes the current phase's spec,
// never survivors.toml.
func TestSurvivorRouteLeavesStoreUntouched(t *testing.T) {
	dir := setupSurvivorFixture(t)
	store := storeFileOf(dir)
	src := markedStore()
	mustWrite(t, store, src)

	if err := runCmd(t, Survivor(), "route", "internal/x.go:4",
		"--op", "CONDITIONALS_BOUNDARY", "--target", "beta"); err != nil {
		t.Fatalf("route: %v", err)
	}
	if got := mustRead(t, store); got != src {
		t.Errorf("route changed survivors.toml:\n%s", got)
	}
}
