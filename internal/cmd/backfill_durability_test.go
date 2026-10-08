package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/changes"
	"github.com/Rivil/dross/internal/phase"
	"github.com/Rivil/dross/internal/state"
)

// TestDonenessIgnoresCompletionBreadcrumb is the guard against the history
// fallback coming back. A "completed <slug>" breadcrumb in state.json is a
// window entry, not a record: it is capped at 50 and evicted silently. A phase
// carrying one and no changes.json status is not done, and every doneness
// surface has to agree on that.
func TestDonenessIgnoresCompletionBreadcrumb(t *testing.T) {
	dir := progressRepo(t, "v1.3", "active", "breadcrumb-only")
	scaffoldPhase(t, dir, "breadcrumb-only", "")
	touchHistory(t, dir, "completed breadcrumb-only")

	root := filepath.Join(dir, ".dross")
	if phase.Done(root, "breadcrumb-only") {
		t.Error("a completion breadcrumb alone must not read done — that is the capped-window fallback this phase deleted")
	}
	if phase.IsDone(root, "breadcrumb-only", true) {
		t.Error("the inner reader must agree with the entry point")
	}
	rep, err := buildMilestoneProgress(root, "v1.3")
	if err != nil {
		t.Fatal(err)
	}
	if rep.Done != 0 {
		t.Errorf("done = %d, want 0 — milestone progress must not count a breadcrumb", rep.Done)
	}
}

// TestDonenessSurvivesHistoryEviction is the durability contract c-3 states: a
// phase marked complete keeps its done count once 50 further state actions age
// the whole history window past it. A reader still consulting history reports
// done before the churn and not-done after — the same silent regression
// mutation-diff-scope hit.
func TestDonenessSurvivesHistoryEviction(t *testing.T) {
	dir := progressRepo(t, "v1.3", "active", "marked")
	scaffoldPhase(t, dir, "marked", changes.StatusComplete)
	root := filepath.Join(dir, ".dross")

	if !phase.Done(root, "marked") {
		t.Fatal("precondition: a marker-complete phase reads done before any churn")
	}

	// 50 actions is exactly the cap, so nothing of the pre-churn window
	// survives — including any breadcrumb a fallback could have leaned on.
	for i := 0; i < 50; i++ {
		touchHistory(t, dir, "unrelated action")
	}
	s, err := state.Load(filepath.Join(root, state.File))
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range s.History {
		if strings.HasPrefix(strings.TrimSpace(a.Action), "completed marked") {
			t.Fatal("precondition: the churn must have evicted every completion breadcrumb")
		}
	}

	if !phase.Done(root, "marked") {
		t.Error("a durable marker must outlive the history window — doneness read a window, not a record")
	}
	rep, err := buildMilestoneProgress(root, "v1.3")
	if err != nil {
		t.Fatal(err)
	}
	if rep.Done != 1 {
		t.Errorf("done = %d, want 1 after history eviction", rep.Done)
	}
}

// TestDonenessAcceptsExactlyShippedAndComplete pins the switch to two values.
// A third accepted marker — "backfilled", "verified", a verify verdict — would
// widen doneness without any surface asking for it, which is the divergence the
// single reader exists to prevent.
func TestDonenessAcceptsExactlyShippedAndComplete(t *testing.T) {
	done := map[string]bool{
		changes.StatusShipped:  true,
		changes.StatusComplete: true,
	}
	candidates := []string{
		changes.StatusShipped,
		changes.StatusComplete,
		"",
		"backfilled",
		"inferred",
		"verified",
		"pass",
		"done",
		"in_progress",
		"planned",
		"COMPLETE",
	}
	for _, status := range candidates {
		dir := t.TempDir()
		root := filepath.Join(dir, ".dross")
		if err := os.MkdirAll(filepath.Join(root, "phases", "p"), 0o755); err != nil {
			t.Fatal(err)
		}
		if status != "" {
			if err := changes.SetStatus(root, "p", status); err != nil {
				t.Fatal(err)
			}
		}
		if got := phase.Done(root, "p"); got != done[status] {
			t.Errorf("status %q reads done=%v, want %v — the switch accepts exactly {shipped, complete}", status, got, done[status])
		}
	}
}
