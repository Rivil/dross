package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// otherCmdPrompts are the five remaining interactive prompts audited in phase 13:
// the two heavy audits, the backfill engine, and the handoff pair. Each has a
// single gated decision, retrofitted uniformly to invoke `dross interaction show`
// in its pre-flight (the scan_command_emitter + handoff_emitter_exception
// decisions). Twin of coreLoopPrompts / setupPrompts.
var otherCmdPrompts = []string{"architecture", "secure", "quality", "pause", "resume"}

// TestOtherCmdsWireEmitter proves c-2: each of the five remaining prompts invokes
// `dross interaction show` in its pre-flight and carries no dead nested @-include
// line. Twin of TestSetupPromptsWireEmitter — if a prompt drops the emitter call,
// the live command stops delivering the playbook at its gated turn.
func TestOtherCmdsWireEmitter(t *testing.T) {
	root := repoRootFromTest(t)
	for _, name := range otherCmdPrompts {
		body := otherCmdPromptBody(t, root, name)
		if !strings.Contains(body, "dross interaction show") {
			t.Errorf("%s.md pre-flight must invoke `dross interaction show` to deliver the interaction playbook (c-2)", name)
		}
		if strings.Contains(body, deadIncludeLine) {
			t.Errorf("%s.md still carries the dead nested @-include line %q — delivery is via `dross interaction show`", name, deadIncludeLine)
		}
	}
}

// TestOtherCmdsReferenceContract proves c-2: each of the five prompts references
// the interaction contract in prose, so the binding interaction style is visible
// to a reader, not just wired in pre-flight. Twin of
// TestSetupPromptsReferenceContract; reuses interactionRefPhrase.
func TestOtherCmdsReferenceContract(t *testing.T) {
	root := repoRootFromTest(t)
	for _, name := range otherCmdPrompts {
		body := otherCmdPromptBody(t, root, name)
		if !strings.Contains(body, interactionRefPhrase) {
			t.Errorf("%s.md must reference the contract (%q) so the interaction style is visible (c-2)", name, interactionRefPhrase)
		}
	}
}

// TestOtherCmdsAuditSectionsConform proves c-1: the audit doc records each of the
// five remaining commands as conforming. A section that still carries a pending/
// partial/violates marker (⬜ 🟡 ❌) or lacks ✅ means the audit didn't reflect the
// post-retrofit reality. Twin of TestSetupAuditSectionsConform.
func TestOtherCmdsAuditSectionsConform(t *testing.T) {
	root := repoRootFromTest(t)
	b, err := os.ReadFile(filepath.Join(root, "docs", "interaction-audit.md"))
	if err != nil {
		t.Fatalf("read interaction-audit.md: %v", err)
	}
	doc := string(b)
	for _, name := range otherCmdPrompts {
		section := coreLoopAuditSection(t, doc, name)
		if !strings.Contains(section, "✅") {
			t.Errorf("audit section dross-%s must be marked conforming (✅) post-retrofit (c-1)", name)
		}
		for _, marker := range []string{"⬜", "🟡", "❌"} {
			if strings.Contains(section, marker) {
				t.Errorf("audit section dross-%s still carries a non-conforming marker %q — retrofit incomplete (c-1)", name, marker)
			}
		}
	}
}

// TestReadmeInteractionSection proves c-3: the README documents the interaction
// model in its own top-level "## Interaction" section, and that section carries
// the contract's defining phrasing. A regression that deletes the section or
// guts its key phrases fails here.
func TestReadmeInteractionSection(t *testing.T) {
	readme := readmeBody(t)
	if !strings.Contains(readme, "\n## Interaction\n") {
		t.Fatal("README.md must have a top-level '## Interaction' section documenting the interaction model (c-3)")
	}
	section := promptSection(t, readme, "## Interaction")
	for _, phrase := range []string{"propose-and-react", "one decision per turn"} {
		if !strings.Contains(section, phrase) {
			t.Errorf("README '## Interaction' section must state the contract phrase %q (c-3)", phrase)
		}
	}
}

// TestReadmeInteractionPlacement proves the locked readme_section decision: the
// "## Interaction" heading sits immediately after "## Concept" — i.e. it is the
// first top-level "## " heading following Concept. A future edit that relocates
// the section elsewhere still passes the section test above but fails here.
func TestReadmeInteractionPlacement(t *testing.T) {
	readme := readmeBody(t)
	const concept = "\n## Concept\n"
	ci := strings.Index(readme, concept)
	if ci < 0 {
		t.Fatal("README.md must have a '## Concept' section (placement anchor for ## Interaction)")
	}
	after := readme[ci+len(concept):]
	next := strings.Index(after, "\n## ")
	if next < 0 {
		t.Fatal("no top-level heading follows '## Concept'")
	}
	if !strings.HasPrefix(after[next:], "\n## Interaction\n") {
		t.Errorf("'## Interaction' must be the first section after '## Concept' (locked readme_section placement); next heading was %q",
			firstLine(after[next+1:]))
	}
}

// otherCmdPromptBody reads one of the five prompts' markdown body, failing on a
// read error.
func otherCmdPromptBody(t *testing.T, root, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, "assets", "prompts", name+".md"))
	if err != nil {
		t.Fatalf("read %s.md: %v", name, err)
	}
	return string(b)
}

// readmeBody reads README.md at the repo root.
func readmeBody(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(repoRootFromTest(t), "README.md"))
	if err != nil {
		t.Fatalf("read README.md: %v", err)
	}
	return string(b)
}

// firstLine returns the text up to the first newline — used for a readable error.
func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
