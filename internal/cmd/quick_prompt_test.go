package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/review"
)

// quickPromptContent loads assets/prompts/quick.md and normalises it —
// lowercased, with backticks and asterisk emphasis stripped — so assertions
// test the presence of a rule, not its exact formatting. Unlike the ship.md
// helper this does NOT strip underscores: the needles here are store keys
// (quick_base) and stripping them would mangle the thing being asserted.
func quickPromptContent(t *testing.T) string {
	t.Helper()
	root := repoRootFromTest(t)
	b, err := os.ReadFile(filepath.Join(root, "assets", "prompts", "quick.md"))
	if err != nil {
		t.Fatalf("read quick.md: %v", err)
	}
	s := strings.ToLower(string(b))
	return strings.NewReplacer("`", "", "*", "").Replace(s)
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

// quickSoloReview is quick.md's solo review block.
func quickSoloReview(t *testing.T) (string, string) {
	t.Helper()
	body := promptBody(t, "quick.md")
	i := strings.Index(body, "**Solo review (`--solo` only).**")
	j := strings.Index(body, "## 5. Commit")
	if i < 0 || j < i {
		t.Fatal("quick.md has no solo review block before §5")
	}
	return body, body[i:j]
}

// TestQuickSoloReview (c-8): a solo quick is reviewed after its green test and
// before its commit; pair mode spawns no reviewer.
func TestQuickSoloReview(t *testing.T) {
	body, block := quickSoloReview(t)
	test := strings.Index(body, "\ndross test\n")
	ctx := strings.Index(body, "dross review context")
	spawn := strings.Index(body, `subagent_type: "`+review.ReviewerAgent+`"`)
	add := strings.Index(body, "git add <touched-files>")
	if test < 0 || !(test < ctx && ctx < spawn && spawn < add) {
		t.Errorf("solo quick review order: dross test %d, review context %d, spawn %d, git add %d", test, ctx, spawn, add)
	}
	if !strings.Contains(block, "In **pair mode skip this entirely**") {
		t.Error("the quick's solo review block no longer says pair mode skips it")
	}
	if strings.Count(body, review.ReviewerAgent) != strings.Count(block, review.ReviewerAgent) {
		t.Error("quick.md spawns the reviewer outside its solo review block")
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
		"§4 red paths":    sectionOf(body, "## 4. Diff + test gate", "**Solo review"),
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
