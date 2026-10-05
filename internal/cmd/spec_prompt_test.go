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

// TestSpecPromptIncludeFirstEitherOr proves c-1's framing half as revised on
// 2026-09-29: a surfaced candidate is sorted include-first — an in-scope one is
// pulled in with no defer question, a borderline one leads with "add to current
// phase", and only one with a clear home elsewhere leads with "defer it". Drop
// the framing and the needles disappear; restore the defer-first lead and the
// absence check fails.
func TestSpecPromptIncludeFirstEitherOr(t *testing.T) {
	content := specPromptContent(t)
	for _, needle := range []string{
		"include-first",                  // the rule's name
		"belongs in this phase",          // case 1: pulled in, no defer question
		"lead with add to current phase", // case 2: borderline leads with add
		"clearly has a home elsewhere",   // case 3: the only defer lead
		"defer it",                       // the alternative, spelled out
	} {
		if !strings.Contains(content, needle) {
			t.Errorf("spec.md §4a missing include-first framing %q", needle)
		}
	}
	if strings.Contains(content, "defer-first") {
		t.Error("spec.md still carries the retired defer-first lead")
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

// TestSpecPromptNoFreeRecall proves the deletion half of the slate rework: the
// §2 free-recall "list 3-7 outcomes" ask is gone. Restore the phrase and this
// fails.
func TestSpecPromptNoFreeRecall(t *testing.T) {
	content := specPromptContent(t)
	if strings.Contains(content, "list 3-7") {
		t.Error("spec.md §2 must not free-ask 'list 3-7' outcomes — the candidate slate replaced it")
	}
}

// TestSpecPromptCandidateSlate proves the slate half: §2 opens with a proposed
// candidate slate derived from milestone scope + gap analysis, gated per item
// with accept/reword/drop, and keeps the trailing catch-all.
func TestSpecPromptCandidateSlate(t *testing.T) {
	content := specPromptContent(t)
	for _, needle := range []string{
		"candidate slate",        // propose/candidate framing
		"milestone scope",        // derivation: milestone success criteria + position
		"gap analysis",           // derivation: CLI/context delta
		"accept / reword / drop", // per-item gate
		"one per turn",           // per-item, not batched
		"anything missing",       // retained catch-all
	} {
		if !strings.Contains(content, needle) {
			t.Errorf("spec.md §2 candidate-slate framing missing %q", needle)
		}
	}
}

// TestSpecPromptQualityBar pins the retained §2 quality bar — the
// testable/measurable pushback survives the slate rework.
func TestSpecPromptQualityBar(t *testing.T) {
	content := specPromptContent(t)
	for _, needle := range []string{
		"push back",
		"not testable",
		"measurable",
	} {
		if !strings.Contains(content, needle) {
			t.Errorf("spec.md §2 quality bar missing %q", needle)
		}
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

// TestSpecPromptRecordsAbsorbedItems: once spec.toml is written, /dross-spec
// records every criterion it seeded from a parked item with `dross deferred
// absorb`, naming the phase explicitly — current_phase is only set in §6.
func TestSpecPromptRecordsAbsorbedItems(t *testing.T) {
	content := specPromptContent(t)
	section5 := strings.Index(content, "## 5. write spec.toml")
	absorb := strings.Index(content, "dross deferred absorb")
	if section5 < 0 {
		t.Fatal("spec.md has no '## 5. Write spec.toml' heading")
	}
	if absorb < 0 {
		t.Fatal("spec.md never instructs dross deferred absorb for criteria seeded from parked items")
	}
	if absorb < section5 {
		t.Fatal("spec.md places dross deferred absorb before spec.toml is written (§5)")
	}
	line := content[absorb:]
	if i := strings.Index(line, "\n"); i >= 0 {
		line = line[:i]
	}
	if !strings.Contains(line, "--phase <phase-id>") {
		t.Errorf("the absorb instruction does not pass --phase <phase-id> explicitly: %q", line)
	}
	// A reworded parked item is still absorbed: only "accepted as written"
	// would leave its board card open forever.
	if !strings.Contains(content[section5:absorb], "seeded from a §1 parked item") || !strings.Contains(content[section5:absorb], "reworded") {
		t.Error("spec.md's absorb instruction does not cover a reworded parked item")
	}
}
