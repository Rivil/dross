package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/mutation"
	"github.com/Rivil/dross/internal/verify"
)

// TestVerifyPromptOffloadGuidance pins verify.md §2's offload passage
// (subagent-offload-audit c-2/c-4): the criterion-mapping step must
// carry size-gated read-only offload guidance, and the passage must
// keep both halves of the contract — the size gate (offload_posture
// lock: conditional, never mandatory) and the agent-gate boundary
// (judgement/cross-check/verdict stay in the main loop; fan-out agents
// never fill in verify.toml).
func TestVerifyPromptOffloadGuidance(t *testing.T) {
	b, err := os.ReadFile(filepath.Join(repoRootFromTest(t), "assets", "prompts", "verify.md"))
	if err != nil {
		t.Fatal(err)
	}
	prompt := string(b)

	for _, phrase := range []string{
		// the guidance itself
		"Offload the reading when the surface is large",
		"read-only subagents",
		// the size gate — conditional, never mandatory (offload_posture)
		"For a small phase, stay inline",
		// the agent-gate boundary: authority never moves
		"never fill in verify.toml or decide the verdict",
		// the audit doc is the disposition's home
		"docs/subagent-offload-audit.md",
	} {
		if !strings.Contains(prompt, phrase) {
			t.Errorf("verify.md lost its offload guidance phrase %q", phrase)
		}
	}
}

// verifyPromptBody reads assets/prompts/verify.md.
func verifyPromptBody(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(repoRootFromTest(t), "assets", "prompts", "verify.md"))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// TestVerifyPromptNamesEveryMutationStatus is the ONLY gate keeping the prompt
// in step with the Go constants: mutation_status is absent from
// internal/configenum, so no enum-divergence check covers it. Adding a status
// to verify.go without teaching the prompt about it would leave /dross-verify
// applying the score thresholds to a value that has no score.
func TestVerifyPromptNamesEveryMutationStatus(t *testing.T) {
	prompt := verifyPromptBody(t)
	for _, status := range []string{
		verify.MutationMeasured,
		verify.MutationUnmeasurable,
		verify.MutationSkipped,
		verify.MutationOutOfScope,
	} {
		if !strings.Contains(prompt, status) {
			t.Errorf("verify.md never names mutation_status %q", status)
		}
	}
}

// TestVerifyPromptPutsOutOfScopeInTheNonThresholdBranch: out-of-scope carries a
// 0.00 score that means "nothing measured these changes", not "the tests are
// bad". Pasting it into the measured branch would fail every such phase on the
// < 0.60 cutoff — the same shape as the FeastAhead unmeasurable bug.
func TestVerifyPromptPutsOutOfScopeInTheNonThresholdBranch(t *testing.T) {
	prompt := verifyPromptBody(t)

	// Both boundaries are the START of their branch's line, so the measured
	// slice cannot accidentally swallow the head of the branch that follows.
	measured := strings.Index(prompt, "\nIf `mutation_status == \"measured\"`")
	nonThreshold := strings.Index(prompt, "\nIf `mutation_status` is `unmeasurable`")
	if measured < 0 || nonThreshold < 0 {
		t.Fatalf("verify.md's verdict branches moved: measured=%d nonThreshold=%d", measured, nonThreshold)
	}
	if nonThreshold < measured {
		t.Fatalf("expected the coverage-only branch to follow the measured one")
	}
	measuredBranch := prompt[measured:nonThreshold]
	if !strings.Contains(prompt[nonThreshold:], "base verdict on criterion coverage alone") {
		t.Error("the second branch no longer says it judges on criterion coverage alone")
	}
	if strings.Contains(measuredBranch, verify.MutationOutOfScope) {
		t.Errorf("out-of-scope appears in the threshold-applying branch:\n%s", measuredBranch)
	}

	// And it must appear in the branch that explicitly withholds the cutoffs.
	tail := prompt[nonThreshold:]
	if end := strings.Index(tail, "\n## "); end > 0 {
		tail = tail[:end]
	}
	if !strings.Contains(tail, verify.MutationOutOfScope) {
		t.Errorf("out-of-scope is not listed in the coverage-only branch:\n%s", tail)
	}
	if !strings.Contains(prompt, "0.80/0.60 cutoffs do NOT apply") {
		t.Error("the coverage-only branch must say the cutoffs do not apply")
	}
}

// TestVerifyPromptCrossCheckReadsInScopeOnly: the cross-check must be pointed
// at the filtered survivor list and told not to re-raise the filtered ones.
// Per-survivor findings for another file's debt is a standing backlog no phase
// can drain, which rule r-02 forbids; the routing lives with the count NOTE
// and the named successor phase.
func TestVerifyPromptCrossCheckReadsInScopeOnly(t *testing.T) {
	prompt := verifyPromptBody(t)
	for _, phrase := range []string{
		"languages[].mutation.surviving",
		"out_of_scope",
		"Do not re-list the `out_of_scope` entries as findings",
	} {
		if !strings.Contains(prompt, phrase) {
			t.Errorf("verify.md cross-check missing %q", phrase)
		}
	}
}

// TestVerifyPromptWeightsSurvivorsByOrigin: both tags must be named and
// distinguished. A cross-check blind to the tag treats a survivor on a line
// the phase wrote the same as one it merely edited around.
func TestVerifyPromptWeightsSurvivorsByOrigin(t *testing.T) {
	prompt := verifyPromptBody(t)
	for _, phrase := range []string{
		"origin",
		mutation.OriginInHunk,
		mutation.OriginInherited,
	} {
		if !strings.Contains(prompt, phrase) {
			t.Errorf("verify.md never mentions %q — the cross-check cannot weight by origin", phrase)
		}
	}
}

// TestVerifyPromptReportBlockCarriesInScopeCount: §4's template is what the
// user actually reads. If it prints the score alone it contradicts
// printVerifySummary, which prints the sample size beside it — and the sample
// size is the locked substitute for a small-denominator threshold.
func TestVerifyPromptReportBlockCarriesInScopeCount(t *testing.T) {
	prompt := verifyPromptBody(t)
	if !strings.Contains(prompt, "in-scope mutants") {
		t.Error("verify.md §4 report block does not carry the in-scope mutant count")
	}
	if !strings.Contains(prompt, "mutants_in_scope") {
		t.Error("verify.md never tells the LLM to preserve mutants_in_scope in [summary]")
	}
}

// TestReadmeStatesScopingGuarantee: the README's `dross verify` row claimed the
// score covered everything the tools mutated. It has to say what is true now,
// or the docs describe the bug rather than the fix.
func TestReadmeStatesScopingGuarantee(t *testing.T) {
	b, err := os.ReadFile(filepath.Join(repoRootFromTest(t), "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	readme := string(b)

	row := ""
	for _, line := range strings.Split(readme, "\n") {
		if strings.Contains(line, "`dross verify <phase>`") {
			row = line
			break
		}
	}
	if row == "" {
		t.Fatal("README has no `dross verify <phase>` row")
	}
	for _, phrase := range []string{
		"only the phase's own changed files",
		"out_of_scope",
		verify.MutationOutOfScope,
	} {
		if !strings.Contains(row, phrase) {
			t.Errorf("README verify row missing %q:\n%s", phrase, row)
		}
	}
}
