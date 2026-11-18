package facts

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/architecture"
	"github.com/Rivil/dross/internal/codex"
)

func sym(name string) Citation { return Citation{Kind: KindSymbol, Raw: name, Symbol: name} }
func line(p string, n int, s string) Citation {
	return Citation{Kind: KindLine, Raw: p, Path: p, Line: n, Symbol: s}
}

func mustSymbol(t *testing.T, r *Resolver, name string) architecture.Status {
	t.Helper()
	res, err := r.Symbol(sym(name))
	if err != nil {
		t.Fatalf("Symbol(%s): %v", name, err)
	}
	return res.Status
}

func mustLine(t *testing.T, r *Resolver, c Citation) Resolved {
	t.Helper()
	res, err := r.Line(c)
	if err != nil {
		t.Fatalf("Line(%+v): %v", c, err)
	}
	return res
}

// TestStrictMatchIsNotTheArchitectureMatcher: the classifier carries no
// fallback of its own — `pkg.Bar` against a bare `Bar` in package other is
// Unresolved under StrictMatch and OK under the architecture matcher.
func TestStrictMatchIsNotTheArchitectureMatcher(t *testing.T) {
	syms := []codex.Symbol{{Name: "Bar", Package: "other", Line: 3}}
	link := architecture.SymbolLink{Symbol: "pkg.Bar", File: "x.go", Line: 3}
	if got := architecture.Classify(link, syms, StrictMatch).Status; got != architecture.StatusUnresolved {
		t.Errorf("Classify(pkg.Bar, StrictMatch) = %v, want unresolved", got)
	}
	if got := architecture.Classify(link, syms, architecture.MatchSymbols).Status; got != architecture.StatusOK {
		t.Errorf("Classify(pkg.Bar, MatchSymbols) = %v, want ok", got)
	}
}

// TestStrictMatchStaysOutOfArchitecture (c-8): a wrong-package bullet still
// resolves through the ARCHITECTURE.md check, while facts reports it.
func TestStrictMatchStaysOutOfArchitecture(t *testing.T) {
	root := t.TempDir()
	write(t, root, "codex/codex.go", "package codex\n\nfunc Index() {}\n")
	doc := "### F\n\nx\n\n- `wrongpkg.Index` — `codex/codex.go:3`\n\n_x_\n"
	res := architecture.ResolveAllIn(doc, root)
	if len(res) != 1 || res[0].Status != architecture.StatusOK {
		t.Fatalf("ResolveAllIn = %+v, want one ok link", res)
	}
	r := NewResolver(root)
	if got := mustSymbol(t, r, "wrongpkg.Index"); got != architecture.StatusUnresolved {
		t.Errorf("facts Symbol(wrongpkg.Index) = %v, want unresolved", got)
	}
	if got := mustLine(t, r, line("codex/codex.go", 3, "wrongpkg.Index")).Status; got != architecture.StatusUnresolved {
		t.Errorf("facts Line(wrongpkg.Index — codex/codex.go:3) = %v, want unresolved", got)
	}
	if got := mustSymbol(t, r, "codex.Index"); got != architecture.StatusOK {
		t.Errorf("facts Symbol(codex.Index) = %v, want ok", got)
	}
}

// TestStrictMatchNoLastComponentFallback: every qualifier must match.
func TestStrictMatchNoLastComponentFallback(t *testing.T) {
	root := t.TempDir()
	write(t, root, "internal/phase/phase.go", "package phase\n\ntype Spec struct{}\n")
	write(t, root, "internal/other/o.go", "package other\n\ntype T struct{}\n\nfunc (T) String() string { return \"\" }\n")
	r := NewResolver(root)
	for name, want := range map[string]architecture.Status{
		"quoted.String":  architecture.StatusUnresolved,
		"phase.Spec":     architecture.StatusOK,
		"cmd.Spec":       architecture.StatusUnresolved,
		"Spec":           architecture.StatusOK,
		"T.String":       architecture.StatusOK,
		"other.T":        architecture.StatusOK,
		"other.T.String": architecture.StatusOK,
		"os.Stat":        architecture.StatusUnresolved, // external packages are not exempt
	} {
		if got := mustSymbol(t, r, name); got != want {
			t.Errorf("Symbol(%s) = %v, want %v", name, got, want)
		}
	}
}

// TestResolverAmbiguityMovesAndSkips: duplicates are ambiguous, a moved
// symbol names its new line and turns gone when deleted, and a line citation
// into an unindexed language is skipped.
func TestResolverAmbiguityMovesAndSkips(t *testing.T) {
	root := t.TempDir()
	write(t, root, "a/changes.go", "package a\n\ntype Changes struct{}\n\nfunc (Changes) Record() {}\n")
	write(t, root, "b/changes.go", "package b\n\ntype Changes struct{}\n\nfunc (Changes) Record() {}\n")
	links := "package architecture\n\n// one\n// two\n// three\nfunc Resolve() {}\n" // Resolve at 6
	write(t, root, "internal/architecture/links.go", links)
	write(t, root, "tool.py", "def f():\n    pass\n")

	r := NewResolver(root)
	if got := mustSymbol(t, r, "Changes.Record"); got != architecture.StatusAmbiguous {
		t.Errorf("Symbol(Changes.Record) in two files = %v, want ambiguous", got)
	}
	if got := mustLine(t, r, line("changes.go", 5, "Changes.Record")).Status; got != architecture.StatusAmbiguous {
		t.Errorf("bare changes.go shared by two files = %v, want ambiguous", got)
	}
	moved := mustLine(t, r, line("internal/architecture/links.go", 3, "Resolve"))
	if moved.Status != architecture.StatusMoved || moved.NewLine != 6 {
		t.Errorf("Resolve cited :3, lives :6 → %+v, want moved to 6", moved)
	}
	if got := mustLine(t, r, line("links.go", 3, "architecture.Resolve")); got.Status != architecture.StatusMoved || got.NewLine != 6 {
		t.Errorf("bare unique links.go → %+v, want moved to 6", got)
	}
	if got := mustLine(t, r, line("tool.py", 1, "f")).Status; got != architecture.StatusSkipped {
		t.Errorf("line into .py = %v, want skipped", got)
	}
	if got := mustLine(t, r, line("nosuch.go", 1, "X")).Status; got != architecture.StatusUnresolved {
		t.Errorf("line into a missing file = %v, want unresolved", got)
	}
	if got := mustLine(t, r, line("tool.py", 1, "")).Status; got != architecture.StatusSkipped {
		t.Errorf("unpaired line = %v, want skipped", got)
	}

	write(t, root, "internal/architecture/links.go", "package architecture\n")
	gone := mustLine(t, NewResolver(root), line("internal/architecture/links.go", 3, "Resolve"))
	if gone.Status != architecture.StatusUnresolved {
		t.Errorf("deleted Resolve = %+v, want unresolved", gone)
	}
}

// stubCanIndex makes every file with ext unindexable, as if its tool were
// missing, for the duration of the test.
func stubCanIndex(t *testing.T, ext string) {
	t.Helper()
	prev := canIndex
	t.Cleanup(func() { canIndex = prev })
	canIndex = func(f string) bool {
		if strings.EqualFold(filepath.Ext(f), ext) {
			return false
		}
		return prev(f)
	}
}

// TestDegradedCensus: an unindexable source file outside testdata/fixtures
// degrades the index (unmatched symbols are skipped); the same file only
// under testdata/ or fixtures/ does not.
func TestDegradedCensus(t *testing.T) {
	stubCanIndex(t, ".ts")
	base := func(t *testing.T) string {
		root := t.TempDir()
		write(t, root, "main.go", "package main\n\nfunc Run() {}\n")
		return root
	}
	cases := []struct {
		name, file string
		degraded   bool
	}{
		{"ts under src", "src/app.ts", true},
		{"ts under testdata", "internal/x/testdata/app.ts", false},
		{"ts under fixtures", "fixtures/web/app.ts", false},
		{"py under src", "src/tool.py", true},
		{"py under fixtures", "fixtures/tool.py", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := base(t)
			write(t, root, tc.file, "x\n")
			r := NewResolver(root)
			got := mustSymbol(t, r, "app.Missing")
			deg, why := r.Degraded()
			if deg != tc.degraded {
				t.Errorf("Degraded = %v (%s), want %v", deg, why, tc.degraded)
			}
			want := architecture.StatusUnresolved
			if tc.degraded {
				want = architecture.StatusSkipped
				if !strings.Contains(why, tc.file) {
					t.Errorf("degraded reason %q does not name %s", why, tc.file)
				}
			}
			if got != want {
				t.Errorf("unmatched symbol = %v, want %v", got, want)
			}
			if s := mustSymbol(t, r, "main.Run"); s != architecture.StatusOK {
				t.Errorf("a matched symbol still resolves when degraded: got %v", s)
			}
		})
	}
}

// TestIndexIsLazyAndTolerant: no symbol or paired-line work builds nothing,
// and an unparsable testdata .go file does not stop the rest indexing.
func TestIndexIsLazyAndTolerant(t *testing.T) {
	prev := buildIndex
	t.Cleanup(func() { buildIndex = prev })
	builds := 0
	buildIndex = func(root string, files []string) *treeIndex {
		builds++
		return prev(root, files)
	}

	root := t.TempDir()
	write(t, root, "internal/x/testdata/broken.go", "package broken\n\nfunc (\n")
	write(t, root, "internal/x/x.go", "package x\n\nfunc Good() {}\n")

	r := NewResolver(root)
	if _, err := r.Files(); err != nil {
		t.Fatal(err)
	}
	if res := mustLine(t, r, line("internal/x/x.go", 3, "")); res.Status != architecture.StatusSkipped {
		t.Errorf("unpaired line = %v", res.Status)
	}
	if deg, _ := r.Degraded(); deg {
		t.Error("an unbuilt index reported degraded")
	}
	if builds != 0 {
		t.Fatalf("index built %d times with no symbol or paired-line citation, want 0", builds)
	}

	if got := mustSymbol(t, r, "x.Good"); got != architecture.StatusOK {
		t.Errorf("x.Good = %v, want ok despite the broken testdata file", got)
	}
	if deg, why := r.Degraded(); deg {
		t.Errorf("a broken testdata file degraded the index: %s", why)
	}
	mustSymbol(t, r, "x.Other")
	if builds != 1 {
		t.Errorf("index built %d times, want exactly 1", builds)
	}
}

// TestStrictMatchOnRealTree: against this repository, real symbols resolve
// and the motivating phantom does not.
func TestStrictMatchOnRealTree(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Skipf("repo root not found at %s: %v", root, err)
	}
	r := NewResolver(root)
	for _, name := range []string{"codex.Index", "architecture.Classify", "facts.StrictMatch"} {
		if got := mustSymbol(t, r, name); got != architecture.StatusOK {
			t.Errorf("Symbol(%s) = %v on the real tree, want ok", name, got)
		}
	}
	if got := mustSymbol(t, r, "quoted.String"); got == architecture.StatusOK {
		t.Errorf("Symbol(quoted.String) resolved on the real tree; it must not")
	}
}

// TestNestedWorktreeChangesNothing: a nested repo copy of the tree (a
// worktree under .claude/worktrees) never enters the index, so nothing turns
// ambiguous.
func TestNestedWorktreeChangesNothing(t *testing.T) {
	root := t.TempDir()
	git(t, root, "init", "-q")
	src := "package a\n\ntype Changes struct{}\n\nfunc (Changes) Record() {}\n"
	write(t, root, "a/changes.go", src)
	git(t, root, "add", ".")
	git(t, root, "commit", "-q", "-m", "base")

	classify := func() []architecture.Status {
		r := NewResolver(root)
		return []architecture.Status{
			mustSymbol(t, r, "a.Changes.Record"),
			mustSymbol(t, r, "Changes"),
			mustLine(t, r, line("changes.go", 5, "Changes.Record")).Status,
		}
	}
	before := classify()

	nested := filepath.Join(root, ".claude", "worktrees", "w")
	write(t, nested, "a/changes.go", src)
	git(t, nested, "init", "-q")

	after := classify()
	for i := range before {
		if before[i] != after[i] || before[i] != architecture.StatusOK {
			t.Errorf("classification %d: before %v, after %v — want ok both times", i, before[i], after[i])
		}
	}
}

// TestResolverRejectsWrongKind: a citation handed to the wrong method is an
// error, never a silent skip.
func TestResolverRejectsWrongKind(t *testing.T) {
	r := NewResolver(t.TempDir())
	path := Citation{Kind: KindPath, Raw: "a.go", Path: "a.go"}
	for _, c := range []Citation{path, sym("pkg.X")} {
		if _, err := r.Line(c); err == nil {
			t.Errorf("Line(%s citation) returned no error", c.Kind)
		}
	}
	for _, c := range []Citation{path, line("a.go", 1, "X")} {
		if _, err := r.Symbol(c); err == nil {
			t.Errorf("Symbol(%s citation) returned no error", c.Kind)
		}
	}
}

// TestBrokenProjectFileDegrades: an unparsable file outside testdata/fixtures
// may hold the cited symbol, so an unmatched symbol is skipped, not gone.
func TestBrokenProjectFileDegrades(t *testing.T) {
	root := t.TempDir()
	write(t, root, "internal/x/broken.go", "package x\n\nfunc (\n")
	write(t, root, "internal/x/x.go", "package x\n\nfunc Good() {}\n")
	r := NewResolver(root)
	if got := mustSymbol(t, r, "x.Lost"); got != architecture.StatusSkipped {
		t.Errorf("x.Lost with a broken project file = %v, want skipped", got)
	}
	deg, why := r.Degraded()
	if !deg || !strings.Contains(why, "internal/x/broken.go") {
		t.Errorf("Degraded = %v (%q), want true naming the broken file", deg, why)
	}
}
