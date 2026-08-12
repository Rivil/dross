package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// milestonePromptText reads assets/prompts/milestone.md verbatim. Unlike
// docText it keeps case, backticks and line structure — the assertions below
// are about where things sit in the document, not just whether the words occur.
func milestonePromptText(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(repoRootFromTest(t), "assets", "prompts", "milestone.md"))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// section returns the text from `heading` up to the next `## ` heading, so an
// assertion about one arm cannot be satisfied by wording that lives in another.
func section(t *testing.T, text, heading string) string {
	t.Helper()
	i := strings.Index(text, heading)
	if i < 0 {
		t.Fatalf("prompt has no %q heading", heading)
	}
	rest := text[i+len(heading):]
	if j := strings.Index(rest, "\n## "); j >= 0 {
		return rest[:j]
	}
	return rest
}

// TestMilestonePromptDispatchesBeforeScoping (c-4): scoping is an ARM of the
// dispatch, not the default path the command falls into. If the progress call
// came after the title question, the command would already have committed to
// scoping before it knew whether a milestone was live.
func TestMilestonePromptDispatchesBeforeScoping(t *testing.T) {
	text := milestonePromptText(t)
	call := strings.Index(text, "dross milestone progress --json")
	if call < 0 {
		t.Fatal("the prompt never runs `dross milestone progress --json`")
	}
	title := strings.Index(text, "## 2. Title")
	if title < 0 {
		t.Fatal("the prompt has no `## 2. Title` heading")
	}
	if call > title {
		t.Error("the dispatch call comes after the title question — scoping is the default path, not an arm")
	}
}

// TestMilestonePromptAllDoneArmGatesTheMerge: c-4 names the merge-commit gate
// explicitly. dross does not drive the merge, so this instruction is narration
// or it is nothing — and it has to live inside the arm that opens the PR, not
// somewhere else in the document.
func TestMilestonePromptAllDoneArmGatesTheMerge(t *testing.T) {
	arm := section(t, milestonePromptText(t), "## 8. Completion arm")
	for _, want := range []string{
		"dross milestone complete",
		"--finalize",
		"merge commit",
		"not a squash",
	} {
		if !strings.Contains(strings.ToLower(arm), strings.ToLower(want)) {
			t.Errorf("the completion arm does not state %q", want)
		}
	}
}

// TestMilestonePromptOutstandingArmOnlyReports: the arm for a live milestone
// with work left must report and stop. Falling through into scoping would
// re-walk success criteria the milestone already has.
func TestMilestonePromptOutstandingArmOnlyReports(t *testing.T) {
	dispatch := section(t, milestonePromptText(t), "## 0. Pre-flight")
	i := strings.Index(dispatch, "**otherwise**")
	if i < 0 {
		t.Fatal("the dispatch has no outstanding-work arm")
	}
	arm := dispatch[i:]
	for _, want := range []string{"remaining", "/dross-spec"} {
		if !strings.Contains(arm, want) {
			t.Errorf("the outstanding arm does not name %q", want)
		}
	}
	if !strings.Contains(arm, "**stop**") {
		t.Error("the outstanding arm does not stop")
	}
	// The two write verbs the scope arm is built out of. Reaching either from
	// here means the report fell through into scoping a milestone that is
	// already scoped and half-delivered.
	for _, forbidden := range []string{"dross milestone create", "dross milestone add"} {
		if strings.Contains(arm, forbidden) {
			t.Errorf("the outstanding arm reaches %q — it must report and stop", forbidden)
		}
	}
}

// TestMilestonePromptDoesNotDeriveDonenessItself: the CLI owns the definition
// of done. A prompt re-deriving it from `phase list` or a verify verdict would
// drift from the locked phases_done_test rule the moment either changed.
func TestMilestonePromptDoesNotDeriveDoneness(t *testing.T) {
	dispatch := section(t, milestonePromptText(t), "## 0. Pre-flight")
	low := strings.ToLower(dispatch)
	if !strings.Contains(low, "do not re-derive") && !strings.Contains(low, "don't re-derive") {
		t.Error("the dispatch does not forbid re-deriving doneness")
	}
	for _, src := range []string{"phase list", "verify.toml"} {
		if !strings.Contains(low, src) {
			t.Errorf("the dispatch does not rule out %q as a doneness source", src)
		}
	}
}

// TestMilestonePromptStatesArmPrecedence: a just-finalized milestone satisfies
// BOTH `status == complete` and `all_done`, with current_milestone still set.
// Without a stated order the run would try to complete a milestone it has
// already closed.
func TestMilestonePromptStatesArmPrecedence(t *testing.T) {
	dispatch := section(t, milestonePromptText(t), "## 0. Pre-flight")
	low := strings.ToLower(dispatch)
	if !strings.Contains(low, "status` first") && !strings.Contains(low, "status first") &&
		!strings.Contains(low, "on `status` first") {
		t.Error("the dispatch does not say status is branched on before all_done")
	}
	if !strings.Contains(low, "all_done") {
		t.Error("the dispatch never mentions all_done, so no precedence can be stated")
	}
}

// TestMilestonePromptNamesAlreadyFinalizedAndBranchGone: both are normal
// outcomes of the close-out. A prompt that does not name them reads either as a
// failure, and the natural response to a "failure" at that point is to start
// undoing a finalize that actually worked.
func TestMilestonePromptNamesFinalizeOutcomes(t *testing.T) {
	text := milestonePromptText(t)
	low := strings.ToLower(text)
	for _, want := range []string{
		"already finalized",
		"re-running `--finalize` is safe",
		"gone",
		"dross milestone set <version> status complete",
	} {
		if !strings.Contains(low, strings.ToLower(want)) {
			t.Errorf("the prompt does not name the finalize outcome %q", want)
		}
	}
}

// TestMilestonePromptNoMilestoneArmReadsTheExit: `milestone progress` exits
// non-zero when there is no current_milestone, and the dispatch has to read
// that as an arm rather than as a broken command.
func TestMilestonePromptNoMilestoneArmReadsTheExit(t *testing.T) {
	dispatch := section(t, milestonePromptText(t), "## 0. Pre-flight")
	i := strings.Index(dispatch, "non-zero")
	if i < 0 {
		t.Fatal("the dispatch never mentions the non-zero exit")
	}
	arm := dispatch[i:]
	if !strings.Contains(arm, "current_milestone") {
		t.Error("the no-milestone arm does not name the no-current_milestone message")
	}
	if !strings.Contains(strings.ToLower(arm), "not a broken command") &&
		!strings.Contains(strings.ToLower(arm), "is an arm") {
		t.Error("the dispatch does not say the non-zero exit is an arm rather than a failure")
	}
}

// TestMilestonePromptKeepsPreflightAndNoSentinel: the interaction-coverage and
// footer-audit gates both key off this file. The dispatch rewrite must not cost
// the pre-flight, and must not introduce a clear-point footer to a command
// enrolled as exempt from one.
func TestMilestonePromptKeepsPreflightAndNoSentinel(t *testing.T) {
	text := milestonePromptText(t)
	for _, want := range []string{"dross rule show", "dross interaction show"} {
		if !strings.Contains(text, want) {
			t.Errorf("the pre-flight no longer runs %q", want)
		}
	}
	if strings.Contains(text, "safe to /clear") {
		t.Error("milestone.md gained a clear-point sentinel — it is enrolled as footer-audit exempt")
	}
}
