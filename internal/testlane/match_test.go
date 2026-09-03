package testlane

import (
	"reflect"
	"testing"
)

// TestDoubleStarCrossesDirectories separates the two star forms, which is the
// whole reason this package exists rather than a bare filepath.Match call:
// filepath.Match has no `**` at all, so a `**` that collapsed to one segment
// would make every lane written the obvious way silently miss its own package
// tree.
func TestDoubleStarCrossesDirectories(t *testing.T) {
	cases := []struct {
		pattern string
		path    string
		want    bool
	}{
		{"internal/**/*.go", "internal/cmd/test.go", true},
		{"internal/**/*.go", "internal/a/b/c.go", true},
		{"internal/**/*.go", "internal/x.go", true}, // ** spans zero segments too
		{"internal/**/*.go", "main.go", false},
		{"internal/*.go", "internal/x.go", true},
		{"internal/*.go", "internal/cmd/test.go", false},
		{"internal/**", "internal/cmd/test.go", true},
		{"**/*.go", "internal/cmd/test.go", true},
		{"**/*.go", "main.go", true},
		{"**/*.go", "docs/x.md", false},
	}
	for _, c := range cases {
		if got := matchGlob(c.pattern, c.path); got != c.want {
			t.Errorf("matchGlob(%q, %q) = %v, want %v", c.pattern, c.path, got, c.want)
		}
	}
}

// TestSingleStarStopsAtSeparator is the other half of the pair: if `*` were
// allowed to cross a separator, a docs lane would swallow every nested file and
// two lanes that look disjoint on the page would both fire.
func TestSingleStarStopsAtSeparator(t *testing.T) {
	if matchGlob("docs/*.md", "docs/api/v1.md") {
		t.Error("docs/*.md must not match docs/api/v1.md — * stops at a separator")
	}
	if !matchGlob("docs/*.md", "docs/x.md") {
		t.Error("docs/*.md must match docs/x.md")
	}
}

// TestTrailingSlashMeansEverythingBeneath: `docs/` is what a user writes for a
// directory, and no file path ends in a separator, so without the expansion it
// would be a lane that matches nothing while looking perfectly reasonable.
func TestTrailingSlashMeansEverythingBeneath(t *testing.T) {
	for _, p := range []string{"docs/x.md", "docs/sub/y.md"} {
		if !matchGlob("docs/", p) {
			t.Errorf("docs/ must match %s", p)
		}
	}
	if matchGlob("docs/", "other/x.md") {
		t.Error("docs/ must not match outside docs")
	}
}

// TestLiteralPatternMatchesOnlyItself keeps a metacharacter-free pattern exact.
// A lane declared as `main.go` must not become a prefix or substring rule.
func TestLiteralPatternMatchesOnlyItself(t *testing.T) {
	if !matchGlob("main.go", "main.go") {
		t.Error("literal pattern must match its own path")
	}
	for _, p := range []string{"cmd/main.go", "main.go.bak", "main"} {
		if matchGlob("main.go", p) {
			t.Errorf("literal main.go must not match %s", p)
		}
	}
}

// TestUncompilableGlobMatchesNothing: validate reports a broken pattern as a
// project.toml problem; the matcher's job is only to not panic and not match.
// Matching on error would be worse than missing — a lane whose glob is garbage
// would run against everything.
func TestUncompilableGlobMatchesNothing(t *testing.T) {
	if matchGlob("internal/[cmd/**", "internal/cmd/test.go") {
		t.Error("an uncompilable pattern must match nothing")
	}
}

// TestSelectIsOrderedAndDeduped pins the locked multi_lane decision: every lane
// with a hit runs, in declaration order, once. An implementation that returned
// the first match only would give array position a precedence the decision
// explicitly denies it.
func TestSelectIsOrderedAndDeduped(t *testing.T) {
	globs := [][]string{
		{"internal/**"},
		{"**/*.go"},
	}
	got := Select(globs, []string{"internal/cmd/test.go"})
	if !reflect.DeepEqual(got.Lanes, []int{0, 1}) {
		t.Errorf("Lanes = %v, want [0 1] in declaration order", got.Lanes)
	}
	if len(got.Unmatched) != 0 || len(got.OutOfTree) != 0 {
		t.Errorf("a matched path must not also be reported: %+v", got)
	}
}

// TestTwoGlobsInOneLaneYieldOneIndex: a duplicate index is not cosmetic —
// downstream it is one lane's command spawned twice, so a green run would
// double the wall-clock and a red one would report the same failure twice.
func TestTwoGlobsInOneLaneYieldOneIndex(t *testing.T) {
	globs := [][]string{{"internal/**", "**/*.go"}}
	got := Select(globs, []string{"internal/cmd/test.go"})
	if !reflect.DeepEqual(got.Lanes, []int{0}) {
		t.Errorf("Lanes = %v, want [0] exactly once", got.Lanes)
	}
}

// TestDotSlashPrefixIsNotAMiss: an agent building argv from plan.toml or from
// `go list` output routinely emits ./-prefixed paths. If normalization dropped
// that, the file set would match no lane and the run would refuse — a refusal
// with no visible cause, since the path on screen looks correct.
func TestDotSlashPrefixIsNotAMiss(t *testing.T) {
	globs := [][]string{{"internal/**"}}
	plain := Select(globs, []string{"internal/cmd/test.go"})
	dotted := Select(globs, []string{"./internal/cmd/test.go"})
	if !reflect.DeepEqual(plain.Lanes, dotted.Lanes) {
		t.Errorf("./-prefixed path resolved differently: %v vs %v", plain.Lanes, dotted.Lanes)
	}
	if len(dotted.Unmatched) != 0 {
		t.Errorf("./-prefixed path reported as unmatched: %v", dotted.Unmatched)
	}
}

// TestSelectReportsUnmatchedPaths: the miss has to come back. A selector that
// returned only the lanes it found would let `--files docs/x.md` look like a
// clean resolution of zero lanes, which downstream reads as a green run that
// measured nothing.
func TestSelectReportsUnmatchedPaths(t *testing.T) {
	globs := [][]string{{"internal/**"}}
	got := Select(globs, []string{"internal/a.go", "docs/x.md", "README.md"})
	if !reflect.DeepEqual(got.Lanes, []int{0}) {
		t.Errorf("Lanes = %v, want [0]", got.Lanes)
	}
	if !reflect.DeepEqual(got.Unmatched, []string{"docs/x.md", "README.md"}) {
		t.Errorf("Unmatched = %v, want both misses in argv order", got.Unmatched)
	}
}

// TestUnmatchedKeepsTheCallersSpelling: the refusal text is read by whoever
// typed the argv, so it must echo what they typed. Printing a normalized form
// sends them looking for a path that appears nowhere in their command.
func TestUnmatchedKeepsTheCallersSpelling(t *testing.T) {
	got := Select([][]string{{"internal/**"}}, []string{"./docs/x.md"})
	if !reflect.DeepEqual(got.Unmatched, []string{"./docs/x.md"}) {
		t.Errorf("Unmatched = %v, want the path as written", got.Unmatched)
	}
}

// TestOutOfTreePathsAreTheirOwnCategory is the locked split between an argv
// fault and a lane-configuration gap. Collapsed into Unmatched, an absolute
// path would be reported as "no lane matches this" and send the user to edit
// project.toml over a mistake in their own command line.
func TestOutOfTreePathsAreTheirOwnCategory(t *testing.T) {
	globs := [][]string{{"internal/**"}}
	got := Select(globs, []string{"/etc/passwd", "../outside/x.go"})
	if !reflect.DeepEqual(got.OutOfTree, []string{"/etc/passwd", "../outside/x.go"}) {
		t.Errorf("OutOfTree = %v, want both escapes", got.OutOfTree)
	}
	if len(got.Unmatched) != 0 {
		t.Errorf("an escape must not also be reported as unmatched: %v", got.Unmatched)
	}
}

// TestEscapeIsDetectedAfterCleaning: `internal/../../x` reads as an in-tree
// path segment by segment and is an escape only once cleaned. Checking the raw
// string for a leading `..` would pass it straight through.
func TestEscapeIsDetectedAfterCleaning(t *testing.T) {
	got := Select([][]string{{"internal/**"}}, []string{"internal/../../x.go"})
	if !reflect.DeepEqual(got.OutOfTree, []string{"internal/../../x.go"}) {
		t.Errorf("OutOfTree = %v, want the escaping path", got.OutOfTree)
	}
	// The inverse: a `..` that resolves back inside is not an escape.
	inside := Select([][]string{{"internal/**"}}, []string{"internal/cmd/../a.go"})
	if !reflect.DeepEqual(inside.Lanes, []int{0}) {
		t.Errorf("a path that cleans back inside must resolve normally, got %+v", inside)
	}
}

// TestEmptyPathSetStatesNothing: the selector reports facts. An empty set is
// zero lanes, zero misses and zero escapes — it is t-5's job to decide that a
// run which resolved nothing must not report green, and a selector that
// invented a refusal here would take that decision away from it.
func TestEmptyPathSetStatesNothing(t *testing.T) {
	got := Select([][]string{{"internal/**"}}, nil)
	if len(got.Lanes) != 0 || len(got.Unmatched) != 0 || len(got.OutOfTree) != 0 {
		t.Errorf("empty path set must state nothing, got %+v", got)
	}
}

// TestNoLanesDeclaredReportsEveryPathAsAMiss: with no lanes there is nothing to
// match, and every path is therefore unplaced. Reporting them keeps the
// lane-less repo's behaviour a decision the caller makes rather than one the
// selector makes by returning an empty answer that looks like success.
func TestNoLanesDeclaredReportsEveryPathAsAMiss(t *testing.T) {
	got := Select(nil, []string{"internal/a.go"})
	if len(got.Lanes) != 0 {
		t.Errorf("Lanes = %v, want none", got.Lanes)
	}
	if !reflect.DeepEqual(got.Unmatched, []string{"internal/a.go"}) {
		t.Errorf("Unmatched = %v, want the path", got.Unmatched)
	}
}

// TestDuplicatePathsAreReportedOnce: the same miss twice in one argv is one
// problem, and repeating it in the refusal makes a two-path mistake look like a
// four-path one.
func TestDuplicatePathsAreReportedOnce(t *testing.T) {
	got := Select([][]string{{"internal/**"}}, []string{"docs/x.md", "./docs/x.md"})
	if !reflect.DeepEqual(got.Unmatched, []string{"docs/x.md"}) {
		t.Errorf("Unmatched = %v, want the miss reported once", got.Unmatched)
	}
}

// contains reports membership, so the Matched assertions can say "and NOT this
// path" without spelling out a whole slice each time.
func contains(hay []string, needle string) bool {
	for _, s := range hay {
		if s == needle {
			return true
		}
	}
	return false
}

// TestMatchedRecordsOnlyThePathsThatHitTheLane is the c-4 property at its
// source: a lane's selector is derived from ITS paths. An implementation that
// appends every in-tree path to every hit lane still produces the right Lanes,
// so it would pass every pre-existing test here and only surface as a go lane
// spawning `go test ./docs/...` at the run site.
func TestMatchedRecordsOnlyThePathsThatHitTheLane(t *testing.T) {
	globs := [][]string{{"internal/**"}, {"docs/"}}
	sel := Select(globs, []string{"internal/a.go", "docs/x.md", "README.md", "/etc/passwd"})

	if !reflect.DeepEqual(sel.Matched[0], []string{"internal/a.go"}) {
		t.Errorf("Matched[0] = %v, want exactly [internal/a.go]", sel.Matched[0])
	}
	if !reflect.DeepEqual(sel.Matched[1], []string{"docs/x.md"}) {
		t.Errorf("Matched[1] = %v, want exactly [docs/x.md]", sel.Matched[1])
	}
	// Asserted in BOTH entries: the unmatched in-tree path and the
	// out-of-tree one are the two shapes that must never reach a runner.
	for lane, got := range sel.Matched {
		if contains(got, "README.md") {
			t.Errorf("lane %d carries README.md, which matched no lane: %v", lane, got)
		}
		if contains(got, "/etc/passwd") || contains(got, "etc/passwd") {
			t.Errorf("lane %d carries an out-of-tree path: %v", lane, got)
		}
	}
}

// TestMatchedIsNormalizedWhileUnmatchedIsNot pins the deliberate split between
// the two: a human reads Unmatched and must find the path they typed, while
// Matched is appended to a command line where two spellings of one file must
// not derive two selectors. A single shared slice breaks one assertion or the
// other.
func TestMatchedIsNormalizedWhileUnmatchedIsNot(t *testing.T) {
	globs := [][]string{{"internal/**"}}
	sel := Select(globs, []string{"./internal/a.go", "./README.md"})

	if !reflect.DeepEqual(sel.Matched[0], []string{"internal/a.go"}) {
		t.Errorf("Matched[0] = %v, want the normalized [internal/a.go]", sel.Matched[0])
	}
	if !reflect.DeepEqual(sel.Unmatched, []string{"./README.md"}) {
		t.Errorf("Unmatched = %v, want the caller's own ./README.md", sel.Unmatched)
	}
}

// TestMatchedRecordsAPathOncePerLane extends the existing one-hit-settles-a-lane
// rule from lane indices to paths: two of a lane's globs catching the same file
// must not put that file in the selector twice, the same way they never enqueue
// the lane's command twice.
func TestMatchedRecordsAPathOncePerLane(t *testing.T) {
	globs := [][]string{{"internal/**", "internal/cmd/*.go"}}
	sel := Select(globs, []string{"internal/cmd/test.go"})

	if len(sel.Matched[0]) != 1 {
		t.Fatalf("Matched[0] = %v, want one entry — both globs caught the same path", sel.Matched[0])
	}
}

// TestAPathMatchedByTwoLanesAppearsInBoth mirrors Select's existing per-lane
// hit[i] behaviour: two lanes claiming a file both run, so both derive a
// selector carrying it. Recording it against only the first lane would leave
// the second spawning its whole suite.
func TestAPathMatchedByTwoLanesAppearsInBoth(t *testing.T) {
	globs := [][]string{{"internal/**"}, {"**/*.go"}}
	sel := Select(globs, []string{"internal/a.go"})

	if !reflect.DeepEqual(sel.Lanes, []int{0, 1}) {
		t.Fatalf("Lanes = %v, want both lanes selected", sel.Lanes)
	}
	if !contains(sel.Matched[0], "internal/a.go") || !contains(sel.Matched[1], "internal/a.go") {
		t.Errorf("Matched = %v, want internal/a.go under both lanes", sel.Matched)
	}
}

// TestAnUnhitLaneHasNoMatchedKeyAtAll: absence, not a nil value. The run site
// ranges over lanes to build selectors, and a key present with an empty slice
// would let a `for i := range Matched` resurrect a lane Lanes deliberately
// excluded — a lane spawning its command over a file set that never touched it.
func TestAnUnhitLaneHasNoMatchedKeyAtAll(t *testing.T) {
	globs := [][]string{{"internal/**"}, {"docs/"}}
	sel := Select(globs, []string{"internal/a.go"})

	if _, ok := sel.Matched[1]; ok {
		t.Errorf("lane 1 matched nothing but has a Matched key: %v", sel.Matched)
	}
	if len(sel.Matched) != 1 {
		t.Errorf("Matched has %d keys, want only the one hit lane: %v", len(sel.Matched), sel.Matched)
	}
}

// TestNoMatchLeavesMatchedNil: a file set that hit nothing must not allocate a
// map, so the zero Selection's "nothing to run and nothing to report" stays
// exactly that.
func TestNoMatchLeavesMatchedNil(t *testing.T) {
	sel := Select([][]string{{"internal/**"}}, []string{"README.md"})
	if sel.Matched != nil {
		t.Errorf("Matched = %v, want nil when no lane was hit", sel.Matched)
	}
}
