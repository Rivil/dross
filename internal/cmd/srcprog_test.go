package cmd

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/tools/go/callgraph"
	"golang.org/x/tools/go/callgraph/cha"
	"golang.org/x/tools/go/callgraph/vta"
	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/go/ssa"
	"golang.org/x/tools/go/ssa/ssautil"
)

// The shared source program: the whole module, type-checked from source and
// lowered to SSA once per test binary, for every scan that reasons about
// dross's own code (exec taint, path-field taint, exec-consent reach).
//
// Loading is the expensive part — go list, type-checking every module package
// from syntax, SSA construction and the VTA call graph — so it happens behind
// one sync.Once. A second loader would double the cost silently; srcProgLoads
// counts real loads and TestMain fails the binary when it reads more than one,
// and TestOnePackagesLoadCallSite pins that packages.Load has exactly one call
// site in this package's tests.
//
// golang.org/x/tools is imported from _test.go files only (the phase's
// loader_mechanism lock): it never links into the shipped binary, which
// TestSourceScansStayOutOfTheBinary pins.

// ambientEnv is the environment this binary inherited, captured at package
// initialisation — before TestMain swaps HOME for a throwaway dir and before
// any test's t.Setenv. The loader runs `go list` under it so the go command
// finds the real build and module caches: under TestMain's empty HOME every
// dependency's export data would be rebuilt cold, and the module cache would
// be empty.
var ambientEnv = os.Environ()

// srcProgram is the loaded module: syntax and types for every package under
// modulePath, export data for everything else, SSA bodies for module packages
// only, and one VTA call graph over all of it.
type srcProgram struct {
	Root string         // module root (the directory holding go.mod)
	Env  []string       // the environment packages.Load ran under
	Fset *token.FileSet // shared by every module package's syntax
	// Pkgs is every package the load matched, sorted by PkgPath. Tests:false,
	// so these are the non-test variants only.
	Pkgs  []*packages.Package
	Prog  *ssa.Program
	SSA   map[*packages.Package]*ssa.Package
	CHA   *callgraph.Graph // the seed VTA refined; kept for unresolved-dispatch reporting
	VTA   *callgraph.Graph
	Phase srcLoadTiming
}

// srcLoadTiming is the cost of each loading stage, kept so the c-8 budget can
// be read off a run rather than estimated.
type srcLoadTiming struct {
	Load, SSA, CHA, VTA time.Duration
}

func (t srcLoadTiming) total() time.Duration { return t.Load + t.SSA + t.CHA + t.VTA }

// srcLoadMode asks for syntax and full types info on the matched packages;
// with NeedDeps unset, go/packages reads every dependency from export data
// instead of type-checking it from source.
const srcLoadMode = packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles |
	packages.NeedImports | packages.NeedTypes | packages.NeedTypesInfo | packages.NeedSyntax |
	packages.NeedModule

var (
	srcProgOnce  sync.Once
	srcProg      *srcProgram
	srcProgErr   error
	srcProgLoads atomic.Int32
)

// sourceProgram returns the shared program, loading it on first use. A load
// failure is fatal to every caller: a scan over a partial program would pass
// on whatever it failed to see.
func sourceProgram(t testing.TB) *srcProgram {
	t.Helper()
	srcProgOnce.Do(func() { srcProg, srcProgErr = loadSourceProgram() })
	if srcProgErr != nil {
		t.Fatalf("loading the source program: %v", srcProgErr)
	}
	return srcProg
}

// srcLoadCountErr is TestMain's once-per-binary check: more than one real load
// means a second loader bypassed the sync.Once.
func srcLoadCountErr(n int32) error {
	if n > 1 {
		return fmt.Errorf("source program loaded %d times — the load is once per test binary (sourceProgram in srcprog_test.go)", n)
	}
	return nil
}

func loadSourceProgram() (*srcProgram, error) {
	srcProgLoads.Add(1)

	root, err := findModuleRoot()
	if err != nil {
		return nil, err
	}
	env, err := loaderEnv()
	if err != nil {
		return nil, err
	}
	p := &srcProgram{Root: root, Env: env, Fset: token.NewFileSet()}

	start := time.Now()
	cfg := &packages.Config{
		Mode:  srcLoadMode,
		Dir:   root,
		Env:   env,
		Fset:  p.Fset,
		Tests: false,
	}
	pkgs, err := packages.Load(cfg, "./...")
	if err != nil {
		return nil, fmt.Errorf("packages.Load: %w", err)
	}
	if err := loadErrors(pkgs); err != nil {
		return nil, err
	}
	sort.Slice(pkgs, func(i, j int) bool { return pkgs[i].PkgPath < pkgs[j].PkgPath })
	p.Pkgs = pkgs
	p.Phase.Load = time.Since(start)

	// ssautil.Packages creates SSA packages for the whole import graph but
	// hands syntax only to the matched packages, so Build constructs bodies for
	// module code and leaves every dependency function external (no Blocks).
	// InstantiateGenerics is what VTA requires of its input.
	start = time.Now()
	prog, ssaPkgs := ssautil.Packages(pkgs, ssa.InstantiateGenerics)
	p.SSA = make(map[*packages.Package]*ssa.Package, len(pkgs))
	for i, sp := range ssaPkgs {
		if sp == nil {
			return nil, fmt.Errorf("no SSA package for %s (ill-typed)", pkgs[i].PkgPath)
		}
		p.SSA[pkgs[i]] = sp
	}
	prog.Build()
	p.Prog = prog
	p.Phase.SSA = time.Since(start)

	start = time.Now()
	p.CHA = cha.CallGraph(prog)
	p.Phase.CHA = time.Since(start)

	start = time.Now()
	p.VTA = vta.CallGraph(ssautil.AllFunctions(prog), p.CHA)
	p.Phase.VTA = time.Since(start)
	return p, nil
}

// loaderEnv is ambientEnv with the build and module caches pinned to where the
// ambient go command keeps them, and the module proxy switched off: the load
// reads what the test build already fetched and never reaches the network.
func loaderEnv() ([]string, error) {
	cmd := exec.Command("go", "env", "GOCACHE", "GOMODCACHE")
	cmd.Env = ambientEnv
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("go env GOCACHE GOMODCACHE under the ambient environment: %w", err)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) != 2 || lines[0] == "" || lines[1] == "" {
		return nil, fmt.Errorf("go env GOCACHE GOMODCACHE: want two non-empty lines, got %q", out)
	}
	env := withEnvVar(ambientEnv, "GOCACHE", lines[0])
	env = withEnvVar(env, "GOMODCACHE", lines[1])
	return withEnvVar(env, "GOPROXY", "off"), nil
}

// withEnvVar returns a copy of env with key set to val, replacing every
// existing entry for key.
func withEnvVar(env []string, key, val string) []string {
	out := make([]string, 0, len(env)+1)
	for _, kv := range env {
		if !strings.HasPrefix(kv, key+"=") {
			out = append(out, kv)
		}
	}
	return append(out, key+"="+val)
}

// envValue reads key from an environment slice; the last entry wins, as it
// does for exec.
func envValue(env []string, key string) (string, bool) {
	val, ok := "", false
	for _, kv := range env {
		if v, found := strings.CutPrefix(kv, key+"="); found {
			val, ok = v, true
		}
	}
	return val, ok
}

// findModuleRoot walks up from the working directory to the dir holding go.mod.
func findModuleRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("no go.mod above the test's working directory")
		}
		dir = parent
	}
}

// loadErrors gathers every list, parse and type error anywhere in the loaded
// graph. Any one is fatal: a package that failed to type-check has holes in
// its TypesInfo, and a scan would read those holes as "nothing here".
func loadErrors(pkgs []*packages.Package) error {
	var msgs []string
	packages.Visit(pkgs, nil, func(p *packages.Package) {
		for _, e := range p.Errors {
			msgs = append(msgs, p.PkgPath+": "+e.Error())
		}
		for _, e := range p.TypeErrors {
			msgs = append(msgs, p.PkgPath+": type error: "+e.Error())
		}
	})
	if len(msgs) > 0 {
		return fmt.Errorf("%d package error(s) in the source load:\n  %s", len(msgs), strings.Join(msgs, "\n  "))
	}
	return nil
}

// inModule reports whether an import path belongs to this module.
func inModule(path string) bool {
	return path == modulePath || strings.HasPrefix(path, modulePath+"/")
}

// TestSourceProgramLoadsOnce pins the once-per-binary counter and the message
// TestMain prints when it trips.
func TestSourceProgramLoadsOnce(t *testing.T) {
	sourceProgram(t)
	sourceProgram(t)
	if n := srcProgLoads.Load(); n != 1 {
		t.Errorf("srcProgLoads = %d after two sourceProgram calls, want 1", n)
	}
	if err := srcLoadCountErr(1); err != nil {
		t.Errorf("srcLoadCountErr(1) = %v, want nil", err)
	}
	err := srcLoadCountErr(2)
	if err == nil || !strings.Contains(err.Error(), "source program loaded 2 times") {
		t.Errorf("srcLoadCountErr(2) = %v, want it to say \"source program loaded 2 times\"", err)
	}
}

// TestOnePackagesLoadCallSite keeps the load behind its sync.Once: exactly one
// reference to packages.Load across this package's test files, so a helper
// that loads for itself — the way to double the suite's load cost without
// anyone noticing — fails here naming its file:line.
func TestOnePackagesLoadCallSite(t *testing.T) {
	sites, err := packagesLoadSites(".")
	if err != nil {
		t.Fatal(err)
	}
	if len(sites) != 1 || !strings.HasPrefix(sites[0], "srcprog_test.go:") {
		t.Errorf("packages.Load referenced at %v — want exactly one site, in srcprog_test.go (sourceProgram's loader)", sites)
	}
}

// TestPackagesLoadSitesSeesAliases proves the walk behind
// TestOnePackagesLoadCallSite finds a load under a renamed import and as a
// function value, and ignores a same-named Load from another package.
func TestPackagesLoadSitesSeesAliases(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "a_test.go"), `package cmd

import pk "golang.org/x/tools/go/packages"

var f = pk.Load

func g() { pk.Load(nil) }
`)
	mustWrite(t, filepath.Join(dir, "b_test.go"), `package cmd

import packages "example.com/other"

func h() { packages.Load(nil) }
`)
	mustWrite(t, filepath.Join(dir, "c.go"), `package cmd

import "golang.org/x/tools/go/packages"

func i() { packages.Load(nil) }
`)
	sites, err := packagesLoadSites(dir)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"a_test.go:5", "a_test.go:7"}; strings.Join(sites, ",") != strings.Join(want, ",") {
		t.Errorf("sites = %v, want %v", sites, want)
	}
}

// packagesLoadSites lists file:line for every reference to
// golang.org/x/tools/go/packages.Load in dir's _test.go files, resolving the
// import name per file.
func packagesLoadSites(dir string) ([]string, error) {
	matches, err := filepath.Glob(filepath.Join(dir, "*_test.go"))
	if err != nil {
		return nil, err
	}
	fset := token.NewFileSet()
	var sites []string
	for _, path := range matches {
		f, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			return nil, err
		}
		name := ""
		for _, imp := range f.Imports {
			if p, _ := strconv.Unquote(imp.Path.Value); p == "golang.org/x/tools/go/packages" {
				name = "packages"
				if imp.Name != nil {
					name = imp.Name.Name
				}
			}
		}
		if name == "" {
			continue
		}
		ast.Inspect(f, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "Load" {
				return true
			}
			if x, ok := sel.X.(*ast.Ident); ok && x.Name == name {
				pos := fset.Position(sel.Pos())
				sites = append(sites, fmt.Sprintf("%s:%d", filepath.Base(pos.Filename), pos.Line))
			}
			return true
		})
	}
	sort.Strings(sites)
	return sites, nil
}

// TestSharedLoadUsesAmbientGoCaches pins the loader's environment: the go
// caches are the ones the ambient HOME resolves to — never paths under
// TestMain's throwaway HOME, where every dependency would rebuild cold — and
// the proxy is off, so the load reads only what the test build fetched.
func TestSharedLoadUsesAmbientGoCaches(t *testing.T) {
	p := sourceProgram(t)

	cmd := exec.Command("go", "env", "GOCACHE", "GOMODCACHE")
	cmd.Env = withEnvVar(os.Environ(), "HOME", ambientHome)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go env under the ambient HOME: %v", err)
	}
	want := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(want) != 2 {
		t.Fatalf("go env GOCACHE GOMODCACHE printed %q", out)
	}
	testHome := os.Getenv("HOME")
	for i, key := range []string{"GOCACHE", "GOMODCACHE"} {
		got, ok := envValue(p.Env, key)
		if !ok {
			t.Errorf("the loader's Config.Env does not set %s", key)
			continue
		}
		if got != want[i] {
			t.Errorf("loader %s = %q, want the ambient %q", key, got, want[i])
		}
		if testHome != "" && strings.HasPrefix(got, testHome) {
			t.Errorf("loader %s = %q sits under TestMain's throwaway HOME %q", key, got, testHome)
		}
	}
	if got, _ := envValue(p.Env, "HOME"); got != ambientHome {
		t.Errorf("loader HOME = %q, want the ambient %q", got, ambientHome)
	}
	if got, _ := envValue(p.Env, "GOPROXY"); got != "off" {
		t.Errorf("loader GOPROXY = %q, want off — the load must not depend on the network", got)
	}
	if len(p.Pkgs) == 0 {
		t.Error("the load under GOPROXY=off matched no packages")
	}
}

// TestLoadErrorsNamesTheBrokenPackage keeps loadErrors fail-closed: a single
// type error anywhere in the graph comes back as an error naming its package.
func TestLoadErrorsNamesTheBrokenPackage(t *testing.T) {
	dep := &packages.Package{PkgPath: "example.com/broken", TypeErrors: []types.Error{{Msg: "undefined: x"}}}
	root := &packages.Package{PkgPath: "example.com/root", Imports: map[string]*packages.Package{"example.com/broken": dep}}
	err := loadErrors([]*packages.Package{root})
	if err == nil || !strings.Contains(err.Error(), "example.com/broken") || !strings.Contains(err.Error(), "undefined: x") {
		t.Errorf("loadErrors = %v, want an error naming example.com/broken and its type error", err)
	}
	listErr := &packages.Package{PkgPath: "example.com/unlisted", Errors: []packages.Error{{Msg: "no Go files"}}}
	if err := loadErrors([]*packages.Package{listErr}); err == nil || !strings.Contains(err.Error(), "example.com/unlisted") {
		t.Errorf("loadErrors = %v, want an error naming example.com/unlisted", err)
	}
	if err := loadErrors([]*packages.Package{{PkgPath: "example.com/clean"}}); err != nil {
		t.Errorf("loadErrors on a clean package = %v", err)
	}
}

// TestSourceProgramIsComplete checks the live load: no errors anywhere, every
// matched package carries syntax and full types info, and none of it came
// from a _test.go file.
func TestSourceProgramIsComplete(t *testing.T) {
	p := sourceProgram(t)
	if err := loadErrors(p.Pkgs); err != nil {
		t.Fatal(err)
	}
	for _, pkg := range p.Pkgs {
		if !inModule(pkg.PkgPath) {
			t.Errorf("matched package %s is outside %s", pkg.PkgPath, modulePath)
		}
		if pkg.Types == nil || pkg.TypesInfo == nil || len(pkg.Syntax) == 0 {
			t.Errorf("%s: Types=%v TypesInfo=%v Syntax=%d — every matched package needs all three",
				pkg.PkgPath, pkg.Types != nil, pkg.TypesInfo != nil, len(pkg.Syntax))
		}
		for _, f := range pkg.CompiledGoFiles {
			if strings.HasSuffix(f, "_test.go") {
				t.Errorf("%s: test file %s entered the load (Tests must be false)", pkg.PkgPath, f)
			}
		}
	}
	t.Logf("source program: %d packages; load %v, ssa %v, cha %v, vta %v (total %v)",
		len(p.Pkgs), p.Phase.Load, p.Phase.SSA, p.Phase.CHA, p.Phase.VTA, p.Phase.total())
}

// TestSSABodiesAreModuleOnly keeps the dominant cost bounded: SSA bodies are
// built for this module's functions only. A load that also lowered its
// dependencies would multiply SSA and VTA time under -race.
func TestSSABodiesAreModuleOnly(t *testing.T) {
	p := sourceProgram(t)
	built := 0
	for fn := range ssautil.AllFunctions(p.Prog) {
		if fn.Blocks == nil {
			continue
		}
		if fn.Pkg == nil {
			// Shared synthetic wrappers (bound methods, interface thunks,
			// promoted-method wrappers) belong to no package; their one-block
			// bodies are built wherever they are used.
			if fn.Synthetic == "" {
				t.Errorf("%s has a body but no package and is not synthetic", fn)
			}
			continue
		}
		if path := fn.Pkg.Pkg.Path(); !inModule(path) {
			t.Errorf("%s has an SSA body but belongs to %s — dependency bodies must stay unbuilt", fn, path)
			continue
		}
		built++
	}
	if built == 0 {
		t.Error("no module function has an SSA body — Build never ran")
	}
}

// TestSourceScansStayOutOfTheBinary holds the loader_mechanism lock:
// golang.org/x/tools is a test-only import. No non-test file may import it, and
// it must be absent from cmd/dross's import closure.
func TestSourceScansStayOutOfTheBinary(t *testing.T) {
	p := sourceProgram(t)
	const tools = "golang.org/x/tools"
	isTools := func(path string) bool { return path == tools || strings.HasPrefix(path, tools+"/") }

	for _, pkg := range p.Pkgs {
		for _, f := range pkg.Syntax {
			for _, imp := range f.Imports {
				if path, _ := strconv.Unquote(imp.Path.Value); isTools(path) {
					t.Errorf("%s imports %s — x/tools is for _test.go files only", p.Fset.Position(imp.Pos()), path)
				}
			}
		}
	}

	var bin *packages.Package
	for _, pkg := range p.Pkgs {
		if pkg.PkgPath == modulePath+"/cmd/dross" {
			bin = pkg
		}
	}
	if bin == nil {
		t.Fatal("cmd/dross is not in the source load")
	}
	closure := 0
	packages.Visit([]*packages.Package{bin}, func(pkg *packages.Package) bool {
		closure++
		if isTools(pkg.PkgPath) {
			t.Errorf("%s is in cmd/dross's import closure", pkg.PkgPath)
		}
		return true
	}, nil)
	if closure < 20 {
		t.Errorf("cmd/dross's import closure has %d packages — the walk is not seeing the graph", closure)
	}
}
