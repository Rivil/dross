package cmd

import (
	"strings"
	"testing"
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
