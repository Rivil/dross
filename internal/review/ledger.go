package review

// Outcome is what one review round recorded.
type Outcome string

const (
	// OutcomePass: the reviewer passed the context it was handed, bound to
	// the tree that context was built from.
	OutcomePass Outcome = "pass"
	// OutcomeBlock: the verdict carried at least one blocking finding.
	OutcomeBlock Outcome = "block"
	// OutcomeUnavailable: the reviewer ran but its verdict could not be
	// trusted (unparseable, self-contradicting, or its prompt was widened).
	// Sticky: no later round lifts it (review_unavailable).
	OutcomeUnavailable Outcome = "unavailable"
	// OutcomeStale: the reviewer was handed a context whose digest no longer
	// matches the tree. Neither a pass nor a block; it does not count against
	// the fix round.
	OutcomeStale Outcome = "stale"
)

// Round is one recorded review. Spec and Quality stay separate on disk too.
type Round struct {
	Outcome Outcome   `json:"outcome"`
	Tree    string    `json:"tree,omitempty"`
	Digest  string    `json:"digest,omitempty"`
	Spec    []Finding `json:"spec,omitempty"`
	Quality []Finding `json:"quality,omitempty"`
	Cause   string    `json:"cause,omitempty"`
}

// Status is where a ledger of rounds stands.
type Status string

const (
	StatusNone        Status = "none"
	StatusPass        Status = "pass"
	StatusBlocked     Status = "blocked"
	StatusExhausted   Status = "exhausted"
	StatusUnavailable Status = "unavailable"
)

// State is a ledger's standing. Tree is set for a pass — the gate compares it
// with the commit candidate. Cause is set for unavailable and exhausted.
type State struct {
	Status Status
	Tree   string
	Cause  string
}

// MaxBlocks is the one-fix-round cap (c-3): the first block opens the fix
// round, the second exhausts it.
const MaxBlocks = 2

// StateOf walks the rounds in order. The first terminal event wins and sticks:
// an unavailable round, or the second block. Until then the latest pass or
// block stands; stale rounds change nothing and count for nothing.
func StateOf(rounds []Round) State {
	st := State{Status: StatusNone}
	blocks := 0
	for _, r := range rounds {
		switch r.Outcome {
		case OutcomeUnavailable:
			return State{Status: StatusUnavailable, Cause: r.Cause}
		case OutcomeBlock:
			blocks++
			if blocks >= MaxBlocks {
				return State{Status: StatusExhausted, Cause: "still blocked after the one fix round"}
			}
			st = State{Status: StatusBlocked}
		case OutcomePass:
			st = State{Status: StatusPass, Tree: r.Tree}
		}
	}
	return st
}

// Resolution says what became of a finding (c-6).
type Resolution string

const (
	ResolvedFixed      Resolution = "fixed in the fix round"
	ResolvedLeft       Resolution = "non-blocking, left"
	ResolvedUnresolved Resolution = "unresolved — task failed"
)

// FindingKind separates spec-compliance from code-quality findings.
type FindingKind string

const (
	KindSpec    FindingKind = "spec"
	KindQuality FindingKind = "quality"
)

// Resolved is one finding with the round it came from and its resolution.
type Resolved struct {
	Round      int
	Kind       FindingKind
	Finding    Finding
	Resolution Resolution
}

// Resolve labels every finding of every round. Non-blocking findings are left
// by definition. A blocking finding was fixed in the fix round when the ledger
// ends in a pass; otherwise it is unresolved and the task failed.
func Resolve(rounds []Round) []Resolved {
	fixed := StateOf(rounds).Status == StatusPass
	var out []Resolved
	for i, r := range rounds {
		for _, f := range r.Spec {
			out = append(out, Resolved{Round: i + 1, Kind: KindSpec, Finding: f, Resolution: blockingResolution(fixed)})
		}
		for _, f := range r.Quality {
			res := ResolvedLeft
			if f.Severity == Blocking {
				res = blockingResolution(fixed)
			}
			out = append(out, Resolved{Round: i + 1, Kind: KindQuality, Finding: f, Resolution: res})
		}
	}
	return out
}

func blockingResolution(fixed bool) Resolution {
	if fixed {
		return ResolvedFixed
	}
	return ResolvedUnresolved
}
