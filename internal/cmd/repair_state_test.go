package cmd

import (
	"path/filepath"
	"testing"
)

// repairStateFixture builds a repo with two phase-completion commits on
// main (one plain, one carrying a GitHub squash-merge " (#N)" suffix) plus
// one ordinary non-phase commit, then checks out a phase/<id> branch with a
// spec.toml recording a milestone.
func repairStateFixture(t *testing.T) (repoDir, root string) {
	t.Helper()
	dir := t.TempDir()
	gitInit(t, dir, "")

	mustWrite(t, filepath.Join(dir, "README.md"), "base\n")
	mustGit(t, dir, "add", ".")
	mustGit(t, dir, "commit", "-q", "-m", "chore: baseline")

	mustWrite(t, filepath.Join(dir, "src", "a.go"), "package a\n")
	mustGit(t, dir, "add", ".")
	mustGit(t, dir, "commit", "-q", "-m", "phase alpha: Alpha Phase")

	mustWrite(t, filepath.Join(dir, "src", "b.go"), "package b\n")
	mustGit(t, dir, "add", ".")
	mustGit(t, dir, "commit", "-q", "-m", "chore: unrelated tidy-up")

	mustWrite(t, filepath.Join(dir, "src", "c.go"), "package c\n")
	mustGit(t, dir, "add", ".")
	mustGit(t, dir, "commit", "-q", "-m", "phase beta: Beta Phase (#42)")

	root = filepath.Join(dir, ".dross")
	mustWrite(t, filepath.Join(root, "phases", "beta", "spec.toml"),
		"[phase]\nid = \"beta\"\ntitle = \"Beta Phase\"\nmilestone = \"v2.0\"\n")
	// gamma is scaffolded but never shipped — no "phase gamma: ..." commit
	// exists anywhere in the log, exercising the "even when no commit marker
	// references it yet" half of the contract.
	mustWrite(t, filepath.Join(root, "phases", "gamma", "spec.toml"),
		"[phase]\nid = \"gamma\"\ntitle = \"Gamma Phase\"\nmilestone = \"v2.0\"\n")
	mustGit(t, dir, "add", ".")
	mustGit(t, dir, "commit", "-q", "-m", "chore: scaffold beta + gamma")

	mustGit(t, dir, "checkout", "-q", "-b", "phase/gamma")
	return dir, root
}

func TestReconstructStateBuildsHistoryFromPhaseCommits(t *testing.T) {
	dir, root := repairStateFixture(t)

	s, err := reconstructState(dir, root, "main")
	if err != nil {
		t.Fatalf("reconstructState: %v", err)
	}
	if len(s.History) != 2 {
		t.Fatalf("expected 2 history entries (non-phase commit skipped), got %d: %+v", len(s.History), s.History)
	}
	if s.History[0].Action != "phase alpha shipped: Alpha Phase" {
		t.Errorf("history[0] = %q, want prefix for alpha", s.History[0].Action)
	}
	// The GitHub squash-merge " (#42)" suffix must be stripped from the title.
	if s.History[1].Action != "phase beta shipped: Beta Phase" {
		t.Errorf("history[1] = %q, want #42 suffix stripped", s.History[1].Action)
	}
}

func TestReconstructStateSetsCurrentPhaseFromBranch(t *testing.T) {
	dir, root := repairStateFixture(t)

	s, err := reconstructState(dir, root, "main")
	if err != nil {
		t.Fatalf("reconstructState: %v", err)
	}
	// gamma has no "phase gamma: ..." commit anywhere in the log — CurrentPhase
	// must still come from the checked-out branch name alone.
	if s.CurrentPhase != "gamma" {
		t.Errorf("CurrentPhase = %q, want gamma", s.CurrentPhase)
	}
	if s.CurrentMilestone != "v2.0" {
		t.Errorf("CurrentMilestone = %q, want v2.0 (from gamma's spec.toml)", s.CurrentMilestone)
	}
}

func TestReconstructStateSkipsNonMatchingCommits(t *testing.T) {
	dir, root := repairStateFixture(t)

	s, err := reconstructState(dir, root, "main")
	if err != nil {
		t.Fatalf("reconstructState: %v", err)
	}
	for _, h := range s.History {
		if h.Action == "chore: unrelated tidy-up" || h.Action == "chore: scaffold beta + gamma" {
			t.Errorf("non-phase commit leaked into history: %q", h.Action)
		}
	}
}
