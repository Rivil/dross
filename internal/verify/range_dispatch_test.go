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
