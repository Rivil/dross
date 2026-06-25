package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// executePromptContent loads assets/prompts/execute.md and normalises it
// (lowercased, backticks/emphasis stripped) so assertions test for the presence
// of an instruction rather than its exact formatting.
func executePromptContent(t *testing.T) string {
	t.Helper()
	root := repoRootFromTest(t)
	b, err := os.ReadFile(filepath.Join(root, "assets", "prompts", "execute.md"))
	if err != nil {
		t.Fatalf("read execute.md: %v", err)
	}
	s := strings.ToLower(string(b))
	return strings.NewReplacer("`", "", "*", "", "_", "").Replace(s)
}

// TestExecutePromptWiresPhaseNumber proves pc-3's "the version patch digit uses
// DisplayNumber" clause: the execute orchestration sets the patch digit from
// `dross phase number` rather than counting by hand. Removing that wiring fails
// this. (r-01: the prompt edit is only live after `make install`.)
func TestExecutePromptWiresPhaseNumber(t *testing.T) {
	content := executePromptContent(t)
	if !strings.Contains(content, "dross phase number") {
		t.Error("execute.md must derive the version patch digit from `dross phase number`")
	}
}

// TestExecutePromptInvokesLoadout proves c-4 end-to-end: the execute orchestration
// must call `dross stack loadout` and inject the block. If the invocation is
// removed from execute.md, this fails.
func TestExecutePromptInvokesLoadout(t *testing.T) {
	content := executePromptContent(t)
	for _, needle := range []string{"dross stack loadout", "inject"} {
		if !strings.Contains(content, needle) {
			t.Errorf("execute.md must wire the stack loadout: missing %q", needle)
		}
	}
}
