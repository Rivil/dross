package cmd

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// This guard closes the second half of test-suite hermeticity. The first half —
// the package-wide HOME pin in hermetic_env_test.go — stops a test inheriting
// the developer's global config. This half stops a test reading the developer's
// own checkout of .dross.
//
// The failure mode is a local green that CI cannot reproduce. A fresh checkout
// has every TRACKED file under .dross (project.toml, rules.toml, survivors.toml,
// changes.json, milestones/, phases/) and none of the GITIGNORED ones
// (state.json, handoff.md, local.toml, security/, quality/, techdebt/). A test
// that reaches for an ignored path therefore passes on the author's box off data
// no other machine has, and reddens — or, worse, silently skips — everywhere
// else. Two tests did exactly this before this guard existed:
//
//   - TestProgressAgainstThisRepo handed the real .dross directory to
//     buildMilestoneProgress, whose phaseIsDone falls back to state.json history.
//     It never spelled "state.json" anywhere, which is why the rule below cannot
//     be a name check alone.
//   - TestHandoffParksNoHomelessFinding read .dross/handoff.md and Skipf'd in CI,
//     so the guard it implemented never ran where it mattered.
//
// THE RULE: a test that composes a repo-root walker with ".dross" must name a
// tracked file in the same expression. Naming an ignored one fails. Naming
// nothing — handing the bare directory onward — also fails, because what the
// callee reaches cannot be audited from the source.

// rootWalkers are the helpers that resolve the REAL repository checkout. A path
// built from any of them is a path into the developer's own tree, as opposed to
// the t.TempDir() fixtures the rest of the suite uses.
var rootWalkers = map[string]bool{
	"repoRootFromTest":      true,
	"repoRoot":              true,
	"repoRootForDocs":       true,
	"repoRootForHybridTest": true,
	"repoRootFromHere":      true,
	"moduleRoot":            true,
}

// ignoredDrossNames reads .gitignore and returns the names directly under
// .dross/ that a fresh checkout will not have.
//
// Derived rather than hardcoded on purpose: the guard has to stay true as
// .gitignore changes, and a list copied into this file would drift the first
// time someone ignores a new artifact directory.
func ignoredDrossNames(gitignore string) map[string]bool {
	out := map[string]bool{}
	for _, ln := range strings.Split(gitignore, "\n") {
		ln = strings.TrimSpace(ln)
		if ln == "" || strings.HasPrefix(ln, "#") {
			continue
		}
		ln = strings.TrimPrefix(ln, "/")
		rest, ok := strings.CutPrefix(ln, ".dross/")
		if !ok {
			continue
		}
		if name := strings.Trim(rest, "/"); name != "" {
			out[name] = true
		}
	}
	return out
}

// drossReadViolations reports every composed real-repo .dross read in one test
// source file. It is a pure function over source text so its teeth can be proven
// on synthetic input rather than on whatever the tree happens to contain today.
func drossReadViolations(filename, src string, ignored map[string]bool) ([]string, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filename, src, 0)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", filename, err)
	}

	// Idents bound to a walker's result, so `root := repoRootFromTest(t)`
	// followed by a Join on `root` is caught like the inline form.
	rooted := map[string]bool{}
	ast.Inspect(file, func(n ast.Node) bool {
		as, ok := n.(*ast.AssignStmt)
		if !ok {
			return true
		}
		for i, rhs := range as.Rhs {
			if i >= len(as.Lhs) || !callsWalker(rhs) {
				continue
			}
			if id, ok := as.Lhs[i].(*ast.Ident); ok {
				rooted[id.Name] = true
			}
		}
		return true
	})

	var problems []string
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || !isFilepathJoin(call.Fun) || len(call.Args) == 0 {
			return true
		}
		if !callsWalker(call.Args[0]) && !isRootedIdent(call.Args[0], rooted) {
			return true
		}

		lits, drossAt := joinLiterals(call.Args[1:])
		if drossAt < 0 {
			return true // a real-repo path that never enters .dross
		}
		pos := fset.Position(call.Pos())
		after := lits[drossAt+1:]
		if len(after) == 0 {
			problems = append(problems, fmt.Sprintf(
				"%s:%d: hands the real repo's .dross directory to a callee without naming a file — "+
					"what it reads cannot be audited here, and a loader that falls back to state.json "+
					"will pass locally and not on a fresh checkout; use a t.TempDir() fixture",
				filename, pos.Line))
			return true
		}
		if ignored[after[0]] {
			problems = append(problems, fmt.Sprintf(
				"%s:%d: reads the real repo's .dross/%s, which .gitignore keeps out of a fresh "+
					"checkout — this passes on your machine and cannot on CI; use a t.TempDir() fixture",
				filename, pos.Line, after[0]))
		}
		return true
	})

	sort.Strings(problems)
	return problems, nil
}

// callsWalker reports whether an expression is a direct call to a root walker.
func callsWalker(e ast.Expr) bool {
	call, ok := e.(*ast.CallExpr)
	if !ok {
		return false
	}
	id, ok := call.Fun.(*ast.Ident)
	return ok && rootWalkers[id.Name]
}

func isRootedIdent(e ast.Expr, rooted map[string]bool) bool {
	id, ok := e.(*ast.Ident)
	return ok && rooted[id.Name]
}

func isFilepathJoin(fun ast.Expr) bool {
	sel, ok := fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Join" {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	return ok && pkg.Name == "filepath"
}

// joinLiterals returns the string-literal arguments in order, and the index of
// ".dross" among them (-1 when absent). A non-literal argument is recorded as ""
// so it still occupies a position — a computed segment after .dross is a named
// segment for our purposes, just not one we can read.
func joinLiterals(args []ast.Expr) ([]string, int) {
	lits := make([]string, 0, len(args))
	drossAt := -1
	for _, a := range args {
		val := ""
		if bl, ok := a.(*ast.BasicLit); ok && bl.Kind == token.STRING {
			if s, err := strconv.Unquote(bl.Value); err == nil {
				val = s
			}
		}
		if val == RootDirName && drossAt < 0 {
			drossAt = len(lits)
		}
		lits = append(lits, val)
	}
	return lits, drossAt
}

func TestIgnoredSetComesFromGitignore(t *testing.T) {
	got := ignoredDrossNames("/dist\n# a comment\n.dross/handoff.md\n.dross/quality/\nnot-dross/x\n")
	want := map[string]bool{"handoff.md": true, "quality": true}
	if len(got) != len(want) {
		t.Fatalf("names = %v, want %v", got, want)
	}
	for name := range want {
		if !got[name] {
			t.Errorf("%s missing from %v", name, got)
		}
	}
	// The point of deriving: a newly ignored path is covered with no edit here.
	if !ignoredDrossNames(".dross/brand-new-artifacts/\n")["brand-new-artifacts"] {
		t.Error("a path newly added to .gitignore must become guarded automatically")
	}
}

func TestDetectsGitignoredDrossRead(t *testing.T) {
	src := `package x
import "path/filepath"
func f(t *T) { _ = filepath.Join(repoRootFromTest(t), ".dross", "handoff.md") }
`
	got := violations(t, src)
	if len(got) != 1 {
		t.Fatalf("want exactly one violation, got %v", got)
	}
	if !strings.Contains(got[0], "handoff.md") {
		t.Errorf("the violation must name the path it reached for: %s", got[0])
	}
}

func TestDetectsUnauditableDrossDirRead(t *testing.T) {
	src := `package x
import "path/filepath"
func f(t *T) { _ = buildMilestoneProgress(filepath.Join(repoRootFromTest(t), ".dross"), "v1.3") }
`
	got := violations(t, src)
	if len(got) != 1 {
		t.Fatalf("a bare .dross directory read must be caught — a name-only guard misses it: %v", got)
	}
}

func TestDetectsReadThroughAssignedRoot(t *testing.T) {
	src := `package x
import "path/filepath"
func f(t *T) {
	root := repoRootFromTest(t)
	_ = filepath.Join(root, ".dross", "state.json")
}
`
	if got := violations(t, src); len(got) != 1 {
		t.Fatalf("binding the walker to a variable must not launder the read: %v", got)
	}
}

func TestPermitsTrackedDrossRead(t *testing.T) {
	src := `package x
import "path/filepath"
func f(t *T) {
	_ = filepath.Join(repoRootFromTest(t), ".dross", "project.toml")
	_ = filepath.Join(repoRootFromTest(t), ".dross", "rules.toml")
	_ = filepath.Join(repoRootFromTest(t), ".dross", "survivors.toml")
	_ = filepath.Join(repoRootFromTest(t), "assets", "prompts", "spec.md")
	_ = filepath.Join(t.TempDir(), ".dross", "state.json")
}
`
	if got := violations(t, src); len(got) != 0 {
		t.Errorf("tracked .dross files and fixture paths must stay legal: %v", got)
	}
}

func violations(t *testing.T, src string) []string {
	t.Helper()
	got, err := drossReadViolations("synthetic_test.go", src,
		map[string]bool{"handoff.md": true, "state.json": true, "local.toml": true})
	if err != nil {
		t.Fatalf("detector: %v", err)
	}
	return got
}

// TestNoTestReadsGitignoredDross is the assertion that would have caught both
// historical bugs. It walks every *_test.go in the module, not just this
// package: internal/rules and internal/survivor read the real .dross too.
func TestNoTestReadsGitignoredDross(t *testing.T) {
	root := repoRootFromTest(t)
	gitignore, err := os.ReadFile(filepath.Join(root, ".gitignore"))
	if err != nil {
		t.Fatalf("read .gitignore: %v", err)
	}
	ignored := ignoredDrossNames(string(gitignore))
	if len(ignored) == 0 {
		t.Fatal("no ignored .dross paths parsed — the guard would be vacuous")
	}

	var scanned int
	var problems []string
	err = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", "dist", "reports":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), "_test.go") {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		found, err := drossReadViolations(rel, string(src), ignored)
		if err != nil {
			return err
		}
		scanned++
		problems = append(problems, found...)
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if scanned < 200 {
		t.Fatalf("scanned only %d test files — the walk is not covering the module", scanned)
	}
	for _, p := range problems {
		t.Error(p)
	}
}

// TestArchitectureNamesHermeticGuard keeps the documented principle and the
// enforced one from drifting apart. The deferred item that produced this phase
// read "the principle exists but nothing enforces it" — it was worse than that:
// the Test-suite hermeticity entry covered HOME leakage only and did not
// mention the repo's own .dross at all. An entry that omits half the rule is how
// the next author learns the wrong rule.
//
// `dross doctor` already reports an anchor whose line has moved; this asserts
// the anchors are PRESENT, which a resolver cannot.
func TestArchitectureNamesHermeticGuard(t *testing.T) {
	body, err := os.ReadFile(filepath.Join(repoRootFromTest(t), "ARCHITECTURE.md"))
	if err != nil {
		t.Fatalf("read ARCHITECTURE.md: %v", err)
	}
	doc := string(body)
	start := strings.Index(doc, "### Test-suite hermeticity")
	if start < 0 {
		t.Fatal("no Test-suite hermeticity entry")
	}
	entry := doc[start:]
	if end := strings.Index(entry[1:], "\n### "); end >= 0 {
		entry = entry[:end+1]
	}

	for _, want := range []string{
		"drossReadViolations",
		"TestNoTestReadsGitignoredDross",
		"ignoredDrossNames",
		"liveRecordRoot",
	} {
		if !strings.Contains(entry, want) {
			t.Errorf("the hermeticity entry does not name %s — the enforcement is undocumented", want)
		}
	}
	// The rule itself, not just the symbols: someone reading the entry has to
	// learn what is and is not allowed.
	if !strings.Contains(entry, "gitignored") || !strings.Contains(entry, "tracked") {
		t.Error("the entry must state the tracked-vs-gitignored distinction the rule turns on")
	}
}
