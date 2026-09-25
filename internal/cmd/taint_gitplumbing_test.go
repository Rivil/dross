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

// The git plumbing burn-downs' guards, now over internal/gitrun's verbs: Run
// (effect-only, output to stderr), Trim (pinned ref invocations, one marker)
// and Raw/Read (content, unmarked). Their zero-findings gates are retired into
// TestNoSpawnOutputEscapes (taint_audit_test.go); what stays is the verb pin
// that keeps Trim's marker true, the check that no marker binds inside Raw or
// Read, and the fixture proving content is not cleared by Trim's. The spawns
// share a file, so origin scoping keys on the spawn's exact line, never on the
// file.

// gitHelperSpawnLines returns the file and line(s) of the exec.Command call
// inside each named verb function in internal/gitrun.
func gitHelperSpawnLines(t *testing.T, helpers ...string) map[token.Position]bool {
	t.Helper()
	out := spawnLinesIn(liveView(t), gitrunPath, helpers...)
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

// TestGitRunGateIsScopedByOrigin: findingsFromLines keys on a spawn's exact
// line. A finding whose origin is gitrun.Trim's spawn — in the same file as
// Run's, reaching the same kind of error — is not kept when asking for Run's;
// one whose origin is Run's is.
func TestGitRunGateIsScopedByOrigin(t *testing.T) {
	runLines := gitHelperSpawnLines(t, "Run")
	trimLines := gitHelperSpawnLines(t, "Trim")
	var run, trim token.Position
	for p := range runLines {
		run = p
	}
	for p := range trimLines {
		trim = p
	}
	if filepath.Base(run.Filename) != filepath.Base(trim.Filename) {
		t.Logf("Run and Trim now live in different files (%s, %s)", run.Filename, trim.Filename)
	}
	fs := []taintFinding{
		{Escape: token.Position{Filename: "x.go", Line: 1}, What: "is passed to fmt.Errorf", Origins: []token.Position{trim}},
		{Escape: token.Position{Filename: "x.go", Line: 2}, What: "is passed to fmt.Errorf", Origins: []token.Position{run}},
	}
	got := findingsFromLines(fs, runLines)
	if len(got) != 1 || got[0].Escape.Line != 2 {
		t.Errorf("the gate kept %v, want only the Run-origin finding", got)
	}
}

// gitTrimPinned is everything gitrun.Trim may run: per verb, the options allowed ahead of the separator, by name. Trim's
// marker says its output is a ref name, an object id, a count, a boolean or
// git's version; this is what makes that true. for-each-ref's --format is
// further judged atom by atom, and --contains takes the next element as its
// argument. rev-parse's --short abbreviates an object id and
// --is-inside-work-tree prints true or false — neither prints content, while
// --git-path, --show-toplevel and the like print paths and are out.
var gitTrimPinned = map[string]map[string]bool{
	"rev-parse":    {"--verify": true, "--quiet": true, "--is-shallow-repository": true, "--short": true, "--is-inside-work-tree": true},
	"symbolic-ref": {"--short": true},
	"merge-base":   {},
	"rev-list":     {"--count": true},
	"for-each-ref": {"--format": true, "--count": true, "--contains": true},
	"ls-remote":    {"--heads": true},
	"--version":    {},
}

// gitTrimSiteFloor is ~25% under the 40 gitrun.Trim call sites the walk finds
// (51 before the content reads moved to gitRead, since rewritten onto
// gitrun.Read).
const gitTrimSiteFloor = 30

// forEachRefAtom matches one %(atom[:modifier]) in a for-each-ref format.
var forEachRefAtom = regexp.MustCompile(`%\(([^):]*)[^)]*\)`)

// gitArgBuilders are the argv builders whose first argument is the verb and
// second the options; everything after them is positional by construction.
var gitArgBuilders = map[string]bool{"gitRefArgs": true, "gitPathArgs": true, "gitRefPathArgs": true}

// isGitTrimCall reports whether call is a gitrun.Trim selector call.
func isGitTrimCall(call *ast.CallExpr) bool {
	fn, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := fn.X.(*ast.Ident)
	return ok && pkg.Name == "gitrun" && fn.Sel.Name == "Trim"
}

// gitTrimProblems walks files for gitrun.Trim calls and reports every one whose
// verb or options fall outside gitTrimPinned, and how many sites it examined.
func gitTrimProblems(fset *token.FileSet, files []*ast.File) ([]string, int) {
	var out []string
	sites := 0
	for _, f := range files {
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || !isGitTrimCall(call) || len(call.Args) < 2 {
				return true
			}
			sites++
			if why := gitTrimArgvProblem(call); why != "" {
				out = append(out, fmt.Sprintf("%s: gitrun.Trim %s — not in the pinned ref set; a content read goes through gitrun.Read or gitrun.Raw",
					fset.Position(call.Pos()), why))
			}
			return true
		})
	}
	sort.Strings(out)
	return out, sites
}

// gitTrimArgvProblem resolves one gitrun.Trim call's verb and options — literal
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

// TestGitTrimRunsRefVerbsOnly: every live gitrun.Trim call site, in any
// package, runs a pinned ref invocation, the walk sees enough of them to mean
// it, and a content read, a contents atom or a remote URL query each trip.
func TestGitTrimRunsRefVerbsOnly(t *testing.T) {
	v := liveView(t)
	var files []*ast.File
	for _, p := range v.Pkgs {
		files = append(files, p.Syntax...)
	}
	problems, sites := gitTrimProblems(v.Fset, files)
	for _, p := range problems {
		t.Error(p)
	}
	if err := gitTrimSiteFloorErr(sites); err != nil {
		t.Error(err)
	}
	t.Logf("examined %d gitrun.Trim call sites", sites)
	if gitTrimSiteFloorErr(gitTrimSiteFloor) != nil || gitTrimSiteFloorErr(gitTrimSiteFloor-1) == nil {
		t.Error("the floor must pass at its minimum and fail one under it")
	}

	cases := []struct {
		src, want string // want "" means clean
	}{
		{`gitrun.Trim(dir, "log", "--oneline")`, "runs `log`"},
		{`gitrun.Trim(dir, "remote", "get-url", "origin")`, "runs `remote`"},
		{`gitrun.Trim(dir, "rev-parse", "--git-path", "x")`, "option `--git-path`"},
		{`gitrun.Trim(dir, "rev-parse", "--short", "HEAD")`, ""},
		{`gitrun.Trim(dir, "rev-parse", "--is-inside-work-tree")`, ""},
		{`gitrun.Trim(dir, gitRefArgs("for-each-ref", []string{"--format=%(contents)"}, "refs/heads/")...)`, "format atom `%(contents)`"},
		{`gitrun.Trim(dir, "ls-remote", "--get-url", "origin")`, "option `--get-url`"},
		{`gitrun.Trim(dir, gitRefArgs("rev-list", []string{"--pretty=%B"}, "HEAD")...)`, "option `--pretty`"},
		{`gitrun.Trim(".", "--version", "--build-options")`, "`--version` with arguments"},
		{`gitrun.Trim(dir, gitRefArgs("for-each-ref", []string{"--format=%(refname:short) %(objectname)", "--contains", sha}, "refs/")...)`, ""},
		{`gitrun.Trim(dir, gitRefArgs("rev-list", []string{"--count"}, a+".."+b)...)`, ""},
		{`gitrun.Trim(dir, "symbolic-ref", "--short", "HEAD")`, ""},
		{`gitrun.Trim(".", "--version")`, ""},
	}
	for _, c := range cases {
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, "internal/security/fixture.go", "package security\n\nfunc f() {\n\t_, _ = "+c.src+"\n}\n", 0)
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
		case c.want != "" && (len(got) != 1 || !strings.Contains(got[0], c.want) || !strings.Contains(got[0], "internal/security/fixture.go:4")):
			t.Errorf("%s: got %v, want one problem naming %q and the call site", c.src, got, c.want)
		}
	}
}

// gitHelperNames are the cmd helpers gitrun replaced.
var gitHelperNames = map[string]bool{"gitTrim": true, "gitRead": true, "gitRun": true, "gitNoOut": true}

// TestGitHelperDelegatesAreGone: no production file under internal/ or cmd/
// declares a function named after a replaced helper. Test files may — the
// fixture shim keeps the old names for setup — but a production one would be
// a second way to spawn git beside the runner.
func TestGitHelperDelegatesAreGone(t *testing.T) {
	v := liveView(t)
	for _, p := range v.Pkgs {
		for _, f := range p.Syntax {
			for _, d := range f.Decls {
				if fd, ok := d.(*ast.FuncDecl); ok && fd.Recv == nil && gitHelperNames[fd.Name.Name] {
					t.Errorf("%s declares %s — git goes through internal/gitrun, not a package-local helper", v.Fset.Position(fd.Pos()), fd.Name.Name)
				}
			}
		}
	}
}

// markersInside names every taint marker that binds a line inside one of the
// named top-level functions of package pkg.
func markersInside(v *srcView, pkg string, markers []taintMarker, funcs ...string) []string {
	want := map[string]bool{}
	for _, f := range funcs {
		want[f] = true
	}
	var out []string
	for _, p := range v.Pkgs {
		if p.Path != pkg {
			continue
		}
		for _, f := range p.Syntax {
			for _, d := range f.Decls {
				fd, ok := d.(*ast.FuncDecl)
				if !ok || fd.Recv != nil || !want[fd.Name.Name] {
					continue
				}
				start, end := v.Fset.Position(fd.Pos()), v.Fset.Position(fd.End())
				for _, m := range markers {
					if m.file == start.Filename && m.bound >= start.Line && m.bound <= end.Line {
						out = append(out, fmt.Sprintf("%s:%d: a taint-cleared marker binds inside %s — content verbs return the repo's text and each caller marks what it slices out",
							filepath.Base(m.file), m.line, fd.Name.Name))
					}
				}
			}
		}
	}
	sort.Strings(out)
	return out
}

// TestGitrunContentVerbsCarryNoMarker: Raw and Read return content, so no
// taint-cleared marker may bind inside either — one there would clear every
// log, diff and status any caller reads. Trim's marker is the package's only
// one; moved into Raw's body it is reported.
func TestGitrunContentVerbsCarryNoMarker(t *testing.T) {
	v := liveView(t)
	markers := taintMarkersIn(t, "internal/gitrun")
	if len(markers) == 0 {
		t.Fatal("internal/gitrun carries no taint-cleared marker — Trim's is gone, and this check would be vacuous")
	}
	for _, f := range markersInside(v, gitrunPath, markers, "Raw", "Read") {
		t.Error(f)
	}
	if got := markersInside(v, gitrunPath, markers, "Trim"); len(got) != 1 {
		t.Errorf("Trim holds %d taint-cleared markers, want its one: %v", len(got), got)
	}
	var rawLine int
	var file string
	for _, p := range v.Pkgs {
		if p.Path != gitrunPath {
			continue
		}
		for _, f := range p.Syntax {
			for _, d := range f.Decls {
				if fd, ok := d.(*ast.FuncDecl); ok && fd.Name.Name == "Raw" {
					pos := v.Fset.Position(fd.Body.Lbrace)
					rawLine, file = pos.Line, pos.Filename
				}
			}
		}
	}
	if rawLine == 0 {
		t.Fatal("found no gitrun.Raw")
	}
	moved := markers[0]
	moved.file, moved.line, moved.bound = file, rawLine+1, rawLine+2
	if got := markersInside(v, gitrunPath, []taintMarker{moved}, "Raw", "Read"); len(got) != 1 {
		t.Errorf("a marker moved into Raw gave %v, want one finding", got)
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
