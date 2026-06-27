package findings

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
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

// TestStoreRoundTripLastRun fails if Save/Load drops the new store-level last_run
// or disturbs the finding records alongside it.
func TestStoreRoundTripLastRun(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.toml")
	now := time.Date(2026, 6, 27, 10, 0, 0, 0, time.UTC)
	in := &Store{LastRun: now, Records: []Record{
		{Fingerprint: "abc", State: StateTracked, Title: "keep"},
	}}
	if err := SaveStore(path, in); err != nil {
		t.Fatal(err)
	}
	out, err := LoadStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if !out.LastRun.Equal(now) {
		t.Fatalf("round-trip dropped last_run: got %v want %v", out.LastRun, now)
	}
	if out.NeverRun() {
		t.Fatal("NeverRun() true after a stamped round-trip")
	}
	if len(out.Records) != 1 || out.Records[0].Fingerprint != "abc" {
		t.Fatalf("round-trip disturbed records: %+v", out.Records)
	}
}

// TestLoadStoreLegacyWithoutLastRun pins backward compatibility: a pre-existing
// state.toml from before last_run existed loads with records intact and reads as
// never-run.
func TestLoadStoreLegacyWithoutLastRun(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.toml")
	legacy := "[[finding]]\nfingerprint = \"abc\"\nstate = \"tracked\"\ntitle = \"old\"\n"
	if err := os.WriteFile(path, []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := LoadStore(path)
	if err != nil {
		t.Fatalf("legacy load errored: %v", err)
	}
	if !out.NeverRun() {
		t.Fatalf("legacy store without last_run should read NeverRun, got %v", out.LastRun)
	}
	if len(out.Records) != 1 || out.Records[0].Title != "old" {
		t.Fatalf("legacy load disturbed records: %+v", out.Records)
	}
}

// TestLoadStoreMissingNeverRun pins the never-run contract for a missing file:
// LoadStore yields a store whose LastRun.IsZero() (NeverRun) is true.
func TestLoadStoreMissingNeverRun(t *testing.T) {
	s, err := LoadStore(filepath.Join(t.TempDir(), "absent.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if !s.NeverRun() {
		t.Fatal("missing state.toml should read NeverRun (IsZero), but didn't")
	}
}

// TestStampLastRunPersists fails if StampLastRun doesn't durably record the run:
// after stamping an empty store, the reloaded last_run is non-zero.
func TestStampLastRunPersists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.toml")
	now := time.Date(2026, 6, 27, 12, 0, 0, 0, time.UTC)
	if err := StampLastRun(path, now); err != nil {
		t.Fatal(err)
	}
	out, err := LoadStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if out.NeverRun() {
		t.Fatal("StampLastRun on an empty store left it NeverRun")
	}
	if !out.LastRun.Equal(now) {
		t.Fatalf("StampLastRun persisted %v, want %v", out.LastRun, now)
	}
}

// TestStampLastRunPreservesRecords fails if stamping clobbers existing findings —
// it must merge the timestamp into the existing ledger, not overwrite it.
func TestStampLastRunPreservesRecords(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.toml")
	in := &Store{Records: []Record{{Fingerprint: "keep", State: StateResolved, Title: "survive"}}}
	if err := SaveStore(path, in); err != nil {
		t.Fatal(err)
	}
	if err := StampLastRun(path, time.Date(2026, 6, 27, 9, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	out, err := LoadStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := out.Get("keep"); !ok || got.Title != "survive" {
		t.Fatalf("StampLastRun disturbed records: %+v ok=%v", got, ok)
	}
	if out.NeverRun() {
		t.Fatal("StampLastRun didn't set last_run")
	}
}

// TestLoadStoreGarbledLastRunErrors pins the corrupt-signal contract: a malformed
// last_run datetime returns an error rather than a panic or a silent zero.
func TestLoadStoreGarbledLastRunErrors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.toml")
	if err := os.WriteFile(path, []byte("last_run = \"not-a-timestamp\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadStore(path); err == nil {
		t.Fatal("LoadStore accepted a garbled last_run; want an error, not a silent zero")
	}
}

func names(es []os.DirEntry) []string {
	out := make([]string, len(es))
	for i, e := range es {
		out[i] = e.Name()
	}
	return out
}
