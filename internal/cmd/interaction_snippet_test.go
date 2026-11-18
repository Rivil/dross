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

// TestInteractionSnippetHasIncludeFirstPattern proves c-3 as revised on
// 2026-09-29: the shared playbook sorts a surfaced candidate include-first — an
// in-scope one is simply included, a borderline one is an either/or that leads
// with "add to current phase", and only one with a clear home elsewhere leads
// with "defer it". If any case is dropped from _interaction.md, one of these
// needles disappears and this fails, so spec.md and plan.md can safely lean on
// the playbook instead of restating it. The old defer-first lead is asserted
// absent, so it cannot drift back in.
func TestInteractionSnippetHasIncludeFirstPattern(t *testing.T) {
	content := interactionSnippetContent(t)
	for _, needle := range []string{
		"include-first",                     // the rule's name
		"belongs here",                      // case 1: just include it
		`leads with "add to current phase"`, // case 2: borderline leads with add
		"clearly has a home elsewhere",      // case 3: the only defer lead
		"defer it",                          // the alternative
		"deferral is not the safe default",  // the reason the lead flipped
	} {
		if !strings.Contains(content, needle) {
			t.Errorf("include-first pattern incomplete: snippet missing %q", needle)
		}
	}
	if strings.Contains(content, "defer-first") {
		t.Error("snippet still carries the retired defer-first lead")
	}
}

// commandPrompts returns every assets/prompts/*.md and assets/commands/*.md
// path, relative to the repo root.
func commandPrompts(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	for _, dir := range []string{"prompts", "commands"} {
		matches, err := filepath.Glob(filepath.Join(root, "assets", dir, "*.md"))
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range matches {
			rel, _ := filepath.Rel(root, m)
			out = append(out, filepath.ToSlash(rel))
		}
	}
	if len(out) == 0 {
		t.Fatal("found no prompt files — the walk is broken")
	}
	return out
}

// TestContextNudgeRetired: the context-checkpoint nudge is retired, so no
// prompt carries its rule, quotes its marker, or calls its verb — a leftover
// line would steer the agent toward a checkpoint nothing signals any more.
func TestContextNudgeRetired(t *testing.T) {
	root := repoRootFromTest(t)
	for _, rel := range commandPrompts(t, root) {
		b, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatal(err)
		}
		s := string(b)
		for _, gone := range []string{"## Context checkpoint", "context checkpoint:", "dross hooks nudge", "[context] threshold"} {
			if strings.Contains(s, gone) {
				t.Errorf("%s still carries %q from the retired context nudge", rel, gone)
			}
		}
	}
}

// TestFooterPromptsLoadPlaybook: the playbook reaches every "safe to /clear"
// wrap-up only if each footer-bearing prompt loads it.
func TestFooterPromptsLoadPlaybook(t *testing.T) {
	root := repoRootFromTest(t)
	res, err := footerCoverage(root)
	if err != nil {
		t.Fatalf("footerCoverage: %v", err)
	}
	checked := 0
	for _, name := range res.Covered {
		st, err := promptFooterState(root, name)
		if err != nil {
			t.Fatal(err)
		}
		if st != footerPresent {
			continue
		}
		checked++
		b, err := os.ReadFile(filepath.Join(root, "assets", "prompts", name+".md"))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(b), "dross interaction show") {
			t.Errorf("%s.md carries the clear-point footer but never runs `dross interaction show`, so the playbook cannot reach its wrap-up", name)
		}
	}
	if checked == 0 {
		t.Fatal("no footer-bearing prompt found — the check would pass vacuously")
	}
}
