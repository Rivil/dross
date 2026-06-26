package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// specPromptContent loads assets/prompts/spec.md and normalises it (lowercased,
// backticks/emphasis stripped) so assertions test for the presence of an
// instruction rather than its exact formatting. (r-01: the prompt edit is only
// live after `make install`; this reads the assets/ source directly.)
func specPromptContent(t *testing.T) string {
	t.Helper()
	root := repoRootFromTest(t)
	b, err := os.ReadFile(filepath.Join(root, "assets", "prompts", "spec.md"))
	if err != nil {
		t.Fatalf("read spec.md: %v", err)
	}
	s := strings.ToLower(string(b))
	return strings.NewReplacer("`", "", "*", "", "_", "").Replace(s)
}

// TestSpecPromptRoutesFourDestinations proves c-2: §4 offers all four routing
// destinations for a deferred idea. Drop any phrase and this fails.
func TestSpecPromptRoutesFourDestinations(t *testing.T) {
	content := specPromptContent(t)
	for _, needle := range []string{
		"pull into the current phase",
		"park in the milestone backlog",
		"attach to a named future phase",
		"someday",
	} {
		if !strings.Contains(content, needle) {
			t.Errorf("spec.md §4 missing routing destination %q", needle)
		}
	}
}

// TestSpecPromptParkSequence proves c-3: the park branch chains `dross milestone
// add … phases` with the `dross deferred route … --target` stamp.
func TestSpecPromptParkSequence(t *testing.T) {
	content := specPromptContent(t)
	for _, needle := range []string{"dross milestone add", "dross deferred route", "--target"} {
		if !strings.Contains(content, needle) {
			t.Errorf("spec.md park branch missing %q", needle)
		}
	}
}

// TestSpecPromptSomedayOnlyExplicit proves the someday half of c-2: an item is
// left unrouted only on an explicit someday pick, never the silent default.
func TestSpecPromptSomedayOnlyExplicit(t *testing.T) {
	content := specPromptContent(t)
	if !strings.Contains(content, "explicit someday") {
		t.Error("spec.md must require an explicit someday pick to leave an item unrouted")
	}
}

// TestSpecPromptResurfaceSeed proves c-4: §1 seeds candidate criteria via the
// `dross deferred list --target … --json` CLI lookup, not a prompt-side grep.
func TestSpecPromptResurfaceSeed(t *testing.T) {
	content := specPromptContent(t)
	for _, needle := range []string{"dross deferred list --target", "--json"} {
		if !strings.Contains(content, needle) {
			t.Errorf("spec.md re-surface seed missing %q", needle)
		}
	}
}

// TestSpecPromptSkipAlreadyRouted proves c-5: re-running skips items that
// already carry a target, so there's no duplicate routing.
func TestSpecPromptSkipAlreadyRouted(t *testing.T) {
	content := specPromptContent(t)
	if !strings.Contains(content, "already has a target") {
		t.Error("spec.md must skip deferred items that already have a target (dedup)")
	}
}
