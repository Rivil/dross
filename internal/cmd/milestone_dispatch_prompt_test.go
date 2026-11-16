package cmd

import (
	"io/fs"
	"regexp"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/Rivil/dross/assets"
)

// milestonePromptText is milestone.md as the agent loads it, the closeout
// partial inlined at its fetch line. Unlike docText it keeps case, backticks
// and line structure — the assertions below are about where things sit in the
// document, not just whether the words occur. The partial opens no heading, so
// section() still reads §8 to its end.
func milestonePromptText(t *testing.T) string {
	t.Helper()
	return expandedPrompt(t, "milestone.md")
}

// milestoneBasePrompt is milestone.md without its partials: where a fetch
// line sits against the steps around it is a fact of the base prompt.
func milestoneBasePrompt(t *testing.T) string {
	t.Helper()
	b, err := fs.ReadFile(promptFS, "prompts/milestone.md")
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
// of done. A prompt re-deriving it from a verify verdict or the shape of the
// phases array would drift from the locked phases_done_test rule the moment
// either changed.
//
// `dross phase list` left this forbidden list when it started reading doneness
// through the shared reader (internal/phase/done.go): it is now the same answer rendered
// per phase, not a second derivation of it.
func TestMilestonePromptDoesNotDeriveDoneness(t *testing.T) {
	dispatch := section(t, milestonePromptText(t), "## 0. Pre-flight")
	low := strings.ToLower(dispatch)
	if !strings.Contains(low, "do not re-derive") && !strings.Contains(low, "don't re-derive") {
		t.Error("the dispatch does not forbid re-deriving doneness")
	}
	for _, src := range []string{"verify.toml", "phases array"} {
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

// TestMilestonePromptCompletionArmChecksFirst (c-7): the arm runs the
// close-out check before it asks to close out and before it opens the PR, and
// a failing check fetches the closeout walk. A check after the confirm would
// ask the user to close a milestone the CLI is about to refuse.
func TestMilestonePromptCompletionArmChecksFirst(t *testing.T) {
	arm := section(t, milestoneBasePrompt(t), "## 8. Completion arm")
	check := strings.Index(arm, "\ndross milestone complete <version> --check\n")
	fetch := strings.Index(arm, "**When** `closeout=unresolved`")
	ask := strings.Index(arm, "AskUserQuestion")
	open := strings.Index(arm, "\ndross milestone complete <version>\n")
	if check < 0 {
		t.Fatal("the completion arm never runs `dross milestone complete <version> --check`")
	}
	if fetch < 0 {
		t.Fatal("the completion arm has no `closeout=unresolved` fetch line")
	}
	if line, _, _ := strings.Cut(arm[fetch:], "\n"); !strings.HasSuffix(line, "`dross prompt partial milestone closeout`") {
		t.Errorf("the closeout=unresolved line does not fetch the closeout partial: %q", line)
	}
	if ask < 0 || open < 0 {
		t.Fatalf("the completion arm lost its confirm (%t) or its bare open command (%t)", ask >= 0, open >= 0)
	}
	if !(check < fetch && fetch < ask && ask < open) {
		t.Errorf("want --check, then the closeout fetch, then the confirm, then the open; got check@%d fetch@%d ask@%d open@%d",
			check, fetch, ask, open)
	}

	common, err := assets.Fetched(promptFS, "milestone", assets.CommonPath())
	if err != nil {
		t.Fatal(err)
	}
	if len(common) != 0 {
		t.Errorf("a clean close-out fetches %v; want nothing", common)
	}
	facts := assets.CommonPath()
	facts["closeout"] = "unresolved"
	got, err := assets.Fetched(promptFS, "milestone", facts)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(got, ",") != "closeout" {
		t.Errorf("an unresolved close-out fetches %v; want [closeout]", got)
	}
}

// TestMilestonePromptCloseoutPartialWalksOnePerTurn (c-7): the walk takes one
// item per turn with a resolve and an acknowledge option, acks through the CLI
// with a reason, and loops on --check until it is clean before the PR opens.
// An unverified phase is never offered an ack (locked force_unverified).
func TestMilestonePromptCloseoutPartialWalksOnePerTurn(t *testing.T) {
	b, err := assets.Partial(promptFS, "milestone", "closeout")
	if err != nil {
		t.Fatal(err)
	}
	walk := string(b)
	if !strings.Contains(milestonePromptText(t), walk) {
		t.Fatal("the expanded milestone prompt does not inline the closeout partial")
	}
	for _, want := range []string{
		"**one per turn**",
		"AskUserQuestion",
		"**resolve it now**",
		"**acknowledge it**",
		"dross milestone ack <item> <version> --reason",
		"never invent one",
	} {
		if !strings.Contains(walk, want) {
			t.Errorf("the closeout walk lost %q", want)
		}
	}
	ack := strings.Index(walk, "dross milestone ack")
	recheck := strings.LastIndex(walk, "re-run `dross milestone complete <version> --check`")
	if ack < 0 || recheck < ack {
		t.Errorf("the walk must re-run --check after acting on an item; ack@%d re-check@%d", ack, recheck)
	} else {
		loop := walk[recheck:]
		for _, want := range []string{"until it exits 0", "Never open the integration PR while `--check` exits non-zero"} {
			if !strings.Contains(loop, want) {
				t.Errorf("after its re-check the walk does not say %q", want)
			}
		}
	}

	var unverified string
	for _, line := range strings.Split(walk, "\n") {
		if strings.HasPrefix(line, "- **Unverified**") {
			unverified = line
		}
	}
	if unverified == "" {
		t.Fatal("the walk has no `- **Unverified**` rule")
	}
	for _, want := range []string{"/dross-verify", "--force-unverified", "no acknowledgement"} {
		if !strings.Contains(unverified, want) {
			t.Errorf("the unverified rule does not name %q: %s", want, unverified)
		}
	}
	if strings.Contains(unverified, "dross milestone ack") {
		t.Errorf("the walk offers `dross milestone ack` for an unverified phase: %s", unverified)
	}
}

// milestoneSpanRe is an inline `dross milestone …` span.
var milestoneSpanRe = regexp.MustCompile("`(dross milestone [^`]+)`")

// milestoneRefs lists every `dross milestone …` invocation in text: inline
// spans, and the lines of fenced blocks.
func milestoneRefs(text string) []string {
	var out []string
	fenced := false
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			fenced = !fenced
			continue
		}
		if fenced {
			if strings.HasPrefix(trimmed, "dross milestone ") {
				out = append(out, trimmed)
			}
			continue
		}
		for _, m := range milestoneSpanRe.FindAllStringSubmatch(line, -1) {
			out = append(out, m[1])
		}
	}
	return out
}

// TestMilestonePromptCommandsResolve: every `dross milestone` subcommand and
// flag the expanded prompt runs resolves against the real command, in the
// TestProtectedPromptCommandsResolve pattern — a prompt edit has no compiler.
// The close-out ones must be among them, so the sweep cannot pass vacuously.
func TestMilestonePromptCommandsResolve(t *testing.T) {
	root := &cobra.Command{Use: "dross"}
	root.AddCommand(Milestone())
	flagRe := regexp.MustCompile(`(?:^|\s)--([a-z][a-z-]*)`)
	seen := map[string]bool{}
	for _, ref := range milestoneRefs(milestonePromptText(t)) {
		fields := strings.Fields(ref)
		if len(fields) < 3 {
			t.Errorf("milestone.md runs `%s`, which names no subcommand", ref)
			continue
		}
		sub := fields[2]
		c, _, err := root.Find([]string{"milestone", sub})
		if err != nil || c.Name() != sub {
			t.Errorf("milestone.md runs `%s`: %q is not a `dross milestone` subcommand", ref, sub)
			continue
		}
		seen[sub] = true
		for _, f := range flagRe.FindAllStringSubmatch(ref, -1) {
			if c.Flags().Lookup(f[1]) == nil {
				t.Errorf("milestone.md runs `%s`: --%s is not a flag of `dross milestone %s`", ref, f[1], sub)
				continue
			}
			seen[sub+" --"+f[1]] = true
		}
	}
	for _, want := range []string{"ack", "ack --reason", "complete --check", "complete --force-unverified", "complete --finalize"} {
		if !seen[want] {
			t.Errorf("milestone.md never runs `dross milestone %s`", want)
		}
	}
}
