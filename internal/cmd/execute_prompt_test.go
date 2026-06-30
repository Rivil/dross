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

// TestExecutePromptEmitsTypedLandmark proves c-1's producer side: execute.md
// records the landmark through the typed `--landmark feature=…, symbol=…, loc=…,
// what=…` flag and no longer through the legacy `--notes "feature: …"` form. If
// the prompt regresses to the notes-string landmark, the forbidden token returns
// and this fails. (r-01: gates the source prompt directly, independent of install.)
func TestExecutePromptEmitsTypedLandmark(t *testing.T) {
	content := executePromptContent(t)
	for _, needle := range []string{"--landmark", "feature=", "symbol=", "loc=", "what="} {
		if !strings.Contains(content, needle) {
			t.Errorf("execute.md must emit the typed landmark: missing %q", needle)
		}
	}
	if strings.Contains(content, `--notes "feature:`) {
		t.Error("execute.md must not encode the landmark in --notes (legacy `--notes \"feature: …\"` form survived)")
	}
}
