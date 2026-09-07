package cmd

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Rivil/dross/internal/compilefence"
	"github.com/Rivil/dross/internal/findings"
	"github.com/Rivil/dross/internal/pathfence"
	"github.com/Rivil/dross/internal/security"
)

func TestSecurityCommandRegistered(t *testing.T) {
	sec := Security()
	if sec.Name() != "security" {
		t.Fatalf("command name = %q, want \"security\"", sec.Name())
	}
	want := map[string]bool{"detect": false, "run": false, "scaffold": false}
	for _, c := range sec.Commands() {
		if _, ok := want[c.Name()]; ok {
			want[c.Name()] = true
		}
	}
	for name, found := range want {
		if !found {
			t.Errorf("dross security is missing the %q subcommand", name)
		}
	}
}

func TestSecurityDetectOutput(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "main.go"), "package main")

	out := captureStdout(t, func() {
		if err := runCmd(t, Security(), "detect", dir); err != nil {
			t.Fatalf("detect: %v", err)
		}
	})
	if !strings.Contains(out, "scanners:") {
		t.Fatalf("detect output has no scanners section:\n%s", out)
	}
	// govulncheck is a Go scanner; main.go → go, so it must appear with a status
	// marker (installed or missing) — that's the installed-vs-missing report.
	if !strings.Contains(out, "govulncheck") {
		t.Errorf("detect output missing govulncheck:\n%s", out)
	}
	if !strings.Contains(out, "[installed]") && !strings.Contains(out, "[missing]") {
		t.Errorf("detect output has no installed/missing status markers:\n%s", out)
	}
}

func TestSecurityRunCreatesDir(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	if err := runCmd(t, Init()); err != nil {
		t.Fatal(err)
	}
	// A run must succeed even when no scanners are installed (partial coverage),
	// not hard-error.
	if err := runCmd(t, Security(), "run", "."); err != nil {
		t.Fatalf("run hard-errored (should proceed with partial coverage): %v", err)
	}
	secDir := filepath.Join(dir, ".dross", "security")
	runDir := soleRunDir(t, secDir)
	if _, err := os.Stat(filepath.Join(secDir, runDir, "report.md")); err != nil {
		t.Errorf("run did not write report.md in the run dir: %v", err)
	}
}

// soleRunDir returns the name of the single run directory under dir, ignoring the
// sibling state.toml signal ledger (which now lives beside the run dirs). It
// fails unless exactly one run directory is present.
func soleRunDir(t *testing.T, dir string) string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	var runs []string
	for _, e := range entries {
		if e.IsDir() {
			runs = append(runs, e.Name())
		}
	}
	if len(runs) != 1 {
		t.Fatalf("%s should hold exactly one run dir, got dirs %v (all entries %v)", dir, runs, entries)
	}
	return runs[0]
}

// TestSecurityRunStampsLastRun fails if a run doesn't record the store-level
// last_run signal (status would then read "never run" right after a run), and if
// stamping clobbers a pre-existing findings ledger instead of merging.
func TestSecurityRunStampsLastRun(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	if err := runCmd(t, Init()); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(dir, ".dross")
	if err := os.MkdirAll(security.SecurityDir(root), 0o755); err != nil {
		t.Fatal(err)
	}
	statePath := security.StatePath(root)
	// Seed an existing ledger record so we can prove the stamp merges, not overwrites.
	seed := &findings.Store{Records: []findings.Record{
		{Fingerprint: "keep", State: findings.StateTracked, Title: "survive"},
	}}
	if err := findings.SaveStore(statePath, seed); err != nil {
		t.Fatal(err)
	}

	if err := runCmd(t, Security(), "run", "."); err != nil {
		t.Fatalf("run: %v", err)
	}

	store, err := findings.LoadStore(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if store.NeverRun() {
		t.Fatal("security run did not stamp last_run; the area would read 'never run' right after a run")
	}
	if time.Since(store.LastRun) > time.Minute {
		t.Fatalf("stamped last_run is not ~now: %v", store.LastRun)
	}
	if got, ok := store.Get("keep"); !ok || got.Title != "survive" {
		t.Fatalf("stamp clobbered the existing ledger record: %+v ok=%v", got, ok)
	}
}

func TestSecurityScaffoldCommand(t *testing.T) {
	runDir := t.TempDir()
	ledger := security.Ledger{Findings: []security.Finding{
		{ID: "f-1", Title: "cmd injection in git shell-out", Severity: security.SeverityCritical,
			Class: "cmd-injection", Refutation: "panel: confirmed reachable"},
	}}
	if err := security.Save(containedIn(t, runDir, "findings.toml"), ledger); err != nil {
		t.Fatal(err)
	}

	if err := runCmd(t, Security(), "scaffold", runDir); err != nil {
		t.Fatalf("scaffold on a valid ledger errored: %v", err)
	}
	// The happy path must actually write spec.toml — this fails if any of the
	// command's `if err != nil` guards were negated (an early return would skip
	// the write).
	if _, err := os.Stat(filepath.Join(runDir, "spec.toml")); err != nil {
		t.Fatalf("scaffold did not write spec.toml: %v", err)
	}
}

func TestSecurityScaffoldEmptyLedgerErrors(t *testing.T) {
	runDir := t.TempDir()
	// A finding with no refutation is not a survivor → zero survivors → the
	// scaffold writer's empty guard fires, and the command must surface that error
	// (this kills the negation of the WriteScaffoldSpec error guard).
	ledger := security.Ledger{Findings: []security.Finding{
		{ID: "f-1", Severity: security.SeverityHigh, Refutation: ""},
	}}
	if err := security.Save(containedIn(t, runDir, "findings.toml"), ledger); err != nil {
		t.Fatal(err)
	}
	if err := runCmd(t, Security(), "scaffold", runDir); err == nil {
		t.Fatal("scaffold on a zero-survivor ledger returned nil; want an error")
	}
}

// withDockleInstalled overrides the security command's PATH lookup so dockle reads as
// installed, letting the image-scan decision be exercised without dockle on the box.
func withDockleInstalled(t *testing.T) {
	t.Helper()
	prev := securityLookPath
	securityLookPath = func(bin string) (string, error) {
		if bin == security.DockleBin {
			return "/fake/dockle", nil
		}
		return prev(bin)
	}
	t.Cleanup(func() { securityLookPath = prev })
}

// TestSecurityRunImageFlagRunsDockle proves a supplied --image makes the run scan
// that exact ref with dockle. If the flag is ignored, no "scanning image" line is
// emitted and this fails.
func TestSecurityRunImageFlagRunsDockle(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	if err := runCmd(t, Init()); err != nil {
		t.Fatal(err)
	}
	withDockleInstalled(t)
	t.Setenv("DROSS_IMAGE", "")

	const ref = "registry.example.com/app:1.2.3"
	out := captureStdout(t, func() {
		if err := runCmd(t, Security(), "run", "--image", ref, "."); err != nil {
			t.Fatalf("run: %v", err)
		}
	})
	if !strings.Contains(out, "dockle: scanning image "+ref) {
		t.Fatalf("a supplied --image must run dockle against that ref:\n%s", out)
	}
}

// TestSecurityRunNoImageSkipsDockle proves that with dockle installed but no image
// supplied, the run skips dockle WITH the no-image reason — never a silent ran/all-clear.
func TestSecurityRunNoImageSkipsDockle(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	if err := runCmd(t, Init()); err != nil {
		t.Fatal(err)
	}
	withDockleInstalled(t)
	t.Setenv("DROSS_IMAGE", "")

	out := captureStdout(t, func() {
		if err := runCmd(t, Security(), "run", "."); err != nil {
			t.Fatalf("run: %v", err)
		}
	})
	if strings.Contains(out, "scanning image") {
		t.Fatalf("no image supplied must not run dockle:\n%s", out)
	}
	if !strings.Contains(out, "dockle: skipped") || !strings.Contains(out, "needs a built image") {
		t.Fatalf("no image must skip dockle with the no-image reason, not read as all-clear:\n%s", out)
	}
}

// TestSecurityRunEmptyImageFlagSkips pins the empty-flag guard: a whitespace --image
// is treated as no image (skipped), never as a real ref.
func TestSecurityRunEmptyImageFlagSkips(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	if err := runCmd(t, Init()); err != nil {
		t.Fatal(err)
	}
	withDockleInstalled(t)
	t.Setenv("DROSS_IMAGE", "")

	out := captureStdout(t, func() {
		if err := runCmd(t, Security(), "run", "--image", "   ", "."); err != nil {
			t.Fatalf("run: %v", err)
		}
	})
	if strings.Contains(out, "scanning image") {
		t.Fatalf("a whitespace --image must be treated as no image (skipped):\n%s", out)
	}
	if !strings.Contains(out, "dockle: skipped") {
		t.Fatalf("empty --image must skip dockle:\n%s", out)
	}
}

// TestSecurityRunImageEnvFallback proves the c-8 config channel: $DROSS_IMAGE feeds
// dockle when --image is unset.
func TestSecurityRunImageEnvFallback(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	if err := runCmd(t, Init()); err != nil {
		t.Fatal(err)
	}
	withDockleInstalled(t)
	t.Setenv("DROSS_IMAGE", "env.example.com/img:9")

	out := captureStdout(t, func() {
		if err := runCmd(t, Security(), "run", "."); err != nil {
			t.Fatalf("run: %v", err)
		}
	})
	if !strings.Contains(out, "dockle: scanning image env.example.com/img:9") {
		t.Fatalf("$DROSS_IMAGE must feed dockle when --image is unset:\n%s", out)
	}
}

// containedIn builds the run-dir Contained the security and quality commands
// build at their eight call sites, so a test can hand the retyped
// security.Save / quality.Save a value without duplicating the Contain call.
func containedIn(t *testing.T, runDir, name string) pathfence.Contained {
	t.Helper()
	c, err := pathfence.Contain(runDir, "run directory", name)
	if err != nil {
		t.Fatalf("Contain(%q, %q): %v", runDir, name, err)
	}
	return c
}

// assertRunDirContainment is the shared behaviour the two duplicated
// containedPath blocks (security_test.go and quality_test.go) collapsed onto
// when containedPath was deleted: one implementation, asserted once, exercised
// from both commands' read-only tests.
//
// The message assertions are tighter than containedPath's were: a refusal must
// name the offending path, the artifact ("run directory") and the root, so a
// delegation that drops the c-5 context fails here rather than passing silently.
func assertRunDirContainment(t *testing.T, runDir string) {
	t.Helper()

	for _, name := range []string{"../main.go", filepath.Join("..", "..", "etc", "passwd")} {
		_, err := pathfence.Contain(runDir, "run directory", name)
		if err == nil {
			t.Errorf("Contain accepted %q — it must refuse a path escaping the run dir", name)
			continue
		}
		if !errors.Is(err, pathfence.ErrEscapes) {
			t.Errorf("Contain(%q) error is not ErrEscapes: %v", name, err)
		}
		msg := err.Error()
		for _, want := range []string{name, "run directory", runDir} {
			if !strings.Contains(msg, want) {
				t.Errorf("refusal of %q does not name %q — a hand-edited artifact cannot be fixed from the message alone:\n%s",
					name, want, msg)
			}
		}
	}

	// An absolute path is refused too, and distinguishably so: it never escapes,
	// so it carries ErrAbsolute rather than ErrEscapes.
	if _, err := pathfence.Contain(runDir, "run directory", filepath.Join(runDir, "report.md")); !errors.Is(err, pathfence.ErrAbsolute) {
		t.Errorf("Contain must refuse an absolute path with ErrAbsolute, got %v", err)
	}

	// A normal artifact name resolves inside the run dir.
	got, err := pathfence.Contain(runDir, "run directory", "report.md")
	if err != nil {
		t.Fatalf("Contain refused a normal name: %v", err)
	}
	if !strings.HasPrefix(got.String(), runDir+string(os.PathSeparator)) {
		t.Errorf("Contain(%q) = %q, escapes run dir", "report.md", got.String())
	}
	if got.Rel() != "report.md" {
		t.Errorf("Contain(%q).Rel() = %q, want the root-relative form", "report.md", got.Rel())
	}
}

func TestSecurityRunReadOnly(t *testing.T) {
	runDir := t.TempDir()

	// A finding-derived name escaping the run dir must be refused. The check is
	// pathfence's now — containedPath is gone — but the behaviour it guarded is
	// asserted here unchanged.
	assertRunDirContainment(t, runDir)

	// A full run must touch only paths under .dross/security/.
	repo := t.TempDir()
	chdir(t, repo)
	if err := runCmd(t, Init()); err != nil {
		t.Fatal(err)
	}
	if err := runCmd(t, Security(), "run", "."); err != nil {
		t.Fatal(err)
	}
	secDir := filepath.Join(repo, ".dross", "security")
	err := filepath.Walk(secDir, func(path string, _ os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !strings.HasPrefix(path, secDir) {
			t.Fatalf("run wrote outside .dross/security/: %q", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// TestSecurityCover_ReconcileLoadsRealLedger drives the REAL securityFindings
// ItemsForRun closure (not the fakeTool stub) end-to-end via `reconcile <run-dir>`.
// A valid findings.toml with one finding must reconcile as "1 new". If either
// guard in the closure — pathfence.Contain's err check (security.go:46) or
// security.Load's err check (security.go:50) — is negated to `if err == nil`, the
// closure returns early with zero items and the output reads "0 new" instead.
func TestSecurityCover_ReconcileLoadsRealLedger(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	if err := runCmd(t, Init()); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(dir, ".dross")
	if err := os.MkdirAll(security.SecurityDir(root), 0o755); err != nil {
		t.Fatal(err)
	}
	runDir := t.TempDir()
	ledger := security.Ledger{Findings: []security.Finding{
		{ID: "f-1", Title: "tainted exec", Severity: security.SeverityHigh,
			Class: "cmd-injection", File: "x.go", Refutation: "panel: confirmed"},
	}}
	if err := security.Save(containedIn(t, runDir, "findings.toml"), ledger); err != nil {
		t.Fatal(err)
	}

	out := captureStdout(t, func() {
		if err := runCmd(t, securityFindings(), "reconcile", runDir); err != nil {
			t.Fatalf("reconcile: %v", err)
		}
	})
	if !strings.Contains(out, "1 new") {
		t.Fatalf("reconcile of a one-finding ledger must report 1 new (proves the real "+
			"ItemsForRun loaded the ledger, not an early return):\n%s", out)
	}
}

// TestSecurityCover_ReconcileMalformedLedgerErrors pins the security.Load error
// guard in the ItemsForRun closure (security.go:50). A garbled findings.toml makes
// security.Load return an error; the real guard returns it and reconcile fails.
// Negating that guard to `if err == nil` would swallow the error, fall through to
// ledger.Items() on an empty ledger, and report a bogus success.
func TestSecurityCover_ReconcileMalformedLedgerErrors(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	if err := runCmd(t, Init()); err != nil {
		t.Fatal(err)
	}
	runDir := t.TempDir()
	mustWrite(t, filepath.Join(runDir, "findings.toml"), "this is not = valid toml [[[")

	if err := runCmd(t, securityFindings(), "reconcile", runDir); err == nil {
		t.Fatal("reconcile of a malformed findings.toml returned nil; the security.Load " +
			"error guard must surface the parse error")
	}
}

// --- t-11 structural guards: the second containment implementation is gone ---

// TestContainedPathIsGone asserts the duplicate check was DELETED, not merely
// left unused. c-1 is about there being one implementation; a dead-but-present
// containedPath would satisfy every behavioural test here and still be a second
// ..-prefix test in non-test code for the next reader to reach for.
func TestContainedPathIsGone(t *testing.T) {
	fset := token.NewFileSet()
	err := filepath.Walk("..", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}
		f, perr := parser.ParseFile(fset, path, nil, 0)
		if perr != nil {
			return perr
		}
		for _, d := range f.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if ok && fn.Name.Name == "containedPath" {
				t.Errorf("%s still declares containedPath — the second containment implementation must be deleted, not left unused", path)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// TestRunDirSitesPassContainedThrough is the structural half of c-1/c-3 for the
// cmd layer. Behavioural tests cannot see the difference between a site that
// keeps its Contained and one that unwraps it and re-joins — both open the same
// file — so the shape is asserted directly:
//
//   - each file builds its paths with exactly four pathfence.Contain calls,
//   - no site re-derives a path with filepath.Join over the run dir,
//   - no site unwraps the value it passes on with .String(),
//   - neither report writer reaches os.WriteFile behind the seam's back.
func TestRunDirSitesPassContainedThrough(t *testing.T) {
	for _, file := range []string{"security.go", "quality.go"} {
		t.Run(file, func(t *testing.T) {
			fset := token.NewFileSet()
			f, err := parser.ParseFile(fset, file, nil, 0)
			if err != nil {
				t.Fatal(err)
			}
			contains := 0
			ast.Inspect(f, func(n ast.Node) bool {
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
				case pkg != nil && pkg.Name == "pathfence" && sel.Sel.Name == "Contain":
					contains++
				case pkg != nil && pkg.Name == "os" && sel.Sel.Name == "WriteFile":
					t.Errorf("%s:%d: os.WriteFile — the report writer must go through pathfence.WriteFile",
						file, fset.Position(call.Pos()).Line)
				case pkg != nil && pkg.Name == "filepath" && sel.Sel.Name == "Join" && joinsRunDir(call):
					t.Errorf("%s:%d: filepath.Join over the run dir — the site must keep the Contained it was given, not re-derive the path",
						file, fset.Position(call.Pos()).Line)
				case sel.Sel.Name == "String" && pkg != nil && containedVar(pkg.Name):
					t.Errorf("%s:%d: %s.String() — the Contained must pass through unconverted; unwrapping here un-moves the boundary t-3 moved",
						file, fset.Position(call.Pos()).Line, pkg.Name)
				}
				return true
			})
			if contains != 4 {
				t.Errorf("%s has %d pathfence.Contain calls, want 4 (findings.toml x2, spec.toml, report.md)", file, contains)
			}
		})
	}
}

// joinsRunDir reports whether a filepath.Join call takes runDir as its first
// argument — the shape a site would revert to if it stopped carrying a Contained.
func joinsRunDir(call *ast.CallExpr) bool {
	if len(call.Args) == 0 {
		return false
	}
	id, ok := call.Args[0].(*ast.Ident)
	return ok && id.Name == "runDir"
}

// containedVar names the locals that hold a Contained at the eight call sites.
func containedVar(name string) bool {
	switch name {
	case "ledgerPath", "outPath", "reportPath":
		return true
	}
	return false
}

// TestRunDirSitesCannotUnwrap is the compile-time counterpart: inserting a
// .String() between a run-dir Contain and the retyped security/quality function
// must fail to BUILD, so the boundary t-3 moved cannot be quietly un-moved by a
// later edit that "just needed a string here".
func TestRunDirSitesCannotUnwrap(t *testing.T) {
	if testing.Short() {
		t.Skip("runs go build")
	}
	compilefence.AssertCompiles(t, `package tmpfence

import (
	"github.com/Rivil/dross/internal/pathfence"
	"github.com/Rivil/dross/internal/security"
)

func f(runDir string) {
	c, err := pathfence.Contain(runDir, "run directory", "findings.toml")
	if err != nil {
		return
	}
	_, _ = security.Load(c)
}
`)

	compilefence.AssertDoesNotCompile(t, `package tmpfence

import (
	"github.com/Rivil/dross/internal/pathfence"
	"github.com/Rivil/dross/internal/security"
)

func f(runDir string) {
	c, err := pathfence.Contain(runDir, "run directory", "findings.toml")
	if err != nil {
		return
	}
	_, _ = security.Load(c.String())
}
`, "as pathfence.Contained value in argument to security.Load")

	compilefence.AssertDoesNotCompile(t, `package tmpfence

import (
	"github.com/Rivil/dross/internal/pathfence"
	"github.com/Rivil/dross/internal/quality"
)

func f(runDir string) {
	c, err := pathfence.Contain(runDir, "run directory", "spec.toml")
	if err != nil {
		return
	}
	_ = quality.WriteScaffoldSpec(c.String(), "07-x", "x", quality.Ledger{})
}
`, "as pathfence.Contained value in argument to quality.WriteScaffoldSpec")
}
