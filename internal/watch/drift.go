package watch

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/Rivil/dross/internal/changes"
	"github.com/Rivil/dross/internal/milestone"
	"github.com/Rivil/dross/internal/phase"
	"github.com/Rivil/dross/internal/state"
)

// Drift buckets. These reuse the same phase-state signals `dross status`
// computes (runnable/failed task, verify verdict, the phase's completion
// record), reimplemented in-package to avoid an internal/cmd import cycle
// (per the locked drift_signals decision).
const (
	// DriftInProgress: the phase still has a runnable or failed task, or no
	// loadable plan yet — execution isn't finished.
	DriftInProgress = "in_progress"
	// DriftCompleteUnverified: every task is done but verify hasn't passed
	// (verify.toml absent, or verdict empty/pending/partial/fail).
	DriftCompleteUnverified = "complete_unverified"
	// DriftVerifiedUnshipped: verify passed but the phase has neither been
	// shipped nor completed — its changes.json carries no `complete` status
	// (written by `dross phase complete` after it confirms the merge) and it has
	// no open PR awaiting one.
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
// on. A phase whose completion record reads `complete` contributes no drift.
func ClassifyDrift(root string, st *state.State) ([]PhaseDrift, error) {
	ids, err := phase.List(root)
	if err != nil {
		return nil, err
	}
	// Scope to the current milestone's phases when one is set. A heartbeat
	// surfaces in-flight work, not every phase ever. It used to be load-bearing
	// for correctness too — doneness came from the state.History 50-entry
	// window, so an old phase whose `completed <slug>` breadcrumb had aged out
	// misread as verified_unshipped forever, and scoping hid it. classifyPhase
	// now reads the phase's own completion record, which never scrolls, so this
	// is relevance filtering alone. With no current milestone (or an unloadable
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
	// The authority is the phase's own record, not a state.History breadcrumb:
	// history is a capped window, so a breadcrumb read has a long-finished phase
	// reappear in the digest fifty actions later.
	if changes.Complete(root, id) {
		return "", false
	}
	// A shipped phase with an open PR is waiting on a merge, not drifting.
	// `dross ship` leaves current_phase set and marks it shipped; only a
	// confirmed merge clears it, so this window is reachable on every phase
	// between the push and the merge. Calling it "verified but unshipped"
	// there is simply false — it names the one thing that already happened.
	if stateHasShipped(st, id) && phaseHasOpenPR(root, id) {
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

// stateHasShipped reports whether id is the current phase and reads `shipped`.
// Keyed on the status field rather than history so a phase re-shipped after
// review edits reads the same either way.
func stateHasShipped(st *state.State, id string) bool {
	return st != nil && st.CurrentPhase == id && st.CurrentPhaseStatus == "shipped"
}

// phaseHasOpenPR reports whether the phase's changes.json carries a PR number —
// the tracked, phase-scoped "this was shipped" record, written by `dross ship`
// post-push. Paired with stateHasShipped so a status field alone, with no PR to
// point at, still counts as drift.
func phaseHasOpenPR(root, id string) bool {
	ch, err := changes.Load(changes.FilePath(root, id), id)
	return err == nil && ch.PR > 0
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
