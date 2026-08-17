package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
	body, err := os.ReadFile(filepath.Join(repoRootFromTest(t), "assets", "prompts", "plan.md"))
	if err != nil {
		t.Fatalf("read plan.md: %v", err)
	}
	doc := string(body)

	for _, rung := range []struct{ trigger, why string }{
		{"--no-review", "the opt-out rung — without it, skipping review looks impossible"},
		{"the default", "the default rung is the one with no flag, so only the prose can announce it"},
		{"--panel", "the decomposition rung — and the one users wrongly reach for to get review"},
	} {
		if !strings.Contains(doc, rung.trigger) {
			t.Errorf("plan.md no longer describes %q: %s", rung.trigger, rung.why)
		}
	}

	// The distinction the item got wrong: --panel drafts the plan three ways,
	// it is not the way to obtain a review. If the prompt stops saying so, the
	// same confusion returns.
	if !strings.Contains(doc, "decomposition") {
		t.Error("plan.md must say --panel is about decomposition, not critique — that confusion is what routed this phase here")
	}
}
