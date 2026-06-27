package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// shipPromptContent loads assets/prompts/ship.md and normalises it —
// lowercased, with markdown emphasis and backticks stripped — so assertions
// test the presence of a rule, not its exact formatting.
func shipPromptContent(t *testing.T) string {
	t.Helper()
	root := repoRootFromTest(t)
	b, err := os.ReadFile(filepath.Join(root, "assets", "prompts", "ship.md"))
	if err != nil {
		t.Fatalf("read ship.md: %v", err)
	}
	s := strings.ToLower(string(b))
	return strings.NewReplacer("`", "", "*", "", "_", "").Replace(s)
}

// TestShipPromptRecoverySection (c-5) gates the recovery cookbook: all three
// mid-merge failure states and both recovery commands must be present, and the
// section must never instruct manual .dross/ surgery (the drift the cookbook
// exists to prevent).
func TestShipPromptRecoverySection(t *testing.T) {
	content := shipPromptContent(t)

	// Required: the three failure-state phrases + both recovery commands.
	for _, n := range []string{
		"fast-forward",
		"diverged",
		"dirty tree",
		"dross phase complete --recover",
		"dross ship recover",
	} {
		if !strings.Contains(content, n) {
			t.Errorf("ship.md recovery section missing required phrase %q", n)
		}
	}

	// Forbidden: manual .dross/ surgery presented as a user step. The whole
	// point is that a dross command owns the restore — reintroducing these
	// must fail the gate.
	for _, n := range []string{
		"git add .dross",
		"-- .dross/",
	} {
		if strings.Contains(content, n) {
			t.Errorf("ship.md must not instruct manual .dross/ surgery (found %q)", n)
		}
	}
}
