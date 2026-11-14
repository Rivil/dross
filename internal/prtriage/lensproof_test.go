package prtriage_test

import (
	"fmt"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/prtriage"
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

// recordedSections reads a recorded proof comment, requires SplitReview to
// accept it whole, and returns its findings by `### ` section title (the
// heading minus its "(N findings)" count). A section split alone numbers its
// findings from 1; only Loc, Severity and Text are read.
func recordedSections(t *testing.T, name string) map[string][]prtriage.Finding {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(lensFixtureDir, name))
	if err != nil {
		t.Fatal(err)
	}
	body := string(raw)
	if _, ok := prtriage.SplitReview(body); !ok {
		t.Fatalf("%s no longer splits as a /dross-review comment", name)
	}
	bodies, err := lensSectionLines(body)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	out := map[string][]prtriage.Finding{}
	for title, lines := range bodies {
		// ok is false only for a section with no findings; fs is then nil,
		// which the callers read as "no findings".
		fs, _ := prtriage.SplitReview(prtriage.ReviewHeader + "\n" + strings.Join(lines, "\n"))
		out[title] = fs
	}
	return out
}

// proofLenses are the six `### ` section titles a recorded comment may hold.
var proofLenses = []string{"Security", "Code quality", "Test efficacy", "Spec fidelity", "Edge cases", "Silent failures"}

// lensSectionLines walks a recorded comment and returns each `### ` lens
// section's lines, up to the `---` footer. Every heading after the comment's
// first line must be a `### ` lens heading: one recorded at another level
// (`## Edge cases`, `#### Silent failures`) would otherwise leave its findings
// filed under the previous section, and a clean-run check would pass on a
// comment whose new-lens sections it never read.
func lensSectionLines(body string) (map[string][]string, error) {
	bodies := map[string][]string{}
	var cur string
	for i, l := range strings.Split(body, "\n") {
		if h, ok := strings.CutPrefix(l, "### "); ok {
			cur, _, _ = strings.Cut(h, " (")
			if !slices.Contains(proofLenses, cur) {
				return nil, fmt.Errorf("line %d: section %q is not one of the six lens headings %q", i+1, cur, proofLenses)
			}
			continue
		}
		if i > 0 && markdownHeading.MatchString(l) {
			return nil, fmt.Errorf("line %d: heading %q is not a `### ` lens heading; its findings would be read as part of section %q", i+1, l, cur)
		}
		if l == "---" {
			cur = ""
		}
		if cur != "" {
			bodies[cur] = append(bodies[cur], l)
		}
	}
	return bodies, nil
}

// markdownHeading matches an ATX heading line at any level.
var markdownHeading = regexp.MustCompile(`^#{1,6}(\s|$)`)

func TestRecordedSectionsRefusesMisleveledHeading(t *testing.T) {
	head := prtriage.ReviewHeader + " — phase review-edge-lenses\n\n"
	finding := "- **FLAG** — `seeded/handler.go:29` — error discarded\n"
	for _, tc := range []struct{ name, heading string }{
		{"h4 lens", "#### Silent failures (1 finding)"},
		{"h2 lens", "## Edge cases (1 finding)"},
		{"bare h4", "####"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := head + "### Test efficacy (1 finding)\n" + finding + "\n" + tc.heading + "\n" + finding + "\n---\n"
			if _, err := lensSectionLines(body); err == nil {
				t.Errorf("a comment holding %q was accepted; its finding would be filed under Test efficacy", tc.heading)
			}
		})
	}

	ok := head + "### Test efficacy (1 finding)\n" + finding + "\n### Silent failures (1 finding)\n" + finding + "\n---\n*Posted by `/dross-review`.*\n"
	secs, err := lensSectionLines(ok)
	if err != nil {
		t.Fatalf("a well-levelled comment was refused: %v", err)
	}
	if len(secs["Silent failures"]) == 0 || len(secs["Test efficacy"]) == 0 {
		t.Errorf("well-levelled sections not both read: %q", secs)
	}

	if _, err := lensSectionLines(head + "### Style (1 finding)\n" + finding); err == nil {
		t.Error("a non-lens ### section was accepted")
	}
}

// locLine returns a finding's line in <dir>/handler.go, or 0 when its Loc
// names another file or no line. A lens may cite the file by any suffix of
// its path, down to a bare handler.go (the reviewed diff holds only one).
func locLine(f prtriage.Finding, dir string) int {
	path, lines, ok := strings.Cut(f.Loc, ":")
	base := dir + "/handler.go"
	if !ok || (path != "handler.go" && path != base && !strings.HasSuffix(path, "/"+base)) {
		return 0
	}
	first, _, _ := strings.Cut(lines, "-")
	n, err := strconv.Atoi(first)
	if err != nil {
		return 0
	}
	return n
}

func TestLensProofSeeded(t *testing.T) {
	start, end := seededSwitchSpan(t)
	discard := seededDiscardLine(t)
	secs := recordedSections(t, "seeded-comment.md")

	edgeHit := false
	for _, f := range secs["Edge cases"] {
		if l := locLine(f, "seeded"); l >= start && l <= end && strings.Contains(f.Text, "Refunded") && strings.Contains(f.Text, "status.go") {
			edgeHit = true
		}
	}
	if !edgeHit {
		t.Errorf("Edge cases holds no finding inside the seeded switch (lines %d-%d) naming Refunded and status.go: %+v", start, end, secs["Edge cases"])
	}

	silentHit := false
	for _, f := range secs["Silent failures"] {
		if locLine(f, "seeded") == discard {
			silentHit = true
		}
	}
	if !silentHit {
		t.Errorf("Silent failures holds no finding at the seeded discard (line %d): %+v", discard, secs["Silent failures"])
	}

	// c-6: one defect, one lens.
	for _, sec := range []string{"Code quality", "Edge cases"} {
		for _, f := range secs[sec] {
			if locLine(f, "seeded") == discard {
				t.Errorf("%s also reports the swallowed error at line %d: %q", sec, discard, f.Text)
			}
		}
	}
}

func TestLensProofClean(t *testing.T) {
	secs := recordedSections(t, "clean-comment.md")
	for _, sec := range []string{"Edge cases", "Silent failures"} {
		for _, f := range secs[sec] {
			t.Errorf("%s reported a finding on the clean fixture: %q", sec, f.Text)
		}
	}
}
