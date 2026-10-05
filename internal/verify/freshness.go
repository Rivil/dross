package verify

import (
	"fmt"
	"regexp"
	"sort"
)

// Freshness is how a recorded verdict stands against the tree it would ship.
type Freshness int

const (
	// FreshnessUnknown: the verdict records no tree — written before trees
	// were recorded, or hand-edited without one and with no tests.json copy
	// to fall back on. Ship warns and proceeds (locked legacy_freshness).
	FreshnessUnknown Freshness = iota
	// Fresh: the tree the run measured is the tree being shipped.
	Fresh
	// Stale: a file outside the exemptions changed, appeared or went away
	// since the run.
	Stale
	// Malformed: a recorded id is not an object id, so it is never handed
	// to git.
	Malformed
)

// TreeRef names one measured tree: the HEAD it was taken at and its
// fingerprint.
type TreeRef struct {
	Commit string
	Tree   string
}

// FreshnessReport is ClassifyFreshness's verdict on one recorded tree.
type FreshnessReport struct {
	State Freshness
	// Recorded is the tree the verdict was measured at, and where it was
	// read from ("verify.toml" or "tests.json"). Empty when unknown.
	Recorded TreeRef
	Source   string
	// Changed lists, sorted, the paths that differ between the recorded tree
	// and the current one. Empty when stale but unlistable.
	Changed []string
	// ListErr is why a stale verdict's changed paths could not be listed —
	// the recorded tree was pruned, or never reached this clone. Stale all
	// the same: a tree that cannot be compared is not the tree measured.
	ListErr error
	// Field names the malformed field.
	Field string
}

// TreeDiffer lists the paths whose entries differ between two trees. An
// interface rather than a func value so the exec-consent audit's call graph
// resolves the one implementation that reaches git, instead of every func of
// this shape in the module.
type TreeDiffer interface {
	Diff(a, b string) ([]string, error)
}

// objectID is a git object id: SHA-1 or SHA-256, lowercase hex.
var objectID = regexp.MustCompile(`^[0-9a-f]{40}([0-9a-f]{24})?$`)

// ClassifyFreshness compares the tree a verdict was measured at with current,
// the tree that would ship now. diff is asked only for a stale verdict with
// well-formed ids.
//
// The recorded tree is verify.toml's. When a hand edit dropped it, tests.json's
// copy stands in — but only when both files carry the same generated_at, so a
// tests.json from another run never vouches for this verdict. With neither,
// freshness is unknown and git is never asked.
func ClassifyFreshness(v *Verify, t *Tests, current TreeRef, diff TreeDiffer) FreshnessReport {
	var rec TreeRef
	var source string
	switch {
	case v != nil && v.Verify.MeasuredTree != "":
		rec, source = TreeRef{Commit: v.Verify.MeasuredCommit, Tree: v.Verify.MeasuredTree}, VerifyFile
	case v != nil && t != nil && t.MeasuredTree != "" && t.GeneratedAt.Equal(v.Verify.GeneratedAt):
		rec, source = TreeRef{Commit: t.MeasuredCommit, Tree: t.MeasuredTree}, TestsFile
	default:
		return FreshnessReport{State: FreshnessUnknown}
	}
	r := FreshnessReport{Recorded: rec, Source: source}
	if !objectID.MatchString(rec.Tree) {
		r.State, r.Field = Malformed, source+" measured_tree"
		return r
	}
	if rec.Commit != "" && !objectID.MatchString(rec.Commit) {
		r.State, r.Field = Malformed, source+" measured_commit"
		return r
	}
	if rec.Tree == current.Tree {
		r.State = Fresh
		return r
	}
	r.State = Stale
	changed, err := diff.Diff(rec.Tree, current.Tree)
	if err != nil {
		r.ListErr = fmt.Errorf("list the files changed since %s: %w", rec.Tree, err)
		return r
	}
	r.Changed = append([]string(nil), changed...)
	sort.Strings(r.Changed)
	return r
}
