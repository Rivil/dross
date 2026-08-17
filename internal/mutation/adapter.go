// Package mutation runs language-specific mutation testing tools and
// normalises the output. v0 defines the interface and a no-op default.
//
// Adapter plan:
//   - TS/Svelte → Stryker  (npx stryker run --reporters json)
//   - C# .NET   → Stryker.NET
//   - Go        → Gremlins (go install github.com/go-gremlins/gremlins)
//   - GDScript  → no native tool; fallback to gut/gdUnit4 + LLM judge
//   - HTML/CSS  → not applicable; fallback to Playwright snapshot diff
//
// Each adapter consumes a list of source files (the files touched in the
// phase being verified) and produces a Report.
package mutation

import "errors"

// Report is the normalised result format consumed by verify.
type Report struct {
	Tool     string // "stryker" | "gremlins" | ...
	Killed   int    // mutants the tests caught — good
	Survived int    // mutants that escaped — theatrical tests (includes NotCovered)
	Timeout  int
	Errors   int
	// Score is killed / (killed + survived + timeout) — see PooledScore in
	// score.go, which is the one place the formula lives. The comment here
	// used to claim killed/(killed+survived), which no adapter computed.
	Score     float64
	Surviving []Mutant

	// NotCovered is the subset of Survived where tests never executed the
	// mutated line at all (gremlins' "NOT COVERED" status). Tracked
	// separately because high NotCovered + low LIVED usually means a
	// coverage-tool blind spot (e.g. Go's package-init code in top-level
	// `var` arrays) rather than weak assertions — actionable diagnosis
	// the score alone can't surface. Other adapters (Stryker, Stryker.NET)
	// don't report this status and leave the field at zero.
	NotCovered int

	// Files attributes the same counters per source file: every mutant
	// counted in the aggregates above is counted in exactly one row here.
	// That is what lets verify recompute a score over a SUBSET of files —
	// the phase's own change set — without re-running the tool. An adapter
	// that populates the aggregates but not this map silently hands verify
	// a report it can only score whole-package.
	//
	// Not persisted: tests.json carries the filtered aggregates and the
	// out-of-scope survivor list, not the raw per-file table.
	Files map[string]FileStat `json:"-"`
}

// FileStat is one file's slice of a Report — the same per-status counters,
// attributed to the file the mutant landed in.
type FileStat struct {
	Killed     int
	Survived   int
	Timeout    int
	Errors     int
	NotCovered int
}

// plus returns the element-wise sum of two rows. Used both to accumulate
// mutants into a row and to merge per-package reports.
func (f FileStat) plus(o FileStat) FileStat {
	return FileStat{
		Killed:     f.Killed + o.Killed,
		Survived:   f.Survived + o.Survived,
		Timeout:    f.Timeout + o.Timeout,
		Errors:     f.Errors + o.Errors,
		NotCovered: f.NotCovered + o.NotCovered,
	}
}

// addFile accumulates one row into r.Files, allocating the map on first use.
func (r *Report) addFile(file string, s FileStat) {
	if r.Files == nil {
		r.Files = map[string]FileStat{}
	}
	r.Files[file] = r.Files[file].plus(s)
}

// Origin tags record how a surviving mutant relates to the phase's diff.
// Set by verify's diff scoping, not by an adapter.
const (
	// OriginInHunk means the mutated line sits inside a hunk the phase changed.
	OriginInHunk = "in-hunk"
	// OriginInherited means the phase touched the file but not this line —
	// still in scope and still gating, just weaker evidence.
	OriginInherited = "inherited"
)

// Mutant is one specific change that survived.
type Mutant struct {
	File    string
	Line    int
	Op      string // operator (e.g. "ConditionalNegation")
	Snippet string // the surviving mutated source slice

	// Origin is OriginInHunk or OriginInherited once diff scoping has
	// classified the mutant; empty as the adapter produces it. It lives on
	// Mutant rather than on a verify-side wrapper because kept survivors
	// reach tests.json through languages[].mutation.surviving, which
	// serialises []Mutant directly.
	Origin string

	// Key is the survivor's cross-run identity (internal/survivor), resolved
	// during verify rather than by an adapter. It is what lets an acceptance
	// recorded in one phase still match this mutant in a later phase's run
	// after unrelated edits have shifted its line.
	Key string

	// Lifecycle is the one state this survivor carries in a run — in-diff,
	// routed, accepted, or unclassified. Empty as the adapter produces it;
	// a survivor that reaches tests.json still empty is a classification bug,
	// not an unremarkable default.
	Lifecycle string

	// Note carries the reason a survivor's state needs explaining: the
	// destination of a routed survivor, the ambiguity that stopped an
	// acceptance from suppressing it, or the resolution error that left it
	// unclassified.
	Note string
}

// Adapter runs mutation tests for one language family.
type Adapter interface {
	Name() string
	Supports(file string) bool
	Run(files []string) (*Report, error)
}

// ErrNotImplemented is returned by stub adapters in v0.
var ErrNotImplemented = errors.New("mutation adapter not yet implemented")

// Dispatch picks an adapter for a file extension. Returns nil if none.
func Dispatch(file string, adapters []Adapter) Adapter {
	for _, a := range adapters {
		if a.Supports(file) {
			return a
		}
	}
	return nil
}
