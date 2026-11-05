package cmd

import (
	"io/fs"
	"strings"
	"testing"

	"github.com/Rivil/dross/assets"
	"github.com/Rivil/dross/internal/review"
)

// quickPromptContent loads quick.md expanded with its partials and normalises
// it with normaliseKeepUnderscores, so assertions test the presence of a rule,
// not its exact formatting.
func quickPromptContent(t *testing.T) string {
	t.Helper()
	return normaliseKeepUnderscores(expandedPrompt(t, "quick.md"))
}

// normaliseKeepUnderscores lowercases s and strips backticks and asterisk
// emphasis. Unlike normalisePrompt it does NOT strip underscores: the quick
// needles are store keys (quick_base, current_phase_status) and stripping them
// would mangle the thing being asserted.
func normaliseKeepUnderscores(s string) string {
	return strings.NewReplacer("`", "", "*", "").Replace(strings.ToLower(s))
}

// TestQuickPromptRecordsStandaloneBase (c-7) gates the half of the quick base
// record that lives in the prompt: a standalone quick task must record the
// branch it committed to, and the in-phase arm must NOT — a phase's base is
// already owned by its phase-scoped changes.json, and writing the machine-local
// quick_base there too would leave two records to disagree.
//
// The arms are split on the same line in quick.md's pre-flight step 4, so the
// needle check is per-line rather than over the whole document.
func TestQuickPromptRecordsStandaloneBase(t *testing.T) {
	content := quickPromptContent(t)

	const needle = "local set quick_base"

	if !strings.Contains(content, needle) {
		t.Fatalf("quick.md must tell a standalone quick task to record its base (%q)", needle)
	}

	// Scope to the pre-flight section — "standalone" and "in-phase" recur
	// later in the prompt, and only step 4's arms are the subject here.
	preflight, _, ok := strings.Cut(content, "\n## 1.")
	if !ok {
		t.Fatal("quick.md no longer has a section after pre-flight to cut at")
	}

	var standalone, inPhase string
	for _, line := range strings.Split(preflight, "\n") {
		switch {
		// Prefix match, not the whole parenthetical: both arms carry
		// qualifiers now (the in-phase arm excludes a shipped phase, the
		// standalone arm absorbs it) and those will keep evolving.
		case strings.Contains(line, "standalone (no current_phase"):
			standalone = line
		case strings.Contains(line, "in-phase (current_phase set"):
			inPhase = line
		}
	}
	if standalone == "" || inPhase == "" {
		t.Fatalf("quick.md pre-flight step 4 no longer has both branch arms (standalone=%t in-phase=%t)",
			standalone != "", inPhase != "")
	}
	if !strings.Contains(standalone, needle) {
		t.Errorf("the standalone arm must record the base: %q", standalone)
	}
	if strings.Contains(inPhase, needle) {
		t.Errorf("the in-phase arm must NOT record quick_base — changes.json owns it: %q", inPhase)
	}
}

// rawQuickBase is quick.md without its partials, read through assets.FS: the
// text every quick loads, pair or solo.
func rawQuickBase(t *testing.T) string {
	t.Helper()
	b, err := assets.FS.ReadFile("prompts/quick.md")
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// quickSoloReview returns quick.md as loaded and the solo partial's review
// block: from its lead-in up to the §5 solo arm.
func quickSoloReview(t *testing.T) (string, string) {
	t.Helper()
	solo := expandedPrompt(t, "_quick.solo.md")
	i := strings.Index(solo, "**Solo review (`--solo` only).**")
	j := strings.Index(solo, "**§5, solo**")
	if i < 0 || j < i {
		t.Fatal("the quick solo partial has no solo review block before its §5 arm")
	}
	return promptBody(t, "quick.md"), solo[i:j]
}

// TestQuickSoloReview (c-8): a solo quick is reviewed after its green test and
// before its commit; pair mode spawns no reviewer. The review lives in the solo
// partial; base §4 marks its place with a re-fetch between the green `dross
// test` and §5.
func TestQuickSoloReview(t *testing.T) {
	body, block := quickSoloReview(t)
	run, ctx := mustIdx(t, block, "after the green `dross test`, before the commit"), mustIdx(t, block, "dross review context")
	spawn := mustIdx(t, block, `subagent_type: "`+review.ReviewerAgent+`"`)
	status, commit := mustIdx(t, block, "dross review status"), mustIdx(t, block, "`pass` → commit (§5)")
	if !(run < ctx && ctx < spawn && spawn < status && status < commit) {
		t.Errorf("solo quick review order: dross test %d, review context %d, spawn %d, review status %d, commit %d — want that order", run, ctx, spawn, status, commit)
	}
	// Each fix-round arm re-runs the test before it rebuilds the review.
	for _, arm := range []string{"- `blocked` →", "- `pass-stale` →"} {
		line, _, _ := strings.Cut(block[mustIdx(t, block, arm):], "\n")
		rerun, rebuild := strings.Index(line, "re-run `dross test`"), strings.Index(line, "`dross review context`")
		if rerun < 0 || rebuild < rerun {
			t.Errorf("the %s arm lost its `dross test` re-run ahead of the review context: %q", arm, line)
		}
	}
	if !strings.Contains(block, "In **pair mode skip this entirely**") {
		t.Error("the quick's solo review block no longer says pair mode skips it")
	}
	if strings.Count(body, review.ReviewerAgent) != strings.Count(block, review.ReviewerAgent) {
		t.Error("quick.md spawns the reviewer outside its solo review block")
	}

	base := rawQuickBase(t)
	four := sectionOf(base, "## 4. Diff + test gate", "## 5. Commit")
	test := bareDrossTestRE.FindStringIndex(four)
	if test == nil {
		t.Fatal("base §4 lost its bare `dross test`")
	}
	green := mustIdx(t, four, "**Green**")
	refetch := mustIdx(t, four, "**Re-fetch** if context was compacted: `dross prompt partial quick solo`")
	if !(test[0] < green && green < refetch) {
		t.Errorf("base §4 order: dross test %d, Green %d, solo re-fetch %d — want the re-fetch after the green test, before §5", test[0], green, refetch)
	}
}

// TestQuickSoloFetchPlacement (locked branch_placement): the solo partial is
// fetched once, by the first fetch line after the `dross quick begin --solo`
// fence, still in §0 — so a pair quick never pays for it — and the text it
// carries is gone from the base.
func TestQuickSoloFetchPlacement(t *testing.T) {
	base := rawQuickBase(t)
	fence := strings.Index(base, "dross quick begin --solo \"<description>\"\n")
	insight := strings.Index(base, "## 1. Insight")
	if fence < 0 || insight < fence {
		t.Fatal("quick.md lost the solo begin fence before §1")
	}
	fenceLine := strings.Count(base[:fence], "\n") + 1
	points, err := assets.FetchPoints(assets.FS, "quick.md")
	if err != nil {
		t.Fatal(err)
	}
	var first *assets.FetchPoint
	solo := 0
	for i, p := range points {
		if p.Partial == "solo" && !p.Refetch {
			solo++
		}
		// Re-fetch lines count too: any fetch between the fence and the
		// solo When line is a stray one.
		if first == nil && p.Line > fenceLine {
			first = &points[i]
		}
	}
	if first == nil || first.Refetch || first.Partial != "solo" || first.Key != "mode" || first.Value != "solo" {
		t.Errorf("first fetch after the solo begin fence is %+v, want `mode=solo` → solo", first)
	}
	if first != nil && first.Line > strings.Count(base[:insight], "\n") {
		t.Errorf("the solo fetch (line %d) sits past §0", first.Line)
	}
	if solo != 1 {
		t.Errorf("quick.md has %d When lines fetching solo, want exactly 1", solo)
	}
	for _, moved := range []string{"**Solo mode** (`--solo`)", "**Red, solo mode**", "**Solo review", "`solo: yes`", "dross review context", "In solo mode, proceed without a gate"} {
		if strings.Contains(base, moved) {
			t.Errorf("base quick.md still carries %q; it lives in the solo partial", moved)
		}
	}
}

// TestQuickCommonPathFetchesNothing (c-2): a pair quick to its commit fetches
// no partial; a solo quick fetches exactly the solo one.
func TestQuickCommonPathFetchesNothing(t *testing.T) {
	common, err := assets.Fetched(promptFS, "quick", assets.CommonPath())
	if err != nil {
		t.Fatal(err)
	}
	if len(common) != 0 {
		t.Errorf("a pair quick fetches %v on the common path; want nothing", common)
	}
	facts := assets.CommonPath()
	facts["mode"] = "solo"
	got, err := assets.Fetched(promptFS, "quick", facts)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(got, ",") != "solo" {
		t.Errorf("a solo quick fetches %v; want [solo]", got)
	}
}

// TestQuickSoloBeginNeedsReviewer: the refused-solo-begin stop stays in base
// §0. The partial is fetched only once begin has recorded solo, so a stop that
// moved into it would never be read by the run it is meant to stop.
func TestQuickSoloBeginNeedsReviewer(t *testing.T) {
	pre := sectionOf(rawQuickBase(t), "## 0. Pre-flight", "## 1.")
	stop := strings.Index(pre, "A solo begin refuses unless the solo task reviewer is installed and current — **stop and ask the user to run `dross install`**")
	fetch := strings.Index(pre, "**When** `mode=solo`")
	if stop < 0 {
		t.Fatal("base §0 no longer stops a refused solo begin and routes it to `dross install`")
	}
	if fetch < 0 || stop > fetch {
		t.Errorf("base §0: refusal stop at %d, solo fetch at %d — the stop must precede the fetch", stop, fetch)
	}
}

// TestQuickMarkerClosed: the quick is recorded before any code is written, and
// every way the run ends clears the record.
func TestQuickMarkerClosed(t *testing.T) {
	body := promptBody(t, "quick.md")
	pre := sectionOf(body, "## 0. Pre-flight", "## 1.")
	if !strings.Contains(pre, "dross quick begin \"<description>\"") || !strings.Contains(pre, "dross quick begin --solo \"<description>\"") || !strings.Contains(pre, "only in solo mode") {
		t.Error("§0 no longer records the quick (with --solo only in solo mode)")
	}
	if strings.Index(body, "dross quick begin") > strings.Index(body, "## 3. Implement") {
		t.Error("`dross quick begin` comes after §3 Implement")
	}
	for name, sec := range map[string]string{
		"§2 user abort":   sectionOf(body, "## 2. Propose", "## 3."),
		"§4 red paths":    sectionOf(body, "## 4. Diff + test gate", "## 5."),
		"§5 after commit": sectionOf(body, "## 5. Commit", "## 6."),
	} {
		if !strings.Contains(sec, "dross quick end") {
			t.Errorf("%s lost `dross quick end`", name)
		}
	}
	_, block := quickSoloReview(t)
	if !strings.Contains(block, "dross quick end") {
		t.Error("the blocked-review abort lost `dross quick end`")
	}
}

// TestQuickSoloReviewBlocked: one fix round; a review that still fails
// discards, clears the marker and bumps nothing.
func TestQuickSoloReviewBlocked(t *testing.T) {
	_, block := quickSoloReview(t)
	if !strings.Contains(block, "**one fix round**, never more") {
		t.Error("the blocked branch no longer caps the fix round at one")
	}
	fail := block[strings.Index(block, "- `exhausted` or `unavailable`"):]
	for _, want := range []string{"discard the working changes", "dross quick end", "No version bump", "**or the spawn itself errored**"} {
		if !strings.Contains(fail, want) {
			t.Errorf("the failed-review path lost %q", want)
		}
	}
}

// TestQuickDiscardBeforeEnd: t-20's downgrade guard refuses `dross quick end`
// while a solo quick has uncommitted code, so every solo abort path discards
// first.
func TestQuickDiscardBeforeEnd(t *testing.T) {
	body, block := quickSoloReview(t)
	red := body[strings.Index(body, "**Red, solo mode**"):]
	red = red[:strings.Index(red, "\n")]
	fail := block[strings.Index(block, "- `exhausted` or `unavailable`"):]
	for name, path := range map[string]string{"solo red": red, "failed review": fail} {
		d, e := strings.Index(path, "discard the working changes"), strings.Index(path, "dross quick end")
		if d < 0 || e < 0 || d > e {
			t.Errorf("%s runs `dross quick end` before discarding the code", name)
		}
	}
}

// TestReviewerSpawnWaitsForVerdict (review_pass_signal as amended): neither
// prompt requires a foreground spawn, and both wait for the reviewer to finish
// before reading `dross review status`.
func TestReviewerSpawnWaitsForVerdict(t *testing.T) {
	for _, name := range []string{"execute.md", "quick.md"} {
		body := promptBody(t, name)
		if strings.Contains(body, "run_in_background: false") {
			t.Errorf("%s still requires a foreground spawn", name)
		}
		wait := strings.Index(body, "**wait for its completion notice**")
		status := strings.Index(body, "dross review status")
		if wait < 0 || status < 0 || wait > status {
			t.Errorf("%s does not wait for the reviewer to finish before `dross review status` (wait %d, status %d)", name, wait, status)
		}
	}
}

func TestQuickTestStepDescribesSummary(t *testing.T) {
	assertTestStepDescribesSummary(t, "quick.md", testStepSection(t, rawPrompt(t, "quick.md"), "## 4.", "## 5."))
}

// TestQuickHasNoWriter (quick_solo): a quick stays inline, solo or not — no
// quick prompt or quick partial names the writer or its dispatch.
func TestQuickHasNoWriter(t *testing.T) {
	texts := map[string]string{"quick.md": expandedPrompt(t, "quick.md")}
	partials, err := fs.Glob(promptFS, "prompts/_quick*.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range partials {
		b, err := fs.ReadFile(promptFS, p)
		if err != nil {
			t.Fatal(err)
		}
		texts[p] = string(b)
	}
	for name, s := range texts {
		for _, gone := range []string{review.WriterAgent, "task dispatch"} {
			if strings.Contains(s, gone) {
				t.Errorf("%s mentions %q", name, gone)
			}
		}
	}
}
