package cmd

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/state"
)

// legacyStateBranchFixture builds the residual case migration_scope leaves
// behind: a branch cut before the untrack that still TRACKS .dross/state.json,
// while the working tree holds a live untracked copy. Switching to that branch
// is what git refuses. Returns the repo dir and the name of the legacy branch.
func legacyStateBranchFixture(t *testing.T) (string, string) {
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

	// The legacy branch: a tracked copy of state.json, as every branch cut
	// before the migration has.
	mustGit(t, dir, "checkout", "-q", "-b", "legacy")
	mustGit(t, dir, "add", "-f", ".dross/"+state.File)
	mustGit(t, dir, "commit", "-q", "-m", "chore(dross): pre-untrack state copy")
	mustGit(t, dir, "checkout", "-q", "main")

	// Back on main git removed the file it does not track. The live copy is
	// what the machine has accumulated since — and what must survive.
	stPath := filepath.Join(dir, ".dross", state.File)
	live := state.New()
	for i := 0; i < 12; i++ {
		live.Touch("live entry")
	}
	if err := live.Save(stPath); err != nil {
		t.Fatal(err)
	}
	return dir, "legacy"
}

// TestGitDoesNotRefuseIgnoredClobber pins the fact the guard exists for: git
// declines to overwrite an untracked working-tree file only when it is NOT
// ignored. For an ignored path it overwrites silently, so the untrack alone
// protects nothing and dross has to refuse itself.
func TestGitDoesNotRefuseIgnoredClobber(t *testing.T) {
	dir, legacy := legacyStateBranchFixture(t)
	stPath := filepath.Join(dir, ".dross", state.File)

	if out, err := gitCombined(dir, "checkout", legacy); err != nil {
		t.Fatalf("precondition: git is expected to ALLOW this switch, got: %v\n%s", err, out)
	}
	s, err := state.Load(stPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.History) == 12 {
		t.Skip("git now refuses to clobber ignored files — the dross guard is belt-and-braces")
	}
	if len(s.History) >= 12 {
		t.Errorf("expected the stale copy to have replaced the live 12-entry one, got %d", len(s.History))
	}
}

// TestCheckoutRefusalNamesStateJSON: the guard has to name the file and the
// commands that resolve it, not just fail.
func TestCheckoutRefusalNamesStateJSON(t *testing.T) {
	dir, legacy := legacyStateBranchFixture(t)

	err := checkoutBranch(dir, legacy)
	if err == nil {
		t.Fatal("dross must refuse a switch that would overwrite the live state.json")
	}
	for _, want := range []string{".dross/state.json", "cp .dross/state.json"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal should contain %q, got:\n%v", want, err)
		}
	}
}

// TestCheckoutRefusalIsNonDestructive: the guard explains, it does not act. The
// live file keeps its entries and HEAD has not moved.
func TestCheckoutRefusalIsNonDestructive(t *testing.T) {
	dir, legacy := legacyStateBranchFixture(t)
	headBefore := mustGit(t, dir, "symbolic-ref", "--short", "HEAD")

	if err := checkoutBranch(dir, legacy); err == nil {
		t.Fatal("expected a refusal")
	}

	s, err := state.Load(filepath.Join(dir, ".dross", state.File))
	if err != nil {
		t.Fatalf("the live state should still be on disk: %v", err)
	}
	if len(s.History) != 12 {
		t.Errorf("the live state was mutated to get past the refusal: %d entries, want 12", len(s.History))
	}
	if now := mustGit(t, dir, "symbolic-ref", "--short", "HEAD"); now != headBefore {
		t.Errorf("HEAD moved despite the refusal: %s -> %s", headBefore, now)
	}
}

// TestCheckoutRefusalPassesThroughOtherErrors: a checkout blocked by an
// unrelated file must keep git's own text, which names that file. Rewriting
// every failure into the state.json message hides what actually went wrong.
func TestCheckoutRefusalPassesThroughOtherErrors(t *testing.T) {
	dir := t.TempDir()
	gitInit(t, dir, "")
	chdir(t, dir)
	if err := runCmd(t, Init()); err != nil {
		t.Fatalf("init: %v", err)
	}
	mustWrite(t, filepath.Join(dir, "README.md"), "base\n")
	mustGit(t, dir, "add", ".")
	mustGit(t, dir, "commit", "-q", "-m", "chore: baseline")

	// A branch carrying a tracked NOTES.md, and an untracked local one blocking it.
	mustGit(t, dir, "checkout", "-q", "-b", "other")
	mustWrite(t, filepath.Join(dir, "NOTES.md"), "branch copy\n")
	mustGit(t, dir, "add", "NOTES.md")
	mustGit(t, dir, "commit", "-q", "-m", "docs: notes")
	mustGit(t, dir, "checkout", "-q", "main")
	mustWrite(t, filepath.Join(dir, "NOTES.md"), "local copy\n")

	err := checkoutBranch(dir, "other")
	if err == nil {
		t.Fatal("expected git to refuse on the untracked NOTES.md")
	}
	if !strings.Contains(err.Error(), "NOTES.md") {
		t.Errorf("git's own text should survive and name the real file: %v", err)
	}
	if strings.Contains(err.Error(), "cp .dross/state.json") {
		t.Errorf("an unrelated refusal was rewritten into the state.json message: %v", err)
	}

	// Unknown branch: also passed through, not classified.
	if err := checkoutBranch(dir, "no-such-branch"); err == nil {
		t.Error("checking out a nonexistent branch should error")
	} else if strings.Contains(err.Error(), "cp .dross/state.json") {
		t.Errorf("an unknown-branch failure was rewritten: %v", err)
	}
}

// TestPhaseCreateSurfacesCheckoutRefusal: `phase create` forks with `git
// checkout -b <branch> <base>`, so the refusal fires there too — on any base
// ref that still tracks the file. It is the call site a user hits first.
func TestPhaseCreateSurfacesCheckoutRefusal(t *testing.T) {
	dir, legacy := legacyStateBranchFixture(t)

	// Point new work at the legacy base: forking off it has to replay the
	// base's tree, which carries the tracked copy.
	err := checkoutBranchNew(dir, "phase/auth", legacy)
	if err == nil {
		t.Fatal("forking off a base that tracks state.json must refuse")
	}
	for _, want := range []string{".dross/state.json", "cp .dross/state.json"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal should contain %q, got:\n%v", want, err)
		}
	}
	if s, lerr := state.Load(filepath.Join(dir, ".dross", state.File)); lerr != nil || len(s.History) != 12 {
		t.Errorf("the live state must survive a refused fork: %v / %+v", lerr, s)
	}
}

// TestPhaseCompleteSurfacesCheckoutRefusal: completion switches to the base
// before reconciling. If that switch fails, the run must stop — deleting the
// phase branch after a failed switch throws the work away.
func TestPhaseCompleteSurfacesCheckoutRefusal(t *testing.T) {
	dir, _ := completeFixture(t)
	stubPRMerged(t, true)

	// Give main the pre-untrack shape: a tracked state.json that the live
	// untracked copy on phase/auth would collide with.
	mustGit(t, dir, "checkout", "-q", "main")
	mustGit(t, dir, "add", "-f", ".dross/"+state.File)
	mustGit(t, dir, "commit", "-q", "-m", "chore(dross): pre-untrack state copy")
	mustGit(t, dir, "checkout", "-q", "phase/auth")
	stPath := filepath.Join(dir, ".dross", state.File)
	live := state.New()
	for i := 0; i < 12; i++ {
		live.Touch("live entry")
	}
	if err := live.Save(stPath); err != nil {
		t.Fatal(err)
	}

	err := runCmd(t, Phase(), "complete")
	if err == nil {
		t.Fatal("complete must not exit zero when the switch to the base failed")
	}
	if !strings.Contains(err.Error(), ".dross/state.json") {
		t.Errorf("the failure should name the blocking file: %v", err)
	}
	// The work survives on both sides.
	if b := mustGit(t, dir, "branch", "--list", "phase/auth"); b == "" {
		t.Error("a failed switch must not delete the local phase branch")
	}
	if s, lerr := state.Load(stPath); lerr != nil || len(s.History) != 12 {
		t.Errorf("the live state must survive: %v / %+v", lerr, s)
	}
}
