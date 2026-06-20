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
