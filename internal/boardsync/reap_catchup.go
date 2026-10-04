package boardsync

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Rivil/dross/internal/changes"
	"github.com/Rivil/dross/internal/forge"
	"github.com/Rivil/dross/internal/phase"
)

// The catch-up half of the sweep. Reap closes cards the forward lifecycle left
// open; a phase that never got a card at all — its sync failed, or ran before
// the board was enabled — has nothing for that sweep to find. Catch-up lists
// those completed phases and, under --apply, creates their cards straight at
// the terminal state through the same FinalizePhase `dross phase complete`
// runs (locked catch_up_home).

// MissingPhase is a completed phase the board is missing cards for.
type MissingPhase struct {
	Phase     string
	PhaseCard bool     // the phase's own card is missing
	Tasks     []string // plan tasks with no card
	// CannotCreate, when set, is why --apply skips the phase: the record
	// FinalizePhase builds a card from is not there to build it.
	CannotCreate string
}

// catchUpLanes are the namespaces catch-up creates cards in.
var catchUpLanes = []string{"Phases", "Tasks"}

// FindMissing walks every phase whose changes.json reads complete and asks the
// tracker which of its cards exist — one State "all" lookup on dross/phase:<id>
// per phase, which phase and task cards both carry. Never a bulk marker
// listing: those come back one page at a time on the forges, and a truncated
// listing would read the overflow as missing and mint duplicates.
//
// A card board.json links counts as present too, whatever the lookup says. It
// is read-only: a dry run built on it writes nothing.
func FindMissing(ctx *Ctx, namespaces []string) ([]MissingPhase, error) {
	if !catchUpInScope(namespaces) {
		return nil, nil
	}
	ids, err := phase.List(ctx.Root)
	if err != nil {
		return nil, err
	}
	var out []MissingPhase
	for _, id := range ids {
		if !changes.Complete(ctx.Root, id) {
			continue
		}
		m, err := missingFor(ctx, id)
		if err != nil {
			return nil, fmt.Errorf("catch-up %s: %w", id, err)
		}
		if m.PhaseCard || len(m.Tasks) > 0 {
			out = append(out, m)
		}
	}
	return out, nil
}

// catchUpInScope matches the --namespace values the way ValidateReapNamespaces
// accepts them — trimmed, any case — so `--namespace phases` scopes catch-up
// exactly as it scopes the sweep.
func catchUpInScope(namespaces []string) bool {
	if len(namespaces) == 0 {
		return true
	}
	for _, n := range namespaces {
		for _, lane := range catchUpLanes {
			if strings.EqualFold(strings.TrimSpace(n), lane) {
				return true
			}
		}
	}
	return false
}

func missingFor(ctx *Ctx, id string) (MissingPhase, error) {
	m := MissingPhase{Phase: id}
	found, err := ctx.Client.ListIssues(forge.IssueFilter{State: "all", Labels: []string{PhaseLabel(id)}})
	if err != nil {
		return m, Wrap(err)
	}
	phaseCard := false
	taskCards := map[string]bool{}
	taskPrefix := TaskLabel(id, "")
	for _, iss := range found {
		if !HasMarker(iss) {
			continue
		}
		isTask := false
		for _, l := range iss.Labels {
			if t, ok := strings.CutPrefix(l, taskPrefix); ok && t != "" {
				taskCards[t], isTask = true, true
			}
		}
		if !isTask {
			phaseCard = true
		}
	}
	if _, ok := ctx.Board.PhaseIssue(id); ok {
		phaseCard = true
	}
	m.PhaseCard = !phaseCard

	dir := phase.Dir(ctx.Root, id)
	if plan, err := phase.LoadPlan(filepath.Join(dir, "plan.toml")); err == nil {
		for _, t := range plan.Task {
			if _, linked := ctx.Board.TaskIssue(id, t.ID); !linked && !taskCards[t.ID] {
				m.Tasks = append(m.Tasks, t.ID)
			}
		}
	}
	if m.PhaseCard || len(m.Tasks) > 0 {
		if _, err := os.Stat(filepath.Join(dir, "spec.toml")); err != nil {
			m.CannotCreate = "no spec.toml to build its cards from"
		}
	}
	return m, nil
}

// ApplyMissing creates each missing phase's cards at their terminal state
// through FinalizePhase. Record-and-continue: one phase that fails does not
// stop the rest, and the error names every one that did. Creations are not
// journaled — reap --undo restores cards a sweep closed, and these were never
// open.
func ApplyMissing(ctx *Ctx, missing []MissingPhase) error {
	var failed []string
	for _, m := range missing {
		if m.CannotCreate != "" {
			continue
		}
		if err := FinalizePhase(ctx, m.Phase); err != nil {
			failed = append(failed, fmt.Sprintf("%s: %v", m.Phase, err))
		}
	}
	if len(failed) > 0 {
		return fmt.Errorf("catch-up could not create the cards of %d phase(s):\n  %s", len(failed), strings.Join(failed, "\n  "))
	}
	return nil
}
