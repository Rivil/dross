package cmd

import (
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

// ytCard is one card the discovery fake serves.
type ytCard struct {
	key    string
	labels []string
}

// discoverYT serves the two reads discovery makes — the tag index and a
// label-filtered issue query — and fails the test on any write, for the same
// reason the classify fixture does: discovery is part of the read-only half.
type discoverYT struct {
	cards []ytCard
	// resolved names the cards the tracker already holds done.
	resolved map[string]bool
	// queried records the label query dross actually sent, so a sweep that
	// silently widened past the marker is visible.
	queried string
}

func (f *discoverYT) handler(t *testing.T) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			t.Errorf("discovery issued a %s to %s — it is read-only", r.Method, r.URL.Path)
			_, _ = io.WriteString(w, `{}`)
			return
		}
		switch {
		case r.URL.Path == "/api/issueTags":
			// Every label any fixture uses has to exist, or YouTrack's
			// ListIssues drops it from the query and returns nothing.
			var rows []string
			for _, name := range f.tagNames() {
				rows = append(rows, `{"id":"tid-`+name+`","name":"`+name+`"}`)
			}
			_, _ = io.WriteString(w, "["+strings.Join(rows, ",")+"]")
		case r.URL.Path == "/api/issues":
			f.queried = r.URL.Query().Get("query")
			var rows []string
			for _, c := range f.cards {
				rows = append(rows, f.render(c))
			}
			_, _ = io.WriteString(w, "["+strings.Join(rows, ",")+"]")
		case strings.HasPrefix(r.URL.Path, "/api/issues/"):
			key := strings.TrimPrefix(r.URL.Path, "/api/issues/")
			for _, c := range f.cards {
				if c.key == key {
					_, _ = io.WriteString(w, f.render(c))
					return
				}
			}
			_, _ = io.WriteString(w, `{"idReadable":"`+key+`","resolved":null}`)
		default:
			t.Errorf("unexpected GET %s", r.URL.Path)
			_, _ = io.WriteString(w, `{}`)
		}
	}
}

func (f *discoverYT) render(c ytCard) string {
	var tags []string
	for _, l := range c.labels {
		tags = append(tags, `{"id":"tid-`+l+`","name":"`+l+`"}`)
	}
	resolved := "null"
	if f.resolved[c.key] {
		resolved = "1700000000000"
	}
	return `{"idReadable":"` + c.key + `","resolved":` + resolved + `,"tags":[` + strings.Join(tags, ",") + `]}`
}

func (f *discoverYT) tagNames() []string {
	seen := map[string]bool{labelMarker: true}
	out := []string{labelMarker}
	for _, c := range f.cards {
		for _, l := range c.labels {
			if !seen[l] {
				seen[l] = true
				out = append(out, l)
			}
		}
	}
	return out
}

// discoverRepo is the classify fixture's sibling for the marker sweep.
func discoverRepo(t *testing.T, f *discoverYT, boardJSON string) (string, *boardCtx) {
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

const emptyBoard = `{"phases":{},"tasks":{},"quicks":{},"milestones":{}}`

// TestUnlinkedMirrorIsDiscovered is c-7's shape: a card dross authored, whose
// board.json link died with the phase branch, is still classified — the
// DRO-33/36/37/38/95/96 case.
//
// The negative half is the load-bearing one: the SAME fixture is run through
// the link-only classifier first and must find nothing. Without it the test
// would pass on a board.json entry nobody noticed, and the marker sweep could
// be deleted with the suite still green.
func TestUnlinkedMirrorIsDiscovered(t *testing.T) {
	f := &discoverYT{resolved: map[string]bool{}, cards: []ytCard{
		{key: "DRO-33", labels: []string{labelMarker, phaseLabel("01-lost")}},
	}}
	dir, ctx := discoverRepo(t, f, emptyBoard)
	writeChanges(t, dir, "01-lost", "complete")

	linked, err := classifyReap(ctx, nil)
	if err != nil {
		t.Fatalf("classify: %v", err)
	}
	if hasKey(linked.Cards, "DRO-33") {
		t.Fatal("the fixture is not actually unlinked — board.json accounts for DRO-33")
	}

	found, unclassifiable, err := discoverReap(ctx, reapLanes)
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	if len(unclassifiable) != 0 {
		t.Errorf("unclassifiable = %v, want none", cardKeys(unclassifiable))
	}
	if len(found) != 1 {
		t.Fatalf("found %d orphans, want 1", len(found))
	}
	if found[0].card.Key != "DRO-33" || found[0].card.Lane != "Phases" {
		t.Errorf("orphan = %+v, want DRO-33 in lane Phases", found[0].card)
	}
	if found[0].verdict != reapStranded {
		t.Errorf("verdict = %v, want stranded", found[0].verdict)
	}
	if !strings.Contains(f.queried, labelMarker) {
		t.Errorf("issue query %q does not filter on the dross marker", f.queried)
	}
}

// TestUnattributableMarkerCardIsNamedNotGuessed: same card, identity label
// removed. dross wrote it, so it is not a human's issue to leave alone — but
// nothing on disk speaks for it, so it is named rather than dropped or guessed
// at.
func TestUnattributableMarkerCardIsNamedNotGuessed(t *testing.T) {
	f := &discoverYT{resolved: map[string]bool{}, cards: []ytCard{{key: "DRO-33", labels: []string{labelMarker}}}}
	dir, ctx := discoverRepo(t, f, emptyBoard)
	writeChanges(t, dir, "01-lost", "complete")

	found, unclassifiable, err := discoverReap(ctx, reapLanes)
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	if len(found) != 0 {
		t.Errorf("closed %d cards with no identity label", len(found))
	}
	if len(unclassifiable) != 1 || unclassifiable[0].Key != "DRO-33" {
		t.Errorf("unclassifiable = %v, want DRO-33 named", cardKeys(unclassifiable))
	}
	if strings.TrimSpace(unclassifiable[0].Why) == "" {
		t.Error("an unclassifiable card with no reason is as useless as a dropped one")
	}
}

// TestOrphanOfAnIncompletePhaseIsAbsent: recovery says WHICH artefact a card
// mirrors; the record still says whether it is done. A live phase's orphaned
// card stays open.
func TestOrphanOfAnIncompletePhaseIsAbsent(t *testing.T) {
	f := &discoverYT{resolved: map[string]bool{}, cards: []ytCard{
		{key: "DRO-36", labels: []string{labelMarker, phaseLabel("01-live")}},
	}}
	dir, ctx := discoverRepo(t, f, emptyBoard)
	writeChanges(t, dir, "01-live", "") // scaffolded, not finished

	found, unclassifiable, err := discoverReap(ctx, reapLanes)
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	if len(found) != 0 || len(unclassifiable) != 0 {
		t.Errorf("a live phase's orphan reached the plan: %d found, %v unclassifiable", len(found), cardKeys(unclassifiable))
	}
}

// TestHumanFiledCardIsNeverInInventory: the marker is what separates dross's
// own writing from everyone else's. A card without it is invisible to the
// sweep in both directions — not closed, and not even named.
func TestHumanFiledCardIsNeverInInventory(t *testing.T) {
	f := &discoverYT{resolved: map[string]bool{}, cards: []ytCard{
		{key: "DRO-33", labels: []string{labelMarker, phaseLabel("01-lost")}},
		{key: "DRO-77", labels: []string{"bug"}}, // a human's issue
	}}
	dir, ctx := discoverRepo(t, f, emptyBoard)
	writeChanges(t, dir, "01-lost", "complete")

	found, unclassifiable, err := discoverReap(ctx, reapLanes)
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	for _, c := range found {
		if c.card.Key == "DRO-77" {
			t.Error("a human-filed card reached the close plan")
		}
	}
	if hasKey(unclassifiable, "DRO-77") {
		t.Error("a human-filed card was named as an unclassifiable dross mirror")
	}
}

// TestMarkerCardAlreadyLinkedIsNotDoubleCounted: the two inventory sources
// overlap by design, and the union must be deduped by issue key — otherwise a
// card is planned twice and the run's counts lie.
func TestMarkerCardAlreadyLinkedIsNotDoubleCounted(t *testing.T) {
	f := &discoverYT{resolved: map[string]bool{}, cards: []ytCard{
		{key: "PROJ-1", labels: []string{labelMarker, phaseLabel("01-auth")}},
	}}
	dir, ctx := discoverRepo(t, f, phaseAndTaskBoard)
	writeChanges(t, dir, "01-auth", "complete")

	linked, err := classifyReap(ctx, nil)
	if err != nil {
		t.Fatalf("classify: %v", err)
	}
	if !hasKey(linked.Cards, "PROJ-1") {
		t.Fatal("the fixture's linked card is not in the link-derived plan")
	}
	found, _, err := discoverReap(ctx, reapLanes)
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	for _, c := range found {
		if c.card.Key == "PROJ-1" {
			t.Error("a card board.json already accounts for was rediscovered — the union would plan it twice")
		}
	}
}

// TestDiscoveryRespectsTheNamespaceFilter: --namespace scopes both inventory
// sources or it scopes neither usefully.
func TestDiscoveryRespectsTheNamespaceFilter(t *testing.T) {
	f := &discoverYT{resolved: map[string]bool{}, cards: []ytCard{
		{key: "DRO-33", labels: []string{labelMarker, phaseLabel("01-lost")}},
		{key: "DRO-34", labels: []string{labelMarker, taskLabel("01-lost", "t-1")}},
	}}
	dir, ctx := discoverRepo(t, f, emptyBoard)
	writeChanges(t, dir, "01-lost", "complete")

	lanes, err := resolveReapLanes([]string{"Tasks"})
	if err != nil {
		t.Fatalf("resolve lanes: %v", err)
	}
	found, _, err := discoverReap(ctx, lanes)
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	if len(found) != 1 || found[0].card.Key != "DRO-34" {
		var got []string
		for _, c := range found {
			got = append(got, c.card.Key)
		}
		t.Errorf("found %v, want only the task-lane orphan DRO-34", got)
	}
}

// TestResolvedUnclassifiableCardIsNotALooseEnd: a card the tracker already
// holds resolved is not a loose end, whatever dross can or cannot attribute it
// to. Without the filter the unclassifiable list carries already-closed cards on
// every run forever — the inert re-listing the survivor-drain habit exists to
// stop — and a post-sweep plan could never read clean. Proven against three
// such cards (DRO-36/37/38) on this repo's own board.
func TestResolvedUnclassifiableCardIsNotALooseEnd(t *testing.T) {
	open := &discoverYT{
		cards:    []ytCard{{key: "DRO-36", labels: []string{labelMarker}}},
		resolved: map[string]bool{},
	}
	_, ctx := discoverRepo(t, open, emptyBoard)
	_, unclassifiable, err := discoverReap(ctx, reapLanes)
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	if !hasKey(unclassifiable, "DRO-36") {
		t.Fatal("an OPEN unattributable card must still be named — the filter below would then prove nothing")
	}

	closed := &discoverYT{
		cards:    []ytCard{{key: "DRO-36", labels: []string{labelMarker}}},
		resolved: map[string]bool{"DRO-36": true},
	}
	_, ctx2 := discoverRepo(t, closed, emptyBoard)
	_, unclassifiable2, err := discoverReap(ctx2, reapLanes)
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	if hasKey(unclassifiable2, "DRO-36") {
		t.Error("a card the tracker already holds resolved is still reported as a loose end")
	}
}

// orphanFor finds one discovered orphan by issue key. The verdict is asserted
// explicitly at every call site because `found` carries BOTH stranded and
// unattributable candidates — only reapStillOpen is dropped — so membership
// alone would pass a card that regressed from "close it" to "nothing speaks
// for it".
func orphanFor(t *testing.T, found []candidate, key string) candidate {
	t.Helper()
	for _, c := range found {
		if c.card.Key == key {
			return c
		}
	}
	var keys []string
	for _, c := range found {
		keys = append(keys, c.card.Key)
	}
	t.Fatalf("orphan %s is not in the plan; got %v", key, keys)
	return candidate{}
}

// hasOrphan reports whether a key reached the plan at all, in either verdict.
func hasOrphan(found []candidate, key string) bool {
	for _, c := range found {
		if c.card.Key == key {
			return true
		}
	}
	return false
}

// --- the dross/target: identity arm ---
//
// Discovery recovers three identity kinds and until now only `dross/phase:`
// had a fixture. A `dross/target:` card names a DESTINATION PHASE, and it is
// the shape DRO-33 was recovered by on the live board — a routed backlog item
// whose board.json link died with its phase branch.

// TestOrphanTargetFollowsItsDestinationRecord: the destination's completion
// record decides, exactly as it does on the linked path. The card's own state
// says nothing.
func TestOrphanTargetFollowsItsDestinationRecord(t *testing.T) {
	seed := func(t *testing.T, destStatus string) []candidate {
		t.Helper()
		f := &discoverYT{resolved: map[string]bool{}, cards: []ytCard{
			{key: "DRO-33", labels: []string{labelMarker, targetLabel("02-dest")}},
		}}
		dir, ctx := discoverRepo(t, f, emptyBoard)
		writeChanges(t, dir, "02-dest", destStatus)

		found, unclassifiable, err := discoverReap(ctx, reapLanes)
		if err != nil {
			t.Fatalf("discover: %v", err)
		}
		if len(unclassifiable) != 0 {
			t.Errorf("a card carrying an identity label was reported unclassifiable: %v", cardKeys(unclassifiable))
		}
		return found
	}

	t.Run("destination complete strands the card", func(t *testing.T) {
		c := orphanFor(t, seed(t, "complete"), "DRO-33")
		if c.verdict != reapStranded {
			t.Errorf("verdict = %v, want stranded", c.verdict)
		}
		if c.card.Lane != "Backlog" {
			t.Errorf("lane = %q, want Backlog — a dross/target: card is a backlog mirror, not a phase one", c.card.Lane)
		}
		// The ROUTING is what has to survive into the plan line, not merely
		// the slug: the underlying phase verdict already names the slug in its
		// own record path, so asserting on "02-dest" alone would pass even if
		// the routed-to framing were dropped and the reader lost the reason
		// this backlog card is answerable to that phase at all.
		if !strings.Contains(c.card.Why, "routed to 02-dest") {
			t.Errorf("Why = %q, want it to say the item is routed to 02-dest — a plan line that cannot be audited is the thing c-2 forbids", c.card.Why)
		}
	})

	t.Run("an unfinished destination leaves the card open", func(t *testing.T) {
		if found := seed(t, ""); hasOrphan(found, "DRO-33") {
			t.Error("a routed orphan reached the plan while its destination's record shows no completion")
		}
	})
}

// TestOrphanTargetOnAnUnscaffoldedRoadmapSlugIsStillOpen is the 6186cbf
// distinction on the discovery side: an absent phase directory cannot tell
// "renamed or lost" from "on a roadmap and not built yet". Conflating them
// reported live work as an unexplained mirror on every run, and until now that
// fix was proven only for the linked path.
func TestOrphanTargetOnAnUnscaffoldedRoadmapSlugIsStillOpen(t *testing.T) {
	f := &discoverYT{resolved: map[string]bool{}, cards: []ytCard{
		{key: "DRO-33", labels: []string{labelMarker, targetLabel("not-built-yet")}},
		{key: "DRO-34", labels: []string{labelMarker, targetLabel("vanished")}},
	}}
	dir, ctx := discoverRepo(t, f, emptyBoard)
	mustWrite(t, filepath.Join(dir, ".dross", "milestones", "v9.0.toml"),
		"phases = [\"not-built-yet\"]\n\n[milestone]\nversion = \"v9.0\"\nstatus = \"active\"\n")

	found, unclassifiable, err := discoverReap(ctx, reapLanes)
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	if hasOrphan(found, "DRO-33") || hasKey(unclassifiable, "DRO-33") {
		t.Error("a card routed to unbuilt roadmap work reached the plan — that is live backlog, correctly open")
	}
	// The other half, or the assertion above would pass on a discovery path
	// that simply dropped every unscaffolded target.
	c := orphanFor(t, found, "DRO-34")
	if c.verdict != reapUnattributable {
		t.Errorf("verdict = %v, want unattributable — a slug on no roadmap with no directory is unexplained, not open", c.verdict)
	}
}

// --- the dross/deferred: identity arm ---
//
// A `dross/deferred:` card names an ITEM ID rather than a phase, so it
// resolves through the deferred stores. DRO-95 and DRO-96 were recovered this
// way on the live board.

// TestOrphanDeferredResolvesByItemID: the item's own record decides. A
// discovery path that never read the deferred stores would report both cards
// identically.
func TestOrphanDeferredResolvesByItemID(t *testing.T) {
	seed := func(t *testing.T, entry string) ([]candidate, []reapCard) {
		t.Helper()
		f := &discoverYT{resolved: map[string]bool{}, cards: []ytCard{
			{key: "DRO-95", labels: []string{labelMarker, deferredLabel("abc123")}},
		}}
		dir, ctx := discoverRepo(t, f, emptyBoard)
		writeSpec(t, dir, "01-src", "[phase]\nid=\"01-src\"\ntitle=\"Src\"\n\n"+entry)

		found, unclassifiable, err := discoverReap(ctx, reapLanes)
		if err != nil {
			t.Fatalf("discover: %v", err)
		}
		return found, unclassifiable
	}

	t.Run("a dismissed item is a decision, so its mirror is stranded", func(t *testing.T) {
		found, _ := seed(t, "[[deferred]]\n  id = \"abc123\"\n  text = \"an idea\"\n  dismissed = true\n")
		c := orphanFor(t, found, "DRO-95")
		if c.verdict != reapStranded {
			t.Errorf("verdict = %v, want stranded", c.verdict)
		}
		if c.card.Lane != "Backlog" {
			t.Errorf("lane = %q, want Backlog", c.card.Lane)
		}
	})

	t.Run("a live item leaves its mirror open", func(t *testing.T) {
		found, _ := seed(t, "[[deferred]]\n  id = \"abc123\"\n  text = \"an idea\"\n")
		if hasOrphan(found, "DRO-95") {
			t.Error("an undismissed, unrouted deferred item's mirror reached the plan")
		}
	})

	t.Run("an id no store explains is named, not closed", func(t *testing.T) {
		found, _ := seed(t, "[[deferred]]\n  id = \"other\"\n  text = \"a different idea\"\n")
		c := orphanFor(t, found, "DRO-95")
		if c.verdict != reapUnattributable {
			t.Errorf("verdict = %v, want unattributable", c.verdict)
		}
		if !strings.Contains(c.card.Why, "abc123") {
			t.Errorf("Why = %q, want the unexplained backlog key named", c.card.Why)
		}
	})
}

// TestOrphanTaskLabelNamingNoPhaseIsUnattributable: a task label carries
// `<phase>/<task>` and the PHASE half is what holds the completion record. A
// label with no phase half has nothing to read, and must be named rather than
// dropped or resolved against an empty slug.
func TestOrphanTaskLabelNamingNoPhaseIsUnattributable(t *testing.T) {
	f := &discoverYT{resolved: map[string]bool{}, cards: []ytCard{
		{key: "DRO-37", labels: []string{labelMarker, "dross/task:t-1"}},
	}}
	_, ctx := discoverRepo(t, f, emptyBoard)

	found, _, err := discoverReap(ctx, reapLanes)
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	c := orphanFor(t, found, "DRO-37")
	if c.verdict != reapUnattributable {
		t.Errorf("verdict = %v, want unattributable", c.verdict)
	}
	if !strings.Contains(c.card.Why, "t-1") {
		t.Errorf("Why = %q, want the malformed label quoted back", c.card.Why)
	}
}
