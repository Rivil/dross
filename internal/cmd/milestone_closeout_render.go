package cmd

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"sort"
	"strings"

	"github.com/Rivil/dross/internal/milestone"
)

// closeoutApplied is an acknowledgement whose item is still produced: it
// carries the item past `milestone complete` and into the PR body.
type closeoutApplied struct {
	Item closeoutItem
	Ack  milestone.CloseoutAck
}

// closeoutJoined is a close-out report joined with the milestone's acks.
type closeoutJoined struct {
	// Open are the produced items no ack covers.
	Open []closeoutItem
	// Errors are collector failures: blocking and never ackable.
	Errors []closeoutError
	// Applied are acks whose item is still produced.
	Applied []closeoutApplied
	// Stale are acks whose item is no longer produced (c-11). They are
	// listed, never counted as blocking, and left out of the PR body.
	Stale []milestone.CloseoutAck
}

// Blocking counts what stands between the milestone and its PR: every
// unacknowledged item plus every collector error.
func (j closeoutJoined) Blocking() int {
	return len(j.Open) + len(j.Errors)
}

// joinCloseoutAcks partitions acks against the produced items. An item with
// no ack id (an unverified phase) is never cleared by an ack, so an ack
// phase:x for an x that is only unverified matches nothing and reads stale.
func joinCloseoutAcks(r closeoutReport, acks []milestone.CloseoutAck) closeoutJoined {
	byItem := make(map[string]milestone.CloseoutAck, len(acks))
	for _, a := range acks {
		byItem[a.Item] = a
	}
	used := map[string]bool{}
	j := closeoutJoined{Errors: r.Errors}
	for _, it := range r.Items {
		if it.ID != "" {
			if a, ok := byItem[it.ID]; ok {
				j.Applied = append(j.Applied, closeoutApplied{Item: it, Ack: a})
				used[it.ID] = true
				continue
			}
		}
		j.Open = append(j.Open, it)
	}
	for _, a := range acks {
		if !used[a.Item] {
			j.Stale = append(j.Stale, a)
		}
	}
	sortCloseoutItems(j.Open)
	sort.SliceStable(j.Applied, func(a, b int) bool { return closeoutLess(j.Applied[a].Item, j.Applied[b].Item) })
	sort.SliceStable(j.Stale, func(a, b int) bool { return j.Stale[a].Item < j.Stale[b].Item })
	return j
}

// loadCloseout collects a milestone's close-out items and joins them with its
// acks. No milestone record has neither items nor acks (c-8).
func loadCloseout(root, version string) (closeoutJoined, error) {
	r, err := milestoneCloseout(root, version)
	if err != nil {
		return closeoutJoined{}, err
	}
	var acks []milestone.CloseoutAck
	path := milestone.FilePath(root, version)
	if _, err := os.Stat(path); err == nil {
		m, err := milestone.Load(path)
		if err != nil {
			return closeoutJoined{}, err
		}
		acks = m.Closeout
	} else if !errors.Is(err, fs.ErrNotExist) {
		return closeoutJoined{}, err
	}
	return joinCloseoutAcks(r, acks), nil
}

var closeoutKindTitle = map[string]string{
	closeoutUnverified: "unverified phases",
	closeoutNotDone:    "phases not done",
	closeoutDeferred:   "unabsorbed deferred items",
	closeoutSurvivor:   "undisposed survivors",
}

func closeoutKindRank(kind string) int {
	for i, k := range closeoutKinds {
		if k == kind {
			return i
		}
	}
	return len(closeoutKinds)
}

func closeoutLess(a, b closeoutItem) bool {
	if ra, rb := closeoutKindRank(a.Kind), closeoutKindRank(b.Kind); ra != rb {
		return ra < rb
	}
	if a.Subject != b.Subject {
		return a.Subject < b.Subject
	}
	return a.ID < b.ID
}

func sortCloseoutItems(items []closeoutItem) {
	sort.SliceStable(items, func(a, b int) bool { return closeoutLess(items[a], items[b]) })
}

// renderCloseout prints the blocking enumeration: one heading per kind with
// anything unresolved, in fixed order, each line naming the command that
// clears it; then acknowledged items with their reasons and stale acks under
// their own heading. It prints no wall-clock value, so two renders of the same
// tree are byte-identical.
func renderCloseout(j closeoutJoined) string {
	var b strings.Builder
	if j.Blocking() == 0 {
		b.WriteString("close-out: nothing unresolved\n")
	} else {
		fmt.Fprintf(&b, "close-out: %d unresolved\n", j.Blocking())
	}
	for _, kind := range closeoutKinds {
		var items []closeoutItem
		for _, it := range j.Open {
			if it.Kind == kind {
				items = append(items, it)
			}
		}
		var errs []closeoutError
		for _, e := range j.Errors {
			if e.Kind == kind {
				errs = append(errs, e)
			}
		}
		if len(items) == 0 && len(errs) == 0 {
			continue
		}
		fmt.Fprintf(&b, "\n%s (%d):\n", closeoutKindTitle[kind], len(items)+len(errs))
		for _, it := range items {
			label := it.Subject
			if it.ID != "" {
				label = it.ID
			}
			fmt.Fprintf(&b, "  %s — %s\n    clear: %s\n", label, it.Detail, it.Clear)
		}
		for _, e := range errs {
			fmt.Fprintf(&b, "  ERROR — %v\n    clear: fix the unreadable file, then re-run `dross milestone complete --check`\n", e.Err)
		}
	}
	if len(j.Applied) > 0 {
		fmt.Fprintf(&b, "\nacknowledged (%d):\n", len(j.Applied))
		for _, a := range j.Applied {
			fmt.Fprintf(&b, "  %s — %s (%s)\n", a.Ack.Item, a.Ack.Reason, a.Item.Detail)
		}
	}
	if len(j.Stale) > 0 {
		fmt.Fprintf(&b, "\nstale acknowledgements (%d) — item no longer unresolved, not blocking:\n", len(j.Stale))
		for _, a := range j.Stale {
			fmt.Fprintf(&b, "  %s — %s\n", a.Item, a.Reason)
		}
	}
	return b.String()
}
