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

// TestSpecPromptDeferFirstEitherOr proves c-1's framing half: a surfaced
// borderline candidate is routed through the defer-first either/or from the
// playbook — lead "defer it", offer "add to current phase". Drop the framing and
// the needles disappear.
func TestSpecPromptDeferFirstEitherOr(t *testing.T) {
	content := specPromptContent(t)
	for _, needle := range []string{
		"defer-first",          // defer leads
		"add to current phase", // the alternative
		"defer it",             // the lead option, spelled out
	} {
		if !strings.Contains(content, needle) {
			t.Errorf("spec.md §4a missing defer-first framing %q", needle)
		}
	}
}

// TestSpecPromptTwoStepRouting proves c-1's routing-layering half: the entry gate
// (defer vs add) precedes destination routing, and the post-defer step does NOT
// re-offer the pull-in (the §4a double-offer reconciliation). Collapse the two
// steps or restore the duplicate pull-in and this fails.
func TestSpecPromptTwoStepRouting(t *testing.T) {
	content := specPromptContent(t)
	for _, needle := range []string{
		"two-step",          // the layered structure
		"entry gate",        // step 1
		"does not re-offer", // step 2 drops the duplicate pull-in
	} {
		if !strings.Contains(content, needle) {
			t.Errorf("spec.md §4a two-step routing missing %q", needle)
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

// TestSpecPromptNoGrayAreaPreselection proves c-1: §3 no longer asks the user
// which gray areas to discuss — the multiSelect pre-selection gate is gone.
func TestSpecPromptNoGrayAreaPreselection(t *testing.T) {
	content := specPromptContent(t)
	for _, banned := range []string{
		"which of these should we pin down",
		"present for selection",
	} {
		if strings.Contains(content, banned) {
			t.Errorf("spec.md §3 must not contain the pre-selection gate %q", banned)
		}
	}
}

// TestSpecPromptWalksEveryGrayArea proves c-2: §3 walks every gray area
// one-by-one, one decision per turn, with an explicit user off-ramp.
func TestSpecPromptWalksEveryGrayArea(t *testing.T) {
	content := specPromptContent(t)
	for _, needle := range []string{
		"walk every",
		"one at a time",
		"off-ramp",
	} {
		if !strings.Contains(content, needle) {
			t.Errorf("spec.md §3 walk-all instruction missing %q", needle)
		}
	}
}

// TestSpecPromptUncertaintyDiscriminator proves c-3: the discriminator is
// Claude's own uncertainty, and the "decide internals yourself" boundary is kept.
func TestSpecPromptUncertaintyDiscriminator(t *testing.T) {
	content := specPromptContent(t)
	for _, needle := range []string{
		"cannot confidently",    // uncertainty framing
		"genuinely uncertain",   // uncertainty framing
		"decide these yourself", // retained boundary
	} {
		if !strings.Contains(content, needle) {
			t.Errorf("spec.md §3 discriminator/boundary missing %q", needle)
		}
	}
}

// TestSpecPromptGrayAreaOrdering proves the count_ordering decision: a soft ~3–4
// guideline, ordered most-impactful first.
func TestSpecPromptGrayAreaOrdering(t *testing.T) {
	content := specPromptContent(t)
	for _, needle := range []string{
		"most-impactful",
		"3–4",
	} {
		if !strings.Contains(content, needle) {
			t.Errorf("spec.md §3 count/ordering guidance missing %q", needle)
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
