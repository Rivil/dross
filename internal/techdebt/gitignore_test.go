package techdebt

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// repoRoot walks up from the test's working directory to the module root (the
// directory holding go.mod), so the repo .gitignore is reachable.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found walking up from test dir")
		}
		dir = parent
	}
}

// TestTechDebtArtifactsGitignored asserts the tech-debt run artifacts are
// actually ignored by git — a behavioural check (`git check-ignore`) that a
// string match on the .gitignore line would miss. Tech-debt run dirs are
// disposable; the durable signal is state.toml, so the run output stays out of
// git (symmetric with security/quality). git check-ignore exits 0 when ignored.
func TestTechDebtArtifactsGitignored(t *testing.T) {
	root := repoRoot(t)
	cmd := exec.Command("git", "-C", root, "check-ignore", ".dross/techdebt/x/report.md")
	if err := cmd.Run(); err != nil {
		t.Fatalf("git check-ignore reports .dross/techdebt/x/report.md is NOT ignored (err=%v); "+
			"tech-debt run artifacts must be gitignored so a run never dirties the repo", err)
	}
}
