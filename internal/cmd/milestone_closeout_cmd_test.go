package cmd

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Rivil/dross/internal/milestone"
	"github.com/Rivil/dross/internal/verify"
)

// The four headings a close-out refusal groups its items under.
const (
	headUnverified = "unverified phases ("
	headNotDone    = "phases not done ("
	headDeferred   = "unabsorbed deferred items ("
	headSurvivor   = "undisposed survivors ("
)

// todaysMilestoneBody is the integration PR body as it was before close-out
// acknowledgements existed, for v0.9 into main.
const todaysMilestoneBody = "Integration PR for milestone v0.9.\n\n" +
	"Merge as a **merge commit** (not squash) to preserve per-phase history on main."

var closeoutT0 = time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)

// seedEveryCloseoutKind gives milestone v0.9 one unresolved item of each kind:
//
//	unv   — complete, never verified: unverified
//	built — verified, no completion record: phase:built
//	p1    — verified and complete, its run recording survivor K: survivor:K
//	src   — outside the milestone, deferring d0 to built: deferred:src#0
func seedEveryCloseoutKind(t *testing.T, dir string) {
	t.Helper()
	root := filepath.Join(dir, ".dross")
	setMilestonePhases(t, dir, "v0.9", []string{"unv", "built", "p1"})
	ckPhase(t, root, "unv", "complete")
	ckVerify(t, root, "built", closeoutT0)
	ckVerify(t, root, "p1", closeoutT0, verify.SurvivorRecord{Key: "K", File: "f.go", Line: 12, Op: "op"})
	ckPhase(t, root, "p1", "complete")
	ckSpec(t, root, "src", "[[deferred]]\n  id = \"d0\"\n  text = \"carry me\"\n  target = \"built\"\n")
}

// c-4: one refusal names every kind, and a refusal moves no ref — the gate
// runs before the fetch and the head push, so an unpushed commit stays
// unpushed and origin's milestone branch is where it was.
func TestMilestoneCompleteRefusesWithEveryCloseoutKind(t *testing.T) {
	dir, cap, branch := milestonePushHeadFixture(t)
	seedEveryCloseoutKind(t, dir)
	mustWrite(t, filepath.Join(dir, "late.txt"), "committed locally, never pushed\n")
	mustGit(t, dir, "add", "late.txt")
	mustGit(t, dir, "commit", "-q", "-m", "late work on the milestone branch")
	local := mustGit(t, dir, "rev-parse", branch)
	originBefore := mustGit(t, dir, "ls-remote", "origin", "refs/heads/"+branch)
	refsBefore := mustGit(t, dir, "for-each-ref")
	if strings.Contains(originBefore, local) {
		t.Fatalf("fixture is not set up: origin already has the local commit %s", local)
	}

	err := runCmd(t, Milestone(), "complete", "v0.9")
	if err == nil {
		t.Fatal("a milestone with every close-out kind unresolved opened its PR")
	}
	for _, want := range []string{headUnverified, headNotDone, headDeferred, headSurvivor,
		"unv", "phase:built", "deferred:src#0", "survivor:K"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal is missing %q:\n%v", want, err)
		}
	}
	if cap.created != 0 || cap.posts != 0 {
		t.Errorf("refusal still reached the provider (created=%d posts=%d)", cap.created, cap.posts)
	}
	if after := mustGit(t, dir, "ls-remote", "origin", "refs/heads/"+branch); after != originBefore {
		t.Errorf("origin %s moved on a refusal: %q -> %q", branch, originBefore, after)
	}
	if after := mustGit(t, dir, "for-each-ref"); after != refsBefore {
		t.Errorf("refs changed on a refusal:\nbefore:\n%s\nafter:\n%s", refsBefore, after)
	}
}

// seedForcedPastCollectorError gives milestone v0.9 one unverified phase and
// a source spec.toml that will not parse. --force-unverified clears the
// unverified item, leaving only the deferred and survivor collector errors —
// which the flag must not drop.
func seedForcedPastCollectorError(t *testing.T, dir string) {
	t.Helper()
	root := filepath.Join(dir, ".dross")
	setMilestonePhases(t, dir, "v0.9", []string{"unv"})
	ckPhase(t, root, "unv", "complete")
	ckSpec(t, root, "src", "[[deferred]\n  broken = ***\n")
}

// --force-unverified skips the unverified kind and nothing else, and leaves
// no record of having done so.
func TestMilestoneCompleteForceUnverifiedDropsOnlyThatKind(t *testing.T) {
	t.Run("other kinds still refuse", func(t *testing.T) {
		dir, cap := milestoneOpenFixture(t)
		seedEveryCloseoutKind(t, dir)
		err := runCmd(t, Milestone(), "complete", "v0.9", "--force-unverified")
		if err == nil {
			t.Fatal("--force-unverified waved through the not-done, deferred and survivor kinds")
		}
		if strings.Contains(err.Error(), headUnverified) {
			t.Errorf("refusal under --force-unverified still lists the unverified kind:\n%v", err)
		}
		for _, want := range []string{headNotDone, headDeferred, headSurvivor} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("refusal under --force-unverified is missing %q:\n%v", want, err)
			}
		}
		if cap.created != 0 {
			t.Errorf("PR opened despite the refusal (created=%d)", cap.created)
		}
	})
	t.Run("only unverified opens with no ack line", func(t *testing.T) {
		dir, cap := milestoneOpenFixture(t)
		setMilestonePhases(t, dir, "v0.9", []string{"unv"})
		ckPhase(t, filepath.Join(dir, ".dross"), "unv", "complete")
		path := milestone.FilePath(filepath.Join(dir, ".dross"), "v0.9")
		before := sha256OfFile(t, path)

		if err := runCmd(t, Milestone(), "complete", "v0.9", "--force-unverified"); err != nil {
			t.Fatalf("--force-unverified did not override an unverified-only milestone: %v", err)
		}
		if cap.created != 1 {
			t.Fatalf("PR was not opened (created=%d)", cap.created)
		}
		if cap.body != todaysMilestoneBody {
			t.Errorf("the override reached the PR body as an acknowledgement:\n%s", cap.body)
		}
		if sha256OfFile(t, path) != before {
			t.Error("--force-unverified recorded something in the milestone toml")
		}
	})
	t.Run("a collector error still refuses", func(t *testing.T) {
		dir, cap := milestoneOpenFixture(t)
		seedForcedPastCollectorError(t, dir)
		err := runCmd(t, Milestone(), "complete", "v0.9", "--force-unverified")
		if err == nil {
			t.Fatal("--force-unverified opened the PR past a source spec.toml that will not parse")
		}
		if strings.Contains(err.Error(), headUnverified) {
			t.Errorf("refusal under --force-unverified still lists the unverified kind:\n%v", err)
		}
		for _, want := range []string{headDeferred, "ERROR — "} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("refusal under --force-unverified is missing %q:\n%v", want, err)
			}
		}
		if cap.created != 0 {
			t.Errorf("PR opened despite the refusal (created=%d)", cap.created)
		}
	})
	t.Run("a not-done phase still refuses", func(t *testing.T) {
		dir, cap := milestoneOpenFixture(t)
		setMilestonePhases(t, dir, "v0.9", []string{"built"})
		ckVerify(t, filepath.Join(dir, ".dross"), "built", closeoutT0)
		err := runCmd(t, Milestone(), "complete", "v0.9", "--force-unverified")
		if err == nil || !strings.Contains(err.Error(), "phase:built") {
			t.Fatalf("not-done phase under --force-unverified: err = %v, want a refusal naming phase:built", err)
		}
		if cap.created != 0 {
			t.Errorf("PR opened despite the refusal (created=%d)", cap.created)
		}
	})
}

// c-5: an acknowledged item stops blocking, and the ack's item and reason
// reach the PR body.
func TestMilestoneCompleteCarriesAcksIntoPRBody(t *testing.T) {
	dir, cap := milestoneOpenFixture(t)
	setMilestonePhases(t, dir, "v0.9", []string{"built"})
	ckVerify(t, filepath.Join(dir, ".dross"), "built", closeoutT0)

	if err := runCmd(t, Milestone(), "ack", "phase:built", "v0.9", "--reason", "moved to v2.0"); err != nil {
		t.Fatalf("ack: %v", err)
	}
	if err := runCmd(t, Milestone(), "complete", "v0.9"); err != nil {
		t.Fatalf("an acknowledged item still blocked: %v", err)
	}
	if cap.created != 1 {
		t.Fatalf("PR was not opened (created=%d)", cap.created)
	}
	for _, want := range []string{todaysMilestoneBody, "Close-out acknowledgements", "phase:built", "moved to v2.0"} {
		if !strings.Contains(cap.body, want) {
			t.Errorf("PR body is missing %q:\n%s", want, cap.body)
		}
	}
}

// A reason typed across lines stays one bullet in the PR body.
func TestMilestonePRBodyKeepsEachAckOnOneLine(t *testing.T) {
	body := milestonePRBody("v0.9", "main", []closeoutApplied{{
		Item: closeoutItem{Detail: "no shipped\nor complete record"},
		Ack:  milestone.CloseoutAck{Item: "phase:built", Reason: "moved\n- to v2.0"},
	}})
	want := todaysMilestoneBody + "\n\n## Close-out acknowledgements\n\n" +
		"Unresolved when this PR opened, and acknowledged with `dross milestone ack`:\n\n" +
		"- `phase:built` — moved - to v2.0 (no shipped or complete record)"
	if body != want {
		t.Errorf("body =\n%q\nwant\n%q", body, want)
	}
}

// c-8: with nothing to close out — no record, or no phases — the PR opens and
// its body is byte-for-byte the one it always was.
func TestMilestoneCompleteEmptyCloseoutKeepsTodaysBody(t *testing.T) {
	for name, seed := range map[string]func(t *testing.T, dir string){
		"no milestone record": func(*testing.T, string) {},
		"empty phases":        func(t *testing.T, dir string) { setMilestonePhases(t, dir, "v0.9", nil) },
	} {
		t.Run(name, func(t *testing.T) {
			dir, cap := milestoneOpenFixture(t)
			seed(t, dir)
			if err := runCmd(t, Milestone(), "complete", "v0.9"); err != nil {
				t.Fatalf("complete: %v", err)
			}
			if cap.created != 1 {
				t.Fatalf("PR was not opened (created=%d)", cap.created)
			}
			if cap.body != todaysMilestoneBody {
				t.Errorf("body =\n%q\nwant\n%q", cap.body, todaysMilestoneBody)
			}
		})
	}
}

// c-10: --finalize runs after the merge, so it never runs the close-out
// check — a landed milestone finalizes whatever is still unresolved.
func TestMilestoneFinalizeSkipsTheCloseoutCheck(t *testing.T) {
	dir, version := finalizeFixture(t, true, "active")
	// Seed on main AND origin/main: committed, so finalize's dirty-tree gate
	// passes, and in the tree from the start, so a check run anywhere in
	// finalize would see it.
	mustGit(t, dir, "merge", "--ff-only", "-q", "origin/main")
	root := filepath.Join(dir, ".dross")
	setMilestonePhases(t, dir, version, []string{"built"})
	ckVerify(t, root, "built", closeoutT0)
	ckSpec(t, root, "src", "[[deferred]]\n  id = \"d0\"\n  text = \"carry me\"\n  target = \"built\"\n")
	mustGit(t, dir, "add", ".dross")
	mustGit(t, dir, "commit", "-q", "-m", "chore(dross): unresolved close-out")
	mustGit(t, dir, "push", "-q", "origin", "main")
	j, err := loadCloseout(root, version)
	if err != nil {
		t.Fatal(err)
	}
	if len(j.Open) < 2 {
		t.Fatalf("fixture is not set up: want a not-done phase and a deferred item open, got %+v", j.Open)
	}

	out := captureStdout(t, func() {
		if err := runCmd(t, Milestone(), "complete", version, "--finalize"); err != nil {
			t.Fatalf("finalize refused an unresolved close-out: %v", err)
		}
	})
	if got := milestoneStatusOf(t, dir, version); got != milestoneStatusComplete {
		t.Errorf("status = %q, want %q", got, milestoneStatusComplete)
	}
	for _, h := range []string{"close-out", headNotDone, headDeferred} {
		if strings.Contains(out, h) {
			t.Errorf("finalize printed close-out output %q:\n%s", h, out)
		}
	}
}

// c-11: an ack whose item has since resolved is stale and stays out of the PR
// body — listing it would claim the phase still rides in unfinished.
func TestMilestoneCompleteLeavesStaleAcksOutOfPRBody(t *testing.T) {
	dir, cap := milestoneOpenFixture(t)
	root := filepath.Join(dir, ".dross")
	setMilestonePhases(t, dir, "v0.9", []string{"p"})
	ckVerify(t, root, "p", closeoutT0)
	if err := runCmd(t, Milestone(), "ack", "phase:p", "v0.9", "--reason", "parked"); err != nil {
		t.Fatalf("ack: %v", err)
	}
	ckPhase(t, root, "p", "complete")

	if err := runCmd(t, Milestone(), "complete", "v0.9"); err != nil {
		t.Fatalf("complete: %v", err)
	}
	if cap.created != 1 {
		t.Fatalf("PR was not opened (created=%d)", cap.created)
	}
	if strings.Contains(cap.body, "phase:p") || strings.Contains(cap.body, "parked") {
		t.Errorf("a stale ack reached the PR body:\n%s", cap.body)
	}
}
