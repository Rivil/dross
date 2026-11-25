package phase

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

// This file is the single rule set for a decision's reversibility tag and its
// one-way confirmation. `dross validate`, the plan-lock gate, `dross phase
// confirm` and both renderers (the ship PR body and the task-reviewer
// context) judge a decision through these functions, so no two surfaces can
// disagree about whether a decision is tagged, one-way or confirmed.

// DoorTags is the closed set of reversibility tags, in the order messages list
// them. Any other value — a different case, an underscore, stray whitespace —
// is unknown.
var DoorTags = []string{DoorReversible, DoorOneWay}

func isDoorTag(v string) bool {
	for _, t := range DoorTags {
		if t == v {
			return true
		}
	}
	return false
}

// DecisionHash is the confirmation record for a decision: "sha256:" and the
// full hex digest of key, a NUL separator and choice. The separator keeps
// ("ab","c") and ("a","bc") apart; the phase id is deliberately not an input,
// so renaming a phase keeps its confirmations.
func DecisionHash(key, choice string) string {
	sum := sha256.Sum256([]byte(key + "\x00" + choice))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// confirmed reports whether d holds a confirmation matching its current key
// and choice.
func confirmed(d Decision) bool {
	return d.Confirmed != "" && d.Confirmed == DecisionHash(d.Key, d.Choice)
}

// Reversibility problem kinds.
const (
	ReversibilityMissing = "missing"
	ReversibilityUnknown = "unknown"
)

// ReversibilityProblem is one decision whose reversibility tag is missing or
// not one of DoorTags. Index is the decision's position in the full
// spec.Decisions, so a caller that filters the list (to Locked, say) still
// names the right decisions[i].
type ReversibilityProblem struct {
	Index  int
	Key    string
	Locked bool
	Kind   string // ReversibilityMissing or ReversibilityUnknown
	Value  string // the offending value; empty for a missing tag
}

// String renders the problem as one line naming `decisions[i] (<key>)`.
func (p ReversibilityProblem) String() string {
	want := strings.Join(DoorTags, ", ")
	if p.Kind == ReversibilityMissing {
		return fmt.Sprintf("decisions[%d] (%s): reversibility missing — want one of %s", p.Index, p.Key, want)
	}
	return fmt.Sprintf("decisions[%d] (%s): unknown reversibility %q — want one of %s", p.Index, p.Key, p.Value, want)
}

// ReversibilityProblems returns the decisions of spec whose tag is wrong, in
// decision order. An unknown value is reported on every decision, locked or
// not. A missing tag is reported only on a locked decision and only when
// requireTags is set: a phase already complete predates the tag, and
// backfilling it would stamp reversibility claims nobody made. A spec with no
// problems returns nil.
func ReversibilityProblems(spec *Spec, requireTags bool) []ReversibilityProblem {
	if spec == nil {
		return nil
	}
	var out []ReversibilityProblem
	for i, d := range spec.Decisions {
		p := ReversibilityProblem{Index: i, Key: d.Key, Locked: d.Locked, Value: d.Reversibility}
		switch {
		case d.Reversibility == "":
			if !d.Locked || !requireTags {
				continue
			}
			p.Kind = ReversibilityMissing
		case !isDoorTag(d.Reversibility):
			p.Kind = ReversibilityUnknown
		default:
			continue
		}
		out = append(out, p)
	}
	return out
}

// Unconfirmed is a locked one-way decision with no confirmation matching its
// current key and choice. Stale is set when a confirmation is recorded but no
// longer matches — the choice was edited after the user confirmed it, or the
// hash was copied from another decision.
type Unconfirmed struct {
	Key   string
	Stale bool
}

// UnconfirmedOneWay returns the locked one-way decisions of spec that the
// plan-lock gate refuses, in decision order. Reversible, untagged and
// unlocked decisions are never listed: only a locked decision is a door. A
// spec with none returns nil.
func UnconfirmedOneWay(spec *Spec) []Unconfirmed {
	if spec == nil {
		return nil
	}
	var out []Unconfirmed
	for _, d := range spec.Decisions {
		if !d.Locked || d.Reversibility != DoorOneWay || confirmed(d) {
			continue
		}
		out = append(out, Unconfirmed{Key: d.Key, Stale: d.Confirmed != ""})
	}
	return out
}

// DoorLabel is how a renderer marks d: "one-way, unconfirmed" for a locked
// one-way decision the gate would refuse, "one-way" for any other one-way
// decision (an unlocked one cannot be confirmed, so it is never called
// unconfirmed), "reversible", and "" for an untagged legacy decision — or an
// unknown tag, which ReversibilityProblems reports instead.
func DoorLabel(d Decision) string {
	switch d.Reversibility {
	case DoorOneWay:
		if d.Locked && !confirmed(d) {
			return "one-way, unconfirmed"
		}
		return "one-way"
	case DoorReversible:
		return "reversible"
	default:
		return ""
	}
}
