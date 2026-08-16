package mutation

import (
	"os"
	"path/filepath"
	"testing"
)

// inapplicableFixture writes a source file carrying all three shapes the filter
// has to tell apart: a const block, a string concatenation, and ordinary
// arithmetic.
func inapplicableFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	src := `package x

const (
	base   = 10
	offset = base + 5
)

func Greet(name string) string {
	return "name: " + name
}

func Add(a, b int) int {
	return a + b
}
`
	if err := os.WriteFile(filepath.Join(dir, "x.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// lineOf returns the 1-based line holding needle, so the tests name shapes
// rather than line numbers that shift when the fixture is edited.
func lineOf(t *testing.T, dir, needle string) int {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, "x.go"))
	if err != nil {
		t.Fatal(err)
	}
	for i, l := range splitLines(string(b)) {
		if contains(l, needle) {
			return i + 1
		}
	}
	t.Fatalf("fixture has no line containing %q", needle)
	return 0
}

// TestInapplicableMutantsAreDropped: a const-initializer ARITHMETIC_BASE has no
// coverage block, so no test can ever kill it. Reported as a survivor it
// inflates the denominator and spends a human decision on something with no
// other outcome available.
func TestInapplicableMutantsAreDropped(t *testing.T) {
	dir := inapplicableFixture(t)
	constLine := lineOf(t, dir, "offset = base + 5")

	r := &Report{
		Tool: "gremlins", Survived: 1, NotCovered: 1,
		Files:     map[string]FileStat{"x.go": {Survived: 1, NotCovered: 1}},
		Surviving: []Mutant{{File: "x.go", Line: constLine, Op: "ARITHMETIC_BASE"}},
	}
	DropInapplicable(r, dir)

	if len(r.Surviving) != 0 {
		t.Errorf("the const-initializer mutant is still in the survivor list: %+v", r.Surviving)
	}
	if r.Survived != 0 || r.NotCovered != 0 {
		t.Errorf("counters still carry it: survived=%d not_covered=%d", r.Survived, r.NotCovered)
	}
	if s := r.Files["x.go"]; s.Survived != 0 || s.NotCovered != 0 {
		t.Errorf("the per-file row still carries it: %+v — a row left behind makes the file's numbers disagree with the report's", s)
	}
}

// TestStringConcatMutantsAreNotDropped is the narrowing, and it is the half
// that matters most.
//
// The idea that reached this phase asked to drop these too. This repo's own
// TestStringConcatArithmeticMutantIsNoOp proves the swap does not compile AND
// that gremlins only builds mutants on covered lines — so a concat SURVIVOR is
// always a coverage gap that covering the line would close. Dropping it would
// silence a real gap under the banner of removing a no-op.
func TestStringConcatMutantsAreNotDropped(t *testing.T) {
	dir := inapplicableFixture(t)
	concatLine := lineOf(t, dir, `"name: " + name`)

	r := &Report{
		Tool: "gremlins", Survived: 1,
		Files:     map[string]FileStat{"x.go": {Survived: 1}},
		Surviving: []Mutant{{File: "x.go", Line: concatLine, Op: "ARITHMETIC_BASE"}},
	}
	DropInapplicable(r, dir)

	if len(r.Surviving) != 1 {
		t.Errorf("a string-concat survivor was dropped — that survivor is a coverage gap, and hiding it is the opposite of this filter's purpose")
	}
	if r.Survived != 1 {
		t.Errorf("survived = %d, want 1", r.Survived)
	}
}

// TestRealArithmeticMutantsSurviveTheFilter: keyed on the line's SHAPE, never
// on the operator alone. ARITHMETIC_BASE on ordinary arithmetic is exactly the
// mutant this tooling exists to surface.
func TestRealArithmeticMutantsSurviveTheFilter(t *testing.T) {
	dir := inapplicableFixture(t)
	addLine := lineOf(t, dir, "return a + b")

	r := &Report{
		Tool: "gremlins", Survived: 1,
		Files:     map[string]FileStat{"x.go": {Survived: 1}},
		Surviving: []Mutant{{File: "x.go", Line: addLine, Op: "ARITHMETIC_BASE"}},
	}
	DropInapplicable(r, dir)

	if len(r.Surviving) != 1 {
		t.Fatal("a real arithmetic mutant was dropped — the filter is keying on the operator, which silences the signal it exists to protect")
	}
}

// TestDroppedMutantsLeaveTheDenominator: a mutant filtered out of the list but
// left in the denominator is the disagreement this whole phase closes, in a new
// place.
func TestDroppedMutantsLeaveTheDenominator(t *testing.T) {
	dir := inapplicableFixture(t)
	constLine := lineOf(t, dir, "offset = base + 5")

	r := &Report{
		Tool: "gremlins", Killed: 9, Survived: 1, NotCovered: 1,
		Files:     map[string]FileStat{"x.go": {Killed: 9, Survived: 1, NotCovered: 1}},
		Surviving: []Mutant{{File: "x.go", Line: constLine, Op: "ARITHMETIC_BASE"}},
	}
	DropInapplicable(r, dir)

	if got := r.Killed + r.Survived + r.Timeout; got != 9 {
		t.Errorf("denominator = %d, want 9 — the dropped mutant is still being scored against", got)
	}
	if r.Score != 1.0 {
		t.Errorf("score = %v, want 1.0 — nine of nine killable mutants were killed", r.Score)
	}
}

// TestUnreadableSourceDropsNothing: the filter removes what it is CERTAIN
// about. "I could not read the source" is not certainty, and dropping on a
// guess would be the same lie as reporting an unkillable mutant, pointing the
// other way.
func TestUnreadableSourceDropsNothing(t *testing.T) {
	r := &Report{
		Tool: "gremlins", Survived: 1,
		Files:     map[string]FileStat{"gone.go": {Survived: 1}},
		Surviving: []Mutant{{File: "gone.go", Line: 4, Op: "ARITHMETIC_BASE"}},
	}
	DropInapplicable(r, t.TempDir())
	if len(r.Surviving) != 1 {
		t.Error("a mutant was dropped on a file the filter could not read")
	}

	// And an empty root is the same: no basis, no drop.
	r2 := &Report{Survived: 1, Surviving: []Mutant{{File: "x.go", Line: 4, Op: "ARITHMETIC_BASE"}}}
	DropInapplicable(r2, "")
	if len(r2.Surviving) != 1 {
		t.Error("a mutant was dropped with no root to check against")
	}
}

// TestNonArithmeticMutantsOnConstLinesStay: the rule is the operator AND the
// shape. A CONDITIONALS_NEGATION on a const line is a different claim and this
// filter has no evidence about it.
func TestNonArithmeticMutantsOnConstLinesStay(t *testing.T) {
	dir := inapplicableFixture(t)
	constLine := lineOf(t, dir, "offset = base + 5")

	r := &Report{
		Tool: "gremlins", Survived: 1,
		Files:     map[string]FileStat{"x.go": {Survived: 1}},
		Surviving: []Mutant{{File: "x.go", Line: constLine, Op: "CONDITIONALS_NEGATION"}},
	}
	DropInapplicable(r, dir)
	if len(r.Surviving) != 1 {
		t.Error("a non-arithmetic mutant on a const line was dropped — the filter's evidence covers ARITHMETIC_BASE only")
	}
}

func splitLines(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	return append(out, s[start:])
}

func contains(hay, needle string) bool {
	for i := 0; i+len(needle) <= len(hay); i++ {
		if hay[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
