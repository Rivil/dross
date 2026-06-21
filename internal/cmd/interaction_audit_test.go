package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// interactiveCommands returns the set of command names whose
// assets/commands/dross-<name>.md wrapper lists AskUserQuestion — i.e. the
// commands the interaction contract applies to. This mirrors the doc's stated
// scope (`grep -l AskUserQuestion assets/commands/`).
func interactiveCommands(t *testing.T, root string) []string {
	t.Helper()
	dir := filepath.Join(root, "assets", "commands")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	var names []string
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() || !strings.HasPrefix(n, "dross-") || !strings.HasSuffix(n, ".md") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, n))
		if err != nil {
			t.Fatalf("read %s: %v", n, err)
		}
		if !strings.Contains(string(b), "AskUserQuestion") {
			continue
		}
		names = append(names, strings.TrimSuffix(strings.TrimPrefix(n, "dross-"), ".md"))
	}
	return names
}

// TestInteractionAuditEnumeratesEveryInteractiveCommand proves c-4: docs/
// interaction-audit.md must have a section for every interactive command, with a
// per-decision-point table that carries a conformance column. If a new
// interactive command is added without a section, this fails.
func TestInteractionAuditEnumeratesEveryInteractiveCommand(t *testing.T) {
	root := repoRootFromTest(t)
	b, err := os.ReadFile(filepath.Join(root, "docs", "interaction-audit.md"))
	if err != nil {
		t.Fatalf("read interaction-audit.md: %v", err)
	}
	doc := string(b)
	lower := strings.ToLower(doc)

	// Table must declare a decision-point column and a conformance column.
	if !strings.Contains(lower, "decision point") {
		t.Error("audit doc must have a 'Decision point' column")
	}
	if !strings.Contains(lower, "conforms") {
		t.Error("audit doc must have a 'Conforms' conformance column")
	}

	for _, name := range interactiveCommands(t, root) {
		heading := "### dross-" + name
		idx := strings.Index(doc, heading)
		if idx < 0 {
			t.Errorf("interactive command %q has no section (%q) in interaction-audit.md", name, heading)
			continue
		}
		// Section runs from its heading to the next "### " heading (or EOF).
		rest := doc[idx+len(heading):]
		if next := strings.Index(rest, "\n### "); next >= 0 {
			rest = rest[:next]
		}
		// A per-decision-point table = header row + separator + >=1 data row,
		// i.e. at least three lines beginning with "|".
		pipeLines := 0
		for _, ln := range strings.Split(rest, "\n") {
			if strings.HasPrefix(strings.TrimSpace(ln), "|") {
				pipeLines++
			}
		}
		if pipeLines < 3 {
			t.Errorf("section %q must list at least one per-decision-point row (found %d table lines)", heading, pipeLines)
		}
	}
}
