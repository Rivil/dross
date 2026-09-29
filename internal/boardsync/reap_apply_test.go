package boardsync

import (
	"errors"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/forge"
	"github.com/Rivil/dross/internal/reaplog"
)

// The write half of the sweep, over faultBoard: per-card failure isolation,
// the journal of PRIOR state, and the relabel that makes a reaped card read
// like a forward-closed one.

var uatLabels = []string{LabelMarker, PhaseLabel("p"), StatusLabel(StatusUAT)}

func lastRun(t *testing.T, ctx *Ctx) *reaplog.Run {
	t.Helper()
	log, err := reaplog.Load(reaplog.FilePath(ctx.Root))
	if err != nil {
		t.Fatal(err)
	}
	return log.Last()
}

func journalled(t *testing.T, run *reaplog.Run, key string) reaplog.Card {
	t.Helper()
	for _, c := range run.Cards {
		if c.Issue == key {
			return c
		}
	}
	t.Fatalf("%s is not in the journalled run (%d cards)", key, len(run.Cards))
	return reaplog.Card{}
}

// TestApplyJournalsPriorStateBeforeClosing: the ledger records what the card
// was before dross touched it, every card lands on its lane terminal with its
// identity labels intact, and only the backlog lane drops its link.
func TestApplyJournalsPriorStateBeforeClosing(t *testing.T) {
	f := newFaultBoard()
	for _, k := range []string{"PROJ-1", "PROJ-2", "PROJ-3"} {
		f.seed(forge.Issue{Key: k, Labels: uatLabels})
	}
	ctx, out := boardCtx(t, f)
	ctx.Board.SetPhase("p", "PROJ-1")
	ctx.Board.SetTask("p", "t-1", "PROJ-2")
	ctx.Board.SetBacklog("slug:x", "PROJ-3")
	ctx.Board.SetBacklog("slug:other", "PROJ-99")
	plan := &ReapPlan{Cards: []ReapCard{
		{Key: "PROJ-1", Lane: "Phases", Terminal: "complete"},
		{Key: "PROJ-2", Lane: "Tasks", Terminal: "task-verified"},
		{Key: "PROJ-3", Lane: "Backlog", Terminal: "complete"},
	}}

	if err := Apply(ctx, plan); err != nil {
		t.Fatal(err)
	}
	closed := append([]string(nil), f.closed...)
	sort.Strings(closed)
	if !reflect.DeepEqual(closed, []string{"PROJ-1", "PROJ-2", "PROJ-3"}) {
		t.Errorf("closed %v, want all three", closed)
	}
	run := lastRun(t, ctx)
	for _, c := range plan.Cards {
		j := journalled(t, run, c.Key)
		if j.PriorState != "open" || !reflect.DeepEqual(j.PriorLabels, uatLabels) || j.Outcome != reaplog.OutcomeClosed || j.Class != c.Lane {
			t.Errorf("%s journalled %+v, want prior open with the pre-close labels, closed, class %s", c.Key, j, c.Lane)
		}
		want := []string{LabelMarker, PhaseLabel("p"), StatusLabel(c.Terminal)}
		if got := f.issues[c.Key].Labels; !reflect.DeepEqual(got, want) {
			t.Errorf("%s labels = %v, want %v", c.Key, got, want)
		}
	}
	if j := journalled(t, run, "PROJ-3"); j.DroppedLink != "slug:x" {
		t.Errorf("backlog card DroppedLink = %q, want slug:x", j.DroppedLink)
	}
	for _, k := range []string{"PROJ-1", "PROJ-2"} {
		if j := journalled(t, run, k); j.DroppedLink != "" {
			t.Errorf("%s dropped %q — only the backlog lane drops its link", k, j.DroppedLink)
		}
	}
	if _, ok := ctx.Board.BacklogID("slug:x"); ok {
		t.Error("the reaped backlog key is still linked")
	}
	if id, _ := ctx.Board.BacklogID("slug:other"); id != "PROJ-99" {
		t.Error("an unrelated backlog key was dropped")
	}
	if _, ok := ctx.Board.PhaseIssue("p"); !ok {
		t.Error("the phase link was dropped")
	}
	if !strings.Contains(out.String(), "reaped 3 card(s)") {
		t.Errorf("narration = %q", out.String())
	}
}

// TestApplyIsolatesPerCardFailures: one card's failure never stops the rest;
// each failure is journalled and named, and the run exits non-zero.
func TestApplyIsolatesPerCardFailures(t *testing.T) {
	f := newFaultBoard()
	for _, k := range []string{"A-1", "B-1", "C-1", "D-1"} {
		f.seed(forge.Issue{Key: k, Labels: uatLabels})
	}
	f.failGet["A-1"] = errors.New("unreachable")
	f.nilGet["B-1"] = true
	f.refuseClose["C-1"] = true
	f.failUpdate["D-1"] = errors.New("tags locked")
	ctx, out := boardCtx(t, f)
	plan := &ReapPlan{Cards: []ReapCard{
		{Key: "A-1", Lane: "Phases", Terminal: "complete"},
		{Key: "B-1", Lane: "Phases", Terminal: "complete"},
		{Key: "C-1", Lane: "Phases", Terminal: "complete"},
		{Key: "D-1", Lane: "Phases", Terminal: "complete"},
	}}

	var err error
	stderr := captureStderr(t, func() { err = Apply(ctx, plan) })
	if err == nil || err.Error() != "3 of 4 card(s) could not be closed" {
		t.Fatalf("err = %v, want 3 of 4", err)
	}
	if f.issues["D-1"].State != "closed" {
		t.Error("the healthy card was not closed")
	}
	run := lastRun(t, ctx)
	for _, k := range []string{"A-1", "B-1", "C-1"} {
		if j := journalled(t, run, k); j.Outcome != reaplog.OutcomeFailed {
			t.Errorf("%s outcome %q, want failed", k, j.Outcome)
		}
		if !strings.Contains(stderr, "  "+k+": ") {
			t.Errorf("stderr does not name %s:\n%s", k, stderr)
		}
	}
	if !strings.Contains(stderr, "B-1: read prior state: issue not found") {
		t.Errorf("a nil read is not named as not found:\n%s", stderr)
	}
	if j := journalled(t, run, "D-1"); j.Outcome != reaplog.OutcomeClosed {
		t.Errorf("D-1 outcome %q — a failed relabel must not fail a verified close", j.Outcome)
	}
	if !strings.Contains(stderr, "closed D-1 but could not update its status label") {
		t.Errorf("the failed relabel was not warned about:\n%s", stderr)
	}
	if !strings.Contains(out.String(), "reaped 1 card(s), 3 failed:") {
		t.Errorf("narration = %q", out.String())
	}
}

// TestRelabelKeepsIdentityLabelsAndSkipsNoOps: the relabel swaps only the
// status label, writes nothing when the card already reads terminal, and
// collapses stale status labels to one.
func TestRelabelKeepsIdentityLabelsAndSkipsNoOps(t *testing.T) {
	f := newFaultBoard()
	for _, k := range []string{"N-1", "S-1", "M-1", "F-1"} {
		f.seed(forge.Issue{Key: k})
	}
	f.failUpdate["F-1"] = errors.New("locked")
	ctx, _ := boardCtx(t, f)
	card := func(k string) ReapCard { return ReapCard{Key: k, Lane: "Phases", Terminal: "complete"} }

	if err := relabelReapedCard(ctx, card("N-1"), nil); err != nil {
		t.Fatal(err)
	}
	if got := f.issues["N-1"].Labels; !reflect.DeepEqual(got, []string{StatusLabel("complete")}) {
		t.Errorf("no prior labels -> %v, want only the terminal status", got)
	}

	if err := relabelReapedCard(ctx, card("S-1"), []string{LabelMarker, PhaseLabel("p"), StatusLabel("complete")}); err != nil {
		t.Fatal(err)
	}
	if got := f.callsOf("UpdateIssue"); !reflect.DeepEqual(got, []string{"UpdateIssue N-1"}) {
		t.Errorf("UpdateIssue calls = %v — a card already on its terminal label must not be rewritten", got)
	}

	if err := relabelReapedCard(ctx, card("M-1"), []string{StatusLabel("complete"), StatusLabel("uat")}); err != nil {
		t.Fatal(err)
	}
	if got := f.issues["M-1"].Labels; !reflect.DeepEqual(got, []string{StatusLabel("complete")}) {
		t.Errorf("right + stale status -> %v, want exactly one status label", got)
	}

	if err := relabelReapedCard(ctx, card("F-1"), nil); err == nil || !strings.HasPrefix(err.Error(), "board:") {
		t.Errorf("failed relabel = %v, want a board: error", err)
	}
}

// TestApplyWithNothingClosedWritesNoJournal: a run with no cards leaves no
// ledger and never shadows the last real run; a ledger that cannot be read is
// an error, not a fresh start.
func TestApplyWithNothingClosedWritesNoJournal(t *testing.T) {
	f := newFaultBoard()
	f.seed(forge.Issue{Key: "PROJ-1", Labels: uatLabels})
	ctx, _ := boardCtx(t, f)

	if err := Apply(ctx, &ReapPlan{}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(reaplog.FilePath(ctx.Root)); !os.IsNotExist(err) {
		t.Errorf("an empty apply wrote a ledger (stat err %v)", err)
	}

	if err := Apply(ctx, &ReapPlan{Cards: []ReapCard{{Key: "PROJ-1", Lane: "Phases", Terminal: "complete"}}}); err != nil {
		t.Fatal(err)
	}
	before := *lastRun(t, ctx)
	if err := Apply(ctx, &ReapPlan{}); err != nil {
		t.Fatal(err)
	}
	if after := *lastRun(t, ctx); !reflect.DeepEqual(after, before) {
		t.Errorf("a no-op apply replaced the last run: %+v -> %+v", before, after)
	}

	g := newFaultBoard()
	g.seed(forge.Issue{Key: "PROJ-1", Labels: uatLabels})
	gctx, _ := boardCtx(t, g)
	mustWrite(t, reaplog.FilePath(gctx.Root), "{not json")
	if err := Apply(gctx, &ReapPlan{Cards: []ReapCard{{Key: "PROJ-1", Lane: "Phases", Terminal: "complete"}}}); err == nil {
		t.Error("a corrupt ledger was silently replaced")
	}
}

// TestBoardNamespaceNamesAreTheMapFields: the --namespace vocabulary is read
// off board.Board's map fields.
func TestBoardNamespaceNamesAreTheMapFields(t *testing.T) {
	if got, want := BoardNamespaceNames(), []string{"Backlog", "Milestones", "Phases", "Quicks", "Tasks"}; !reflect.DeepEqual(got, want) {
		t.Errorf("BoardNamespaceNames = %v, want %v", got, want)
	}
}

// TestValidateReapNamespacesNamesUnknowns: matching is case- and
// space-insensitive, and a refusal names every unknown and the real set.
func TestValidateReapNamespacesNamesUnknowns(t *testing.T) {
	for _, ok := range [][]string{nil, {}, {" BACKLOG "}, {" Phases "}} {
		if err := ValidateReapNamespaces(ok); err != nil {
			t.Errorf("ValidateReapNamespaces(%q) = %v, want nil", ok, err)
		}
	}
	err := ValidateReapNamespaces([]string{"phases", "bogus", "nope"})
	if err == nil {
		t.Fatal("unknown namespaces were accepted")
	}
	if msg := err.Error(); !strings.Contains(msg, "unknown --namespace bogus, nope;") || !strings.Contains(msg, "Backlog, Milestones, Phases, Quicks, Tasks") {
		t.Errorf("err = %q, want both unknowns named and all five listed", msg)
	}
}

// TestInventoryDedupesLinkedAndOrphanCards: the inventory is the linked plan
// plus the unlinked marker cards, each card once, with the cards no record
// speaks for handed back separately.
func TestInventoryDedupesLinkedAndOrphanCards(t *testing.T) {
	f := &discoverYT{resolved: map[string]bool{}, cards: []ytCard{
		{key: "DRO-1", labels: []string{LabelMarker, PhaseLabel("01-a")}},
		{key: "DRO-2", labels: []string{LabelMarker, PhaseLabel("02-b")}},
		{key: "DRO-3", labels: []string{LabelMarker}},
	}}
	dir, ctx := discoverRepo(t, f, `{"phases":{"01-a":"DRO-1"},"tasks":{},"quicks":{},"milestones":{}}`)
	writeChanges(t, dir, "01-a", "complete")
	writeChanges(t, dir, "02-b", "complete")

	plan, orphans, err := Inventory(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	keys := cardKeys(plan.Cards)
	sort.Strings(keys)
	if !reflect.DeepEqual(keys, []string{"DRO-1", "DRO-2"}) {
		t.Errorf("planned %v, want the linked DRO-1 and the orphan DRO-2, once each", keys)
	}
	if got := cardKeys(orphans); !reflect.DeepEqual(got, []string{"DRO-3"}) {
		t.Errorf("orphans = %v, want the unclassifiable DRO-3", got)
	}

	t.Run("unknown namespace is refused before any request", func(t *testing.T) {
		tr := strictTracker(t)
		_, yctx := ytRepoMode(t, tr, `{}`, "epic")
		if plan, _, err := Inventory(yctx, []string{"bogus"}); err == nil || plan != nil {
			t.Errorf("= (%v, %v), want (nil, error)", plan, err)
		}
	})

	t.Run("discovery failure is an error, not a partial plan", func(t *testing.T) {
		tr := strictTracker(t)
		tr.on("GET", "/api/issueTags", `[{"id":"t1","name":"dross"}]`)
		tr.fail("GET", "/api/issues")
		_, yctx := ytRepoMode(t, tr, `{}`, "epic")
		if plan, orphans, err := Inventory(yctx, nil); err == nil || plan != nil || orphans != nil {
			t.Errorf("= (%v, %v, %v), want (nil, nil, error)", plan, orphans, err)
		}
	})
}

// TestReapFailureAndLedgerPath pins the small pieces the sweep reports with.
func TestReapFailureAndLedgerPath(t *testing.T) {
	root := t.TempDir()
	if got, want := reapLogPathFor(root), reaplog.FilePath(root); got != want {
		t.Errorf("reapLogPathFor = %q, want %q", got, want)
	}
	inner := errors.New("refused")
	f := &reapFailure{key: "K-1", err: inner}
	if f.Error() != "K-1: refused" || !errors.Is(errors.Unwrap(f), inner) {
		t.Errorf("reapFailure = %q / %v", f.Error(), errors.Unwrap(f))
	}
}
