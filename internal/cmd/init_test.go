package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/architecture"
	"github.com/Rivil/dross/internal/project"
)

func loadInitedProject(t *testing.T, dir string) *project.Project {
	t.Helper()
	p, err := project.Load(filepath.Join(dir, ".dross", "project.toml"))
	if err != nil {
		t.Fatalf("load project.toml: %v", err)
	}
	return p
}

func TestInitSeedsRuntimeFromProfile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	chdir(t, dir)

	if err := runCmd(t, Init()); err != nil {
		t.Fatalf("init: %v", err)
	}
	p := loadInitedProject(t, dir)

	if p.Stack.Profile != "go" {
		t.Errorf("[stack].profile = %q, want \"go\"", p.Stack.Profile)
	}
	want := map[string]string{
		"test":      "go test -count=1 ./...",
		"typecheck": "go vet ./...",
		"format":    "gofmt -l .",
		"build":     "make build",
	}
	got := map[string]string{
		"test":      p.Runtime.TestCommand,
		"typecheck": p.Runtime.TypecheckCommand,
		"format":    p.Runtime.FormatCommand,
		"build":     p.Runtime.BuildCommand,
	}
	for k, w := range want {
		if got[k] != w {
			t.Errorf("[runtime].%s = %q, want %q (must come from the Go profile, not a hardcoded guess)", k, got[k], w)
		}
	}
}

func TestInitUnsupportedLeavesRuntimeUnseeded(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# project"), 0o644); err != nil {
		t.Fatal(err)
	}
	chdir(t, dir)

	if err := runCmd(t, Init()); err != nil {
		t.Fatalf("init: %v", err)
	}
	p := loadInitedProject(t, dir)

	if p.Stack.Profile != "" {
		t.Errorf("[stack].profile = %q, want empty for an unsupported stack", p.Stack.Profile)
	}
	for name, cmd := range map[string]string{
		"test": p.Runtime.TestCommand, "typecheck": p.Runtime.TypecheckCommand,
		"format": p.Runtime.FormatCommand, "build": p.Runtime.BuildCommand,
	} {
		if cmd != "" {
			t.Errorf("[runtime].%s = %q, want empty — no fabricated commands on an unsupported stack", name, cmd)
		}
	}
}

// TestInitSeedsArchitectureSkeleton guards c-3: dross init must seed an
// ARCHITECTURE.md skeleton at repo root. If seeding is removed, this fails
// (test_contract 1).
func TestInitSeedsArchitectureSkeleton(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)

	if err := runCmd(t, Init()); err != nil {
		t.Fatalf("init: %v", err)
	}

	path := filepath.Join(dir, architecture.File)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("ARCHITECTURE.md not seeded at repo root: %v", err)
	}
	content := mustRead(t, path)
	if !strings.Contains(content, "# Architecture") {
		t.Errorf("seeded ARCHITECTURE.md missing header:\n%s", content)
	}
	if !strings.Contains(content, "organized by feature") {
		t.Errorf("seeded doc should declare feature-organization:\n%s", content)
	}
}

// TestInitDoesNotClobberExistingArchitecture guards first-creation-only: init
// must not overwrite an ARCHITECTURE.md the repo already has (idempotent
// refresh is deferred).
func TestInitDoesNotClobberExistingArchitecture(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)

	path := filepath.Join(dir, architecture.File)
	const existing = "# Mine\n\nhand-written.\n"
	if err := os.WriteFile(path, []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := runCmd(t, Init()); err != nil {
		t.Fatalf("init: %v", err)
	}

	if got := mustRead(t, path); got != existing {
		t.Errorf("init clobbered existing ARCHITECTURE.md:\ngot:  %q\nwant: %q", got, existing)
	}
}
