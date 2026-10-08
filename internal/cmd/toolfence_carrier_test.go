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

	"github.com/Rivil/dross/internal/toolfence"
)

// c-4, the assignment half: every write to a Recorded field comes from the
// shared recorder.
//
// The stored field is a plain Go string — the carrier's unexported field cannot
// marshal — so no type can carry this guarantee at the assignment. The guard
// therefore keys on the CALL: an accepted right-hand side either runs through
// mutation.RecordLegError or copies an already-declared field.
//
// A NotToolStream field is deliberately outside this arm. lifecycle.go composes
// Mutant.Note with fmt.Sprintf and must stay green, or the guard is unadoptable
// rather than fail-closed — which is exactly why reclassification cannot be the
// cheap way out of a future red. TestRecordedFieldsCannotBeReclassifiedAway is
// the other side of that.

// carrierName is the last segment of the Recorded entries' declared Carrier —
// read from the registry rather than written here, so changing the carrier
// changes what this guard accepts.
func carrierName(t *testing.T) string {
	t.Helper()
	seen := map[string]bool{}
	for _, f := range toolfence.Fields() {
		if f.Recorded != nil {
			parts := strings.Split(f.Recorded.Carrier, ".")
			seen[parts[len(parts)-1]] = true
		}
	}
	if len(seen) != 1 {
		t.Fatalf("the Recorded entries name %d distinct carriers, want exactly 1: %v", len(seen), seen)
	}
	for k := range seen {
		return k
	}
	return ""
}

// recordedFields maps a struct's short name to the Recorded field names on it.
func recordedFields(t *testing.T) map[string]map[string]bool {
	t.Helper()
	out := map[string]map[string]bool{}
	for _, f := range toolfence.Fields() {
		if f.Recorded == nil {
			continue
		}
		short := f.Struct
		if i := strings.LastIndex(short, "."); i >= 0 {
			short = short[i+1:]
		}
		if out[short] == nil {
			out[short] = map[string]bool{}
		}
		out[short][f.Field] = true
	}
	if len(out) == 0 {
		t.Fatal("the registry declares no Recorded field, so this whole arm asserts nothing")
	}
	return out
}

// TestEveryRecordedWriteComesFromTheCarrier runs the arm over the live tree.
func TestEveryRecordedWriteComesFromTheCarrier(t *testing.T) {
	carrier := carrierName(t)
	recorded := recordedFields(t)

	total, byRoot := 0, map[string]int{}
	for _, dir := range toolfenceRoots(t) {
		for _, name := range goFilesIn(t, dir) {
			b, err := os.ReadFile(name)
			if err != nil {
				t.Fatal(err)
			}
			ok, bad := scanRecordedWrites(t, name, string(b), carrier, recorded)
			total += ok + len(bad)
			byRoot[dir] += ok + len(bad)
			for _, hit := range bad {
				t.Errorf("%s — assign it through %s(err).String(), or copy an already-declared field", hit, carrier)
			}
		}
	}

	// Vacuity floor. Not "at least one per root": internal/telemetry holds no
	// writer at all — Event.ErrorDetail's only writer in the tree is
	// internal/cmd/telemetry.go, which the registry says in as many words. The
	// floor is that the walk saw Recorded writes AT ALL, and saw them where the
	// registry says they live.
	if total == 0 {
		t.Fatal("the walk found no write to any Recorded field — it is passing because it resolved nothing")
	}
	if byRoot["../verify"] == 0 {
		t.Error("no Recorded write resolved in internal/verify, which holds the record-and-continue site and the leg-summary copy")
	}
}

// TestCarrierScanIsCalibrated drives the same scan over the two fixtures, whose
// answers are written down. The live tree is clean, so without this the scan
// passes exactly as well when it has stopped matching.
func TestCarrierScanIsCalibrated(t *testing.T) {
	carrier := carrierName(t)
	recorded := recordedFields(t)

	t.Run("writer_ok", func(t *testing.T) {
		ok, bad := scanRecordedWrites(t, "writer_ok.go", readToolfenceFixture(t, "writer_ok.go.txt"), carrier, recorded)
		if len(bad) != 0 {
			t.Errorf("the accepted shapes tripped the guard: %v", bad)
		}
		// The accepted arm must cover a METHOD CALL and a CONVERSION wrapping
		// the carrier's return, and a copy of an already-declared field — not
		// only a bare call.
		if ok != 3 {
			t.Errorf("the scan accepted %d Recorded writes in writer_ok, want 3 (carrier, conversion-wrapped carrier, field copy)", ok)
		}
	})

	t.Run("writer_raw", func(t *testing.T) {
		ok, bad := scanRecordedWrites(t, "writer_raw.go", readToolfenceFixture(t, "writer_raw.go.txt"), carrier, recorded)
		if len(bad) != 3 {
			t.Errorf("the scan reported %d raw writes in writer_raw, want 3 (a literal, a Sprintf, and err.Error()): %v", len(bad), bad)
		}
		if ok != 0 {
			t.Errorf("the scan accepted %d of writer_raw's writes", ok)
		}
	})

	// The NotToolStream exemption, pinned from the accepting side: a raw
	// fmt.Sprintf into OutOfScopeMutant.Note is not this arm's business.
	t.Run("not-tool-stream is untouched", func(t *testing.T) {
		_, bad := scanRecordedWrites(t, "writer_ok.go", readToolfenceFixture(t, "writer_ok.go.txt"), carrier, recorded)
		for _, hit := range bad {
			if strings.Contains(hit, "Note") {
				t.Errorf("a NotToolStream field was reported by the Recorded arm: %s", hit)
			}
		}
	})
}

// TestCmdRootIsWalkedNotJustDeclared. internal/cmd holds three declared fields'
// live writers, so a walk that quietly skipped its own package would leave those
// unguarded while every assertion above stayed green.
//
// The proof is the raw fixture under a cmd-root filename: same source, same
// scan, and it must trip there too.
func TestCmdRootIsWalkedNotJustDeclared(t *testing.T) {
	roots := toolfenceRoots(t)
	if len(roots) == 0 || roots[0] != "." {
		t.Fatalf("toolfence.Roots() no longer names internal/cmd first: %v", roots)
	}
	if n := len(goFilesIn(t, ".")); n == 0 {
		t.Fatal("the walk lists no non-test file in internal/cmd — the root is declared but not walked")
	}

	_, bad := scanRecordedWrites(t, "./synthetic_writer.go",
		readToolfenceFixture(t, "writer_raw.go.txt"), carrierName(t), recordedFields(t))
	if len(bad) == 0 {
		t.Error("the raw fixture placed under the internal/cmd root did not trip the guard")
	}
}

// TestRecordedFieldsCannotBeReclassifiedAway. Flipping LegSummary.Error or
// LanguageRun.Error to NotToolStream would silence writer_raw without removing
// a byte of tool output — the cheap way out of a future red. It fails here.
func TestRecordedFieldsCannotBeReclassifiedAway(t *testing.T) {
	want := []string{"verify.LanguageRun.Error", "verify.LegSummary.Error"}
	got := map[string]bool{}
	for _, f := range toolfence.Fields() {
		if f.Recorded != nil {
			got[f.Name()] = true
		}
	}
	for _, name := range want {
		if !got[name] {
			t.Errorf("%s is no longer Recorded — reclassifying it silences the raw-write guard without removing any tool output", name)
		}
	}

	// And the guard really does go quiet under a flip, which is why the pin
	// above is load-bearing rather than decorative.
	flipped := map[string]map[string]bool{"LegSummary": {}, "LanguageRun": {}}
	_, bad := scanRecordedWrites(t, "writer_raw.go", readToolfenceFixture(t, "writer_raw.go.txt"), carrierName(t), flipped)
	if len(bad) != 0 {
		t.Errorf("a registry declaring no Recorded field still reported %v — the scan is not keyed on the registry at all", bad)
	}
}

// --- the scan ---------------------------------------------------------------

// scanRecordedWrites returns how many Recorded writes were ACCEPTED and one
// report per write that was not.
//
// A write is in scope when it targets a Recorded field of a Recorded struct:
// either a composite-literal key on that struct, or `x.Field = …` where x was
// bound to that struct in the same function. Scoping by the receiver's binding
// is what keeps an unrelated `env.Error = &msg` out of the arm — the guard has
// no type information, so the binding is the only honest signal available.
func scanRecordedWrites(t *testing.T, name, src, carrier string, recorded map[string]map[string]bool) (int, []string) {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, name, src, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse %s: %v", name, err)
	}

	accepted := 0
	var bad []string
	report := func(pos token.Pos, field string, rhs ast.Expr) {
		if acceptedRHS(rhs, carrier, recorded) {
			accepted++
			return
		}
		bad = append(bad, fmt.Sprintf("%s:%d: a raw value is assigned to the Recorded field %s",
			name, fset.Position(pos).Line, field))
	}

	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}
		bound := boundToRecordedStruct(fn, recorded)
		ast.Inspect(fn, func(n ast.Node) bool {
			switch node := n.(type) {
			case *ast.CompositeLit:
				fields, ok := recorded[shortTypeName(node.Type)]
				if !ok {
					return true
				}
				for _, elt := range node.Elts {
					kv, ok := elt.(*ast.KeyValueExpr)
					if !ok {
						continue
					}
					if key, ok := kv.Key.(*ast.Ident); ok && fields[key.Name] {
						report(kv.Pos(), shortTypeName(node.Type)+"."+key.Name, kv.Value)
					}
				}
			case *ast.AssignStmt:
				for i, lhs := range node.Lhs {
					sel, ok := lhs.(*ast.SelectorExpr)
					if !ok {
						continue
					}
					recv, ok := sel.X.(*ast.Ident)
					if !ok {
						continue
					}
					structName, ok := bound[recv.Name]
					if !ok || !recorded[structName][sel.Sel.Name] {
						continue
					}
					if i < len(node.Rhs) {
						report(sel.Pos(), structName+"."+sel.Sel.Name, node.Rhs[i])
					}
				}
			}
			return true
		})
	}
	sort.Strings(bad)
	return accepted, bad
}

// acceptedRHS reports whether a right-hand side may write a Recorded field: it
// runs through the carrier (bare, or wrapped in a method call or conversion), or
// it copies a field that is itself declared Recorded.
func acceptedRHS(e ast.Expr, carrier string, recorded map[string]map[string]bool) bool {
	fromCarrier := false
	ast.Inspect(e, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		switch fn := call.Fun.(type) {
		case *ast.Ident:
			if fn.Name == carrier {
				fromCarrier = true
			}
		case *ast.SelectorExpr:
			if fn.Sel.Name == carrier {
				fromCarrier = true
			}
		}
		return true
	})
	if fromCarrier {
		return true
	}

	// A copy of an already-declared field. Not a no-op wrapper: the guarantee
	// travels with the value, and requiring a re-wrap here would be ceremony.
	// err.Error() is excluded — a method CALL is not a field read.
	sel, ok := e.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	for _, fields := range recorded {
		if fields[sel.Sel.Name] {
			return true
		}
	}
	return false
}

// boundToRecordedStruct maps local identifiers to the Recorded struct they hold,
// from `var x T`, `x := T{…}`, `x := &T{…}` and parameters of type T or *T.
func boundToRecordedStruct(fn *ast.FuncDecl, recorded map[string]map[string]bool) map[string]string {
	out := map[string]string{}
	bind := func(name string, typ ast.Expr) {
		if n := shortTypeName(typ); recorded[n] != nil {
			out[name] = n
		}
	}
	if fn.Type.Params != nil {
		for _, p := range fn.Type.Params.List {
			for _, id := range p.Names {
				bind(id.Name, p.Type)
			}
		}
	}
	ast.Inspect(fn, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.DeclStmt:
			gd, ok := node.Decl.(*ast.GenDecl)
			if !ok {
				return true
			}
			for _, spec := range gd.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok || vs.Type == nil {
					continue
				}
				for _, id := range vs.Names {
					bind(id.Name, vs.Type)
				}
			}
		case *ast.AssignStmt:
			if node.Tok != token.DEFINE {
				return true
			}
			for i, lhs := range node.Lhs {
				id, ok := lhs.(*ast.Ident)
				if !ok || i >= len(node.Rhs) {
					continue
				}
				rhs := node.Rhs[i]
				if u, ok := rhs.(*ast.UnaryExpr); ok {
					rhs = u.X
				}
				if cl, ok := rhs.(*ast.CompositeLit); ok {
					bind(id.Name, cl.Type)
				}
			}
		}
		return true
	})
	return out
}

// shortTypeName renders a type expression as its bare type name, dropping the
// package qualifier and any pointer: verify.LegSummary and *LegSummary both
// resolve to LegSummary, which is how the registry names it.
func shortTypeName(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.SelectorExpr:
		return t.Sel.Name
	case *ast.StarExpr:
		return shortTypeName(t.X)
	case *ast.ArrayType:
		return shortTypeName(t.Elt)
	}
	return ""
}

func readToolfenceFixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "toolfence", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
