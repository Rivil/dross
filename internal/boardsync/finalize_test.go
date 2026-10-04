package boardsync

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/board"
	"github.com/Rivil/dross/internal/forge"
)

const finalizeSpec = "[phase]\nid = \"p\"\ntitle = \"Phase\"\n%s\n[[criteria]]\nid = \"c-1\"\ntext = \"x\"\n"

const finalizePlan = `[phase]
id = "p"

[[task]]
id = "t-1"
wave = 1
title = "one"
status = "done"

[[task]]
id = "t-2"
wave = 1
title = "two"
status = "done"

[[task]]
id = "t-3"
wave = 1
title = "three"
status = "done"
`

// finalizeFixture writes phase p (spec, optional plan, changes.json at status)
// under a fresh Ctx over client. milestone, when set, goes on the spec.
func finalizeFixture(t *testing.T, client forge.BoardClient, status string, withPlan bool, milestone string) *Ctx {
	t.Helper()
	ctx, _ := boardCtx(t, client)
	dir := filepath.Join(ctx.Root, "phases", "p")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	ms := ""
	if milestone != "" {
		ms = "milestone = \"" + milestone + "\"\n"
	}
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("spec.toml", strings.Replace(finalizeSpec, "%s", ms, 1))
	if withPlan {
		write("plan.toml", finalizePlan)
	}
	write("changes.json", `{"phase":"p","status":"`+status+`","tasks":{}}`)
	return ctx
}

// seedLifecycle places the phase card (PROJ-1) at shipped and t-1..t-3
// (PROJ-2..4) at task-in-review, all open and cached in board.json.
func seedLifecycle(ctx *Ctx, f *faultBoard) {
	f.seed(forge.Issue{Key: "PROJ-1", Labels: []string{LabelMarker, PhaseLabel("p"), StatusLabel("shipped")}})
	ctx.Board.SetPhase("p", "PROJ-1")
	for i, id := range []string{"t-1", "t-2", "t-3"} {
		key := "PROJ-" + itoa(i+2)
		f.seed(forge.Issue{Key: key, Labels: []string{LabelMarker, PhaseLabel("p"), TaskLabel("p", id), StatusLabel(StatusTaskInReview)}})
		ctx.Board.SetTask("p", id, key)
	}
}

func wantTerminal(t *testing.T, f *faultBoard, key, status string) {
	t.Helper()
	iss := f.issues[key]
	if iss == nil {
		t.Fatalf("%s is not on the board", key)
	}
	if !hasLabel(iss.Labels, StatusLabel(status)) {
		t.Errorf("%s labels = %v, want %s", key, iss.Labels, StatusLabel(status))
	}
	if iss.State != "closed" {
		t.Errorf("%s state = %q, want closed", key, iss.State)
	}
}

// TestFinalizeClosesExistingCards: a shipped phase card and three in-review
// task cards all reach their terminal state.
func TestFinalizeClosesExistingCards(t *testing.T) {
	sb := newStateBoard()
	ctx := finalizeFixture(t, sb, "complete", true, "")
	seedLifecycle(ctx, sb.faultBoard)

	if err := FinalizePhase(ctx, "p"); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"PROJ-2", "PROJ-3", "PROJ-4"} {
		wantTerminal(t, sb.faultBoard, key, StatusTaskComplete)
	}
	wantTerminal(t, sb.faultBoard, "PROJ-1", StatusComplete)
	if got := sb.issues["PROJ-1"].Labels; hasLabel(got, StatusLabel("shipped")) {
		t.Errorf("the phase card kept its old status label: %v", got)
	}
}

// TestFinalizeCreatesMissingCards: with no cards at all, the phase card and
// every task card are created and closed, and board.json records the links.
func TestFinalizeCreatesMissingCards(t *testing.T) {
	f := newFaultBoard()
	ctx := finalizeFixture(t, f, "complete", true, "")

	if err := FinalizePhase(ctx, "p"); err != nil {
		t.Fatal(err)
	}
	if len(f.created) != 4 {
		t.Fatalf("created %d cards, want the phase card and three task cards", len(f.created))
	}
	saved, err := board.Load(ctx.BoardPath)
	if err != nil {
		t.Fatal(err)
	}
	phaseKey, ok := saved.PhaseIssue("p")
	if !ok {
		t.Fatal("board.json has no phase link")
	}
	wantTerminal(t, f, phaseKey, StatusComplete)
	for _, id := range []string{"t-1", "t-2", "t-3"} {
		key, ok := saved.TaskIssue("p", id)
		if !ok {
			t.Errorf("board.json has no link for %s", id)
			continue
		}
		wantTerminal(t, f, key, StatusTaskComplete)
	}
}

// TestFinalizeIsIdempotent: a second run on a finalized board only reads, and
// leaves board.json byte-identical.
func TestFinalizeIsIdempotent(t *testing.T) {
	f := newFaultBoard()
	ctx := finalizeFixture(t, f, "complete", true, "")
	if err := FinalizePhase(ctx, "p"); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(ctx.BoardPath)
	if err != nil {
		t.Fatal(err)
	}
	f.calls = nil

	if err := FinalizePhase(ctx, "p"); err != nil {
		t.Fatal(err)
	}
	for _, c := range f.calls {
		if !strings.HasPrefix(c, "GetIssue ") && !strings.HasPrefix(c, "ListIssues ") {
			t.Errorf("a second finalize wrote: %s", c)
		}
	}
	after, err := os.ReadFile(ctx.BoardPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Errorf("board.json changed on a second finalize:\n%s\n---\n%s", before, after)
	}
}

// TestFinalizeRelabelsDoneCardMissingLabel: the skip needs the terminal label
// AND a done card. A card closed by hand without the label is relabelled.
func TestFinalizeRelabelsDoneCardMissingLabel(t *testing.T) {
	f := newFaultBoard()
	ctx := finalizeFixture(t, f, "complete", true, "")
	seedLifecycle(ctx, f)
	f.issues["PROJ-3"].State = "closed" // t-2: closed, still labelled task-in-review

	if err := FinalizePhase(ctx, "p"); err != nil {
		t.Fatal(err)
	}
	if got := f.callsOf("UpdateIssue"); !containsCall(got, "UpdateIssue PROJ-3") {
		t.Errorf("updates = %v, want PROJ-3 relabelled", got)
	}
	wantTerminal(t, f, "PROJ-3", StatusTaskComplete)
}

// TestFinalizeRefusesIncompletePhase: a shipped phase is not finished — the
// refusal names its status and touches no board.
func TestFinalizeRefusesIncompletePhase(t *testing.T) {
	f := newFaultBoard()
	ctx := finalizeFixture(t, f, "shipped", true, "")
	seedLifecycle(ctx, f)

	err := FinalizePhase(ctx, "p")
	if err == nil || !strings.Contains(err.Error(), "shipped") {
		t.Fatalf("err = %v, want a refusal naming shipped", err)
	}
	if len(f.calls) != 0 {
		t.Errorf("a refused finalize called the board: %v", f.calls)
	}
}

// TestFinalizeContinuesPastRefusedClose: one stuck card is named, and the
// others — the phase card included — still reach their terminal state.
func TestFinalizeContinuesPastRefusedClose(t *testing.T) {
	f := newFaultBoard()
	ctx := finalizeFixture(t, f, "complete", true, "")
	seedLifecycle(ctx, f)
	f.refuseClose["PROJ-3"] = true // t-2

	err := FinalizePhase(ctx, "p")
	if err == nil || !strings.Contains(err.Error(), "task t-2") {
		t.Fatalf("err = %v, want t-2 named", err)
	}
	for _, key := range []string{"PROJ-2", "PROJ-4"} {
		wantTerminal(t, f, key, StatusTaskComplete)
	}
	wantTerminal(t, f, "PROJ-1", StatusComplete)
}

// TestFinalizeWithoutPlan: with plan.toml gone, a task card is still found by
// its phase and task labels and closed. A marker card carrying only the phase
// label is not a task card and is left alone.
func TestFinalizeWithoutPlan(t *testing.T) {
	f := newFaultBoard()
	ctx := finalizeFixture(t, f, "complete", false, "")
	f.seed(forge.Issue{Key: "PROJ-1", Labels: []string{LabelMarker, PhaseLabel("p"), StatusLabel("shipped")}})
	ctx.Board.SetPhase("p", "PROJ-1")
	f.seed(forge.Issue{Key: "PROJ-5", Labels: []string{LabelMarker, PhaseLabel("p"), TaskLabel("p", "t-9"), StatusLabel(StatusTaskInReview)}})
	f.seed(forge.Issue{Key: "PROJ-6", Labels: []string{LabelMarker, PhaseLabel("p")}})

	if err := FinalizePhase(ctx, "p"); err != nil {
		t.Fatal(err)
	}
	wantTerminal(t, f, "PROJ-1", StatusComplete)
	wantTerminal(t, f, "PROJ-5", StatusTaskComplete)
	for _, c := range f.calls {
		if strings.HasSuffix(c, " PROJ-6") && !strings.HasPrefix(c, "GetIssue ") {
			t.Errorf("the phase-label-only card was touched: %s", c)
		}
	}
	if f.issues["PROJ-6"].State != "open" {
		t.Error("the phase-label-only card was closed")
	}
}

// TestFinalizeUsesCachedMilestoneOnly: a missing phase card is created without
// opening a milestone — only a milestone id board.json already holds is used.
func TestFinalizeUsesCachedMilestoneOnly(t *testing.T) {
	f := newFaultBoard()
	ctx := finalizeFixture(t, f, "complete", false, "v1")
	if err := FinalizePhase(ctx, "p"); err != nil {
		t.Fatal(err)
	}
	if len(f.milestones) != 0 {
		t.Errorf("finalize ensured a milestone: %+v", f.milestones)
	}
	if len(f.created) != 1 || f.created[0].Milestone != 0 {
		t.Errorf("created = %+v, want one phase card with no milestone", f.created)
	}
	key, _ := ctx.Board.PhaseIssue("p")
	wantTerminal(t, f, key, StatusComplete)

	cached := newFaultBoard()
	ctx = finalizeFixture(t, cached, "complete", false, "v1")
	ctx.Board.SetMilestone("v1", "7")
	if err := FinalizePhase(ctx, "p"); err != nil {
		t.Fatal(err)
	}
	if len(cached.created) != 1 || cached.created[0].Milestone != 7 {
		t.Errorf("created = %+v, want the cached milestone 7", cached.created)
	}
	if len(cached.milestones) != 0 {
		t.Errorf("finalize ensured a milestone: %+v", cached.milestones)
	}
}

func containsCall(calls []string, want string) bool {
	for _, c := range calls {
		if c == want {
			return true
		}
	}
	return false
}

// TestFinalizeNeverTakesATaskCardForThePhase: with no phase link cached, the
// phase-label query also returns the task cards. A task card sorting first —
// PROJ-100 before PROJ-98 — must not be taken for the phase card.
func TestFinalizeNeverTakesATaskCardForThePhase(t *testing.T) {
	f := newFaultBoard()
	ctx := finalizeFixture(t, f, "complete", true, "")
	f.seed(forge.Issue{Key: "PROJ-98", Labels: []string{LabelMarker, PhaseLabel("p"), StatusLabel("shipped")}})
	f.seed(forge.Issue{Key: "PROJ-100", Labels: []string{LabelMarker, PhaseLabel("p"), TaskLabel("p", "t-1"), StatusLabel(StatusTaskInReview)}})

	if err := FinalizePhase(ctx, "p"); err != nil {
		t.Fatal(err)
	}
	if key, _ := ctx.Board.PhaseIssue("p"); key != "PROJ-98" {
		t.Errorf("phase card = %s, want PROJ-98", key)
	}
	wantTerminal(t, f, "PROJ-98", StatusComplete)
	wantTerminal(t, f, "PROJ-100", StatusTaskComplete)
	if hasLabel(f.issues["PROJ-100"].Labels, StatusLabel(StatusComplete)) {
		t.Error("the task card was labelled as the phase card")
	}
}

// TestFinalizeClosesLabelledOpenCard: the skip needs BOTH halves. A card that
// already carries its terminal label but is still open gets closed.
func TestFinalizeClosesLabelledOpenCard(t *testing.T) {
	f := newFaultBoard()
	ctx := finalizeFixture(t, f, "complete", true, "")
	seedLifecycle(ctx, f)
	f.issues["PROJ-4"].Labels = []string{LabelMarker, PhaseLabel("p"), TaskLabel("p", "t-3"), StatusLabel(StatusTaskComplete)}

	if err := FinalizePhase(ctx, "p"); err != nil {
		t.Fatal(err)
	}
	if !containsCall(f.callsOf("CloseIssue"), "CloseIssue PROJ-4") {
		t.Errorf("closes = %v, want the labelled-but-open PROJ-4 closed", f.callsOf("CloseIssue"))
	}
	wantTerminal(t, f, "PROJ-4", StatusTaskComplete)
}

// TestFinalizePhaseCardFailureIsReported: a phase card that does not close is
// named in the error and never narrated as complete.
func TestFinalizePhaseCardFailureIsReported(t *testing.T) {
	f := newFaultBoard()
	ctx := finalizeFixture(t, f, "complete", true, "")
	seedLifecycle(ctx, f)
	f.refuseClose["PROJ-1"] = true
	out := ctx.Out.(interface{ String() string })

	err := FinalizePhase(ctx, "p")
	if err == nil || !strings.Contains(err.Error(), "phase PROJ-1") {
		t.Fatalf("err = %v, want the phase card named", err)
	}
	if strings.Contains(out.String(), "already complete") {
		t.Errorf("narration claims the failed phase card is complete: %q", out.String())
	}
}

// TestFinalizeTasksSurviveAPhaseCardCreateFailure: the task cards need the
// phase card only for their body text, so a failed create does not stop them.
func TestFinalizeTasksSurviveAPhaseCardCreateFailure(t *testing.T) {
	f := newFaultBoard()
	ctx := finalizeFixture(t, f, "complete", true, "")
	for i, id := range []string{"t-1", "t-2", "t-3"} {
		key := "PROJ-" + itoa(i+2)
		f.seed(forge.Issue{Key: key, Labels: []string{LabelMarker, PhaseLabel("p"), TaskLabel("p", id), StatusLabel(StatusTaskInReview)}})
		ctx.Board.SetTask("p", id, key)
	}
	f.failCreate["p — Phase"] = os.ErrPermission

	err := FinalizePhase(ctx, "p")
	if err == nil || !strings.Contains(err.Error(), "phase card") {
		t.Fatalf("err = %v, want the phase card's create failure named", err)
	}
	for _, key := range []string{"PROJ-2", "PROJ-3", "PROJ-4"} {
		wantTerminal(t, f, key, StatusTaskComplete)
	}
}
