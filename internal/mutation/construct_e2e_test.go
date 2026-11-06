package mutation

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
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

// requireSvelte is requireE2E for a test that needs the real compiler. A
// node_modules older than the svelte pin has none, and that is a stale install
// to name, not a resolver failure to report.
func requireSvelte(t *testing.T) string {
	t.Helper()
	root := requireE2E(t)
	if _, err := os.Stat(filepath.Join(root, "node_modules", "svelte", "package.json")); err != nil {
		reason := "the fixture's node_modules has no svelte — run `npm ci --ignore-scripts` in " + tsFixtureDir
		if os.Getenv("DROSS_REQUIRE_E2E") != "" {
			t.Fatalf("DROSS_REQUIRE_E2E is set but the end-to-end run cannot proceed: %s", reason)
		}
		t.Skip(reason)
	}
	return root
}

// svelteProbe resolves src as a .svelte inside the fixture tree, the one
// place svelte/compiler resolves from.
func svelteProbe(t *testing.T, root, src string) ([]Construct, error) {
	t.Helper()
	probe := filepath.Join(root, "src", "probe-svelte")
	if err := os.MkdirAll(probe, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(probe) })
	if err := os.WriteFile(filepath.Join(probe, "Probe.svelte"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	return (&Stryker{ProjectRoot: root}).Constructs("src/probe-svelte/Probe.svelte")
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

// TestRealParserSpansMatchTheExpansionForSvelte pins the .svelte spans against
// the real compiler and the real Babel: the JavaScript that produces them has no
// other test. The name rides the unanchored -run selector of ci.yml's e2e job,
// the one job where DROSS_REQUIRE_E2E turns a skip into a failure.
func TestRealParserSpansMatchTheExpansionForSvelte(t *testing.T) {
	root := requireSvelte(t)

	t.Run("a function on line 40 reports line 40", func(t *testing.T) {
		src := "<script lang=\"ts\">\n" + strings.Repeat("  // pad\n", 38) +
			"  function late(): number {\n    return 1;\n  }\n</script>\n"
		got, err := svelteProbe(t, root, src)
		if err != nil {
			t.Fatal(err)
		}
		want := []Construct{{Start: 40, End: 42, Kind: "FunctionDeclaration", Name: "late"}}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("constructs = %v, want %v", got, want)
		}
	})

	t.Run("both script blocks resolve, sorted and disjoint", func(t *testing.T) {
		widget := filepath.Join(root, "src", "Widget.svelte")
		got, err := (&Stryker{ProjectRoot: root}).Constructs("src/Widget.svelte")
		if err != nil {
			t.Fatal(err)
		}
		// The needle ties each literal span to the fixture's real text, so an
		// edit to Widget.svelte fails here rather than passing on stale lines.
		rows := []struct {
			needle string
			want   Construct
		}{
			{"export const step", Construct{3, 3, "VariableDeclaration", "step"}},
			{"let count: number", Construct{7, 7, "VariableDeclaration", "count"}},
			{"function increment", Construct{9, 12, "FunctionDeclaration", "increment"}},
			{"function reset", Construct{14, 18, "FunctionDeclaration", "reset"}},
			{"<button onclick={() =>", Construct{21, 23, "Element", "button"}},
			{"{#if count", Construct{25, 29, "IfBlock", ""}},
		}
		var want []Construct
		for _, r := range rows {
			if line := fixtureLine(t, widget, r.needle); line != r.want.Start {
				t.Errorf("%q is on line %d of the fixture, the table says %d", r.needle, line, r.want.Start)
			}
			want = append(want, r.want)
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("constructs = %v, want %v", got, want)
		}
	})

	t.Run("a script inside a comment is no construct", func(t *testing.T) {
		got, err := svelteProbe(t, root, "<!-- <script>x</script> -->\n<script>\n  let y = 1;\n</script>\n")
		if err != nil {
			t.Fatal(err)
		}
		want := []Construct{{Start: 3, End: 3, Kind: "VariableDeclaration", Name: "y"}}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("constructs = %v, want %v", got, want)
		}
	})

	t.Run("script and markup on one line merge", func(t *testing.T) {
		got, err := svelteProbe(t, root, "<script>let x = 1;</script><div>{x}</div>\n")
		if err != nil {
			t.Fatal(err)
		}
		want := []Construct{{Start: 1, End: 1, Kind: mergedKind}}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("constructs = %v, want %v", got, want)
		}
	})

	t.Run("a multi-line block is one construct and style is none", func(t *testing.T) {
		got, err := svelteProbe(t, root, "{#if a}\n  <p>{a}</p>\n{:else}\n  <p>b</p>\n{/if}\n\n<style>\n  p { color: red; }\n</style>\n")
		if err != nil {
			t.Fatal(err)
		}
		want := []Construct{{Start: 1, End: 5, Kind: "IfBlock"}}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("constructs = %v, want %v", got, want)
		}
	})

	t.Run("a style the compiler cannot read is still none", func(t *testing.T) {
		got, err := svelteProbe(t, root, "<style lang=\"scss\">\n  $c: red;\n  p { color: $c; // note\n  }\n</style>\n")
		if err != nil {
			t.Fatal(err)
		}
		if got == nil || len(got) != 0 {
			t.Errorf("constructs = %#v, want an empty non-nil slice", got)
		}
	})
}

// TestUnresolvableParserIsUnavailableForSvelte: the real causes behind
// ast-unavailable on a .svelte, each named by dross with no compiler text in
// the detail. A position is the file's own, inside a script block too. The name
// rides ci.yml's selector, as above.
func TestUnresolvableParserIsUnavailableForSvelte(t *testing.T) {
	root := requireSvelte(t)

	t.Run("compiler unresolvable from the workdir", func(t *testing.T) {
		bare := t.TempDir()
		if err := os.WriteFile(filepath.Join(bare, "App.svelte"), []byte("<p>a</p>\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		_, err := (&Stryker{ProjectRoot: bare}).Constructs("App.svelte")
		want := "cannot resolve svelte/compiler from " + bare
		if !errors.Is(err, ErrASTUnavailable) || !strings.Contains(err.Error(), want) {
			t.Fatalf("err = %v, want ErrASTUnavailable with %q", err, want)
		}
	})

	t.Run("malformed markup", func(t *testing.T) {
		_, err := svelteProbe(t, root, "<div>\n  {#if x}\n</div>\n")
		if !errors.Is(err, ErrASTUnavailable) || !strings.Contains(err.Error(), "parse error at 3:0") {
			t.Fatalf("err = %v, want ErrASTUnavailable with the position 3:0", err)
		}
		if strings.Contains(err.Error(), "attempted to close") {
			t.Errorf("the compiler's own message leaked into the persisted detail: %v", err)
		}
	})

	t.Run("malformed script body", func(t *testing.T) {
		_, err := svelteProbe(t, root, "<p>a</p>\n\n<script lang=\"ts\">\n  let = ;\n</script>\n")
		if !errors.Is(err, ErrASTUnavailable) || !strings.Contains(err.Error(), "parse error at 4:") {
			t.Fatalf("err = %v, want ErrASTUnavailable with a position on the file's line 4", err)
		}
	})
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
