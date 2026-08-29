package cmd

// `dross test lane edit --prepare` — the one lane field that changes in place.
//
// Every assertion here is about what SURVIVES the edit as much as what changes:
// the lane's other fields, its position in the document, and its consent grant.
// An edit that got the new value right while quietly reordering the file or
// dropping a grant would satisfy a test that only checked Prepare.

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/project"
)

func mustEditLane(t *testing.T, args ...string) {
	t.Helper()
	if err := runCmd(t, Test(), append([]string{"lane", "edit"}, args...)...); err != nil {
		t.Fatalf("lane edit %v: %v", args, err)
	}
}

// twoLaneEditFixture declares a lane to edit and a neighbour that must be left
// alone — in content AND in position. A single-lane fixture cannot tell an
// in-place edit from a remove-then-append.
func twoLaneEditFixture(t *testing.T) string {
	dir := laneFixture(t)
	mustAddLane(t, "go", "--match", "internal/**", "--match", "main.go", "--command", "go test -count=1 ./...")
	mustAddLane(t, "docs", "--match", "docs/", "--command", "markdownlint docs")
	return dir
}

// TestLaneEditChangesOnlyThePrepare: asserted as a whole-struct compare rather
// than field by field, so a field this edit was never meant to touch cannot
// slip through by not being checked.
//
// The neighbour's POSITION is asserted too. A remove-then-append
// implementation produces the right values and moves the edited lane to the end
// of the document — which reorders every lane's block on a change the user
// described as setting one line.
func TestLaneEditChangesOnlyThePrepare(t *testing.T) {
	dir := twoLaneEditFixture(t)
	before := loadLanes(t, dir)

	mustEditLane(t, "go", "--prepare", "make build")

	after := loadLanes(t, dir)
	if len(after) != 2 {
		t.Fatalf("want 2 lanes, got %d", len(after))
	}
	if after[0].Name != "go" || after[1].Name != "docs" {
		t.Errorf("the edit reordered the document: %s then %s", after[0].Name, after[1].Name)
	}
	if !reflect.DeepEqual(after[1], before[1]) {
		t.Errorf("the neighbour changed:\n before %+v\n after  %+v", before[1], after[1])
	}

	want := before[0]
	want.Prepare = "make build"
	if !reflect.DeepEqual(after[0], want) {
		t.Errorf("the edit changed more than the prepare:\n want %+v\n got  %+v", want, after[0])
	}
}

// TestLaneEditDistinguishesOmittedFromEmpty is the cobra-Changed half.
//
// "Not passed" and "passed empty" are different requests — leave it alone, and
// clear it. Read from the VALUE rather than from Changed they collapse into
// one clearing behaviour, and `dross test lane edit go` typed with no flag at
// all would silently drop a prepare the user had set.
func TestLaneEditDistinguishesOmittedFromEmpty(t *testing.T) {
	dir := twoLaneEditFixture(t)
	mustEditLane(t, "go", "--prepare", "make build")
	path := filepath.Join(dir, RootDirName, project.File)

	t.Run("omitted errors and writes nothing", func(t *testing.T) {
		before := mustRead(t, path)
		err := runCmd(t, Test(), "lane", "edit", "go")
		if err == nil {
			t.Fatal("`lane edit` with no --prepare was accepted")
		}
		if !strings.Contains(err.Error(), "--prepare") {
			t.Errorf("the refusal does not say what is missing: %v", err)
		}
		if after := mustRead(t, path); after != before {
			t.Errorf("a rejected edit rewrote project.toml:\n--- before\n%s\n--- after\n%s", before, after)
		}
		if got := loadLanes(t, dir)[0].Prepare; got != "make build" {
			t.Errorf("the existing prepare was dropped: %q", got)
		}
	})

	t.Run("empty clears the key", func(t *testing.T) {
		mustEditLane(t, "go", "--prepare", "")
		if got := loadLanes(t, dir)[0].Prepare; got != "" {
			t.Errorf("prepare = %q, want it cleared", got)
		}
		if body := mustRead(t, path); strings.Contains(body, "prepare") {
			t.Errorf("a cleared prepare left its key behind:\n%s", body)
		}
	})
}

// TestLaneEditKeepsTheGrantAndStalesIt is the whole reason this verb exists.
//
// remove-then-re-add is the workaround the broader edit surface was deferred
// in favour of, and for prepare it is not one: `lane remove` drops the grant,
// so the lane comes back ABSENT — reported as never trusted. That collapses
// STALE into ABSENT and loses the only signal that says a line the user
// approved has since been rewritten.
func TestLaneEditKeepsTheGrantAndStalesIt(t *testing.T) {
	dir := twoLaneEditFixture(t)
	root := filepath.Join(dir, RootDirName)
	if err := GrantLaneConsent(root, "go", laneConsentLine(loadLanes(t, dir)[0])); err != nil {
		t.Fatal(err)
	}

	mustEditLane(t, "go", "--prepare", "make build")

	l, err := loadLocal(localPath(root))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := l.TrustedLaneCommands["go"]; !ok {
		t.Fatalf("the edit revoked the grant: %v — a lane the user HAS trusted now reads as one they never have", l.TrustedLaneCommands)
	}

	err = runCmd(t, Test(), "lane", "edit", "docs", "--prepare", "x") // unrelated lane
	if err != nil {
		t.Fatal(err)
	}
	err = runCmd(t, Trust(), "--lane", "go", "--check")
	if err == nil {
		t.Fatal("--check passed on a lane whose prepare was just edited")
	}
	if !strings.Contains(err.Error(), "has CHANGED since you trusted it") {
		t.Errorf("the refusal reads as a first run, not a rewrite: %v", err)
	}
	if strings.Contains(err.Error(), "has not been trusted") {
		t.Errorf("the edit collapsed STALE into ABSENT: %v", err)
	}
}

// TestLaneEditNamesTheFixOnlyWhenItChanged: a trust instruction printed on a
// no-op re-set teaches the reader that the message carries no information, and
// the next real one gets skimmed.
func TestLaneEditNamesTheFixOnlyWhenItChanged(t *testing.T) {
	twoLaneEditFixture(t)

	var changed string
	if err := runCmdCapturing(t, &changed, Test(), "lane", "edit", "go", "--prepare", "make build"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(changed, "dross trust --lane go") {
		t.Errorf("an edit that changed the consent line did not name the fix:\n%s", changed)
	}

	var noop string
	if err := runCmdCapturing(t, &noop, Test(), "lane", "edit", "go", "--prepare", "make build"); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(noop, "dross trust --lane") {
		t.Errorf("re-setting the same value still told the user to re-consent:\n%s", noop)
	}
}

// TestLaneEditUnknownNameLeavesTheFileAlone: refused before the save, in the
// shape TestLaneAddWithoutCommandLeavesTheFileUnchanged already pins. A refusal
// that ran after the round trip would pass an error-only test while having
// rewritten the document.
func TestLaneEditUnknownNameLeavesTheFileAlone(t *testing.T) {
	dir := twoLaneEditFixture(t)
	path := filepath.Join(dir, RootDirName, project.File)
	before := mustRead(t, path)

	err := runCmd(t, Test(), "lane", "edit", "nope", "--prepare", "x")
	if err == nil {
		t.Fatal("`lane edit nope` was accepted")
	}
	for _, want := range []string{"nope", "go", "docs"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not mention %q: %v", want, err)
		}
	}
	if after := mustRead(t, path); after != before {
		t.Errorf("a rejected edit rewrote project.toml:\n--- before\n%s\n--- after\n%s", before, after)
	}
}

// TestLaneEditNormalizesAWhitespaceOnlyPrepare: the edit path resolves the one
// shape that disagrees with itself to absent, exactly as `lane add` does. Two
// verbs that wrote different things for the same input would put a lane on disk
// that `dross validate` then rejects.
func TestLaneEditNormalizesAWhitespaceOnlyPrepare(t *testing.T) {
	dir := twoLaneEditFixture(t)
	mustEditLane(t, "go", "--prepare", "   ")

	if got := loadLanes(t, dir)[0].Prepare; got != "" {
		t.Errorf("prepare = %q, want it normalized to absent", got)
	}
	if body := mustRead(t, filepath.Join(dir, RootDirName, project.File)); strings.Contains(body, "prepare") {
		t.Errorf("a whitespace-only prepare rendered a prepare key:\n%s", body)
	}
}

// TestLaneEditInstallLeavesTheTestGrantAlone is c-7's non-staleness half,
// proved at the cheapest layer there is: the consent line itself.
//
// install_consent is locked against exactly this regression. Folding Install
// into laneConsentLine the way Prepare is folded in would staleness-refuse a
// lane's ordinary test runs the moment an install line was added — a line that
// has never executed breaking a gate that was passing the day before.
func TestLaneEditInstallLeavesTheTestGrantAlone(t *testing.T) {
	dir := twoLaneEditFixture(t)
	root := filepath.Join(dir, RootDirName)
	before := laneConsentLine(loadLanes(t, dir)[0])
	if err := GrantLaneConsent(root, "go", before); err != nil {
		t.Fatal(err)
	}

	mustEditLane(t, "go", "--install", "npm i -g pnpm")

	lane := loadLanes(t, dir)[0]
	if lane.Install != "npm i -g pnpm" {
		t.Fatalf("install = %q, want it written", lane.Install)
	}
	if after := laneConsentLine(lane); after != before {
		t.Errorf("the install line reached the lane's TEST consent line:\n before %q\n after  %q", before, after)
	}
	// The grant itself, not just the line it was taken over: --check is the
	// gate a real run passes through, and it is what would start refusing.
	if err := runCmd(t, Trust(), "--lane", "go", "--check"); err != nil {
		t.Errorf("adding an install line staled the lane's test grant: %v", err)
	}
}

// TestLaneEditInstallDistinguishesOmittedFromEmpty: the per-flag Changed guard,
// one flag deeper than the command-level one. An unconditional write would let
// `--toolchain go` silently drop an install line the user never mentioned.
func TestLaneEditInstallDistinguishesOmittedFromEmpty(t *testing.T) {
	dir := twoLaneEditFixture(t)
	path := filepath.Join(dir, RootDirName, project.File)
	mustEditLane(t, "go", "--install", "npm i -g pnpm")

	t.Run("another flag leaves it intact", func(t *testing.T) {
		mustEditLane(t, "go", "--toolchain", "go")
		if got := loadLanes(t, dir)[0].Install; got != "npm i -g pnpm" {
			t.Errorf("editing --toolchain changed the install line: %q", got)
		}
	})

	t.Run("empty clears the key", func(t *testing.T) {
		mustEditLane(t, "go", "--install", "")
		if got := loadLanes(t, dir)[0].Install; got != "" {
			t.Errorf("install = %q, want it cleared", got)
		}
		// Asserted on the key's ABSENCE, not on an empty string: omitempty is
		// what keeps a cleared field from writing `install = ""` into every
		// document that ever carried one.
		if body := mustRead(t, path); strings.Contains(body, "install") {
			t.Errorf("a cleared install left its key behind:\n%s", body)
		}
	})
}

// TestLaneEditInstallAloneIsNotTheNoOpRefusal: --install has to satisfy the
// command-level "nothing to change" guard on its own, or the one flag this task
// adds would be unreachable through the verb it was added to.
func TestLaneEditInstallAloneIsNotTheNoOpRefusal(t *testing.T) {
	dir := twoLaneEditFixture(t)

	if err := runCmd(t, Test(), "lane", "edit", "go", "--install", "go install ./cmd/x"); err != nil {
		t.Fatalf("`lane edit --install` alone was refused: %v", err)
	}
	if got := loadLanes(t, dir)[0].Install; got != "go install ./cmd/x" {
		t.Errorf("install = %q, want it written", got)
	}

	// The refusal's own text still has to name the flag, or a user who passes
	// nothing is told about two of the three fields they could have set.
	err := runCmd(t, Test(), "lane", "edit", "go")
	if err == nil {
		t.Fatal("`lane edit` with no flag at all was accepted")
	}
	if !strings.Contains(err.Error(), "--install") {
		t.Errorf("the refusal does not name --install: %v", err)
	}
}

// TestLaneAddWritesAndListsTheInstallLine: the declaration surface's other
// half. `edit` cannot set a field `add` never writes and `list` never shows —
// the user would have no way to see what they declared short of reading
// project.toml.
func TestLaneAddWritesAndListsTheInstallLine(t *testing.T) {
	dir := laneFixture(t)
	mustAddLane(t, "x", "--match", "x/**", "--command", "go test ./x", "--install", "go install ./x")

	if got := loadLanes(t, dir)[0].Install; got != "go install ./x" {
		t.Errorf("install = %q, want it written", got)
	}
	if body := mustRead(t, filepath.Join(dir, RootDirName, project.File)); !strings.Contains(body, `install = "go install ./x"`) {
		t.Errorf("the install key is missing from project.toml:\n%s", body)
	}

	var listed string
	if err := runCmdCapturing(t, &listed, Test(), "lane", "list"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(listed, "go install ./x") {
		t.Errorf("`lane list` does not print the install line:\n%s", listed)
	}

	// The opt-in half: a lane that declares none must not render an
	// `install: -` row, which would read as a field every lane is expected to
	// set.
	mustAddLane(t, "y", "--match", "y/**", "--command", "go test ./y")
	var plain string
	if err := runCmdCapturing(t, &plain, Test(), "lane", "list"); err != nil {
		t.Fatal(err)
	}
	if strings.Count(plain, "install:") != 1 {
		t.Errorf("a lane declaring no install line still printed a row:\n%s", plain)
	}
}
