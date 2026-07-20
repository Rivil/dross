package watch

import (
	"os"
	"testing"
)

// seen builds a present baseline (as if a prior run persisted this seen-set).
func seen(m map[string]string) *State { return &State{Issues: m, present: true} }

func ids(items []Item) map[string]bool {
	out := map[string]bool{}
	for _, it := range items {
		out[it.ID] = true
	}
	return out
}

func TestWatchDiff(t *testing.T) {
	cases := []struct {
		name      string
		prior     *State
		feed      []Item
		wantNew   []string
		wantCurrent []string
	}{
		{
			name:        "unseen id is new",
			prior:       seen(map[string]string{}),
			feed:        []Item{{ID: "a", State: "open"}},
			wantNew:     []string{"a"},
			wantCurrent: nil,
		},
		{
			name:        "reopen (open->closed) is new",
			prior:       seen(map[string]string{"a": "open"}),
			feed:        []Item{{ID: "a", State: "closed"}},
			wantNew:     []string{"a"},
			wantCurrent: nil,
		},
		{
			name:        "cosmetic retitle same state is not new",
			prior:       seen(map[string]string{"a": "open"}),
			feed:        []Item{{ID: "a", State: "open", Title: "renamed"}},
			wantNew:     nil,
			wantCurrent: []string{"a"},
		},
		{
			name:        "byte-identical feed yields no new",
			prior:       seen(map[string]string{"a": "open", "b": "closed"}),
			feed:        []Item{{ID: "a", State: "open"}, {ID: "b", State: "closed"}},
			wantNew:     nil,
			wantCurrent: []string{"a", "b"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := tc.prior.Diff(tc.feed)
			gotNew, gotCur := ids(d.New), ids(d.Current)
			for _, id := range tc.wantNew {
				if !gotNew[id] {
					t.Errorf("expected %q in New, got new=%v", id, d.New)
				}
			}
			for _, id := range tc.wantCurrent {
				if !gotCur[id] {
					t.Errorf("expected %q in Current, got current=%v", id, d.Current)
				}
			}
			if len(d.New) != len(tc.wantNew) {
				t.Errorf("New count = %d, want %d (%v)", len(d.New), len(tc.wantNew), d.New)
			}
		})
	}
}

func TestWatchFirstRun(t *testing.T) {
	// A not-present baseline (nil/absent prior) seeds: nothing is new, every
	// feed item is current.
	s := &State{Issues: map[string]string{}} // present == false
	feed := []Item{{ID: "a", State: "open"}, {ID: "b", State: "closed"}}
	d := s.Diff(feed)
	if len(d.New) != 0 {
		t.Fatalf("first run must flag nothing new, got %v", d.New)
	}
	if len(d.Current) != 2 {
		t.Fatalf("first run must label all feed items current, got %v", d.Current)
	}
}

func TestWatchStateRoundTrip(t *testing.T) {
	dir := t.TempDir()
	p := FilePath(dir)

	s := &State{Issues: map[string]string{}}
	s.Update([]Item{{ID: "1", State: "open"}, {ID: "2", State: "closed"}})
	if err := s.Save(p); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Issues["1"] != "open" || got.Issues["2"] != "closed" {
		t.Fatalf("round-trip lost seen-set: %v", got.Issues)
	}
	// Reloaded baseline is present, so an unchanged feed yields no new.
	if d := got.Diff([]Item{{ID: "1", State: "open"}, {ID: "2", State: "closed"}}); len(d.New) != 0 {
		t.Fatalf("reloaded baseline should be present (no new on unchanged feed), got %v", d.New)
	}

	// Load of a missing file is an empty baseline, not an error.
	missing, err := Load(FilePath(t.TempDir()))
	if err != nil {
		t.Fatalf("Load of missing file errored: %v", err)
	}
	if len(missing.Issues) != 0 {
		t.Fatalf("missing file should give empty baseline, got %v", missing.Issues)
	}
}

func TestSaveAtomicRename(t *testing.T) {
	dir := t.TempDir()
	p := FilePath(dir)

	// Establish a good baseline (state A).
	a := &State{Issues: map[string]string{}}
	a.Update([]Item{{ID: "1", State: "open"}})
	if err := a.Save(p); err != nil {
		t.Fatalf("initial Save: %v", err)
	}

	// Occupy the temp path with a directory so the temp write fails — this
	// stands in for an interrupted write. An in-place writer would clobber the
	// destination here; an atomic temp+rename writer leaves it intact.
	if err := os.Mkdir(p+".tmp", 0o755); err != nil {
		t.Fatalf("prep tmp dir: %v", err)
	}

	b := &State{Issues: map[string]string{}}
	b.Update([]Item{{ID: "2", State: "closed"}})
	if err := b.Save(p); err == nil {
		t.Fatal("expected Save to fail when the temp path is blocked")
	}

	// The prior file must survive byte-for-byte: A intact, no leaked B.
	got, err := Load(p)
	if err != nil {
		t.Fatalf("Load after failed Save: %v", err)
	}
	if got.Issues["1"] != "open" {
		t.Fatalf("original baseline was clobbered by a failed Save: %v", got.Issues)
	}
	if _, leaked := got.Issues["2"]; leaked {
		t.Fatalf("failed Save partially wrote B into the destination: %v", got.Issues)
	}
}

func TestLoadCorruptDegrades(t *testing.T) {
	dir := t.TempDir()
	p := FilePath(dir)
	if err := os.WriteFile(p, []byte("{ this is not valid json"), 0o644); err != nil {
		t.Fatalf("write corrupt file: %v", err)
	}

	s, err := Load(p)
	if err != nil {
		t.Fatalf("corrupt state file should degrade, not error: %v", err)
	}
	if len(s.Issues) != 0 {
		t.Fatalf("corrupt file should give empty baseline, got %v", s.Issues)
	}
	// Degraded baseline behaves as first-run: nothing is flagged new.
	if d := s.Diff([]Item{{ID: "x", State: "open"}}); len(d.New) != 0 {
		t.Fatalf("corrupt baseline must not flag the backlog as new, got %v", d.New)
	}
}
