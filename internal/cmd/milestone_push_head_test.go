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

// milestonePushHeadUnpushedFixture is milestonePushHeadFixture with the
// milestone branch created locally but never pushed, so origin has no
// refs/remotes/origin/<branch> and pushMilestoneHeadIfAhead reaches its
// no-upstream `push -u` arm — unreachable from the pushed fixture.
func milestonePushHeadUnpushedFixture(t *testing.T) (string, *msPRCapture, string) {
	t.Helper()
	dir, cap := milestoneOpenFixture(t)
	branch := "milestone/v0.9"
	mustGit(t, dir, "push", "-q", "-u", "origin", "main")
	mustGit(t, dir, "branch", branch)
	mustGit(t, dir, "checkout", "-q", branch)
	mustGit(t, dir, "fetch", "-q", "origin")
	return dir, cap, branch
}

// c-1: a milestone head with no upstream whose `push -u` is refused is a hard
// error naming the branch, reports pushed=false, and opens no integration PR.
//
// Two observations on one fixture, because neither alone is the criterion: the
// direct call is the only place pushed= is observable at all, and cap.created
// is only meaningful through the command (from a bare function call it is 0
// whatever happens, which asserts nothing).
//
// The cap.created==0 half is load-bearing ONLY because nothing in the open path
// pushes before pushMilestoneHeadIfAhead (milestone.go:261), so with the
// pre-receive hook installed the head push is the first thing origin refuses.
// If a push is ever added earlier in the open path, this test starts passing
// vacuously — the command would fail on that earlier push instead.
func TestMilestonePushHeadNoUpstreamRefusedPushIsHardError(t *testing.T) {
	dir, cap, branch := milestonePushHeadUnpushedFixture(t)
	// AFTER the fixture's own pushes — the hook refuses everything, including
	// the main push the fixture needs to succeed.
	rejectPushes(t, originOf(t, dir))

	pushed, _, err := pushMilestoneHeadIfAhead(dir, branch)
	if err == nil {
		t.Fatalf("a refused no-upstream push must be a hard error (pushed=%v)", pushed)
	}
	if pushed {
		t.Error("pushed=true reported for a push git refused")
	}
	if !strings.Contains(err.Error(), branch) {
		t.Errorf("error %q does not name the branch %q", err, branch)
	}

	cerr := runCmd(t, Milestone(), "complete", "v0.9")
	if cerr == nil {
		t.Fatal("`milestone complete` must fail when the head push is refused")
	}
	if !strings.Contains(cerr.Error(), branch) {
		t.Errorf("complete failed for some other reason than the head push: %v", cerr)
	}
	if cap.created != 0 {
		t.Errorf("no PR may be opened when the head push failed, created=%d", cap.created)
	}
}

// c-2: the ahead-push failure names the exact number of commits that would be
// lost at --finalize. Asserted as the literal phrase with the count in it, not
// as a bare digit — git's combined output is appended to the message and would
// satisfy a substring search for "2" on its own.
func TestMilestonePushHeadRefusedAheadPushNamesCommitCount(t *testing.T) {
	dir, _, branch := milestonePushHeadFixture(t)
	for _, name := range []string{"one.txt", "two.txt"} {
		mustWrite(t, filepath.Join(dir, name), "local only\n")
		mustGit(t, dir, "add", name)
		mustGit(t, dir, "commit", "-q", "-m", "local "+name)
	}
	rejectPushes(t, originOf(t, dir))

	pushed, _, err := pushMilestoneHeadIfAhead(dir, branch)
	if err == nil {
		t.Fatalf("a refused ahead push must be a hard error (pushed=%v)", pushed)
	}
	if pushed {
		t.Error("pushed=true reported for a push git refused")
	}
	if want := "push of 2 local commit(s)"; !strings.Contains(err.Error(), want) {
		t.Errorf("error %q does not carry %q — the count of commits at risk", err, want)
	}
	if !strings.Contains(err.Error(), branch) {
		t.Errorf("error %q does not name the branch %q", err, branch)
	}
}

// c-5: a failed `git fetch origin` is a hard error naming the fetch, and
// nothing is pushed. Without this the run would proceed to an ahead/behind
// comparison computed against a stale refs/remotes/origin/<branch> — which,
// for an already-published branch, reports "not ahead" and silently skips the
// push the criterion exists to guarantee.
func TestMilestonePushHeadFetchFailureIsHardError(t *testing.T) {
	dir, _, branch := milestonePushHeadFixture(t)
	origin := originOf(t, dir)
	before := mustGit(t, origin, "rev-parse", branch)

	mustWrite(t, filepath.Join(dir, "local.txt"), "local only\n")
	mustGit(t, dir, "add", "local.txt")
	mustGit(t, dir, "commit", "-q", "-m", "local work")

	// Repoint origin at a path that does not exist, so the fetch fails before
	// any comparison can be made. The real bare origin is still on disk and is
	// what the assertions below read.
	mustGit(t, dir, "remote", "set-url", "origin", filepath.Join(t.TempDir(), "gone.git"))

	pushed, _, err := pushMilestoneHeadIfAhead(dir, branch)
	if err == nil {
		t.Fatalf("a failed fetch must be a hard error (pushed=%v)", pushed)
	}
	if pushed {
		t.Error("pushed=true reported for a run that never got past the fetch")
	}
	if !strings.Contains(err.Error(), "fetch") {
		t.Errorf("error %q does not name the fetch as the cause", err)
	}
	if after := mustGit(t, origin, "rev-parse", branch); after != before {
		t.Errorf("origin %s moved from %s to %s on a failed fetch", branch, before, after)
	}
}
