package verify

import (
	"fmt"
	"strings"
	"time"

	"github.com/Rivil/dross/internal/mutation"
)

// EffectiveRange is one line range as it was actually handed to a mutation
// tool — widened to the enclosing top-level construct — together with the
// construct that produced it. It is the persisted counterpart of
// mutation.Range: Scope.Hunks keeps the raw diff, this keeps what the tool
// was told, and the two sit side by side in tests.json so a run's claimed
// scope is provable from its own record rather than inferred from argv.
type EffectiveRange struct {
	Start int `json:"start"`
	End   int `json:"end"`
	// Construct names what the range was widened to — a top-level AST
	// construct's Label(), or ConstructHunk when the lines stayed the raw
	// hunk. Always set: a range that cannot say why it is what it is would
	// be the line-pad heuristic's silence under another name.
	Construct string `json:"construct"`
}

// Whole-file reasons: the CLOSED set of ways a file in a scoped run ends up
// mutated whole rather than by range. Each is a named fallback, never an
// absence — a leg that measured whole files must say why, or a whole-file run
// reads as a ranged one.
//
// Severity is split by cause (the fallback_severity lock). Whole-file is more
// measurement, never less, so a STRUCTURAL fallback cannot make a pass
// dishonest and is recorded without degrading the scope. A hunk the adapter
// COULD have used and did not is a lost signal, and those two surface through
// Scope.Degraded so the run prints the existing warning.
const (
	// WholeFileNoRangeRunner: the adapter has no RunRanges (gremlins).
	// Informational.
	WholeFileNoRangeRunner = "adapter-lacks-range-runner"
	// WholeFileNoHunks: the scope carries no hunks at all — a degraded diff,
	// a base ref that went missing. Degrades on a range-capable adapter.
	WholeFileNoHunks = "scope-has-no-hunks"
	// WholeFileAbsentFromHunks: the file is in scope but no hunk names it
	// (recorded by `dross changes`, not in the git diff). Informational.
	WholeFileAbsentFromHunks = "file-absent-from-hunks"
	// WholeFileMalformedRange: a raw hunk the tool would refuse. Degrades —
	// the hunk existed and was lost.
	WholeFileMalformedRange = "malformed-range"
	// WholeFileASTUnavailable: the file's top-level constructs could not be
	// resolved — no node, no parser in the tree, a parse error, a
	// RangeRunner with no resolver at all. Degrades on a range-capable
	// adapter: the hunk existed and its precision was lost, and a missing
	// parser is an environment fault that would otherwise silently cost
	// every run its narrowing. The cause goes on the Degraded line, never
	// into this value.
	WholeFileASTUnavailable = "ast-unavailable"
)

// ASTResult is one file's construct resolution: what the resolver returned,
// or why it could not. Exactly one of the two is meaningful.
type ASTResult struct {
	Constructs []mutation.Construct
	Err        error
}

// ASTIndex is the resolved constructs for every file a leg may range, keyed
// by the same repo-relative slash path the scope's hunks use. It is built
// BEFORE PlanRanges (resolveConstructs), so the planner stays pure: it reads
// the index, it never spawns. A file absent from the index is unresolved,
// which the planner records as ast-unavailable — the record must say what
// it did not know, not guess.
type ASTIndex map[string]ASTResult

// RangePlan is what PlanRanges decided for one adapter's leg, before anything
// runs. Dispatch is the map to hand RunRanges; Ranges is the same decision in
// its persisted shape; WholeFile names every file that is NOT in Dispatch and
// why; Degraded is what the caller appends to Scope.Degraded.
//
// Dispatch and Ranges are built from the same values in the same loop, so
// what the record claims and what the tool was told cannot drift apart.
type RangePlan struct {
	Dispatch  map[string][]mutation.Range
	Ranges    map[string][]EffectiveRange
	WholeFile map[string]string
	Degraded  []string
}

// PlanRanges classifies each of files for adapter a under scope: ranged, or
// whole-file with a named reason. Pure — no dispatch, no I/O — so the same
// call serves the attached path (which then runs the plan) and the detached
// one (which only records it).
//
// A nil scope is not a scoped run at all (plain Run, no diff), and yields an
// empty plan: provenance describes how a scope was applied, and there is
// nothing to describe. An EMPTY scope — one with no hunks — is different: the
// run was meant to be scoped and could not be, which is the NoHunks reason.
//
// Malformed is judged on the RAW hunk, before expansion, and before the
// resolver is ever consulted: a hunk the tool would refuse must surface as
// exactly that, not be widened into something legal by a construct that
// happens to enclose it. The offending numbers go into the Degraded line,
// never into the closed reason value.
//
// Per file the order is: absent from hunks → malformed → ast-unavailable →
// ranged. Ranges and Dispatch are projected from the one expansion in the
// same loop, so the record and the argv cannot drift.
func PlanRanges(a mutation.Adapter, files []string, scope *Scope, asts ASTIndex) RangePlan {
	var plan RangePlan
	if scope == nil {
		return plan
	}
	plan.WholeFile = make(map[string]string, len(files))

	if _, ok := a.(mutation.RangeRunner); !ok {
		for _, f := range files {
			plan.WholeFile[f] = WholeFileNoRangeRunner
		}
		return plan
	}
	if len(scope.Hunks) == 0 {
		for _, f := range files {
			plan.WholeFile[f] = WholeFileNoHunks
		}
		plan.Degraded = append(plan.Degraded, fmt.Sprintf(
			"%s: scope has no hunks; mutating %d file(s) whole", a.Name(), len(files)))
		return plan
	}

	plan.Dispatch = make(map[string][]mutation.Range, len(files))
	plan.Ranges = make(map[string][]EffectiveRange, len(files))
	for _, f := range files {
		hunks := scope.Hunks[f]
		if len(hunks) == 0 {
			plan.WholeFile[f] = WholeFileAbsentFromHunks
			continue
		}
		if bad, malformed := firstMalformed(hunks); malformed {
			plan.WholeFile[f] = WholeFileMalformedRange
			plan.Degraded = append(plan.Degraded, fmt.Sprintf(
				"%s: malformed hunk %d-%d in %s; mutating the whole file", a.Name(), bad.Start, bad.End, f))
			continue
		}
		res, resolved := asts[f]
		if !resolved || res.Err != nil {
			plan.WholeFile[f] = WholeFileASTUnavailable
			plan.Degraded = append(plan.Degraded, fmt.Sprintf(
				"%s: AST unavailable for %s (%s); mutating the whole file", a.Name(), f, astDetail(res, resolved)))
			continue
		}
		eff := expandToConstructs(hunks, res.Constructs)
		dispatch := make([]mutation.Range, 0, len(eff))
		for _, r := range eff {
			dispatch = append(dispatch, mutation.Range{Start: r.Start, End: r.End})
		}
		plan.Dispatch[f] = dispatch
		plan.Ranges[f] = eff
	}
	if len(plan.Dispatch) == 0 {
		plan.Dispatch = nil
		plan.Ranges = nil
	}
	return plan
}

// astDetail is the cause printed on the Degraded line: the resolver's own
// words with the sentinel prefix stripped (it is already the line's subject),
// or a statement that nothing resolved the file at all.
func astDetail(res ASTResult, resolved bool) string {
	if !resolved {
		return "no construct resolution recorded"
	}
	msg := res.Err.Error()
	if rest, ok := strings.CutPrefix(msg, mutation.ErrASTUnavailable.Error()+": "); ok {
		return rest
	}
	return msg
}

// firstMalformed returns the first raw hunk that fails mutation.Range.Valid —
// the same predicate stryker's argv builder refuses on.
func firstMalformed(hunks []Range) (Range, bool) {
	for _, h := range hunks {
		if !(mutation.Range{Start: h.Start, End: h.End}).Valid() {
			return h, true
		}
	}
	return Range{}, false
}

// Provenance is the range-provenance projection of one recorded run: what
// `dross verify scope <phase>` prints, and what its --json emits. It is a
// field-for-field copy of the loaded Tests — the same VALUES, nothing
// re-rendered — so the command answers "what did this run actually measure"
// from the record itself rather than from a summary of it.
//
// It lives beside the fields it projects rather than in internal/cmd so the
// shape of the record and the shape of its readout cannot drift apart
// unnoticed. Files here is a copy of Scope.Files — an in-scope path list dross
// has already contained on the way in — and is never opened by anything that
// reads this type.
type Provenance struct {
	Phase       string             `json:"phase"`
	GeneratedAt time.Time          `json:"generated_at"`
	Files       []string           `json:"files"`
	Hunks       map[string][]Range `json:"hunks,omitempty"`
	Legs        []ProvenanceLeg    `json:"legs"`
}

// ProvenanceLeg is one LanguageRun's provenance: what it was dispatched, what
// it ranged, and what it mutated whole.
type ProvenanceLeg struct {
	Name      string                      `json:"name"`
	Tool      string                      `json:"tool"`
	Files     []string                    `json:"files"`
	Ranges    map[string][]EffectiveRange `json:"ranges,omitempty"`
	WholeFile map[string]string           `json:"whole_file,omitempty"`
}

// ProvenanceOf projects a loaded Tests. A run recorded with no scope (a
// plain Run) projects empty Files and Hunks rather than nil-dereferencing —
// the readout must be able to say "unscoped" about an old record.
func ProvenanceOf(t *Tests) Provenance {
	p := Provenance{Phase: t.Phase, GeneratedAt: t.GeneratedAt}
	if t.Scope != nil {
		p.Files = t.Scope.Files
		p.Hunks = t.Scope.Hunks
	}
	for _, lr := range t.Languages {
		p.Legs = append(p.Legs, ProvenanceLeg{
			Name:      lr.Name,
			Tool:      lr.Tool,
			Files:     lr.Files,
			Ranges:    lr.Ranges,
			WholeFile: lr.WholeFile,
		})
	}
	return p
}
