package cmd

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// The residual guard behind c-1 and c-4's residual clause.
//
// The type system carries the guarantee wherever a path is held as a
// pathfence.Contained: a caller that skipped the check has no value to pass.
// Two gaps are left that no type can close, and this file is both of them.
//
//   (a) UNWRAP. Contained.String() returns the joined absolute form, so
//       os.ReadFile(c.String()) WORKS. Nothing but a scan catches a site
//       quietly bypassing the audited seam. Contained.Rel() is worse: it
//       resolves against the process working directory, so the call opens the
//       wrong file. Both accessors are banned inside an os.* argument.
//
//   (b) LITERAL. A second ..-prefix implementation reintroduced anywhere
//       outside internal/pathfence is the exact thing this phase removed.
//
// WHAT THIS DOES NOT CATCH, so c-4 is not read as wider than it is: a brand-new
// consumer that loads a declared path field and calls os.* on the RAW string,
// never touching Contained at all. Deciding that an os.* argument traces back
// to a declared field is dataflow over resolved types — go/types, not go/ast —
// and it is filed as a deferred item against secret-detection.
//
// Every must-trip and must-not-trip case is a testdata fixture rather than an
// assertion about the live tree. A scan calibrated on its own post-fix output
// proves only that it was written after the fix.

// scannedPackages are the packages the unwrap ban covers. internal/security and
// internal/quality are in the set because t-3 moved the run-dir file operations
// into them: a set that stopped at six would miss the exact sites this phase
// newly contains, which is where the residual clause has to bite hardest.
var scannedPackages = []string{
	"../cmd", "../verify", "../phase", "../changes",
	"../testlane", "../remote", "../security", "../quality",
}

// pathMarkers are the tokens that make a ".." literal a PATH comparison rather
// than a git rev-range or a ref-name rule. Matched within the enclosing
// function, so a marker elsewhere in a large file cannot implicate an unrelated
// literal.
var pathMarkers = map[string]bool{
	"filepath.Separator": true, "os.PathSeparator": true,
	"path.Clean": true, "path.Base": true,
	"filepath.Rel": true, "filepath.IsAbs": true, "filepath.Clean": true,
}

// osVerbs are the calls that actually touch the filesystem.
var osVerbs = map[string]bool{
	"Stat": true, "Open": true, "OpenFile": true,
	"ReadFile": true, "WriteFile": true, "Remove": true, "RemoveAll": true,
	"Create": true, "MkdirAll": true,
}

// --- (a) the unwrap ban -----------------------------------------------------

// TestNoUnwrappedPathReachesAnOSCall runs the ban over the live tree.
func TestNoUnwrappedPathReachesAnOSCall(t *testing.T) {
	visited := map[string]int{}
	for _, dir := range scannedPackages {
		for _, f := range goSourcesIn(t, dir) {
			visited[dir]++
			for _, hit := range findUnwrapCalls(t, f.name, f.src) {
				t.Errorf("%s: %s — route it through the pathfence seam "+
					"(pathfence.ReadFile / WriteFile / Stat), or assign the string to a "+
					"variable before the call if the value is not a Contained", f.name, hit)
			}
		}
	}
	// Vacuity: a walk that stopped visiting would report nothing and look green.
	for _, dir := range scannedPackages {
		if visited[dir] == 0 {
			t.Errorf("the unwrap walk visited no non-test file in %s — it has stopped "+
				"seeing that package and every assertion over it is vacuous", dir)
		}
	}
}

// TestUnwrapScanTripsOnItsFixtures is the walker self-test. Without it the ban
// above passes on an empty tree exactly as it passes on a clean one.
func TestUnwrapScanTripsOnItsFixtures(t *testing.T) {
	for _, tc := range []struct {
		fixture string
		want    []string // the accessor+verb pairs, in source order
	}{
		{"unwrap_os.go.txt", []string{
			"os.ReadFile(...String()...)",
			"os.ReadFile(...Rel()...)",
			"os.Stat(...String()...)",
			"os.Remove(...Rel()...)",
			"os.WriteFile(...String()...)",
		}},
		{"rundir_revert.go.txt", []string{
			"os.WriteFile(...String()...)",
			"os.WriteFile(...String()...)",
			"os.WriteFile(...String()...)",
		}},
		{"seam_ok.go.txt", nil},
	} {
		t.Run(tc.fixture, func(t *testing.T) {
			got := findUnwrapCalls(t, tc.fixture, readFixture(t, tc.fixture))
			if len(got) != len(tc.want) {
				t.Fatalf("%s reported %d findings, want %d:\n%s",
					tc.fixture, len(got), len(tc.want), strings.Join(got, "\n"))
			}
			for i, want := range tc.want {
				if !strings.Contains(got[i], want) {
					t.Errorf("finding %d = %q, want it to name %q", i, got[i], want)
				}
			}
		})
	}
}

// TestUnwrapScanSkipsTestFiles pins the non-test-only rule. The live
// strings.Builder at internal/cmd/mutation_remote_wiring_test.go:47 is an
// instance of the banned shape, so a walk that included tests would be red on
// arrival — and the type-blind ban would then be unadoptable rather than
// fail-closed.
//
// It reuses the must-trip fixture under a _test.go NAME, so the two cases
// differ in exactly one thing: the filename.
func TestUnwrapScanSkipsTestFiles(t *testing.T) {
	src := readFixture(t, "unwrap_os.go.txt")

	if got := findUnwrapCalls(t, "unwrap_os.go", src); len(got) == 0 {
		t.Fatal("the fixture reports nothing under a non-test name — this comparison measures nothing")
	}
	if got := scanTree(t, "unwrap_os_test.go", src); len(got) != 0 {
		t.Errorf("the same source under a _test.go name still reported %v", got)
	}
}

// --- (b) the literal ban ----------------------------------------------------

// TestNoSecondDotDotImplementation runs the literal scan over the live tree.
func TestNoSecondDotDotImplementation(t *testing.T) {
	for _, dir := range scannedPackages {
		for _, f := range goSourcesIn(t, dir) {
			for _, hit := range findPathDotDot(t, f.name, f.src) {
				t.Errorf("%s: %s — containment is internal/pathfence's job; "+
					"call pathfence.Contain, InTree or Segment instead of testing \"..\" here",
					f.name, hit)
			}
		}
	}
}

// TestDotDotScanTripsOnItsFixtures self-tests the literal scan against the two
// shapes this phase removed and the two it must leave alone.
func TestDotDotScanTripsOnItsFixtures(t *testing.T) {
	trip := findPathDotDot(t, "dotdot_path.go.txt", readFixture(t, "dotdot_path.go.txt"))
	if len(trip) != 3 {
		t.Errorf("the must-trip fixture reported %d findings, want 3 "+
			"(the os.PathSeparator form, the path.Base form, and the path.Clean form):\n%s",
			len(trip), strings.Join(trip, "\n"))
	}

	if pass := findPathDotDot(t, "dotdot_revrange.go.txt", readFixture(t, "dotdot_revrange.go.txt")); len(pass) != 0 {
		t.Errorf("a git rev-range or a ref-name rule was reported as a path comparison:\n%s",
			strings.Join(pass, "\n"))
	}
}

// TestDotDotScanSeesTheRealLiterals is the vacuity guard for the literal walk.
// internal/pathfence is EXCLUDED from the ban — it is where containment lives —
// so it is the natural place to prove the finder still finds anything at all.
func TestDotDotScanSeesTheRealLiterals(t *testing.T) {
	found := 0
	for _, f := range goSourcesIn(t, "../pathfence") {
		found += len(findPathDotDot(t, f.name, f.src))
	}
	if found == 0 {
		t.Fatal("the literal finder reports nothing inside internal/pathfence, which is where " +
			"every remaining \"..\" test lives — the walk has stopped seeing literals and the " +
			"ban over every other package is vacuous")
	}
}

// --- the walkers ------------------------------------------------------------

type goSource struct{ name, src string }

// goSourcesIn reads every non-test .go file in dir.
func goSourcesIn(t *testing.T, dir string) []goSource {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	var out []goSource
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, goSource{name: filepath.Join(dir, name), src: string(b)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].name < out[j].name })
	return out
}

func readFixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "pathfence_scan", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return string(b)
}

// scanTree applies the non-test filter and then the unwrap scan, so the
// filename rule can be tested independently of the scan itself.
func scanTree(t *testing.T, name, src string) []string {
	t.Helper()
	if strings.HasSuffix(name, "_test.go") {
		return nil
	}
	return findUnwrapCalls(t, name, src)
}

// findUnwrapCalls reports every os.* call with a .String() or .Rel() call
// anywhere inside an argument.
//
// It is TYPE-BLIND and says so: go/ast cannot tell a Contained from a
// strings.Builder, so the rule is a flat ban on those two accessor names inside
// an os.* argument. That is fail-closed by design — a scan that tried to exempt
// the buffer case would need go/types and would silently exempt the real one
// too.
func findUnwrapCalls(t *testing.T, name, src string) []string {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, name, src, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", name, err)
	}
	var out []string
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if pkg, ok := sel.X.(*ast.Ident); !ok || pkg.Name != "os" || !osVerbs[sel.Sel.Name] {
			return true
		}
		for _, arg := range call.Args {
			if acc := unwrapAccessor(arg); acc != "" {
				out = append(out, fmt.Sprintf("line %d: os.%s(...%s()...)",
					fset.Position(call.Pos()).Line, sel.Sel.Name, acc))
				break
			}
		}
		return true
	})
	return out
}

// unwrapAccessor returns "String" or "Rel" if either is called anywhere inside
// e, or "" otherwise.
func unwrapAccessor(e ast.Expr) string {
	var found string
	ast.Inspect(e, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || found != "" {
			return found == ""
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || len(call.Args) != 0 {
			return true
		}
		if sel.Sel.Name == "String" || sel.Sel.Name == "Rel" {
			found = sel.Sel.Name
			return false
		}
		return true
	})
	return found
}

// findPathDotDot reports every ".." literal that sits in a path context.
//
// Two shape rules keep the legal uses out, rather than an exemption list that
// would go stale the moment a fifth call site is added:
//
//   - a literal that is an operand of a `+` is a git rev-list RANGE
//     (main+".."+work), not a comparison against a cleaned path;
//   - a literal in a function that makes no path call at all is a ref-name rule
//     (refguard's strings.Contains), which git requires for its own reasons.
func findPathDotDot(t *testing.T, name, src string) []string {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, name, src, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", name, err)
	}
	var out []string
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		if !hasPathMarker(fn.Body) {
			continue
		}
		concat := concatOperands(fn.Body)
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING || lit.Value != `".."` || concat[lit] {
				return true
			}
			out = append(out, fmt.Sprintf("line %d: %s tests \"..\" directly",
				fset.Position(lit.Pos()).Line, fn.Name.Name))
			return true
		})
	}
	return out
}

// hasPathMarker reports whether a function body contains any token that makes
// its ".." a path question: a separator constant, a "../" literal, or a call to
// one of the path helpers.
func hasPathMarker(body *ast.BlockStmt) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		switch v := n.(type) {
		case *ast.SelectorExpr:
			if id, ok := v.X.(*ast.Ident); ok && pathMarkers[id.Name+"."+v.Sel.Name] {
				found = true
			}
		case *ast.BasicLit:
			if v.Kind == token.STRING && v.Value == `"../"` {
				found = true
			}
		}
		return !found
	})
	return found
}

// concatOperands indexes every string literal that is an operand of a `+`, so
// a rev-range can be told from a comparison without naming its call sites.
func concatOperands(body *ast.BlockStmt) map[*ast.BasicLit]bool {
	out := map[*ast.BasicLit]bool{}
	ast.Inspect(body, func(n ast.Node) bool {
		bin, ok := n.(*ast.BinaryExpr)
		if !ok || bin.Op != token.ADD {
			return true
		}
		for _, side := range []ast.Expr{bin.X, bin.Y} {
			if lit, ok := side.(*ast.BasicLit); ok {
				out[lit] = true
			}
		}
		return true
	})
	return out
}
