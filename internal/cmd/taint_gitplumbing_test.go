package cmd

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// The git plumbing burn-down gate. Scoped BY ORIGIN to one spawn: the
// exec.Command in gitRun, the effect-only helper whose output goes to stderr.
// gitTrim's spawn sits in the same file and feeds the same callers, but its
// findings are t-19's burn-down, not this one's — so this gate keys on the
// origin's exact line, never on the file.

// gitHelperSpawnLines returns the file and line(s) of the exec.Command call
// inside each named helper function in internal/cmd.
func gitHelperSpawnLines(t *testing.T, helpers ...string) map[token.Position]bool {
	t.Helper()
	out := spawnLinesIn(liveView(t), modulePath+"/internal/cmd", helpers...)
	if len(out) == 0 {
		t.Fatalf("found no exec.Command inside %v — the gate would see nothing", helpers)
	}
	return out
}

// spawnLinesIn returns the file and line of each exec.Command call inside the
// named top-level functions of a view's package pkg ("" for every package).
func spawnLinesIn(v *srcView, pkg string, helpers ...string) map[token.Position]bool {
	want := map[string]bool{}
	for _, h := range helpers {
		want[h] = true
	}
	out := map[token.Position]bool{}
	for _, p := range v.Pkgs {
		if pkg != "" && p.Path != pkg {
			continue
		}
		for _, f := range p.Syntax {
			for _, d := range f.Decls {
				fd, ok := d.(*ast.FuncDecl)
				if !ok || fd.Recv != nil || !want[fd.Name.Name] || fd.Body == nil {
					continue
				}
				ast.Inspect(fd.Body, func(n ast.Node) bool {
					call, ok := n.(*ast.CallExpr)
					if !ok {
						return true
					}
					if obj := execCalleeObject(p.Info, call); obj != nil && obj.Pkg() != nil &&
						obj.Pkg().Path() == "os/exec" && obj.Name() == "Command" {
						pos := v.Fset.Position(call.Pos())
						out[token.Position{Filename: pos.Filename, Line: pos.Line}] = true
					}
					return true
				})
			}
		}
	}
	return out
}

// findingsFromLines keeps the findings with an origin on one of lines.
func findingsFromLines(fs []taintFinding, lines map[token.Position]bool) []taintFinding {
	var out []taintFinding
	for _, f := range fs {
		for _, o := range f.Origins {
			if lines[token.Position{Filename: o.Filename, Line: o.Line}] {
				out = append(out, f)
				break
			}
		}
	}
	return out
}

// TestNoGitRunOutputEscapes is the gate: nothing gitRun's spawn printed
// escapes.
func TestNoGitRunOutputEscapes(t *testing.T) {
	taint, _ := execTaintScan(liveView(t))
	for _, f := range findingsFromLines(taint, gitHelperSpawnLines(t, "gitRun")) {
		t.Errorf("%s — %s", f, execTaintRemedy)
	}
}

// TestGitRunGateIsScopedByOrigin: a finding whose origin is gitTrim's spawn —
// in the same file, reaching the same kind of error — is not this gate's; one
// whose origin is gitRun's is.
func TestGitRunGateIsScopedByOrigin(t *testing.T) {
	runLines := gitHelperSpawnLines(t, "gitRun")
	trimLines := gitHelperSpawnLines(t, "gitTrim")
	var run, trim token.Position
	for p := range runLines {
		run = p
	}
	for p := range trimLines {
		trim = p
	}
	if filepath.Base(run.Filename) != filepath.Base(trim.Filename) {
		t.Logf("gitRun and gitTrim now live in different files (%s, %s)", run.Filename, trim.Filename)
	}
	fs := []taintFinding{
		{Escape: token.Position{Filename: "x.go", Line: 1}, What: "is passed to fmt.Errorf", Origins: []token.Position{trim}},
		{Escape: token.Position{Filename: "x.go", Line: 2}, What: "is passed to fmt.Errorf", Origins: []token.Position{run}},
	}
	got := findingsFromLines(fs, runLines)
	if len(got) != 1 || got[0].Escape.Line != 2 {
		t.Errorf("the gate kept %v, want only the gitRun-origin finding", got)
	}
}

// gitReadCallerFiles are the files t-19's burn-down touches: where gitTrim's
// content reads moved to gitRead, and where a caller marks the SHA, branch or
// path it slices out.
var gitReadCallerFiles = []string{
	"internal/cmd/ship_recover.go",
	"internal/cmd/basebranch.go",
	"internal/cmd/milestone.go",
	"internal/cmd/phase_backfill.go",
	"internal/cmd/repair_files.go",
	"internal/cmd/repair_phasedirs.go",
	"internal/cmd/repair_state.go",
	"internal/cmd/secretscan.go",
	"internal/cmd/verifyscope.go",
	"internal/verify/scope.go",
}

// TestNoGitTrimOutputEscapes is the gate for the two reading helpers: nothing
// gitTrim's or gitRead's spawn printed escapes, wherever it lands, and no
// marker in the files this burn-down touched is malformed or idle.
func TestNoGitTrimOutputEscapes(t *testing.T) {
	taint, markers := execTaintScan(liveView(t))
	for _, f := range findingsFromLines(taint, gitHelperSpawnLines(t, "gitTrim", "gitRead")) {
		t.Errorf("%s — %s", f, execTaintRemedy)
	}
	root := sourceProgram(t).Root
	for _, m := range markers {
		rel, _ := filepath.Rel(root, m.Escape.Filename)
		if containsString(gitReadCallerFiles, filepath.ToSlash(rel)) {
			t.Error(m.String())
		}
	}
}

// gitTrimPinned is everything gitTrim may run: per verb, the options allowed
// ahead of the separator, by name. gitTrim's marker says its output is a ref
// name, an object id, a count or git's version; this is what makes that true.
// for-each-ref's --format is further judged atom by atom, and --contains takes
// the next element as its argument.
var gitTrimPinned = map[string]map[string]bool{
	"rev-parse":    {"--verify": true, "--quiet": true, "--is-shallow-repository": true},
	"symbolic-ref": {"--short": true},
	"merge-base":   {},
	"rev-list":     {"--count": true},
	"for-each-ref": {"--format": true, "--count": true, "--contains": true},
	"ls-remote":    {"--heads": true},
	"--version":    {},
}

// gitTrimSiteFloor is ~25% under the 40 gitTrim call sites the walk finds
// (51 before the content reads moved to gitRead).
const gitTrimSiteFloor = 30

// forEachRefAtom matches one %(atom[:modifier]) in a for-each-ref format.
var forEachRefAtom = regexp.MustCompile(`%\(([^):]*)[^)]*\)`)

// gitArgBuilders are the argv builders whose first argument is the verb and
// second the options; everything after them is positional by construction.
var gitArgBuilders = map[string]bool{"gitRefArgs": true, "gitPathArgs": true, "gitRefPathArgs": true}

// gitTrimProblems walks files for gitTrim calls and reports every one whose
// verb or options fall outside gitTrimPinned, and how many sites it examined.
func gitTrimProblems(fset *token.FileSet, files []*ast.File) ([]string, int) {
	var out []string
	sites := 0
	for _, f := range files {
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			if id, ok := call.Fun.(*ast.Ident); !ok || id.Name != "gitTrim" || len(call.Args) < 2 {
				return true
			}
			sites++
			if why := gitTrimArgvProblem(call); why != "" {
				out = append(out, fmt.Sprintf("%s: gitTrim %s — not in the pinned ref set; a content read goes through gitRead",
					fset.Position(call.Pos()), why))
			}
			return true
		})
	}
	sort.Strings(out)
	return out, sites
}

// gitTrimArgvProblem resolves one gitTrim call's verb and options — literal
// arguments, or a gitRefArgs/gitPathArgs/gitRefPathArgs builder — and says what
// is wrong with them, or "".
func gitTrimArgvProblem(call *ast.CallExpr) string {
	args := call.Args[1:]
	if b, ok := args[0].(*ast.CallExpr); ok && len(args) == 1 && call.Ellipsis.IsValid() {
		id, ok := b.Fun.(*ast.Ident)
		if !ok || !gitArgBuilders[id.Name] || len(b.Args) < 2 {
			return "runs an argv this audit cannot resolve"
		}
		verb, ok := stringLit(b.Args[0])
		if !ok {
			return "runs a verb this audit cannot resolve"
		}
		var opts []ast.Expr
		switch o := b.Args[1].(type) {
		case *ast.Ident:
			if o.Name != "nil" {
				return "runs `" + verb + "` with options this audit cannot resolve"
			}
		case *ast.CompositeLit:
			opts = o.Elts
		default:
			return "runs `" + verb + "` with options this audit cannot resolve"
		}
		return pinnedOptsProblem(verb, opts, false)
	}
	verb, ok := stringLit(args[0])
	if !ok {
		return "runs a verb this audit cannot resolve"
	}
	return pinnedOptsProblem(verb, args[1:], true)
}

// pinnedOptsProblem judges a verb and the elements that follow it. With bare
// set there is no separator: a literal not starting with "-" is a positional,
// and a non-literal cannot be classified at all.
func pinnedOptsProblem(verb string, elts []ast.Expr, bare bool) string {
	allowed, ok := gitTrimPinned[verb]
	if !ok {
		return "runs `" + verb + "`"
	}
	if verb == "--version" && len(elts) > 0 {
		return "runs `--version` with arguments"
	}
	for i := 0; i < len(elts); i++ {
		v, ok := stringLit(elts[i])
		if !ok {
			if bare {
				return "runs `" + verb + "` with a non-literal argument and no separator"
			}
			return "runs `" + verb + "` with a non-literal option"
		}
		if !strings.HasPrefix(v, "-") {
			if bare {
				continue
			}
			return "runs `" + verb + "` with a positional among its options"
		}
		name, val, _ := strings.Cut(v, "=")
		if !allowed[name] {
			return "runs `" + verb + "` with option `" + name + "`"
		}
		switch name {
		case "--format":
			for _, m := range forEachRefAtom.FindAllStringSubmatch(val, -1) {
				if m[1] != "refname" && m[1] != "objectname" {
					return "runs `" + verb + "` with format atom `%(" + m[1] + ")`"
				}
			}
		case "--contains":
			i++ // its argument
		}
	}
	return ""
}

// TestGitTrimRunsRefVerbsOnly: every live gitTrim call site runs a pinned ref
// invocation, the walk sees enough of them to mean it, and a content read, a
// contents atom or a remote URL query each trip.
func TestGitTrimRunsRefVerbsOnly(t *testing.T) {
	v := liveView(t)
	var files []*ast.File
	for _, p := range v.Pkgs {
		if p.Path == modulePath+"/internal/cmd" {
			files = append(files, p.Syntax...)
		}
	}
	problems, sites := gitTrimProblems(v.Fset, files)
	for _, p := range problems {
		t.Error(p)
	}
	if err := gitTrimSiteFloorErr(sites); err != nil {
		t.Error(err)
	}
	t.Logf("examined %d gitTrim call sites", sites)
	if gitTrimSiteFloorErr(gitTrimSiteFloor) != nil || gitTrimSiteFloorErr(gitTrimSiteFloor-1) == nil {
		t.Error("the floor must pass at its minimum and fail one under it")
	}

	cases := []struct {
		src, want string // want "" means clean
	}{
		{`gitTrim(dir, "log", "--oneline")`, "runs `log`"},
		{`gitTrim(dir, gitRefArgs("for-each-ref", []string{"--format=%(contents)"}, "refs/heads/")...)`, "format atom `%(contents)`"},
		{`gitTrim(dir, "ls-remote", "--get-url", "origin")`, "option `--get-url`"},
		{`gitTrim(dir, gitRefArgs("rev-list", []string{"--pretty=%B"}, "HEAD")...)`, "option `--pretty`"},
		{`gitTrim(".", "--version", "--build-options")`, "`--version` with arguments"},
		{`gitTrim(dir, gitRefArgs("for-each-ref", []string{"--format=%(refname:short) %(objectname)", "--contains", sha}, "refs/")...)`, ""},
		{`gitTrim(dir, gitRefArgs("rev-list", []string{"--count"}, a+".."+b)...)`, ""},
		{`gitTrim(dir, "symbolic-ref", "--short", "HEAD")`, ""},
		{`gitTrim(".", "--version")`, ""},
	}
	for _, c := range cases {
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, "fixture.go", "package cmd\n\nfunc f() {\n\t_, _ = "+c.src+"\n}\n", 0)
		if err != nil {
			t.Fatalf("%s: %v", c.src, err)
		}
		got, n := gitTrimProblems(fset, []*ast.File{f})
		if n != 1 {
			t.Errorf("%s: the walk examined %d sites, want 1", c.src, n)
			continue
		}
		switch {
		case c.want == "" && len(got) != 0:
			t.Errorf("%s: a pinned invocation tripped: %v", c.src, got)
		case c.want != "" && (len(got) != 1 || !strings.Contains(got[0], c.want) || !strings.Contains(got[0], "fixture.go:4")):
			t.Errorf("%s: got %v, want one problem naming %q and the call site", c.src, got, c.want)
		}
	}
}

func gitTrimSiteFloorErr(n int) error {
	if n < gitTrimSiteFloor {
		return fmt.Errorf("the gitTrim audit examined only %d call sites, under its floor of %d — the walk has stopped resolving them", n, gitTrimSiteFloor)
	}
	return nil
}

// TestGitReadContentIsNotCleared: gitTrim's marker covers only what gitTrim
// runs. A `git diff -U0` patch read through gitRead — verifyscope's shape —
// and returned in an error trips at the error, naming gitRead's spawn; a SHA
// read through the marked gitTrim is clean.
func TestGitReadContentIsNotCleared(t *testing.T) {
	fx := loadFixture(t, fixturePath("taint", "gitread.go.txt"))
	taint, markers := execTaintScan(fx.srcView)
	assertCorpus(t, fx, "taint", taintHits(taint))
	for _, m := range markers {
		t.Error(m.String())
	}
	read := spawnLinesIn(fx.srcView, "", "gitRead")
	if len(read) == 0 {
		t.Fatal("the fixture's gitRead holds no exec.Command")
	}
	if len(findingsFromLines(taint, read)) == 0 {
		t.Error("no finding names gitRead's spawn as its origin")
	}
}
