package boardsync

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
	got, ok := PlanStatusForLifecycle(StatusTaskComplete)
	if !ok || got != phase.StatusDone {
		t.Errorf("PlanStatusForLifecycle(%q) = %q,%v want %q,true — a card in the terminal column must read back as a done task",
			StatusTaskComplete, got, ok, phase.StatusDone)
	}

	if lc, _ := LifecycleForPlanStatus(phase.StatusDone); lc != StatusTaskInReview {
		t.Errorf("LifecycleForPlanStatus(%q) = %q, want %q — the terminal status must never appear at a per-task execute edge",
			phase.StatusDone, lc, StatusTaskInReview)
	}

	// The inversion is still exactly the inversion for everything else: only
	// the one terminal entry is extra.
	if len(BoardStatusToPlan) != len(TaskLifecycle)+1 {
		t.Errorf("BoardStatusToPlan has %d entries and TaskLifecycle %d — the board->plan direction should be the inversion plus exactly one terminal entry",
			len(BoardStatusToPlan), len(TaskLifecycle))
	}
	for planStatus, lifecycle := range TaskLifecycle {
		if got, _ := PlanStatusForLifecycle(lifecycle); got != planStatus {
			t.Errorf("PlanStatusForLifecycle(%q) = %q, want %q — the inverted pairs must stay inverted",
				lifecycle, got, planStatus)
		}
	}
}
