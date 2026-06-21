package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// interactionIncludeLine is the literal @-include the spec.md pilot uses to pull
// in the shared playbook. Phases 11-13 repeat this exact line for every other
// interactive prompt, so it is asserted verbatim here.
const interactionIncludeLine = "@~/.claude/dross/prompts/_interaction.md"

// TestSpecPilotIncludesSnippet proves c-3's mechanical half: spec.md carries the
// literal @-include line, and the path it points at resolves (through the
// installed prompts symlink) to a readable file. The live two-level expansion —
// whether the text actually reaches the model — is the irreducibly manual half,
// recorded in docs/interaction-audit.md.
func TestSpecPilotIncludesSnippet(t *testing.T) {
	root := repoRootFromTest(t)
	b, err := os.ReadFile(filepath.Join(root, "assets", "prompts", "spec.md"))
	if err != nil {
		t.Fatalf("read spec.md: %v", err)
	}
	if !strings.Contains(string(b), interactionIncludeLine) {
		t.Fatalf("spec.md is missing the pilot @-include line %q", interactionIncludeLine)
	}

	// Resolve the ~-path against the installed symlink. If dross isn't installed
	// in this environment, skip rather than fail — the line presence above is the
	// repo-level invariant; resolution depends on `make install` having run.
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("no home dir to resolve install path: %v", err)
	}
	installed := filepath.Join(home, ".claude", "dross", "prompts", "_interaction.md")
	if _, err := os.Stat(installed); err != nil {
		t.Skipf("snippet not installed at %s (run `make install`): %v", installed, err)
	}
	if _, err := os.ReadFile(installed); err != nil {
		t.Errorf("installed snippet at %s is not readable: %v", installed, err)
	}
}
