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

// basePushFixture is setupMilestoneRepo with main pushed, so origin/<base>
// exists and the safety-net push has something to compare against.
func basePushFixture(t *testing.T) (dir, origin string) {
	t.Helper()
	dir, origin = setupMilestoneRepo(t)
	mustGit(t, dir, "push", "-q", "-u", "origin", "main")
	return dir, origin
}

// c-2: local base one .dross-only chore ahead → the pre-flight pushes and
// origin/<base>..<base> is empty afterward.
func TestPushBaseDrossOnlyAheadPushes(t *testing.T) {
	dir, _ := basePushFixture(t)
	mustWrite(t, filepath.Join(dir, ".dross", "handoff.md"), "# handoff\n")
	mustGit(t, dir, "add", ".dross")
	mustGit(t, dir, "commit", "-q", "-m", "chore(dross): pause snapshot")

	pushed, err := pushBaseIfAheadDrossOnly(dir, "main")
	if err != nil {
		t.Fatalf("a .dross-only ahead base should push cleanly: %v", err)
	}
	if !pushed {
		t.Error("expected pushed=true for a .dross-only ahead base")
	}
	if ahead := mustGit(t, dir, "rev-list", "origin/main..main"); ahead != "" {
		t.Errorf("origin/main..main should be empty after the safety-net push, got: %q", ahead)
	}
}

// c-2: a base ahead by a code commit → refuse and push nothing.
func TestPushBaseCodeAheadRefusesAndPushesNothing(t *testing.T) {
	dir, origin := basePushFixture(t)
	mustWrite(t, filepath.Join(dir, "src.go"), "package src\n")
	mustGit(t, dir, "add", "src.go")
	mustGit(t, dir, "commit", "-q", "-m", "feat: code committed to main")
	remoteBefore := mustGit(t, origin, "rev-parse", "main")

	pushed, err := pushBaseIfAheadDrossOnly(dir, "main")
	if err == nil {
		t.Fatal("expected refusal when the base is ahead by a code commit")
	}
	if pushed {
		t.Error("pushed must be false on refusal")
	}
	if after := mustGit(t, origin, "rev-parse", "main"); after != remoteBefore {
		t.Errorf("refusal must push nothing: origin main moved %s -> %s", remoteBefore, after)
	}
}

// c-2: not ahead at all → no-op, no push.
func TestPushBaseNotAheadNoop(t *testing.T) {
	dir, _ := basePushFixture(t)
	pushed, err := pushBaseIfAheadDrossOnly(dir, "main")
	if err != nil {
		t.Fatalf("clean base must be a no-op: %v", err)
	}
	if pushed {
		t.Error("pushed must be false when the base is not ahead")
	}
}

// push_failure decision: unreachable origin → hard non-nil error, never
// warn-and-continue.
func TestPushBaseUnreachableOriginHardError(t *testing.T) {
	dir, _ := basePushFixture(t)
	mustWrite(t, filepath.Join(dir, ".dross", "handoff.md"), "# handoff\n")
	mustGit(t, dir, "add", ".dross")
	mustGit(t, dir, "commit", "-q", "-m", "chore(dross): pause snapshot")
	mustGit(t, dir, "remote", "set-url", "origin", filepath.Join(t.TempDir(), "gone"))

	if _, err := pushBaseIfAheadDrossOnly(dir, "main"); err == nil {
		t.Fatal("unreachable origin must be a hard error")
	}
}

// chore_push decision: pause is a local-only writer — no push even when the
// base ends up ahead.
func TestPauseNeverPushes(t *testing.T) {
	dir, origin := basePushFixture(t)
	mustWrite(t, filepath.Join(dir, ".dross", "handoff.md"), "# handoff\n")
	mustGit(t, dir, "add", ".dross")
	mustGit(t, dir, "commit", "-q", "-m", "chore(dross): earlier snapshot")
	remoteBefore := mustGit(t, origin, "rev-parse", "main")

	if err := runCmd(t, Pause(), "--auto"); err != nil {
		t.Fatalf("pause --auto: %v", err)
	}
	if after := mustGit(t, origin, "rev-parse", "main"); after != remoteBefore {
		t.Errorf("pause must never push: origin main moved %s -> %s", remoteBefore, after)
	}
	if ahead := mustGit(t, dir, "rev-list", "origin/main..main"); ahead == "" {
		t.Error("fixture broke: the base should still be ahead after pause (nothing pushed)")
	}
}

// A truly diverged base (ahead AND behind origin) is owned by the ff-only /
// --recover machinery — the safety net must no-op, not refuse or push, so the
// downstream guided errors still fire.
func TestPushBaseDivergedNoop(t *testing.T) {
	dir, origin := basePushFixture(t)
	// Ahead: a .dross chore on local main.
	mustWrite(t, filepath.Join(dir, ".dross", "handoff.md"), "# handoff\n")
	mustGit(t, dir, "add", ".dross")
	mustGit(t, dir, "commit", "-q", "-m", "chore(dross): snapshot")
	// Behind: a commit on origin/main that local main lacks (pushed from a
	// throwaway clone of the pre-chore tip).
	mustGit(t, dir, "checkout", "-q", "-b", "upstream-sim", "origin/main")
	mustWrite(t, filepath.Join(dir, "upstream.txt"), "x\n")
	mustGit(t, dir, "add", "upstream.txt")
	mustGit(t, dir, "commit", "-q", "-m", "feat: upstream squash")
	mustGit(t, dir, "push", "-q", "origin", "upstream-sim:main")
	mustGit(t, dir, "checkout", "-q", "main")
	mustGit(t, dir, "branch", "-q", "-D", "upstream-sim")
	remoteBefore := mustGit(t, origin, "rev-parse", "main")

	pushed, err := pushBaseIfAheadDrossOnly(dir, "main")
	if err != nil {
		t.Fatalf("diverged base must be a silent no-op for the safety net: %v", err)
	}
	if pushed {
		t.Error("safety net must not push into a diverged base")
	}
	if after := mustGit(t, origin, "rev-parse", "main"); after != remoteBefore {
		t.Errorf("origin main must be untouched: %s -> %s", remoteBefore, after)
	}
}

// c-2: ship's pre-flight safety net pushes .dross-only chores sitting
// unpushed on the local base, so local main == origin/main after shipping.
// (Lives here with the safety-net unit tests; uses ship_test.go's fixtures.)
func TestShipPushesBaseAheadDrossChores(t *testing.T) {
	dir := shipFixture(t, "https://forge.example/me/p.git")
	shipMockFlow(t, dir)
	mustGit(t, dir, "push", "-q", "origin", "main")
	// A .dross-only chore on local main that origin lacks (pause snapshot).
	mustGit(t, dir, "checkout", "-q", "main")
	mustWrite(t, filepath.Join(dir, ".dross", "handoff.md"), "# handoff\n")
	mustGit(t, dir, "add", ".dross")
	mustGit(t, dir, "commit", "-q", "-m", "chore(dross): pause snapshot")
	mustGit(t, dir, "checkout", "-q", "phase/x")

	if err := runCmd(t, Ship()); err != nil {
		t.Fatalf("ship: %v", err)
	}
	if ahead := mustGit(t, dir, "rev-list", "origin/main..main"); ahead != "" {
		t.Errorf("local main should be fully pushed after ship's safety net, got ahead: %q", ahead)
	}
}
