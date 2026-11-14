package cmd

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// deferredIDLine is the whole of new-id's stdout: one bare 16-hex id. A label,
// prefix or second line is a shape a prompt capturing the id would mis-read.
var deferredIDLine = regexp.MustCompile(`^[0-9a-f]{16}$`)

// snapshotSpecs reads every phase spec.toml under the fixture's .dross tree,
// keyed by path, so a later read can prove each one byte-identical.
func snapshotSpecs(t *testing.T, dir string) map[string]string {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(dir, ".dross", "phases", "*", "spec.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) == 0 {
		t.Fatal("fixture has no spec.toml to snapshot — the byte-identity check would be vacuous")
	}
	out := map[string]string{}
	for _, p := range paths {
		out[p] = mustRead(t, p)
	}
	return out
}

// TestDeferredNewIDPrintsOneBareID: stdout is exactly one line holding the id
// and nothing else, so `$(dross deferred new-id)` is the id verbatim.
func TestDeferredNewIDPrintsOneBareID(t *testing.T) {
	setupDeferredFixture(t)
	var out string
	if err := runCmdCapturing(t, &out, Deferred(), "new-id"); err != nil {
		t.Fatalf("new-id: %v", err)
	}
	if !strings.HasSuffix(out, "\n") || strings.Count(out, "\n") != 1 {
		t.Fatalf("stdout must be exactly one line, got %q", out)
	}
	if line := strings.TrimSuffix(out, "\n"); !deferredIDLine.MatchString(line) {
		t.Errorf("stdout line %q does not match %s", line, deferredIDLine)
	}
}

// TestDeferredNewIDWritesNothing: minting an id files nothing. The project
// store stays absent (a store write would create it) and every phase spec is
// byte-identical afterwards.
func TestDeferredNewIDWritesNothing(t *testing.T) {
	dir := setupDeferredFixture(t)
	store := filepath.Join(dir, ".dross", "deferred.toml")
	if _, err := os.Stat(store); !os.IsNotExist(err) {
		t.Fatalf("fixture precondition: %s must be absent (stat err = %v)", store, err)
	}
	before := snapshotSpecs(t, dir)

	if err := runCmd(t, Deferred(), "new-id"); err != nil {
		t.Fatalf("new-id: %v", err)
	}

	if _, err := os.Stat(store); !os.IsNotExist(err) {
		t.Errorf("new-id created %s; the verb must write nothing (stat err = %v)", store, err)
	}
	for path, want := range before {
		if got := mustRead(t, path); got != want {
			t.Errorf("new-id rewrote %s:\n--- before\n%s\n--- after\n%s", path, want, got)
		}
	}
}

// TestDeferredNewIDReadsTheProjectWideIDSet: the verb mints through
// mintDeferredID, which collects every phase's ids first. A malformed spec in
// an unrelated phase therefore fails the verb and names that spec — a verb that
// called deferred.NewID with an empty used-set would print an id here instead.
// That the collected set is actually avoided is pinned by
// TestNewIDAvoidsCollisions.
func TestDeferredNewIDReadsTheProjectWideIDSet(t *testing.T) {
	dir := setupDeferredFixture(t)
	mustWrite(t, filepath.Join(dir, ".dross", "phases", "delta", "spec.toml"), "[phase\nid = \"delta\"\n")

	var out string
	err := runCmdCapturing(t, &out, Deferred(), "new-id")
	if err == nil {
		t.Fatalf("new-id must fail when another phase's spec is malformed; printed %q", out)
	}
	if want := filepath.Join("phases", "delta", "spec.toml"); !strings.Contains(err.Error(), want) {
		t.Errorf("error should name the malformed spec %s: %v", want, err)
	}
	if deferredIDLine.MatchString(strings.TrimSpace(out)) {
		t.Errorf("new-id printed an id despite failing to read the project's ids: %q", out)
	}
}

// TestDeferredNewIDIsRegistered: the verb resolves under `deferred`, and the
// README's command row lists it — a shipped verb absent from either is
// unreachable or undiscoverable.
func TestDeferredNewIDIsRegistered(t *testing.T) {
	root := &cobra.Command{Use: "dross"}
	root.AddCommand(Deferred())
	c, _, err := root.Find([]string{"deferred", "new-id"})
	if err != nil {
		t.Fatalf("deferred new-id does not resolve: %v", err)
	}
	if c.CommandPath() != "dross deferred new-id" {
		t.Errorf("resolved %q, want dross deferred new-id", c.CommandPath())
	}
	if !hasVerb(readmeDeferredVerbs(t), "new-id") {
		t.Error("README's deferred row does not list the `new-id` verb")
	}
}
