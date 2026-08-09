package cmd

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/state"
)

// The incident this phase closes, end to end.
//
// On the state-json-branch-safety ship, `gh pr merge --squash --delete-branch`
// did its own raw checkout of the base branch as part of deleting the source
// branch. The base still tracked a copy of `.dross/state.json` from before that
// file was untracked, so the checkout replayed a two-entry copy over a live one
// holding the machine's whole history — outside every guard dross owns, because
// dross never issued the checkout.
//
// Two things are asserted here, and they need each other:
//
//   - TestShipToCompleteKeepsLiveState drives the real ship → merge → complete
//     flow and asserts the live copy comes out whole, with the completion record
//     that `dross phase complete` — not ship — writes.
//   - The control sub-test performs the unguarded checkout by hand and asserts
//     it DOES truncate the live copy. Without a demonstrated clobber, the
//     assertions above would pass on a fixture that never had one to survive.
//
// Scope honesty: no Go test can run `gh pr merge --delete-branch`, so the main
// flow cannot reproduce the provider's checkout directly. The executable guard
// against reintroducing it is TestShipPromptNoUnguardedSwitch, which greps the
// prompt. This test covers the half that is reachable in-process: the live copy
// survives dross's own flow, and the completion record is written by the command
// that confirms the merge.

// simulateSquashMerge performs the provider's squash-merge on the bare origin
// from a SEPARATE clone. Doing it in the live repo would mean switching branches
// there, which is the very operation under test — the simulation must not be the
// thing that clobbers.
func simulateSquashMerge(t *testing.T, dir, phaseBranch, base string) {
	t.Helper()
	remoteURL := mustGit(t, dir, "remote", "get-url", "origin")

	sim := t.TempDir()
	c := exec.Command("git", "clone", "-q", remoteURL, sim)
	if out, err := c.CombinedOutput(); err != nil {
		t.Fatalf("clone origin for squash simulation: %v\n%s", err, out)
	}
	for _, args := range [][]string{
		{"config", "user.email", "test@example.com"},
		{"config", "user.name", "Test"},
		{"config", "commit.gpgsign", "false"},
		{"config", "gc.auto", "0"},
	} {
		mustGit(t, sim, args...)
	}
	mustGit(t, sim, "checkout", "-q", "-b", "squash-sim", "origin/"+base)
	// The squash carries the phase's tree — src/ and its .dross/ records,
	// including the changes.json PR number ship committed post-push. It does
	// NOT carry state.json: that file is machine-local and rides nothing.
	mustGit(t, sim, "checkout", "origin/"+phaseBranch, "--", "src/", ".dross/")
	mustGit(t, sim, "add", "-A")
	mustGit(t, sim, "commit", "-q", "-m", "feat(squash): "+phaseBranch)
	mustGit(t, sim, "push", "-q", "--force", "origin", "squash-sim:"+base)
	mustGit(t, dir, "fetch", "-q", "origin")
}

// seedLiveHistory writes the machine-local shape a real repo carries: a long
// accumulated history at a version nothing else in the fixture uses, so a
// clobber is visible in three independent ways (entry count, first action,
// version). Returns the first action.
func seedLiveHistory(t *testing.T, stPath string) string {
	t.Helper()
	live, err := state.Load(stPath)
	if err != nil {
		t.Fatalf("load live state: %v", err)
	}
	live.Version = "9.9.9.9"
	first := "live entry 1"
	live.Touch(first)
	for i := 2; i <= incidentLiveEntries; i++ {
		live.Touch("live entry " + string(rune('a'+i)))
	}
	if err := live.Save(stPath); err != nil {
		t.Fatal(err)
	}
	return first
}

// TestShipToCompleteKeepsLiveState (c-4) drives the whole hardened flow: a real
// `dross ship` against the mock provider, the provider's squash-merge simulated
// on the bare origin, then `dross phase complete` with the PR authoritatively
// merged. The live machine-local state.json must come out the other side whole.
//
// It fails against pre-phase code at the mid-flow checkpoint: ship used to write
// the completion record itself, before the PR existed, asserting a completion
// nothing had confirmed.
func TestShipToCompleteKeepsLiveState(t *testing.T) {
	dir := shipFixture(t, "https://forge.example/me/p.git")
	shipMockFlow(t, dir)
	stubPRMerged(t, true)

	// The base has to exist on origin: ship pushes only the phase branch, and
	// both the squash simulation and complete's fast-forward read origin/main.
	// Pushed by refspec so nothing switches branches here.
	mustGit(t, dir, "push", "-q", "origin", "main:main")
	mustGit(t, dir, "fetch", "-q", "origin")

	stPath := filepath.Join(dir, ".dross", state.File)
	first := seedLiveHistory(t, stPath)

	// 1) Ship — pushes phase/x, opens PR #99, records it on the branch.
	if err := runCmd(t, Ship()); err != nil {
		t.Fatalf("ship: %v", err)
	}

	// Mid-flow checkpoint: ship marks the phase shipped and stops. The PR is
	// open, not merged — nothing may claim the phase completed yet.
	mid, err := state.Load(stPath)
	if err != nil {
		t.Fatalf("live state should be readable after ship: %v", err)
	}
	if hasAction(mid, "completed x") {
		t.Error("ship must not write the completion record — the PR is not merged yet")
	}
	if mid.CurrentPhase != "x" || mid.CurrentPhaseStatus != "shipped" {
		t.Errorf("after ship the phase should read x/shipped, got %q/%q", mid.CurrentPhase, mid.CurrentPhaseStatus)
	}

	// 2) The provider squash-merges the PR onto origin/main.
	simulateSquashMerge(t, dir, "phase/x", "main")

	// 3) Complete — the only writer of the completion record.
	//
	// A non-zero exit would be an allowed outcome here: refusing IS the live
	// state surviving, per the switchbranch guard. A truncated state.json is
	// not. This fixture's base carries no tracked copy, so the run should
	// genuinely finish.
	completeErr := runCmd(t, Phase(), "complete", "x")

	s, err := state.Load(stPath)
	if err != nil {
		t.Fatalf("the live state should still be readable (complete err: %v): %v", completeErr, err)
	}
	if len(s.History) < incidentLiveEntries {
		t.Errorf("live history was truncated: %d entries, want >= %d (complete err: %v)",
			len(s.History), incidentLiveEntries, completeErr)
	}
	if !hasAction(s, first) {
		t.Errorf("the first live action %q is gone: %+v", first, actions(s))
	}
	if s.Version != "9.9.9.9" {
		t.Errorf("the version was replaced: %q, want 9.9.9.9", s.Version)
	}
	if completeErr != nil {
		t.Fatalf("complete should finish on an untracked base: %v", completeErr)
	}
	if !hasAction(s, "completed x") {
		t.Errorf("`dross phase complete` should have written the completion record: %+v", actions(s))
	}
	if s.CurrentPhase != "" {
		t.Errorf("complete should clear current_phase, got %q", s.CurrentPhase)
	}
}

// TestRawCheckoutStillClobbersLiveState is the mandatory control: the
// unguarded `git checkout <base>` that `gh pr merge --delete-branch` performs
// DOES destroy the live copy when the base still tracks one. git declines to
// overwrite an untracked working-tree file only when that file is not ignored;
// state.json is ignored, so git overwrites it without a word.
//
// Without this, the survival assertions above would be vacuous — a fixture that
// never had a clobber to prevent proves nothing about preventing one.
func TestRawCheckoutStillClobbersLiveState(t *testing.T) {
	dir := shipFixture(t, "https://forge.example/me/p.git")
	stPath := filepath.Join(dir, ".dross", state.File)

	// The pre-untrack legacy shape: main's tree carries a two-entry copy.
	// Branches cut before the untrack still look exactly like this, and history
	// was deliberately not rewritten (locked migration_scope).
	mustGit(t, dir, "checkout", "-q", "main")
	stale := state.New()
	stale.Version = "0.0.1.0"
	stale.Touch("ancient one")
	stale.Touch("ancient two")
	if err := stale.Save(stPath); err != nil {
		t.Fatal(err)
	}
	mustGit(t, dir, "add", "-f", ".dross/"+state.File)
	mustGit(t, dir, "commit", "-q", "-m", "chore(dross): pre-untrack state copy")
	if !strings.Contains(mustGit(t, dir, "ls-tree", "-r", "--name-only", "main"), ".dross/"+state.File) {
		t.Fatal("precondition: main should track a copy of state.json")
	}

	// Back on the phase branch, which carries no copy — so the switch away
	// deleted the file, and the machine writes its live one fresh.
	mustGit(t, dir, "checkout", "-q", "phase/x")
	live := state.New()
	live.Version = "9.9.9.9"
	live.Touch("live entry 1")
	for i := 2; i <= incidentLiveEntries; i++ {
		live.Touch("live entry " + string(rune('a'+i)))
	}
	if err := live.Save(stPath); err != nil {
		t.Fatal(err)
	}

	// The raw checkout the provider's --delete-branch performs.
	mustGit(t, dir, "checkout", "-q", "main")

	clobbered, err := state.Load(stPath)
	if err != nil {
		t.Fatalf("load after the raw checkout: %v", err)
	}
	if len(clobbered.History) >= incidentLiveEntries {
		t.Errorf("expected the tracked two-entry copy to replace the live one, got %d entries — "+
			"the fixture no longer reproduces the clobber, so the survival test above proves nothing",
			len(clobbered.History))
	}
	if clobbered.Version != "0.0.1.0" {
		t.Errorf("expected the stale version to have won, got %q", clobbered.Version)
	}

	// …and dross's own switch refuses it, which is why the flow above survives.
	mustGit(t, dir, "checkout", "-q", "phase/x")
	if err := live.Save(stPath); err != nil {
		t.Fatal(err)
	}
	if err := checkoutBranch(dir, "main"); err == nil {
		t.Error("dross's guarded checkout must refuse the switch that git performs silently")
	}
	survived, err := state.Load(stPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(survived.History) < incidentLiveEntries {
		t.Errorf("the guarded refusal must leave the live copy alone, got %d entries", len(survived.History))
	}
}
