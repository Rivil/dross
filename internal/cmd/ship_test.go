package cmd

import (
	"encoding/json"
	"errors"
	"github.com/Rivil/dross/internal/hostallow"
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
	got := buildOpenOpts(p, hostallow.Policy{})
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
	got := buildCommentOpts(p, hostallow.Policy{})
	if got.Provider != "gitlab" || got.AuthEnv != "GL_TOKEN" || got.AuthScheme != "bearer" || got.ProjectID != "42" {
		t.Errorf("comment opts dropped a field: %+v", got)
	}
}

// bitbucketRemote is the [remote] shape whose credential is HTTP Basic
// user:token — the only one where a dropped auth_user is fatal.
func bitbucketRemote() *project.Project {
	return &project.Project{Remote: project.Remote{
		Provider:   "bitbucket",
		URL:        "https://bitbucket.org/acme/widget",
		APIBase:    "https://api.bitbucket.org/2.0",
		AuthEnv:    "BB_TOKEN",
		AuthUser:   "wsuser",
		AuthScheme: "basic",
	}}
}

// TestBuildOpenOptsCarriesAuthUser pins the join point between the schema field
// (project.Remote.AuthUser) and the backend that needs it. This is the one gap
// no ship-package test can see: those construct OpenOpts directly, so every one
// of them passes while a real Bitbucket ship 401s on base64(:token).
func TestBuildOpenOptsCarriesAuthUser(t *testing.T) {
	got := buildOpenOpts(bitbucketRemote(), hostallow.Policy{})
	if got.AuthUser != "wsuser" {
		t.Errorf("auth_user not copied onto OpenOpts: %q", got.AuthUser)
	}
	// The mergeGate path builds OpenOpts too, so the same field is what lets
	// PRMerged answer for a Bitbucket PR rather than erroring on credentials.
	if got.Provider != "bitbucket" || got.AuthEnv != "BB_TOKEN" || got.AuthScheme != "basic" {
		t.Errorf("bitbucket remote fields not copied: %+v", got)
	}
}

func TestBuildCommentOptsCarriesAuthUser(t *testing.T) {
	got := buildCommentOpts(bitbucketRemote(), hostallow.Policy{})
	if got.AuthUser != "wsuser" {
		t.Errorf("auth_user not copied onto CommentOpts: %q", got.AuthUser)
	}
	if got.Provider != "bitbucket" || got.AuthEnv != "BB_TOKEN" || got.AuthScheme != "basic" {
		t.Errorf("bitbucket remote fields not copied: %+v", got)
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
	// Rewritten wholesale, so it must carry the base `phase create` recorded
	// at fork time — otherwise the fixture silently models a phase that never
	// recorded one, and ship's overwrite would look like a first write.
	mustWrite(t, filepath.Join(phaseDir, "changes.json"), `{
  "phase": "x",
  "base": "main",
  "tasks": {
    "t1": {"files": ["src/tag.ts"], "commit": "`+commitSHA+`", "completed_at": "2026-05-02T10:00:00Z"}
  }
}`)
	gitCommit(t, dir, "chore(dross): record task t1")
	return dir
}

// gitCommit stages everything and commits. --allow-empty because the thing
// being committed is often a state.json write, which is gitignored — the point
// of the call is to leave a clean tree and advance the branch, and an empty
// commit does both.
func gitCommit(t *testing.T, dir, msg string) {
	t.Helper()
	mustGit(t, dir, "add", "-A")
	mustGit(t, dir, "commit", "-q", "--allow-empty", "-m", msg)
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

// TestShipWrongBranchRefusalNamesGuardedCheckout (c-1): the off-branch refusal
// has to point at `dross phase checkout`, not `git checkout`. This phase
// removed every raw switch from ship.md, but a refusal that hands the user the
// unguarded verb reopens the same hole by hand — and the prompt guard greps
// ship.md only, so nothing covered the CLI's own narration.
//
// The fixture branches off the phase tip rather than checking out main: the
// branch check sits behind the spec + verify gates, so a working tree without
// .dross/phases/x would fail at "load spec" and never reach the line under
// test.
func TestShipWrongBranchRefusalNamesGuardedCheckout(t *testing.T) {
	dir := shipFixture(t, "https://forge.example/me/p.git")
	mustGit(t, dir, "checkout", "-q", "-b", "sidequest")

	err := runCmd(t, Ship(), "--no-push")
	if err == nil {
		t.Fatal("expected ship to refuse off the phase branch")
	}
	msg := err.Error()

	if !strings.Contains(msg, "dross phase checkout x") {
		t.Errorf("refusal should name the guarded verb with the phase id; got: %s", msg)
	}
	if strings.Contains(msg, "git checkout") {
		t.Errorf("refusal must not hand the user a raw checkout; got: %s", msg)
	}
	// Both branches still named, so the verb assertion cannot be satisfied by
	// a refusal that dropped the diagnostic detail.
	for _, needle := range []string{"phase/x", "sidequest"} {
		if !strings.Contains(msg, needle) {
			t.Errorf("refusal should name %q; got: %s", needle, msg)
		}
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
	// The API lives on a different host from [remote].url here, which is the
	// case the machine-local escape hatch exists for: authorize it by hand on
	// this machine, never through committed config.
	if err := runCmd(t, Local(), "set", "allow_hosts", server.Listener.Addr().String()); err != nil {
		t.Fatal(err)
	}
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

	// Ship marks the phase shipped and stops there: current_phase stays set
	// and history records `shipped x`. The completed-state transition belongs
	// to `dross phase complete`, behind the merge gate. The PR URL is printed,
	// not persisted.
	st, _ := state.Load(filepath.Join(dir, ".dross", "state.json"))
	if st.CurrentPhase != "x" {
		t.Errorf("ship should leave current_phase set to the shipped phase, got %q", st.CurrentPhase)
	}
	if st.CurrentPhaseStatus != "shipped" {
		t.Errorf("ship should set current_phase_status to shipped, got %q", st.CurrentPhaseStatus)
	}
	foundShipped := false
	for _, a := range st.History {
		if strings.Contains(a.Action, "completed x") {
			t.Errorf("ship must not record `completed x` — that is phase complete's write: %+v", st.History)
		}
		if strings.Contains(a.Action, "shipped x") {
			foundShipped = true
		}
	}
	if !foundShipped {
		t.Errorf("state history should record `shipped x`; history: %+v", st.History)
	}

	// Remote should have received phase/x directly (no synthetic
	// pr/<id> branch any more).
	remoteRefs := mustGit(t, remoteDir, "for-each-ref", "--format=%(refname:short)", "refs/heads")
	if !strings.Contains(remoteRefs, "phase/x") {
		t.Errorf("expected phase/x on remote, got: %q", remoteRefs)
	}

	// The shipped record is written locally and stays there: state.json is
	// gitignored (locked state_tracking), so the pushed ref must carry no copy
	// of it at all. Re-staging it would put the clobber back.
	if gitAllowFail(remoteDir, "cat-file", "-e", "phase/x:.dross/state.json") {
		t.Error("the pushed ref carries .dross/state.json — it must stay machine-local")
	}
	var local state.State
	localBytes := mustRead(t, filepath.Join(dir, ".dross", state.File))
	if err := json.Unmarshal([]byte(localBytes), &local); err != nil {
		t.Fatalf("parse local state.json: %v", err)
	}
	if local.CurrentPhaseStatus != "shipped" {
		t.Errorf("ship should mark the phase shipped locally, got %q", local.CurrentPhaseStatus)
	}

	// Ship must return on a clean working tree: the PR-number record is
	// committed as part of ship, not left uncommitted. If that commit step is
	// dropped, the tree is dirty here.
	if st := mustGit(t, dir, "status", "--porcelain"); st != "" {
		t.Errorf("working tree should be clean after ship, got: %q", st)
	}
	// t-1: the opened PR number (99 from the mock) is persisted into the
	// phase's changes.json and committed post-push, so `dross phase complete`
	// can look up THIS phase's authoritative merge status. That record commit
	// is HEAD.
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
	// There is no `chore(dross): ship x` commit any more: state.json was the
	// only thing it ever carried, and that write is machine-local now.
	if log := mustGit(t, dir, "log", "--pretty=%s"); strings.Contains(log, "chore(dross): ship x") {
		t.Errorf("ship should no longer commit a state fold:\n%s", log)
	}
}

// TestShipPushesPRRecordToPhaseBranch proves c-1/c-2: the PR-number record
// commit is PUSHED to origin's phase-branch ref, not left as a local-only
// post-push commit. It reads changes.json at the pushed branch tip in the bare
// remote and asserts it carries the opened PR number. Before this fix the record
// commit was made after the push and never reached origin, so the pushed tree's
// changes.json had PR:0 — which made phase complete's mergeGate fall back and
// refuse every squash-merged completion.
func TestShipPushesPRRecordToPhaseBranch(t *testing.T) {
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

	// The API lives on a different host from [remote].url here, which is the
	// case the machine-local escape hatch exists for: authorize it by hand on
	// this machine, never through committed config.
	if err := runCmd(t, Local(), "set", "allow_hosts", server.Listener.Addr().String()); err != nil {
		t.Fatal(err)
	}
	if err := runCmd(t, Project(), "set", "remote.api_base", server.URL); err != nil {
		t.Fatal(err)
	}
	gitCommit(t, dir, "test: point api_base at mock")

	if err := runCmd(t, Ship()); err != nil {
		t.Fatalf("ship: %v", err)
	}

	// The record commit must be HEAD of the PUSHED ref, not a local-only commit
	// past origin. If the push were dropped, origin/phase-x would trail local
	// HEAD by the record commit and this SHA comparison would differ.
	localHead := mustGit(t, dir, "rev-parse", "HEAD")
	remoteHead := mustGit(t, remoteDir, "rev-parse", "phase/x")
	if localHead != remoteHead {
		t.Fatalf("PR-record commit is local-only: local HEAD %s != pushed phase/x %s", localHead, remoteHead)
	}

	// Read changes.json at the pushed branch tip in the bare remote and assert it
	// carries the opened PR number — i.e. the record actually reached origin (and
	// so will reach the base via the squash-merge).
	pushedChanges := mustGit(t, remoteDir, "show", "phase/x:.dross/phases/x/changes.json")
	var pushed changes.Changes
	if err := json.Unmarshal([]byte(pushedChanges), &pushed); err != nil {
		t.Fatalf("parse pushed changes.json: %v", err)
	}
	if pushed.PR != 99 {
		t.Errorf("pushed changes.json should carry PR 99, got %d (record left local-only)", pushed.PR)
	}
	// The base rides the same commit and push as the PR number, so the squash
	// carries a consistent (base, pr) pair onto the base branch — that pair is
	// what `dross phase complete` reconciles against.
	if pushed.Base != "main" {
		t.Errorf("pushed changes.json should carry base \"main\", got %q (base write left local-only)", pushed.Base)
	}

	// And the pushed tip must be the record commit itself.
	if msg := mustGit(t, remoteDir, "log", "-1", "--pretty=%s", "phase/x"); msg != "chore(dross): record PR #99 for x" {
		t.Errorf("pushed phase/x tip should be the PR-record commit, got: %q", msg)
	}
}

// TestShipNoPushIssuesNoRecordPush proves c-3's --no-push scope: with --no-push
// ship returns before opening a PR, so no PR-record push (or any push) can fire.
// A bare remote is wired up; if ship pushed anything, phase/x would appear there.
func TestShipNoPushIssuesNoRecordPush(t *testing.T) {
	dir := shipFixture(t, "https://forge.example/me/p.git")

	remoteDir := t.TempDir()
	mustGit(t, remoteDir, "init", "-q", "--bare")
	mustGit(t, dir, "remote", "set-url", "origin", remoteDir)

	if err := runCmd(t, Ship(), "--no-push"); err != nil {
		t.Fatalf("ship --no-push: %v", err)
	}

	// Nothing was pushed: the bare remote has no branches at all.
	if refs := mustGit(t, remoteDir, "for-each-ref", "--format=%(refname:short)", "refs/heads"); strings.TrimSpace(refs) != "" {
		t.Errorf("--no-push must not push any ref, but remote has: %q", refs)
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

	// The API lives on a different host from [remote].url here, which is the
	// case the machine-local escape hatch exists for: authorize it by hand on
	// this machine, never through committed config.
	if err := runCmd(t, Local(), "set", "allow_hosts", server.Listener.Addr().String()); err != nil {
		t.Fatal(err)
	}
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

	// The API lives on a different host from [remote].url here, which is the
	// case the machine-local escape hatch exists for: authorize it by hand on
	// this machine, never through committed config.
	if err := runCmd(t, Local(), "set", "allow_hosts", server.Listener.Addr().String()); err != nil {
		t.Fatal(err)
	}
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

	// No human narration leaked onto stdout. The last needle is ship's
	// post-PR line naming the completion-record owner — it moved with the
	// write, so the needle moves with it rather than going vacuous.
	for _, line := range []string{"Pushed", "PR opened", "dross phase complete"} {
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

// TestShipIsReShippable ships the same phase twice. A re-ship after review
// edits must exit 0, leave the phase reading `shipped`, and append exactly one
// `shipped <id>` entry — the breadcrumb is history-scan-guarded, so a second
// run re-asserts the status without doubling the row. It must also return on a
// clean tree, never bail on "nothing to commit".
func TestShipIsReShippable(t *testing.T) {
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

	// The API lives on a different host from [remote].url here, which is the
	// case the machine-local escape hatch exists for: authorize it by hand on
	// this machine, never through committed config.
	if err := runCmd(t, Local(), "set", "allow_hosts", server.Listener.Addr().String()); err != nil {
		t.Fatal(err)
	}
	if err := runCmd(t, Project(), "set", "remote.api_base", server.URL); err != nil {
		t.Fatal(err)
	}
	gitCommit(t, dir, "test: point api_base at mock")

	// First ship — resolves the phase from current_phase and leaves it set.
	if err := runCmd(t, Ship()); err != nil {
		t.Fatalf("first ship: %v", err)
	}
	if st := mustGit(t, dir, "status", "--porcelain"); st != "" {
		t.Fatalf("tree dirty after first ship: %q", st)
	}

	// Second ship — current_phase is still set, so no argument is needed. It
	// must succeed and leave a clean tree (re-writes the same shipped status).
	if err := runCmd(t, Ship()); err != nil {
		t.Fatalf("re-ship should be idempotent, got: %v", err)
	}
	if st := mustGit(t, dir, "status", "--porcelain"); st != "" {
		t.Errorf("tree should be clean after re-ship, got: %q", st)
	}

	s, err := state.Load(filepath.Join(dir, ".dross", state.File))
	if err != nil {
		t.Fatal(err)
	}
	if s.CurrentPhaseStatus != "shipped" {
		t.Errorf("re-ship should leave current_phase_status shipped, got %q", s.CurrentPhaseStatus)
	}
	shipped := 0
	for _, a := range s.History {
		if strings.Contains(a.Action, "shipped x") {
			shipped++
		}
	}
	if shipped != 1 {
		t.Errorf("re-ship should not double the `shipped x` entry, got %d: %+v", shipped, s.History)
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

// readShippedBase reads a phase's recorded forked-from base from the working
// tree.
func readShippedBase(t *testing.T, dir, phaseID string) string {
	t.Helper()
	ch, err := changes.Load(changes.FilePath(filepath.Join(dir, ".dross"), phaseID), phaseID)
	if err != nil {
		t.Fatalf("load changes for %s: %v", phaseID, err)
	}
	return ch.Base
}

// TestShipOverwritesRecordedBase is the authoritative half of
// base_write_timing (c-2): create recorded "main", but the PR was opened
// against milestone/v0.9, and completion must reconcile against what the PR
// actually targeted. A milestone scoped after the fork is the ordinary way the
// two diverge.
func TestShipOverwritesRecordedBase(t *testing.T) {
	dir := shipFixture(t, "https://forge.example/me/p.git")
	cap := shipMockFlow(t, dir)

	if got := readShippedBase(t, dir, "x"); got != "main" {
		t.Fatalf("fixture precondition: create-time base = %q, want %q", got, "main")
	}

	activateMilestone(t, dir, "v0.9")
	mustGit(t, dir, "push", "origin", "milestone/v0.9")

	if err := runCmd(t, Ship()); err != nil {
		t.Fatalf("ship: %v", err)
	}
	if cap.openedBase != "milestone/v0.9" {
		t.Fatalf("fixture precondition: PR opened against %q, want milestone/v0.9", cap.openedBase)
	}
	if got := readShippedBase(t, dir, "x"); got != "milestone/v0.9" {
		t.Errorf("recorded base = %q, want %q — ship must overwrite the create-time value", got, "milestone/v0.9")
	}
}

// TestShipEarlyReturnsLeaveBaseIntact pins that the base write sits with the
// PR record, past --no-push and --print-body: those flags open no PR, so
// there is no authoritative base to record and no commit to make.
func TestShipEarlyReturnsLeaveBaseIntact(t *testing.T) {
	for _, flag := range []string{"--no-push", "--print-body"} {
		t.Run(flag, func(t *testing.T) {
			dir := shipFixture(t, "https://forge.example/me/p.git")
			remoteDir := t.TempDir()
			mustGit(t, remoteDir, "init", "-q", "--bare")
			mustGit(t, dir, "remote", "set-url", "origin", remoteDir)

			headBefore := mustGit(t, dir, "rev-parse", "HEAD")
			if err := runCmd(t, Ship(), flag); err != nil {
				t.Fatalf("ship %s: %v", flag, err)
			}
			if got := readShippedBase(t, dir, "x"); got != "main" {
				t.Errorf("%s changed the recorded base to %q, want the create-time %q", flag, got, "main")
			}
			if got := mustGit(t, dir, "rev-parse", "HEAD"); got != headBefore {
				t.Errorf("%s produced an extra commit: HEAD %s -> %s", flag, headBefore, got)
			}
		})
	}
}

// TestShipReconcilesRecordedQuickBase (c-7) is the ship-side twin of
// TestPhaseCompleteReconcilesRecordedQuickBase. pushQuickBaseIfRecorded has two
// call sites — phase complete's and ship's — and the criterion says "any
// command that later reconciles it", so both need a failing test behind them:
// with only the complete-side test, deleting ship's call leaves the suite green
// while a standalone quick task's chores sit unpushed through the whole ship.
func TestShipReconcilesRecordedQuickBase(t *testing.T) {
	dir := shipFixture(t, "https://forge.example/me/p.git")
	shipMockFlow(t, dir)

	// A branch standing in for a standalone quick task's target, carrying an
	// unpushed .dross-only chore. It is not this phase's base (that is main),
	// which is exactly the case the record exists for.
	mustGit(t, dir, "push", "-q", "origin", "main:quick-target")
	mustGit(t, dir, "branch", "quick-target", "main")
	mustGit(t, dir, "fetch", "-q", "origin")
	mustGit(t, dir, "checkout", "-q", "quick-target")
	mustWrite(t, filepath.Join(dir, ".dross", "handoff.md"), "# quick chore\n")
	mustGit(t, dir, "add", ".dross/handoff.md")
	mustGit(t, dir, "commit", "-q", "-m", "chore(dross): quick task bookkeeping")
	mustGit(t, dir, "checkout", "-q", "phase/x")

	if err := runCmd(t, Local(), "set", "quick_base", "quick-target"); err != nil {
		t.Fatalf("local set: %v", err)
	}
	if ahead := mustGit(t, dir, "rev-list", "origin/quick-target..quick-target"); ahead == "" {
		t.Fatal("fixture precondition: quick-target should be ahead of origin")
	}

	if err := runCmd(t, Ship()); err != nil {
		t.Fatalf("ship: %v", err)
	}
	if ahead := mustGit(t, dir, "rev-list", "origin/quick-target..quick-target"); ahead != "" {
		t.Errorf("ship left the recorded quick_base unpushed: %q", ahead)
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

// c-1: ship's pre-stage gate refuses real code dirt before anything is staged
// or pushed — the bare remote must never see phase/x.
func TestShipRefusesStrayCodeDirtBeforePush(t *testing.T) {
	dir := shipFixture(t, "https://forge.example/me/p.git")
	shipMockFlow(t, dir)
	mustWrite(t, filepath.Join(dir, "src/stray.ts"), "x\n")

	err := runCmd(t, Ship())
	if err == nil || !strings.Contains(err.Error(), "working tree is dirty") {
		t.Fatalf("expected dirty-tree refusal on stray code dirt, got: %v", err)
	}
	if !strings.Contains(err.Error(), "stray.ts") {
		t.Errorf("refusal should name the offending path: %v", err)
	}
	remoteDir := mustGit(t, dir, "remote", "get-url", "origin")
	refs := mustGit(t, remoteDir, "for-each-ref", "--format=%(refname:short)", "refs/heads")
	if strings.Contains(refs, "phase/x") {
		t.Errorf("refusal must land before the push; remote has: %q", refs)
	}
}

// c-1: ship with only .dross dirt auto-commits it and proceeds — the pushed
// ref carries the bookkeeping, and the local tree ends clean.
func TestShipAutoCommitsDrossDirtThenPushes(t *testing.T) {
	dir := shipFixture(t, "https://forge.example/me/p.git")
	shipMockFlow(t, dir)
	mustWrite(t, filepath.Join(dir, ".dross", "handoff.md"), "# handoff\n")

	if err := runCmd(t, Ship()); err != nil {
		t.Fatalf("ship should proceed past .dross-only dirt: %v", err)
	}
	log := mustGit(t, dir, "log", "--format=%s")
	if !strings.Contains(log, "chore(dross): auto-commit bookkeeping") {
		t.Errorf("expected an auto-commit chore in the log:\n%s", log)
	}
	remoteDir := mustGit(t, dir, "remote", "get-url", "origin")
	pushed := mustGit(t, remoteDir, "show", "phase/x:.dross/handoff.md")
	if !strings.Contains(pushed, "handoff") {
		t.Errorf("pushed ref should carry the auto-committed file, got: %q", pushed)
	}
	if st := mustGit(t, dir, "status", "--porcelain"); st != "" {
		t.Errorf("tree should be clean after ship, got: %q", st)
	}
}

// --- ship auto-heal of unfinalized verdicts (verify-auto-finalize c-1) ---

// TestShipHealsUnfinalizedVerdict: shipping a phase whose verify.toml
// verdict is resolved but never finalized must record the verify
// outcome event (with the verdict tag) and write the finalized marker
// back, before the pass gate evaluates.
func TestShipHealsUnfinalizedVerdict(t *testing.T) {
	dir := shipFixture(t, "https://forge.example/me/p.git")

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("DROSS_NO_TELEMETRY", "") // re-enable (chdir pins it to "1")

	if err := runCmd(t, Ship(), "--no-push"); err != nil {
		t.Fatalf("ship --no-push: %v", err)
	}

	telemBody := mustRead(t, filepath.Join(home, ".claude/dross", "telemetry.jsonl"))
	if !strings.Contains(telemBody, `"verdict":"pass"`) {
		t.Errorf("ship should record the resolved verdict outcome event:\n%s", telemBody)
	}
	vbody := mustRead(t, filepath.Join(dir, ".dross", "phases", "x", "verify.toml"))
	if !strings.Contains(vbody, "finalized = true") {
		t.Errorf("ship heal must write finalized=true into verify.toml:\n%s", vbody)
	}
}

// TestShipHealIdempotent: shipping an already-finalized phase must not
// emit a duplicate outcome event.
func TestShipHealIdempotent(t *testing.T) {
	dir := shipFixture(t, "https://forge.example/me/p.git")

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("DROSS_NO_TELEMETRY", "")

	// Manual finalize first — the primary path.
	if err := runCmd(t, Verify(), "finalize", "x"); err != nil {
		t.Fatalf("finalize: %v", err)
	}
	gitCommit(t, dir, "chore(dross): record verify") // marker write dirties the tree

	if err := runCmd(t, Ship(), "--no-push"); err != nil {
		t.Fatalf("ship --no-push: %v", err)
	}

	telemBody := mustRead(t, filepath.Join(home, ".claude/dross", "telemetry.jsonl"))
	if got := strings.Count(telemBody, `"verdict":"pass"`); got != 1 {
		t.Errorf("ship on finalized phase must not duplicate the outcome event, got %d:\n%s", got, telemBody)
	}
}

// TestShipHealRecordsPartialThenRefuses: a partial verdict is recorded
// (heal fires) but the pass-only gate still refuses the ship.
func TestShipHealRecordsPartialThenRefuses(t *testing.T) {
	dir := shipFixture(t, "https://forge.example/me/p.git")

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("DROSS_NO_TELEMETRY", "")

	verifyPath := filepath.Join(dir, ".dross", "phases", "x", "verify.toml")
	body, _ := os.ReadFile(verifyPath)
	body = []byte(strings.Replace(string(body), `verdict = "pass"`, `verdict = "partial"`, 1))
	if err := os.WriteFile(verifyPath, body, 0o644); err != nil {
		t.Fatal(err)
	}

	err := runCmd(t, Ship(), "--no-push")
	if err == nil {
		t.Fatal("expected refusal for partial verdict")
	}
	if !strings.Contains(err.Error(), "force-unverified") {
		t.Errorf("refusal should mention --force-unverified: %v", err)
	}
	telemBody := mustRead(t, filepath.Join(home, ".claude/dross", "telemetry.jsonl"))
	if !strings.Contains(telemBody, `"verdict":"partial"`) {
		t.Errorf("partial verdict should be recorded before the refusal:\n%s", telemBody)
	}
}

// TestShipRecordsShippedNotCompleted (c-2): ship records the intermediate
// truth and nothing more. A phase is not complete until its PR is merged, and
// ship runs before that is known — so it marks the phase `shipped`, leaves
// current_phase set, and writes no `completed <id>` breadcrumb. Restoring
// either clear fails this. The record stays machine-local, as before.
func TestShipRecordsShippedNotCompleted(t *testing.T) {
	dir := shipFixture(t, "https://forge.example/me/p.git")
	shipMockFlow(t, dir)

	if err := runCmd(t, Ship()); err != nil {
		t.Fatalf("ship: %v", err)
	}

	s, err := state.Load(filepath.Join(dir, ".dross", state.File))
	if err != nil {
		t.Fatal(err)
	}
	if s.CurrentPhase != "x" {
		t.Errorf("ship must leave current_phase set — only a confirmed merge clears it; got %q", s.CurrentPhase)
	}
	if s.CurrentPhaseStatus != "shipped" {
		t.Errorf("ship should set current_phase_status to shipped, got %q", s.CurrentPhaseStatus)
	}
	shipped := false
	for _, a := range s.History {
		if strings.Contains(a.Action, "completed x") {
			t.Errorf("ship must not write the completion record — that is phase complete's: %+v", s.History)
		}
		if strings.Contains(a.Action, "shipped x") {
			shipped = true
		}
	}
	if !shipped {
		t.Errorf("ship should record `shipped x` locally: %+v", s.History)
	}
	// …and nothing on HEAD carries it.
	if gitAllowFail(dir, "cat-file", "-e", "HEAD:.dross/"+state.File) {
		t.Error("HEAD carries .dross/state.json — the record must stay machine-local")
	}
	if st := mustGit(t, dir, "status", "--porcelain"); st != "" {
		t.Errorf("ship should leave a clean tree, got: %q", st)
	}
}

// TestShipNarratesCompleteOwnsRecord (c-3): the post-PR narration must not
// claim the completion record rides the squash — it doesn't any more. It has
// to name the command that actually writes it, so the user knows the run isn't
// finished at ship time.
func TestShipNarratesCompleteOwnsRecord(t *testing.T) {
	dir := shipFixture(t, "https://forge.example/me/p.git")
	shipMockFlow(t, dir)

	out := captureStdout(t, func() {
		if err := runCmd(t, Ship()); err != nil {
			t.Fatalf("ship: %v", err)
		}
	})

	// The retired phrases come from retiredSquashClaims (squash_claim_guard_test.go)
	// rather than being spelled out here: that file is the only one the
	// repo-wide scan skips, so naming a forbidden phrase anywhere else would
	// trip the scan with the very assertion that forbids it.
	lower := strings.ToLower(out)
	for _, stale := range retiredSquashClaims {
		if strings.Contains(lower, stale) {
			t.Errorf("ship still narrates the retired claim %q:\n%s", stale, out)
		}
	}
	if !strings.Contains(out, "dross phase complete") {
		t.Errorf("ship should name `dross phase complete` as the completion-record writer:\n%s", out)
	}
}

// TestPromptsNeverStageStateJSONByPath (c-3): an explicit `git add
// .dross/state.json` in a shipped prompt hard-fails every run of that slash
// command once the file is gitignored. The directory form is the only safe one.
func TestPromptsNeverStageStateJSONByPath(t *testing.T) {
	dir := filepath.Join(repoRootFromTest(t), "assets", "prompts")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(b), "\n") {
			if strings.Contains(line, "git add") && strings.Contains(line, state.File) {
				t.Errorf("%s:%d stages state.json by explicit path: %s", e.Name(), i+1, strings.TrimSpace(line))
			}
		}
	}
}
