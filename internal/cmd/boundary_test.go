package cmd

import (
	"bufio"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// boundary_test.go proves the cmd-package-decomposition boundary by IMPORT
// DIRECTION, over the live tree (the locked proof_shape decision):
//
//  1. github.com/spf13/cobra is imported by internal/cmd and nowhere else —
//     the extracted packages are libraries, not command trees.
//  2. No package under internal/ imports internal/cmd — the direction is
//     one-way, so nothing extracted can quietly reach back for a helper.
//  3. internal/cmd imports each of the four extracted packages — the
//     decomposition is wired in, not sitting beside the old code.
//  4. The forbidden-import BAN: no non-test internal/cmd file imports os/exec,
//     net/http, go/ast, encoding/json or BurntSushi/toml. It began as a
//     shrink-only ratchet with a baseline of the files still importing each;
//     cmd-exec-baseline-drain drained every entry and deleted the baseline, so
//     there is no list to add a file to — spawning, HTTP, source parsing and
//     codecs live in their domain packages, and a cmd file reaching for one is
//     a red test with no escape hatch.
//  5. internal/gitrun is a LEAF: it imports only the standard library, so
//     consent and remote — which cmd imports — can spawn git through it
//     without an import cycle.
//
// The checker is a pure function over {package -> imports} so the same rules
// run over the live tree AND over synthetic maps that prove each rule fires.
// A vacuity floor mirrors execConsentFloor: a walk that saw too few packages,
// or missed internal/cmd, is an error rather than a pass.

// forbiddenInCmd are the imports banned from non-test internal/cmd files. The
// two codecs joined in cmd-exec-baseline-drain (c-8): decode/persist logic
// belongs in its domain package and CLI --json/TOML output in one rendering
// package, so a cmd file reaching for either is domain logic landing in the
// wrong place.
var forbiddenInCmd = []string{"os/exec", "net/http", "go/ast", "encoding/json", "github.com/BurntSushi/toml"}

// extractedPackages are the packages logic was pulled out of cmd into, which
// cmd must import — the four cmd-package-decomposition extracted, then
// cmd-exec-baseline-drain's local.toml store and output rendering. An import
// that vanished means the extraction was satisfied by deleting the feature,
// not by moving it.
var extractedPackages = []string{
	modulePath + "/internal/consent",
	modulePath + "/internal/boardsync",
	modulePath + "/internal/diag",
	modulePath + "/internal/mutationcfg",
	modulePath + "/internal/localstore",
	modulePath + "/internal/render",
}

const (
	modulePath  = "github.com/Rivil/dross"
	cmdPkgPath  = modulePath + "/internal/cmd"
	cobraPath   = "github.com/spf13/cobra"
	gitrunPath  = modulePath + "/internal/gitrun"
	boundaryMin = 30 // packages the walk must see before its verdict counts
)

// pkgImports is one package's non-test import surface, per file, so the
// ratchet can name the file that carries a forbidden import.
type pkgImports struct {
	Path  string              // import path, e.g. github.com/Rivil/dross/internal/cmd
	Files map[string][]string // file basename -> imports
}

// imports flattens the per-file set.
func (p pkgImports) imports() map[string]bool {
	out := map[string]bool{}
	for _, imps := range p.Files {
		for _, i := range imps {
			out[i] = true
		}
	}
	return out
}

// walkImports parses (ImportsOnly) every non-test .go file under internalDir,
// skipping testdata, and returns {package path -> imports}.
func walkImports(internalDir string) (map[string]pkgImports, error) {
	pkgs := map[string]pkgImports{}
	fset := token.NewFileSet()
	err := filepath.WalkDir(internalDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		f, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			return fmt.Errorf("parse %s: %w", path, err)
		}
		rel, err := filepath.Rel(internalDir, filepath.Dir(path))
		if err != nil {
			return err
		}
		pkgPath := modulePath + "/internal/" + filepath.ToSlash(rel)
		p, ok := pkgs[pkgPath]
		if !ok {
			p = pkgImports{Path: pkgPath, Files: map[string][]string{}}
		}
		var imps []string
		for _, spec := range f.Imports {
			v, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				return fmt.Errorf("%s: bad import %s", path, spec.Path.Value)
			}
			imps = append(imps, v)
		}
		p.Files[filepath.Base(path)] = imps
		pkgs[pkgPath] = p
		return nil
	})
	return pkgs, err
}

// checkBoundary applies the five rules plus the vacuity floor and returns
// every finding, one line each, sorted. An empty result is a pass. It takes the
// package map and nothing else — there is no allowlist to hand it
// (TestFlatBanHasNoAllowlist).
func checkBoundary(pkgs map[string]pkgImports) []string {
	var findings []string
	if len(pkgs) < boundaryMin {
		findings = append(findings, fmt.Sprintf("vacuity: the walk saw %d packages under internal/, want at least %d — is it pointed at the tree?", len(pkgs), boundaryMin))
	}
	cmd, hasCmd := pkgs[cmdPkgPath]
	if !hasCmd {
		findings = append(findings, "vacuity: the walk did not see internal/cmd, so no rule below can fire")
		sort.Strings(findings)
		return findings
	}

	// 1 + 2: cobra exclusivity and no reverse import, over every package.
	for _, p := range pkgs {
		imps := p.imports()
		if p.Path != cmdPkgPath && imps[cobraPath] {
			findings = append(findings, fmt.Sprintf("cobra exclusivity: %s imports %s — only internal/cmd builds command trees", p.Path, cobraPath))
		}
		if imps[cmdPkgPath] {
			findings = append(findings, fmt.Sprintf("reverse import: %s imports %s — the boundary is one-way", p.Path, cmdPkgPath))
		}
	}

	// 5: the git runner is a leaf. Absent, the rule has nothing to judge, which
	// is its own finding rather than a pass.
	if gr, ok := pkgs[gitrunPath]; !ok {
		findings = append(findings, "vacuity: the walk did not see internal/gitrun, so the leaf rule cannot fire")
	} else {
		for imp := range gr.imports() {
			if first, _, _ := strings.Cut(imp, "/"); strings.Contains(first, ".") {
				findings = append(findings, fmt.Sprintf("leaf: internal/gitrun imports %s — the runner imports only the standard library, so every package can spawn git through it", imp))
			}
		}
	}

	// 3: cmd wires in each extracted package.
	cmdImps := cmd.imports()
	for _, want := range extractedPackages {
		if !cmdImps[want] {
			findings = append(findings, fmt.Sprintf("wiring: internal/cmd does not import %s — the extraction is not consumed", want))
		}
	}

	// 4: the flat ban. Test files may import anything — they are not the
	// command tree — and walkImports never hands them over, but a map built
	// by hand might.
	banned := map[string]bool{}
	for _, f := range forbiddenInCmd {
		banned[f] = true
	}
	for file, imps := range cmd.Files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		for _, i := range imps {
			if banned[i] {
				findings = append(findings, fmt.Sprintf("ban: internal/cmd/%s imports %s — the command tree maps flags onto domain packages; move the logic there", file, i))
			}
		}
	}
	sort.Strings(findings)
	return findings
}

// TestCmdBoundaryByImportDirection is the live-tree run: green exactly when no
// cmd file imports a banned package and every direction rule holds.
func TestCmdBoundaryByImportDirection(t *testing.T) {
	pkgs, err := walkImports(filepath.Join(repoRootFromTest(t), "internal"))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range checkBoundary(pkgs) {
		t.Error(f)
	}
	t.Logf("boundary walk saw %d packages", len(pkgs))
}

// TestCmdForbiddenImportRatchet is the ban's table-driven self-test, kept
// under the ratchet's name. Exhaustively, on a clone of the live tree: every
// banned import injected into every non-test internal/cmd file yields exactly
// one finding naming both. The same import in a synthetic cmd x_test.go, or in
// a package outside cmd, yields none.
func TestCmdForbiddenImportRatchet(t *testing.T) {
	live, err := walkImports(filepath.Join(repoRootFromTest(t), "internal"))
	if err != nil {
		t.Fatal(err)
	}
	if got := checkBoundary(live); len(got) != 0 {
		t.Fatalf("precondition: the live tree is not clean:\n%s", strings.Join(got, "\n"))
	}
	pkgs := clonePkgs(live)
	cmd := pkgs[cmdPkgPath]
	var files []string
	for f := range cmd.Files {
		files = append(files, f)
	}
	sort.Strings(files)
	if len(files) < 100 {
		t.Fatalf("the clone holds %d internal/cmd files — not the live tree", len(files))
	}
	for _, forbidden := range forbiddenInCmd {
		for _, file := range files {
			orig := cmd.Files[file]
			cmd.Files[file] = append(append([]string(nil), orig...), forbidden)
			got := checkBoundary(pkgs)
			cmd.Files[file] = orig
			if len(got) != 1 || !strings.Contains(got[0], "internal/cmd/"+file+" imports "+forbidden) {
				t.Errorf("%s importing %s: findings = %v, want exactly one naming both", file, forbidden, got)
			}
		}
		cmd.Files["x_test.go"] = []string{forbidden}
		if got := checkBoundary(pkgs); len(got) != 0 {
			t.Errorf("a cmd test file importing %s was banned: %v", forbidden, got)
		}
		delete(cmd.Files, "x_test.go")
		pkgs[modulePath+"/internal/foo"] = pkgImports{Path: modulePath + "/internal/foo", Files: map[string][]string{"foo.go": {forbidden}}}
		if got := checkBoundary(pkgs); len(got) != 0 {
			t.Errorf("a package outside cmd importing %s was banned: %v", forbidden, got)
		}
		delete(pkgs, modulePath+"/internal/foo")
	}
}

// TestFlatBanHasNoAllowlist: the ban has no allowlist to creep back into —
// checkBoundary takes the package map alone, and no package-level var in this
// file is named for a baseline or an allowance.
func TestFlatBanHasNoAllowlist(t *testing.T) {
	if n := reflect.TypeOf(checkBoundary).NumIn(); n != 1 {
		t.Errorf("checkBoundary takes %d parameters, want only the package map — an allowlist parameter is back", n)
	}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, filepath.Join(repoRootFromTest(t), "internal", "cmd", "boundary_test.go"), nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	allowish := regexp.MustCompile(`(?i)baseline|allow`)
	for _, d := range f.Decls {
		gen, ok := d.(*ast.GenDecl)
		if !ok || gen.Tok != token.VAR {
			continue
		}
		for _, spec := range gen.Specs {
			for _, n := range spec.(*ast.ValueSpec).Names {
				if allowish.MatchString(n.Name) {
					t.Errorf("boundary_test.go declares var %s — the ban has no allowlist", n.Name)
				}
			}
		}
	}
}

// gitSpawnNames are the functions that used to spawn git outside the runner.
var gitSpawnNames = map[string]bool{
	"gitTrim": true, "gitRead": true, "gitRun": true, "gitNoOut": true, "gitBranchTrim": true,
	"gitStatusRaw": true, "gitRemoteOriginURL": true, "ShortSHA": true,
}

// gitSpawnFindings scans the non-test .go files under roots (testdata
// skipped): every exec.Command/CommandContext with a literal "git" binary, and
// every function named after a retired git helper, outside internal/gitrun is a
// finding. It returns the findings and how many git spawns it saw inside.
func gitSpawnFindings(repo string, roots ...string) ([]string, int, error) {
	fset := token.NewFileSet()
	var out []string
	inside := 0
	for _, r := range roots {
		err := filepath.WalkDir(filepath.Join(repo, r), func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if d.Name() == "testdata" {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			rel, _ := filepath.Rel(repo, path)
			rel = filepath.ToSlash(rel)
			inRunner := strings.HasPrefix(rel, "internal/gitrun/")
			f, err := parser.ParseFile(fset, path, nil, 0)
			if err != nil {
				return err
			}
			for _, decl := range f.Decls {
				if fd, ok := decl.(*ast.FuncDecl); ok && fd.Recv == nil && gitSpawnNames[fd.Name.Name] && !inRunner {
					out = append(out, fmt.Sprintf("%s: func %s — git goes through internal/gitrun, not a package-local helper", fset.Position(fd.Pos()), fd.Name.Name))
				}
			}
			ast.Inspect(f, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok || (sel.Sel.Name != "Command" && sel.Sel.Name != "CommandContext") {
					return true
				}
				if pkg, ok := sel.X.(*ast.Ident); !ok || pkg.Name != "exec" {
					return true
				}
				bin := 0
				if sel.Sel.Name == "CommandContext" {
					bin = 1
				}
				if len(call.Args) <= bin {
					return true
				}
				if lit, ok := stringLit(call.Args[bin]); !ok || lit != "git" {
					return true
				}
				if inRunner {
					inside++
				} else {
					out = append(out, fmt.Sprintf("%s: spawns git directly — go through internal/gitrun", fset.Position(call.Pos())))
				}
				return true
			})
			return nil
		})
		if err != nil {
			return nil, 0, err
		}
	}
	sort.Strings(out)
	return out, inside, nil
}

// TestEveryGitSpawnIsInTheRunner is c-3's census: across internal/ and cmd/,
// the only git spawns are internal/gitrun's, and there is at least one of them
// — a census that saw none would pass by seeing nothing. Test files are not
// scanned: fixtures and the setup shims spawn git freely.
func TestEveryGitSpawnIsInTheRunner(t *testing.T) {
	repo := repoRootFromTest(t)
	found, inside, err := gitSpawnFindings(repo, "internal", "cmd")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range found {
		t.Error(f)
	}
	if inside == 0 {
		t.Fatal("found no git spawn inside internal/gitrun — the census is blind")
	}

	dir := t.TempDir()
	write := func(rel, body string) {
		mustWrite(t, filepath.Join(dir, filepath.FromSlash(rel)), body)
	}
	write("internal/gitrun/gitrun.go", "package gitrun\n\nimport \"os/exec\"\n\nfunc run() error { return exec.Command(\"git\", \"status\").Run() }\n")
	write("internal/codex/git.go", "package codex\n\nimport \"os/exec\"\n\nfunc log() error { return exec.Command(\"git\", \"status\").Run() }\n")
	write("internal/quality/run.go", "package quality\n\nfunc ShortSHA(dir string) string { return \"\" }\n")
	write("internal/cmd/x_test.go", "package cmd\n\nimport \"os/exec\"\n\nfunc gitTrim() error { return exec.Command(\"git\", \"status\").Run() }\n")
	write("internal/cmd/testdata/fixture.go", "package fixture\n\nimport \"os/exec\"\n\nfunc f() error { return exec.Command(\"git\", \"log\").Run() }\n")
	got, in, err := gitSpawnFindings(dir, "internal")
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(got, "\n")
	if len(got) != 2 || !strings.Contains(joined, "codex/git.go") || !strings.Contains(joined, "func ShortSHA") {
		t.Errorf("synthetic census = %v, want exactly the codex spawn and quality's ShortSHA", got)
	}
	if in != 1 {
		t.Errorf("synthetic census saw %d spawns inside gitrun, want 1", in)
	}
}

// tomlStoreTypes names every struct type in files that carries a toml struct
// tag — the shape of a TOML document's decode target. internal/cmd must declare
// none: local.toml's store lives in internal/localstore and every other TOML
// document in its domain package (locked local_store_proof), so a toml-tagged
// struct in cmd is a store being rebuilt beside the command tree. An anonymous
// struct counts too, named by its line.
func tomlStoreTypes(fset *token.FileSet, files []*ast.File) []string {
	hasTomlTag := func(st *ast.StructType) bool {
		for _, fld := range st.Fields.List {
			if fld.Tag == nil {
				continue
			}
			tag, err := strconv.Unquote(fld.Tag.Value)
			if err != nil {
				continue
			}
			if _, ok := reflect.StructTag(tag).Lookup("toml"); ok {
				return true
			}
		}
		return false
	}
	var out []string
	for _, f := range files {
		file := filepath.Base(fset.Position(f.Pos()).Filename)
		named := map[*ast.StructType]bool{}
		ast.Inspect(f, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.TypeSpec:
				if st, ok := n.Type.(*ast.StructType); ok {
					named[st] = true
					if hasTomlTag(st) {
						out = append(out, fmt.Sprintf("store type: internal/cmd/%s declares %s with toml tags — decode it in its domain package, not beside the command tree", file, n.Name.Name))
					}
				}
			case *ast.StructType:
				if !named[n] && hasTomlTag(n) {
					out = append(out, fmt.Sprintf("store type: internal/cmd/%s declares an anonymous struct with toml tags at line %d — decode it in its domain package, not beside the command tree", file, fset.Position(n.Pos()).Line))
				}
			}
			return true
		})
	}
	sort.Strings(out)
	return out
}

// parseNonTestGo parses every non-test .go file directly in dir.
func parseNonTestGo(t *testing.T, dir string) (*token.FileSet, []*ast.File) {
	t.Helper()
	fset := token.NewFileSet()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var files []*ast.File
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, f)
	}
	return fset, files
}

// TestCmdDeclaresNoTomlStore is the positive half of c-5's proof: cmd imports
// internal/localstore (extractedPackages) and declares no TOML decode target
// of its own. A synthetic toml-tagged type is named by file and type; a
// json-only struct is not a store and passes.
func TestCmdDeclaresNoTomlStore(t *testing.T) {
	fset, files := parseNonTestGo(t, filepath.Join(repoRootFromTest(t), "internal", "cmd"))
	if len(files) < 100 {
		t.Fatalf("parsed %d internal/cmd files — the walk is not pointed at the package", len(files))
	}
	for _, f := range tomlStoreTypes(fset, files) {
		t.Error(f)
	}

	synth := token.NewFileSet()
	parse := func(name, src string) *ast.File {
		f, err := parser.ParseFile(synth, name, src, 0)
		if err != nil {
			t.Fatal(err)
		}
		return f
	}
	toml := parse("issue.go", "package cmd\n\ntype x struct{ A string `toml:\"a\"` }\n")
	jsonOnly := parse("watch.go", "package cmd\n\ntype y struct{ A string `json:\"a\"` }\n")
	anon := parse("task.go", "package cmd\n\nvar z struct{ A string `toml:\"a\"` }\n")
	got := tomlStoreTypes(synth, []*ast.File{toml, jsonOnly})
	if len(got) != 1 || !strings.Contains(got[0], "internal/cmd/issue.go declares x ") {
		t.Errorf("findings = %v, want exactly one naming issue.go and x", got)
	}
	if got := tomlStoreTypes(synth, []*ast.File{jsonOnly}); len(got) != 0 {
		t.Errorf("a json-only struct was reported as a store: %v", got)
	}
	if got := tomlStoreTypes(synth, []*ast.File{anon}); len(got) != 1 || !strings.Contains(got[0], "task.go declares an anonymous struct") {
		t.Errorf("an anonymous toml struct gave %v, want one finding", got)
	}
}

// commandTreeLeftovers names everything in a file that is not part of a
// command tree: any type, var or const declaration, and any func that does not
// return exactly *cobra.Command.
func commandTreeLeftovers(fset *token.FileSet, f *ast.File) []string {
	file := filepath.Base(fset.Position(f.Pos()).Filename)
	var out []string
	for _, d := range f.Decls {
		switch d := d.(type) {
		case *ast.GenDecl:
			if d.Tok == token.IMPORT {
				continue
			}
			for _, spec := range d.Specs {
				switch sp := spec.(type) {
				case *ast.TypeSpec:
					out = append(out, fmt.Sprintf("%s declares type %s", file, sp.Name.Name))
				case *ast.ValueSpec:
					for _, n := range sp.Names {
						out = append(out, fmt.Sprintf("%s declares %s %s", file, d.Tok, n.Name))
					}
				}
			}
		case *ast.FuncDecl:
			if !returnsCobraCommand(d) {
				out = append(out, fmt.Sprintf("%s declares func %s, which does not return *cobra.Command", file, d.Name.Name))
			}
		}
	}
	return out
}

// returnsCobraCommand reports whether fd returns exactly one *cobra.Command.
func returnsCobraCommand(fd *ast.FuncDecl) bool {
	if fd.Recv != nil || fd.Type.Results == nil || len(fd.Type.Results.List) != 1 || len(fd.Type.Results.List[0].Names) > 1 {
		return false
	}
	star, ok := fd.Type.Results.List[0].Type.(*ast.StarExpr)
	if !ok {
		return false
	}
	sel, ok := star.X.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	return ok && pkg.Name == "cobra" && sel.Sel.Name == "Command"
}

// TestLocalGoIsOnlyTheCommandTree: with the store in internal/localstore,
// local.go is `dross local get|set` and nothing else — a type, var, const or
// helper there is the store growing back beside the command tree.
func TestLocalGoIsOnlyTheCommandTree(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, filepath.Join(repoRootFromTest(t), "internal", "cmd", "local.go"), nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range commandTreeLeftovers(fset, f) {
		t.Error(l)
	}
	cmds := 0
	for _, d := range f.Decls {
		if fd, ok := d.(*ast.FuncDecl); ok && returnsCobraCommand(fd) {
			cmds++
		}
	}
	if cmds < 3 {
		t.Errorf("local.go holds %d command constructors, want Local, localGet and localSet", cmds)
	}

	synth := token.NewFileSet()
	src := "package cmd\n\nimport \"github.com/spf13/cobra\"\n\n" +
		"const LocalFile = \"local.toml\"\n\n" +
		"type localStore struct{}\n\n" +
		"var localKeys = map[string]int{}\n\n" +
		"func Local() *cobra.Command { return nil }\n\n" +
		"func loadLocal(path string) (*localStore, error) { return nil, nil }\n"
	sf, err := parser.ParseFile(synth, "local.go", src, 0)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(commandTreeLeftovers(synth, sf), "\n")
	for _, want := range []string{"declares const LocalFile", "declares type localStore", "declares var localKeys", "declares func loadLocal"} {
		if !strings.Contains(got, want) {
			t.Errorf("a leftover was not named (%q):\n%s", want, got)
		}
	}
	if strings.Contains(got, "func Local,") {
		t.Errorf("a command constructor was reported:\n%s", got)
	}
}

// TestBoundaryDirectionRulesFire drives rules 1-3 over synthetic package maps
// and a synthetic tree: a fake internal/consent importing internal/cmd, a fake
// internal/foo importing cobra, and a cmd that dropped internal/consent.
func TestBoundaryDirectionRulesFire(t *testing.T) {
	root := repoRootFromTest(t)
	live, err := walkImports(filepath.Join(root, "internal"))
	if err != nil {
		t.Fatal(err)
	}

	t.Run("reverse import names both packages", func(t *testing.T) {
		pkgs := clonePkgs(live)
		consent := pkgs[modulePath+"/internal/consent"]
		consent.Files["x.go"] = []string{cmdPkgPath}
		pkgs[modulePath+"/internal/consent"] = consent
		got := checkBoundary(pkgs)
		if len(got) != 1 || !strings.Contains(got[0], "internal/consent imports "+cmdPkgPath) {
			t.Errorf("findings = %v", got)
		}
	})
	t.Run("cobra outside cmd fails exclusivity", func(t *testing.T) {
		pkgs := clonePkgs(live)
		pkgs[modulePath+"/internal/foo"] = pkgImports{Path: modulePath + "/internal/foo", Files: map[string][]string{"foo.go": {cobraPath}}}
		got := checkBoundary(pkgs)
		if len(got) != 1 || !strings.Contains(got[0], "cobra exclusivity: "+modulePath+"/internal/foo") {
			t.Errorf("findings = %v", got)
		}
	})
	t.Run("gitrun importing a module package fails the leaf rule", func(t *testing.T) {
		pkgs := clonePkgs(live)
		gr, ok := pkgs[gitrunPath]
		if !ok {
			t.Fatal("the live walk has no internal/gitrun")
		}
		gr.Files["gitrun.go"] = append(append([]string(nil), gr.Files["gitrun.go"]...), modulePath+"/internal/consent")
		got := checkBoundary(pkgs)
		if len(got) != 1 || !strings.Contains(got[0], "leaf: internal/gitrun imports "+modulePath+"/internal/consent") {
			t.Errorf("findings = %v, want exactly one leaf finding naming internal/consent", got)
		}
	})
	t.Run("cmd dropping an extracted package fails wiring", func(t *testing.T) {
		pkgs := clonePkgs(live)
		cmd := pkgImports{Path: cmdPkgPath, Files: map[string][]string{}}
		for file, imps := range live[cmdPkgPath].Files {
			var kept []string
			for _, i := range imps {
				if i != modulePath+"/internal/consent" {
					kept = append(kept, i)
				}
			}
			cmd.Files[file] = kept
		}
		pkgs[cmdPkgPath] = cmd
		got := checkBoundary(pkgs)
		if len(got) != 1 || !strings.Contains(got[0], "does not import "+modulePath+"/internal/consent") {
			t.Errorf("findings = %v", got)
		}
	})
	t.Run("a synthetic tree on disk parses the same way", func(t *testing.T) {
		dir := t.TempDir()
		write := func(rel, body string) {
			p := filepath.Join(dir, rel)
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		write("consent/x.go", "package consent\n\nimport _ \""+cmdPkgPath+"\"\n")
		write("consent/testdata/skipped.go", "package skipped\n\nimport _ \""+cobraPath+"\"\n")
		write("consent/x_test.go", "package consent\n\nimport _ \""+cobraPath+"\"\n")
		pkgs, err := walkImports(dir)
		if err != nil {
			t.Fatal(err)
		}
		if len(pkgs) != 1 {
			t.Fatalf("walk saw %d packages, want 1 (testdata and _test.go skipped)", len(pkgs))
		}
		p := pkgs[modulePath+"/internal/consent"]
		if !p.imports()[cmdPkgPath] || p.imports()[cobraPath] {
			t.Errorf("parsed imports = %v", p.imports())
		}
	})
}

// TestBoundaryVacuityFloor: an empty root, or a walk that sees cmd alone,
// returns the floor error rather than a pass.
func TestBoundaryVacuityFloor(t *testing.T) {
	empty, err := walkImports(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	got := checkBoundary(empty)
	if len(got) == 0 || !strings.Contains(strings.Join(got, "\n"), "vacuity") {
		t.Errorf("an empty tree passed: %v", got)
	}

	only, err := walkImports(filepath.Join(repoRootFromTest(t), "internal", "cmd"))
	if err != nil {
		t.Fatal(err)
	}
	got = checkBoundary(only)
	if len(got) == 0 || !strings.Contains(got[0], "vacuity") {
		t.Errorf("a walk over cmd alone passed: %v", got)
	}
}

// testFuncDeclRE is the shape tests_before.txt was recorded with.
var testFuncDeclRE = regexp.MustCompile(`^func (Test[A-Za-z0-9_]*)\(`)

// testMultiplicity counts, for every `func Test*` name under internalDir
// (testdata included, exactly as t-1 recorded it), the number of packages
// defining it.
func testMultiplicity(internalDir string) (map[string]map[string]bool, error) {
	out := map[string]map[string]bool{}
	err := filepath.WalkDir(internalDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, err := filepath.Rel(internalDir, filepath.Dir(path))
		if err != nil {
			return err
		}
		pkg := filepath.ToSlash(rel)
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
		for sc.Scan() {
			if m := testFuncDeclRE.FindStringSubmatch(sc.Text()); m != nil {
				if out[m[1]] == nil {
					out[m[1]] = map[string]bool{}
				}
				out[m[1]][pkg] = true
			}
		}
		return sc.Err()
	})
	return out, err
}

// TestNoTestLost: every test name recorded before the phase is defined in
// exactly as many packages now as then. Fewer means a test was deleted rather
// than moved; more means it was copied and left behind. Pre-existing homonyms
// (TestRegistryIsWellFormed in cmd and pathfence) keep their multiplicity and
// stay green. New names are free.
func TestNoTestLost(t *testing.T) {
	root := repoRootFromTest(t)
	before := map[string]map[string]bool{}
	f, err := os.Open(filepath.Join(root, "internal", "cmd", "testdata", "cli_surface", "tests_before.txt"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		name, pkg, ok := strings.Cut(sc.Text(), "\t")
		if !ok {
			t.Fatalf("malformed row %q", sc.Text())
		}
		if before[name] == nil {
			before[name] = map[string]bool{}
		}
		before[name][pkg] = true
	}
	if len(before) < 1000 {
		t.Fatalf("tests_before.txt holds %d names — the inventory is not the one t-1 recorded", len(before))
	}
	now, err := testMultiplicity(filepath.Join(root, "internal"))
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(before))
	for n := range before {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		was, is := len(before[n]), len(now[n])
		switch {
		case is < was:
			t.Errorf("%s: defined in %d package(s), was %d (%s) — dropped rather than moved", n, is, was, keys(before[n]))
		case is > was:
			t.Errorf("%s: defined in %d package(s), was %d — copied and left behind (%s)", n, is, was, keys(now[n]))
		}
	}
	// The homonym t-1 named keeps its multiplicity.
	if len(now["TestRegistryIsWellFormed"]) != 2 {
		t.Errorf("TestRegistryIsWellFormed multiplicity = %d, want the pre-existing 2", len(now["TestRegistryIsWellFormed"]))
	}
}

// TestNoTestLostDetectsDropsAndCopies proves the conservation check on a
// synthetic tree: one name dropped, one copied, one homonym unchanged.
func TestNoTestLostDetectsDropsAndCopies(t *testing.T) {
	dir := t.TempDir()
	write := func(rel, body string) {
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("a/a_test.go", "package a\n\nfunc TestKept(t *testing.T) {}\nfunc TestCopied(t *testing.T) {}\nfunc TestHomonym(t *testing.T) {}\n")
	write("b/b_test.go", "package b\n\nfunc TestCopied(t *testing.T) {}\nfunc TestHomonym(t *testing.T) {}\n")
	now, err := testMultiplicity(dir)
	if err != nil {
		t.Fatal(err)
	}
	before := map[string]int{"TestKept": 1, "TestCopied": 1, "TestHomonym": 2, "TestDropped": 1}
	var drops, copies int
	for n, was := range before {
		switch is := len(now[n]); {
		case is < was:
			drops++
		case is > was:
			copies++
		}
	}
	if drops != 1 || copies != 1 {
		t.Errorf("drops=%d copies=%d, want 1 and 1 (homonym and kept unchanged)", drops, copies)
	}
}

func clonePkgs(in map[string]pkgImports) map[string]pkgImports {
	out := make(map[string]pkgImports, len(in))
	for k, p := range in {
		files := make(map[string][]string, len(p.Files))
		for f, imps := range p.Files {
			files[f] = append([]string(nil), imps...)
		}
		out[k] = pkgImports{Path: p.Path, Files: files}
	}
	return out
}

func keys(m map[string]bool) string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return strings.Join(out, ", ")
}
