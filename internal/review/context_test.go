package review

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/phase"
	"github.com/Rivil/dross/internal/rules"
	"github.com/Rivil/dross/internal/treefp"
)

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

func write(t *testing.T, dir, rel, body string) {
	t.Helper()
	p := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// repo is a committed repository with a.go, b.go and a tracked .dross/.
func repo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	dir := t.TempDir()
	git(t, dir, "init", "-q", "-b", "main")
	for _, kv := range [][2]string{{"user.email", "t@example.com"}, {"user.name", "T"}, {"commit.gpgsign", "false"}, {"gc.auto", "0"}} {
		git(t, dir, "config", kv[0], kv[1])
	}
	write(t, dir, "a.go", "package a\n")
	write(t, dir, "b.go", "package b\n")
	write(t, dir, ".dross/state.json", "{}\n")
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-q", "-m", "init")
	return dir
}

func taskScope() Scope {
	return Scope{
		Kind:  KindTask,
		Phase: "p",
		Task: phase.Task{
			ID: "t-3", Title: "Build the thing", Files: []string{"a.go"},
			Description: "Change a.go.", Covers: []string{"c-1", "c-3"},
			TestContract: []string{"if A breaks, TestA fails"}, Status: phase.StatusInProgress,
		},
		Criteria: []phase.Criterion{
			{ID: "c-1", Text: "COVERED-ONE"}, {ID: "c-2", Text: "UNCOVERED-TWO"}, {ID: "c-3", Text: "COVERED-THREE"},
		},
		Decisions: []phase.Decision{
			{Key: "locked_one", Choice: "LOCKED-CHOICE", Locked: true},
			{Key: "open_one", Choice: "UNLOCKED-CHOICE"},
		},
		Rules: []rules.Resolved{
			{Rule: rules.Rule{ID: "r-hard", Severity: rules.Hard, Text: "HARD-RULE"}, Scope: rules.Project},
			{Rule: rules.Rule{ID: "r-soft", Severity: rules.Soft, Text: "SOFT-RULE"}, Scope: rules.Project},
		},
	}
}

func build(t *testing.T, dir string, s Scope, sec Secrets) Context {
	t.Helper()
	c, err := BuildContext(dir, s, sec)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestContextTreeMatchesWorkingTree(t *testing.T) {
	dir := repo(t)
	write(t, dir, "a.go", "package a // edited\n")
	write(t, dir, "c.go", "package c\n")
	c := build(t, dir, taskScope(), Secrets{})
	tree, err := treefp.WorkingTree(dir)
	if err != nil {
		t.Fatal(err)
	}
	if c.Tree != tree {
		t.Fatalf("context tree %q != WorkingTree %q: a pass would vouch for a tree the gate never compares", c.Tree, tree)
	}
}

func TestContextWithholdsSecretPaths(t *testing.T) {
	dir := repo(t)
	write(t, dir, "a.go", "package a // edited\n")
	write(t, dir, "deploy/prod.env", "do-not-render-this-line\n")
	c := build(t, dir, taskScope(), Secrets{Patterns: []string{"*.env"}})
	if strings.Contains(c.Body, "do-not-render-this-line") {
		t.Fatal("a withheld secret path's content reached the context")
	}
	if !strings.Contains(c.Body, "(content withheld — secret path: deploy/prod.env)") {
		t.Fatal("the withheld path is not named")
	}
	if !strings.Contains(c.Body, "+package a // edited") {
		t.Fatal("withholding deploy/prod.env dropped the rest of the diff")
	}
}

func TestContextMalformedGatesRefuses(t *testing.T) {
	dir := repo(t)
	write(t, dir, "a.go", "package a // edited\n")
	_, err := BuildContext(dir, taskScope(), Secrets{
		Patterns: []string{"*.env"},
		Err:      errors.New("~/.claude/dross/gates.toml: toml: line 3: expected '='"),
	})
	if err == nil || !strings.Contains(err.Error(), "gates.toml") {
		t.Fatalf("BuildContext with a malformed secret list = %v, want a refusal naming the file", err)
	}
}

func TestContextScope(t *testing.T) {
	dir := repo(t)
	write(t, dir, "a.go", "package a // edited\n")
	s := taskScope()
	c := build(t, dir, s, Secrets{})
	for _, want := range []string{"COVERED-ONE", "COVERED-THREE", "LOCKED-CHOICE", "HARD-RULE", "[dross-commit-hygiene]", "if A breaks, TestA fails", "Change a.go."} {
		if !strings.Contains(c.Body, want) {
			t.Errorf("context lacks %q", want)
		}
	}
	for _, bad := range []string{"UNCOVERED-TWO", "UNLOCKED-CHOICE", "SOFT-RULE", phase.StatusInProgress, "status:"} {
		if strings.Contains(c.Body, bad) {
			t.Errorf("context carries %q", bad)
		}
	}

	s.Task.Status = phase.StatusPending
	if again := build(t, dir, s, Secrets{}); again.Digest != c.Digest {
		t.Fatal("a status flip changed the digest")
	}

	q := build(t, dir, Scope{Kind: KindQuick, Description: "QUICK-DESCRIPTION", Rules: s.Rules}, Secrets{})
	if !strings.Contains(q.Body, "QUICK-DESCRIPTION") || !strings.Contains(q.Body, "HARD-RULE") || !strings.Contains(q.Body, "[dross-commit-hygiene]") {
		t.Fatalf("quick context lacks its description or the hard rules:\n%s", q.Body)
	}
	for _, bad := range []string{"## Task record", "## Covered criteria", "## Locked decisions", "COVERED-ONE"} {
		if strings.Contains(q.Body, bad) {
			t.Errorf("quick context carries plan content %q", bad)
		}
	}
}

func TestContextDigest(t *testing.T) {
	dir := repo(t)
	write(t, dir, "a.go", "package a // edited\n")
	one := build(t, dir, taskScope(), Secrets{})
	two := build(t, dir, taskScope(), Secrets{})
	if one.Digest != two.Digest || !strings.HasPrefix(one.Digest, "sha256:") {
		t.Fatalf("two builds on an unchanged tree: %q vs %q", one.Digest, two.Digest)
	}
	sum := sha256.Sum256([]byte(one.Body))
	if one.Digest != "sha256:"+hex.EncodeToString(sum[:]) {
		t.Fatal("the digest is not sha256 over the body")
	}
	if !strings.HasPrefix(string(one.File()), "dross-review-context "+one.Digest+"\n") {
		t.Fatal("the context file does not open with its digest header")
	}
	write(t, dir, "a.go", "package a // editeD\n")
	if three := build(t, dir, taskScope(), Secrets{}); three.Digest == one.Digest {
		t.Fatal("a one-byte edit left the digest unchanged")
	}
}

func TestContextNoCode(t *testing.T) {
	dir := repo(t)
	write(t, dir, ".dross/state.json", `{"x":1}`+"\n")
	if _, err := BuildContext(dir, taskScope(), Secrets{}); !errors.Is(err, ErrNoCode) {
		t.Fatalf("a .dross/-only change built a context (err %v), want ErrNoCode", err)
	}
}

// TestContextLeavesIndexAlone: building a context and asking Dirty both work
// on a scratch index; what the user staged stays staged, byte for byte.
func TestContextLeavesIndexAlone(t *testing.T) {
	dir := repo(t)
	write(t, dir, "a.go", "package a // staged\n")
	git(t, dir, "add", "a.go")
	write(t, dir, "b.go", "package b // unstaged\n")
	write(t, dir, "c.go", "package c\n")
	state := func() string {
		b, err := os.ReadFile(filepath.Join(dir, ".git", "index"))
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(b)
		return hex.EncodeToString(sum[:]) + git(t, dir, "diff", "--cached", "--name-only")
	}
	before := state()
	build(t, dir, taskScope(), Secrets{})
	if _, err := treefp.Dirty(dir); err != nil {
		t.Fatal(err)
	}
	if after := state(); after != before {
		t.Fatal("the real index changed")
	}
}

// TestReviewImportDirection: gatestate stores []review.Round, so review
// importing gate or gatestate would cycle. Secret paths arrive as a parameter.
func TestReviewImportDirection(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	checked := 0
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, e.Name(), nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		checked++
		for _, imp := range f.Imports {
			p, _ := strconv.Unquote(imp.Path.Value)
			if strings.HasSuffix(p, "/internal/gate") || strings.HasSuffix(p, "/internal/gatestate") {
				t.Errorf("%s imports %s", e.Name(), p)
			}
		}
	}
	if checked == 0 {
		t.Fatal("checked no files")
	}
}
