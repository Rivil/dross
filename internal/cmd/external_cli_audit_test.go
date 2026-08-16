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
)

// A repo-wide gate on one property: no test's OUTCOME may depend on whether an
// unvendored external CLI happens to be installed on the machine running it.
//
// This exists because the failure is invisible until a machine without the CLI
// runs the suite. `postGitHubComment` called exec.LookPath("gh") on every error
// path, which silently overrode the ghCommand seam its test had stubbed: the
// test asserting that gh's own output is surfaced passed on a laptop with gh
// and reddened on helicon, which has none. Nothing about the code changed
// between the two runs. That is the worst shape a red can take, because it
// looks exactly like a real failure and teaches the loop to ignore reds — and
// the remote runner makes it likelier, not less likely.
//
// THE RULE:
//
//   - A test may drive an unvendored CLI through its package's overridable
//     command seam (see externalCLISeams). The seam is the supported answer:
//     stub it and the binary is never consulted.
//   - A test that genuinely needs the REAL binary must guard on exec.LookPath
//     in the same function and t.Skip when it is absent, naming it.
//   - Anything else — a bare exec.Command("gh", …) in a _test.go — is a
//     finding.
//
// NOT IN SCOPE, with reason:
//
//   - `git` is a hard prerequisite of dross itself, asserted by doctor and used
//     by almost every fixture in this suite. A repo that cannot run git cannot
//     run dross, so gating on it would flag the entire fixture layer for a
//     dependency that is already mandatory.
//   - POSIX shell utilities (sh, true, false, echo, sleep, cat) are present on
//     every platform this repo supports and are how the spawn-seam tests
//     simulate a command without depending on anything real.
//   - `go` is the toolchain compiling the test. If it is missing, nothing ran.
var unvendoredCLIs = map[string]string{
	"gh":          "GitHub CLI",
	"glab":        "GitLab CLI",
	"gremlins":    "Go mutation runner",
	"npx":         "Node package runner",
	"npm":         "Node package manager",
	"pnpm":        "Node package manager",
	"yarn":        "Node package manager",
	"dotnet":      "the .NET SDK",
	"rsync":       "remote sync",
	"ssh":         "remote transport",
	"semgrep":     "SAST scanner",
	"ast-grep":    "structural search",
	"osv-scanner": "dependency scanner",
	"minisign":    "release signing",
}

// externalCLISeams names the overridable package-level var each CLI is
// supposed to be driven through, as "<package-dir>:<var>".
//
// It is deliberately NOT an exception list. Nothing here suppresses a finding;
// it is the remedy the failure message points at, and
// TestExternalCLIAuditSeamsAreNamed asserts every entry resolves to a real
// package-level var — so an entry cannot be added to wave a bypass through.
var externalCLISeams = map[string]string{
	"gh": "internal/ship:ghCommand",
}

// externalCLIRoots are the trees walked. Both are scanned because the bug this
// gate was written against lived in internal/ship, not internal/cmd — a walk
// that stopped at this package would have missed it entirely.
var externalCLIRoots = []string{"internal", "cmd"}

type cliFinding struct {
	pos  string
	bin  string
	hint string
}

func (f cliFinding) String() string {
	return fmt.Sprintf("%s: test drives %q (%s) directly — %s", f.pos, f.bin, unvendoredCLIs[f.bin], f.hint)
}

// auditTestFile reports every unguarded unvendored-CLI invocation in one parsed
// test file.
//
// Guards are collected per FUNCTION, not per file: a LookPath guard in one test
// says nothing about the test below it, and treating the file as the unit is
// how a guard gets "inherited" by a test that never had one.
func auditTestFile(fset *token.FileSet, f *ast.File) []cliFinding {
	var out []cliFinding
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		guarded, skips := guardsIn(fn.Body)
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			bin, ok := execBinary(call)
			if !ok {
				return true
			}
			if _, unvendored := unvendoredCLIs[bin]; !unvendored {
				return true
			}
			if guarded[bin] && skips {
				return true
			}
			out = append(out, cliFinding{
				pos:  fset.Position(call.Pos()).String(),
				bin:  bin,
				hint: remedyFor(bin),
			})
			return true
		})
	}
	return out
}

func remedyFor(bin string) string {
	if seam, ok := externalCLISeams[bin]; ok {
		return "stub the " + seam + " seam, or guard on exec.LookPath(" + quoteBin(bin) + ") + t.Skip"
	}
	return "guard on exec.LookPath(" + quoteBin(bin) + ") and t.Skip when it is absent"
}

func quoteBin(s string) string { return "\"" + s + "\"" }

// guardsIn reports which binaries a function LookPath-guards, and whether it
// skips at all. Both halves are required: a LookPath whose result is ignored
// guards nothing.
func guardsIn(body *ast.BlockStmt) (map[string]bool, bool) {
	guarded := map[string]bool{}
	skips := false
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		pkg, _ := sel.X.(*ast.Ident)
		switch {
		case pkg != nil && pkg.Name == "exec" && sel.Sel.Name == "LookPath" && len(call.Args) == 1:
			if lit, ok := testStringLit(call.Args[0]); ok {
				guarded[lit] = true
			}
		case strings.HasPrefix(sel.Sel.Name, "Skip"):
			skips = true
		}
		return true
	})
	return guarded, skips
}

// execBinary returns the literal binary name of an exec.Command /
// exec.CommandContext call.
//
// A non-literal binary is deliberately NOT a finding here, unlike in the
// subprocess-argument audit: test code routinely builds a command name from a
// table, and the property this gate protects is about which binaries a test
// depends on existing, which a computed name cannot answer either way.
func execBinary(call *ast.CallExpr) (string, bool) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return "", false
	}
	pkg, _ := sel.X.(*ast.Ident)
	if pkg == nil || pkg.Name != "exec" {
		return "", false
	}
	idx := 0
	switch sel.Sel.Name {
	case "Command", "LookPath":
		if sel.Sel.Name == "LookPath" {
			return "", false // a lookup is the guard, not the dependency
		}
	case "CommandContext":
		idx = 1
	default:
		return "", false
	}
	if len(call.Args) <= idx {
		return "", false
	}
	return testStringLit(call.Args[idx])
}

func testStringLit(e ast.Expr) (string, bool) {
	lit, ok := e.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	return strings.Trim(lit.Value, "`\""), true
}

func runCLIAudit(t *testing.T) ([]cliFinding, int) {
	t.Helper()
	root := repoRootForDocs(t)
	fset := token.NewFileSet()
	var findings []cliFinding
	scanned := 0

	for _, r := range externalCLIRoots {
		err := filepath.WalkDir(filepath.Join(root, r), func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(path, "_test.go") {
				return nil
			}
			f, perr := parser.ParseFile(fset, path, nil, 0)
			if perr != nil {
				return perr
			}
			scanned++
			findings = append(findings, auditTestFile(fset, f)...)
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", r, err)
		}
	}
	return findings, scanned
}

// TestNoTestDependsOnUnvendoredCLI is the gate.
func TestNoTestDependsOnUnvendoredCLI(t *testing.T) {
	findings, scanned := runCLIAudit(t)
	if scanned == 0 {
		t.Fatal("the audit scanned zero test files — it is not looking where it thinks it is")
	}
	if len(findings) == 0 {
		return
	}
	msgs := make([]string, 0, len(findings))
	for _, f := range findings {
		msgs = append(msgs, f.String())
	}
	sort.Strings(msgs)
	t.Errorf("%d test(s) depend on an unvendored CLI being installed; a red from one says nothing about the code:\n  %s",
		len(findings), strings.Join(msgs, "\n  "))
}

// TestExternalCLIAuditCatchesABypass: the guard's own failure path. A gate that
// silently matches nothing passes forever and protects nothing — which is the
// state this whole property was in until a host without gh ran the suite.
func TestExternalCLIAuditCatchesABypass(t *testing.T) {
	got := auditTestSnippet(t, `func TestBypass(t *testing.T) {
	out, _ := exec.Command("gh", "pr", "view").CombinedOutput()
	_ = out
}`)
	if len(got) != 1 {
		t.Fatalf("the audit reported %d finding(s) over a direct gh invocation, want exactly 1: %v", len(got), got)
	}
	if got[0].bin != "gh" {
		t.Errorf("finding names %q, want gh", got[0].bin)
	}
	if !strings.Contains(got[0].String(), "ghCommand") {
		t.Errorf("the finding does not point at the seam: %s", got[0])
	}

	// CommandContext is the same dependency wearing a different call.
	ctxForm := auditTestSnippet(t, `func TestBypassCtx(t *testing.T) {
	_ = exec.CommandContext(context.Background(), "dotnet", "tool", "list")
}`)
	if len(ctxForm) != 1 {
		t.Fatalf("exec.CommandContext escaped the audit: %v", ctxForm)
	}
}

// TestExternalCLIAuditAllowsGuardedUse: a test that genuinely needs the real
// binary and says so is fine. Both halves are required — a LookPath whose
// result is thrown away guards nothing.
func TestExternalCLIAuditAllowsGuardedUse(t *testing.T) {
	clean := auditTestSnippet(t, `func TestGuarded(t *testing.T) {
	if _, err := exec.LookPath("gh"); err != nil {
		t.Skip("gh is not installed")
	}
	_ = exec.Command("gh", "--version")
}`)
	if len(clean) != 0 {
		t.Errorf("a LookPath-guarded, skipping test was flagged: %v", clean)
	}

	noSkip := auditTestSnippet(t, `func TestLooksButDoesNotSkip(t *testing.T) {
	_, _ = exec.LookPath("gh")
	_ = exec.Command("gh", "--version")
}`)
	if len(noSkip) != 1 {
		t.Errorf("a LookPath whose result is ignored was accepted: %v", noSkip)
	}

	wrongBin := auditTestSnippet(t, `func TestGuardsTheWrongThing(t *testing.T) {
	if _, err := exec.LookPath("glab"); err != nil {
		t.Skip("glab is not installed")
	}
	_ = exec.Command("gh", "--version")
}`)
	if len(wrongBin) != 1 {
		t.Errorf("a guard on a DIFFERENT binary was accepted: %v", wrongBin)
	}

	// The exclusions have to hold, or the gate flags the whole fixture layer.
	for _, bin := range []string{"git", "sh", "true", "go"} {
		if f := auditTestSnippet(t, `func TestExcluded(t *testing.T) { _ = exec.Command("`+bin+`", "x") }`); len(f) != 0 {
			t.Errorf("%s is not an unvendored CLI but was flagged: %v", bin, f)
		}
	}
}

// TestExternalCLIAuditGuardsAreFunctionScoped: a guard in one test must not
// license the test below it. File-scoped collection is the plausible-looking
// version of this walk that silently stops working.
func TestExternalCLIAuditGuardsAreFunctionScoped(t *testing.T) {
	got := auditTestSnippet(t, `func TestGuarded(t *testing.T) {
	if _, err := exec.LookPath("gh"); err != nil {
		t.Skip("gh is not installed")
	}
	_ = exec.Command("gh", "--version")
}

func TestUnguarded(t *testing.T) {
	_ = exec.Command("gh", "pr", "list")
}`)
	if len(got) != 1 {
		t.Fatalf("reported %d finding(s), want exactly 1 — the second test inherited the first's guard: %v", len(got), got)
	}
	if !strings.Contains(got[0].pos, ":") {
		t.Errorf("finding carries no position: %+v", got[0])
	}
}

// TestExternalCLIAuditSeamsAreNamed: every seam the audit points a reader at
// must be a real package-level var. The map is a remedy list, not an exception
// list — an entry that resolved to nothing would be a bypass waved through by
// naming, which is exactly what this must not become.
func TestExternalCLIAuditSeamsAreNamed(t *testing.T) {
	root := repoRootForDocs(t)
	for bin, seam := range externalCLISeams {
		dir, name, ok := strings.Cut(seam, ":")
		if !ok {
			t.Errorf("seam %q for %s is not in <package-dir>:<var> form", seam, bin)
			continue
		}
		if !packageDeclaresVar(t, filepath.Join(root, dir), name) {
			t.Errorf("%s names seam %s, but %s declares no package-level var %q", bin, seam, dir, name)
		}
	}
}

// packageDeclaresVar reports whether dir's non-test sources declare a
// package-level var called name.
func packageDeclaresVar(t *testing.T, dir, name string) bool {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	fset := token.NewFileSet()
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, filepath.Join(dir, e.Name()), nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", e.Name(), err)
		}
		for _, decl := range f.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok || gd.Tok != token.VAR {
				continue
			}
			for _, spec := range gd.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for _, id := range vs.Names {
					if id.Name == name {
						return true
					}
				}
			}
		}
	}
	return false
}

func auditTestSnippet(t *testing.T, body string) []cliFinding {
	t.Helper()
	fset := token.NewFileSet()
	const preamble = "package cmd\n" +
		"import (\n\t\"context\"\n\t\"os/exec\"\n\t\"testing\"\n)\n" +
		"var _ = context.Background\n"
	f, err := parser.ParseFile(fset, "snippet_test.go", preamble+body+"\n", 0)
	if err != nil {
		t.Fatalf("snippet does not parse: %v\n%s", err, body)
	}
	return auditTestFile(fset, f)
}
