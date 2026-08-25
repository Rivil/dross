package cmd

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/state"
	"github.com/Rivil/dross/internal/watch"
)

// One fixture, four re-entry surfaces, one answer.
//
// This is the second half of the c-5 guard. The first is
// completion_record_truth_test.go, which asks `dross status`, `dross milestone
// progress` and `dross phase list --milestone` the same doneness question. It
// cannot be extended to cover these four: truthFixture is git-free (three of the
// surfaces below need a real branch and a real origin) and askAllThree asserts
// on an N/M count regex, which none of these four print.
//
// So c-5's "one fixture" means one fixture across THESE four surfaces, not a
// merge of the two guards. The four:
//
//   - the watch drift digest's classifier
//   - the `N phase branches are waiting on a completion` count
//   - `dross status`'s shipped / waiting-on-merge line
//   - the SessionStart re-entry line
//
// Each of them used to decide doneness from something that is not the phase's
// completion record — a `completed <id>` breadcrumb in state.json's capped
// 50-entry history, or verify.toml's verdict. Both are readings that agree with
// the record right up until they don't: the breadcrumb ages out, and a verdict
// is not a completion. A per-surface test cannot catch the divergence, because
// each one passes alone against its own fixture. Only asking all four the same
// question over one repo does.
//
// The fixture inverts both wrong readings at once:
//
//	changes.json  status = "complete"   ← the record: this phase is closed out
//	state.json    50 entries, none of them `completed closed`
//	verify.toml   exists, carries NO verdict
//	phase/closed  still a local branch, still unmerged on origin/main
//
// A surface that reads history reports the phase as outstanding. So does one
// that reads the verdict. The record says otherwise, and the record is the only
// thing that never scrolls.
//
// The guard is behavioral: it runs the four surfaces and compares answers. A
// source scan would pass over a fresh helper doing the same wrong thing.

// reentryTruthFixture builds that repo and leaves HEAD on phase/closed — the
// old branch a completed phase is visited from, which is the only place the
// waiting-on-merge line is reachable at all. Returns the repo dir.
func reentryTruthFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	remote := t.TempDir()
	mustGit(t, remote, "init", "-q", "--bare", "-b", "main")
	gitInit(t, dir, remote)
	chdir(t, dir)

	if err := runCmd(t, Init()); err != nil {
		t.Fatalf("init: %v", err)
	}
	mustRunSet(t, "project.name", "test-app")
	mustRunSet(t, "runtime.mode", "native")
	mustRunSet(t, "repo.git_main_branch", "main")
	if err := runCmd(t, State(), "set", "current_milestone", "v1.5"); err != nil {
		t.Fatal(err)
	}
	if err := runCmd(t, State(), "set", "current_phase", "closed"); err != nil {
		t.Fatal(err)
	}

	mustWrite(t, filepath.Join(dir, "README.md"), "base\n")
	mustGit(t, dir, "add", ".")
	mustGit(t, dir, "commit", "-q", "-m", "chore: baseline")
	mustGit(t, dir, "push", "-q", "-u", "origin", "main")

	// The branch a completed phase leaves behind: still local, never merged
	// onto origin/main. `dross phase complete` deletes it, but nothing forces
	// that, and a phase whose branch survives is the shape every one of these
	// surfaces used to re-announce forever.
	mustGit(t, dir, "checkout", "-q", "-b", "phase/closed")
	mustWrite(t, filepath.Join(dir, "closed.txt"), "work\n")
	mustGit(t, dir, "add", ".")
	mustGit(t, dir, "commit", "-q", "-m", "feat(closed): work")

	root := filepath.Join(dir, ".dross")
	mustWrite(t, filepath.Join(root, "milestones", "v1.5.toml"),
		"phases = [\"closed\"]\n\n[milestone]\nversion = \"v1.5\"\nstatus = \"active\"\n")

	pdir := filepath.Join(root, "phases", "closed")
	mustWrite(t, filepath.Join(pdir, "spec.toml"), "[phase]\n  id = \"closed\"\n  title = \"closed\"\n")
	mustWrite(t, filepath.Join(pdir, "plan.toml"), `[phase]
id = "closed"
[[task]]
id = "t-1"
wave = 1
title = "work"
files = ["closed.txt"]
covers = ["c-1"]
status = "done"
`)
	// verify.toml EXISTS but carries no verdict — the inverted half. A
	// verdict-derived doneness read calls this phase unfinished; its record
	// says it is closed out.
	mustWrite(t, filepath.Join(pdir, "verify.toml"), "[verify]\nphase = \"closed\"\n")
	mustWrite(t, filepath.Join(pdir, "changes.json"),
		`{"phase":"closed","pr":7,"base":"main","status":"complete","tasks":{}}`)

	// Fifty further actions, none of them the completion — the ordinary shape
	// of state.json a couple of phases later.
	st, err := state.Load(filepath.Join(root, state.File))
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 50; i++ {
		st.Touch(fmt.Sprintf("touched something %d", i))
	}
	if err := st.Save(filepath.Join(root, state.File)); err != nil {
		t.Fatal(err)
	}
	return dir
}

// assertReentryFixtureIsInverted fails loudly if the fixture stops being the
// thing the guard needs. Every assertion below is meaningless without this:
// a breadcrumb that never aged out, or a verdict that reads pass, would let a
// reverted surface agree with the record by accident.
func assertReentryFixtureIsInverted(t *testing.T, dir string) {
	t.Helper()
	root := filepath.Join(dir, ".dross")
	st, err := state.Load(filepath.Join(root, state.File))
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range st.History {
		if strings.Contains(a.Action, "completed closed") {
			t.Fatal("fixture must have aged the completion breadcrumb out of history")
		}
	}
	if v := readVerifyVerdict(filepath.Join(root, "phases", "closed", "verify.toml")); v != "" {
		t.Fatalf("fixture's verify.toml must carry no verdict, got %q", v)
	}
	if got := mustGit(t, dir, "symbolic-ref", "--short", "HEAD"); got != "phase/closed" {
		t.Fatalf("fixture must be visited from phase/closed, got %q", got)
	}
	if mustGit(t, dir, "rev-parse", "--verify", "--quiet", "refs/heads/phase/closed") == "" {
		t.Fatal("fixture must keep the phase branch")
	}
}

// TestFourReentrySurfacesAgreeOnTheCompletionRecord is the guard.
//
// Every assertion is over the SAME repo in the same state. A surface that goes
// back to state.History's breadcrumb, or to verify.toml's verdict, disagrees
// with the other three here and names itself in the failure.
func TestFourReentrySurfacesAgreeOnTheCompletionRecord(t *testing.T) {
	dir := reentryTruthFixture(t)
	assertReentryFixtureIsInverted(t, dir)
	root := filepath.Join(dir, ".dross")

	st, err := state.Load(filepath.Join(root, state.File))
	if err != nil {
		t.Fatal(err)
	}

	// 1. The watch drift digest.
	ds, err := watch.ClassifyDrift(root, st)
	if err != nil {
		t.Fatalf("ClassifyDrift: %v", err)
	}
	for _, d := range ds {
		if d.Phase == "closed" {
			t.Errorf("the watch drift classifier reports a completed phase as %q drift", d.Kind)
		}
	}

	// 2. The `N phase branches are waiting on a completion` count. Held at ONE
	// candidate at most on purpose: above one, suggestNext returns the
	// reconcile suggestion ahead of every doneness-sensitive arm and assertion
	// 4 stops testing what it names.
	if n := reconcilableCount(root); n != 0 {
		t.Errorf("the reconcile count reports %d phase branch(es) waiting on a completion, want 0", n)
	}

	out := captureStdout(t, func() {
		if err := runCmd(t, Status()); err != nil {
			t.Fatalf("status: %v", err)
		}
	})

	// 3. The shipped / waiting-on-merge line.
	if strings.Contains(out, "shipped:") {
		t.Errorf("the shipped reader announces a completed phase as waiting on a merge:\n%s", out)
	}

	// 4. The re-entry line. With the record read, the only thing outstanding
	// here is the unfinalized verdict; a surface reading history or the verdict
	// instead treats the phase as unfinished and advises the PR.
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	last := lines[len(lines)-1]
	if !strings.Contains(last, "dross verify finalize closed") {
		t.Errorf("the re-entry line should name the one outstanding step, got:\n%s", last)
	}
	for _, wrong := range []string{"merge the open PR", "dross phase complete", "/dross-ship"} {
		if strings.Contains(last, wrong) {
			t.Errorf("the re-entry line advises %q on a phase whose record says complete:\n%s", wrong, last)
		}
	}
}
