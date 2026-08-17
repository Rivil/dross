package cmd

import "github.com/Rivil/dross/internal/phase"

// The two task vocabularies, and the conversion between them.
//
// plan.toml speaks pending | in_progress | done | failed (phase.Status*). The
// board speaks lifecycle statuses — task-in-progress, task-in-review — which is
// what reaches a tracker's workflow field and its dross/status label.
//
// Until now nothing in Go converted one to the other. The pairing existed only
// as prose in execute.md, where `dross task status … in_progress` happens to sit
// beside `--status task-in-progress`, held together by a guard that checks the
// two literals appear on adjacent lines. That is enough to stop the edges being
// transposed; it is not enough to read a board state and know what plan status
// it means, which is what an inbound sync has to do.
//
// So the mapping lives here, in one place, testable — rather than being
// re-derived at each edge that needs it.

// taskLifecycle is the lifecycle status the board carries for a plan status.
// A status with no board meaning maps to "", which callers read as "dross does
// not mirror this one".
//
// pending has no board status on purpose: a task nobody has started is the
// default state of a freshly synced issue, and asserting a label for it would
// mean re-labelling every task on every sync to say nothing changed.
//
// failed also has none. It is a local judgement about a run, not a column on a
// board, and inventing one would need a state-map key no prompt emits — which
// the lifecycle divergence guard refuses by design.
var taskLifecycle = map[string]string{
	phase.StatusInProgress: statusTaskInProgress,
	phase.StatusDone:       statusTaskInReview,
}

// boardStatusToPlan is the inverse, over the subset the board can express.
//
// Built by inverting taskLifecycle rather than written out, so the two cannot
// disagree — a mapping declared twice is a mapping that drifts.
var boardStatusToPlan = func() map[string]string {
	inv := make(map[string]string, len(taskLifecycle))
	for planStatus, lifecycle := range taskLifecycle {
		inv[lifecycle] = planStatus
	}
	return inv
}()

// lifecycleForPlanStatus returns the board lifecycle status for a plan task
// status, and whether the board mirrors that status at all.
func lifecycleForPlanStatus(planStatus string) (string, bool) {
	s, ok := taskLifecycle[planStatus]
	return s, ok
}

// planStatusForLifecycle returns the plan status a board lifecycle status
// means, and whether it maps to one.
//
// Deliberately keyed on the LIFECYCLE status — the value dross wrote into the
// issue's dross/status label — rather than on the tracker's own state name.
// Both provider state maps are non-injective: Jira sends in-progress and
// task-in-progress to the same "In Progress", and shipped and complete to the
// same "Done". Inverting them is ambiguous by construction, whereas the label
// records exactly what dross last asserted.
func planStatusForLifecycle(lifecycle string) (string, bool) {
	s, ok := boardStatusToPlan[lifecycle]
	return s, ok
}
