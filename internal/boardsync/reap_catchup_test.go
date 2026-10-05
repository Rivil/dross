package boardsync

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/forge"
	"github.com/Rivil/dross/internal/reaplog"
)

// catchUpPhase writes phase id (spec, an optional plan with tasks, and
// changes.json at status) under ctx's root.
func catchUpPhase(t *testing.T, ctx *Ctx, id, status string, withSpec bool, tasks ...string) {
	t.Helper()
	dir := filepath.Join(ctx.Root, "phases", id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if withSpec {
		write("spec.toml", "[phase]\nid = \""+id+"\"\ntitle = \""+strings.ToUpper(id)+"\"\n\n[[criteria]]\nid = \"c-1\"\ntext = \"x\"\n")
	}
	if len(tasks) > 0 {
		var b strings.Builder
		b.WriteString("[phase]\nid = \"" + id + "\"\n")
		for _, tid := range tasks {
			b.WriteString("\n[[task]]\nid = \"" + tid + "\"\nwave = 1\ntitle = \"" + tid + "\"\nstatus = \"done\"\n")
		}
		write("plan.toml", b.String())
	}
	write("changes.json", `{"phase":"`+id+`","status":"`+status+`","tasks":{}}`)
}

func writeCalls(f *faultBoard) []string {
	var out []string
	for _, c := range f.calls {
		if !strings.HasPrefix(c, "GetIssue ") && !strings.HasPrefix(c, "ListIssues ") {
			out = append(out, c)
		}
	}
	return out
}

// TestFindMissingListsAnUnknownPhase: a complete phase the board has never
// heard of is listed with every task, through one per-phase lookup and never a
// bulk marker listing — and nothing is written.
func TestFindMissingListsAnUnknownPhase(t *testing.T) {
	f := newFaultBoard()
	ctx, _ := boardCtx(t, f)
	catchUpPhase(t, ctx, "p", "complete", true, "t-1", "t-2", "t-3")

	missing, err := FindMissing(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(missing) != 1 || !missing[0].PhaseCard || strings.Join(missing[0].Tasks, ",") != "t-1,t-2,t-3" {
		t.Fatalf("missing = %+v, want p with its phase card and three tasks", missing)
	}
	if got := f.callsOf("ListIssues"); len(got) != 1 || got[0] != "ListIssues "+PhaseLabel("p") {
		t.Errorf("lookups = %v, want exactly one dross/phase:p query and no marker listing", got)
	}
	if w := writeCalls(f); len(w) != 0 {
		t.Errorf("detection wrote: %v", w)
	}
}

// TestFindMissingSkipsWhatExists: a closed card absent from board.json still
// counts (the lookup reads every state); with the phase card present only the
// card-less task is listed; a phase that only shipped is not touched.
func TestFindMissingSkipsWhatExists(t *testing.T) {
	f := newFaultBoard()
	ctx, _ := boardCtx(t, f)
	catchUpPhase(t, ctx, "closed", "complete", true)
	f.seed(forge.Issue{Key: "PROJ-1", State: "closed", Labels: []string{LabelMarker, PhaseLabel("closed")}})
	catchUpPhase(t, ctx, "partial", "complete", true, "t-1", "t-2")
	f.seed(forge.Issue{Key: "PROJ-2", Labels: []string{LabelMarker, PhaseLabel("partial")}})
	f.seed(forge.Issue{Key: "PROJ-3", Labels: []string{LabelMarker, PhaseLabel("partial"), TaskLabel("partial", "t-1")}})
	catchUpPhase(t, ctx, "shipped", "shipped", true, "t-1")

	missing, err := FindMissing(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(missing) != 1 || missing[0].Phase != "partial" || missing[0].PhaseCard || strings.Join(missing[0].Tasks, ",") != "t-2" {
		t.Errorf("missing = %+v, want only partial's t-2", missing)
	}
}

// TestApplyMissingCreatesAtTerminal: --apply creates the cards straight at
// their terminal state, closed, and a second look finds nothing missing.
func TestApplyMissingCreatesAtTerminal(t *testing.T) {
	f := newFaultBoard()
	ctx, _ := boardCtx(t, f)
	catchUpPhase(t, ctx, "p", "complete", true, "t-1", "t-2")
	missing, err := FindMissing(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := ApplyMissing(ctx, missing); err != nil {
		t.Fatal(err)
	}
	key, ok := ctx.Board.PhaseIssue("p")
	if !ok {
		t.Fatal("no phase card was linked")
	}
	wantTerminal(t, f, key, StatusComplete)
	for _, tid := range []string{"t-1", "t-2"} {
		k, ok := ctx.Board.TaskIssue("p", tid)
		if !ok {
			t.Fatalf("no card linked for %s", tid)
		}
		wantTerminal(t, f, k, StatusTaskComplete)
	}
	again, err := FindMissing(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(again) != 0 {
		t.Errorf("after --apply, still missing: %+v", again)
	}
}

// TestApplyMissingContinuesPastAFailure: one phase whose create fails is named,
// and the next phase is still created.
func TestApplyMissingContinuesPastAFailure(t *testing.T) {
	f := newFaultBoard()
	ctx, _ := boardCtx(t, f)
	catchUpPhase(t, ctx, "a", "complete", true)
	catchUpPhase(t, ctx, "b", "complete", true)
	f.failCreate["a — A"] = os.ErrPermission
	missing, err := FindMissing(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	err = ApplyMissing(ctx, missing)
	if err == nil || !strings.Contains(err.Error(), "a:") {
		t.Fatalf("err = %v, want phase a named", err)
	}
	if key, ok := ctx.Board.PhaseIssue("b"); !ok {
		t.Error("phase b was not created after a failed")
	} else {
		wantTerminal(t, f, key, StatusComplete)
	}
}

// TestFindMissingRespectsNamespace: a sweep scoped away from the phase and task
// lanes makes no catch-up lookup at all.
func TestFindMissingRespectsNamespace(t *testing.T) {
	f := newFaultBoard()
	ctx, _ := boardCtx(t, f)
	catchUpPhase(t, ctx, "p", "complete", true, "t-1")
	missing, err := FindMissing(ctx, []string{"Backlog"})
	if err != nil {
		t.Fatal(err)
	}
	if len(missing) != 0 || len(f.calls) != 0 {
		t.Errorf("missing = %+v, calls = %v; want nothing looked up", missing, f.calls)
	}
}

// TestFindMissingNamespaceAnyCase: --namespace is matched the way the sweep
// validates it — trimmed, any case — so `phases` scopes catch-up in.
func TestFindMissingNamespaceAnyCase(t *testing.T) {
	for _, ns := range []string{"phases", " Tasks ", "PHASES"} {
		f := newFaultBoard()
		ctx, _ := boardCtx(t, f)
		catchUpPhase(t, ctx, "p", "complete", true, "t-1")
		missing, err := FindMissing(ctx, []string{ns})
		if err != nil {
			t.Fatal(err)
		}
		if len(missing) != 1 {
			t.Errorf("--namespace %q: missing = %+v, want p listed", ns, missing)
		}
	}
}

// TestFindMissingNoSpecCannotCreate: a complete phase with no spec.toml is
// listed as cannot-create, and --apply skips it.
func TestFindMissingNoSpecCannotCreate(t *testing.T) {
	f := newFaultBoard()
	ctx, _ := boardCtx(t, f)
	catchUpPhase(t, ctx, "nospec", "complete", false)
	missing, err := FindMissing(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(missing) != 1 || missing[0].CannotCreate == "" {
		t.Fatalf("missing = %+v, want nospec listed as cannot-create", missing)
	}
	if err := ApplyMissing(ctx, missing); err != nil {
		t.Fatal(err)
	}
	if len(f.created) != 0 {
		t.Errorf("--apply created %d card(s) for a phase it cannot build", len(f.created))
	}
}

// TestUndoLeavesCatchUpCards: one sweep both closes a stranded card and
// catches a phase up. The journal holds only the close, so undo restores that
// one card and touches none of the cards catch-up created.
func TestUndoLeavesCatchUpCards(t *testing.T) {
	sb := newStateBoard()
	ctx, _ := boardCtx(t, sb)
	sb.seed(forge.Issue{Key: "PROJ-90", Labels: []string{LabelMarker, PhaseLabel("old"), StatusLabel("shipped")}})
	catchUpPhase(t, ctx, "p", "complete", true, "t-1")
	plan := &ReapPlan{Cards: []ReapCard{{Key: "PROJ-90", Lane: "Phases", Terminal: StatusComplete, Why: "stranded"}}}
	var err error
	if plan.Missing, err = FindMissing(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if err := Apply(ctx, plan); err != nil {
		t.Fatal(err)
	}
	log, err := reaplog.Load(reaplog.FilePath(ctx.Root))
	if err != nil {
		t.Fatal(err)
	}
	closed := log.Last().Closed()
	if len(closed) != 1 || closed[0].Issue != "PROJ-90" {
		t.Fatalf("journal = %+v, want only the stranded PROJ-90", closed)
	}
	created, _ := ctx.Board.PhaseIssue("p")
	sb.calls = nil
	if err := Undo(ctx); err != nil {
		t.Fatal(err)
	}
	for _, c := range writeCalls(sb.faultBoard) {
		if !strings.HasSuffix(c, " PROJ-90") {
			t.Errorf("undo wrote to a card catch-up created: %s", c)
		}
	}
	if len(sb.callsOf("SetStateRaw")) == 0 {
		t.Error("undo restored nothing — the journaled close was not replayed")
	}
	if sb.issues[created].State != "closed" {
		t.Errorf("catch-up's phase card %s was reopened by undo", created)
	}
}
