package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/milestone"
)

// milestoneStatusOf reads [milestone].status straight off disk.
func milestoneStatusOf(t *testing.T, dir, version string) string {
	t.Helper()
	m, err := milestone.Load(milestone.FilePath(filepath.Join(dir, ".dross"), version))
	if err != nil {
		t.Fatalf("load milestone %s: %v", version, err)
	}
	return m.Milestone.Status
}

func originOf(t *testing.T, dir string) string {
	t.Helper()
	return mustGit(t, dir, "remote", "get-url", "origin")
}

// rejectPushes installs a pre-receive hook on the bare origin that refuses
// everything, so the remote branch delete at the end of finalize fails the way
// a protected branch would.
func rejectPushes(t *testing.T, origin string) {
	t.Helper()
	hook := filepath.Join(origin, "hooks", "pre-receive")
	if err := os.MkdirAll(filepath.Dir(hook), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(hook, []byte("#!/bin/sh\necho 'protected branch' >&2\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
}

// finalizeFixture is milestoneFinalizeFixture plus a committed milestone toml.
//
// The toml has to be committed BEFORE the branch is cut and the merge simulated,
// or it is either untracked dirt (which finalize refuses on) or a local-only
// commit that leaves main diverged from the origin it is about to fast-forward
// from. Seeding it on main first puts it on every branch that follows.
func finalizeFixture(t *testing.T, merged bool, status string) (string, string) {
	t.Helper()
	version := "v0.9"
	dir, _ := setupMilestoneRepo(t)
	writeMilestoneToml(t, filepath.Join(dir, ".dross"), version, status, "")
	mustGit(t, dir, "add", ".dross")
	mustGit(t, dir, "commit", "-q", "-m", "chore(dross): scope "+version)
	mustGit(t, dir, "push", "-q", "-u", "origin", "main")

	mustGit(t, dir, "branch", "milestone/"+version)
	mustGit(t, dir, "checkout", "-q", "milestone/"+version)
	mustWrite(t, filepath.Join(dir, "phasework.txt"), "x\n")
	mustGit(t, dir, "add", ".")
	mustGit(t, dir, "commit", "-q", "-m", "phase work")
	mustGit(t, dir, "push", "-q", "-u", "origin", "milestone/"+version)

	if merged {
		// The milestone->main merge lands on origin only, leaving local main
		// behind for finalize to fast-forward.
		mustGit(t, dir, "checkout", "-q", "-b", "mergetmp", "main")
		mustGit(t, dir, "merge", "--no-ff", "-q", "-m", "merge "+version, "milestone/"+version)
		mustGit(t, dir, "push", "-q", "origin", "mergetmp:main")
		mustGit(t, dir, "checkout", "-q", "main")
		mustGit(t, dir, "branch", "-D", "mergetmp")
	} else {
		mustGit(t, dir, "checkout", "-q", "main")
	}
	mustGit(t, dir, "fetch", "-q", "origin")
	return dir, version
}

// TestFinalizeWritesCompleteStatus is c-1. Finalizing is the moment a milestone
// is done; leaving it `active` on disk means every later reader — doctor's stale
// check, milestone progress, the slash command's dispatch — is looking at a
// milestone that says it is still in flight.
func TestFinalizeWritesCompleteStatus(t *testing.T) {
	dir, version := finalizeFixture(t, true, "active")

	if err := runCmd(t, Milestone(), "complete", version, "--finalize"); err != nil {
		t.Fatalf("finalize: %v", err)
	}
	if got := milestoneStatusOf(t, dir, version); got != milestoneStatusComplete {
		t.Errorf("status = %q, want %q", got, milestoneStatusComplete)
	}
}

// TestFinalizeSecondRunSaysAlreadyFinalized is c-2. The first finalize deletes
// the branch local and remote, so a re-run has no ref to answer a merge question
// with — and the old code answered it anyway, with a refusal claiming the PR had
// not merged.
func TestFinalizeSecondRunSaysAlreadyFinalized(t *testing.T) {
	dir, version := finalizeFixture(t, true, "active")

	if err := runCmd(t, Milestone(), "complete", version, "--finalize"); err != nil {
		t.Fatalf("first finalize: %v", err)
	}

	var out string
	err := runCmdCapturing(t, &out, Milestone(), "complete", version, "--finalize")
	if err != nil {
		t.Fatalf("a second finalize must exit 0, got: %v", err)
	}
	if !strings.Contains(out, "already finalized") {
		t.Errorf("output should say the milestone is already finalized:\n%s", out)
	}
	if strings.Contains(out, "is not merged into") {
		t.Errorf("the re-run still emits the unmerged refusal:\n%s", out)
	}
	// Nothing was there to delete, and nothing was attempted.
	if b := mustGit(t, dir, "branch", "--list", "milestone/"+version); b != "" {
		t.Errorf("a re-run resurrected a branch: %q", b)
	}
}

// TestFinalizeBranchGoneIsNotReportedAsUnmerged is c-3 at the command level. A
// classifier that gets this right is no use if milestoneFinalize prints its own
// old refusal over the top.
func TestFinalizeBranchGoneIsNotReportedAsUnmerged(t *testing.T) {
	dir, version := finalizeFixture(t, true, "active")

	// Somebody deleted the milestone branch by hand, on origin and locally,
	// without recording the milestone as finished.
	mustGit(t, dir, "push", "-q", "--delete", "origin", "milestone/"+version)
	mustGit(t, dir, "branch", "-D", "milestone/"+version)
	mustGit(t, dir, "fetch", "-q", "--prune", "origin")

	var out string
	err := runCmdCapturing(t, &out, Milestone(), "complete", version, "--finalize")
	combined := out
	if err != nil {
		combined += "\n" + err.Error()
	}
	if !strings.Contains(combined, "gone") {
		t.Errorf("finalize should say the branch is gone:\n%s", combined)
	}
	if strings.Contains(combined, "is not merged into") {
		t.Errorf("a missing branch is still reported as unmerged:\n%s", combined)
	}
}

// TestFinalizeStackedChildFlipsStatus is locked stacked_child_status: a child
// merged into its parent is done even though main never moved. Leaving it
// `active` would sit it there forever if the parent is never finalized.
func TestFinalizeStackedChildFlipsStatus(t *testing.T) {
	dir, _ := setupMilestoneRepo(t)
	// Seeded and committed on main before anything branches, so the toml is
	// present on every branch finalize might land HEAD on.
	writeMilestoneToml(t, filepath.Join(dir, ".dross"), "v1.1", "active", "milestone/v1.0")
	mustGit(t, dir, "add", ".dross")
	mustGit(t, dir, "commit", "-q", "-m", "chore(dross): scope v1.1")
	mustGit(t, dir, "push", "-q", "-u", "origin", "main")

	// Parent, pushed and deliberately never merged into main.
	mustGit(t, dir, "checkout", "-q", "-b", "milestone/v1.0")
	mustWrite(t, filepath.Join(dir, "parent.txt"), "p\n")
	mustGit(t, dir, "add", ".")
	mustGit(t, dir, "commit", "-q", "-m", "feat: parent")
	mustGit(t, dir, "push", "-q", "-u", "origin", "milestone/v1.0")

	// Child cut from the parent and merged back into it.
	mustGit(t, dir, "checkout", "-q", "-b", "milestone/v1.1")
	mustWrite(t, filepath.Join(dir, "child.txt"), "c\n")
	mustGit(t, dir, "add", ".")
	mustGit(t, dir, "commit", "-q", "-m", "feat: child")
	mustGit(t, dir, "push", "-q", "-u", "origin", "milestone/v1.1")
	mustGit(t, dir, "checkout", "-q", "milestone/v1.0")
	mustGit(t, dir, "merge", "-q", "--no-ff", "-m", "merge: v1.1", "milestone/v1.1")
	mustGit(t, dir, "push", "-q", "origin", "milestone/v1.0")
	mustGit(t, dir, "checkout", "-q", "main")
	mustGit(t, dir, "fetch", "-q", "origin")

	mainBefore := mustGit(t, dir, "rev-parse", "main")

	if err := runCmd(t, Milestone(), "complete", "v1.1", "--finalize"); err != nil {
		t.Fatalf("finalize a stacked child: %v", err)
	}
	if got := milestoneStatusOf(t, dir, "v1.1"); got != milestoneStatusComplete {
		t.Errorf("status = %q, want %q", got, milestoneStatusComplete)
	}
	if now := mustGit(t, dir, "rev-parse", "main"); now != mainBefore {
		t.Errorf("local main moved (%s -> %s) for a child that never reached main", mainBefore, now)
	}
}

// TestFinalizeRecordsStatusBeforeTeardown pins the ordering, which is the whole
// reason the write sits where it does. If the delete fails after the marker is
// written the run is recoverable — the re-run says already-finalized. If the
// marker were written after, a half-finished finalize would leave the branch
// gone and the status active, and nothing could tell that state from an
// unmerged milestone ever again.
func TestFinalizeRecordsStatusBeforeTeardown(t *testing.T) {
	dir, version := finalizeFixture(t, true, "active")
	rejectPushes(t, originOf(t, dir))

	err := runCmd(t, Milestone(), "complete", version, "--finalize")
	if err == nil {
		t.Fatal("a rejected remote delete must surface as an error")
	}
	if !strings.Contains(err.Error(), "push origin --delete") && !strings.Contains(err.Error(), "--delete") {
		t.Errorf("the error should name the failed remote delete: %v", err)
	}
	if got := milestoneStatusOf(t, dir, version); got != milestoneStatusComplete {
		t.Fatalf("status = %q after a failed teardown, want %q — the marker must precede the deletes", got, milestoneStatusComplete)
	}

	// And the run is recoverable: the re-run knows it is done.
	var out string
	if err := runCmdCapturing(t, &out, Milestone(), "complete", version, "--finalize"); err != nil {
		t.Fatalf("the re-run after a failed teardown must exit 0, got: %v", err)
	}
	if !strings.Contains(out, "already finalized") {
		t.Errorf("re-run should report already-finalized:\n%s", out)
	}
}

// TestFinalizeReportsLeftoverBranchWithoutDeleting: the already-finalized arm
// answers a question. It names the leftover and hands over `dross milestone
// prune` rather than deleting a ref behind a command the user expected to be a
// no-op (locked prune_surface).
func TestFinalizeReportsLeftoverBranchWithoutDeleting(t *testing.T) {
	dir, version := finalizeFixture(t, true, milestoneStatusComplete)

	var out string
	if err := runCmdCapturing(t, &out, Milestone(), "complete", version, "--finalize"); err != nil {
		t.Fatalf("already-finalized must exit 0: %v", err)
	}
	if !strings.Contains(out, "milestone/"+version) {
		t.Errorf("the leftover branch should be named:\n%s", out)
	}
	if !strings.Contains(out, "dross milestone prune") {
		t.Errorf("the leftover report should point at prune:\n%s", out)
	}
	// Re-checked against git, not against the narration.
	if err := gitNoOut(dir, "rev-parse", "--verify", "--quiet", "refs/heads/milestone/"+version); err != nil {
		t.Error("the already-finalized arm deleted the local branch — it must delete nothing")
	}
}

// TestOneUnmergedRefusalInCmd: the refusal now lives in the classifier. A copy
// left behind in milestoneFinalize would keep firing for the cases c-2 and c-3
// just split out of it, and the two texts would drift apart unnoticed.
func TestOneUnmergedRefusalInCmd(t *testing.T) {
	root := filepath.Join(repoRootFromTest(t), "internal", "cmd")
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	var hits []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			t.Fatal(err)
		}
		if n := strings.Count(string(b), "is not merged into"); n > 0 {
			for i := 0; i < n; i++ {
				hits = append(hits, name)
			}
		}
	}
	if len(hits) != 1 {
		t.Errorf("the unmerged refusal appears %d times in internal/cmd (%v), want exactly 1", len(hits), hits)
	}
}
