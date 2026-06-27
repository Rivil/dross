package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Rivil/dross/internal/findings"
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
	if err := security.Save(filepath.Join(runDir, "findings.toml"), ledger); err != nil {
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
	if err := security.Save(filepath.Join(runDir, "findings.toml"), ledger); err != nil {
		t.Fatal(err)
	}
	if err := runCmd(t, Security(), "scaffold", runDir); err == nil {
		t.Fatal("scaffold on a zero-survivor ledger returned nil; want an error")
	}
}

func TestSecurityRunReadOnly(t *testing.T) {
	runDir := t.TempDir()

	// A finding-derived name escaping the run dir must be refused.
	if _, err := containedPath(runDir, "../main.go"); err == nil {
		t.Error("containedPath accepted \"../main.go\" — it must refuse a path escaping the run dir")
	}
	if _, err := containedPath(runDir, filepath.Join("..", "..", "etc", "passwd")); err == nil {
		t.Error("containedPath accepted a deep traversal path; it must refuse it")
	}
	// A normal artifact name resolves inside the run dir.
	got, err := containedPath(runDir, "report.md")
	if err != nil {
		t.Fatalf("containedPath refused a normal name: %v", err)
	}
	if !strings.HasPrefix(got, runDir+string(os.PathSeparator)) {
		t.Errorf("containedPath(%q) = %q, escapes run dir", "report.md", got)
	}

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
	err = filepath.Walk(secDir, func(path string, _ os.FileInfo, err error) error {
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
