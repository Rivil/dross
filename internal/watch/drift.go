package watch

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/Rivil/dross/internal/milestone"
	"github.com/Rivil/dross/internal/phase"
	"github.com/Rivil/dross/internal/state"
)

// Drift buckets. These reuse the same phase-state signals `dross status`
// computes (runnable/failed task, verify verdict, `completed <slug>` in
// history), reimplemented in-package to avoid an internal/cmd import cycle
// (per the locked drift_signals decision).
const (
	// DriftInProgress: the phase still has a runnable or failed task, or no
	// loadable plan yet — execution isn't finished.
	DriftInProgress = "in_progress"
	// DriftCompleteUnverified: every task is done but verify hasn't passed
	// (verify.toml absent, or verdict empty/pending/partial/fail).
	DriftCompleteUnverified = "complete_unverified"
	// DriftVerifiedUnshipped: verify passed but the phase hasn't been completed
	// (no `completed <slug>` folded into state history by ship).
	DriftVerifiedUnshipped = "verified_unshipped"
)

// PhaseDrift is one phase that hasn't reached a clean, closed-out state.
type PhaseDrift struct {
	Phase string `json:"phase"`
	Kind  string `json:"kind"`
}

// ClassifyDrift walks every phase under root/phases and returns the ones still
// in flight, bucketed. It reads only phase files + the passed state (no board
// client), which is the drift-only path the board-off/unreachable digest leans
// on. A phase that is completed (shipped) contributes no drift.
func ClassifyDrift(root string, st *state.State) ([]PhaseDrift, error) {
	ids, err := phase.List(root)
	if err != nil {
		return nil, err
	}
	// Scope to the current milestone's phases when one is set. A heartbeat
	// surfaces in-flight work, not every phase ever — and scoping sidesteps the
	// state.History 50-entry cap: a phase shipped many milestones ago has no
	// `completed <slug>` record left, so unscoped it would misread as
	// verified_unshipped forever. With no current milestone (or an unloadable
	// one), fall back to every phase.
	if scope := currentMilestonePhases(root, st); scope != nil {
		ids = intersect(ids, scope)
	}
	var out []PhaseDrift
	for _, id := range ids {
		if kind, drifting := classifyPhase(root, id, st); drifting {
			out = append(out, PhaseDrift{Phase: id, Kind: kind})
		}
	}
	return out, nil
}

// currentMilestonePhases returns the set of phase ids in the current milestone,
// or nil when there's no current milestone or it can't be loaded (→ no scoping).
func currentMilestonePhases(root string, st *state.State) map[string]bool {
	if st == nil || st.CurrentMilestone == "" {
		return nil
	}
	m, err := milestone.Load(milestone.FilePath(root, st.CurrentMilestone))
	if err != nil {
		return nil
	}
	set := make(map[string]bool, len(m.Phases))
	for _, p := range m.Phases {
		set[p] = true
	}
	return set
}

// intersect keeps only the phase ids present in scope, preserving order.
func intersect(ids []string, scope map[string]bool) []string {
	var out []string
	for _, id := range ids {
		if scope[id] {
			out = append(out, id)
		}
	}
	return out
}

// classifyPhase returns a phase's drift bucket and whether it drifts at all.
func classifyPhase(root, id string, st *state.State) (string, bool) {
	// A completed phase is closed out — never drift, regardless of verdict.
	if stateHasCompleted(st, id) {
		return "", false
	}
	dir := phase.Dir(root, id)
	if readVerdict(filepath.Join(dir, "verify.toml")) == "pass" {
		return DriftVerifiedUnshipped, true
	}
	// Not verified-pass: distinguish complete-but-unverified from in-progress.
	// A missing or garbled plan is tolerated as "still in flight" — never a panic.
	plan, err := phase.LoadPlan(filepath.Join(dir, "plan.toml"))
	if err != nil {
		return DriftInProgress, true
	}
	_, _, _, failed := plan.Summary()
	if plan.NextRunnable() != nil || failed > 0 {
		return DriftInProgress, true
	}
	return DriftCompleteUnverified, true
}

// stateHasCompleted reports whether state history carries a `completed <id>`
// action — mirrors status.go's stateRecordsCompleted.
func stateHasCompleted(st *state.State, id string) bool {
	if st == nil {
		return false
	}
	for _, a := range st.History {
		if strings.Contains(a.Action, "completed "+id) {
			return true
		}
	}
	return false
}

// readVerdict scans a verify.toml for its `verdict` line and returns the value,
// or "" if the file is absent/unreadable/has no verdict — mirrors status.go's
// readVerifyVerdict so a partial or malformed verify.toml degrades quietly.
func readVerdict(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "verdict") {
			parts := strings.SplitN(line, "=", 2)
			if len(parts) != 2 {
				continue
			}
			return strings.Trim(strings.TrimSpace(parts[1]), `"`)
		}
	}
	return ""
}
