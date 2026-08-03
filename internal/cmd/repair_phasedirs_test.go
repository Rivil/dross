package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

// phaseDirsFixture builds a repo with two phase dirs (x, y) pushed to a bare
// origin/main, then deletes x from the local working tree — simulating a
// checkout that wiped a phase dir origin still knows about. Returns the
// repo dir and its .dross root.
func phaseDirsFixture(t *testing.T) (repoDir, root string) {
	t.Helper()
	dir := t.TempDir()
	remoteDir := t.TempDir()
	mustGit(t, remoteDir, "init", "-q", "--bare", "-b", "main")
	gitInit(t, dir, remoteDir)

	root = filepath.Join(dir, ".dross")
	mustWrite(t, filepath.Join(root, "phases", "x", "spec.toml"), "id = \"x\"\n")
	mustWrite(t, filepath.Join(root, "phases", "x", "plan.toml"), "id = \"x\"\n")
	mustWrite(t, filepath.Join(root, "phases", "y", "spec.toml"), "id = \"y\"\n")
	mustGit(t, dir, "add", ".")
	mustGit(t, dir, "commit", "-q", "-m", "chore: scaffold x + y")
	mustGit(t, dir, "push", "-q", "-u", "origin", "main")

	if err := os.RemoveAll(filepath.Join(root, "phases", "x")); err != nil {
		t.Fatal(err)
	}
	return dir, root
}

func TestDetectMissingPhaseDirsFlagsWipedDir(t *testing.T) {
	dir, root := phaseDirsFixture(t)

	missing, err := detectMissingPhaseDirs(dir, root, "main")
	if err != nil {
		t.Fatalf("detect: %v", err)
	}
	if len(missing) != 1 || missing[0] != "x" {
		t.Fatalf("expected only x flagged missing, got %v", missing)
	}
}

func TestRestorePathFromRefRepopulatesPhaseDir(t *testing.T) {
	dir, root := phaseDirsFixture(t)

	if err := restorePathFromRef(dir, "origin/main", ".dross/phases/x"); err != nil {
		t.Fatalf("restore: %v", err)
	}

	spec := mustRead(t, filepath.Join(root, "phases", "x", "spec.toml"))
	if spec != "id = \"x\"\n" {
		t.Fatalf("restored spec.toml = %q, want original", spec)
	}
	plan := mustRead(t, filepath.Join(root, "phases", "x", "plan.toml"))
	if plan != "id = \"x\"\n" {
		t.Fatalf("restored plan.toml = %q, want original", plan)
	}
}

func TestDetectMissingPhaseDirsIgnoresUnknownDirs(t *testing.T) {
	dir, root := phaseDirsFixture(t)

	missing, err := detectMissingPhaseDirs(dir, root, "main")
	if err != nil {
		t.Fatalf("detect: %v", err)
	}
	for _, id := range missing {
		if id == "z" {
			t.Fatalf("z never existed on origin/main and must not be flagged: %v", missing)
		}
	}
	// y is present locally and on origin — must never be flagged either.
	for _, id := range missing {
		if id == "y" {
			t.Fatalf("y is present locally and must not be flagged: %v", missing)
		}
	}
}
