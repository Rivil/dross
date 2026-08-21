package cmd

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

// readOnlyYT is a YouTrack stand-in that serves reads and FAILS THE TEST on any
// request that is not a GET.
//
// That inversion is the point of the fixture, not a detail of it: classify is
// the half of reap that must be safe to run on a live board at any time, so a
// classifier that reached for a write — or that decided a verdict by patching a
// card and seeing what stuck — has to redden here rather than on someone's
// tracker. `resolved` names the cards the tracker already holds done.
type readOnlyYT struct {
	resolved map[string]bool
	gets     int
}

func (f *readOnlyYT) handler(t *testing.T) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			t.Errorf("classify issued a %s to %s — classification is read-only", r.Method, r.URL.Path)
			_, _ = io.WriteString(w, `{}`)
			return
		}
		switch {
		case r.URL.Path == "/api/issueTags":
			_, _ = io.WriteString(w, `[]`)
		case r.URL.Path == "/api/issues":
			_, _ = io.WriteString(w, `[]`)
		case strings.HasPrefix(r.URL.Path, "/api/issues/"):
			f.gets++
			key := strings.TrimPrefix(r.URL.Path, "/api/issues/")
			resolved := "null"
			if f.resolved[key] {
				resolved = "1700000000000"
			}
			_, _ = io.WriteString(w, `{"idReadable":"`+key+`","resolved":`+resolved+`}`)
		default:
			t.Errorf("unexpected GET %s", r.URL.Path)
			_, _ = io.WriteString(w, `{}`)
		}
	}
}

// reapRepo scaffolds a YouTrack board in epic mode — the one shape whose
// milestone slot holds an issue — with the given board.json, and returns the
// repo dir plus a live boardCtx.
func reapRepo(t *testing.T, f *readOnlyYT, boardJSON string) (string, *boardCtx) {
	t.Helper()
	srv := httptest.NewServer(f.handler(t))
	t.Cleanup(srv.Close)
	dir := youtrackBoardRepo(t, srv.URL)
	mustRunSet(t, "board.milestone_mode", "epic")
	mustWrite(t, filepath.Join(dir, ".dross", "board.json"), boardJSON)
	ctx, enabled, err := openBoard()
	if err != nil {
		t.Fatalf("openBoard: %v", err)
	}
	if !enabled {
		t.Fatal("board sync must be enabled in the fixture")
	}
	return dir, ctx
}

// writeChanges writes a phase's completion record. status "" writes a record
// with no status field at all — the pre-status shape, which reads as "unknown",
// never as "done".
func writeChanges(t *testing.T, dir, slug, status string) {
	t.Helper()
	rec := map[string]any{"phase": slug, "tasks": map[string]any{}}
	if status != "" {
		rec["status"] = status
	}
	b, err := json.Marshal(rec)
	if err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(dir, ".dross", "phases", slug, "changes.json"), string(b))
}

// cardKeys flattens a plan list to its issue keys, for order-independent
// comparison.
func cardKeys(cards []reapCard) []string {
	out := make([]string, 0, len(cards))
	for _, c := range cards {
		out = append(out, c.Key)
	}
	return out
}

func hasKey(cards []reapCard, key string) bool {
	for _, c := range cards {
		if c.Key == key {
			return true
		}
	}
	return false
}

func cardFor(t *testing.T, cards []reapCard, key string) reapCard {
	t.Helper()
	for _, c := range cards {
		if c.Key == key {
			return c
		}
	}
	t.Fatalf("no card %s in %v", key, cardKeys(cards))
	return reapCard{}
}

// phaseAndTaskBoard links one phase card and two of its task cards.
const phaseAndTaskBoard = `{
  "phases": {"01-auth": "PROJ-1"},
  "tasks": {"01-auth/t-1": {"issue": "PROJ-2"}, "01-auth/t-2": {"issue": "PROJ-3"}},
  "quicks": {}, "milestones": {}
}`

// TestPhaseCardIsClassifiedFromItsCompletionRecord is the load-bearing case for
// c-3. The two fixtures hold the BOARD STATE IDENTICAL and vary only the record
// on disk, so a classifier that read iss.State, iss.Resolved or the
// dross/status label to decide strandedness would return the same answer twice
// and fail one half of this test whichever way it leaned.
func TestPhaseCardIsClassifiedFromItsCompletionRecord(t *testing.T) {
	t.Run("record complete yields the phase card and its tasks", func(t *testing.T) {
		f := &readOnlyYT{resolved: map[string]bool{}}
		dir, ctx := reapRepo(t, f, phaseAndTaskBoard)
		writeChanges(t, dir, "01-auth", "complete")

		plan, err := classifyReap(ctx, nil)
		if err != nil {
			t.Fatalf("classify: %v", err)
		}
		for _, want := range []string{"PROJ-1", "PROJ-2", "PROJ-3"} {
			if !hasKey(plan.Cards, want) {
				t.Errorf("card %s missing from the plan; got %v", want, cardKeys(plan.Cards))
			}
		}
	})

	t.Run("record with no status yields nothing", func(t *testing.T) {
		f := &readOnlyYT{resolved: map[string]bool{}}
		dir, ctx := reapRepo(t, f, phaseAndTaskBoard)
		writeChanges(t, dir, "01-auth", "") // same cards, same board state

		plan, err := classifyReap(ctx, nil)
		if err != nil {
			t.Fatalf("classify: %v", err)
		}
		if len(plan.Cards) != 0 {
			t.Errorf("plan closes %v off a record with no status — an unknown status is not done", cardKeys(plan.Cards))
		}
	})
}

// TestPhaseAndTaskCardsCarryTheirOwnTerminal: the two lanes end differently and
// the classifier must say so per card, not once per run. A single shared
// terminal would leave every task card in the phase lane's `complete`.
func TestPhaseAndTaskCardsCarryTheirOwnTerminal(t *testing.T) {
	f := &readOnlyYT{resolved: map[string]bool{}}
	dir, ctx := reapRepo(t, f, phaseAndTaskBoard)
	writeChanges(t, dir, "01-auth", "complete")

	plan, err := classifyReap(ctx, nil)
	if err != nil {
		t.Fatalf("classify: %v", err)
	}
	if got := cardFor(t, plan.Cards, "PROJ-1").Terminal; got != "complete" {
		t.Errorf("phase card terminal = %q, want complete", got)
	}
	if got := cardFor(t, plan.Cards, "PROJ-2").Terminal; got != statusTaskComplete {
		t.Errorf("task card terminal = %q, want %s", got, statusTaskComplete)
	}
}

// TestShippedRecordIsNotYetComplete: shipped is a live forward state, not a
// stranded one — the phase's finalize half has not run, and reaping it to
// `complete` would announce a completion no record carries.
func TestShippedRecordIsNotYetComplete(t *testing.T) {
	f := &readOnlyYT{resolved: map[string]bool{}}
	dir, ctx := reapRepo(t, f, phaseAndTaskBoard)
	writeChanges(t, dir, "01-auth", "shipped")

	plan, err := classifyReap(ctx, nil)
	if err != nil {
		t.Fatalf("classify: %v", err)
	}
	if len(plan.Cards) != 0 {
		t.Errorf("plan closes %v off a shipped record; only complete reaches the phase lane's terminal", cardKeys(plan.Cards))
	}
}

// TestPhaseCardWithNoDirectoryIsUnattributable: a record that is absent is not
// a record that says no. The card is named, never closed.
func TestPhaseCardWithNoDirectoryIsUnattributable(t *testing.T) {
	f := &readOnlyYT{resolved: map[string]bool{}}
	_, ctx := reapRepo(t, f, `{"phases":{"01-gone":"PROJ-9"},"tasks":{},"quicks":{},"milestones":{}}`)

	plan, err := classifyReap(ctx, nil)
	if err != nil {
		t.Fatalf("classify: %v", err)
	}
	if hasKey(plan.Cards, "PROJ-9") {
		t.Error("a phase with no directory on disk was classified stranded")
	}
	if !hasKey(plan.Unattributable, "PROJ-9") {
		t.Errorf("PROJ-9 vanished from the plan entirely; unattributable = %v", cardKeys(plan.Unattributable))
	}
}

// TestEpicFollowsMilestoneStatusNotTheCard: the epic's own state says nothing.
// milestone.toml is the record.
func TestEpicFollowsMilestoneStatusNotTheCard(t *testing.T) {
	const bd = `{"phases":{},"tasks":{},"quicks":{},"milestones":{"v1.5":"PROJ-7"}}`

	t.Run("active milestone leaves the epic open", func(t *testing.T) {
		f := &readOnlyYT{resolved: map[string]bool{}}
		dir, ctx := reapRepo(t, f, bd)
		writeMilestoneToml(t, filepath.Join(dir, ".dross"), "v1.5", "active", "")

		plan, err := classifyReap(ctx, nil)
		if err != nil {
			t.Fatalf("classify: %v", err)
		}
		if hasKey(plan.Cards, "PROJ-7") {
			t.Error("an epic whose milestone is still active was classified stranded")
		}
	})

	t.Run("complete milestone yields the epic", func(t *testing.T) {
		f := &readOnlyYT{resolved: map[string]bool{}}
		dir, ctx := reapRepo(t, f, bd)
		writeMilestoneToml(t, filepath.Join(dir, ".dross"), "v1.5", "complete", "")

		plan, err := classifyReap(ctx, nil)
		if err != nil {
			t.Fatalf("classify: %v", err)
		}
		if !hasKey(plan.Cards, "PROJ-7") {
			t.Errorf("the epic of a complete milestone is not in the plan; got %v", cardKeys(plan.Cards))
		}
	})
}

// TestSlugBacklogNeedsItsPhaseDir: a `slug:` mirror resolves because the slug
// was scaffolded. A slug that simply left the set — renamed, or another
// milestone's — is unattributable, not resolved.
func TestSlugBacklogNeedsItsPhaseDir(t *testing.T) {
	f := &readOnlyYT{resolved: map[string]bool{}}
	dir, ctx := reapRepo(t, f, `{"phases":{},"tasks":{},"quicks":{},"milestones":{},
	  "backlog":{"slug:built":"PROJ-20","slug:renamed":"PROJ-21"}}`)
	writeChanges(t, dir, "built", "complete") // creates the phase directory too

	plan, err := classifyReap(ctx, nil)
	if err != nil {
		t.Fatalf("classify: %v", err)
	}
	if !hasKey(plan.Cards, "PROJ-20") {
		t.Errorf("a scaffolded slug's mirror is not in the plan; got %v", cardKeys(plan.Cards))
	}
	if hasKey(plan.Cards, "PROJ-21") {
		t.Error("a slug with no phase directory was classified stranded")
	}
	if !hasKey(plan.Unattributable, "PROJ-21") {
		t.Errorf("PROJ-21 was dropped instead of named; unattributable = %v", cardKeys(plan.Unattributable))
	}
}

// TestRoutedBacklogFollowsItsTargetRecord: a routed item resolves only once the
// phase it was routed INTO has completed — read from that phase's changes.json,
// never from its card.
func TestRoutedBacklogFollowsItsTargetRecord(t *testing.T) {
	seed := func(t *testing.T, targetStatus string) *reapPlan {
		t.Helper()
		f := &readOnlyYT{resolved: map[string]bool{}}
		dir, ctx := reapRepo(t, f, `{"phases":{},"tasks":{},"quicks":{},"milestones":{},
		  "backlog":{"someday:id:abc123":"PROJ-30"}}`)
		writeSpec(t, dir, "01-src", "[phase]\nid=\"01-src\"\ntitle=\"Src\"\n\n"+
			"[[deferred]]\n  id = \"abc123\"\n  text = \"an idea\"\n  target = \"02-dest\"\n")
		writeChanges(t, dir, "02-dest", targetStatus)
		plan, err := classifyReap(ctx, nil)
		if err != nil {
			t.Fatalf("classify: %v", err)
		}
		return plan
	}

	if plan := seed(t, "complete"); !hasKey(plan.Cards, "PROJ-30") {
		t.Errorf("a routed item whose target completed is not in the plan; got %v", cardKeys(plan.Cards))
	}
	if plan := seed(t, ""); hasKey(plan.Cards, "PROJ-30") {
		t.Error("a routed item was closed while its target phase's record shows no completion")
	}
}

// TestQuickIsNeverAutoClosed: version ordering is not a completion record.
// state.json's counter proves a LATER bump happened, which is equally true of a
// quick that shipped and one abandoned halfway.
func TestQuickIsNeverAutoClosed(t *testing.T) {
	f := &readOnlyYT{resolved: map[string]bool{}}
	_, ctx := reapRepo(t, f, `{"phases":{},"tasks":{},"milestones":{},"quicks":{"1.0.0.1":"PROJ-40"}}`)
	// The repo's own version is well past the quick's ref.
	if err := runCmd(t, State(), "set", "version", "1.5.4.0"); err != nil {
		t.Fatalf("set version: %v", err)
	}

	plan, err := classifyReap(ctx, nil)
	if err != nil {
		t.Fatalf("classify: %v", err)
	}
	if hasKey(plan.Cards, "PROJ-40") {
		t.Error("a quick was classified stranded on version ordering alone — that is not a completion record")
	}
	if !hasKey(plan.Unattributable, "PROJ-40") {
		t.Errorf("the quick was dropped rather than named for a human; unattributable = %v", cardKeys(plan.Unattributable))
	}
}

// TestAlreadyTerminalIsNotStranded is what makes c-4's post-sweep dry run print
// an honestly empty plan rather than re-listing everything it just closed.
func TestAlreadyTerminalIsNotStranded(t *testing.T) {
	f := &readOnlyYT{resolved: map[string]bool{"PROJ-1": true, "PROJ-2": true, "PROJ-3": true}}
	dir, ctx := reapRepo(t, f, phaseAndTaskBoard)
	writeChanges(t, dir, "01-auth", "complete")

	plan, err := classifyReap(ctx, nil)
	if err != nil {
		t.Fatalf("classify: %v", err)
	}
	if len(plan.Cards) != 0 || len(plan.Unattributable) != 0 {
		t.Errorf("cards the tracker already holds resolved are still in the plan: %v / %v",
			cardKeys(plan.Cards), cardKeys(plan.Unattributable))
	}
	if f.gets == 0 {
		t.Fatal("no card was read back — the already-terminal filter cannot have run")
	}
}

// TestEveryPlanCardNamesItsJustifyingRecord: a plan line with no `why` is an
// unauditable close.
func TestEveryPlanCardNamesItsJustifyingRecord(t *testing.T) {
	f := &readOnlyYT{resolved: map[string]bool{}}
	dir, ctx := reapRepo(t, f, phaseAndTaskBoard)
	writeChanges(t, dir, "01-auth", "complete")

	plan, err := classifyReap(ctx, nil)
	if err != nil {
		t.Fatalf("classify: %v", err)
	}
	if len(plan.Cards) == 0 {
		t.Fatal("empty plan — nothing to check")
	}
	for _, c := range plan.Cards {
		if strings.TrimSpace(c.Why) == "" {
			t.Errorf("card %s (%s) names no justifying record", c.Key, c.Lane)
		}
	}
}
