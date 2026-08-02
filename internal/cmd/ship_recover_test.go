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

	// The restore commit exists on HEAD AND is pushed (c-2): local main ends
	// level with origin/main, not sitting one chore ahead to re-seed
	// divergence at the next squash-merge.
	if msg := mustGit(t, dir, "log", "-1", "--format=%s"); !strings.Contains(msg, "restore .dross/") {
		t.Errorf("HEAD should be the restore commit, got: %q", msg)
	}
	if ahead := mustGit(t, dir, "rev-list", "origin/main..HEAD"); ahead != "" {
		t.Errorf("the restore commit must be pushed (origin/main..HEAD empty), got: %q", ahead)
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

// TestShipRecoverGuardsTheRecordedBase (c-4): recover resets a branch, so it
// must guard and reset the branch THIS phase was forked from. Resolving
// repo.git_main_branch unconditionally meant that under a milestone the
// command accepted being on main and hard-reset it — a branch with nothing to
// do with the phase being recovered.
func TestShipRecoverGuardsTheRecordedBase(t *testing.T) {
	dir, _ := recoverFixture(t)

	mustWrite(t, filepath.Join(dir, ".dross/phases/01-x/changes.json"),
		`{"phase":"01-x","base":"milestone/v0.9"}`)
	mustGit(t, dir, "add", ".dross/phases/01-x/changes.json")
	mustGit(t, dir, "commit", "-q", "-m", "chore(dross): record milestone base")

	// On main — which is NOT this phase's base.
	err := runCmd(t, Ship(), "recover")
	if err == nil {
		t.Fatal("expected a refusal: main is not the recorded base")
	}
	if !strings.Contains(err.Error(), "must be on milestone/v0.9") {
		t.Errorf("guard should name the recorded base: %v", err)
	}
}

// TestShipRecoverFallsBackToMainWithNoRecord keeps the legacy one-shot working:
// the repos this command exists for predate the base record, and their phase
// work really did live on main.
func TestShipRecoverFallsBackToMainWithNoRecord(t *testing.T) {
	dir, _ := recoverFixture(t)

	if err := os.Remove(filepath.Join(dir, ".dross/phases/01-x/changes.json")); err != nil {
		t.Fatal(err)
	}
	mustGit(t, dir, "add", "-A")
	mustGit(t, dir, "commit", "-q", "-m", "chore: drop the record")

	// The guard resolves main (we're on it) and the reset follows suit.
	if err := runCmd(t, Ship(), "recover"); err != nil {
		t.Fatalf("no-record recovery should fall back to git_main_branch: %v", err)
	}
	if cur := mustGit(t, dir, "symbolic-ref", "--short", "HEAD"); cur != "main" {
		t.Errorf("HEAD = %q, want main", cur)
	}
	if ahead := mustGit(t, dir, "rev-list", "origin/main..main"); ahead != "" {
		t.Errorf("the restore commit should be pushed, got ahead: %q", ahead)
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

// inSyncFixture builds the healed steady state: origin/main already carries
// the full .dross/ tree (the linguist-generated era where .dross/ ships on
// main), and local main is at origin. Recovery here must be a clean no-op.
func inSyncFixture(t *testing.T) string {
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

	root := filepath.Join(dir, ".dross")
	st, _ := state.Load(filepath.Join(root, state.File))
	st.CurrentPhase = "01-x"
	if err := st.Save(filepath.Join(root, state.File)); err != nil {
		t.Fatal(err)
	}

	// Commit the full tree — .dross/ artefacts included — and push so
	// origin/main == local main with the .dross/ tree present on both.
	mustWrite(t, filepath.Join(dir, ".dross/phases/01-x/spec.toml"), `id = "01-x"`)
	mustWrite(t, filepath.Join(dir, "README.md"), "base\n")
	mustGit(t, dir, "add", ".")
	mustGit(t, dir, "commit", "-q", "-m", "chore: baseline with .dross")
	mustGit(t, dir, "push", "-q", "-u", "origin", "main")
	mustGit(t, dir, "fetch", "-q", "origin")
	return dir
}

// TestShipRecoverIdempotentNoOp (c-6): on an already-in-sync repo, recovery
// restores nothing new, so the delta gate must skip the commit entirely —
// HEAD stays level with origin/main. Drop the gate and the state.Touch would
// manufacture a phantom commit, pushing the count to 1.
func TestShipRecoverIdempotentNoOp(t *testing.T) {
	dir := inSyncFixture(t)

	if err := runCmd(t, Ship(), "recover"); err != nil {
		t.Fatalf("recover on in-sync repo should succeed: %v", err)
	}

	ahead := mustGit(t, dir, "rev-list", "--count", "origin/main..HEAD")
	if ahead != "0" {
		t.Errorf("expected 0 commits ahead of origin/main (clean no-op), got %s", ahead)
	}
}

// multiPhaseRecoverFixture mirrors recoverFixture but the pre-merge HEAD
// carries TWO phases' artefacts (01-x and 02-y), while origin/main's squash
// has neither — the setup that exposes a partial (current-phase-only) restore.
func multiPhaseRecoverFixture(t *testing.T) (string, string) {
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

	root := filepath.Join(dir, ".dross")
	st, _ := state.Load(filepath.Join(root, state.File))
	st.CurrentPhase = "02-y"
	if err := st.Save(filepath.Join(root, state.File)); err != nil {
		t.Fatal(err)
	}

	mustWrite(t, filepath.Join(dir, "README.md"), "base\n")
	mustGit(t, dir, "add", ".")
	mustGit(t, dir, "commit", "-q", "-m", "chore: baseline")
	mustGit(t, dir, "push", "-q", "-u", "origin", "main")
	baseSHA := mustGit(t, dir, "rev-parse", "HEAD")

	// Two phases' worth of artefacts accumulate on the phase commits.
	mustWrite(t, filepath.Join(dir, "src/a.ts"), "export const a = 1\n")
	mustWrite(t, filepath.Join(dir, ".dross/phases/01-x/spec.toml"), `id = "01-x"`)
	mustGit(t, dir, "add", ".")
	mustGit(t, dir, "commit", "-q", "-m", "feat: phase 01-x")

	mustWrite(t, filepath.Join(dir, "src/b.ts"), "export const b = 2\n")
	mustWrite(t, filepath.Join(dir, ".dross/phases/02-y/spec.toml"), `id = "02-y"`)
	mustGit(t, dir, "add", ".")
	mustGit(t, dir, "commit", "-q", "-m", "feat: phase 02-y")
	preMergeSHA := mustGit(t, dir, "rev-parse", "HEAD")

	// Squash on origin: source files only, no .dross/ phase artefacts.
	mustGit(t, dir, "checkout", "-q", "-b", "squash-sim", baseSHA)
	mustGit(t, dir, "checkout", preMergeSHA, "--", "src/")
	mustGit(t, dir, "add", "src/")
	mustGit(t, dir, "commit", "-q", "-m", "feat(squash): phases 01-x + 02-y")
	mustGit(t, dir, "push", "-q", "--force", "origin", "squash-sim:main")

	mustGit(t, dir, "checkout", "-q", "main")
	mustGit(t, dir, "branch", "-D", "squash-sim")
	mustGit(t, dir, "fetch", "-q", "origin")

	return dir, preMergeSHA
}

// TestRecoverRestoresAllPhases (c-2): recovery must restore the FULL
// cumulative .dross/ tree, not just the current phase's. A per-phase restore
// would drop 01-x (current_phase is 02-y) and fail this guard.
func TestRecoverRestoresAllPhases(t *testing.T) {
	dir, _ := multiPhaseRecoverFixture(t)

	if err := runCmd(t, Ship(), "recover"); err != nil {
		t.Fatalf("recover: %v", err)
	}

	headTree := mustGit(t, dir, "ls-tree", "-r", "--name-only", "HEAD")
	for _, want := range []string{
		".dross/phases/01-x/spec.toml",
		".dross/phases/02-y/spec.toml",
	} {
		if !strings.Contains(headTree, want) {
			t.Errorf("HEAD tree missing %s after recovery — partial restore regression:\n%s", want, headTree)
		}
	}
}

// The three tests below pin the state.json exclusions (c-3). Recovery restores
// a .dross/ tree from a pre-merge commit, and before the untrack that commit
// carried a copy of state.json — so the restore would overwrite the live,
// machine-local file with whatever history that commit held, and the `git add`
// that follows would put the file back in the index.

// stateExclusionFixture is recoverFixture plus the pre-untrack shape: the base
// commit on origin/main force-tracks a 2-entry state.json, and the live working
// copy holds N entries. Returns the repo dir and the live entry count.
func stateExclusionFixture(t *testing.T) (string, int) {
	t.Helper()
	dir, _ := recoverFixture(t)

	// The stale, TRACKED copy lives on a side branch so local main never tracks
	// state.json — recovery's `reset --hard origin/main` would otherwise delete
	// the live file before the restore step even runs. origin/main stays as
	// recoverFixture left it (the squash, missing the phase artefacts), so the
	// run has a real delta rather than hitting the in-sync early return.
	stPath := filepath.Join(dir, ".dross", state.File)
	mustGit(t, dir, "checkout", "-q", "-b", "pre-untrack")
	stale := state.New()
	stale.Touch("ancient one")
	stale.Touch("ancient two")
	if err := stale.Save(stPath); err != nil {
		t.Fatal(err)
	}
	mustGit(t, dir, "add", "-f", ".dross/"+state.File)
	mustGit(t, dir, "commit", "-q", "-m", "chore(dross): pre-untrack state copy")
	preSHA := mustGit(t, dir, "rev-parse", "HEAD")

	// Back on main, where git has just removed the file it does not track. The
	// live copy is everything the machine accumulated since.
	mustGit(t, dir, "checkout", "-q", "main")
	live := state.New()
	live.CurrentPhase = "01-x"
	for i := 0; i < 40; i++ {
		live.Touch("live entry")
	}
	if err := live.Save(stPath); err != nil {
		t.Fatal(err)
	}

	// Point recovery at the pre-untrack SHA: that commit is the one carrying
	// the stale copy, and restoring from it is the case under test.
	t.Setenv("DROSS_TEST_PRE_MERGE_SHA", preSHA)
	return dir, len(live.History)
}

// TestRecoveryDoesNotRestoreStateJSON is deliberately run against the IN-SYNC
// path. On the delta path recovery reloads and re-saves state after the
// restore, which would paper over a clobber; the early return writes nothing,
// so the file on disk is exactly what the checkout left. That is also the path
// that got materially more reachable once state.json stopped guaranteeing a
// delta.
func TestRecoveryDoesNotRestoreStateJSON(t *testing.T) {
	dir, want := stateExclusionFixture(t)
	sha := os.Getenv("DROSS_TEST_PRE_MERGE_SHA")

	// Give origin/main the full .dross/ tree so there is nothing to restore.
	mustGit(t, dir, "push", "-q", "--force", "origin", "main")
	mustGit(t, dir, "fetch", "-q", "origin")
	headBefore := mustGit(t, dir, "rev-parse", "HEAD")

	var out string
	if err := runCmdCapturing(t, &out, Ship(), "recover", "--pre-merge-sha", sha); err != nil {
		t.Fatalf("recover: %v", err)
	}
	if !strings.Contains(out, "already in sync") {
		t.Fatalf("precondition: this run should hit the in-sync early return:\n%s", out)
	}
	if now := mustGit(t, dir, "rev-parse", "HEAD"); now != headBefore {
		t.Errorf("a no-op recovery wrote a commit: %s -> %s", headBefore, now)
	}

	s, err := state.Load(filepath.Join(dir, ".dross", state.File))
	if err != nil {
		t.Fatal(err)
	}
	if len(s.History) != want {
		t.Errorf("the live state was overwritten: history is %d entries, live had %d", len(s.History), want)
	}
	for _, a := range s.History {
		if strings.HasPrefix(a.Action, "ancient") {
			t.Fatalf("the stale copy from %s was restored over the live file: %+v", short(sha), s.History)
		}
	}
}

func TestRecoveryDoesNotRetrackStateJSON(t *testing.T) {
	dir, _ := stateExclusionFixture(t)
	sha := os.Getenv("DROSS_TEST_PRE_MERGE_SHA")

	if err := runCmd(t, Ship(), "recover", "--pre-merge-sha", sha); err != nil {
		t.Fatalf("recover: %v", err)
	}

	if tracked := mustGit(t, dir, "ls-files", ".dross/"+state.File); tracked != "" {
		t.Errorf("recovery re-tracked state.json: %q", tracked)
	}
	if names := mustGit(t, dir, "show", "--name-only", "--format=", "HEAD"); strings.Contains(names, state.File) {
		t.Errorf("the recovery commit lists state.json:\n%s", names)
	}
}

func TestRecoveryStillRestoresPhaseArtifacts(t *testing.T) {
	dir, _ := stateExclusionFixture(t)
	sha := os.Getenv("DROSS_TEST_PRE_MERGE_SHA")

	if err := runCmd(t, Ship(), "recover", "--pre-merge-sha", sha); err != nil {
		t.Fatalf("recover: %v", err)
	}

	// The exclusion must be exactly one path wide.
	headTree := mustGit(t, dir, "ls-tree", "-r", "--name-only", "HEAD")
	for _, want := range []string{".dross/phases/01-x/spec.toml", ".dross/phases/01-x/changes.json"} {
		if !strings.Contains(headTree, want) {
			t.Errorf("the exclusion is scoped too widely — HEAD tree missing %s:\n%s", want, headTree)
		}
	}
}
