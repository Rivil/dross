package survivor

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// staleFixture writes a small tree and a store of acceptances against it.
func staleFixture(t *testing.T) (root string, s *Store) {
	t.Helper()
	root = t.TempDir()
	writeFile(t, filepath.Join(root, "present.go"), "package p\n\n\tcase 1:\n\treturn nil\n")
	writeFile(t, filepath.Join(root, "moved.go"), "package p\n\nx := 1\ny := 2\n\tif limit > 0 {\n")

	s = &Store{}
	for _, a := range []Acceptance{
		{Key: "k-present", File: "present.go", Op: "OP", Text: "case 1:", Line: 3, Reason: "ceiling"},
		{Key: "k-moved", File: "moved.go", Op: "OP", Text: "if limit > 0 {", Line: 2, Reason: "drifted"},
		{Key: "k-gonefile", File: "deleted.go", Op: "OP", Text: "return err", Line: 7, Reason: "removed"},
		{Key: "k-gonetext", File: "present.go", Op: "OP", Text: "return errors.New(\"x\")", Line: 9, Reason: "rewritten"},
	} {
		if err := s.Add(a); err != nil {
			t.Fatalf("Add(%s): %v", a.Key, err)
		}
	}
	return root, s
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func staleByKey(rep Report) map[string]string {
	out := map[string]string{}
	for _, s := range rep.Stale {
		out[s.Key] = s.Reason
	}
	return out
}

// TestStaleDetectsDeletedFile is c-5's first half: an acceptance pointing at a
// file that is gone must surface, not sit in the store silently suppressing
// nothing forever.
func TestStaleDetectsDeletedFile(t *testing.T) {
	root, s := staleFixture(t)
	got := staleByKey(StaleAcceptances(root, s))
	if got["k-gonefile"] != ReasonFileGone {
		t.Errorf("deleted-file acceptance reported as %q, want %q", got["k-gonefile"], ReasonFileGone)
	}
}

// TestStaleDetectsVanishedText is the second half: the file survives but the
// exact source text the acceptance was recorded against is gone, so the mutant
// it silenced can no longer be produced.
func TestStaleDetectsVanishedText(t *testing.T) {
	root, s := staleFixture(t)
	got := staleByKey(StaleAcceptances(root, s))
	if got["k-gonetext"] != ReasonTextGone {
		t.Errorf("vanished-text acceptance reported as %q, want %q", got["k-gonetext"], ReasonTextGone)
	}
}

// TestStaleIgnoresLineDrift guards against staleness degenerating into a
// line-number check. The acceptance records line 2 and the text actually sits on
// line 5; a positional check would retire a perfectly live acceptance on every
// unrelated edit above it.
func TestStaleIgnoresLineDrift(t *testing.T) {
	root, s := staleFixture(t)
	got := staleByKey(StaleAcceptances(root, s))
	if reason, ok := got["k-moved"]; ok {
		t.Errorf("acceptance whose text merely moved lines reported stale (%q)", reason)
	}
}

// TestStaleLeavesIntactAcceptancesAlone covers the scoping failure mode: an
// acceptance in a file the run never examined, whose subject is intact, must not
// be reported stale. Staleness is a property of the working tree, not of which
// files a mutation run happened to cover — narrowing it to the run's file set
// would retire every acceptance outside the current phase's diff.
func TestStaleLeavesIntactAcceptancesAlone(t *testing.T) {
	root, s := staleFixture(t)
	writeFile(t, filepath.Join(root, "untouched", "far.go"), "package q\n\n\tswitch v {\n")
	if err := s.Add(Acceptance{Key: "k-far", File: filepath.Join("untouched", "far.go"), Op: "OP", Text: "switch v {", Reason: "elsewhere"}); err != nil {
		t.Fatal(err)
	}

	got := staleByKey(StaleAcceptances(root, s))
	for _, key := range []string{"k-present", "k-far"} {
		if reason, ok := got[key]; ok {
			t.Errorf("intact acceptance %s reported stale (%q)", key, reason)
		}
	}
}

// TestStaleReportsReadErrorsSeparately: "I could not look" is not "it is gone".
// Collapsing the two would let a permissions blip retire live acceptances.
func TestStaleReportsReadErrorsSeparately(t *testing.T) {
	root := t.TempDir()
	// A directory where a file is expected reads as an error that is not
	// ErrNotExist.
	if err := os.MkdirAll(filepath.Join(root, "weird.go"), 0o755); err != nil {
		t.Fatal(err)
	}
	s := &Store{}
	if err := s.Add(Acceptance{Key: "k-unreadable", File: "weird.go", Op: "OP", Text: "return nil", Reason: "why"}); err != nil {
		t.Fatal(err)
	}

	rep := StaleAcceptances(root, s)
	if len(rep.Stale) != 0 {
		t.Errorf("unreadable file classified stale: %+v", rep.Stale)
	}
	if len(rep.Unverifiable) != 1 {
		t.Fatalf("want 1 unverifiable entry, got %+v", rep.Unverifiable)
	}
	if rep.Unverifiable[0].Err == nil || !strings.Contains(rep.Unverifiable[0].Err.Error(), "weird.go") {
		t.Errorf("unverifiable entry must carry the read error naming the file, got %v", rep.Unverifiable[0].Err)
	}
}

// TestStaleSignatureHasNoClock pins the no_acceptance_ttl lock structurally. A
// clock or duration parameter appearing here is expiry-by-age sneaking back in,
// which would re-flood the backlog on a timer with no new information.
func TestStaleSignatureHasNoClock(t *testing.T) {
	ft := reflect.TypeOf(StaleAcceptances)
	if ft.NumIn() != 2 {
		t.Fatalf("StaleAcceptances takes %d parameters, want 2 (root, store) — a third is likely a clock or TTL", ft.NumIn())
	}
	banned := []reflect.Type{
		reflect.TypeOf(time.Time{}),
		reflect.TypeOf(time.Duration(0)),
	}
	for i := 0; i < ft.NumIn(); i++ {
		for _, b := range banned {
			if ft.In(i) == b {
				t.Errorf("parameter %d is %s — staleness must be structural, never time-based", i, b)
			}
		}
	}
}

// TestStaleDoesNotMutateTheStore: a staleness pass reports, it does not retire.
// Deciding what to do about a stale acceptance is a human's call, and a pass
// that quietly rewrote the file would make that decision invisibly.
func TestStaleDoesNotMutateTheStore(t *testing.T) {
	root, s := staleFixture(t)
	path := Path(filepath.Join(root, RootDirName))
	if err := Save(path, s); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	countBefore := len(s.Accepted)

	StaleAcceptances(root, s)

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Errorf("staleness pass rewrote the store:\nbefore:\n%s\nafter:\n%s", before, after)
	}
	if len(s.Accepted) != countBefore {
		t.Errorf("staleness pass changed the in-memory store from %d to %d entries", countBefore, len(s.Accepted))
	}
}

// TestStaleTextlessAcceptanceIsUnverifiable: a hand-written entry with no
// recorded text cannot be located, and claiming its subject is gone would be an
// unwarranted retirement.
func TestStaleTextlessAcceptanceIsUnverifiable(t *testing.T) {
	root := t.TempDir()
	s := &Store{Accepted: []Acceptance{{Key: "k-notext", File: "present.go", Op: "OP", Reason: "hand-written"}}}

	rep := StaleAcceptances(root, s)
	if len(rep.Stale) != 0 {
		t.Errorf("textless acceptance claimed stale: %+v", rep.Stale)
	}
	if len(rep.Unverifiable) != 1 {
		t.Fatalf("want 1 unverifiable entry, got %+v", rep.Unverifiable)
	}
}

// TestStaleNilStore: a run with no store at all must not panic — the first-run
// path reaches here with an empty store.
func TestStaleNilStore(t *testing.T) {
	rep := StaleAcceptances(t.TempDir(), nil)
	if len(rep.Stale) != 0 || len(rep.Unverifiable) != 0 {
		t.Errorf("nil store produced %+v, want an empty report", rep)
	}
}
