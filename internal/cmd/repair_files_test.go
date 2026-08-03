package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

// repairFilesFixture builds a repo with one committed tracked .dross/ file
// baseline, returning the repo dir.
func repairFilesFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	gitInit(t, dir, "")
	mustWrite(t, filepath.Join(dir, ".dross", "project.toml"), "name = \"x\"\n")
	mustWrite(t, filepath.Join(dir, ".dross", "phases", "01-x", "spec.toml"), "id = \"01-x\"\n")
	mustGit(t, dir, "add", ".")
	mustGit(t, dir, "commit", "-q", "-m", "chore: baseline")
	return dir
}

func TestDetectModifiedOrMissingTrackedReportsDeletedFile(t *testing.T) {
	dir := repairFilesFixture(t)
	specPath := filepath.Join(dir, ".dross", "phases", "01-x", "spec.toml")
	if err := os.Remove(specPath); err != nil {
		t.Fatal(err)
	}

	found, err := detectModifiedOrMissingTracked(dir)
	if err != nil {
		t.Fatalf("detect: %v", err)
	}
	if len(found) != 1 || found[0].Path != ".dross/phases/01-x/spec.toml" || !found[0].Missing {
		t.Fatalf("expected one missing spec.toml finding, got %+v", found)
	}
}

func TestDetectModifiedOrMissingTrackedReportsClobberedContent(t *testing.T) {
	dir := repairFilesFixture(t)
	projPath := filepath.Join(dir, ".dross", "project.toml")
	mustWrite(t, projPath, "name = \"clobbered\"\n")

	found, err := detectModifiedOrMissingTracked(dir)
	if err != nil {
		t.Fatalf("detect: %v", err)
	}
	if len(found) != 1 || found[0].Path != ".dross/project.toml" || found[0].Missing {
		t.Fatalf("expected one clobbered project.toml finding, got %+v", found)
	}
}

func TestDetectModifiedOrMissingTrackedHealthyIsEmpty(t *testing.T) {
	dir := repairFilesFixture(t)

	found, err := detectModifiedOrMissingTracked(dir)
	if err != nil {
		t.Fatalf("detect: %v", err)
	}
	if len(found) != 0 {
		t.Fatalf("expected no findings on a healthy tree, got %+v", found)
	}
}

func TestRestorePathFromRefRestoresExactBlob(t *testing.T) {
	dir := repairFilesFixture(t)
	projPath := filepath.Join(dir, ".dross", "project.toml")
	mustWrite(t, projPath, "name = \"clobbered\"\n")

	if err := restorePathFromRef(dir, "HEAD", ".dross/project.toml"); err != nil {
		t.Fatalf("restore: %v", err)
	}

	body := mustRead(t, projPath)
	if body != "name = \"x\"\n" {
		t.Fatalf("restored content = %q, want original", body)
	}
	if status := mustGit(t, dir, "status", "--porcelain"); status != "" {
		t.Fatalf("expected clean tree after restore, got:\n%s", status)
	}
}

func TestRestorePathFromRefRecreatesMissingFile(t *testing.T) {
	dir := repairFilesFixture(t)
	specPath := filepath.Join(dir, ".dross", "phases", "01-x", "spec.toml")
	if err := os.Remove(specPath); err != nil {
		t.Fatal(err)
	}

	if err := restorePathFromRef(dir, "HEAD", ".dross/phases/01-x/spec.toml"); err != nil {
		t.Fatalf("restore: %v", err)
	}

	if _, err := os.Stat(specPath); err != nil {
		t.Fatalf("expected spec.toml to be recreated: %v", err)
	}
	if status := mustGit(t, dir, "status", "--porcelain"); status != "" {
		t.Fatalf("expected clean tree after restore, got:\n%s", status)
	}
}
