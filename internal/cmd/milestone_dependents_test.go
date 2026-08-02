package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/milestone"
)

// writeMilestoneWithBase drops a milestone toml recording `base`, the fact the
// delete gate reads.
func writeMilestoneWithBase(t *testing.T, dir, version, base string) {
	t.Helper()
	m := &milestone.Milestone{}
	m.Milestone.Version = version
	m.Milestone.Status = "active"
	m.Milestone.Base = base
	if err := m.Save(milestone.FilePath(filepath.Join(dir, ".dross"), version)); err != nil {
		t.Fatal(err)
	}
}

// stackedDeleteFixture builds main <- milestone/v1.2 <- milestone/v1.3, with
// v1.2's work already landed on main and v1.3 still open on top of it. squash
// lands v1.2 the way a forge squash-merge does (so the stale detector names it,
// which is what `prune` acts on); otherwise it lands as a merge commit on
// origin only, leaving local main behind for `--finalize` to fast-forward.
func stackedDeleteFixture(t *testing.T, squash bool) string {
	t.Helper()
	dir := pruneFixture(t)
	// Recorded before the branching and committed on main, so every later
	// checkout carries them and no command meets a dirty tree.
	writeMilestoneWithBase(t, dir, "v1.2", "main")
	writeMilestoneWithBase(t, dir, "v1.3", "milestone/v1.2")
	mustGit(t, dir, "add", "-A")
	mustGit(t, dir, "commit", "-q", "-m", "chore: milestone records")
	mustGit(t, dir, "push", "-q", "origin", "main")

	mustGit(t, dir, "checkout", "-q", "-b", "milestone/v1.2", "main")
	commitOn(t, dir, "milestone/v1.2", "v12.txt", "v12\n", "feat: v1.2 work")
	mustGit(t, dir, "push", "-q", "origin", "milestone/v1.2")

	mustGit(t, dir, "checkout", "-q", "-b", "milestone/v1.3", "milestone/v1.2")
	commitOn(t, dir, "milestone/v1.3", "v13.txt", "v13\n", "feat: v1.3 work")
	mustGit(t, dir, "push", "-q", "origin", "milestone/v1.3")

	if squash {
		squashOnto(t, dir, "milestone/v1.2", "feat(squash): v1.2")
		mustGit(t, dir, "push", "-q", "origin", "main")
	} else {
		mustGit(t, dir, "checkout", "-q", "-b", "mergetmp", "main")
		mustGit(t, dir, "merge", "--no-ff", "-q", "-m", "merge v1.2", "milestone/v1.2")
		mustGit(t, dir, "push", "-q", "origin", "mergetmp:main")
		mustGit(t, dir, "checkout", "-q", "main")
		mustGit(t, dir, "branch", "-D", "mergetmp")
	}

	mustGit(t, dir, "checkout", "-q", "main")
	mustGit(t, dir, "fetch", "-q", "origin")
	return dir
}

// The core protection: a branch an unmerged stacked child still sits on is not
// deleted, however stale it looks on its own.
func TestPruneRefusesBranchWithUnmergedDependent(t *testing.T) {
	dir := stackedDeleteFixture(t, true)

	err := runCmd(t, Milestone(), "prune")
	if err == nil {
		t.Fatal("prune deleted a branch an unmerged milestone is stacked on")
	}
	if !strings.Contains(err.Error(), "v1.3") {
		t.Errorf("the refusal must name the dependent milestone: %v", err)
	}
	if !branchExists(t, dir, "milestone/v1.2") {
		t.Error("local milestone/v1.2 was deleted despite the refusal")
	}
	if !remoteHas(t, dir, "milestone/v1.2") {
		t.Error("origin/milestone/v1.2 was deleted despite the refusal")
	}
}

func TestFinalizeRefusesBranchWithUnmergedDependent(t *testing.T) {
	dir := stackedDeleteFixture(t, false)
	mainBefore := mustGit(t, dir, "rev-parse", "main")

	err := runCmd(t, Milestone(), "complete", "v1.2", "--finalize")
	if err == nil {
		t.Fatal("finalize deleted a branch an unmerged milestone is stacked on")
	}
	if !strings.Contains(err.Error(), "v1.3") {
		t.Errorf("the refusal must name the dependent milestone: %v", err)
	}
	if !branchExists(t, dir, "milestone/v1.2") {
		t.Error("local milestone/v1.2 was deleted despite the refusal")
	}
	if !remoteHas(t, dir, "milestone/v1.2") {
		t.Error("origin/milestone/v1.2 was deleted despite the refusal")
	}
	if got := mustGit(t, dir, "rev-parse", "main"); got != mainBefore {
		t.Error("local main advanced on a refused finalize")
	}
}

// A gate keyed on "a dependent record exists" rather than "an unmerged
// dependent exists" would wedge the repo permanently — a merged child keeps its
// recorded base forever.
func TestPruneDeletesOnceDependentHasMerged(t *testing.T) {
	dir := stackedDeleteFixture(t, true)
	// Land v1.3 on origin/main too.
	mustGit(t, dir, "checkout", "-q", "-b", "mergetmp", "main")
	mustGit(t, dir, "merge", "--no-ff", "-q", "-m", "merge v1.3", "milestone/v1.3")
	mustGit(t, dir, "push", "-q", "origin", "mergetmp:main")
	mustGit(t, dir, "checkout", "-q", "main")
	mustGit(t, dir, "branch", "-D", "mergetmp")
	mustGit(t, dir, "fetch", "-q", "origin")

	if err := runCmd(t, Milestone(), "prune"); err != nil {
		t.Fatalf("prune should proceed once the dependent has merged: %v", err)
	}
	if branchExists(t, dir, "milestone/v1.2") {
		t.Error("local milestone/v1.2 should have been pruned")
	}
	if remoteHas(t, dir, "milestone/v1.2") {
		t.Error("origin/milestone/v1.2 should have been pruned")
	}
}

// Every milestone shipped before v1.2 records no base, so it is a dependent of
// nothing — otherwise the gate would block every prune in every repo.
func TestNoRecordedBaseIsNeverADependent(t *testing.T) {
	dir := stackedDeleteFixture(t, true)
	writeMilestoneWithBase(t, dir, "v1.3", "")

	deps, err := dependentMilestones(filepath.Join(dir, ".dross"), dir, "milestone/v1.2", "main")
	if err != nil {
		t.Fatal(err)
	}
	if len(deps) != 0 {
		t.Errorf("an unrecorded base made %v a dependent", deps)
	}
}

// A milestone naming its own branch as base must not block its own deletion.
func TestSelfBaseDoesNotSelfBlock(t *testing.T) {
	dir := stackedDeleteFixture(t, true)
	writeMilestoneWithBase(t, dir, "v1.3", "milestone/v1.3")

	deps, err := dependentMilestones(filepath.Join(dir, ".dross"), dir, "milestone/v1.3", "main")
	if err != nil {
		t.Fatal(err)
	}
	if len(deps) != 0 {
		t.Errorf("a self-recorded base blocked its own branch: %v", deps)
	}
}

// Fail closed. An unreadable toml is indistinguishable from "no dependents",
// and the consequence of guessing is an irreversible remote delete.
func TestPruneRefusesOnUnreadableMilestoneToml(t *testing.T) {
	dir := stackedDeleteFixture(t, true)
	broken := filepath.Join(dir, ".dross", "milestones", "v1.4.toml")
	if err := os.WriteFile(broken, []byte("[milestone\n  version = ***\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := runCmd(t, Milestone(), "prune")
	if err == nil {
		t.Fatal("prune deleted on a short scan; an unreadable record must fail closed")
	}
	if !strings.Contains(err.Error(), "v1.4") {
		t.Errorf("the error should name the unreadable record: %v", err)
	}
	if !branchExists(t, dir, "milestone/v1.2") {
		t.Error("local milestone/v1.2 was deleted despite the failed scan")
	}
}

// mergedIntoParentFixture: milestone/v1.2 is stacked on milestone/v1.1 and has
// merged into it on origin — but main has NOT advanced. This is the stacking
// model's second half, and a finalize guard fixed on origin/main refuses it
// forever.
func mergedIntoParentFixture(t *testing.T) string {
	t.Helper()
	dir := pruneFixture(t)
	writeMilestoneWithBase(t, dir, "v1.2", "milestone/v1.1")
	mustGit(t, dir, "add", "-A")
	mustGit(t, dir, "commit", "-q", "-m", "chore: milestone records")
	mustGit(t, dir, "push", "-q", "origin", "main")

	mustGit(t, dir, "checkout", "-q", "-b", "milestone/v1.1", "main")
	commitOn(t, dir, "milestone/v1.1", "v11.txt", "v11\n", "feat: v1.1 work")
	mustGit(t, dir, "push", "-q", "origin", "milestone/v1.1")

	mustGit(t, dir, "checkout", "-q", "-b", "milestone/v1.2", "milestone/v1.1")
	commitOn(t, dir, "milestone/v1.2", "v12.txt", "v12\n", "feat: v1.2 work")
	mustGit(t, dir, "push", "-q", "origin", "milestone/v1.2")

	// v1.2 -> v1.1 merge, on origin and locally: the parent received it.
	mustGit(t, dir, "checkout", "-q", "milestone/v1.1")
	mustGit(t, dir, "merge", "--no-ff", "-q", "-m", "merge v1.2 into v1.1", "milestone/v1.2")
	mustGit(t, dir, "push", "-q", "origin", "milestone/v1.1")

	mustGit(t, dir, "checkout", "-q", "main")
	mustGit(t, dir, "fetch", "-q", "origin")
	return dir
}

func TestFinalizeAcceptsMergeIntoRecordedParent(t *testing.T) {
	dir := mergedIntoParentFixture(t)
	mainBefore := mustGit(t, dir, "rev-parse", "main")

	out := captureStdout(t, func() {
		if err := runCmd(t, Milestone(), "complete", "v1.2", "--finalize"); err != nil {
			t.Fatalf("a guard fixed on origin/main refuses this forever: %v", err)
		}
	})

	if branchExists(t, dir, "milestone/v1.2") {
		t.Error("local milestone/v1.2 should have been deleted")
	}
	if remoteHas(t, dir, "milestone/v1.2") {
		t.Error("origin/milestone/v1.2 should have been deleted")
	}
	// main did not receive this merge, so it must not have moved.
	if got := mustGit(t, dir, "rev-parse", "main"); got != mainBefore {
		t.Errorf("local main advanced to %s; the merge landed on the parent, not main", got)
	}
	if !strings.Contains(out, "milestone/v1.1") {
		t.Errorf("the finalize line must name the branch that received the merge:\n%s", out)
	}
	if strings.Contains(out, "main is at origin") {
		t.Errorf("the output claims main advanced when it did not:\n%s", out)
	}
}
