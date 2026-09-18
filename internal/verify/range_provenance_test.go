package verify

import (
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
		plan := PlanRanges(a, []string{"src/a.ts"}, scope)
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
	plan := PlanRanges(a, []string{"a.ts"}, scope)
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
	plan := PlanRanges(a, []string{"a.ts", "b.ts"}, scope)
	if _, present := plan.Dispatch["a.ts"]; present {
		t.Error("a.ts must not be dispatched")
	}
	want := []EffectiveRange{{Start: 1, End: 37, Pad: 25}}
	if got := plan.Ranges["b.ts"]; !reflect.DeepEqual(got, want) {
		t.Errorf("Ranges[b.ts] = %v, want %v", got, want)
	}
	if got := plan.Dispatch["b.ts"]; len(got) != 1 || got[0] != (mutation.Range{Start: 1, End: 37}) {
		t.Errorf("Dispatch[b.ts] = %v, want [{1 37}]", got)
	}
	if _, present := plan.WholeFile["b.ts"]; present {
		t.Error("b.ts is ranged and must not carry a whole-file reason")
	}
}

func TestFileAbsentFromHunksIsInformational(t *testing.T) {
	a := &rangingAdapter{name: "stryker"}
	scope := scopeWithHunks([]string{"a.ts", "b.ts"}, map[string][]Range{"a.ts": {{Start: 10, End: 12}}})
	plan := PlanRanges(a, []string{"a.ts", "b.ts"}, scope)
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
	plan := PlanRanges(a, files, scope)
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

	plan := PlanRanges(&rangingAdapter{name: "stryker"}, files, scope)
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
	plain := PlanRanges(&plainAdapter{name: "gremlins"}, files, scope)
	if len(plain.Degraded) != 0 {
		t.Errorf("a hunk-less scope on a plain adapter adds nothing, got %v", plain.Degraded)
	}
}

func TestEffectiveRangesArePostPadPostMerge(t *testing.T) {
	a := &rangingAdapter{name: "stryker"}
	hunks := []Range{{Start: 10, End: 12}, {Start: 30, End: 31}}
	scope := scopeWithHunks([]string{"a.ts"}, map[string][]Range{"a.ts": hunks})
	plan := PlanRanges(a, []string{"a.ts"}, scope)

	want := []EffectiveRange{{Start: 1, End: 56, Pad: 25}}
	if got := plan.Ranges["a.ts"]; !reflect.DeepEqual(got, want) {
		t.Errorf("Ranges[a.ts] = %v, want %v", got, want)
	}
	// And it IS padAndMerge's output, not a parallel computation.
	merged := padAndMerge(hunks)
	if len(merged) != len(plan.Dispatch["a.ts"]) {
		t.Fatalf("dispatch %v, padAndMerge %v", plan.Dispatch["a.ts"], merged)
	}
	for i, r := range merged {
		if plan.Dispatch["a.ts"][i] != r {
			t.Errorf("dispatch[%d] = %v, padAndMerge = %v", i, plan.Dispatch["a.ts"][i], r)
		}
		if e := plan.Ranges["a.ts"][i]; e.Start != r.Start || e.End != r.End {
			t.Errorf("recorded[%d] = %v, dispatched %v", i, e, r)
		}
	}
}

func TestWholeFileReasonsAreAClosedSet(t *testing.T) {
	for _, c := range []string{WholeFileNoRangeRunner, WholeFileNoHunks, WholeFileAbsentFromHunks, WholeFileMalformedRange} {
		if !wholeFileReasons[c] {
			t.Errorf("constant %q is not in the closed set", c)
		}
	}
	if len(wholeFileReasons) != 4 {
		t.Errorf("closed set has %d entries, want 4", len(wholeFileReasons))
	}
	// Every classification path lands inside it.
	files := []string{"a.ts", "b.ts", "c.ts"}
	scope := scopeWithHunks(files, map[string][]Range{
		"a.ts": {{Start: 0, End: 3}},
		"b.ts": {{Start: 10, End: 12}},
	})
	assertClosedReasons(t, PlanRanges(&rangingAdapter{name: "stryker"}, files, scope))
	assertClosedReasons(t, PlanRanges(&plainAdapter{name: "gremlins"}, files, scope))
	assertClosedReasons(t, PlanRanges(&rangingAdapter{name: "stryker"}, files, scopeWithHunks(files, nil)))
}

// A nil scope is not a scoped run: no ranges, no reasons, nothing degraded.
func TestNilScopePlansNothing(t *testing.T) {
	plan := PlanRanges(&rangingAdapter{name: "stryker"}, []string{"a.ts"}, nil)
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
