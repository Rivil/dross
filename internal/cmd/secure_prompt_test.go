package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// securePromptContent loads assets/prompts/secure.md (the audit orchestration
// prompt) and normalises it — lowercased, with markdown emphasis and backticks
// stripped — so the assertions below test the *presence of a rule*, not its exact
// formatting.
func securePromptContent(t *testing.T) string {
	t.Helper()
	root := repoRootFromTest(t)
	b, err := os.ReadFile(filepath.Join(root, "assets", "prompts", "secure.md"))
	if err != nil {
		t.Fatalf("read secure.md: %v", err)
	}
	s := strings.ToLower(string(b))
	return strings.NewReplacer("`", "", "*", "", "_", "").Replace(s)
}

// TestSecurePromptMandatedSections content-gates the four rules secure.md must
// carry (t-7 authors them; this is where they're asserted). Each criterion is an
// individually-failing sub-assertion, so removing any one mandated section from
// secure.md fails exactly that sub-test rather than a single coarse pass/fail.
func TestSecurePromptMandatedSections(t *testing.T) {
	content := securePromptContent(t)
	cases := []struct {
		name    string
		needles []string
	}{
		{"c-2 refute-panel majority-vote drop", []string{"refute", "majority vote", "drop"}},
		{"c-3 context-free, reads no .dross planning artifacts", []string{"context-free", "no .dross/ planning artifacts"}},
		{"c-6 read-only, no --fix, never edits app code", []string{"no --fix", "never edit"}},
		{"c-5 propose-then-ask before locking the scaffold", []string{"propose-then-ask before locking"}},
		{"c-1/c-3 post-scan reconcile against prior state", []string{"dross security findings reconcile"}},
		{"private-code semgrep no-egress guidance", []string{"metrics=off", "config auto", "semgrep.dev", "private"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, n := range tc.needles {
				if !strings.Contains(content, n) {
					t.Errorf("secure.md is missing the required phrase %q for %s", n, tc.name)
				}
			}
		})
	}
}
