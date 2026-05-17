package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/state"
)

// recoverFixture builds the divergent state that the old strip-filter
// used to leave behind on every ship:
//   - local main has the cumulative `.dross/` tree from phase commits
//   - origin/main has a synthetic squash commit with the source files
//     only (no phase `.dross/` artefacts).
//
// Returns (repo dir, pre-merge SHA holding the full `.dross/` tree).
func recoverFixture(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	remoteDir := t.TempDir()
	mustGit(t, remoteDir, "init", "-q", "--bare", "-b", "main")
	gitInit(t, dir, remoteDir)
	chdir(t, dir)

	if err := runCmd(t, Init()); err != nil {
		t.Fatalf("init: %v", err)
	}
	if err := runCmd(t, Project(), "set", "repo.git_main_branch", "main"); err != nil {
		t.Fatal(err)
	}

	// Mark current_phase so `dross ship recover` (no args) works.
	root := filepath.Join(dir, ".dross")
	st, _ := state.Load(filepath.Join(root, state.File))
	st.CurrentPhase = "01-x"
	if err := st.Save(filepath.Join(root, state.File)); err != nil {
		t.Fatal(err)
	}

	// Baseline commit. Commit the init scaffold (.dross/ + .gitattributes)
	// so .dross/ has tracked content from the start.
	mustWrite(t, filepath.Join(dir, "README.md"), "base\n")
	mustGit(t, dir, "add", ".")
	mustGit(t, dir, "commit", "-q", "-m", "chore: baseline")
	mustGit(t, dir, "push", "-q", "-u", "origin", "main")
	baseSHA := mustGit(t, dir, "rev-parse", "HEAD")

	// Phase commits — code + .dross/ artefacts.
	mustWrite(t, filepath.Join(dir, "src/a.ts"), "export const a = 1\n")
	mustWrite(t, filepath.Join(dir, ".dross/phases/01-x/spec.toml"), `id = "01-x"`)
	mustGit(t, dir, "add", ".")
	mustGit(t, dir, "commit", "-q", "-m", "feat: add a + spec")

	mustWrite(t, filepath.Join(dir, "src/b.ts"), "export const b = 2\n")
	mustWrite(t, filepath.Join(dir, ".dross/phases/01-x/changes.json"), `{"phase":"01-x"}`)
	mustGit(t, dir, "add", ".")
	mustGit(t, dir, "commit", "-q", "-m", "feat: add b + changes")
	preMergeSHA := mustGit(t, dir, "rev-parse", "HEAD")

	// Synthesize the squash on origin/main: source files only, no phase
	// .dross/ artefacts. Mirrors what the old strip-filter produced after
	// `gh pr merge --squash`.
	mustGit(t, dir, "checkout", "-q", "-b", "squash-sim", baseSHA)
	mustGit(t, dir, "checkout", preMergeSHA, "--", "src/")
	mustGit(t, dir, "add", "src/")
	mustGit(t, dir, "commit", "-q", "-m", "feat(squash): phase 01-x")
	mustGit(t, dir, "push", "-q", "--force", "origin", "squash-sim:main")

	// Restore local main to the pre-merge state.
	mustGit(t, dir, "checkout", "-q", "main")
	mustGit(t, dir, "branch", "-D", "squash-sim")
	// Update the local origin/main tracking ref to match remote — without
	// this, the fetch in recover would still see the old origin/main.
	mustGit(t, dir, "fetch", "-q", "origin")

	return dir, preMergeSHA
}

func TestShipRecoverHappyPath(t *testing.T) {
	dir, preMergeSHA := recoverFixture(t)

	if err := runCmd(t, Ship(), "recover"); err != nil {
		t.Fatalf("recover: %v", err)
	}

	// HEAD should be 1 commit ahead of origin/main (the restore commit).
	ahead := mustGit(t, dir, "rev-list", "--count", "origin/main..HEAD")
	if ahead != "1" {
		t.Errorf("expected 1 commit ahead of origin/main, got %s", ahead)
	}

	// .dross/ phase artefacts must be back in the tree.
	headTree := mustGit(t, dir, "ls-tree", "-r", "--name-only", "HEAD")
	for _, want := range []string{
		".dross/phases/01-x/spec.toml",
		".dross/phases/01-x/changes.json",
		".dross/project.toml",
		"src/a.ts",
		"src/b.ts",
	} {
		if !strings.Contains(headTree, want) {
			t.Errorf("HEAD tree missing %s:\n%s", want, headTree)
		}
	}

	// Working tree must reflect the same files on disk.
	for _, want := range []string{".dross/phases/01-x/spec.toml", "src/a.ts"} {
		if _, err := os.Stat(filepath.Join(dir, want)); err != nil {
			t.Errorf("working tree missing %s: %v", want, err)
		}
	}

	// state.json should record the merge action.
	s, _ := state.Load(filepath.Join(dir, ".dross", state.File))
	found := false
	for _, a := range s.History {
		if strings.Contains(a.Action, "merged 01-x") {
			found = true
		}
	}
	if !found {
		t.Errorf("state history should record merge; history: %+v", s.History)
	}

	// preMergeSHA is referenced for diagnostic clarity.
	_ = preMergeSHA
}

func TestShipRecoverRefusesDirtyTree(t *testing.T) {
	dir, _ := recoverFixture(t)

	// Stage an extra change to dirty the tree.
	mustWrite(t, filepath.Join(dir, "src/dirty.ts"), "dirty\n")

	err := runCmd(t, Ship(), "recover")
	if err == nil {
		t.Fatal("expected error when working tree is dirty")
	}
	if !strings.Contains(err.Error(), "dirty") {
		t.Errorf("error should mention dirty tree: %v", err)
	}
}

func TestShipRecoverRefusesWrongBranch(t *testing.T) {
	dir, _ := recoverFixture(t)

	mustGit(t, dir, "checkout", "-q", "-b", "feature")

	err := runCmd(t, Ship(), "recover")
	if err == nil {
		t.Fatal("expected error when not on main")
	}
	if !strings.Contains(err.Error(), "must be on main") {
		t.Errorf("error should mention wrong branch: %v", err)
	}
}

func TestShipRecoverRefusesSHAWithoutDross(t *testing.T) {
	dir, _ := recoverFixture(t)

	// Build a commit pointed at the well-known empty tree — guaranteed
	// to have no .dross/ tree object. Real-world this would be a user
	// pointing at the wrong SHA; the pre-check should surface a clear
	// error instead of letting `git checkout -- .dross/` fail with a
	// pathspec error.
	const emptyTreeSHA = "4b825dc642cb6eb9a060e54bf8d69288fbee4904"
	noDrossSHA := mustGit(t, dir, "commit-tree", emptyTreeSHA, "-m", "empty")

	err := runCmd(t, Ship(), "recover", "--pre-merge-sha="+noDrossSHA)
	if err == nil {
		t.Fatal("expected error: SHA has no .dross/ tree object")
	}
	if !strings.Contains(err.Error(), "no .dross/ tree") {
		t.Errorf("error should explain missing tree: %v", err)
	}
}

func TestShipRecoverPreMergeSHAOverride(t *testing.T) {
	dir, preMergeSHA := recoverFixture(t)

	// Simulate "user already manually reset main", then recover with the
	// explicit pre-merge SHA — the documented escape hatch.
	mustGit(t, dir, "reset", "--hard", "origin/main")

	if err := runCmd(t, Ship(), "recover", "--pre-merge-sha="+preMergeSHA); err != nil {
		t.Fatalf("recover with --pre-merge-sha: %v", err)
	}

	headTree := mustGit(t, dir, "ls-tree", "-r", "--name-only", "HEAD")
	if !strings.Contains(headTree, ".dross/phases/01-x/spec.toml") {
		t.Errorf("override path should restore phase .dross/:\n%s", headTree)
	}
}
