package cmd

import (
	"path/filepath"
	"reflect"
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

// TestLaneAddPersistsSelectorFields: the two opt-in fields have to survive the
// round trip to disk and come back out of `lane list`, or --selector would be a
// flag the user can type and nothing would ever read.
func TestLaneAddPersistsSelectorFields(t *testing.T) {
	dir := laneFixture(t)
	mustAddLane(t, "go", "--match", "internal/**", "--command", "go test",
		"--selector", "go-package", "--empty-exit", "5")

	lanes := loadLanes(t, dir)
	if len(lanes) != 1 {
		t.Fatalf("want 1 lane on disk, got %d", len(lanes))
	}
	if lanes[0].Selector != "go-package" {
		t.Errorf("selector = %q, want go-package", lanes[0].Selector)
	}
	if !reflect.DeepEqual(lanes[0].EmptyExit, []int{5}) {
		t.Errorf("empty_exit = %v, want [5]", lanes[0].EmptyExit)
	}

	var out string
	if err := runCmdCapturing(t, &out, Test(), "lane", "list"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "selector: go-package") {
		t.Errorf("lane list hid the selector:\n%s", out)
	}
	if !strings.Contains(out, "empty-exit: 5") {
		t.Errorf("lane list hid the empty-exit codes:\n%s", out)
	}
}

// TestLaneAddRefusesBadSelectorFieldsBeforeTheWrite: every one of these is a
// lane `dross validate` would reject, so the CLI must not be the thing that
// writes it. Asserted on the FILE as well as the error, in the shape
// TestLaneAddWithoutCommandLeavesTheFileUnchanged already pins — a refusal that
// ran after the save would pass an error-only test while leaving the bad lane
// on disk.
func TestLaneAddRefusesBadSelectorFieldsBeforeTheWrite(t *testing.T) {
	cases := []struct {
		name   string
		args   []string
		needle string
	}{
		// A style dross cannot translate; the refusal names the set.
		{"unknown-style", []string{"--selector", "packages"}, "path | dir | go-package"},
		// 0 is the runner's success code.
		{"success-code", []string{"--selector", "path", "--empty-exit", "0"}, "success code"},
		// 255 is ssh's transport failure, spent by internal/remote.
		{"ssh-code", []string{"--selector", "path", "--empty-exit", "255"}, "transport-failure"},
		// Outside the byte a process exits with.
		{"out-of-range", []string{"--selector", "path", "--empty-exit", "300"}, "0-255"},
		// A code that can never fire, because the lane always runs whole.
		{"code-without-selector", []string{"--empty-exit", "5"}, "no selector"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := laneFixture(t)
			path := filepath.Join(dir, RootDirName, project.File)
			before := mustRead(t, path)

			args := append([]string{"lane", "add", "go", "--match", "internal/**", "--command", "go test"}, tc.args...)
			err := runCmd(t, Test(), args...)
			if err == nil {
				t.Fatalf("lane add accepted %v", tc.args)
			}
			if !strings.Contains(err.Error(), tc.needle) {
				t.Errorf("the refusal must say why (%q), got: %v", tc.needle, err)
			}
			if after := mustRead(t, path); after != before {
				t.Errorf("a rejected add rewrote project.toml:\n--- before\n%s\n--- after\n%s", before, after)
			}
		})
	}
}

// TestLaneAddNormalizesTheSelector: what lands on disk is the canonical
// spelling, so list, validate and the run site never disagree with each other
// over what the user typed.
func TestLaneAddNormalizesTheSelector(t *testing.T) {
	dir := laneFixture(t)
	mustAddLane(t, "go", "--match", "internal/**", "--command", "go test",
		"--selector", " GO-PACKAGE ")

	if got := loadLanes(t, dir)[0].Selector; got != "go-package" {
		t.Errorf("selector = %q, want the normalized go-package", got)
	}
}

// TestLaneListOmitsUndeclaredSelectorFields asserts ABSENCE, which is the whole
// opt-in claim: a lane written before this phase must not start listing fields
// that read as something the user is expected to go and set.
func TestLaneListOmitsUndeclaredSelectorFields(t *testing.T) {
	laneFixture(t)
	mustAddLane(t, "go", "--match", "internal/**", "--command", "go test")

	var out string
	if err := runCmdCapturing(t, &out, Test(), "lane", "list"); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "selector") {
		t.Errorf("a lane declaring no selector listed one:\n%s", out)
	}
	if strings.Contains(out, "empty-exit") {
		t.Errorf("a lane declaring no empty-exit codes listed them:\n%s", out)
	}
}

// TestSelectorFieldsSurviveAnotherLanesRemoval: `lane remove` rewrites every
// surviving lane through the encoder, so the two new fields ride that path too.
// A field the rewrite dropped would leave a lane silently unscoped after an
// unrelated edit.
func TestSelectorFieldsSurviveAnotherLanesRemoval(t *testing.T) {
	dir := laneFixture(t)
	mustAddLane(t, "go", "--match", "internal/**", "--command", "go test",
		"--selector", "go-package", "--empty-exit", "5")
	mustAddLane(t, "docs", "--match", "docs/", "--command", "true")

	if err := runCmd(t, Test(), "lane", "remove", "docs"); err != nil {
		t.Fatal(err)
	}
	lanes := loadLanes(t, dir)
	if len(lanes) != 1 {
		t.Fatalf("want the go lane left, got %d", len(lanes))
	}
	if lanes[0].Selector != "go-package" || !reflect.DeepEqual(lanes[0].EmptyExit, []int{5}) {
		t.Errorf("removing another lane dropped the survivor's selector fields: selector=%q empty_exit=%v",
			lanes[0].Selector, lanes[0].EmptyExit)
	}
}

// laneBlock returns just the lines `lane list` printed for one lane: its
// header and every indented line under it, up to the next lane's header.
//
// Per-lane rather than whole-output, because the absence assertions below are
// about ONE lane. A `strings.Contains(out, "prepare")` over a listing that
// also holds a prepared lane can never fail, so the assertion that an opt-in
// field stays invisible would be vacuous exactly where it matters.
func laneBlock(t *testing.T, out, name string) string {
	t.Helper()
	var block []string
	collecting := false
	for _, line := range strings.Split(out, "\n") {
		switch {
		case line == name:
			collecting = true
			block = append(block, line)
		case collecting && strings.HasPrefix(line, " "):
			block = append(block, line)
		case collecting:
			return strings.Join(block, "\n")
		}
	}
	if !collecting {
		t.Fatalf("lane %q has no block in the listing:\n%s", name, out)
	}
	return strings.Join(block, "\n")
}

// TestLaneAddPersistsPrepare: --prepare has to survive the round trip to disk
// and come back out of `lane list`, or it would be a flag the user can type
// and nothing would ever read — a cold host left unbootstrapped while the
// config says otherwise.
func TestLaneAddPersistsPrepare(t *testing.T) {
	dir := laneFixture(t)
	mustAddLane(t, "go", "--match", "internal/**", "--command", "go test", "--prepare", "make build")

	lanes := loadLanes(t, dir)
	if len(lanes) != 1 {
		t.Fatalf("want 1 lane on disk, got %d", len(lanes))
	}
	if lanes[0].Prepare != "make build" {
		t.Errorf("prepare = %q, want \"make build\" — the flag never reached the block", lanes[0].Prepare)
	}
	if lanes[0].Command != "go test" {
		t.Errorf("command = %q — the prepare must not disturb the line consent binds to", lanes[0].Command)
	}

	var out string
	if err := runCmdCapturing(t, &out, Test(), "lane", "list"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "prepare: make build") {
		t.Errorf("lane list hid the prepare:\n%s", out)
	}
}

// TestLaneListOmitsAnUndeclaredPrepare asserts ABSENCE against a NEIGHBOUR
// that does declare one, which is the whole opt-in claim: a lane written
// before this phase must not start listing a field that reads as something
// the user is expected to go and set.
func TestLaneListOmitsAnUndeclaredPrepare(t *testing.T) {
	laneFixture(t)
	mustAddLane(t, "go", "--match", "internal/**", "--command", "go test", "--prepare", "make build")
	mustAddLane(t, "docs", "--match", "docs/", "--command", "markdownlint docs")

	var out string
	if err := runCmdCapturing(t, &out, Test(), "lane", "list"); err != nil {
		t.Fatal(err)
	}
	if got := laneBlock(t, out, "go"); !strings.Contains(got, "prepare: make build") {
		t.Errorf("the declaring lane's block hid its prepare:\n%s", got)
	}
	if got := laneBlock(t, out, "docs"); strings.Contains(got, "prepare") {
		t.Errorf("a lane declaring no prepare listed one:\n%s", got)
	}
}

// TestLaneAddWithPrepareStartsUngranted: declaring a bootstrap line is not
// consenting to run it, exactly as declaring a command is not. A prepare that
// arrived pre-granted would be the one line in the pair nobody was ever shown.
func TestLaneAddWithPrepareStartsUngranted(t *testing.T) {
	dir := laneFixture(t)
	var out string
	if err := runCmdCapturing(t, &out, Test(), "lane", "add", "p",
		"--match", "internal/**", "--command", "go test", "--prepare", "make build"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "dross trust --lane p") {
		t.Errorf("the add did not point at the grant it still needs:\n%s", out)
	}
	l, err := loadLocal(localPath(filepath.Join(dir, RootDirName)))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := l.TrustedLaneCommands["p"]; ok {
		t.Errorf("adding a lane granted it: %v", l.TrustedLaneCommands)
	}
}

// TestLaneAddNormalizesAWhitespaceOnlyPrepare: whitespace-only is the one
// shape that disagrees with itself — non-empty for the consent fingerprint,
// empty for every reader. The CLI resolves it to absent before the write, so
// only a hand-edited project.toml can carry one (and `dross validate` reports
// that; see TestValidateNamesLaneWithWhitespaceOnlyPrepare).
func TestLaneAddNormalizesAWhitespaceOnlyPrepare(t *testing.T) {
	dir := laneFixture(t)
	mustAddLane(t, "go", "--match", "internal/**", "--command", "go test", "--prepare", "   ")

	if got := loadLanes(t, dir)[0].Prepare; got != "" {
		t.Errorf("prepare = %q, want it normalized to absent", got)
	}
	if body := mustRead(t, filepath.Join(dir, RootDirName, project.File)); strings.Contains(body, "prepare") {
		t.Errorf("a whitespace-only prepare rendered a prepare key:\n%s", body)
	}
}

// TestPrepareSurvivesAnotherLanesRemoval: `lane remove` rewrites every
// surviving lane through the encoder, so prepare rides that path too. A field
// the rewrite dropped would leave a lane silently unbootstrapped after an
// unrelated edit — and, because consent covers both lines, silently re-grant
// under a fingerprint the user never saw.
func TestPrepareSurvivesAnotherLanesRemoval(t *testing.T) {
	dir := laneFixture(t)
	mustAddLane(t, "go", "--match", "internal/**", "--command", "go test", "--prepare", "make build")
	mustAddLane(t, "docs", "--match", "docs/", "--command", "true")

	if err := runCmd(t, Test(), "lane", "remove", "docs"); err != nil {
		t.Fatal(err)
	}
	lanes := loadLanes(t, dir)
	if len(lanes) != 1 {
		t.Fatalf("want the go lane left, got %d", len(lanes))
	}
	if lanes[0].Prepare != "make build" {
		t.Errorf("removing another lane dropped the survivor's prepare: %q", lanes[0].Prepare)
	}
}
