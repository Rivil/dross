package verify

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/mutation"
)

// wholeFileReasons is the closed set, spelled out here rather than derived
// from the constants so a fifth value added to the source without a
// deliberate edit here fails TestWholeFileReasonsAreAClosedSet.
var wholeFileReasons = map[string]bool{
	"adapter-lacks-range-runner": true,
	"scope-has-no-hunks":         true,
	"file-absent-from-hunks":     true,
	"malformed-range":            true,
	"ast-unavailable":            true,
}

// planFor is the pair RunScoped runs — resolve, then plan — so a planner
// test exercises the index the way the run builds it rather than a
// hand-made one.
func planFor(a mutation.Adapter, files []string, scope *Scope) RangePlan {
	return PlanRanges(a, files, scope, resolveConstructs(a, files, scope))
}

func assertClosedReasons(t *testing.T, plan RangePlan) {
	t.Helper()
	for f, reason := range plan.WholeFile {
		if !wholeFileReasons[reason] {
			t.Errorf("WholeFile[%s] = %q, outside the closed reason set", f, reason)
		}
	}
}

// The single validator: verify must never dispatch a range stryker's argv
// builder would refuse. runArgs is unexported in internal/mutation, so the
// contract is pinned through the predicate both layers share —
// mutation.Range.Valid — which TestRunArgsFallsBackOnAMalformedRange pins as
// runArgs' only refusal; every range PlanRanges dispatches must satisfy it,
// so runArgs emits `file:start-end` for every key and never falls back.
func TestVerifyNeverDispatchesARangeStrykerWouldRefuse(t *testing.T) {
	a := &rangingAdapter{name: "stryker"}
	for _, raw := range []Range{{0, 5}, {5, 2}, {-3, 4}} {
		scope := scopeWithHunks([]string{"src/a.ts"}, map[string][]Range{"src/a.ts": {raw}})
		plan := planFor(a, []string{"src/a.ts"}, scope)
		if got := plan.WholeFile["src/a.ts"]; got != WholeFileMalformedRange {
			t.Errorf("raw %v: WholeFile = %q, want %q", raw, got, WholeFileMalformedRange)
		}
		if _, present := plan.Dispatch["src/a.ts"]; present {
			t.Errorf("raw %v: a malformed file must be absent from the dispatch map", raw)
		}
		for f, rs := range plan.Dispatch {
			for _, r := range rs {
				if !r.Valid() {
					t.Errorf("raw %v: dispatched %s:%d-%d, which runArgs would refuse", raw, f, r.Start, r.End)
				}
			}
		}
	}
}

func TestMalformedRawHunkIsCaughtBeforePadding(t *testing.T) {
	a := &rangingAdapter{name: "stryker"}
	scope := scopeWithHunks([]string{"a.ts"}, map[string][]Range{"a.ts": {{Start: 0, End: 3}}})
	plan := planFor(a, []string{"a.ts"}, scope)
	if plan.WholeFile["a.ts"] != WholeFileMalformedRange {
		t.Fatalf("WholeFile = %v, want a.ts malformed", plan.WholeFile)
	}
	if rs := plan.Dispatch["a.ts"]; len(rs) != 0 {
		t.Errorf("{0,3} was padded and dispatched as %v instead of being refused raw", rs)
	}
	if len(plan.Degraded) != 1 {
		t.Fatalf("want one Degraded line, got %v", plan.Degraded)
	}
	for _, want := range []string{"a.ts", "0-3"} {
		if !strings.Contains(plan.Degraded[0], want) {
			t.Errorf("Degraded line lacks %q: %s", want, plan.Degraded[0])
		}
	}
	assertClosedReasons(t, plan)
}

func TestMalformedHunkFallsBackOnlyItsOwnFile(t *testing.T) {
	a := &rangingAdapter{name: "stryker"}
	scope := scopeWithHunks([]string{"a.ts", "b.ts"}, map[string][]Range{
		"a.ts": {{Start: 0, End: 3}},
		"b.ts": {{Start: 10, End: 12}},
	})
	plan := planFor(a, []string{"a.ts", "b.ts"}, scope)
	if _, present := plan.Dispatch["a.ts"]; present {
		t.Error("a.ts must not be dispatched")
	}
	want := []EffectiveRange{{Start: 10, End: 12, Construct: ConstructHunk}}
	if got := plan.Ranges["b.ts"]; !reflect.DeepEqual(got, want) {
		t.Errorf("Ranges[b.ts] = %v, want %v", got, want)
	}
	if got := plan.Dispatch["b.ts"]; len(got) != 1 || got[0] != (mutation.Range{Start: 10, End: 12}) {
		t.Errorf("Dispatch[b.ts] = %v, want [{10 12}]", got)
	}
	if _, present := plan.WholeFile["b.ts"]; present {
		t.Error("b.ts is ranged and must not carry a whole-file reason")
	}
}

func TestFileAbsentFromHunksIsInformational(t *testing.T) {
	a := &rangingAdapter{name: "stryker"}
	scope := scopeWithHunks([]string{"a.ts", "b.ts"}, map[string][]Range{"a.ts": {{Start: 10, End: 12}}})
	plan := planFor(a, []string{"a.ts", "b.ts"}, scope)
	if got := plan.WholeFile["b.ts"]; got != WholeFileAbsentFromHunks {
		t.Errorf("WholeFile[b.ts] = %q, want %q", got, WholeFileAbsentFromHunks)
	}
	if len(plan.Ranges) != 1 || len(plan.Ranges["a.ts"]) == 0 {
		t.Errorf("Ranges = %v, want a.ts only", plan.Ranges)
	}
	if len(plan.Degraded) != 0 {
		t.Errorf("file-absent-from-hunks must not degrade, got %v", plan.Degraded)
	}
	assertClosedReasons(t, plan)
}

func TestNoRangeRunnerIsInformational(t *testing.T) {
	a := &plainAdapter{name: "gremlins"}
	files := []string{"a.go", "b.go"}
	scope := scopeWithHunks(files, map[string][]Range{"a.go": {{Start: 10, End: 12}}})
	plan := planFor(a, files, scope)
	for _, f := range files {
		if got := plan.WholeFile[f]; got != WholeFileNoRangeRunner {
			t.Errorf("WholeFile[%s] = %q, want %q", f, got, WholeFileNoRangeRunner)
		}
	}
	if plan.Ranges != nil {
		t.Errorf("an adapter without RunRanges records no ranges, got %v", plan.Ranges)
	}
	if plan.Dispatch != nil {
		t.Errorf("nothing to dispatch by range, got %v", plan.Dispatch)
	}
	if len(plan.Degraded) != 0 {
		t.Errorf("adapter-lacks-range-runner must not degrade, got %v", plan.Degraded)
	}
	assertClosedReasons(t, plan)
}

func TestHunklessScopeOnACapableAdapterDegrades(t *testing.T) {
	files := []string{"a.ts", "b.ts"}
	scope := scopeWithHunks(files, nil)

	plan := planFor(&rangingAdapter{name: "stryker"}, files, scope)
	for _, f := range files {
		if got := plan.WholeFile[f]; got != WholeFileNoHunks {
			t.Errorf("WholeFile[%s] = %q, want %q", f, got, WholeFileNoHunks)
		}
	}
	if len(plan.Degraded) != 1 || !strings.Contains(plan.Degraded[0], "stryker") {
		t.Errorf("want exactly one Degraded line naming the adapter, got %v", plan.Degraded)
	}
	assertClosedReasons(t, plan)

	// Same scope, an adapter that could not have ranged anyway: nothing lost.
	plain := planFor(&plainAdapter{name: "gremlins"}, files, scope)
	if len(plain.Degraded) != 0 {
		t.Errorf("a hunk-less scope on a plain adapter adds nothing, got %v", plain.Degraded)
	}
}

func TestEffectiveRangesAreTheConstructSpans(t *testing.T) {
	a := &rangingAdapter{name: "stryker", constructs: map[string][]mutation.Construct{
		"a.ts": {{Start: 1, End: 56, Kind: "ClassDeclaration", Name: "Tally"}},
	}}
	hunks := []Range{{Start: 10, End: 12}, {Start: 30, End: 31}}
	scope := scopeWithHunks([]string{"a.ts"}, map[string][]Range{"a.ts": hunks})
	plan := planFor(a, []string{"a.ts"}, scope)

	want := []EffectiveRange{{Start: 1, End: 56, Construct: "ClassDeclaration Tally"}}
	if got := plan.Ranges["a.ts"]; !reflect.DeepEqual(got, want) {
		t.Errorf("Ranges[a.ts] = %v, want %v", got, want)
	}
	// And it IS expandToConstructs' output, not a parallel computation.
	expanded := expandToConstructs(hunks, a.constructs["a.ts"])
	if len(expanded) != len(plan.Dispatch["a.ts"]) {
		t.Fatalf("dispatch %v, expandToConstructs %v", plan.Dispatch["a.ts"], expanded)
	}
	for i, r := range expanded {
		if d := plan.Dispatch["a.ts"][i]; d.Start != r.Start || d.End != r.End {
			t.Errorf("dispatch[%d] = %v, expandToConstructs = %v", i, d, r)
		}
		if plan.Ranges["a.ts"][i] != r {
			t.Errorf("recorded[%d] = %v, expanded %v", i, plan.Ranges["a.ts"][i], r)
		}
	}
}

// The malformed check runs on the RAW hunk before any expansion, and the
// resolver is never consulted for a file it would refuse.
func TestMalformedRawHunkIsCaughtBeforeExpansion(t *testing.T) {
	a := &rangingAdapter{name: "stryker", constructs: map[string][]mutation.Construct{
		"a.ts": {{Start: 1, End: 40, Kind: "FunctionDeclaration", Name: "f"}},
	}}
	scope := scopeWithHunks([]string{"a.ts"}, map[string][]Range{"a.ts": {{Start: 0, End: 3}}})
	plan := planFor(a, []string{"a.ts"}, scope)
	if got := plan.WholeFile["a.ts"]; got != WholeFileMalformedRange {
		t.Errorf("WholeFile[a.ts] = %q, want %q (a construct must not launder a malformed hunk)", got, WholeFileMalformedRange)
	}
	if len(a.asked) != 0 {
		t.Errorf("the resolver was asked for a malformed file: %v", a.asked)
	}
}

// c-4: an unresolvable file falls open to whole-file under the ONE closed
// reason, the cause lands on the Degraded line, and its neighbours are not
// taken down with it.
func TestASTUnavailableDegradesAndFallsWholeOnlyItsOwnFile(t *testing.T) {
	a := &rangingAdapter{name: "stryker",
		fileErr: map[string]error{"a.ts": fmt.Errorf("%w: parse error at 12:4", mutation.ErrASTUnavailable)},
		constructs: map[string][]mutation.Construct{
			"b.ts": {{Start: 5, End: 30, Kind: "FunctionDeclaration", Name: "ok"}},
		}}
	scope := scopeWithHunks([]string{"a.ts", "b.ts"}, map[string][]Range{
		"a.ts": {{Start: 10, End: 12}},
		"b.ts": {{Start: 10, End: 12}},
	})
	plan := planFor(a, []string{"a.ts", "b.ts"}, scope)
	if got := plan.WholeFile["a.ts"]; got != WholeFileASTUnavailable {
		t.Errorf("WholeFile[a.ts] = %q, want exactly %q", got, WholeFileASTUnavailable)
	}
	if _, present := plan.Dispatch["a.ts"]; present {
		t.Error("a.ts must not be dispatched")
	}
	if len(plan.Degraded) != 1 {
		t.Fatalf("want one Degraded line, got %v", plan.Degraded)
	}
	for _, want := range []string{"stryker", "a.ts", "parse error at 12:4"} {
		if !strings.Contains(plan.Degraded[0], want) {
			t.Errorf("Degraded line lacks %q: %s", want, plan.Degraded[0])
		}
	}
	if strings.Count(plan.Degraded[0], "AST unavailable") != 1 {
		t.Errorf("the sentinel prefix leaked into the detail: %s", plan.Degraded[0])
	}
	want := []EffectiveRange{{Start: 5, End: 30, Construct: "FunctionDeclaration ok"}}
	if got := plan.Ranges["b.ts"]; !reflect.DeepEqual(got, want) {
		t.Errorf("the sibling must still range: Ranges[b.ts] = %v, want %v", got, want)
	}
	assertClosedReasons(t, plan)
}

// A range-capable adapter with a NIL index is every hunked file unresolved,
// not a panic and not a bare-hunk range.
func TestNilIndexOnARangeRunnerIsUnavailable(t *testing.T) {
	scope := scopeWithHunks([]string{"a.ts"}, map[string][]Range{"a.ts": {{Start: 10, End: 12}}})
	plan := PlanRanges(&rangingAdapter{name: "stryker"}, []string{"a.ts"}, scope, nil)
	if got := plan.WholeFile["a.ts"]; got != WholeFileASTUnavailable {
		t.Errorf("WholeFile[a.ts] = %q, want %q", got, WholeFileASTUnavailable)
	}
	if len(plan.Degraded) != 1 || !strings.Contains(plan.Degraded[0], "no construct resolution recorded") {
		t.Errorf("Degraded = %v, want the unresolved cause", plan.Degraded)
	}
}

// The collector passes a nil index for gremlins; a non-range adapter never
// reaches the index at all.
func TestNilIndexOnANonRangeAdapter(t *testing.T) {
	files := []string{"a.go", "b.go"}
	scope := scopeWithHunks(files, map[string][]Range{"a.go": {{Start: 10, End: 12}}})
	plan := PlanRanges(&mutation.Gremlins{}, files, scope, nil)
	for _, f := range files {
		if got := plan.WholeFile[f]; got != WholeFileNoRangeRunner {
			t.Errorf("WholeFile[%s] = %q, want %q", f, got, WholeFileNoRangeRunner)
		}
	}
	if len(plan.Degraded) != 0 {
		t.Errorf("a non-range adapter must not degrade, got %v", plan.Degraded)
	}
}

// The persisted shape: no pad anywhere, a construct on every range.
func TestRecordCarriesConstructNotPad(t *testing.T) {
	if _, has := reflect.TypeOf(EffectiveRange{}).FieldByName("Pad"); has {
		t.Fatal("EffectiveRange still has a Pad field")
	}
	if _, has := reflect.TypeOf(LegSummary{}).FieldByName("Pad"); has {
		t.Fatal("LegSummary still has a Pad field")
	}
	a := &rangingAdapter{name: "stryker", constructs: map[string][]mutation.Construct{
		"a.ts": {{Start: 1, End: 40, Kind: "FunctionDeclaration", Name: "f"}},
	}}
	scope := scopeWithHunks([]string{"a.ts", "b.ts"}, map[string][]Range{
		"a.ts": {{Start: 10, End: 12}},
		"b.ts": {{Start: 3, End: 3}},
	})
	tests, err := RunScoped("p", []string{"a.ts", "b.ts"}, []mutation.Adapter{a}, scope)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(tests.Languages[0])
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), `"pad"`) {
		t.Errorf("a marshalled leg still carries a pad key:\n%s", raw)
	}
	var back LanguageRun
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatal(err)
	}
	for f, rs := range back.Ranges {
		for _, r := range rs {
			if r.Construct == "" {
				t.Errorf("%s: range %v round-tripped with no construct", f, r)
			}
		}
	}
	if got := back.Ranges["b.ts"]; len(got) != 1 || got[0].Construct != ConstructHunk {
		t.Errorf("an unenclosed hunk must be labelled %q, got %v", ConstructHunk, got)
	}
}

func TestWholeFileReasonsAreAClosedSet(t *testing.T) {
	for _, c := range []string{WholeFileNoRangeRunner, WholeFileNoHunks, WholeFileAbsentFromHunks, WholeFileMalformedRange, WholeFileASTUnavailable} {
		if !wholeFileReasons[c] {
			t.Errorf("constant %q is not in the closed set", c)
		}
	}
	if len(wholeFileReasons) != 5 {
		t.Errorf("closed set has %d entries, want 5", len(wholeFileReasons))
	}
	// Every classification path lands inside it.
	files := []string{"a.ts", "b.ts", "c.ts"}
	scope := scopeWithHunks(files, map[string][]Range{
		"a.ts": {{Start: 0, End: 3}},
		"b.ts": {{Start: 10, End: 12}},
	})
	assertClosedReasons(t, planFor(&rangingAdapter{name: "stryker"}, files, scope))
	assertClosedReasons(t, planFor(&plainAdapter{name: "gremlins"}, files, scope))
	assertClosedReasons(t, planFor(&rangingAdapter{name: "stryker"}, files, scopeWithHunks(files, nil)))
	assertClosedReasons(t, planFor(&rangingAdapter{name: "stryker", constructErr: errors.New("x")}, files, scope))
	assertClosedReasons(t, planFor(&rangeOnlyAdapter{name: "stryker"}, files, scope))
}

// A nil scope is not a scoped run: no ranges, no reasons, nothing degraded.
func TestNilScopePlansNothing(t *testing.T) {
	plan := PlanRanges(&rangingAdapter{name: "stryker"}, []string{"a.ts"}, nil, nil)
	if !reflect.DeepEqual(plan, RangePlan{}) {
		t.Errorf("nil scope must yield an empty plan, got %+v", plan)
	}
}

// Range.Valid is the shared predicate; pin its edges once, here, where verify
// leans on it.
func TestRangeValidEdges(t *testing.T) {
	for _, tc := range []struct {
		r    mutation.Range
		want bool
	}{
		{mutation.Range{Start: 1, End: 1}, true},
		{mutation.Range{Start: 1, End: 2}, true},
		{mutation.Range{Start: 0, End: 5}, false},
		{mutation.Range{Start: 5, End: 2}, false},
		{mutation.Range{Start: -3, End: 4}, false},
	} {
		if got := tc.r.Valid(); got != tc.want {
			t.Errorf("%v.Valid() = %v, want %v", tc.r, got, tc.want)
		}
	}
}

// ProvenanceOf is the seam `dross verify scope` reads through, and its only
// callers live in internal/cmd — so the nil-scope branch at the top of it was
// killed by TestVerifyScopeJSONIsTheRecordVerbatim over there and by nothing
// here, where gremlins scores it (verify run r-20260918-073404). Both arms
// are pinned in-package: a scoped record projects its Files and Hunks by
// value, an unscoped one projects neither and does not dereference nil.
func TestProvenanceOfProjectsScopeAndToleratesNil(t *testing.T) {
	files := []string{"a.ts", "b.go"}
	hunks := map[string][]Range{"a.ts": {{Start: 10, End: 12}}}
	scoped := &Tests{
		Phase: "p",
		Scope: scopeWithHunks(files, hunks),
		Languages: []LanguageRun{
			{Name: "typescript", Tool: "stryker", Files: []string{"a.ts"},
				Ranges: map[string][]EffectiveRange{"a.ts": {{Start: 1, End: 37, Construct: "FunctionDeclaration tally"}}}},
			{Name: "go", Tool: "gremlins", Files: []string{"b.go"},
				WholeFile: map[string]string{"b.go": WholeFileNoRangeRunner}},
		},
	}
	p := ProvenanceOf(scoped)
	if p.Phase != "p" {
		t.Errorf("Phase = %q, want p", p.Phase)
	}
	if !reflect.DeepEqual(p.Files, scoped.Scope.Files) {
		t.Errorf("Files = %v, scope has %v", p.Files, scoped.Scope.Files)
	}
	if !reflect.DeepEqual(p.Hunks, scoped.Scope.Hunks) {
		t.Errorf("Hunks = %v, scope has %v", p.Hunks, scoped.Scope.Hunks)
	}
	if len(p.Legs) != 2 {
		t.Fatalf("Legs = %+v, want both legs", p.Legs)
	}
	for i, lr := range scoped.Languages {
		leg := p.Legs[i]
		if leg.Name != lr.Name || leg.Tool != lr.Tool || !reflect.DeepEqual(leg.Files, lr.Files) {
			t.Errorf("legs[%d] = %+v, run has name=%s tool=%s files=%v", i, leg, lr.Name, lr.Tool, lr.Files)
		}
		if !reflect.DeepEqual(leg.Ranges, lr.Ranges) || !reflect.DeepEqual(leg.WholeFile, lr.WholeFile) {
			t.Errorf("legs[%d] provenance = ranges %v whole_file %v, run has %v / %v",
				i, leg.Ranges, leg.WholeFile, lr.Ranges, lr.WholeFile)
		}
	}

	unscoped := &Tests{Phase: "old", Languages: scoped.Languages}
	q := ProvenanceOf(unscoped) // must not panic on the nil Scope
	if q.Files != nil || q.Hunks != nil {
		t.Errorf("an unscoped record projected files=%v hunks=%v, want neither", q.Files, q.Hunks)
	}
	if len(q.Legs) != 2 {
		t.Errorf("an unscoped record lost its legs: %+v", q.Legs)
	}
}
