package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestEnvListEmpty(t *testing.T) {
	chdir(t, t.TempDir())
	t.Setenv("HOME", t.TempDir())

	out := captureStdout(t, func() {
		_ = runCmd(t, Env(), "list")
	})
	if !strings.Contains(out, "(no env keys)") {
		t.Errorf("expected empty marker, got:\n%s", out)
	}
}

func TestEnvListMasksValues(t *testing.T) {
	chdir(t, t.TempDir())
	home := t.TempDir()
	t.Setenv("HOME", home)

	settings := map[string]any{
		"env": map[string]any{
			"FORGEJO_TOKEN": "secret-token-value-12345",
			"GITHUB_TOKEN":  "another-secret",
		},
		"model": "claude-opus-4-7",
	}
	settingsFile := filepath.Join(home, ".claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(settingsFile), 0o755); err != nil {
		t.Fatal(err)
	}
	b, _ := json.MarshalIndent(settings, "", "  ")
	if err := os.WriteFile(settingsFile, b, 0o600); err != nil {
		t.Fatal(err)
	}

	out := captureStdout(t, func() {
		_ = runCmd(t, Env(), "list")
	})
	// Keys should appear; values must NOT.
	for _, want := range []string{"FORGEJO_TOKEN", "GITHUB_TOKEN", "set (length 24)", "set (length 14)"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in output, got:\n%s", want, out)
		}
	}
	for _, leak := range []string{"secret-token-value", "another-secret"} {
		if strings.Contains(out, leak) {
			t.Errorf("secret value leaked in output: %s", out)
		}
	}
}

func TestEnvListSurfacesProjectAuthEnv(t *testing.T) {
	dir := t.TempDir()
	gitInit(t, dir, "https://forge.example/me/p.git")
	chdir(t, dir)
	t.Setenv("HOME", t.TempDir())

	if err := runCmd(t, Init()); err != nil {
		t.Fatalf("init: %v", err)
	}
	if err := runCmd(t, Project(), "set", "remote.auth_env", "FORGEJO_TOKEN"); err != nil {
		t.Fatalf("project set: %v", err)
	}

	out := captureStdout(t, func() {
		_ = runCmd(t, Env(), "list")
	})
	if !strings.Contains(out, "FORGEJO_TOKEN") || !strings.Contains(out, "NOT SET") {
		t.Errorf("expected FORGEJO_TOKEN flagged as NOT SET, got:\n%s", out)
	}
}

func TestEnvUnsetRemovesKey(t *testing.T) {
	chdir(t, t.TempDir())
	home := t.TempDir()
	t.Setenv("HOME", home)

	settingsFile := filepath.Join(home, ".claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(settingsFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(settingsFile, []byte(`{"env":{"FOO":"bar","KEEP":"baz"},"model":"x"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := runCmd(t, Env(), "unset", "FOO"); err != nil {
		t.Fatalf("unset: %v", err)
	}
	body, _ := os.ReadFile(settingsFile)
	var got map[string]any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("parse: %v", err)
	}
	envMap := got["env"].(map[string]any)
	if _, ok := envMap["FOO"]; ok {
		t.Error("FOO should have been removed")
	}
	if _, ok := envMap["KEEP"]; !ok {
		t.Error("KEEP should be preserved")
	}
	if got["model"] != "x" {
		t.Error("non-env keys should be preserved across mutate")
	}
}

func TestMutateSettingsCreatesFile(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, ".claude", "settings.json")

	err := mutateSettings(path, func(doc map[string]any) {
		envMap := map[string]any{"FOO": "bar"}
		doc["env"] = envMap
	})
	if err != nil {
		t.Fatalf("mutate: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("expected 0o600 perms (token storage), got %v", info.Mode().Perm())
	}

	doc, err := readSettings(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	envMap := doc["env"].(map[string]any)
	if !reflect.DeepEqual(envMap, map[string]any{"FOO": "bar"}) {
		t.Errorf("env mismatch: %+v", envMap)
	}
}

func TestMutateSettingsPreservesUnrelatedKeys(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, ".claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	original := `{"env":{"A":"1"},"model":"m","theme":"dark"}`
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := mutateSettings(path, func(doc map[string]any) {
		envMap := doc["env"].(map[string]any)
		envMap["B"] = "2"
	}); err != nil {
		t.Fatalf("mutate: %v", err)
	}

	doc, _ := readSettings(path)
	if doc["model"] != "m" || doc["theme"] != "dark" {
		t.Errorf("non-env keys lost: %+v", doc)
	}
	envMap := doc["env"].(map[string]any)
	if envMap["A"] != "1" || envMap["B"] != "2" {
		t.Errorf("env keys lost: %+v", envMap)
	}
}
