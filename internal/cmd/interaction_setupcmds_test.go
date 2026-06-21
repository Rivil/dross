package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// setupPrompts are the seven setup/config interactive prompts retrofitted in
// phase 12. They mirror the core-loop retrofit (phase 11) one tier out from the
// spec→plan→execute→verify→ship pipeline: project bootstrap, adoption, settings,
// rules, board triage, one-shot tasks, and milestone scoping.
var setupPrompts = []string{"init", "onboard", "options", "rule", "inbox", "quick", "milestone"}

// TestSetupPromptsWireEmitter proves c-1: each setup/config prompt invokes
// `dross interaction show` in its pre-flight and carries no dead nested
// @-include line. Twin of TestCoreLoopPromptsWireEmitter — if a prompt drops the
// emitter call, the live command stops delivering the playbook.
func TestSetupPromptsWireEmitter(t *testing.T) {
	root := repoRootFromTest(t)
	for _, name := range setupPrompts {
		path := filepath.Join(root, "assets", "prompts", name+".md")
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s.md: %v", name, err)
		}
		body := string(b)
		if !strings.Contains(body, "dross interaction show") {
			t.Errorf("%s.md pre-flight must invoke `dross interaction show` to deliver the interaction playbook (c-1)", name)
		}
		if strings.Contains(body, deadIncludeLine) {
			t.Errorf("%s.md still carries the dead nested @-include line %q — delivery is via `dross interaction show`", name, deadIncludeLine)
		}
	}
}

// TestSetupPromptsReferenceContract proves c-2: each setup/config prompt
// references the interaction contract in prose, so the binding interaction style
// is visible to a reader, not just wired in pre-flight. Twin of
// TestCoreLoopPromptsReferenceContract; reuses interactionRefPhrase.
func TestSetupPromptsReferenceContract(t *testing.T) {
	root := repoRootFromTest(t)
	for _, name := range setupPrompts {
		path := filepath.Join(root, "assets", "prompts", name+".md")
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s.md: %v", name, err)
		}
		if !strings.Contains(string(b), interactionRefPhrase) {
			t.Errorf("%s.md must reference the contract (%q) so the interaction style is visible (c-2)", name, interactionRefPhrase)
		}
	}
}

// setupPromptBody reads a setup prompt's markdown body, failing the test on a
// read error. Shared by the c-3/c-4 structural tests below.
func setupPromptBody(t *testing.T, root, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, "assets", "prompts", name+".md"))
	if err != nil {
		t.Fatalf("read %s.md: %v", name, err)
	}
	return string(b)
}

// TestSetupPromptDecisionAnchors proves c-3 per prompt: each retrofitted decision
// walk keys on a stable string inside its own section proving the propose-and-
// react shape. A regression that reverts a walk to a bundled turn drops the
// anchor and fails the owning prompt's case — pointing at exactly one owner,
// closing phase 11's "audit-doc proxy only" gap for the setup family.
func TestSetupPromptDecisionAnchors(t *testing.T) {
	root := repoRootFromTest(t)
	cases := []struct {
		prompt  string
		heading string
		anchors []string
	}{
		{"init", "## 1. Vision", []string{"one field per turn"}},
		{"onboard", "## 1. Identity", []string{"one field per turn"}},
		{"options", "## Section pick", []string{"section-pick gate"}},
		{"milestone", "## 3. Success criteria", []string{"one criterion per turn"}},
		{"quick", "## 2. Propose", []string{"`proceed`", "`steer`", "`show me <X>`", "`abort`"}},
		{"rule", "## Add — one decision per turn", []string{"separate proposal turns"}},
		{"inbox", "## 2. Triage each issue", []string{"one issue per turn"}},
	}
	for _, c := range cases {
		section := promptSection(t, setupPromptBody(t, root, c.prompt), c.heading)
		for _, a := range c.anchors {
			if !strings.Contains(section, a) {
				t.Errorf("%s.md %q must carry the anchor %q proving its propose-and-react shape (c-3)", c.prompt, c.heading, a)
			}
		}
	}
}

// TestSetupNoBundledTurns proves c-3 generically: each single-decision section
// must hold exactly one AskUserQuestion, so a future edit that bundles two
// decisions into one turn fails here even where no named anchor covers it. The
// intentional multi-turn walks (init §1, milestone §3, rule Add) are excluded —
// they are guarded by TestSetupPromptDecisionAnchors instead.
func TestSetupNoBundledTurns(t *testing.T) {
	root := repoRootFromTest(t)
	singles := []struct{ prompt, heading string }{
		{"options", "## Section pick"},
		{"options", "## How each section works"},
		{"milestone", "## 2. Title"},
		{"inbox", "## 2. Triage each issue"},
	}
	for _, s := range singles {
		section := promptSection(t, setupPromptBody(t, root, s.prompt), s.heading)
		if n := strings.Count(section, "AskUserQuestion"); n != 1 {
			t.Errorf("%s.md %q must hold exactly one AskUserQuestion turn (c-3); found %d", s.prompt, s.heading, n)
		}
	}
}

// TestSetupNoArtifactDump proves c-4: every setup command that composes a config
// artifact carries an explicit no-paste directive, so confirmation is a one-line
// summary rather than dumping the artifact back. Positive assertion (the phase-11
// pattern) — the presence of the directive — not a fragile keyword scan for a bad
// paste instruction.
func TestSetupNoArtifactDump(t *testing.T) {
	root := repoRootFromTest(t)
	for _, name := range []string{"init", "onboard", "options", "rule", "milestone"} {
		body := setupPromptBody(t, root, name)
		if !strings.Contains(body, "never paste") {
			t.Errorf("%s.md must carry a 'never paste ... back' directive so the composed artifact is confirmed by summary, not dumped (c-4)", name)
		}
	}
}
