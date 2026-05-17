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
