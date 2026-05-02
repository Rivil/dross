package verify

import (
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"github.com/Rivil/dross/internal/mutation"
)

// fakeAdapter is a deterministic Adapter for unit-testing Run().
type fakeAdapter struct {
	name        string
	supportsExt []string
	report      *mutation.Report
	err         error
}

func (f *fakeAdapter) Name() string { return f.name }
func (f *fakeAdapter) Supports(file string) bool {
	for _, e := range f.supportsExt {
		if filepath.Ext(file) == e {
			return true
		}
	}
	return false
}
func (f *fakeAdapter) Run(_ []string) (*mutation.Report, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.report, nil
}

func TestRunGroupsByAdapter(t *testing.T) {
	stry := &fakeAdapter{
		name:        "stryker",
		supportsExt: []string{".ts", ".tsx"},
		report: &mutation.Report{
			Tool: "stryker", Killed: 4, Survived: 1, Score: 0.8,
		},
	}
	gremlins := &fakeAdapter{
		name:        "gremlins",
		supportsExt: []string{".go"},
		report: &mutation.Report{
			Tool: "gremlins", Killed: 2, Survived: 0, Score: 1.0,
		},
	}

	files := []string{"src/api.ts", "src/main.go", "src/util.ts", "static/x.html"}
	got, err := Run("01-test", files, []mutation.Adapter{stry, gremlins})
	if err != nil {
		t.Fatal(err)
	}

	if got.Phase != "01-test" {
		t.Errorf("phase: %q", got.Phase)
	}
	if len(got.Languages) != 2 {
		t.Fatalf("expected 2 language runs, got %d", len(got.Languages))
	}

	byTool := map[string]LanguageRun{}
	for _, lr := range got.Languages {
		byTool[lr.Tool] = lr
	}
	tsFiles := byTool["stryker"].Files
	sort.Strings(tsFiles)
	if !reflect.DeepEqual(tsFiles, []string{"src/api.ts", "src/util.ts"}) {
		t.Errorf("ts files: %v", tsFiles)
	}
	if !reflect.DeepEqual(byTool["gremlins"].Files, []string{"src/main.go"}) {
		t.Errorf("go files: %v", byTool["gremlins"].Files)
	}
	if len(got.Skipped) != 1 || got.Skipped[0].File != "static/x.html" {
		t.Errorf("expected x.html skipped: %+v", got.Skipped)
	}
}

func TestRunNoFiles(t *testing.T) {
	got, err := Run("01-x", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.Phase != "01-x" {
		t.Errorf("phase: %q", got.Phase)
	}
	if len(got.Languages) != 0 || len(got.Skipped) != 0 {
		t.Errorf("expected empty result: %+v", got)
	}
}

func TestRunPropagatesAdapterError(t *testing.T) {
	stry := &fakeAdapter{
		name:        "stryker",
		supportsExt: []string{".ts"},
		err:         errExample{},
	}
	if _, err := Run("p", []string{"x.ts"}, []mutation.Adapter{stry}); err == nil {
		t.Fatal("expected error from adapter to propagate")
	}
}

type errExample struct{}

func (errExample) Error() string { return "boom" }

func TestSkeletonSeedsFromMachineResults(t *testing.T) {
	t1 := &Tests{
		Phase: "01-x",
		Languages: []LanguageRun{
			{
				Tool: "stryker",
				Files: []string{"src/x.ts"},
				Mutation: &mutation.Report{
					Tool: "stryker", Killed: 9, Survived: 1, Score: 0.9,
					Surviving: []mutation.Mutant{
						{File: "src/x.ts", Line: 42, Op: "ConditionalExpression"},
					},
				},
			},
		},
		Skipped: []SkippedFile{{File: "static/y.html", Reason: "no html adapter"}},
	}
	v := Skeleton(t1, []string{"c-1", "c-2"})

	if v.Verify.Phase != "01-x" {
		t.Errorf("phase: %q", v.Verify.Phase)
	}
	if v.Verify.Verdict != "pending" {
		t.Errorf("expected pending verdict pre-LLM; got %q", v.Verify.Verdict)
	}
	if v.Summary.CriteriaTotal != 2 {
		t.Errorf("criteria total: %d", v.Summary.CriteriaTotal)
	}
	if v.Summary.MutationScore != 0.9 {
		t.Errorf("mutation score: %v", v.Summary.MutationScore)
	}
	if v.Summary.MutantsKilled != 9 || v.Summary.MutantsSurvived != 1 {
		t.Errorf("mutant counts: k=%d s=%d", v.Summary.MutantsKilled, v.Summary.MutantsSurvived)
	}
	if len(v.Criteria) != 2 || v.Criteria[0].Status != "unknown" {
		t.Errorf("criteria seeded wrong: %+v", v.Criteria)
	}
	// 1 finding for the surviving mutant + 1 for the skipped file
	if len(v.Findings) != 2 {
		t.Fatalf("findings: %+v", v.Findings)
	}
	flagCount, noteCount := 0, 0
	for _, f := range v.Findings {
		switch f.Severity {
		case "FLAG":
			flagCount++
		case "NOTE":
			noteCount++
		}
	}
	if flagCount != 1 || noteCount != 1 {
		t.Errorf("expected 1 FLAG (surviving mutant) + 1 NOTE (skip); got flags=%d notes=%d",
			flagCount, noteCount)
	}
}

func TestFilesFromChanges(t *testing.T) {
	in := map[string][]string{
		"t-1": {"src/a.ts", "src/b.ts"},
		"t-2": {"src/b.ts", "src/c.go"}, // b.ts dedupes
	}
	got := FilesFromChanges(in)
	want := []string{"src/a.ts", "src/b.ts", "src/c.go"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v want %v", got, want)
	}
}

func TestSplitFiles(t *testing.T) {
	mut, snap := SplitFiles([]string{
		"src/a.ts", "src/b.go", "src/c.cs",
		"static/index.html", "static/style.css",
		"README.md",
	})
	sort.Strings(mut)
	sort.Strings(snap)
	wantMut := []string{"src/a.ts", "src/b.go", "src/c.cs"}
	wantSnap := []string{"README.md", "static/index.html", "static/style.css"}
	if !reflect.DeepEqual(mut, wantMut) {
		t.Errorf("mutable: %v want %v", mut, wantMut)
	}
	if !reflect.DeepEqual(snap, wantSnap) {
		t.Errorf("snapshot: %v want %v", snap, wantSnap)
	}
}

func TestCombineScore(t *testing.T) {
	cases := []struct {
		existing, next, want float64
	}{
		{0, 0.8, 0.8},
		{0.6, 0, 0.6},
		{0.6, 0.8, 0.7}, // mean
	}
	for _, tc := range cases {
		if got := combineScore(tc.existing, tc.next); got != tc.want {
			t.Errorf("combine(%v,%v) = %v want %v", tc.existing, tc.next, got, tc.want)
		}
	}
}

func TestTestsSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tests.json")
	original := &Tests{
		Phase: "01-x",
		Languages: []LanguageRun{
			{Tool: "stryker", Files: []string{"a.ts"},
				Mutation: &mutation.Report{Tool: "stryker", Killed: 5, Score: 1.0}},
		},
	}
	if err := original.Save(path); err != nil {
		t.Fatal(err)
	}
	got, err := LoadTests(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Phase != "01-x" || len(got.Languages) != 1 {
		t.Errorf("round-trip drift: %+v", got)
	}
	if got.Languages[0].Mutation.Killed != 5 {
		t.Errorf("mutation drift: %+v", got.Languages[0].Mutation)
	}
}

func TestLoadTestsMissingReturnsNil(t *testing.T) {
	got, err := LoadTests(filepath.Join(t.TempDir(), "nope.json"))
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Errorf("expected nil for missing file, got %+v", got)
	}
}

func TestVerifyRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "verify.toml")
	original := &Verify{
		Verify:  VerifyMeta{Phase: "01-x", Verdict: "pass"},
		Summary: VerifySummary{MutationScore: 0.9, CriteriaTotal: 2, CriteriaCovered: 2},
		Criteria: []CriterionResult{
			{ID: "c-1", Status: "covered", Tests: []string{"x.test.ts:12"}},
			{ID: "c-2", Status: "covered", Tests: []string{"y.test.ts:5"}},
		},
		Findings: []Finding{
			{Severity: "NOTE", Text: "all green"},
		},
	}
	if err := original.Save(path); err != nil {
		t.Fatal(err)
	}
	got, err := LoadVerify(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Verify.Verdict != "pass" {
		t.Errorf("verdict: %q", got.Verify.Verdict)
	}
	if len(got.Criteria) != 2 || got.Criteria[0].Status != "covered" {
		t.Errorf("criteria drift: %+v", got.Criteria)
	}
}
