package watch

import (
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

	st := &state.State{History: []state.Activity{{Action: "completed 05-shipped"}}}

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
