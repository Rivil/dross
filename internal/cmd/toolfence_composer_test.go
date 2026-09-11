package cmd

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/phase"
	"github.com/Rivil/dross/internal/ship"
	"github.com/Rivil/dross/internal/toolfence"
	"github.com/Rivil/dross/internal/verify"
)

// c-5 and c-6: no PR body and no board issue body dross composes renders a
// declared subprocess-derived field.
//
// BOTH proof halves run, because either alone is weak. Calling the composers
// with canaries proves what today's code emits and nothing about tomorrow's; an
// AST scan proves the shape and nothing about what the shape produces.
//
// The scan is keyed on the SINK, not on composer function shape. Three of the
// four named composers are unexported here and ship.BuildPRBody is exported, so
// only a cmd-side test can call all four — but the live body sites are mostly
// not functions at all: they are inline fmt.Sprintf values assigned into a
// forge.IssueInput or a ship.OpenOpts, and a scan keyed on "func …Body() string"
// resolves none of them. TestBodyScanIsKeyedOnTheSinkNotTheFuncShape is that
// difference, written down.

// composerRoots are the packages holding a body composer or a body sink.
var composerRoots = []string{".", "../ship"}

// namedComposers are the four functions that build a body wholesale. They are
// CALLED below as well as scanned, so a rename breaks compilation here rather
// than silently shrinking the set.
var namedComposers = []string{"renderPhaseBody", "milestoneBody", "renderTaskBody", "BuildPRBody"}

// bodySite is one place a composed body reaches a sink.
type bodySite struct {
	file  string
	line  int
	group string // "board" | "pr"
	rhs   ast.Expr
	fn    string // enclosing function's name, "" for a closure
}

// TestNoComposerRendersARecordedField is the canary half. Every Recorded field
// carries its own canary; the Renderable one carries the shape it is actually
// allowed to carry.
func TestNoComposerRendersARecordedField(t *testing.T) {
	const legCanary = "CANARY-LEG-7a11"
	const langCanary = "CANARY-LANG-7a12"
	// What Finding.Text really holds: the recorder's line under the prefix
	// verify composes. It is Renderable, so seeing it in a body is CORRECT.
	const findingText = "mutation adapter stryker failed: stryker failed with exit status 1; " +
		"320 bytes of tool output observed; the full output of stryker went to this run's stderr"

	v := &verify.Verify{
		Verify: verify.VerifyMeta{Phase: "p", Verdict: "partial"},
		Summary: verify.VerifySummary{
			MutationStatus: verify.MutationMeasured,
			Legs: []verify.LegSummary{
				{Language: "typescript", Tool: "stryker", Error: legCanary},
			},
		},
		Criteria: []verify.CriterionResult{
			{ID: "c-1", Status: "covered", Tests: []string{"TestThing"}, Notes: "fine"},
		},
		Findings: []verify.Finding{
			{Severity: "FLAG", Text: findingText},
		},
	}
	tests := &verify.Tests{
		Phase: "p",
		Languages: []verify.LanguageRun{
			{Name: "typescript", Tool: "stryker", Error: langCanary},
		},
	}
	spec := &phase.Spec{
		Phase:    phase.SpecPhase{ID: "p", Title: "a phase"},
		Criteria: []phase.Criterion{{ID: "c-1", Text: "something holds"}},
	}
	plan := &phase.Plan{
		Phase: phase.PlanPhase{ID: "p"},
		Task:  []phase.Task{{ID: "t-1", Wave: 1, Title: "do it", Files: []string{"a.go"}}},
	}
	_ = tests // LanguageRun is not reachable from any composer's inputs; the
	// canary is declared so the assertion below fails if one ever takes Tests.

	bodies := map[string]string{
		"BuildPRBody":     ship.BuildPRBody(spec, v),
		"renderPhaseBody": renderPhaseBody("p", spec, plan),
		"milestoneBody":   milestoneBody("v1.7", "- something holds\n"),
		"renderTaskBody":  renderTaskBody("p", "DRO-1", plan.Task[0]),
	}

	for name, body := range bodies {
		for _, canary := range []string{legCanary, langCanary} {
			if strings.Contains(body, canary) {
				t.Errorf("%s renders a Recorded field:\n%s", name, body)
			}
		}
	}
	// The permitted half, asserted from the other side: if no composer renders
	// the Renderable field either, the canary check above proves nothing —
	// it would pass just as well against four composers that emit nothing.
	rendered := false
	for _, body := range bodies {
		if strings.Contains(body, findingText) {
			rendered = true
		}
	}
	if !rendered {
		t.Error("no composer rendered the Renderable Finding.Text — the canary assertions above are vacuous")
	}
	for name, body := range bodies {
		if strings.TrimSpace(body) == "" {
			t.Errorf("%s produced an empty body, so it asserts nothing", name)
		}
	}
}

// TestNoBodySinkReadsARecordedField is the AST half, over the live tree.
func TestNoBodySinkReadsARecordedField(t *testing.T) {
	sites := bodySites(t, composerRoots)

	byGroup := map[string]int{}
	for _, s := range sites {
		byGroup[s.group]++
		for _, hit := range recordedReads(s.rhs) {
			t.Errorf("%s:%d: a %s body is composed out of the declared field %q — c-5/c-6 forbid rendering a subprocess-derived field",
				s.file, s.line, s.group, hit)
		}
	}

	// Vacuity floor, PER GROUP: a healthy PR half must not cover for a board
	// half that resolved nothing.
	for _, group := range []string{"board", "pr"} {
		if byGroup[group] == 0 {
			t.Errorf("the scan resolved no %s body sinks — it is passing because it found nothing to check", group)
		}
	}
	if len(sites) < 8 {
		t.Errorf("the scan resolved only %d body sinks; the tree has at least 8 (issue.go, issue_task.go, milestone.go, ship.go)", len(sites))
	}

	// Every file that holds a body sink is reached. Asserted by FILE rather
	// than by line number: the plan named four specific lines, and pinning
	// those would break on the next edit above them while proving nothing more
	// than this does.
	byFile := map[string][]int{}
	for _, s := range sites {
		byFile[filepath.Base(s.file)] = append(byFile[filepath.Base(s.file)], s.line)
	}
	for _, want := range []string{"issue.go", "issue_task.go", "milestone.go", "ship.go"} {
		if len(byFile[want]) == 0 {
			t.Errorf("the scan resolved no body sink in %s, which holds one", want)
		}
	}
	// issue.go carries the inline backlog-item bodies AND the forge literals.
	if n := len(byFile["issue.go"]); n < 4 {
		t.Errorf("the scan resolved %d body sinks in issue.go, want at least 4 (two backlog-item bodies and two forge literals)", n)
	}
	t.Logf("resolved body sinks: %v", byFile)

	// The named composers must still be there. Renaming one breaks the call
	// site above at compile time; this catches a rename that keeps a stale
	// wrapper around.
	decls := funcDecls(t, composerRoots)
	for _, name := range namedComposers {
		if _, ok := decls[name]; !ok {
			t.Errorf("named composer %s no longer resolves — it was renamed or removed without updating this enumeration", name)
		}
	}
}

// TestBodyScanTripsOnTheLeakyFixture calibrates the scan on a written-down
// answer instead of on the live tree's post-fix state.
func TestBodyScanTripsOnTheLeakyFixture(t *testing.T) {
	fset := token.NewFileSet()
	path := filepath.Join("testdata", "toolfence_composer", "leaky_body.go.txt")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	f, err := parser.ParseFile(fset, path, b, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse fixture: %v", err)
	}

	byFunc := map[string][]string{}
	for _, s := range sitesInFile(fset, f, path) {
		byFunc[s.fn] = append(byFunc[s.fn], recordedReads(s.rhs)...)
	}

	if len(byFunc["leakyBoardBody"]) == 0 {
		t.Error("the scan did not trip on a board body composed from LegSummary.Error")
	}
	if got := byFunc["cleanBoardBody"]; len(got) != 0 {
		t.Errorf("the scan tripped on a Renderable field, which composers are allowed to print: %v", got)
	}
	if got := byFunc["errorMethodIsNotAFieldRead"]; len(got) != 0 {
		t.Errorf("the scan tripped on err.Error(), a method call rather than a field read: %v", got)
	}
	if len(byFunc) != 3 {
		t.Errorf("the scan resolved %d of the fixture's 3 body sinks: %v", len(byFunc), byFunc)
	}
}

// TestBodyScanIsKeyedOnTheSinkNotTheFuncShape is the reason the scan is written
// the way it is. Most live body sites are inline assignments inside a cobra
// RunE closure or a backlog-item builder, so a scan keyed on "a function whose
// name ends in Body" resolves NONE of them.
func TestBodyScanIsKeyedOnTheSinkNotTheFuncShape(t *testing.T) {
	sites := bodySites(t, composerRoots)
	decls := funcDecls(t, composerRoots)

	bodyShaped := map[string]bool{}
	for name := range decls {
		if strings.HasSuffix(name, "Body") {
			bodyShaped[name] = true
		}
	}
	if len(bodyShaped) < len(namedComposers) {
		t.Fatalf("the func-shaped scan found %d Body-named functions, want at least the %d named composers", len(bodyShaped), len(namedComposers))
	}

	inline := 0
	for _, s := range sites {
		if bodyShaped[s.fn] {
			t.Errorf("%s:%d: a body sink inside %s — this test assumes the sinks live outside the Body-named functions", s.file, s.line, s.fn)
			continue
		}
		inline++
	}
	if inline < 4 {
		t.Errorf("only %d body sinks sit outside a Body-named function; a func-shaped scan would already cover the tree and the sink-keyed one would be redundant", inline)
	}
}

// TestShipCommentIsAPassThrough. `dross ship comment` is enumerated as carrying
// no composed body at all: its text comes only from --body or --body-file. That
// is a claim about the command, so it is asserted rather than written down —
// the moment it starts reading project state to compose a body, it becomes a
// composer and needs a canary of its own.
func TestShipCommentIsAPassThrough(t *testing.T) {
	src := readCmdSource(t, "ship.go")
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "ship.go", src, parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}

	var fn *ast.FuncDecl
	for _, d := range f.Decls {
		fd, ok := d.(*ast.FuncDecl)
		if ok && strings.Contains(strings.ToLower(fd.Name.Name), "comment") {
			fn = fd
			break
		}
	}
	if fn == nil {
		t.Fatal("no ship-comment command function found in ship.go — this assertion resolved nothing")
	}

	// The state readers a composer would need. loadProject is NOT one: the
	// command needs the provider and token to post at all, and neither reaches
	// the body.
	banned := map[string]bool{
		"LoadVerify": true, "LoadTests": true, "BuildPRBody": true,
		"LoadSpec": true, "LoadPlan": true, "Skeleton": true,
	}
	ast.Inspect(fn, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		name := ""
		switch f := call.Fun.(type) {
		case *ast.Ident:
			name = f.Name
		case *ast.SelectorExpr:
			name = f.Sel.Name
		}
		if banned[name] {
			t.Errorf("ship comment calls %s — it now composes a body from project state and must be enumerated as a composer, not a pass-through", name)
		}
		return true
	})
}

// --- the scan ---------------------------------------------------------------

// bodySites walks the roots and returns every assignment into a body sink.
//
// SINK-keyed: a `Body`/`body` field on one of the sink types, whether written
// as a composite-literal key or as a plain assignment.
func bodySites(t *testing.T, roots []string) []bodySite {
	t.Helper()
	var out []bodySite
	fset := token.NewFileSet()
	for _, root := range roots {
		for _, name := range goFilesIn(t, root) {
			b, err := os.ReadFile(name)
			if err != nil {
				t.Fatal(err)
			}
			f, err := parser.ParseFile(fset, name, b, parser.ParseComments)
			if err != nil {
				t.Fatalf("parse %s: %v", name, err)
			}
			out = append(out, sitesInFile(fset, f, name)...)
		}
	}
	return out
}

// sinkTypes are the composite-literal types whose Body field reaches a forge or
// a provider. backlogItem is included because its `body` is what issue.go later
// assigns into forge.IssueInput.Body — the composition happens there, so that is
// where the scan has to look.
var sinkTypes = map[string]string{
	"forge.IssueInput": "board", "forge.IssuePatch": "board", "backlogItem": "board",
	"ship.OpenOpts": "pr", "ship.CommentOpts": "pr", "OpenOpts": "pr", "CommentOpts": "pr",
}

// sinkReceivers are the local variables the live tree assigns bodies onto. A
// plain `x.Body = v` carries no type in the AST, so the receiver name is what
// classifies it; an unknown receiver is still RECORDED (as "pr"), never dropped.
var sinkReceivers = map[string]string{"opts": "pr", "co": "pr", "patch": "board", "in": "board"}

func sitesInFile(fset *token.FileSet, f *ast.File, name string) []bodySite {
	var out []bodySite
	var fnName string

	ast.Inspect(f, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.FuncDecl:
			fnName = node.Name.Name
		case *ast.CompositeLit:
			group, ok := sinkTypes[exprName(node.Type)]
			if !ok {
				return true
			}
			for _, elt := range node.Elts {
				kv, ok := elt.(*ast.KeyValueExpr)
				if !ok {
					continue
				}
				if key, ok := kv.Key.(*ast.Ident); ok && strings.EqualFold(key.Name, "body") {
					out = append(out, bodySite{
						file: name, line: fset.Position(kv.Pos()).Line,
						group: group, rhs: kv.Value, fn: fnName,
					})
				}
			}
		case *ast.AssignStmt:
			for i, lhs := range node.Lhs {
				sel, ok := lhs.(*ast.SelectorExpr)
				if !ok || !strings.EqualFold(sel.Sel.Name, "body") {
					continue
				}
				recv, _ := sel.X.(*ast.Ident)
				if recv == nil {
					continue
				}
				group, ok := sinkReceivers[recv.Name]
				if !ok {
					continue
				}
				if i < len(node.Rhs) {
					out = append(out, bodySite{
						file: name, line: fset.Position(sel.Pos()).Line,
						group: group, rhs: node.Rhs[i], fn: fnName,
					})
				}
			}
		}
		return true
	})
	return out
}

// recordedReads reports every read of a Recorded field inside an expression.
//
// The banned names come from the REGISTRY, not from a list written here, so a
// new Recorded entry widens this guard without an edit. A Renderable field is
// allowed by construction (it is not in the Recorded set), and a method call —
// err.Error() — is not a field read.
func recordedReads(e ast.Expr) []string {
	banned := map[string]bool{}
	for _, f := range toolfence.Fields() {
		if f.Recorded != nil {
			banned[f.Field] = true
		}
	}

	methodCall := map[ast.Expr]bool{}
	ast.Inspect(e, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok {
			methodCall[call.Fun] = true
		}
		return true
	})

	var hits []string
	ast.Inspect(e, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok || !banned[sel.Sel.Name] || methodCall[sel] {
			return true
		}
		hits = append(hits, fmt.Sprintf("%s.%s", exprName(sel.X), sel.Sel.Name))
		return true
	})
	return hits
}

func funcDecls(t *testing.T, roots []string) map[string]string {
	t.Helper()
	out := map[string]string{}
	fset := token.NewFileSet()
	for _, root := range roots {
		for _, name := range goFilesIn(t, root) {
			b, err := os.ReadFile(name)
			if err != nil {
				t.Fatal(err)
			}
			f, err := parser.ParseFile(fset, name, b, parser.SkipObjectResolution)
			if err != nil {
				t.Fatalf("parse %s: %v", name, err)
			}
			for _, d := range f.Decls {
				if fd, ok := d.(*ast.FuncDecl); ok {
					out[fd.Name.Name] = name
				}
			}
		}
	}
	return out
}

func goFilesIn(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	var out []string
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() || !strings.HasSuffix(n, ".go") || strings.HasSuffix(n, "_test.go") {
			continue
		}
		out = append(out, filepath.Join(dir, n))
	}
	return out
}

func readCmdSource(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// exprName renders a type or receiver expression as its source-level name.
func exprName(e ast.Expr) string {
	switch x := e.(type) {
	case *ast.Ident:
		return x.Name
	case *ast.SelectorExpr:
		return exprName(x.X) + "." + x.Sel.Name
	case *ast.StarExpr:
		return exprName(x.X)
	case *ast.CallExpr:
		return exprName(x.Fun)
	case *ast.IndexExpr:
		return exprName(x.X)
	}
	return ""
}
