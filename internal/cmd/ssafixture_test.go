package cmd

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"

	"golang.org/x/tools/go/callgraph"
	"golang.org/x/tools/go/callgraph/cha"
	"golang.org/x/tools/go/callgraph/vta"
	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/go/ssa"
	"golang.org/x/tools/go/ssa/ssautil"
	"golang.org/x/tools/txtar"
)

// Fixture programs. A source scan is proven on small .go.txt sources before it
// is trusted on the live tree, and those sources have to be real, type-checked
// Go: a taint engine reads types and SSA, not text.
//
// A fixture is type-checked against the SAME dependency types the shared load
// resolved (srcprog_test.go) — os/exec, cobra, dross's own packages — so it
// needs no second go list. It is then lowered into its OWN ssa.Program with its
// own CHA and VTA graphs. Never an overlay into the live load: a fixture's
// functions and types must not appear in the live call graph, or a fixture
// written to trip a scan could change a live verdict.

// fixtureModule prefixes every fixture package path; a fixture's package clause
// names the last element.
const fixtureModule = "example.com/fixture"

// srcView is what a source scan reads. The live program and every fixture
// program present the same shape, so a scan is one function over either.
type srcView struct {
	Fset *token.FileSet
	Prog *ssa.Program
	// Pkgs are the packages the scan examines — every module package for the
	// live view, the fixture's own packages for a fixture. Dependencies are in
	// Prog but not here.
	Pkgs []*srcPkg
	CHA  *callgraph.Graph
	VTA  *callgraph.Graph
}

// srcPkg is one examined package: syntax, types and its SSA package.
type srcPkg struct {
	Path   string
	Types  *types.Package
	Info   *types.Info
	Syntax []*ast.File
	SSA    *ssa.Package
}

var (
	liveViewOnce sync.Once
	liveViewVal  *srcView
)

// liveView is the shared source program as a srcView.
func liveView(t testing.TB) *srcView {
	t.Helper()
	p := sourceProgram(t)
	liveViewOnce.Do(func() {
		v := &srcView{Fset: p.Fset, Prog: p.Prog, CHA: p.CHA, VTA: p.VTA}
		for _, pkg := range p.Pkgs {
			v.Pkgs = append(v.Pkgs, &srcPkg{
				Path: pkg.PkgPath, Types: pkg.Types, Info: pkg.TypesInfo,
				Syntax: pkg.Syntax, SSA: p.SSA[pkg],
			})
		}
		liveViewVal = v
	})
	return liveViewVal
}

// fixtureSource is one fixture file. Name is what positions print: the .go.txt
// file name, or "archive.go.txt/section.go" for a section of a txtar archive.
type fixtureSource struct {
	Name string
	Src  []byte
}

// ssaFixture is a type-checked fixture lowered to SSA in its own program.
type ssaFixture struct {
	*srcView
	Srcs []fixtureSource
}

// isFixtureTestFile mirrors the live load's Tests:false. A fixture named like a
// test file is out, exactly as a _test.go file is out of the load.
func isFixtureTestFile(name string) bool {
	return strings.HasSuffix(name, "_test.go.txt") || strings.HasSuffix(name, "_test.go")
}

// readFixtureSources reads .go.txt files. A file holding a txtar archive
// contributes one source per section, which is how a single fixture carries
// several packages; any other file is one source.
func readFixtureSources(paths ...string) ([]fixtureSource, error) {
	var out []fixtureSource
	for _, path := range paths {
		base := filepath.Base(path)
		if isFixtureTestFile(base) {
			continue
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		ar := txtar.Parse(body)
		if len(ar.Files) == 0 {
			out = append(out, fixtureSource{Name: base, Src: body})
			continue
		}
		for _, f := range ar.Files {
			if isFixtureTestFile(f.Name) {
				continue
			}
			out = append(out, fixtureSource{Name: base + "/" + f.Name, Src: f.Data})
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no fixture sources in %v", paths)
	}
	return out, nil
}

var (
	liveImportsOnce sync.Once
	liveImportsVal  map[string]*packages.Package
)

// liveImportable maps import path to every package in the live load's graph
// whose types are complete — the module's own packages and everything they
// import directly. An indirect dependency the module never names is not
// importable by a fixture either.
func liveImportable(t testing.TB) map[string]*packages.Package {
	t.Helper()
	p := sourceProgram(t)
	liveImportsOnce.Do(func() {
		m := map[string]*packages.Package{}
		packages.Visit(p.Pkgs, nil, func(pkg *packages.Package) {
			if pkg.Types != nil && pkg.Types.Complete() {
				m[pkg.PkgPath] = pkg
			}
		})
		liveImportsVal = m
	})
	return liveImportsVal
}

// loadFixture reads and builds a fixture, failing the test on any error.
func loadFixture(t testing.TB, paths ...string) *ssaFixture {
	t.Helper()
	srcs, err := readFixtureSources(paths...)
	if err != nil {
		t.Fatal(err)
	}
	fx, err := typecheckFixture(liveImportable(t), srcs)
	if err != nil {
		t.Fatal(err)
	}
	return fx
}

// fixturePath is testdata/<dir>/<name> under this package.
func fixturePath(dir, name string) string {
	return filepath.Join("testdata", dir, name)
}

// typecheckFixture groups sources by package clause, type-checks each package
// through an importer that resolves sibling fixture packages first and the live
// load's graph second, and builds a separate SSA program over the result. Any
// parse or type error is returned — no SSA is ever built from ill-typed source,
// because a scan over a hole reads it as "nothing here".
func typecheckFixture(live map[string]*packages.Package, srcs []fixtureSource) (*ssaFixture, error) {
	fset := token.NewFileSet()
	byPkg := map[string][]*ast.File{}
	for _, s := range srcs {
		f, err := parser.ParseFile(fset, s.Name, s.Src, parser.ParseComments)
		if err != nil {
			return nil, fmt.Errorf("fixture does not parse: %w", err)
		}
		path := fixtureModule + "/" + f.Name.Name
		byPkg[path] = append(byPkg[path], f)
	}

	order, err := fixtureOrder(byPkg)
	if err != nil {
		return nil, err
	}

	checked := map[string]*srcPkg{}
	importer := importerFunc(func(path string) (*types.Package, error) {
		if p, ok := checked[path]; ok {
			return p.Types, nil
		}
		if p, ok := live[path]; ok {
			return p.Types, nil
		}
		return nil, fmt.Errorf("fixture imports %q, which is neither a fixture package nor in the live load's import graph", path)
	})
	for _, path := range order {
		var errs []string
		conf := &types.Config{
			Importer: importer,
			Error: func(err error) {
				if te, ok := err.(types.Error); ok {
					errs = append(errs, fmt.Sprintf("%s: %s", te.Fset.Position(te.Pos), te.Msg))
					return
				}
				errs = append(errs, err.Error())
			},
		}
		info := &types.Info{
			Types:        map[ast.Expr]types.TypeAndValue{},
			Instances:    map[*ast.Ident]types.Instance{},
			Defs:         map[*ast.Ident]types.Object{},
			Uses:         map[*ast.Ident]types.Object{},
			Implicits:    map[ast.Node]types.Object{},
			Selections:   map[*ast.SelectorExpr]*types.Selection{},
			Scopes:       map[ast.Node]*types.Scope{},
			FileVersions: map[*ast.File]string{},
		}
		tpkg, _ := conf.Check(path, fset, byPkg[path], info)
		if len(errs) > 0 {
			return nil, fmt.Errorf("fixture package %s does not type-check:\n  %s", path, strings.Join(errs, "\n  "))
		}
		checked[path] = &srcPkg{Path: path, Types: tpkg, Info: info, Syntax: byPkg[path]}
	}

	prog := ssa.NewProgram(fset, ssa.InstantiateGenerics)
	// Every live package a fixture reaches, transitively, gets a body-less SSA
	// package: the builder needs a Package for any object it references, and
	// none of those bodies belongs to the fixture.
	var roots []*packages.Package
	for _, path := range order {
		for _, imp := range checked[path].Types.Imports() {
			if p, ok := live[imp.Path()]; ok {
				roots = append(roots, p)
			}
		}
	}
	packages.Visit(roots, nil, func(p *packages.Package) {
		if p.Types != nil {
			prog.CreatePackage(p.Types, nil, nil, true)
		}
	})
	v := &srcView{Fset: fset, Prog: prog}
	for _, path := range order {
		sp := checked[path]
		sp.SSA = prog.CreatePackage(sp.Types, sp.Syntax, sp.Info, true)
		v.Pkgs = append(v.Pkgs, sp)
	}
	prog.Build()
	v.CHA = cha.CallGraph(prog)
	v.VTA = vta.CallGraph(ssautil.AllFunctions(prog), v.CHA)
	return &ssaFixture{srcView: v, Srcs: srcs}, nil
}

// fixtureOrder sorts fixture packages so each is type-checked after the
// sibling packages it imports; the input file order does not matter.
func fixtureOrder(byPkg map[string][]*ast.File) ([]string, error) {
	paths := make([]string, 0, len(byPkg))
	for p := range byPkg {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	var order []string
	state := map[string]int{} // 0 new, 1 visiting, 2 done
	var visit func(string) error
	visit = func(p string) error {
		switch state[p] {
		case 1:
			return fmt.Errorf("fixture packages import each other in a cycle through %s", p)
		case 2:
			return nil
		}
		state[p] = 1
		for _, f := range byPkg[p] {
			for _, imp := range f.Imports {
				dep, _ := strconv.Unquote(imp.Path.Value)
				if _, ok := byPkg[dep]; ok {
					if err := visit(dep); err != nil {
						return err
					}
				}
			}
		}
		state[p] = 2
		order = append(order, p)
		return nil
	}
	for _, p := range paths {
		if err := visit(p); err != nil {
			return nil, err
		}
	}
	return order, nil
}

type importerFunc func(path string) (*types.Package, error)

func (f importerFunc) Import(path string) (*types.Package, error) { return f(path) }

// TestFixtureTypeChecksOnTheSharedGraph: a two-package fixture reaches os/exec,
// cobra and a sibling fixture package, in either file order. An importer that
// saw only the standard library would fail on cobra.
func TestFixtureTypeChecksOnTheSharedGraph(t *testing.T) {
	root := fixturePath("ssa_fixture", "root.go.txt")
	helper := fixturePath("ssa_fixture", "helper.go.txt")
	for _, order := range [][]string{{root, helper}, {helper, root}} {
		fx := loadFixture(t, order...)
		var got []string
		for _, p := range fx.Pkgs {
			got = append(got, p.Path)
		}
		want := []string{fixtureModule + "/helperpkg", fixtureModule + "/root"}
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("order %v: fixture packages = %v, want %v (dependency first)", order, got, want)
		}
		rootPkg := fx.Pkgs[1].Types
		imports := map[string]bool{}
		for _, imp := range rootPkg.Imports() {
			imports[imp.Path()] = true
		}
		for _, p := range []string{"os/exec", "github.com/spf13/cobra", fixtureModule + "/helperpkg"} {
			if !imports[p] {
				t.Errorf("order %v: root does not import %s (imports %v)", order, p, imports)
			}
		}
		if fn := fx.Pkgs[1].SSA.Func("Command"); fn == nil || fn.Blocks == nil {
			t.Errorf("order %v: root.Command has no SSA body", order)
		}
	}
}

// TestBadFixturesFailLoudly: a missing import and a type error are errors that
// name what is wrong — never a skipped fixture, and never SSA built from
// ill-typed source.
func TestBadFixturesFailLoudly(t *testing.T) {
	live := liveImportable(t)
	for _, tc := range []struct{ file, want string }{
		{"badimport.go.txt", `"example.com/nowhere/missing"`},
		{"typeerror.go.txt", "typeerror.go.txt:"},
	} {
		srcs, err := readFixtureSources(fixturePath("ssa_fixture", tc.file))
		if err != nil {
			t.Fatal(err)
		}
		fx, err := typecheckFixture(live, srcs)
		if err == nil {
			t.Errorf("%s built (%d packages) — it must fail", tc.file, len(fx.Pkgs))
			continue
		}
		if !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: error %q does not name %s", tc.file, err, tc.want)
		}
		if tc.file == "typeerror.go.txt" && !strings.Contains(err.Error(), "typeerror.go.txt:6:") {
			t.Errorf("%s: error %q does not name the offending line 6", tc.file, err)
		}
	}
}

// TestFixtureIsolation: fixture builds stay out of the live program. A
// _test.go.txt source is excluded like a _test.go file is from the load, a
// build never runs a second load, and no fixture function ever appears in the
// live call graph.
func TestFixtureIsolation(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "iso.go.txt"), "package iso\n\nfunc Kept() {}\n")
	mustWrite(t, filepath.Join(dir, "iso_test.go.txt"), "package iso\n\nfunc Dropped() {}\n")
	mustWrite(t, filepath.Join(dir, "multi.go.txt"),
		"-- a.go --\npackage iso\n\nfunc FromSection() {}\n-- a_test.go --\npackage iso\n\nfunc SectionDropped() {}\n")

	live := liveView(t)
	loadsBefore := srcProgLoads.Load()
	fx := loadFixture(t, filepath.Join(dir, "iso.go.txt"), filepath.Join(dir, "iso_test.go.txt"),
		filepath.Join(dir, "multi.go.txt"))
	if n := srcProgLoads.Load(); n != loadsBefore {
		t.Errorf("building a fixture moved the load counter %d -> %d — fixtures must never load", loadsBefore, n)
	}
	scope := fx.Pkgs[0].Types.Scope()
	for name, want := range map[string]bool{"Kept": true, "FromSection": true, "Dropped": false, "SectionDropped": false} {
		if got := scope.Lookup(name) != nil; got != want {
			t.Errorf("fixture declares %s = %v, want %v", name, got, want)
		}
	}

	root := loadFixture(t, fixturePath("ssa_fixture", "root.go.txt"), fixturePath("ssa_fixture", "helper.go.txt"))
	if root.Prog == live.Prog {
		t.Fatal("the fixture shares the live ssa.Program")
	}
	for fn := range live.VTA.Nodes {
		if fn != nil && fn.Pkg != nil && strings.HasPrefix(fn.Pkg.Pkg.Path(), fixtureModule) {
			t.Errorf("fixture function %s is in the live call graph", fn)
		}
	}
	for _, p := range live.Prog.AllPackages() {
		if strings.HasPrefix(p.Pkg.Path(), fixtureModule) {
			t.Errorf("fixture package %s is in the live program", p.Pkg.Path())
		}
	}
}

// TestFixtureCycleIsAnError: sibling packages that import each other cannot be
// ordered, and the builder says so instead of recursing.
func TestFixtureCycleIsAnError(t *testing.T) {
	srcs := []fixtureSource{
		{Name: "a.go.txt", Src: []byte("package a\n\nimport _ \"example.com/fixture/b\"\n")},
		{Name: "b.go.txt", Src: []byte("package b\n\nimport _ \"example.com/fixture/a\"\n")},
	}
	if _, err := typecheckFixture(liveImportable(t), srcs); err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Errorf("typecheckFixture = %v, want a cycle error", err)
	}
	if _, err := readFixtureSources(filepath.Join(t.TempDir(), "only_test.go.txt")); err == nil {
		t.Error("a path list holding only test fixtures read as a fixture")
	}
}
