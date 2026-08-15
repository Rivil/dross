package cmd

import (
	"strings"
	"testing"
)

// planSection returns plan.md's §2G gray-area block, so every assertion below
// is about the section that owns the rule rather than about the prompt at
// large — a phrase found somewhere else in the file proves nothing about the
// walk.
func planGraySection(t *testing.T) string {
	t.Helper()
	body := promptBody(t, "plan.md")
	start := strings.Index(body, "## 2G.")
	if start < 0 {
		t.Fatal("plan.md has no §2G gray-area section — the planner's own uncertainty is presented as settled")
	}
	rest := body[start:]
	if end := strings.Index(rest, "\n## 3."); end > 0 {
		rest = rest[:end]
	}
	return rest
}

// TestPlanPromptWalksGrayAreas is c-1, including WHERE the walk sits.
//
// Position is not cosmetic: a walk placed after the proposal is a post-mortem —
// the user is reacting to a finished decomposition, which is the exact framing
// ("presented as settled") the criterion exists to fix.
func TestPlanPromptWalksGrayAreas(t *testing.T) {
	body := promptBody(t, "plan.md")
	gray := strings.Index(body, "## 2G.")
	decomp := strings.Index(body, "## 2. Goal-backward decomposition")
	propose := strings.Index(body, "## 3. Propose")

	if gray < 0 {
		t.Fatal("no gray-area section in plan.md")
	}
	if !(decomp < gray && gray < propose) {
		t.Errorf("the gray-area walk is not between decomposition and the proposal (decomp=%d gray=%d propose=%d) — after the proposal it is a post-mortem", decomp, gray, propose)
	}

	section := planGraySection(t)
	for _, want := range []string{"every", "one at a time", "AskUserQuestion"} {
		if !strings.Contains(strings.ToLower(section), strings.ToLower(want)) {
			t.Errorf("the walk does not say %q:\n%s", want, section)
		}
	}
}

// TestPlanWalkHasNoSelectionStep is c-2, and it targets the specific regression
// the criterion names rather than the idea in general.
func TestPlanWalkHasNoSelectionStep(t *testing.T) {
	section := planGraySection(t)
	low := strings.ToLower(section)

	if !strings.Contains(low, "no selection step") {
		t.Errorf("the walk does not forbid a selection step:\n%s", section)
	}
	// multiSelect is how a walk becomes a checklist, and a checklist with
	// anything pre-ticked is a pre-filtered list wearing a walk's clothes.
	if strings.Contains(low, "multiselect") && !strings.Contains(low, "do not") && !strings.Contains(low, "never") {
		t.Errorf("the walk mentions multiSelect without forbidding it:\n%s", section)
	}
	for _, want := range []string{"do not ask which areas", "pre-ticked"} {
		if !strings.Contains(low, want) {
			t.Errorf("the walk does not rule out %q:\n%s", want, section)
		}
	}
	// The off-ramp is the ONLY early exit, and taking it has to be said out
	// loud — a silent truncation is indistinguishable from never having walked.
	if !strings.Contains(low, "off-ramp") {
		t.Errorf("the walk has no user off-ramp:\n%s", section)
	}
	if !strings.Contains(low, "never self-truncate") {
		t.Errorf("the walk does not forbid self-truncation:\n%s", section)
	}
}

// TestPlanGrayAreasAreDecompositionScoped is c-3, and it requires BOTH halves
// of the boundary.
//
// With only the "what qualifies" half the walk drifts upward into spec's locked
// decisions; with only the "what does not" half it has no positive definition
// and drifts downward into executor detail. Either way the walk stops being
// about decomposition.
func TestPlanGrayAreasAreDecompositionScoped(t *testing.T) {
	low := strings.ToLower(planGraySection(t))

	for _, want := range []string{"task boundaries", "wave ordering"} {
		if !strings.Contains(low, want) {
			t.Errorf("the walk does not name %q as a qualifying area", want)
		}
	}
	if !strings.Contains(low, "locked") {
		t.Errorf("the walk does not rule out re-opening spec's locked decisions — that is re-litigating a settled question")
	}
	if !strings.Contains(low, "executor") {
		t.Errorf("the walk does not rule out what the executor resolves — asking stalls the plan on detail the user cannot answer yet")
	}
}

// TestPlanWalkMayBeEmpty is c-4. A prompt with no permitted empty answer
// manufactures areas, and invented questions are what teach a user to stop
// reading them.
func TestPlanWalkMayBeEmpty(t *testing.T) {
	low := strings.ToLower(planGraySection(t))
	if !strings.Contains(low, "no minimum") {
		t.Errorf("the walk does not say there is no minimum number of areas")
	}
	if !strings.Contains(low, "say so in one line") {
		t.Errorf("the walk does not tell the planner how to report having no areas — an unstated skip reads as a skipped step")
	}
}

// TestPlanAndSpecWalkAgree: the two commands are read minutes apart by the same
// person, and a second interaction shape for one job is a second thing to learn
// and a second thing to drift.
func TestPlanAndSpecWalkAgree(t *testing.T) {
	plan := strings.ToLower(planGraySection(t))
	spec := strings.ToLower(promptBody(t, "spec.md"))

	for _, rule := range []string{"no selection step", "off-ramp"} {
		if !strings.Contains(spec, rule) {
			t.Errorf("spec.md lost the %q rule — the parity assertion below would then pass by both being wrong", rule)
		}
		if !strings.Contains(plan, rule) {
			t.Errorf("plan.md's walk lost the %q rule that spec.md still carries", rule)
		}
	}
}

// TestPlanPromptGuardsReadTheShippedFile: a guard passing against a fixture
// while the shipped prompt regressed is worse than no guard — it reports health
// it is not measuring.
func TestPlanPromptGuardsReadTheShippedFile(t *testing.T) {
	body := promptBody(t, "plan.md")
	if !strings.Contains(body, "# /dross-plan") {
		t.Fatalf("promptBody did not return the shipped plan.md (first 80 chars: %.80q)", body)
	}
	if len(body) < 2000 {
		t.Errorf("plan.md is suspiciously short (%d bytes) — the guards may be reading the wrong file", len(body))
	}
}
