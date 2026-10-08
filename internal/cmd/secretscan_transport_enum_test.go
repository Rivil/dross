package cmd

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/secretscan"
)

// c-4, the transport half: every function in internal/ship and internal/forge
// that reaches an outbound primitive is DECLARED in secretscan.Transports(),
// and each declaration's disposition is judged against the source — a
// Screened entry's scan call precedes its request, a ReadOnly entry's request
// is a literal GET with a nil body, a Seam is called only from its one
// Screened caller — and every declaration still resolves to a function.
//
// It lives in internal/cmd for the same reason the toolfence and pathfence
// walkers do: it parses the transport packages, which import the registry.
//
// Attribution rule, written down: a call belongs to the FuncDecl it sits in,
// or — for a func literal that is the initialiser of a package-level var —
// to that var's name. That second arm is what lets the exec.Command inside
// `var ghCommand = func(...)` resolve to the Seam entry rather than vanish.

// transportRoots are the packages the walk covers, relative to internal/cmd.
var transportRoots = []string{"../ship", "../forge"}

// transportCall is one outbound-primitive or scanner call inside a function.
type transportCall struct {
	kind string // "http.NewRequest" | "http.NewRequestWithContext" | "http.Post" | "http.Get" | "Do" | "exec.Command" | "seam:<ident>" | "secretscan.<Call>"
	call *ast.CallExpr
}

// transportFn is one attributed function with the calls the walk found in it.
type transportFn struct {
	name     string // "pkg.Func", "pkg.Recv.Method" or "pkg.var"
	bare     string // the identifier a same-package call would use
	pos      token.Pos
	requests []transportCall
	screens  []transportCall
}

// transportFinding is one judged failure, keyed by the function it is about
// so a test can assert the exact set rather than grep prose.
type transportFinding struct {
	fn  string
	arm string // "undeclared" | "screen-missing" | "screen-order" | "read-only" | "seam-caller" | "stale"
	msg string
}

func (f transportFinding) String() string { return f.fn + " [" + f.arm + "]: " + f.msg }

// --- the walk ---------------------------------------------------------------

// attributedFn is a function body with its attribution, before call
// classification (which needs the seam identifier set).
type attributedFn struct {
	name, bare string
	pos        token.Pos
	body       *ast.BlockStmt
	isVar      bool // a func literal bound to a package-level var
}

func attributeFile(t *testing.T, fset *token.FileSet, name, src, pkg string) []attributedFn {
	t.Helper()
	f, err := parser.ParseFile(fset, name, src, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse %s: %v", name, err)
	}
	var out []attributedFn
	for _, d := range f.Decls {
		switch d := d.(type) {
		case *ast.FuncDecl:
			if d.Body == nil {
				continue
			}
			bare := d.Name.Name
			full := pkg + "." + bare
			if d.Recv != nil && len(d.Recv.List) == 1 {
				if r := receiverName(d.Recv.List[0].Type); r != "" {
					full = pkg + "." + r + "." + bare
				}
			}
			out = append(out, attributedFn{name: full, bare: bare, pos: d.Pos(), body: d.Body})
		case *ast.GenDecl:
			if d.Tok != token.VAR {
				continue
			}
			for _, s := range d.Specs {
				vs, ok := s.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for i, v := range vs.Values {
					lit, ok := v.(*ast.FuncLit)
					if !ok || i >= len(vs.Names) {
						continue
					}
					bare := vs.Names[i].Name
					out = append(out, attributedFn{name: pkg + "." + bare, bare: bare, pos: vs.Pos(), body: lit.Body, isVar: true})
				}
			}
		}
	}
	return out
}

func receiverName(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.StarExpr:
		return receiverName(t.X)
	case *ast.Ident:
		return t.Name
	}
	return ""
}

// primitiveKind classifies a call as an outbound primitive, or "" if it is
// not one. Seam identifiers are classified by the caller, which knows them.
func primitiveKind(call *ast.CallExpr) string {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return ""
	}
	if x, ok := sel.X.(*ast.Ident); ok {
		switch x.Name + "." + sel.Sel.Name {
		case "http.NewRequest", "http.NewRequestWithContext", "http.Post", "http.Get", "exec.Command":
			return x.Name + "." + sel.Sel.Name
		}
	}
	if sel.Sel.Name == "Do" && len(call.Args) == 1 {
		return "Do"
	}
	return ""
}

func screenKind(call *ast.CallExpr) string {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return ""
	}
	if x, ok := sel.X.(*ast.Ident); ok && x.Name == "secretscan" && secretscan.ScreenCalls[sel.Sel.Name] {
		return "secretscan." + sel.Sel.Name
	}
	return ""
}

// classify turns attributed bodies into transportFns. A seam identifier is
// any registered Seam's bare name, plus any func-literal-bound package var
// whose body reaches a primitive — so an unregistered seam (fixture case f)
// is both an undeclared site itself and a seam its callers are judged on.
func classify(fns []attributedFn, reg []secretscan.Transport) []*transportFn {
	seams := map[string]bool{}
	for _, tr := range reg {
		if tr.Seam != nil {
			seams[bareFunc(tr.Func)] = true
		}
	}
	for _, fn := range fns {
		if !fn.isVar {
			continue
		}
		ast.Inspect(fn.body, func(n ast.Node) bool {
			if c, ok := n.(*ast.CallExpr); ok && primitiveKind(c) != "" {
				seams[fn.bare] = true
			}
			return true
		})
	}

	var out []*transportFn
	for _, fn := range fns {
		tf := &transportFn{name: fn.name, bare: fn.bare, pos: fn.pos}
		ast.Inspect(fn.body, func(n ast.Node) bool {
			c, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			if k := primitiveKind(c); k != "" {
				tf.requests = append(tf.requests, transportCall{kind: k, call: c})
			} else if k := screenKind(c); k != "" {
				tf.screens = append(tf.screens, transportCall{kind: k, call: c})
			} else if id, ok := c.Fun.(*ast.Ident); ok && seams[id.Name] {
				tf.requests = append(tf.requests, transportCall{kind: "seam:" + id.Name, call: c})
			}
			return true
		})
		out = append(out, tf)
	}
	return out
}

// bareFunc strips a receiver qualifier: "Client.doRaw" → "doRaw", "jsonPost" → "jsonPost".
func bareFunc(f string) string {
	if i := strings.LastIndex(f, "."); i >= 0 {
		return f[i+1:]
	}
	return f
}

// liveTransportFns walks the live tree and reports how many non-test files
// each root contributed.
func liveTransportFns(t *testing.T, fset *token.FileSet, reg []secretscan.Transport) ([]*transportFn, map[string]int) {
	t.Helper()
	var fns []attributedFn
	visited := map[string]int{}
	for _, dir := range transportRoots {
		pkg := filepath.Base(dir)
		visited[pkg] = 0
		for _, name := range goFilesIn(t, dir) {
			visited[pkg]++
			b, err := os.ReadFile(name)
			if err != nil {
				t.Fatal(err)
			}
			fns = append(fns, attributeFile(t, fset, name, string(b), pkg)...)
		}
	}
	return classify(fns, reg), visited
}

func fixtureTransportFns(t *testing.T, fset *token.FileSet, reg []secretscan.Transport) []*transportFn {
	t.Helper()
	name := filepath.Join("testdata", "secretscan", "leaky_transport.go.txt")
	b, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	return classify(attributeFile(t, fset, name, string(b), "ship"), reg)
}

// --- the judge --------------------------------------------------------------

func firstPos(calls []transportCall, want func(transportCall) bool) token.Pos {
	best := token.NoPos
	for _, c := range calls {
		if !want(c) {
			continue
		}
		if best == token.NoPos || c.call.Pos() < best {
			best = c.call.Pos()
		}
	}
	return best
}

// isGetNil reports whether an http.NewRequest call has a literal "GET" method
// and a nil body.
func isGetNil(c *ast.CallExpr) bool {
	if len(c.Args) != 3 {
		return false
	}
	lit, ok := c.Args[0].(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING || lit.Value != `"GET"` {
		return false
	}
	id, ok := c.Args[2].(*ast.Ident)
	return ok && id.Name == "nil"
}

// judgeTransports runs every arm over the walked functions against a registry.
func judgeTransports(fset *token.FileSet, fns []*transportFn, reg []secretscan.Transport) []transportFinding {
	var out []transportFinding
	byName := map[string]secretscan.Transport{}
	for _, tr := range reg {
		byName[tr.Name()] = tr
	}
	known := map[string]*transportFn{}
	for _, fn := range fns {
		known[fn.name] = fn
	}
	at := func(p token.Pos) string { return fset.Position(p).String() }

	// Seam callers, over the whole walk: bare seam ident → set of caller names.
	seamCallers := map[string]map[string]bool{}
	for _, fn := range fns {
		for _, r := range fn.requests {
			if s, ok := strings.CutPrefix(r.kind, "seam:"); ok {
				if seamCallers[s] == nil {
					seamCallers[s] = map[string]bool{}
				}
				seamCallers[s][fn.name] = true
			}
		}
	}

	for _, fn := range fns {
		if len(fn.requests) == 0 {
			continue
		}
		tr, ok := byName[fn.name]
		if !ok {
			r := fn.requests[0]
			out = append(out, transportFinding{fn.name, "undeclared",
				"calls " + r.kind + " at " + at(r.call.Pos()) + " with no secretscan.Transports() entry"})
			continue
		}
		switch {
		case tr.Screened != nil:
			want := "secretscan." + tr.Screened.Call
			screen := firstPos(fn.screens, func(c transportCall) bool { return c.kind == want })
			if screen == token.NoPos {
				out = append(out, transportFinding{fn.name, "screen-missing",
					"declared Screened{" + tr.Screened.Call + "} but calls no " + want})
				break
			}
			req := firstPos(fn.requests, func(transportCall) bool { return true })
			if screen > req {
				out = append(out, transportFinding{fn.name, "screen-order",
					want + " at " + at(screen) + " follows the request at " + at(req)})
			}
		case tr.ReadOnly != nil:
			newReqs := 0
			for _, r := range fn.requests {
				switch r.kind {
				case "http.NewRequest":
					newReqs++
					if !isGetNil(r.call) {
						out = append(out, transportFinding{fn.name, "read-only",
							"http.NewRequest at " + at(r.call.Pos()) + " is not a literal \"GET\" with a nil body"})
					}
				case "Do":
				default:
					out = append(out, transportFinding{fn.name, "read-only",
						"declared ReadOnly but calls " + r.kind + " at " + at(r.call.Pos())})
				}
			}
			if newReqs == 0 {
				out = append(out, transportFinding{fn.name, "read-only",
					"declared ReadOnly but has no http.NewRequest call to judge"})
			}
		case tr.Seam != nil:
			caller := tr.Seam.ScreenedCaller
			if c, ok := byName[caller]; !ok || c.Screened == nil {
				out = append(out, transportFinding{fn.name, "seam-caller",
					"ScreenedCaller " + caller + " is not a registered Screened transport"})
			}
			callers := seamCallers[fn.bare]
			if !callers[caller] {
				out = append(out, transportFinding{fn.name, "seam-caller",
					"ScreenedCaller " + caller + " never calls the seam identifier " + fn.bare})
			}
			for _, name := range sortedKeys(callers) {
				if name != caller {
					out = append(out, transportFinding{name, "seam-caller",
						"calls seam " + fn.name + " outside its ScreenedCaller " + caller})
				}
			}
		}
	}

	// Stale arm: every declaration resolves to a FuncDecl or package var.
	for _, tr := range reg {
		if _, ok := known[tr.Name()]; !ok {
			out = append(out, transportFinding{tr.Name(), "stale",
				"declared in secretscan.Transports() but no FuncDecl or package-level var with that name exists in " + tr.Package})
		}
	}
	return out
}

func findingsFor(fs []transportFinding, fn string) []transportFinding {
	var out []transportFinding
	for _, f := range fs {
		if f.fn == fn {
			out = append(out, f)
		}
	}
	return out
}

func flaggedFns(fs []transportFinding) []string {
	set := map[string]bool{}
	for _, f := range fs {
		set[f.fn] = true
	}
	return sortedKeys(set)
}

func fixtureRegistry() []secretscan.Transport {
	return []secretscan.Transport{
		{Package: "ship", Func: "cleanPost", Screened: &secretscan.Screened{Call: "ScanPayload"}},
		{Package: "ship", Func: "lateScreen", Screened: &secretscan.Screened{Call: "ScanPayload"}},
		{Package: "ship", Func: "readOnlyGet", ReadOnly: &secretscan.ReadOnly{Why: "GET, nil body"}},
		{Package: "ship", Func: "screenedGH", Screened: &secretscan.Screened{Call: "ScanArgv"}},
		{Package: "ship", Func: "ghCommand", Seam: &secretscan.Seam{ScreenedCaller: "ship.screenedGH"}},
	}
}

// --- live arms --------------------------------------------------------------

// TestEveryOutboundSeamIsRegistered is the undeclared arm over the live tree,
// with the vacuity floors: ≥ 10 sites, spread over both packages, and the
// func-literal var attribution proven on open.go's ghCommand.
func TestEveryOutboundSeamIsRegistered(t *testing.T) {
	fset := token.NewFileSet()
	reg := secretscan.Transports()
	fns, visited := liveTransportFns(t, fset, reg)
	for pkg, n := range visited {
		if n == 0 {
			t.Errorf("the walk visited no non-test file in %s — every assertion over it is vacuous", pkg)
		}
	}

	sites := 0
	perPkg := map[string]int{}
	for _, fn := range fns {
		if len(fn.requests) > 0 {
			sites++
			perPkg[strings.SplitN(fn.name, ".", 2)[0]]++
		}
	}
	if sites < 10 {
		t.Errorf("the walk resolved %d transport sites, want at least 10 — it is passing because it found nothing to check", sites)
	}
	for _, pkg := range []string{"ship", "forge"} {
		if perPkg[pkg] == 0 {
			t.Errorf("no transport site resolved in %s", pkg)
		}
	}

	seam := false
	for _, fn := range fns {
		if fn.name != "ship.ghCommand" {
			continue
		}
		for _, r := range fn.requests {
			if r.kind == "exec.Command" {
				seam = true
			}
		}
	}
	if !seam {
		t.Error("the exec.Command inside `var ghCommand = func(...)` was not attributed to ship.ghCommand — func-literal-bound package vars are being lost")
	}

	for _, f := range judgeTransports(fset, fns, reg) {
		t.Error(f)
	}
}

// TestScreenPrecedesRequestInEveryScreenedTransport states the ordering arm
// explicitly for each Screened entry rather than only through the judge.
func TestScreenPrecedesRequestInEveryScreenedTransport(t *testing.T) {
	fset := token.NewFileSet()
	reg := secretscan.Transports()
	fns, _ := liveTransportFns(t, fset, reg)
	byName := map[string]*transportFn{}
	for _, fn := range fns {
		byName[fn.name] = fn
	}
	checked := 0
	for _, tr := range reg {
		if tr.Screened == nil {
			continue
		}
		fn, ok := byName[tr.Name()]
		if !ok {
			t.Errorf("%s: not found in the walk", tr.Name())
			continue
		}
		want := "secretscan." + tr.Screened.Call
		screen := firstPos(fn.screens, func(c transportCall) bool { return c.kind == want })
		req := firstPos(fn.requests, func(transportCall) bool { return true })
		if screen == token.NoPos || req == token.NoPos {
			t.Errorf("%s: screen=%v request=%v — one side is missing, the ordering cannot be judged", tr.Name(), screen, req)
			continue
		}
		if screen > req {
			t.Errorf("%s: %s at %s follows the request at %s", tr.Name(), want, fset.Position(screen), fset.Position(req))
		}
		checked++
	}
	if checked < 8 {
		t.Errorf("checked %d screened transports, want the 8 in the registry", checked)
	}
}

// TestSeamHasExactlyOneScreenedCaller: Validate rejects a Seam with no
// ScreenedCaller, and over the live tree the only function calling the
// ghCommand identifier is screenedGH (t-4's TestNoRawGhCommandCallOutsideTheSeam
// folded in).
func TestSeamHasExactlyOneScreenedCaller(t *testing.T) {
	errs := secretscan.Validate([]secretscan.Transport{{Package: "ship", Func: "x", Seam: &secretscan.Seam{}}})
	if len(errs) != 1 || !strings.Contains(errs[0].Error(), "no ScreenedCaller") {
		t.Errorf("Validate must reject a Seam with an empty ScreenedCaller: %v", errs)
	}

	fset := token.NewFileSet()
	reg := secretscan.Transports()
	fns, _ := liveTransportFns(t, fset, reg)
	callers := map[string]bool{}
	for _, fn := range fns {
		for _, r := range fn.requests {
			if r.kind == "seam:ghCommand" {
				callers[fn.name] = true
			}
		}
	}
	if got := sortedKeys(callers); len(got) != 1 || got[0] != "ship.screenedGH" {
		t.Errorf("functions calling ghCommand = %v, want exactly [ship.screenedGH]", got)
	}
}

// TestReadOnlyTransportsSendNoBody: each ReadOnly entry's http.NewRequest has
// a "GET" literal first arg and a nil third arg.
func TestReadOnlyTransportsSendNoBody(t *testing.T) {
	fset := token.NewFileSet()
	reg := secretscan.Transports()
	fns, _ := liveTransportFns(t, fset, reg)
	byName := map[string]*transportFn{}
	for _, fn := range fns {
		byName[fn.name] = fn
	}
	checked := 0
	for _, tr := range reg {
		if tr.ReadOnly == nil {
			continue
		}
		fn, ok := byName[tr.Name()]
		if !ok {
			t.Errorf("%s: not found in the walk", tr.Name())
			continue
		}
		newReqs := 0
		for _, r := range fn.requests {
			if r.kind != "http.NewRequest" {
				continue
			}
			newReqs++
			if !isGetNil(r.call) {
				t.Errorf("%s: http.NewRequest at %s is not (\"GET\", …, nil)", tr.Name(), fset.Position(r.call.Pos()))
			}
		}
		if newReqs == 0 {
			t.Errorf("%s: no http.NewRequest call to judge", tr.Name())
		}
		checked++
	}
	if checked == 0 {
		t.Error("no ReadOnly entry in the registry — the arm asserts nothing")
	}
}

// --- calibration -----------------------------------------------------------

// TestTransportScanTripsOnTheLeakyFixture drives every arm over a fixture
// whose answers are written down: a, b, d and f are flagged; c and e — and the
// seam pair itself — pass.
func TestTransportScanTripsOnTheLeakyFixture(t *testing.T) {
	fset := token.NewFileSet()
	reg := fixtureRegistry()
	if errs := secretscan.Validate(reg); len(errs) != 0 {
		t.Fatalf("fixture registry malformed: %v", errs)
	}
	findings := judgeTransports(fset, fixtureTransportFns(t, fset, reg), reg)

	want := []string{"ship.lateScreen", "ship.leakyGH", "ship.leakyPost", "ship.rawGH"}
	if got := flaggedFns(findings); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("flagged %v, want %v\n%v", got, want, findings)
	}
	arm := func(fn, arm string) {
		t.Helper()
		for _, f := range findingsFor(findings, fn) {
			if f.arm == arm {
				return
			}
		}
		t.Errorf("%s: no %s finding: %v", fn, arm, findingsFor(findings, fn))
	}
	arm("ship.leakyPost", "undeclared")    // (a)
	arm("ship.lateScreen", "screen-order") // (b)
	arm("ship.leakyGH", "seam-caller")     // (d)
	arm("ship.rawGH", "undeclared")        // (f)
	for _, fn := range []string{"ship.cleanPost", "ship.readOnlyGet", "ship.screenedGH", "ship.ghCommand"} {
		if fs := findingsFor(findings, fn); len(fs) != 0 {
			t.Errorf("%s must pass, got %v", fn, fs)
		}
	}
}

// TestStaleTransportDeclarationFails feeds the live walk a registry entry
// naming a function that does not exist; the stale arm must name it.
func TestStaleTransportDeclarationFails(t *testing.T) {
	fset := token.NewFileSet()
	reg := append(secretscan.Transports(),
		secretscan.Transport{Package: "ship", Func: "nothingHere", Screened: &secretscan.Screened{Call: "ScanPayload"}},
		secretscan.Transport{Package: "forge", Func: "Ghost.doRaw", Screened: &secretscan.Screened{Call: "ScanPayload"}},
	)
	fns, _ := liveTransportFns(t, fset, reg)
	findings := judgeTransports(fset, fns, reg)
	for _, name := range []string{"ship.nothingHere", "forge.Ghost.doRaw"} {
		stale := false
		for _, f := range findingsFor(findings, name) {
			if f.arm == "stale" {
				stale = true
			}
		}
		if !stale {
			t.Errorf("%s: not reported as stale: %v", name, findings)
		}
	}
	if got := flaggedFns(findings); len(got) != 2 {
		t.Errorf("only the two synthetic entries should be flagged, got %v", got)
	}
}
