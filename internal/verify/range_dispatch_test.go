package verify

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/mutation"
)

// rangingAdapter records which call arm RunScoped chose, and with what.
type rangingAdapter struct {
	name         string
	ranRanges    map[string][]mutation.Range
	rangedCall   bool
	plainCall    bool
	files        []string
	constructs   map[string][]mutation.Construct
	constructErr error            // returned for every file when set
	fileErr      map[string]error // returned for that file only
	asked        []string         // files Constructs was called for, in order
	askedBefore  bool             // every Constructs call preceded the run
}

func (r *rangingAdapter) Name() string              { return r.name }
func (r *rangingAdapter) Supports(file string) bool { return true }

func (r *rangingAdapter) Run(files []string) (*mutation.Report, error) {
	r.plainCall = true
	r.files = files
	return &mutation.Report{Tool: r.name}, nil
}

func (r *rangingAdapter) RunRanges(files []string, ranges map[string][]mutation.Range) (*mutation.Report, error) {
	r.rangedCall = true
	r.files = files
	r.ranRanges = ranges
	return &mutation.Report{Tool: r.name}, nil
}

// Constructs is the canned resolver arm: constructs keyed by file, and an
// error to return in place of them. Nil constructs with a nil error is a
// file with no top-level nodes, which expands to the bare hunk.
func (r *rangingAdapter) Constructs(file string) ([]mutation.Construct, error) {
	r.asked = append(r.asked, file)
	r.askedBefore = !r.rangedCall && !r.plainCall
	if r.constructErr != nil {
		return nil, r.constructErr
	}
	if err := r.fileErr[file]; err != nil {
		return nil, err
	}
	return r.constructs[file], nil
}

// rangeOnlyAdapter is a RangeRunner that is NOT a ConstructResolver: it can
// narrow, but cannot say what encloses a line.
type rangeOnlyAdapter struct {
	name       string
	rangedCall bool
	plainCall  bool
}

func (r *rangeOnlyAdapter) Name() string              { return r.name }
func (r *rangeOnlyAdapter) Supports(file string) bool { return true }
func (r *rangeOnlyAdapter) Run(_ []string) (*mutation.Report, error) {
	r.plainCall = true
	return &mutation.Report{Tool: r.name}, nil
}
func (r *rangeOnlyAdapter) RunRanges(_ []string, _ map[string][]mutation.Range) (*mutation.Report, error) {
	r.rangedCall = true
	return &mutation.Report{Tool: r.name}, nil
}

// plainAdapter implements Adapter and NOT RangeRunner — gremlins' shape.
type plainAdapter struct {
	name      string
	plainCall bool
}

func (p *plainAdapter) Name() string              { return p.name }
func (p *plainAdapter) Supports(file string) bool { return true }
func (p *plainAdapter) Run(_ []string) (*mutation.Report, error) {
	p.plainCall = true
	return &mutation.Report{Tool: p.name}, nil
}

func scopeWithHunks(files []string, hunks map[string][]Range) *Scope {
	return NewScope(ScopeInput{Root: "/repo", Recorded: files, Hunks: hunks})
}

func TestRunScopedNarrowsToTheChangedLines(t *testing.T) {
	a := &rangingAdapter{name: "stryker"}
	scope := scopeWithHunks(
		[]string{"web/src/a.ts", "web/src/b.ts"},
		map[string][]Range{"web/src/a.ts": {{Start: 10, End: 12}}},
	)
	if _, err := RunScoped("p", []string{"web/src/a.ts", "web/src/b.ts"},
		[]mutation.Adapter{a}, scope); err != nil {
		t.Fatalf("RunScoped: %v", err)
	}
	if !a.rangedCall || a.plainCall {
		t.Fatalf("wanted the ranged arm; ranged=%v plain=%v", a.rangedCall, a.plainCall)
	}
	// The stub resolves no constructs, so the hunk stays the hunk: 10-12.
	// Widening is asserted on its own in range_expand_test; what this case
	// pins is that a.ts is narrowed at all and b.ts is not.
	if got := a.ranRanges["web/src/a.ts"]; len(got) != 1 || got[0].Start != 10 || got[0].End != 12 {
		t.Errorf("ranges for a.ts = %v, want [{10 12}]", got)
	}
	// b.ts has no hunks, so it carries no range and the adapter mutates it
	// whole. Its ABSENCE from the map is the fail-open signal — an empty slice
	// would be a range that selects nothing.
	if _, present := a.ranRanges["web/src/b.ts"]; present {
		t.Error("a file with no hunks must be absent from the range map, not present and empty")
	}
	if len(a.files) != 2 {
		t.Errorf("both files must still be dispatched, got %v", a.files)
	}
}

// A degraded diff must not narrow. phaseScope already degrades loudly rather
// than narrowing quietly, and this seam must not undo that.
func TestRunScopedWithNoHunksRunsWholeFiles(t *testing.T) {
	a := &rangingAdapter{name: "stryker"}
	scope := scopeWithHunks([]string{"web/src/a.ts"}, nil)
	if _, err := RunScoped("p", []string{"web/src/a.ts"},
		[]mutation.Adapter{a}, scope); err != nil {
		t.Fatalf("RunScoped: %v", err)
	}
	if a.rangedCall || !a.plainCall {
		t.Errorf("no hunks must mean the plain arm; ranged=%v plain=%v", a.rangedCall, a.plainCall)
	}
}

// Hunks that name only files this adapter is not running must not put it on
// the ranged arm with an empty map — that is whole-file scope with extra steps.
func TestRunScopedWithHunksForOtherFilesRunsWholeFiles(t *testing.T) {
	a := &rangingAdapter{name: "stryker"}
	scope := scopeWithHunks(
		[]string{"web/src/a.ts", "web/src/other.ts"},
		map[string][]Range{"web/src/other.ts": {{Start: 1, End: 1}}},
	)
	if _, err := RunScoped("p", []string{"web/src/a.ts"},
		[]mutation.Adapter{a}, scope); err != nil {
		t.Fatalf("RunScoped: %v", err)
	}
	if a.rangedCall || !a.plainCall {
		t.Errorf("wanted the plain arm; ranged=%v plain=%v", a.rangedCall, a.plainCall)
	}
}

// The guard that keeps the Go leg alive: asserting RangeRunner without
// checking would stop gremlins running at all.
func TestRunScopedStillRunsAnAdapterThatCannotRange(t *testing.T) {
	p := &plainAdapter{name: "gremlins"}
	scope := scopeWithHunks(
		[]string{"internal/a.go"},
		map[string][]Range{"internal/a.go": {{Start: 4, End: 8}}},
	)
	if _, err := RunScoped("p", []string{"internal/a.go"},
		[]mutation.Adapter{p}, scope); err != nil {
		t.Fatalf("RunScoped: %v", err)
	}
	if !p.plainCall {
		t.Error("an adapter without RunRanges must still be Run")
	}
}

func TestRunScopedWithANilScopeRunsWholeFiles(t *testing.T) {
	a := &rangingAdapter{name: "stryker"}
	if _, err := RunScoped("p", []string{"web/src/a.ts"},
		[]mutation.Adapter{a}, nil); err != nil {
		t.Fatalf("RunScoped: %v", err)
	}
	if a.rangedCall || !a.plainCall {
		t.Errorf("a nil scope must mean the plain arm; ranged=%v plain=%v", a.rangedCall, a.plainCall)
	}
}

// The dispatch must carry the CONSTRUCT span, not the raw hunk — otherwise
// the resolver is asked and its answer thrown away.
func TestRunScopedDispatchesTheConstructSpan(t *testing.T) {
	a := &rangingAdapter{name: "stryker", constructs: map[string][]mutation.Construct{
		"web/src/a.ts": {{Start: 80, End: 130, Kind: "FunctionDeclaration", Name: "run"}},
	}}
	scope := scopeWithHunks(
		[]string{"web/src/a.ts"},
		map[string][]Range{"web/src/a.ts": {{Start: 100, End: 104}}},
	)
	tests, err := RunScoped("p", []string{"web/src/a.ts"}, []mutation.Adapter{a}, scope)
	if err != nil {
		t.Fatalf("RunScoped: %v", err)
	}
	got := a.ranRanges["web/src/a.ts"]
	if len(got) != 1 || got[0].Start != 80 || got[0].End != 130 {
		t.Errorf("dispatched ranges = %v, want [{80 130}]", got)
	}
	rec := tests.Languages[0].Ranges["web/src/a.ts"]
	if len(rec) != 1 || rec[0] != (EffectiveRange{Start: 80, End: 130, Construct: "FunctionDeclaration run"}) {
		t.Errorf("recorded = %v, want the construct span with its label", rec)
	}
}

// A RangeRunner with no resolver must not range on bare hunks — that is the
// ungenerated-mutant hole this phase closes. Every hunked file falls open,
// the scope degrades, and the plain arm runs.
func TestRangeRunnerWithoutResolverDegrades(t *testing.T) {
	a := &rangeOnlyAdapter{name: "stryker"}
	scope := scopeWithHunks(
		[]string{"a.ts", "b.ts"},
		map[string][]Range{"a.ts": {{Start: 10, End: 12}}, "b.ts": {{Start: 3, End: 3}}},
	)
	tests, err := RunScoped("p", []string{"a.ts", "b.ts"}, []mutation.Adapter{a}, scope)
	if err != nil {
		t.Fatalf("RunScoped: %v", err)
	}
	if a.rangedCall || !a.plainCall {
		t.Errorf("want the plain arm; ranged=%v plain=%v", a.rangedCall, a.plainCall)
	}
	lr := tests.Languages[0]
	for _, f := range []string{"a.ts", "b.ts"} {
		if lr.WholeFile[f] != WholeFileASTUnavailable {
			t.Errorf("whole_file[%s] = %q, want %q", f, lr.WholeFile[f], WholeFileASTUnavailable)
		}
	}
	if lr.Ranges != nil {
		t.Errorf("no file may be ranged, got %v", lr.Ranges)
	}
	// scopeWithHunks already carries one degraded line of its own (no git
	// contribution); the plan's lines land after it.
	var named int
	for _, l := range scope.Degraded {
		if strings.Contains(l, "no construct resolver") && strings.Contains(l, "stryker") {
			named++
		}
	}
	if named != 2 {
		t.Errorf("want one degraded line per hunked file naming the cause, got %v", scope.Degraded)
	}
}

// The resolver is a spawn per file, so it is asked only for files the
// planner would range, and always before the tool is dispatched.
func TestResolverIsAskedOnlyForRangeableFilesAndBeforeDispatch(t *testing.T) {
	// A plain adapter: never asked (it is not a RangeRunner; the resolver
	// method is not even reachable).
	// A hunk-less scope: never asked.
	a := &rangingAdapter{name: "stryker"}
	if _, err := RunScoped("p", []string{"a.ts"}, []mutation.Adapter{a},
		scopeWithHunks([]string{"a.ts"}, nil)); err != nil {
		t.Fatal(err)
	}
	if len(a.asked) != 0 {
		t.Errorf("a hunk-less scope asked the resolver for %v", a.asked)
	}

	// Absent from hunks and malformed: never asked; the well-formed
	// neighbour: asked exactly once, before RunRanges.
	a = &rangingAdapter{name: "stryker"}
	scope := scopeWithHunks([]string{"a.ts", "b.ts", "c.ts"}, map[string][]Range{
		"a.ts": {{Start: 10, End: 12}},
		"c.ts": {{Start: 0, End: 3}},
	})
	if _, err := RunScoped("p", []string{"a.ts", "b.ts", "c.ts"}, []mutation.Adapter{a}, scope); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(a.asked, []string{"a.ts"}) {
		t.Errorf("resolver asked for %v, want exactly [a.ts]", a.asked)
	}
	if !a.askedBefore {
		t.Error("the resolver was asked after the tool had been dispatched")
	}
	if !a.rangedCall {
		t.Error("the well-formed file did not reach the ranged arm")
	}
}

// A resolver that fails falls the file open — it never aborts the leg.
func TestResolverFailureStillRunsTheLeg(t *testing.T) {
	a := &rangingAdapter{name: "stryker", constructErr: errors.New("node: not found")}
	scope := scopeWithHunks([]string{"a.ts"}, map[string][]Range{"a.ts": {{Start: 10, End: 12}}})
	tests, err := RunScoped("p", []string{"a.ts"}, []mutation.Adapter{a}, scope)
	if err != nil {
		t.Fatalf("RunScoped: %v", err)
	}
	lr := tests.Languages[0]
	if lr.Mutation == nil || lr.Error != "" {
		t.Fatalf("the leg did not run: mutation=%v error=%q", lr.Mutation, lr.Error)
	}
	if lr.WholeFile["a.ts"] != WholeFileASTUnavailable {
		t.Errorf("whole_file[a.ts] = %q, want %q", lr.WholeFile["a.ts"], WholeFileASTUnavailable)
	}
	if !a.plainCall || a.rangedCall {
		t.Errorf("want the plain arm; ranged=%v plain=%v", a.rangedCall, a.plainCall)
	}
}

// The recorded ranges ARE the dispatched ranges: one plan, executed and
// persisted from the same value. A second loop deriving the record would be a
// place for the two to drift.
func TestRecordedRangesAreTheDispatchedRanges(t *testing.T) {
	a := &rangingAdapter{name: "stryker"}
	files := []string{"src/a.ts", "src/b.ts", "src/c.ts"}
	scope := scopeWithHunks(files, map[string][]Range{
		"src/a.ts": {{Start: 10, End: 12}},
		"src/b.ts": {{Start: 100, End: 104}, {Start: 120, End: 121}},
	})
	tests, err := RunScoped("p", files, []mutation.Adapter{a}, scope)
	if err != nil {
		t.Fatalf("RunScoped: %v", err)
	}
	if !a.rangedCall {
		t.Fatal("the ranged arm was not taken")
	}
	lr := tests.Languages[0]
	if len(lr.Ranges) != len(a.ranRanges) {
		t.Fatalf("recorded %d files, dispatched %d", len(lr.Ranges), len(a.ranRanges))
	}
	for f, dispatched := range a.ranRanges {
		recorded := lr.Ranges[f]
		if len(recorded) != len(dispatched) {
			t.Fatalf("%s: recorded %v, dispatched %v", f, recorded, dispatched)
		}
		for i := range dispatched {
			if recorded[i].Start != dispatched[i].Start || recorded[i].End != dispatched[i].End {
				t.Errorf("%s[%d]: recorded %v, dispatched %v", f, i, recorded[i], dispatched[i])
			}
			if recorded[i].Construct == "" {
				t.Errorf("%s[%d]: recorded range carries no construct", f, i)
			}
		}
	}
	if got := lr.WholeFile["src/c.ts"]; got != WholeFileAbsentFromHunks {
		t.Errorf("c.ts whole_file = %q, want %q", got, WholeFileAbsentFromHunks)
	}
}
