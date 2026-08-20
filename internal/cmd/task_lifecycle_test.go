package cmd

import (
	"testing"

	"github.com/Rivil/dross/internal/phase"
)

// TestTerminalTaskStatusIsReadableButNeverDerived pins the one deliberate
// asymmetry in the plan<->board conversion.
//
// task-complete is written once per phase, at ship finalize, over every card at
// once — never at a per-task edge. So no plan status may derive it: if
// committing a task started emitting it, one finished task would mark itself as
// though its whole phase had shipped. The reverse reading is still required —
// a card a human drags into the terminal column has to read back as a done
// task, not as a column an inbound sync cannot interpret.
func TestTerminalTaskStatusIsReadableButNeverDerived(t *testing.T) {
	got, ok := planStatusForLifecycle(statusTaskComplete)
	if !ok || got != phase.StatusDone {
		t.Errorf("planStatusForLifecycle(%q) = %q,%v want %q,true — a card in the terminal column must read back as a done task",
			statusTaskComplete, got, ok, phase.StatusDone)
	}

	if lc, _ := lifecycleForPlanStatus(phase.StatusDone); lc != statusTaskInReview {
		t.Errorf("lifecycleForPlanStatus(%q) = %q, want %q — the terminal status must never appear at a per-task execute edge",
			phase.StatusDone, lc, statusTaskInReview)
	}

	// The inversion is still exactly the inversion for everything else: only
	// the one terminal entry is extra.
	if len(boardStatusToPlan) != len(taskLifecycle)+1 {
		t.Errorf("boardStatusToPlan has %d entries and taskLifecycle %d — the board->plan direction should be the inversion plus exactly one terminal entry",
			len(boardStatusToPlan), len(taskLifecycle))
	}
	for planStatus, lifecycle := range taskLifecycle {
		if got, _ := planStatusForLifecycle(lifecycle); got != planStatus {
			t.Errorf("planStatusForLifecycle(%q) = %q, want %q — the inverted pairs must stay inverted",
				lifecycle, got, planStatus)
		}
	}
}

// TestTerminalRunClearsTheReviewLabel is c-2's headline, end to end: after the
// finalize emission, no mirrored task card still carries
// dross/status:task-in-review, and each one reads back done on its provider.
//
// This is the assertion `task-complete` had to exist for — it is not a valid
// --status until this task adds it, so before now the run under test could not
// even be spelled.
//
// The done verdict is asserted by the run exiting zero rather than by a second
// read: closeBoardIssue re-reads every card and fails the command unless the
// tracker agrees, so a fake that closed nothing could not reach this point.
func TestTerminalRunClearsTheReviewLabel(t *testing.T) {
	f := newTaskCloseFake(t)
	dir := taskCloseRepo(t, "forgejo", f, "t-1", "t-2")

	// The execute loop's state: every card mirrored and sitting in review.
	if err := runCmd(t, Issue(), "task-sync", "01-auth", "--status", statusTaskInReview); err != nil {
		t.Fatalf("seed task-sync: %v", err)
	}
	for _, id := range []string{"t-1", "t-2"} {
		key, _ := loadBoardFile(t, dir).TaskIssue("01-auth", id)
		if !slicesHas(f.labelsOf(key), statusLabel(statusTaskInReview)) {
			t.Fatalf("%s does not start in review: %v", id, f.labelsOf(key))
		}
	}

	// Ship finalize.
	_ = captureStdout(t, func() {
		_ = captureStderr(t, func() {
			if err := runCmd(t, Issue(), "task-sync", "01-auth", "--status", statusTaskComplete, "--close"); err != nil {
				t.Fatalf("terminal task-sync: %v", err)
			}
		})
	})

	bd := loadBoardFile(t, dir)
	for _, id := range []string{"t-1", "t-2"} {
		key, ok := bd.TaskIssue("01-auth", id)
		if !ok {
			t.Fatalf("%s lost its link", id)
		}
		labels := f.labelsOf(key)
		if slicesHas(labels, statusLabel(statusTaskInReview)) {
			t.Errorf("%s still carries %s after finalize: %v", id, statusLabel(statusTaskInReview), labels)
		}
		if !slicesHas(labels, statusLabel(statusTaskComplete)) {
			t.Errorf("%s does not carry %s: %v", id, statusLabel(statusTaskComplete), labels)
		}
		if f.closeCount(key) != 1 {
			t.Errorf("%s closes = %d, want 1", id, f.closeCount(key))
		}
	}
}
