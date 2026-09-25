package cmd

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
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
//       wrong file. Both accessors are banned inside an os.* argument — when,
//       and only when, the type checker says the receiver IS a Contained. A
//       strings.Builder's String() is not the seam and is not flagged; a
//       Contained renamed through any number of locals still is.
//
//   (b) LITERAL. A second ..-prefix implementation reintroduced anywhere
//       outside internal/pathfence is the exact thing this phase removed.
//
// Both walk the scanned set every source scan walks (srcscope_test.go): every
// package the shared load matched, with no list of package names to extend.
//
// A consumer that loads a declared path field and calls os.* on the RAW
// string, never touching Contained at all, is the path-field taint scan's
// (pathtaint_audit_test.go), not this file's.
//
// Every must-trip and must-not-trip case is a testdata fixture rather than an
// assertion about the live tree. A scan calibrated on its own post-fix output
// proves only that it was written after the fix.

// pathMarkers are the tokens that make a ".." literal a PATH comparison rather
// than a git rev-range or a ref-name rule. Matched within the enclosing
// function, so a marker elsewhere in a large file cannot implicate an unrelated
// literal.
var pathMarkers = map[string]bool{
	"filepath.Separator": true, "os.PathSeparator": true,
	"path.Clean": true, "path.Base": true,
	"filepath.Rel": true, "filepath.IsAbs": true, "filepath.Clean": true,
}

// The unwrap ban's floors: ~25% under the 278 os.* call sites it examines
// today, and ~25% under the 4 in internal/security — the run-dir writers this
// ban exists for, and too few for the whole-tree floor to notice them gone.
const (
	unwrapOSSiteFloor         = 208
	unwrapSecurityOSSiteFloor = 3
)

// unwrapFloorErr is the coverage check over a walk's os.* site counts.
func unwrapFloorErr(osSites int, perPkg map[string]int) error {
	if osSites < unwrapOSSiteFloor {
		return fmt.Errorf("the unwrap ban examined %d os.* call sites, under the floor of %d — the walk has narrowed", osSites, unwrapOSSiteFloor)
	}
	if n := perPkg[modulePath+"/internal/security"]; n < unwrapSecurityOSSiteFloor {
		return fmt.Errorf("the unwrap ban examined %d os.* call sites in internal/security, under its floor of %d — the walk lost the run-dir writers", n, unwrapSecurityOSSiteFloor)
	}
	return nil
}

// isContained reports whether t is pathfence.Contained, or a pointer to it.
func isContained(t types.Type) bool {
	if p, ok := t.(*types.Pointer); ok {
		t = p.Elem()
	}
	n, ok := t.(*types.Named)
	return ok && n.Obj().Pkg() != nil && n.Obj().Pkg().Path() == modulePath+"/internal/pathfence" && n.Obj().Name() == "Contained"
}

// isOSFunc reports whether a call's callee is a package-level function of os.
func isOSFunc(info *types.Info, call *ast.CallExpr) (string, bool) {
	fn, ok := execCalleeObject(info, call).(*types.Func)
	if !ok || fn.Pkg() == nil || fn.Pkg().Path() != "os" || fn.Type().(*types.Signature).Recv() != nil {
		return "", false
	}
	return fn.Name(), true
}

// findUnwrapCalls reports every os.* call with a Contained's String() or Rel()
// anywhere inside an argument, and counts the os.* call sites it examined.
func findUnwrapCalls(fset *token.FileSet, info *types.Info, files []*ast.File) (hits []string, osSites int) {
	for _, f := range files {
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			verb, ok := isOSFunc(info, call)
			if !ok {
				return true
			}
			osSites++
			for _, arg := range call.Args {
				if acc := containedAccessor(info, arg); acc != "" {
					pos := fset.Position(call.Pos())
					hits = append(hits, fmt.Sprintf("%s:%d: os.%s(...%s()...)", pos.Filename, pos.Line, verb, acc))
					break
				}
			}
			return true
		})
	}
	return hits, osSites
}

// containedAccessor returns "String" or "Rel" if either is called on a
// Contained anywhere inside e, or "" otherwise.
func containedAccessor(info *types.Info, e ast.Expr) string {
	var found string
	ast.Inspect(e, func(n ast.Node) bool {
		if found != "" {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok || len(call.Args) != 0 {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || (sel.Sel.Name != "String" && sel.Sel.Name != "Rel") {
			return true
		}
		if isContained(info.TypeOf(sel.X)) {
			found = sel.Sel.Name
			return false
		}
		return true
	})
	return found
}

// unwrapLive runs the ban over every package of a view, returning the hits,
// the os.* sites examined in total and per package, and the files visited per
// package.
func unwrapLive(v *srcView) (hits []string, osSites int, perPkg, visited map[string]int) {
	perPkg, visited = map[string]int{}, map[string]int{}
	for _, p := range v.Pkgs {
		h, n := findUnwrapCalls(v.Fset, p.Info, p.Syntax)
		hits = append(hits, h...)
		osSites += n
		perPkg[p.Path] += n
		visited[p.Path] += len(p.Syntax)
	}
	return hits, osSites, perPkg, visited
}

// --- (a) the unwrap ban -----------------------------------------------------

// TestNoUnwrappedPathReachesAnOSCall runs the ban over the live tree.
func TestNoUnwrappedPathReachesAnOSCall(t *testing.T) {
	hits, osSites, perPkg, visited := unwrapLive(liveView(t))
	for _, hit := range hits {
		t.Errorf("%s — route it through the pathfence seam (pathfence.ReadFile / WriteFile / Stat)", hit)
	}
	assertWalkCovers(t, "unwrap", visited)
	if err := unwrapFloorErr(osSites, perPkg); err != nil {
		t.Error(err)
	}
}

// assertWalkCovers pins the derived walk: every package in it contributed
// files, and the two packages the old hand-kept list never named are in it.
func assertWalkCovers(t *testing.T, walk string, visited map[string]int) {
	t.Helper()
	for pkg, n := range visited {
		if n == 0 {
			t.Errorf("the %s walk visited %s with zero files", walk, pkg)
		}
	}
	for _, pkg := range []string{"internal/argfence", "internal/hostallow"} {
		if visited[modulePath+"/"+pkg] == 0 {
			t.Errorf("the %s walk did not visit %s", walk, pkg)
		}
	}
}

// TestUnwrapFloorCatchesADroppedPackage: the floors are only worth their
// numbers if losing the package this ban was written for trips them, and if
// they pass at their minimum and fail one under it.
func TestUnwrapFloorCatchesADroppedPackage(t *testing.T) {
	live := liveView(t)
	if _, all, perPkg, _ := unwrapLive(live); unwrapFloorErr(all, perPkg) != nil {
		t.Fatalf("the live walk is already under a floor: %v", unwrapFloorErr(all, perPkg))
	}
	narrowed := *live
	narrowed.Pkgs = nil
	for _, p := range live.Pkgs {
		if p.Path != modulePath+"/internal/security" {
			narrowed.Pkgs = append(narrowed.Pkgs, p)
		}
	}
	if _, n, perPkg, _ := unwrapLive(&narrowed); unwrapFloorErr(n, perPkg) == nil {
		t.Errorf("dropping internal/security (%d os.* sites left) passed the floors", n)
	}
	sec := map[string]int{modulePath + "/internal/security": unwrapSecurityOSSiteFloor}
	if err := unwrapFloorErr(unwrapOSSiteFloor, sec); err != nil {
		t.Errorf("the floors fail at their own minimum: %v", err)
	}
	if unwrapFloorErr(unwrapOSSiteFloor-1, sec) == nil {
		t.Error("the whole-tree floor passes one under its minimum")
	}
	sec[modulePath+"/internal/security"]--
	if unwrapFloorErr(unwrapOSSiteFloor, sec) == nil {
		t.Error("the internal/security floor passes one under its minimum")
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
		}},
		{"unwrap_renamed.go.txt", []string{
			"os.ReadFile(...String()...)",
		}},
		{"rundir_revert.go.txt", []string{
			"os.WriteFile(...String()...)",
			"os.WriteFile(...String()...)",
			"os.WriteFile(...String()...)",
		}},
		{"seam_ok.go.txt", nil},
	} {
		t.Run(tc.fixture, func(t *testing.T) {
			fx := loadFixture(t, fixturePath("pathfence_scan", tc.fixture))
			var got []string
			for _, p := range fx.Pkgs {
				h, _ := findUnwrapCalls(fx.Fset, p.Info, p.Syntax)
				got = append(got, h...)
			}
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
// strings.Builder at internal/cmd/mutation_remote_wiring_test.go:47 would no
// longer trip a type-keyed ban, but a test file's own Contained handling is
// not production code, and the load (Tests:false) never sees it.
//
// It reuses the must-trip fixture under a _test NAME, so the two cases differ
// in exactly one thing: the filename.
func TestUnwrapScanSkipsTestFiles(t *testing.T) {
	for _, p := range liveView(t).Pkgs {
		for _, f := range p.Syntax {
			if name := liveView(t).Fset.Position(f.Pos()).Filename; strings.HasSuffix(name, "_test.go") {
				t.Errorf("the unwrap walk sees test file %s", name)
			}
		}
	}
	body, err := os.ReadFile(fixturePath("pathfence_scan", "unwrap_os.go.txt"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "unwrap_os.go.txt"), string(body))
	mustWrite(t, filepath.Join(dir, "unwrap_os_test.go.txt"), string(body))
	fx := loadFixture(t, filepath.Join(dir, "unwrap_os.go.txt"), filepath.Join(dir, "unwrap_os_test.go.txt"))
	var hits []string
	for _, p := range fx.Pkgs {
		h, _ := findUnwrapCalls(fx.Fset, p.Info, p.Syntax)
		hits = append(hits, h...)
	}
	if len(hits) == 0 {
		t.Fatal("the fixture reports nothing under a non-test name — this comparison measures nothing")
	}
	for _, h := range hits {
		if strings.Contains(h, "_test.go") {
			t.Errorf("the same source under a _test name still reported %s", h)
		}
	}
}

// --- (b) the literal ban ----------------------------------------------------

// dotDotLive runs the literal scan over every scanned package except
// internal/pathfence, where containment lives.
func dotDotLive(v *srcView) (hits []string, visited map[string]int) {
	visited = map[string]int{}
	for _, p := range v.Pkgs {
		if p.Path == modulePath+"/internal/pathfence" {
			continue
		}
		for _, f := range p.Syntax {
			visited[p.Path]++
			hits = append(hits, findPathDotDot(v.Fset, f)...)
		}
	}
	return hits, visited
}

// TestNoSecondDotDotImplementation runs the literal scan over the live tree.
func TestNoSecondDotDotImplementation(t *testing.T) {
	hits, visited := dotDotLive(liveView(t))
	for _, hit := range hits {
		t.Errorf("%s — containment is internal/pathfence's job; "+
			"call pathfence.Contain, InTree or Segment instead of testing \"..\" here", hit)
	}
	assertWalkCovers(t, "\"..\"", visited)
}

// TestDotDotScanTripsOnItsFixtures self-tests the literal scan against the two
// shapes this phase removed and the two it must leave alone.
func TestDotDotScanTripsOnItsFixtures(t *testing.T) {
	trip := findPathDotDotSrc(t, "dotdot_path.go.txt")
	if len(trip) != 3 {
		t.Errorf("the must-trip fixture reported %d findings, want 3 "+
			"(the os.PathSeparator form, the path.Base form, and the path.Clean form):\n%s",
			len(trip), strings.Join(trip, "\n"))
	}

	if pass := findPathDotDotSrc(t, "dotdot_revrange.go.txt"); len(pass) != 0 {
		t.Errorf("a git rev-range or a ref-name rule was reported as a path comparison:\n%s",
			strings.Join(pass, "\n"))
	}
}

// TestDotDotScanSeesTheRealLiterals is the vacuity guard for the literal walk.
// internal/pathfence is EXCLUDED from the ban — it is where containment lives —
// so it is the natural place to prove the finder still finds anything at all.
func TestDotDotScanSeesTheRealLiterals(t *testing.T) {
	v := liveView(t)
	found := 0
	for _, p := range v.Pkgs {
		if p.Path != modulePath+"/internal/pathfence" {
			continue
		}
		for _, f := range p.Syntax {
			found += len(findPathDotDot(v.Fset, f))
		}
	}
	if found == 0 {
		t.Fatal("the literal finder reports nothing inside internal/pathfence, which is where " +
			"every remaining \"..\" test lives — the walk has stopped seeing literals and the " +
			"ban over every other package is vacuous")
	}
}

// --- the walkers ------------------------------------------------------------

// findPathDotDotSrc parses a pathfence_scan fixture and runs the literal scan.
// The literal scan is syntactic, so the fixture need not type-check.
func findPathDotDotSrc(t *testing.T, name string) []string {
	t.Helper()
	b, err := os.ReadFile(fixturePath("pathfence_scan", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, name, b, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", name, err)
	}
	return findPathDotDot(fset, f)
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
func findPathDotDot(fset *token.FileSet, f *ast.File) []string {
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
			pos := fset.Position(lit.Pos())
			out = append(out, fmt.Sprintf("%s:%d: %s tests \"..\" directly",
				pos.Filename, pos.Line, fn.Name.Name))
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
