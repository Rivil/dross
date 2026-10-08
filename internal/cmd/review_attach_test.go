package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/Rivil/dross/internal/changes"
	"github.com/Rivil/dross/internal/gatestate"
	"github.com/Rivil/dross/internal/phase"
	"github.com/Rivil/dross/internal/review"
)

const attachPlan = `[phase]
id = "p"
[[task]]
id = "t-2"
wave = 1
title = "two"
files = ["b.go"]
covers = ["c-1"]
[[task]]
id = "t-3"
wave = 1
title = "three"
files = ["a.go"]
covers = ["c-1"]
`

// attachFixture is a committed dross repo with phase p planned and the
// execute mode recorded.
func attachFixture(t *testing.T, mode string) string {
	t.Helper()
	dir := t.TempDir()
	gitInit(t, dir, "https://forge.example/me/p.git")
	chdir(t, dir)
	scaffoldPhaseWithPlan(t, "p", attachPlan)
	mustWrite(t, filepath.Join(dir, "a.go"), "package a\n")
	gitCommit(t, dir, "init")
	if err := gatestate.SaveExecute(dir, gatestate.Execute{Phase: "p", Mode: mode, At: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	return dir
}

var (
	b1 = review.Finding{Criterion: "c-1", Severity: review.Blocking, Text: "B1 pair skip untested"}
	b2 = review.Finding{Criterion: "c-1", Severity: review.Blocking, Text: "B2 still untested"}
	f1 = review.Finding{Severity: review.Flag, Text: "F1 long func"}
)

func saveLedger(t *testing.T, dir string, rec gatestate.Review) {
	t.Helper()
	if rec.Attempt == "" {
		rec.Attempt = "base-1"
	}
	if err := gatestate.SaveReview(dir, rec); err != nil {
		t.Fatal(err)
	}
}

func loadReviews(t *testing.T, dir string) map[string]changes.TaskReview {
	t.Helper()
	c, err := changes.Load(changes.FilePath(filepath.Join(dir, ".dross"), "p"), "p")
	if err != nil {
		t.Fatal(err)
	}
	return c.Reviews
}

func TestChangesRecordAttachesReview(t *testing.T) {
	dir := attachFixture(t, "solo")
	saveLedger(t, dir, gatestate.Review{Kind: review.KindTask, Phase: "p", Task: "t-3", Rounds: []review.Round{
		{Outcome: review.OutcomeBlock, Spec: []review.Finding{b1}, Quality: []review.Finding{f1}},
		{Outcome: review.OutcomePass, Tree: "tree-2"},
	}})
	if err := runCmd(t, Changes(), "record", "p", "t-3", "--files", "a.go", "--commit", "abc"); err != nil {
		t.Fatal(err)
	}
	got, ok := loadReviews(t, dir)["t-3"]
	if !ok {
		t.Fatal("changes record attached no review for t-3")
	}
	if got.Outcome != "pass" || got.Rounds != 2 || len(got.Findings) != 2 {
		t.Fatalf("review = %+v, want pass after 2 rounds with 2 findings", got)
	}
	if got.Findings[0].Text != b1.Text || got.Findings[0].Resolution != "fixed in the fix round" || got.Findings[0].Kind != "spec" {
		t.Errorf("B1 = %+v", got.Findings[0])
	}
	if got.Findings[1].Text != f1.Text || got.Findings[1].Resolution != "non-blocking, left" || got.Findings[1].Kind != "quality" {
		t.Errorf("F1 = %+v", got.Findings[1])
	}

	if err := runCmd(t, Changes(), "record", "p", "t-2", "--files", "b.go"); err != nil {
		t.Fatal(err)
	}
	if _, ok := loadReviews(t, dir)["t-2"]; ok {
		t.Fatal("t-3's ledger was attached to t-2")
	}
}

func TestChangesRecordIgnoresForeignLedgers(t *testing.T) {
	for _, rec := range []gatestate.Review{
		{Kind: review.KindTask, Phase: "other", Task: "t-3", Rounds: []review.Round{{Outcome: review.OutcomePass, Tree: "x"}}},
		{Kind: review.KindQuick, Rounds: []review.Round{{Outcome: review.OutcomePass, Tree: "x"}}},
	} {
		dir := attachFixture(t, "solo")
		saveLedger(t, dir, rec)
		if err := runCmd(t, Changes(), "record", "p", "t-3", "--files", "a.go"); err != nil {
			t.Fatal(err)
		}
		if r := loadReviews(t, dir); len(r) != 0 {
			t.Fatalf("a %s ledger for %q attached %+v", rec.Kind, rec.Phase, r)
		}
	}
}

func TestPairRecordHasNoReview(t *testing.T) {
	dir := attachFixture(t, "pair")
	saveLedger(t, dir, gatestate.Review{Kind: review.KindTask, Phase: "p", Task: "t-3", Rounds: []review.Round{{Outcome: review.OutcomePass, Tree: "x"}}})
	if err := runCmd(t, Changes(), "record", "p", "t-3", "--files", "a.go"); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(changes.FilePath(filepath.Join(dir, ".dross"), "p"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), `"reviews"`) {
		t.Fatalf("a pair-mode task's record gained reviews:\n%s", b)
	}
}

func attachTask(t *testing.T, dir, id string) phase.Task {
	t.Helper()
	plan, err := phase.LoadPlan(filepath.Join(dir, ".dross", "phases", "p", "plan.toml"))
	if err != nil {
		t.Fatal(err)
	}
	return *plan.FindTask(id)
}

func TestFailedReasonFromReview(t *testing.T) {
	dir := attachFixture(t, "solo")
	saveLedger(t, dir, gatestate.Review{Kind: review.KindTask, Phase: "p", Task: "t-3", Rounds: []review.Round{
		{Outcome: review.OutcomeBlock, Spec: []review.Finding{b1}},
		{Outcome: review.OutcomeBlock, Spec: []review.Finding{b2}},
	}})
	if err := runCmd(t, Task(), "status", "p", "t-3", "failed"); err != nil {
		t.Fatal(err)
	}
	if r := attachTask(t, dir, "t-3").Reason; !strings.Contains(r, "still blocked after the one fix round") || !strings.Contains(r, "B1 pair skip untested") {
		t.Fatalf("derived reason = %q", r)
	}
	got := loadReviews(t, dir)["t-3"]
	if got.Outcome != "exhausted" || len(got.Findings) != 2 || got.Findings[1].Resolution != "unresolved — task failed" {
		t.Fatalf("attached review = %+v", got)
	}

	dir = attachFixture(t, "solo")
	saveLedger(t, dir, gatestate.Review{Kind: review.KindTask, Phase: "p", Task: "t-3", Rounds: []review.Round{
		{Outcome: review.OutcomeUnavailable, Cause: "verdict did not parse: no dross-verdict fence"},
	}})
	if err := runCmd(t, Task(), "status", "p", "t-3", "failed"); err != nil {
		t.Fatal(err)
	}
	if r := attachTask(t, dir, "t-3").Reason; r != "reviewer unavailable: verdict did not parse: no dross-verdict fence" {
		t.Fatalf("unavailable reason = %q", r)
	}

	if err := runCmd(t, Task(), "status", "p", "t-3", "failed", "--reason", "x"); err != nil {
		t.Fatal(err)
	}
	if r := attachTask(t, dir, "t-3").Reason; r != "x" {
		t.Fatalf("an explicit --reason was replaced: %q", r)
	}
}

func TestSoloFailedRequiresStash(t *testing.T) {
	dir := attachFixture(t, "solo")
	mustWrite(t, filepath.Join(dir, "a.go"), "package a // half done\n")
	err := runCmd(t, Task(), "status", "p", "t-3", "failed")
	if err == nil || !strings.Contains(err.Error(), stashCommand) {
		t.Fatalf("failed with a modified a.go = %v, want a refusal naming %s", err, stashCommand)
	}
	if attachTask(t, dir, "t-3").Status == phase.StatusFailed {
		t.Fatal("the refusal still wrote the status")
	}
	mustGit(t, dir, "checkout", "--", "a.go")
	mustWrite(t, filepath.Join(dir, "b.go"), "package b\n")
	if err := runCmd(t, Task(), "status", "p", "t-3", "failed"); err == nil {
		t.Fatal("failed with an untracked b.go was accepted")
	}
	if err := os.Remove(filepath.Join(dir, "b.go")); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(dir, ".dross", "state.json"), `{"x":1}`)
	if err := runCmd(t, Task(), "status", "p", "t-3", "failed"); err != nil {
		t.Fatalf("a .dross/-only dirty tree refused: %v", err)
	}

	pair := attachFixture(t, "pair")
	mustWrite(t, filepath.Join(pair, "a.go"), "package a // half done\n")
	if err := runCmd(t, Task(), "status", "p", "t-3", "failed"); err != nil {
		t.Fatalf("a pair run refused: %v", err)
	}
}

func TestAttachErrorWarns(t *testing.T) {
	dir := attachFixture(t, "solo")
	if err := os.WriteFile(gatestate.Path(dir, gatestate.ReviewFile), []byte(`{"kind":"task","rou`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := runCmd(t, Changes(), "record", "p", "t-3", "--files", "a.go", "--commit", "abc"); err != nil {
		t.Fatalf("a corrupt ledger failed changes record: %v", err)
	}
	c, err := changes.Load(changes.FilePath(filepath.Join(dir, ".dross"), "p"), "p")
	if err != nil {
		t.Fatal(err)
	}
	if c.Tasks["t-3"].Commit != "abc" {
		t.Fatal("a corrupt ledger skipped the task record")
	}
}

// TestNoReviewFlags: review content reaches the change record only from the
// recorder's ledger (review_pass_signal) — no flag lets the executing agent
// write its own.
func TestNoReviewFlags(t *testing.T) {
	for _, cmd := range []struct {
		name  string
		flags []string
	}{
		{"changes record", flagNames(changesRecord())},
		{"task status", flagNames(taskStatus())},
	} {
		for _, f := range cmd.flags {
			for _, banned := range []string{"review", "finding", "verdict", "round", "outcome", "resolution"} {
				if strings.Contains(f, banned) {
					t.Errorf("%s has flag --%s carrying review content", cmd.name, f)
				}
			}
		}
	}
}

func flagNames(c *cobra.Command) []string {
	var out []string
	c.Flags().VisitAll(func(f *pflag.Flag) { out = append(out, f.Name) })
	return out
}
