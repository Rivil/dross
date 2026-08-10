package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/Rivil/dross/internal/phase"
	"github.com/Rivil/dross/internal/survivor"
)

// setupSurvivorFixture builds a repo with two phases (alpha current, beta a
// routing destination) and a source file holding one unique and one duplicated
// line. Returns the repo dir.
func setupSurvivorFixture(t *testing.T) string {
	t.Helper()
	dir := realTempDir(t)
	chdir(t, dir)
	if err := runCmd(t, Init()); err != nil {
		t.Fatal(err)
	}
	mustRunSet(t, "project.name", "test-app")
	mustRunSet(t, "runtime.mode", "native")

	for _, id := range []string{"alpha", "beta"} {
		mustWrite(t, filepath.Join(dir, ".dross", "phases", id, "spec.toml"),
			"[phase]\nid = \""+id+"\"\ntitle = \""+id+"\"\n\n[[criteria]]\nid = \"c-1\"\ntext = \"x\"\n")
	}
	if err := runCmd(t, State(), "set", "current_phase", "alpha"); err != nil {
		t.Fatal(err)
	}

	mustWrite(t, filepath.Join(dir, "internal", "x.go"), `package x

func f(limit int) error {
	if limit > 0 {
		return nil
	}
	return nil
}
`)
	return dir
}

func storeFileOf(dir string) string {
	return filepath.Join(dir, ".dross", survivor.StoreFile)
}

// TestSurvivorAcceptRequiresAReason is c-2 at the CLI boundary: an acceptance
// with no reason and no category must fail and leave no store behind. A store
// created by a rejected accept would suppress nothing but would look, to the
// next reader, like a deliberate record.
func TestSurvivorAcceptRequiresAReason(t *testing.T) {
	dir := setupSurvivorFixture(t)

	err := runCmd(t, Survivor(), "accept", "internal/x.go:4", "--op", "CONDITIONALS_BOUNDARY")
	if err == nil {
		t.Fatal("accept with no reason and no category succeeded, want error")
	}
	if _, statErr := os.Stat(storeFileOf(dir)); !os.IsNotExist(statErr) {
		t.Errorf("rejected accept left a store behind (stat err = %v)", statErr)
	}
}

// TestSurvivorAcceptWritesRepoRootStore pins the acceptance_store lock: the
// record lands at <root>/.dross/survivors.toml, not under a phase dir, and is
// readable from a nested subdirectory. A phase-local store would die at the
// squash-merge exactly the way the board mapping did.
func TestSurvivorAcceptWritesRepoRootStore(t *testing.T) {
	dir := setupSurvivorFixture(t)

	if err := runCmd(t, Survivor(), "accept", "internal/x.go:4",
		"--op", "CONDITIONALS_BOUNDARY", "--reason", "boundary is unobservable here"); err != nil {
		t.Fatalf("accept: %v", err)
	}

	if _, err := os.Stat(storeFileOf(dir)); err != nil {
		t.Fatalf("no repo-root store written: %v", err)
	}
	phaseDir := filepath.Join(dir, ".dross", "phases", "alpha")
	if _, err := os.Stat(filepath.Join(phaseDir, survivor.StoreFile)); err == nil {
		t.Error("acceptance written under the phase dir — the store must be repo-level")
	}

	store, err := survivor.Load(storeFileOf(dir))
	if err != nil {
		t.Fatal(err)
	}
	if len(store.Accepted) != 1 {
		t.Fatalf("store holds %d entries, want 1", len(store.Accepted))
	}
	got := store.Accepted[0]
	if got.File != "internal/x.go" {
		t.Errorf("File = %q, want the repo-relative path", got.File)
	}
	if got.Text != "if limit > 0 {" {
		t.Errorf("Text = %q, want the normalized source text", got.Text)
	}
	if got.AcceptedIn != "alpha" {
		t.Errorf("AcceptedIn = %q, want the phase that was current", got.AcceptedIn)
	}

	// And from a nested subdirectory the same store is what list reads.
	chdir(t, filepath.Join(dir, "internal"))
	var out string
	if err := runCmdCapturing(t, &out, Survivor(), "list"); err != nil {
		t.Fatalf("list from subdir: %v", err)
	}
	if !strings.Contains(out, "internal/x.go") {
		t.Errorf("list from a nested subdir did not read the repo-root store:\n%s", out)
	}
}

// TestSurvivorStoreIsTracked pins the other half of acceptance_store: the store
// must ride in git history where a reviewer sees the reason. An ignored store is
// invisible to review and dies at the squash.
func TestSurvivorStoreIsTracked(t *testing.T) {
	body, err := os.ReadFile(filepath.Join(repoRootFromTest(t), ".gitignore"))
	if err != nil {
		t.Fatal(err)
	}
	if ignoresPath(string(body), ".dross/"+survivor.StoreFile) {
		t.Errorf(".gitignore excludes .dross/%s — acceptances must be tracked", survivor.StoreFile)
	}
}

// TestSurvivorAcceptWarnsOnAmbiguity: an ambiguous acceptance will not suppress,
// so recording it silently would leave the user watching the survivor reappear
// forever with no idea why.
func TestSurvivorAcceptWarnsOnAmbiguity(t *testing.T) {
	dir := setupSurvivorFixture(t)
	// Lines 5 and 7 of the fixture are both "return nil".
	var out string
	err := runCmdCapturing(t, &out, Survivor(), "accept", "internal/x.go:5",
		"--op", "OP", "--reason", "unreachable")
	if err != nil {
		t.Fatalf("accept: %v", err)
	}
	if !strings.Contains(out, "ambiguous") {
		t.Errorf("accepting an ambiguous line printed no warning:\n%s", out)
	}
	if _, err := os.Stat(storeFileOf(dir)); err != nil {
		t.Errorf("ambiguous acceptance should still be recorded: %v", err)
	}
}

// TestSurvivorAcceptCategoryFlag covers --category end to end: one invocation
// defines the shared prose, a second references it with no --reason at all.
// That second form is the whole point of acceptance_granularity — a bulk drain
// records many entries against one reason — and it is the shape that reaches
// Add's category-only branch. The flag shipped with no test behind it.
func TestSurvivorAcceptCategoryFlag(t *testing.T) {
	dir := setupSurvivorFixture(t)
	const prose = "gremlins switch-case attribution ceiling"

	// Lines 3 and 4 of the fixture are each unique, so neither acceptance is
	// withheld for ambiguity and the category path is what is under test.
	if err := runCmd(t, Survivor(), "accept", "internal/x.go:4",
		"--op", "CONDITIONALS_BOUNDARY", "--category", "switch-ceiling", "--reason", prose); err != nil {
		t.Fatalf("accept defining the category: %v", err)
	}
	if err := runCmd(t, Survivor(), "accept", "internal/x.go:3",
		"--op", "CONDITIONALS_NEGATION", "--category", "switch-ceiling"); err != nil {
		t.Fatalf("accept referencing the category with no --reason: %v", err)
	}

	raw, err := os.ReadFile(storeFileOf(dir))
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(raw), prose); n != 1 {
		t.Errorf("prose encoded %d times through the CLI, want exactly 1:\n%s", n, raw)
	}

	store, err := survivor.Load(storeFileOf(dir))
	if err != nil {
		t.Fatal(err)
	}
	if len(store.Accepted) != 2 {
		t.Fatalf("store holds %d entries, want 2", len(store.Accepted))
	}
	for _, a := range store.Accepted {
		got, err := store.ReasonFor(a)
		if err != nil {
			t.Fatalf("ReasonFor(%s): %v", a.Key, err)
		}
		if got != prose {
			t.Errorf("ReasonFor(%s) = %q, want the shared prose", a.Key, got)
		}
	}

	var out string
	if err := runCmdCapturing(t, &out, Survivor(), "list"); err != nil {
		t.Fatalf("list: %v", err)
	}
	if n := strings.Count(out, prose); n != 2 {
		t.Errorf("list resolved the shared reason for %d of 2 entries:\n%s", n, out)
	}

	// A category nothing registered must be refused, not stored as an entry
	// whose reason can never resolve.
	before, err := os.ReadFile(storeFileOf(dir))
	if err != nil {
		t.Fatal(err)
	}
	if err := runCmd(t, Survivor(), "accept", "internal/x.go:1",
		"--op", "OP", "--category", "no-such-category"); err == nil {
		t.Error("accept naming an unregistered category succeeded, want error")
	}
	after, err := os.ReadFile(storeFileOf(dir))
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Errorf("rejected accept mutated the store:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

// TestSurvivorRouteAppendsToCurrentPhaseSpec is c-7 + routed_state_source: the
// deferred entry carries both the survivor key and the target, lands in the
// CURRENT phase's spec (deferred items live in the spec that deferred them),
// and shows up under `deferred list --target`.
func TestSurvivorRouteAppendsToCurrentPhaseSpec(t *testing.T) {
	dir := setupSurvivorFixture(t)

	if err := runCmd(t, Survivor(), "route", "internal/x.go:4",
		"--op", "CONDITIONALS_BOUNDARY", "--target", "beta"); err != nil {
		t.Fatalf("route: %v", err)
	}

	// The entry is in alpha (current), not beta (target). Asserted on the exact
	// file, since `deferred list --target` collects across every spec and cannot
	// tell which one gained the row.
	alpha, err := phase.LoadSpec(filepath.Join(dir, ".dross", "phases", "alpha", "spec.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(alpha.Deferred) != 1 {
		t.Fatalf("current phase spec has %d deferred entries, want 1", len(alpha.Deferred))
	}
	if alpha.Deferred[0].Survivor == "" {
		t.Error("routed entry carries no survivor key")
	}
	if alpha.Deferred[0].Target != "beta" {
		t.Errorf("routed entry target = %q, want beta", alpha.Deferred[0].Target)
	}

	beta, err := phase.LoadSpec(filepath.Join(dir, ".dross", "phases", "beta", "spec.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(beta.Deferred) != 0 {
		t.Errorf("the entry landed in the TARGET phase's spec: %+v", beta.Deferred)
	}

	got := listJSON(t, "--target", "beta", "--json")
	if len(got) != 1 || got[0].Survivor != alpha.Deferred[0].Survivor {
		t.Errorf("deferred list --target beta did not surface the routed survivor: %+v", got)
	}
}

// TestSurvivorRouteRejectsUnknownTargetWithoutWriting: a typo'd slug must error
// before the spec is touched, or routing leaves a dangling destination behind.
func TestSurvivorRouteRejectsUnknownTargetWithoutWriting(t *testing.T) {
	dir := setupSurvivorFixture(t)
	specPath := filepath.Join(dir, ".dross", "phases", "alpha", "spec.toml")
	before, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatal(err)
	}

	if err := runCmd(t, Survivor(), "route", "internal/x.go:4",
		"--op", "OP", "--target", "no-such-phase"); err == nil {
		t.Fatal("routing to a nonexistent slug succeeded, want error")
	}

	after, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Errorf("failed route mutated the spec:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

// TestSurvivorRouteRequiresCurrentPhase: with nowhere to put the deferred item,
// routing must fail cleanly rather than inventing a home for it.
func TestSurvivorRouteRequiresCurrentPhase(t *testing.T) {
	dir := setupSurvivorFixture(t)
	if err := runCmd(t, State(), "set", "current_phase", ""); err != nil {
		t.Fatal(err)
	}

	if err := runCmd(t, Survivor(), "route", "internal/x.go:4",
		"--op", "OP", "--target", "beta"); err == nil {
		t.Fatal("route with no current phase succeeded, want error")
	}
	beta, err := phase.LoadSpec(filepath.Join(dir, ".dross", "phases", "beta", "spec.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(beta.Deferred) != 0 {
		t.Errorf("route with no current phase wrote into another spec: %+v", beta.Deferred)
	}
}

// TestSurvivorListJSONIsBareArray pins the json_shape convention: no `# <path>`
// header line, because a `#` line is not JSON and every consumer would have to
// strip it.
func TestSurvivorListJSONIsBareArray(t *testing.T) {
	setupSurvivorFixture(t)
	if err := runCmd(t, Survivor(), "accept", "internal/x.go:4",
		"--op", "OP", "--reason", "boundary unobservable"); err != nil {
		t.Fatal(err)
	}

	var out string
	if err := runCmdCapturing(t, &out, Survivor(), "list", "--json"); err != nil {
		t.Fatalf("list --json: %v", err)
	}
	if strings.HasPrefix(strings.TrimSpace(out), "#") {
		t.Errorf("--json emitted a header line:\n%s", out)
	}
	var entries []survivor.Acceptance
	if err := json.Unmarshal([]byte(out), &entries); err != nil {
		t.Fatalf("output is not an unmarshalable array: %v\n%s", err, out)
	}
	if len(entries) != 1 {
		t.Errorf("got %d entries, want 1", len(entries))
	}
}

// TestSurvivorListStaleFiltering: --stale shows only acceptances whose subject
// is gone, and an empty store is a clean exit-0 sentinel rather than an error.
func TestSurvivorListStaleFiltering(t *testing.T) {
	dir := setupSurvivorFixture(t)

	var empty string
	if err := runCmdCapturing(t, &empty, Survivor(), "list"); err != nil {
		t.Fatalf("list on an empty store: %v", err)
	}
	if !strings.Contains(empty, "(no accepted survivors)") {
		t.Errorf("empty store should print the sentinel, got:\n%s", empty)
	}

	if err := runCmd(t, Survivor(), "accept", "internal/x.go:4",
		"--op", "OP", "--reason", "live one"); err != nil {
		t.Fatal(err)
	}
	if err := runCmd(t, Survivor(), "accept", "internal/x.go:5",
		"--op", "OP", "--reason", "about to vanish"); err != nil {
		t.Fatal(err)
	}

	var live string
	if err := runCmdCapturing(t, &live, Survivor(), "list", "--stale"); err != nil {
		t.Fatalf("list --stale: %v", err)
	}
	if strings.Contains(live, "internal/x.go") {
		t.Errorf("--stale listed acceptances whose subjects are intact:\n%s", live)
	}

	// Delete the file: both acceptances lose their subject.
	if err := os.Remove(filepath.Join(dir, "internal", "x.go")); err != nil {
		t.Fatal(err)
	}
	var stale string
	if err := runCmdCapturing(t, &stale, Survivor(), "list", "--stale"); err != nil {
		t.Fatalf("list --stale after deletion: %v", err)
	}
	if strings.Count(stale, "internal/x.go") != 2 {
		t.Errorf("--stale should list both acceptances once the file is gone:\n%s", stale)
	}
}

// TestSurvivorUnknownSubcommandExitsNonZero: without the guard, cobra prints
// help and exits 0, so a typo lands in telemetry as a successful no-op.
func TestSurvivorUnknownSubcommandExitsNonZero(t *testing.T) {
	root := &cobra.Command{Use: "dross"}
	root.AddCommand(Survivor())
	EnforceSubcommandKnown(root)

	if err := runCmd(t, root, "survivor", "frob"); err == nil {
		t.Fatal("dross survivor frob exited 0, want a non-zero unknown-subcommand error")
	}
}
