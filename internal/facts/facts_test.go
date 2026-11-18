package facts

import (
	"reflect"
	"testing"

	"github.com/Rivil/dross/internal/architecture"
	"github.com/Rivil/dross/internal/phase"
)

// factsRepo is a small tree: x.go with Init at line 7, and a real file a.go.
func factsRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	write(t, root, "x.go", "package x\n\n// one\n// two\n// three\n\nfunc Init() {}\n")
	write(t, root, "a.go", "package x\n")
	return root
}

func check(t *testing.T, root string, spec *phase.Spec, plan *phase.Plan) Report {
	t.Helper()
	rep, err := Check(root, spec, plan)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	return rep
}

func lines(rep Report) []string {
	var out []string
	for _, f := range rep.Unresolved {
		out = append(out, f.String())
	}
	return out
}

// TestCheckScansOnlyPresentTenseFields: a missing path cited only in a
// decision why, a deferred entry or an edge reason is never reported.
func TestCheckScansOnlyPresentTenseFields(t *testing.T) {
	root := factsRepo(t)
	spec := &phase.Spec{
		Criteria:  []phase.Criterion{{ID: "c-1", Text: "uses a.go"}},
		Decisions: []phase.Decision{{Key: "k", Choice: "keep a.go", Why: "because gone.go broke"}},
		Deferred:  []phase.Deferred{{Text: "later gone.go", Why: "gone.go someday"}},
		Edges:     []phase.Edge{{Class: "empty", Outcome: "na", Reason: "gone.go never empty"}},
	}
	if got := lines(check(t, root, spec, nil)); len(got) != 0 {
		t.Errorf("unscanned fields leaked findings: %q", got)
	}
}

// TestCheckFieldLabels pins every field label exactly.
func TestCheckFieldLabels(t *testing.T) {
	root := factsRepo(t)
	spec := &phase.Spec{
		Criteria:  []phase.Criterion{{ID: "c-1", Text: "fine"}, {ID: "c-2", Text: "see gone1.go"}},
		Decisions: []phase.Decision{{Key: "symbol_shape", Choice: "per gone2.go"}},
	}
	plan := &phase.Plan{Task: []phase.Task{{
		ID: "t-3", Files: []string{"a.go"},
		Description:  "edit gone3.go",
		TestContract: []string{"ok", "reads gone4.go"},
	}}}
	want := []string{
		"criteria c-2: missing gone1.go",
		"decisions symbol_shape choice: missing gone2.go",
		"task t-3 description: missing gone3.go",
		"task t-3 test_contract[1]: missing gone4.go",
	}
	if got := lines(check(t, root, spec, plan)); !reflect.DeepEqual(got, want) {
		t.Errorf("labels:\n got %q\nwant %q", got, want)
	}
}

// TestCheckPlannedFilesSuppress: a path in any task's files is planned — it
// suppresses the same path cited anywhere, and is itself never a finding.
func TestCheckPlannedFilesSuppress(t *testing.T) {
	root := factsRepo(t)
	spec := &phase.Spec{Criteria: []phase.Criterion{{ID: "c-1", Text: "adds internal/new/thing.go"}}}
	plan := &phase.Plan{Task: []phase.Task{
		{ID: "t-1", Files: []string{"./internal/new/thing.go", "internal/also/missing.go"}, Description: "create it"},
		{ID: "t-2", Files: []string{"a.go"}, Description: "then use internal/new/thing.go:4"},
	}}
	if got := lines(check(t, root, spec, plan)); len(got) != 0 {
		t.Errorf("planned files were reported: %q", got)
	}
}

// TestCheckNewListMatching: spec.New entries normalise like citations.
func TestCheckNewListMatching(t *testing.T) {
	root := factsRepo(t)
	spec := &phase.Spec{
		New: []string{"foo.go", "pkg.Name"},
		Criteria: []phase.Criterion{
			{ID: "c-1", Text: "see foo.go:12. and foo.go"},
			{ID: "c-2", Text: "but fooo.go is a typo"},
			{ID: "c-3", Text: "calls `pkg.Name()`"},
		},
	}
	want := []string{"criteria c-2: missing fooo.go"}
	if got := lines(check(t, root, spec, nil)); !reflect.DeepEqual(got, want) {
		t.Errorf("new-list matching:\n got %q\nwant %q", got, want)
	}

	// A stored line citation clears exactly that line, and a bare name clears
	// the symbol.
	spec = &phase.Spec{
		New:      []string{"bar.go:3", "Widget.Run"},
		Criteria: []phase.Criterion{{ID: "c-1", Text: "bar.go:3 then bar.go:4 and `Widget.Run()`"}},
	}
	want = []string{"criteria c-1: missing bar.go:4"}
	if got := lines(check(t, root, spec, nil)); !reflect.DeepEqual(got, want) {
		t.Errorf("line/symbol new entries:\n got %q\nwant %q", got, want)
	}

	// Wrapped entries normalise too: backticks, a trailing "()" and sentence
	// punctuation never stop an entry from vouching.
	spec = &phase.Spec{
		New:      []string{"`baz.go`", "`Make()`", "qux.go:7."},
		Criteria: []phase.Criterion{{ID: "c-1", Text: "baz.go, `Make()` and qux.go:7 but not qux.go:8"}},
	}
	want = []string{"criteria c-1: missing qux.go:8"}
	if got := lines(check(t, root, spec, nil)); !reflect.DeepEqual(got, want) {
		t.Errorf("wrapped new entries:\n got %q\nwant %q", got, want)
	}
}

// TestCheckNilPlanAndDeterminism: no plan checks the spec alone; two runs are
// identical.
func TestCheckNilPlanAndDeterminism(t *testing.T) {
	root := factsRepo(t)
	spec := &phase.Spec{Criteria: []phase.Criterion{
		{ID: "c-1", Text: "b.go c.go `x.Missing` `Init` — x.go:3"},
		{ID: "c-2", Text: "a.go"},
	}}
	first := check(t, root, spec, nil)
	second := check(t, root, spec, nil)
	if !reflect.DeepEqual(first, second) {
		t.Errorf("two runs differ:\n%+v\n%+v", first, second)
	}
	want := []string{
		"criteria c-1: missing b.go",
		"criteria c-1: missing c.go",
		"criteria c-1: unresolved x.Missing",
		"criteria c-1: moved `Init` — x.go:3 now at line 7",
	}
	if got := lines(first); !reflect.DeepEqual(got, want) {
		t.Errorf("order:\n got %q\nwant %q", got, want)
	}
	if _, err := Check(root, nil, nil); err == nil {
		t.Error("Check with no spec returned no error")
	}
}

// TestKindMapping pins the Status → kind vocabulary.
func TestKindMapping(t *testing.T) {
	for _, tc := range []struct {
		k    Kind
		st   architecture.Status
		want string
	}{
		{KindPath, architecture.StatusUnresolved, "missing"},
		{KindLine, architecture.StatusMoved, "moved"},
		{KindLine, architecture.StatusUnresolved, "gone"},
		{KindSymbol, architecture.StatusUnresolved, "unresolved"},
		{KindPath, architecture.StatusOK, ""},
		{KindLine, architecture.StatusOK, ""},
		{KindLine, architecture.StatusAmbiguous, ""},
		{KindLine, architecture.StatusSkipped, ""},
		{KindSymbol, architecture.StatusAmbiguous, ""},
		{KindSymbol, architecture.StatusSkipped, ""},
		{KindSymbol, architecture.StatusOK, ""},
	} {
		if got := kindFor(tc.k, tc.st); got != tc.want {
			t.Errorf("kindFor(%s, %v) = %q, want %q", tc.k, tc.st, got, tc.want)
		}
	}
}

// TestCheckEndToEnd: extraction and resolution agree on a moved symbol, a
// gone symbol and a paired line into a missing file.
func TestCheckEndToEnd(t *testing.T) {
	root := factsRepo(t)
	spec := &phase.Spec{Criteria: []phase.Criterion{
		{ID: "c-1", Text: "`Init` — x.go:3"},
		{ID: "c-2", Text: "`Gone` — x.go:3 and `Init` — x.go:7"},
		{ID: "c-3", Text: "`Init` — nowhere.go:7"},
	}}
	want := []string{
		"criteria c-1: moved `Init` — x.go:3 now at line 7",
		"criteria c-2: gone `Gone` — x.go:3",
		"criteria c-3: missing `Init` — nowhere.go:7",
	}
	rep := check(t, root, spec, nil)
	if got := lines(rep); !reflect.DeepEqual(got, want) {
		t.Errorf("end to end:\n got %q\nwant %q", got, want)
	}
	if rep.Unresolved[0].NewLine != 7 || rep.Unresolved[0].Kind != KindMoved {
		t.Errorf("moved finding = %+v", rep.Unresolved[0])
	}
}

// TestCheckReportsDegraded: a degraded index surfaces on the report, and its
// unmatched symbols are not findings.
func TestCheckReportsDegraded(t *testing.T) {
	root := factsRepo(t)
	write(t, root, "src/tool.py", "def f():\n    pass\n")
	spec := &phase.Spec{Criteria: []phase.Criterion{{ID: "c-1", Text: "`tool.Missing`"}}}
	rep := check(t, root, spec, nil)
	if !rep.Degraded || rep.DegradedWhy == "" {
		t.Errorf("report not degraded: %+v", rep)
	}
	if len(rep.Unresolved) != 0 {
		t.Errorf("degraded index still reported: %q", lines(rep))
	}
}
