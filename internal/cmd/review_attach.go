package cmd

import (
	"fmt"
	"os"

	"github.com/Rivil/dross/internal/changes"
	"github.com/Rivil/dross/internal/gatestate"
	"github.com/Rivil/dross/internal/review"
	"github.com/Rivil/dross/internal/treefp"
)

// stashCommand sets a failed solo task's code aside without touching .dross/:
// the bookkeeping that records the failure must survive the stash.
const stashCommand = `git stash push -u -- . ':(exclude).dross'`

// soloRun reports whether /dross-execute recorded a solo run of phaseID.
func soloRun(repoDir, phaseID string) (bool, error) {
	ex, err := gatestate.LoadExecute(repoDir)
	if err != nil {
		return false, err
	}
	return ex != nil && ex.Phase == phaseID && ex.Mode == "solo", nil
}

// taskReviewFor returns the solo reviewer's record for one task, resolved for
// the change record, or nil when none applies: a pair run never has one, and
// the ledger must name this exact phase and task. The record is only ever
// read from the ledger the PostToolUse recorder wrote — no flag carries
// review content, so the executing agent cannot author its own review.
func taskReviewFor(repoDir, phaseID, taskID string) (*changes.TaskReview, error) {
	solo, err := soloRun(repoDir, phaseID)
	if err != nil || !solo {
		return nil, err
	}
	rec, err := gatestate.LoadReview(repoDir)
	if err != nil || rec == nil {
		return nil, err
	}
	if rec.Kind != review.KindTask || rec.Phase != phaseID || rec.Task != taskID {
		return nil, nil
	}
	return resolvedReview(rec.Rounds), nil
}

// resolvedReview turns ledger rounds into the change record's shape: the
// final standing, how many rounds reviewed this task's tree (a stale round
// reviewed none), and every finding with its resolution.
func resolvedReview(rounds []review.Round) *changes.TaskReview {
	st := review.StateOf(rounds)
	tr := &changes.TaskReview{Outcome: string(st.Status), Cause: st.Cause}
	for _, r := range rounds {
		if r.Outcome != review.OutcomeStale {
			tr.Rounds++
		}
	}
	for _, r := range review.Resolve(rounds) {
		tr.Findings = append(tr.Findings, changes.ReviewFinding{
			Round:      r.Round,
			Kind:       string(r.Kind),
			Severity:   string(r.Finding.Severity),
			Criterion:  r.Finding.Criterion,
			Text:       r.Finding.Text,
			Resolution: string(r.Resolution),
		})
	}
	return tr
}

// reviewFailureReason derives a failed task's reason from a review that
// failed it, or "" when the review did not.
func reviewFailureReason(tr *changes.TaskReview) string {
	if tr == nil {
		return ""
	}
	switch review.Status(tr.Outcome) {
	case review.StatusUnavailable:
		return "reviewer unavailable: " + tr.Cause
	case review.StatusExhausted:
		reason := "reviewer: " + tr.Cause
		for _, f := range tr.Findings {
			if f.Resolution == string(review.ResolvedUnresolved) {
				crit := ""
				if f.Criterion != "" {
					crit = " (" + f.Criterion + ")"
				}
				return fmt.Sprintf("%s — %s%s: %s", reason, f.Kind, crit, f.Text)
			}
		}
		return reason
	}
	return ""
}

// refuseDirtySoloFailure refuses to mark a solo task failed while it still has
// uncommitted code: a failed task is never committed (c-3), and its changes
// would otherwise ride into the next task's whole-tree review.
func refuseDirtySoloFailure(repoDir, phaseID, taskID string) error {
	solo, err := soloRun(repoDir, phaseID)
	if err != nil || !solo {
		return err
	}
	dirty, err := treefp.Dirty(repoDir)
	if err != nil {
		return fmt.Errorf("refusing to mark %s failed: could not tell whether code is uncommitted: %w", taskID, err)
	}
	if dirty {
		return fmt.Errorf("refusing to mark %s/%s failed with uncommitted code: a failed solo task is never committed, "+
			"and its changes would reach the next task's review. Set them aside first:\n\n    %s",
			phaseID, taskID, stashCommand)
	}
	return nil
}

// warnAttach names a review that could not be attached. The task record
// still lands: losing the bookkeeping of a committed task over a damaged
// review ledger would be the worse failure.
func warnAttach(phaseID, taskID string, err error) {
	fmt.Fprintf(os.Stderr, "warning: could not attach the solo review to %s/%s: %v\n", phaseID, taskID, err)
}
