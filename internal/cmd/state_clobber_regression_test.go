package cmd

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/state"
)

// The incident, end to end.
//
// A long-lived branch created before .dross/state.json was untracked still
// carries a copy of it. Switching to that branch replayed the copy over the
// live file, dropping an accumulated history to whatever the stale copy held.
//
// The fixture is built through `dross init`, never by hand-writing .gitignore:
// the claim under test is that the SHIPPED scaffold prevents this, so a
// hand-rolled ignore would prove nothing and let the init path regress unseen.
// Every dross invocation runs in-process through the cobra root — a subprocess
// would resolve the installed binary, which rule r-01 flags as routinely stale.

const incidentLiveEntries = 12

// incidentRepo builds the pre-migration shape: a dross repo whose `legacy`
// branch tracks a 2-entry state.json, with a live untracked copy holding
// incidentLiveEntries. Returns the repo dir and the first live action.
func incidentRepo(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	gitInit(t, dir, "")
	chdir(t, dir)
	if err := runCmd(t, Init()); err != nil {
		t.Fatalf("init: %v", err)
	}
	// The scaffold itself is the thing under test.
	if !checkIgnored(t, dir, drossStateIgnorePath) {
		t.Fatalf("`dross init` should have scaffolded the state.json ignore")
	}
	mustWrite(t, filepath.Join(dir, "README.md"), "base\n")
	mustGit(t, dir, "add", ".")
	mustGit(t, dir, "commit", "-q", "-m", "chore: baseline")

	stPath := filepath.Join(dir, ".dross", state.File)

	// The long-lived branch, cut before the untrack: a 2-entry copy, tracked.
	mustGit(t, dir, "checkout", "-q", "-b", "legacy")
	stale := state.New()
	stale.Version = "0.0.1.0"
	stale.Touch("ancient one")
	stale.Touch("ancient two")
	if err := stale.Save(stPath); err != nil {
		t.Fatal(err)
	}
	mustGit(t, dir, "add", "-f", ".dross/"+state.File)
	mustGit(t, dir, "commit", "-q", "-m", "chore(dross): pre-untrack state copy")
	mustGit(t, dir, "checkout", "-q", "main")

	// The live copy: everything the machine accumulated since.
	live := state.New()
	live.Version = "9.9.9.9"
	first := "live entry 1"
	live.Touch(first)
	for i := 2; i <= incidentLiveEntries; i++ {
		live.Touch("live entry " + string(rune('a'+i)))
	}
	if err := live.Save(stPath); err != nil {
		t.Fatal(err)
	}
	return dir, first
}

// TestStaleBranchCheckoutCannotClobberLiveState is the regression. Against
// pre-milestone code the switch succeeds and the history drops to the stale
// copy's two entries; now the live state survives — either untouched, or
// because the switch was refused outright.
func TestStaleBranchCheckoutCannotClobberLiveState(t *testing.T) {
	dir, first := incidentRepo(t)
	stPath := filepath.Join(dir, ".dross", state.File)

	// A non-zero exit is an allowed outcome: refusing IS the live state
	// surviving. What is not allowed is a switch that took the stale copy.
	switchErr := checkoutBranch(dir, "legacy")

	s, err := state.Load(stPath)
	if err != nil {
		t.Fatalf("the live state should still be readable (switch err: %v): %v", switchErr, err)
	}
	if len(s.History) < incidentLiveEntries {
		t.Errorf("the stale copy clobbered the live one: %d entries, want >= %d",
			len(s.History), incidentLiveEntries)
	}
	if !hasAction(s, first) {
		t.Errorf("the first live action %q is gone: %+v", first, actions(s))
	}
	// The version is part of the same file and travels with it.
	if s.Version != "9.9.9.9" {
		t.Errorf("the version came from the stale branch: %q, want 9.9.9.9", s.Version)
	}

	// Control: with the file deliberately re-tracked, the same switch DOES
	// clobber. Without this the assertions above could pass for any reason —
	// a fixture that never had a clobber to prevent proves nothing.
	t.Run("re-tracked state is still clobbered", func(t *testing.T) {
		mustGit(t, dir, "add", "-f", ".dross/"+state.File)
		mustGit(t, dir, "commit", "-q", "-m", "chore: re-track state.json")
		mustGit(t, dir, "checkout", "-q", "legacy")

		clobbered, err := state.Load(stPath)
		if err != nil {
			t.Fatal(err)
		}
		if len(clobbered.History) >= incidentLiveEntries {
			t.Errorf("expected the tracked copy to be replaced by legacy's 2-entry one, got %d entries",
				len(clobbered.History))
		}
	})
}

// TestIncidentStateNeverEntersIndex: the file must stay out of the index across
// the whole scaffold-and-commit sequence. One `git add .dross` that picks it up
// puts the incident back.
func TestIncidentStateNeverEntersIndex(t *testing.T) {
	dir, _ := incidentRepo(t)

	// The directory form every dross prompt uses, plus a phase artefact to
	// prove the add is doing real work rather than being a no-op.
	mustWrite(t, filepath.Join(dir, ".dross", "phases", "auth", "spec.toml"), "id = \"auth\"\n")
	mustGit(t, dir, "add", ".dross")
	mustGit(t, dir, "commit", "-q", "-m", "chore(dross): phase artefact")

	if tracked := mustGit(t, dir, "ls-files", ".dross/"+state.File); tracked != "" {
		t.Errorf("state.json re-entered the index: %q", tracked)
	}
	if names := mustGit(t, dir, "show", "--name-only", "--format=", "HEAD"); strings.Contains(names, state.File) {
		t.Errorf("the commit carries state.json:\n%s", names)
	}
	if !strings.Contains(mustGit(t, dir, "ls-tree", "-r", "--name-only", "HEAD"), ".dross/phases/auth/spec.toml") {
		t.Error("the phase artefact should have been committed — the add must not be a blanket no-op")
	}
}

// TestIncidentRecoverKeepsLiveHistory: recovery is the other half of the
// incident. It resets the base to origin and restores a .dross/ tree from a
// pre-merge commit, so both steps have to leave the live state alone.
//
// The complementary case — recovery pointed at a SHA that itself carries a
// stale tracked copy — is TestRecoveryDoesNotRestoreStateJSON in
// ship_recover_test.go, which builds exactly that SHA.
func TestIncidentRecoverKeepsLiveHistory(t *testing.T) {
	dir, _, _ := divergedCompleteFixture(t)
	stubPRMerged(t, true)

	stPath := filepath.Join(dir, ".dross", state.File)
	live, err := state.Load(stPath)
	if err != nil {
		t.Fatal(err)
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

	if err := runCmd(t, Phase(), "complete", "--recover"); err != nil {
		t.Fatalf("complete --recover: %v", err)
	}

	s, err := state.Load(stPath)
	if err != nil {
		t.Fatal(err)
	}
	if !hasAction(s, first) {
		t.Errorf("recovery lost the live history: %+v", actions(s))
	}
	if hasAction(s, "ancient") {
		t.Errorf("recovery restored a stale copy: %+v", actions(s))
	}
	if s.Version != "9.9.9.9" {
		t.Errorf("recovery reset the version to %q", s.Version)
	}
	if tracked := mustGit(t, dir, "ls-files", ".dross/"+state.File); tracked != "" {
		t.Errorf("recovery re-tracked state.json: %q", tracked)
	}
	if names := mustGit(t, dir, "show", "--name-only", "--format=", "HEAD"); strings.Contains(names, state.File) {
		t.Errorf("the recovery commit carries state.json:\n%s", names)
	}
}
