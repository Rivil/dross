// Package reaplog manages .dross/reap-log.json — the ledger `dross issue reap
// --apply` writes so a sweep is reversible.
//
// It records, per closed card, the state the card held BEFORE the sweep touched
// it. That is the whole reason the package exists: a board's own history cannot
// answer "which column was this in yesterday" on every tracker, and a reap that
// cannot be undone is a reap nobody will run against ninety live cards.
//
// It is a standalone package rather than a map on board.Board deliberately.
// board's package doc commits to holding nothing but the cross-references, and
// a new map field there would register as a mirror namespace with the guard
// that demands every namespace have a reap path — which this ledger is not.
//
// The file is machine-local and gitignored, for the reason state.json and
// local.toml are: it records one machine's writes to one tracker. Tracked, it
// would dirty the tree on every apply, ride ship's clean-tree auto-commit into
// the phase branch, and drag a stale undo target through the squash into every
// later tree.
package reaplog

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// File is the canonical filename inside .dross/.
const File = "reap-log.json"

// FilePath is .dross/reap-log.json. root is the .dross directory.
func FilePath(root string) string { return filepath.Join(root, File) }

// The outcomes a card entry can carry. A card is written to the ledger only
// once its fate is known, so there is no pending value.
const (
	// OutcomeClosed — the close was written AND verified on the read-back.
	OutcomeClosed = "closed"
	// OutcomeFailed — the write or its verification failed. Recorded so the
	// run's report can name it, never replayed by undo: restoring a card the
	// sweep never moved would write a state nobody asked for.
	OutcomeFailed = "failed"
)

// Card is one card the sweep touched, with everything undo needs to put it
// back.
type Card struct {
	// Issue is the tracker's readable id — the same id space board.json keys
	// its links by, so one ledger serves every backend.
	Issue string `json:"issue"`
	// Class is the mirror lane, named by the board.Board map field that
	// records it. Undo needs it to know which namespace a dropped link belongs
	// to.
	Class string `json:"class"`
	// PriorState is the tracker's OWN state name — the column the card sat in
	// before the sweep. Not a dross lifecycle status: undo writes it back
	// verbatim, and mapping it through a lifecycle status would lose the
	// distinction between the columns that share one.
	PriorState string `json:"prior_state"`
	// PriorResolved is the tracker's own done verdict at the same moment.
	PriorResolved bool `json:"prior_resolved"`
	// PriorLabels is the card's full label set before the sweep.
	PriorLabels []string `json:"prior_labels,omitempty"`
	// DroppedLink is the board.json key the sweep deleted for this card,
	// empty when the lane's forward path keeps its link. Recorded rather than
	// re-derived: by undo time the live board.json no longer holds it.
	DroppedLink string `json:"dropped_link,omitempty"`
	// Outcome is OutcomeClosed or OutcomeFailed.
	Outcome string `json:"outcome"`
}

// Run is one `--apply` invocation.
type Run struct {
	StartedAt time.Time `json:"started_at"`
	Cards     []Card    `json:"cards"`
}

// Closed returns only the cards the run actually closed.
//
// It is what undo iterates, and the filter is the point: a card whose close
// failed was never moved, so "restoring" it would write a state the sweep is
// not responsible for.
func (r *Run) Closed() []Card {
	if r == nil {
		return nil
	}
	var out []Card
	for _, c := range r.Cards {
		if c.Outcome == OutcomeClosed {
			out = append(out, c)
		}
	}
	return out
}

// Log is the on-disk ledger: every applied run, oldest first.
type Log struct {
	Runs []Run `json:"runs"`
}

// Load reads the ledger. A missing file is an empty log, not an error — a
// first `--undo` on a repo that has never applied should say there is nothing
// to undo, not fail.
func Load(path string) (*Log, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return &Log{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var l Log
	if err := json.Unmarshal(b, &l); err != nil {
		return nil, fmt.Errorf("unmarshal reap log: %w", err)
	}
	return &l, nil
}

// Append adds a run to the end of the ledger.
func (l *Log) Append(r Run) { l.Runs = append(l.Runs, r) }

// Last returns the most recent run, or nil when the ledger is empty. It is the
// undo target: the last run is the only one anyone realistically wants back,
// which is the locked undo_shape decision.
func (l *Log) Last() *Run {
	if l == nil || len(l.Runs) == 0 {
		return nil
	}
	return &l.Runs[len(l.Runs)-1]
}

// Save writes the ledger, creating .dross/ if needed.
func (l *Log) Save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal reap log: %w", err)
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}
