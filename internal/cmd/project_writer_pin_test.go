package cmd

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// This guard pins project.toml to ONE write path: project.(*Project).Save.
//
// The lossless patcher lives behind Save, so a command that opens the file
// itself — os.WriteFile on a path built from project.File, a stray
// toml.NewEncoder pointed at a file — is a command that round-trips the
// struct and drops every comment and hand-added key the patcher exists to
// keep. The go/ast scan below reads every non-test .go under internal/ and
// refuses that shape by file:line; the synthetic cases prove the detector's
// teeth on source it has never seen, the same way hermetic_dross_read_test.go
// proves its own.
//
// THE RULE, outside internal/project: never open, create, rename onto or
// encode into project.toml — load it with project.Load, mutate the struct,
// call Save. Inside internal/project: exactly one BurntSushi encoder call
// site (encodeFresh), and the file itself is only ever written by
// writeAtomic, which only Save calls.

// fileWriters are the os functions that put bytes at a path; the index is
// which argument is the path (Rename's destination is its second).
var fileWriters = map[string]int{
	"WriteFile": 0,
	"Create":    0,
	"OpenFile":  0,
	"Rename":    1,
}

// encodeFreshCallers is every function in internal/project allowed to ask
// the encoder for text. A new entry is a new re-encode path and needs a
// reason here, not just a call.
var encodeFreshCallers = map[string]bool{
	"Save":            true, // a path that does not exist yet
	"verifyPatched":   true, // canonical form of both sides, compared
	"renderValue":     true, // one spliced value
	"encodeCanonical": true, // the differ's generic tree
}

// projectWriterViolations reports the lines in src (a file OUTSIDE
// internal/project) that write project.toml by any route but Save:
//
//   - (a) an os.WriteFile / os.Create / os.OpenFile / os.Rename whose path
//     argument mentions project.File or the literal "project.toml", directly
//     or through an identifier assigned from such an expression;
//   - (b) a toml.NewEncoder whose writer is anything but os.Stdout, or any
//     toml.Marshal, in a file that references project.File at all.
func projectWriterViolations(filename, src string) ([]string, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filename, src, 0)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", filename, err)
	}

	// Identifiers bound to an expression that mentions the project file, so
	// `p := filepath.Join(root, project.File); os.WriteFile(p, …)` is caught
	// like the inline form.
	tainted := map[string]bool{}
	ast.Inspect(file, func(n ast.Node) bool {
		as, ok := n.(*ast.AssignStmt)
		if !ok {
			return true
		}
		for i, rhs := range as.Rhs {
			if i >= len(as.Lhs) || !mentionsProjectFile(rhs) {
				continue
			}
			if id, ok := as.Lhs[i].(*ast.Ident); ok {
				tainted[id.Name] = true
			}
		}
		return true
	})

	refsProjectFile := false
	ast.Inspect(file, func(n ast.Node) bool {
		if refsProjectFile {
			return false
		}
		if e, ok := n.(ast.Expr); ok && isSelector(e, "project", "File") {
			refsProjectFile = true
		}
		return true
	})

	var problems []string
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		pkg, ok := sel.X.(*ast.Ident)
		if !ok {
			return true
		}
		pos := fset.Position(call.Pos())
		switch pkg.Name {
		case "os":
			idx, isWriter := fileWriters[sel.Sel.Name]
			if !isWriter || idx >= len(call.Args) {
				return true
			}
			arg := call.Args[idx]
			if id, ok := arg.(*ast.Ident); (ok && tainted[id.Name]) || mentionsProjectFile(arg) {
				problems = append(problems, fmt.Sprintf(
					"%s:%d: os.%s writes project.toml directly — load it with project.Load, mutate, and call Save; "+
						"any other writer round-trips the struct and drops comments and hand-added keys",
					filename, pos.Line, sel.Sel.Name))
			}
		case "toml":
			if !refsProjectFile {
				return true
			}
			switch sel.Sel.Name {
			case "NewEncoder":
				if len(call.Args) == 1 && isSelector(call.Args[0], "os", "Stdout") {
					return true // printing to the terminal is not a write
				}
				fallthrough
			case "Marshal":
				problems = append(problems, fmt.Sprintf(
					"%s:%d: toml.%s in a file that handles project.toml — the only encoder for that file is "+
						"project.Save; encode to os.Stdout to print, never to a file",
					filename, pos.Line, sel.Sel.Name))
			}
		}
		return true
	})

	sort.Strings(problems)
	return problems, nil
}

// mentionsProjectFile reports whether the expression names project.File or
// carries the literal "project.toml" anywhere inside it.
func mentionsProjectFile(e ast.Expr) bool {
	found := false
	ast.Inspect(e, func(n ast.Node) bool {
		if found {
			return false
		}
		switch x := n.(type) {
		case *ast.SelectorExpr:
			if isSelector(x, "project", "File") {
				found = true
			}
		case *ast.BasicLit:
			if x.Kind == token.STRING {
				if s, err := strconv.Unquote(x.Value); err == nil && strings.Contains(s, "project.toml") {
					found = true
				}
			}
		}
		return !found
	})
	return found
}

func isSelector(e ast.Expr, pkg, name string) bool {
	sel, ok := e.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != name {
		return false
	}
	id, ok := sel.X.(*ast.Ident)
	return ok && id.Name == pkg
}

// projectPackagePins is the inside-internal/project half: the encoder call
// sites, who calls encodeFresh, the file-writing call sites, and who calls
// writeAtomic — computed over source so the assertions can be spelled out.
type projectPackagePins struct {
	encoders          []string // "<Sel> in <func> (<file>:<line>)"
	encodeFreshCalls  map[string]bool
	fileWrites        []string // "os.<Sel> in <func> (<file>:<line>)"
	writeAtomicCalls  map[string]bool
	encodeFreshExists bool
}

func scanProjectPackage(files map[string]string) (projectPackagePins, error) {
	pins := projectPackagePins{encodeFreshCalls: map[string]bool{}, writeAtomicCalls: map[string]bool{}}
	fset := token.NewFileSet()
	for name, src := range files {
		file, err := parser.ParseFile(fset, name, src, 0)
		if err != nil {
			return pins, fmt.Errorf("parse %s: %w", name, err)
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok {
				continue
			}
			if fn.Name.Name == "encodeFresh" {
				pins.encodeFreshExists = true
			}
			ast.Inspect(fn, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				pos := fset.Position(call.Pos())
				if id, ok := call.Fun.(*ast.Ident); ok {
					switch id.Name {
					case "encodeFresh":
						pins.encodeFreshCalls[fn.Name.Name] = true
					case "writeAtomic":
						pins.writeAtomicCalls[fn.Name.Name] = true
					}
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				pkg, ok := sel.X.(*ast.Ident)
				if !ok {
					return true
				}
				switch {
				case pkg.Name == "toml" && (sel.Sel.Name == "NewEncoder" || sel.Sel.Name == "Marshal"):
					pins.encoders = append(pins.encoders, fmt.Sprintf("toml.%s in %s (%s:%d)", sel.Sel.Name, fn.Name.Name, filepath.Base(name), pos.Line))
				case pkg.Name == "os" && (sel.Sel.Name == "CreateTemp" || isFileWriter(sel.Sel.Name)):
					pins.fileWrites = append(pins.fileWrites, fmt.Sprintf("os.%s in %s (%s:%d)", sel.Sel.Name, fn.Name.Name, filepath.Base(name), pos.Line))
				}
				return true
			})
		}
	}
	sort.Strings(pins.encoders)
	sort.Strings(pins.fileWrites)
	return pins, nil
}

func isFileWriter(name string) bool {
	_, ok := fileWriters[name]
	return ok
}

// projectPackageViolations spells the inside-internal/project rule over a
// scan: one encoder in encodeFresh, its callers from the allowlist, file
// writes only in writeAtomic (plus Save's read-only probe), writeAtomic
// called only by Save.
func projectPackageViolations(pins projectPackagePins) []string {
	var problems []string
	if !pins.encodeFreshExists {
		problems = append(problems, "internal/project has no encodeFresh — the single encoder helper is gone")
	}
	if len(pins.encoders) != 1 || !strings.HasPrefix(pins.encoders[0], "toml.NewEncoder in encodeFresh") {
		problems = append(problems, fmt.Sprintf("internal/project encoder call sites = %v; want exactly [toml.NewEncoder in encodeFresh] — "+
			"a second encoder is a re-encode path the patcher never verifies", pins.encoders))
	}
	for caller := range pins.encodeFreshCalls {
		if !encodeFreshCallers[caller] {
			problems = append(problems, fmt.Sprintf("internal/project: %s calls encodeFresh — not in the allowlist (%v)", caller, sortedKeys(encodeFreshCallers)))
		}
	}
	for _, w := range pins.fileWrites {
		if !strings.Contains(w, " in writeAtomic ") && !strings.HasPrefix(w, "os.OpenFile in Save ") {
			problems = append(problems, fmt.Sprintf("internal/project: %s — the file is written by writeAtomic alone", w))
		}
	}
	for caller := range pins.writeAtomicCalls {
		if caller != "Save" {
			problems = append(problems, fmt.Sprintf("internal/project: %s calls writeAtomic — only Save may", caller))
		}
	}
	sort.Strings(problems)
	return problems
}

func writerViolations(t *testing.T, src string) []string {
	t.Helper()
	got, err := projectWriterViolations("synthetic.go", src)
	if err != nil {
		t.Fatalf("detector: %v", err)
	}
	return got
}

// --- synthetic proofs of the detector ----------------------------------------

func TestProjectTomlHasOneWriter(t *testing.T) {
	got := writerViolations(t, `package cmd
func f(root string, b []byte) error {
	return os.WriteFile(filepath.Join(root, project.File), b, 0o644)
}
`)
	if len(got) != 1 || !strings.Contains(got[0], "synthetic.go:3") || !strings.Contains(got[0], "os.WriteFile") {
		t.Errorf("violations = %q, want one at synthetic.go:3 naming os.WriteFile", got)
	}
}

func TestWriterPinCatchesIndirectPath(t *testing.T) {
	for name, src := range map[string]string{
		"two-step": `package cmd
func f(root string, b []byte) error {
	p := filepath.Join(root, project.File)
	return os.WriteFile(p, b, 0o644)
}
`,
		"literal": `package cmd
func f(root string) (*os.File, error) {
	return os.Create(filepath.Join(root, ".dross", "project.toml"))
}
`,
		"rename onto": `package cmd
func f(root, tmp string) error {
	dst := filepath.Join(root, project.File)
	return os.Rename(tmp, dst)
}
`,
		"openfile": `package cmd
func f(path string) (*os.File, error) {
	return os.OpenFile(path+"/project.toml", os.O_WRONLY|os.O_TRUNC, 0o644)
}
`,
	} {
		t.Run(name, func(t *testing.T) {
			if got := writerViolations(t, src); len(got) != 1 {
				t.Errorf("violations = %q, want exactly one", got)
			}
		})
	}
}

func TestDetectsStrayProjectEncoder(t *testing.T) {
	got := writerViolations(t, `package cmd
func f(root string, p *project.Project) error {
	f, err := os.Create(filepath.Join(root, "other.toml"))
	if err != nil {
		return err
	}
	_ = project.File
	return toml.NewEncoder(f).Encode(p)
}
`)
	if len(got) != 1 || !strings.Contains(got[0], "synthetic.go:8") || !strings.Contains(got[0], "toml.NewEncoder") {
		t.Errorf("violations = %q, want one at synthetic.go:8 naming toml.NewEncoder", got)
	}
	got = writerViolations(t, `package cmd
func f(p *project.Project) ([]byte, error) {
	_ = project.File
	return toml.Marshal(p)
}
`)
	if len(got) != 1 || !strings.Contains(got[0], "toml.Marshal") {
		t.Errorf("violations = %q, want one naming toml.Marshal", got)
	}
}

func TestWriterPinAllowsStdoutEncode(t *testing.T) {
	got := writerViolations(t, `package cmd
func show(p *project.Project) error {
	return toml.NewEncoder(os.Stdout).Encode(p)
}
func write(p *project.Project, root string) error {
	return p.Save(filepath.Join(root, project.File))
}
func unrelated(root string, b []byte) error {
	return os.WriteFile(filepath.Join(root, "rules.toml"), b, 0o644)
}
`)
	if len(got) != 0 {
		t.Errorf("false positives: %q", got)
	}
}

func TestEncodeFreshIsTheOnlyEncoder(t *testing.T) {
	// The real package: one encoder, in encodeFresh, with the pinned callers.
	root := repoRootFromTest(t)
	files := projectPackageSources(t, root)
	pins, err := scanProjectPackage(files)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range projectPackageViolations(pins) {
		t.Error(p)
	}

	// Teeth: a toml.Marshal re-encode fallback added to the package is
	// counted as a second encoder site, as is a second NewEncoder.
	files["fallback.go"] = `package project
func fallback(p *Project) ([]byte, error) { return toml.Marshal(p) }
`
	pins, err = scanProjectPackage(files)
	if err != nil {
		t.Fatal(err)
	}
	if got := projectPackageViolations(pins); len(got) == 0 || !strings.Contains(strings.Join(got, "\n"), "toml.Marshal in fallback") {
		t.Errorf("a toml.Marshal fallback was not reported: %q", got)
	}
	delete(files, "fallback.go")

	// And a new caller of encodeFresh, or a new writer of the file, is
	// refused by name.
	files["stray.go"] = `package project
func stray(p *Project, path string) error {
	b, _ := encodeFresh(p)
	return os.WriteFile(path, b, 0o644)
}
`
	pins, err = scanProjectPackage(files)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(projectPackageViolations(pins), "\n")
	if !strings.Contains(joined, "stray calls encodeFresh") || !strings.Contains(joined, "os.WriteFile in stray") {
		t.Errorf("a stray encodeFresh caller writing the file was not reported:\n%s", joined)
	}
}

// --- the real scan -------------------------------------------------------------

func projectPackageSources(t *testing.T, root string) map[string]string {
	t.Helper()
	dir := filepath.Join(root, "internal", "project")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		files[e.Name()] = string(b)
	}
	if len(files) == 0 {
		t.Fatal("no sources found under internal/project")
	}
	return files
}

// TestNoWriterBypassesProjectSave is the assertion with teeth today: every
// non-test .go under internal/ outside internal/project is scanned, and any
// route to project.toml other than Save fails by file:line.
func TestNoWriterBypassesProjectSave(t *testing.T) {
	root := repoRootFromTest(t)
	var scanned int
	var problems []string
	err := filepath.WalkDir(filepath.Join(root, "internal"), func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == "project" && filepath.Dir(path) == filepath.Join(root, "internal") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".go") || strings.HasSuffix(d.Name(), "_test.go") {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		found, err := projectWriterViolations(rel, string(src))
		if err != nil {
			return err
		}
		scanned++
		problems = append(problems, found...)
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if scanned < 100 {
		t.Fatalf("scanned only %d files — the walk is not covering internal/", scanned)
	}
	for _, p := range problems {
		t.Error(p)
	}
}
