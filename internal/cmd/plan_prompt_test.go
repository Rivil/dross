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

// TestPlanFactsFetchPoint proves c-4's placement: the spec's citations are
// checked in §1, so in expanded plan.md the facts run and the walk it fetches
// both land before "## 2. Goal-backward decomposition" — nothing is decomposed
// on an unchecked citation. The walk loads only when the run exits non-zero.
func TestPlanFactsFetchPoint(t *testing.T) {
	facts := assets.CommonPath()
	facts["facts"] = "unresolved"
	got, err := assets.Fetched(promptFS, "plan", facts)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(got, ",") != "facts" {
		t.Errorf("plan fetches %v under facts=unresolved; want [facts]", got)
	}

	exp := expandedPrompt(t, "plan.md")
	h1, h2 := strings.Index(exp, "\n## 1. "), strings.Index(exp, "\n## 2. Goal-backward decomposition")
	if h1 < 0 || h2 < 0 {
		t.Fatalf("expanded plan.md lacks ## 1 (%d) or ## 2 (%d)", h1, h2)
	}
	run := strings.Index(exp, "`dross phase facts <id> --json`")
	fetch := strings.Index(exp, "**When** `facts=unresolved` — `dross phase facts` exited non-zero: `dross prompt partial plan facts`")
	body, err := assets.Partial(promptFS, "plan", "facts")
	if err != nil {
		t.Fatal(err)
	}
	walk := strings.Index(exp, string(body))
	for name, at := range map[string]int{"facts run": run, "fetch line": fetch, "inlined walk": walk} {
		if at < h1 || at > h2 {
			t.Errorf("%s at %d; want it inside §1, between ## 1 at %d and ## 2 at %d", name, at, h1, h2)
		}
	}
	if !(run < fetch && fetch < walk) {
		t.Errorf("want facts run < fetch line < walk, got %d %d %d", run, fetch, walk)
	}
}

// TestPlanFactsWalk pins the partial's content: one citation per
// AskUserQuestion turn, both outcomes, mark-new for the new arm, `dross task
// edit` for plan-side fixes, and a re-run until exit 0 before §2. Drop any one
// and the walk stops being c-4's.
func TestPlanFactsWalk(t *testing.T) {
	body, err := assets.Partial(promptFS, "plan", "facts")
	if err != nil {
		t.Fatal(err)
	}
	raw := string(body)
	for _, needle := range []string{
		"AskUserQuestion",
		"one citation per turn",
		"**fix the citation**",
		"**confirm it is new**",
		"dross phase mark-new <id> <citation>",
		"dross task edit <id> t-N",
		"plan-side finding",
		"re-run `dross phase facts <id> --json`",
		"until it exits 0",
		"Before §2",
	} {
		if !strings.Contains(raw, needle) {
			t.Errorf("_plan.facts.md lost %q", needle)
		}
	}
	// The plan-side arm must route through the CLI, never a hand edit.
	if i, j := strings.Index(raw, "plan-side finding"), strings.Index(raw, "dross task edit"); i < 0 || j < i {
		t.Errorf("plan-side findings must be fixed with `dross task edit` (plan-side at %d, task edit at %d)", i, j)
	}
}

// TestPlanFactsRerunBeforeLock proves c-4's plan-side half: §5 re-runs `dross
// phase facts` after plan.toml is written and before the lock question, so
// §6's plan-lock never meets an unhandled refusal.
func TestPlanFactsRerunBeforeLock(t *testing.T) {
	section := promptSection(t, expandedPrompt(t, "plan.md"), "## 5. Write plan.toml")
	write := strings.Index(section, "Use the `Write` tool")
	rerun := strings.Index(section, "`dross phase facts <id>`")
	lock := strings.Index(section, "Plan written: N tasks across W waves — lock it?")
	if write < 0 || rerun < 0 || lock < 0 || !(write < rerun && rerun < lock) {
		t.Errorf("plan.md §5: want Write < facts re-run < lock question, got %d %d %d", write, rerun, lock)
	}
	if !strings.Contains(section[:max(lock, 0)], "**When** `facts=unresolved`") {
		t.Error("plan.md §5 must fetch the facts walk when the re-run exits non-zero, before the lock question")
	}
}

// TestReadmePlanRowNamesCitationWalk: the README's /dross-plan row is where a
// reader learns what the command does; it must say plan walks unresolved
// citations, and name mark-new as the confirm-new arm.
func TestReadmePlanRowNamesCitationWalk(t *testing.T) {
	var row string
	for _, line := range strings.Split(readmeBody(t), "\n") {
		if strings.HasPrefix(line, "| `/dross-plan` |") {
			row = line
			break
		}
	}
	if row == "" {
		t.Fatal("no README row for `/dross-plan`")
	}
	for _, needle := range []string{"`dross phase facts`", "unresolved citation", "one per turn", "`dross phase mark-new`"} {
		if !strings.Contains(row, needle) {
			t.Errorf("README /dross-plan row lacks %q:\n%s", needle, row)
		}
	}
}
