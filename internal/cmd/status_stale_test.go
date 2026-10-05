package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/changes"
	"github.com/Rivil/dross/internal/treefp"
	"github.com/Rivil/dross/internal/verify"
)

func staleStatusOut(t *testing.T) string {
	t.Helper()
	return captureStdout(t, func() {
		if err := runCmd(t, Status()); err != nil {
			t.Fatalf("status: %v", err)
		}
	})
}

// staleStatusFixture is the ship fixture — phase x on phase/x, a pass verdict
// that measured its tree — with two files changed and committed since.
func staleStatusFixture(t *testing.T) string {
	t.Helper()
	dir := stampedShipFixture(t)
	mustWrite(t, filepath.Join(dir, "src/tag.ts"), "export const tag = 9\n")
	mustWrite(t, filepath.Join(dir, "src/other.ts"), "export const o = 1\n")
	gitCommit(t, dir, "changes after verify")
	return dir
}

// readyForNextStep fills in what suggestNext checks before it reaches the
// phase's own step: a complete project.toml and a current milestone. Both are
// .dross writes, so they leave the verdict's freshness alone.
func readyForNextStep(t *testing.T, dir string) {
	t.Helper()
	mustWrite(t, filepath.Join(dir, ".dross", "phases", "x", "plan.toml"),
		"[phase]\n  id = \"x\"\n\n[[task]]\n  id = \"t-1\"\n  wave = 1\n  title = \"tag\"\n  covers = [\"C1\"]\n  status = \"done\"\n")
	mustRunSet(t, "project.name", "p")
	mustRunSet(t, "runtime.mode", "native")
	if err := runCmd(t, State(), "set", "current_milestone", "v1"); err != nil {
		t.Fatal(err)
	}
}

func setVerdict(t *testing.T, dir, verdict string) {
	t.Helper()
	path := filepath.Join(dir, ".dross", "phases", "x", verify.VerifyFile)
	v, err := verify.LoadVerify(path)
	if err != nil || v == nil {
		t.Fatalf("load verify.toml: %v", err)
	}
	v.Verify.Verdict = verdict
	if err := v.Save(path); err != nil {
		t.Fatal(err)
	}
}

// TestStatusMarksAStalePassWithCount: a pass that predates changed files is
// marked, with how many.
func TestStatusMarksAStalePassWithCount(t *testing.T) {
	staleStatusFixture(t)
	out := staleStatusOut(t)
	if !strings.Contains(out, "stale") || !strings.Contains(out, "2 file") {
		t.Errorf("status did not mark the stale pass with its count:\n%s", out)
	}
}

// TestStatusNoMarkerWhenFreshOrLegacy: fresh, exempt-only changes, a verdict
// with no tree, and a complete phase all print no stale line.
func TestStatusNoMarkerWhenFreshOrLegacy(t *testing.T) {
	t.Run("fresh", func(t *testing.T) {
		stampedShipFixture(t)
		if out := staleStatusOut(t); strings.Contains(out, "stale:") {
			t.Errorf("a fresh pass was marked stale:\n%s", out)
		}
	})
	t.Run("exempt changes", func(t *testing.T) {
		dir := stampedShipFixture(t)
		mustWrite(t, filepath.Join(dir, "ARCHITECTURE.md"), "# Architecture\n\nlandmarks\n")
		mustWrite(t, filepath.Join(dir, ".dross/phases/x/notes.md"), "bookkeeping\n")
		gitCommit(t, dir, "exempt only")
		if out := staleStatusOut(t); strings.Contains(out, "stale:") {
			t.Errorf(".dross/ and ARCHITECTURE.md changes marked the pass stale:\n%s", out)
		}
	})
	t.Run("legacy", func(t *testing.T) {
		dir := shipFixture(t, "https://forge.example/me/p.git")
		mustWrite(t, filepath.Join(dir, "src/tag.ts"), "export const tag = 10\n")
		gitCommit(t, dir, "change on a verdict with no tree")
		if out := staleStatusOut(t); strings.Contains(out, "stale:") {
			t.Errorf("a verdict with no recorded tree was marked stale:\n%s", out)
		}
	})
	t.Run("complete", func(t *testing.T) {
		dir := staleStatusFixture(t)
		path := changes.FilePath(filepath.Join(dir, ".dross"), "x")
		ch, err := changes.Load(path, "x")
		if err != nil {
			t.Fatal(err)
		}
		ch.Status = changes.StatusComplete
		if err := ch.Save(path); err != nil {
			t.Fatal(err)
		}
		if out := staleStatusOut(t); strings.Contains(out, "stale:") {
			t.Errorf("a complete phase was marked stale:\n%s", out)
		}
	})
}

// TestStatusNoMarkerOnNonPass: only a pass is marked — a pending or failing
// verdict is already not shippable.
func TestStatusNoMarkerOnNonPass(t *testing.T) {
	for _, verdict := range []string{"pending", "fail"} {
		t.Run(verdict, func(t *testing.T) {
			dir := staleStatusFixture(t)
			setVerdict(t, dir, verdict)
			if out := staleStatusOut(t); strings.Contains(out, "stale:") {
				t.Errorf("a %s verdict was marked stale:\n%s", verdict, out)
			}
		})
	}
}

// TestStatusNoMarkerFromMain: from another branch — where the session hook
// usually runs — the phase's freshness is not judged against that tree.
func TestStatusNoMarkerFromMain(t *testing.T) {
	dir := staleStatusFixture(t)
	mustGit(t, dir, "checkout", "-q", "main")
	if out := staleStatusOut(t); strings.Contains(out, "stale:") {
		t.Errorf("status on main marked phase x stale against main's tree:\n%s", out)
	}
	// A branch cut from the phase tip carries the verdict and the changed
	// files, so only the branch check keeps it quiet: phase x is judged on
	// phase/x and nowhere else.
	mustGit(t, dir, "checkout", "-q", "phase/x")
	mustGit(t, dir, "checkout", "-q", "-b", "elsewhere")
	if out := staleStatusOut(t); strings.Contains(out, "stale:") {
		t.Errorf("status off phase/x judged phase x against another branch's tree:\n%s", out)
	}
}

// TestStatusStaleSilentOnError: status is a hook target, so a check that
// cannot run prints nothing and breaks nothing else.
func TestStatusStaleSilentOnError(t *testing.T) {
	t.Run("capture fails", func(t *testing.T) {
		staleStatusFixture(t)
		prev := measuredTreeFn
		measuredTreeFn = func(string) (treefp.Measured, error) { return treefp.Measured{}, errors.New("index.lock exists") }
		t.Cleanup(func() { measuredTreeFn = prev })
		out := staleStatusOut(t)
		if strings.Contains(out, "stale") || !strings.Contains(out, "phase:") {
			t.Errorf("a failed check broke or marked status:\n%s", out)
		}
	})
	t.Run("no longer a git repo", func(t *testing.T) {
		dir := staleStatusFixture(t)
		if err := os.RemoveAll(filepath.Join(dir, ".git")); err != nil {
			t.Fatal(err)
		}
		out := staleStatusOut(t)
		if strings.Contains(out, "stale") || !strings.Contains(out, "phase:") {
			t.Errorf("a non-git root carrying a tree broke or marked status:\n%s", out)
		}
	})
}

// TestStatusStaleWithPrunedTree: a recorded tree this clone does not have
// still marks the verdict — without a count, never silently.
func TestStatusStaleWithPrunedTree(t *testing.T) {
	dir := shipFixture(t, "https://forge.example/me/p.git")
	setRecordedTree(t, dir, "", "0123456789abcdef0123456789abcdef01234567")
	if out := staleStatusOut(t); !strings.Contains(out, "stale (changed files could not be listed)") {
		t.Errorf("an unlistable recorded tree was not marked:\n%s", out)
	}
}

// TestStatusStaleSuggestsReverify: the next step for a stale pass is
// /dross-verify, not ship, and the footer stays byte-equal to reentry's.
func TestStatusStaleSuggestsReverify(t *testing.T) {
	dir := staleStatusFixture(t)
	readyForNextStep(t, dir)
	out := staleStatusOut(t)
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	last := lines[len(lines)-1]
	if !strings.Contains(last, "/dross-verify") || strings.Contains(last, "/dross-ship") {
		t.Errorf("the footer does not route a stale pass to /dross-verify: %q", last)
	}
	reentry := captureStdout(t, func() {
		if err := runCmd(t, Reentry()); err != nil {
			t.Fatal(err)
		}
	})
	if want := decodeReentry(t, reentry); last != want {
		t.Errorf("status footer != reentry line:\nstatus:  %q\nreentry: %q", last, want)
	}
}

// TestStatusStaleWhileShippedSuggestsReverify: with the PR open and a fix
// pushed since the pass, the next step is a re-verify — not the merge.
func TestStatusStaleWhileShippedSuggestsReverify(t *testing.T) {
	dir := stampedShipFixture(t)
	shipMockFlow(t, dir)
	stampVerdict(t, dir)
	if err := runCmd(t, Ship(), "x"); err != nil {
		t.Fatalf("ship: %v", err)
	}
	mustWrite(t, filepath.Join(dir, "src/tag.ts"), "export const tag = 11 // CI fix\n")
	gitCommit(t, dir, "fix CI after the PR opened")
	readyForNextStep(t, dir)

	out := staleStatusOut(t)
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	last := lines[len(lines)-1]
	if !strings.Contains(out, "stale:") {
		t.Errorf("a shipped phase with a stale pass was not marked:\n%s", out)
	}
	if !strings.Contains(last, "/dross-verify") || strings.Contains(last, "merge") {
		t.Errorf("the footer sends a stale shipped phase to merge, not re-verify: %q", last)
	}
}
