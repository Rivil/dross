package boardsync

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/board"
	"github.com/Rivil/dross/internal/forge"
	"github.com/Rivil/dross/internal/reaplog"
)

// TestJournalledPriorStateFallsBackToTheOpenClosedState: a forge or GitLab
// board has no column model, so WorkflowState is empty on every card — and
// those backends ARE StateWriters, so undo really does run against them.
// Journalling WorkflowState alone would record "" and make every restore write
// an empty state and fail its read-back.
func TestJournalledPriorStateFallsBackToTheOpenClosedState(t *testing.T) {
	for _, tc := range []struct {
		name string
		iss  forge.Issue
		want string
	}{
		{"a board with a column model", forge.Issue{WorkflowState: "In Review", State: "open"}, "In Review"},
		{"a flat open/closed board", forge.Issue{State: "open"}, "open"},
	} {
		if got := priorStateOf(&tc.iss); got != tc.want {
			t.Errorf("%s: journalled prior state %q, want %q", tc.name, got, tc.want)
		}
	}
}

// writeLedger saves runs as the reap ledger under ctx.Root.
func writeLedger(t *testing.T, ctx *Ctx, runs ...reaplog.Run) {
	t.Helper()
	l := &reaplog.Log{Runs: runs}
	if err := l.Save(reaplog.FilePath(ctx.Root)); err != nil {
		t.Fatal(err)
	}
}

// TestUndoRefusesClientWithoutStateWriter: a board with no column model is
// refused by provider name before anything is read or written — even the
// ledger, so a corrupt one cannot turn the refusal into a different error.
func TestUndoRefusesClientWithoutStateWriter(t *testing.T) {
	f := newFaultBoard()
	ctx, _ := boardCtx(t, f)
	mustWrite(t, ctx.BoardPath, `{"backlog":{"slug:x":"PROJ-1"}}`)
	mustWrite(t, reaplog.FilePath(ctx.Root), "{corrupt")
	before, err := os.ReadFile(ctx.BoardPath)
	if err != nil {
		t.Fatal(err)
	}

	err = Undo(ctx)
	if err == nil || !strings.Contains(err.Error(), "fakeboard") || !strings.Contains(err.Error(), "nothing was written") {
		t.Errorf("err = %v, want a refusal naming the provider and saying nothing was written", err)
	}
	if len(f.calls) != 0 {
		t.Errorf("board calls %v, want none", f.calls)
	}
	after, _ := os.ReadFile(ctx.BoardPath)
	if !bytes.Equal(before, after) {
		t.Error("board.json changed under a refused undo")
	}
}

// TestUndoWithEmptyLedgerSaysNothingToUndo: no ledger is a clean no-op; a
// ledger that cannot be read is an error.
func TestUndoWithEmptyLedgerSaysNothingToUndo(t *testing.T) {
	s := newStateBoard()
	ctx, out := boardCtx(t, s)
	if err := Undo(ctx); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "nothing to undo") || len(s.calls) != 0 {
		t.Errorf("narration %q, calls %v; want nothing to undo and no writes", out.String(), s.calls)
	}

	mustWrite(t, reaplog.FilePath(ctx.Root), "{corrupt")
	if err := Undo(ctx); err == nil {
		t.Error("a corrupt ledger was treated as empty")
	}
}

// undoFixture journals one run: A closed with labels, B closed with no labels
// and a dropped backlog link, C failed, D closed in the Tasks lane with a
// DroppedLink that lane never restores.
func undoFixture(t *testing.T) (*Ctx, *stateBoard, *bytes.Buffer) {
	t.Helper()
	s := newStateBoard()
	for _, k := range []string{"A-1", "B-1", "C-1", "D-1"} {
		s.seed(forge.Issue{Key: k, State: "closed", Labels: []string{StatusLabel("complete")}})
	}
	ctx, out := boardCtx(t, s)
	writeLedger(t, ctx, reaplog.Run{Cards: []reaplog.Card{
		{Issue: "A-1", Class: "Phases", PriorState: "In Review", PriorLabels: []string{LabelMarker, StatusLabel(StatusUAT)}, Outcome: reaplog.OutcomeClosed},
		{Issue: "B-1", Class: "Backlog", PriorState: "Submitted", DroppedLink: "slug:b", Outcome: reaplog.OutcomeClosed},
		{Issue: "C-1", Class: "Phases", PriorState: "Open", Outcome: reaplog.OutcomeFailed},
		{Issue: "D-1", Class: "Tasks", PriorState: "Open", DroppedLink: "p/t-1", Outcome: reaplog.OutcomeClosed},
	}})
	return ctx, s, out
}

// TestUndoWritesPriorStateThroughStateWriter: each closed card gets its
// journalled column back verbatim, its labels when it had any, and — for the
// backlog lane only — its board.json link.
func TestUndoWritesPriorStateThroughStateWriter(t *testing.T) {
	ctx, s, out := undoFixture(t)
	if err := Undo(ctx); err != nil {
		t.Fatal(err)
	}
	want := []stateWrite{{"A-1", "In Review"}, {"B-1", "Submitted"}, {"D-1", "Open"}}
	if !reflect.DeepEqual(s.states, want) {
		t.Errorf("state writes = %v, want %v (the failed card is not restored)", s.states, want)
	}
	if got := s.callsOf("UpdateIssue"); !reflect.DeepEqual(got, []string{"UpdateIssue A-1"}) {
		t.Errorf("label restores = %v, want only A-1 (B-1 had no prior labels)", got)
	}
	if got := s.issues["A-1"].Labels; !reflect.DeepEqual(got, []string{LabelMarker, StatusLabel(StatusUAT)}) {
		t.Errorf("A-1 labels = %v, want the journalled set", got)
	}
	saved, err := board.Load(ctx.BoardPath)
	if err != nil {
		t.Fatal(err)
	}
	if id, _ := saved.BacklogID("slug:b"); id != "B-1" {
		t.Errorf("slug:b -> %q, want the backlog link restored to B-1", id)
	}
	if _, ok := saved.BacklogID("p/t-1"); ok {
		t.Error("a Tasks-lane DroppedLink was put into the backlog map")
	}
	if got := out.String(); got != "restored 3 card(s)\n" {
		t.Errorf("narration = %q", got)
	}
}

// TestUndoContinuesPastSetStateFailure: a card the tracker refuses is named
// and counted, and the rest are still restored; a failed label restore only
// warns, because the column is the load-bearing write.
func TestUndoContinuesPastSetStateFailure(t *testing.T) {
	ctx, s, out := undoFixture(t)
	s.refuseState["A-1"] = errors.New("transition refused")
	var err error
	stderr := captureStderr(t, func() { err = Undo(ctx) })
	if err == nil || err.Error() != "1 of 3 card(s) could not be restored" {
		t.Fatalf("err = %v, want 1 of 3", err)
	}
	if !strings.Contains(stderr, "  A-1: transition refused") {
		t.Errorf("stderr does not name A-1:\n%s", stderr)
	}
	if !reflect.DeepEqual(s.states, []stateWrite{{"B-1", "Submitted"}, {"D-1", "Open"}}) {
		t.Errorf("state writes = %v, want B-1 and D-1 still restored", s.states)
	}
	if !strings.Contains(out.String(), "restored 2 card(s), 1 failed:") {
		t.Errorf("narration = %q", out.String())
	}

	ctx, s, out = undoFixture(t)
	s.failUpdate["A-1"] = errors.New("tags locked")
	stderr = captureStderr(t, func() { err = Undo(ctx) })
	if err != nil {
		t.Fatalf("a failed label restore failed the undo: %v", err)
	}
	if !strings.Contains(stderr, "restored A-1 but could not put its labels back") {
		t.Errorf("stderr = %q", stderr)
	}
	if got := out.String(); got != "restored 3 card(s)\n" {
		t.Errorf("narration = %q, want the count unchanged", got)
	}
}

// TestUndoOnlyReversesTheLastRun: earlier runs in the ledger are history, not
// undo targets.
func TestUndoOnlyReversesTheLastRun(t *testing.T) {
	s := newStateBoard()
	s.seed(forge.Issue{Key: "OLD-1", State: "closed"})
	s.seed(forge.Issue{Key: "NEW-1", State: "closed"})
	ctx, _ := boardCtx(t, s)
	writeLedger(t, ctx,
		reaplog.Run{Cards: []reaplog.Card{{Issue: "OLD-1", Class: "Phases", PriorState: "Open", Outcome: reaplog.OutcomeClosed}}},
		reaplog.Run{Cards: []reaplog.Card{{Issue: "NEW-1", Class: "Phases", PriorState: "Open", Outcome: reaplog.OutcomeClosed}}},
	)
	if err := Undo(ctx); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(s.states, []stateWrite{{"NEW-1", "Open"}}) {
		t.Errorf("state writes = %v, want only the last run's NEW-1", s.states)
	}
}

// TestApplyThenUndoRoundTrip: undo is apply's inverse — every card's state,
// label set and the backlog map come back exactly.
//
// Every card here carries labels before the sweep. A card with none is left
// out on purpose: Undo skips the label restore when PriorLabels is empty
// (reap_undo.go), so such a card keeps dross/status:<terminal> by current
// design — parked as a deferred defect, not asserted here.
func TestApplyThenUndoRoundTrip(t *testing.T) {
	s := newStateBoard()
	s.seed(forge.Issue{Key: "PROJ-1", Labels: []string{LabelMarker, PhaseLabel("p"), StatusLabel(StatusUAT)}})
	s.seed(forge.Issue{Key: "PROJ-2", Labels: []string{LabelMarker, TaskLabel("p", "t-1"), StatusLabel(StatusTaskInReview)}})
	s.seed(forge.Issue{Key: "PROJ-3", Labels: []string{LabelMarker, DeferredLabel("d1")}})
	ctx, _ := boardCtx(t, s)
	ctx.Board.SetPhase("p", "PROJ-1")
	ctx.Board.SetTask("p", "t-1", "PROJ-2")
	ctx.Board.SetBacklog(DeferredBacklogKey("d1"), "PROJ-3")

	type snap struct {
		State  string
		Labels []string
	}
	snapshot := func() (map[string]snap, map[string]string) {
		cards := map[string]snap{}
		for k, iss := range s.issues {
			cards[k] = snap{iss.State, append([]string(nil), iss.Labels...)}
		}
		backlog := map[string]string{}
		for _, k := range ctx.Board.BacklogKeys() {
			backlog[k], _ = ctx.Board.BacklogID(k)
		}
		return cards, backlog
	}
	cardsBefore, backlogBefore := snapshot()

	plan := &ReapPlan{Cards: []ReapCard{
		{Key: "PROJ-1", Lane: "Phases", Terminal: "complete"},
		{Key: "PROJ-2", Lane: "Tasks", Terminal: "task-verified"},
		{Key: "PROJ-3", Lane: "Backlog", Terminal: "complete"},
	}}
	if err := Apply(ctx, plan); err != nil {
		t.Fatal(err)
	}
	if mid, _ := snapshot(); reflect.DeepEqual(mid, cardsBefore) {
		t.Fatal("Apply changed nothing — the round trip would pass vacuously")
	}
	if err := Undo(ctx); err != nil {
		t.Fatal(err)
	}
	cardsAfter, backlogAfter := snapshot()
	if !reflect.DeepEqual(cardsAfter, cardsBefore) {
		t.Errorf("cards after undo = %+v\nwant %+v", cardsAfter, cardsBefore)
	}
	if !reflect.DeepEqual(backlogAfter, backlogBefore) {
		t.Errorf("backlog after undo = %v, want %v", backlogAfter, backlogBefore)
	}
}

// TestUndoSurfacesBoardSaveFailure: a restore whose board.json cannot be
// written is an error, not a "restored" line over links that were never saved.
func TestUndoSurfacesBoardSaveFailure(t *testing.T) {
	ctx, _, out := undoFixture(t)
	ctx.BoardPath = filepath.Join(t.TempDir(), "missing", "board.json")
	if err := Undo(ctx); err == nil || !strings.Contains(err.Error(), "board.json") {
		t.Errorf("err = %v, want the save error", err)
	}
	if strings.Contains(out.String(), "restored") {
		t.Errorf("narrated %q over an unsaved board", out.String())
	}
}
