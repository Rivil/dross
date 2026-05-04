package codex

import (
	"os"
	"os/exec"
	"path/filepath"
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
		"Pi":          "const",
		"MaxRetries":  "const",
		"MinDelay":    "const",
		"Counter":     "var",
		"User":        "type",
		"Stringer":    "type",
		"New":         "function",
		"User.Greet":  "method",
		"User.Quiet":  "method",
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
