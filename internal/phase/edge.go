package phase

import (
	"fmt"
	"strings"
)

// This file is the single rule set for [[edges]] records. `dross validate`,
// the plan-lock gate in `dross state set` and `dross phase edges` all judge a
// record through EdgeProblems / EdgeStatus, so the three surfaces cannot
// disagree about whether an edge is resolved.

// EdgeClass is one kind of edge case the spec step can probe, with the
// one-line hint it is proposed under.
type EdgeClass struct {
	Name string
	Hint string
}

// EdgeClasses is the closed set of edge classes, in the order the spec lists
// them. A record naming any other class — a typo, a different case, stray
// whitespace — is unresolved.
var EdgeClasses = []EdgeClass{
	{"boundary", "limits and off-by-one: zero, one, the maximum, one past it"},
	{"empty", "nothing there: empty string, nil or empty list, a missing file or key"},
	{"encoding", "bytes vs text: unicode, CRLF, BOM, escaping, odd characters in names"},
	{"ordering", "sequence matters: unsorted input, map iteration, stable output order"},
	{"precision", "numeric loss: rounding, float comparison, overflow, truncation"},
	{"idempotency", "run it twice: a re-run, retry or re-save changes nothing the second time"},
	{"concurrency", "at the same time: parallel runs, races, partial writes, locks"},
	{"adjacency", "the neighbour: a sibling feature, file or caller this change can break"},
}

// Edge outcomes: what became of a proposed class.
const (
	EdgeCovered   = "covered"
	EdgeDismissed = "dismissed"
	EdgeDeferred  = "deferred"
)

// EdgeOutcomes is the closed set of outcomes a record may carry.
var EdgeOutcomes = []string{EdgeCovered, EdgeDismissed, EdgeDeferred}

// edgeClassNames is EdgeClasses' names, for messages.
func edgeClassNames() []string {
	names := make([]string, len(EdgeClasses))
	for i, c := range EdgeClasses {
		names[i] = c.Name
	}
	return names
}

func isEdgeClass(name string) bool {
	for _, c := range EdgeClasses {
		if c.Name == name {
			return true
		}
	}
	return false
}

// EdgeProblems returns one line per unresolved record of spec.Edges, in record
// order, each naming the record as `edges[i] (<class>)` and every defect it
// has. A spec with no records, or only resolved ones, returns nil.
//
// knownDeferred is the PROJECT-WIDE set of [[deferred]] ids — every phase
// spec plus the _project store, as deferred.Collect gathers them — because a
// deferred edge may park its work in another phase. The empty id never
// matches, even when the set holds it: an id-less item cannot be linked to.
func EdgeProblems(spec *Spec, knownDeferred map[string]bool) []string {
	var out []string
	for i, defects := range edgeDefects(spec, knownDeferred) {
		if len(defects) == 0 {
			continue
		}
		out = append(out, fmt.Sprintf("edges[%d] (%s): %s", i, spec.Edges[i].Class, strings.Join(defects, "; ")))
	}
	return out
}

// EdgeStatus partitions EdgeClasses by spec.Edges. A class is resolved when
// every record naming it is resolved, unresolved when any is not, and open
// when no record names it. Each list is in EdgeClasses order and non-nil;
// records naming an unknown class fall in none of them (EdgeProblems names
// those).
func EdgeStatus(spec *Spec, knownDeferred map[string]bool) (resolved, unresolved, open []string) {
	recorded := map[string]bool{}
	bad := map[string]bool{}
	for i, defects := range edgeDefects(spec, knownDeferred) {
		class := spec.Edges[i].Class
		recorded[class] = true
		if len(defects) > 0 {
			bad[class] = true
		}
	}
	resolved, unresolved, open = []string{}, []string{}, []string{}
	for _, c := range EdgeClasses {
		switch {
		case !recorded[c.Name]:
			open = append(open, c.Name)
		case bad[c.Name]:
			unresolved = append(unresolved, c.Name)
		default:
			resolved = append(resolved, c.Name)
		}
	}
	return resolved, unresolved, open
}

// edgeDefects returns, per record of spec.Edges, the defects that leave it
// unresolved — an empty slice for a resolved record.
func edgeDefects(spec *Spec, knownDeferred map[string]bool) [][]string {
	criteria := map[string]bool{}
	for _, c := range spec.Criteria {
		if c.ID != "" {
			criteria[c.ID] = true
		}
	}
	firstAt := map[string]int{}
	out := make([][]string, len(spec.Edges))
	for i, e := range spec.Edges {
		var defects []string
		if !isEdgeClass(e.Class) {
			defects = append(defects, fmt.Sprintf("unknown class %q — want one of %s",
				e.Class, strings.Join(edgeClassNames(), ", ")))
		} else if j, dup := firstAt[e.Class]; dup {
			defects = append(defects, fmt.Sprintf("duplicate class — edges[%d] already records %q", j, e.Class))
		} else {
			firstAt[e.Class] = i
		}
		switch e.Outcome {
		case EdgeCovered:
			if len(e.Criteria) == 0 {
				defects = append(defects, "covered names no criterion")
			}
			var missing []string
			for _, id := range e.Criteria {
				if !criteria[id] {
					missing = append(missing, fmt.Sprintf("%q", id))
				}
			}
			if len(missing) > 0 {
				defects = append(defects, fmt.Sprintf("covered names %s, not in [[criteria]]", strings.Join(missing, ", ")))
			}
		case EdgeDismissed:
			if strings.TrimSpace(e.Reason) == "" {
				defects = append(defects, "dismissed with no reason")
			}
		case EdgeDeferred:
			switch {
			case e.Deferred == "":
				defects = append(defects, "deferred names no [[deferred]] id")
			case !knownDeferred[e.Deferred]:
				defects = append(defects, fmt.Sprintf("deferred names [[deferred]] id %q, which no phase spec or the project store holds", e.Deferred))
			}
		default:
			defects = append(defects, fmt.Sprintf("unknown outcome %q — want one of %s",
				e.Outcome, strings.Join(EdgeOutcomes, ", ")))
		}
		out[i] = defects
	}
	return out
}
