package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// setupPrompts are the seven setup/config interactive prompts retrofitted in
// phase 12. They mirror the core-loop retrofit (phase 11) one tier out from the
// spec→plan→execute→verify→ship pipeline: project bootstrap, adoption, settings,
// rules, board triage, one-shot tasks, and milestone scoping.
var setupPrompts = []string{"init", "onboard", "options", "rule", "inbox", "quick", "milestone"}

// TestSetupPromptsWireEmitter proves c-1: each setup/config prompt invokes
// `dross interaction show` in its pre-flight and carries no dead nested
// @-include line. Twin of TestCoreLoopPromptsWireEmitter — if a prompt drops the
// emitter call, the live command stops delivering the playbook.
func TestSetupPromptsWireEmitter(t *testing.T) {
	root := repoRootFromTest(t)
	for _, name := range setupPrompts {
		path := filepath.Join(root, "assets", "prompts", name+".md")
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s.md: %v", name, err)
		}
		body := string(b)
		if !strings.Contains(body, "dross interaction show") {
			t.Errorf("%s.md pre-flight must invoke `dross interaction show` to deliver the interaction playbook (c-1)", name)
		}
		if strings.Contains(body, deadIncludeLine) {
			t.Errorf("%s.md still carries the dead nested @-include line %q — delivery is via `dross interaction show`", name, deadIncludeLine)
		}
	}
}

// TestSetupPromptsReferenceContract proves c-2: each setup/config prompt
// references the interaction contract in prose, so the binding interaction style
// is visible to a reader, not just wired in pre-flight. Twin of
// TestCoreLoopPromptsReferenceContract; reuses interactionRefPhrase.
func TestSetupPromptsReferenceContract(t *testing.T) {
	root := repoRootFromTest(t)
	for _, name := range setupPrompts {
		path := filepath.Join(root, "assets", "prompts", name+".md")
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s.md: %v", name, err)
		}
		if !strings.Contains(string(b), interactionRefPhrase) {
			t.Errorf("%s.md must reference the contract (%q) so the interaction style is visible (c-2)", name, interactionRefPhrase)
		}
	}
}
