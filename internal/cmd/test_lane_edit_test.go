package cmd

// `dross test lane edit --prepare` — the one lane field that changes in place.
//
// Every assertion here is about what SURVIVES the edit as much as what changes:
// the lane's other fields, its position in the document, and its consent grant.
// An edit that got the new value right while quietly reordering the file or
// dropping a grant would satisfy a test that only checked Prepare.

import (
	"os"
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

// --- the widened field set (c-1, c-3) ---

// laneConsentState reports what this machine says about one lane's command
// grant, resolved through the lane as project.toml currently holds it.
func laneConsentState(t *testing.T, dir, name string) ConsentState {
	t.Helper()
	root := filepath.Join(dir, RootDirName)
	p, err := project.Load(filepath.Join(root, project.File))
	if err != nil {
		t.Fatal(err)
	}
	lane, err := findLane(p, name)
	if err != nil {
		t.Fatal(err)
	}
	return laneState(t, root, dir, name, laneConsentLine(lane))
}

// TestLaneEditRewritesTheCommandInPlaceAndStalesTheGrant is c-3's core claim
// for the field the old surface said was remove-then-re-add. Remove-then-re-add
// DROPPED the grant, reporting a lane the user had trusted as one they never
// had; the in-place edit keeps it and reports it stale, which is the honest
// reading and the one the consent ladder exists to express.
func TestLaneEditRewritesTheCommandInPlaceAndStalesTheGrant(t *testing.T) {
	dir := twoLaneEditFixture(t)
	root := filepath.Join(dir, RootDirName)
	before := loadLanes(t, dir)
	mustGrantLane(t, root, "go", laneConsentLine(before[0]))
	if got := laneConsentState(t, dir, "go"); got != ConsentGranted {
		t.Fatalf("the grant did not take: %v", got)
	}

	var out string
	if err := runCmdCapturing(t, &out, Test(), "lane", "edit", "go", "--command", "go test -race ./..."); err != nil {
		t.Fatalf("lane edit --command: %v", err)
	}

	after := loadLanes(t, dir)
	if after[0].Command != "go test -race ./..." {
		t.Errorf("the command was not rewritten, got %q", after[0].Command)
	}
	if after[0].Name != "go" || after[1].Name != "docs" {
		t.Errorf("the edit reordered the document: %s then %s", after[0].Name, after[1].Name)
	}
	if !reflect.DeepEqual(after[1], before[1]) {
		t.Errorf("the neighbour changed:\n before %+v\n after  %+v", before[1], after[1])
	}
	if got := laneConsentState(t, dir, "go"); got != ConsentStale {
		t.Errorf("the grant reports %v, want stale — remove-then-re-add is what reported ABSENT", got)
	}
	if !strings.Contains(out, "dross trust --lane go") {
		t.Errorf("a moved consent line must name the fix:\n%s", out)
	}
}

// TestLaneEditRewritesEveryInPlaceFieldKeepingPosition walks the whole widened
// set. Each case asserts the field landed, the lane kept its position and the
// neighbour is untouched — a remove-then-append implementation gets the value
// right and moves the block to the end of the document.
func TestLaneEditRewritesEveryInPlaceFieldKeepingPosition(t *testing.T) {
	for _, tc := range []struct {
		name  string
		args  []string
		check func(t *testing.T, lane project.TestLane)
	}{
		{"match", []string{"--match", "cmd/**"}, func(t *testing.T, l project.TestLane) {
			if !reflect.DeepEqual(l.Match, []string{"cmd/**"}) {
				t.Errorf("match = %q", l.Match)
			}
		}},
		{"selector", []string{"--selector", "dir"}, func(t *testing.T, l project.TestLane) {
			if l.Selector != "dir" {
				t.Errorf("selector = %q", l.Selector)
			}
		}},
		{"selector normalized", []string{"--selector", "  GO-PACKAGE "}, func(t *testing.T, l project.TestLane) {
			if l.Selector != "go-package" {
				t.Errorf("selector = %q, want the normalized spelling", l.Selector)
			}
		}},
		{"empty-exit", []string{"--selector", "dir", "--empty-exit", "5", "--empty-exit", "6"}, func(t *testing.T, l project.TestLane) {
			if !reflect.DeepEqual(l.EmptyExit, []int{5, 6}) {
				t.Errorf("empty_exit = %v", l.EmptyExit)
			}
		}},
		{"selector-template", []string{"--selector", "dir", "--selector-template", "--package {path}"}, func(t *testing.T, l project.TestLane) {
			if l.SelectorTemplate != "--package {path}" {
				t.Errorf("selector_template = %q", l.SelectorTemplate)
			}
		}},
		{"selector-join", []string{"--selector", "dir", "--selector-template", "-R {paths}", "--selector-join", "|"}, func(t *testing.T, l project.TestLane) {
			if l.SelectorJoin != "|" {
				t.Errorf("selector_join = %q", l.SelectorJoin)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := twoLaneEditFixture(t)
			before := loadLanes(t, dir)

			mustEditLane(t, append([]string{"go"}, tc.args...)...)

			after := loadLanes(t, dir)
			if after[0].Name != "go" || after[1].Name != "docs" {
				t.Fatalf("the edit reordered the document: %s then %s", after[0].Name, after[1].Name)
			}
			if !reflect.DeepEqual(after[1], before[1]) {
				t.Errorf("the neighbour changed:\n before %+v\n after  %+v", before[1], after[1])
			}
			tc.check(t, after[0])
		})
	}
}

// TestLaneEditRefusesAMalformedLaneBeforeTheWrite is the widened gate: the
// newly editable fields can introduce faults the two field-group refusals never
// covered, and writing one would put a lane on disk that `dross validate` then
// rejects — the invariant this verb exists to hold.
//
// project.toml is compared as BYTES, so a refusal that loaded, mutated and
// saved the same content by a round trip still fails.
func TestLaneEditRefusesAMalformedLaneBeforeTheWrite(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"blank command", []string{"--command", "   "}, "command"},
		{"cleared match", []string{"--match", ""}, "match"},
		{"uncompilable glob", []string{"--match", "internal/[a-"}, "compile"},
		{"unknown selector", []string{"--selector", "packages"}, "selector"},
		{"template with no selector", []string{"--selector-template", "--package {path}"}, "selector_template"},
		{"template with no placeholder", []string{"--selector", "dir", "--selector-template", "--package"}, "{path}"},
		{"join with no paths placeholder", []string{"--selector", "dir", "--selector-join", "|"}, "selector_join"},
		{"empty-exit with no selector", []string{"--empty-exit", "5"}, "empty_exit"},
		{"non-numeric empty-exit", []string{"--empty-exit", "five"}, "not a number"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := twoLaneEditFixture(t)
			path := filepath.Join(dir, RootDirName, project.File)
			before := mustRead(t, path)

			err := runCmd(t, Test(), append([]string{"lane", "edit", "go"}, tc.args...)...)
			if err == nil {
				t.Fatal("the malformed edit was accepted")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("the refusal does not mention %q: %v", tc.want, err)
			}
			if after := mustRead(t, path); after != before {
				t.Errorf("a rejected edit rewrote project.toml:\n--- before\n%s\n--- after\n%s", before, after)
			}
		})
	}
}

// TestLaneEditReadsTheProposedLaneAlone: the gate validates a SYNTHETIC
// one-lane project. Feeding it the whole modified document would refuse this
// edit because a DIFFERENT lane is malformed — and `lane edit` is precisely the
// tool for fixing a lane that a hand-edited project.toml broke.
func TestLaneEditReadsTheProposedLaneAlone(t *testing.T) {
	dir := twoLaneEditFixture(t)
	// A neighbour no CLI verb would have written: no command at all.
	appendLanes(t, dir, `[[runtime.test_lane]]
name = "broken"
match = ["broken/**"]`)

	mustEditLane(t, "go", "--command", "go test -race ./...")

	after := loadLanes(t, dir)
	if after[0].Command != "go test -race ./..." {
		t.Errorf("the edit was refused over an unrelated broken lane, got %q", after[0].Command)
	}
	if len(after) != 3 || after[2].Name != "broken" {
		t.Errorf("the broken neighbour was rewritten or dropped: %+v", after)
	}
}

// TestLaneEditClearGesturesDropTheirFields: `--empty-exit ""` and `--selector
// ""` are the only way to unset those fields short of removing the lane, which
// is the remove-then-re-add this verb exists to end.
func TestLaneEditClearGesturesDropTheirFields(t *testing.T) {
	dir := twoLaneEditFixture(t)
	mustEditLane(t, "go", "--selector", "dir", "--empty-exit", "5",
		"--selector-template", "-R {paths}", "--selector-join", "|")

	mustEditLane(t, "go", "--empty-exit", "")
	if got := loadLanes(t, dir)[0]; len(got.EmptyExit) != 0 {
		t.Errorf(`--empty-exit "" did not clear the field: %v`, got.EmptyExit)
	}

	mustEditLane(t, "go", "--selector-join", "", "--selector-template", "")
	if got := loadLanes(t, dir)[0]; got.SelectorTemplate != "" || got.SelectorJoin != "" {
		t.Errorf(`the template clear gesture left %q / %q`, got.SelectorTemplate, got.SelectorJoin)
	}

	mustEditLane(t, "go", "--selector", "")
	if got := loadLanes(t, dir)[0]; got.Selector != "" {
		t.Errorf(`--selector "" did not clear the field: %q`, got.Selector)
	}

	// The key must be GONE, not written empty: omitempty is what keeps a lane
	// that dropped a field byte-identical to one that never declared it.
	if raw := mustRead(t, filepath.Join(dir, RootDirName, project.File)); strings.Contains(raw, "selector_template") || strings.Contains(raw, "empty_exit") {
		t.Errorf("a cleared field survived as a key:\n%s", raw)
	}
}

// TestLaneEditOmittedFlagsLeaveEveryOtherFieldAlone is the Changed guard across
// the widened set. An unconditional write would let `--command x` clear a
// template the user never mentioned — the same omitted-means-clear collapse
// that would silently drop a prepare, one flag deeper and across eight fields.
func TestLaneEditOmittedFlagsLeaveEveryOtherFieldAlone(t *testing.T) {
	dir := twoLaneEditFixture(t)
	mustEditLane(t, "go",
		"--selector", "dir",
		"--empty-exit", "5",
		"--selector-template", "-R {paths}",
		"--selector-join", "|",
		"--prepare", "make build",
		"--toolchain", "go",
		"--install", "brew install go")
	before := loadLanes(t, dir)[0]

	mustEditLane(t, "go", "--command", "go test -race ./...")

	after := loadLanes(t, dir)[0]
	want := before
	want.Command = "go test -race ./..."
	if !reflect.DeepEqual(after, want) {
		t.Errorf("an omitted flag changed the field it names:\n before %+v\n after  %+v", want, after)
	}
}

// TestLaneEditWithNoFlagsIsRefused: the no-op refusal has to name the widened
// set, or a user reading it would believe the fields it omits are still
// remove-then-re-add.
func TestLaneEditWithNoFlagsIsRefused(t *testing.T) {
	twoLaneEditFixture(t)
	err := runCmd(t, Test(), "lane", "edit", "go")
	if err == nil {
		t.Fatal("`lane edit go` with no flags was accepted")
	}
	for _, want := range []string{"--match", "--command", "--selector", "--selector-template", "--selector-join", "--empty-exit"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the no-op refusal does not name %s: %v", want, err)
		}
	}
}

// TestLaneEditHelpNoLongerClaimsRemoveThenReAdd is the stale-claim assertion.
// The help text said match, command, selector and empty_exit were
// remove-then-re-add; c-3 falsifies that, and a help text that still says it
// routes the user around the very verb that now does the job — and around its
// refusals, into hand-editing project.toml.
func TestLaneEditHelpNoLongerClaimsRemoveThenReAdd(t *testing.T) {
	c := testLaneEdit()
	text := c.Short + "\n" + c.Long
	for _, field := range []string{"match", "command", "selector", "empty_exit"} {
		for _, line := range strings.Split(text, "\n") {
			if strings.Contains(line, "remove") && strings.Contains(line, field) {
				t.Errorf("the help still routes %s through remove-then-re-add: %q", field, line)
			}
		}
	}
	// The one field that IS still remove-then-re-add must still say so: it
	// keys the consent store, so it cannot move without the grant moving.
	if !strings.Contains(strings.ToUpper(text), "NAME") {
		t.Errorf("the help no longer says the lane's name is the field that cannot be edited:\n%s", text)
	}
}

// TestLaneAddWritesAndEchoesTheTemplate is the declaration echo the deferred
// `lane list` rendering rests on: `lane list` does not print a template, so if
// `lane add` stops echoing one there is no surface that shows a declared
// template without reading project.toml back.
func TestLaneAddWritesAndEchoesTheTemplate(t *testing.T) {
	dir := laneFixture(t)
	var out string
	if err := runCmdCapturing(t, &out, Test(), "lane", "add", "rust",
		"--match", "crates/**", "--command", "cargo test",
		"--selector", "dir", "--selector-template", "--package {path}"); err != nil {
		t.Fatalf("lane add: %v", err)
	}
	lane := loadLanes(t, dir)[0]
	if lane.SelectorTemplate != "--package {path}" {
		t.Errorf("selector_template = %q", lane.SelectorTemplate)
	}
	if !strings.Contains(out, "--package {path}") {
		t.Errorf("`lane add` did not echo the declared template:\n%s", out)
	}

	var joinOut string
	if err := runCmdCapturing(t, &joinOut, Test(), "lane", "add", "ctest",
		"--match", "tests/**", "--command", "ctest",
		"--selector", "path", "--selector-template", "-R {paths}", "--selector-join", "|"); err != nil {
		t.Fatalf("lane add: %v", err)
	}
	if !strings.Contains(joinOut, "-R {paths}") || !strings.Contains(joinOut, "selector-join: |") {
		t.Errorf("`lane add` did not echo the declared join:\n%s", joinOut)
	}
}

// TestLaneAddRefusesATemplateWithNoSelectorBeforeTheWrite: the add path is
// gated by the same rules validate reports, so a template with nothing to place
// never reaches disk.
func TestLaneAddRefusesATemplateWithNoSelectorBeforeTheWrite(t *testing.T) {
	dir := laneFixture(t)
	path := filepath.Join(dir, RootDirName, project.File)
	before := mustRead(t, path)

	err := runCmd(t, Test(), "lane", "add", "rust",
		"--match", "crates/**", "--command", "cargo test",
		"--selector-template", "--package {path}")
	if err == nil {
		t.Fatal("a template with no selector was accepted")
	}
	if !strings.Contains(err.Error(), "selector_template") {
		t.Errorf("the refusal does not name the field: %v", err)
	}
	if after := mustRead(t, path); after != before {
		t.Errorf("a rejected add rewrote project.toml:\n--- before\n%s\n--- after\n%s", before, after)
	}
}

// TestLaneEditEchoesTheTemplate is the edit-side half of the declaration echo.
func TestLaneEditEchoesTheTemplate(t *testing.T) {
	twoLaneEditFixture(t)
	var out string
	if err := runCmdCapturing(t, &out, Test(), "lane", "edit", "go",
		"--selector", "path", "--selector-template", "-R {paths}", "--selector-join", "|"); err != nil {
		t.Fatalf("lane edit: %v", err)
	}
	if !strings.Contains(out, "-R {paths}") || !strings.Contains(out, "selector-join: |") {
		t.Errorf("`lane edit` did not echo the declared template and join:\n%s", out)
	}
}

// TestLaneEditWarnsOnAScopedWholeTreeCommand is the edit-side half of c-4's
// declaration-time surface. An edit is how a lane ACQUIRES the combination —
// adding a selector to a whole-tree command, or rewriting a scoped command into
// a whole-tree one — so a warning that only fired on `lane add` would miss the
// path most likely to introduce it.
func TestLaneEditWarnsOnAScopedWholeTreeCommand(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
	}{
		{"selector added to a whole-tree command", []string{"--selector", "go-package"}},
		{"command rewritten to whole-tree", []string{"--selector", "dir", "--command", "go test ."}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := twoLaneEditFixture(t)
			var out string
			if err := runCmdCapturing(t, &out, Test(), append([]string{"lane", "edit", "go"}, tc.args...)...); err != nil {
				t.Fatalf("`lane edit` refused a lane it should only warn about: %v", err)
			}
			// Written anyway: the warning is advisory, and a lane the user
			// cannot save is a refusal by another name.
			if got := loadLanes(t, dir)[0]; got.Selector == "" {
				t.Fatalf("the edit was not written: %+v", got)
			}
			if !strings.Contains(out, "⚠") || !strings.Contains(out, `"go"`) {
				t.Errorf("`lane edit` did not warn, or the warning does not name the lane:\n%s", out)
			}
		})
	}
}

// TestLaneEditDoesNotWarnWhenTheCommandStopsBeingWholeTree: the warning reads
// the PROPOSED lane, not the one on disk, so an edit that fixes the very
// combination it warns about must come back clean.
func TestLaneEditDoesNotWarnWhenTheCommandStopsBeingWholeTree(t *testing.T) {
	twoLaneEditFixture(t)
	mustEditLane(t, "go", "--selector", "go-package")

	var out string
	if err := runCmdCapturing(t, &out, Test(), "lane", "edit", "go", "--command", "go test -count=1"); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "⚠") {
		t.Errorf("the warning fired on the edit that removed the whole-tree token:\n%s", out)
	}
}

// TestLaneSelectorRefusalIsGone pins the deletion.
//
// laneSelectorRefusal covered one field group and was reachable while selector
// and empty_exit were the only fields the CLI could write. t-5/t-6 replaced that
// with laneRefusal, which validates a synthetic one-lane project through
// laneProblems — and laneProblems already calls laneSelectorProblems, so the old
// function's check became a strict subset of the live gate with no callers.
//
// It was found by mutation testing, not by review: gremlins reported a survivor
// at its `len(problems) == 0` branch, NOT COVERED, because nothing reached it.
// A dead guard is worse than no guard — it reads as protection while protecting
// nothing, and the next person to touch this file has to re-derive that it is
// unreachable.
//
// Asserted over the package source rather than by calling it, because the point
// is that it does not exist to call.
func TestLaneSelectorRefusalIsGone(t *testing.T) {
	src, err := os.ReadFile("test_lane.go")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(src), "func laneSelectorRefusal(") {
		t.Error("laneSelectorRefusal is back. laneRefusal already runs laneProblems " +
			"over the whole proposed lane, and laneProblems calls laneSelectorProblems — " +
			"so this would be a second, unreachable copy of a live check")
	}
	// The live gate must still be there, or this test would pass on a file that
	// deleted the wrong thing.
	if !strings.Contains(string(src), "func laneRefusal(") {
		t.Error("laneRefusal is missing — the gate that replaced it")
	}
}
