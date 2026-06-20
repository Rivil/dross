package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
	entries, err := os.ReadDir(secDir)
	if err != nil {
		t.Fatalf("no .dross/security dir created: %v", err)
	}
	if len(entries) != 1 || !entries[0].IsDir() {
		t.Fatalf(".dross/security should hold exactly one run dir, got %v", entries)
	}
	if _, err := os.Stat(filepath.Join(secDir, entries[0].Name(), "report.md")); err != nil {
		t.Errorf("run did not write report.md in the run dir: %v", err)
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
