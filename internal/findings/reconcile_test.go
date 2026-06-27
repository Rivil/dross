package findings

import (
	"reflect"
	"testing"
)

// TestReconcileNewIsTracked: a never-seen fingerprint is inserted as tracked
// (not regressed) and reported as new.
func TestReconcileNewIsTracked(t *testing.T) {
	store := &Store{}
	it := Item{Class: "perf", File: "a.go", Title: "N+1 loader"}
	res := Reconcile(store, []Item{it}, "run1")
	if len(res.New) != 1 {
		t.Fatalf("New = %d, want 1", len(res.New))
	}
	g, ok := store.Get(it.Fingerprint())
	if !ok || g.State != StateTracked || g.Regressed {
		t.Fatalf("new finding stored as %+v, ok=%v; want state=tracked regressed=false", g, ok)
	}
}

// TestReconcileFoldsDismissed: a fresh item matching a dismissed entry is folded
// (not surfaced as new) and stays dismissed.
func TestReconcileFoldsDismissed(t *testing.T) {
	it := Item{Class: "sec", File: "a.go", Title: "hardcoded key"}
	fp := it.Fingerprint()
	store := &Store{Records: []Record{{Fingerprint: fp, State: StateDismissed, Title: it.Title, File: it.File, Class: it.Class}}}
	res := Reconcile(store, []Item{it}, "run2")
	if len(res.New) != 0 {
		t.Fatalf("dismissed finding emitted as new (New=%d)", len(res.New))
	}
	if len(res.Folded) != 1 {
		t.Fatalf("Folded = %d, want 1", len(res.Folded))
	}
	if g, _ := store.Get(fp); g.State != StateDismissed {
		t.Fatalf("dismissed finding flipped to %q on re-run", g.State)
	}
}

// TestReconcileFoldsResolved: a fresh item matching a resolved entry is not
// surfaced as new, and the stored state stays resolved (not flipped to tracked).
func TestReconcileFoldsResolved(t *testing.T) {
	it := Item{Class: "sec", File: "a.go", Title: "path traversal"}
	fp := it.Fingerprint()
	store := &Store{Records: []Record{{Fingerprint: fp, State: StateResolved, Title: it.Title, File: it.File, Class: it.Class}}}
	res := Reconcile(store, []Item{it}, "run2")
	if len(res.New) != 0 {
		t.Fatalf("resolved finding emitted as new (New=%d)", len(res.New))
	}
	if g, _ := store.Get(fp); g.State != StateResolved {
		t.Fatalf("resolved finding flipped to %q on re-run; want it carried as resolved", g.State)
	}
}

// TestReconcileResolvedReappearsStaysResolvedRegressed: c-4 — a resolved finding
// that reappears stays resolved AND is flagged regressed.
func TestReconcileResolvedReappearsStaysResolvedRegressed(t *testing.T) {
	it := Item{Class: "sec", File: "a.go", Title: "ssrf in fetch"}
	fp := it.Fingerprint()
	store := &Store{Records: []Record{{Fingerprint: fp, State: StateResolved, Regressed: false, Title: it.Title, File: it.File, Class: it.Class}}}
	res := Reconcile(store, []Item{it}, "run2")
	if len(res.Regressed) != 1 {
		t.Fatalf("Regressed = %d, want 1", len(res.Regressed))
	}
	g, _ := store.Get(fp)
	if g.State != StateResolved || !g.Regressed {
		t.Fatalf("reappeared resolved finding = state %q regressed %v; want resolved + regressed=true", g.State, g.Regressed)
	}
}

// TestReconcileDoesNotMutateScan: the locked no-prejudice rule made falsifiable —
// Reconcile must not mutate the input scan items.
func TestReconcileDoesNotMutateScan(t *testing.T) {
	items := []Item{
		{Class: "sec", File: "a.go", Title: "one"},
		{Class: "perf", File: "b.go", Title: "two"},
	}
	before := append([]Item(nil), items...)
	store := &Store{Records: []Record{{Fingerprint: items[0].Fingerprint(), State: StateResolved, Title: "one", File: "a.go", Class: "sec"}}}
	Reconcile(store, items, "run1")
	if !reflect.DeepEqual(items, before) {
		t.Fatalf("Reconcile mutated the scan items: got %+v, want %+v", items, before)
	}
}

// TestReconcileIdenticalFingerprintDedup: two fresh items that fingerprint
// identically (here via path-spelling drift) reconcile to one durable record.
func TestReconcileIdenticalFingerprintDedup(t *testing.T) {
	store := &Store{}
	a := Item{Class: "sec", File: "./internal/x.go", Title: "dup"}
	b := Item{Class: "sec", File: "internal/x.go", Title: "dup"}
	if a.Fingerprint() != b.Fingerprint() {
		t.Fatal("test precondition: the two items should share a fingerprint")
	}
	res := Reconcile(store, []Item{a, b}, "run1")
	if len(store.Records) != 1 {
		t.Fatalf("identical fingerprints created %d records, want 1", len(store.Records))
	}
	if len(res.New) != 1 {
		t.Fatalf("dedup reported %d new, want 1", len(res.New))
	}
}

// TestReconcileDeletedFileRetention: a prior tracked record whose finding is
// absent this run is retained as-is — not dropped, not marked regressed.
func TestReconcileDeletedFileRetention(t *testing.T) {
	ghost := Record{Fingerprint: "ghost", State: StateTracked, Title: "old", File: "gone.go", Class: "sec"}
	store := &Store{Records: []Record{ghost}}
	Reconcile(store, []Item{{Class: "sec", File: "other.go", Title: "fresh"}}, "run1")
	g, ok := store.Get("ghost")
	if !ok {
		t.Fatal("a finding absent this run was dropped from the store")
	}
	if g.State != StateTracked || g.Regressed {
		t.Fatalf("retained record was altered: state %q regressed %v; want tracked, not regressed", g.State, g.Regressed)
	}
}
