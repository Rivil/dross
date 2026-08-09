package cmd

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/milestone"
)

func TestMilestoneMergedIntoMainReadsAncestry(t *testing.T) {
	t.Run("merged on origin", func(t *testing.T) {
		dir, version := milestoneFinalizeFixture(t, true)
		merged, localOnly, err := milestoneMergedIntoMain(dir, "milestone/"+version, "main")
		if err != nil {
			t.Fatal(err)
		}
		if !merged {
			t.Error("milestone merged into origin/main read as unmerged")
		}
		if localOnly {
			t.Error("origin was reachable; the answer should not be flagged local-only")
		}
	})

	t.Run("not merged", func(t *testing.T) {
		dir, version := milestoneFinalizeFixture(t, false)
		merged, localOnly, err := milestoneMergedIntoMain(dir, "milestone/"+version, "main")
		if err != nil {
			t.Fatal(err)
		}
		if merged {
			t.Error("an unmerged milestone read as merged — is the is-ancestor argument order swapped?")
		}
		if localOnly {
			t.Error("origin was reachable; the answer should not be flagged local-only")
		}
	})
}

// The probe must key on git ancestry, never on milestone.status: a milestone
// left at status="active" after its PR merged is exactly the lie that would
// stack the next milestone onto a branch about to be deleted.
func TestMilestoneMergedIgnoresMilestoneStatus(t *testing.T) {
	dir, version := milestoneFinalizeFixture(t, true)
	path := milestone.FilePath(filepath.Join(dir, ".dross"), version)
	m := &milestone.Milestone{}
	m.Milestone.Version = version
	m.Milestone.Status = "active"
	if err := m.Save(path); err != nil {
		t.Fatal(err)
	}

	merged, _, err := milestoneMergedIntoMain(dir, "milestone/"+version, "main")
	if err != nil {
		t.Fatal(err)
	}
	if !merged {
		t.Error(`status="active" overrode git ancestry; the merge already landed on origin/main`)
	}
}

// Offline is a supported mode. With origin unreachable the probe still answers
// — from this repo's own refs — and says the answer is local-only. Without the
// fallback, `milestone create` / `complete` / `prune` are unusable on a plane.
func TestMilestoneMergedFallsBackOffline(t *testing.T) {
	dir, version := milestoneFinalizeFixture(t, false)
	mustGit(t, dir, "remote", "set-url", "origin", filepath.Join(t.TempDir(), "gone.git"))

	merged, localOnly, err := milestoneMergedIntoMain(dir, "milestone/"+version, "main")
	if err != nil {
		t.Fatalf("offline probe returned an error instead of a local answer: %v", err)
	}
	if !localOnly {
		t.Error("an unreachable origin must be reported as a local-only answer")
	}
	if merged {
		t.Error("refs/heads/main does not contain the milestone; it read as merged")
	}
}

// The discriminating offline shape: origin/main already carries the merge, so
// the stale remote-tracking ref says "merged" while refs/heads/main — this
// repo's own, verifiable state — does not. With the fetch failing, the probe
// must answer from refs/heads and flag it, rather than presenting a snapshot
// of unknown age as a fact about origin.
func TestMilestoneMergedOfflineAnswersFromLocalHeads(t *testing.T) {
	dir, version := milestoneFinalizeFixture(t, true)
	if merged, _, err := milestoneMergedIntoMain(dir, "milestone/"+version, "main"); err != nil || !merged {
		t.Fatalf("precondition: origin/main should carry the merge (merged=%v, err=%v)", merged, err)
	}
	mustGit(t, dir, "remote", "set-url", "origin", filepath.Join(t.TempDir(), "gone.git"))

	merged, localOnly, err := milestoneMergedIntoMain(dir, "milestone/"+version, "main")
	if err != nil {
		t.Fatalf("offline probe errored: %v", err)
	}
	if !localOnly {
		t.Error("an unreachable origin must be reported as a local-only answer")
	}
	if merged {
		t.Error("the answer came from the stale remote-tracking ref, not refs/heads")
	}
}

// A branch origin has never seen is a perfectly normal local branch — answer
// from refs/heads rather than failing on an unknown revision.
func TestMilestoneMergedBranchNeverPushed(t *testing.T) {
	dir, _ := setupMilestoneRepo(t)
	mustGit(t, dir, "push", "-q", "-u", "origin", "main")
	mustGit(t, dir, "branch", "milestone/v0.9")

	merged, localOnly, err := milestoneMergedIntoMain(dir, "milestone/v0.9", "main")
	if err != nil {
		t.Fatalf("unpushed branch produced an error: %v", err)
	}
	if !localOnly {
		t.Error("an origin-unknown branch is a local answer and must say so")
	}
	// Cut from main with no work on top, so main contains it.
	if !merged {
		t.Error("a branch at main's tip is contained in main")
	}
}

// The state a repo is in right after `milestone complete --finalize`: the
// branch is deleted on both sides. A vanished parent is nothing to stack on,
// so it reads as merged. An implementation that errors here makes every later
// `milestone create` fail until current_milestone is hand-cleared.
func TestMilestoneMergedVanishedBranch(t *testing.T) {
	dir, version := milestoneFinalizeFixture(t, true)
	mustGit(t, dir, "push", "-q", "origin", "--delete", "milestone/"+version)
	mustGit(t, dir, "branch", "-D", "milestone/"+version)
	mustGit(t, dir, "fetch", "-q", "--prune", "origin")

	merged, _, err := milestoneMergedIntoMain(dir, "milestone/"+version, "main")
	if err != nil {
		t.Fatalf("a deleted milestone branch produced an error: %v", err)
	}
	if !merged {
		t.Error("a branch that exists nowhere must read as merged")
	}
}

func TestMilestoneMergedRejectsEmptyArgs(t *testing.T) {
	dir, _ := setupMilestoneRepo(t)
	if _, _, err := milestoneMergedIntoMain(dir, "", "main"); err == nil {
		t.Error("expected an error for an empty branch")
	}
	_, _, err := milestoneMergedIntoMain(dir, "milestone/v0.9", "")
	if err == nil {
		t.Fatal("expected an error for an empty main branch")
	}
	if !strings.Contains(err.Error(), "main branch") {
		t.Errorf("error should name the missing main branch: %v", err)
	}
}
