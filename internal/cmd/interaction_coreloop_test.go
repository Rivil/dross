package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// coreLoopPrompts are the five interactive prompts retrofitted in phase 11
// (spec.md is the phase-10 pilot, guarded separately by interaction_pilot_test).
var coreLoopPrompts = []string{"plan", "execute", "verify", "ship", "review"}

// interactionRefPhrase is the grep-verifiable phrase every retrofitted prompt
// must carry so a reader sees the interaction style is binding (c-2). spec.md's
// pilot established it; the core-loop prompts mirror it.
const interactionRefPhrase = "interaction playbook"

// TestCoreLoopPromptsWireEmitter proves c-1: each core-loop prompt invokes
// `dross interaction show` in its pre-flight and carries no dead nested
// @-include line. This is the grep-verification half of the retrofit — if a
// prompt drops the emitter call, the live command stops delivering the playbook.
func TestCoreLoopPromptsWireEmitter(t *testing.T) {
	root := repoRootFromTest(t)
	for _, name := range coreLoopPrompts {
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

// TestCoreLoopPromptsReferenceContract proves c-2: each core-loop prompt
// references the interaction contract in prose, so the binding interaction style
// is visible to a reader, not just wired in pre-flight.
func TestCoreLoopPromptsReferenceContract(t *testing.T) {
	root := repoRootFromTest(t)
	for _, name := range coreLoopPrompts {
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

// TestCoreLoopAuditSectionsConform proves c-5: the audit doc records each of the
// five retrofitted commands as conforming. A section that still carries a
// pending/partial/violates marker (⬜ 🟡 ❌) means the retrofit didn't land or
// regressed — the audit must reflect the post-retrofit reality.
func TestCoreLoopAuditSectionsConform(t *testing.T) {
	root := repoRootFromTest(t)
	b, err := os.ReadFile(filepath.Join(root, "docs", "interaction-audit.md"))
	if err != nil {
		t.Fatalf("read interaction-audit.md: %v", err)
	}
	doc := string(b)
	for _, name := range coreLoopPrompts {
		section := coreLoopAuditSection(t, doc, name)
		if !strings.Contains(section, "✅") {
			t.Errorf("audit section dross-%s must be marked conforming (✅) post-retrofit (c-5)", name)
		}
		for _, marker := range []string{"⬜", "🟡", "❌"} {
			if strings.Contains(section, marker) {
				t.Errorf("audit section dross-%s still carries a non-conforming marker %q — retrofit incomplete (c-5)", name, marker)
			}
		}
	}
}

// coreLoopAuditSection returns the text of the `### dross-<name>` section, from
// its heading to the next "### " heading (or EOF). Mirrors the slicing in
// interaction_audit_test.go.
func coreLoopAuditSection(t *testing.T, doc, name string) string {
	t.Helper()
	heading := "### dross-" + name
	idx := strings.Index(doc, heading)
	if idx < 0 {
		t.Fatalf("interaction-audit.md has no section %q", heading)
	}
	rest := doc[idx+len(heading):]
	if next := strings.Index(rest, "\n### "); next >= 0 {
		rest = rest[:next]
	}
	return rest
}

// promptSection returns the text of a prompt's `## ` section, from the given
// heading to the next "## " heading (or EOF). Used to assert per-decision-point
// structure inside a single prompt without a brittle whole-file string count.
func promptSection(t *testing.T, body, heading string) string {
	t.Helper()
	idx := strings.Index(body, heading)
	if idx < 0 {
		t.Fatalf("prompt section %q not found", heading)
	}
	rest := body[idx+len(heading):]
	if next := strings.Index(rest, "\n## "); next >= 0 {
		rest = rest[:next]
	}
	return rest
}

// TestShipPromptDecisionTurnsSeparate proves c-3 directly for ship.md: its three
// decision points (§2 body override, §3 reviewers, §6 merge gate) are each their
// own single AskUserQuestion turn, and reviewers is a propose-and-react turn
// rather than a silent config-write. A regression that bundles two decisions
// into one turn, or reverts reviewers to a bare `dross project set`, fails here —
// closing the gap that left c-3 weak (audit-doc proxy only) at first verify.
func TestShipPromptDecisionTurnsSeparate(t *testing.T) {
	root := repoRootFromTest(t)
	b, err := os.ReadFile(filepath.Join(root, "assets", "prompts", "ship.md"))
	if err != nil {
		t.Fatalf("read ship.md: %v", err)
	}
	ship := string(b)

	turns := []struct {
		heading string // section heading anchor
		anchor  string // the stable decision prompt inside that turn
	}{
		{"## 2. Body override", "Use generated body, or write your own?"},
		{"## 3. Reviewers", "Request reviewers"},
		{"## 6. Merge gate", "Merge now?"},
	}
	for _, tn := range turns {
		section := promptSection(t, ship, tn.heading)
		if n := strings.Count(section, "AskUserQuestion"); n != 1 {
			t.Errorf("ship.md %q must hold exactly one AskUserQuestion turn (c-3); found %d", tn.heading, n)
		}
		if !strings.Contains(section, tn.anchor) {
			t.Errorf("ship.md %q must key its turn on the stable anchor %q (c-3)", tn.heading, tn.anchor)
		}
	}

	// Reviewers must be a propose-and-react turn, not a silent config-write.
	reviewers := promptSection(t, ship, "## 3. Reviewers")
	if !strings.Contains(reviewers, "rather than silently writing config") {
		t.Error("ship.md §3 must drive reviewers as a propose-and-react turn ('rather than silently writing config'), not a silent config-write (c-3)")
	}
}

// TestPlanPromptNoArtifactDump proves c-4 directly for plan.md: §5 confirms the
// written plan.toml with a one-line summary and explicitly forbids pasting the
// toml back. A regression that re-adds a wholesale artifact dump would remove or
// contradict the no-paste directive, failing here — closing the gap that left
// c-4 weak (audit-doc proxy only) at first verify. (No negative [[task]]
// assertion: §5 legitimately documents the plan.toml schema with [[task]].)
func TestPlanPromptNoArtifactDump(t *testing.T) {
	root := repoRootFromTest(t)
	b, err := os.ReadFile(filepath.Join(root, "assets", "prompts", "plan.md"))
	if err != nil {
		t.Fatalf("read plan.md: %v", err)
	}
	section := promptSection(t, string(b), "## 5. Write plan.toml")
	for _, anchor := range []string{"Plan written: N tasks across W waves", "Don't paste the toml back"} {
		if !strings.Contains(section, anchor) {
			t.Errorf("plan.md §5 must carry %q so the plan is confirmed by summary, not by dumping the toml (c-4)", anchor)
		}
	}
}
