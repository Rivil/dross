package cmd

import (
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
)

// TestPlanPromptDescribesEveryReviewTier keeps the review ladder legible.
//
// /dross-plan has had three rungs for a while — no independent read
// (--no-review), one cold reviewer (the DEFAULT, no flag), and the three-lens
// panel (--panel) — but only two of them have flags, so the default one was
// easy to miss entirely. A backlog item asked for "a --panel-free lightweight
// second opinion: a single cold reviewer over the finished decomposition",
// which is an exact description of the rung that already ran on every plan.
// Something documented that badly is indistinguishable from something missing.
//
// The guard checks each rung's TRIGGER is described, not that the prose matches
// a fixed phrase — a guard pinned to wording fails on every rewording until
// someone deletes it, which is how guards die.
func TestPlanPromptDescribesEveryReviewTier(t *testing.T) {
	for _, p := range reviewTierProblems(t) {
		t.Error(p)
	}
}

// reviewTierProblems reads plan.md expanded with its partials — the --panel
// rung may live in _plan.panel.md — and reports each review rung it no longer
// describes.
func reviewTierProblems(t *testing.T) []string {
	t.Helper()
	doc := expandedPrompt(t, "plan.md")

	var out []string
	for _, rung := range []struct{ trigger, why string }{
		{"--no-review", "the opt-out rung — without it, skipping review looks impossible"},
		{"the default", "the default rung is the one with no flag, so only the prose can announce it"},
		{"--panel", "the decomposition rung — and the one users wrongly reach for to get review"},
	} {
		if !strings.Contains(doc, rung.trigger) {
			out = append(out, "plan.md no longer describes "+strconv.Quote(rung.trigger)+": "+rung.why)
		}
	}

	// The distinction the item got wrong: --panel drafts the plan three ways,
	// it is not the way to obtain a review. If the prompt stops saying so, the
	// same confusion returns.
	if !strings.Contains(doc, "decomposition") {
		out = append(out, "plan.md must say --panel is about decomposition, not critique — that confusion is what routed this phase here")
	}
	return out
}

// TestReviewTiersSeeThePanelPartial: with the --panel rung moved into
// _plan.panel.md, the tier check still finds it. The seeded base drops
// --no-review, so exactly that rung is reported — a read of the bare base would
// also miss --panel and decomposition, and a read off disk would miss nothing.
func TestReviewTiersSeeThePanelPartial(t *testing.T) {
	seedPromptFS(t, fstest.MapFS{
		"prompts/plan.md":        {Data: []byte("# plan\nReview is the default: one cold reviewer.\n**When** `panel=on` — the user asks for the panel: `dross prompt partial plan panel`\nend\n")},
		"prompts/_plan.panel.md": {Data: []byte("The --panel rung drafts the decomposition three ways.\n")},
	})
	got := reviewTierProblems(t)
	if len(got) != 1 || !strings.Contains(got[0], `"--no-review"`) {
		t.Errorf("reviewTierProblems over the seeded split = %q, want only the --no-review rung reported", got)
	}
}
