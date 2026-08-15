package cmd

import (
	"strings"
	"testing"
)

// prunableFixture is a repo with exactly one stale milestone branch, present
// locally and on origin — the shape prune is about to delete.
func prunableFixture(t *testing.T) string {
	t.Helper()
	dir := pruneFixture(t)
	mustGit(t, dir, "checkout", "-q", "-b", "milestone/v1.0", "main")
	commitOn(t, dir, "milestone/v1.0", "a.txt", "a\n", "feat: a")
	mustGit(t, dir, "push", "-q", "origin", "milestone/v1.0")
	squashOnto(t, dir, "milestone/v1.0", "feat(squash): v1.0")
	pushMain(t, dir)
	seedCompleteMilestone(t, dir, "v1.0")
	mustGit(t, dir, "checkout", "-q", "main")
	return dir
}

// TestPruneDryRunDeletesNothing is the one property the flag exists for. A dry
// run that deleted anything would be worse than no flag, because the name is a
// promise.
func TestPruneDryRunDeletesNothing(t *testing.T) {
	dir := prunableFixture(t)

	var out string
	if err := runCmdCapturing(t, &out, Milestone(), "prune", "--dry-run"); err != nil {
		t.Fatalf("prune --dry-run: %v", err)
	}
	if !branchExists(t, dir, "milestone/v1.0") {
		t.Error("--dry-run deleted the local branch")
	}
	if !remoteHas(t, dir, "milestone/v1.0") {
		t.Error("--dry-run deleted the branch on origin")
	}
	// A dry run that prints nothing tells the user nothing.
	if !strings.Contains(out, "milestone/v1.0") {
		t.Errorf("--dry-run did not list what it would delete:\n%s", out)
	}
	if !strings.Contains(out, "nothing deleted") {
		t.Errorf("--dry-run did not say it deleted nothing:\n%s", out)
	}
}

// TestPruneRefusesWithoutConsent: the tests run with a non-interactive stdin,
// which is exactly the state a script or a CI job is in. Silently proceeding
// there is the shape the confirmation exists to prevent — a prompt that is
// skipped when nobody is watching is a delay, not a confirmation.
func TestPruneRefusesWithoutConsent(t *testing.T) {
	dir := prunableFixture(t)

	var out string
	err := runCmdCapturing(t, &out, Milestone(), "prune")
	if err == nil {
		t.Fatal("prune deleted branches without a confirmation on a non-interactive stdin")
	}
	if !branchExists(t, dir, "milestone/v1.0") {
		t.Error("the local branch was deleted despite the refusal")
	}
	if !remoteHas(t, dir, "milestone/v1.0") {
		t.Error("the branch on origin was deleted despite the refusal")
	}
	if !strings.Contains(err.Error(), "--yes") {
		t.Errorf("the refusal does not name the way through: %v", err)
	}
	// It still showed the user what it was asking about.
	if !strings.Contains(out, "milestone/v1.0") {
		t.Errorf("the prompt did not name the branches:\n%s", out)
	}
}

// TestPruneYesSkipsThePrompt: the scripted path must not block on stdin, and
// must still do the work.
func TestPruneYesSkipsThePrompt(t *testing.T) {
	dir := prunableFixture(t)

	var out string
	if err := runCmdCapturing(t, &out, Milestone(), "prune", "--yes"); err != nil {
		t.Fatalf("prune --yes: %v", err)
	}
	if branchExists(t, dir, "milestone/v1.0") {
		t.Error("--yes did not delete the local branch")
	}
	if remoteHas(t, dir, "milestone/v1.0") {
		t.Error("--yes did not delete the branch on origin")
	}
}

// TestPruneListsWhatItDeletes compares the previewed set against the deleted
// set, so a preview that named only some of the branches is caught. A partial
// preview is the failure mode a confirmation gate is most likely to develop.
func TestPruneListsWhatItDeletes(t *testing.T) {
	dir := pruneFixture(t)
	for _, v := range []string{"v1.0", "v1.1"} {
		branch := "milestone/" + v
		mustGit(t, dir, "checkout", "-q", "-b", branch, "main")
		commitOn(t, dir, branch, v+".txt", v+"\n", "feat: "+v)
		mustGit(t, dir, "push", "-q", "origin", branch)
		squashOnto(t, dir, branch, "feat(squash): "+v)
		pushMain(t, dir)
		seedCompleteMilestone(t, dir, v)
		mustGit(t, dir, "checkout", "-q", "main")
	}

	var preview string
	if err := runCmdCapturing(t, &preview, Milestone(), "prune", "--dry-run"); err != nil {
		t.Fatalf("prune --dry-run: %v", err)
	}
	var deleted string
	if err := runCmdCapturing(t, &deleted, Milestone(), "prune", "--yes"); err != nil {
		t.Fatalf("prune --yes: %v", err)
	}

	for _, v := range []string{"v1.0", "v1.1"} {
		branch := "milestone/" + v
		if !strings.Contains(preview, branch) {
			t.Errorf("the preview did not name %s, which was then deleted:\n%s", branch, preview)
		}
		if !strings.Contains(deleted, branch) {
			t.Errorf("the delete run did not report %s:\n%s", branch, deleted)
		}
		if branchExists(t, dir, branch) {
			t.Errorf("%s survived the prune", branch)
		}
	}
}

// TestPruneWithNothingStaleSaysSoBeforeAsking: a tidy repo must not be prompted
// to confirm deleting nothing.
func TestPruneWithNothingStaleSaysSoBeforeAsking(t *testing.T) {
	dir := pruneFixture(t)
	_ = dir

	var out string
	if err := runCmdCapturing(t, &out, Milestone(), "prune"); err != nil {
		t.Fatalf("prune on a tidy repo must exit 0: %v", err)
	}
	if !strings.Contains(out, "nothing to prune") {
		t.Errorf("prune did not report an empty set:\n%s", out)
	}
}
