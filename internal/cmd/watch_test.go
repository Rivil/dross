package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/watch"
)

// driftPhase scaffolds a phase whose plan is fully done but unverified, so
// ClassifyDrift buckets it complete_unverified (→ suggested /dross-verify).
func driftPhase(t *testing.T, dir, id string) {
	t.Helper()
	writeSpec(t, dir, id, "[phase]\nid=\""+id+"\"\ntitle=\"X\"\n")
	writePlan(t, dir, id, "[phase]\nid = \""+id+"\"\n[[task]]\nid = \"t1\"\nwave = 1\nstatus = \"done\"\n")
}

func runWatchJSON(t *testing.T) watchDigest {
	t.Helper()
	out := captureStdout(t, func() {
		if err := runCmd(t, Watch(), "--json"); err != nil {
			t.Fatalf("watch --json: %v", err)
		}
	})
	var d watchDigest
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &d); err != nil {
		t.Fatalf("watch --json not valid digest JSON: %v\n%s", err, out)
	}
	return d
}

func TestWatchJSONShapeAndExit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[{"number":21,"title":"a real bug","state":"open"}]`))
	}))
	t.Cleanup(srv.Close)
	dir := boardRepo(t, srv.URL, true)
	driftPhase(t, dir, "01-x")

	d := runWatchJSON(t)
	// Shape: the four contract fields must be present and coherent.
	if d.Suggested == "" {
		t.Error("digest missing suggested_command")
	}
	if len(d.Drift) == 0 {
		t.Error("expected the done-but-unverified phase to appear as drift")
	}
}

func TestWatchReadOnlyBoundary(t *testing.T) {
	var methods []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		methods = append(methods, r.Method)
		_, _ = w.Write([]byte(`[{"number":21,"title":"bug","state":"open"}]`))
	}))
	t.Cleanup(srv.Close)
	dir := boardRepo(t, srv.URL, true)

	// Seed a known board.json and snapshot its bytes.
	boardJSON := filepath.Join(dir, ".dross", "board.json")
	mustWrite(t, boardJSON, `{"phases":{},"quicks":{},"milestones":{}}`)
	before := mustRead(t, boardJSON)

	_ = runWatchJSON(t)

	// board.json must be byte-identical — watch never writes it.
	if after := mustRead(t, boardJSON); after != before {
		t.Errorf("board.json mutated by watch:\nbefore=%q\nafter=%q", before, after)
	}
	// Only GET may have been issued to the board.
	for _, m := range methods {
		if m != http.MethodGet {
			t.Errorf("watch issued a non-GET board request (%s) — must be read-only", m)
		}
	}
	// The one permitted write: watch.state.json.
	if _, err := os.Stat(filepath.Join(dir, ".dross", "watch.state.json")); err != nil {
		t.Errorf("watch.state.json should have been written: %v", err)
	}
}

func TestWatchSecondRunZeroNew(t *testing.T) {
	call := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		call++
		if call == 1 {
			_, _ = w.Write([]byte(`[{"number":21,"title":"bug","state":"open"}]`))
			return
		}
		// From the second run on, #21 is unchanged and #22 is brand new.
		_, _ = w.Write([]byte(`[{"number":21,"title":"bug","state":"open"},{"number":22,"title":"new bug","state":"open"}]`))
	}))
	t.Cleanup(srv.Close)
	_ = boardRepo(t, srv.URL, true)

	// Run 1 seeds the baseline: nothing is new on the first tick.
	first := runWatchJSON(t)
	if len(first.New) != 0 {
		t.Fatalf("first run must seed (no new), got %v", first.New)
	}

	// Run 2: #21 carried (zero-new), only #22 flagged new.
	second := runWatchJSON(t)
	if len(second.New) != 1 || second.New[0].ID != "22" {
		t.Fatalf("second run should flag only #22 new (21 carried), got %v", second.New)
	}
}

func TestWatchBoardDisabled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("board must not be contacted when disabled: %s %s", r.Method, r.URL.Path)
	}))
	t.Cleanup(srv.Close)
	dir := boardRepo(t, srv.URL, false) // board sync off
	driftPhase(t, dir, "01-x")

	d := runWatchJSON(t)
	if d.BoardOK {
		t.Error("board_ok must be false when board sync is disabled")
	}
	if len(d.New) != 0 || len(d.Current) != 0 {
		t.Errorf("no board issues expected when disabled, got new=%v current=%v", d.New, d.Current)
	}
	if len(d.Drift) == 0 {
		t.Error("drift must still be reported with the board off")
	}
	if d.Suggested != "/dross-verify" {
		t.Errorf("suggested = %q, want /dross-verify (drift-driven)", d.Suggested)
	}
}

func TestWatchBoardUnreachable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	dir := boardRepo(t, srv.URL, true)
	driftPhase(t, dir, "01-x")
	srv.Close() // make the board unreachable — requests now fail

	// Pre-seed a baseline so we can prove it survives an unreachable tick.
	wPath := filepath.Join(dir, ".dross", "watch.state.json")
	mustWrite(t, wPath, `{"issues":{"99":"open"}}`)
	before := mustRead(t, wPath)

	d := runWatchJSON(t)
	if d.BoardOK {
		t.Error("board_ok must be false when the board is unreachable")
	}
	if len(d.Drift) == 0 {
		t.Error("drift must still be reported when the board is unreachable")
	}
	// The baseline must be preserved — watch.state.json is only written when the
	// board was reached, else the next healthy tick re-flags the whole backlog.
	if after := mustRead(t, wPath); after != before {
		t.Errorf("unreachable tick overwrote the baseline:\nbefore=%q\nafter=%q", before, after)
	}
}

func TestSuggestPrecedence(t *testing.T) {
	complete := []watch.PhaseDrift{{Phase: "a", Kind: watch.DriftCompleteUnverified}}
	verified := []watch.PhaseDrift{{Phase: "b", Kind: watch.DriftVerifiedUnshipped}}
	inProg := []watch.PhaseDrift{{Phase: "c", Kind: watch.DriftInProgress}}

	cases := []struct {
		name     string
		drift    []watch.PhaseDrift
		newCount int
		want     string
	}{
		{"complete beats new-issues", complete, 3, "/dross-verify"},
		{"verified beats new-issues", verified, 3, "/dross-ship"},
		{"complete beats verified", append(append([]watch.PhaseDrift{}, complete...), verified...), 0, "/dross-verify"},
		{"new issues when no advance", nil, 2, "/dross-inbox"},
		{"in-progress does not trigger", inProg, 0, "/dross-status"},
		{"idle no-new", nil, 0, "/dross-status"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := suggestedCommand(tc.drift, tc.newCount)
			if got == "" {
				t.Fatal("suggestedCommand returned empty — must always return exactly one")
			}
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}
