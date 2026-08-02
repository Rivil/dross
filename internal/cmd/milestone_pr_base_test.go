package cmd

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/milestone"
)

// stackedPRFixture layers a real milestone/v1.1 -> origin topology under
// milestoneOpenFixture's mock provider, and writes a v1.2 milestone recording
// v1.1 as its base. merged simulates the v1.1->main merge on origin.
func stackedPRFixture(t *testing.T, base string, merged bool) (string, *msPRCapture) {
	t.Helper()
	dir, cap := milestoneOpenFixture(t)
	// Commit the fixture's project settings on main before branching: they are
	// still uncommitted, and a checkout would carry them onto the milestone
	// branch and then lose them on the way back.
	mustGit(t, dir, "add", "-A")
	mustGit(t, dir, "commit", "-q", "-m", "project settings")
	mustGit(t, dir, "push", "-q", "-u", "origin", "main")
	mustGit(t, dir, "branch", "milestone/v1.1")
	mustGit(t, dir, "checkout", "-q", "milestone/v1.1")
	mustWrite(t, filepath.Join(dir, "v11work.txt"), "x\n")
	mustGit(t, dir, "add", ".")
	mustGit(t, dir, "commit", "-q", "-m", "v1.1 work")
	mustGit(t, dir, "push", "-q", "-u", "origin", "milestone/v1.1")

	mustGit(t, dir, "branch", "milestone/v1.2", "milestone/v1.1")
	mustGit(t, dir, "push", "-q", "-u", "origin", "milestone/v1.2")

	if merged {
		mustGit(t, dir, "checkout", "-q", "-b", "mergetmp", "main")
		mustGit(t, dir, "merge", "--no-ff", "-q", "-m", "merge v1.1", "milestone/v1.1")
		mustGit(t, dir, "push", "-q", "origin", "mergetmp:main")
		mustGit(t, dir, "checkout", "-q", "main")
		mustGit(t, dir, "branch", "-D", "mergetmp")
	}
	mustGit(t, dir, "checkout", "-q", "main")
	mustGit(t, dir, "fetch", "-q", "origin")

	m := &milestone.Milestone{}
	m.Milestone.Version = "v1.2"
	m.Milestone.Status = "active"
	m.Milestone.Base = base
	if err := m.Save(milestone.FilePath(filepath.Join(dir, ".dross"), "v1.2")); err != nil {
		t.Fatal(err)
	}
	return dir, cap
}

// The stacked PR targets its recorded parent while that parent is unmerged, so
// the diff is v1.2's own commits rather than v1.1's replayed as its own.
func TestMilestoneCompleteTargetsUnmergedParent(t *testing.T) {
	_, cap := stackedPRFixture(t, "milestone/v1.1", false)

	if err := runCmd(t, Milestone(), "complete", "v1.2"); err != nil {
		t.Fatalf("open: %v", err)
	}
	if cap.base != "milestone/v1.1" || cap.head != "milestone/v1.2" {
		t.Errorf("PR base/head = %q/%q; want milestone/v1.1 / milestone/v1.2", cap.base, cap.head)
	}
}

// Once the parent has landed on main, the PR retargets — never a branch that
// `milestone complete --finalize` is about to delete.
func TestMilestoneCompleteRetargetsMainOnceParentMerged(t *testing.T) {
	_, cap := stackedPRFixture(t, "milestone/v1.1", true)

	if err := runCmd(t, Milestone(), "complete", "v1.2"); err != nil {
		t.Fatalf("open: %v", err)
	}
	if cap.base != "main" {
		t.Errorf("PR base = %q; a merged parent must not be targeted", cap.base)
	}
}

// A PR base has to exist on the forge. With the parent deleted on origin the
// command targets main rather than erroring on a dead ref.
func TestMilestoneCompleteTargetsMainWhenParentDeletedOnOrigin(t *testing.T) {
	dir, cap := stackedPRFixture(t, "milestone/v1.1", false)
	mustGit(t, dir, "push", "-q", "origin", "--delete", "milestone/v1.1")
	mustGit(t, dir, "fetch", "-q", "--prune", "origin")

	if err := runCmd(t, Milestone(), "complete", "v1.2"); err != nil {
		t.Fatalf("open with a parent deleted on origin: %v", err)
	}
	if cap.base != "main" {
		t.Errorf("PR base = %q; a parent gone from origin must not be targeted", cap.base)
	}
}

// Every milestone shipped before v1.2 records no base, and reads as main.
func TestMilestoneCompleteNoRecordedBaseTargetsMain(t *testing.T) {
	_, cap := stackedPRFixture(t, "", false)

	if err := runCmd(t, Milestone(), "complete", "v1.2"); err != nil {
		t.Fatalf("open: %v", err)
	}
	if cap.base != "main" {
		t.Errorf("PR base = %q; an unrecorded base reads as main", cap.base)
	}
}

// The 409 duplicate path is the one people read when re-running the command —
// it has to name the base the PR actually targets, not a hardcoded main.
func TestMilestoneCompleteIdempotentPathReportsResolvedBase(t *testing.T) {
	_, cap := stackedPRFixture(t, "milestone/v1.1", false)

	if err := runCmd(t, Milestone(), "complete", "v1.2"); err != nil {
		t.Fatalf("first open: %v", err)
	}
	out := captureStdout(t, func() {
		if err := runCmd(t, Milestone(), "complete", "v1.2"); err != nil {
			t.Fatalf("rerun should be idempotent: %v", err)
		}
	})
	if !strings.Contains(out, "milestone/v1.2 -> milestone/v1.1") {
		t.Errorf("the already-open line must report the resolved base:\n%s", out)
	}
	if cap.created != 1 {
		t.Errorf("expected exactly one PR created, got %d", cap.created)
	}
}

// A milestone recording its own branch as base would open a PR against itself.
// Refuse rather than let the provider produce something incoherent.
func TestMilestoneCompleteRefusesSelfTargetedBase(t *testing.T) {
	_, cap := stackedPRFixture(t, "milestone/v1.2", false)

	err := runCmd(t, Milestone(), "complete", "v1.2")
	if err == nil {
		t.Fatal("expected a refusal for a self-targeted base")
	}
	if !strings.Contains(err.Error(), "milestone/v1.2") {
		t.Errorf("the refusal should name the branch: %v", err)
	}
	if cap.posts != 0 {
		t.Errorf("no PR should have been attempted, got %d POSTs", cap.posts)
	}
}
