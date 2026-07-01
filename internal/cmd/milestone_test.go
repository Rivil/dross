package cmd

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMilestoneCreateAndList(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	if err := runCmd(t, Init()); err != nil {
		t.Fatal(err)
	}

	for _, v := range []string{"v0.1", "v0.2", "v1.0"} {
		if err := runCmd(t, Milestone(), "create", v); err != nil {
			t.Fatalf("create %s: %v", v, err)
		}
		if _, err := os.Stat(filepath.Join(dir, ".dross/milestones", v+".toml")); err != nil {
			t.Errorf("toml not written for %s: %v", v, err)
		}
	}

	out := captureStdout(t, func() {
		runCmd(t, Milestone(), "list")
	})
	for _, want := range []string{"v0.1", "v0.2", "v1.0"} {
		if !strings.Contains(out, want) {
			t.Errorf("list missing %s:\n%s", want, out)
		}
	}
}

func TestMilestoneCreateRefusesIfExists(t *testing.T) {
	chdir(t, t.TempDir())
	if err := runCmd(t, Init()); err != nil {
		t.Fatal(err)
	}
	if err := runCmd(t, Milestone(), "create", "v0.1"); err != nil {
		t.Fatal(err)
	}
	err := runCmd(t, Milestone(), "create", "v0.1")
	if err == nil {
		t.Fatal("expected error on duplicate create")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("error should mention existence: %v", err)
	}
}

func TestMilestoneShowPrintsToml(t *testing.T) {
	chdir(t, t.TempDir())
	if err := runCmd(t, Init()); err != nil {
		t.Fatal(err)
	}
	if err := runCmd(t, Milestone(), "create", "v0.1"); err != nil {
		t.Fatal(err)
	}
	out := captureStdout(t, func() {
		runCmd(t, Milestone(), "show", "v0.1")
	})
	for _, want := range []string{
		"v0.1.toml",
		`version = "v0.1"`,
		`status = "planning"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("show missing %q:\n%s", want, out)
		}
	}
}

func TestMilestoneListEmpty(t *testing.T) {
	chdir(t, t.TempDir())
	if err := runCmd(t, Init()); err != nil {
		t.Fatal(err)
	}
	out := captureStdout(t, func() {
		runCmd(t, Milestone(), "list")
	})
	if !strings.Contains(out, "no milestones") {
		t.Errorf("expected 'no milestones' on empty list:\n%s", out)
	}
}

func TestMilestoneSetGet(t *testing.T) {
	chdir(t, t.TempDir())
	if err := runCmd(t, Init()); err != nil {
		t.Fatal(err)
	}
	if err := runCmd(t, Milestone(), "create", "v0.1"); err != nil {
		t.Fatal(err)
	}

	if err := runCmd(t, Milestone(), "set", "v0.1", "milestone.title", "First release"); err != nil {
		t.Fatalf("set title: %v", err)
	}
	if err := runCmd(t, Milestone(), "set", "v0.1", "milestone.status", "active"); err != nil {
		t.Fatalf("set status: %v", err)
	}

	out := captureStdout(t, func() {
		runCmd(t, Milestone(), "get", "v0.1", "milestone.title")
	})
	if !strings.Contains(out, "First release") {
		t.Errorf("get title returned %q", out)
	}

	out = captureStdout(t, func() {
		runCmd(t, Milestone(), "get", "v0.1", "milestone.status")
	})
	if !strings.Contains(out, "active") {
		t.Errorf("get status returned %q", out)
	}
}

func TestMilestoneAddListsAreIdempotent(t *testing.T) {
	chdir(t, t.TempDir())
	if err := runCmd(t, Init()); err != nil {
		t.Fatal(err)
	}
	if err := runCmd(t, Milestone(), "create", "v0.1"); err != nil {
		t.Fatal(err)
	}

	for _, c := range []string{
		"perft suite passes at depth 5",
		"mutation score >= 0.80",
		"perft suite passes at depth 5", // duplicate — should be ignored
	} {
		if err := runCmd(t, Milestone(), "add", "v0.1", "scope.success_criteria", c); err != nil {
			t.Fatalf("add criterion %q: %v", c, err)
		}
	}
	for _, ng := range []string{"no engine", "no UCI"} {
		if err := runCmd(t, Milestone(), "add", "v0.1", "scope.non_goals", ng); err != nil {
			t.Fatal(err)
		}
	}
	for _, p := range []string{"01-board-fen", "02-pseudolegal-moves"} {
		if err := runCmd(t, Milestone(), "add", "v0.1", "phases", p); err != nil {
			t.Fatal(err)
		}
	}

	out := captureStdout(t, func() {
		runCmd(t, Milestone(), "get", "v0.1", "scope.success_criteria")
	})
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 2 {
		t.Errorf("expected 2 unique success criteria (dup ignored), got %d:\n%s", len(lines), out)
	}

	out = captureStdout(t, func() {
		runCmd(t, Milestone(), "get", "v0.1", "phases")
	})
	if !strings.Contains(out, "01-board-fen") || !strings.Contains(out, "02-pseudolegal-moves") {
		t.Errorf("phases get missing entries:\n%s", out)
	}
}

func TestMilestoneAddAcceptsFieldAliases(t *testing.T) {
	chdir(t, t.TempDir())
	if err := runCmd(t, Init()); err != nil {
		t.Fatal(err)
	}
	if err := runCmd(t, Milestone(), "create", "v0.1"); err != nil {
		t.Fatal(err)
	}

	// Bare spellings and scope.phases should all resolve to the canonical
	// field rather than failing with "not a list field".
	aliases := map[string][2]string{
		"success_criteria": {"perft passes", "scope.success_criteria"},
		"non_goals":        {"no engine", "scope.non_goals"},
		"scope.phases":     {"01-board", "phases"},
	}
	for alias, want := range aliases {
		if err := runCmd(t, Milestone(), "add", "v0.1", alias, want[0]); err != nil {
			t.Fatalf("add via alias %q: %v", alias, err)
		}
		out := captureStdout(t, func() {
			runCmd(t, Milestone(), "get", "v0.1", want[1])
		})
		if !strings.Contains(out, want[0]) {
			t.Errorf("alias %q did not append to %s; get returned:\n%s", alias, want[1], out)
		}
	}
}

func TestMilestoneAddUnknownFieldListsValid(t *testing.T) {
	chdir(t, t.TempDir())
	if err := runCmd(t, Init()); err != nil {
		t.Fatal(err)
	}
	if err := runCmd(t, Milestone(), "create", "v0.1"); err != nil {
		t.Fatal(err)
	}
	err := runCmd(t, Milestone(), "add", "v0.1", "bogus", "x")
	if err == nil || !strings.Contains(err.Error(), "not a list field") ||
		!strings.Contains(err.Error(), "scope.success_criteria") {
		t.Errorf("expected actionable error listing valid fields, got: %v", err)
	}
}

func TestMilestoneSetRejectsListPaths(t *testing.T) {
	chdir(t, t.TempDir())
	if err := runCmd(t, Init()); err != nil {
		t.Fatal(err)
	}
	if err := runCmd(t, Milestone(), "create", "v0.1"); err != nil {
		t.Fatal(err)
	}
	err := runCmd(t, Milestone(), "set", "v0.1", "scope.success_criteria", "x")
	if err == nil || !strings.Contains(err.Error(), "use `dross milestone add`") {
		t.Errorf("expected helpful error pointing at add, got: %v", err)
	}
}

func TestMilestoneGetRejectsUnknownField(t *testing.T) {
	chdir(t, t.TempDir())
	if err := runCmd(t, Init()); err != nil {
		t.Fatal(err)
	}
	if err := runCmd(t, Milestone(), "create", "v0.1"); err != nil {
		t.Fatal(err)
	}
	err := runCmd(t, Milestone(), "get", "v0.1", "nonsense.field")
	if err == nil || !strings.Contains(err.Error(), "unknown") {
		t.Errorf("expected unknown-field error, got: %v", err)
	}
}

func TestMilestoneVersionDefaultsToCurrent(t *testing.T) {
	chdir(t, t.TempDir())
	if err := runCmd(t, Init()); err != nil {
		t.Fatal(err)
	}
	if err := runCmd(t, Milestone(), "create", "v0.1"); err != nil {
		t.Fatal(err)
	}
	if err := runCmd(t, State(), "set", "current_milestone", "v0.1"); err != nil {
		t.Fatal(err)
	}

	// set/add without a version target the current milestone.
	if err := runCmd(t, Milestone(), "set", "milestone.title", "Defaulted"); err != nil {
		t.Fatalf("set without version: %v", err)
	}
	if err := runCmd(t, Milestone(), "add", "scope.success_criteria", "c-1 holds"); err != nil {
		t.Fatalf("add without version: %v", err)
	}

	// get without a version reads the current milestone.
	out := captureStdout(t, func() {
		runCmd(t, Milestone(), "get", "milestone.title")
	})
	if !strings.Contains(out, "Defaulted") {
		t.Errorf("get without version returned %q", out)
	}
	out = captureStdout(t, func() {
		runCmd(t, Milestone(), "get", "scope.success_criteria")
	})
	if !strings.Contains(out, "c-1 holds") {
		t.Errorf("add/get without version round-trip failed:\n%s", out)
	}

	// The explicit-version form still works and points at the same milestone.
	out = captureStdout(t, func() {
		runCmd(t, Milestone(), "get", "v0.1", "milestone.title")
	})
	if !strings.Contains(out, "Defaulted") {
		t.Errorf("explicit-version get returned %q", out)
	}
}

func TestMilestoneNoVersionNoCurrentErrors(t *testing.T) {
	chdir(t, t.TempDir())
	if err := runCmd(t, Init()); err != nil {
		t.Fatal(err)
	}
	if err := runCmd(t, Milestone(), "create", "v0.1"); err != nil {
		t.Fatal(err)
	}
	// No current_milestone set — omitting the version must fail clearly.
	err := runCmd(t, Milestone(), "get", "milestone.title")
	if err == nil || !strings.Contains(err.Error(), "current_milestone") {
		t.Errorf("expected current_milestone error, got: %v", err)
	}
}

// TestMilestoneCover_ShowStateLoadError exercises milestoneShow line 103
// (state.Load err != nil): .dross exists so FindRoot succeeds, but state.json
// is removed so the fallback state load fails. Kills the CONDITIONALS_NEGATION
// mutant — negating the guard would panic/skip instead of returning this error.
func TestMilestoneCover_ShowStateLoadError(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	if err := runCmd(t, Init()); err != nil {
		t.Fatal(err)
	}
	if err := runCmd(t, Milestone(), "create", "v0.1"); err != nil {
		t.Fatal(err)
	}
	// Remove state.json so state.Load errors while root still resolves.
	if err := os.Remove(filepath.Join(dir, ".dross", "state.json")); err != nil {
		t.Fatal(err)
	}
	err := runCmd(t, Milestone(), "show")
	if err == nil || !strings.Contains(err.Error(), "load state") {
		t.Errorf("expected 'load state' error when state.json missing, got: %v", err)
	}
}

// TestMilestoneCover_ShowNoCurrentMilestone exercises milestoneShow line 107
// true branch (version == "" after a clean state load): state loads fine but
// current_milestone is empty, so show with no arg must return the
// current_milestone guidance error rather than proceeding.
func TestMilestoneCover_ShowNoCurrentMilestone(t *testing.T) {
	chdir(t, t.TempDir())
	if err := runCmd(t, Init()); err != nil {
		t.Fatal(err)
	}
	if err := runCmd(t, Milestone(), "create", "v0.1"); err != nil {
		t.Fatal(err)
	}
	// Init leaves current_milestone empty; omitting the version must fail here.
	err := runCmd(t, Milestone(), "show")
	if err == nil || !strings.Contains(err.Error(), "current_milestone") {
		t.Errorf("expected current_milestone error, got: %v", err)
	}
}

// TestMilestoneCover_ShowDefaultsToCurrent exercises milestoneShow line 103
// err == nil and line 107 version != "" (both false branches): state loads and
// current_milestone is set, so show with no arg prints that milestone's toml.
// Kills the negation mutants that would instead error on this happy path.
func TestMilestoneCover_ShowDefaultsToCurrent(t *testing.T) {
	chdir(t, t.TempDir())
	if err := runCmd(t, Init()); err != nil {
		t.Fatal(err)
	}
	if err := runCmd(t, Milestone(), "create", "v0.1"); err != nil {
		t.Fatal(err)
	}
	if err := runCmd(t, State(), "set", "current_milestone", "v0.1"); err != nil {
		t.Fatal(err)
	}
	var showErr error
	out := captureStdout(t, func() {
		showErr = runCmd(t, Milestone(), "show")
	})
	if showErr != nil {
		t.Fatalf("show with no version should default to current: %v", showErr)
	}
	for _, want := range []string{"v0.1.toml", `version = "v0.1"`} {
		if !strings.Contains(out, want) {
			t.Errorf("show defaulted to current missing %q:\n%s", want, out)
		}
	}
}

// setupMilestoneRepo builds a git repo with a bare origin and one commit on
// main, chdir'd and dross-initialised — the fixture the branch-topology tests
// share. Returns the working dir and the bare origin path.
func setupMilestoneRepo(t *testing.T) (dir, origin string) {
	t.Helper()
	dir = t.TempDir()
	origin = t.TempDir()
	mustGit(t, origin, "init", "--bare", "-q", "-b", "main")
	gitInit(t, dir, origin)
	chdir(t, dir)
	if err := runCmd(t, Init()); err != nil {
		t.Fatalf("init: %v", err)
	}
	mustGit(t, dir, "add", "-A")
	mustGit(t, dir, "commit", "-q", "-m", "init")
	return dir, origin
}

func TestMilestoneCreateCutsBranchFromMain(t *testing.T) {
	dir, _ := setupMilestoneRepo(t)
	if err := runCmd(t, Milestone(), "create", "v0.9"); err != nil {
		t.Fatalf("create: %v", err)
	}
	// Branch exists locally (mustGit fatals if the ref is missing).
	mustGit(t, dir, "rev-parse", "--verify", "refs/heads/milestone/v0.9")
	// HEAD stayed on main.
	if head := mustGit(t, dir, "symbolic-ref", "--short", "HEAD"); head != "main" {
		t.Errorf("HEAD moved off main to %q", head)
	}
	// Tip equals main's tip (cut from main, not some other commit).
	mainTip := mustGit(t, dir, "rev-parse", "main")
	msTip := mustGit(t, dir, "rev-parse", "milestone/v0.9")
	if mainTip != msTip {
		t.Errorf("milestone tip %s != main tip %s", msTip, mainTip)
	}
}

func TestMilestoneCreatePushesEagerly(t *testing.T) {
	dir, _ := setupMilestoneRepo(t)
	if err := runCmd(t, Milestone(), "create", "v0.9"); err != nil {
		t.Fatalf("create: %v", err)
	}
	if out := mustGit(t, dir, "ls-remote", "--heads", "origin", "milestone/v0.9"); !strings.Contains(out, "milestone/v0.9") {
		t.Errorf("milestone/v0.9 not on origin; ls-remote:\n%s", out)
	}
}

func TestMilestoneCreateRerunIdempotent(t *testing.T) {
	dir, _ := setupMilestoneRepo(t)
	if err := runCmd(t, Milestone(), "create", "v0.9"); err != nil {
		t.Fatalf("first create: %v", err)
	}
	// Simulate a re-scope: drop the toml so the command runs past its
	// existence guard and re-enters the branch/push step with the ref
	// already present locally and on origin.
	if err := os.Remove(filepath.Join(dir, ".dross/milestones/v0.9.toml")); err != nil {
		t.Fatal(err)
	}
	if err := runCmd(t, Milestone(), "create", "v0.9"); err != nil {
		t.Fatalf("re-scope should no-op the existing branch, got: %v", err)
	}
	// Ref survived the re-scope (mustGit fatals if it's gone).
	mustGit(t, dir, "rev-parse", "--verify", "refs/heads/milestone/v0.9")
}

func TestMilestoneCreateNoGitSkips(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	if err := runCmd(t, Init()); err != nil {
		t.Fatalf("init: %v", err)
	}
	// No git repo here — create must still write the toml and not error.
	if err := runCmd(t, Milestone(), "create", "v0.9"); err != nil {
		t.Fatalf("create in non-git dir should skip branch cleanly, got: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".dross/milestones/v0.9.toml")); err != nil {
		t.Errorf("toml not written in non-git dir: %v", err)
	}
}

// msPRCapture records what the mock provider saw when opening the milestone PR.
type msPRCapture struct {
	posts   int
	created int
	base    string
	head    string
}

// milestoneOpenFixture stands up a dross repo (git + bare origin) with a forgejo
// remote pointed at a mock server, so `dross milestone complete` can open a PR.
// The mock returns 201 for the first POST /pulls and 409 (duplicate) afterwards,
// so idempotency is observable. repo.squash_merge=true is set to prove the
// merge-commit instruction is emitted regardless.
func milestoneOpenFixture(t *testing.T) (string, *msPRCapture) {
	t.Helper()
	dir, _ := setupMilestoneRepo(t)
	for _, set := range [][]string{
		{"set", "remote.provider", "forgejo"},
		{"set", "remote.url", "https://forge.example/me/p"},
		{"set", "remote.auth_env", "MOCK_FORGEJO_TOKEN"},
		{"set", "repo.git_main_branch", "main"},
		{"set", "repo.squash_merge", "true"},
	} {
		if err := runCmd(t, Project(), set...); err != nil {
			t.Fatalf("project %v: %v", set, err)
		}
	}
	t.Setenv("MOCK_FORGEJO_TOKEN", "secret")

	cap := &msPRCapture{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if strings.HasSuffix(r.URL.Path, "/pulls") && r.Method == "POST" {
			cap.posts++
			if cap.posts == 1 {
				var doc map[string]any
				_ = json.Unmarshal(body, &doc)
				cap.base, _ = doc["base"].(string)
				cap.head, _ = doc["head"].(string)
				cap.created++
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"number":42,"html_url":"https://forge.example/me/p/pulls/42"}`))
				return
			}
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(`{"message":"pull request already exists for these targets"}`))
			return
		}
		t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
	}))
	t.Cleanup(server.Close)
	if err := runCmd(t, Project(), "set", "remote.api_base", server.URL); err != nil {
		t.Fatal(err)
	}
	return dir, cap
}

func TestMilestoneCompleteOpensSinglePRToMain(t *testing.T) {
	dir, cap := milestoneOpenFixture(t)
	_ = dir
	if err := runCmd(t, Milestone(), "complete", "v0.9"); err != nil {
		t.Fatalf("open: %v", err)
	}
	if cap.base != "main" || cap.head != "milestone/v0.9" {
		t.Errorf("PR base/head = %q/%q; want main / milestone/v0.9", cap.base, cap.head)
	}
	// Second run is idempotent — the provider's duplicate is tolerated and no
	// second PR is created.
	if err := runCmd(t, Milestone(), "complete", "v0.9"); err != nil {
		t.Fatalf("rerun should be idempotent, got: %v", err)
	}
	if cap.created != 1 {
		t.Errorf("expected exactly one PR created, got %d", cap.created)
	}
}

func TestMilestoneCompleteUsesMergeCommit(t *testing.T) {
	milestoneOpenFixture(t) // repo.squash_merge=true is configured
	out := captureStdout(t, func() {
		if err := runCmd(t, Milestone(), "complete", "v0.9"); err != nil {
			t.Fatalf("open: %v", err)
		}
	})
	low := strings.ToLower(out)
	if !strings.Contains(low, "merge commit") || !strings.Contains(low, "squash") {
		t.Errorf("open should instruct a non-squash merge-commit even with repo.squash_merge=true; got:\n%s", out)
	}
}

// milestoneFinalizeFixture builds a repo whose milestone/v0.9 has real work and
// is pushed to origin. When merged, it also simulates the milestone->main merge
// on origin (without advancing local main), so finalize has a ff to do; when
// not, origin/main lacks the milestone so finalize must refuse.
func milestoneFinalizeFixture(t *testing.T, merged bool) (string, string) {
	t.Helper()
	version := "v0.9"
	dir, _ := setupMilestoneRepo(t)
	mustGit(t, dir, "push", "-q", "-u", "origin", "main")
	mustGit(t, dir, "branch", "milestone/"+version)
	mustGit(t, dir, "checkout", "-q", "milestone/"+version)
	mustWrite(t, filepath.Join(dir, "phasework.txt"), "x\n")
	mustGit(t, dir, "add", ".")
	mustGit(t, dir, "commit", "-q", "-m", "phase work")
	mustGit(t, dir, "push", "-q", "-u", "origin", "milestone/"+version)

	if merged {
		// Simulate the milestone->main merge on origin via a throwaway branch,
		// so origin/main gains the milestone while local main stays behind.
		mustGit(t, dir, "checkout", "-q", "-b", "mergetmp", "main")
		mustGit(t, dir, "merge", "--no-ff", "-q", "-m", "merge "+version, "milestone/"+version)
		mustGit(t, dir, "push", "-q", "origin", "mergetmp:main")
		mustGit(t, dir, "checkout", "-q", "main")
		mustGit(t, dir, "branch", "-D", "mergetmp")
	} else {
		mustGit(t, dir, "checkout", "-q", "main")
	}
	mustGit(t, dir, "fetch", "-q", "origin")
	return dir, version
}

func TestMilestoneCompleteFinalizeCleansUp(t *testing.T) {
	dir, version := milestoneFinalizeFixture(t, true)
	if err := runCmd(t, Milestone(), "complete", version, "--finalize"); err != nil {
		t.Fatalf("finalize: %v", err)
	}
	if l, o := mustGit(t, dir, "rev-parse", "main"), mustGit(t, dir, "rev-parse", "origin/main"); l != o {
		t.Errorf("main not ff'd to origin: local %s != origin %s", l, o)
	}
	if b := mustGit(t, dir, "branch", "--list", "milestone/"+version); b != "" {
		t.Errorf("local milestone branch not deleted: %q", b)
	}
	if r := mustGit(t, dir, "ls-remote", "--heads", "origin", "milestone/"+version); r != "" {
		t.Errorf("remote milestone branch not deleted: %q", r)
	}
}

func TestMilestoneCleanupRefusesUnmerged(t *testing.T) {
	dir, version := milestoneFinalizeFixture(t, false)
	err := runCmd(t, Milestone(), "complete", version, "--finalize")
	if err == nil {
		t.Fatal("expected refusal when the milestone is not merged into main")
	}
	if !strings.Contains(err.Error(), "not merged") {
		t.Errorf("error should explain the unmerged state: %v", err)
	}
	// Nothing deleted — the milestone branch survives the refusal.
	if b := mustGit(t, dir, "branch", "--list", "milestone/"+version); b == "" {
		t.Error("local milestone branch should NOT be deleted on refusal")
	}
	if r := mustGit(t, dir, "ls-remote", "--heads", "origin", "milestone/"+version); r == "" {
		t.Error("remote milestone branch should NOT be deleted on refusal")
	}
}
