package cmd

import (
	"path/filepath"
	"strings"
	"testing"
)

// This file reproduces the incident that motivated the base-truth work, end to
// end, at the level the user actually hit it.
//
// The shape: a phase forked off main, shipped and squash-merged onto
// origin/main — while current_milestone pointed at a milestone whose local
// milestone/<v> branch was still sitting in the repo, stale. Completion
// re-derived its reconcile branch from current_milestone, picked that stale
// milestone branch, and fast-forwarded it with a merge that had landed
// somewhere else entirely, then deleted the phase branch on the way out.
//
// Both tests below assert the same invariant from opposite ends: whatever
// happens, the milestone branch the phase never forked from is not moved, and
// the phase branch is not deleted unless the run genuinely completed.

// staleMilestoneFixture builds the incident state and returns the repo dir and
// the milestone version whose branch must not be touched.
//
//   - phase/auth forked from main, base recorded as "main"
//   - PR #42 recorded, squash-merged onto origin/main
//   - current_milestone = v9.9, with a stale local milestone/v9.9 branch that
//     origin also has — so a fast-forward of it would be observable on both
//   - HEAD on phase/auth, the post-ship position
func staleMilestoneFixture(t *testing.T) (string, string) {
	t.Helper()
	const version = "v9.9"

	dir, _ := completeFixture(t) // phase/auth off main, base "main", PR #42 merged onto origin/main

	// Push the phase branch: ship leaves it on origin, and whether completion
	// deletes it there is half of what these tests assert.
	mustGit(t, dir, "push", "-q", "origin", "phase/auth")

	// The stale milestone branch: cut from main's pre-squash baseline and
	// pushed, so it exists locally AND on origin and any movement is visible.
	mustGit(t, dir, "branch", "milestone/"+version, "main")
	mustGit(t, dir, "push", "-q", "origin", "milestone/"+version)
	mustGit(t, dir, "fetch", "-q", "origin")

	// Point state at it — the input the old inference read.
	if err := runCmd(t, State(), "set", "current_milestone", version); err != nil {
		t.Fatalf("state set current_milestone: %v", err)
	}
	mustGit(t, dir, "add", ".dross/state.json")
	mustGit(t, dir, "commit", "-q", "-m", "chore(dross): scope "+version)

	return dir, version
}

// assertMilestoneUntouched is the invariant both arms share: the branch the
// phase never forked from is byte-identical to where it started, locally and
// on origin.
func assertMilestoneUntouched(t *testing.T, dir, version, localBefore, originBefore string) {
	t.Helper()
	if got := mustGit(t, dir, "rev-parse", "milestone/"+version); got != localBefore {
		t.Errorf("local milestone/%s was moved: %s -> %s", version, localBefore, got)
	}
	if got := mustGit(t, dir, "rev-parse", "origin/milestone/"+version); got != originBefore {
		t.Errorf("origin/milestone/%s was moved: %s -> %s", version, originBefore, got)
	}
}

// TestStaleMilestoneIncidentCompletesAgainstRecordedBase is the success arm:
// with the base recorded, completion reconciles main — the branch the phase
// was actually forked from and merged into — and leaves the stale milestone
// branch alone.
func TestStaleMilestoneIncidentCompletesAgainstRecordedBase(t *testing.T) {
	dir, version := staleMilestoneFixture(t)
	stubPRMerged(t, true)

	msLocal := mustGit(t, dir, "rev-parse", "milestone/"+version)
	msOrigin := mustGit(t, dir, "rev-parse", "origin/milestone/"+version)

	var out string
	err := runCmdCapturing(t, &out, Phase(), "complete")
	if err != nil {
		t.Fatalf("complete should succeed against the recorded base: %v", err)
	}

	// Landed on main, at origin.
	if cur := mustGit(t, dir, "symbolic-ref", "--short", "HEAD"); cur != "main" {
		t.Errorf("HEAD = %q, want main (the recorded base)", cur)
	}
	if local, origin := mustGit(t, dir, "rev-parse", "main"), mustGit(t, dir, "rev-parse", "origin/main"); local != origin {
		t.Errorf("main not at origin after completion: %s != %s", local, origin)
	}
	// Phase branch gone, locally and on origin.
	if b := mustGit(t, dir, "branch", "--list", "phase/auth"); b != "" {
		t.Errorf("phase/auth should be deleted locally, got %q", b)
	}
	if r := mustGit(t, dir, "ls-remote", "--heads", "origin", "phase/auth"); r != "" {
		t.Errorf("phase/auth should be deleted on origin, got %q", r)
	}
	// The milestone branch was never the target — and is never named as one.
	assertMilestoneUntouched(t, dir, version, msLocal, msOrigin)
	if strings.Contains(out, "milestone/"+version) {
		t.Errorf("completion named the milestone branch as its reconcile target:\n%s", out)
	}
}

// TestStaleMilestoneIncidentRefusesWithoutRecord is the refusal arm: strip the
// base record — the shape of a phase created before this check — and the run
// must refuse rather than fall back to the milestone-inferred base. The
// refusal is a no-op: the milestone branch is untouched and the phase branch
// survives on both sides, so the work is recoverable with one --base re-run.
func TestStaleMilestoneIncidentRefusesWithoutRecord(t *testing.T) {
	dir, version := staleMilestoneFixture(t)
	stubPRMerged(t, true)

	// Strip the base, keeping the PR number: a phase that shipped before the
	// record existed.
	mustWrite(t, filepath.Join(dir, ".dross/phases/auth/changes.json"),
		`{"phase":"auth","pr":42,"tasks":{}}`)
	mustGit(t, dir, "add", ".dross/phases/auth/changes.json")
	mustGit(t, dir, "commit", "-q", "-m", "chore(dross): pre-record shape")

	msLocal := mustGit(t, dir, "rev-parse", "milestone/"+version)
	msOrigin := mustGit(t, dir, "rev-parse", "origin/milestone/"+version)
	mainBefore := mustGit(t, dir, "rev-parse", "main")

	err := runCmd(t, Phase(), "complete")
	if err == nil {
		t.Fatal("expected a refusal: no base recorded, and inferring one is the bug")
	}
	if !strings.Contains(err.Error(), "--base") {
		t.Errorf("refusal should name the --base escape hatch: %v", err)
	}

	// Nothing moved.
	assertMilestoneUntouched(t, dir, version, msLocal, msOrigin)
	if got := mustGit(t, dir, "rev-parse", "main"); got != mainBefore {
		t.Errorf("main was moved by a refused complete: %s -> %s", mainBefore, got)
	}
	if cur := mustGit(t, dir, "symbolic-ref", "--short", "HEAD"); cur != "phase/auth" {
		t.Errorf("HEAD = %q, want phase/auth — a refusal must not relocate the user", cur)
	}
	// The work survives on both sides.
	if b := mustGit(t, dir, "branch", "--list", "phase/auth"); b == "" {
		t.Error("a refused complete must not delete the local phase branch")
	}
	if r := mustGit(t, dir, "ls-remote", "--heads", "origin", "phase/auth"); r == "" {
		t.Error("a refused complete must not delete the remote phase branch")
	}

	// And the documented recovery actually works, from exactly this state.
	if err := runCmd(t, Phase(), "complete", "--base", "main"); err != nil {
		t.Fatalf("--base main should carry the refused run through: %v", err)
	}
	assertMilestoneUntouched(t, dir, version, msLocal, msOrigin)
	if cur := mustGit(t, dir, "symbolic-ref", "--short", "HEAD"); cur != "main" {
		t.Errorf("after --base main, HEAD = %q, want main", cur)
	}
}
