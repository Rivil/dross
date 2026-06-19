package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/architecture"
)

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
