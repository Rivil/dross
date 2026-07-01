package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// captureOutErr runs fn with os.Stdout and os.Stderr each redirected to a pipe,
// returning what was printed to each. The base-branch command writes the branch
// name to stdout and the nudge to stderr via fmt.Print*/Fprintf directly, so
// both streams must be captured to assert the split.
func captureOutErr(t *testing.T, fn func()) (out, errOut string) {
	t.Helper()
	origOut, origErr := os.Stdout, os.Stderr
	ro, wo, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	re, we, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout, os.Stderr = wo, we
	doneO, doneE := make(chan string), make(chan string)
	go func() { var b bytes.Buffer; _, _ = b.ReadFrom(ro); doneO <- b.String() }()
	go func() { var b bytes.Buffer; _, _ = b.ReadFrom(re); doneE <- b.String() }()
	fn()
	_ = wo.Close()
	_ = we.Close()
	os.Stdout, os.Stderr = origOut, origErr
	return <-doneO, <-doneE
}

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

func TestBaseBranchCmdPrintsMilestone(t *testing.T) {
	dir, _ := setupMilestoneRepo(t)
	if err := runCmd(t, Milestone(), "create", "v0.9"); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := runCmd(t, State(), "set", "current_milestone", "v0.9"); err != nil {
		t.Fatalf("state set: %v", err)
	}
	_ = dir
	out, _ := captureOutErr(t, func() {
		if err := runCmd(t, BaseBranch()); err != nil {
			t.Fatalf("base-branch: %v", err)
		}
	})
	if strings.TrimSpace(out) != "milestone/v0.9" {
		t.Errorf("stdout = %q; want milestone/v0.9", strings.TrimSpace(out))
	}
}

func TestBaseBranchCmdPrintsMainNoMilestone(t *testing.T) {
	setupMilestoneRepo(t)
	out, _ := captureOutErr(t, func() {
		if err := runCmd(t, BaseBranch()); err != nil {
			t.Fatalf("base-branch: %v", err)
		}
	})
	if strings.TrimSpace(out) != "main" {
		t.Errorf("stdout = %q; want main", strings.TrimSpace(out))
	}
}

func TestBaseBranchCmdNudgesNoMilestone(t *testing.T) {
	setupMilestoneRepo(t)
	out, errOut := captureOutErr(t, func() {
		if err := runCmd(t, BaseBranch()); err != nil {
			t.Fatalf("base-branch: %v", err)
		}
	})
	// stdout stays the bare branch name (consumable via $(dross base-branch))...
	if strings.TrimSpace(out) != "main" {
		t.Errorf("stdout should be the bare branch name, got %q", strings.TrimSpace(out))
	}
	// ...while the nudge lands on stderr.
	if !strings.Contains(errOut, "dross milestone") {
		t.Errorf("stderr should nudge naming `dross milestone`, got %q", errOut)
	}
}
