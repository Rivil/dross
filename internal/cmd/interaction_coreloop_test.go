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
