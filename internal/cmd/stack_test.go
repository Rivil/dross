package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/project"
)

func TestStackDetectNonGoExitsZeroUnsupported(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"name":"x"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	var err error
	out := captureStdout(t, func() {
		err = runCmd(t, Stack(), "detect", dir)
	})
	if err != nil {
		t.Fatalf("detect must exit 0 (advisory), got error: %v", err)
	}
	got := strings.TrimSpace(out)
	if got == "go" {
		t.Fatal("package.json-only path must not detect as go")
	}
	if got != "unsupported" {
		t.Fatalf("want 'unsupported', got %q", got)
	}
}

func TestStackShowUnknownIDErrors(t *testing.T) {
	err := runCmd(t, Stack(), "show", "nonexistent-stack-xyz")
	if err == nil {
		t.Fatal("show on an unknown id must error (non-zero exit)")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error should explain id-not-found, got: %v", err)
	}
}

func TestStackProfileFieldRoundTrips(t *testing.T) {
	path := filepath.Join(t.TempDir(), "project.toml")
	p := &project.Project{Stack: project.Stack{Languages: []string{"go"}, Profile: "go"}}
	if err := p.Save(path); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `profile = "go"`) {
		t.Errorf("encoded project.toml missing [stack].profile, got:\n%s", data)
	}

	got, err := project.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Stack.Profile != "go" {
		t.Errorf("Profile dropped on round-trip: got %q", got.Stack.Profile)
	}
}

func TestStackRejectsUnknownSubcommand(t *testing.T) {
	c := Stack()
	EnforceSubcommandKnown(c)

	// All five subcommands present.
	want := map[string]bool{"detect": false, "show": false, "list": false, "apply": false, "loadout": false}
	for _, sub := range c.Commands() {
		if _, ok := want[sub.Name()]; ok {
			want[sub.Name()] = true
		}
	}
	for name, present := range want {
		if !present {
			t.Errorf("subcommand %q missing from stack command tree", name)
		}
	}

	if err := runCmd(t, c, "bogus"); err == nil {
		t.Fatal("unknown subcommand 'bogus' must error")
	}
}

func TestStackLoadoutCommandRenders(t *testing.T) {
	var err error
	out := captureStdout(t, func() {
		err = runCmd(t, Stack(), "loadout", "go")
	})
	if err != nil {
		t.Fatalf("loadout go: %v", err)
	}
	if !strings.Contains(out, "## Stack loadout (go)") {
		t.Errorf("loadout command did not render the markdown block; got:\n%s", out)
	}
}

func TestStackListIncludesDocker(t *testing.T) {
	var err error
	out := captureStdout(t, func() {
		err = runCmd(t, Stack(), "list")
	})
	if err != nil {
		t.Fatalf("stack list: %v", err)
	}
	if !strings.Contains(out, "docker") {
		t.Errorf("`stack list` must surface the docker profile; got:\n%s", out)
	}
}

func TestStackShowDocker(t *testing.T) {
	var err error
	out := captureStdout(t, func() {
		err = runCmd(t, Stack(), "show", "docker")
	})
	if err != nil {
		t.Fatalf("`stack show docker` must succeed once docker.toml embeds, got: %v", err)
	}
	// The encoded profile must carry its marker patterns and the hadolint tool.
	for _, want := range []string{"file_patterns", "hadolint"} {
		if !strings.Contains(out, want) {
			t.Errorf("`stack show docker` output missing %q; got:\n%s", want, out)
		}
	}
}

func TestStackApplyResyncsRuntime(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	drossDir := filepath.Join(dir, ".dross")
	if err := os.MkdirAll(drossDir, 0o755); err != nil {
		t.Fatal(err)
	}
	stale := &project.Project{
		Stack:   project.Stack{Languages: []string{"go"}},
		Runtime: project.Runtime{Mode: "native", TestCommand: "STALE test", BuildCommand: "STALE build"},
	}
	if err := stale.Save(filepath.Join(drossDir, project.File)); err != nil {
		t.Fatal(err)
	}
	chdir(t, dir)

	if err := runCmd(t, Stack(), "apply"); err != nil {
		t.Fatalf("apply: %v", err)
	}
	p, err := project.Load(filepath.Join(drossDir, project.File))
	if err != nil {
		t.Fatal(err)
	}
	if p.Runtime.TestCommand != "go test -count=1 ./..." {
		t.Errorf("stale [runtime].test not overwritten: %q", p.Runtime.TestCommand)
	}
	if p.Stack.Profile != "go" {
		t.Errorf("[stack].profile = %q, want \"go\"", p.Stack.Profile)
	}
}

func TestStackApplyUnsupportedDoesNotFabricate(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# x"), 0o644); err != nil {
		t.Fatal(err)
	}
	drossDir := filepath.Join(dir, ".dross")
	if err := os.MkdirAll(drossDir, 0o755); err != nil {
		t.Fatal(err)
	}
	p0 := &project.Project{Runtime: project.Runtime{Mode: "native"}}
	if err := p0.Save(filepath.Join(drossDir, project.File)); err != nil {
		t.Fatal(err)
	}
	chdir(t, dir)

	if err := runCmd(t, Stack(), "apply"); err == nil {
		t.Fatal("apply on an unsupported stack must error, not fabricate commands")
	}
	p, err := project.Load(filepath.Join(drossDir, project.File))
	if err != nil {
		t.Fatal(err)
	}
	if p.Runtime.TestCommand != "" {
		t.Errorf("fabricated a command on an unsupported stack: %q", p.Runtime.TestCommand)
	}
}
