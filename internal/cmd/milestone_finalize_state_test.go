package cmd

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/milestone"
)

// finalizeRepo is staleRepo plus a .dross root: a repo with a bare origin, one
// baseline commit pushed to main, and somewhere to write milestone tomls.
// Returns (repoDir, drossRoot).
func finalizeRepo(t *testing.T) (string, string) {
	t.Helper()
	dir := staleRepo(t)
	root := filepath.Join(dir, ".dross")
	return dir, root
}

// writeMilestoneToml writes .dross/milestones/<version>.toml at the given
// status, with base recorded when non-empty.
func writeMilestoneToml(t *testing.T, root, version, status, base string) {
	t.Helper()
	m := &milestone.Milestone{}
	m.Milestone.Version = version
	m.Milestone.Status = status
	m.Milestone.Base = base
	if err := m.Save(milestone.FilePath(root, version)); err != nil {
		t.Fatalf("save milestone %s: %v", version, err)
	}
}

// pushMilestoneBranch cuts milestone/<version> from the current branch, puts one
// commit on it and pushes it to origin.
func pushMilestoneBranch(t *testing.T, dir, version string) {
	t.Helper()
	branch := "milestone/" + version
	mustGit(t, dir, "checkout", "-q", "-b", branch)
	commitOn(t, dir, branch, version+".txt", version+"\n", "feat: "+version)
	mustGit(t, dir, "push", "-q", "-u", "origin", branch)
}

// TestClassifyAlreadyFinalizedNeedsNoBranch is the case c-2 exists for: after a
// successful finalize the branch is gone from both local and origin, so git has
// no evidence left. The toml's status="complete" is the only marker that
// survives, and it has to be read before any ancestry question is asked
// (locked already_finalized_evidence).
func TestClassifyAlreadyFinalizedNeedsNoBranch(t *testing.T) {
	dir, root := finalizeRepo(t)
	writeMilestoneToml(t, root, "v1.0", "complete", "")

	// Precondition: the branch exists nowhere — not locally, not on origin.
	// Every git-derived answer here is "no such ref".
	if gitRefExists(dir, "refs/heads/milestone/v1.0") || gitRefExists(dir, "refs/remotes/origin/milestone/v1.0") {
		t.Fatal("precondition: milestone/v1.0 should not exist in this fixture")
	}

	got, err := classifyFinalize(root, dir, "main", "milestone/v1.0", "v1.0")
	if err != nil {
		t.Fatal(err)
	}
	if got.State != finalizeAlreadyDone {
		t.Fatalf("state = %q, want %q (message: %s)", got.State, finalizeAlreadyDone, got.Message)
	}
	if !strings.Contains(got.Message, "already finalized") {
		t.Errorf("message %q does not say already finalized", got.Message)
	}
	if strings.Contains(got.Message, "is not merged into") {
		t.Errorf("already-finalized message reuses the unmerged refusal: %q", got.Message)
	}
}

// TestClassifyBranchGoneIsNotUnmerged is c-3. A branch deleted outside dross,
// with the milestone still active, used to hit the merge guard and come back as
// "not merged into origin/main yet" — a claim about a branch that is not there
// to be merged.
func TestClassifyBranchGoneIsNotUnmerged(t *testing.T) {
	dir, root := finalizeRepo(t)
	writeMilestoneToml(t, root, "v1.0", "active", "")

	got, err := classifyFinalize(root, dir, "main", "milestone/v1.0", "v1.0")
	if err != nil {
		t.Fatal(err)
	}
	if got.State != finalizeBranchGone {
		t.Fatalf("state = %q, want %q (message: %s)", got.State, finalizeBranchGone, got.Message)
	}
	if !strings.Contains(got.Message, "gone") {
		t.Errorf("message %q does not say the branch is gone", got.Message)
	}
	if strings.Contains(got.Message, "is not merged into") {
		t.Errorf("branch-gone message still carries the unmerged refusal: %q", got.Message)
	}
}

// TestClassifyBranchGoneNamesTheRemedy keeps the dead end from being a dead
// end: the message points at the one command that records the milestone as
// finished, and that command's path really is settable.
func TestClassifyBranchGoneNamesTheRemedy(t *testing.T) {
	dir, root := finalizeRepo(t)
	writeMilestoneToml(t, root, "v1.0", "active", "")

	got, err := classifyFinalize(root, dir, "main", "milestone/v1.0", "v1.0")
	if err != nil {
		t.Fatal(err)
	}
	if want := "dross milestone set v1.0 status complete"; !strings.Contains(got.Message, want) {
		t.Errorf("message %q does not name the remedy %q", got.Message, want)
	}
	// The remedy is only a remedy if the path it names is writable.
	var settable bool
	for _, p := range milestoneSettablePaths {
		if p == "milestone.status" {
			settable = true
		}
	}
	if !settable {
		t.Error("milestone.status is not in milestoneSettablePaths — the branch-gone remedy names a path `milestone set` refuses")
	}
}

// TestClassifyUnmergedKeepsTheRefusal pins the case the original guard was
// written for. Splitting off already-finalized and branch-gone must not soften
// the one refusal that protects unmerged work.
func TestClassifyUnmergedKeepsTheRefusal(t *testing.T) {
	dir, root := finalizeRepo(t)
	writeMilestoneToml(t, root, "v1.0", "active", "")
	pushMilestoneBranch(t, dir, "v1.0")
	mustGit(t, dir, "checkout", "-q", "main")

	got, err := classifyFinalize(root, dir, "main", "milestone/v1.0", "v1.0")
	if err != nil {
		t.Fatal(err)
	}
	if got.State != finalizeUnmerged {
		t.Fatalf("state = %q, want %q (message: %s)", got.State, finalizeUnmerged, got.Message)
	}
	if !strings.Contains(got.Message, "is not merged into origin/") {
		t.Errorf("message %q lost the unmerged refusal", got.Message)
	}
	if got.Target != "main" {
		t.Errorf("target = %q, want main", got.Target)
	}
}

// TestClassifyMergedIntoMain is the ordinary close: the milestone PR merged,
// origin/main contains the branch, teardown is safe and local main can advance.
func TestClassifyMergedIntoMain(t *testing.T) {
	dir, root := finalizeRepo(t)
	writeMilestoneToml(t, root, "v1.0", "active", "")
	pushMilestoneBranch(t, dir, "v1.0")
	mustGit(t, dir, "checkout", "-q", "main")
	mustGit(t, dir, "merge", "-q", "--no-ff", "-m", "merge: v1.0", "milestone/v1.0")
	mustGit(t, dir, "push", "-q", "origin", "main")
	mustGit(t, dir, "fetch", "-q", "origin")

	got, err := classifyFinalize(root, dir, "main", "milestone/v1.0", "v1.0")
	if err != nil {
		t.Fatal(err)
	}
	if got.State != finalizeMerged {
		t.Fatalf("state = %q, want %q (message: %s)", got.State, finalizeMerged, got.Message)
	}
	if got.Target != "main" {
		t.Errorf("target = %q, want main", got.Target)
	}
	if !got.MergedIntoMain {
		t.Error("MergedIntoMain = false for a milestone merged into origin/main")
	}
}

// TestClassifyStackedChildMergedIntoBase is locked stacked_child_status: a child
// merged into the parent it was cut from is merged, even though origin/main has
// never seen it. Target names the parent, and MergedIntoMain stays false so the
// caller does not fast-forward main off the back of it.
func TestClassifyStackedChildMergedIntoBase(t *testing.T) {
	dir, root := finalizeRepo(t)

	// Parent milestone branch, pushed and left unmerged into main.
	pushMilestoneBranch(t, dir, "v1.0")

	// Child cut from the parent, merged back into it — main never advances.
	mustGit(t, dir, "checkout", "-q", "-b", "milestone/v1.1")
	commitOn(t, dir, "milestone/v1.1", "child.txt", "child\n", "feat: child")
	mustGit(t, dir, "push", "-q", "-u", "origin", "milestone/v1.1")
	mustGit(t, dir, "checkout", "-q", "milestone/v1.0")
	mustGit(t, dir, "merge", "-q", "--no-ff", "-m", "merge: v1.1 into v1.0", "milestone/v1.1")
	mustGit(t, dir, "push", "-q", "origin", "milestone/v1.0")
	mustGit(t, dir, "checkout", "-q", "main")
	mustGit(t, dir, "fetch", "-q", "origin")

	writeMilestoneToml(t, root, "v1.1", "active", "milestone/v1.0")

	got, err := classifyFinalize(root, dir, "main", "milestone/v1.1", "v1.1")
	if err != nil {
		t.Fatal(err)
	}
	if got.State != finalizeMerged {
		t.Fatalf("state = %q, want %q (message: %s)", got.State, finalizeMerged, got.Message)
	}
	if got.Target != "milestone/v1.0" {
		t.Errorf("target = %q, want the recorded base milestone/v1.0", got.Target)
	}
	if got.MergedIntoMain {
		t.Error("MergedIntoMain = true, but origin/main never saw this child")
	}
}
