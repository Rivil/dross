package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// architecturePromptContent loads assets/prompts/architecture.md and normalises
// it (lowercased, backticks/emphasis stripped) so assertions test for an
// instruction's presence rather than its exact formatting.
func architecturePromptContent(t *testing.T) string {
	t.Helper()
	root := repoRootFromTest(t)
	b, err := os.ReadFile(filepath.Join(root, "assets", "prompts", "architecture.md"))
	if err != nil {
		t.Fatalf("read architecture.md: %v", err)
	}
	s := strings.ToLower(string(b))
	return strings.NewReplacer("`", "", "*", "", "_", "").Replace(s)
}

// TestArchitecturePromptRefreshMerge proves c-2: the backfill prompt no longer
// stops when ARCHITECTURE.md exists (the "First-creation only" guard is lifted)
// and instead instructs a heading-keyed in-place refresh-merge that never
// silently drops an entry the scan missed. Regressing either clause fails this.
// (r-01: gates the source prompt directly, independent of `make install`.)
func TestArchitecturePromptRefreshMerge(t *testing.T) {
	content := architecturePromptContent(t)

	// The first-creation-only stop guard must be gone.
	if strings.Contains(content, "first-creation only") {
		t.Error("architecture.md §0.3 must not retain the 'First-creation only' stop guard")
	}

	// The feature-keyed in-place refresh-merge — including the flag-don't-drop
	// instruction — must be present.
	for _, needle := range []string{
		"refresh-merge",
		"feature heading",
		"in place",
		"never silently drop",
		"flag",
	} {
		if !strings.Contains(content, needle) {
			t.Errorf("architecture.md must instruct the refresh-merge: missing %q", needle)
		}
	}
}
