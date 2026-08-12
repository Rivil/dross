package cmd

import (
	"path/filepath"
	"strings"
	"testing"
)

// pruneFixture builds a dross repo on `main` with an origin remote and returns
// its directory. Branch shapes are layered on per test.
func pruneFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	remote := t.TempDir()
	mustGit(t, remote, "init", "-q", "--bare", "-b", "main")
	gitInit(t, dir, remote)
	chdir(t, dir)
	if err := runCmd(t, Init()); err != nil {
		t.Fatalf("init: %v", err)
	}
	mustWrite(t, filepath.Join(dir, "README.md"), "base\n")
	mustGit(t, dir, "add", ".")
	mustGit(t, dir, "commit", "-q", "-m", "chore: baseline")
	mustGit(t, dir, "push", "-q", "-u", "origin", "main")
	return dir
}

// seedCompleteMilestone records the milestone as finished and commits the
// record. Prune's detector reports only branches whose milestone says complete,
// and prune itself refuses on a dirty tree, so the toml has to be both present
// and committed.
func seedCompleteMilestone(t *testing.T, dir, version string) {
	t.Helper()
	completeMilestone(t, dir, version)
	mustGit(t, dir, "add", ".dross")
	mustGit(t, dir, "commit", "-q", "-m", "chore(dross): record "+version+" complete")
}

func branchExists(t *testing.T, dir, branch string) bool {
	t.Helper()
	return gitAllowFail(dir, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch)
}

func remoteHas(t *testing.T, dir, branch string) bool {
	t.Helper()
	return mustGit(t, dir, "ls-remote", "--heads", "origin", branch) != ""
}

// TestPruneDeletesOnlyStaleBranches is the whole safety property: the
// squash-merged branch goes, local and on origin, and the unmerged one is
// untouched. Acting beyond the detector's result destroys real work.
func TestPruneDeletesOnlyStaleBranches(t *testing.T) {
	dir := pruneFixture(t)

	// Stale: squash-merged onto main, and pushed.
	mustGit(t, dir, "checkout", "-q", "-b", "milestone/v1.0", "main")
	commitOn(t, dir, "milestone/v1.0", "a.txt", "a\n", "feat: a")
	mustGit(t, dir, "push", "-q", "origin", "milestone/v1.0")
	squashOnto(t, dir, "milestone/v1.0", "feat(squash): v1.0")
	pushMain(t, dir)
	seedCompleteMilestone(t, dir, "v1.0")

	// Live: one commit that is nowhere on main.
	mustGit(t, dir, "checkout", "-q", "-b", "milestone/v1.2", "main")
	commitOn(t, dir, "milestone/v1.2", "z.txt", "z\n", "feat: z")
	mustGit(t, dir, "push", "-q", "origin", "milestone/v1.2")
	mustGit(t, dir, "checkout", "-q", "main")

	var out string
	if err := runCmdCapturing(t, &out, Milestone(), "prune"); err != nil {
		t.Fatalf("prune: %v", err)
	}

	if branchExists(t, dir, "milestone/v1.0") {
		t.Error("the squash-merged branch should be gone locally")
	}
	if remoteHas(t, dir, "milestone/v1.0") {
		t.Error("the squash-merged branch should be gone on origin")
	}
	if !branchExists(t, dir, "milestone/v1.2") {
		t.Error("prune deleted a branch the detector did not name — local milestone/v1.2 is gone")
	}
	if !remoteHas(t, dir, "milestone/v1.2") {
		t.Error("prune deleted a branch the detector did not name — origin/milestone/v1.2 is gone")
	}
	if !strings.Contains(out, "milestone/v1.0") {
		t.Errorf("prune should name what it deleted:\n%s", out)
	}
}

// TestPruneRefusesOnCurrentHEAD: git cannot delete the branch you are standing
// on, and skipping it silently reports a prune that did not happen.
func TestPruneRefusesOnCurrentHEAD(t *testing.T) {
	dir := pruneFixture(t)
	// Seeded before the branch is cut, so milestone/v1.0 carries the record
	// too — prune is run from that branch here.
	seedCompleteMilestone(t, dir, "v1.0")
	mustGit(t, dir, "checkout", "-q", "-b", "milestone/v1.0", "main")
	commitOn(t, dir, "milestone/v1.0", "a.txt", "a\n", "feat: a")
	squashOnto(t, dir, "milestone/v1.0", "feat(squash): v1.0")
	pushMain(t, dir)
	mustGit(t, dir, "checkout", "-q", "milestone/v1.0")

	err := runCmd(t, Milestone(), "prune")
	if err == nil {
		t.Fatal("prune must refuse while HEAD is on a branch it would delete")
	}
	if !strings.Contains(err.Error(), "milestone/v1.0") {
		t.Errorf("the refusal should name the branch: %v", err)
	}
	if !branchExists(t, dir, "milestone/v1.0") {
		t.Error("a refused prune must delete nothing")
	}
}

// TestPruneLocalOnlyBranchSkipsRemote: an unconditional `push --delete` fails
// outright for a branch origin never had.
func TestPruneLocalOnlyBranchSkipsRemote(t *testing.T) {
	dir := pruneFixture(t)
	mustGit(t, dir, "checkout", "-q", "-b", "milestone/v1.0", "main")
	commitOn(t, dir, "milestone/v1.0", "a.txt", "a\n", "feat: a")
	squashOnto(t, dir, "milestone/v1.0", "feat(squash): v1.0")
	pushMain(t, dir)
	seedCompleteMilestone(t, dir, "v1.0")

	var out string
	if err := runCmdCapturing(t, &out, Milestone(), "prune"); err != nil {
		t.Fatalf("a local-only stale branch should prune cleanly: %v", err)
	}
	if branchExists(t, dir, "milestone/v1.0") {
		t.Error("the local branch should be gone")
	}
	if strings.Contains(out, "origin") {
		t.Errorf("nothing should have been deleted on origin:\n%s", out)
	}
}

// TestPruneNothingStaleIsCleanNoOp: the common case runs to exit 0 with an
// explicit "nothing to do", and a second run behaves exactly like the first.
func TestPruneNothingStaleIsCleanNoOp(t *testing.T) {
	dir := pruneFixture(t)
	mustGit(t, dir, "checkout", "-q", "-b", "milestone/v1.2", "main")
	commitOn(t, dir, "milestone/v1.2", "z.txt", "z\n", "feat: z")
	mustGit(t, dir, "checkout", "-q", "main")

	var first, second string
	if err := runCmdCapturing(t, &first, Milestone(), "prune"); err != nil {
		t.Fatalf("prune with nothing stale should exit 0: %v", err)
	}
	if !strings.Contains(first, "nothing to prune") {
		t.Errorf("prune should say so when there is nothing to do:\n%s", first)
	}
	if err := runCmdCapturing(t, &second, Milestone(), "prune"); err != nil {
		t.Fatalf("second prune: %v", err)
	}
	if first != second {
		t.Errorf("prune is not idempotent:\nfirst:\n%s\nsecond:\n%s", first, second)
	}
	if !branchExists(t, dir, "milestone/v1.2") {
		t.Error("a no-op prune deleted a live branch")
	}
}

// TestPruneRefusesDirtyTree: refs disappearing while uncommitted work sits in
// the tree is the worst pairing available. Bookkeeping-only .dross/ dirt is
// auto-committed as everywhere else; real code dirt refuses.
func TestPruneRefusesDirtyTree(t *testing.T) {
	dir := pruneFixture(t)
	mustGit(t, dir, "checkout", "-q", "-b", "milestone/v1.0", "main")
	commitOn(t, dir, "milestone/v1.0", "a.txt", "a\n", "feat: a")
	squashOnto(t, dir, "milestone/v1.0", "feat(squash): v1.0")
	pushMain(t, dir)
	seedCompleteMilestone(t, dir, "v1.0")
	mustWrite(t, filepath.Join(dir, "uncommitted.txt"), "dirt\n")

	err := runCmd(t, Milestone(), "prune")
	if err == nil {
		t.Fatal("prune must refuse on a dirty tree")
	}
	if !strings.Contains(err.Error(), "dirty") {
		t.Errorf("the refusal should name the dirty tree: %v", err)
	}
	if !branchExists(t, dir, "milestone/v1.0") {
		t.Error("a refused prune must delete nothing")
	}
}
