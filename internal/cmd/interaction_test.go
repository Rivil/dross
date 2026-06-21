package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Rivil/dross/assets"
)

// TestInteractionShowEmitsPlaybook proves c-3's delivery half: `dross interaction
// show` is registered and prints the playbook with all four markers plus the
// accept/reword/drop sentinel. If the command is unregistered or prints empty,
// this fails.
func TestInteractionShowEmitsPlaybook(t *testing.T) {
	out := captureStdout(t, func() {
		c := Interaction()
		c.SetArgs([]string{"show"})
		if err := c.Execute(); err != nil {
			t.Fatalf("interaction show: %v", err)
		}
	})
	if strings.TrimSpace(out) == "" {
		t.Fatal("interaction show printed nothing")
	}
	low := strings.ToLower(out)
	for _, needle := range []string{"one decision per turn", "propose", "wall", "never paste the build artifact back"} {
		if !strings.Contains(low, needle) {
			t.Errorf("playbook marker missing from emitter output: %q", needle)
		}
	}
	for _, opt := range []string{"accept", "reword", "drop"} {
		if !strings.Contains(low, opt) {
			t.Errorf("accept/reword/drop sentinel missing from emitter output: %q", opt)
		}
	}
}

// TestInteractionCommandRegistered guards the reachability gap a prior plan review
// flagged: the command must be wired into the real root tree in cmd/dross/main.go.
// Defining Interaction() but never adding it to root.AddCommand leaves it
// unreachable — the emitter would compile but `dross interaction show` would 404.
func TestInteractionCommandRegistered(t *testing.T) {
	root := repoRootFromTest(t)
	b, err := os.ReadFile(filepath.Join(root, "cmd", "dross", "main.go"))
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	if !strings.Contains(string(b), "cmd.Interaction()") {
		t.Error("cmd.Interaction() is not registered in cmd/dross/main.go root.AddCommand — `dross interaction show` would be unreachable")
	}
}

// TestInteractionPlaybookSingleSource proves the emitter's content is the same
// file make install links, not a drifting copy. go:embed makes them identical at
// build time; this guards the embed directive against being repointed at a
// separate file.
func TestInteractionPlaybookSingleSource(t *testing.T) {
	root := repoRootFromTest(t)
	onDisk, err := os.ReadFile(filepath.Join(root, "assets", "prompts", "_interaction.md"))
	if err != nil {
		t.Fatalf("read _interaction.md: %v", err)
	}
	if assets.InteractionPlaybook != string(onDisk) {
		t.Error("embedded InteractionPlaybook diverges from assets/prompts/_interaction.md on disk — not single-source")
	}
}
