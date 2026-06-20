package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// qualityPromptContent loads assets/prompts/quality.md (the audit orchestration
// prompt) and normalises it — lowercased, with markdown emphasis and backticks
// stripped — so the assertions below test the *presence of a rule*, not its exact
// formatting.
func qualityPromptContent(t *testing.T) string {
	t.Helper()
	root := repoRootFromTest(t)
	b, err := os.ReadFile(filepath.Join(root, "assets", "prompts", "quality.md"))
	if err != nil {
		t.Fatalf("read quality.md: %v", err)
	}
	s := strings.ToLower(string(b))
	s = strings.NewReplacer("`", "", "*", "", "_", "").Replace(s)
	// Collapse all whitespace (incl. line wraps) to single spaces so a mandated
	// phrase is found regardless of where the prose happens to wrap.
	return strings.Join(strings.Fields(s), " ")
}

// TestQualityPromptMandatedSections content-gates the locked rules quality.md must
// carry (t-7 authors them; this is where they're asserted). Each rule is an
// individually-failing sub-assertion, so removing any one mandated section from
// quality.md fails exactly that sub-test rather than a single coarse pass/fail.
func TestQualityPromptMandatedSections(t *testing.T) {
	content := qualityPromptContent(t)
	cases := []struct {
		name    string
		needles []string
	}{
		{"c-2 refute-panel majority-vote drop", []string{"refute", "majority vote", "drop"}},
		{"context calibrate-only downrank never-suppress + code-only sweep",
			[]string{"calibrate", "downrank", "never suppress", "tool sweep reads no .dross"}},
		{"c-3 detect-gate-sweep + tool-coverage manifest gate",
			[]string{"tool-coverage manifest", "proceed with partial coverage", "install the missing tools first"}},
		{"c-4 propose-then-ask before locking the scaffold", []string{"propose-then-ask before locking"}},
		{"c-5 read-only, no --fix, never edits app code", []string{"read-only", "no --fix", "never edit"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, n := range tc.needles {
				if !strings.Contains(content, n) {
					t.Errorf("quality.md is missing the required phrase %q for %s", n, tc.name)
				}
			}
		})
	}
}
