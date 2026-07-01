package cmd

import (
	"os"
	"path/filepath"
	"strings"
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

// TestAuditDocDocumentsConvention pins the c-4 documentation: interaction-audit.md
// states the machine-checked coverage convention (both classification states), and
// _interaction.md — the playbook a command author reads — cross-references it. A
// future edit that guts either statement fails here, so the convention can't
// silently drift out of the docs.
func TestAuditDocDocumentsConvention(t *testing.T) {
	root := repoRootFromTest(t)

	audit := mustRead(t, filepath.Join(root, "docs", "interaction-audit.md"))
	for _, phrase := range []string{
		"Coverage convention",
		"### dross-<name>", // the interactive → section half
		"## Exempt",        // the non-interactive → exempt half
		"fail",             // fail-closed framing
	} {
		if !strings.Contains(audit, phrase) {
			t.Errorf("interaction-audit.md must document the coverage convention (missing %q)", phrase)
		}
	}

	playbook := mustRead(t, filepath.Join(root, "assets", "prompts", "_interaction.md"))
	for _, phrase := range []string{
		"Coverage convention",
		"docs/interaction-audit.md", // cross-reference target
		"Exempt",
	} {
		if !strings.Contains(playbook, phrase) {
			t.Errorf("_interaction.md must cross-reference the coverage convention (missing %q)", phrase)
		}
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

// interactionCovWrite lays down an arbitrary file tree under a fresh temp dir and
// returns the root, so a test can drive interactionCoverage past an exact branch.
func interactionCovWrite(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, content := range files {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// interactionCovReason pulls the Uncovered reason string for name out of a result.
func interactionCovReason(res coverageResult, name string) (string, bool) {
	for _, g := range res.Uncovered {
		if g.Name == name {
			return g.Reason, true
		}
	}
	return "", false
}

// TestInteractionCoverageCover_uncoveredReasons pins the exact human-readable
// Reason string for each interactive-side gap branch. The reason embeds the
// command name via `+ name +` string concatenation (interaction_coverage.go:63
// and :66); asserting the fully-composed message — name spliced into the middle —
// is what catches a mutated concatenation that would otherwise change nothing
// observable, since the other coverage tests only inspect Uncovered names.
func TestInteractionCoverageCover_uncoveredReasons(t *testing.T) {
	cases := []struct {
		name    string
		cmdName string
		// files beyond the shim+prompt for cmdName; the audit doc always exists.
		auditDoc   string
		wantReason string
	}{
		{
			// interactive && isExempt → interaction_coverage.go:62-63
			name:     "interactive_but_exempt",
			cmdName:  "qux",
			auditDoc: "# Interaction audit\n\n## Exempt\n\n| Command | Reason |\n|---|---|\n| qux | listed here by mistake |\n",
			wantReason: "shim lists AskUserQuestion but it is on the Exempt list — " +
				"give it a `### dross-qux` audit section instead",
		},
		{
			// interactive && !hasSection (and not exempt) → interaction_coverage.go:64-66
			name:       "interactive_no_section",
			cmdName:    "quux",
			auditDoc:   "# Interaction audit\n\n## Exempt\n\n| Command | Reason |\n|---|---|\n",
			wantReason: "interactive but has no `### dross-quux` section in interaction-audit.md",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := interactionCovWrite(t, map[string]string{
				"assets/commands/dross-" + tc.cmdName + ".md": "allowed-tools: AskUserQuestion\n",
				"assets/prompts/" + tc.cmdName + ".md":        "# " + tc.cmdName + "\n",
				"docs/interaction-audit.md":                   tc.auditDoc,
			})
			res, err := interactionCoverage(root)
			if err != nil {
				t.Fatalf("interactionCoverage: %v", err)
			}
			got, ok := interactionCovReason(res, tc.cmdName)
			if !ok {
				t.Fatalf("%q should be uncovered; uncovered=%v", tc.cmdName, uncoveredSet(res))
			}
			if got != tc.wantReason {
				t.Errorf("reason mismatch:\n got: %q\nwant: %q", got, tc.wantReason)
			}
		})
	}
}
