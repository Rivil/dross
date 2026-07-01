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
	// only — it never reaches origin/main, matching production.
	mustWrite(t, filepath.Join(dir, ".dross/phases/auth/changes.json"),
		`{"phase":"auth","pr":42,"tasks":{}}`)
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
	// can supply it now.
	stubPRMerged(t, false)
	err := runCmd(t, Phase(), "complete", "auth")
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
		`{"phase":"auth","tasks":{}}`)
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
	// PRMergedFunc=true) and the run reaches the ff-divergence logic under test.
	mustWrite(t, filepath.Join(dir, ".dross/phases/auth/changes.json"),
		`{"phase":"auth","pr":77,"tasks":{}}`)
	mustGit(t, dir, "add", ".dross/phases/auth/changes.json")
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

	// Local main diverges: same completion record + a phase artefact origin
	// lost. Built on local main (baseline), so it shares only baseline with
	// origin/main -> ff-only aborts.
	mustWrite(t, filepath.Join(dir, ".dross/phases/auth/spec.toml"), `id = "auth"`)
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
// resolution must take args[0] (non-empty), so the run advances to the
// dirty-tree guard. Negating the arm would drop through to the branch fallback
// on `main`, yield an empty id, and fail with "no phase id given" instead —
// the two errors distinguish the mutant.
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
		t.Fatal("expected dirty-tree error once the id resolves from args[0]")
	}
	if !strings.Contains(err.Error(), "working tree is dirty") {
		t.Errorf("args[0] should resolve the id and reach the dirty guard; got: %v", err)
	}
	if strings.Contains(err.Error(), "no phase id given") {
		t.Errorf("args[0] arm must supply the id, not fall through to the empty-id error: %v", err)
	}
}

// TestPhaseCover_CompleteStateResolvesPhaseID exercises the
// `s.CurrentPhase != ""` switch arm (phase.go:257). With NO args but a set
// current_phase, resolution must take state (non-empty) and reach the
// dirty-tree guard. Negating the arm to `== ""` would skip it, fall through to
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
		t.Fatal("expected dirty-tree error once the id resolves from current_phase")
	}
	if !strings.Contains(err.Error(), "working tree is dirty") {
		t.Errorf("current_phase should resolve the id and reach the dirty guard; got: %v", err)
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
	// look up (tests stub PRMergedFunc for the answer).
	mustWrite(t, filepath.Join(dir, ".dross/phases/auth/changes.json"),
		`{"phase":"auth","pr":42,"tasks":{}}`)
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

func TestPhaseCompleteMilestoneDivergedAborts(t *testing.T) {
	dir, _, version := completeMilestoneFixture(t)
	stubPRMerged(t, true) // gate passes; the ff-divergence is what's under test
	// Introduce local divergence: a commit on local milestone/<v> that origin
	// lacks (origin carries the squash the local branch doesn't) → ff aborts.
	mustGit(t, dir, "checkout", "-q", "milestone/"+version)
	mustWrite(t, filepath.Join(dir, "local-only.txt"), "x\n")
	mustGit(t, dir, "add", ".")
	mustGit(t, dir, "commit", "-q", "-m", "local divergence")
	localBefore := mustGit(t, dir, "rev-parse", "milestone/"+version)
	mustGit(t, dir, "checkout", "-q", "phase/auth")

	err := runCmd(t, Phase(), "complete")
	if err == nil {
		t.Fatal("expected non-destructive abort on a diverged milestone branch")
	}
	if !strings.Contains(err.Error(), "diverged") {
		t.Errorf("error should explain divergence: %v", err)
	}
	// Nothing reset: local milestone tip unchanged and phase branch still present.
	if after := mustGit(t, dir, "rev-parse", "milestone/"+version); after != localBefore {
		t.Errorf("milestone/%s tip changed (should be untouched): %s -> %s", version, localBefore, after)
	}
	if b := mustGit(t, dir, "branch", "--list", "phase/auth"); b == "" {
		t.Error("phase/auth should NOT be deleted on a non-destructive abort")
	}
}
