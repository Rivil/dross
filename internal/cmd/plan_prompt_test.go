package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// planPromptContent loads assets/prompts/plan.md and normalises it (lowercased,
// backticks/emphasis stripped) so assertions test for the presence of an
// instruction rather than its exact formatting. (r-01: the prompt edit is only
// live after `make install`; this reads the assets/ source directly.)
func planPromptContent(t *testing.T) string {
	t.Helper()
	root := repoRootFromTest(t)
	b, err := os.ReadFile(filepath.Join(root, "assets", "prompts", "plan.md"))
	if err != nil {
		t.Fatalf("read plan.md: %v", err)
	}
	s := strings.ToLower(string(b))
	return strings.NewReplacer("`", "", "*", "", "_", "").Replace(s)
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
