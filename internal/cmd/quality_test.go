package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/quality"
)

func TestQualityCommandRegistered(t *testing.T) {
	q := Quality()
	if q.Name() != "quality" {
		t.Fatalf("command name = %q, want \"quality\"", q.Name())
	}
	want := map[string]bool{"detect": false, "run": false, "scaffold": false}
	for _, c := range q.Commands() {
		if _, ok := want[c.Name()]; ok {
			want[c.Name()] = true
		}
	}
	for name, found := range want {
		if !found {
			t.Errorf("dross quality is missing the %q subcommand", name)
		}
	}
}

func TestQualityDetectOutput(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "main.go"), "package main")

	out := captureStdout(t, func() {
		if err := runCmd(t, Quality(), "detect", dir); err != nil {
			t.Fatalf("detect: %v", err)
		}
	})
	if !strings.Contains(out, "analyzers:") {
		t.Fatalf("detect output has no analyzers section:\n%s", out)
	}
	// gocyclo is a Go analyzer; main.go → go, so it must appear with a status
	// marker (installed or missing) — that's the installed-vs-missing report.
	if !strings.Contains(out, "gocyclo") {
		t.Errorf("detect output missing gocyclo:\n%s", out)
	}
	if !strings.Contains(out, "[installed]") && !strings.Contains(out, "[missing]") {
		t.Errorf("detect output has no installed/missing status markers:\n%s", out)
	}
}

func TestQualityRunCreatesDir(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	if err := runCmd(t, Init()); err != nil {
		t.Fatal(err)
	}
	// A run must succeed even when no analyzers are installed (partial coverage),
	// not hard-error.
	if err := runCmd(t, Quality(), "run", "."); err != nil {
		t.Fatalf("run hard-errored (should proceed with partial coverage): %v", err)
	}
	qDir := filepath.Join(dir, ".dross", "quality")
	entries, err := os.ReadDir(qDir)
	if err != nil {
		t.Fatalf("no .dross/quality dir created: %v", err)
	}
	if len(entries) != 1 || !entries[0].IsDir() {
		t.Fatalf(".dross/quality should hold exactly one run dir, got %v", entries)
	}
}

func TestQualityRunWritesManifest(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	if err := runCmd(t, Init()); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(dir, "main.go"), "package main")
	if err := runCmd(t, Quality(), "run", "."); err != nil {
		t.Fatal(err)
	}
	qDir := filepath.Join(dir, ".dross", "quality")
	entries, err := os.ReadDir(qDir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("expected one run dir under .dross/quality, got %v (err=%v)", entries, err)
	}
	report, err := os.ReadFile(filepath.Join(qDir, entries[0].Name(), "report.md"))
	if err != nil {
		t.Fatalf("run did not write report.md: %v", err)
	}
	// The report must carry the tool-coverage manifest — a named Go analyzer with
	// a ran/skipped status. If the manifest section were dropped, a thin toolbelt
	// would read as a clean "all clear".
	if !strings.Contains(string(report), "Tool coverage") {
		t.Errorf("report.md has no Tool coverage section:\n%s", report)
	}
	if !strings.Contains(string(report), "gocyclo") {
		t.Errorf("report.md manifest missing the gocyclo analyzer:\n%s", report)
	}
}

func TestQualityScaffold(t *testing.T) {
	runDir := t.TempDir()
	ledger := quality.Ledger{Findings: []quality.Finding{
		{ID: "f-1", Title: "god function orchestrates the whole run", Risk: quality.RiskCritical,
			Dimension: quality.Complexity, Refutation: "panel: central, churny — confirmed"},
	}}
	if err := quality.Save(filepath.Join(runDir, "findings.toml"), ledger); err != nil {
		t.Fatal(err)
	}

	if err := runCmd(t, Quality(), "scaffold", runDir); err != nil {
		t.Fatalf("scaffold on a valid ledger errored: %v", err)
	}
	// The happy path must actually write spec.toml — this fails if any of the
	// command's `if err != nil` guards were negated (an early return would skip
	// the write).
	if _, err := os.Stat(filepath.Join(runDir, "spec.toml")); err != nil {
		t.Fatalf("scaffold did not write spec.toml: %v", err)
	}
}

func TestQualityScaffoldEmptyErrors(t *testing.T) {
	runDir := t.TempDir()
	// A finding with no refutation is not a survivor → zero survivors → the
	// scaffold writer's empty guard fires, and the command must surface that error
	// (this kills the negation of the WriteScaffoldSpec error guard).
	ledger := quality.Ledger{Findings: []quality.Finding{
		{ID: "f-1", Risk: quality.RiskHigh, Refutation: ""},
	}}
	if err := quality.Save(filepath.Join(runDir, "findings.toml"), ledger); err != nil {
		t.Fatal(err)
	}
	if err := runCmd(t, Quality(), "scaffold", runDir); err == nil {
		t.Fatal("scaffold on a zero-survivor ledger returned nil; want an error")
	}
}

func TestQualityRunReadOnly(t *testing.T) {
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

	// A full run must touch only paths under .dross/quality/.
	repo := t.TempDir()
	chdir(t, repo)
	if err := runCmd(t, Init()); err != nil {
		t.Fatal(err)
	}
	if err := runCmd(t, Quality(), "run", "."); err != nil {
		t.Fatal(err)
	}
	qDir := filepath.Join(repo, ".dross", "quality")
	err = filepath.Walk(qDir, func(path string, _ os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !strings.HasPrefix(path, qDir) {
			t.Fatalf("run wrote outside .dross/quality/: %q", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
