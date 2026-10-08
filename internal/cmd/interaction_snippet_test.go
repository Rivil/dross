package cmd

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/ctxnudge"
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

// contextRuleSection returns _interaction.md's "## Context checkpoint" section,
// raw (not lowercased), up to the next "## " heading.
func contextRuleSection(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(repoRootFromTest(t), "assets", "prompts", "_interaction.md"))
	if err != nil {
		t.Fatalf("read _interaction.md: %v", err)
	}
	_, sec, ok := strings.Cut(string(b), "\n## Context checkpoint\n")
	if !ok {
		t.Fatal("_interaction.md has no \"## Context checkpoint\" section")
	}
	sec, _, _ = strings.Cut(sec, "\n## ")
	return sec
}

// TestInteractionSnippetHasContextNudgeRule: the playbook carries the one rule
// that turns a nudge into a checkpoint lead (locked gate_scope), keyed on the
// exact marker the hook prints, with every scope limit the spec locked.
func TestInteractionSnippetHasContextNudgeRule(t *testing.T) {
	sec := strings.ToLower(contextRuleSection(t))
	if !strings.Contains(sec, strings.ToLower(ctxnudge.Marker)) {
		t.Errorf("the context rule does not quote the hook's marker %q verbatim", ctxnudge.Marker)
	}
	for _, phrase := range []string{
		"checkpoint + /clear", "next durable boundary", "even mid-wave", "§1g", "overrides",
		"safe to /clear", "--solo", "informational", "before its artifact is written", "/dross-pause",
	} {
		if !strings.Contains(sec, phrase) {
			t.Errorf("the context rule lacks %q", phrase)
		}
	}
}

// TestInteractionShowCarriesContextRule: the rule reaches a session only
// through the embedded playbook `dross interaction show` prints.
func TestInteractionShowCarriesContextRule(t *testing.T) {
	out := captureStdout(t, func() {
		c := Interaction()
		c.SetArgs([]string{"show"})
		if err := c.Execute(); err != nil {
			t.Fatalf("interaction show: %v", err)
		}
	})
	if !strings.Contains(out, ctxnudge.Marker) {
		t.Errorf("`dross interaction show` does not carry %q — the embedded playbook is stale", ctxnudge.Marker)
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

// TestNudgeRuleLivesOnlyInPlaybook: one rule, in one place (gate_scope). A
// command prompt that quoted the marker would be a second copy to drift.
func TestNudgeRuleLivesOnlyInPlaybook(t *testing.T) {
	root := repoRootFromTest(t)
	for _, rel := range commandPrompts(t, root) {
		if rel == "assets/prompts/_interaction.md" {
			continue
		}
		b, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(b), ctxnudge.Marker) {
			t.Errorf("%s quotes %q; the nudge rule lives only in _interaction.md", rel, ctxnudge.Marker)
		}
	}
}

// drossVerb matches a `dross <verb>` CLI call (not a /dross-<name> slash command).
var drossVerb = regexp.MustCompile(`\bdross [a-z]`)

// TestContextRuleAddsNoCalls: the signal reaches the agent only through the
// hook's injected line (locked signal_path) — no prompt calls the hook verb,
// none checks context through a dross call, and the rule itself names none.
func TestContextRuleAddsNoCalls(t *testing.T) {
	root := repoRootFromTest(t)
	for _, rel := range commandPrompts(t, root) {
		b, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatal(err)
		}
		s := string(b)
		// options.md §15 documents the wired hooks by command; it calls none.
		if rel != "assets/prompts/options.md" && strings.Contains(s, "dross hooks nudge") {
			t.Errorf("%s calls `dross hooks nudge`; it is a hook verb, never a prompt step", rel)
		}
		if strings.Contains(s, "dross context") {
			t.Errorf("%s runs a `dross context` check; the nudge line is the only signal", rel)
		}
	}
	if m := drossVerb.FindString(contextRuleSection(t)); m != "" {
		t.Errorf("the context rule names a dross call (%q…); it must add no round-trip", m)
	}
}

// TestContextRuleSizeBudget: the rule is cache-read on every later turn of
// every interactive command, so it stays short.
func TestContextRuleSizeBudget(t *testing.T) {
	if n := len(contextRuleSection(t)); n > 700 {
		t.Errorf("the context rule is %d bytes; budget is 700", n)
	}
}

// TestFooterPromptsLoadPlaybook: the rule reaches every "safe to /clear"
// wrap-up only if each footer-bearing prompt loads the playbook.
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
			t.Errorf("%s.md carries the clear-point footer but never runs `dross interaction show`, so the context rule cannot reach its wrap-up", name)
		}
	}
	if checked == 0 {
		t.Fatal("no footer-bearing prompt found — the check would pass vacuously")
	}
}
