package cmd

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"

	"github.com/Rivil/dross/internal/pathfence"
)

// The scanned set. Every source scan — exec taint, path-field taint, the
// pathfence bans, exec-consent — walks the packages the shared load matched:
// the whole module, derived from go list, with no list of roots to edit. A
// package added under internal/ or cmd/ tomorrow is in scope the day it
// compiles.
//
// Deriving the set moves the risk from "a list someone forgot to extend" to "a
// file the load never saw". The load sees what the go command builds on this
// platform, so three kinds of file fall outside it: a file excluded by build
// constraints (another GOOS, //go:build ignore), a file in a nested module, and
// a file in a directory the module does not match. None of those is ever
// type-checked, so a scan cannot see a spawn inside one. The cross-check below
// sweeps the tree with go/parser — which ignores build constraints — and fails
// on any such file that spawns a process or reads a declared path field.
//
// The sweep skips exactly what the go command itself can never build:
// testdata, and directories whose name starts with "." or "_". Nested modules
// are NOT skipped: that code is buildable, just not by this module's load.

// srcScopeFloor is ~25% under today's 46 packages. A load that silently lost a
// quarter of the module — a pattern narrowed from ./... — falls under it.
const srcScopeFloor = 34

// srcSpawnFileFloor is ~25% under the files that construct a command.
//
// Reset in cmd-exec-baseline-drain t-13 to floor(0.75 x the logged census):
// 20 -> 11, logged at 15 files (from 28 when the floor was set). git's spawns
// collapsed into internal/gitrun and cmd constructs no command of its own, so
// the drop is the consolidation, not a blinder sweep.
const srcSpawnFileFloor = 11

// srcScope is the package set the source scans walk: every package the shared
// load matched.
func srcScope(t testing.TB) []*packages.Package {
	t.Helper()
	return sourceProgram(t).Pkgs
}

// goNeverBuilds reports whether the go command ignores a directory by name:
// testdata, and names starting with "." or "_". It matches whole path
// segments, so "testdatabase" is an ordinary directory.
func goNeverBuilds(name string) bool {
	return name == "testdata" || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_")
}

// inNeverBuiltDir reports whether any directory segment of a slash-separated
// relative path is one goNeverBuilds names.
func inNeverBuiltDir(rel string) bool {
	segs := strings.Split(rel, "/")
	for _, s := range segs[:len(segs)-1] {
		if goNeverBuilds(s) {
			return true
		}
	}
	return false
}

// loadedDirs is the set of repo-relative directories the load matched.
func loadedDirs(root string, pkgs []*packages.Package) (map[string]bool, error) {
	out := map[string]bool{}
	for _, p := range pkgs {
		if len(p.GoFiles) == 0 {
			return nil, fmt.Errorf("%s has no Go files", p.PkgPath)
		}
		rel, err := filepath.Rel(root, filepath.Dir(p.GoFiles[0]))
		if err != nil {
			return nil, err
		}
		out[filepath.ToSlash(rel)] = true
	}
	return out, nil
}

// walkedGoDirs is the set of repo-relative directories in root's module that
// hold a non-test .go file — the independent count the load is compared with.
// Nested modules belong to another module and are left out here; the
// cross-check sweep covers their files.
func walkedGoDirs(root string) (map[string]bool, error) {
	out := map[string]bool{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != root && goNeverBuilds(d.Name()) {
				return filepath.SkipDir
			}
			if path != root {
				if _, err := os.Stat(filepath.Join(path, "go.mod")); err == nil {
					return filepath.SkipDir
				}
			}
			return nil
		}
		if strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go") {
			rel, err := filepath.Rel(root, filepath.Dir(path))
			if err != nil {
				return err
			}
			out[filepath.ToSlash(rel)] = true
		}
		return nil
	})
	return out, err
}

// scopeSweep is what the go/parser cross-check found.
type scopeSweep struct {
	Swept      []string // every non-test .go file parsed, repo-relative
	SpawnFiles []string // files constructing an exec.Cmd
	Outside    []string // "<file>: spawn" / "<file>: field read" for files the load never saw
}

// sweepOutsideLoad parses every non-test .go file under root, build
// constraints ignored, and reports each that constructs an exec.Cmd or selects
// a declared path field while being absent from loaded (absolute file paths).
//
// A field read is matched by selector NAME: without types a parse cannot tell
// changes.TaskRecord.Files from any other .Files. The over-approximation costs
// nothing — it only ever reports files the typed scans cannot see at all.
func sweepOutsideLoad(root string, loaded map[string]bool, fieldNames map[string]bool) (scopeSweep, error) {
	var sw scopeSweep
	fset := token.NewFileSet()
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != root && goNeverBuilds(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		f, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			return fmt.Errorf("sweep: %w", err)
		}
		sw.Swept = append(sw.Swept, rel)
		spawn, fieldRead := fileSpawnsOrReadsFields(f, fieldNames)
		if spawn {
			sw.SpawnFiles = append(sw.SpawnFiles, rel)
		}
		if loaded[path] {
			return nil
		}
		if spawn {
			sw.Outside = append(sw.Outside, rel+": spawn")
		}
		if fieldRead {
			sw.Outside = append(sw.Outside, rel+": field read")
		}
		return nil
	})
	sort.Strings(sw.Swept)
	sort.Strings(sw.SpawnFiles)
	sort.Strings(sw.Outside)
	return sw, err
}

// fileSpawnsOrReadsFields reports whether f references exec.Command or
// exec.CommandContext (under whatever name the file imports os/exec as), and
// whether it selects any of fieldNames.
func fileSpawnsOrReadsFields(f *ast.File, fieldNames map[string]bool) (spawn, fieldRead bool) {
	execName := ""
	for _, imp := range f.Imports {
		if p, _ := strconv.Unquote(imp.Path.Value); p == "os/exec" {
			execName = "exec"
			if imp.Name != nil {
				execName = imp.Name.Name
			}
		}
	}
	ast.Inspect(f, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if x, ok := sel.X.(*ast.Ident); ok && execName != "" && x.Name == execName &&
			(sel.Sel.Name == "Command" || sel.Sel.Name == "CommandContext") {
			spawn = true
		}
		if fieldNames[sel.Sel.Name] {
			fieldRead = true
		}
		return true
	})
	return spawn, fieldRead
}

// declaredFieldNames is the set of Go field names the pathfence registry
// declares.
func declaredFieldNames() map[string]bool {
	out := map[string]bool{}
	for _, f := range pathfence.Fields() {
		out[f.Field] = true
	}
	return out
}

// loadedFiles is every file the load type-checked, by absolute path.
func loadedFiles(pkgs []*packages.Package) map[string]bool {
	out := map[string]bool{}
	for _, p := range pkgs {
		for _, f := range p.CompiledGoFiles {
			out[f] = true
		}
	}
	return out
}

// TestScannedSetMatchesTheModule: the loaded directories and an independent
// WalkDir agree exactly, and the packages every scan most needs are in it.
func TestScannedSetMatchesTheModule(t *testing.T) {
	p := sourceProgram(t)
	loaded, err := loadedDirs(p.Root, srcScope(t))
	if err != nil {
		t.Fatal(err)
	}
	walked, err := walkedGoDirs(p.Root)
	if err != nil {
		t.Fatal(err)
	}
	for d := range walked {
		if !loaded[d] {
			t.Errorf("%s holds non-test Go files but the load did not match it", d)
		}
	}
	for d := range loaded {
		if !walked[d] {
			t.Errorf("the load matched %s, which the walk does not see as a Go directory", d)
		}
	}
	for _, d := range []string{"internal/cmd", "internal/ship", "internal/mutation", "cmd/dross", "cmd/testsummary", "assets"} {
		if !loaded[d] {
			t.Errorf("%s is not in the scanned set", d)
		}
	}
}

// TestScannedSetFloor: a narrowed load, or one that reached into testdata,
// fails here.
func TestScannedSetFloor(t *testing.T) {
	pkgs := srcScope(t)
	if err := srcScopeFloorErr(len(pkgs)); err != nil {
		t.Error(err)
	}
	for _, p := range pkgs {
		for _, f := range p.GoFiles {
			if strings.Contains(filepath.ToSlash(f), "/testdata/") {
				t.Errorf("%s: %s is under testdata", p.PkgPath, f)
			}
		}
	}
	if err := srcScopeFloorErr(srcScopeFloor); err != nil {
		t.Errorf("the floor fails at its own minimum: %v", err)
	}
	if srcScopeFloorErr(srcScopeFloor-1) == nil {
		t.Error("the floor passes one package under its minimum")
	}
}

func srcScopeFloorErr(n int) error {
	if n < srcScopeFloor {
		return fmt.Errorf("the scanned set holds %d packages, under the floor of %d — the load narrowed", n, srcScopeFloor)
	}
	return nil
}

// TestNoSpawnOrFieldReadOutsideTheLoad is the live cross-check: every file
// that spawns or reads a declared path field is one the typed scans see.
func TestNoSpawnOrFieldReadOutsideTheLoad(t *testing.T) {
	p := sourceProgram(t)
	sw, err := sweepOutsideLoad(p.Root, loadedFiles(srcScope(t)), declaredFieldNames())
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range sw.Outside {
		t.Errorf("%s — outside the typed load, so no source scan can see it", o)
	}
	if len(sw.SpawnFiles) < srcSpawnFileFloor {
		t.Errorf("the sweep found %d spawning files, under the floor of %d — it has gone blind", len(sw.SpawnFiles), srcSpawnFileFloor)
	}
	swept := map[string]bool{}
	for _, f := range sw.Swept {
		swept[f] = true
	}
	for _, f := range []string{"internal/mutation/freespace_unix.go", "internal/mutation/freespace_other.go"} {
		if !swept[f] {
			t.Errorf("%s was not swept — the sweep is honouring build constraints", f)
		}
	}
}

// TestCrossCheckReportsEachWayOutOfTheLoad builds a tree holding each kind of
// file the load cannot see, and requires each reported by name and kind — then
// none reported once every file is in the loaded set.
func TestCrossCheckReportsEachWayOutOfTheLoad(t *testing.T) {
	root := t.TempDir()
	spawn := "package p\n\nimport \"os/exec\"\n\nvar _ = exec.Command(\"true\")\n"
	files := map[string]string{
		"go.mod":            "module example.com/scopetest\n\ngo 1.22\n",
		"in/a.go":           spawn,
		"extra/spawn.go":    spawn,
		"in/ignored.go":     "//go:build ignore\n\n" + spawn,
		"nested/go.mod":     "module example.com/nested\n\ngo 1.22\n",
		"nested/n.go":       spawn,
		"in/tagged.go":      "//go:build special\n\npackage p\n\nfunc f(rec struct{ Files []string }) []string { return rec.Files }\n",
		"testdata/t.go":     spawn,
		"in/_skip/s.go":     spawn,
		"in/a_test.go":      spawn,
		"in/plain.go":       "package p\n\nfunc g() {}\n",
		"testdatabase/d.go": spawn,
	}
	for rel, body := range files {
		mustWrite(t, filepath.Join(root, filepath.FromSlash(rel)), body)
	}
	loaded := map[string]bool{
		filepath.Join(root, "in", "a.go"):     true,
		filepath.Join(root, "in", "plain.go"): true,
	}
	fields := map[string]bool{"Files": true}

	sw, err := sweepOutsideLoad(root, loaded, fields)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"extra/spawn.go: spawn",
		"in/ignored.go: spawn",
		"in/tagged.go: field read",
		"nested/n.go: spawn",
		"testdatabase/d.go: spawn",
	}
	if strings.Join(sw.Outside, "\n") != strings.Join(want, "\n") {
		t.Errorf("outside-load report:\n%s\nwant:\n%s", strings.Join(sw.Outside, "\n"), strings.Join(want, "\n"))
	}

	for _, rel := range []string{"extra/spawn.go", "in/ignored.go", "nested/n.go", "in/tagged.go", "testdatabase/d.go"} {
		loaded[filepath.Join(root, filepath.FromSlash(rel))] = true
	}
	sw, err = sweepOutsideLoad(root, loaded, fields)
	if err != nil {
		t.Fatal(err)
	}
	if len(sw.Outside) != 0 {
		t.Errorf("with every file loaded the sweep still reported %v", sw.Outside)
	}
}

// TestTestdataExclusionIsBySegment: testdata is excluded as a whole path
// segment, never as a substring.
func TestTestdataExclusionIsBySegment(t *testing.T) {
	for rel, want := range map[string]bool{
		"internal/mutation/testdata/ceiling/x.go": true,
		"testdata/x.go":              true,
		"internal/testdatabase/x.go": false,
		"internal/mytestdata/x.go":   false,
		"internal/.hidden/x.go":      true,
		"internal/_scratch/x.go":     true,
		"internal/cmd/x.go":          false,
	} {
		if got := inNeverBuiltDir(rel); got != want {
			t.Errorf("inNeverBuiltDir(%q) = %v, want %v", rel, got, want)
		}
	}
	walked, err := walkedGoDirs(sourceProgram(t).Root)
	if err != nil {
		t.Fatal(err)
	}
	for d := range walked {
		if inNeverBuiltDir(d + "/x.go") {
			t.Errorf("walkedGoDirs entered %s", d)
		}
	}
}
