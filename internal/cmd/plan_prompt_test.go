package cmd

import (
	"strings"
	"testing"

	"github.com/Rivil/dross/assets"
)

// planPromptContent loads plan.md expanded with its partials and normalises it
// (lowercased, backticks/emphasis stripped) so assertions test for the presence
// of an instruction rather than its exact formatting. (r-01: the prompt edit is
// only live after `make install`; this reads the embedded assets directly.)
func planPromptContent(t *testing.T) string {
	t.Helper()
	return normalisePrompt(expandedPrompt(t, "plan.md"))
}

// TestPlanPromptBorderlineTaskIncludeFirst proves c-2 as revised on 2026-09-29:
// a borderline/optional task proposed during §3 steering is surfaced through
// the playbook's include-first either/or (lead "add as a task", offer "defer
// it"), not slipped in silently — and a task that serves a criterion is not
// asked about at all. Drop the framing and these needles disappear; restore the
// defer-first lead and the absence check fails.
func TestPlanPromptBorderlineTaskIncludeFirst(t *testing.T) {
	content := planPromptContent(t)
	for _, needle := range []string{
		"borderline",                 // the trigger
		"include-first",              // the rule's name
		`leads with "add as a task"`, // add leads
		"defer it",                   // the alternative
	} {
		if !strings.Contains(content, needle) {
			t.Errorf("plan.md §3 missing borderline-task include-first framing %q", needle)
		}
	}
	if strings.Contains(content, "defer-first") {
		t.Error("plan.md still carries the retired defer-first lead")
	}
}

// TestPlanPromptCoverageGapEitherOr proves c-4: the coverage-gap check is an
// explicit either/or (add a covering task vs defer the criterion), not the old
// inline "either add a task … or move … to deferred" instruction. Collapse it
// back to prose and the needles disappear.
func TestPlanPromptCoverageGapEitherOr(t *testing.T) {
	content := planPromptContent(t)
	for _, needle := range []string{
		"add a covering task", // one arm
		"defer the criterion", // the other arm
	} {
		if !strings.Contains(content, needle) {
			t.Errorf("plan.md §4 coverage-gap either/or missing %q", needle)
		}
	}
}

// TestPlanPanelFetchPoint proves t-10's split: panel mode loads only under
// --panel, its fetch line sits where §2P was (the last line before ## 2G), the
// expansion keeps heading order 2 -> 2P -> 2G, and base plan.md no longer
// cites a §2P that only the partial carries.
func TestPlanPanelFetchPoint(t *testing.T) {
	common, err := assets.Fetched(promptFS, "plan", assets.CommonPath())
	if err != nil {
		t.Fatal(err)
	}
	facts := assets.CommonPath()
	facts["panel"] = "on"
	panel, err := assets.Fetched(promptFS, "plan", facts)
	if err != nil {
		t.Fatal(err)
	}
	if len(common) != 0 || strings.Join(panel, ",") != "panel" {
		t.Errorf("plan fetches %v on the common path and %v under panel=on; want [] and [panel]", common, panel)
	}

	raw, err := assets.FS.ReadFile("prompts/plan.md")
	if err != nil {
		t.Fatal(err)
	}
	base := string(raw)
	if strings.Contains(base, "§2P") {
		t.Error("base plan.md still cites §2P, which only _plan.panel.md carries")
	}
	g := strings.Index(base, "\n## 2G")
	if g < 0 {
		t.Fatal("plan.md has no ## 2G heading")
	}
	before := strings.Split(strings.TrimRight(base[:g], "\n"), "\n")
	if last := before[len(before)-1]; last != "**When** `panel=on` — `--panel` was passed: `dross prompt partial plan panel`" {
		t.Errorf("last line before ## 2G = %q; want the panel fetch line", last)
	}

	out, err := assets.Expand(promptFS, "plan")
	if err != nil {
		t.Fatal(err)
	}
	exp := string(out)
	h2, h2p, h2g := strings.Index(exp, "\n## 2. "), strings.Index(exp, "\n## 2P. "), strings.Index(exp, "\n## 2G. ")
	if h2 < 0 || h2p < 0 || h2g < 0 || !(h2 < h2p && h2p < h2g) {
		t.Errorf("expanded plan heading order: ## 2 at %d, ## 2P at %d, ## 2G at %d; want 2 < 2P < 2G", h2, h2p, h2g)
	}
}

// TestPlanPanelBriefSurvivesExpansion: the lens-planner brief lives only in
// _plan.panel.md, so the expanded plan prompt must still name every lens
// draft and the judge's synthesis — dropping one from the partial fails here.
func TestPlanPanelBriefSurvivesExpansion(t *testing.T) {
	content := planPromptContent(t)
	for _, needle := range []string{"panel/risk.md", "panel/mvp.md", "panel/verification.md", "panel/synthesis.md"} {
		if !strings.Contains(content, needle) {
			t.Errorf("expanded plan.md lost the panel brief's %q", needle)
		}
	}
}
