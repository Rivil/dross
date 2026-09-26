package localstore

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// detached_test.go holds the detached-run record's tests, moved here from
// internal/cmd's local_detached_test.go so the package that owns the record is
// the one whose tests cover it. Assertions are unchanged; only the fixture is
// local — storeRoot in place of a full dross root, since nothing here resolves
// a project.

// TestImmediateRunRoundTripsAsUnscheduled pins the zero ScheduledFor, which is
// what distinguishes an immediate dispatch from an off-hours one.
//
// Asserted through Scheduled() rather than against the zero time directly: the
// callers ask the question that way, and a marshalling that turned the zero
// time into some other sentinel would still have to answer it correctly.
func TestImmediateRunRoundTripsAsUnscheduled(t *testing.T) {
	root := storeRoot(t)
	repoDir := filepath.Dir(root)
	rec := sampleDetachedRun()
	rec.ScheduledFor = time.Time{}
	rec.State = "running"

	if err := RecordDetachedRun(root, repoDir, rec); err != nil {
		t.Fatalf("record: %v", err)
	}
	got, err := FindDetachedRun(root, repoDir, rec.Phase)
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

// TestTwoPhasesEachKeepTheirOwnRun is the other half of the guard above: the
// refusal is per PHASE, not a global one-run-at-a-time lock. A rule that
// refused any second run would make the pool useless and would block a second
// phase for the length of the first one's leg.
func TestTwoPhasesEachKeepTheirOwnRun(t *testing.T) {
	root := storeRoot(t)
	repoDir := filepath.Dir(root)
	a := sampleDetachedRun()
	b := sampleDetachedRun()
	b.Phase = "lane-host-affinity"
	b.RunID = "r-20260831-0100"

	if err := RecordDetachedRun(root, repoDir, a); err != nil {
		t.Fatalf("record a: %v", err)
	}
	if err := RecordDetachedRun(root, repoDir, b); err != nil {
		t.Fatalf("a second PHASE was refused a run: %v", err)
	}
	for _, want := range []DetachedRun{a, b} {
		got, err := FindDetachedRun(root, repoDir, want.Phase)
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
	root := storeRoot(t)
	repoDir := filepath.Dir(root)
	rec := sampleDetachedRun()
	if err := RecordDetachedRun(root, repoDir, rec); err != nil {
		t.Fatalf("record: %v", err)
	}

	removed, err := ClearDetachedRun(root, repoDir, rec.Phase)
	if err != nil {
		t.Fatalf("clear: %v", err)
	}
	if !removed {
		t.Error("clearing a recorded run reported nothing removed")
	}
	got, err := FindDetachedRun(root, repoDir, rec.Phase)
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Errorf("the run survived the clear: %+v", got)
	}

	// The second clear is the unknown-phase case, and it must be
	// distinguishable rather than an error-free no-op reported as success.
	removed, err = ClearDetachedRun(root, repoDir, rec.Phase)
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
	root := storeRoot(t)
	repoDir := filepath.Dir(root)
	keep := sampleDetachedRun()
	keep.Phase = "lane-host-affinity"
	keep.RunID = "r-keep"
	drop := sampleDetachedRun()

	for _, r := range []DetachedRun{keep, drop} {
		if err := RecordDetachedRun(root, repoDir, r); err != nil {
			t.Fatalf("record %s: %v", r.Phase, err)
		}
	}
	if _, err := ClearDetachedRun(root, repoDir, drop.Phase); err != nil {
		t.Fatalf("clear: %v", err)
	}

	runs, err := ReadDetachedRuns(root, repoDir)
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
	for key := range Keys {
		if strings.Contains(key, "detached") || strings.Contains(key, "run_dir") {
			t.Errorf("Keys exposes %q — `dross local set` must not be able to "+
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
	root := filepath.Join(dir, ".dross")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, File), []byte("remote_host = \"evil\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustGit(t, dir, "add", "-f", ".dross/"+File)
	mustGit(t, dir, "commit", "-m", "track the local store")

	if _, err := ReadDetachedRuns(root, dir); err == nil {
		t.Error("ReadDetachedRuns read a tracked local.toml")
	}
	if _, err := FindDetachedRun(root, dir, "remote-run-detach"); err == nil {
		t.Error("FindDetachedRun read a tracked local.toml")
	}
	if err := RecordDetachedRun(root, dir, sampleDetachedRun()); err == nil {
		t.Error("RecordDetachedRun wrote into a tracked local.toml")
	}
	if _, err := ClearDetachedRun(root, dir, "remote-run-detach"); err == nil {
		t.Error("ClearDetachedRun wrote into a tracked local.toml")
	}
}
