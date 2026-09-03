package verify

import (
	"testing"

	"github.com/Rivil/dross/internal/mutation"
)

// rangingAdapter records which call arm RunScoped chose, and with what.
type rangingAdapter struct {
	name       string
	ranRanges  map[string][]mutation.Range
	rangedCall bool
	plainCall  bool
	files      []string
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
	// PADDED: the hunk is 10-12, and hunkContextLines widens it to 1-37 (the
	// start clamps at line 1). The pad is asserted on its own in
	// TestPadAndMergeWidensEachHunk; what this case pins is that a.ts is
	// narrowed at all and b.ts is not.
	if got := a.ranRanges["web/src/a.ts"]; len(got) != 1 || got[0].Start != 1 || got[0].End != 37 {
		t.Errorf("ranges for a.ts = %v, want [{1 37}]", got)
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

// The pad exists because a mutant can enclose the changed line while starting
// above it — measured: portion-cascade.ts:29-29 finds 2 of the line's 3
// mutants, because the function-body block opens on line 28.
func TestPadAndMergeWidensEachHunk(t *testing.T) {
	got := padAndMerge([]Range{{Start: 100, End: 104}})
	want := []mutation.Range{{Start: 75, End: 129}}
	if len(got) != 1 || got[0] != want[0] {
		t.Errorf("padAndMerge = %v, want %v", got, want)
	}
}

// A hunk near the top of a file must not produce a range starting at or below
// zero: Stryker's spec is 1-based and runArgs refuses a Start <= 0 by falling
// back to the whole file, which would silently un-narrow the run.
func TestPadAndMergeClampsToTheFirstLine(t *testing.T) {
	got := padAndMerge([]Range{{Start: 3, End: 4}})
	if len(got) != 1 || got[0].Start != 1 {
		t.Errorf("padAndMerge = %v, want a range starting at line 1", got)
	}
}

// Two hunks whose pads overlap must merge. Two overlapping specs for one file
// make the argv claim a scope it does not have.
func TestPadAndMergeMergesOverlappingHunks(t *testing.T) {
	got := padAndMerge([]Range{{Start: 100, End: 101}, {Start: 120, End: 121}})
	if len(got) != 1 {
		t.Fatalf("padAndMerge = %v, want one merged range", got)
	}
	if got[0].Start != 75 || got[0].End != 146 {
		t.Errorf("merged range = %v, want {75 146}", got[0])
	}
}

// ...but hunks far apart must stay apart, or narrowing collapses into the
// whole file one merge at a time.
func TestPadAndMergeKeepsDistantHunksSeparate(t *testing.T) {
	got := padAndMerge([]Range{{Start: 10, End: 11}, {Start: 900, End: 901}})
	if len(got) != 2 {
		t.Errorf("padAndMerge = %v, want two ranges", got)
	}
}

func TestPadAndMergeOnNoHunksIsNil(t *testing.T) {
	if got := padAndMerge(nil); got != nil {
		t.Errorf("padAndMerge(nil) = %v, want nil", got)
	}
}

// The dispatch must carry the PADDED range, not the raw hunk — otherwise the
// pad is computed and thrown away.
func TestRunScopedPassesPaddedRanges(t *testing.T) {
	a := &rangingAdapter{name: "stryker"}
	scope := scopeWithHunks(
		[]string{"web/src/a.ts"},
		map[string][]Range{"web/src/a.ts": {{Start: 100, End: 104}}},
	)
	if _, err := RunScoped("p", []string{"web/src/a.ts"},
		[]mutation.Adapter{a}, scope); err != nil {
		t.Fatalf("RunScoped: %v", err)
	}
	got := a.ranRanges["web/src/a.ts"]
	if len(got) != 1 || got[0].Start != 75 || got[0].End != 129 {
		t.Errorf("dispatched ranges = %v, want [{75 129}]", got)
	}
}
