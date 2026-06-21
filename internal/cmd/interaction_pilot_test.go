package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// deadIncludeLine is the nested @-include the pilot disproved: the command
// wrapper expands the top-level @-include of spec.md, but spec.md's own
// @-include of the snippet arrives as literal text and never reaches the model.
// spec.md must no longer carry it — delivery is via `dross interaction show`.
const deadIncludeLine = "@~/.claude/dross/prompts/_interaction.md"

// TestSpecPilotUsesEmitter proves c-3's delivery half post-pilot: spec.md's
// pre-flight invokes `dross interaction show` and no longer carries the dead
// nested @-include line. Phases 11-13 repeat this emitter call for every other
// interactive prompt.
func TestSpecPilotUsesEmitter(t *testing.T) {
	root := repoRootFromTest(t)
	b, err := os.ReadFile(filepath.Join(root, "assets", "prompts", "spec.md"))
	if err != nil {
		t.Fatalf("read spec.md: %v", err)
	}
	spec := string(b)
	if strings.Contains(spec, deadIncludeLine) {
		t.Errorf("spec.md still carries the dead nested @-include line %q — delivery is via `dross interaction show`", deadIncludeLine)
	}
	if !strings.Contains(spec, "dross interaction show") {
		t.Error("spec.md pre-flight must invoke `dross interaction show` to deliver the interaction playbook")
	}
}

// TestPilotResultRecorded proves the c-3 pilot outcome is recorded as resolved in
// the audit doc — not left pending. The sentinel is a concrete resolution phrase,
// not a date format, so it stays falsifiable.
func TestPilotResultRecorded(t *testing.T) {
	root := repoRootFromTest(t)
	b, err := os.ReadFile(filepath.Join(root, "docs", "interaction-audit.md"))
	if err != nil {
		t.Fatalf("read interaction-audit.md: %v", err)
	}
	audit := string(b)
	if !strings.Contains(audit, "resolved via the `dross interaction show` emitter") {
		t.Error("interaction-audit.md must record the pilot as resolved via the `dross interaction show` emitter")
	}
	if strings.Contains(audit, "⬜ pending human verification") {
		t.Error("interaction-audit.md still marks the pilot pending — the pilot has run; record the outcome")
	}
}
