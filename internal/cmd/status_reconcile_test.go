package cmd

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// TestStatusNamesReconcileNotAChoreList: the complaint this closes is that a
// read-only surface handed the user N lines to type. One suggestion, not N.
func TestStatusNamesReconcileNotAChoreList(t *testing.T) {
	reconcileFixture(t, "alpha", "beta", "gamma")

	var out string
	if err := runCmdCapturing(t, &out, Status()); err != nil {
		t.Fatalf("status: %v", err)
	}
	if !strings.Contains(out, "dross phase reconcile") {
		t.Fatalf("status does not name the batch verb:\n%s", out)
	}
	if n := strings.Count(out, "dross phase complete"); n > 0 {
		t.Errorf("status printed %d per-phase complete line(s) alongside the batch verb — that is the chore list this replaces:\n%s", n, out)
	}
	if !strings.Contains(out, "3 phase branches") {
		t.Errorf("status does not say how many are waiting:\n%s", out)
	}
}

// TestSinglePhaseStillNamesCompleteDirectly: naming a batch verb for one item
// is worse guidance than naming the one command that does it.
func TestSinglePhaseStillNamesCompleteDirectly(t *testing.T) {
	reconcileFixture(t, "alpha")

	var out string
	if err := runCmdCapturing(t, &out, Status()); err != nil {
		t.Fatalf("status: %v", err)
	}
	if strings.Contains(out, "dross phase reconcile") {
		t.Errorf("status suggested the batch verb for a single outstanding phase:\n%s", out)
	}
}

// TestReadOnlySurfacesMutateNothing is the property that makes these surfaces
// safe to run at a glance — and the one a "helpful" suggestion is most likely
// to break by acting on what it found.
//
// Snapshotting the phase dirs, the branch list and state.json before and after
// is the assertion: anything status or watch did would show up in one of the
// three.
func TestReadOnlySurfacesMutateNothing(t *testing.T) {
	dir := reconcileFixture(t, "alpha", "beta")
	root := filepath.Join(dir, ".dross")

	snapshot := func() (phases []string, branches string, st string) {
		t.Helper()
		entries, err := os.ReadDir(filepath.Join(root, "phases"))
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			phases = append(phases, e.Name())
		}
		sort.Strings(phases)
		branches = mustGit(t, dir, "branch", "--list", "--format=%(refname:short)")
		b, err := os.ReadFile(filepath.Join(root, "state.json"))
		if err != nil {
			t.Fatal(err)
		}
		return phases, branches, string(b)
	}

	beforePhases, beforeBranches, beforeState := snapshot()

	var out string
	if err := runCmdCapturing(t, &out, Status()); err != nil {
		t.Fatalf("status: %v", err)
	}

	afterPhases, afterBranches, afterState := snapshot()
	if strings.Join(beforePhases, ",") != strings.Join(afterPhases, ",") {
		t.Errorf("status changed the phase dirs: %v -> %v", beforePhases, afterPhases)
	}
	if beforeBranches != afterBranches {
		t.Errorf("status changed the branch list:\n%s\n->\n%s", beforeBranches, afterBranches)
	}
	if beforeState != afterState {
		t.Errorf("status wrote to state.json — a read-only surface that mutates on a glance is a trap")
	}
}

// TestReconcilableCountDoesNotReadState: the count is derived from each
// phase's own completion record, so state.json is not an input at all — a
// garbled one changes nothing.
//
// This inverts the assertion that stood here before. The count used to load
// state.json for its `completed <id>` breadcrumbs and degrade to zero when that
// load failed; the breadcrumb read is gone (a capped 50-entry window is not a
// record), and with it the only reason status ever opened state.json on this
// path. Zero-on-unreadable would now be a silent wrong answer rather than a
// graceful one.
func TestReconcilableCountDoesNotReadState(t *testing.T) {
	dir := reconcileFixture(t, "alpha", "beta")
	root := filepath.Join(dir, ".dross")
	if err := os.WriteFile(filepath.Join(root, "state.json"), []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := reconcilableCount(root); got != 2 {
		t.Errorf("reconcilableCount with an unreadable state = %d, want 2 — state.json is not an input", got)
	}
}

// TestReconcilableCountIgnoresAnUnreadablePhaseList: a status line is not worth
// failing a status call over, so the count still degrades to zero rather than
// propagating a read error into a command the user runs to orient themselves.
func TestReconcilableCountIgnoresAnUnreadablePhaseList(t *testing.T) {
	dir := reconcileFixture(t, "alpha", "beta")
	root := filepath.Join(dir, ".dross")
	if err := os.RemoveAll(filepath.Join(root, "phases")); err != nil {
		t.Fatal(err)
	}
	// A regular file where the phases directory belongs: listing it errors.
	if err := os.WriteFile(filepath.Join(root, "phases"), []byte("not a directory"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := reconcilableCount(root); got != 0 {
		t.Errorf("reconcilableCount over an unlistable phases dir = %d, want 0", got)
	}
}
