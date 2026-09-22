package cmd

import (
	"fmt"

	"github.com/Rivil/dross/internal/boardsync"

	"github.com/spf13/cobra"
)

// `dross issue reap` — the stranded-mirror sweep.
//
// It is DRY-RUN BY DEFAULT. Without --apply it classifies and prints, and
// issues no write of any kind. That is not politeness about a destructive verb:
// the first live run of this command against a real board moves ninety cards at
// once, and a plan that can be read, scoped and re-read before anything moves is
// what makes that a reviewable act rather than a leap.
//
// One verb owns all five namespaces rather than a --reap mode on each existing
// sync verb (the locked verb_shape decision): one classify-then-close pipeline,
// one test site, and a whole-board plan printable in a single pass.
func issueReap() *cobra.Command {
	var namespaces []string
	var apply, undo bool
	c := &cobra.Command{
		Use:   "reap",
		Short: "Close board mirrors the forward lifecycle left stranded",
		Long: `Classify every dross-authored board card against the record on disk and
close the ones whose artefact provably finished.

Dry-run by default: with no --apply it prints the plan — card id, lane, and the
record that justifies closing it — and writes nothing.

Every close decision comes from the on-disk record, never from the card's own
state. A card whose artefact is not complete is never closed; a card no record
explains is named as unattributable and left open.`,
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			ctx, enabled, err := openBoard()
			if err != nil {
				return err
			}
			if !enabled {
				return nil // no-op when board sync is off
			}
			if undo {
				// Refused rather than quietly ignored: --apply says "write the
				// plan" and --namespace scopes one, and neither has any meaning
				// for a reversal that replays a recorded run verbatim. Silently
				// dropping them would let an operator believe they had scoped
				// an undo they had not.
				if apply {
					return fmt.Errorf("--undo and --apply are opposite directions; pass one")
				}
				if len(namespaces) > 0 {
					return fmt.Errorf("--undo replays the last recorded run verbatim and cannot be scoped by --namespace")
				}
				return boardsync.Undo(ctx)
			}
			plan, unclassifiable, err := boardsync.Inventory(ctx, namespaces)
			if err != nil {
				return err
			}
			printReapPlan(plan, unclassifiable)
			if apply {
				return boardsync.Apply(ctx, plan)
			}
			return nil
		},
	}
	c.Flags().StringArrayVar(&namespaces, "namespace", nil,
		"limit the sweep to one mirror namespace (repeatable; default: every namespace)")
	c.Flags().BoolVar(&apply, "apply", false, "write the plan to the board (default: dry run)")
	c.Flags().BoolVar(&undo, "undo", false, "restore the cards the last applied run closed, to the exact state each held before it")
	return c
}

// printReapPlan renders the classified inventory grouped by lane.
//
// Every stranded line carries the RECORD that justified it, not just the card
// id. A plan that only lists ids asks the reader to take ninety closes on
// trust; a plan that names `phases/03-auth/changes.json status=complete` beside
// each one can be argued with.
func printReapPlan(plan *boardsync.ReapPlan, unclassifiable []boardsync.ReapCard) {
	byLane := map[string][]boardsync.ReapCard{}
	unattributableByLane := map[string][]boardsync.ReapCard{}
	for _, c := range plan.Cards {
		byLane[c.Lane] = append(byLane[c.Lane], c)
	}
	for _, c := range plan.Unattributable {
		unattributableByLane[c.Lane] = append(unattributableByLane[c.Lane], c)
	}

	lanes := 0
	for _, lane := range boardsync.ReapLanes {
		stranded := byLane[lane.Name]
		unattributable := unattributableByLane[lane.Name]
		if len(stranded) == 0 && len(unattributable) == 0 {
			continue
		}
		lanes++
		Printf("%s (%d stranded, %d unattributable) -> %s\n",
			lane.Name, len(stranded), len(unattributable), lane.Terminal)
		for _, c := range stranded {
			Printf("  %-10s %s\n", c.Key, c.Why)
		}
		for _, c := range unattributable {
			Printf("  %-10s [unattributable] %s\n", c.Key, c.Why)
		}
	}

	if len(unclassifiable) > 0 {
		// Cards dross wrote that carry no identity label. Not a human's issue
		// to leave alone, and not something any record can speak for — so
		// named, and never closed.
		Printf("Unclassifiable (%d) -> never closed\n", len(unclassifiable))
		for _, c := range unclassifiable {
			Printf("  %-10s %s\n", c.Key, c.Why)
		}
	}

	if len(plan.Cards) == 0 && len(plan.Unattributable) == 0 && len(unclassifiable) == 0 {
		Print("no stranded mirrors — every card matches its record")
		return
	}
	Printf("\n%d stranded across %d %s, %d unattributable (named, never closed)\n",
		len(plan.Cards), lanes, plural(lanes, "lane", "lanes"), len(plan.Unattributable))
}
