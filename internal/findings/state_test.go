package findings

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestStoreRoundTrip fails if Save/Load drops the state or regressed field —
// the two fields that carry the durable lifecycle decision.
func TestStoreRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.toml")
	in := &Store{Records: []Record{
		{Fingerprint: "abc123", State: StateResolved, Regressed: true,
			Title: "N+1 query", File: "internal/x.go", Class: "perf", LastRun: "20260627T100000-deadbee"},
	}}
	if err := SaveStore(path, in); err != nil {
		t.Fatal(err)
	}
	out, err := LoadStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Records) != 1 {
		t.Fatalf("round-trip lost records: got %d", len(out.Records))
	}
	if g := out.Records[0]; g.State != StateResolved || !g.Regressed {
		t.Fatalf("round-trip dropped state/regressed: state=%q regressed=%v", g.State, g.Regressed)
	}
}

// TestStoreKeyedLookupAndStateValidation fails if Get returns the wrong entry
// after reload, or if Valid() accepts an empty/unknown state.
func TestStoreKeyedLookupAndStateValidation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.toml")
	in := &Store{Records: []Record{
		{Fingerprint: "aaa", State: StateTracked, Title: "first"},
		{Fingerprint: "bbb", State: StateDismissed, Title: "second"},
	}}
	if err := SaveStore(path, in); err != nil {
		t.Fatal(err)
	}
	out, err := LoadStore(path)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := out.Get("bbb")
	if !ok || got.Title != "second" || got.State != StateDismissed {
		t.Fatalf("Get(\"bbb\") = %+v, ok=%v; want the dismissed 'second' record", got, ok)
	}
	if _, ok := out.Get("missing"); ok {
		t.Error("Get returned ok=true for an absent fingerprint")
	}
	for _, bad := range []State{"", "bogus"} {
		if bad.Valid() {
			t.Errorf("Valid() accepted invalid state %q", bad)
		}
	}
	for _, good := range []State{StateTracked, StateResolved, StateDismissed} {
		if !good.Valid() {
			t.Errorf("Valid() rejected valid state %q", good)
		}
	}
}

// TestLoadStoreMissingReturnsEmpty pins the first-run contract: a missing
// state.toml is not an error, it's an empty store.
func TestLoadStoreMissingReturnsEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "does-not-exist.toml")
	s, err := LoadStore(path)
	if err != nil {
		t.Fatalf("LoadStore of a missing file errored: %v", err)
	}
	if len(s.Records) != 0 {
		t.Fatalf("missing file yielded %d records, want empty store", len(s.Records))
	}
}

// TestLoadStoreGarbledErrors pins the corrupt-state contract: garbled TOML
// returns an error, never a panic.
func TestLoadStoreGarbledErrors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.toml")
	if err := os.WriteFile(path, []byte("[[finding]]\nfingerprint = \"abc"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadStore(path); err == nil {
		t.Fatal("LoadStore accepted garbled TOML; want an error, not a panic")
	}
}

// TestSaveStoreAtomicLeavesPriorIntact pins the interrupted-write contract: when
// the atomic save can't complete (here, a read-only directory blocks the temp
// file), SaveStore returns an error and the prior state.toml is left intact and
// re-loadable — never truncated. Also asserts a successful save leaves no temp
// residue.
func TestSaveStoreAtomicLeavesPriorIntact(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("directory-permission injection is POSIX-specific")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "state.toml")
	prior := &Store{Records: []Record{{Fingerprint: "keep", State: StateResolved, Title: "must survive"}}}
	if err := SaveStore(path, prior); err != nil {
		t.Fatal(err)
	}
	// A successful save must leave only state.toml — no .tmp residue.
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 || entries[0].Name() != "state.toml" {
		t.Fatalf("successful save left residue: %v", names(entries))
	}

	// Make the directory read-only so CreateTemp (and thus the atomic save)
	// fails, then attempt to overwrite with new content.
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(dir, 0o700) // restore so t.TempDir cleanup can remove it
	next := &Store{Records: []Record{{Fingerprint: "new", State: StateTracked, Title: "should not land"}}}
	if err := SaveStore(path, next); err == nil {
		t.Fatal("SaveStore succeeded into a read-only dir; expected the atomic temp step to fail")
	}
	// Restore write access and confirm the prior content is intact.
	os.Chmod(dir, 0o700)
	out, err := LoadStore(path)
	if err != nil {
		t.Fatalf("prior state.toml unreadable after a failed save: %v", err)
	}
	if got, ok := out.Get("keep"); !ok || got.Title != "must survive" {
		t.Fatalf("failed save corrupted the prior state: got %+v ok=%v", got, ok)
	}
}

func names(es []os.DirEntry) []string {
	out := make([]string, len(es))
	for i, e := range es {
		out[i] = e.Name()
	}
	return out
}
