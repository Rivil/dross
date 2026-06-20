package quality

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// repoRoot walks up from the test's working directory to the module root (the
// directory holding go.mod). Tests run with cwd set to their package dir, so the
// repo .gitignore is not directly relative.
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

// TestQualityArtifactsGitignored asserts the quality run artifacts are actually
// ignored by git — a behavioural check (`git check-ignore`) that catches a wrong
// pattern a string-match on the .gitignore line would miss. The gitignore keeps
// raw per-run reports out of the repo; the committed record is the remediation
// spec (the locked report_artifact decision). git check-ignore exits 0 when the
// path is ignored, 1 when it is not.
func TestQualityArtifactsGitignored(t *testing.T) {
	root := repoRoot(t)
	cmd := exec.Command("git", "-C", root, "check-ignore", ".dross/quality/x/report.md")
	if err := cmd.Run(); err != nil {
		t.Fatalf("git check-ignore reports .dross/quality/x/report.md is NOT ignored (err=%v); "+
			"quality run artifacts must be gitignored so raw per-run findings are never committed", err)
	}
}
