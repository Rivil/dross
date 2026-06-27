package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// resumePromptContent loads assets/prompts/resume.md and normalises it —
// lowercased, with markdown emphasis and backticks stripped — so assertions
// test the presence of a rule, not its exact formatting.
func resumePromptContent(t *testing.T) string {
	t.Helper()
	root := repoRootFromTest(t)
	b, err := os.ReadFile(filepath.Join(root, "assets", "prompts", "resume.md"))
	if err != nil {
		t.Fatalf("read resume.md: %v", err)
	}
	s := strings.ToLower(string(b))
	return strings.NewReplacer("`", "", "*", "", "_", "").Replace(s)
}

// TestResumePromptStaleStateSection (c-4) content-gates the stale-completion
// drift case resume.md must carry: the stale-completed phrase, a reconcile
// pointer (origin), and the never-auto-mutate caveat. Each is an
// individually-failing sub-assertion, so dropping the section fails exactly
// those needles.
func TestResumePromptStaleStateSection(t *testing.T) {
	content := resumePromptContent(t)
	cases := []struct {
		name    string
		needles []string
	}{
		{"stale-completed phrase", []string{"stale", "completed"}},
		{"reconcile pointer", []string{"reconcile", "origin"}},
		{"never-auto-mutate caveat", []string{"never auto-mutate"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, n := range tc.needles {
				if !strings.Contains(content, n) {
					t.Errorf("resume.md is missing the required phrase %q for %s", n, tc.name)
				}
			}
		})
	}
}
