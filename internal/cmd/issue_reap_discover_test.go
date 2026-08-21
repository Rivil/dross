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
	return `{"idReadable":"` + c.key + `","resolved":null,"tags":[` + strings.Join(tags, ",") + `]}`
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
	f := &discoverYT{cards: []ytCard{
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
	f := &discoverYT{cards: []ytCard{{key: "DRO-33", labels: []string{labelMarker}}}}
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
	f := &discoverYT{cards: []ytCard{
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
	f := &discoverYT{cards: []ytCard{
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
	f := &discoverYT{cards: []ytCard{
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
	f := &discoverYT{cards: []ytCard{
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
