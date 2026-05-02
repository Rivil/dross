package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// chdir changes cwd for the duration of the test.
// Tests using chdir cannot run in parallel — cwd is process-global.
func chdir(t *testing.T, dir string) {
	t.Helper()
	prev, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(prev) })
}

// runCmd executes a cobra command tree with args, capturing stdout and stderr.
// Note: handlers in this package use fmt.Print* directly (the Print/Printf
// helpers in root.go), so we can't intercept the actual binary output via
// cmd.SetOut. Tests that need to assert on stdout should use os.Pipe wrapping
// — left for a follow-up. For now the tests below assert on filesystem effects
// and returned errors.
func runCmd(t *testing.T, cmd *cobra.Command, args ...string) error {
	t.Helper()
	cmd.SetArgs(args)
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetErr(new(bytes.Buffer))
	return cmd.Execute()
}

func TestInitCreatesArtefacts(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)

	if err := runCmd(t, Init()); err != nil {
		t.Fatalf("init: %v", err)
	}

	for _, want := range []string{
		".dross",
		".dross/project.toml",
		".dross/state.json",
		".dross/rules.toml",
		".dross/milestones",
		".dross/phases",
	} {
		if _, err := os.Stat(filepath.Join(dir, want)); err != nil {
			t.Errorf("missing %s: %v", want, err)
		}
	}
}

func TestInitRefusesIfDrossExists(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	if err := os.MkdirAll(filepath.Join(dir, ".dross"), 0o755); err != nil {
		t.Fatal(err)
	}
	err := runCmd(t, Init())
	if err == nil {
		t.Fatal("expected init to refuse with existing .dross/")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("error should mention existence: %v", err)
	}
}

func TestInitForceOverwrites(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	if err := os.MkdirAll(filepath.Join(dir, ".dross"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Drop a sentinel that --force should remove.
	sentinel := filepath.Join(dir, ".dross", "sentinel")
	if err := os.WriteFile(sentinel, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := runCmd(t, Init(), "--force"); err != nil {
		t.Fatalf("init --force: %v", err)
	}
	if _, err := os.Stat(sentinel); !os.IsNotExist(err) {
		t.Errorf("sentinel should have been wiped: err=%v", err)
	}
}

func TestOnboardScansSignals(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)

	// Drop signal files.
	mustWrite(t, filepath.Join(dir, "package.json"), `{"name":"x"}`)
	mustWrite(t, filepath.Join(dir, "pnpm-lock.yaml"), "")
	mustWrite(t, filepath.Join(dir, "tsconfig.json"), "{}")
	mustWrite(t, filepath.Join(dir, "Dockerfile"), "FROM node:22")
	mustWrite(t, filepath.Join(dir, "docker-compose.yml"), "services:\n  app: {}")

	if err := runCmd(t, Onboard()); err != nil {
		t.Fatalf("onboard: %v", err)
	}

	// project.toml should reflect the scan.
	body := mustRead(t, filepath.Join(dir, ".dross", "project.toml"))
	for _, want := range []string{
		`mode = "docker"`,
		`dev_command = "docker compose up"`,
		`package_manager = "pnpm"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("project.toml missing %q\n--- body ---\n%s", want, body)
		}
	}
}

func TestValidateFlagsIncompleteProject(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	if err := runCmd(t, Init()); err != nil {
		t.Fatalf("init: %v", err)
	}
	// Init leaves project.name empty — validate should flag it.
	err := runCmd(t, Validate())
	if err == nil {
		t.Fatal("validate should error on incomplete project.toml")
	}
	if !strings.Contains(err.Error(), "problem") {
		t.Errorf("error should mention problems: %v", err)
	}
}

func TestValidatePassesWhenComplete(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)

	if err := runCmd(t, Init()); err != nil {
		t.Fatal(err)
	}
	mustRunSet(t, "project.name", "test-app")
	mustRunSet(t, "runtime.mode", "docker")

	if err := runCmd(t, Validate()); err != nil {
		t.Fatalf("validate should pass: %v", err)
	}
}

func TestRuleAddListRemove(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	if err := runCmd(t, Init()); err != nil {
		t.Fatal(err)
	}

	if err := runCmd(t, Rule(), "add", "--scope", "project", "always docker"); err != nil {
		t.Fatalf("add: %v", err)
	}

	body := mustRead(t, filepath.Join(dir, ".dross", "rules.toml"))
	if !strings.Contains(body, "always docker") {
		t.Errorf("rules.toml missing rule:\n%s", body)
	}

	if err := runCmd(t, Rule(), "remove", "--scope", "project", "r-01"); err != nil {
		t.Fatalf("remove: %v", err)
	}
	body = mustRead(t, filepath.Join(dir, ".dross", "rules.toml"))
	if strings.Contains(body, "always docker") {
		t.Error("rule should be removed from file")
	}
}

func TestPhaseCreateAutoNumbers(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	if err := runCmd(t, Init()); err != nil {
		t.Fatal(err)
	}

	if err := runCmd(t, Phase(), "create", "auth middleware"); err != nil {
		t.Fatalf("create 1: %v", err)
	}
	if err := runCmd(t, Phase(), "create", "billing"); err != nil {
		t.Fatalf("create 2: %v", err)
	}

	for _, want := range []string{"01-auth-middleware", "02-billing"} {
		path := filepath.Join(dir, ".dross", "phases", want)
		if _, err := os.Stat(path); err != nil {
			t.Errorf("phase dir %s missing: %v", want, err)
		}
	}
}

func TestFindRootWalksUp(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	if err := runCmd(t, Init()); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(dir, "deep", "nested")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	chdir(t, sub)

	root, err := FindRoot()
	if err != nil {
		t.Fatalf("FindRoot: %v", err)
	}
	// Resolve symlinks for comparison — macOS /tmp is /private/tmp.
	wantRoot, _ := filepath.EvalSymlinks(filepath.Join(dir, ".dross"))
	gotRoot, _ := filepath.EvalSymlinks(root)
	if gotRoot != wantRoot {
		t.Errorf("FindRoot: got %q want %q", gotRoot, wantRoot)
	}
}

func TestFindRootErrorsWhenNoneFound(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	if _, err := FindRoot(); err == nil {
		t.Fatal("expected ErrNoRoot")
	}
}

// helpers

func mustWrite(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mustRead(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func mustRunSet(t *testing.T, field, value string) {
	t.Helper()
	if err := runCmd(t, Project(), "set", field, value); err != nil {
		t.Fatalf("project set %s=%s: %v", field, value, err)
	}
}
