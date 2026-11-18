package codex

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// makeFixture builds a tiny project with a .git dir so projectRoot
// resolves to the temp dir.
func makeFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

func writeFile(t *testing.T, dir, rel, body string) string {
	t.Helper()
	full := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return full
}

func TestGoIndexerExtractsAllKinds(t *testing.T) {
	dir := makeFixture(t)
	src := `package thing

const Pi = 3.14
const (
	MaxRetries = 3
	MinDelay   = 10
)

var Counter int

type User struct {
	Name string
}

type Stringer interface {
	String() string
}

func New(name string) *User {
	return &User{Name: name}
}

func (u *User) Greet() string {
	return "hi " + u.Name
}

func (u User) Quiet() {}
`
	path := writeFile(t, dir, "thing.go", src)

	g := &GoIndexer{}
	if !g.Supports(path) {
		t.Fatal("Supports should return true for .go")
	}
	syms, err := g.Symbols(path)
	if err != nil {
		t.Fatal(err)
	}

	got := map[string]string{} // name → kind
	for _, s := range syms {
		got[s.Name] = s.Kind
	}
	want := map[string]string{
		"Pi":         "const",
		"MaxRetries": "const",
		"MinDelay":   "const",
		"Counter":    "var",
		"User":       "type",
		"Stringer":   "type",
		"New":        "function",
		"User.Greet": "method",
		"User.Quiet": "method",
	}
	for name, kind := range want {
		if got[name] != kind {
			t.Errorf("symbol %q: got kind %q, want %q (full set: %v)", name, got[name], kind, got)
		}
	}
}

func TestGoIndexerTagsTestFiles(t *testing.T) {
	dir := makeFixture(t)
	src := `package thing

import "testing"

func TestSomething(t *testing.T) {}
`
	path := writeFile(t, dir, "thing_test.go", src)
	syms, err := (&GoIndexer{}).Symbols(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(syms) != 1 || syms[0].Name != "TestSomething" {
		t.Fatalf("expected one TestSomething symbol, got %+v", syms)
	}
	if !strings.HasSuffix(syms[0].Kind, "(test)") {
		t.Errorf("test-file symbol should be tagged: %q", syms[0].Kind)
	}
}

func TestGoIndexerReturnsParseErrorForGarbage(t *testing.T) {
	dir := makeFixture(t)
	path := writeFile(t, dir, "broken.go", "this is not go")
	if _, err := (&GoIndexer{}).Symbols(path); err == nil {
		t.Error("expected parse error on non-Go input")
	}
}

func TestIndexEnrichesWithSiblingsAndRefs(t *testing.T) {
	dir := makeFixture(t)
	target := writeFile(t, dir, "core/foo.go", `package core

func DoTheThing() {}
`)
	writeFile(t, dir, "core/sibling.go", `package core

func helper() {}
`)
	writeFile(t, dir, "callers/uses_foo.go", `package callers

import _ "x/core"

func use() {
	// imagine we call DoTheThing here
	_ = "DoTheThing"
}
`)

	res, err := Index([]string{target})
	if err != nil {
		t.Fatal(err)
	}

	// Symbol from target file
	foundDoTheThing := false
	for _, s := range res.Symbols {
		if s.Name == "DoTheThing" && s.Kind == "function" {
			foundDoTheThing = true
		}
	}
	if !foundDoTheThing {
		t.Errorf("DoTheThing not in symbols: %+v", res.Symbols)
	}

	// Sibling shows up
	sawSibling := false
	for _, s := range res.Siblings {
		if strings.HasSuffix(s, "core/sibling.go") {
			sawSibling = true
		}
	}
	if !sawSibling {
		t.Errorf("sibling.go missing from siblings: %v", res.Siblings)
	}

	// Cross-file ref to DoTheThing
	sawCaller := false
	for _, c := range res.Callers {
		if c.Name == "DoTheThing" && strings.HasSuffix(c.File, "callers/uses_foo.go") {
			sawCaller = true
		}
	}
	if !sawCaller {
		t.Errorf("expected ref to DoTheThing in callers/uses_foo.go: %+v", res.Callers)
	}
}

func TestIndexEmptyTargets(t *testing.T) {
	res, err := Index(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Symbols)+len(res.Siblings)+len(res.RecentLog)+len(res.Callers) != 0 {
		t.Errorf("empty input should yield empty result: %+v", res)
	}
}

func TestIndexUnsupportedLanguageStillReturnsSiblings(t *testing.T) {
	dir := makeFixture(t)
	target := writeFile(t, dir, "site/index.html", "<html></html>")
	writeFile(t, dir, "site/style.css", "body { color: red }")

	res, err := Index([]string{target})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Symbols) != 0 {
		t.Errorf("HTML has no Go indexer; symbols should be empty: %+v", res.Symbols)
	}
	sawSibling := false
	for _, s := range res.Siblings {
		if strings.HasSuffix(s, "site/style.css") {
			sawSibling = true
		}
	}
	if !sawSibling {
		t.Errorf("siblings should still populate for unsupported langs: %v", res.Siblings)
	}
}

func TestIndexReportsParseErrorOnTarget(t *testing.T) {
	dir := makeFixture(t)
	target := writeFile(t, dir, "broken.go", "this is not go")
	res, err := Index([]string{target})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Errors) == 0 {
		t.Errorf("expected parse error to be recorded on Result.Errors: %+v", res)
	}
}

func TestRecentLogSurfacesCommits(t *testing.T) {
	dir := t.TempDir()
	// Real git repo so `git log` actually returns something.
	mustRun := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	mustRun("init", "-q", "-b", "main")
	mustRun("config", "user.email", "t@example.com")
	mustRun("config", "user.name", "Test")
	mustRun("config", "commit.gpgsign", "false")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustRun("add", "README.md")
	mustRun("commit", "-q", "-m", "feat: initial commit")

	lines, err := recentLog(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 1 || !strings.Contains(lines[0], "feat: initial commit") {
		t.Errorf("expected one log line with subject: %v", lines)
	}
}

func TestSupportsFile(t *testing.T) {
	// Languages codex has an indexer for (by extension) — true regardless of
	// whether the ast-grep binary is installed.
	for _, f := range []string{"a.go", "pkg/b.ts", "c.tsx", "d.svelte", "e.cs", "f.gd"} {
		if !SupportsFile(f) {
			t.Errorf("SupportsFile(%q) = false, want true", f)
		}
	}
	// No indexer for these → consumers should Skip, not flag as unresolved.
	for _, f := range []string{"README.md", "x.py", "y.txt", "noext", "z.json"} {
		if SupportsFile(f) {
			t.Errorf("SupportsFile(%q) = true, want false", f)
		}
	}
}

// TestIndexSymbolsSortStably covers Index's symbol comparator, which no other
// test reaches: sort.Slice never calls a comparator with fewer than two
// elements, and every existing Index test indexes a single file with a single
// symbol.
//
// Ordering is not cosmetic here — the Result is what a prompt reads to decide
// what to look at first, and an unstable order makes the same repo produce a
// different briefing on every run.
func TestIndexSymbolsSortStably(t *testing.T) {
	dir := makeFixture(t)
	// Two files, two symbols each: enough for both comparator branches — the
	// cross-file compare and the same-file line compare.
	bravo := writeFile(t, dir, "core/bravo.go", `package core

func BravoOne() {}

func BravoTwo() {}
`)
	alpha := writeFile(t, dir, "core/alpha.go", `package core

func AlphaOne() {}

func AlphaTwo() {}
`)

	res, err := Index([]string{bravo, alpha})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Symbols) < 4 {
		t.Fatalf("indexed %d symbols, want at least 4 — the fixture is not exercising the sort: %+v",
			len(res.Symbols), res.Symbols)
	}

	// Ascending by File, then by Line. Asserted as a total order over the
	// whole slice, so inverting either comparison shows up.
	for i := 1; i < len(res.Symbols); i++ {
		prev, cur := res.Symbols[i-1], res.Symbols[i]
		switch {
		case prev.File > cur.File:
			t.Errorf("symbols not sorted by file at %d: %q then %q", i, prev.File, cur.File)
		case prev.File == cur.File && prev.Line > cur.Line:
			t.Errorf("symbols not sorted by line within %s at %d: %d then %d", cur.File, i, prev.Line, cur.Line)
		}
	}

	// The premise the same-file branch needs: at least one adjacent pair
	// shares a file, and at least one pair does not. Without both, the test
	// would pass while exercising only half the comparator.
	var sameFile, crossFile int
	for i := 1; i < len(res.Symbols); i++ {
		if res.Symbols[i-1].File == res.Symbols[i].File {
			sameFile++
		} else {
			crossFile++
		}
	}
	if sameFile == 0 || crossFile == 0 {
		t.Fatalf("fixture exercises only one comparator branch (same-file=%d cross-file=%d)", sameFile, crossFile)
	}

	// And the first file really is the lexically smaller one, so the sort did
	// something rather than preserving input order by luck.
	if filepath.Base(res.Symbols[0].File) != "alpha.go" {
		t.Errorf("first symbol is in %q, want alpha.go — input order was bravo, alpha",
			filepath.Base(res.Symbols[0].File))
	}
}

// TestCanIndexReflectsToolAvailability pins CanIndex against SupportsFile:
// with ast-grep absent, an ast-grep language is still *supported* but cannot
// be *indexed*, while Go (stdlib parser) always can.
func TestCanIndexReflectsToolAvailability(t *testing.T) {
	prev := astGrepAvailableFn
	t.Cleanup(func() { astGrepAvailableFn = prev })

	astGrepAvailableFn = func() bool { return false }
	if CanIndex("a.ts") {
		t.Error("CanIndex(a.ts) = true with ast-grep absent, want false")
	}
	if !SupportsFile("a.ts") {
		t.Error("SupportsFile(a.ts) must stay true with ast-grep absent")
	}
	if !CanIndex("a.go") {
		t.Error("CanIndex(a.go) = false, want true (stdlib parser needs no tool)")
	}

	astGrepAvailableFn = func() bool { return true }
	if !CanIndex("a.ts") {
		t.Error("CanIndex(a.ts) = false with ast-grep present, want true")
	}
	for _, f := range []string{"x.py", "README.md", "noext"} {
		if CanIndex(f) {
			t.Errorf("CanIndex(%q) = true, want false (no indexer for the language)", f)
		}
	}
}

// TestSymbolsMatchesIndexWithoutGit pins Symbols to Index's symbol output for
// one Go file in a directory that is not a git repo: same symbols, same order,
// no error — the per-file entry point drops the enrichment, not the data.
func TestSymbolsMatchesIndexWithoutGit(t *testing.T) {
	dir := t.TempDir() // deliberately no .git
	path := writeFile(t, dir, "two.go", `package two

func B() {}

type T struct{}

func (T) M() {}

func A() {}
`)
	got, err := Symbols(path)
	if err != nil {
		t.Fatalf("Symbols: %v", err)
	}
	res, err := Index([]string{path})
	if err != nil {
		t.Fatalf("Index: %v", err)
	}
	if len(got) != 4 {
		t.Fatalf("Symbols returned %d symbols, want 4: %+v", len(got), got)
	}
	if !reflect.DeepEqual(got, res.Symbols) {
		t.Errorf("Symbols(f) != Index([f]).Symbols\nSymbols: %+v\n  Index: %+v", got, res.Symbols)
	}
}

// TestSymbolsUnsupportedAndBroken pins Symbols' two error paths: a language
// with no indexer, and a .go file the parser rejects. Neither may panic.
func TestSymbolsUnsupportedAndBroken(t *testing.T) {
	dir := t.TempDir()
	broken := writeFile(t, dir, "broken.go", "package broken\n\nfunc (\n")
	if _, err := Symbols(broken); err == nil {
		t.Error("Symbols on a syntactically broken .go file returned no error")
	}
	py := writeFile(t, dir, "x.py", "def f():\n    pass\n")
	if _, err := Symbols(py); !errors.Is(err, ErrUnsupportedLanguage) {
		t.Errorf("Symbols(x.py) err = %v, want ErrUnsupportedLanguage", err)
	}
}

// TestGoIndexerSetsPackageFromClause pins Symbol.Package to the package
// clause, not the directory name, on every symbol kind including methods.
func TestGoIndexerSetsPackageFromClause(t *testing.T) {
	dir := t.TempDir()
	// Directory "bar", package clause "foo": Package must follow the clause.
	path := writeFile(t, dir, "bar/f.go", `package foo

const C = 1

var V int

type T struct{}

func (T) M() {}

func F() {}
`)
	syms, err := Symbols(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(syms) != 5 {
		t.Fatalf("got %d symbols, want 5: %+v", len(syms), syms)
	}
	sawMethod := false
	for _, s := range syms {
		if s.Package != "foo" {
			t.Errorf("symbol %q: Package = %q, want %q", s.Name, s.Package, "foo")
		}
		if s.Name == "T.M" {
			sawMethod = true
		}
	}
	if !sawMethod {
		t.Errorf("method must keep Name \"T.M\": %+v", syms)
	}

	// The real binary's entry file lives in cmd/dross but declares package main.
	mainSyms, err := Symbols(filepath.Join("..", "..", "cmd", "dross", "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	if len(mainSyms) == 0 {
		t.Fatal("cmd/dross/main.go yielded no symbols")
	}
	for _, s := range mainSyms {
		if s.Package != "main" {
			t.Errorf("cmd/dross/main.go symbol %q: Package = %q, want main", s.Name, s.Package)
		}
	}
}

// TestIndexKeepsSymbolDataAddingPackage re-indexes the
// TestGoIndexerExtractsAllKinds fixture and pins every symbol's Name, Kind and
// Line exactly: adding Package must not disturb existing data, only add to it.
func TestIndexKeepsSymbolDataAddingPackage(t *testing.T) {
	dir := makeFixture(t)
	path := writeFile(t, dir, "thing.go", `package thing

const Pi = 3.14
const (
	MaxRetries = 3
	MinDelay   = 10
)

var Counter int

type User struct {
	Name string
}

type Stringer interface {
	String() string
}

func New(name string) *User {
	return &User{Name: name}
}

func (u *User) Greet() string {
	return "hi " + u.Name
}

func (u User) Quiet() {}
`)
	res, err := Index([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	type row struct {
		Name, Kind string
		Line       int
	}
	want := []row{
		{"Pi", "const", 3},
		{"MaxRetries", "const", 5},
		{"MinDelay", "const", 6},
		{"Counter", "var", 9},
		{"User", "type", 11},
		{"Stringer", "type", 15},
		{"New", "function", 19},
		{"User.Greet", "method", 23},
		{"User.Quiet", "method", 27},
	}
	var got []row
	for _, s := range res.Symbols {
		got = append(got, row{s.Name, s.Kind, s.Line})
		if s.Package != "thing" {
			t.Errorf("symbol %q: Package = %q, want thing", s.Name, s.Package)
		}
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Index symbol data changed.\n got: %+v\nwant: %+v", got, want)
	}
}
