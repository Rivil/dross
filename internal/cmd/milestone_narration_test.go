package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// docText reads a repo-root-relative doc and normalises it (lowercased,
// backticks/emphasis stripped, whitespace collapsed) so assertions test for the
// presence of a claim rather than its exact formatting or line wrapping.
func docText(t *testing.T, parts ...string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(append([]string{repoRootFromTest(t)}, parts...)...))
	if err != nil {
		t.Fatalf("read %s: %v", filepath.Join(parts...), err)
	}
	s := strings.ToLower(string(b))
	s = strings.NewReplacer("`", "", "*", "", "_", "", "\n", " ").Replace(s)
	return strings.Join(strings.Fields(s), " ")
}

// The cut point is conditional now, so the unconditional claim must be gone —
// deleting it without stating the new rule is caught by the next test.
func TestMilestonePromptDropsUnconditionalCutClaim(t *testing.T) {
	if strings.Contains(docText(t, "assets", "prompts", "milestone.md"), "integration branch from main") {
		t.Error("assets/prompts/milestone.md still claims the milestone branch is cut from main unconditionally")
	}
}

// Every narration of the cut must name BOTH arms — the current milestone's
// branch while unmerged, the main branch otherwise. A doc that deletes the old
// claim without stating the new rule fails here, and the failure names the file
// so a partial pass is not green.
func TestMilestoneCutNarrationStatesBothArms(t *testing.T) {
	docs := [][]string{
		{"assets", "prompts", "milestone.md"},
		{"README.md"},
		{"docs", "roadmap.md"},
	}
	for _, parts := range docs {
		name := filepath.Join(parts...)
		text := docText(t, parts...)
		for _, needle := range []string{
			"conditional",       // the rule is stated as conditional at all
			"still unmerged",    // the stacked arm
			"current milestone", // what it stacks on
			"main",              // the fallback arm
			"base",              // the branch is recorded
		} {
			if !strings.Contains(text, needle) {
				t.Errorf("%s does not state the conditional cut rule (missing %q)", name, needle)
			}
		}
	}
}

// The flag exists in the CLI; dropping it from the README row leaves users with
// no documented way out of a wrong automatic answer.
func TestReadmeMilestoneRowDocumentsBaseFlag(t *testing.T) {
	text := docText(t, "README.md")
	if !strings.Contains(text, "--base <branch> forces the cut point") {
		t.Error("the README milestone row must document --base as the cut-point override")
	}
}

// The PR-target claims t-5/t-6 falsified: a milestone PR no longer always
// targets main, and main no longer always receives the merge.
func TestPRTargetNarrationNamesRecordedParent(t *testing.T) {
	ship := docText(t, "assets", "prompts", "ship.md")
	if strings.Contains(ship, "opens one milestone/<version> → main pr") {
		t.Error("assets/prompts/ship.md still claims milestone complete opens a milestone/<version> → main PR unconditionally")
	}
	if !strings.Contains(ship, "recorded parent") {
		t.Error("assets/prompts/ship.md must say the integration PR targets the recorded parent while it is unmerged")
	}

	readme := docText(t, "README.md")
	if strings.Contains(readme, "the milestone lands in main as a merge commit") {
		t.Error("README.md still claims the milestone always lands in main")
	}
	if strings.Contains(readme, "complete opens the milestone→main pr") {
		t.Error("the README milestone row still claims complete always opens a milestone→main PR")
	}
	if !strings.Contains(readme, "recorded base") {
		t.Error("README.md must say the integration PR targets the recorded base while it is unmerged")
	}
}
