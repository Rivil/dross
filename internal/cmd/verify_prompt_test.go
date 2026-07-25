package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestVerifyPromptOffloadGuidance pins verify.md §2's offload passage
// (subagent-offload-audit c-2/c-4): the criterion-mapping step must
// carry size-gated read-only offload guidance, and the passage must
// keep both halves of the contract — the size gate (offload_posture
// lock: conditional, never mandatory) and the agent-gate boundary
// (judgement/cross-check/verdict stay in the main loop; fan-out agents
// never fill in verify.toml).
func TestVerifyPromptOffloadGuidance(t *testing.T) {
	b, err := os.ReadFile(filepath.Join(repoRootFromTest(t), "assets", "prompts", "verify.md"))
	if err != nil {
		t.Fatal(err)
	}
	prompt := string(b)

	for _, phrase := range []string{
		// the guidance itself
		"Offload the reading when the surface is large",
		"read-only subagents",
		// the size gate — conditional, never mandatory (offload_posture)
		"For a small phase, stay inline",
		// the agent-gate boundary: authority never moves
		"never fill in verify.toml or decide the verdict",
		// the audit doc is the disposition's home
		"docs/subagent-offload-audit.md",
	} {
		if !strings.Contains(prompt, phrase) {
			t.Errorf("verify.md lost its offload guidance phrase %q", phrase)
		}
	}
}
