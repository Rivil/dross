package cmd

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os/exec"
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

// spawnLinesIn returns the file and line of each exec.Command or
// exec.CommandContext call inside the named functions — top-level or methods —
// of a view's package pkg ("" for every package).
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
				if !ok || !want[fd.Name.Name] || fd.Body == nil {
					continue
				}
				ast.Inspect(fd.Body, func(n ast.Node) bool {
					call, ok := n.(*ast.CallExpr)
					if !ok {
						return true
					}
					if obj := execCalleeObject(p.Info, call); obj != nil && obj.Pkg() != nil &&
						obj.Pkg().Path() == "os/exec" && (obj.Name() == "Command" || obj.Name() == "CommandContext") {
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
	trimLines := gitHelperSpawnLines(t, "Trim", "TrimWith")
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
	// One spawn each: Trim and Raw hand off to TrimWith and RawWith, so a
	// second spawn would be a second, unpinned way to run git.
	if len(trimLines) != 1 {
		t.Errorf("internal/gitrun holds %d Trim spawns, want exactly one: %v", len(trimLines), trimLines)
	}
	if raw := gitHelperSpawnLines(t, "Raw", "RawWith"); len(raw) != 1 {
		t.Errorf("internal/gitrun holds %d Raw spawns, want exactly one: %v", len(raw), raw)
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

// gitTrimArgv returns the git argv of a Trim call the pin judges — everything
// after the dir — and whether call is one: gitrun.Trim(dir, ...) and
// gitrun.TrimWith(opts, dir, ...) anywhere, or a bare Trim/TrimWith call inside
// package gitrun itself (ShortSHA's, which reads through Trim and so leans on
// its marker). The options value is not the pin's business; the argv is.
func gitTrimArgv(call *ast.CallExpr, inGitrun bool) ([]ast.Expr, bool) {
	var name string
	switch fn := call.Fun.(type) {
	case *ast.SelectorExpr:
		pkg, ok := fn.X.(*ast.Ident)
		if !ok || pkg.Name != "gitrun" {
			return nil, false
		}
		name = fn.Sel.Name
	case *ast.Ident:
		if !inGitrun {
			return nil, false
		}
		name = fn.Name
	default:
		return nil, false
	}
	switch {
	case name == "Trim" && len(call.Args) >= 2:
		return call.Args[1:], true
	case name == "TrimWith" && len(call.Args) >= 3:
		return call.Args[2:], true
	}
	return nil, false
}

// gitrunTrimForwarder reports the one call the pin skips: inside package
// gitrun, the package-level Trim handing its own variadic argv to TrimWith.
// Trim's callers are the sites; the forwarding carries no verb to judge.
func gitrunTrimForwarder(f *ast.File) map[*ast.CallExpr]bool {
	out := map[*ast.CallExpr]bool{}
	if f.Name.Name != "gitrun" {
		return out
	}
	for _, d := range f.Decls {
		fd, ok := d.(*ast.FuncDecl)
		if !ok || fd.Recv != nil || fd.Name.Name != "Trim" || fd.Body == nil {
			continue
		}
		ast.Inspect(fd.Body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || !call.Ellipsis.IsValid() {
				return true
			}
			if args, ok := gitTrimArgv(call, true); ok && len(args) == 1 {
				if _, ok := args[0].(*ast.Ident); ok {
					out[call] = true
				}
			}
			return true
		})
	}
	return out
}

// gitTrimProblems walks files for gitrun.Trim calls and reports every one whose
// verb or options fall outside gitTrimPinned, and how many sites it examined.
func gitTrimProblems(fset *token.FileSet, files []*ast.File) ([]string, int) {
	var out []string
	sites := 0
	for _, f := range files {
		inGitrun := f.Name.Name == "gitrun"
		skip := gitrunTrimForwarder(f)
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || skip[call] {
				return true
			}
			args, ok := gitTrimArgv(call, inGitrun)
			if !ok {
				return true
			}
			sites++
			if why := gitTrimArgvProblem(args, call.Ellipsis.IsValid()); why != "" {
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
func gitTrimArgvProblem(args []ast.Expr, spread bool) string {
	if b, ok := args[0].(*ast.CallExpr); ok && len(args) == 1 && spread {
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
		{`gitrun.TrimWith(gitrun.Options{Timeout: time.Second, NoOptionalLocks: true}, dir, gitRefArgs("for-each-ref", []string{"--format=%(contents)"}, "refs/heads/")...)`, "format atom `%(contents)`"},
		{`gitrun.TrimWith(opts, dir, "log", "--oneline")`, "runs `log`"},
		{`gitrun.TrimWith(opts, dir, "symbolic-ref", "--short", "HEAD")`, ""},
	}
	// Inside package gitrun a bare Trim is the runner's own and is judged.
	gfset := token.NewFileSet()
	gf, err := parser.ParseFile(gfset, "internal/gitrun/extra.go", "package gitrun\n\nfunc f() {\n\t_, _ = Trim(dir, \"log\", \"--oneline\")\n}\n", 0)
	if err != nil {
		t.Fatal(err)
	}
	if got, n := gitTrimProblems(gfset, []*ast.File{gf}); n != 1 || len(got) != 1 || !strings.Contains(got[0], "runs `log`") {
		t.Errorf("a bare Trim inside gitrun: examined %d, problems %v; want the log read reported", n, got)
	}
	// gitrun's own Trim forwarding its argv to TrimWith is skipped; the same
	// spread from any other function is a site it cannot resolve.
	ff, err := parser.ParseFile(gfset, "internal/gitrun/fwd.go", "package gitrun\n\n"+
		"func Trim(dir string, args ...string) (string, error) { return TrimWith(Options{}, dir, args...) }\n\n"+
		"func other(dir string, args ...string) (string, error) { return TrimWith(Options{}, dir, args...) }\n", 0)
	if err != nil {
		t.Fatal(err)
	}
	if got, n := gitTrimProblems(gfset, []*ast.File{ff}); n != 1 || len(got) != 1 || !strings.Contains(got[0], "fwd.go:5") {
		t.Errorf("the forwarder skip: examined %d, problems %v; want only other()'s spread reported", n, got)
	}
	// Outside package gitrun a bare Trim is someone else's function.
	of, err := parser.ParseFile(gfset, "internal/security/extra.go", "package security\n\nfunc f() {\n\t_, _ = Trim(dir, \"log\", \"--oneline\")\n}\n", 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, n := gitTrimProblems(gfset, []*ast.File{of}); n != 0 {
		t.Errorf("a bare Trim outside gitrun was examined (%d sites)", n)
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
var gitHelperNames = map[string]bool{"gitTrim": true, "gitRead": true, "gitRun": true, "gitNoOut": true, "gitBranchTrim": true}

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

// TestIsAncestorOverARealRepo: merge-base answers ancestry with its exit code,
// read through gitrun.ExitCode — 0 yes, 1 no, anything else an error.
func TestIsAncestorOverARealRepo(t *testing.T) {
	dir := t.TempDir()
	gitInit(t, dir, "")
	commitFile(t, dir, "a.go", "package a\n")
	mustGit(t, dir, "checkout", "-q", "-b", "side")
	commitFile(t, dir, "b.go", "package a\n")

	if ok, err := isAncestor(dir, "main", "side"); !ok || err != nil {
		t.Errorf("isAncestor(main, side) = %v, %v; want true, nil", ok, err)
	}
	if ok, err := isAncestor(dir, "side", "main"); ok || err != nil {
		t.Errorf("isAncestor(side, main) = %v, %v; want false, nil (exit 1)", ok, err)
	}
	if _, err := isAncestor(dir, "no-such-ref", "main"); err == nil {
		t.Error("isAncestor with a missing ref answered instead of failing")
	}
}

// TestPatchIDMatchesGitByHand: patchIDOfDiff pipes Raw's diff into patch-id
// through RawWith's stdin. Its id is the one `git diff | git patch-id
// --stable` gives by hand, over a two-file diff; an empty diff gives "".
func TestPatchIDMatchesGitByHand(t *testing.T) {
	dir := t.TempDir()
	gitInit(t, dir, "")
	commitFile(t, dir, "a.go", "package a\n")
	base := mustGit(t, dir, "rev-parse", "HEAD")
	mustWrite(t, filepath.Join(dir, "a.go"), "package a\n\nvar x = 1\n")
	mustWrite(t, filepath.Join(dir, "b.go"), "package a\n")
	mustGit(t, dir, "add", "a.go", "b.go")
	mustGit(t, dir, "commit", "-q", "-m", "two files")
	head := mustGit(t, dir, "rev-parse", "HEAD")

	got, err := patchIDOfDiff(dir, base, head)
	if err != nil {
		t.Fatal(err)
	}
	diff := exec.Command("git", "-C", dir, "diff", base, head)
	pid := exec.Command("git", "-C", dir, "patch-id", "--stable")
	pipe, err := diff.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	pid.Stdin = pipe
	if err := diff.Start(); err != nil {
		t.Fatal(err)
	}
	out, err := pid.Output()
	if err != nil {
		t.Fatal(err)
	}
	if err := diff.Wait(); err != nil {
		t.Fatal(err)
	}
	want := strings.Fields(string(out))
	if len(want) == 0 || got != want[0] {
		t.Errorf("patchIDOfDiff = %q, want %v by hand", got, want)
	}
	if got, err := patchIDOfDiff(dir, head, head); err != nil || got != "" {
		t.Errorf("an empty diff gave %q, %v; want \"\"", got, err)
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
	for _, f := range markersInside(v, gitrunPath, markers, "Raw", "RawWith", "Read") {
		t.Error(f)
	}
	if got := markersInside(v, gitrunPath, markers, "Trim", "TrimWith"); len(got) != 1 {
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
				if fd, ok := d.(*ast.FuncDecl); ok && fd.Name.Name == "RawWith" {
					pos := v.Fset.Position(fd.Body.Lbrace)
					rawLine, file = pos.Line, pos.Filename
				}
			}
		}
	}
	if rawLine == 0 {
		t.Fatal("found no gitrun.RawWith")
	}
	moved := markers[0]
	moved.file, moved.line, moved.bound = file, rawLine+1, rawLine+2
	if got := markersInside(v, gitrunPath, []taintMarker{moved}, "Raw", "RawWith", "Read"); len(got) != 1 {
		t.Errorf("a marker moved into RawWith gave %v, want one finding", got)
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
