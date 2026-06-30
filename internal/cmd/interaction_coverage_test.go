package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

// TestInteractionCoverageFailClosed is the live gate (c-1): every command-backed
// prompt in this repo must be classified — interactive-with-audit-section or
// enrolled in the interaction-audit.md `## Exempt` list. A new non-interactive
// command not added to Exempt (or an interactive one without a section) fails
// here, naming the offending prompt.
func TestInteractionCoverageFailClosed(t *testing.T) {
	root := repoRootFromTest(t)
	res, err := interactionCoverage(root)
	if err != nil {
		t.Fatalf("interactionCoverage: %v", err)
	}
	for _, gap := range res.Uncovered {
		t.Errorf("prompt %q is unclassified: %s", gap.Name, gap.Reason)
	}
	if len(res.Covered) == 0 {
		t.Fatal("classified zero command-backed prompts — enumeration is broken")
	}
}

// writeCoverageFixture builds a minimal repo tree under a temp dir with three
// command-backed prompts plus a partial:
//   - foo: interactive (shim lists AskUserQuestion) + has an audit section → covered
//   - bar: non-interactive, never exempt → the always-uncovered probe
//   - baz: non-interactive, exempt iff exemptBaz → covered/uncovered toggle
//   - _partial: must never enter the universe
func writeCoverageFixture(t *testing.T, exemptBaz bool) string {
	t.Helper()
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
	write("assets/commands/dross-foo.md", "allowed-tools: AskUserQuestion\n")
	write("assets/prompts/foo.md", "# foo\n")
	write("assets/commands/dross-bar.md", "allowed-tools: Read\n")
	write("assets/prompts/bar.md", "# bar\n")
	write("assets/commands/dross-baz.md", "allowed-tools: Read\n")
	write("assets/prompts/baz.md", "# baz\n")
	write("assets/prompts/_partial.md", "# partial\n")

	exemptRow := ""
	if exemptBaz {
		exemptRow = "| baz | read-only test fixture |\n"
	}
	write("docs/interaction-audit.md",
		"# Interaction audit\n\n"+
			"### dross-foo\n\n| Decision point | Conforms |\n|---|---|\n| pick | yes |\n\n"+
			"## Exempt\n\n| Command | Reason |\n|---|---|\n"+exemptRow)
	return root
}

func uncoveredSet(res coverageResult) map[string]bool {
	m := map[string]bool{}
	for _, g := range res.Uncovered {
		m[g.Name] = true
	}
	return m
}

// TestInteractionCoverageDetectsUnclassified proves the first contract: a
// non-interactive command-backed prompt with no Exempt entry is flagged, while
// interactive-with-section and exempt prompts are not, and _-partials stay out
// of the universe entirely.
func TestInteractionCoverageDetectsUnclassified(t *testing.T) {
	root := writeCoverageFixture(t, true)
	res, err := interactionCoverage(root)
	if err != nil {
		t.Fatalf("interactionCoverage: %v", err)
	}
	uncovered := uncoveredSet(res)

	if !uncovered["bar"] {
		t.Errorf("'bar' (non-interactive, not exempt) should be flagged unclassified; uncovered=%v", uncovered)
	}
	if uncovered["foo"] {
		t.Error("'foo' (interactive + audit section) should be covered, not flagged")
	}
	if uncovered["baz"] {
		t.Error("'baz' (non-interactive + exempt) should be covered, not flagged")
	}
	if uncovered["partial"] || uncovered["_partial"] {
		t.Error("a _-prefixed partial leaked into the coverage universe")
	}
}

// TestInteractionCoverageExemptRemovalFails proves the second contract: drop a
// non-interactive command's Exempt entry and it becomes unclassified — the same
// mechanism that fails the build if status or plan-review is removed from the
// real doc's Exempt list.
func TestInteractionCoverageExemptRemovalFails(t *testing.T) {
	root := writeCoverageFixture(t, false)
	res, err := interactionCoverage(root)
	if err != nil {
		t.Fatalf("interactionCoverage: %v", err)
	}
	if !uncoveredSet(res)["baz"] {
		t.Error("'baz' should become unclassified once removed from the Exempt list")
	}
}

// TestParseExemptListReadsRealDoc pins that the live interaction-audit.md enrolls
// exactly the two intended non-interactive commands (c-2): status and plan-review.
func TestParseExemptListReadsRealDoc(t *testing.T) {
	root := repoRootFromTest(t)
	b, err := os.ReadFile(filepath.Join(root, "docs", "interaction-audit.md"))
	if err != nil {
		t.Fatalf("read interaction-audit.md: %v", err)
	}
	exempt := parseExemptList(string(b))
	for _, name := range []string{"status", "plan-review"} {
		if reason := exempt[name]; reason == "" {
			t.Errorf("%q must be on the Exempt list with a reason; got none", name)
		}
	}
}
