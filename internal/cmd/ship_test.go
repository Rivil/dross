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

	// Write a baseline file + commit so we have a parent for the phase commit.
	mustWrite(t, filepath.Join(dir, "README.md"), "base\n")
	gitCommit(t, dir, "initial baseline")

	// Configure project [remote] for forgejo (we'll mock the API).
	for _, set := range [][]string{
		{"set", "remote.provider", "forgejo"},
		{"set", "remote.api_base", "https://forge.example/api/v1"}, // overridden via t.Setenv pattern not possible; tests below override per-call
		{"set", "remote.log_api", "true"},
		{"set", "remote.auth_env", "MOCK_FORGEJO_TOKEN"},
		{"set", "remote.reviewers", "alice"},
		{"set", "repo.git_main_branch", "main"},
	} {
		if err := runCmd(t, Project(), set...); err != nil {
			t.Fatalf("project %v: %v", set, err)
		}
	}

	// Create phase 01-x with spec + verify + changes.
	root := filepath.Join(dir, ".dross")
	phaseDir := filepath.Join(root, "phases", "01-x")
	if err := os.MkdirAll(phaseDir, 0o755); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(phaseDir, "spec.toml"), `[phase]
id = "01-x"
title = "Tagging"

[[criteria]]
id = "C1"
text = "Tags can be added"
`)
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

	// Write phase code + record commit in changes.json.
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

	// Mark as current phase in state.
	st, _ := state.Load(filepath.Join(root, state.File))
	st.CurrentPhase = "01-x"
	if err := st.Save(filepath.Join(root, state.File)); err != nil {
		t.Fatal(err)
	}
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

func TestShipNoPushBuildsBranchAndStops(t *testing.T) {
	dir := shipFixture(t, "https://forge.example/me/p.git")

	if err := runCmd(t, Ship(), "--no-push"); err != nil {
		t.Fatalf("ship --no-push: %v", err)
	}

	// pr/01-x should exist locally.
	out := mustGit(t, dir, "branch", "--list", "pr/01-x")
	if !strings.Contains(out, "pr/01-x") {
		t.Errorf("expected pr/01-x branch, got: %q", out)
	}

	// No state.json shipped action recorded yet (no PR opened).
	st, _ := state.Load(filepath.Join(dir, ".dross", "state.json"))
	for _, a := range st.History {
		if strings.HasPrefix(a.Action, "shipped 01-x") {
			t.Error("should not record shipped action under --no-push")
		}
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
	gitCommit(t, dir, "test: flip verdict") // FilterSquash needs clean tree

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

	// Point project.toml at the mock server, then commit (FilterSquash
	// needs a clean working tree).
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

	// Remote should have received pr/01-x.
	remoteRefs := mustGit(t, remoteDir, "for-each-ref", "--format=%(refname:short)", "refs/heads")
	if !strings.Contains(remoteRefs, "pr/01-x") {
		t.Errorf("expected pr/01-x on remote, got: %q", remoteRefs)
	}
}
