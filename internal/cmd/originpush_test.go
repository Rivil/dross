package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// phasePushFixture is basePushFixture plus a phase/x branch, one commit past
// main, checked out and pushed with an upstream — the shape every ship re-run
// starts from. Tests then move local or origin to build each arm.
func phasePushFixture(t *testing.T) (dir, origin string) {
	t.Helper()
	dir, origin = basePushFixture(t)
	mustGit(t, dir, "checkout", "-q", "-b", "phase/x")
	mustWrite(t, filepath.Join(dir, "x.go"), "package x\n")
	mustGit(t, dir, "add", "x.go")
	mustGit(t, dir, "commit", "-q", "-m", "feat(x): first")
	mustGit(t, dir, "push", "-q", "-u", "origin", "phase/x")
	return dir, origin
}

// commitLocal adds one commit on the checked-out branch.
func commitLocal(t *testing.T, dir, name string) {
	t.Helper()
	mustWrite(t, filepath.Join(dir, name), "x\n")
	mustGit(t, dir, "add", name)
	mustGit(t, dir, "commit", "-q", "-m", "feat: "+name)
}

// pushForeignToOrigin lands a commit on origin's phase/x that local phase/x
// lacks, from a throwaway branch cut at origin's tip (the
// TestPushBaseDivergedNoop recipe). Note the push updates the local
// remote-tracking ref too — use cloneAndPush when the fetch itself is the
// thing under test.
func pushForeignToOrigin(t *testing.T, dir string) {
	t.Helper()
	mustGit(t, dir, "checkout", "-q", "-b", "upstream-sim", "origin/phase/x")
	mustWrite(t, filepath.Join(dir, "upstream.txt"), "x\n")
	mustGit(t, dir, "add", "upstream.txt")
	mustGit(t, dir, "commit", "-q", "-m", "feat: review commit from elsewhere")
	mustGit(t, dir, "push", "-q", "origin", "upstream-sim:phase/x")
	mustGit(t, dir, "checkout", "-q", "phase/x")
	mustGit(t, dir, "branch", "-q", "-D", "upstream-sim")
}

// cloneAndPush pushes a commit to origin's phase/x from a SECOND clone, so the
// first clone's refs/remotes/origin/phase/x is stale until it fetches.
func cloneAndPush(t *testing.T, origin string) {
	t.Helper()
	other := t.TempDir()
	mustGit(t, other, "clone", "-q", "-b", "phase/x", origin, ".")
	mustGit(t, other, "config", "user.email", "other@example.com")
	mustGit(t, other, "config", "user.name", "Other")
	mustGit(t, other, "config", "commit.gpgsign", "false")
	mustWrite(t, filepath.Join(other, "elsewhere.txt"), "x\n")
	mustGit(t, other, "add", "elsewhere.txt")
	mustGit(t, other, "commit", "-q", "-m", "feat: pushed from another machine")
	mustGit(t, other, "push", "-q", "origin", "phase/x")
}

// refuseAllPushes installs a pre-receive hook on the bare origin that rejects
// every push, so any push attempt is a hard error rather than a silent success.
func refuseAllPushes(t *testing.T, origin string) {
	t.Helper()
	hook := filepath.Join(origin, "hooks", "pre-receive")
	if err := os.MkdirAll(filepath.Dir(hook), 0o755); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\necho 'refused by policy hook' >&2\nexit 1\n"
	if err := os.WriteFile(hook, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
}

// c-1: a local-only commit on phase/x with a CLEAN index is pushed. A gate
// reading `git diff --cached` (the old ship.go shape) sees nothing staged,
// pushes nothing, and fails here.
func TestPushPhaseBranchAheadCleanIndexPushes(t *testing.T) {
	dir, _ := phasePushFixture(t)
	commitLocal(t, dir, "record.txt")
	if staged := mustGit(t, dir, "diff", "--cached", "--name-only"); staged != "" {
		t.Fatalf("fixture broke: index must be clean, got staged %q", staged)
	}

	pushed, err := pushPhaseBranch(dir, "phase/x", false)
	if err != nil {
		t.Fatalf("ahead-only phase branch must push: %v", err)
	}
	if !pushed {
		t.Error("pushed=false for a branch one commit ahead of origin")
	}
	if ahead := mustGit(t, dir, "rev-list", "origin/phase/x..phase/x"); ahead != "" {
		t.Errorf("origin/phase/x..phase/x should be empty after the push, got %q", ahead)
	}
}

// Level with origin: nothing to push, and nothing is attempted — the hook
// would turn an always-push mutant into a hard error.
func TestPushPhaseBranchLevelNoPush(t *testing.T) {
	dir, origin := phasePushFixture(t)
	refuseAllPushes(t, origin)

	pushed, err := pushPhaseBranch(dir, "phase/x", false)
	if err != nil {
		t.Fatalf("level branch must be a no-op, got: %v", err)
	}
	if pushed {
		t.Error("pushed=true for a branch level with origin")
	}
}

// diverged_phase_branch: ahead AND behind refuses with the pull and --force
// paths named, and moves nothing; with force it pushes over origin's copy.
func TestPushPhaseBranchDivergedRefuses(t *testing.T) {
	dir, origin := phasePushFixture(t)
	commitLocal(t, dir, "local.txt")
	pushForeignToOrigin(t, dir)
	remoteBefore := mustGit(t, origin, "rev-parse", "phase/x")

	pushed, err := pushPhaseBranch(dir, "phase/x", false)
	if err == nil {
		t.Fatalf("diverged phase branch must refuse (pushed=%v)", pushed)
	}
	if pushed {
		t.Error("pushed=true on a refusal")
	}
	for _, want := range []string{"git pull --rebase origin phase/x", "--force"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not name %q", err, want)
		}
	}
	if after := mustGit(t, origin, "rev-parse", "phase/x"); after != remoteBefore {
		t.Errorf("refusal must push nothing: origin phase/x moved %s -> %s", remoteBefore, after)
	}

	pushed, err = pushPhaseBranch(dir, "phase/x", true)
	if err != nil {
		t.Fatalf("force on a diverged branch must push: %v", err)
	}
	if !pushed {
		t.Error("pushed=false after a forced push")
	}
	if got, want := mustGit(t, origin, "rev-parse", "phase/x"), mustGit(t, dir, "rev-parse", "HEAD"); got != want {
		t.Errorf("after --force origin phase/x = %s; want local HEAD %s", got, want)
	}
}

// First ship: origin/phase/x does not exist. The push succeeds AND sets the
// upstream — the prompt's later bare `git push` on the branch depends on it.
func TestPushPhaseBranchMissingPushesWithUpstream(t *testing.T) {
	dir, origin := basePushFixture(t)
	mustGit(t, dir, "checkout", "-q", "-b", "phase/x")
	commitLocal(t, dir, "x.go")

	pushed, err := pushPhaseBranch(dir, "phase/x", false)
	if err != nil {
		t.Fatalf("first push of phase/x must succeed: %v", err)
	}
	if !pushed {
		t.Error("pushed=false on the first push")
	}
	if got, want := mustGit(t, origin, "rev-parse", "phase/x"), mustGit(t, dir, "rev-parse", "HEAD"); got != want {
		t.Errorf("origin phase/x = %s; want local HEAD %s", got, want)
	}
	if up := mustGit(t, dir, "rev-parse", "--abbrev-ref", "phase/x@{upstream}"); up != "origin/phase/x" {
		t.Errorf("upstream = %q; want origin/phase/x (the -u is load-bearing)", up)
	}
}

// Behind only: origin carries a commit local lacks and local has nothing new.
// Refuse naming the pull; there is nothing to force, so --force is not offered.
func TestPushPhaseBranchBehindOnlyRefuses(t *testing.T) {
	dir, origin := phasePushFixture(t)
	pushForeignToOrigin(t, dir)
	remoteBefore := mustGit(t, origin, "rev-parse", "phase/x")

	pushed, err := pushPhaseBranch(dir, "phase/x", false)
	if err == nil {
		t.Fatalf("behind-only phase branch must refuse (pushed=%v)", pushed)
	}
	if pushed {
		t.Error("pushed=true on a refusal")
	}
	if !strings.Contains(err.Error(), "git pull --rebase origin phase/x") {
		t.Errorf("error %q does not name the pull", err)
	}
	if strings.Contains(err.Error(), "--force") {
		t.Errorf("error %q offers --force on a behind-only branch — nothing to force", err)
	}
	if after := mustGit(t, origin, "rev-parse", "phase/x"); after != remoteBefore {
		t.Errorf("refusal must push nothing: origin phase/x moved %s -> %s", remoteBefore, after)
	}
}

// The fetch is load-bearing: a commit pushed from a second clone is invisible
// to the local remote-tracking ref until compareWithOrigin fetches. A helper
// that skips the fetch reads in-sync and fails here.
func TestOriginCmpSeesUnfetchedRemoteCommit(t *testing.T) {
	dir, origin := phasePushFixture(t)
	cloneAndPush(t, origin)
	if stale := mustGit(t, dir, "rev-list", "phase/x..origin/phase/x"); stale != "" {
		t.Fatalf("fixture broke: local tracking ref already sees the foreign commit %q", stale)
	}

	d, err := compareWithOrigin(dir, "phase/x")
	if err != nil {
		t.Fatalf("compare: %v", err)
	}
	if !d.Behind {
		t.Error("Behind=false — the foreign commit was never fetched")
	}
	if d.Missing || len(d.Ahead) != 0 {
		t.Errorf("got %+v; want behind only", d)
	}
}

// A never-pushed branch reads Missing, not error — that is the first-ship case
// and swallowing it as a failure would block every new phase.
func TestOriginCmpMissingRemoteRef(t *testing.T) {
	dir, _ := basePushFixture(t)
	mustGit(t, dir, "checkout", "-q", "-b", "phase/x")
	commitLocal(t, dir, "x.go")

	d, err := compareWithOrigin(dir, "phase/x")
	if err != nil {
		t.Fatalf("missing origin ref must not be an error: %v", err)
	}
	if !d.Missing {
		t.Errorf("Missing=false for a branch origin has never seen: %+v", d)
	}
}

// Ahead-only reads the ahead SHAs newest first and Behind=false — the shape
// both consumers key their push on.
func TestOriginCmpAheadListsLocalOnlyCommits(t *testing.T) {
	dir, _ := phasePushFixture(t)
	commitLocal(t, dir, "one.txt")
	commitLocal(t, dir, "two.txt")

	d, err := compareWithOrigin(dir, "phase/x")
	if err != nil {
		t.Fatalf("compare: %v", err)
	}
	if d.Missing || d.Behind {
		t.Errorf("got %+v; want ahead only", d)
	}
	if len(d.Ahead) != 2 || d.Ahead[0] != mustGit(t, dir, "rev-parse", "HEAD") {
		t.Errorf("Ahead = %v; want the two local commits, HEAD first", d.Ahead)
	}
}

// shared_origin_gate: the base safety net consumes compareWithOrigin and runs
// no rev-list of its own. Source pin on the argv literal, so the doc comment
// may still describe the comparison.
func TestBaseBranchCarriesNoOwnRevList(t *testing.T) {
	src, err := os.ReadFile(filepath.Join(repoRootFromTest(t), "internal", "cmd", "basebranch.go"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(src), `"rev-list"`) {
		t.Error("basebranch.go carries its own rev-list argv — the origin comparison lives in compareWithOrigin only")
	}
	if !strings.Contains(string(src), "compareWithOrigin(") {
		t.Error("basebranch.go does not call compareWithOrigin")
	}
}
