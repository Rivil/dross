package diag

import (
	"fmt"
	"strings"
)

// Reach is what the owning repo could say about a pinned commit: whether some
// refs/remotes/origin/* ref contains it. internal/cmd classifies it with git;
// this package only renders the verdict.
type Reach string

const (
	// Reachable: some refs/remotes/origin/* ref contains the commit.
	Reachable Reach = "reachable"
	// Unreachable: the repo can see origin's refs and none contains it.
	Unreachable Reach = "unreachable"
	// Indeterminate: this repo cannot answer the question — a shallow clone,
	// or no origin refs to judge against.
	Indeterminate Reach = "cannot-determine"
)

// RedProofPin is one classified pin — the record's phase, SHA and doc, plus
// everything cmd read on its behalf: the reachability verdict (or the error
// that stopped it), the SHA the doc itself carries (or the error reading it)
// and the repair hint doctor names for an unreachable pin.
type RedProofPin struct {
	Phase string
	SHA   string
	// Doc is the replay doc's repo-relative path, for the message only.
	Doc string

	Reach    Reach
	Why      string
	ReachErr error

	DocSHA string
	DocErr error

	RepointHint string
}

// RedProof renders every pin. readErr is the discovery failure, when the
// record could not be read at all: one Issue line, every pin dropped.
// Discovery refuses the whole record when a doc escapes the repo — a corrupt
// artifact stops the run — so an escaping doc suppresses the other pins'
// verdicts. That is the accepted cost of the hard lane, pinned by a test rather
// than left as prose. The wording never says "which cannot be read": that is
// the per-pin unreadable-doc arm's diagnosis, and an operator has to be able
// to tell a corrupt path from a missing file.
//
// present is false when the repo records no pins, so projects without red
// proofs get no section.
func RedProof(pins []RedProofPin, readErr error) ([]Line, bool) {
	if readErr != nil {
		return []Line{issue(fmt.Sprintf("red-proof pins could not be read: %v", readErr))}, true
	}
	if len(pins) == 0 {
		return nil, false
	}
	var lines []Line
	for _, pin := range pins {
		lines = append(lines, PinLines(pin)...)
	}
	return lines, true
}

// PinLines is the per-pin verdict. A pin earns its ✓ only by being reachable
// AND agreeing with its doc: the record staying sound while the prose names a
// different commit still sends the next reader to the wrong place.
func PinLines(pin RedProofPin) []Line {
	if pin.ReachErr != nil {
		return []Line{issue(fmt.Sprintf("%s: cannot check the pin in %s: %v", pin.Phase, pin.Doc, pin.ReachErr))}
	}

	var lines []Line
	switch pin.Reach {
	case Unreachable:
		lines = append(lines, issue(fmt.Sprintf(
			"%s: %s pins %s, which is unreachable — %s. Fix: %s",
			pin.Phase, pin.Doc, pin.SHA, pin.Why, pin.RepointHint)))
	case Indeterminate:
		lines = append(lines, warn(fmt.Sprintf(
			"%s: cannot determine whether %s (pinned by %s) is reachable — %s",
			pin.Phase, Short(pin.SHA), pin.Doc, pin.Why)))
	}

	// The doc cross-check runs whatever the verdict: it is a separate claim
	// about a separate artefact, and a shallow clone can still read a file.
	switch {
	case pin.DocErr != nil:
		lines = append(lines, issue(fmt.Sprintf(
			"%s: pins %s as its replay doc, which cannot be read: %v", pin.Phase, pin.Doc, pin.DocErr)))
	case pin.DocSHA == "":
		lines = append(lines, issue(fmt.Sprintf(
			"%s: %s carries no `base commit:` line, so nothing cross-checks the recorded %s",
			pin.Phase, pin.Doc, Short(pin.SHA))))
	case !SameCommitSHA(pin.DocSHA, pin.SHA):
		lines = append(lines, issue(fmt.Sprintf(
			"%s: %s says base commit %s but the record pins %s — the prose and the record disagree",
			pin.Phase, pin.Doc, pin.DocSHA, pin.SHA)))
	}

	if len(lines) == 0 {
		lines = append(lines, ok(fmt.Sprintf(
			"%s: %s pins %s, %s", pin.Phase, pin.Doc, Short(pin.SHA), pin.Why)))
	}
	return lines
}

// SameCommitSHA compares a doc's pin against a record's. Either side may be
// abbreviated — the doc is written by hand for a human reader — so containment
// counts, and an empty operand never matches.
func SameCommitSHA(a, b string) bool {
	a, b = strings.ToLower(strings.TrimSpace(a)), strings.ToLower(strings.TrimSpace(b))
	if a == "" || b == "" {
		return false
	}
	return strings.HasPrefix(a, b) || strings.HasPrefix(b, a)
}

// Short abbreviates a SHA to seven characters for a message.
func Short(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}
