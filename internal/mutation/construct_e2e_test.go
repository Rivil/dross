package mutation

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The construct-range proof, against the real parser and the real tool.
//
// Gated like the Stryker e2e run: skipped with a reason that names the
// missing piece when node or the fixture's node_modules are absent, and
// fatal under DROSS_REQUIRE_E2E so the CI leg cannot silently stop existing.

func requireE2E(t *testing.T) string {
	t.Helper()
	if reason := e2eSkipReason(); reason != "" {
		if os.Getenv("DROSS_REQUIRE_E2E") != "" {
			t.Fatalf("DROSS_REQUIRE_E2E is set but the end-to-end run cannot proceed: %s", reason)
		}
		t.Skip(reason)
	}
	root, err := filepath.Abs(tsFixtureDir)
	if err != nil {
		t.Fatal(err)
	}
	return root
}

// fixtureLine returns the 1-based line of the first line containing needle.
func fixtureLine(t *testing.T, path, needle string) int {
	t.Helper()
	body := readFileString(t, path)
	for i, line := range strings.Split(body, "\n") {
		if strings.Contains(line, needle) {
			return i + 1
		}
	}
	t.Fatalf("%s has no line containing %q", path, needle)
	return 0
}

// mutantsIn counts every mutant Stryker generated for file, whatever its
// outcome: the claim under test is about GENERATION, not the score.
func mutantsIn(report *Report, file string) int {
	for path, st := range report.Files {
		if strings.HasSuffix(filepath.ToSlash(path), file) {
			return st.Killed + st.Survived + st.Timeout + st.Errors + st.NotCovered
		}
	}
	return 0
}

// The retired heuristic's window, hard-coded here as the thing being
// retired: the constant is gone from source, and this is the one place it
// may still be spelled — as a measurement of what it lost.
const retiredPad = 25

// TestConstructRangeRecoversTheDeepMutant is the phase's acceptance: an edit
// deep inside a long function, ranged to its enclosing construct, generates
// mutants the ±25 window could not — the function body's own block mutant
// among them — and the construct span does not leak into the next function.
func TestConstructRangeRecoversTheDeepMutant(t *testing.T) {
	root := requireE2E(t)
	const file = "src/long.ts"
	longTS := filepath.Join(root, file)
	bigLine := fixtureLine(t, longTS, "export function big(")
	smallLine := fixtureLine(t, longTS, "export function small<")
	edited := fixtureLine(t, longTS, "dross:deep-edit")
	if edited-bigLine <= retiredPad {
		t.Fatalf("the fixture's deep edit (line %d) is within %d lines of the function start (line %d) — it would not have been lost", edited, retiredPad, bigLine)
	}

	s := &Stryker{ProjectRoot: root}
	cs, err := s.Constructs(file)
	if err != nil {
		t.Fatalf("Constructs: %v", err)
	}
	var big *Construct
	for i := range cs {
		if cs[i].Name == "big" {
			big = &cs[i]
		}
	}
	if big == nil {
		t.Fatalf("no construct named big among %v", cs)
	}
	if big.Start != bigLine {
		t.Errorf("big starts at line %d, the fixture's `export function big(` is line %d", big.Start, bigLine)
	}
	if big.End >= smallLine {
		t.Errorf("big's span ends at %d, at or past small's first line %d — the range leaked", big.End, smallLine)
	}
	if big.Label() != "FunctionDeclaration big" {
		t.Errorf("label = %q", big.Label())
	}

	run := func(r Range) int {
		t.Helper()
		report, err := s.RunRanges([]string{file}, map[string][]Range{file: {r}})
		if err != nil {
			t.Fatalf("RunRanges(%v): %v", r, err)
		}
		n := mutantsIn(report, file)
		t.Logf("range %d-%d: %d mutants in %s", r.Start, r.End, n, file)
		return n
	}
	nLine := run(Range{Start: edited, End: edited})
	padStart := edited - retiredPad
	if padStart < 1 {
		padStart = 1
	}
	nPad := run(Range{Start: padStart, End: edited + retiredPad})
	nConstruct := run(Range{Start: big.Start, End: big.End})

	if nConstruct <= nPad {
		t.Errorf("the construct span generated %d mutants, the retired ±%d window %d — nothing the window lost was recovered", nConstruct, retiredPad, nPad)
	}
	if nConstruct <= nLine {
		t.Errorf("the construct span generated %d mutants, the bare line %d", nConstruct, nLine)
	}
}

// TestBabelParsesGenericsAndJSX pins the per-extension plugin choice against
// the real parser: a .ts with angle-bracket generics needs no jsx plugin (and
// must not get one), a .tsx with JSX needs it, and JSX in a .ts is a parse
// error with a position rather than a silent acceptance.
func TestBabelParsesGenericsAndJSX(t *testing.T) {
	root := requireE2E(t)
	probe := filepath.Join(root, "src", "probe")
	if err := os.MkdirAll(probe, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(probe) })
	write := func(name, src string) string {
		t.Helper()
		if err := os.WriteFile(filepath.Join(probe, name), []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
		return "src/probe/" + name
	}
	s := &Stryker{ProjectRoot: root}

	generics := write("generics.ts", "export function first<T>(xs: T[]): T {\n  return <T>xs[0];\n}\n")
	cs, err := s.Constructs(generics)
	if err != nil || len(cs) != 1 || cs[0].Label() != "FunctionDeclaration first" {
		t.Errorf("generics .ts: constructs = %v, err = %v; want one FunctionDeclaration first", cs, err)
	}

	jsx := write("view.tsx", "export function View() {\n  return <div>hi</div>;\n}\n")
	cs, err = s.Constructs(jsx)
	if err != nil || len(cs) != 1 || cs[0].Label() != "FunctionDeclaration View" {
		t.Errorf("jsx .tsx: constructs = %v, err = %v; want one FunctionDeclaration View", cs, err)
	}

	jsxInTS := write("bad.ts", "export function View() {\n  return <div>hi</div>;\n}\n")
	_, err = s.Constructs(jsxInTS)
	if !errors.Is(err, ErrASTUnavailable) || !strings.Contains(err.Error(), "parse error at 2:") {
		t.Errorf("jsx in .ts: err = %v, want ErrASTUnavailable with a position on line 2", err)
	}
}

// TestRealParserSpansMatchTheExpansion pins the layout the Go expansion
// relies on, against the real parser: constructs are sorted and disjoint,
// the blank line between big and small belongs to neither (so a hunk there
// stays a bare hunk), and each function is exactly one construct (so two
// hunks inside big map to one range, and a hunk straddling big's last line
// and small's first maps to both).
func TestRealParserSpansMatchTheExpansion(t *testing.T) {
	root := requireE2E(t)
	longTS := filepath.Join(root, "src", "long.ts")
	s := &Stryker{ProjectRoot: root}
	cs, err := s.Constructs("src/long.ts")
	if err != nil {
		t.Fatal(err)
	}
	if len(cs) != 2 {
		t.Fatalf("want exactly two constructs (big, small), got %v", cs)
	}
	big, small := cs[0], cs[1]
	if big.Label() != "FunctionDeclaration big" || small.Label() != "FunctionDeclaration small" {
		t.Fatalf("labels = %q, %q", big.Label(), small.Label())
	}
	if big.End >= small.Start {
		t.Errorf("constructs overlap or touch: big %d-%d, small %d-%d", big.Start, big.End, small.Start, small.End)
	}
	blank := small.Start - 1
	if line := strings.Split(readFileString(t, longTS), "\n")[blank-1]; strings.TrimSpace(line) != "" {
		t.Errorf("line %d between the functions is not blank: %q", blank, line)
	}
	if big.End >= blank {
		t.Errorf("big's span %d-%d covers the blank line %d — a hunk there would not stay a bare hunk", big.Start, big.End, blank)
	}
	if fixtureLine(t, longTS, "export function small<") != small.Start {
		t.Errorf("small's span starts at %d, not at its export line", small.Start)
	}
}

// TestUnresolvableParserIsUnavailable: the real cause behind
// ast-unavailable on a machine with node but no Stryker — the parser cannot
// be resolved from the workdir, and the detail says which package.
func TestUnresolvableParserIsUnavailable(t *testing.T) {
	requireE2E(t)
	bare := t.TempDir()
	if err := os.WriteFile(filepath.Join(bare, "a.ts"), []byte("export const x = 1;\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := &Stryker{ProjectRoot: bare}
	_, err := s.Constructs("a.ts")
	if !errors.Is(err, ErrASTUnavailable) || !strings.Contains(err.Error(), "@babel/parser") {
		t.Fatalf("err = %v, want ErrASTUnavailable naming @babel/parser", err)
	}
}
