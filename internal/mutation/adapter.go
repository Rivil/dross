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
	Tool      string // "stryker" | "gremlins" | ...
	Killed    int    // mutants the tests caught — good
	Survived  int    // mutants that escaped — theatrical tests (includes NotCovered)
	Timeout   int
	Errors    int
	Score     float64 // killed / (killed + survived)
	Surviving []Mutant

	// NotCovered is the subset of Survived where tests never executed the
	// mutated line at all (gremlins' "NOT COVERED" status). Tracked
	// separately because high NotCovered + low LIVED usually means a
	// coverage-tool blind spot (e.g. Go's package-init code in top-level
	// `var` arrays) rather than weak assertions — actionable diagnosis
	// the score alone can't surface. Other adapters (Stryker, Stryker.NET)
	// don't report this status and leave the field at zero.
	NotCovered int
}

// Mutant is one specific change that survived.
type Mutant struct {
	File    string
	Line    int
	Op      string // operator (e.g. "ConditionalNegation")
	Snippet string // the surviving mutated source slice
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
