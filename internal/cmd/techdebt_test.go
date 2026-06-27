package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/findings"
	"github.com/Rivil/dross/internal/techdebt"
)

// TestTechdebtCommandRegistered guards reachability: Techdebt() must be wired
// into the real root tree in cmd/dross/main.go, or `dross techdebt` would 404.
func TestTechdebtCommandRegistered(t *testing.T) {
	root := repoRootFromTest(t)
	b, err := os.ReadFile(filepath.Join(root, "cmd", "dross", "main.go"))
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	if !strings.Contains(string(b), "cmd.Techdebt()") {
		t.Error("cmd.Techdebt() is not registered in cmd/dross/main.go root.AddCommand — `dross techdebt` would be unreachable")
	}
}

// TestTechdebtRunNoGitFallback fails if the command can't run outside a git repo:
// it must fall back to a tree walk, complete with a "nogit" run id, still scan
// the walked files, and stamp last_run (otherwise the area reads "never run"
// right after a run).
func TestTechdebtRunNoGitFallback(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	if err := runCmd(t, Init()); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(dir, "debt.go"), "package main // FIXME real debt\n")

	if err := runCmd(t, Techdebt()); err != nil {
		t.Fatalf("techdebt hard-errored outside a git repo (should fall back): %v", err)
	}

	tdDir := filepath.Join(dir, ".dross", "techdebt")
	runDir := soleRunDir(t, tdDir)
	if !strings.Contains(runDir, "nogit") {
		t.Errorf("run id = %q, want a nogit id outside a git repo", runDir)
	}
	report, err := os.ReadFile(filepath.Join(tdDir, runDir, techdebt.ReportName))
	if err != nil {
		t.Fatalf("run did not write a report: %v", err)
	}
	if !strings.Contains(string(report), "debt.go") {
		t.Errorf("tree-walk scan missed the walked file's marker:\n%s", report)
	}
	store, err := findings.LoadStore(techdebt.StatePath(filepath.Join(dir, ".dross")))
	if err != nil {
		t.Fatal(err)
	}
	if store.NeverRun() {
		t.Fatal("techdebt run did not stamp last_run; the area would read 'never run' right after a run")
	}
}

// TestTechdebtEnumeratesTrackedFiles fails if the command scans untracked files:
// the locked decision is "across tracked files", so git ls-files enumeration must
// include a tracked marker and exclude an untracked one.
func TestTechdebtEnumeratesTrackedFiles(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	if err := runCmd(t, Init()); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "init")
	mustWrite(t, filepath.Join(dir, "tracked.go"), "package x // TODO tracked debt\n")
	gitRun(t, dir, "add", "tracked.go")
	// Staged but not the untracked one — ls-files reads the index, so this is
	// "tracked" even without a commit.
	mustWrite(t, filepath.Join(dir, "untracked.go"), "package x // FIXME untracked debt\n")

	if err := runCmd(t, Techdebt()); err != nil {
		t.Fatalf("techdebt: %v", err)
	}

	tdDir := filepath.Join(dir, ".dross", "techdebt")
	report, err := os.ReadFile(filepath.Join(tdDir, soleRunDir(t, tdDir), techdebt.ReportName))
	if err != nil {
		t.Fatal(err)
	}
	s := string(report)
	if !strings.Contains(s, "tracked.go") {
		t.Errorf("report missing the tracked file's marker:\n%s", s)
	}
	if strings.Contains(s, "untracked.go") {
		t.Errorf("report scanned an untracked file (violates the tracked-files decision):\n%s", s)
	}
}

// gitRun runs a git subcommand in dir and fails the test on error.
func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}
