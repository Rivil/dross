package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// boundaryPrompts are the seven durable-boundary commands c-1 obliges to carry
// the clear-point footer.
var boundaryPrompts = []string{"spec", "plan", "execute", "verify", "ship", "quick", "pause"}

// TestBoundaryPromptsCarryFooter proves the t-3 content side of c-1 against the
// real repo: each of the seven boundary prompts contains the sentinel followed
// by a concrete /dross-… next command on the same line, failing by name.
func TestBoundaryPromptsCarryFooter(t *testing.T) {
	root := repoRootFromTest(t)
	for _, name := range boundaryPrompts {
		state, err := promptFooterState(root, name)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		switch state {
		case footerAbsent:
			t.Errorf("prompt %q is missing the clear-point footer (%q + /dross-… next command)", name, clearPointSentinel)
		case footerMalformed:
			t.Errorf("prompt %q carries the sentinel without a /dross-… next command on the same line", name)
		}
	}
}

// TestFooterCoverageFailClosed is the live gate: every command-backed prompt in
// this repo must be classified — footer-bearing or enrolled in the
// footer-audit.md `## Exempt` table. A new prompt with neither fails here,
// naming the offender.
func TestFooterCoverageFailClosed(t *testing.T) {
	root := repoRootFromTest(t)
	res, err := footerCoverage(root)
	if err != nil {
		t.Fatalf("footerCoverage: %v", err)
	}
	for _, gap := range res.Uncovered {
		t.Errorf("prompt %q is unclassified: %s", gap.Name, gap.Reason)
	}
	if len(res.Covered) == 0 {
		t.Fatal("classified zero command-backed prompts — enumeration is broken")
	}
}

// writeFooterFixture builds a minimal repo tree with four command-backed
// prompts:
//   - ship: footer toggled by shipFooter — the deleted-sentinel probe
//   - foo:  well-formed footer → covered
//   - bad:  sentinel with no /dross- command → always malformed
//   - baz:  footer-less, exempt iff exemptBaz (reason controlled by bazReason)
func writeFooterFixture(t *testing.T, shipFooter, exemptBaz bool, bazReason string) string {
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
	footer := clearPointSentinel + " · fresh session: /dross-status\n"

	shipBody := "# ship\n"
	if shipFooter {
		shipBody += footer
	}
	write("assets/commands/dross-ship.md", "allowed-tools: Read\n")
	write("assets/prompts/ship.md", shipBody)

	write("assets/commands/dross-foo.md", "allowed-tools: Read\n")
	write("assets/prompts/foo.md", "# foo\n"+footer)

	write("assets/commands/dross-bad.md", "allowed-tools: Read\n")
	write("assets/prompts/bad.md", "# bad\n"+clearPointSentinel+" and nothing else\n")

	write("assets/commands/dross-baz.md", "allowed-tools: Read\n")
	write("assets/prompts/baz.md", "# baz\n")

	exemptRow := ""
	if exemptBaz {
		exemptRow = "| baz | " + bazReason + " |\n"
	}
	write("docs/footer-audit.md",
		"# Footer audit\n\n## Exempt\n\n| Command | Reason |\n|---|---|\n"+exemptRow)
	return root
}

// TestFooterCoverageNamesDeletedFooter proves the fail-closed naming contract:
// deleting ship.md's sentinel makes the gate flag "ship"; restoring it clears
// the flag.
func TestFooterCoverageNamesDeletedFooter(t *testing.T) {
	root := writeFooterFixture(t, false, true, "fixture")
	res, err := footerCoverage(root)
	if err != nil {
		t.Fatalf("footerCoverage: %v", err)
	}
	if !uncoveredSet(res)["ship"] {
		t.Errorf("'ship' (footer deleted, not exempt) must be flagged; uncovered=%v", uncoveredSet(res))
	}
	if uncoveredSet(res)["foo"] {
		t.Error("'foo' (well-formed footer) must be covered")
	}

	root = writeFooterFixture(t, true, true, "fixture")
	res, err = footerCoverage(root)
	if err != nil {
		t.Fatalf("footerCoverage: %v", err)
	}
	if uncoveredSet(res)["ship"] {
		t.Error("'ship' with its footer restored must be covered")
	}
}

// TestFooterCoverageFlagsMalformedSentinel: a sentinel with no /dross- command
// on its line is malformed, not covered — a half-edited footer can't pass.
func TestFooterCoverageFlagsMalformedSentinel(t *testing.T) {
	root := writeFooterFixture(t, true, true, "fixture")
	res, err := footerCoverage(root)
	if err != nil {
		t.Fatalf("footerCoverage: %v", err)
	}
	reason, flagged := interactionCovReason(res, "bad")
	if !flagged {
		t.Fatalf("'bad' (sentinel without command) must be flagged; uncovered=%v", uncoveredSet(res))
	}
	if !strings.Contains(reason, "without a /dross-") {
		t.Errorf("malformed reason should say what's missing, got %q", reason)
	}
}

// TestFooterCoverageExemptReasonRequired: an Exempt row with an empty reason
// cell fails — every exemption states why.
func TestFooterCoverageExemptReasonRequired(t *testing.T) {
	root := writeFooterFixture(t, true, true, "")
	res, err := footerCoverage(root)
	if err != nil {
		t.Fatalf("footerCoverage: %v", err)
	}
	reason, flagged := interactionCovReason(res, "baz")
	if !flagged {
		t.Fatalf("'baz' (exempt without reason) must be flagged; uncovered=%v", uncoveredSet(res))
	}
	if !strings.Contains(reason, "no reason") {
		t.Errorf("empty-reason flag should say the reason cell is missing, got %q", reason)
	}
}

// TestClearPointExemptRemovalFails: dropping a footer-less command from the
// Exempt list makes it unclassified — the audit can shrink only by a prompt
// gaining a footer.
func TestClearPointExemptRemovalFails(t *testing.T) {
	root := writeFooterFixture(t, true, false, "")
	res, err := footerCoverage(root)
	if err != nil {
		t.Fatalf("footerCoverage: %v", err)
	}
	if !uncoveredSet(res)["baz"] {
		t.Errorf("'baz' must become unclassified once removed from the Exempt list; uncovered=%v", uncoveredSet(res))
	}
}

// TestFooterCoverageFlagsFooteredExempt: a prompt that gains a footer while
// still enrolled in Exempt is misclassified — the row must be removed so the
// doc never contradicts the prompts.
func TestFooterCoverageFlagsFooteredExempt(t *testing.T) {
	root := writeFooterFixture(t, true, true, "fixture")
	// Give the exempt 'baz' a footer while its Exempt row remains.
	p := filepath.Join(root, "assets", "prompts", "baz.md")
	body := "# baz\n" + clearPointSentinel + " · fresh session: /dross-status\n"
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := footerCoverage(root)
	if err != nil {
		t.Fatalf("footerCoverage: %v", err)
	}
	reason, flagged := interactionCovReason(res, "baz")
	if !flagged {
		t.Fatalf("footered-but-exempt 'baz' must be flagged; uncovered=%v", uncoveredSet(res))
	}
	if !strings.Contains(reason, "Exempt") {
		t.Errorf("misclassification reason should point at the Exempt row, got %q", reason)
	}
}
