package cmd

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/codex"
)

// TestCodexCover_argsRequired exercises the empty-args guard (codex.go:21).
// Real `len(args) == 0` returns the specific error; the negated form would
// skip it and proceed, so asserting the exact message kills the mutant.
func TestCodexCover_argsRequired(t *testing.T) {
	cmd := Codex()
	err := cmd.RunE(cmd, []string{})
	if err == nil {
		t.Fatal("expected error when no target files are given")
	}
	if !strings.Contains(err.Error(), "at least one target file is required") {
		t.Fatalf("wrong error: %v", err)
	}
}

// TestCodexCover_rendersForValidFile drives the happy path of RunE with a
// real file. It kills the negation on codex.go:21 (non-empty args must NOT
// short-circuit to the error) and codex.go:25 (err == nil must fall through
// to renderCodex, not early-return): both are observable via the presence of
// the "# codex" header in captured stdout plus a nil error.
func TestCodexCover_rendersForValidFile(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	mustWrite(t, filepath.Join(dir, "sample.go"), "package p\n\nfunc Foo() {}\n")

	cmd := Codex()
	var runErr error
	out := captureStdout(t, func() {
		runErr = cmd.RunE(cmd, []string{"sample.go"})
	})
	if runErr != nil {
		t.Fatalf("RunE on a valid file: %v", runErr)
	}
	if !strings.Contains(out, "# codex") {
		t.Errorf("expected codex header in output, got:\n%s", out)
	}
}

// TestCodexCover_renderSections pins the five `len(res.X) > 0` section guards
// in renderCodex (codex.go:41,49,57,65,73). A fully-populated Result must emit
// every section header (kills CONDITIONALS_NEGATION: >0 -> <=0), and an empty
// Result must emit none of them (kills CONDITIONALS_BOUNDARY: >0 -> >=0).
func TestCodexCover_renderSections(t *testing.T) {
	headers := []string{
		"## symbols",
		"## refs (best-effort cross-file mentions)",
		"## siblings",
		"## recent activity",
		"## errors (non-fatal)",
	}

	full := &codex.Result{
		TargetFiles: []string{"a.go"},
		Symbols:     []codex.Symbol{{Name: "Foo", Kind: "function", File: "a.go", Line: 3}},
		Callers:     []codex.Symbol{{Name: "Foo", File: "b.go", Line: 7}},
		Siblings:    []string{"pkg/b.go"},
		RecentLog:   []string{"abc123 fix thing"},
		Errors:      []string{"a.go: boom"},
	}
	got := captureStdout(t, func() { renderCodex(full) })
	for _, want := range headers {
		if !strings.Contains(got, want) {
			t.Errorf("populated render missing %q\n--- output ---\n%s", want, got)
		}
	}

	empty := &codex.Result{TargetFiles: []string{"a.go"}}
	got = captureStdout(t, func() { renderCodex(empty) })
	for _, notWant := range headers {
		if strings.Contains(got, notWant) {
			t.Errorf("empty render should omit %q\n--- output ---\n%s", notWant, got)
		}
	}
}
