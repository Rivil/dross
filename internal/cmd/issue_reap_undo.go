package cmd

import (
	"fmt"
	"os"

	"github.com/Rivil/dross/internal/forge"
	"github.com/Rivil/dross/internal/reaplog"
)

// `dross issue reap --undo` — reverse the last applied run.
//
// The reversal lives on the verb that caused it rather than an `unreap`
// sibling, and it targets the LAST run only: that is the locked undo_shape
// decision, and the last run is the only one anyone realistically wants back.
//
// It writes each card's recorded prior column back UNCONDITIONALLY. There is
// deliberately no conflict check: a card someone moved since the sweep is
// exactly the card most likely to need putting back, and skipping it would make
// undo quietly partial — the operator would read "restored" and find a board
// that still is not.

// undoReap restores the cards of the last applied run.
func undoReap(ctx *boardCtx) error {
	// The capability gate is asserted HERE, at the call site, rather than on
	// BoardClient — the same shape the no-linker fallback uses. A backend with
	// no column model cannot restore an arbitrary prior column, and the honest
	// answer is to refuse by name and write nothing rather than reopen every
	// card into `open` and report success.
	writer, ok := ctx.client.(forge.StateWriter)
	if !ok {
		return fmt.Errorf("--undo needs a board whose cards have a workflow state; %s has no column model, so a card that sat in a named column cannot be restored — nothing was written",
			ctx.proj.Board.Provider)
	}

	path := reaplog.FilePath(ctx.root)
	log, err := reaplog.Load(path)
	if err != nil {
		return err
	}
	run := log.Last()
	closed := run.Closed()
	if len(closed) == 0 {
		Print("nothing to undo — no applied sweep is recorded")
		return nil
	}

	var failures []*reapFailure
	restored := 0
	for _, card := range closed {
		if err := writer.SetStateRaw(card.Issue, card.PriorState); err != nil {
			failures = append(failures, &reapFailure{key: card.Issue, err: err})
			continue
		}
		if card.DroppedLink != "" {
			restoreDroppedLink(ctx, card)
		}
		restored++
	}

	if err := ctx.board.Save(ctx.boardPath); err != nil {
		return err
	}

	Printf("restored %d card(s)", restored)
	if len(failures) == 0 {
		Print("")
		return nil
	}
	Printf(", %d failed:\n", len(failures))
	for _, f := range failures {
		fmt.Fprintf(os.Stderr, "  %s\n", f.Error())
	}
	return fmt.Errorf("%d of %d card(s) could not be restored", len(failures), len(closed))
}

// restoreDroppedLink puts back the board.json entry the sweep deleted. Only the
// lanes that drop one record a DroppedLink, so the class switch has exactly the
// arms lanesDroppingTheirLink names.
func restoreDroppedLink(ctx *boardCtx, card reaplog.Card) {
	if card.Class == "Backlog" {
		ctx.board.SetBacklog(card.DroppedLink, card.Issue)
	}
}
