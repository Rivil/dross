package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// interactionSnippetContent loads assets/prompts/_interaction.md lowercased but
// WITHOUT stripping markdown punctuation — the canonical contract phrases are
// matched verbatim so drift between the snippet and the dross-interaction-contract
// builtin rule (guarded from the rule side in internal/rules) is caught here.
func interactionSnippetContent(t *testing.T) string {
	t.Helper()
	root := repoRootFromTest(t)
	b, err := os.ReadFile(filepath.Join(root, "assets", "prompts", "_interaction.md"))
	if err != nil {
		t.Fatalf("read _interaction.md: %v", err)
	}
	return strings.ToLower(string(b))
}

// TestInteractionSnippetHasPlaybookMarkers proves c-2's content half: the shared
// snippet must spell out all four playbook markers. If any is dropped, the
// snippet has stopped carrying the full playbook and this fails.
func TestInteractionSnippetHasPlaybookMarkers(t *testing.T) {
	content := interactionSnippetContent(t)
	markers := map[string][]string{
		"propose-and-react":         {"propose", "react"},
		"one decision per turn":     {"one decision per turn"},
		"no walls of text":          {"wall"},
		"never paste artifact back": {"never paste the build artifact back"},
	}
	for name, needles := range markers {
		for _, n := range needles {
			if !strings.Contains(content, n) {
				t.Errorf("playbook marker %q missing: snippet has no %q", name, n)
			}
		}
	}
}

// TestInteractionSnippetHasAcceptRewordDropExample proves the snippet ships a
// concrete AskUserQuestion accept/reword/drop example, not just abstract advice.
func TestInteractionSnippetHasAcceptRewordDropExample(t *testing.T) {
	content := interactionSnippetContent(t)
	if !strings.Contains(content, "askuserquestion") {
		t.Error("snippet must reference AskUserQuestion as the turn-driving tool")
	}
	for _, opt := range []string{"accept", "reword", "drop"} {
		if !strings.Contains(content, opt) {
			t.Errorf("snippet missing the %q option from the accept/reword/drop gate", opt)
		}
	}
}

// TestInteractionSnippetHasDeferOrAddPattern proves c-3: the shared playbook
// carries the defer-or-add pattern for borderline candidates — a defer-first
// either/or (lead with "defer it", offer "add to current phase"), scoped to
// genuinely borderline items. If any half of the pattern is dropped from
// _interaction.md, one of these needles disappears and this fails, so spec.md
// and plan.md can safely defer to the playbook instead of restating it.
func TestInteractionSnippetHasDeferOrAddPattern(t *testing.T) {
	content := interactionSnippetContent(t)
	for _, needle := range []string{
		"defer it",             // the lead option
		"add to current phase", // the alternative
		"defer-first",          // defer leads
		"borderline",           // the trigger — only for borderline candidates
	} {
		if !strings.Contains(content, needle) {
			t.Errorf("defer-or-add pattern incomplete: snippet missing %q", needle)
		}
	}
}
