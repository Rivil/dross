package cmd

import (
	"path/filepath"
	"strings"
	"testing"
)

// One scripted lifecycle over a real repo, asserting the SEAM between the two
// halves of this phase: finalize writes [milestone].status = complete (t-4),
// and the stale-branch detector reads it (t-5).
//
// Neither task's own tests can see this handoff. t-4's never load the detector;
// t-5's never run finalize, they set the status by hand. So each could be
// individually green while the pair is broken — finalize writing a status
// nothing reads, or the detector gating on a status nothing writes.
//
// The four steps are one continuous run, in order, over the same repo. Split
// into independent tests they would each need to fabricate the state the
// previous step produced, which is exactly the fabrication this test exists to
// avoid.
func TestMilestoneLifecycleCloseSeam(t *testing.T) {
	version := "v1.0"
	msBranch := "milestone/" + version
	dir, _ := setupMilestoneRepo(t)
	root := filepath.Join(dir, ".dross")

	// An ACTIVE milestone, recorded and committed on main before anything
	// branches so every later checkout carries the record.
	writeMilestoneToml(t, root, version, "active", "")
	mustGit(t, dir, "add", ".dross")
	mustGit(t, dir, "commit", "-q", "-m", "chore(dross): scope "+version)
	mustGit(t, dir, "push", "-q", "-u", "origin", "main")

	// Milestone work, pushed, then merged into main ON ORIGIN — the state a
	// repo is in the moment the integration PR merges.
	mustGit(t, dir, "checkout", "-q", "-b", msBranch)
	mustWrite(t, filepath.Join(dir, "work.txt"), "work\n")
	mustGit(t, dir, "add", ".")
	mustGit(t, dir, "commit", "-q", "-m", "feat: milestone work")
	mustGit(t, dir, "push", "-q", "-u", "origin", msBranch)
	mustGit(t, dir, "checkout", "-q", "-b", "mergetmp", "main")
	mustGit(t, dir, "merge", "-q", "--no-ff", "-m", "merge "+version, msBranch)
	mustGit(t, dir, "push", "-q", "origin", "mergetmp:main")
	mustGit(t, dir, "checkout", "-q", "main")
	mustGit(t, dir, "branch", "-D", "mergetmp")
	mustGit(t, dir, "fetch", "-q", "origin")

	// --- Step 1: the active gate holds -----------------------------------
	//
	// The branch's work is demonstrably on origin/main, and the milestone is
	// still active. Reporting it here is what would let `dross milestone prune`
	// delete a live integration branch out from under the phases still forking
	// off it.
	got, err := staleMilestoneBranches(root, dir, "main")
	if err != nil {
		t.Fatal(err)
	}
	if b, ok := byName(got)[msBranch]; ok {
		t.Fatalf("step 1: an ACTIVE milestone's merged branch was reported stale: %+v", b)
	}

	// --- Step 2: finalize flips the status and tears the branch down ------
	if err := runCmd(t, Milestone(), "complete", version, "--finalize"); err != nil {
		t.Fatalf("step 2: finalize: %v", err)
	}
	if got := milestoneStatusOf(t, dir, version); got != milestoneStatusComplete {
		t.Fatalf("step 2: status = %q, want %q", got, milestoneStatusComplete)
	}
	for _, ref := range []string{"refs/heads/" + msBranch, "refs/remotes/origin/" + msBranch} {
		if gitRefExists(dir, ref) {
			t.Errorf("step 2: %s survived finalize", ref)
		}
	}

	// --- Step 3: the re-run is an answer, not a refusal --------------------
	//
	// Both refs are gone now, so git has nothing left to answer a merge
	// question with. The status written in step 2 is the only evidence, and it
	// is what keeps this from being the old "not merged into origin/main yet".
	var out string
	if err := runCmdCapturing(t, &out, Milestone(), "complete", version, "--finalize"); err != nil {
		t.Fatalf("step 3: a second finalize must exit 0, got: %v", err)
	}
	if !strings.Contains(out, "already finalized") {
		t.Errorf("step 3: re-run should report already-finalized:\n%s", out)
	}
	if strings.Contains(out, "is not merged into") {
		t.Errorf("step 3: re-run answered with the unmerged refusal:\n%s", out)
	}

	// --- Step 4: the gate now lets the detector through --------------------
	//
	// Recreate the leftover the gate is ultimately about — a milestone branch
	// still sitting in the repo after its milestone closed. This is the outcome
	// that reverting EITHER half destroys: without the status flip the branch
	// stays invisible forever, and without the gate it was visible while the
	// milestone was still live.
	mustGit(t, dir, "branch", msBranch, "main")

	got, err = staleMilestoneBranches(root, dir, "main")
	if err != nil {
		t.Fatal(err)
	}
	b, ok := byName(got)[msBranch]
	if !ok {
		t.Fatalf("step 4: a finalized milestone's leftover branch is prunable: %+v", got)
	}
	if b.Reason != reasonMerged {
		t.Errorf("step 4: reason = %q, want %q", b.Reason, reasonMerged)
	}
}
