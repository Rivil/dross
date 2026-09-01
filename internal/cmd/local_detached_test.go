package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// sampleDetachedRun is the fully-populated record every round-trip assertion
// compares against. Every field is set to a DISTINCT non-zero value, so a
// helper that dropped one — or that wrote two fields from the same source —
// fails rather than passing on a coincidence of empty strings.
func sampleDetachedRun() detachedRun {
	return detachedRun{
		Phase:        "remote-run-detach",
		RunID:        "r-20260830-2201",
		Host:         "helicon",
		Workdir:      "/var/lib/buildcache/src/dross",
		RunDir:       "/var/lib/buildcache/src/dross/.dross-runs/r-20260830-2201",
		DispatchedAt: time.Date(2026, 8, 30, 22, 1, 5, 0, time.UTC),
		ScheduledFor: time.Date(2026, 8, 31, 2, 0, 0, 0, time.UTC),
		State:        "scheduled",
	}
}

// TestDetachedRunRoundTripsEveryField is the record's basic contract: what the
// dispatch writes, a later process reads back.
//
// It matters more here than for an ordinary config key because the reader is a
// DIFFERENT session — that is the whole point of the phase. A field silently
// dropped on the way in is not discovered until a fetch hours later cannot find
// the run, by which time the leg has already been paid for.
//
// Compared field-for-field rather than with a single struct equality, so a
// failure names which field was lost.
func TestDetachedRunRoundTripsEveryField(t *testing.T) {
	root := chdirDross(t)
	repoDir := filepath.Dir(root)
	want := sampleDetachedRun()

	if err := recordDetachedRun(root, repoDir, want); err != nil {
		t.Fatalf("record: %v", err)
	}

	got, err := findDetachedRun(root, repoDir, want.Phase)
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if got == nil {
		t.Fatal("the recorded run was not found")
	}
	for _, f := range []struct {
		name      string
		got, want any
	}{
		{"Phase", got.Phase, want.Phase},
		{"RunID", got.RunID, want.RunID},
		{"Host", got.Host, want.Host},
		{"Workdir", got.Workdir, want.Workdir},
		{"RunDir", got.RunDir, want.RunDir},
		{"State", got.State, want.State},
	} {
		if f.got != f.want {
			t.Errorf("%s did not round-trip: got %v want %v", f.name, f.got, f.want)
		}
	}
	// Times compared with Equal rather than ==: a TOML decode may return a
	// different *location carrying the same instant, and == on time.Time
	// compares the monotonic/location fields too.
	if !got.DispatchedAt.Equal(want.DispatchedAt) {
		t.Errorf("DispatchedAt did not round-trip: got %v want %v", got.DispatchedAt, want.DispatchedAt)
	}
	if !got.ScheduledFor.Equal(want.ScheduledFor) {
		t.Errorf("ScheduledFor did not round-trip: got %v want %v", got.ScheduledFor, want.ScheduledFor)
	}
}

// TestImmediateRunRoundTripsAsUnscheduled pins the zero ScheduledFor, which is
// what distinguishes an immediate dispatch from an off-hours one.
//
// Asserted through Scheduled() rather than against the zero time directly: the
// callers ask the question that way, and a marshalling that turned the zero
// time into some other sentinel would still have to answer it correctly.
func TestImmediateRunRoundTripsAsUnscheduled(t *testing.T) {
	root := chdirDross(t)
	repoDir := filepath.Dir(root)
	rec := sampleDetachedRun()
	rec.ScheduledFor = time.Time{}
	rec.State = "running"

	if err := recordDetachedRun(root, repoDir, rec); err != nil {
		t.Fatalf("record: %v", err)
	}
	got, err := findDetachedRun(root, repoDir, rec.Phase)
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if got == nil {
		t.Fatal("the recorded run was not found")
	}
	if got.Scheduled() {
		t.Errorf("an immediate run reports as scheduled (ScheduledFor=%v)", got.ScheduledFor)
	}

	// And the scheduled one must still report scheduled, or the check above
	// would pass for a Scheduled() that always returned false.
	if !sampleDetachedRun().Scheduled() {
		t.Error("a run carrying a start time does not report as scheduled")
	}
}

// TestSecondRunForOnePhaseIsRefused is the one_run_per_phase decision made
// mechanical.
//
// Two live runs both write the phase's tests.json when collected, and the loser
// wins silently: whichever fetch lands second overwrites the first with numbers
// from a different dispatch, at a different time, possibly on a different host.
// The refusal must NAME the run already in flight — a bare "already exists"
// leaves the user with no way to find what to cancel.
func TestSecondRunForOnePhaseIsRefused(t *testing.T) {
	root := chdirDross(t)
	repoDir := filepath.Dir(root)
	first := sampleDetachedRun()
	if err := recordDetachedRun(root, repoDir, first); err != nil {
		t.Fatalf("record: %v", err)
	}

	second := first
	second.RunID = "r-20260830-2359"
	second.Host = "anachryon"
	err := recordDetachedRun(root, repoDir, second)
	if err == nil {
		t.Fatal("a second detached run for one phase was accepted")
	}
	for _, want := range []string{first.RunID, first.Host, first.Phase} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not name %q: %v", want, err)
		}
	}

	// The refusal must not have half-written: the stored run is still the
	// first one, not the second and not both.
	runs, err := readDetachedRuns(root, repoDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 || runs[0].RunID != first.RunID {
		t.Errorf("the refused record mutated the store: %+v", runs)
	}
}

// TestTwoPhasesEachKeepTheirOwnRun is the other half of the guard above: the
// refusal is per PHASE, not a global one-run-at-a-time lock. A rule that
// refused any second run would make the pool useless and would block a second
// phase for the length of the first one's leg.
func TestTwoPhasesEachKeepTheirOwnRun(t *testing.T) {
	root := chdirDross(t)
	repoDir := filepath.Dir(root)
	a := sampleDetachedRun()
	b := sampleDetachedRun()
	b.Phase = "lane-host-affinity"
	b.RunID = "r-20260831-0100"

	if err := recordDetachedRun(root, repoDir, a); err != nil {
		t.Fatalf("record a: %v", err)
	}
	if err := recordDetachedRun(root, repoDir, b); err != nil {
		t.Fatalf("a second PHASE was refused a run: %v", err)
	}
	for _, want := range []detachedRun{a, b} {
		got, err := findDetachedRun(root, repoDir, want.Phase)
		if err != nil {
			t.Fatal(err)
		}
		if got == nil || got.RunID != want.RunID {
			t.Errorf("phase %q did not keep its own run: %+v", want.Phase, got)
		}
	}
}

// TestClearDetachedRunReportsWhetherItRemovedOne is what lets a cancel of an
// unknown phase be an error rather than a silent success.
//
// A caller that cannot tell "removed" from "there was nothing" reports both as
// done — so a user who mistyped a phase id is told the run is cancelled while
// it keeps running on the host and keeps blocking a re-dispatch.
func TestClearDetachedRunReportsWhetherItRemovedOne(t *testing.T) {
	root := chdirDross(t)
	repoDir := filepath.Dir(root)
	rec := sampleDetachedRun()
	if err := recordDetachedRun(root, repoDir, rec); err != nil {
		t.Fatalf("record: %v", err)
	}

	removed, err := clearDetachedRun(root, repoDir, rec.Phase)
	if err != nil {
		t.Fatalf("clear: %v", err)
	}
	if !removed {
		t.Error("clearing a recorded run reported nothing removed")
	}
	got, err := findDetachedRun(root, repoDir, rec.Phase)
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Errorf("the run survived the clear: %+v", got)
	}

	// The second clear is the unknown-phase case, and it must be
	// distinguishable rather than an error-free no-op reported as success.
	removed, err = clearDetachedRun(root, repoDir, rec.Phase)
	if err != nil {
		t.Fatalf("clearing an absent run errored: %v", err)
	}
	if removed {
		t.Error("clearing an absent run reported that it removed one")
	}
}

// TestClearingOneRunLeavesTheOthers pins the filter rather than the file being
// rewritten wholesale. A clear implemented as "write back only the phase I was
// asked about" passes every single-run test above and silently drops every
// other outstanding run — each of which is a leg already paid for on a host.
func TestClearingOneRunLeavesTheOthers(t *testing.T) {
	root := chdirDross(t)
	repoDir := filepath.Dir(root)
	keep := sampleDetachedRun()
	keep.Phase = "lane-host-affinity"
	keep.RunID = "r-keep"
	drop := sampleDetachedRun()

	for _, r := range []detachedRun{keep, drop} {
		if err := recordDetachedRun(root, repoDir, r); err != nil {
			t.Fatalf("record %s: %v", r.Phase, err)
		}
	}
	if _, err := clearDetachedRun(root, repoDir, drop.Phase); err != nil {
		t.Fatalf("clear: %v", err)
	}

	runs, err := readDetachedRuns(root, repoDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 || runs[0].Phase != keep.Phase || runs[0].RunID != keep.RunID {
		t.Errorf("clearing one run did not leave the other intact: %+v", runs)
	}
}

// TestDetachedRunIsAbsentFromLocalKeys is the authorization boundary.
//
// The record names a host AND a directory a later `verify results` reads a
// report out of. Exposed through the generic key-writer, an agent could point a
// fetch at a machine and a path the user never saw — the same argument that
// keeps remote_host and remote_pool out, one step further along, because this
// one also names where on that machine to read.
//
// Asserted over the key SET rather than by trying one spelling, so a future key
// added for any part of the record is caught too.
func TestDetachedRunIsAbsentFromLocalKeys(t *testing.T) {
	for key := range localKeys {
		if strings.Contains(key, "detached") || strings.Contains(key, "run_dir") {
			t.Errorf("localKeys exposes %q — `dross local set` must not be able to "+
				"name a host and a path a later fetch reads a report from", key)
		}
	}
}

// TestDetachedRunReadsRefuseATrackedStore is the committed-store refusal, and
// the reason it is asserted on this record specifically: a tracked local.toml
// carrying a detached run is a repo naming the machine AND the directory this
// machine's next `verify results` will read a mutation report out of. Refused
// unread, exactly as the grant beside it is.
//
// Every entry point is checked. A guard on the reader alone would still let a
// dispatch write into a tracked store, and a guard on the writer alone would
// let a fetch trust one.
func TestDetachedRunReadsRefuseATrackedStore(t *testing.T) {
	dir := t.TempDir()
	gitInit(t, dir, "git@github.com:Rivil/dross.git")
	root := filepath.Join(dir, RootDirName)
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	writeCompleteRoot(t, root)
	if err := os.WriteFile(filepath.Join(root, LocalFile), []byte("remote_host = \"evil\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustGit(t, dir, "add", "-f", RootDirName+"/"+LocalFile)
	mustGit(t, dir, "commit", "-m", "track the local store")

	if _, err := readDetachedRuns(root, dir); err == nil {
		t.Error("readDetachedRuns read a tracked local.toml")
	}
	if _, err := findDetachedRun(root, dir, "remote-run-detach"); err == nil {
		t.Error("findDetachedRun read a tracked local.toml")
	}
	if err := recordDetachedRun(root, dir, sampleDetachedRun()); err == nil {
		t.Error("recordDetachedRun wrote into a tracked local.toml")
	}
	if _, err := clearDetachedRun(root, dir, "remote-run-detach"); err == nil {
		t.Error("clearDetachedRun wrote into a tracked local.toml")
	}
}
