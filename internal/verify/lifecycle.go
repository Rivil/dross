package verify

import "fmt"

// The four lifecycle states. Every survivor a run reports carries exactly one
// of them (c-1) — a survivor with no state is reported as unclassified rather
// than blending into the list, which is how a standing backlog used to hide in
// plain sight.
const (
	// LifecycleInDiff — the survivor sits in a file this phase touched. It is
	// this phase's to answer for.
	LifecycleInDiff = "in-diff"
	// LifecycleRouted — a deferred item carries this survivor's key and a
	// destination. Debt with a home stays visible, labelled by where it went.
	LifecycleRouted = "routed"
	// LifecycleAccepted — recorded in .dross/survivors.toml with a reason.
	// The only state that earns silence.
	LifecycleAccepted = "accepted"
	// LifecycleUnclassified — no state applies: out of the diff, unrouted,
	// unaccepted, or its identity could not be resolved. The state that says
	// "this needs a decision", so it can never be quietly inherited.
	LifecycleUnclassified = "unclassified"
)

// Identifier resolves a survivor's cross-run identity from its position. The
// classifier takes it as an interface rather than calling internal/survivor
// directly, so classification stays pure and fully fakeable — the resolver
// reads the working tree, and a classifier that needed one would be untestable
// without a repo on disk.
type Identifier interface {
	// Identify returns the survivor's key and whether that key is ambiguous
	// within its file. An error means the subject could not be resolved.
	Identify(file string, line int, op string) (key string, ambiguous bool, err error)
}

// Survivor is the classifier's input: one surviving mutant, in or out of the
// phase's scope.
type Survivor struct {
	File       string
	Line       int
	Op         string
	Language   string
	OutOfScope bool
}

// Classified is one survivor with its resolved identity and its single state.
type Classified struct {
	Survivor
	Key   string
	State string
	// Target is the destination slug of a routed survivor; empty in every
	// other state. It also rides inside Note, which is what carries the
	// destination onto the wire — a routed record with no destination is
	// bucketed-but-homeless, which routed_state_source rules out.
	Target string
	// Note explains a state that needs explaining: the destination of a routed
	// survivor, the ambiguity that stopped an acceptance from suppressing it,
	// or the resolution error that left it unclassified. Empty for a plain
	// in-diff survivor, which needs no explanation.
	Note string
}

// Lifecycle is the classification of a run's survivors into the four states.
// Each survivor appears in exactly one bucket.
type Lifecycle struct {
	InDiff       []Classified
	Routed       []Classified
	Accepted     []Classified
	Unclassified []Classified
}

// Total is the number of classified survivors across all four states. It must
// equal the number of distinct survivors handed to Classify — a survivor that
// is double-listed or silently dropped is exactly the failure c-1 rules out.
func (l *Lifecycle) Total() int {
	return len(l.InDiff) + len(l.Routed) + len(l.Accepted) + len(l.Unclassified)
}

// All returns every classified survivor in a stable state order.
func (l *Lifecycle) All() []Classified {
	out := make([]Classified, 0, l.Total())
	out = append(out, l.InDiff...)
	out = append(out, l.Routed...)
	out = append(out, l.Accepted...)
	out = append(out, l.Unclassified...)
	return out
}

// Classify assigns each survivor exactly one lifecycle state.
//
// accepted maps a survivor key to the reason it was accepted; routed maps a key
// to the phase slug it was routed to. Both are opaque key→string maps so the
// classifier depends on neither the acceptance store nor the deferred
// machinery.
//
// Precedence is accepted > routed > in-diff > unclassified: an acceptance is a
// decision already made, so a survivor that is both accepted and routed is
// accepted, once, rather than counted twice.
//
// Two rules deliberately fail towards noise rather than silence:
//   - an ambiguous key never suppresses. A lapsed acceptance is noise; a false
//     suppression hides a real bug (the locked survivor_identity decision).
//   - an unresolvable subject is unclassified with the error in its note, never
//     assumed to be in-diff and never dropped.
//
// Duplicate rows for one file/line/op collapse to a single record, so one
// acceptance suppresses the survivor rather than one of its copies.
func Classify(survivors []Survivor, accepted, routed map[string]string, id Identifier) *Lifecycle {
	l := &Lifecycle{}
	seen := make(map[Survivor]bool, len(survivors))
	for _, s := range survivors {
		if seen[s] {
			continue
		}
		seen[s] = true
		l.add(classifyOne(s, accepted, routed, id))
	}
	return l
}

func classifyOne(s Survivor, accepted, routed map[string]string, id Identifier) Classified {
	c := Classified{Survivor: s}

	key, ambiguous, err := id.Identify(s.File, s.Line, s.Op)
	if err != nil {
		c.State = LifecycleUnclassified
		c.Note = fmt.Sprintf("identity unresolved: %v", err)
		return c
	}
	c.Key = key

	if reason, ok := accepted[key]; ok {
		if ambiguous {
			// The acceptance is real but the key it matches is not unique in
			// the file, so it cannot be attributed to THIS survivor. Re-emit.
			c.Note = "accepted key is ambiguous in this file (identical source text on more than one line) — acceptance withheld"
		} else {
			c.State = LifecycleAccepted
			c.Note = reason
			return c
		}
	}
	if target, ok := routed[key]; ok {
		c.State = LifecycleRouted
		c.Target = target
		c.Note = joinNote(c.Note, "routed to "+target)
		return c
	}
	if s.OutOfScope {
		c.State = LifecycleUnclassified
		c.Note = joinNote(c.Note, "out of this phase's diff with no acceptance and no destination")
		return c
	}
	c.State = LifecycleInDiff
	return c
}

func joinNote(existing, add string) string {
	if existing == "" {
		return add
	}
	return existing + "; " + add
}

func (l *Lifecycle) add(c Classified) {
	switch c.State {
	case LifecycleAccepted:
		l.Accepted = append(l.Accepted, c)
	case LifecycleRouted:
		l.Routed = append(l.Routed, c)
	case LifecycleInDiff:
		l.InDiff = append(l.InDiff, c)
	default:
		l.Unclassified = append(l.Unclassified, c)
	}
}

// ApplyLifecycle classifies every survivor in a run — in-scope and out-of-scope
// alike — and stamps the resolved key, state and note back onto the records
// that reach tests.json. It returns the classification so the caller can print
// the per-state counts.
//
// The in-scope aggregates are untouched: an acceptance suppresses a *finding*,
// never a *count*. Folding acceptances out of the denominator would let a phase
// improve its mutation score by declaring its survivors acceptable, which is
// the one thing an acceptance must never buy.
func ApplyLifecycle(t *Tests, accepted, routed map[string]string, id Identifier) *Lifecycle {
	if t == nil {
		return &Lifecycle{}
	}
	var in []Survivor
	for _, lr := range t.Languages {
		if lr.Mutation == nil {
			continue
		}
		for _, m := range lr.Mutation.Surviving {
			in = append(in, Survivor{File: m.File, Line: m.Line, Op: m.Op, Language: lr.Name})
		}
	}
	for _, o := range t.OutOfScope {
		in = append(in, Survivor{File: o.File, Line: o.Line, Op: o.Op, Language: o.Language, OutOfScope: true})
	}

	l := Classify(in, accepted, routed, id)

	byPos := make(map[string]Classified, l.Total())
	for _, c := range l.All() {
		byPos[posKey(c.File, c.Line, c.Op)] = c
	}
	for _, lr := range t.Languages {
		if lr.Mutation == nil {
			continue
		}
		for i := range lr.Mutation.Surviving {
			m := &lr.Mutation.Surviving[i]
			if c, ok := byPos[posKey(m.File, m.Line, m.Op)]; ok {
				m.Key, m.Lifecycle, m.Note = c.Key, c.State, c.Note
			}
		}
	}
	for i := range t.OutOfScope {
		o := &t.OutOfScope[i]
		if c, ok := byPos[posKey(o.File, o.Line, o.Op)]; ok {
			o.Key, o.Lifecycle, o.Note = c.Key, c.State, c.Note
		}
	}
	return l
}

func posKey(file string, line int, op string) string {
	return fmt.Sprintf("%s\x00%d\x00%s", file, line, op)
}

// staticIdentifier is the no-op resolver: it hands back an empty key with no
// error. Used when identity resolution is unavailable, so classification still
// runs and every survivor still lands in a state.
type staticIdentifier struct{}

func (staticIdentifier) Identify(string, int, string) (string, bool, error) { return "", false, nil }

// NoIdentity is an Identifier that resolves nothing. With it, no acceptance and
// no route can match, so every survivor falls through to in-diff or
// unclassified — the honest answer when identity cannot be resolved at all.
var NoIdentity Identifier = staticIdentifier{}
