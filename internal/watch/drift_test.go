package watch

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/Rivil/dross/internal/state"
)

// writePhase scaffolds root/phases/<id> with an optional plan.toml and
// verify.toml body. Empty bodies are skipped (file absent).
func writePhase(t *testing.T, root, id, plan, verify string) {
	t.Helper()
	dir := filepath.Join(root, "phases", id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	if plan != "" {
		if err := os.WriteFile(filepath.Join(dir, "plan.toml"), []byte(plan), 0o644); err != nil {
			t.Fatalf("write plan: %v", err)
		}
	}
	if verify != "" {
		if err := os.WriteFile(filepath.Join(dir, "verify.toml"), []byte(verify), 0o644); err != nil {
			t.Fatalf("write verify: %v", err)
		}
	}
}

// writeChangesStatus writes root/phases/<id>/changes.json carrying just a
// status. An empty status writes the pre-status shape (no status field), which
// reads as "unknown", never as "done".
func writeChangesStatus(t *testing.T, root, id, status string) {
	t.Helper()
	dir := filepath.Join(root, "phases", id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	body := fmt.Sprintf(`{"phase":%q,"tasks":{}}`, id)
	if status != "" {
		body = fmt.Sprintf(`{"phase":%q,"status":%q,"tasks":{}}`, id, status)
	}
	if err := os.WriteFile(filepath.Join(dir, "changes.json"), []byte(body), 0o644); err != nil {
		t.Fatalf("write changes: %v", err)
	}
}

// agedOutHistory is a full 50-entry state history that carries no `completed`
// breadcrumb for any phase — what state.json looks like once fifty further
// actions have scrolled a completion out of the capped window.
func agedOutHistory() []state.Activity {
	h := make([]state.Activity, 0, 50)
	for i := 0; i < 50; i++ {
		h = append(h, state.Activity{Action: fmt.Sprintf("touched something %d", i)})
	}
	return h
}

func driftKind(ds []PhaseDrift, id string) (string, bool) {
	for _, d := range ds {
		if d.Phase == id {
			return d.Kind, true
		}
	}
	return "", false
}

const planPending = `[phase]
id = "p"
[[task]]
id = "t-1"
wave = 1
status = "pending"
`

const planDone = `[phase]
id = "p"
[[task]]
id = "t-1"
wave = 1
status = "done"
`

func TestClassifyDrift(t *testing.T) {
	root := t.TempDir()
	writePhase(t, root, "01-inprogress", planPending, "")                             // runnable task
	writePhase(t, root, "02-unverified", planDone, "")                                // all done, no verify
	writePhase(t, root, "03-unverified-pending", planDone, "verdict = \"pending\"\n") // all done, verify pending
	writePhase(t, root, "04-unshipped", planDone, "verdict = \"pass\"\n")             // verified, not completed
	writePhase(t, root, "05-shipped", planDone, "verdict = \"pass\"\n")               // verified AND completed → omit
	writeChangesStatus(t, root, "05-shipped", "complete")

	st := &state.State{}

	ds, err := ClassifyDrift(root, st)
	if err != nil {
		t.Fatalf("ClassifyDrift: %v", err)
	}

	want := map[string]string{
		"01-inprogress":         DriftInProgress,
		"02-unverified":         DriftCompleteUnverified,
		"03-unverified-pending": DriftCompleteUnverified,
		"04-unshipped":          DriftVerifiedUnshipped,
	}
	for id, kind := range want {
		got, ok := driftKind(ds, id)
		if !ok {
			t.Errorf("phase %s missing from drift, want %s", id, kind)
			continue
		}
		if got != kind {
			t.Errorf("phase %s: got %s, want %s", id, got, kind)
		}
	}
	// The shipped phase must NOT appear as drift.
	if kind, ok := driftKind(ds, "05-shipped"); ok {
		t.Errorf("completed phase 05-shipped should not drift, got %s", kind)
	}
}

// TestDriftShippedPhaseNotUnshipped: `dross ship` leaves current_phase set and
// marks it shipped; only a confirmed merge clears it. So a verify-pass phase
// with an open PR is waiting on a merge, not drifting — calling it
// "verified but unshipped" names the one thing that already happened. Without
// both signals it is genuinely unshipped and still drifts.
func TestDriftShippedPhaseNotUnshipped(t *testing.T) {
	root := t.TempDir()
	writePhase(t, root, "auth", planDone, "verdict = \"pass\"\n")
	if err := os.WriteFile(filepath.Join(root, "phases", "auth", "changes.json"),
		[]byte(`{"phase":"auth","pr":42,"base":"main","tasks":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	shipped := &state.State{CurrentPhase: "auth", CurrentPhaseStatus: "shipped"}
	ds, err := ClassifyDrift(root, shipped)
	if err != nil {
		t.Fatalf("ClassifyDrift: %v", err)
	}
	if kind, ok := driftKind(ds, "auth"); ok {
		t.Errorf("a shipped phase with an open PR is awaiting merge, not drift; got %s", kind)
	}

	// Control 1: the same phase with no shipped status still drifts.
	ds, err = ClassifyDrift(root, &state.State{})
	if err != nil {
		t.Fatalf("ClassifyDrift: %v", err)
	}
	if kind, ok := driftKind(ds, "auth"); !ok || kind != DriftVerifiedUnshipped {
		t.Errorf("without the shipped status the phase should still drift, got %q ok=%v", kind, ok)
	}

	// Control 2: shipped status but no recorded PR — nothing to wait on, so
	// the status field alone must not suppress the drift.
	if err := os.Remove(filepath.Join(root, "phases", "auth", "changes.json")); err != nil {
		t.Fatal(err)
	}
	ds, err = ClassifyDrift(root, shipped)
	if err != nil {
		t.Fatalf("ClassifyDrift: %v", err)
	}
	if kind, ok := driftKind(ds, "auth"); !ok || kind != DriftVerifiedUnshipped {
		t.Errorf("a shipped status with no PR to point at should still drift, got %q ok=%v", kind, ok)
	}
}

func TestClassifyDriftNoBoard(t *testing.T) {
	// The classifier takes no board client — it reads only phase files + state.
	root := t.TempDir()
	writePhase(t, root, "01-x", planPending, "")
	st := &state.State{}

	ds, err := ClassifyDrift(root, st)
	if err != nil {
		t.Fatalf("ClassifyDrift with no board: %v", err)
	}
	if kind, ok := driftKind(ds, "01-x"); !ok || kind != DriftInProgress {
		t.Fatalf("drift-only classification failed: %v", ds)
	}
}

func TestDriftMissingPlanTolerated(t *testing.T) {
	root := t.TempDir()
	// Phase dir with no plan.toml at all.
	writePhase(t, root, "01-noplan", "", "")
	// Phase dir with a garbled plan.toml.
	writePhase(t, root, "02-garbled", "this is : not [valid toml", "")

	st := &state.State{}
	ds, err := ClassifyDrift(root, st) // must not panic
	if err != nil {
		t.Fatalf("ClassifyDrift over broken plans: %v", err)
	}
	for _, id := range []string{"01-noplan", "02-garbled"} {
		if kind, ok := driftKind(ds, id); !ok || kind != DriftInProgress {
			t.Errorf("phase %s with broken plan should be in_progress, got %q ok=%v", id, kind, ok)
		}
	}
}

// TestClassifyDriftScopedToMilestone proves the heartbeat only surfaces the
// current milestone's phases — old phases from past milestones (whose
// `completed` record has aged out of the 50-entry history) don't pollute drift.
func TestClassifyDriftScopedToMilestone(t *testing.T) {
	root := t.TempDir()
	writePhase(t, root, "01-inscope", planPending, "")
	writePhase(t, root, "02-outofscope", planPending, "")

	mdir := filepath.Join(root, "milestones")
	if err := os.MkdirAll(mdir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mdir, "v1.toml"), []byte("phases = [\"01-inscope\"]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	st := &state.State{CurrentMilestone: "v1"}

	ds, err := ClassifyDrift(root, st)
	if err != nil {
		t.Fatalf("ClassifyDrift: %v", err)
	}
	if _, ok := driftKind(ds, "01-inscope"); !ok {
		t.Error("in-scope phase should still appear as drift")
	}
	if kind, ok := driftKind(ds, "02-outofscope"); ok {
		t.Errorf("out-of-scope phase must be filtered from drift, got %q", kind)
	}
}

// TestDriftCompletionRecordSurvivesAnAgedOutHistory is c-1's load-bearing case.
// The phase is verified-pass and plainly done, but state.json's history is a
// full 50-entry window that no longer carries its `completed <slug>` breadcrumb
// — exactly what a repo looks like a couple of phases later. A classifier
// reading history resurrects it as verified-unshipped drift on every heartbeat;
// one reading the phase's own record stays silent forever.
func TestDriftCompletionRecordSurvivesAnAgedOutHistory(t *testing.T) {
	root := t.TempDir()
	writePhase(t, root, "auth", planDone, "verdict = \"pass\"\n")
	writeChangesStatus(t, root, "auth", "complete")

	st := &state.State{History: agedOutHistory()}
	ds, err := ClassifyDrift(root, st)
	if err != nil {
		t.Fatalf("ClassifyDrift: %v", err)
	}
	if kind, ok := driftKind(ds, "auth"); ok {
		t.Errorf("a complete record with an aged-out breadcrumb must not drift, got %q", kind)
	}

	// Control: the breadcrumb is present but the record is not — history must
	// not be able to close a phase out either.
	writeChangesStatus(t, root, "auth", "")
	st = &state.State{History: []state.Activity{{Action: "completed auth"}}}
	ds, err = ClassifyDrift(root, st)
	if err != nil {
		t.Fatalf("ClassifyDrift: %v", err)
	}
	if kind, ok := driftKind(ds, "auth"); !ok || kind != DriftVerifiedUnshipped {
		t.Errorf("a `completed auth` breadcrumb with no record must not suppress drift, got %q ok=%v", kind, ok)
	}
}

// TestDriftShippedRecordStillDrifts pins the narrowing. `shipped` is a phase
// mid-flight between the push and the merge; with no open PR to point at there
// is nothing waiting, so the digest must still name it. Widening the suppressor
// to phaseDone (which counts shipped as done) silences exactly that.
func TestDriftShippedRecordStillDrifts(t *testing.T) {
	root := t.TempDir()
	writePhase(t, root, "auth", planDone, "verdict = \"pass\"\n")
	writeChangesStatus(t, root, "auth", "shipped")

	ds, err := ClassifyDrift(root, &state.State{})
	if err != nil {
		t.Fatalf("ClassifyDrift: %v", err)
	}
	if kind, ok := driftKind(ds, "auth"); !ok || kind != DriftVerifiedUnshipped {
		t.Errorf("a shipped record with no open PR should still drift, got %q ok=%v", kind, ok)
	}
}
