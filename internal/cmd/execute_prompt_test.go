package cmd

import (
	"regexp"
	"strings"
	"testing"

	"github.com/Rivil/dross/assets"
	"github.com/Rivil/dross/internal/gate"
	"github.com/Rivil/dross/internal/review"
	"github.com/Rivil/dross/internal/runsummary"
)

// executePromptContent loads assets/prompts/execute.md and normalises it
// (lowercased, backticks/emphasis stripped) so assertions test for the presence
// of an instruction rather than its exact formatting.
func executePromptContent(t *testing.T) string {
	t.Helper()
	return normalisePrompt(expandedPrompt(t, "execute.md"))
}

// TestExecutePromptWiresPhaseNumber proves pc-3's "the version patch digit uses
// DisplayNumber" clause: `dross execute begin`, which absorbed the prompt's
// version step, sets the patch digit to what `dross phase number` prints for
// the phase rather than counting by hand.
func TestExecutePromptWiresPhaseNumber(t *testing.T) {
	executeFixture(t)
	writeBeginMilestone(t, repoDirHere(t), "v1.8", "a", "b", "p")
	mustRunState(t, "current_milestone", "v1.8")
	mustRunState(t, "version", "1.8.1.4")
	var num string
	if err := runCmdCapturing(t, &num, Phase(), "number", "p"); err != nil {
		t.Fatal(err)
	}
	want := "1.8." + strings.TrimSpace(num) + ".0"
	if want != "1.8.3.0" {
		t.Fatalf("phase number p = %q, want 3 — the fixture's array is a, b, p", num)
	}
	out, err := beginOut(t, "p")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "version: 1.8.1.4 -> "+want+"\n") {
		t.Errorf("begin printed %q, want the patch from `dross phase number` (%s)", out, want)
	}
}

// TestExecutePromptStaleBinaryStop: begin's version line is how the prompt
// knows the binary absorbed the steps §0 no longer spells out, so §0 must say a
// missing `version:` line is a stale binary to reinstall, not a skipped step.
func TestExecutePromptStaleBinaryStop(t *testing.T) {
	pre := strings.Join(strings.Fields(sectionOf(promptBody(t, "execute.md"), "## 0. Pre-flight", "## 1.")), " ")
	for _, want := range []string{"No `version:` line means a stale binary", "stop", "`make install`"} {
		if !strings.Contains(pre, want) {
			t.Errorf("§0 lost the stale-binary stop: missing %q", want)
		}
	}
	for _, gone := range []string{"dross state set version", "dross issue phase sync <id> --status in-progress"} {
		if strings.Contains(pre, gone) {
			t.Errorf("§0 still spells out %q, which `execute begin` now does", gone)
		}
	}
}

// TestExecutePromptInvokesLoadout proves c-4 end-to-end: the execute orchestration
// must call `dross stack loadout` and inject the block. If the invocation is
// removed from execute.md, this fails.
func TestExecutePromptInvokesLoadout(t *testing.T) {
	content := executePromptContent(t)
	for _, needle := range []string{"dross stack loadout", "inject"} {
		if !strings.Contains(content, needle) {
			t.Errorf("execute.md must wire the stack loadout: missing %q", needle)
		}
	}
}

// executeGateSection extracts the §1g post-commit gate section from the
// normalised prompt. Failing to find it at all is the primary regression this
// guards: the gate must be POST-commit (its own section after 1f), not grafted
// onto the §1c pre-task approach approval.
func executeGateSection(t *testing.T) string {
	t.Helper()
	content := executePromptContent(t)
	start := strings.Index(content, "### 1g. post-commit gate")
	if start == -1 {
		t.Fatal("execute.md must have a '### 1g. Post-commit gate' section after 1f — a checkpoint option on the pre-task approach gate does not satisfy c-2")
	}
	section := content[start:]
	if end := strings.Index(section, "\n## "); end != -1 {
		section = section[:end]
	}
	return section
}

// TestExecutePromptPostCommitGateOptions proves c-2's gate shape: the
// post-commit gate enumerates continue, stop, AND checkpoint.
func TestExecutePromptPostCommitGateOptions(t *testing.T) {
	section := executeGateSection(t)
	for _, opt := range []string{"continue", "stop", "checkpoint"} {
		if !strings.Contains(section, opt) {
			t.Errorf("post-commit gate must enumerate %q", opt)
		}
	}
}

// TestExecutePromptCheckpointConsistencyBeforeReentry proves c-2's ordering:
// checkpoint instructs the plan.toml/state consistency check (dross validate +
// dross task next) BEFORE emitting the `--from <next-task>` re-entry command.
func TestExecutePromptCheckpointConsistencyBeforeReentry(t *testing.T) {
	section := executeGateSection(t)
	validateIdx := strings.Index(section, "dross validate")
	reentryIdx := strings.Index(section, "--from <next-task>")
	if validateIdx == -1 {
		t.Fatal("checkpoint must confirm consistency via dross validate")
	}
	if reentryIdx == -1 {
		t.Fatal("checkpoint must emit the /dross-execute --from <next-task> re-entry command")
	}
	if validateIdx > reentryIdx {
		t.Error("consistency check must be instructed BEFORE the re-entry command is emitted")
	}
}

// TestExecutePromptCheckpointWaveLead proves the checkpoint_posture locked
// decision: at wave boundaries the gate leads with checkpoint as the
// recommended option. Below the context threshold that order is unchanged
// (context-boundaries c-4): mid-wave still leads with continue, and the
// checkpoint bullet calls exactly the two consistency commands — the context
// rule adds no call to it (signal_path).
func TestExecutePromptCheckpointWaveLead(t *testing.T) {
	section := executeGateSection(t)
	if !strings.Contains(section, "wave") {
		t.Fatal("post-commit gate must special-case wave boundaries")
	}
	if !strings.Contains(section, "lead with checkpoint") {
		t.Error("wave-boundary branch must mark checkpoint as the lead/recommended option")
	}
	if !strings.Contains(section, "mid-wave, lead with continue") {
		t.Error("mid-wave must still lead with continue below the context threshold")
	}

	_, bullet, ok := strings.Cut(section, "\n- checkpoint —")
	if !ok {
		t.Fatal("post-commit gate has no `checkpoint` bullet")
	}
	bullet, _, _ = strings.Cut(bullet, "\n- stop")
	calls := map[string]bool{}
	for _, m := range regexp.MustCompile(`dross ([a-z]+)( next)?`).FindAllStringSubmatch(bullet, -1) {
		calls[m[1]+m[2]] = true
	}
	if len(calls) != 2 || !calls["validate"] || !calls["task next"] {
		t.Errorf("checkpoint bullet calls %v; want exactly dross validate and dross task next", calls)
	}
}

// TestExecutePromptEmitsTypedLandmark proves c-1's producer side: execute.md
// records the landmark through the typed `--landmark feature=…, symbol=…, loc=…,
// what=…` flag and no longer through the legacy `--notes "feature: …"` form. If
// the prompt regresses to the notes-string landmark, the forbidden token returns
// and this fails. (r-01: gates the source prompt directly, independent of install.)
func TestExecutePromptEmitsTypedLandmark(t *testing.T) {
	content := executePromptContent(t)
	for _, needle := range []string{"--landmark", "feature=", "symbol=", "loc=", "what="} {
		if !strings.Contains(content, needle) {
			t.Errorf("execute.md must emit the typed landmark: missing %q", needle)
		}
	}
	if strings.Contains(content, `--notes "feature:`) {
		t.Error("execute.md must not encode the landmark in --notes (legacy `--notes \"feature: …\"` form survived)")
	}
}

// TestExecutePromptOffloadGuidance pins execute.md §1b's offload passage
// (subagent-offload-audit c-3/c-4): code insight fans out read-only
// subagents when the file surface is large, and the passage keeps both
// halves of the contract — the size gate (offload_posture lock:
// conditional, never mandatory) and the agent-gate boundary (only the
// main loop writes code).
func TestExecutePromptOffloadGuidance(t *testing.T) {
	prompt := executePromptContent(t)

	for _, phrase := range []string{
		"offload the reading when the task's file surface is large",
		"read-only subagents",
		"never edit files or commit; only the main loop writes code",
		"for a typical small task, stay inline",
		"docs/subagent-offload-audit.md",
	} {
		if !strings.Contains(prompt, phrase) {
			t.Errorf("execute.md lost its offload guidance phrase %q", phrase)
		}
	}
}

// TestExecutePromptDocumentsPrepareExit is the agent-facing half of c-3.
//
// exitPrepareFailed is worth nothing if the reader of the exit status does not
// know what 7 means. An agent that fell through to "non-zero, and not one of
// the codes I know" reads a failed bootstrap as a red suite and goes hunting a
// bug in code that was never executed — the exact failure the code was split
// out to prevent. Both halves are asserted: the entry itself, and its
// membership in the line that says which codes mean the run did not happen.
// (r-01: the prompt edit is only live after `make install`.)
func TestExecutePromptDocumentsPrepareExit(t *testing.T) {
	content := executePromptContent(t)
	if !strings.Contains(content, "- 7 —") {
		t.Error("execute.md's exit-code list has no entry for 7 — an agent would read a failed prepare as a red suite")
	}
	if !strings.Contains(content, "prepare") {
		t.Error("execute.md's exit-code list never names the prepare command")
	}
	// The literal is pinned to the CURRENT list. The drift-proof version of
	// this — every declared exit code, read out of test.go — is
	// TestExecuteSummaryNamesEveryNonRedCode; this one stays because it is
	// what names 7 specifically, and 7's whole point is not reading as a red.
	if !strings.Contains(content, "2, 3, 4, 5, 6, 7 and 8 all mean the run did not happen") {
		t.Error("execute.md's did-not-happen line still omits 7, so 7 reads as a verdict about the code")
	}
}

// TestExecutePromptPullsBoardTaskMoves guards the inbound half of board task
// sync. The outbound calls (`dross issue task sync`) have been wired since the
// task loop existed, but nothing ever called `dross issue task pull`, so a card
// moved on the board never reached plan.toml and `dross task next` picked from a
// stale plan. Removing the edge again fails this.
//
// The `--apply` needle matters as much as the verb: the bare command only
// reports, so an edge that never names `--apply` can surface drift but can never
// close it. (r-01: the prompt edit is only live after `make install`.)
func TestExecutePromptPullsBoardTaskMoves(t *testing.T) {
	content := executePromptContent(t)
	for _, needle := range []string{"dross issue task pull", "--apply"} {
		if !strings.Contains(content, needle) {
			t.Errorf("execute.md must wire the inbound board task pull: missing %q", needle)
		}
	}
	// The apply path is fetched only when begin's pull: line reports a move.
	common, err := assets.Fetched(promptFS, "execute", assets.CommonPath())
	if err != nil {
		t.Fatal(err)
	}
	facts := assets.CommonPath()
	facts["board_pull"] = "reported"
	reported, err := assets.Fetched(promptFS, "execute", facts)
	if err != nil {
		t.Fatal(err)
	}
	if contains(common, "board-moves") || !contains(reported, "board-moves") {
		t.Errorf("board-moves fetched %v on the common path and %v on board_pull=reported; want only the latter", common, reported)
	}
}

// TestExecutePromptOffersApproveLabel: the pair-approval gate records an
// approval only when the answer is exactly gate.ApproveLabel(task), so §1c
// must offer that label — built from the same function, so a format drift on
// either side fails here — in place of the old bare `proceed`, alongside the
// steer / show me / skip reactions. The hard rule must name it too.
func TestExecutePromptOffersApproveLabel(t *testing.T) {
	body := promptBody(t, "execute.md")
	label := "`" + gate.ApproveLabel("<task-id>") + "`"
	c := sectionOf(body, "### 1c.", "### ")
	if c == "" {
		t.Fatal("execute.md has no §1c section")
	}
	if !strings.Contains(c, "- "+label) {
		t.Errorf("§1c does not offer the option %s", label)
	}
	for _, opt := range []string{"`steer`", "`show me <X>`", "`skip`"} {
		if !strings.Contains(c, "- "+opt) {
			t.Errorf("§1c lost the %s option", opt)
		}
	}
	if strings.Contains(c, "`proceed`") {
		t.Error("§1c still offers a bare `proceed` — the gate records only the approve label")
	}
	rules := sectionOf(body, "## Hard rules", "## ")
	if !strings.Contains(rules, label) || strings.Contains(rules, "`proceed`") {
		t.Errorf("the pair-mode hard rule must name %s, not `proceed`", label)
	}
}

// TestExecutePromptBeginsBeforeLoop: the pair-approval gate reads the mode
// `dross execute begin` records, so the prompt must record it — both
// branches — before the per-task loop writes anything.
func TestExecutePromptBeginsBeforeLoop(t *testing.T) {
	body := promptBody(t, "execute.md")
	loop := strings.Index(body, "## 1. Per-task loop")
	if loop < 0 {
		t.Fatal("execute.md has no \"## 1. Per-task loop\" heading")
	}
	for _, line := range []string{"dross execute begin <id>\n", "dross execute begin <id> --solo\n"} {
		if i := strings.Index(body, line); i < 0 || i > loop {
			t.Errorf("%q must appear before the per-task loop (at %d, loop at %d)", strings.TrimSpace(line), i, loop)
		}
	}
}

// TestExecutePromptLaneCommitRunsFullSuite: in a laned repo §1e's --files run
// records no green, so a commit straight after it would be refused. §1f must
// run a bare `dross test` — the full run the commit gate counts — before the
// task's files are staged.
func TestExecutePromptLaneCommitRunsFullSuite(t *testing.T) {
	f := sectionOf(promptBody(t, "execute.md"), "### 1f.", "### ")
	if f == "" {
		t.Fatal("execute.md has no §1f section")
	}
	if !strings.Contains(f, "[[runtime.test_lane]]") {
		t.Error("§1f must say when the extra full run is needed: when [[runtime.test_lane]] is declared")
	}
	add := strings.Index(f, "git add <task.files>")
	test := bareDrossTestRE.FindStringIndex(f)
	if add < 0 || test == nil || test[0] > add {
		t.Errorf("§1f must run a bare `dross test` before `git add <task.files>` (test at %v, add at %d)", test, add)
	}
}

// soloReview is execute.md §1f, and the solo review block inside it.
func soloReview(t *testing.T) (string, string) {
	t.Helper()
	f := expandedPrompt(t, "_execute.solo.md")
	i := strings.Index(f, "**Solo review (`--solo` only).**")
	j := strings.Index(f, "The commit gate checks this too")
	if i < 0 || j < i {
		t.Fatal("the solo partial has no solo review block")
	}
	return f, f[i:j]
}

// rawExecuteBase is execute.md without its partials, read through assets.FS:
// the text every run loads, solo or pair.
func rawExecuteBase(t *testing.T) string {
	t.Helper()
	b, err := assets.FS.ReadFile("prompts/execute.md")
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func mustIdx(t *testing.T, s, sub string) int {
	t.Helper()
	i := strings.Index(s, sub)
	if i < 0 {
		t.Fatalf("missing %q", sub)
	}
	return i
}

// TestExecuteSoloReviewPlacement (c-1): in a solo run the reviewer runs after
// the full green `dross test` and before the commit; pair mode spawns none.
func TestExecuteSoloReviewPlacement(t *testing.T) {
	f := sectionOf(rawExecuteBase(t), "### 1f.", "### ")
	test := bareDrossTestRE.FindStringIndex(f)
	if test == nil {
		t.Fatal("§1f lost its bare `dross test`")
	}
	refetch, add := mustIdx(t, f, "**Re-fetch** if context was compacted: `dross prompt partial execute solo`"), mustIdx(t, f, "git add <task.files>")
	if !(test[0] < refetch && refetch < add) {
		t.Errorf("§1f order: dross test %d, solo re-fetch %d, git add %d — want that order", test[0], refetch, add)
	}
	_, block := soloReview(t)
	run, ctx, spawn, status := mustIdx(t, block, "`dross test`"), mustIdx(t, block, "dross review context"), mustIdx(t, block, `subagent_type: "`+review.ReviewerAgent+`"`), mustIdx(t, block, "dross review status")
	if !(run < ctx && ctx < spawn && spawn < status) {
		t.Errorf("solo review order: dross test %d, review context %d, spawn %d, review status %d — want that order", run, ctx, spawn, status)
	}
	if !strings.Contains(block, "In **pair mode skip this entirely**: the human is the gate.") {
		t.Error("the solo review block no longer says pair mode skips it")
	}
	c := sectionOf(promptBody(t, "execute.md"), "### 1c.", "### ")
	if strings.Contains(c, "dross review") || strings.Contains(c, review.ReviewerAgent) {
		t.Error("§1c (the pair approval step) spawns or prepares the reviewer")
	}
}

// TestExecuteSoloFetchedOnceAfterBegin (locked solo_branch): the solo partial
// is fetched once per run, in §0, and the first fetch after the solo begin
// fence is that one — so a pair run never pays for it.
func TestExecuteSoloFetchedOnceAfterBegin(t *testing.T) {
	base := rawExecuteBase(t)
	fence := strings.Index(base, "dross execute begin <id> --solo\n")
	loop := strings.Index(base, "## 1. Per-task loop")
	if fence < 0 || loop < fence {
		t.Fatal("execute.md lost the solo begin fence before the loop")
	}
	fenceLine := strings.Count(base[:fence], "\n") + 1
	points, err := assets.FetchPoints(assets.FS, "execute.md")
	if err != nil {
		t.Fatal(err)
	}
	var first *assets.FetchPoint
	solo := 0
	for i, p := range points {
		if p.Partial == "solo" && !p.Refetch {
			solo++
		}
		if first == nil && !p.Refetch && p.Line > fenceLine {
			first = &points[i]
		}
	}
	if first == nil || first.Partial != "solo" || first.Key != "mode" || first.Value != "solo" {
		t.Errorf("first fetch after the solo begin fence is %+v, want `mode=solo` → solo", first)
	}
	if first != nil && first.Line > strings.Count(base[:loop], "\n") {
		t.Errorf("the solo fetch (line %d) sits past §0", first.Line)
	}
	if solo != 1 {
		t.Errorf("execute.md has %d When lines fetching solo, want exactly 1", solo)
	}
}

// TestExecuteReviewerSpawnForeground: the spawn carries the reviewer's exact
// name and the printed line as the whole prompt. Since review_pass_signal's
// amendment a background spawn records through its SubagentStop, so the step
// no longer demands a foreground spawn — it waits for the reviewer to finish.
func TestExecuteReviewerSpawnForeground(t *testing.T) {
	_, block := soloReview(t)
	var spawn string
	for _, para := range strings.Split(block, "\n") {
		if strings.Contains(para, "subagent_type:") {
			spawn = para
		}
	}
	for _, want := range []string{`subagent_type: "` + review.ReviewerAgent + `"`, "**verbatim** as the whole prompt", "Add nothing to it", "**wait for its completion notice**"} {
		if !strings.Contains(spawn, want) {
			t.Errorf("the spawn step lost %q:\n%s", want, spawn)
		}
	}
}

// TestExecuteReviewFailurePath (c-3): one fix round, re-tested before the
// re-review; a still-failing review sets the code aside before marking the
// task failed, and the loop moves on.
func TestExecuteReviewFailurePath(t *testing.T) {
	_, block := soloReview(t)
	blocked := block[mustIdx(t, block, "- `blocked`"):mustIdx(t, block, "- `pass-stale`")]
	if !strings.Contains(blocked, "**one fix round**, never more") {
		t.Error("the blocked branch no longer caps the fix round at one")
	}
	if mustIdx(t, blocked, "re-run `dross test`") > mustIdx(t, blocked, "dross review context") {
		t.Error("the fix round re-reviews before re-running `dross test`")
	}
	fail := block[mustIdx(t, block, "- `exhausted` or `unavailable`"):]
	stash, failed, next := mustIdx(t, fail, "git stash push -u -- . ':(exclude).dross'"), mustIdx(t, fail, "dross task status <phase> <task-id> failed"), mustIdx(t, fail, "dross task next <phase>")
	if !(stash < failed && failed < next) {
		t.Errorf("failure path order: stash %d, failed %d, task next %d — want stash, then failed, then next", stash, failed, next)
	}
}

// TestExecuteReviewSpawnError (review_unavailable): an errored spawn records
// nothing, so the prompt itself must route it to a failed task.
func TestExecuteReviewSpawnError(t *testing.T) {
	_, block := soloReview(t)
	fail := block[mustIdx(t, block, "- `exhausted` or `unavailable`"):]
	for _, want := range []string{"**or the spawn itself errored**", `--reason "<the error>"`} {
		if !strings.Contains(fail, want) {
			t.Errorf("the failure path lost %q", want)
		}
	}
}

func TestExecuteNoCodeSkipsReview(t *testing.T) {
	_, block := soloReview(t)
	if !strings.Contains(block, "A task that changed only `.dross/` (no code) also skips it") {
		t.Error("the solo review block no longer skips .dross/-only tasks")
	}
}

// TestExecuteSoloRedStashes: the red-test solo path must stash before
// marking failed — `failed` refuses while a solo task's code is uncommitted.
func TestExecuteSoloRedStashes(t *testing.T) {
	solo := expandedPrompt(t, "_execute.solo.md")
	red := solo[mustIdx(t, solo, "**Red, solo mode**"):]
	if mustIdx(t, red, "git stash push -u -- . ':(exclude).dross'") > mustIdx(t, red, "dross task status <phase> <task-id> failed") {
		t.Error("the solo red path marks failed before stashing")
	}
}

func TestExecutePromptSoloBeginNeedsReviewer(t *testing.T) {
	pre := sectionOf(rawExecuteBase(t), "## 0. Pre-flight", "## 1.")
	if !strings.Contains(pre, "A `--solo` begin refuses unless the solo task reviewer") || !strings.Contains(pre, "`dross install`") {
		t.Error("§0 no longer routes a refused solo begin to `dross install`")
	}
}

// rawPrompt reads assets/prompts/<name> unnormalised.
func rawPrompt(t *testing.T, name string) string {
	t.Helper()
	return expandedPrompt(t, name)
}

// testStepSection is the text from the line starting with from up to the next line
// starting with to, whitespace-normalised so a phrase wrapped across lines
// still matches.
func testStepSection(t *testing.T, s, from, to string) string {
	t.Helper()
	i := strings.Index(s, "\n"+from)
	if i < 0 {
		t.Fatalf("no %q heading", from)
	}
	rest := s[i+1:]
	if j := strings.Index(rest, "\n"+to); j >= 0 {
		rest = rest[:j]
	}
	return strings.Join(strings.Fields(rest), " ")
}

// assertTestStepDescribesSummary is the shared c-3 check: the test step tells
// the agent `dross test` prints a summary and keeps the log, quotes the marker
// a cut failure list ends with, and no longer promises streaming or a tail.
func assertTestStepDescribesSummary(t *testing.T, name, step string) {
	t.Helper()
	whole := strings.Join(strings.Fields(rawPrompt(t, name)), " ")
	for _, gone := range []string{"streaming output as it arrives", "last 30-40 lines"} {
		if strings.Contains(whole, gone) {
			t.Errorf("%s still says %q", name, gone)
		}
	}
	for _, want := range []string{".dross/logs/", runsummary.MoreFailingPrefix} {
		if !strings.Contains(step, want) {
			t.Errorf("%s's test step does not name %q", name, want)
		}
	}
}

func TestExecuteTestStepDescribesSummary(t *testing.T) {
	assertTestStepDescribesSummary(t, "execute.md", testStepSection(t, rawPrompt(t, "execute.md"), "### 1e.", "### 1f."))
}

// TestGreenPathNeverReadsLog: a green run's summary is the whole story — no
// prompt sends the agent to read the run log on green.
func TestGreenPathNeverReadsLog(t *testing.T) {
	read := regexp.MustCompile(`(?i)\b(cat|read|open)\b`)
	for _, name := range []string{"execute.md", "quick.md"} {
		raw := rawPrompt(t, name)
		i := strings.Index(raw, "\n**Green**")
		if i < 0 {
			t.Fatalf("%s has no Green branch", name)
		}
		green := raw[i+1:]
		if j := strings.Index(green, "\n**Red"); j >= 0 {
			green = green[:j]
		}
		if strings.Contains(green, ".dross/logs") && read.MatchString(green) {
			t.Errorf("%s's Green branch sends the agent to read the run log:\n%s", name, green)
		}
	}
}

// TestTestStepPromptsNotGrown: the rewrite costs no context — both prompts are
// no larger than at the phase's base commit 54ad9064.
func TestTestStepPromptsNotGrown(t *testing.T) {
	for name, limit := range map[string]int{"execute.md": 25854, "quick.md": 18102} {
		if n := len(rawPrompt(t, name)); n > limit {
			t.Errorf("%s is %d bytes, over its %d-byte base size", name, n, limit)
		}
	}
}

// TestExecuteAbsorbedProseDeleted: §0's version, sync and pull mechanics went
// to `execute begin`, so they were deleted rather than moved — expanded
// execute.md keeps that room for the fetch lines later splits add.
func TestExecuteAbsorbedProseDeleted(t *testing.T) {
	const limit = 25500
	if n := len(rawPrompt(t, "execute.md")); n > limit {
		t.Errorf("expanded execute.md is %d bytes, over %d — was absorbed §0 prose moved instead of deleted?", n, limit)
	}
}

// soloWriterBranch is the solo partial's writer dispatch block: from its
// lead-in up to the solo red path.
func soloWriterBranch(t *testing.T) string {
	t.Helper()
	solo := expandedPrompt(t, "_execute.solo.md")
	i := strings.Index(solo, "**Writer dispatch (`--solo` only).**")
	j := strings.Index(solo, "**Red, solo mode**")
	if i < 0 || j < i {
		t.Fatal("the solo partial has no writer dispatch block before its red path")
	}
	return solo[i:j]
}

// TestSoloWriterDispatch (c-2): the main loop asks `dross task dispatch` before
// it spawns anything, and spawns the writer with the printed prompt verbatim,
// in the background.
func TestSoloWriterDispatch(t *testing.T) {
	w := soloWriterBranch(t)
	dispatch, spawn := mustIdx(t, w, "dross task dispatch <phase> <task-id>"), mustIdx(t, w, "the printed `subagent_type`")
	if dispatch > spawn {
		t.Errorf("the writer is spawned (%d) before `dross task dispatch` runs (%d)", spawn, dispatch)
	}
	for _, want := range []string{"`dispatch: inline (…)` → §1b–§1e as written", "the printed `prompt:` line **verbatim** as the whole prompt", "`run_in_background: true`", "no `model`", "**Wait for its completion notice**"} {
		if !strings.Contains(w, want) {
			t.Errorf("the writer branch lost %q", want)
		}
	}
}

// TestSoloWriterGatesStayInMainLoop (c-3): a green writer is followed, in the
// main loop and in order, by the bare suite, the cold review and the commit;
// its own green is never the gate.
func TestSoloWriterGatesStayInMainLoop(t *testing.T) {
	w := soloWriterBranch(t)
	green := w[mustIdx(t, w, "- `test: green`"):mustIdx(t, w, "**Writer failed**")]
	test, ctx, commit := mustIdx(t, green, "bare `dross test`"), mustIdx(t, green, "`dross review context`"), mustIdx(t, green, "the commit")
	if !(test < ctx && ctx < commit) {
		t.Errorf("writer-green order: dross test %d, review context %d, commit %d — want that order", test, ctx, commit)
	}
	for _, want := range []string{"§1f in the main loop", "The writer's green certifies nothing."} {
		if !strings.Contains(green, want) {
			t.Errorf("the writer-green arm lost %q", want)
		}
	}
}

// TestSoloWriterReportOnly (c-2): the main loop reads the writer's report and
// nothing else — no task file, no diff, none of the inline steps.
func TestSoloWriterReportOnly(t *testing.T) {
	w := soloWriterBranch(t)
	for _, want := range []string{"skip §1b–§1e", "read only its `dross-writer-report` block", "never Read the task's files, never `git diff`"} {
		if !strings.Contains(w, want) {
			t.Errorf("the writer branch lost %q", want)
		}
	}
	writer := w[mustIdx(t, w, "- `dispatch: writer`"):]
	// What is left once the allowed, negated mentions are cut must never send
	// the main loop into the code.
	rest := strings.NewReplacer("skip §1b–§1e", "", "never Read the task's files, never `git diff`", "").Replace(writer)
	for _, gone := range []string{"git diff", "Read the task", "task.files", "§1b", "§1d"} {
		if strings.Contains(rest, gone) {
			t.Errorf("the writer branch sends the main loop to %q", gone)
		}
	}
}

// TestSoloWriterFailurePath (c-5): every way a writer fails — spawn error, red,
// no report, a red suite after it, a failed fix round — fails the task by
// stashing and then marking it failed with a writer reason; none is fixed
// inline.
func TestSoloWriterFailurePath(t *testing.T) {
	w := soloWriterBranch(t)
	fail := w[mustIdx(t, w, "**Writer failed**"):mustIdx(t, w, "- A `blocked` review")]
	for _, want := range []string{"the spawn errored", "the report says `test: red`", "there is no report", "the bare `dross test` after it is red", "never fix it inline"} {
		if !strings.Contains(fail, want) {
			t.Errorf("the writer failure arm lost %q", want)
		}
	}
	stash, failed, next := mustIdx(t, fail, "git stash push -u -- . ':(exclude).dross'"), mustIdx(t, fail, `dross task status <phase> <task-id> failed --reason "writer: `), mustIdx(t, fail, "dross task next <phase>")
	if !(stash < failed && failed < next) {
		t.Errorf("writer failure order: stash %d, failed %d, task next %d — want that order", stash, failed, next)
	}
	round := w[mustIdx(t, w, "- A `blocked` review"):]
	for _, want := range []string{"goes to the same writer, never inline", "load `SendMessage` via ToolSearch", "SendMessage unavailable, a red report or no reply → **Writer failed**"} {
		if !strings.Contains(round, want) {
			t.Errorf("the writer fix round lost %q", want)
		}
	}
	for _, inline := range []string{"one bounded fix", "fix here", "Edit pass"} {
		if strings.Contains(w, inline) {
			t.Errorf("the writer branch offers an inline fix: %q", inline)
		}
	}
}

// TestPairPromptHasNoWriter (c-4): pair mode never meets the writer — not in
// base execute.md, not in any partial a pair run can fetch.
func TestPairPromptHasNoWriter(t *testing.T) {
	texts := map[string]string{"execute.md": rawExecuteBase(t)}
	points, err := assets.FetchPoints(promptFS, "execute.md")
	if err != nil {
		t.Fatal(err)
	}
	pair := 0
	for _, p := range points {
		if p.Refetch || (p.Key == "mode" && p.Value != "pair") {
			continue
		}
		b, err := assets.Partial(promptFS, "execute", p.Partial)
		if err != nil {
			t.Fatal(err)
		}
		texts["_execute."+p.Partial+".md"] = string(b)
		pair++
	}
	if pair == 0 {
		t.Fatal("no execute partial is reachable in pair mode — the check went vacuous")
	}
	for name, s := range texts {
		for _, gone := range []string{review.WriterAgent, "task dispatch"} {
			if strings.Contains(s, gone) {
				t.Errorf("%s, loaded in pair mode, mentions %q", name, gone)
			}
		}
	}
}

// TestRecoveryWriterInFlight (c-5): a resumed run whose task still has a
// launched writer fails it — stash, then failed — and never takes it over.
func TestRecoveryWriterInFlight(t *testing.T) {
	r := expandedPrompt(t, "_execute.recovery.md")
	arm := r[mustIdx(t, r, "- **A writer still in flight**"):]
	arm = arm[:mustIdx(t, arm, "- **Dirty tree**")]
	for _, want := range []string{".dross/gate/writer.json", `"state": "launched"`, "Never resume the task inline"} {
		if !strings.Contains(arm, want) {
			t.Errorf("the writer-in-flight arm lost %q", want)
		}
	}
	stash, failed := mustIdx(t, arm, "git stash push -u -- . ':(exclude).dross'"), mustIdx(t, arm, `dross task status <phase> <task-id> failed --reason "writer: `)
	if stash > failed {
		t.Error("the writer-in-flight arm marks failed before stashing")
	}
}
