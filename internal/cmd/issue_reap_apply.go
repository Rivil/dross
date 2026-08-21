package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/Rivil/dross/internal/reaplog"
)

// The write half of the sweep.
//
// Two properties matter more than anything else here, and both are about what
// happens when it goes wrong on card 40 of 90.
//
// First, failure isolation. A refused close must not stop the remaining cards
// from reaching their terminal state — one stuck card would otherwise strand
// every card after it, which is precisely the defect this whole phase exists to
// fix. The run collects per-card failures, names each by issue id at the end,
// and exits non-zero.
//
// Second, the journal is written from the state read BEFORE each close. A
// ledger built from the post-close read-back would record the terminal state
// dross just wrote, and undo would then restore every card to the column it is
// already in — a no-op that reports success.

// reapInventory is the whole-board inventory: the cards board.json links, plus
// the dross-authored cards no namespace links at all, deduped by issue id.
//
// It is what BOTH the dry run and the apply run walk. A dry run showing only
// half the inventory would be a plan that does not describe what --apply does,
// which defeats the point of having one.
func reapInventory(ctx *boardCtx, namespaces []string) (*reapPlan, []reapCard, error) {
	if err := validateReapNamespaces(namespaces); err != nil {
		return nil, nil, err
	}
	plan, err := classifyReap(ctx, namespaces)
	if err != nil {
		return nil, nil, err
	}
	lanes, err := resolveReapLanes(namespaces)
	if err != nil {
		return nil, nil, err
	}
	found, unclassifiable, err := discoverReap(ctx, lanes)
	if err != nil {
		return nil, nil, err
	}
	orphanPlan, err := buildReapPlan(ctx, found)
	if err != nil {
		return nil, nil, err
	}

	// Deduped by issue id even though discoverReap already skips linked cards:
	// the two sources overlap by construction, and a card planned twice would
	// be closed twice and counted twice.
	seen := map[string]bool{}
	for _, c := range plan.Cards {
		seen[c.Key] = true
	}
	for _, c := range plan.Unattributable {
		seen[c.Key] = true
	}
	for _, c := range orphanPlan.Cards {
		if !seen[c.Key] {
			seen[c.Key] = true
			plan.Cards = append(plan.Cards, c)
		}
	}
	for _, c := range orphanPlan.Unattributable {
		if !seen[c.Key] {
			seen[c.Key] = true
			plan.Unattributable = append(plan.Unattributable, c)
		}
	}
	var orphans []reapCard
	for _, c := range unclassifiable {
		if !seen[c.Key] {
			seen[c.Key] = true
			orphans = append(orphans, c)
		}
	}
	return plan, orphans, nil
}

// reapFailure is one card the sweep could not close. A distinct type so the
// walk can tell a refused card apart from a failure of the run itself: the
// first is collected and reported, the second aborts.
type reapFailure struct {
	key string
	err error
}

func (e *reapFailure) Error() string { return fmt.Sprintf("%s: %v", e.key, e.err) }
func (e *reapFailure) Unwrap() error { return e.err }

// lanesDroppingTheirLink names the lanes whose forward close path also removes
// the board.json entry — today only the backlog, where reconcileBacklog drops a
// key that has left the live set.
//
// Deliberately not every lane. Dropping a link is not tidiness: it is the only
// record that ties an artefact to its card, and the sweep drops one only where
// the forward path already does. Each drop is recorded in the journal so undo
// can put it back.
var lanesDroppingTheirLink = map[string]bool{"Backlog": true}

// applyReap writes the plan to the board, journalling each card's prior state
// first.
func applyReap(ctx *boardCtx, plan *reapPlan) error {
	run := reaplog.Run{StartedAt: time.Now().UTC()}
	var failures []*reapFailure
	closed := 0

	for _, card := range plan.Cards {
		// Read BEFORE the write. This is the ledger's whole value: a state
		// captured after the close is the state dross just wrote, and undo
		// built from it restores nothing.
		prior, err := ctx.client.GetIssue(card.Key)
		if err != nil || prior == nil {
			if err == nil {
				err = fmt.Errorf("issue not found")
			}
			failures = append(failures, &reapFailure{key: card.Key, err: fmt.Errorf("read prior state: %w", err)})
			run.Cards = append(run.Cards, reaplog.Card{Issue: card.Key, Class: card.Lane, Outcome: reaplog.OutcomeFailed})
			continue
		}
		entry := reaplog.Card{
			Issue:         card.Key,
			Class:         card.Lane,
			PriorState:    prior.WorkflowState,
			PriorResolved: prior.Resolved,
			PriorLabels:   prior.Labels,
		}

		// closeBoardIssue writes the MAPPED lane terminal and verifies the
		// read-back, so a workflow that accepted the request and refused the
		// transition is a failure here rather than a false "closed" line.
		if err := closeBoardIssue(ctx, card.Key, card.Terminal); err != nil {
			entry.Outcome = reaplog.OutcomeFailed
			run.Cards = append(run.Cards, entry)
			failures = append(failures, &reapFailure{key: card.Key, err: err})
			continue
		}
		entry.Outcome = reaplog.OutcomeClosed
		if lanesDroppingTheirLink[card.Lane] {
			entry.DroppedLink = dropBacklogLink(ctx, card.Key)
		}
		run.Cards = append(run.Cards, entry)
		closed++
	}

	if err := ctx.board.Save(ctx.boardPath); err != nil {
		return err
	}
	if err := appendReapRun(ctx, run); err != nil {
		return err
	}

	Printf("\nreaped %d card(s)", closed)
	if len(failures) == 0 {
		Print("")
		return nil
	}
	Printf(", %d failed:\n", len(failures))
	for _, f := range failures {
		fmt.Fprintf(os.Stderr, "  %s\n", f.Error())
	}
	// Named by issue id and non-zero: a sweep that half-worked and exited 0
	// would be indistinguishable from one that worked.
	return fmt.Errorf("%d of %d card(s) could not be closed", len(failures), len(plan.Cards))
}

// dropBacklogLink removes the board.json backlog key pointing at this issue and
// returns it, so the journal can restore it.
func dropBacklogLink(ctx *boardCtx, issue string) string {
	for _, key := range ctx.board.BacklogKeys() {
		if id, ok := ctx.board.BacklogID(key); ok && id == issue {
			ctx.board.DeleteBacklog(key)
			return key
		}
	}
	return ""
}

// appendReapRun writes the run to the ledger. A run that closed nothing is not
// journalled: an empty undo target would shadow the real one, so a second
// no-op apply would make the previous run unreachable.
func appendReapRun(ctx *boardCtx, run reaplog.Run) error {
	if len(run.Cards) == 0 {
		return nil
	}
	path := reaplog.FilePath(ctx.root)
	log, err := reaplog.Load(path)
	if err != nil {
		return err
	}
	log.Append(run)
	return log.Save(path)
}

// reapLogPathFor is the ledger path for a repo root, for callers that hold a
// dross root rather than a boardCtx.
func reapLogPathFor(root string) string { return filepath.Join(root, reaplog.File) }
