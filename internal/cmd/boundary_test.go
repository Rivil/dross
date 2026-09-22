package cmd

import (
	"bufio"
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
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
//  4. The forbidden-import RATCHET (locked ratchet_baseline): a non-test
//     internal/cmd file importing os/exec, net/http or go/ast must be named in
//     cmdForbiddenBaseline, and every baseline entry must still import the
//     package it is listed for. Adding a file fails; a stale entry fails asking
//     for its removal. That is what makes the list shrink-only: new domain
//     logic landing in cmd is a red test, and the debt is visible.
//
// The checker is a pure function over {package -> imports} so the same rules
// run over the live tree AND over synthetic maps that prove each rule fires.
// A vacuity floor mirrors execConsentFloor: a walk that saw too few packages,
// or missed internal/cmd, is an error rather than a pass.

// forbiddenInCmd are the imports the ratchet gates in internal/cmd.
var forbiddenInCmd = []string{"os/exec", "net/http", "go/ast"}

// cmdForbiddenBaseline is the shrink-only allowlist: the internal/cmd files
// still importing a forbidden package after this phase, per package. Derived
// by grep when the phase landed; doctor.go is deliberately absent (t-7 drained
// its os/exec), and the four load-bearing spawn sites — phase.go,
// survivor_drain.go, test.go, verify.go — are here because they are the
// gated surface itself, not domain logic. Draining the rest is the
// cmd-exec-baseline-drain phase's job; remove an entry here the moment its
// import goes, or the test asks you to.
var cmdForbiddenBaseline = map[string][]string{
	"os/exec": {
		"cleantree.go", "init.go", "lane_install.go", "milestone_stale.go", "pause.go",
		"phase.go", "redproof_replay.go", "run.go", "ship_recover.go", "stack.go",
		"statusline.go", "survivor_drain.go", "techdebt.go", "test.go", "update.go",
		"verify.go", "worktree_files.go",
	},
	"net/http": {"update.go"},
	"go/ast":   {},
}

// extractedPackages are the four the phase pulled out of cmd, which cmd must
// import.
var extractedPackages = []string{
	modulePath + "/internal/consent",
	modulePath + "/internal/boardsync",
	modulePath + "/internal/diag",
	modulePath + "/internal/mutationcfg",
}

const (
	modulePath  = "github.com/Rivil/dross"
	cmdPkgPath  = modulePath + "/internal/cmd"
	cobraPath   = "github.com/spf13/cobra"
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

// checkBoundary applies the four rules plus the vacuity floor and returns
// every finding, one line each, sorted. An empty result is a pass.
func checkBoundary(pkgs map[string]pkgImports, baseline map[string][]string) []string {
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

	// 3: cmd wires in each extracted package.
	cmdImps := cmd.imports()
	for _, want := range extractedPackages {
		if !cmdImps[want] {
			findings = append(findings, fmt.Sprintf("wiring: internal/cmd does not import %s — the extraction is not consumed", want))
		}
	}

	// 4: the ratchet, per forbidden package.
	for _, forbidden := range forbiddenInCmd {
		allowed := map[string]bool{}
		for _, f := range baseline[forbidden] {
			allowed[f] = true
		}
		importing := map[string]bool{}
		for file, imps := range cmd.Files {
			for _, i := range imps {
				if i == forbidden {
					importing[file] = true
				}
			}
		}
		for file := range importing {
			if !allowed[file] {
				findings = append(findings, fmt.Sprintf("ratchet: internal/cmd/%s imports %s and is not in the baseline — move the logic out of cmd rather than widening the list", file, forbidden))
			}
		}
		for file := range allowed {
			if !importing[file] {
				findings = append(findings, fmt.Sprintf("ratchet: baseline lists internal/cmd/%s for %s but it no longer imports it — remove it (the list only shrinks)", file, forbidden))
			}
		}
	}
	sort.Strings(findings)
	return findings
}

// TestCmdBoundaryByImportDirection is the live-tree run: green exactly when
// the baseline lists the observed importers and every direction rule holds.
func TestCmdBoundaryByImportDirection(t *testing.T) {
	pkgs, err := walkImports(filepath.Join(repoRootFromTest(t), "internal"))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range checkBoundary(pkgs, cmdForbiddenBaseline) {
		t.Error(f)
	}
	t.Logf("boundary walk saw %d packages", len(pkgs))
}

// TestCmdForbiddenImportRatchet proves the ratchet bites in both directions
// over a copy of the live tree: adding "os/exec" to a copy of issue.go is a
// finding naming the file and the import, a baseline entry that no longer
// imports is a finding asking for its removal, and the real baseline plus one
// extra existing cmd file fails.
func TestCmdForbiddenImportRatchet(t *testing.T) {
	root := repoRootFromTest(t)
	live, err := walkImports(filepath.Join(root, "internal"))
	if err != nil {
		t.Fatal(err)
	}
	if got := checkBoundary(live, cmdForbiddenBaseline); len(got) != 0 {
		t.Fatalf("precondition: the live tree is not clean:\n%s", strings.Join(got, "\n"))
	}

	t.Run("a new importer fails naming file and import", func(t *testing.T) {
		pkgs := clonePkgs(live)
		cmd := pkgs[cmdPkgPath]
		cmd.Files["issue.go"] = append(append([]string(nil), cmd.Files["issue.go"]...), "os/exec")
		pkgs[cmdPkgPath] = cmd
		got := checkBoundary(pkgs, cmdForbiddenBaseline)
		if len(got) != 1 || !strings.Contains(got[0], "internal/cmd/issue.go imports os/exec") {
			t.Errorf("findings = %v, want one naming issue.go and os/exec", got)
		}
	})
	t.Run("a stale entry fails asking for removal", func(t *testing.T) {
		baseline := cloneBaseline(cmdForbiddenBaseline)
		baseline["os/exec"] = append(baseline["os/exec"], "issue.go") // issue.go does not import os/exec
		got := checkBoundary(live, baseline)
		if len(got) != 1 || !strings.Contains(got[0], "baseline lists internal/cmd/issue.go for os/exec") || !strings.Contains(got[0], "remove it") {
			t.Errorf("findings = %v, want one asking to remove issue.go", got)
		}
	})
	t.Run("a file that dropped its import fails as stale", func(t *testing.T) {
		pkgs := clonePkgs(live)
		cmd := pkgs[cmdPkgPath]
		var kept []string
		for _, i := range cmd.Files["cleantree.go"] {
			if i != "os/exec" {
				kept = append(kept, i)
			}
		}
		cmd.Files["cleantree.go"] = kept
		pkgs[cmdPkgPath] = cmd
		got := checkBoundary(pkgs, cmdForbiddenBaseline)
		if len(got) != 1 || !strings.Contains(got[0], "internal/cmd/cleantree.go for os/exec") {
			t.Errorf("findings = %v, want one stale entry for cleantree.go", got)
		}
	})
	t.Run("go/ast has no importer and an empty baseline", func(t *testing.T) {
		if len(cmdForbiddenBaseline["go/ast"]) != 0 {
			t.Error("the go/ast baseline is not empty")
		}
		for file, imps := range live[cmdPkgPath].Files {
			for _, i := range imps {
				if i == "go/ast" {
					t.Errorf("internal/cmd/%s imports go/ast", file)
				}
			}
		}
	})
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
		got := checkBoundary(pkgs, cmdForbiddenBaseline)
		if len(got) != 1 || !strings.Contains(got[0], "internal/consent imports "+cmdPkgPath) {
			t.Errorf("findings = %v", got)
		}
	})
	t.Run("cobra outside cmd fails exclusivity", func(t *testing.T) {
		pkgs := clonePkgs(live)
		pkgs[modulePath+"/internal/foo"] = pkgImports{Path: modulePath + "/internal/foo", Files: map[string][]string{"foo.go": {cobraPath}}}
		got := checkBoundary(pkgs, cmdForbiddenBaseline)
		if len(got) != 1 || !strings.Contains(got[0], "cobra exclusivity: "+modulePath+"/internal/foo") {
			t.Errorf("findings = %v", got)
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
		got := checkBoundary(pkgs, cmdForbiddenBaseline)
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
	got := checkBoundary(empty, cmdForbiddenBaseline)
	if len(got) == 0 || !strings.Contains(strings.Join(got, "\n"), "vacuity") {
		t.Errorf("an empty tree passed: %v", got)
	}

	only, err := walkImports(filepath.Join(repoRootFromTest(t), "internal", "cmd"))
	if err != nil {
		t.Fatal(err)
	}
	got = checkBoundary(only, cmdForbiddenBaseline)
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

func cloneBaseline(in map[string][]string) map[string][]string {
	out := make(map[string][]string, len(in))
	for k, v := range in {
		out[k] = append([]string(nil), v...)
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
