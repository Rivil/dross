package mutation

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// This file is the evidence behind the gremlins-attribution-ceiling category
// and behind the string-concat half of the ARITHMETIC_BASE question. Both were
// previously asserted in prose. An acceptance whose reason rests on an
// assertion is indistinguishable from one that is simply wrong, so each claim
// here is checked against a live tool run rather than restated.
//
// testdata/ceiling/ holds the fixture: a switch-case condition, a const
// initializer and a string concatenation. `go build ./...` and `go test ./...`
// skip testdata, so the fixture is only ever driven by explicit path from here.

const ceilingPkg = "./testdata/ceiling"

// ceilingSourceLine returns the 1-based line in the fixture holding substr,
// failing if it is absent or occurs more than once. Line numbers are derived
// rather than hardcoded: a fixture edit that shifts them must not silently
// re-point an assertion at the wrong line.
func ceilingSourceLine(t *testing.T, substr string) int {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "ceiling", "ceiling.go"))
	if err != nil {
		t.Fatal(err)
	}
	found := 0
	line := 0
	for i, l := range strings.Split(string(b), "\n") {
		if strings.Contains(l, substr) {
			found++
			line = i + 1
		}
	}
	if found != 1 {
		t.Fatalf("fixture holds %d lines containing %q, want exactly 1", found, substr)
	}
	return line
}

// coverBlock is one entry of a go-cover profile.
type coverBlock struct {
	startLine, endLine, count int
}

// ceilingCoverage runs `go test -coverprofile` over the fixture LIVE and parses
// the profile. Live rather than recorded, because the claim is about what the
// test suite does today: a recorded profile would keep proving the ceiling long
// after the fixture stopped exercising the line.
func ceilingCoverage(t *testing.T) []coverBlock {
	t.Helper()
	profile := filepath.Join(t.TempDir(), "cover.out")
	cmd := exec.Command("go", "test", "-count=1", "-coverprofile="+profile, ceilingPkg)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("coverage run over the fixture failed: %v\n%s", err, out)
	}
	b, err := os.ReadFile(profile)
	if err != nil {
		t.Fatal(err)
	}

	var blocks []coverBlock
	for _, line := range strings.Split(string(b), "\n") {
		// <path>:<startLine>.<startCol>,<endLine>.<endCol> <numStmt> <count>
		if !strings.Contains(line, "ceiling.go:") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 3 {
			t.Fatalf("unparseable profile line %q", line)
		}
		span := fields[0][strings.LastIndex(fields[0], ":")+1:]
		from, to, ok := strings.Cut(span, ",")
		if !ok {
			t.Fatalf("unparseable span %q", span)
		}
		start := atoiBefore(t, from, ".")
		end := atoiBefore(t, to, ".")
		count, err := strconv.Atoi(fields[2])
		if err != nil {
			t.Fatalf("unparseable count in %q: %v", line, err)
		}
		blocks = append(blocks, coverBlock{startLine: start, endLine: end, count: count})
	}
	if len(blocks) == 0 {
		t.Fatal("coverage profile holds no blocks for the fixture")
	}
	return blocks
}

func atoiBefore(t *testing.T, s, sep string) int {
	t.Helper()
	head, _, _ := strings.Cut(s, sep)
	n, err := strconv.Atoi(head)
	if err != nil {
		t.Fatalf("unparseable line number in %q: %v", s, err)
	}
	return n
}

// coveredCount returns the highest execution count of any block covering line,
// and whether line falls inside a block at all. The two are different facts: a
// line with no block was never instrumented, which is the const initializer's
// whole story.
func coveredCount(blocks []coverBlock, line int) (int, bool) {
	best, inBlock := 0, false
	for _, b := range blocks {
		if line >= b.startLine && line <= b.endLine {
			inBlock = true
			if b.count > best {
				best = b.count
			}
		}
	}
	return best, inBlock
}

// ceilingMutants returns the recorded gremlins report's mutants for the fixture.
type ceilingMutant struct {
	Line   int
	Type   string
	Status string
}

func ceilingReport(t *testing.T) []ceilingMutant {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "ceiling", "gremlins-report.json"))
	if err != nil {
		t.Fatal(err)
	}
	var raw gremlinsReport
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatalf("recorded report is unparseable: %v", err)
	}
	var out []ceilingMutant
	for _, f := range raw.Files {
		for _, m := range f.Mutations {
			out = append(out, ceilingMutant{Line: m.Line, Type: m.Type, Status: m.Status})
		}
	}
	if len(out) == 0 {
		t.Fatal("recorded report holds no mutants")
	}
	return out
}

func mutantsOn(all []ceilingMutant, line int) []ceilingMutant {
	var out []ceilingMutant
	for _, m := range all {
		if m.Line == line {
			out = append(out, m)
		}
	}
	return out
}

// TestAttributionCeilingIsReal is the proof the gremlins-attribution-ceiling
// category cites. A switch-case CONDITION that the test suite provably
// executes — count>=1 in a live coverage profile — is still reported NOT
// COVERED by gremlins. go-cover's block for a case arm begins at the colon, so
// the condition's own columns belong to no block and gremlins, reading that
// profile, concludes the line was never run.
//
// This is why those survivors cannot be killed by writing more tests: the tests
// already run them. Without this test the category's reason is an assertion,
// and an acceptance resting on an assertion is indistinguishable from a wrong one.
func TestAttributionCeilingIsReal(t *testing.T) {
	caseLine := ceilingSourceLine(t, "case r >= 'a' && r <= 'z':")
	blocks := ceilingCoverage(t)

	count, inBlock := coveredCount(blocks, caseLine)
	if !inBlock {
		t.Fatalf("fixture line %d is in no coverage block — the fixture no longer exercises the switch", caseLine)
	}
	if count < 1 {
		t.Fatalf("fixture line %d has coverage count %d, want >=1: the proof needs the line PROVABLY executed", caseLine, count)
	}

	got := mutantsOn(ceilingReport(t), caseLine)
	if len(got) == 0 {
		t.Fatalf("recorded report holds no mutants on line %d", caseLine)
	}
	for _, m := range got {
		if m.Status != "NOT COVERED" {
			t.Errorf("line %d mutant %s recorded as %q, want NOT COVERED — re-recording it as covered would dissolve the ceiling this category rests on",
				caseLine, m.Type, m.Status)
		}
	}
}

// TestConstInitializerHasNoCoverageBlock is the const half's OWN evidence,
// deliberately distinct from the string-concat proof below.
//
// A const initializer is evaluated at compile time and produces no statement,
// so go-cover emits no block covering it — not a block with count 0, no block
// at all. Gremlins only compiles and runs mutants on lines the profile shows as
// covered, so a const line's mutant is never built, never run, and can never be
// killed by any test a caller could write. That, and not "the operator looks
// odd", is what makes a const-initializer ARITHMETIC_BASE survivor genuinely
// unkillable and therefore acceptable.
func TestConstInitializerHasNoCoverageBlock(t *testing.T) {
	constLine := ceilingSourceLine(t, "const Greeting =")
	blocks := ceilingCoverage(t)

	if _, inBlock := coveredCount(blocks, constLine); inBlock {
		t.Fatalf("const initializer line %d sits inside a coverage block — if go-cover ever instruments consts, this acceptance category must be re-derived", constLine)
	}

	got := mutantsOn(ceilingReport(t), constLine)
	if len(got) == 0 {
		t.Fatalf("recorded report holds no mutants on the const line %d", constLine)
	}
	sawArithmetic := false
	for _, m := range got {
		if m.Type == "ARITHMETIC_BASE" {
			sawArithmetic = true
		}
		if m.Status != "NOT COVERED" {
			t.Errorf("const-line mutant %s recorded as %q, want NOT COVERED", m.Type, m.Status)
		}
	}
	if !sawArithmetic {
		t.Errorf("no ARITHMETIC_BASE mutant on the const line — the category's subject is not present in the evidence")
	}

	// And the mutation itself is not even well-formed Go, so there is nothing
	// a test could observe if it ever were compiled.
	assertStringMinusDoesNotCompile(t, `const Greeting = "hello" - ", "`)
}

// TestStringConcatArithmeticMutantIsNoOp is the string-concat half, and it
// cuts the OTHER way — which is why it is stated here rather than assumed.
//
// The `+` → `-` swap on all-string operands is not a behavioural mutation: it
// does not compile. So it can never be a live mutant that tests failed to
// catch. But gremlins only builds mutants on COVERED lines: cover the line and
// the swap is compiled, fails, and is reported KILLED — which the fixture's
// concat line shows. A string-concat ARITHMETIC_BASE SURVIVOR is therefore
// always a coverage gap, killable by covering the line, and never an
// inapplicable-operator gap.
//
// This is the evidence that split bogus_arithmetic_class: the const half is
// accepted, the string-concat half is killed.
func TestStringConcatArithmeticMutantIsNoOp(t *testing.T) {
	concatLine := ceilingSourceLine(t, `return "name: " + name`)
	blocks := ceilingCoverage(t)

	count, inBlock := coveredCount(blocks, concatLine)
	if !inBlock || count < 1 {
		t.Fatalf("concat line %d is not covered (inBlock=%v count=%d) — the fixture must exercise it for this half of the proof", concatLine, inBlock, count)
	}

	got := mutantsOn(ceilingReport(t), concatLine)
	if len(got) == 0 {
		t.Fatalf("recorded report holds no mutants on the concat line %d", concatLine)
	}
	arithmetic := 0
	for _, m := range got {
		if m.Type != "ARITHMETIC_BASE" {
			continue
		}
		arithmetic++
		if m.Status != "KILLED" {
			t.Errorf("covered concat-line ARITHMETIC_BASE recorded as %q, want KILLED — if a COVERED string concatenation can survive, the split between the two halves is wrong",
				m.Status)
		}
	}
	if arithmetic == 0 {
		t.Fatal("no ARITHMETIC_BASE mutant on the concat line — the evidence the category cites is not present")
	}

	assertStringMinusDoesNotCompile(t, `func Describe(name string) string { return "name: " - name }`)
}

// assertStringMinusDoesNotCompile builds decl in a throwaway module and
// requires the type-checker to reject it. This is the mechanism behind both
// halves: gremlins' ARITHMETIC_BASE swaps `+` for `-`, and `-` is not defined
// on strings, so the mutant is never a behaviour a test could distinguish.
func assertStringMinusDoesNotCompile(t *testing.T, decl string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module mutantproof\n\ngo 1.22\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	src := "package mutantproof\n\n" + decl + "\n"
	if err := os.WriteFile(filepath.Join(dir, "m.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command("go", "build", "./...")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("the mutated source COMPILED — the mutant is a real behavioural change after all:\n%s", src)
	}
	if !strings.Contains(string(out), "operator - not defined") {
		t.Errorf("build failed for the wrong reason (want an operator-not-defined error):\n%s", out)
	}
}
