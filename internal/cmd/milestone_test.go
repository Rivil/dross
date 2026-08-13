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

	"github.com/Rivil/dross/internal/milestone"
	"github.com/Rivil/dross/internal/state"
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

// The recorded cut branch is readable through the CLI — assets/prompts/
// milestone.md forbids reading the toml directly, so `get` is the only
// sanctioned route to the one fact this milestone exists to store. It is not
// writable: `milestone create` is the sole writer of the base.
func TestMilestoneGetBaseReadableNotSettable(t *testing.T) {
	chdir(t, t.TempDir())
	if err := runCmd(t, Init()); err != nil {
		t.Fatal(err)
	}
	if err := runCmd(t, Milestone(), "create", "v1.2"); err != nil {
		t.Fatal(err)
	}
	path := milestone.FilePath(".dross", "v1.2")
	m, err := milestone.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	m.Milestone.Base = "milestone/v1.1"
	if err := m.Save(path); err != nil {
		t.Fatal(err)
	}

	for _, field := range []string{"milestone.base", "base"} {
		out := captureStdout(t, func() {
			if err := runCmd(t, Milestone(), "get", "v1.2", field); err != nil {
				t.Errorf("get %s: %v", field, err)
			}
		})
		if !strings.Contains(out, "milestone/v1.1") {
			t.Errorf("get %s returned %q, want the recorded base", field, out)
		}
	}

	// Settable would reintroduce the hand-edited base this milestone kills.
	for _, field := range []string{"milestone.base", "base"} {
		err := runCmd(t, Milestone(), "set", "v1.2", field, "milestone/v0.9")
		if err == nil {
			t.Fatalf("set %s succeeded; base must not be settable", field)
		}
		if !strings.Contains(err.Error(), "unsettable") {
			t.Errorf("set %s error = %v, want an unsettable-field refusal", field, err)
		}
	}
	// The refused writes left the recorded base untouched.
	after, err := milestone.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if after.Milestone.Base != "milestone/v1.1" {
		t.Errorf("base is now %q — a refused set still wrote", after.Milestone.Base)
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
// is unparseable so the fallback state load fails. Kills the
// CONDITIONALS_NEGATION mutant — negating the guard would panic/skip instead
// of returning this error.
//
// state.json is corrupted rather than removed: removing it makes the root
// incomplete, which FindRoot reports as not-a-dross-repo before the load is
// ever attempted. A corrupt-but-present file is the case that reaches here,
// and is loud by design (locked decision: completeness_check).
func TestMilestoneCover_ShowStateLoadError(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	if err := runCmd(t, Init()); err != nil {
		t.Fatal(err)
	}
	if err := runCmd(t, Milestone(), "create", "v0.1"); err != nil {
		t.Fatal(err)
	}
	// Corrupt state.json so state.Load errors while root still resolves.
	if err := os.WriteFile(filepath.Join(dir, ".dross", "state.json"), []byte("{{{"), 0o644); err != nil {
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
	// The API lives on a different host from [remote].url here, which is the
	// case the machine-local escape hatch exists for: authorize it by hand on
	// this machine, never through committed config.
	if err := runCmd(t, Local(), "set", "allow_hosts", server.Listener.Addr().String()); err != nil {
		t.Fatal(err)
	}
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

// TestMilestoneFinalizeFromMilestoneBranch runs finalize from the branch it is
// finalizing — the natural place to be, and the only shape that reaches
// milestoneFinalize's `if cur != mainBranch` switch. Both existing finalize
// tests start on main, so that branch-switch call site never runs under them.
func TestMilestoneFinalizeFromMilestoneBranch(t *testing.T) {
	dir, version := milestoneFinalizeFixture(t, true)
	seeded := seedHistory(t, dir, 12)
	mustGit(t, dir, "checkout", "-q", "milestone/"+version)

	if err := runCmd(t, Milestone(), "complete", version, "--finalize"); err != nil {
		t.Fatalf("finalize from the milestone branch: %v", err)
	}

	// The switch happened, and it landed on main rather than leaving HEAD on a
	// branch the same command then deletes.
	if head := mustGit(t, dir, "symbolic-ref", "--short", "HEAD"); head != "main" {
		t.Errorf("finalize should leave HEAD on main, got %q", head)
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
	// The switch off the milestone branch is a checkout like any other, so the
	// live history has to come through it intact.
	assertHistorySurvives(t, dir, seeded)
}

// TestMilestoneFinalizeSurfacesCheckoutRefusal is the guard half of the same
// call site: finalize's switch to main must go through checkoutBranch, so a
// main that still carries a pre-untrack tracked state.json is refused by name
// instead of silently replaying its copy over the live one. Reverting that line
// to a raw `git checkout` passes the test above and fails this one.
func TestMilestoneFinalizeSurfacesCheckoutRefusal(t *testing.T) {
	dir, version := milestoneFinalizeFixture(t, true)

	// Give main the pre-untrack shape. Committing it here also removes the file
	// from the working tree on the way back to the milestone branch, which is
	// why the live copy is written after the switch, not before.
	mustGit(t, dir, "add", "-f", ".dross/"+state.File)
	mustGit(t, dir, "commit", "-q", "-m", "chore(dross): pre-untrack state copy")
	mustGit(t, dir, "checkout", "-q", "milestone/"+version)
	stPath := filepath.Join(dir, ".dross", state.File)
	live := state.New()
	for i := 0; i < 12; i++ {
		live.Touch("live entry")
	}
	if err := live.Save(stPath); err != nil {
		t.Fatal(err)
	}

	err := runCmd(t, Milestone(), "complete", version, "--finalize")
	if err == nil {
		t.Fatal("finalize must not exit zero when the switch to main was refused")
	}
	if !strings.Contains(err.Error(), ".dross/"+state.File) {
		t.Errorf("the failure should name the blocking file: %v", err)
	}
	// A refused switch deletes nothing, on either side.
	if b := mustGit(t, dir, "branch", "--list", "milestone/"+version); b == "" {
		t.Error("a refused switch must not delete the local milestone branch")
	}
	if r := mustGit(t, dir, "ls-remote", "--heads", "origin", "milestone/"+version); r == "" {
		t.Error("a refused switch must not delete the remote milestone branch")
	}
	if s, lerr := state.Load(stPath); lerr != nil || len(s.History) != 12 {
		t.Errorf("the live state must survive the refusal: %v / %+v", lerr, s)
	}
}

// scaffoldMilestone inits a repo with one milestone, current by state.
func scaffoldMilestone(t *testing.T, version string) string {
	t.Helper()
	if err := runCmd(t, Init()); err != nil {
		t.Fatal(err)
	}
	if err := runCmd(t, Milestone(), "create", version); err != nil {
		t.Fatal(err)
	}
	if err := runCmd(t, State(), "set", "current_milestone", version); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(".dross", "milestones", version+".toml")
}

func TestMilestoneSetBareStatus(t *testing.T) {
	chdir(t, t.TempDir())
	path := scaffoldMilestone(t, "v0.1")

	// Bare `status` expands to milestone.status. Without expansion this
	// fails with "unknown or unsettable milestone field: status".
	if err := runCmd(t, Milestone(), "set", "status", "active"); err != nil {
		t.Fatalf("milestone set status active: %v", err)
	}
	if body := mustRead(t, path); !strings.Contains(body, `status = "active"`) {
		t.Errorf("milestone.status not written:\n%s", body)
	}

	// The resolver is additive — the fully dotted path still works.
	if err := runCmd(t, Milestone(), "set", "milestone.status", "complete"); err != nil {
		t.Fatalf("milestone set milestone.status complete: %v", err)
	}
	if body := mustRead(t, path); !strings.Contains(body, `status = "complete"`) {
		t.Errorf("dotted set did not write:\n%s", body)
	}

	// Case/whitespace variation normalises, matching configenum.Normalize.
	if err := runCmd(t, Milestone(), "set", "status", " Planning "); err != nil {
		t.Fatalf("milestone set status ' Planning ': %v", err)
	}
	if body := mustRead(t, path); !strings.Contains(body, `status = "planning"`) {
		t.Errorf("status not normalised on write:\n%s", body)
	}
}

func TestMilestoneSetStatusRejectsOutOfSet(t *testing.T) {
	chdir(t, t.TempDir())
	path := scaffoldMilestone(t, "v0.1")

	for _, bad := range []string{"shipped", "archived", ""} {
		before := mustRead(t, path)
		err := runCmd(t, Milestone(), "set", "status", bad)
		if err == nil {
			t.Fatalf("milestone set status %q succeeded; want rejection", bad)
		}
		if !strings.Contains(err.Error(), "planning | active | complete") {
			t.Errorf("error for %q = %q, want it to name the accepted set", bad, err)
		}
		// Rejection runs before Save.
		if after := mustRead(t, path); after != before {
			t.Errorf("milestone toml mutated by a rejected status %q:\n%s", bad, after)
		}
	}
}

func TestMilestoneSetUnknownBareFieldStillErrors(t *testing.T) {
	chdir(t, t.TempDir())
	scaffoldMilestone(t, "v0.1")

	err := runCmd(t, Milestone(), "set", "foo", "x")
	if err == nil {
		t.Fatal("milestone set foo x succeeded; want unknown-field error")
	}
	if !strings.Contains(err.Error(), "foo") {
		t.Errorf("error = %q, want it to name foo", err)
	}
}

func TestResolveBareMilestoneField(t *testing.T) {
	cases := []struct {
		name       string
		candidates []string
		want       string
		wantErr    string
	}{
		{"status", milestoneSettablePaths, "milestone.status", ""},
		{"title", milestoneSettablePaths, "milestone.title", ""},
		{"milestone.status", milestoneSettablePaths, "milestone.status", ""},
		{"foo", milestoneSettablePaths, "foo", ""},
		// Ambiguity is rejected with the candidates listed, never resolved
		// by taking the first match.
		{"x", []string{"a.x", "b.x"}, "", "ambiguous"},
	}
	for _, c := range cases {
		got, err := resolveBareMilestoneField(c.name, c.candidates)
		switch {
		case c.wantErr != "":
			if err == nil || !strings.Contains(err.Error(), c.wantErr) {
				t.Errorf("resolve(%q) err = %v, want %q", c.name, err, c.wantErr)
				continue
			}
			for _, cand := range c.candidates {
				if !strings.Contains(err.Error(), cand) {
					t.Errorf("ambiguity error %q does not list candidate %s", err, cand)
				}
			}
		case err != nil:
			t.Errorf("resolve(%q) = %v", c.name, err)
		case got != c.want:
			t.Errorf("resolve(%q) = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestMilestoneGetVersionShapeVsPath(t *testing.T) {
	chdir(t, t.TempDir())
	scaffoldMilestone(t, "v1.1")
	if err := runCmd(t, Milestone(), "set", "milestone.title", "friction pass"); err != nil {
		t.Fatal(err)
	}
	if err := runCmd(t, Milestone(), "set", "status", "active"); err != nil {
		t.Fatal(err)
	}

	// Leading version consumed by shape.
	out := captureStdout(t, func() {
		if err := runCmd(t, Milestone(), "get", "v1.1", "milestone.title", "milestone.status"); err != nil {
			t.Errorf("get with version: %v", err)
		}
	})
	var got map[string]string
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("not one JSON object: %v\ngot %q", err, out)
	}
	if got["milestone.title"] != "friction pass" || got["milestone.status"] != "active" {
		t.Errorf("get v1.1 ... = %v", got)
	}

	// No version: falls back to state.current_milestone. Treating args[0]
	// as a version unconditionally fails this case.
	out = captureStdout(t, func() {
		if err := runCmd(t, Milestone(), "get", "milestone.title", "milestone.status"); err != nil {
			t.Errorf("get without version: %v", err)
		}
	})
	got = nil
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("not one JSON object: %v\ngot %q", err, out)
	}
	if got["milestone.title"] != "friction pass" {
		t.Errorf("current_milestone fallback = %v", got)
	}
}

// TestMilestoneGetTypoInFirstPositionNamesThePath is the row a by-elimination
// version check fails: `milstone.title` is not a known field, so elimination
// would swallow it as a version and report "no such milestone".
func TestMilestoneGetTypoInFirstPositionNamesThePath(t *testing.T) {
	chdir(t, t.TempDir())
	scaffoldMilestone(t, "v1.1")

	err := runCmd(t, Milestone(), "get", "milstone.title", "milestone.status")
	if err == nil {
		t.Fatal("a typo'd path succeeded")
	}
	if !strings.Contains(err.Error(), "milstone.title") {
		t.Errorf("error = %q, want it to name milstone.title", err)
	}
	if strings.Contains(err.Error(), "no such milestone") || strings.Contains(err.Error(), "milestones/milstone.title") {
		t.Errorf("the typo'd path was swallowed as a version: %q", err)
	}
}

func TestMilestoneGetListSingleAndMulti(t *testing.T) {
	chdir(t, t.TempDir())
	scaffoldMilestone(t, "v1.1")
	for _, c := range []string{"first", "second"} {
		if err := runCmd(t, Milestone(), "add", "scope.success_criteria", c); err != nil {
			t.Fatal(err)
		}
	}

	// Single path: one entry per line, exactly as today.
	out := captureStdout(t, func() {
		if err := runCmd(t, Milestone(), "get", "scope.success_criteria"); err != nil {
			t.Errorf("get list: %v", err)
		}
	})
	if out != "first\nsecond\n" {
		t.Errorf("single list get = %q, want %q", out, "first\nsecond\n")
	}

	// Multi path: a JSON array, not CSV or newline-joined.
	out = captureStdout(t, func() {
		if err := runCmd(t, Milestone(), "get", "scope.success_criteria", "milestone.version"); err != nil {
			t.Errorf("multi get: %v", err)
		}
	})
	var got map[string]json.RawMessage
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("not one JSON object: %v\ngot %q", err, out)
	}
	if string(got["scope.success_criteria"]) != `["first","second"]` {
		t.Errorf("list in multi mode = %s, want a JSON array", got["scope.success_criteria"])
	}
}

func TestMilestoneGetUnknownPathAmongSeveral(t *testing.T) {
	chdir(t, t.TempDir())
	scaffoldMilestone(t, "v1.1")

	var err error
	out := captureStdout(t, func() {
		err = runCmd(t, Milestone(), "get", "milestone.title", "milestone.nope")
	})
	if err == nil {
		t.Fatal("want an error for an unknown path")
	}
	if !strings.Contains(err.Error(), "milestone.nope") {
		t.Errorf("error = %q, want it to name milestone.nope", err)
	}
	if strings.TrimSpace(out) != "" {
		t.Errorf("a partial object was emitted: %q", out)
	}
}

// TestPruneRefusalNamesGuardedCheckout (c-1): the refusal is the last narration
// in the tree that handed the user a raw `git checkout`. The branch it tells
// them to leave is stale by definition — precisely the vintage that may still
// track .dross/state.json — so the suggested switch off it is the one most
// likely to replay that copy over the live machine-local one.
func TestPruneRefusalNamesGuardedCheckout(t *testing.T) {
	dir := pruneFixture(t)
	// Seeded before the branch is cut, so milestone/v1.0 carries the record
	// too — prune is run from that branch here.
	seedCompleteMilestone(t, dir, "v1.0")
	mustGit(t, dir, "checkout", "-q", "-b", "milestone/v1.0", "main")
	commitOn(t, dir, "milestone/v1.0", "a.txt", "a\n", "feat: a")
	squashOnto(t, dir, "milestone/v1.0", "feat(squash): v1.0")
	pushMain(t, dir)
	mustGit(t, dir, "checkout", "-q", "milestone/v1.0")

	// Precondition: HEAD really is on the stale branch, so the refusal under
	// test is the current-HEAD one and not some earlier gate.
	if cur := mustGit(t, dir, "symbolic-ref", "--short", "HEAD"); cur != "milestone/v1.0" {
		t.Fatalf("precondition: HEAD = %q, want milestone/v1.0", cur)
	}

	err := runCmd(t, Milestone(), "prune")
	if err == nil {
		t.Fatal("prune must refuse while HEAD is on a branch it would delete")
	}
	msg := err.Error()
	if !strings.Contains(msg, "dross checkout main") {
		t.Errorf("the refusal should hand over the guarded verb, got: %v", err)
	}
	if strings.Contains(msg, "git checkout") {
		t.Errorf("the refusal must not suggest a raw checkout, got: %v", err)
	}
	// The verb swap must not cost the diagnostic detail: which branch is
	// stale, and where to go instead.
	if !strings.Contains(msg, "milestone/v1.0") {
		t.Errorf("the refusal should name the stale branch: %v", err)
	}
	if !strings.Contains(msg, "main") {
		t.Errorf("the refusal should name the target branch: %v", err)
	}
}

func TestLooksLikeMilestoneVersion(t *testing.T) {
	for _, s := range []string{"v1.1", "v0.10", "V2.0"} {
		if !looksLikeMilestoneVersion(s) {
			t.Errorf("looksLikeMilestoneVersion(%q) = false, want true", s)
		}
	}
	for _, s := range []string{"milestone.title", "milstone.title", "version", "scope.non_goals", "v", ""} {
		if looksLikeMilestoneVersion(s) {
			t.Errorf("looksLikeMilestoneVersion(%q) = true, want false", s)
		}
	}
}
