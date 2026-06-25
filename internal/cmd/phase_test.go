package cmd

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/state"
)

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
	if cur != "phase/01-meal-tagging" {
		t.Errorf("expected HEAD on phase/01-meal-tagging, got %q", cur)
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

func TestPhaseCreateRefusesWrongBranch(t *testing.T) {
	dir := initWithGit(t)
	mustGit(t, dir, "checkout", "-q", "-b", "feature")

	err := runCmd(t, Phase(), "create", "auth")
	if err == nil {
		t.Fatal("expected error when not on main")
	}
	if !strings.Contains(err.Error(), "must be on main") {
		t.Errorf("error should mention main: %v", err)
	}
}

func TestPhaseCreateRefusesExistingBranch(t *testing.T) {
	dir := initWithGit(t)

	// Pre-create the branch the next phase would want.
	mustGit(t, dir, "branch", "phase/01-auth")

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

	// Pre-create the would-be branch to force the git checkout step to
	// fail. Then verify the phase dir doesn't leak.
	//
	// Note: preflight catches the duplicate BEFORE the dir is created,
	// so the dir-rollback path only triggers on a different class of
	// git failure (e.g., dirty tree appearing mid-flight, signing
	// configured but no key). Asserting "preflight prevents dir
	// creation" is the practical guarantee we care about.
	mustGit(t, dir, "branch", "phase/01-auth")

	if err := runCmd(t, Phase(), "create", "auth"); err == nil {
		t.Fatal("expected error from existing branch")
	}

	// Phase dir must NOT have been created — preflight runs first.
	if _, err := os.Stat(filepath.Join(dir, ".dross", "phases", "01-auth")); err == nil {
		t.Error("phase dir should not exist when preflight fails")
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
	// Make a phase commit so HEAD on phase/01-auth is real.
	mustWrite(t, filepath.Join(dir, "src/auth.ts"), "x\n")
	mustGit(t, dir, "add", ".")
	mustGit(t, dir, "commit", "-q", "-m", "feat(auth): scaffold")

	// Simulate the upstream squash-merge: build a synthetic squash on
	// top of origin/main and push it. The squash must carry the completion
	// record `dross ship` folds in (current_phase cleared + `completed
	// 01-auth` history) — phase complete reads that off origin/main as its
	// merge guard, so without it complete would refuse.
	mustGit(t, dir, "checkout", "-q", "-b", "squash-sim", "origin/main")
	mustGit(t, dir, "checkout", "phase/01-auth", "--", "src/")
	mustGit(t, dir, "add", "src/")
	stPath := filepath.Join(dir, ".dross", "state.json")
	sqState, err := state.Load(stPath)
	if err != nil {
		t.Fatalf("load state for squash sim: %v", err)
	}
	sqState.CurrentPhase = ""
	sqState.CurrentPhaseStatus = ""
	sqState.Touch("completed 01-auth")
	if err := sqState.Save(stPath); err != nil {
		t.Fatalf("save squash state: %v", err)
	}
	mustGit(t, dir, "add", filepath.Join(".dross", "state.json"))
	mustGit(t, dir, "commit", "-q", "-m", "feat(squash): auth")
	mustGit(t, dir, "push", "-q", "--force", "origin", "squash-sim:main")
	mustGit(t, dir, "checkout", "-q", "phase/01-auth")
	mustGit(t, dir, "branch", "-D", "squash-sim")
	mustGit(t, dir, "fetch", "-q", "origin")

	return dir, "01-auth"
}

func TestPhaseCompleteHappyPath(t *testing.T) {
	dir, _ := completeFixture(t)

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
		if strings.Contains(a.Action, "completed 01-auth") {
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

	err := runCmd(t, Phase(), "complete")
	if err == nil {
		t.Fatal("expected error when upstream merge hasn't actually happened")
	}
	if !strings.Contains(err.Error(), "has the PR merged") {
		t.Errorf("error should question whether the PR merged upstream: %v", err)
	}

	// Phase branch must still exist — we didn't lose the work.
	branches := mustGit(t, dir, "branch", "--list", "phase/01-auth")
	if !strings.Contains(branches, "phase/01-auth") {
		t.Errorf("phase/01-auth should still exist after refused complete, got: %q", branches)
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
	// a squash, so there's no `completed 01-auth` record anywhere.
	mustGit(t, dir, "checkout", "-q", "main")
	mustGit(t, dir, "branch", "-D", "phase/01-auth")
	originMain := mustGit(t, dir, "rev-parse", "main")

	// Name the phase explicitly: neither current_phase nor a phase branch
	// can supply it now.
	err := runCmd(t, Phase(), "complete", "01-auth")
	if err == nil {
		t.Fatal("expected refusal when local branch is gone AND origin lacks the completion record")
	}
	if !strings.Contains(err.Error(), "has the PR merged") {
		t.Errorf("error should question whether the PR merged upstream: %v", err)
	}

	// Nothing destructive: main is unchanged and the tree is clean.
	if now := mustGit(t, dir, "rev-parse", "main"); now != originMain {
		t.Errorf("main should be untouched by a refused complete: %q != %q", now, originMain)
	}
	if st := mustGit(t, dir, "status", "--porcelain"); st != "" {
		t.Errorf("tree should be clean after refused complete, got: %q", st)
	}
}

// TestPhaseCompleteDeletesRemoteBranch covers the provider-did-NOT-delete
// case: the phase branch is still live on origin when complete runs, and
// complete must remove it so nothing is left behind.
func TestPhaseCompleteDeletesRemoteBranch(t *testing.T) {
	dir, _ := completeFixture(t)

	// Publish the phase branch to origin (provider --delete-branch aborted
	// or never ran).
	mustGit(t, dir, "push", "-q", "origin", "phase/01-auth")
	if out := mustGit(t, dir, "ls-remote", "--heads", "origin", "phase/01-auth"); out == "" {
		t.Fatal("precondition: origin should have phase/01-auth after push")
	}

	if err := runCmd(t, Phase(), "complete"); err != nil {
		t.Fatalf("complete: %v", err)
	}

	// If the remote-delete step is missing, the ref is still on origin here.
	if out := mustGit(t, dir, "ls-remote", "--heads", "origin", "phase/01-auth"); out != "" {
		t.Errorf("origin should no longer have phase/01-auth after complete, got: %q", out)
	}
}

// TestPhaseCompleteRemoteDeleteIdempotent covers the provider-ALREADY-deleted
// case: origin has no phase branch (completeFixture never pushes it), so the
// remote delete must be a no-op, not an error.
func TestPhaseCompleteRemoteDeleteIdempotent(t *testing.T) {
	dir, _ := completeFixture(t)

	if out := mustGit(t, dir, "ls-remote", "--heads", "origin", "phase/01-auth"); out != "" {
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
			// shipFixture (ship_test.go) lands us on phase/01-x with verify
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

			// 1) Ship — pushes phase/01-x to origin, opens the PR, and (the
			//    t-1 fix) commits its state write so the tree is clean.
			if err := runCmd(t, Ship()); err != nil {
				t.Fatalf("ship: %v", err)
			}

			// 2) Simulate the upstream squash-merge onto origin/main. The
			//    squash carries phase/01-x's src/ AND its .dross/state.json —
			//    ship (t-1) folded the cleared current_phase + `completed
			//    01-x` record into that state.json, and complete reads it off
			//    origin/main as its merge guard.
			mustGit(t, dir, "fetch", "-q", "origin")
			mustGit(t, dir, "checkout", "-q", "-b", "squash-sim", "origin/main")
			mustGit(t, dir, "checkout", "phase/01-x", "--", "src/", ".dross/state.json")
			mustGit(t, dir, "add", "src/", ".dross/state.json")
			mustGit(t, dir, "commit", "-q", "-m", "feat(squash): tagging")
			mustGit(t, dir, "push", "-q", "--force", "origin", "squash-sim:main")
			mustGit(t, dir, "checkout", "-q", "phase/01-x")
			mustGit(t, dir, "branch", "-D", "squash-sim")
			mustGit(t, dir, "fetch", "-q", "origin")

			// Optionally simulate the provider's --delete-branch having run.
			if tc.providerDeleted {
				mustGit(t, dir, "push", "-q", "origin", "--delete", "phase/01-x")
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
			if r := mustGit(t, dir, "ls-remote", "--heads", "origin", "phase/01-x"); r != "" {
				t.Errorf("origin should have no phase/01-x ref, got: %q", r)
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

	// Phase 1 — already set up by shipFixture (on phase/01-x).
	cycle("01-x", "phase/01-x")

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
		if strings.Contains(a.Action, "completed 01-x") {
			has1 = true
		}
		if strings.Contains(a.Action, "completed "+id2) {
			has2 = true
		}
	}
	if !has1 || !has2 {
		t.Errorf("main should carry both completion records; has 01-x=%v %s=%v; history=%+v",
			has1, id2, has2, s.History)
	}
}
