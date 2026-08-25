package cmd

import (
	"path/filepath"
	"strings"
	"testing"
)

// milestonePushHeadFixture extends milestoneOpenFixture with a real
// milestone/<version> branch that exists both locally and on origin, so a test
// can put the two out of sync and observe what `milestone complete` does about
// it before the PR is opened. Returns the repo dir, the PR capture and the
// branch name.
func milestonePushHeadFixture(t *testing.T) (string, *msPRCapture, string) {
	t.Helper()
	dir, cap := milestoneOpenFixture(t)
	branch := "milestone/v0.9"
	mustGit(t, dir, "push", "-q", "-u", "origin", "main")
	mustGit(t, dir, "branch", branch)
	mustGit(t, dir, "push", "-q", "-u", "origin", branch)
	mustGit(t, dir, "checkout", "-q", branch)
	mustGit(t, dir, "fetch", "-q", "origin")
	return dir, cap, branch
}

// A commit made locally on the milestone branch must reach origin BEFORE the
// integration PR is opened. The PR names a head branch the provider resolves
// on its own side, so an unpushed commit is simply not in the PR — and
// `--finalize` then deletes the branch holding it, orphaning the work. This is
// the v1.5 close-out incident: the version-sync commit was dropped exactly
// this way and survived only because its sha was still in the reflog.
func TestMilestoneCompletePushesLocalCommitsBeforeOpeningPR(t *testing.T) {
	dir, cap, branch := milestonePushHeadFixture(t)

	mustWrite(t, filepath.Join(dir, "late.txt"), "committed locally, never pushed\n")
	mustGit(t, dir, "add", "late.txt")
	mustGit(t, dir, "commit", "-q", "-m", "late work on the milestone branch")
	local := mustGit(t, dir, "rev-parse", branch)
	if remote := mustGit(t, dir, "rev-parse", "origin/"+branch); remote == local {
		t.Fatalf("fixture is not set up: origin already has the local commit (%s)", local)
	}

	if err := runCmd(t, Milestone(), "complete", "v0.9"); err != nil {
		t.Fatalf("complete: %v", err)
	}

	if cap.created != 1 {
		t.Fatalf("expected the PR to be opened, created=%d", cap.created)
	}
	if remote := mustGit(t, dir, "rev-parse", "origin/"+branch); remote != local {
		t.Errorf("origin/%s = %s; want %s — the PR was opened against content missing the local commit",
			branch, remote, local)
	}
}

// Already-published is a no-op: nothing to push, and the PR still opens.
func TestMilestoneCompleteNoOpsWhenHeadIsPublished(t *testing.T) {
	dir, cap, branch := milestonePushHeadFixture(t)
	before := mustGit(t, dir, "rev-parse", "origin/"+branch)

	if err := runCmd(t, Milestone(), "complete", "v0.9"); err != nil {
		t.Fatalf("complete: %v", err)
	}

	if cap.created != 1 {
		t.Errorf("expected the PR to be opened, created=%d", cap.created)
	}
	if after := mustGit(t, dir, "rev-parse", "origin/"+branch); after != before {
		t.Errorf("origin/%s moved from %s to %s on a no-op run", branch, before, after)
	}
}

// A genuinely diverged head — ahead AND behind origin — is refused by name,
// and no PR is opened. Force-pushing a shared integration branch to make a PR
// openable is never the right call; the user reconciles first.
func TestMilestoneCompleteRefusesDivergedHead(t *testing.T) {
	dir, cap, branch := milestonePushHeadFixture(t)

	// Advance origin/<branch> via a throwaway branch, so origin gains a commit
	// the local branch will never contain.
	mustGit(t, dir, "checkout", "-q", "-b", "divergetmp", branch)
	mustWrite(t, filepath.Join(dir, "theirs.txt"), "landed on origin\n")
	mustGit(t, dir, "add", "theirs.txt")
	mustGit(t, dir, "commit", "-q", "-m", "someone else's commit")
	mustGit(t, dir, "push", "-q", "origin", "divergetmp:"+branch)
	mustGit(t, dir, "checkout", "-q", branch)
	mustGit(t, dir, "branch", "-D", "divergetmp")

	// And commit locally, so the branch is ahead as well as behind.
	mustWrite(t, filepath.Join(dir, "mine.txt"), "only local\n")
	mustGit(t, dir, "add", "mine.txt")
	mustGit(t, dir, "commit", "-q", "-m", "my commit")
	mustGit(t, dir, "fetch", "-q", "origin")

	err := runCmd(t, Milestone(), "complete", "v0.9")
	if err == nil {
		t.Fatal("expected a refusal on a diverged milestone head, got nil")
	}
	low := strings.ToLower(err.Error())
	if !strings.Contains(low, branch) {
		t.Errorf("refusal should name the branch %q; got: %v", branch, err)
	}
	if !strings.Contains(low, "ahead") || !strings.Contains(low, "behind") {
		t.Errorf("refusal should say the branch is both ahead and behind; got: %v", err)
	}
	if cap.created != 0 {
		t.Errorf("no PR should be opened on a diverged head, created=%d", cap.created)
	}
}

// A machine holding no local copy of the milestone branch has nothing that
// could be missing from the PR — the push step must not invent one, and the
// PR still opens. This is the pre-existing milestoneOpenFixture shape, which
// every other open-mode test relies on.
func TestMilestoneCompleteNoLocalHeadStillOpens(t *testing.T) {
	dir, cap := milestoneOpenFixture(t)
	if b := mustGit(t, dir, "branch", "--list", "milestone/v0.9"); b != "" {
		t.Fatalf("fixture unexpectedly has a local milestone branch: %q", b)
	}
	if err := runCmd(t, Milestone(), "complete", "v0.9"); err != nil {
		t.Fatalf("complete: %v", err)
	}
	if cap.created != 1 {
		t.Errorf("expected the PR to be opened, created=%d", cap.created)
	}
}
