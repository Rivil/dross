package cmd

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// The live exec-output taint gate (c-1). It runs the exec policy, markers
// honoured, over t-4's full scanned set — every package the shared load
// matched — and fails on any spawn output that escapes, anywhere, and on any
// //dross:taint-cleared marker in the tree that is malformed, sits on a
// source, or clears nothing.
//
// It landed after every burn-down, so it was never red mid-phase, and it
// subsumes the six scoped gates those burn-downs carried: their zero-findings
// checks are retired (TestScopedGatesRetired) and the named guards beside them
// stay (TestScopedGuardsSurvive).

// execSourceFloor and execSourceFileFloor are ~25% under the exec sources
// the live tree seeds today and the files they sit in. A scan whose sources
// silently stopped matching would pass the gate by finding nothing.
//
// Reset in cmd-exec-baseline-drain t-13 to floor(0.75 x the logged census):
// 37 -> 27 sources and 24 -> 15 files, logged at 36 across 20. The drop is the
// phase's own doing — git's spawns collapsed into internal/gitrun's four
// verbs, so one site now stands where a dozen per-file copies stood.
const (
	execSourceFloor     = 27
	execSourceFileFloor = 15
)

// execSourceCensus is where the exec policy seeds: distinct origins, and the
// files that hold them.
type execSourceCensus struct {
	Sites map[token.Position]bool
	Files map[string]bool
}

func newExecSourceCensus() *execSourceCensus {
	return &execSourceCensus{Sites: map[token.Position]bool{}, Files: map[string]bool{}}
}

func (c *execSourceCensus) add(p token.Position) {
	c.Sites[token.Position{Filename: p.Filename, Line: p.Line, Column: p.Column}] = true
	c.Files[p.Filename] = true
}

// within is the census restricted to files under root/dir.
func (c *execSourceCensus) within(root, dir string) *execSourceCensus {
	out := newExecSourceCensus()
	for p := range c.Sites {
		if rel, err := filepath.Rel(root, p.Filename); err == nil && strings.HasPrefix(filepath.ToSlash(rel), dir+"/") {
			out.add(p)
		}
	}
	return out
}

func execSourceFloorErr(sites, files int) error {
	if sites < execSourceFloor || files < execSourceFileFloor {
		return fmt.Errorf("the exec-output scan seeded %d sources across %d files, under the floor of %d across %d — "+
			"its sources have stopped matching, and a clean gate over them is vacuous", sites, files, execSourceFloor, execSourceFileFloor)
	}
	return nil
}

// TestNoSpawnOutputEscapes is the gate.
func TestNoSpawnOutputEscapes(t *testing.T) {
	v := liveView(t)
	census := newExecSourceCensus()
	taint, markers := execTaintScanWith(v, func(pos token.Pos) { census.add(v.Fset.Position(pos)) })
	for _, f := range taint {
		t.Error(execTaintMessage(f))
	}
	for _, m := range markers {
		t.Error(m.String())
	}
	if err := execSourceFloorErr(len(census.Sites), len(census.Files)); err != nil {
		t.Error(err)
	}
	root := sourceProgram(t).Root
	cmdOnly := census.within(root, "internal/cmd")
	if execSourceFloorErr(len(cmdOnly.Sites), len(cmdOnly.Files)) == nil {
		t.Errorf("internal/cmd alone meets the source floor (%d sources across %d files) — "+
			"the floor would not notice the scan losing every other package", len(cmdOnly.Sites), len(cmdOnly.Files))
	}
	t.Logf("exec sources: %d across %d files (internal/cmd alone: %d across %d)",
		len(census.Sites), len(census.Files), len(cmdOnly.Sites), len(cmdOnly.Files))
}

// TestExecSourceFloor: each floor passes at its minimum and fails one under.
func TestExecSourceFloor(t *testing.T) {
	if err := execSourceFloorErr(execSourceFloor, execSourceFileFloor); err != nil {
		t.Errorf("the floor fails at its own minimum: %v", err)
	}
	if execSourceFloorErr(execSourceFloor-1, execSourceFileFloor) == nil {
		t.Error("the floor passes one source under its minimum")
	}
	if execSourceFloorErr(execSourceFloor, execSourceFileFloor-1) == nil {
		t.Error("the floor passes one file under its minimum")
	}
}

// scopedGateFiles are the six burn-down gate files the global gate subsumes.
var scopedGateFiles = []string{
	"taint_ship_test.go",
	"taint_gitplumbing_test.go",
	"taint_gitdirect_test.go",
	"taint_scanners_test.go",
	"taint_toolstreams_test.go",
	"taint_usercmds_test.go",
}

// liveZeroFindingsRuns names every test in f that runs the engine over the
// UNALTERED live view — execTaintScan, execTaintScanWith or runTaint handed
// liveView(t) itself. A run over viewWithoutComment(liveView(t), …) is a
// marker-removal check and is not one of them.
func liveZeroFindingsRuns(f *ast.File) []string {
	var out []string
	for _, d := range f.Decls {
		fd, ok := d.(*ast.FuncDecl)
		if !ok || fd.Body == nil || !strings.HasPrefix(fd.Name.Name, "Test") {
			continue
		}
		hit := false
		ast.Inspect(fd.Body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) == 0 {
				return true
			}
			id, ok := call.Fun.(*ast.Ident)
			if !ok || (id.Name != "execTaintScan" && id.Name != "execTaintScanWith" && id.Name != "runTaint") {
				return true
			}
			if isLiveViewCall(call.Args[0]) {
				hit = true
			}
			return true
		})
		if hit {
			out = append(out, fd.Name.Name)
		}
	}
	return out
}

// isLiveViewCall reports whether e is liveView(…), or a variable assigned
// straight from it in the same test (v := liveView(t)).
func isLiveViewCall(e ast.Expr) bool {
	switch x := e.(type) {
	case *ast.CallExpr:
		id, ok := x.Fun.(*ast.Ident)
		return ok && id.Name == "liveView"
	case *ast.Ident:
		if x.Obj == nil {
			return false
		}
		as, ok := x.Obj.Decl.(*ast.AssignStmt)
		if !ok {
			return false
		}
		for i, l := range as.Lhs {
			if li, ok := l.(*ast.Ident); ok && li.Name == x.Name && i < len(as.Rhs) {
				return isLiveViewCall(as.Rhs[i])
			}
		}
	}
	return false
}

// TestScopedGatesRetired: none of the six scoped gate files still runs the
// engine over the live tree for a zero-findings assertion — each such run is
// the global gate again, paid for again.
func TestScopedGatesRetired(t *testing.T) {
	fset := token.NewFileSet()
	for _, name := range scopedGateFiles {
		f, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, fn := range liveZeroFindingsRuns(f) {
			t.Errorf("%s: %s still runs the engine over the live tree — TestNoSpawnOutputEscapes subsumes it", name, fn)
		}
	}

	src := "package cmd\n\nfunc TestGate(t *testing.T) {\n\tv := liveView(t)\n\t_, _ = execTaintScan(v)\n}\n\n" +
		"func TestDirect(t *testing.T) {\n\t_ = runTaint(liveView(t), execTaintPolicy())\n}\n\n" +
		"func TestRemoval(t *testing.T) {\n\t_, _ = execTaintScan(viewWithoutComment(liveView(t), \"f\", 1))\n}\n"
	f, err := parser.ParseFile(fset, "fixture.go", src, 0)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(liveZeroFindingsRuns(f), ","); got != "TestGate,TestDirect" {
		t.Errorf("the retirement check names %q, want TestGate,TestDirect — it must catch both live-run shapes and spare a marker-removal run", got)
	}
}

// scopedGuards are the named guards the retirement must not take with it:
// every CANARY behaviour test this phase added, the t-17 tool-runner marker
// ban, the t-18 no-marker stream-site check, the t-19 verb pin, and the t-14 /
// t-16 marker-removal checks. Each is keyed by name, with the file it lives in.
var scopedGuards = map[string]string{
	"TestShipRecoverFetchFailureOutputGoesToStderr": "internal/cmd/ship_test.go",
	"TestLaneInstallOutputGoesToStderr":             "internal/cmd/taint_usercmds_test.go",
	"TestPhaseGitShowOutputStaysOffTheError":        "internal/cmd/taint_gitdirect_test.go",
	"TestRemoteURLUserinfoNeverPersists":            "internal/cmd/taint_gitdirect_test.go",
	"TestDetectRemoteDropsUserinfo":                 "internal/project/remote_test.go",
	"TestParseHunksDegradedIsFixedProse":            "internal/verify/scope_test.go",
	"TestGhFailureOutputGoesToStderr":               "internal/ship/gh_failure_test.go",
	"TestGhUnparseableOutputGoesToStderr":           "internal/ship/gh_failure_test.go",
	"TestHoldTransportOutputGoesToStderr":           "internal/remote/hold_test.go",
	"TestUnreadableHolderFieldIsFixedProse":         "internal/remote/hold_test.go",
	"TestToolRunnersCarryNoMarker":                  "internal/cmd/taint_toolstreams_test.go",
	"TestStreamSitesCarryNoMarker":                  "internal/cmd/taint_usercmds_test.go",
	"TestGitTrimRunsRefVerbsOnly":                   "internal/cmd/taint_gitplumbing_test.go",
	"TestGhMarkersAreLoadBearing":                   "internal/cmd/taint_ship_test.go",
	"TestScannerRevParseMarkerIsLoadBearing":        "internal/cmd/taint_scanners_test.go",
	"TestEveryScannerMarkerIsLoadBearing":           "internal/cmd/taint_scanners_test.go",
}

// TestScopedGuardsSurvive: every named guard still exists, in its file.
func TestScopedGuardsSurvive(t *testing.T) {
	root := sourceProgram(t).Root
	fset := token.NewFileSet()
	declared := map[string]map[string]bool{}
	var names []string
	for name, file := range scopedGuards {
		names = append(names, name)
		if declared[file] != nil {
			continue
		}
		declared[file] = map[string]bool{}
		f, err := parser.ParseFile(fset, filepath.Join(root, file), nil, 0)
		if err != nil {
			t.Errorf("%s: %v", file, err)
			continue
		}
		for _, d := range f.Decls {
			if fd, ok := d.(*ast.FuncDecl); ok {
				declared[file][fd.Name.Name] = true
			}
		}
	}
	sort.Strings(names)
	for _, name := range names {
		if !declared[scopedGuards[name]][name] {
			t.Errorf("guard %s is gone from %s — the scoped-gate retirement must not take a named guard with it", name, scopedGuards[name])
		}
	}
}
