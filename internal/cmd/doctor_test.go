package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestSameRemoteURL(t *testing.T) {
	tests := []struct {
		a, b string
		want bool
	}{
		{"https://github.com/Rivil/dross", "https://github.com/Rivil/dross.git", true},
		{"git@github.com:Rivil/dross.git", "https://github.com/Rivil/dross", true},
		{"ssh://git@github.com/Rivil/dross.git", "https://github.com/Rivil/dross", true},
		{"https://github.com/Rivil/dross", "https://github.com/other/dross", false},
		{"https://github.com/Rivil/dross", "https://gitlab.com/Rivil/dross", false},
		{"", "", true}, // both empty are equal — caller handles "missing" before calling
	}
	for _, tc := range tests {
		got := sameRemoteURL(tc.a, tc.b)
		if got != tc.want {
			t.Errorf("sameRemoteURL(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestDoctorWarnsOnMissingRemote(t *testing.T) {
	dir := t.TempDir()
	gitInit(t, dir, "https://github.com/Rivil/dross.git")
	chdir(t, dir)
	if err := runCmd(t, Init()); err != nil {
		t.Fatalf("init: %v", err)
	}
	// init now writes [remote] from git origin → doctor should pass.
	out := captureStdout(t, func() {
		_ = runCmd(t, Doctor())
	})
	if !strings.Contains(out, "git origin matches [remote].url") {
		t.Errorf("expected match line in healthy doctor output, got:\n%s", out)
	}
}

func TestDoctorFlagsMismatchedRemote(t *testing.T) {
	dir := t.TempDir()
	gitInit(t, dir, "https://github.com/Rivil/dross.git")
	chdir(t, dir)
	if err := runCmd(t, Init()); err != nil {
		t.Fatalf("init: %v", err)
	}
	// Tamper with project.toml [remote].url
	if err := runCmd(t, Project(), "set", "remote.url", "https://github.com/other/repo"); err != nil {
		t.Fatalf("project set: %v", err)
	}
	out := captureStdout(t, func() {
		_ = runCmd(t, Doctor())
	})
	if !strings.Contains(out, "does not match") {
		t.Errorf("expected mismatch warning, got:\n%s", out)
	}
}

func TestDoctorFlagsMissingAuthEnv(t *testing.T) {
	dir := t.TempDir()
	gitInit(t, dir, "https://github.com/Rivil/dross.git")
	chdir(t, dir)
	if err := runCmd(t, Init()); err != nil {
		t.Fatalf("init: %v", err)
	}
	if err := runCmd(t, Project(), "set", "remote.auth_env", "DROSS_TEST_TOKEN_DEFINITELY_UNSET"); err != nil {
		t.Fatalf("project set: %v", err)
	}
	t.Setenv("DROSS_TEST_TOKEN_DEFINITELY_UNSET", "") // explicit absence
	out := captureStdout(t, func() {
		_ = runCmd(t, Doctor())
	})
	if !strings.Contains(out, "is not set in this shell") {
		t.Errorf("expected auth_env warning, got:\n%s", out)
	}
}

func TestDoctorAcceptsGitLabRemote(t *testing.T) {
	dir := t.TempDir()
	gitInit(t, dir, "https://gitlab.com/Rivil/dross.git")
	chdir(t, dir)
	if err := runCmd(t, Init()); err != nil {
		t.Fatalf("init: %v", err)
	}
	// c-1: a GitLab remote with auth_env set is validated, not rejected.
	if err := runCmd(t, Project(), "set", "remote.auth_env", "DROSS_TEST_GITLAB_TOKEN"); err != nil {
		t.Fatalf("project set: %v", err)
	}
	t.Setenv("DROSS_TEST_GITLAB_TOKEN", "secret")

	var out string
	err := runCmdCapturing(t, &out, Doctor())
	if err != nil {
		t.Fatalf("doctor should accept a well-formed GitLab remote, got error; out:\n%s", out)
	}
	if !strings.Contains(out, "git origin matches [remote].url") {
		t.Errorf("expected origin-match line for the gitlab remote:\n%s", out)
	}
	if !strings.Contains(out, "$DROSS_TEST_GITLAB_TOKEN is set") {
		t.Errorf("expected auth_env-set line for the gitlab remote:\n%s", out)
	}
}

// TestDoctorValidatesBoardBlock proves c-1: doctor validates a configured
// [board] independently of [remote] — flagging an unset $auth_env, an
// unrecognised provider, a malformed base_url, and an invalid milestone_mode,
// while passing a well-formed youtrack board with a ✓ line.
func TestDoctorValidatesBoardBlock(t *testing.T) {
	const tokenEnv = "DROSS_TEST_BOARD_TOKEN"

	// runWithBoard inits a repo with a well-formed youtrack [board] as the
	// baseline, applies the caller's overrides, optionally exports the token,
	// then runs doctor and returns its captured output + error.
	runWithBoard := func(t *testing.T, overrides map[string]string, exportToken bool) (string, error) {
		t.Helper()
		dir := t.TempDir()
		gitInit(t, dir, "https://gitlab.com/Rivil/dross.git")
		chdir(t, dir)
		if err := runCmd(t, Init()); err != nil {
			t.Fatalf("init: %v", err)
		}
		fields := map[string]string{
			// Point [remote].auth_env at the same token so the [remote] block
			// stays clean and only the [board] block decides the verdict.
			"remote.auth_env":      tokenEnv,
			"board.provider":       "youtrack",
			"board.base_url":       "https://yt.example.com",
			"board.auth_env":       tokenEnv,
			"board.project":        "PROJ",
			"board.enabled":        "true",
			"board.milestone_mode": "version",
		}
		for k, v := range overrides {
			fields[k] = v
		}
		for k, v := range fields {
			if err := runCmd(t, Project(), "set", k, v); err != nil {
				t.Fatalf("project set %s: %v", k, err)
			}
		}
		if exportToken {
			t.Setenv(tokenEnv, "secret")
		} else {
			t.Setenv(tokenEnv, "") // explicit absence
		}
		var out string
		err := runCmdCapturing(t, &out, Doctor())
		return out, err
	}

	t.Run("well-formed youtrack board", func(t *testing.T) {
		out, err := runWithBoard(t, nil, true)
		if err != nil {
			t.Fatalf("doctor should accept a well-formed board, got error; out:\n%s", out)
		}
		if !strings.Contains(out, "[board] is well-formed") {
			t.Errorf("expected ✓ board line:\n%s", out)
		}
	})

	bad := []struct {
		name      string
		overrides map[string]string
		export    bool
		want      string
	}{
		{"unset auth_env", nil, false, "auth_env"},
		{"bogus provider", map[string]string{"board.provider": "bogus"}, true, "provider"},
		{"bad base_url", map[string]string{"board.base_url": "not a url"}, true, "base_url"},
		{"invalid milestone_mode", map[string]string{"board.milestone_mode": "bogus"}, true, "milestone_mode"},
	}
	for _, tc := range bad {
		t.Run(tc.name, func(t *testing.T) {
			out, err := runWithBoard(t, tc.overrides, tc.export)
			if err == nil {
				t.Errorf("expected non-nil error for %s; out:\n%s", tc.name, out)
			}
			if !strings.Contains(out, "✗") || !strings.Contains(out, tc.want) {
				t.Errorf("expected ✗ %s line, got:\n%s", tc.want, out)
			}
		})
	}
}

func TestDoctorFlagsInvalidAuthScheme(t *testing.T) {
	dir := t.TempDir()
	gitInit(t, dir, "https://gitlab.com/Rivil/dross.git")
	chdir(t, dir)
	if err := runCmd(t, Init()); err != nil {
		t.Fatalf("init: %v", err)
	}
	if err := runCmd(t, Project(), "set", "remote.auth_scheme", "token"); err != nil {
		t.Fatalf("project set: %v", err)
	}
	var out string
	err := runCmdCapturing(t, &out, Doctor())
	if err == nil {
		t.Error("expected non-nil error for an invalid remote.auth_scheme")
	}
	if !strings.Contains(out, "auth_scheme") || !strings.Contains(out, "invalid") {
		t.Errorf("expected invalid auth_scheme warning, got:\n%s", out)
	}
}

func TestDoctorReturnsErrorOnIssues(t *testing.T) {
	dir := t.TempDir()
	gitInit(t, dir, "https://github.com/Rivil/dross.git")
	chdir(t, dir)
	if err := runCmd(t, Init()); err != nil {
		t.Fatalf("init: %v", err)
	}
	if err := runCmd(t, Project(), "set", "remote.url", "https://example.com/wrong"); err != nil {
		t.Fatalf("project set: %v", err)
	}
	if err := runCmd(t, Doctor()); err == nil {
		t.Error("expected non-nil error from Doctor when issues present")
	}
}

func TestDoctorFlagsMissingFoundationalFile(t *testing.T) {
	dir := t.TempDir()
	gitInit(t, dir, "https://github.com/Rivil/dross.git")
	chdir(t, dir)
	if err := runCmd(t, Init()); err != nil {
		t.Fatalf("init: %v", err)
	}

	// Simulate a botched recovery: project.toml went missing.
	if err := os.Remove(filepath.Join(dir, ".dross", "project.toml")); err != nil {
		t.Fatal(err)
	}

	var doctorOut string
	doctorErr := runCmdCapturing(t, &doctorOut, Doctor())
	if doctorErr == nil {
		t.Fatal("expected error when project.toml is missing")
	}
	if !strings.Contains(doctorOut, ".dross/project.toml") || !strings.Contains(doctorOut, "missing") {
		t.Errorf("output should call out the missing file:\n%s", doctorOut)
	}
	if !strings.Contains(doctorOut, "dross ship recover") {
		t.Errorf("output should hint at recovery remediation:\n%s", doctorOut)
	}
}

func TestDoctorFlagsPhaseCommitsOnMain(t *testing.T) {
	// Build a repo where main has a commit recorded as a phase task —
	// the legacy state we want users to migrate away from.
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

	// Make a phase commit *on main* — the legacy pattern.
	mustWrite(t, filepath.Join(dir, "src/leak.ts"), "x\n")
	mustGit(t, dir, "add", ".")
	mustGit(t, dir, "commit", "-q", "-m", "feat: leak")
	leakSHA := mustGit(t, dir, "rev-parse", "HEAD")

	// Record that commit in a phase's changes.json so doctor can match.
	phaseDir := filepath.Join(dir, ".dross", "phases", "01-leak")
	if err := os.MkdirAll(phaseDir, 0o755); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(phaseDir, "changes.json"),
		`{"phase":"01-leak","tasks":{"t1":{"commit":"`+leakSHA+`"}}}`)

	out := captureStdout(t, func() {
		_ = runCmd(t, Doctor())
	})
	if !strings.Contains(out, "phase commit") || !strings.Contains(out, "ahead of origin/main") {
		t.Errorf("output should flag phase commits on main:\n%s", out)
	}
	if !strings.Contains(out, leakSHA[:7]) {
		t.Errorf("output should name the leaked SHA prefix:\n%s", out)
	}
	if !strings.Contains(out, "git branch phase/<id>") {
		t.Errorf("output should suggest the branch+reset fix:\n%s", out)
	}
}

func TestDoctorFlagsMissingGitattributes(t *testing.T) {
	dir := t.TempDir()
	gitInit(t, dir, "https://github.com/Rivil/dross.git")
	chdir(t, dir)
	if err := runCmd(t, Init()); err != nil {
		t.Fatalf("init: %v", err)
	}

	// Simulate a legacy dross project: .gitattributes never had the
	// linguist-generated line added.
	if err := os.Remove(filepath.Join(dir, ".gitattributes")); err != nil {
		t.Fatal(err)
	}

	out := captureStdout(t, func() {
		_ = runCmd(t, Doctor())
	})
	if !strings.Contains(out, "not marked linguist-generated") {
		t.Errorf("expected linguist-generated warning, got:\n%s", out)
	}
	if !strings.Contains(out, drossGitattributesLine) {
		t.Errorf("output should include the line to add:\n%s", out)
	}
}

// runCmdCapturing runs cmd with args while capturing stdout into *out.
// Use when both the error and the output text matter to the assertion.
func runCmdCapturing(t *testing.T, out *string, cmd *cobra.Command, args ...string) error {
	t.Helper()
	var err error
	*out = captureStdout(t, func() {
		err = runCmd(t, cmd, args...)
	})
	return err
}
