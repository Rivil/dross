package cmd

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/project"
)

// loadLanes re-reads project.toml from disk. Every assertion here goes through
// a fresh Load rather than the in-memory value the verb had, because what is
// under test is what LANDED — a verb that mutated a struct and skipped the Save
// would satisfy any check against its own copy.
func loadLanes(t *testing.T, dir string) []project.TestLane {
	t.Helper()
	p, err := project.Load(filepath.Join(dir, RootDirName, project.File))
	if err != nil {
		t.Fatal(err)
	}
	return p.Runtime.TestLane
}

func mustAddLane(t *testing.T, args ...string) {
	t.Helper()
	if err := runCmd(t, Test(), append([]string{"lane", "add"}, args...)...); err != nil {
		t.Fatalf("lane add %v: %v", args, err)
	}
}

// TestLaneAddPersists: the three fields have to survive the round trip to disk.
// A dropped Save, or a field lost on the way through the encoder, leaves a lane
// that `dross test --files` will never select and nothing else would notice.
func TestLaneAddPersists(t *testing.T) {
	dir := laneFixture(t)
	mustAddLane(t, "go", "--match", "internal/**", "--command", "go test ./...")

	lanes := loadLanes(t, dir)
	if len(lanes) != 1 {
		t.Fatalf("want 1 lane on disk, got %d", len(lanes))
	}
	if lanes[0].Name != "go" {
		t.Errorf("name = %q", lanes[0].Name)
	}
	if len(lanes[0].Match) != 1 || lanes[0].Match[0] != "internal/**" {
		t.Errorf("match = %v", lanes[0].Match)
	}
	if lanes[0].Command != "go test ./..." {
		t.Errorf("command = %q — this is the exact string consent binds to", lanes[0].Command)
	}
}

// TestLaneAddTakesRepeatedMatches: --match is a StringArray, so a lane covering
// two roots is one lane rather than two competing ones. Comma-splitting would
// be wrong here for the same reason it is wrong for --files: a comma is legal
// in a path.
func TestLaneAddTakesRepeatedMatches(t *testing.T) {
	dir := laneFixture(t)
	mustAddLane(t, "go", "--match", "internal/**", "--match", "main.go", "--command", "go test ./...")

	lanes := loadLanes(t, dir)
	if len(lanes) != 1 || len(lanes[0].Match) != 2 {
		t.Fatalf("want one lane with two globs, got %+v", lanes)
	}
	if lanes[0].Match[0] != "internal/**" || lanes[0].Match[1] != "main.go" {
		t.Errorf("globs lost their order or content: %v", lanes[0].Match)
	}
}

// TestLaneAddRejectsDuplicateName: lane names key the consent store, so two
// lanes called `go` would leave one grant with authority over two different
// command lines — a lane you trusted authorizing one you never read.
func TestLaneAddRejectsDuplicateName(t *testing.T) {
	dir := laneFixture(t)
	mustAddLane(t, "go", "--match", "internal/**", "--command", "go test ./internal/...")

	err := runCmd(t, Test(), "lane", "add", "go", "--match", "cmd/**", "--command", "go test ./cmd/...")
	if err == nil {
		t.Fatal("a duplicate lane name was accepted")
	}
	if !strings.Contains(err.Error(), "go") {
		t.Errorf("refusal does not name the existing lane: %v", err)
	}
	if lanes := loadLanes(t, dir); len(lanes) != 1 {
		t.Errorf("the rejected add still wrote: %d lanes on disk", len(lanes))
	}
}

// TestLaneAddWithoutCommandLeavesTheFileUnchanged: the refusal comes before the
// load-modify-save, so a rejected add does not even rewrite project.toml with
// the same content. Byte-for-byte, because a round trip that reformats the file
// turns every rejected command into a spurious diff.
func TestLaneAddWithoutCommandLeavesTheFileUnchanged(t *testing.T) {
	dir := laneFixture(t)
	path := filepath.Join(dir, RootDirName, project.File)
	before := mustRead(t, path)

	if err := runCmd(t, Test(), "lane", "add", "go", "--match", "*.go"); err == nil {
		t.Fatal("a lane with no --command was accepted")
	}
	if after := mustRead(t, path); after != before {
		t.Errorf("a rejected add rewrote project.toml:\n--- before\n%s\n--- after\n%s", before, after)
	}
}

// TestLaneAddWithoutMatchIsRefused: a lane matching nothing can never be
// selected, so accepting it would write a block that looks configured and does
// nothing — the failure mode is silence, which is the worst kind.
func TestLaneAddWithoutMatchIsRefused(t *testing.T) {
	dir := laneFixture(t)
	if err := runCmd(t, Test(), "lane", "add", "go", "--command", "go test ./..."); err == nil {
		t.Fatal("a lane with no --match was accepted")
	}
	if lanes := loadLanes(t, dir); len(lanes) != 0 {
		t.Errorf("the rejected add still wrote: %+v", lanes)
	}
}

// TestLaneListShowsCommand: the command is the value consent binds to, so a
// listing that showed only names and globs would leave the user unable to see
// what `dross trust --lane` is about to authorize.
func TestLaneListShowsCommand(t *testing.T) {
	laneFixture(t)
	mustAddLane(t, "go", "--match", "internal/**", "--command", "go test -count=1 ./...")
	mustAddLane(t, "docs", "--match", "docs/", "--command", "markdownlint docs")

	var out string
	if err := runCmdCapturing(t, &out, Test(), "lane", "list"); err != nil {
		t.Fatalf("lane list: %v", err)
	}
	for _, want := range []string{"go", "internal/**", "go test -count=1 ./...", "docs", "docs/", "markdownlint docs"} {
		if !strings.Contains(out, want) {
			t.Errorf("listing omits %q:\n%s", want, out)
		}
	}
}

// TestLaneListOnALaneLessRepoExitsZero: a repo with no lanes is the supported
// default (locked bare_test_run), not a misconfiguration. An error here would
// make an opt-in feature look like a missing one.
func TestLaneListOnALaneLessRepoExitsZero(t *testing.T) {
	laneFixture(t)
	var out string
	if err := runCmdCapturing(t, &out, Test(), "lane", "list"); err != nil {
		t.Fatalf("lane list on a lane-less repo must exit 0: %v", err)
	}
	if !strings.Contains(out, "no test lanes configured") {
		t.Errorf("listing does not say the repo has none:\n%s", out)
	}
}

// TestLaneRemoveKeepsOtherLanes: removal goes through a full project.Load →
// Save round trip, so the assertion covers the whole document, not just the
// lane array. A round trip that dropped [runtime]'s scalars would take
// test_command with it — and a repo whose test_command vanished during a lane
// edit would fail its next gate for a reason nothing on screen explains.
func TestLaneRemoveKeepsOtherLanes(t *testing.T) {
	dir := laneFixture(t)
	mustRunSet(t, "runtime.test_command", "go test ./...")
	mustAddLane(t, "go", "--match", "internal/**", "--command", "go test ./internal/...")
	mustAddLane(t, "docs", "--match", "docs/", "--command", "markdownlint docs")

	if err := runCmd(t, Test(), "lane", "remove", "docs"); err != nil {
		t.Fatalf("lane remove: %v", err)
	}

	lanes := loadLanes(t, dir)
	if len(lanes) != 1 || lanes[0].Name != "go" {
		t.Fatalf("want only the go lane left, got %+v", lanes)
	}
	p, err := project.Load(filepath.Join(dir, RootDirName, project.File))
	if err != nil {
		t.Fatal(err)
	}
	if p.Runtime.TestCommand != "go test ./..." {
		t.Errorf("runtime.test_command = %q — the round trip dropped it", p.Runtime.TestCommand)
	}
	if p.Runtime.Mode != "native" {
		t.Errorf("runtime.mode = %q — the round trip dropped it", p.Runtime.Mode)
	}
}

// TestLaneRemoveUnknownIsAnError: reporting success for a name that was never
// declared leaves the user believing a lane is gone while it is still there and
// still running.
func TestLaneRemoveUnknownIsAnError(t *testing.T) {
	dir := laneFixture(t)
	mustAddLane(t, "go", "--match", "internal/**", "--command", "go test ./...")

	err := runCmd(t, Test(), "lane", "remove", "nosuch")
	if err == nil {
		t.Fatal("removing an unknown lane reported success")
	}
	if !strings.Contains(err.Error(), "nosuch") || !strings.Contains(err.Error(), "go") {
		t.Errorf("refusal must name the unknown lane and the declared ones: %v", err)
	}
	if lanes := loadLanes(t, dir); len(lanes) != 1 {
		t.Errorf("the failed remove disturbed the lanes: %+v", lanes)
	}
}

// TestLaneRemoveTheLastLaneRestoresTheLaneLessShape: nil rather than an empty
// slice, so omitempty leaves the key out and the document goes back to what a
// repo that never declared a lane looks like.
func TestLaneRemoveTheLastLaneRestoresTheLaneLessShape(t *testing.T) {
	dir := laneFixture(t)
	mustAddLane(t, "go", "--match", "internal/**", "--command", "go test ./...")
	if err := runCmd(t, Test(), "lane", "remove", "go"); err != nil {
		t.Fatal(err)
	}
	if body := mustRead(t, filepath.Join(dir, RootDirName, project.File)); strings.Contains(body, "test_lane") {
		t.Errorf("removing the last lane left the key behind:\n%s", body)
	}
}

// TestLaneRemoveDropsItsGrant is the reason remove touches local.toml at all.
// Left behind, the grant sits keyed by a name nothing declares — harmless until
// someone re-adds a lane under that name, which then starts GRANTED, authorized
// by a fingerprint issued months earlier for whatever the deleted lane ran.
//
// The re-add here uses the SAME command deliberately: that is the case where an
// inherited grant would be invisible, because everything about the lane looks
// like what was trusted.
func TestLaneRemoveDropsItsGrant(t *testing.T) {
	dir := laneFixture(t)
	root := filepath.Join(dir, RootDirName)
	mustAddLane(t, "go", "--match", "internal/**", "--command", "go test ./...")
	if err := runCmd(t, Trust(), "--lane", "go"); err != nil {
		t.Fatalf("trust --lane: %v", err)
	}
	if got := laneState(t, root, dir, "go", "go test ./..."); got != ConsentGranted {
		t.Fatalf("precondition: lane should be granted, got %v", got)
	}

	if err := runCmd(t, Test(), "lane", "remove", "go"); err != nil {
		t.Fatal(err)
	}
	mustAddLane(t, "go", "--match", "internal/**", "--command", "go test ./...")

	if got := laneState(t, root, dir, "go", "go test ./..."); got != ConsentAbsent {
		t.Errorf("a re-added lane inherited the deleted lane's grant: %v", got)
	}
}

// TestLaneVerbShadowsSelector pins the nesting as a deliberate choice rather
// than an accident. cobra resolves args[0] against subcommands before treating
// it as a positional, so `dross test lane` can no longer mean "run the suite
// against a package named lane". Recording it here means a future reader finds
// the trade-off stated instead of rediscovering it as a bug.
func TestLaneVerbShadowsSelector(t *testing.T) {
	laneFixture(t)
	mustRunSet(t, "runtime.test_command", "echo ran")
	rec := installSpawnRecorder(t, nil)

	if err := runCmd(t, Test(), "lane", "list"); err != nil {
		t.Fatalf("lane list: %v", err)
	}
	if n := rec.count(); n != 0 {
		t.Errorf("`dross test lane` spawned %d command(s) — it reached the suite runner, not the subcommand", n)
	}
}
