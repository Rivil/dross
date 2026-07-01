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

// TestPlanPromptBorderlineTaskDeferFirst proves c-2: a borderline/optional task
// proposed during §3 steering is surfaced through the playbook's defer-first
// either/or (lead "defer it", offer "add as a task"), not slipped in silently.
// Drop the framing and these needles disappear.
func TestPlanPromptBorderlineTaskDeferFirst(t *testing.T) {
	content := planPromptContent(t)
	for _, needle := range []string{
		"borderline",    // the trigger
		"defer-first",   // defer leads
		"add as a task", // the alternative
	} {
		if !strings.Contains(content, needle) {
			t.Errorf("plan.md §3 missing borderline-task defer-first framing %q", needle)
		}
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
