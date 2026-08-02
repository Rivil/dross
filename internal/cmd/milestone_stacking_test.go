package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/milestone"
)

// milestoneStackingFixture builds a repo whose milestone/v1.1 carries real work
// and is pushed to origin, with state.current_milestone pointing at it. When
// merged, the v1.1->main merge is simulated on origin (local main stays behind,
// as it does in life until someone finalizes), so the cut resolver sees a
// parent that has already landed.
func milestoneStackingFixture(t *testing.T, merged bool) string {
	t.Helper()
	dir, _ := setupMilestoneRepo(t)
	mustGit(t, dir, "push", "-q", "-u", "origin", "main")
	mustGit(t, dir, "branch", "milestone/v1.1")
	mustGit(t, dir, "checkout", "-q", "milestone/v1.1")
	mustWrite(t, filepath.Join(dir, "v11work.txt"), "x\n")
	mustGit(t, dir, "add", ".")
	mustGit(t, dir, "commit", "-q", "-m", "v1.1 work")
	mustGit(t, dir, "push", "-q", "-u", "origin", "milestone/v1.1")

	if merged {
		mustGit(t, dir, "checkout", "-q", "-b", "mergetmp", "main")
		mustGit(t, dir, "merge", "--no-ff", "-q", "-m", "merge v1.1", "milestone/v1.1")
		mustGit(t, dir, "push", "-q", "origin", "mergetmp:main")
		mustGit(t, dir, "checkout", "-q", "main")
		mustGit(t, dir, "branch", "-D", "mergetmp")
	} else {
		mustGit(t, dir, "checkout", "-q", "main")
	}
	mustGit(t, dir, "fetch", "-q", "origin")
	if err := runCmd(t, State(), "set", "current_milestone", "v1.1"); err != nil {
		t.Fatal(err)
	}
	return dir
}

func recordedBase(t *testing.T, version string) string {
	t.Helper()
	m, err := milestone.Load(milestone.FilePath(".dross", version))
	if err != nil {
		t.Fatal(err)
	}
	return m.Milestone.Base
}

// The stacked case: v1.1 is unmerged, so v1.2 is cut from its tip and records
// it. Recording without cutting — or cutting without recording — is the whole
// defect this phase exists to prevent, so both halves are asserted together.
func TestMilestoneCreateStacksOnUnmergedParent(t *testing.T) {
	dir := milestoneStackingFixture(t, false)

	out := captureStdout(t, func() {
		if err := runCmd(t, Milestone(), "create", "v1.2"); err != nil {
			t.Fatalf("create: %v", err)
		}
	})

	if got, want := mustGit(t, dir, "rev-parse", "milestone/v1.2"), mustGit(t, dir, "rev-parse", "milestone/v1.1"); got != want {
		t.Errorf("v1.2 tip %s != v1.1 tip %s — it was not cut from the parent", got, want)
	}
	if got := recordedBase(t, "v1.2"); got != "milestone/v1.1" {
		t.Errorf("recorded base = %q, want %q", got, "milestone/v1.1")
	}
	if !strings.Contains(out, "cut milestone/v1.2 from milestone/v1.1") {
		t.Errorf("output should name the branch actually cut from:\n%s", out)
	}
}

// Once the parent has merged, the cut goes back to main — a create keyed on
// milestone.status (still "planning"/"active" here) passes the stacked case
// and fails this one.
func TestMilestoneCreateFallsBackToMainOnceParentMerged(t *testing.T) {
	dir := milestoneStackingFixture(t, true)

	out := captureStdout(t, func() {
		if err := runCmd(t, Milestone(), "create", "v1.2"); err != nil {
			t.Fatalf("create: %v", err)
		}
	})

	if got, want := mustGit(t, dir, "rev-parse", "milestone/v1.2"), mustGit(t, dir, "rev-parse", "main"); got != want {
		t.Errorf("v1.2 tip %s != main tip %s — a merged parent must not be stacked on", got, want)
	}
	if got := recordedBase(t, "v1.2"); got != "main" {
		t.Errorf("recorded base = %q, want %q", got, "main")
	}
	if !strings.Contains(out, "cut milestone/v1.2 from main") {
		t.Errorf("output should name main as the cut point:\n%s", out)
	}
}

// The parent is whatever current_milestone names, full stop (locked
// stacking_parent). With it unset, an unmerged milestone branch sitting in the
// repo is NOT stacked on — a ref-scanning implementation fails here.
func TestMilestoneCreateWithoutCurrentMilestoneCutsFromMain(t *testing.T) {
	dir := milestoneStackingFixture(t, false)
	if err := runCmd(t, State(), "set", "current_milestone", ""); err != nil {
		t.Fatal(err)
	}

	if err := runCmd(t, Milestone(), "create", "v1.2"); err != nil {
		t.Fatalf("create: %v", err)
	}
	if got, want := mustGit(t, dir, "rev-parse", "milestone/v1.2"), mustGit(t, dir, "rev-parse", "main"); got != want {
		t.Errorf("v1.2 tip %s != main tip %s — an unset current_milestone must cut from main", got, want)
	}
	if got := recordedBase(t, "v1.2"); got != "main" {
		t.Errorf("recorded base = %q, want %q", got, "main")
	}
}

// The normal state after `milestone complete --finalize`: current_milestone
// still names v1.1 but its branch is gone everywhere. Cutting must fall back to
// main and exit 0, never error — otherwise every later create is wedged until
// the user hand-clears current_milestone.
func TestMilestoneCreateWithVanishedParent(t *testing.T) {
	dir := milestoneStackingFixture(t, true)
	mustGit(t, dir, "push", "-q", "origin", "--delete", "milestone/v1.1")
	mustGit(t, dir, "branch", "-D", "milestone/v1.1")
	mustGit(t, dir, "fetch", "-q", "--prune", "origin")

	if err := runCmd(t, Milestone(), "create", "v1.2"); err != nil {
		t.Fatalf("create with a vanished parent: %v", err)
	}
	if got, want := mustGit(t, dir, "rev-parse", "milestone/v1.2"), mustGit(t, dir, "rev-parse", "main"); got != want {
		t.Errorf("v1.2 tip %s != main tip %s", got, want)
	}
	if got := recordedBase(t, "v1.2"); got != "main" {
		t.Errorf("recorded base = %q, want %q", got, "main")
	}
}

// --base overrides the resolver and is recorded like any automatic answer, so
// an abandoned parent can be stepped around without hand-editing the toml.
func TestMilestoneCreateBaseFlagWins(t *testing.T) {
	dir := milestoneStackingFixture(t, false)
	mustGit(t, dir, "branch", "milestone/v0.9", "main")

	out := captureStdout(t, func() {
		if err := runCmd(t, Milestone(), "create", "v1.2", "--base", "milestone/v0.9"); err != nil {
			t.Fatalf("create --base: %v", err)
		}
	})

	if got, want := mustGit(t, dir, "rev-parse", "milestone/v1.2"), mustGit(t, dir, "rev-parse", "milestone/v0.9"); got != want {
		t.Errorf("v1.2 tip %s != milestone/v0.9 tip %s — --base lost to the resolver", got, want)
	}
	if got := recordedBase(t, "v1.2"); got != "milestone/v0.9" {
		t.Errorf("recorded base = %q, want %q", got, "milestone/v0.9")
	}
	if !strings.Contains(out, "cut milestone/v1.2 from milestone/v0.9") {
		t.Errorf("output should name the forced cut point:\n%s", out)
	}
}

// A bad --base is refused before anything is written: no toml, no branch.
func TestMilestoneCreateBaseFlagUnknownBranchIsAtomic(t *testing.T) {
	dir := milestoneStackingFixture(t, false)

	err := runCmd(t, Milestone(), "create", "v1.2", "--base", "nope")
	if err == nil {
		t.Fatal("expected a refusal for a nonexistent --base branch")
	}
	if !strings.Contains(err.Error(), "no such local branch") {
		t.Errorf("error should name the missing branch: %v", err)
	}
	if _, statErr := os.Stat(milestone.FilePath(filepath.Join(dir, ".dross"), "v1.2")); statErr == nil {
		t.Error("a refused create still wrote the milestone toml")
	}
	if b := mustGit(t, dir, "branch", "--list", "milestone/v1.2"); b != "" {
		t.Errorf("a refused create still cut a branch: %q", b)
	}
}

// Offline the answer comes from local refs, which may be behind origin — so
// say so rather than presenting the cut point as settled fact.
func TestMilestoneCreateNarratesOfflineCutPoint(t *testing.T) {
	dir := milestoneStackingFixture(t, false)
	mustGit(t, dir, "remote", "set-url", "origin", filepath.Join(t.TempDir(), "gone.git"))

	out := captureStdout(t, func() {
		// The eager push to a dead origin fails; the cut and the narration
		// still have to have happened first.
		_ = runCmd(t, Milestone(), "create", "v1.2")
	})

	if !strings.Contains(out, "local refs") {
		t.Errorf("an origin-unreachable create must carry the local-refs caveat:\n%s", out)
	}
	if got, want := mustGit(t, dir, "rev-parse", "milestone/v1.2"), mustGit(t, dir, "rev-parse", "milestone/v1.1"); got != want {
		t.Errorf("v1.2 tip %s != v1.1 tip %s", got, want)
	}
	if got := recordedBase(t, "v1.2"); got != "milestone/v1.1" {
		t.Errorf("recorded base = %q, want %q", got, "milestone/v1.1")
	}
}

// current_milestone naming the version being created is not a parent — it is
// the same milestone, and stacking it on itself is meaningless.
func TestMilestoneCreateIgnoresSelfAsParent(t *testing.T) {
	dir := milestoneStackingFixture(t, false)
	if err := runCmd(t, State(), "set", "current_milestone", "v1.2"); err != nil {
		t.Fatal(err)
	}

	if err := runCmd(t, Milestone(), "create", "v1.2"); err != nil {
		t.Fatalf("create: %v", err)
	}
	if got, want := mustGit(t, dir, "rev-parse", "milestone/v1.2"), mustGit(t, dir, "rev-parse", "main"); got != want {
		t.Errorf("v1.2 tip %s != main tip %s", got, want)
	}
	if got := recordedBase(t, "v1.2"); got != "main" {
		t.Errorf("recorded base = %q, want %q", got, "main")
	}
}
