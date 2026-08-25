package cmd

import (
	"encoding/json"
	"path/filepath"

	"github.com/Rivil/dross/internal/forge"
	"github.com/Rivil/dross/internal/watch"
	"github.com/spf13/cobra"
)

// watchDigest is the read-only snapshot `dross watch` emits: board issues split
// into new-since-last-tick vs carried, the drifting phases, and the single
// suggested next command. board_ok is false when the board was off, unreachable,
// or misconfigured (the digest is drift-only in that case).
type watchDigest struct {
	New       []watch.Item       `json:"new"`
	Current   []watch.Item       `json:"current"`
	Drift     []watch.PhaseDrift `json:"drift"`
	Suggested string             `json:"suggested_command"`
	BoardOK   bool               `json:"board_ok"`
	// Stranded is the count of board mirrors whose artefact finished but whose
	// card did not — the other half of the locked prompt_edge decision, which
	// keeps the sweep off every prompt's hot path and surfaces the debt as a
	// drift signal instead.
	//
	// omitempty on purpose: zero is not a finding, and a digest printed on a
	// loop should stay quiet about a board that is clean. It is also omitted
	// when the board was not reached, where a zero would read as "clean"
	// rather than "unknown".
	Stranded int `json:"stranded,omitempty"`
}

// Watch is the read-only `dross watch` command: it surfaces what changed on the
// board and in the phase spine since the last tick and never mutates anything
// but .dross/watch.state.json. Designed to be run on a `/loop` interval.
func Watch() *cobra.Command {
	var asJSON bool
	c := &cobra.Command{
		Use:   "watch",
		Short: "Read-only digest of board inbound + phase drift since the last tick",
		RunE: func(_ *cobra.Command, _ []string) error {
			dstate, statePath, err := loadState()
			if err != nil {
				return err
			}
			root := filepath.Dir(statePath)

			// Board inbound feed (read-only, mark-free). Board disabled,
			// misconfigured, or unreachable all degrade to a drift-only digest —
			// watch must never error out on a loop.
			var (
				feed         []watch.Item
				boardReached bool
				stranded     int
			)
			if ctx, enabled, oerr := openBoard(); oerr == nil && enabled {
				if issues, lerr := collectInbound(ctx, forge.IssueFilter{State: "open"}); lerr == nil {
					boardReached = true
					for _, iss := range issues {
						feed = append(feed, watch.Item{ID: iss.Key, State: iss.State, Title: iss.Title})
					}
				}
				// Read-only, and degrading the same way the feed does: a
				// classify that fails leaves the count at zero and the line
				// unprinted rather than failing the tick. watch runs on a
				// timer; it must never be the thing that breaks.
				if plan, _, cerr := reapInventory(ctx, nil); cerr == nil {
					stranded = len(plan.Cards)
				}
			}

			wPath := watch.FilePath(root)
			wst, err := watch.Load(wPath)
			if err != nil {
				return err
			}
			diff := wst.Diff(feed)
			if diff.New == nil {
				diff.New = []watch.Item{}
			}
			if diff.Current == nil {
				diff.Current = []watch.Item{}
			}

			drift, err := watch.ClassifyDrift(root, dstate)
			if err != nil {
				return err
			}
			if drift == nil {
				drift = []watch.PhaseDrift{}
			}

			digest := watchDigest{
				New:       diff.New,
				Current:   diff.Current,
				Drift:     drift,
				Suggested: suggestedCommand(drift, len(diff.New), reconcilableCount(root)),
				BoardOK:   boardReached,
				Stranded:  stranded,
			}

			// Persist the seen-set ONLY when the board was actually reached, so an
			// off/unreachable tick preserves the prior baseline instead of
			// re-flagging the whole backlog as new on the next healthy tick.
			if boardReached {
				wst.Update(feed)
				if err := wst.Save(wPath); err != nil {
					return err
				}
			}

			if asJSON {
				out, err := json.Marshal(digest)
				if err != nil {
					return err
				}
				Print(string(out))
				return nil
			}
			renderWatchHuman(digest)
			return nil
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "emit the digest as JSON (for /dross-watch)")
	return c
}

// suggestedCommand ranks the single next command per the locked
// suggestion_precedence decision: advance an in-flight phase (verify a
// complete-but-unverified phase, then ship a verified-but-unshipped one) before
// pulling in new board intake, and fall back to status when nothing presses.
// Returns exactly one command, never empty.
func suggestedCommand(drift []watch.PhaseDrift, newCount, reconcilable int) string {
	var hasComplete, hasVerified bool
	for _, d := range drift {
		switch d.Kind {
		case watch.DriftCompleteUnverified:
			hasComplete = true
		case watch.DriftVerifiedUnshipped:
			hasVerified = true
		}
	}
	switch {
	case hasComplete:
		return "/dross-verify"
	case hasVerified:
		return "/dross-ship"
	case reconcilable > 1:
		// Finished work whose branches are still lying around. It outranks new
		// board intake — cleanup that is already earned beats work not started
		// — and it is named as ONE verb rather than leaked as a chore list.
		return "dross phase reconcile"
	case newCount > 0:
		return "/dross-inbox"
	default:
		return "/dross-status"
	}
}

func renderWatchHuman(d watchDigest) {
	Printf("watch — %d new, %d carried, %d phase(s) drifting\n", len(d.New), len(d.Current), len(d.Drift))
	if !d.BoardOK {
		Print("  board: off/unreachable — drift only\n")
	}
	if d.Stranded > 0 {
		Printf("  stranded: %d board mirror(s) whose artefact finished\n", d.Stranded)
	}
	for _, it := range d.New {
		Printf("  new:   %s %s\n", it.ID, it.Title)
	}
	for _, dr := range d.Drift {
		Printf("  drift: %s (%s)\n", dr.Phase, dr.Kind)
	}
	Printf("  next:  %s\n", d.Suggested)
}
