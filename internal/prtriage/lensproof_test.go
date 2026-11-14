package prtriage_test

import (
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// The c-4 proof fixture: a nested Go module whose order package holds the
// Status value set, a seeded handler carrying one partial Status switch with a
// silent default and one uncommented error discard, and a clean twin with the
// near-misses (exhaustive switch, loud default, reason-commented discard).
// These helpers compute the defect sites from source, so the recorded proof
// run (t-6) is checked against lines the fixture really has.

const lensFixtureDir = "../../fixtures/review-edge-lenses-c4"

const lensFixtureModule = "example.com/lensproof"

// lensFixture is one parsed, type-checked handler file.
type lensFixture struct {
	fset *token.FileSet
	file *ast.File
	info *types.Info
}

// fixtureImporter resolves the fixture's own order package from source and
// everything else (the standard library) from the default importer.
type fixtureImporter struct {
	order *types.Package
	std   types.Importer
}

func (fi fixtureImporter) Import(path string) (*types.Package, error) {
	if path == lensFixtureModule+"/order" {
		return fi.order, nil
	}
	return fi.std.Import(path)
}

func parseFixtureFile(t *testing.T, fset *token.FileSet, rel string) *ast.File {
	t.Helper()
	f, err := parser.ParseFile(fset, filepath.Join(lensFixtureDir, rel), nil, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse %s: %v", rel, err)
	}
	return f
}

// loadLensFixture parses and type-checks <dir>/handler.go against order/.
func loadLensFixture(t *testing.T, dir string) lensFixture {
	t.Helper()
	fset := token.NewFileSet()
	orderFile := parseFixtureFile(t, fset, "order/status.go")
	orderPkg, err := (&types.Config{}).Check(lensFixtureModule+"/order", fset, []*ast.File{orderFile}, nil)
	if err != nil {
		t.Fatalf("type-check order/status.go: %v", err)
	}
	file := parseFixtureFile(t, fset, dir+"/handler.go")
	info := &types.Info{Types: map[ast.Expr]types.TypeAndValue{}}
	conf := types.Config{Importer: fixtureImporter{order: orderPkg, std: importer.Default()}}
	if _, err := conf.Check(lensFixtureModule+"/"+dir, fset, []*ast.File{file}, info); err != nil {
		t.Fatalf("type-check %s/handler.go: %v", dir, err)
	}
	return lensFixture{fset: fset, file: file, info: info}
}

// statusConsts returns the const names declared with type Status in
// order/status.go, in declaration order.
func statusConsts(t *testing.T) []string {
	t.Helper()
	f := parseFixtureFile(t, token.NewFileSet(), "order/status.go")
	return statusConstsIn(f)
}

func statusConstsIn(f *ast.File) []string {
	var out []string
	for _, d := range f.Decls {
		gd, ok := d.(*ast.GenDecl)
		if !ok || gd.Tok != token.CONST {
			continue
		}
		typed := false
		for _, s := range gd.Specs {
			vs := s.(*ast.ValueSpec)
			if id, ok := vs.Type.(*ast.Ident); ok {
				typed = id.Name == "Status"
			}
			if typed {
				for _, n := range vs.Names {
					out = append(out, n.Name)
				}
			}
		}
	}
	return out
}

// statusSwitch returns the handler's one switch statement, its case values
// (the Sel of each order.X) and its default clause (nil when absent).
func statusSwitch(t *testing.T, fx lensFixture) (*ast.SwitchStmt, []string, *ast.CaseClause) {
	t.Helper()
	var sw []*ast.SwitchStmt
	ast.Inspect(fx.file, func(n ast.Node) bool {
		if s, ok := n.(*ast.SwitchStmt); ok {
			sw = append(sw, s)
		}
		return true
	})
	if len(sw) != 1 {
		t.Fatalf("handler has %d switch statements, want 1", len(sw))
	}
	var cases []string
	var def *ast.CaseClause
	for _, st := range sw[0].Body.List {
		cc := st.(*ast.CaseClause)
		if cc.List == nil {
			def = cc
			continue
		}
		for _, e := range cc.List {
			sel, ok := e.(*ast.SelectorExpr)
			if !ok {
				t.Fatalf("switch case %T is not an order.<Status> selector", e)
			}
			cases = append(cases, sel.Sel.Name)
		}
	}
	return sw[0], cases, def
}

// armIsLoud reports whether a case body errors, panics or logs: a panic call,
// an fmt.Errorf / errors.New, or any log / slog call.
func armIsLoud(cc *ast.CaseClause) bool {
	loud := false
	for _, st := range cc.Body {
		ast.Inspect(st, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			switch fn := call.Fun.(type) {
			case *ast.Ident:
				if fn.Name == "panic" {
					loud = true
				}
			case *ast.SelectorExpr:
				if x, ok := fn.X.(*ast.Ident); ok {
					switch {
					case x.Name == "log" || x.Name == "slog":
						loud = true
					case x.Name == "fmt" && fn.Sel.Name == "Errorf", x.Name == "errors" && fn.Sel.Name == "New":
						loud = true
					}
				}
			}
			return true
		})
	}
	return loud
}

// discard is one blank assignment of a call result: `_ = f(...)`.
type discard struct {
	line      int
	commented bool // a comment ends on the line above or sits on the same line
	isError   bool // the call returns error (alone or as its last result)
}

func lensDiscards(fx lensFixture) []discard {
	commentLines := map[int]bool{}
	for _, cg := range fx.file.Comments {
		commentLines[fx.fset.Position(cg.End()).Line] = true
	}
	errType := types.Universe.Lookup("error").Type()
	var out []discard
	ast.Inspect(fx.file, func(n ast.Node) bool {
		as, ok := n.(*ast.AssignStmt)
		if !ok || len(as.Rhs) != 1 {
			return true
		}
		call, ok := as.Rhs[0].(*ast.CallExpr)
		if !ok {
			return true
		}
		for _, l := range as.Lhs {
			if id, ok := l.(*ast.Ident); !ok || id.Name != "_" {
				return true
			}
		}
		line := fx.fset.Position(as.Pos()).Line
		isErr := false
		switch typ := fx.info.Types[call].Type.(type) {
		case *types.Tuple:
			isErr = typ.Len() > 0 && types.Identical(typ.At(typ.Len()-1).Type(), errType)
		case nil:
		default:
			isErr = types.Identical(typ, errType)
		}
		out = append(out, discard{line: line, commented: commentLines[line-1] || commentLines[line], isError: isErr})
		return true
	})
	return out
}

// seededSwitchSpan is the seeded switch's first and last line.
func seededSwitchSpan(t *testing.T) (start, end int) {
	t.Helper()
	fx := loadLensFixture(t, "seeded")
	sw, _, _ := statusSwitch(t, fx)
	return fx.fset.Position(sw.Pos()).Line, fx.fset.Position(sw.End()).Line
}

// seededDiscardLine is the line of the seeded handler's one swallowed error.
func seededDiscardLine(t *testing.T) int {
	t.Helper()
	ds := lensDiscards(loadLensFixture(t, "seeded"))
	if len(ds) != 1 {
		t.Fatalf("seeded handler has %d blank discards, want 1: %+v", len(ds), ds)
	}
	return ds[0].line
}

// declaresStatus reports whether a handler declares Status or its values
// itself, which would put the value set inside the reviewed diff.
func declaresStatus(f *ast.File) bool {
	if len(statusConstsIn(f)) > 0 {
		return true
	}
	for _, d := range f.Decls {
		if gd, ok := d.(*ast.GenDecl); ok && gd.Tok == token.TYPE {
			for _, s := range gd.Specs {
				if s.(*ast.TypeSpec).Name.Name == "Status" {
					return true
				}
			}
		}
	}
	return false
}

func TestLensFixtureSeededShape(t *testing.T) {
	consts := statusConsts(t)
	if want := []string{"Pending", "Paid", "Shipped", "Refunded"}; !slices.Equal(consts, want) {
		t.Fatalf("order/status.go Status consts = %v, want %v (the value set must live outside the reviewed handler)", consts, want)
	}
	fx := loadLensFixture(t, "seeded")
	if declaresStatus(fx.file) {
		t.Error("seeded/handler.go declares Status or its values; the value set must stay in order/status.go")
	}

	_, cases, def := statusSwitch(t, fx)
	var missing []string
	for _, c := range consts {
		if !slices.Contains(cases, c) {
			missing = append(missing, c)
		}
	}
	for _, c := range cases {
		if !slices.Contains(consts, c) {
			t.Errorf("seeded switch handles %q, which is not a Status const", c)
		}
	}
	if !slices.Equal(missing, []string{"Refunded"}) {
		t.Errorf("seeded switch leaves %v unhandled, want exactly [Refunded]", missing)
	}
	if def == nil {
		t.Error("seeded switch has no default arm; it must have a silent one")
	} else if armIsLoud(def) {
		t.Error("seeded switch's default arm errors, panics or logs; it must be silent")
	}

	ds := lensDiscards(fx)
	if len(ds) != 1 {
		t.Fatalf("seeded handler has %d blank call discards, want exactly 1: %+v", len(ds), ds)
	}
	if ds[0].commented {
		t.Errorf("seeded discard at line %d has an adjacent comment; it must be uncommented", ds[0].line)
	}
	if !ds[0].isError {
		t.Errorf("seeded discard at line %d does not discard an error-returning call", ds[0].line)
	}
}

func TestLensFixtureCleanShape(t *testing.T) {
	consts := statusConsts(t)
	fx := loadLensFixture(t, "clean")
	if declaresStatus(fx.file) {
		t.Error("clean/handler.go declares Status or its values; the value set must stay in order/status.go")
	}

	_, cases, def := statusSwitch(t, fx)
	for _, c := range consts {
		if !slices.Contains(cases, c) {
			t.Errorf("clean switch leaves %q unhandled", c)
		}
	}
	if def == nil {
		t.Error("clean switch lost its default arm (the loud-default near-miss)")
	} else if !armIsLoud(def) {
		t.Error("clean switch's default arm is silent; it must error, panic or log")
	}

	ds := lensDiscards(fx)
	if len(ds) != 1 {
		t.Fatalf("clean handler has %d blank call discards, want exactly 1 (the commented near-miss): %+v", len(ds), ds)
	}
	if !ds[0].commented {
		t.Errorf("clean discard at line %d lost its reason comment", ds[0].line)
	}
	if !ds[0].isError {
		t.Errorf("clean discard at line %d does not discard an error-returning call", ds[0].line)
	}
}

func TestLensFixtureIsOwnModule(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(lensFixtureDir, "go.mod"))
	if err != nil {
		t.Fatalf("the fixture must be its own module so ./..., vet and gremlins never see it: %v", err)
	}
	first, _, _ := strings.Cut(string(raw), "\n")
	if first != "module "+lensFixtureModule {
		t.Errorf("fixture go.mod declares %q, want %q", first, "module "+lensFixtureModule)
	}
}

// TestLensFixtureHelpersLocateDefects pins the helpers t-6 reads the recorded
// proof against: the discard sits outside the switch, and both are found.
func TestLensFixtureHelpersLocateDefects(t *testing.T) {
	start, end := seededSwitchSpan(t)
	line := seededDiscardLine(t)
	if start <= 0 || end <= start {
		t.Fatalf("seeded switch span = %d..%d", start, end)
	}
	if line >= start && line <= end {
		t.Errorf("seeded discard line %d falls inside the switch span %d..%d", line, start, end)
	}
}
