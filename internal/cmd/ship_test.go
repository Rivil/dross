package cmd

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/changes"
	"github.com/Rivil/dross/internal/project"
	"github.com/Rivil/dross/internal/ship"
	"github.com/Rivil/dross/internal/state"
)

// TestBuildOpenOptsMapsGitLabFields pins c-2's wiring: the ship command must
// copy remote.auth_scheme and remote.project_id (plus the base remote fields)
// onto ship.OpenOpts. The inline struct literal this replaced was untestable, so
// a regression where GitLab silently used default auth / a derived id even when
// the user overrode them would have gone unnoticed.
func TestBuildOpenOptsMapsGitLabFields(t *testing.T) {
	p := &project.Project{Remote: project.Remote{
		Provider:   "gitlab",
		URL:        "https://gitlab.example/me/proj",
		APIBase:    "https://gitlab.example/api/v4",
		AuthEnv:    "GL_TOKEN",
		AuthScheme: "bearer",
		ProjectID:  "42",
		Reviewers:  []string{"alice"},
	}}
	got := buildOpenOpts(p)
	if got.Provider != "gitlab" || got.URL != p.Remote.URL || got.APIBase != p.Remote.APIBase || got.AuthEnv != "GL_TOKEN" {
		t.Errorf("base remote fields not copied: %+v", got)
	}
	if got.AuthScheme != "bearer" {
		t.Errorf("auth_scheme not copied onto OpenOpts: %q", got.AuthScheme)
	}
	if got.ProjectID != "42" {
		t.Errorf("project_id not copied onto OpenOpts: %q", got.ProjectID)
	}
	if len(got.Reviewers) != 1 || got.Reviewers[0] != "alice" {
		t.Errorf("reviewers not copied: %v", got.Reviewers)
	}
}

// TestBuildCommentOptsMapsGitLabFields pins c-3's wiring: the same provider /
// auth_scheme / project_id fields must reach ship.CommentOpts.
func TestBuildCommentOptsMapsGitLabFields(t *testing.T) {
	p := &project.Project{Remote: project.Remote{
		Provider:   "gitlab",
		URL:        "https://gitlab.example/me/proj",
		APIBase:    "https://gitlab.example/api/v4",
		AuthEnv:    "GL_TOKEN",
		AuthScheme: "bearer",
		ProjectID:  "42",
	}}
	got := buildCommentOpts(p)
	if got.Provider != "gitlab" || got.AuthEnv != "GL_TOKEN" || got.AuthScheme != "bearer" || got.ProjectID != "42" {
		t.Errorf("comment opts dropped a field: %+v", got)
	}
}

// shipFixture builds a fully-initialised dross repo with a phase that
// has spec, verify (pass), and changes recorded — ready to ship.
// Returns the repo dir.
func shipFixture(t *testing.T, originURL string) string {
	t.Helper()
	dir := t.TempDir()
	gitInit(t, dir, originURL)
	chdir(t, dir)

	if err := runCmd(t, Init()); err != nil {
		t.Fatalf("init: %v", err)
	}

	// Configure project [remote] for forgejo before the baseline commit
	// so phase create finds a clean tree.
	for _, set := range [][]string{
		{"set", "remote.provider", "forgejo"},
		{"set", "remote.api_base", "https://forge.example/api/v1"},
		{"set", "remote.log_api", "true"},
		{"set", "remote.auth_env", "MOCK_FORGEJO_TOKEN"},
		{"set", "remote.reviewers", "alice"},
		{"set", "repo.git_main_branch", "main"},
	} {
		if err := runCmd(t, Project(), set...); err != nil {
			t.Fatalf("project %v: %v", set, err)
		}
	}
	mustWrite(t, filepath.Join(dir, "README.md"), "base\n")
	gitCommit(t, dir, "initial baseline")

	// Create the phase via the real CLI — this also checks us out onto
	// phase/x, matching the post-create state ship expects.
	if err := runCmd(t, Phase(), "create", "x"); err != nil {
		t.Fatalf("phase create: %v", err)
	}

	// Drop verify.toml at pass and write phase code on the phase branch.
	root := filepath.Join(dir, ".dross")
	phaseDir := filepath.Join(root, "phases", "x")
	mustWrite(t, filepath.Join(phaseDir, "verify.toml"), `[verify]
phase = "x"
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
tests = ["tag.test.ts:42"]
`)
	// Phase create already wrote a "created" state.json entry that's
	// staged but uncommitted — fold it into the phase commit so the
	// working tree is clean when ship runs.
	mustWrite(t, filepath.Join(dir, "src/tag.ts"), "export const tag = 1\n")
	mustWrite(t, filepath.Join(phaseDir, "spec.toml"), `[phase]
id = "x"
title = "Tagging"

[[criteria]]
id = "C1"
text = "Tags can be added"
`)
	gitCommit(t, dir, "feat(tag): add tagging")
	commitSHA := gitOutT(t, dir, "rev-parse", "HEAD")
	mustWrite(t, filepath.Join(phaseDir, "changes.json"), `{
  "phase": "x",
  "tasks": {
    "t1": {"files": ["src/tag.ts"], "commit": "`+commitSHA+`", "completed_at": "2026-05-02T10:00:00Z"}
  }
}`)
	gitCommit(t, dir, "chore(dross): record task t1")
	return dir
}

func gitCommit(t *testing.T, dir, msg string) {
	t.Helper()
	mustGit(t, dir, "add", "-A")
	mustGit(t, dir, "commit", "-q", "-m", msg)
}

func gitOutT(t *testing.T, dir string, args ...string) string {
	return mustGit(t, dir, args...)
}

func TestShipNoPushSkipsPushAndPR(t *testing.T) {
	dir := shipFixture(t, "https://forge.example/me/p.git")

	if err := runCmd(t, Ship(), "--no-push"); err != nil {
		t.Fatalf("ship --no-push: %v", err)
	}

	// No state.json shipped action recorded — --no-push is a dry run.
	st, _ := state.Load(filepath.Join(dir, ".dross", "state.json"))
	for _, a := range st.History {
		if strings.HasPrefix(a.Action, "shipped x") {
			t.Error("should not record shipped action under --no-push")
		}
	}

	// We should still be on the phase branch (ship doesn't switch).
	cur := mustGit(t, dir, "symbolic-ref", "--short", "HEAD")
	if cur != "phase/x" {
		t.Errorf("expected to stay on phase/x, got %q", cur)
	}
}

func TestShipRefusesUnverified(t *testing.T) {
	dir := shipFixture(t, "https://forge.example/me/p.git")

	// Override verify verdict to "fail".
	verifyPath := filepath.Join(dir, ".dross", "phases", "x", "verify.toml")
	body, _ := os.ReadFile(verifyPath)
	body = []byte(strings.Replace(string(body), `verdict = "pass"`, `verdict = "fail"`, 1))
	if err := os.WriteFile(verifyPath, body, 0o644); err != nil {
		t.Fatal(err)
	}

	err := runCmd(t, Ship(), "--no-push")
	if err == nil {
		t.Fatal("expected error when verdict != pass")
	}
	if !strings.Contains(err.Error(), "force-unverified") {
		t.Errorf("error should mention --force-unverified: %v", err)
	}
}

func TestShipForceUnverifiedSkipsGate(t *testing.T) {
	dir := shipFixture(t, "https://forge.example/me/p.git")
	verifyPath := filepath.Join(dir, ".dross", "phases", "x", "verify.toml")
	body, _ := os.ReadFile(verifyPath)
	body = []byte(strings.Replace(string(body), `verdict = "pass"`, `verdict = "fail"`, 1))
	if err := os.WriteFile(verifyPath, body, 0o644); err != nil {
		t.Fatal(err)
	}
	gitCommit(t, dir, "test: flip verdict") // ship needs clean tree

	if err := runCmd(t, Ship(), "--no-push", "--force-unverified"); err != nil {
		t.Errorf("--force-unverified should bypass gate: %v", err)
	}
}

func TestShipFullFlowAgainstMockProvider(t *testing.T) {
	dir := shipFixture(t, "https://forge.example/me/p.git")

	// Stand up a mock Forgejo + a bare-init "remote" git repo so push works.
	remoteDir := t.TempDir()
	mustGit(t, remoteDir, "init", "-q", "--bare")
	mustGit(t, dir, "remote", "set-url", "origin", remoteDir)

	t.Setenv("MOCK_FORGEJO_TOKEN", "secret")

	var openedTitle string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if strings.HasSuffix(r.URL.Path, "/pulls") && r.Method == "POST" {
			var doc map[string]any
			_ = json.Unmarshal(body, &doc)
			openedTitle, _ = doc["title"].(string)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"number":99,"html_url":"https://forge.example/me/p/pulls/99"}`))
			return
		}
		if strings.HasSuffix(r.URL.Path, "/requested_reviewers") {
			_, _ = w.Write([]byte(`[]`))
			return
		}
		t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
	}))
	t.Cleanup(server.Close)

	// Point project.toml at the mock server. project set runs from main
	// (we're on phase/x and it writes to .dross/project.toml), so
	// commit on the phase branch before shipping (clean tree required).
	if err := runCmd(t, Project(), "set", "remote.api_base", server.URL); err != nil {
		t.Fatal(err)
	}
	gitCommit(t, dir, "test: point api_base at mock")

	if err := runCmd(t, Ship()); err != nil {
		t.Fatalf("ship: %v", err)
	}

	if !strings.Contains(openedTitle, "phase x") {
		t.Errorf("PR title should reference phase id, got: %q", openedTitle)
	}

	// Ship folds the completion record into the branch: state.json now
	// clears current_phase and records `completed x`, written and
	// committed BEFORE the push so the provider's squash-merge carries it
	// to main (c-4). The PR URL is printed, not persisted.
	st, _ := state.Load(filepath.Join(dir, ".dross", "state.json"))
	if st.CurrentPhase != "" {
		t.Errorf("ship should clear current_phase, got %q", st.CurrentPhase)
	}
	foundCompleted := false
	for _, a := range st.History {
		if strings.Contains(a.Action, "completed x") {
			foundCompleted = true
		}
	}
	if !foundCompleted {
		t.Errorf("state history should record `completed x`; history: %+v", st.History)
	}

	// Remote should have received phase/x directly (no synthetic
	// pr/<id> branch any more).
	remoteRefs := mustGit(t, remoteDir, "for-each-ref", "--format=%(refname:short)", "refs/heads")
	if !strings.Contains(remoteRefs, "phase/x") {
		t.Errorf("expected phase/x on remote, got: %q", remoteRefs)
	}

	// The completion record must live in the PUSHED ref, not a local-only
	// post-push commit. Read state.json at the pushed branch tip in the bare
	// remote and assert current_phase is cleared there. If the commit landed
	// after the push (the old behaviour), the pushed tree still carries
	// current_phase and this fails.
	pushedState := mustGit(t, remoteDir, "show", "phase/x:.dross/state.json")
	var pushed state.State
	if err := json.Unmarshal([]byte(pushedState), &pushed); err != nil {
		t.Fatalf("parse pushed state.json: %v", err)
	}
	if pushed.CurrentPhase != "" {
		t.Errorf("pushed ref should carry cleared current_phase, got %q", pushed.CurrentPhase)
	}

	// Ship must return on a clean working tree: both the completion write and
	// the PR-number record are committed as part of ship, not left
	// uncommitted. If either commit step is dropped, the tree is dirty here.
	if st := mustGit(t, dir, "status", "--porcelain"); st != "" {
		t.Errorf("working tree should be clean after ship, got: %q", st)
	}
	// t-1: the opened PR number (99 from the mock) is persisted into the
	// phase's changes.json and committed post-push, so `dross phase complete`
	// can look up THIS phase's authoritative merge status. That record commit
	// is HEAD; the `chore(dross): ship x` state commit is its parent.
	ch, err := changes.Load(changes.FilePath(filepath.Join(dir, ".dross"), "x"), "x")
	if err != nil {
		t.Fatalf("load changes.json: %v", err)
	}
	if ch.PR != 99 {
		t.Errorf("changes.json should carry the opened PR number 99, got %d", ch.PR)
	}
	if msg := mustGit(t, dir, "log", "-1", "--pretty=%s"); msg != "chore(dross): record PR #99 for x" {
		t.Errorf("HEAD should be the PR-record commit, got: %q", msg)
	}
	if msg := mustGit(t, dir, "log", "-1", "--skip=1", "--pretty=%s"); msg != "chore(dross): ship x" {
		t.Errorf("ship-state commit should be HEAD~1, got: %q", msg)
	}
}

// TestShipDoesNotPersistPRWhenOpenFails pins t-1's guard: when the provider
// rejects the PR open (OpenPR returns res==nil), ship must persist NO PR
// number and leave no `record PR` commit — never a misleading PR:0.
func TestShipDoesNotPersistPRWhenOpenFails(t *testing.T) {
	dir := shipFixture(t, "https://forge.example/me/p.git")

	remoteDir := t.TempDir()
	mustGit(t, remoteDir, "init", "-q", "--bare")
	mustGit(t, dir, "remote", "set-url", "origin", remoteDir)
	t.Setenv("MOCK_FORGEJO_TOKEN", "secret")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Reject the PR-open so openForgejoPR returns (nil, err).
		if strings.HasSuffix(r.URL.Path, "/pulls") && r.Method == "POST" {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"message":"boom"}`))
			return
		}
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(server.Close)

	if err := runCmd(t, Project(), "set", "remote.api_base", server.URL); err != nil {
		t.Fatal(err)
	}
	gitCommit(t, dir, "test: point api_base at mock")

	if err := runCmd(t, Ship()); err == nil {
		t.Fatal("expected ship to fail when the provider rejects the PR open")
	}
	ch, err := changes.Load(changes.FilePath(filepath.Join(dir, ".dross"), "x"), "x")
	if err != nil {
		t.Fatalf("load changes.json: %v", err)
	}
	if ch.PR != 0 {
		t.Errorf("no PR should be persisted when open fails, got %d", ch.PR)
	}
	if msg := mustGit(t, dir, "log", "-1", "--pretty=%s"); strings.Contains(msg, "record PR") {
		t.Errorf("no PR-record commit should exist when open fails, HEAD: %q", msg)
	}
}

// shipCapture records what a mock provider received, so --auto assertions
// can check the reviewer endpoint was never hit and the posted body/title.
type shipCapture struct {
	openedTitle  string
	openedBody   string
	openedBase   string
	openedHead   string
	reviewersHit bool
}

// shipMockFlow stands up a bare-init "remote" plus a mock Forgejo server for
// the given fixture repo, points remote.api_base at it (committing so the tree
// stays clean), and returns a capture the caller inspects after shipping. It
// mirrors TestShipFullFlowAgainstMockProvider's setup, factored out so the
// --auto tests don't duplicate the httptest scaffolding.
func shipMockFlow(t *testing.T, dir string) *shipCapture {
	t.Helper()
	remoteDir := t.TempDir()
	mustGit(t, remoteDir, "init", "-q", "--bare")
	mustGit(t, dir, "remote", "set-url", "origin", remoteDir)
	t.Setenv("MOCK_FORGEJO_TOKEN", "secret")

	cap := &shipCapture{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if strings.HasSuffix(r.URL.Path, "/pulls") && r.Method == "POST" {
			var doc map[string]any
			_ = json.Unmarshal(body, &doc)
			cap.openedTitle, _ = doc["title"].(string)
			cap.openedBody, _ = doc["body"].(string)
			cap.openedBase, _ = doc["base"].(string)
			cap.openedHead, _ = doc["head"].(string)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"number":99,"html_url":"https://forge.example/me/p/pulls/99"}`))
			return
		}
		if strings.HasSuffix(r.URL.Path, "/requested_reviewers") {
			cap.reviewersHit = true
			_, _ = w.Write([]byte(`[]`))
			return
		}
		t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
	}))
	t.Cleanup(server.Close)

	if err := runCmd(t, Project(), "set", "remote.api_base", server.URL); err != nil {
		t.Fatal(err)
	}
	gitCommit(t, dir, "test: point api_base at mock")
	return cap
}

// TestShipAutoRequestsZeroReviewers proves c-1's reviewer behaviour: with
// remote.reviewers=[alice] configured, `ship --auto` opens the PR requesting
// zero reviewers (the provider's requested_reviewers endpoint is never hit and
// no "Reviewers requested" line is printed), records a reviewers count of 0 in
// telemetry, and leaves the remote.reviewers config untouched (per the locked
// reviewers_under_auto decision — per-invocation, non-destructive).
func TestShipAutoRequestsZeroReviewers(t *testing.T) {
	dir := shipFixture(t, "https://forge.example/me/p.git")

	// Isolate HOME and re-enable telemetry so we can read back the outcome
	// event's reviewers count. shipFixture's chdir pinned DROSS_NO_TELEMETRY=1.
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("DROSS_NO_TELEMETRY", "")

	cap := shipMockFlow(t, dir)

	out := captureStdout(t, func() {
		if err := runCmd(t, Ship(), "--auto"); err != nil {
			t.Fatalf("ship --auto: %v", err)
		}
	})

	// Zero reviewers requested: the endpoint is never called, and the
	// narration line stays silent.
	if cap.reviewersHit {
		t.Error("--auto must request zero reviewers, but the requested_reviewers endpoint was hit")
	}
	if strings.Contains(out, "Reviewers requested") {
		t.Errorf("--auto must not print a 'Reviewers requested' line:\n%s", out)
	}

	// remote.reviewers config is left untouched (still the fixture's "alice").
	p, _, err := loadProject()
	if err != nil {
		t.Fatalf("reload project: %v", err)
	}
	if len(p.Remote.Reviewers) != 1 || p.Remote.Reviewers[0] != "alice" {
		t.Errorf("--auto must not mutate remote.reviewers, got %v", p.Remote.Reviewers)
	}

	// Telemetry records a reviewers count of 0 and the auto tag.
	telem := mustRead(t, filepath.Join(home, ".claude/dross", "telemetry.jsonl"))
	if !strings.Contains(telem, `"reviewers":0`) {
		t.Errorf("--auto telemetry should record reviewers count 0:\n%s", telem)
	}
	if !strings.Contains(telem, `"auto":"true"`) {
		t.Errorf("--auto telemetry should carry the auto tag:\n%s", telem)
	}
}

// TestShipAutoStillHonorsVerifyGate proves c-3: --auto does not bypass the
// "verify must be pass" gate. A pending verdict still fails unless
// --force-unverified is also passed.
func TestShipAutoStillHonorsVerifyGate(t *testing.T) {
	dir := shipFixture(t, "https://forge.example/me/p.git")

	verifyPath := filepath.Join(dir, ".dross", "phases", "x", "verify.toml")
	body, _ := os.ReadFile(verifyPath)
	body = []byte(strings.Replace(string(body), `verdict = "pass"`, `verdict = "pending"`, 1))
	if err := os.WriteFile(verifyPath, body, 0o644); err != nil {
		t.Fatal(err)
	}

	// --auto alone must still hit the gate on a pending verdict.
	err := runCmd(t, Ship(), "--auto", "--no-push")
	if err == nil {
		t.Fatal("--auto must still fail the verify gate on a pending verdict")
	}
	if !strings.Contains(err.Error(), "force-unverified") {
		t.Errorf("gate error should mention --force-unverified: %v", err)
	}

	// With --force-unverified the gate is bypassed even under --auto.
	gitCommit(t, dir, "test: flip verdict to pending") // clean tree
	if err := runCmd(t, Ship(), "--auto", "--no-push", "--force-unverified"); err != nil {
		t.Errorf("--auto --force-unverified should bypass the verify gate: %v", err)
	}
}

// TestShipAutoExplicitBodyWins proves the locked explicit_flags_win decision:
// --auto governs prompts/defaults only, so an explicit --body still overrides
// the generated body.
func TestShipAutoExplicitBodyWins(t *testing.T) {
	dir := shipFixture(t, "https://forge.example/me/p.git")
	cap := shipMockFlow(t, dir)

	if err := runCmd(t, Ship(), "--auto", "--body", "CUSTOM BODY"); err != nil {
		t.Fatalf("ship --auto --body: %v", err)
	}
	if cap.openedBody != "CUSTOM BODY" {
		t.Errorf("explicit --body must win over --auto's generated default, got %q", cap.openedBody)
	}
}

// TestShipJSONEmitsSingleObjectAndSuppressesNarration proves c-5: `ship --json`
// writes exactly one parseable JSON object with keys url/number/result to stdout
// and suppresses the human narration lines.
func TestShipJSONEmitsSingleObjectAndSuppressesNarration(t *testing.T) {
	dir := shipFixture(t, "https://forge.example/me/p.git")
	shipMockFlow(t, dir)

	out := captureStdout(t, func() {
		if err := runCmd(t, Ship(), "--json"); err != nil {
			t.Fatalf("ship --json: %v", err)
		}
	})

	// No human narration leaked onto stdout.
	for _, line := range []string{"Pushed", "PR opened", "Completion record folded"} {
		if strings.Contains(out, line) {
			t.Errorf("--json must suppress the %q narration line, got:\n%s", line, out)
		}
	}

	// Exactly one line, parseable as a JSON object with the three keys.
	trimmed := strings.TrimSpace(out)
	if strings.Contains(trimmed, "\n") {
		t.Errorf("--json should emit exactly one line, got:\n%s", out)
	}
	var obj map[string]any
	if err := json.Unmarshal([]byte(trimmed), &obj); err != nil {
		t.Fatalf("--json output should parse as one JSON object, got %q: %v", trimmed, err)
	}
	for _, k := range []string{"url", "number", "result"} {
		if _, ok := obj[k]; !ok {
			t.Errorf("--json object missing key %q: %v", k, obj)
		}
	}
}

// TestShipAutoJSONComposable proves c-5's composability clause: `ship --auto
// --json` emits clean JSON that parses, with result "opened" and the PR
// url/number, while still requesting zero reviewers.
func TestShipAutoJSONComposable(t *testing.T) {
	dir := shipFixture(t, "https://forge.example/me/p.git")
	cap := shipMockFlow(t, dir)

	out := captureStdout(t, func() {
		if err := runCmd(t, Ship(), "--auto", "--json"); err != nil {
			t.Fatalf("ship --auto --json: %v", err)
		}
	})

	var obj struct {
		URL    string `json:"url"`
		Number int    `json:"number"`
		Result string `json:"result"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &obj); err != nil {
		t.Fatalf("--auto --json should emit parseable JSON, got %q: %v", out, err)
	}
	if obj.Result != "opened" {
		t.Errorf("result should be \"opened\", got %q", obj.Result)
	}
	if obj.URL == "" || obj.Number != 99 {
		t.Errorf("JSON should carry the PR url/number, got url=%q number=%d", obj.URL, obj.Number)
	}
	if cap.reviewersHit {
		t.Error("--auto --json must still request zero reviewers")
	}
}

// TestShipReshipIsIdempotent ships the same phase twice. Because the first
// ship clears current_phase (folded into the squash), the re-ship must name
// the phase explicitly — and must not error on the second commit/push. A
// re-ship after review edits has to re-write the same completed state safely
// and return on a clean tree, never bail on "nothing to commit".
func TestShipReshipIsIdempotent(t *testing.T) {
	dir := shipFixture(t, "https://forge.example/me/p.git")

	remoteDir := t.TempDir()
	mustGit(t, remoteDir, "init", "-q", "--bare")
	mustGit(t, dir, "remote", "set-url", "origin", remoteDir)
	t.Setenv("MOCK_FORGEJO_TOKEN", "secret")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/pulls") && r.Method == "POST" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"number":99,"html_url":"https://forge.example/me/p/pulls/99"}`))
			return
		}
		if strings.HasSuffix(r.URL.Path, "/requested_reviewers") {
			_, _ = w.Write([]byte(`[]`))
			return
		}
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(server.Close)

	if err := runCmd(t, Project(), "set", "remote.api_base", server.URL); err != nil {
		t.Fatal(err)
	}
	gitCommit(t, dir, "test: point api_base at mock")

	// First ship — resolves the phase from current_phase, then clears it.
	if err := runCmd(t, Ship()); err != nil {
		t.Fatalf("first ship: %v", err)
	}
	if st := mustGit(t, dir, "status", "--porcelain"); st != "" {
		t.Fatalf("tree dirty after first ship: %q", st)
	}

	// Second ship — current_phase is now cleared, so name the phase. It must
	// succeed and leave a clean tree (re-writes the same completed state).
	if err := runCmd(t, Ship(), "x"); err != nil {
		t.Fatalf("re-ship should be idempotent, got: %v", err)
	}
	if st := mustGit(t, dir, "status", "--porcelain"); st != "" {
		t.Errorf("tree should be clean after re-ship, got: %q", st)
	}
}

// shipCoverInitProject inits a dross repo in a fresh temp dir with an isolated
// HOME (no git origin, so [remote] starts empty), then applies the given
// remote.<field>=<value> pairs. Used by the shipComment coverage tests to drive
// its [remote] preflight gate into specific states.
func shipCoverInitProject(t *testing.T, sets ...[2]string) string {
	t.Helper()
	dir := t.TempDir()
	chdir(t, dir)
	t.Setenv("HOME", t.TempDir())
	if err := runCmd(t, Init()); err != nil {
		t.Fatalf("init: %v", err)
	}
	for _, kv := range sets {
		if err := runCmd(t, Project(), "set", kv[0], kv[1]); err != nil {
			t.Fatalf("project set %s=%s: %v", kv[0], kv[1], err)
		}
	}
	return dir
}

// TestShipCover_CommentEarlyReturns exercises shipComment's argument-validation
// gates that return before any project load: the --pr guard (line 339, both the
// boundary and the negation via pr=0, plus the negation-reverse via pr=5), the
// "need --body or --body-file" guard (line 342, both operands), and the
// --body-file read + error branch (lines 345, 347).
func TestShipCover_CommentEarlyReturns(t *testing.T) {
	chdir(t, t.TempDir()) // no .dross here: these gates all return before loadProject
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"pr zero", []string{"comment", "--pr", "0", "--body", "hi"}, "--pr is required"},
		{"no body and no file", []string{"comment", "--pr", "5"}, "either --body or --body-file"},
		{"unreadable body-file", []string{"comment", "--pr", "5", "--body-file", filepath.Join(t.TempDir(), "nope.md")}, "read --body-file"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := runCmd(t, Ship(), c.args...)
			if err == nil {
				t.Fatalf("expected error for %s", c.name)
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("error %q should contain %q", err, c.want)
			}
		})
	}
}

// TestShipCover_CommentLoadProjectError drives shipComment's loadProject error
// branch (line 353): with --pr and --body both valid but no .dross root,
// loadProject returns ErrNoRoot and shipComment must surface it (the mutated
// `err == nil` branch would fall through to a nil-project deref instead).
func TestShipCover_CommentLoadProjectError(t *testing.T) {
	chdir(t, t.TempDir())
	t.Setenv("HOME", t.TempDir())
	err := runCmd(t, Ship(), "comment", "--pr", "5", "--body", "hi")
	if err == nil {
		t.Fatal("expected loadProject error without a .dross root")
	}
	if !strings.Contains(err.Error(), "no .dross") {
		t.Errorf("error should surface the missing-root failure, got: %v", err)
	}
}

// TestShipCover_CommentRemoteMissingURL drives the first operand of shipComment's
// [remote] preflight (line 356): provider is set but url is empty, so the gate
// must fire before reaching PostComment.
func TestShipCover_CommentRemoteMissingURL(t *testing.T) {
	shipCoverInitProject(t, [2]string{"remote.provider", "forgejo"})
	err := runCmd(t, Ship(), "comment", "--pr", "5", "--body", "hi")
	if err == nil {
		t.Fatal("expected [remote] gate to fire with empty url")
	}
	if !strings.Contains(err.Error(), "[remote]") {
		t.Errorf("error should name the missing [remote] config, got: %v", err)
	}
	if strings.Contains(err.Error(), "post comment") {
		t.Errorf("must not reach PostComment when url is empty, got: %v", err)
	}
}

// TestShipCover_CommentRemoteMissingProvider drives the second operand of line
// 356: url is set but provider is empty.
func TestShipCover_CommentRemoteMissingProvider(t *testing.T) {
	shipCoverInitProject(t, [2]string{"remote.url", "https://forge.example/me/p"})
	err := runCmd(t, Ship(), "comment", "--pr", "5", "--body", "hi")
	if err == nil {
		t.Fatal("expected [remote] gate to fire with empty provider")
	}
	if !strings.Contains(err.Error(), "[remote]") {
		t.Errorf("error should name the missing [remote] config, got: %v", err)
	}
	if strings.Contains(err.Error(), "post comment") {
		t.Errorf("must not reach PostComment when provider is empty, got: %v", err)
	}
}

// TestShipCover_CommentReachesPostComment drives the both-configured path of
// line 356 (gate passes) into the PostComment error branch (line 362): with a
// valid url+provider but no api_base, PostComment fails and shipComment wraps it
// as "post comment". The mutated `err == nil` branch would instead print
// "Posted comment" and return nil.
func TestShipCover_CommentReachesPostComment(t *testing.T) {
	shipCoverInitProject(t,
		[2]string{"remote.provider", "forgejo"},
		[2]string{"remote.url", "https://forge.example/me/p"},
		[2]string{"remote.auth_env", "MOCK_FORGEJO_TOKEN"},
	)
	err := runCmd(t, Ship(), "comment", "--pr", "5", "--body", "hi")
	if err == nil {
		t.Fatal("expected PostComment to fail without api_base")
	}
	if !strings.Contains(err.Error(), "post comment") {
		t.Errorf("shipComment must wrap the PostComment failure as 'post comment', got: %v", err)
	}
}

// TestShipCover_ShipBadBodyFile drives the --body-file read-error branch of the
// main ship flow (line 155): a missing --body-file must abort with "read
// --body-file" before any push (guarded by --no-push). The mutated `err == nil`
// branch would swallow the read failure and return cleanly.
func TestShipCover_ShipBadBodyFile(t *testing.T) {
	dir := shipFixture(t, "https://forge.example/me/p.git")
	err := runCmd(t, Ship(), "--no-push", "--body-file", filepath.Join(dir, "no-such-body.md"))
	if err == nil {
		t.Fatal("expected error for unreadable --body-file")
	}
	if !strings.Contains(err.Error(), "read --body-file") {
		t.Errorf("error should mention read --body-file, got: %v", err)
	}
}

// TestShipCover_ResultTag pins shipResultTag's four-way classification (lines
// 379/381/383): the (err, res) matrix maps to failed/partial/opened/noop, so
// negating any operand in those case guards misroutes at least one row.
func TestShipCover_ResultTag(t *testing.T) {
	boom := errors.New("boom")
	res := &ship.OpenResult{URL: "u", Number: 7}
	cases := []struct {
		name string
		err  error
		res  *ship.OpenResult
		want string
	}{
		{"failed", boom, nil, "failed"},
		{"partial", boom, res, "partial"},
		{"opened", nil, res, "opened"},
		{"noop", nil, nil, "noop"},
	}
	for _, c := range cases {
		if got := shipResultTag(c.res, c.err); got != c.want {
			t.Errorf("%s: shipResultTag = %q, want %q", c.name, got, c.want)
		}
	}
}

// activateMilestone sets current_milestone and commits it (keeping the tree
// clean for ship), then creates the local milestone/<version> branch. The
// caller pushes it to origin (or not, to exercise the missing-remote guard).
func activateMilestone(t *testing.T, dir, version string) {
	t.Helper()
	if err := runCmd(t, State(), "set", "current_milestone", version); err != nil {
		t.Fatalf("state set current_milestone: %v", err)
	}
	gitCommit(t, dir, "scope "+version)
	mustGit(t, dir, "branch", "milestone/"+version)
}

func TestShipTargetsMilestoneBranch(t *testing.T) {
	dir := shipFixture(t, "https://forge.example/me/p.git")
	cap := shipMockFlow(t, dir)
	activateMilestone(t, dir, "v0.9")
	mustGit(t, dir, "push", "origin", "milestone/v0.9") // base present on origin

	if err := runCmd(t, Ship()); err != nil {
		t.Fatalf("ship: %v", err)
	}
	if cap.openedBase != "milestone/v0.9" || cap.openedHead != "phase/x" {
		t.Errorf("PR base/head = %q/%q; want milestone/v0.9 / phase/x", cap.openedBase, cap.openedHead)
	}
}

func TestShipTargetsMainNoMilestone(t *testing.T) {
	dir := shipFixture(t, "https://forge.example/me/p.git")
	cap := shipMockFlow(t, dir)

	if err := runCmd(t, Ship()); err != nil {
		t.Fatalf("ship: %v", err)
	}
	if cap.openedBase != "main" {
		t.Errorf("PR base = %q; want main (no milestone)", cap.openedBase)
	}
}

func TestShipRefusesMissingRemoteBase(t *testing.T) {
	dir := shipFixture(t, "https://forge.example/me/p.git")
	shipMockFlow(t, dir)
	activateMilestone(t, dir, "v0.9") // local branch, deliberately NOT pushed

	err := runCmd(t, Ship())
	if err == nil {
		t.Fatal("expected refusal when the milestone base is absent on origin")
	}
	if !strings.Contains(err.Error(), "not on origin") {
		t.Errorf("error should point at the missing origin base: %v", err)
	}
}

func TestShipNudgesNoMilestone(t *testing.T) {
	dir := shipFixture(t, "https://forge.example/me/p.git")
	shipMockFlow(t, dir)

	out := captureStdout(t, func() {
		if err := runCmd(t, Ship()); err != nil {
			t.Fatalf("ship: %v", err)
		}
	})
	if !strings.Contains(out, "dross milestone") {
		t.Errorf("no-milestone ship should nudge naming `dross milestone`; got:\n%s", out)
	}
}
