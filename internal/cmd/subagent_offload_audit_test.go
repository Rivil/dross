package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// offloadAuditGaps returns the prompt names (basename sans .md) under
// root/assets/prompts that have no `### <name>` section in
// root/docs/subagent-offload-audit.md. Underscore partials are exempt.
// Mirrors the interaction-audit coverage convention: every prompt is
// dispositioned or the build fails — there is no silent third state.
func offloadAuditGaps(root string) ([]string, error) {
	doc, err := os.ReadFile(filepath.Join(root, "docs", "subagent-offload-audit.md"))
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(filepath.Join(root, "assets", "prompts"))
	if err != nil {
		return nil, err
	}
	var gaps []string
	for _, e := range entries {
		name, ok := strings.CutSuffix(e.Name(), ".md")
		if !ok || strings.HasPrefix(name, "_") {
			continue
		}
		if !strings.Contains(string(doc), "\n### "+name+"\n") {
			gaps = append(gaps, name)
		}
	}
	return gaps, nil
}

// TestSubagentOffloadAuditCoversEveryPrompt is the live fail-closed gate
// (c-1): every prompt in assets/prompts must carry a disposition section
// in docs/subagent-offload-audit.md. A prompt added without one fails
// here, naming it.
func TestSubagentOffloadAuditCoversEveryPrompt(t *testing.T) {
	root := repoRootFromTest(t)
	gaps, err := offloadAuditGaps(root)
	if err != nil {
		t.Fatalf("offloadAuditGaps: %v", err)
	}
	for _, name := range gaps {
		t.Errorf("prompt %q has no `### %s` section in docs/subagent-offload-audit.md", name, name)
	}
}

// TestSubagentOffloadAuditFailsOnMissingSection drives the checker over a
// fixture tree so the fail path itself is pinned: a prompt without a
// section must be reported, a sectioned one and an underscore partial
// must not.
func TestSubagentOffloadAuditFailsOnMissingSection(t *testing.T) {
	root := t.TempDir()
	write := func(rel, content string) {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("assets/prompts/covered.md", "# covered\n")
	write("assets/prompts/orphan.md", "# orphan\n")
	write("assets/prompts/_partial.md", "# partial\n")
	write("docs/subagent-offload-audit.md", "# audit\n\n### covered\n\ninline-only.\n")

	gaps, err := offloadAuditGaps(root)
	if err != nil {
		t.Fatalf("offloadAuditGaps: %v", err)
	}
	if len(gaps) != 1 || gaps[0] != "orphan" {
		t.Errorf("want exactly [orphan] uncovered, got %v", gaps)
	}
}
