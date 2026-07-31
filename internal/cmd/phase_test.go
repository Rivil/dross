package cmd

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/changes"
	"github.com/Rivil/dross/internal/milestone"
	"github.com/Rivil/dross/internal/ship"
	"github.com/Rivil/dross/internal/state"
)

// stubPRMerged overrides the exported ship.PRMergedFunc seam so `dross phase
// complete`'s merge gate gets a deterministic answer without a `gh` binary or
// network, restoring the real func when the test ends. The happy-path fixtures
// record a PR number, so with merged=true the gate takes the authoritative
// path; merged=false simulates an unmerged PR.
func stubPRMerged(t *testing.T, merged bool) {
	t.Helper()
	prev := ship.PRMergedFunc
	ship.PRMergedFunc = func(ship.OpenOpts) (bool, error) { return merged, nil }
	t.Cleanup(func() { ship.PRMergedFunc = prev })
}

// initWithGit sets up a dross-onboarded git repo at dir with a single
// baseline commit on main, ready for `dross phase create` to fork
// phase/<id> off it.
func initWithGit(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	gitInit(t, dir, "")
	chdir(t, dir)
	if err := runCmd(t, Init()); err != nil {
		t.Fatalf("init: %v", err)
	}
	// Commit the init scaffold so the tree is clean and HEAD exists —
	// branching off needs a parent commit.
	mustWrite(t, filepath.Join(dir, "README.md"), "base\n")
	mustGit(t, dir, "add", ".")
	mustGit(t, dir, "commit", "-q", "-m", "chore: baseline")
	return dir
}

func TestPhaseCreateChecksOutPhaseBranch(t *testing.T) {
	dir := initWithGit(t)

	if err := runCmd(t, Phase(), "create", "meal tagging"); err != nil {
		t.Fatalf("create: %v", err)
	}

	cur := mustGit(t, dir, "symbolic-ref", "--short", "HEAD")
	if cur != "phase/meal-tagging" {
		t.Errorf("expected HEAD on phase/meal-tagging, got %q", cur)
	}
}

func TestPhaseCreateRefusesDirtyTree(t *testing.T) {
	dir := initWithGit(t)
	mustWrite(t, filepath.Join(dir, "uncommitted.txt"), "dirty\n")

	err := runCmd(t, Phase(), "create", "auth")
	if err == nil {
		t.Fatal("expected error on dirty tree")
	}
	if !strings.Contains(err.Error(), "dirty") {
		t.Errorf("error should mention dirty tree: %v", err)
	}
	// The error must name the offending path so the user doesn't have to
	// re-run git status to find what to commit or stash.
	if !strings.Contains(err.Error(), "uncommitted.txt") {
		t.Errorf("dirty-tree error should list the offending file: %v", err)
	}
}

// Under the v0.7 branch model the must-be-on-main guard is gone: create forks
// off the resolved base (main here, no milestone) regardless of the branch you
// happen to be on, so commits from the current branch must NOT leak in.
func TestPhaseCreateFromNonMainRootsOnBase(t *testing.T) {
	dir := initWithGit(t)
	mustGit(t, dir, "checkout", "-q", "-b", "feature")
	mustWrite(t, filepath.Join(dir, "feature.txt"), "x\n")
	mustGit(t, dir, "add", ".")
	mustGit(t, dir, "commit", "-q", "-m", "feature only")
	featureCommit := mustGit(t, dir, "rev-parse", "HEAD")

	if err := runCmd(t, Phase(), "create", "auth"); err != nil {
		t.Fatalf("create from non-main should now succeed: %v", err)
	}
	if cur := mustGit(t, dir, "symbolic-ref", "--short", "HEAD"); cur != "phase/auth" {
		t.Errorf("expected HEAD on phase/auth, got %q", cur)
	}
	if err := gitNoOut(dir, "merge-base", "--is-ancestor", featureCommit, "refs/heads/phase/auth"); err == nil {
		t.Error("phase/auth must root on main, not the current feature branch (feature commit leaked in)")
	}
}

func TestPhaseCreateRefusesExistingBranch(t *testing.T) {
	dir := initWithGit(t)

	// Pre-create the branch the next phase would want.
	mustGit(t, dir, "branch", "phase/auth")

	err := runCmd(t, Phase(), "create", "auth")
	if err == nil {
		t.Fatal("expected error when phase branch already exists")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("error should mention existing branch: %v", err)
	}
}

func TestPhaseCreateNoBranchSkipsGit(t *testing.T) {
	dir := initWithGit(t)

	if err := runCmd(t, Phase(), "create", "--no-branch", "auth"); err != nil {
		t.Fatalf("create --no-branch: %v", err)
	}

	// HEAD should still be on main — no branch was created.
	cur := mustGit(t, dir, "symbolic-ref", "--short", "HEAD")
	if cur != "main" {
		t.Errorf("expected HEAD to stay on main, got %q", cur)
	}
	branches := mustGit(t, dir, "branch", "--list", "phase/*")
	if branches != "" {
		t.Errorf("expected no phase/* branches, got: %q", branches)
	}
}

func TestPhaseCreateRollsBackDirOnBranchFailure(t *testing.T) {
	dir := initWithGit(t)

	// Pre-create the would-be branch so forkPhaseBranch's no-existing-ref
	// guard trips. The guard now runs after the phase dir is mkdir'd, so the
	// guarantee under test is the rollback: an errored create leaves no
	// stray phase dir behind (dir is os.Remove'd on any fork failure).
	mustGit(t, dir, "branch", "phase/auth")

	if err := runCmd(t, Phase(), "create", "auth"); err == nil {
		t.Fatal("expected error from existing branch")
	}

	// Phase dir must NOT survive — the fork failure rolled it back.
	if _, err := os.Stat(filepath.Join(dir, ".dross", "phases", "auth")); err == nil {
		t.Error("phase dir should not exist after a rolled-back create")
	}
}

// TestPhaseCreateSlugIdentity proves create makes a bare <slug>/ dir (no NN-
// prefix), checks out phase/<slug>, sets state, appends the slug to the current
// milestone's phases array, and auto-suffixes on collision.
func TestPhaseCreateSlugIdentity(t *testing.T) {
	dir := initWithGit(t)
	root := filepath.Join(dir, ".dross")
	mustWrite(t, filepath.Join(root, "milestones", "v0.4.toml"),
		"phases = []\n\n[milestone]\nversion = \"v0.4\"\n")
	if err := runCmd(t, State(), "set", "current_milestone", "v0.4"); err != nil {
		t.Fatalf("set milestone: %v", err)
	}
	mustGit(t, dir, "add", ".")
	mustGit(t, dir, "commit", "-q", "-m", "chore: milestone")

	if err := runCmd(t, Phase(), "create", "My Feature"); err != nil {
		t.Fatalf("create: %v", err)
	}

	// Bare slug dir, and no NN- prefixed dir anywhere under phases/.
	if !isDir(filepath.Join(root, "phases", "my-feature")) {
		t.Error("expected phases/my-feature dir")
	}
	ents, _ := os.ReadDir(filepath.Join(root, "phases"))
	nnRe := regexp.MustCompile(`^\d\d-`)
	for _, e := range ents {
		if nnRe.MatchString(e.Name()) {
			t.Errorf("no NN- prefixed dir expected, got %q", e.Name())
		}
	}
	// Branch + state both carry the slug identity.
	if cur := mustGit(t, dir, "symbolic-ref", "--short", "HEAD"); cur != "phase/my-feature" {
		t.Errorf("branch: got %q want phase/my-feature", cur)
	}
	s, _ := state.Load(filepath.Join(root, "state.json"))
	if s.CurrentPhase != "my-feature" {
		t.Errorf("current_phase: got %q want my-feature", s.CurrentPhase)
	}
	// Appended to the milestone array tail — dropping the append leaves it empty.
	m, _ := milestone.Load(milestone.FilePath(root, "v0.4"))
	if len(m.Phases) == 0 || m.Phases[len(m.Phases)-1] != "my-feature" {
		t.Errorf("milestone array tail: got %v want last=my-feature", m.Phases)
	}

	// A second create of the same title collides → my-feature-2, first intact.
	mustGit(t, dir, "add", ".")
	mustGit(t, dir, "commit", "-q", "-m", "chore: phase 1 bookkeeping")
	mustGit(t, dir, "checkout", "-q", "main")
	// Fold phase 1's dir onto main before the second create. Recording the
	// forked-from base at create time makes .dross/phases/<id>/ tracked
	// immediately, so checking out main now takes the first phase's dir away
	// with it — and there'd be no slug collision left to assert.
	mustGit(t, dir, "merge", "-q", "--ff-only", "phase/my-feature")
	if err := runCmd(t, Phase(), "create", "My Feature"); err != nil {
		t.Fatalf("second create: %v", err)
	}
	if !isDir(filepath.Join(root, "phases", "my-feature-2")) {
		t.Error("expected phases/my-feature-2 on collision")
	}
	if !isDir(filepath.Join(root, "phases", "my-feature")) {
		t.Error("first phase dir should be untouched by the collision")
	}
}

// TestPhaseListOrdersByMilestoneArray proves `dross phase list` orders by the
// milestone's phases array, not by directory-name sort: reordering the array
// flips the listing.
func TestPhaseListOrdersByMilestoneArray(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	if err := runCmd(t, Init()); err != nil {
		t.Fatalf("init: %v", err)
	}
	root := filepath.Join(dir, ".dross")
	for _, name := range []string{"alpha", "gamma"} {
		if err := os.MkdirAll(filepath.Join(root, "phases", name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeMilestone := func(phases string) {
		mustWrite(t, filepath.Join(root, "milestones", "v0.4.toml"),
			"phases = ["+phases+"]\n\n[milestone]\nversion = \"v0.4\"\n")
	}
	list := func() string {
		return captureStdout(t, func() {
			if err := runCmd(t, Phase(), "list"); err != nil {
				t.Fatalf("list: %v", err)
			}
		})
	}

	writeMilestone(`"gamma", "alpha"`)
	if got := list(); got != "gamma\nalpha\n" {
		t.Errorf("array order [gamma,alpha]: got %q want \"gamma\\nalpha\\n\"", got)
	}
	// Reverting to ReadDir+sort.Strings would print alphabetical here; the
	// array order must win.
	writeMilestone(`"alpha", "gamma"`)
	if got := list(); got != "alpha\ngamma\n" {
		t.Errorf("array order [alpha,gamma]: got %q want \"alpha\\ngamma\\n\"", got)
	}
}

// TestPhaseNumber proves `dross phase number` reports a phase's 1-based ordinal
// from the current milestone's array, recomputing after a reorder.
func TestPhaseNumber(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	if err := runCmd(t, Init()); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(dir, ".dross")
	writeMs := func(phases string) {
		mustWrite(t, filepath.Join(root, "milestones", "v0.4.toml"),
			"phases = ["+phases+"]\n\n[milestone]\nversion = \"v0.4\"\n")
	}
	if err := runCmd(t, State(), "set", "current_milestone", "v0.4"); err != nil {
		t.Fatal(err)
	}
	num := func(id string) string {
		return strings.TrimSpace(captureStdout(t, func() {
			if err := runCmd(t, Phase(), "number", id); err != nil {
				t.Fatalf("number %s: %v", id, err)
			}
		}))
	}

	writeMs(`"alpha", "beta", "gamma"`)
	if got := num("beta"); got != "2" {
		t.Errorf("number beta: got %q want 2", got)
	}
	if got := num("alpha"); got != "1" {
		t.Errorf("number alpha: got %q want 1", got)
	}
	// Reordering the array moves alpha to position 3 — a directory count would
	// not change; array position does.
	writeMs(`"gamma", "beta", "alpha"`)
	if got := num("alpha"); got != "3" {
		t.Errorf("number alpha after reorder: got %q want 3", got)
	}
	if got := num("missing"); got != "0" {
		t.Errorf("number of phase not in array: got %q want 0", got)
	}
}

// TestStatusPhasePosition proves `dross status` locates the current phase within
// its milestone as "N of M" via the shared DisplayNumber helper.
func TestStatusPhasePosition(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	if err := runCmd(t, Init()); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(dir, ".dross")
	mustWrite(t, filepath.Join(root, "milestones", "v0.4.toml"),
		"phases = [\"alpha\", \"beta\", \"gamma\"]\n\n[milestone]\nversion = \"v0.4\"\n")
	if err := runCmd(t, State(), "set", "current_milestone", "v0.4"); err != nil {
		t.Fatal(err)
	}
	if err := runCmd(t, State(), "set", "current_phase", "beta"); err != nil {
		t.Fatal(err)
	}
	out := captureStdout(t, func() {
		if err := runCmd(t, Status()); err != nil {
			t.Fatalf("status: %v", err)
		}
	})
	if !strings.Contains(out, "2 of 3") {
		t.Errorf("status should locate the phase as `2 of 3`; got:\n%s", out)
	}
}

// completeFixture sets up the post-squash-merge state for `dross phase
// complete`: local has been on phase/<id> with one work commit; origin
// has the squash already on main. Returns repo dir + the phase id.
func completeFixture(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	remoteDir := t.TempDir()
	mustGit(t, remoteDir, "init", "-q", "--bare", "-b", "main")
	gitInit(t, dir, remoteDir)
	chdir(t, dir)

	if err := runCmd(t, Init()); err != nil {
		t.Fatalf("init: %v", err)
	}
	mustWrite(t, filepath.Join(dir, "README.md"), "base\n")
	mustGit(t, dir, "add", ".")
	mustGit(t, dir, "commit", "-q", "-m", "chore: baseline")
	mustGit(t, dir, "push", "-q", "-u", "origin", "main")

	if err := runCmd(t, Phase(), "create", "auth"); err != nil {
		t.Fatalf("phase create: %v", err)
	}
	// Make a phase commit so HEAD on phase/auth is real.
	mustWrite(t, filepath.Join(dir, "src/auth.ts"), "x\n")
	mustGit(t, dir, "add", ".")
	mustGit(t, dir, "commit", "-q", "-m", "feat(auth): scaffold")

	// Record a PR number on phase/auth (as ship does post-push), so complete's
	// merge gate has THIS phase's PR to look up. Committed on the phase branch
	// only — it never reaches origin/main, matching production. It carries the
	// base too: `phase create` records the forked-from branch and ship
	// overwrites it, and complete reconciles against that rather than
	// inferring one.
	mustWrite(t, filepath.Join(dir, ".dross/phases/auth/changes.json"),
		`{"phase":"auth","pr":42,"base":"main","tasks":{}}`)
	mustGit(t, dir, "add", ".dross/phases/auth/changes.json")
	mustGit(t, dir, "commit", "-q", "-m", "chore(dross): record PR #42 for auth")

	// Simulate the upstream squash-merge: build a synthetic squash on
	// top of origin/main and push it. The squash must carry the completion
	// record `dross ship` folds in (current_phase cleared + `completed
	// auth` history) — phase complete reads that off origin/main as its
	// merge guard, so without it complete would refuse.
	mustGit(t, dir, "checkout", "-q", "-b", "squash-sim", "origin/main")
	mustGit(t, dir, "checkout", "phase/auth", "--", "src/")
	mustGit(t, dir, "add", "src/")
	stPath := filepath.Join(dir, ".dross", "state.json")
	sqState, err := state.Load(stPath)
	if err != nil {
		t.Fatalf("load state for squash sim: %v", err)
	}
	sqState.CurrentPhase = ""
	sqState.CurrentPhaseStatus = ""
	sqState.Touch("completed auth")
	if err := sqState.Save(stPath); err != nil {
		t.Fatalf("save squash state: %v", err)
	}
	mustGit(t, dir, "add", filepath.Join(".dross", "state.json"))
	mustGit(t, dir, "commit", "-q", "-m", "feat(squash): auth")
	mustGit(t, dir, "push", "-q", "--force", "origin", "squash-sim:main")
	mustGit(t, dir, "checkout", "-q", "phase/auth")
	mustGit(t, dir, "branch", "-D", "squash-sim")
	mustGit(t, dir, "fetch", "-q", "origin")

	return dir, "auth"
}

func TestPhaseCompleteHappyPath(t *testing.T) {
	dir, _ := completeFixture(t)
	stubPRMerged(t, true) // the recorded PR is authoritatively merged

	if err := runCmd(t, Phase(), "complete"); err != nil {
		t.Fatalf("complete: %v", err)
	}

	cur := mustGit(t, dir, "symbolic-ref", "--short", "HEAD")
	if cur != "main" {
		t.Errorf("expected HEAD on main, got %q", cur)
	}
	branches := mustGit(t, dir, "branch", "--list", "phase/*")
	if branches != "" {
		t.Errorf("phase/* should be deleted, got: %q", branches)
	}

	// state.json: current_phase cleared, completed entry recorded,
	// committed (working tree clean).
	s, _ := state.Load(filepath.Join(dir, ".dross", "state.json"))
	if s.CurrentPhase != "" {
		t.Errorf("current_phase should be cleared, got %q", s.CurrentPhase)
	}
	found := false
	for _, a := range s.History {
		if strings.Contains(a.Action, "completed auth") {
			found = true
		}
	}
	if !found {
		t.Errorf("history should record completion: %+v", s.History)
	}
	status := mustGit(t, dir, "status", "--porcelain")
	if status != "" {
		t.Errorf("working tree should be clean after complete, got: %q", status)
	}
}

// assertRefusalTouchedNothing is the shared c-1 assertion: a refused
// completion must leave HEAD on the phase branch, the phase branch alive, and
// the base ref exactly where it was. The branch switch sits past every refusal
// precisely so a refusal is a re-runnable no-op.
func assertRefusalTouchedNothing(t *testing.T, dir, phaseBranch, base, baseSHABefore string) {
	t.Helper()
	if cur := mustGit(t, dir, "symbolic-ref", "--short", "HEAD"); cur != phaseBranch {
		t.Errorf("refusal moved HEAD off %s: now on %q", phaseBranch, cur)
	}
	if branches := mustGit(t, dir, "branch", "--list", phaseBranch); !strings.Contains(branches, phaseBranch) {
		t.Errorf("%s should survive a refused complete, got: %q", phaseBranch, branches)
	}
	if got := mustGit(t, dir, "rev-parse", base); got != baseSHABefore {
		t.Errorf("refusal moved %s: %s -> %s", base, baseSHABefore, got)
	}
}

// TestPhaseCompleteMergeGateRefusalLeavesHEADOnPhase pins c-1 for the gate
// refusal: an unmerged PR must not cost the user their branch position. The
// checkout used to run before the gate, so a refusal stranded them on the base
// with the phase branch still holding unmerged work.
func TestPhaseCompleteMergeGateRefusalLeavesHEADOnPhase(t *testing.T) {
	dir, _ := completeFixture(t)
	stubPRMerged(t, false) // provider says PR #42 is NOT merged

	mainBefore := mustGit(t, dir, "rev-parse", "main")
	originBefore := mustGit(t, dir, "rev-parse", "origin/main")

	err := runCmd(t, Phase(), "complete")
	if err == nil {
		t.Fatal("expected refusal when the recorded PR is not merged")
	}
	assertRefusalTouchedNothing(t, dir, "phase/auth", "main", mainBefore)

	// The safety-net push must not have fired: local main is behind origin/main
	// here, with nothing of its own to push.
	if got := mustGit(t, dir, "rev-parse", "origin/main"); got != originBefore {
		t.Errorf("safety-net push advanced origin/main on a base with nothing to push: %s -> %s", originBefore, got)
	}
}

// TestPhaseCompleteFetchFailureLeavesHEADOnPhase pins c-1 for the earliest
// refusal — a fetch that can't reach origin. Nothing before the ff-only merge
// needs HEAD, so an unreachable remote must be a pure no-op.
func TestPhaseCompleteFetchFailureLeavesHEADOnPhase(t *testing.T) {
	dir, _ := completeFixture(t)
	stubPRMerged(t, true)

	mainBefore := mustGit(t, dir, "rev-parse", "main")
	mustGit(t, dir, "remote", "set-url", "origin", filepath.Join(dir, "no-such-remote.git"))

	err := runCmd(t, Phase(), "complete")
	if err == nil {
		t.Fatal("expected refusal when fetch cannot reach origin")
	}
	assertRefusalTouchedNothing(t, dir, "phase/auth", "main", mainBefore)
}

// TestPhaseCompleteSafetyNetRefusalLeavesHEADOnPhase pins c-1 for the
// safety-net push refusal: a base carrying unpushed non-.dross work is the
// user's to reconcile, and refusing to do it for them must not also relocate
// them onto that very branch.
func TestPhaseCompleteSafetyNetRefusalLeavesHEADOnPhase(t *testing.T) {
	dir, _ := completeFixture(t)
	stubPRMerged(t, true)

	// Put local main purely ahead of origin/main with a code commit — the
	// shape pushBaseIfAheadDrossOnly refuses on.
	mustGit(t, dir, "checkout", "-q", "main")
	mustGit(t, dir, "reset", "-q", "--hard", "origin/main")
	mustWrite(t, filepath.Join(dir, "src/hotfix.ts"), "x\n")
	mustGit(t, dir, "add", "src/hotfix.ts")
	mustGit(t, dir, "commit", "-q", "-m", "fix: hotfix straight on main")
	mustGit(t, dir, "checkout", "-q", "phase/auth")

	mainBefore := mustGit(t, dir, "rev-parse", "main")

	err := runCmd(t, Phase(), "complete")
	if err == nil {
		t.Fatal("expected refusal when the base is ahead with non-.dross commits")
	}
	if !strings.Contains(err.Error(), "non-.dross") {
		t.Errorf("refusal should name the non-.dross commits: %v", err)
	}
	assertRefusalTouchedNothing(t, dir, "phase/auth", "main", mainBefore)
}

// TestPhaseCompleteReconcilesRecordedBaseNotMilestone is the c-2 regression in
// miniature: the phase recorded main, a stale milestone/<v> branch is sitting
// in the local repo, and current_milestone points at it. Inference picks the
// milestone branch and fast-forwards it with work that never merged there;
// the record picks main.
func TestPhaseCompleteReconcilesRecordedBaseNotMilestone(t *testing.T) {
	dir, _ := completeFixture(t) // records base "main"
	stubPRMerged(t, true)

	// A stale milestone branch + an active milestone: everything the old
	// inference needed to pick the wrong target.
	mustGit(t, dir, "branch", "milestone/v0.9", "main")
	if err := runCmd(t, State(), "set", "current_milestone", "v0.9"); err != nil {
		t.Fatalf("state set: %v", err)
	}
	msBefore := mustGit(t, dir, "rev-parse", "milestone/v0.9")

	if err := runCmd(t, Phase(), "complete"); err != nil {
		t.Fatalf("complete: %v", err)
	}
	if cur := mustGit(t, dir, "symbolic-ref", "--short", "HEAD"); cur != "main" {
		t.Errorf("HEAD = %q, want main (the RECORDED base)", cur)
	}
	if got, want := mustGit(t, dir, "rev-parse", "main"), mustGit(t, dir, "rev-parse", "origin/main"); got != want {
		t.Errorf("main not ff'd to origin: %s != %s", got, want)
	}
	if got := mustGit(t, dir, "rev-parse", "milestone/v0.9"); got != msBefore {
		t.Errorf("the stale milestone branch was moved: %s -> %s", msBefore, got)
	}
}

// TestPhaseCompleteRefusesWithNoRecordedBase pins c-3: no record and no flag
// is a refusal naming the phase, the candidates and --base — never a silent
// fallback to the milestone-inferred base. It must fire before anything
// mutating, so HEAD, the base and the phase branch are all untouched.
func TestPhaseCompleteRefusesWithNoRecordedBase(t *testing.T) {
	dir, _ := completeFixture(t)
	stubPRMerged(t, true)

	// Strip the base from the record, on the phase branch where complete
	// reads it — the shape of a phase created before this check existed.
	mustWrite(t, filepath.Join(dir, ".dross/phases/auth/changes.json"),
		`{"phase":"auth","pr":42,"tasks":{}}`)
	mustGit(t, dir, "add", ".dross/phases/auth/changes.json")
	mustGit(t, dir, "commit", "-q", "-m", "chore(dross): drop the base record")
	mustGit(t, dir, "branch", "milestone/v0.9", "main")
	if err := runCmd(t, State(), "set", "current_milestone", "v0.9"); err != nil {
		t.Fatalf("state set: %v", err)
	}
	mainBefore := mustGit(t, dir, "rev-parse", "main")

	err := runCmd(t, Phase(), "complete")
	if err == nil {
		t.Fatal("expected a refusal when no base is recorded")
	}
	for _, needle := range []string{"auth", "--base", "main", "milestone/v0.9"} {
		if !strings.Contains(err.Error(), needle) {
			t.Errorf("refusal should name %q: %v", needle, err)
		}
	}
	assertRefusalTouchedNothing(t, dir, "phase/auth", "main", mainBefore)
}

// TestPhaseCompleteBaseFlagOverridesMissingRecord is the legacy_escape decision
// (c-3): a branch the user types carries a base-less phase through to
// completion, so a phase in flight when this landed is not stranded.
func TestPhaseCompleteBaseFlagOverridesMissingRecord(t *testing.T) {
	dir, _ := completeFixture(t)
	stubPRMerged(t, true)

	mustWrite(t, filepath.Join(dir, ".dross/phases/auth/changes.json"),
		`{"phase":"auth","pr":42,"tasks":{}}`)
	mustGit(t, dir, "add", ".dross/phases/auth/changes.json")
	mustGit(t, dir, "commit", "-q", "-m", "chore(dross): drop the base record")

	if err := runCmd(t, Phase(), "complete", "--base", "main"); err != nil {
		t.Fatalf("complete --base main: %v", err)
	}
	if b := mustGit(t, dir, "branch", "--list", "phase/auth"); b != "" {
		t.Errorf("phase/auth should be deleted after a successful complete, got %q", b)
	}
}

// TestPhaseCompleteBaseFlagValidated keeps a typo'd branch name from leaking
// out as a raw git checkout failure halfway through the run.
func TestPhaseCompleteBaseFlagValidated(t *testing.T) {
	dir, _ := completeFixture(t)
	stubPRMerged(t, true)
	mainBefore := mustGit(t, dir, "rev-parse", "main")

	err := runCmd(t, Phase(), "complete", "--base", "does-not-exist")
	if err == nil {
		t.Fatal("expected a refusal for a --base naming no local branch")
	}
	if !strings.Contains(err.Error(), "does-not-exist") {
		t.Errorf("refusal should name the branch: %v", err)
	}
	assertRefusalTouchedNothing(t, dir, "phase/auth", "main", mainBefore)
}

// TestPhaseCompleteReadsBaseOffPhaseRef covers the post-squash stale-tree
// state: the working tree predates ship's record commit, but refs/heads/
// phase/<id> carries it. Without this fallback such a phase would refuse even
// though its base is recorded one ref away.
func TestPhaseCompleteReadsBaseOffPhaseRef(t *testing.T) {
	dir, _ := completeFixture(t)
	stubPRMerged(t, true)

	// Strip the base from the working-tree copy only, leaving the PR number
	// so the merge gate is unaffected — refs/heads/phase/auth still carries
	// the base from the fixture's commit.
	mustWrite(t, filepath.Join(dir, ".dross/phases/auth/changes.json"),
		`{"phase":"auth","pr":42,"tasks":{}}`)
	if base := phaseRefRecordedBase(dir, "auth"); base != "main" {
		t.Fatalf("fixture precondition: phase ref carries base %q, want main", base)
	}
	if err := runCmd(t, Phase(), "complete"); err != nil {
		t.Fatalf("complete should read the base off the phase ref: %v", err)
	}
	if cur := mustGit(t, dir, "symbolic-ref", "--short", "HEAD"); cur != "main" {
		t.Errorf("HEAD = %q, want main", cur)
	}
}

func TestPhaseCompleteRefusesDirtyTree(t *testing.T) {
	dir, _ := completeFixture(t)
	mustWrite(t, filepath.Join(dir, "src/dirty.ts"), "x\n")

	err := runCmd(t, Phase(), "complete")
	if err == nil {
		t.Fatal("expected error on dirty tree")
	}
	if !strings.Contains(err.Error(), "dirty") {
		t.Errorf("error should mention dirty tree: %v", err)
	}
	// The error must name the offending path.
	if !strings.Contains(err.Error(), "dirty.ts") {
		t.Errorf("dirty-tree error should list the offending file: %v", err)
	}
}

func TestPhaseCompleteRefusesUnmergedUpstream(t *testing.T) {
	// Build the post-create state but DON'T push the synthetic squash to
	// origin. The user has done phase work locally but no merge has
	// happened upstream. phase complete must refuse so the user doesn't
	// silently lose the phase branch.
	dir := t.TempDir()
	remoteDir := t.TempDir()
	mustGit(t, remoteDir, "init", "-q", "--bare", "-b", "main")
	gitInit(t, dir, remoteDir)
	chdir(t, dir)
	if err := runCmd(t, Init()); err != nil {
		t.Fatalf("init: %v", err)
	}
	mustWrite(t, filepath.Join(dir, "README.md"), "base\n")
	mustGit(t, dir, "add", ".")
	mustGit(t, dir, "commit", "-q", "-m", "chore: baseline")
	mustGit(t, dir, "push", "-q", "-u", "origin", "main")

	if err := runCmd(t, Phase(), "create", "auth"); err != nil {
		t.Fatalf("phase create: %v", err)
	}
	mustWrite(t, filepath.Join(dir, "src/auth.ts"), "x\n")
	mustGit(t, dir, "add", ".")
	mustGit(t, dir, "commit", "-q", "-m", "feat(auth): scaffold")

	// No PR was recorded (never shipped) and phase/auth was never pushed, so
	// the gate falls back to git ancestry and finds it inconclusive. Even a
	// merged=false provider answer would refuse; assert the fallback refusal.
	stubPRMerged(t, false)
	err := runCmd(t, Phase(), "complete")
	if err == nil {
		t.Fatal("expected error when upstream merge hasn't actually happened")
	}
	if !strings.Contains(err.Error(), "cannot confirm") {
		t.Errorf("error should say the merge can't be confirmed: %v", err)
	}

	// Phase branch must still exist — we didn't lose the work.
	branches := mustGit(t, dir, "branch", "--list", "phase/auth")
	if !strings.Contains(branches, "phase/auth") {
		t.Errorf("phase/auth should still exist after refused complete, got: %q", branches)
	}
}

// TestPhaseCompleteRefusesUnmergedNoLocalBranch closes the escape hatch the
// old guard left open: it was nested under "local phase branch ref exists",
// so an abandoned phase whose local branch was already deleted skipped the
// check entirely and the ff-only silently no-op'd — letting complete
// "succeed" on a never-merged phase. The branch-ref-independent guard reads
// origin/<main> directly, so it must still refuse and touch nothing.
func TestPhaseCompleteRefusesUnmergedNoLocalBranch(t *testing.T) {
	dir := t.TempDir()
	remoteDir := t.TempDir()
	mustGit(t, remoteDir, "init", "-q", "--bare", "-b", "main")
	gitInit(t, dir, remoteDir)
	chdir(t, dir)
	if err := runCmd(t, Init()); err != nil {
		t.Fatalf("init: %v", err)
	}
	mustWrite(t, filepath.Join(dir, "README.md"), "base\n")
	mustGit(t, dir, "add", ".")
	mustGit(t, dir, "commit", "-q", "-m", "chore: baseline")
	mustGit(t, dir, "push", "-q", "-u", "origin", "main")

	if err := runCmd(t, Phase(), "create", "auth"); err != nil {
		t.Fatalf("phase create: %v", err)
	}
	mustWrite(t, filepath.Join(dir, "src/auth.ts"), "x\n")
	mustGit(t, dir, "add", ".")
	mustGit(t, dir, "commit", "-q", "-m", "feat(auth): scaffold")

	// Drop the local phase branch (switch to main first) — origin never got
	// a squash, so there's no `completed auth` record anywhere.
	mustGit(t, dir, "checkout", "-q", "main")
	mustGit(t, dir, "branch", "-D", "phase/auth")
	originMain := mustGit(t, dir, "rev-parse", "main")

	// Name the phase explicitly: neither current_phase nor a phase branch
	// can supply it now — and for the same reason neither can supply the
	// recorded base (the record lived on the deleted branch), so --base is
	// what carries the run through to the merge gate under test.
	stubPRMerged(t, false)
	err := runCmd(t, Phase(), "complete", "auth", "--base", "main")
	if err == nil {
		t.Fatal("expected refusal when local branch is gone AND origin lacks the completion record")
	}
	if !strings.Contains(err.Error(), "cannot confirm") {
		t.Errorf("error should say the merge can't be confirmed: %v", err)
	}

	// Nothing destructive: main is unchanged and the tree is clean.
	if now := mustGit(t, dir, "rev-parse", "main"); now != originMain {
		t.Errorf("main should be untouched by a refused complete: %q != %q", now, originMain)
	}
	if st := mustGit(t, dir, "status", "--porcelain"); st != "" {
		t.Errorf("tree should be clean after refused complete, got: %q", st)
	}
}

// TestPhaseCompleteRefusesDraggedBreadcrumb is the core regression (c-1): a
// later merged phase drags a `completed auth` row onto origin/main, but auth's
// own PR is still open. The old guard passed on the breadcrumb alone; the
// authoritative gate (recorded PR + provider merged-status) must refuse — no
// ff, no branch deletion — so the unmerged phase branch isn't lost.
func TestPhaseCompleteRefusesDraggedBreadcrumb(t *testing.T) {
	dir, _ := completeFixture(t) // origin/main carries `completed auth`; PR 42 recorded
	stubPRMerged(t, false)       // …but PR #42 is NOT actually merged

	// Precondition: the breadcrumb really is dragged onto the base.
	originState := mustGit(t, dir, "show", "origin/main:.dross/state.json")
	if !strings.Contains(originState, "completed auth") {
		t.Fatalf("precondition: origin/main should carry the dragged `completed auth` breadcrumb, got:\n%s", originState)
	}
	originMain := mustGit(t, dir, "rev-parse", "origin/main")

	err := runCmd(t, Phase(), "complete")
	if err == nil {
		t.Fatal("must refuse: the PR is unmerged even though a `completed auth` breadcrumb was dragged onto the base")
	}
	if !strings.Contains(err.Error(), "not merged") {
		t.Errorf("error should say PR #42 is not merged: %v", err)
	}
	// Nothing destructive: the phase branch survives and main didn't ff.
	if b := mustGit(t, dir, "branch", "--list", "phase/auth"); !strings.Contains(b, "phase/auth") {
		t.Errorf("phase/auth must survive a refused complete, got %q", b)
	}
	if now := mustGit(t, dir, "rev-parse", "main"); now == originMain {
		t.Errorf("local main should not have fast-forwarded onto the dragged breadcrumb")
	}
}

// TestPhaseCompleteRefusesWhenMergeInconclusive covers the offline/deleted-ref
// fallback (c-5): origin/main carries a `completed auth` breadcrumb, but there
// is NO recorded PR and origin/phase/auth is absent (squash-deleted / never
// pushed). With no authoritative signal the gate falls back to git ancestry,
// finds it inconclusive, and refuses with guidance — never trusting the
// breadcrumb, never panicking, never false-completing.
func TestPhaseCompleteRefusesWhenMergeInconclusive(t *testing.T) {
	dir, _ := completeFixture(t)

	// Strip the recorded PR so the gate has no authoritative signal; with no
	// [remote] provider configured the real PRMergedFunc can't answer either,
	// so the run reaches the ancestry fallback (no stub).
	mustWrite(t, filepath.Join(dir, ".dross/phases/auth/changes.json"),
		`{"phase":"auth","base":"main","tasks":{}}`)
	mustGit(t, dir, "add", ".dross/phases/auth/changes.json")
	mustGit(t, dir, "commit", "-q", "-m", "chore: drop PR record")

	err := runCmd(t, Phase(), "complete")
	if err == nil {
		t.Fatal("must refuse when the merge can't be confirmed (no PR, ref absent)")
	}
	if !strings.Contains(err.Error(), "cannot confirm") {
		t.Errorf("error should be the guided ancestry-fallback refusal: %v", err)
	}
	// Not destructive, no crash: phase/auth survives and the tree is clean.
	if b := mustGit(t, dir, "branch", "--list", "phase/auth"); !strings.Contains(b, "phase/auth") {
		t.Errorf("phase/auth must survive, got %q", b)
	}
	if st := mustGit(t, dir, "status", "--porcelain"); st != "" {
		t.Errorf("tree should be clean after the refusal, got %q", st)
	}
}

// TestPhaseCompleteDeletesRemoteBranch covers the provider-did-NOT-delete
// case: the phase branch is still live on origin when complete runs, and
// complete must remove it so nothing is left behind.
func TestPhaseCompleteDeletesRemoteBranch(t *testing.T) {
	dir, _ := completeFixture(t)
	stubPRMerged(t, true)

	// Publish the phase branch to origin (provider --delete-branch aborted
	// or never ran).
	mustGit(t, dir, "push", "-q", "origin", "phase/auth")
	if out := mustGit(t, dir, "ls-remote", "--heads", "origin", "phase/auth"); out == "" {
		t.Fatal("precondition: origin should have phase/auth after push")
	}

	if err := runCmd(t, Phase(), "complete"); err != nil {
		t.Fatalf("complete: %v", err)
	}

	// If the remote-delete step is missing, the ref is still on origin here.
	if out := mustGit(t, dir, "ls-remote", "--heads", "origin", "phase/auth"); out != "" {
		t.Errorf("origin should no longer have phase/auth after complete, got: %q", out)
	}
}

// TestPhaseCompleteRemoteDeleteIdempotent covers the provider-ALREADY-deleted
// case: origin has no phase branch (completeFixture never pushes it), so the
// remote delete must be a no-op, not an error.
func TestPhaseCompleteRemoteDeleteIdempotent(t *testing.T) {
	dir, _ := completeFixture(t)
	stubPRMerged(t, true)

	if out := mustGit(t, dir, "ls-remote", "--heads", "origin", "phase/auth"); out != "" {
		t.Fatalf("precondition: origin should have no phase branch, got: %q", out)
	}

	// Must not error trying to delete a remote ref that isn't there.
	if err := runCmd(t, Phase(), "complete"); err != nil {
		t.Fatalf("complete should be idempotent when remote branch absent: %v", err)
	}
}

// TestShipToCompleteLeavesZeroManualGit drives the whole hardened flow
// end-to-end: a real `dross ship` (push + mock-provider PR), an upstream
// squash-merge simulation, then `dross phase complete` — and asserts the
// final state needs no manual git. It runs both branch-of c-3: whether the
// provider's --delete-branch already removed the remote phase branch or not.
func TestShipToCompleteLeavesZeroManualGit(t *testing.T) {
	for _, tc := range []struct {
		name            string
		providerDeleted bool // simulate the provider's PR --delete-branch
	}{
		{"provider did not delete branch", false},
		{"provider already deleted branch", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// The forgejo provider can't answer merged-status, so stub the
			// gate to the authoritative merged answer ship's PR record earns.
			stubPRMerged(t, true)
			// shipFixture (ship_test.go) lands us on phase/x with verify
			// pass and [remote] pointed at a forgejo provider.
			dir := shipFixture(t, "https://forge.example/me/p.git")

			// Swap the fake origin URL for a real bare repo so push works,
			// and publish main so complete can fetch/ff it later.
			remoteDir := t.TempDir()
			mustGit(t, remoteDir, "init", "-q", "--bare", "-b", "main")
			mustGit(t, dir, "remote", "set-url", "origin", remoteDir)
			mustGit(t, dir, "push", "-q", "origin", "main")

			// Mock forgejo so ship's PR-open succeeds.
			t.Setenv("MOCK_FORGEJO_TOKEN", "secret")
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case strings.HasSuffix(r.URL.Path, "/pulls") && r.Method == "POST":
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write([]byte(`{"number":99,"html_url":"https://forge.example/me/p/pulls/99"}`))
				case strings.HasSuffix(r.URL.Path, "/requested_reviewers"):
					_, _ = w.Write([]byte(`[]`))
				default:
					t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
				}
			}))
			t.Cleanup(server.Close)
			if err := runCmd(t, Project(), "set", "remote.api_base", server.URL); err != nil {
				t.Fatal(err)
			}
			gitCommit(t, dir, "test: point api_base at mock")

			// 1) Ship — pushes phase/x to origin, opens the PR, and (the
			//    t-1 fix) commits its state write so the tree is clean.
			if err := runCmd(t, Ship()); err != nil {
				t.Fatalf("ship: %v", err)
			}

			// 2) Simulate the upstream squash-merge onto origin/main. The
			//    squash carries phase/x's src/ AND its .dross/state.json —
			//    ship (t-1) folded the cleared current_phase + `completed
			//    x` record into that state.json, and complete reads it off
			//    origin/main as its merge guard.
			mustGit(t, dir, "fetch", "-q", "origin")
			mustGit(t, dir, "checkout", "-q", "-b", "squash-sim", "origin/main")
			mustGit(t, dir, "checkout", "phase/x", "--", "src/", ".dross/state.json")
			mustGit(t, dir, "add", "src/", ".dross/state.json")
			mustGit(t, dir, "commit", "-q", "-m", "feat(squash): tagging")
			mustGit(t, dir, "push", "-q", "--force", "origin", "squash-sim:main")
			mustGit(t, dir, "checkout", "-q", "phase/x")
			mustGit(t, dir, "branch", "-D", "squash-sim")
			mustGit(t, dir, "fetch", "-q", "origin")

			// Optionally simulate the provider's --delete-branch having run.
			if tc.providerDeleted {
				mustGit(t, dir, "push", "-q", "origin", "--delete", "phase/x")
			}

			// 3) Complete — must finish the job with no manual git either way.
			if err := runCmd(t, Phase(), "complete"); err != nil {
				t.Fatalf("complete: %v", err)
			}

			if st := mustGit(t, dir, "status", "--porcelain"); st != "" {
				t.Errorf("working tree should be clean, got: %q", st)
			}
			if cur := mustGit(t, dir, "symbolic-ref", "--short", "HEAD"); cur != "main" {
				t.Errorf("should be on main, got: %q", cur)
			}
			if b := mustGit(t, dir, "branch", "--list", "phase/*"); b != "" {
				t.Errorf("no local phase branch should remain, got: %q", b)
			}
			if r := mustGit(t, dir, "ls-remote", "--heads", "origin", "phase/x"); r != "" {
				t.Errorf("origin should have no phase/x ref, got: %q", r)
			}
		})
	}
}

// TestConsecutivePhasesNoDivergence proves the fix eliminates main
// divergence rather than deferring it one cycle (c-3). It runs the full
// ship → squash → complete loop for two phases back-to-back and asserts
// local main never diverges from origin/main. Under the old behaviour,
// completing phase 1 left a standalone unpushed `chore(dross): complete`
// commit on local main; phase 2 then forked off that commit, the squash
// baked it into origin, and phase 2's completion ff aborted on diverging
// branches. With ship folding the record into the squash and complete
// writing no commit, both completions leave main exactly at origin.
func TestConsecutivePhasesNoDivergence(t *testing.T) {
	stubPRMerged(t, true) // forgejo can't answer merged-status; ship records the PR
	dir := shipFixture(t, "https://forge.example/me/p.git")

	remoteDir := t.TempDir()
	mustGit(t, remoteDir, "init", "-q", "--bare", "-b", "main")
	mustGit(t, dir, "remote", "set-url", "origin", remoteDir)
	mustGit(t, dir, "push", "-q", "origin", "main")

	t.Setenv("MOCK_FORGEJO_TOKEN", "secret")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/pulls") && r.Method == "POST":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"number":99,"html_url":"https://forge.example/me/p/pulls/99"}`))
		case strings.HasSuffix(r.URL.Path, "/requested_reviewers"):
			_, _ = w.Write([]byte(`[]`))
		default:
			_, _ = w.Write([]byte(`{}`))
		}
	}))
	t.Cleanup(server.Close)
	if err := runCmd(t, Project(), "set", "remote.api_base", server.URL); err != nil {
		t.Fatal(err)
	}
	gitCommit(t, dir, "test: point api_base at mock")

	// cycle ships the current phase, simulates the upstream squash-merge
	// (carrying the folded state.json), completes it, and asserts local main
	// has not diverged from origin/main.
	cycle := func(phaseID, branch string) {
		t.Helper()
		if err := runCmd(t, Ship()); err != nil {
			t.Fatalf("ship %s: %v", phaseID, err)
		}
		mustGit(t, dir, "fetch", "-q", "origin")
		mustGit(t, dir, "checkout", "-q", "-b", "squash-sim", "origin/main")
		// The squash carries the phase's src/, its folded state.json, and
		// project.toml (config lands on main via the squash in production
		// too — without it the mock api_base wouldn't reach the next phase).
		mustGit(t, dir, "checkout", branch, "--", "src/", ".dross/state.json", ".dross/project.toml")
		mustGit(t, dir, "add", "src/", ".dross/state.json", ".dross/project.toml")
		mustGit(t, dir, "commit", "-q", "-m", "feat(squash): "+phaseID)
		mustGit(t, dir, "push", "-q", "--force", "origin", "squash-sim:main")
		mustGit(t, dir, "checkout", "-q", branch)
		mustGit(t, dir, "branch", "-D", "squash-sim")
		mustGit(t, dir, "fetch", "-q", "origin")

		if err := runCmd(t, Phase(), "complete", phaseID); err != nil {
			t.Fatalf("complete %s: %v", phaseID, err)
		}
		// No divergence: completion left local main exactly at origin/main.
		// Under the old behaviour these differ by a standalone unpushed
		// `chore(dross): complete` commit.
		localMain := mustGit(t, dir, "rev-parse", "main")
		originMain := mustGit(t, dir, "rev-parse", "origin/main")
		if localMain != originMain {
			t.Fatalf("after completing %s, local main %s diverged from origin/main %s",
				phaseID, localMain, originMain)
		}
	}

	// Phase 1 — already set up by shipFixture (on phase/x).
	cycle("x", "phase/x")

	// Phase 2 — fork a fresh phase off the now-clean main and run it through
	// the same loop. If phase 1's completion had re-seeded divergence, this
	// phase would inherit it and its completion ff would break. Read the
	// created id back from state rather than assuming its ordinal.
	if err := runCmd(t, Phase(), "create", "y"); err != nil {
		t.Fatalf("phase create y: %v", err)
	}
	s2, _ := state.Load(filepath.Join(dir, ".dross", "state.json"))
	id2 := s2.CurrentPhase
	if id2 == "" {
		t.Fatal("phase create should set current_phase for the new phase")
	}
	phaseDir := filepath.Join(dir, ".dross", "phases", id2)
	mustWrite(t, filepath.Join(phaseDir, "spec.toml"), fmt.Sprintf(`[phase]
id = %q
title = "Second"

[[criteria]]
id = "C1"
text = "works"
`, id2))
	mustWrite(t, filepath.Join(phaseDir, "verify.toml"), fmt.Sprintf(`[verify]
phase = %q
generated_at = 2026-05-02T10:00:00Z
verdict = "pass"

[summary]
mutation_score = 0.85
mutants_killed = 17
mutants_survived = 3
criteria_total = 1
criteria_covered = 1
criteria_uncovered = 0

[[criterion]]
id = "C1"
status = "covered"
tests = ["y.test.ts:1"]
`, id2))
	mustWrite(t, filepath.Join(dir, "src/y.ts"), "export const y = 1\n")
	gitCommit(t, dir, "feat(y): second phase")
	cycle(id2, "phase/"+id2)

	// Audit survives: both completions are recorded on main after the loop.
	s, _ := state.Load(filepath.Join(dir, ".dross", "state.json"))
	var has1, has2 bool
	for _, a := range s.History {
		if strings.Contains(a.Action, "completed x") {
			has1 = true
		}
		if strings.Contains(a.Action, "completed "+id2) {
			has2 = true
		}
	}
	if !has1 || !has2 {
		t.Errorf("main should carry both completion records; has x=%v %s=%v; history=%+v",
			has1, id2, has2, s.History)
	}
}

// divergedCompleteFixture builds a TRUE divergence for `dross phase complete`:
// origin/main carries the PR squash (with the `completed auth` record but not
// every .dross/ artefact), while local main carries the SAME completion record
// plus an extra `.dross/phases/auth/spec.toml` the squash lost. Local main and
// origin/main share only the baseline ancestor, so the ff-only aborts. Returns
// repo dir, phase id, and the pre-recovery local main SHA (for byte-for-byte
// no-op assertions). Leaves the working copy on phase/auth.
func divergedCompleteFixture(t *testing.T) (string, string, string) {
	t.Helper()
	dir := t.TempDir()
	remoteDir := t.TempDir()
	mustGit(t, remoteDir, "init", "-q", "--bare", "-b", "main")
	gitInit(t, dir, remoteDir)
	chdir(t, dir)

	if err := runCmd(t, Init()); err != nil {
		t.Fatalf("init: %v", err)
	}
	mustWrite(t, filepath.Join(dir, "README.md"), "base\n")
	mustGit(t, dir, "add", ".")
	mustGit(t, dir, "commit", "-q", "-m", "chore: baseline")
	mustGit(t, dir, "push", "-q", "-u", "origin", "main")

	if err := runCmd(t, Phase(), "create", "auth"); err != nil {
		t.Fatalf("phase create: %v", err)
	}
	mustWrite(t, filepath.Join(dir, "src/auth.ts"), "x\n")
	mustGit(t, dir, "add", ".")
	mustGit(t, dir, "commit", "-q", "-m", "feat(auth): scaffold")

	// Record a PR on phase/auth so the merge gate passes (tests stub
	// PRMergedFunc=true) and the run reaches the ff-divergence logic under
	// test. The phase's spec.toml lives here too — on the phase branch, where
	// /dross-spec writes it — and it's the artefact the origin squash drops
	// and recovery must restore from the phase tip.
	mustWrite(t, filepath.Join(dir, ".dross/phases/auth/spec.toml"), `id = "auth"`)
	mustWrite(t, filepath.Join(dir, ".dross/phases/auth/changes.json"),
		`{"phase":"auth","pr":77,"base":"main","tasks":{}}`)
	mustGit(t, dir, "add", ".dross/phases/auth/")
	mustGit(t, dir, "commit", "-q", "-m", "chore(dross): record PR #77 for auth")

	stPath := filepath.Join(dir, ".dross", "state.json")

	// Origin squash: src/ + completion record, but no phase .dross/ artefacts.
	mustGit(t, dir, "checkout", "-q", "-b", "squash-sim", "origin/main")
	mustGit(t, dir, "checkout", "phase/auth", "--", "src/")
	mustGit(t, dir, "add", "src/")
	sq, err := state.Load(stPath)
	if err != nil {
		t.Fatalf("load squash state: %v", err)
	}
	sq.CurrentPhase = ""
	sq.CurrentPhaseStatus = ""
	sq.Touch("completed auth")
	if err := sq.Save(stPath); err != nil {
		t.Fatal(err)
	}
	mustGit(t, dir, "add", filepath.Join(".dross", "state.json"))
	mustGit(t, dir, "commit", "-q", "-m", "feat(squash): auth")
	mustGit(t, dir, "push", "-q", "--force", "origin", "squash-sim:main")
	mustGit(t, dir, "checkout", "-q", "main")
	mustGit(t, dir, "branch", "-D", "squash-sim")

	// Local main diverges: its own completion record, built on local main
	// (baseline), so it shares only baseline with origin/main -> ff-only
	// aborts. The phase artefact is deliberately NOT written here — it lives
	// on the phase branch, and recovery sourcing it from there is the property
	// under test.
	lm, err := state.Load(stPath)
	if err != nil {
		t.Fatal(err)
	}
	lm.CurrentPhase = ""
	lm.CurrentPhaseStatus = ""
	lm.Touch("completed auth")
	if err := lm.Save(stPath); err != nil {
		t.Fatal(err)
	}
	mustGit(t, dir, "add", ".dross/")
	mustGit(t, dir, "commit", "-q", "-m", "chore(dross): complete auth")
	mainSHA := mustGit(t, dir, "rev-parse", "main")
	mustGit(t, dir, "fetch", "-q", "origin")

	mustGit(t, dir, "checkout", "-q", "phase/auth")
	return dir, "auth", mainSHA
}

// TestPhaseCompleteDivergedNoFlagStops (c-1): without --recover, a diverged
// main makes complete refuse with a pointer to --recover, and local main is
// byte-for-byte unchanged — no partial reset.
func TestPhaseCompleteDivergedNoFlagStops(t *testing.T) {
	dir, _, mainSHA := divergedCompleteFixture(t)
	stubPRMerged(t, true) // gate passes; the ff-divergence refusal is under test

	err := runCmd(t, Phase(), "complete")
	if err == nil {
		t.Fatal("expected complete to refuse on a diverged main without --recover")
	}
	if !strings.Contains(err.Error(), "--recover") {
		t.Errorf("error should point at --recover: %v", err)
	}
	if got := mustGit(t, dir, "rev-parse", "main"); got != mainSHA {
		t.Errorf("local main must be unchanged after a refused complete: was %s, now %s", mainSHA, got)
	}
}

// TestPhaseCompleteRecoverHeals (c-1): with --recover, complete resets main to
// origin, restores the .dross/ artefact the squash lost, deletes the phase
// branch, and finishes on a clean tree — zero manual git.
func TestPhaseCompleteRecoverHeals(t *testing.T) {
	dir, _, _ := divergedCompleteFixture(t)
	stubPRMerged(t, true) // gate passes; the --recover heal path is under test

	if err := runCmd(t, Phase(), "complete", "--recover"); err != nil {
		t.Fatalf("complete --recover should heal a diverged main: %v", err)
	}

	if cur := mustGit(t, dir, "symbolic-ref", "--short", "HEAD"); cur != "main" {
		t.Errorf("expected HEAD on main after recovery, got %q", cur)
	}
	if branches := mustGit(t, dir, "branch", "--list", "phase/*"); branches != "" {
		t.Errorf("phase/* should be deleted after recovery, got: %q", branches)
	}
	// The cumulative .dross/ tree is restored — including the artefact the
	// origin squash dropped.
	headTree := mustGit(t, dir, "ls-tree", "-r", "--name-only", "HEAD")
	if !strings.Contains(headTree, ".dross/phases/auth/spec.toml") {
		t.Errorf("recovery should restore the dropped .dross/ artefact:\n%s", headTree)
	}
	// Completion record survives on HEAD.
	s, _ := state.Load(filepath.Join(dir, ".dross", "state.json"))
	found := false
	for _, a := range s.History {
		if strings.Contains(a.Action, "completed auth") {
			found = true
		}
	}
	if !found {
		t.Errorf("completion record should survive recovery: %+v", s.History)
	}
	if status := mustGit(t, dir, "status", "--porcelain"); status != "" {
		t.Errorf("working tree should be clean after recovery, got: %q", status)
	}
	// c-2 tail: the restore commit is pushed, so main doesn't sit ahead again.
	if ahead := mustGit(t, dir, "rev-list", "origin/main..main"); ahead != "" {
		t.Errorf("the restore commit must be pushed (origin/main..main empty), got: %q", ahead)
	}
}

// TestPhaseCompleteRecoverSourcesTreeFromPhaseTip pins the c-4 split. The
// restored .dross/ tree comes from the phase branch's tip — the only commit
// holding this phase's records — while state.json comes from the base, whose
// cumulative history is the one that must survive. Sourcing the whole tree
// from HEAD (the branch being reset) restores nothing the phase contributed.
func TestPhaseCompleteRecoverSourcesTreeFromPhaseTip(t *testing.T) {
	dir, _, _ := divergedCompleteFixture(t)
	stubPRMerged(t, true)

	// A file that exists ONLY on the phase tip, and a state.json history entry
	// that exists ONLY on the base — one probe per side of the split.
	mustWrite(t, filepath.Join(dir, ".dross/phases/auth/plan.toml"), "phase-only\n")
	mustGit(t, dir, "add", ".dross/phases/auth/plan.toml")
	mustGit(t, dir, "commit", "-q", "-m", "chore(dross): phase-only artefact")

	if err := runCmd(t, Phase(), "complete", "--recover"); err != nil {
		t.Fatalf("complete --recover: %v", err)
	}

	headTree := mustGit(t, dir, "ls-tree", "-r", "--name-only", "HEAD")
	if !strings.Contains(headTree, ".dross/phases/auth/plan.toml") {
		t.Errorf("recovery must restore the phase tip's tree:\n%s", headTree)
	}
	// The phase's own record survives the restore — it is the thing a later
	// re-run of complete would read the PR and base back out of.
	ch, err := changes.Load(changes.FilePath(filepath.Join(dir, ".dross"), "auth"), "auth")
	if err != nil {
		t.Fatal(err)
	}
	if ch.PR != 77 {
		t.Errorf("restored changes.json lost the PR record: %+v", ch)
	}
	// state.json is the BASE's copy: it carries the completion record local
	// main committed, which never existed on the phase branch.
	s, _ := state.Load(filepath.Join(dir, ".dross", "state.json"))
	found := false
	for _, a := range s.History {
		if strings.Contains(a.Action, "completed auth") {
			found = true
		}
	}
	if !found {
		t.Errorf("state.json should be the base's copy, carrying its completion record: %+v", s.History)
	}
}

// TestPhaseCompleteRecoverLeavesStaleMilestoneAlone is the destructive half of
// the incident: --recover is a hard reset, and pointing it at an inferred
// milestone branch the phase never forked from would reset a branch holding
// unrelated work. It must reset only the RECORDED base.
func TestPhaseCompleteRecoverLeavesStaleMilestoneAlone(t *testing.T) {
	dir, _, _ := divergedCompleteFixture(t) // base recorded as "main"
	stubPRMerged(t, true)

	// A stale milestone branch with a commit of its own, plus an active
	// milestone — everything inference needed to target it.
	mustGit(t, dir, "branch", "milestone/v0.9", "main")
	if err := runCmd(t, State(), "set", "current_milestone", "v0.9"); err != nil {
		t.Fatalf("state set: %v", err)
	}
	mustGit(t, dir, "add", ".dross/")
	mustGit(t, dir, "commit", "-q", "-m", "chore(dross): scope v0.9")
	msBefore := mustGit(t, dir, "rev-parse", "milestone/v0.9")

	if err := runCmd(t, Phase(), "complete", "--recover"); err != nil {
		t.Fatalf("complete --recover: %v", err)
	}
	if got := mustGit(t, dir, "rev-parse", "milestone/v0.9"); got != msBefore {
		t.Errorf("--recover reset a branch the phase never forked from: %s -> %s", msBefore, got)
	}
}

// TestPhaseCompleteHelpDescribesRecoverUnderMilestone (c-6) keeps the help
// text honest: it claimed --recover was "not yet supported" under a milestone,
// which stopped being true once the reset started following the recorded base.
func TestPhaseCompleteHelpDescribesRecoverUnderMilestone(t *testing.T) {
	long := phaseComplete().Long

	if strings.Contains(long, "not yet supported") {
		t.Error("--help still claims --recover is unsupported under a milestone")
	}
	for _, needle := range []string{"milestone/", "--base", "--recover"} {
		if !strings.Contains(long, needle) {
			t.Errorf("--help should mention %q", needle)
		}
	}
}

// c-4 verify-before-reset: --recover with an UNMERGED recorded PR refuses at
// the merge gate BEFORE any git reset --hard — local main is byte-for-byte
// unchanged after the refusal.
func TestPhaseCompleteRecoverUnmergedPRRefusesBeforeReset(t *testing.T) {
	dir, _, mainSHA := divergedCompleteFixture(t)
	stubPRMerged(t, false) // the recorded PR is authoritatively NOT merged

	err := runCmd(t, Phase(), "complete", "--recover")
	if err == nil {
		t.Fatal("expected the merge gate to refuse --recover on an unmerged PR")
	}
	if !strings.Contains(err.Error(), "not merged") {
		t.Errorf("refusal should say the PR isn't merged: %v", err)
	}
	if got := mustGit(t, dir, "rev-parse", "main"); got != mainSHA {
		t.Errorf("local main must be untouched when the gate refuses: was %s, now %s", mainSHA, got)
	}
}

// TestPhaseCompleteRecoverRefusesDirty (c-3): --recover on a diverged AND dirty
// tree aborts with the offending file named, leaving local main byte-for-byte
// unchanged — the pre-recovery clean-tree guard fires before any reset.
func TestPhaseCompleteRecoverRefusesDirty(t *testing.T) {
	dir, _, mainSHA := divergedCompleteFixture(t)
	mustWrite(t, filepath.Join(dir, "src/dirty.ts"), "x\n")

	err := runCmd(t, Phase(), "complete", "--recover")
	if err == nil {
		t.Fatal("expected complete --recover to refuse on a dirty tree")
	}
	if !strings.Contains(err.Error(), "dirty.ts") {
		t.Errorf("dirty-tree error should name the offending file: %v", err)
	}
	if got := mustGit(t, dir, "rev-parse", "main"); got != mainSHA {
		t.Errorf("local main must be unchanged when recovery aborts on a dirty tree: was %s, now %s", mainSHA, got)
	}
}

// TestPhaseCover_CompleteArgResolvesPhaseID exercises the `len(args) == 1`
// switch arm (phase.go:255). With an explicit id AND an empty current_phase,
// resolution must take args[0] (non-empty), so the run advances to base
// resolution and refuses by NAME. Negating the arm would drop through to the
// branch fallback on `main`, yield an empty id, and fail with "no phase id
// given" instead — the two errors distinguish the mutant.
func TestPhaseCover_CompleteArgResolvesPhaseID(t *testing.T) {
	dir := t.TempDir()
	gitInit(t, dir, "")
	chdir(t, dir)
	if err := runCmd(t, Init()); err != nil {
		t.Fatalf("init: %v", err)
	}
	// current_phase stays empty (Init leaves it so); dirty the tree so a
	// resolved non-empty id lands on the dirty guard, not any git-network step.
	mustWrite(t, filepath.Join(dir, "uncommitted.txt"), "x\n")

	err := runCmd(t, Phase(), "complete", "explicit-phase")
	if err == nil {
		t.Fatal("expected an error once the id resolves from args[0]")
	}
	if !strings.Contains(err.Error(), "explicit-phase") {
		t.Errorf("args[0] should resolve the id and be named in the refusal; got: %v", err)
	}
	if strings.Contains(err.Error(), "no phase id given") {
		t.Errorf("args[0] arm must supply the id, not fall through to the empty-id error: %v", err)
	}
}

// TestPhaseCover_CompleteStateResolvesPhaseID exercises the
// `s.CurrentPhase != ""` switch arm (phase.go:257). With NO args but a set
// current_phase, resolution must take state (non-empty) and refuse by NAME at
// base resolution. Negating the arm to `== ""` would skip it, fall through to
// the branch fallback on `main`, yield an empty id, and fail with "no phase id
// given".
func TestPhaseCover_CompleteStateResolvesPhaseID(t *testing.T) {
	dir := t.TempDir()
	gitInit(t, dir, "")
	chdir(t, dir)
	if err := runCmd(t, Init()); err != nil {
		t.Fatalf("init: %v", err)
	}
	if err := runCmd(t, State(), "set", "current_phase", "state-phase"); err != nil {
		t.Fatalf("set current_phase: %v", err)
	}
	mustWrite(t, filepath.Join(dir, "uncommitted.txt"), "x\n")

	err := runCmd(t, Phase(), "complete")
	if err == nil {
		t.Fatal("expected an error once the id resolves from current_phase")
	}
	if !strings.Contains(err.Error(), "state-phase") {
		t.Errorf("current_phase should resolve the id and be named in the refusal; got: %v", err)
	}
	if strings.Contains(err.Error(), "no phase id given") {
		t.Errorf("current_phase arm must supply the id: %v", err)
	}
}

// TestPhaseCover_ShowErrorsWithoutRoot exercises the FindRoot error guard in
// phase show (phase.go:460). With no .dross up the tree, FindRoot errors and
// show must propagate it. Negating `if err != nil` would swallow the error,
// continue with an empty root, and return nil after printing "(missing)".
func TestPhaseCover_ShowErrorsWithoutRoot(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	if err := runCmd(t, Phase(), "show", "whatever"); err == nil {
		t.Fatal("expected an error when no .dross root is found")
	}
}

// TestPhaseCover_ShowMissingVsPresent exercises both branches of the ReadFile
// error check in phase show (phase.go:467): a present spec.toml prints its body,
// an absent plan.toml prints the "(missing)" placeholder. Negating the check
// swaps the two — a present file would print "(missing)" and a missing one
// would print an empty body — so each assertion pins one direction.
func TestPhaseCover_ShowMissingVsPresent(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	if err := runCmd(t, Init()); err != nil {
		t.Fatalf("init: %v", err)
	}
	// spec.toml present, plan.toml absent.
	mustWrite(t, filepath.Join(dir, ".dross", "phases", "myp", "spec.toml"),
		`id = "myp" # PHASESHOW_SPEC_MARKER`)

	out := captureStdout(t, func() {
		if err := runCmd(t, Phase(), "show", "myp"); err != nil {
			t.Fatalf("show: %v", err)
		}
	})
	if !strings.Contains(out, "PHASESHOW_SPEC_MARKER") {
		t.Errorf("present spec.toml body should be printed; got:\n%s", out)
	}
	if !strings.Contains(out, "(missing)") {
		t.Errorf("absent plan.toml should print the (missing) placeholder; got:\n%s", out)
	}
}

func TestPhaseCreateRootsOnMilestoneBranch(t *testing.T) {
	dir := initWithGit(t)
	// Activate the milestone and commit it, so the tree is clean before create.
	if err := runCmd(t, State(), "set", "current_milestone", "v0.9"); err != nil {
		t.Fatalf("state set: %v", err)
	}
	mustGit(t, dir, "add", ".")
	mustGit(t, dir, "commit", "-q", "-m", "scope v0.9")

	// A milestone branch carrying a commit that is NOT on main.
	mustGit(t, dir, "branch", "milestone/v0.9")
	mustGit(t, dir, "checkout", "-q", "milestone/v0.9")
	mustWrite(t, filepath.Join(dir, "ms.txt"), "x\n")
	mustGit(t, dir, "add", ".")
	mustGit(t, dir, "commit", "-q", "-m", "milestone only")
	msCommit := mustGit(t, dir, "rev-parse", "HEAD")
	mustGit(t, dir, "checkout", "-q", "main")

	if err := runCmd(t, Phase(), "create", "auth"); err != nil {
		t.Fatalf("create: %v", err)
	}
	// Ancestor probe, not tip equality: tips coincide when main==milestone, so
	// only ancestry proves phase/auth forked off the milestone branch.
	if err := gitNoOut(dir, "merge-base", "--is-ancestor", msCommit, "refs/heads/phase/auth"); err != nil {
		t.Errorf("phase/auth not rooted on milestone/v0.9 (milestone commit not ancestor): %v", err)
	}
}

// readRecordedBase reads the forked-from base out of a phase's changes.json.
func readRecordedBase(t *testing.T, dir, phaseID string) string {
	t.Helper()
	ch, err := changes.Load(changes.FilePath(filepath.Join(dir, ".dross"), phaseID), phaseID)
	if err != nil {
		t.Fatalf("load changes for %s: %v", phaseID, err)
	}
	return ch.Base
}

// TestPhaseCreateRecordsBaseOnMain is the create-time half of base_write_timing
// (c-2): every phase carries the branch it actually forked from from the moment
// it exists, so `dross phase complete` never has to infer one.
func TestPhaseCreateRecordsBaseOnMain(t *testing.T) {
	dir := initWithGit(t)

	if err := runCmd(t, Phase(), "create", "auth"); err != nil {
		t.Fatalf("create: %v", err)
	}
	if got := readRecordedBase(t, dir, "auth"); got != "main" {
		t.Errorf("recorded base = %q, want %q", got, "main")
	}
}

// TestPhaseCreateRecordsMilestoneBase pins that the recorded value is the
// branch actually forked from, not a re-derivation. Under a milestone the fork
// roots on milestone/<version>, and that is what completion must reconcile
// against — recording "main" here is exactly the stale-base bug.
func TestPhaseCreateRecordsMilestoneBase(t *testing.T) {
	dir := initWithGit(t)
	if err := runCmd(t, State(), "set", "current_milestone", "v0.9"); err != nil {
		t.Fatalf("state set: %v", err)
	}
	mustGit(t, dir, "add", ".")
	mustGit(t, dir, "commit", "-q", "-m", "scope v0.9")
	mustGit(t, dir, "branch", "milestone/v0.9")

	if err := runCmd(t, Phase(), "create", "auth"); err != nil {
		t.Fatalf("create: %v", err)
	}
	if got := readRecordedBase(t, dir, "auth"); got != "milestone/v0.9" {
		t.Errorf("recorded base = %q, want %q", got, "milestone/v0.9")
	}
}

// TestPhaseInsertRecordsBase covers the second call site: an inserted phase is
// forked the same way as a created one, so it must carry a base too — a
// base-less phase is un-completable without an explicit --base.
func TestPhaseInsertRecordsBase(t *testing.T) {
	dir := initWithGit(t)
	root := filepath.Join(dir, ".dross")
	mustWrite(t, filepath.Join(root, "milestones", "v1.toml"),
		"phases = [\"auth\"]\n\n[milestone]\n  version = \"v1\"\n  title = \"M\"\n")
	mustWrite(t, filepath.Join(root, "phases", "auth", "spec.toml"),
		"[phase]\n  id = \"auth\"\n  title = \"auth\"\n\n[[criteria]]\n  id = \"c-1\"\n  text = \"x\"\n")
	if err := runCmd(t, State(), "set", "current_milestone", "v1"); err != nil {
		t.Fatalf("state set: %v", err)
	}
	mustGit(t, dir, "add", ".")
	mustGit(t, dir, "commit", "-q", "-m", "scope v1")

	if err := runCmd(t, Phase(), "insert", "billing", "--after", "auth"); err != nil {
		t.Fatalf("insert: %v", err)
	}
	if got := readRecordedBase(t, dir, "billing"); got != "main" {
		t.Errorf("inserted phase's recorded base = %q, want %q — a base-less phase is un-completable without --base", got, "main")
	}
}

// TestPhaseCreateFailedForkLeavesNoRecord pins the write ordering: the base is
// recorded only after `checkout -b` succeeds. create's rollback is
// os.Remove(dir), which only removes an EMPTY directory, so a record written
// before the fork would survive the rollback and leak the phase id — every
// retry would then land on a suffixed slug.
func TestPhaseCreateFailedForkLeavesNoRecord(t *testing.T) {
	dir := initWithGit(t)
	mustGit(t, dir, "branch", "phase/auth") // the fork will collide with this

	if err := runCmd(t, Phase(), "create", "auth"); err == nil {
		t.Fatal("expected create to fail when phase/auth already exists")
	}
	if _, err := os.Stat(filepath.Join(dir, ".dross", "phases", "auth")); !os.IsNotExist(err) {
		t.Errorf("failed fork leaked .dross/phases/auth (stat err = %v) — the rollback only removes an empty dir", err)
	}
}

func TestPhaseCreateRootsOnMainNoMilestone(t *testing.T) {
	dir := initWithGit(t)
	if err := runCmd(t, Phase(), "create", "auth"); err != nil {
		t.Fatalf("create: %v", err)
	}
	mainTip := mustGit(t, dir, "rev-parse", "main")
	phaseTip := mustGit(t, dir, "rev-parse", "phase/auth")
	if mainTip != phaseTip {
		t.Errorf("phase/auth tip %s != main tip %s (should root on main with no milestone)", phaseTip, mainTip)
	}
}

func TestPhaseCreateNudgesNoMilestone(t *testing.T) {
	initWithGit(t)
	out := captureStdout(t, func() {
		if err := runCmd(t, Phase(), "create", "auth"); err != nil {
			t.Fatalf("create: %v", err)
		}
	})
	if !strings.Contains(out, "dross milestone") {
		t.Errorf("no-milestone create should nudge naming `dross milestone`; got:\n%s", out)
	}
}

// completeMilestoneFixture mirrors completeFixture but under an active milestone:
// the phase forks off milestone/<version>, and the simulated squash-merge lands
// on origin/milestone/<version> (not origin/main). Local milestone/<version> is
// left behind origin so complete can fast-forward it. Returns dir, phase id, version.
func completeMilestoneFixture(t *testing.T) (string, string, string) {
	t.Helper()
	version := "v0.9"
	dir := t.TempDir()
	remoteDir := t.TempDir()
	mustGit(t, remoteDir, "init", "-q", "--bare", "-b", "main")
	gitInit(t, dir, remoteDir)
	chdir(t, dir)
	if err := runCmd(t, Init()); err != nil {
		t.Fatalf("init: %v", err)
	}
	mustWrite(t, filepath.Join(dir, "README.md"), "base\n")
	mustGit(t, dir, "add", ".")
	mustGit(t, dir, "commit", "-q", "-m", "chore: baseline")

	// Activate the milestone, then cut + push its integration branch.
	if err := runCmd(t, State(), "set", "current_milestone", version); err != nil {
		t.Fatalf("state set: %v", err)
	}
	mustGit(t, dir, "add", ".")
	mustGit(t, dir, "commit", "-q", "-m", "chore: scope "+version)
	mustGit(t, dir, "push", "-q", "-u", "origin", "main")
	mustGit(t, dir, "branch", "milestone/"+version)
	mustGit(t, dir, "push", "-q", "-u", "origin", "milestone/"+version)

	// Phase forks off the milestone branch (t-3 behaviour).
	if err := runCmd(t, Phase(), "create", "auth"); err != nil {
		t.Fatalf("phase create: %v", err)
	}
	mustWrite(t, filepath.Join(dir, "src/auth.ts"), "x\n")
	mustGit(t, dir, "add", ".")
	mustGit(t, dir, "commit", "-q", "-m", "feat(auth): scaffold")

	// Record a PR number on phase/auth so complete's merge gate has a PR to
	// look up (tests stub PRMergedFunc for the answer), plus the base the PR
	// was opened against — the milestone branch, which is what completion must
	// reconcile.
	mustWrite(t, filepath.Join(dir, ".dross/phases/auth/changes.json"),
		fmt.Sprintf(`{"phase":"auth","pr":42,"base":"milestone/%s","tasks":{}}`, version))
	mustGit(t, dir, "add", ".dross/phases/auth/changes.json")
	mustGit(t, dir, "commit", "-q", "-m", "chore(dross): record PR #42 for auth")

	// Simulate the upstream squash-merge onto the MILESTONE branch: a synthetic
	// squash on top of origin/milestone/<v> carrying the completion record that
	// complete reads as its merge guard.
	mustGit(t, dir, "checkout", "-q", "-b", "squash-sim", "origin/milestone/"+version)
	mustGit(t, dir, "checkout", "phase/auth", "--", "src/")
	mustGit(t, dir, "add", "src/")
	stPath := filepath.Join(dir, ".dross", "state.json")
	sq, err := state.Load(stPath)
	if err != nil {
		t.Fatalf("load squash state: %v", err)
	}
	sq.CurrentPhase = ""
	sq.CurrentPhaseStatus = ""
	sq.Touch("completed auth")
	if err := sq.Save(stPath); err != nil {
		t.Fatal(err)
	}
	mustGit(t, dir, "add", filepath.Join(".dross", "state.json"))
	mustGit(t, dir, "commit", "-q", "-m", "feat(squash): auth")
	mustGit(t, dir, "push", "-q", "--force", "origin", "squash-sim:milestone/"+version)
	mustGit(t, dir, "checkout", "-q", "phase/auth")
	mustGit(t, dir, "branch", "-D", "squash-sim")
	mustGit(t, dir, "fetch", "-q", "origin")
	return dir, "auth", version
}

func TestPhaseCompleteFastForwardsMilestone(t *testing.T) {
	dir, _, version := completeMilestoneFixture(t)
	stubPRMerged(t, true)
	if err := runCmd(t, Phase(), "complete"); err != nil {
		t.Fatalf("complete: %v", err)
	}
	// Local milestone branch fast-forwarded to origin (not main).
	local := mustGit(t, dir, "rev-parse", "milestone/"+version)
	origin := mustGit(t, dir, "rev-parse", "origin/milestone/"+version)
	if local != origin {
		t.Errorf("milestone/%s not ff'd to origin: local %s != origin %s", version, local, origin)
	}
	if cur := mustGit(t, dir, "symbolic-ref", "--short", "HEAD"); cur != "milestone/"+version {
		t.Errorf("HEAD = %q; want milestone/%s (reconcile branch, not main)", cur, version)
	}
	if b := mustGit(t, dir, "branch", "--list", "phase/*"); b != "" {
		t.Errorf("phase/* should be deleted, got %q", b)
	}
}

func TestPhaseCompleteNoMilestoneFfsMain(t *testing.T) {
	dir, _ := completeFixture(t) // no milestone active → main reconcile preserved
	stubPRMerged(t, true)
	if err := runCmd(t, Phase(), "complete"); err != nil {
		t.Fatalf("complete: %v", err)
	}
	if cur := mustGit(t, dir, "symbolic-ref", "--short", "HEAD"); cur != "main" {
		t.Errorf("no-milestone complete should ff main; HEAD = %q", cur)
	}
	if b := mustGit(t, dir, "branch", "--list", "phase/*"); b != "" {
		t.Errorf("phase/* should be deleted, got %q", b)
	}
}

// divergeMilestone puts a local-only commit on milestone/<version> (origin
// carries the squash the local branch doesn't → true divergence), returning
// the pre-divergence local tip. Leaves the working copy on phase/auth.
func divergeMilestone(t *testing.T, dir, version string) string {
	t.Helper()
	mustGit(t, dir, "checkout", "-q", "milestone/"+version)
	mustWrite(t, filepath.Join(dir, "local-only.txt"), "x\n")
	mustGit(t, dir, "add", ".")
	mustGit(t, dir, "commit", "-q", "-m", "local divergence")
	local := mustGit(t, dir, "rev-parse", "milestone/"+version)
	mustGit(t, dir, "checkout", "-q", "phase/auth")
	return local
}

// Without --recover, a diverged milestone base still refuses non-destructively
// — but now pointing at --recover (c-5 dropped the milestone-unsupported abort).
func TestPhaseCompleteMilestoneDivergedNoFlagStops(t *testing.T) {
	dir, _, version := completeMilestoneFixture(t)
	stubPRMerged(t, true) // gate passes; the ff-divergence refusal is under test
	localBefore := divergeMilestone(t, dir, version)

	err := runCmd(t, Phase(), "complete")
	if err == nil {
		t.Fatal("expected non-destructive refusal on a diverged milestone branch without --recover")
	}
	if !strings.Contains(err.Error(), "--recover") {
		t.Errorf("refusal should point at --recover: %v", err)
	}
	if strings.Contains(err.Error(), "does not yet support") {
		t.Errorf("the milestone-unsupported abort must be gone: %v", err)
	}
	// Nothing reset: local milestone tip unchanged and phase branch still present.
	if after := mustGit(t, dir, "rev-parse", "milestone/"+version); after != localBefore {
		t.Errorf("milestone/%s tip changed (should be untouched): %s -> %s", version, localBefore, after)
	}
	if b := mustGit(t, dir, "branch", "--list", "phase/auth"); b == "" {
		t.Error("phase/auth should NOT be deleted on a non-destructive refusal")
	}
}

// c-5 (replaces TestPhaseCompleteMilestoneDivergedAborts): a diverged
// milestone base + --recover resets/restores against origin/milestone/<v>
// instead of aborting with "does not yet support milestone branches" — and
// the restore is pushed (c-2 tail), so the milestone base ends in sync.
func TestPhaseCompleteMilestoneDivergedRecovers(t *testing.T) {
	dir, _, version := completeMilestoneFixture(t)
	stubPRMerged(t, true)
	divergeMilestone(t, dir, version)

	if err := runCmd(t, Phase(), "complete", "--recover"); err != nil {
		t.Fatalf("--recover should heal a diverged milestone base: %v", err)
	}
	// The local milestone branch ends level with origin — reset to the
	// origin squash, restore pushed back up.
	if ahead := mustGit(t, dir, "rev-list", "origin/milestone/"+version+"..milestone/"+version); ahead != "" {
		t.Errorf("milestone/%s should be fully pushed after recovery, got ahead: %q", version, ahead)
	}
	if cur := mustGit(t, dir, "symbolic-ref", "--short", "HEAD"); cur != "milestone/"+version {
		t.Errorf("expected HEAD on milestone/%s after recovery, got %q", version, cur)
	}
	// The divergent local-only commit was reset away (destructive, as warned).
	if tree := mustGit(t, dir, "ls-tree", "-r", "--name-only", "HEAD"); strings.Contains(tree, "local-only.txt") {
		t.Error("the divergent local-only commit should have been reset away")
	}
	if b := mustGit(t, dir, "branch", "--list", "phase/auth"); b != "" {
		t.Errorf("phase/auth should be deleted after a successful recovery, got: %q", b)
	}
}

// c-1: phase create's clean-tree gate auto-commits .dross-only dirt (a pause
// state-touch shouldn't block starting the next phase) instead of refusing.
func TestPhaseCreateAutoCommitsDrossDirt(t *testing.T) {
	dir := initWithGit(t)
	mustWrite(t, filepath.Join(dir, ".dross", "handoff.md"), "# handoff\n")

	if err := runCmd(t, Phase(), "create", "auth"); err != nil {
		t.Fatalf("create should proceed past .dross-only dirt: %v", err)
	}
	if cur := mustGit(t, dir, "symbolic-ref", "--short", "HEAD"); cur != "phase/auth" {
		t.Errorf("expected HEAD on phase/auth, got %q", cur)
	}
	log := mustGit(t, dir, "log", "--format=%s")
	if !strings.Contains(log, "chore(dross): auto-commit bookkeeping") {
		t.Errorf("expected an auto-commit chore in the log:\n%s", log)
	}
}

// c-1: phase complete's clean-tree gate auto-commits .dross-only dirt and
// proceeds (here to the fetch step, which fails on the empty origin — the
// point is the error is no longer the dirty-tree refusal).
func TestPhaseCompleteAutoCommitsDrossDirt(t *testing.T) {
	dir := initWithGit(t)
	mustWrite(t, filepath.Join(dir, ".dross", "handoff.md"), "# handoff\n")

	// --base: phase "x" has no record to read one from, and the base refusal
	// fires before the dirty gate this test is about.
	err := runCmd(t, Phase(), "complete", "x", "--base", "main")
	if err == nil {
		t.Fatal("expected complete to fail later (empty origin), just not on the dirty gate")
	}
	if strings.Contains(err.Error(), "working tree is dirty") {
		t.Errorf(".dross-only dirt must not trip the dirty gate: %v", err)
	}
	log := mustGit(t, dir, "log", "--format=%s")
	if !strings.Contains(log, "chore(dross): auto-commit bookkeeping") {
		t.Errorf("expected an auto-commit chore in the log:\n%s", log)
	}
	if st := mustGit(t, dir, "status", "--porcelain"); st != "" {
		t.Errorf("tree should be clean after the auto-commit gate, got: %q", st)
	}
}

// The refusal path is unchanged for real code dirt at the complete gate — and
// the .dross half must NOT be partially committed alongside the refusal.
func TestPhaseCompleteMixedDirtStillRefuses(t *testing.T) {
	dir := initWithGit(t)
	mustWrite(t, filepath.Join(dir, ".dross", "handoff.md"), "# handoff\n")
	mustWrite(t, filepath.Join(dir, "src.go"), "package src\n")
	before := mustGit(t, dir, "rev-list", "--count", "HEAD")

	err := runCmd(t, Phase(), "complete", "x", "--base", "main")
	if err == nil || !strings.Contains(err.Error(), "working tree is dirty") {
		t.Fatalf("mixed dirt must still hit the dirty-tree refusal: %v", err)
	}
	if after := mustGit(t, dir, "rev-list", "--count", "HEAD"); after != before {
		t.Errorf("refusal must create zero commits: %s -> %s", before, after)
	}
}

// completeFixtureOriginPR is completeFixture's stale-tree variant (c-3): the
// PR record is NOT committed on phase/auth — it exists only inside the squash
// pushed to origin/main (originPR > 0), exactly the 2026-07-23 state where the
// local checkout predates ship's record commit. originPR == 0 leaves no record
// anywhere. origin/phase/auth is never pushed, so the ancestry fallback can
// never confirm the merge — only the provider path (via an origin-side PR
// read) can.
func completeFixtureOriginPR(t *testing.T, originPR int) (string, string) {
	t.Helper()
	dir := t.TempDir()
	remoteDir := t.TempDir()
	mustGit(t, remoteDir, "init", "-q", "--bare", "-b", "main")
	gitInit(t, dir, remoteDir)
	chdir(t, dir)

	if err := runCmd(t, Init()); err != nil {
		t.Fatalf("init: %v", err)
	}
	mustWrite(t, filepath.Join(dir, "README.md"), "base\n")
	mustGit(t, dir, "add", ".")
	mustGit(t, dir, "commit", "-q", "-m", "chore: baseline")
	mustGit(t, dir, "push", "-q", "-u", "origin", "main")

	if err := runCmd(t, Phase(), "create", "auth"); err != nil {
		t.Fatalf("phase create: %v", err)
	}
	mustWrite(t, filepath.Join(dir, "src/auth.ts"), "x\n")
	mustGit(t, dir, "add", ".")
	mustGit(t, dir, "commit", "-q", "-m", "feat(auth): scaffold")

	// Simulate the upstream squash-merge, carrying the completion record —
	// and, when originPR > 0, the phase's changes.json with the PR number
	// (as ship's post-push record commit does before the squash collapses it).
	mustGit(t, dir, "checkout", "-q", "-b", "squash-sim", "origin/main")
	mustGit(t, dir, "checkout", "phase/auth", "--", "src/")
	mustGit(t, dir, "add", "src/")
	if originPR > 0 {
		mustWrite(t, filepath.Join(dir, ".dross/phases/auth/changes.json"),
			fmt.Sprintf(`{"phase":"auth","pr":%d,"base":"main","tasks":{}}`, originPR))
		mustGit(t, dir, "add", ".dross/phases/auth/changes.json")
	}
	stPath := filepath.Join(dir, ".dross", "state.json")
	sqState, err := state.Load(stPath)
	if err != nil {
		t.Fatalf("load state for squash sim: %v", err)
	}
	sqState.CurrentPhase = ""
	sqState.CurrentPhaseStatus = ""
	sqState.Touch("completed auth")
	if err := sqState.Save(stPath); err != nil {
		t.Fatalf("save squash state: %v", err)
	}
	mustGit(t, dir, "add", filepath.Join(".dross", "state.json"))
	mustGit(t, dir, "commit", "-q", "-m", "feat(squash): auth")
	mustGit(t, dir, "push", "-q", "--force", "origin", "squash-sim:main")
	mustGit(t, dir, "checkout", "-q", "phase/auth")
	mustGit(t, dir, "branch", "-D", "squash-sim")
	// Deliberately NO local record of the PR and NO fetch — complete itself
	// fetches; the working tree is the stale pre-record checkout.

	return dir, "auth"
}

// c-3: with the working-tree changes.json absent but origin/main carrying
// PR #7, mergeGate must resolve 7 from origin and take the provider path —
// the ancestry fallback (origin/phase/auth missing) would refuse.
func TestPhaseCompleteResolvesRecordedPRFromOrigin(t *testing.T) {
	dir, _ := completeFixtureOriginPR(t, 7)
	gotPR := 0
	prev := ship.PRMergedFunc
	ship.PRMergedFunc = func(o ship.OpenOpts) (bool, error) {
		gotPR = o.PRNumber
		return true, nil
	}
	t.Cleanup(func() { ship.PRMergedFunc = prev })

	if err := runCmd(t, Phase(), "complete"); err != nil {
		t.Fatalf("complete should succeed via the origin-resolved PR: %v", err)
	}
	if gotPR != 7 {
		t.Errorf("mergeGate should query the provider with the origin-recorded PR #7, got %d", gotPR)
	}
	if cur := mustGit(t, dir, "symbolic-ref", "--short", "HEAD"); cur != "main" {
		t.Errorf("expected HEAD on main after complete, got %q", cur)
	}
}

// c-3 fallback: no PR in the working tree nor on origin → recordedPR stays 0,
// the provider is never queried, and the ancestry fallback refuses unchanged.
func TestPhaseCompleteNoPRAnywhereTakesAncestryFallback(t *testing.T) {
	_, _ = completeFixtureOriginPR(t, 0)
	prev := ship.PRMergedFunc
	ship.PRMergedFunc = func(ship.OpenOpts) (bool, error) {
		t.Error("provider must not be queried when no PR is recorded anywhere")
		return false, nil
	}
	t.Cleanup(func() { ship.PRMergedFunc = prev })

	err := runCmd(t, Phase(), "complete")
	if err == nil {
		t.Fatal("expected the ancestry fallback to refuse (origin/phase/auth missing)")
	}
	if !strings.Contains(err.Error(), "cannot confirm") {
		t.Errorf("expected the guided ancestry refusal, got: %v", err)
	}
}

// --- phase complete auto-heal of unfinalized verdicts (verify-auto-finalize c-3) ---

// completeVerifyToml drops a resolved-but-unfinalized verify.toml on the
// phase branch, committed so the tree stays clean for complete's gates.
func completeVerifyToml(t *testing.T, dir, phaseID, verdict string) {
	t.Helper()
	mustWrite(t, filepath.Join(dir, ".dross/phases/"+phaseID+"/verify.toml"), `[verify]
phase = "`+phaseID+`"
generated_at = 2026-07-25T10:00:00Z
verdict = "`+verdict+`"

[summary]
criteria_total = 1
criteria_covered = 1
`)
	mustGit(t, dir, "add", ".dross/phases/"+phaseID+"/verify.toml")
	mustGit(t, dir, "commit", "-q", "-m", "chore(dross): record verify for "+phaseID)
}

// TestPhaseCompleteHealsUnfinalizedVerdict: completing a phase whose
// verify.toml verdict is resolved but never finalized records the
// outcome event before proceeding, and completion still succeeds.
func TestPhaseCompleteHealsUnfinalizedVerdict(t *testing.T) {
	dir, phaseID := completeFixture(t)
	completeVerifyToml(t, dir, phaseID, "pass")
	stubPRMerged(t, true)

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("DROSS_NO_TELEMETRY", "") // re-enable (chdir pins it to "1")

	if err := runCmd(t, Phase(), "complete", phaseID); err != nil {
		t.Fatalf("complete: %v", err)
	}

	telemBody := mustRead(t, filepath.Join(home, ".claude/dross", "telemetry.jsonl"))
	if !strings.Contains(telemBody, `"verdict":"pass"`) {
		t.Errorf("complete should record the resolved verdict outcome event:\n%s", telemBody)
	}
}

// TestPhaseCompleteHealIdempotent: completing an already-finalized
// phase must not emit a duplicate outcome event.
func TestPhaseCompleteHealIdempotent(t *testing.T) {
	dir, phaseID := completeFixture(t)
	completeVerifyToml(t, dir, phaseID, "pass")
	stubPRMerged(t, true)

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("DROSS_NO_TELEMETRY", "")

	if err := runCmd(t, Verify(), "finalize", phaseID); err != nil {
		t.Fatalf("finalize: %v", err)
	}
	mustGit(t, dir, "add", ".dross/phases/"+phaseID+"/verify.toml")
	mustGit(t, dir, "commit", "-q", "-m", "chore(dross): finalized marker")

	if err := runCmd(t, Phase(), "complete", phaseID); err != nil {
		t.Fatalf("complete: %v", err)
	}

	telemBody := mustRead(t, filepath.Join(home, ".claude/dross", "telemetry.jsonl"))
	if got := strings.Count(telemBody, `"verdict":"pass"`); got != 1 {
		t.Errorf("complete on finalized phase must not duplicate the outcome event, got %d:\n%s", got, telemBody)
	}
}
