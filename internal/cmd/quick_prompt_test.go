package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// quickPromptContent loads assets/prompts/quick.md and normalises it —
// lowercased, with backticks and asterisk emphasis stripped — so assertions
// test the presence of a rule, not its exact formatting. Unlike the ship.md
// helper this does NOT strip underscores: the needles here are store keys
// (quick_base) and stripping them would mangle the thing being asserted.
func quickPromptContent(t *testing.T) string {
	t.Helper()
	root := repoRootFromTest(t)
	b, err := os.ReadFile(filepath.Join(root, "assets", "prompts", "quick.md"))
	if err != nil {
		t.Fatalf("read quick.md: %v", err)
	}
	s := strings.ToLower(string(b))
	return strings.NewReplacer("`", "", "*", "").Replace(s)
}

// TestQuickPromptRecordsStandaloneBase (c-7) gates the half of the quick base
// record that lives in the prompt: a standalone quick task must record the
// branch it committed to, and the in-phase arm must NOT — a phase's base is
// already owned by its phase-scoped changes.json, and writing the machine-local
// quick_base there too would leave two records to disagree.
//
// The arms are split on the same line in quick.md's pre-flight step 4, so the
// needle check is per-line rather than over the whole document.
func TestQuickPromptRecordsStandaloneBase(t *testing.T) {
	content := quickPromptContent(t)

	const needle = "local set quick_base"

	if !strings.Contains(content, needle) {
		t.Fatalf("quick.md must tell a standalone quick task to record its base (%q)", needle)
	}

	// Scope to the pre-flight section — "standalone" and "in-phase" recur
	// later in the prompt, and only step 4's arms are the subject here.
	preflight, _, ok := strings.Cut(content, "\n## 1.")
	if !ok {
		t.Fatal("quick.md no longer has a section after pre-flight to cut at")
	}

	var standalone, inPhase string
	for _, line := range strings.Split(preflight, "\n") {
		switch {
		// Prefix match, not the whole parenthetical: both arms carry
		// qualifiers now (the in-phase arm excludes a shipped phase, the
		// standalone arm absorbs it) and those will keep evolving.
		case strings.Contains(line, "standalone (no current_phase"):
			standalone = line
		case strings.Contains(line, "in-phase (current_phase set"):
			inPhase = line
		}
	}
	if standalone == "" || inPhase == "" {
		t.Fatalf("quick.md pre-flight step 4 no longer has both branch arms (standalone=%t in-phase=%t)",
			standalone != "", inPhase != "")
	}
	if !strings.Contains(standalone, needle) {
		t.Errorf("the standalone arm must record the base: %q", standalone)
	}
	if strings.Contains(inPhase, needle) {
		t.Errorf("the in-phase arm must NOT record quick_base — changes.json owns it: %q", inPhase)
	}
}
