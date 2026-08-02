package cmd

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/state"
)

// phaseCheckoutClobberRepo builds the pre-migration shape around a PHASE
// branch: `phase/legacy` tracks a 2-entry state.json, HEAD is on main, and the
// live untracked copy holds incidentLiveEntries. This is the shape the ship
// prompt used to walk into with a raw `git checkout`.
func phaseCheckoutClobberRepo(t *testing.T) (string, string) {
	t.Helper()
	return checkoutClobberRepo(t, "phase/legacy")
}

// checkoutClobberRepo is the same fixture parameterised by branch name, so the
// non-phase target `dross checkout` serves gets the identical shape rather than
// a near-miss of it.
func checkoutClobberRepo(t *testing.T, branch string) (string, string) {
	t.Helper()
	dir := t.TempDir()
	gitInit(t, dir, "")
	chdir(t, dir)
	if err := runCmd(t, Init()); err != nil {
		t.Fatalf("init: %v", err)
	}
	mustWrite(t, filepath.Join(dir, "README.md"), "base\n")
	mustGit(t, dir, "add", ".")
	mustGit(t, dir, "commit", "-q", "-m", "chore: baseline")

	stPath := filepath.Join(dir, ".dross", state.File)

	mustGit(t, dir, "checkout", "-q", "-b", branch)
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

// TestPhaseCheckoutRefusesClobber (c-1): the subcommand exists so no prompt has
// to type a raw `git checkout phase/<id>`. Swapping checkoutBranch for a bare
// git checkout here makes the switch succeed and takes the live history with
// it — which is precisely the hole the ship prompt still has.
func TestPhaseCheckoutRefusesClobber(t *testing.T) {
	dir, first := phaseCheckoutClobberRepo(t)

	err := runCmd(t, Phase(), "checkout", "legacy")
	if err == nil {
		t.Fatal("expected a refusal: phase/legacy tracks a copy of the live state.json")
	}
	if !strings.Contains(err.Error(), "refusing to switch") {
		t.Errorf("the refusal should be guardLiveState's, got: %v", err)
	}

	// The live copy is untouched — the guard never moves or deletes it.
	s, err := state.Load(filepath.Join(dir, ".dross", state.File))
	if err != nil {
		t.Fatalf("the live state should still be readable: %v", err)
	}
	if len(s.History) < incidentLiveEntries {
		t.Errorf("live history was clobbered: %d entries, want >= %d", len(s.History), incidentLiveEntries)
	}
	if !hasAction(s, first) {
		t.Errorf("the first live action %q is gone: %+v", first, actions(s))
	}
	if s.Version != "9.9.9.9" {
		t.Errorf("the version came from the stale branch: %q", s.Version)
	}
	// And HEAD did not move.
	if cur := mustGit(t, dir, "symbolic-ref", "--short", "HEAD"); cur != "main" {
		t.Errorf("a refused checkout must leave HEAD where it was, got %q", cur)
	}
}

// TestPhaseCheckoutMissingBranch (c-1): a typo'd phase id must refuse, not fall
// through to `checkout -b`. Creating the branch would fork it off whatever HEAD
// happens to be and report success, while the work the user meant to reach sits
// on the branch they mistyped.
func TestPhaseCheckoutMissingBranch(t *testing.T) {
	dir, _ := phaseCheckoutClobberRepo(t)

	err := runCmd(t, Phase(), "checkout", "nope")
	if err == nil {
		t.Fatal("expected a refusal for a phase branch that does not exist")
	}
	if !strings.Contains(err.Error(), "phase/nope") {
		t.Errorf("the refusal should name the branch it looked for, got: %v", err)
	}
	if b := mustGit(t, dir, "branch", "--list", "phase/nope"); b != "" {
		t.Errorf("checkout must never create the branch, got: %q", b)
	}
	if cur := mustGit(t, dir, "symbolic-ref", "--short", "HEAD"); cur != "main" {
		t.Errorf("HEAD should not have moved, got %q", cur)
	}
}

// TestPhaseCheckoutSwitchesOnTheOrdinaryPath: the guard is a pre-check, not a
// blanket refusal — a phase branch carrying no tracked state.json (every branch
// cut after the untrack) switches normally.
func TestPhaseCheckoutSwitchesOnTheOrdinaryPath(t *testing.T) {
	dir := t.TempDir()
	gitInit(t, dir, "")
	chdir(t, dir)
	if err := runCmd(t, Init()); err != nil {
		t.Fatalf("init: %v", err)
	}
	mustWrite(t, filepath.Join(dir, "README.md"), "base\n")
	mustGit(t, dir, "add", ".")
	mustGit(t, dir, "commit", "-q", "-m", "chore: baseline")

	if err := runCmd(t, Phase(), "create", "auth"); err != nil {
		t.Fatalf("phase create: %v", err)
	}
	mustGit(t, dir, "checkout", "-q", "main")

	if err := runCmd(t, Phase(), "checkout", "auth"); err != nil {
		t.Fatalf("checkout of an ordinary phase branch should succeed: %v", err)
	}
	if cur := mustGit(t, dir, "symbolic-ref", "--short", "HEAD"); cur != "phase/auth" {
		t.Errorf("HEAD = %q, want phase/auth", cur)
	}
}

// TestCheckoutRefusesClobberOnNonPhaseBranch (c-1) is
// TestPhaseCheckoutRefusesClobber's shape for a NON-phase target — the case
// `dross phase checkout` structurally cannot serve, since it prefixes "phase/".
// Every narration whose target is main (`dross milestone prune`'s refusal) had
// to name the raw verb until this existed, so the guard has to hold here too or
// pointing that refusal at it buys nothing.
func TestCheckoutRefusesClobberOnNonPhaseBranch(t *testing.T) {
	dir, first := checkoutClobberRepo(t, "legacy-main")

	err := runCmd(t, Checkout(), "legacy-main")
	if err == nil {
		t.Fatal("expected a refusal: legacy-main tracks a copy of the live state.json")
	}
	if !strings.Contains(err.Error(), "refusing to switch") {
		t.Errorf("the refusal should be guardLiveState's, got: %v", err)
	}

	s, err := state.Load(filepath.Join(dir, ".dross", state.File))
	if err != nil {
		t.Fatalf("the live state should still be readable: %v", err)
	}
	if len(s.History) < incidentLiveEntries {
		t.Errorf("live history was clobbered: %d entries, want >= %d", len(s.History), incidentLiveEntries)
	}
	if !hasAction(s, first) {
		t.Errorf("the first live action %q is gone: %+v", first, actions(s))
	}
	if s.Version != "9.9.9.9" {
		t.Errorf("the version came from the stale branch: %q", s.Version)
	}
	if cur := mustGit(t, dir, "symbolic-ref", "--short", "HEAD"); cur != "main" {
		t.Errorf("a refused checkout must leave HEAD where it was, got %q", cur)
	}
}

// TestCheckoutSwitchesOnTheOrdinaryPath: the guard is a pre-check, not a
// blanket refusal. A refusal-only verb would be useless as the thing
// `milestone prune` tells the user to run.
func TestCheckoutSwitchesOnTheOrdinaryPath(t *testing.T) {
	dir := t.TempDir()
	gitInit(t, dir, "")
	chdir(t, dir)
	if err := runCmd(t, Init()); err != nil {
		t.Fatalf("init: %v", err)
	}
	mustWrite(t, filepath.Join(dir, "README.md"), "base\n")
	mustGit(t, dir, "add", ".")
	mustGit(t, dir, "commit", "-q", "-m", "chore: baseline")
	mustGit(t, dir, "checkout", "-q", "-b", "milestone/v1.2")
	mustGit(t, dir, "checkout", "-q", "main")

	if err := runCmd(t, Checkout(), "milestone/v1.2"); err != nil {
		t.Fatalf("checkout of an ordinary branch should succeed: %v", err)
	}
	if cur := mustGit(t, dir, "symbolic-ref", "--short", "HEAD"); cur != "milestone/v1.2" {
		t.Errorf("HEAD = %q, want milestone/v1.2", cur)
	}
}

// TestCheckoutMissingBranch: `git checkout main` in a fresh clone resolves
// through origin/main and creates a local branch. That is a different act from
// switching to the branch the user named, so the guarded verb refuses instead.
func TestCheckoutMissingBranch(t *testing.T) {
	dir, _ := checkoutClobberRepo(t, "legacy-main")

	err := runCmd(t, Checkout(), "nope")
	if err == nil {
		t.Fatal("expected a refusal for a branch that does not exist locally")
	}
	if !strings.Contains(err.Error(), "nope") {
		t.Errorf("the refusal should name the branch it looked for, got: %v", err)
	}
	if b := mustGit(t, dir, "branch", "--list", "nope"); b != "" {
		t.Errorf("checkout must never create the branch, got: %q", b)
	}
	if cur := mustGit(t, dir, "symbolic-ref", "--short", "HEAD"); cur != "main" {
		t.Errorf("HEAD should not have moved, got %q", cur)
	}
}
