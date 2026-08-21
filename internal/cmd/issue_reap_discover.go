package cmd

import (
	"fmt"
	"sort"
	"strings"

	"github.com/Rivil/dross/internal/forge"
)

// Marker-label discovery: the second source the sweep classifies from.
//
// board.json is not a complete index of what dross has written to a tracker.
// A phase-sync mints its issue on phase/<id>, and `phase complete` deletes the
// board.json entry when that branch is reconciled — so the link dies with the
// branch while the card lives on. Six such cards on this repo's own board are
// in no namespace at all. Sweeping only the links would skip exactly the cards
// most likely to be stranded, since losing the link is itself the thing that
// stops the forward lifecycle closing them.
//
// The recovery path is the one the forward sync already relies on: dross stamps
// each mirror with an identity label, precisely because the tracker keeps a
// label and board.json does not. Discovery reads those labels back.
//
// Discovery gets NO verdict path of its own. It recovers which artefact a card
// belongs to, then hands that artefact to the same record-derived verdict the
// linked cards go through. A second verdict path would be a second place for
// "is this done?" to drift, which is the whole failure this phase exists to
// close.

// orphanKind is which identity label a card was recovered by. It is kept
// distinct from the lane because two kinds map to the same lane and resolve
// through different records: a `dross/target:` card names a destination PHASE,
// a `dross/deferred:` card names an ITEM ID, and both are Backlog.
type orphanKind string

const (
	orphanPhase    orphanKind = "phase"
	orphanTask     orphanKind = "task"
	orphanDeferred orphanKind = "deferred"
	orphanTarget   orphanKind = "target"
	orphanQuick    orphanKind = "quick"
)

// orphanLaneFor maps a recovered kind onto its mirror lane.
var orphanLaneFor = map[orphanKind]string{
	orphanPhase:    "Phases",
	orphanTask:     "Tasks",
	orphanDeferred: "Backlog",
	orphanTarget:   "Backlog",
	orphanQuick:    "Quicks",
}

// identityLabels is the label vocabulary the forward sync stamps, in the
// precedence discovery reads them back. A routed deferred item carries both its
// item id and its destination; the item id is the more specific of the two, so
// it is consulted first.
var identityLabels = []struct {
	prefix string
	kind   orphanKind
}{
	{"dross/task:", orphanTask},
	{"dross/phase:", orphanPhase},
	{"dross/deferred:", orphanDeferred},
	{"dross/target:", orphanTarget},
}

// orphanIdentity recovers which artefact a card mirrors from its own labels.
func orphanIdentity(labels []string) (kind orphanKind, artefact string, ok bool) {
	for _, spec := range identityLabels {
		for _, l := range labels {
			if strings.HasPrefix(l, spec.prefix) {
				return spec.kind, strings.TrimPrefix(l, spec.prefix), true
			}
		}
	}
	for _, l := range labels {
		if l == labelQuick {
			return orphanQuick, "", true
		}
	}
	return "", "", false
}

// discoverReap lists every card carrying the dross marker label, drops the ones
// board.json already accounts for, and classifies the remainder from disk.
//
// It returns the classified orphans and, separately, the cards that carry the
// marker but no identity label anything can be recovered from. Those are
// REPORTED, never closed: dross wrote them, so they are not a human's issue to
// leave alone, but nothing on disk can be shown to speak for them.
func discoverReap(ctx *boardCtx, lanes []reapLane) (found []candidate, unclassifiable []reapCard, err error) {
	byLane := map[string]reapLane{}
	for _, l := range lanes {
		byLane[l.Name] = l
	}

	issues, err := ctx.client.ListIssues(forge.IssueFilter{State: "open", Labels: []string{labelMarker}})
	if err != nil {
		return nil, nil, wrapBoard(err)
	}
	sort.Slice(issues, func(i, j int) bool { return issues[i].Key < issues[j].Key })

	for _, iss := range issues {
		if iss.Key == "" {
			continue
		}
		// The marker is re-checked locally rather than delegated to the query.
		// Backends differ in how they combine label filters, and the property
		// being protected — a human's issue is never touched, never even named
		// — is too important to hold only as long as every backend's filter is
		// exact. A card without the marker is not dross's to reason about.
		if !hasLabel(iss.Labels, labelMarker) {
			continue
		}
		// Deduped against the links here, so a card present in both sources is
		// classified exactly once — by the board.json walk, which knows which
		// namespace recorded it without having to infer it from a label.
		if ctx.board.IsLinked(iss.Key) {
			continue
		}
		kind, artefact, ok := orphanIdentity(iss.Labels)
		if !ok {
			unclassifiable = append(unclassifiable, reapCard{
				Key: iss.Key,
				Why: "carries the dross marker but no identity label — nothing on disk can be shown to speak for it",
			})
			continue
		}
		lane := orphanLaneFor[kind]
		spec, wanted := byLane[lane]
		if !wanted {
			continue // this lane is not in the requested namespace filter
		}
		v, why := orphanVerdict(ctx, kind, artefact)
		if v == reapStillOpen {
			continue
		}
		found = append(found, candidate{
			card:    reapCard{Key: iss.Key, Lane: lane, Terminal: spec.Terminal, Why: why},
			verdict: v,
		})
	}
	return found, unclassifiable, nil
}

// orphanVerdict routes a recovered artefact through the same record-derived
// gates the linked cards use. It owns no evidence of its own.
func orphanVerdict(ctx *boardCtx, kind orphanKind, artefact string) (reapVerdict, string) {
	switch kind {
	case orphanPhase:
		return phaseRecordVerdict(ctx.root, artefact)
	case orphanTask:
		// dross/task:<phase>/<task> — the phase half carries the completion
		// record; a task has none of its own.
		slug, _, ok := strings.Cut(artefact, "/")
		if !ok || slug == "" {
			return reapUnattributable, fmt.Sprintf("task label %q names no phase", artefact)
		}
		return phaseRecordVerdict(ctx.root, slug)
	case orphanTarget:
		// A routed item resolves when its destination phase completed.
		v, why := phaseRecordVerdict(ctx.root, artefact)
		if v == reapStranded {
			return v, fmt.Sprintf("routed to %s; %s", artefact, why)
		}
		return v, why
	case orphanDeferred:
		return reapBacklogVerdictByID(ctx, artefact)
	case orphanQuick:
		return reapUnattributable, "quick has no completion record on disk — close it by hand if it finished"
	}
	return reapUnattributable, fmt.Sprintf("identity kind %q has no record to read", kind)
}

// reapBacklogVerdictByID resolves a deferred item by its stable id and applies
// the same backlog verdict the linked path uses.
func reapBacklogVerdictByID(ctx *boardCtx, id string) (reapVerdict, string) {
	deferred, err := collectDeferred(ctx.root)
	if err != nil {
		return reapUnattributable, fmt.Sprintf("could not read the deferred stores: %v", err)
	}
	byKey := map[string]deferredEntry{}
	for _, d := range deferred {
		if d.ID != "" {
			byKey[deferredBacklogKey(d.ID)] = d
		}
	}
	return reapBacklogVerdict(ctx, deferredBacklogKey(id), byKey)
}

// hasLabel reports whether a label set contains name.
func hasLabel(labels []string, name string) bool {
	for _, l := range labels {
		if l == name {
			return true
		}
	}
	return false
}
