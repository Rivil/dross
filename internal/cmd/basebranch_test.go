package cmd

import (
	"path/filepath"
	"testing"
)

// resolveNewWorkBase is the single existence-aware resolver that pins the
// rollout_cutover and no_milestone_fallback locked decisions. These tests
// reuse setupMilestoneRepo (milestone_test.go): a git repo + bare origin +
// one commit on main, dross-initialised.

func TestResolveBase_MilestoneBranchExists(t *testing.T) {
	dir, _ := setupMilestoneRepo(t)
	// create cuts + pushes milestone/v0.9; then make it the active milestone.
	if err := runCmd(t, Milestone(), "create", "v0.9"); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := runCmd(t, State(), "set", "current_milestone", "v0.9"); err != nil {
		t.Fatalf("state set: %v", err)
	}

	base, active, err := resolveNewWorkBase(dir, filepath.Join(dir, ".dross"))
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if base != "milestone/v0.9" || !active {
		t.Errorf("got (%q, %v); want (milestone/v0.9, true)", base, active)
	}
}

func TestResolveBase_CutoverNoBranch(t *testing.T) {
	dir, _ := setupMilestoneRepo(t)
	// Active milestone whose integration branch does NOT exist — the
	// pre-cutover case (v0.7 was scoped before the branch model). Must fall
	// back to main, proving the non-retrofit cutover.
	if err := runCmd(t, State(), "set", "current_milestone", "v0.7"); err != nil {
		t.Fatalf("state set: %v", err)
	}

	base, active, err := resolveNewWorkBase(dir, filepath.Join(dir, ".dross"))
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if base != "main" || active {
		t.Errorf("got (%q, %v); want (main, false) — cutover fallback", base, active)
	}
}

func TestResolveBase_NoMilestone(t *testing.T) {
	dir, _ := setupMilestoneRepo(t)
	// No current_milestone at all → no_milestone_fallback to main.
	base, active, err := resolveNewWorkBase(dir, filepath.Join(dir, ".dross"))
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if base != "main" || active {
		t.Errorf("got (%q, %v); want (main, false)", base, active)
	}
}
