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

	"github.com/Rivil/dross/internal/state"
)

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
	// phase/01-x, matching the post-create state ship expects.
	if err := runCmd(t, Phase(), "create", "x"); err != nil {
		t.Fatalf("phase create: %v", err)
	}

	// Drop verify.toml at pass and write phase code on the phase branch.
	root := filepath.Join(dir, ".dross")
	phaseDir := filepath.Join(root, "phases", "01-x")
	mustWrite(t, filepath.Join(phaseDir, "verify.toml"), `[verify]
phase = "01-x"
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
id = "01-x"
title = "Tagging"

[[criteria]]
id = "C1"
text = "Tags can be added"
`)
	gitCommit(t, dir, "feat(tag): add tagging")
	commitSHA := gitOutT(t, dir, "rev-parse", "HEAD")
	mustWrite(t, filepath.Join(phaseDir, "changes.json"), `{
  "phase": "01-x",
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
		if strings.HasPrefix(a.Action, "shipped 01-x") {
			t.Error("should not record shipped action under --no-push")
		}
	}

	// We should still be on the phase branch (ship doesn't switch).
	cur := mustGit(t, dir, "symbolic-ref", "--short", "HEAD")
	if cur != "phase/01-x" {
		t.Errorf("expected to stay on phase/01-x, got %q", cur)
	}
}

func TestShipRefusesUnverified(t *testing.T) {
	dir := shipFixture(t, "https://forge.example/me/p.git")

	// Override verify verdict to "fail".
	verifyPath := filepath.Join(dir, ".dross", "phases", "01-x", "verify.toml")
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
	verifyPath := filepath.Join(dir, ".dross", "phases", "01-x", "verify.toml")
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
	// (we're on phase/01-x and it writes to .dross/project.toml), so
	// commit on the phase branch before shipping (clean tree required).
	if err := runCmd(t, Project(), "set", "remote.api_base", server.URL); err != nil {
		t.Fatal(err)
	}
	gitCommit(t, dir, "test: point api_base at mock")

	if err := runCmd(t, Ship()); err != nil {
		t.Fatalf("ship: %v", err)
	}

	if !strings.Contains(openedTitle, "phase 01-x") {
		t.Errorf("PR title should reference phase id, got: %q", openedTitle)
	}

	// State should record the shipped action with the PR URL.
	st, _ := state.Load(filepath.Join(dir, ".dross", "state.json"))
	found := false
	for _, a := range st.History {
		if strings.Contains(a.Action, "shipped 01-x") && strings.Contains(a.Action, "pulls/99") {
			found = true
		}
	}
	if !found {
		t.Errorf("state history should record shipped + PR URL; history: %+v", st.History)
	}

	// Remote should have received phase/01-x directly (no synthetic
	// pr/<id> branch any more).
	remoteRefs := mustGit(t, remoteDir, "for-each-ref", "--format=%(refname:short)", "refs/heads")
	if !strings.Contains(remoteRefs, "phase/01-x") {
		t.Errorf("expected phase/01-x on remote, got: %q", remoteRefs)
	}
}
