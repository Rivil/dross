package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultsSaveExtractsFromProject(t *testing.T) {
	dir := t.TempDir()
	gitInit(t, dir, "https://forge.example/me/p.git")
	chdir(t, dir)

	home := t.TempDir()
	t.Setenv("HOME", home)

	if err := runCmd(t, Init()); err != nil {
		t.Fatalf("init: %v", err)
	}

	// Configure a remote on the project.
	for _, set := range [][]string{
		{"set", "remote.provider", "forgejo"},
		{"set", "remote.api_base", "https://forge.example/api/v1"},
		{"set", "remote.log_api", "true"},
		{"set", "remote.auth_env", "FORGEJO_TOKEN"},
		{"set", "remote.reviewers", "alice,bob"},
	} {
		if err := runCmd(t, Project(), set...); err != nil {
			t.Fatalf("project %v: %v", set, err)
		}
	}

	if err := runCmd(t, Defaults(), "save"); err != nil {
		t.Fatalf("defaults save: %v", err)
	}

	body, err := os.ReadFile(filepath.Join(home, ".claude", "dross", "defaults.toml"))
	if err != nil {
		t.Fatalf("defaults.toml not written: %v", err)
	}
	got := string(body)
	for _, want := range []string{
		`provider = "forgejo"`,
		`api_base = "https://forge.example/api/v1"`,
		`log_api = true`,
		`auth_env = "FORGEJO_TOKEN"`,
		`reviewers = ["alice", "bob"]`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("defaults.toml missing %q\n--- body ---\n%s", want, got)
		}
	}
	// URL must NOT be copied — it's project-specific.
	if strings.Contains(got, "url =") {
		t.Errorf("defaults.toml should not contain url:\n%s", got)
	}
}

func TestDefaultsShowOnEmpty(t *testing.T) {
	chdir(t, t.TempDir())
	t.Setenv("HOME", t.TempDir())

	out := captureStdout(t, func() {
		_ = runCmd(t, Defaults(), "show")
	})
	// Empty file is acceptable — show should print path comment + empty toml.
	if !strings.Contains(out, "defaults.toml") {
		t.Errorf("expected path comment in show output, got:\n%s", out)
	}
}
