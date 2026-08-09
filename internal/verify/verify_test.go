package verify

import (
	"errors"
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

// TestRunRecordsAdapterErrorAndContinues pins the record-and-continue
// contract: one failing adapter must not discard the other adapters'
// finished reports. Adapters run in sorted-name order (gremlins < stryker),
// so this is exactly the "stryker misconfigured destroys the gremlins
// report" polyglot failure.
func TestRunRecordsAdapterErrorAndContinues(t *testing.T) {
	stry := &fakeAdapter{
		name:        "stryker",
		supportsExt: []string{".ts"},
		err:         errExample{},
	}
	gremlins := &fakeAdapter{
		name:        "gremlins",
		supportsExt: []string{".go"},
		report: &mutation.Report{
			Tool: "gremlins", Killed: 2, Survived: 0, Score: 1.0,
		},
	}

	got, err := Run("p", []string{"x.ts", "y.go"}, []mutation.Adapter{stry, gremlins})
	if err != nil {
		t.Fatalf("adapter failure must not fail the whole run: %v", err)
	}
	if len(got.Languages) != 2 {
		t.Fatalf("both legs must be recorded, got %d", len(got.Languages))
	}
	byTool := map[string]LanguageRun{}
	for _, lr := range got.Languages {
		byTool[lr.Tool] = lr
	}
	if g := byTool["gremlins"]; g.Mutation == nil || g.Error != "" {
		t.Errorf("gremlins leg must keep its report: %+v", g)
	}
	if s := byTool["stryker"]; s.Error != "boom" || s.Mutation != nil {
		t.Errorf("stryker leg must record the error with nil Mutation: %+v", s)
	}
}

// TestSkeletonSurfacesAdapterError: an errored leg becomes a FLAG finding,
// and a measured leg alongside it still promotes mutation_status to
// measured — the errored leg's nil Mutation is skipped by the status logic.
func TestSkeletonSurfacesAdapterError(t *testing.T) {
	tests := &Tests{
		Phase: "p",
		Languages: []LanguageRun{
			{Name: "typescript", Tool: "stryker", Files: []string{"x.ts"}, Error: "boom"},
			{Name: "go", Tool: "gremlins", Files: []string{"y.go"},
				Mutation: &mutation.Report{Tool: "gremlins", Killed: 2, Score: 1.0}},
		},
	}
	v := Skeleton(tests, []string{"c-1"})

	if v.Summary.MutationStatus != MutationMeasured {
		t.Errorf("measured gremlins leg must promote status past the errored leg, got %q", v.Summary.MutationStatus)
	}
	want := "mutation adapter stryker failed: boom"
	found := false
	for _, f := range v.Findings {
		if f.Text == want {
			found = true
			if f.Severity != "FLAG" {
				t.Errorf("adapter-failure finding severity = %q, want FLAG", f.Severity)
			}
		}
	}
	if !found {
		t.Errorf("missing adapter-failure FLAG finding %q in %+v", want, v.Findings)
	}
}

type errExample struct{}

func (errExample) Error() string { return "boom" }

func TestSkeletonSeedsFromMachineResults(t *testing.T) {
	t1 := &Tests{
		Phase: "01-x",
		Languages: []LanguageRun{
			{
				Tool:  "stryker",
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
	if v.Summary.MutationStatus != MutationMeasured {
		t.Errorf("status: got %q want %q", v.Summary.MutationStatus, MutationMeasured)
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

// Mutation status must distinguish "adapter ran but instrumented zero
// mutants" (unmeasurable — Stryker scope excludes touched files) from
// "no adapter ran at all" (skipped — --skip-mutation or no matching ext)
// from "real mutation results" (measured). Without this distinction the
// verdict heuristic in the LLM prompt treats zero-score-from-no-mutants
// as failure, which was the FeastAhead phase 04/05 dogfood bug.
func TestSkeletonMutationStatus(t *testing.T) {
	cases := []struct {
		name string
		in   *Tests
		want string
	}{
		{
			name: "no language runs at all → skipped",
			in:   &Tests{Phase: "01-x"},
			want: MutationSkipped,
		},
		{
			name: "adapter reported but zero mutants → unmeasurable",
			in: &Tests{
				Phase: "01-x",
				Languages: []LanguageRun{
					{Tool: "stryker", Files: []string{"src/server.ts"},
						Mutation: &mutation.Report{Tool: "stryker"}}, // 0/0/0
				},
			},
			want: MutationUnmeasurable,
		},
		{
			name: "adapter reported real mutants → measured",
			in: &Tests{
				Phase: "01-x",
				Languages: []LanguageRun{
					{Tool: "stryker", Files: []string{"src/x.ts"},
						Mutation: &mutation.Report{Tool: "stryker", Killed: 4, Survived: 1, Score: 0.8}},
				},
			},
			want: MutationMeasured,
		},
		{
			name: "one unmeasurable + one measured run → measured",
			in: &Tests{
				Phase: "01-x",
				Languages: []LanguageRun{
					{Tool: "stryker", Files: []string{"src/server.ts"},
						Mutation: &mutation.Report{Tool: "stryker"}}, // 0/0/0
					{Tool: "gremlins", Files: []string{"src/main.go"},
						Mutation: &mutation.Report{Tool: "gremlins", Killed: 3, Score: 1.0}},
				},
			},
			want: MutationMeasured,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			v := Skeleton(c.in, []string{"c-1"})
			if v.Summary.MutationStatus != c.want {
				t.Errorf("status: got %q want %q", v.Summary.MutationStatus, c.want)
			}
		})
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

// --- diff scoping -----------------------------------------------------------

// errAdapterBoom stands in for any adapter failure.
var errAdapterBoom = errors.New("adapter exploded")

// scopeOf is shorthand for a scope over an explicit file list.
func scopeOf(files ...string) *Scope {
	return NewScope(ScopeInput{Git: files})
}

// reportOf builds a Report whose aggregates and per-file rows agree, the shape
// every adapter is required to produce (see mutation's drift assertions).
func reportOf(tool string, rows map[string]mutation.FileStat, surviving ...mutation.Mutant) *mutation.Report {
	r := &mutation.Report{Tool: tool, Files: rows, Surviving: surviving}
	for _, st := range rows {
		r.Killed += st.Killed
		r.Survived += st.Survived
		r.Timeout += st.Timeout
		r.Errors += st.Errors
		r.NotCovered += st.NotCovered
	}
	if denom := r.Killed + r.Survived + r.Timeout; denom > 0 {
		r.Score = float64(r.Killed) / float64(denom)
	}
	return r
}

// TestFilterReportDropsUntouchedSibling is the phase's whole point: two
// survivors in the same package, one in a file the phase edited and one in a
// file it never opened. Only the first is this phase's problem.
func TestFilterReportDropsUntouchedSibling(t *testing.T) {
	r := reportOf("gremlins",
		map[string]mutation.FileStat{
			"pkg/touched.go":   {Survived: 1},
			"pkg/untouched.go": {Survived: 1},
		},
		mutation.Mutant{File: "pkg/touched.go", Line: 4, Op: "CONDITIONALS_NEGATION"},
		mutation.Mutant{File: "pkg/untouched.go", Line: 9, Op: "ARITHMETIC_BASE"},
	)

	kept, dropped := FilterReport(r, scopeOf("pkg/touched.go"), "go")

	if len(kept.Surviving) != 1 || kept.Surviving[0].File != "pkg/touched.go" {
		t.Errorf("kept survivors: %+v", kept.Surviving)
	}
	if len(dropped) != 1 || dropped[0].File != "pkg/untouched.go" {
		t.Errorf("dropped survivors: %+v", dropped)
	}
}

// TestFilterReportCounterImmunity: a mutant in an untouched file must move
// NEITHER half of the fraction. Both directions are asserted because pruning
// only the Surviving slice passes the survivor half while leaving a
// neighbour's kills inflating the numerator.
func TestFilterReportCounterImmunity(t *testing.T) {
	base := reportOf("gremlins", map[string]mutation.FileStat{
		"a.go": {Killed: 1, Survived: 1},
	}, mutation.Mutant{File: "a.go", Line: 2})
	want, _ := FilterReport(base, scopeOf("a.go"), "go")

	plusKill := reportOf("gremlins", map[string]mutation.FileStat{
		"a.go": {Killed: 1, Survived: 1},
		"b.go": {Killed: 1},
	}, mutation.Mutant{File: "a.go", Line: 2})
	gotKill, _ := FilterReport(plusKill, scopeOf("a.go"), "go")
	if gotKill.Killed != want.Killed || gotKill.Score != want.Score {
		t.Errorf("numerator moved: killed=%d score=%v want killed=%d score=%v",
			gotKill.Killed, gotKill.Score, want.Killed, want.Score)
	}

	plusSurvivor := reportOf("gremlins", map[string]mutation.FileStat{
		"a.go": {Killed: 1, Survived: 1},
		"b.go": {Survived: 1},
	},
		mutation.Mutant{File: "a.go", Line: 2},
		mutation.Mutant{File: "b.go", Line: 7},
	)
	gotSurv, dropped := FilterReport(plusSurvivor, scopeOf("a.go"), "go")
	if gotSurv.Survived != want.Survived || gotSurv.Score != want.Score {
		t.Errorf("denominator moved: survived=%d score=%v want survived=%d score=%v",
			gotSurv.Survived, gotSurv.Score, want.Survived, want.Score)
	}
	if len(dropped) != 1 {
		t.Errorf("dropped: %+v", dropped)
	}
}

// TestFilterReportConcreteArithmetic pins actual numbers rather than a
// relation: the unfiltered report scores 0.91 over 11 mutants, the phase's own
// slice scores 0.50 over 2. Those are the two numbers a reader can check.
func TestFilterReportConcreteArithmetic(t *testing.T) {
	r := reportOf("gremlins", map[string]mutation.FileStat{
		"touched.go":   {Killed: 1, Survived: 1},
		"untouched.go": {Killed: 9},
	}, mutation.Mutant{File: "touched.go", Line: 1})

	kept, _ := FilterReport(r, scopeOf("touched.go"), "go")
	if kept.Killed != 1 || kept.Survived != 1 {
		t.Errorf("counts: killed=%d survived=%d want 1/1", kept.Killed, kept.Survived)
	}
	if kept.Score != 0.5 {
		t.Errorf("score = %v want 0.50 (not 0.91)", kept.Score)
	}
}

// TestFilterReportTimeoutInDenominator pins the denominator convention against
// the live one, killed/(killed+survived+timeout). adapter.go's Score doc
// claims killed/(killed+survived); a recomputation written from that comment
// scores this report 1.00.
func TestFilterReportTimeoutInDenominator(t *testing.T) {
	r := reportOf("gremlins", map[string]mutation.FileStat{
		"a.go": {Killed: 1, Timeout: 1},
	})
	kept, _ := FilterReport(r, scopeOf("a.go"), "go")
	if kept.Score != 0.5 {
		t.Errorf("score = %v want 0.50 — timeouts belong in the denominator", kept.Score)
	}
	if kept.Timeout != 1 {
		t.Errorf("timeout count lost: %d", kept.Timeout)
	}
}

// TestFilterReportTagsEveryKeptSurvivor: no kept survivor may leave the filter
// untagged, and the tag must distinguish a line the phase changed from one it
// merely edited around. Both stay in the denominator — the tag weights
// evidence, it does not gate.
func TestFilterReportTagsEveryKeptSurvivor(t *testing.T) {
	s := NewScope(ScopeInput{
		Git:   []string{"a.go"},
		Hunks: map[string][]Range{"a.go": {{Start: 10, End: 12}}},
	})
	r := reportOf("gremlins", map[string]mutation.FileStat{"a.go": {Survived: 2}},
		mutation.Mutant{File: "a.go", Line: 11},
		mutation.Mutant{File: "a.go", Line: 40},
	)

	kept, _ := FilterReport(r, s, "go")
	if len(kept.Surviving) != 2 {
		t.Fatalf("both survivors stay in scope: %+v", kept.Surviving)
	}
	byLine := map[int]string{}
	for _, m := range kept.Surviving {
		if m.Origin == "" {
			t.Errorf("untagged survivor: %+v", m)
		}
		byLine[m.Line] = m.Origin
	}
	if byLine[11] != mutation.OriginInHunk {
		t.Errorf("line 11 origin = %q want %q", byLine[11], mutation.OriginInHunk)
	}
	if byLine[40] != mutation.OriginInherited {
		t.Errorf("line 40 origin = %q want %q", byLine[40], mutation.OriginInherited)
	}
	if kept.Survived != 2 {
		t.Errorf("an inherited survivor must stay in the denominator: survived=%d", kept.Survived)
	}
}

// TestFilterReportDroppedListIsComplete: filtered survivors are preserved in
// full, not counted and discarded. An implementation that returns only a count
// fails the field assertions.
func TestFilterReportDroppedListIsComplete(t *testing.T) {
	r := reportOf("stryker", map[string]mutation.FileStat{"src/b.ts": {Survived: 2}},
		mutation.Mutant{File: "src/b.ts", Line: 3, Op: "ConditionalExpression"},
		mutation.Mutant{File: "src/b.ts", Line: 8, Op: "ArithmeticOperator"},
	)

	kept, dropped := FilterReport(r, scopeOf("src/a.ts"), "typescript")
	if len(kept.Surviving) != 0 {
		t.Errorf("nothing was in scope: %+v", kept.Surviving)
	}
	want := []OutOfScopeMutant{
		{File: "src/b.ts", Line: 3, Op: "ConditionalExpression", Language: "typescript"},
		{File: "src/b.ts", Line: 8, Op: "ArithmeticOperator", Language: "typescript"},
	}
	if !reflect.DeepEqual(dropped, want) {
		t.Errorf("dropped:\n got %+v\nwant %+v", dropped, want)
	}
}

// TestFilterReportDegenerateInputs: an in-scope file the tool never mutated
// invents no row, and an empty scope keeps nothing at all.
func TestFilterReportDegenerateInputs(t *testing.T) {
	r := reportOf("gremlins", map[string]mutation.FileStat{"a.go": {Killed: 1}})

	kept, _ := FilterReport(r, scopeOf("a.go", "never-mutated.go"), "go")
	if len(kept.Files) != 1 {
		t.Errorf("a file with no mutants must not invent a row: %+v", kept.Files)
	}

	empty, dropped := FilterReport(
		reportOf("gremlins", map[string]mutation.FileStat{"a.go": {Killed: 1, Survived: 1}},
			mutation.Mutant{File: "a.go", Line: 1}),
		NewScope(ScopeInput{}), "go")
	if empty.Killed != 0 || empty.Survived != 0 || empty.Score != 0 {
		t.Errorf("empty scope must keep nothing: %+v", empty)
	}
	if len(dropped) != 1 {
		t.Errorf("everything is dropped under an empty scope: %+v", dropped)
	}

	// A nil scope is "not configured", not "nothing in scope": pass-through.
	same, none := FilterReport(r, nil, "go")
	if same != r || none != nil {
		t.Errorf("nil scope must be a pass-through, got %+v / %+v", same, none)
	}
	if got, _ := FilterReport(nil, scopeOf("a.go"), "go"); got != nil {
		t.Errorf("nil report must not be invented: %+v", got)
	}
}

// gremlinsPayload / strykerPayload express the SAME logical mutant set in the
// two tools' own report formats: one kill and one survivor in a file the phase
// touched, one kill and one survivor in a file it didn't.
const gremlinsPayload = `{
  "go_module": "example.com/m",
  "files": [
    {"file_name": "pkg/touched.go", "mutations": [
      {"line":10,"column":1,"type":"CONDITIONALS_NEGATION","status":"KILLED"},
      {"line":20,"column":1,"type":"ARITHMETIC_BASE","status":"LIVED"}
    ]},
    {"file_name": "pkg/untouched.go", "mutations": [
      {"line":10,"column":1,"type":"CONDITIONALS_NEGATION","status":"KILLED"},
      {"line":30,"column":1,"type":"ARITHMETIC_BASE","status":"LIVED"}
    ]}
  ]
}`

const strykerPayload = `{
  "schemaVersion": "1",
  "files": {
    "src/touched.ts": {"language":"typescript","source":"...","mutants":[
      {"id":"1","mutatorName":"ConditionalExpression","replacement":"true","status":"Killed",
       "location":{"start":{"line":10,"column":1},"end":{"line":10,"column":2}}},
      {"id":"2","mutatorName":"ArithmeticOperator","replacement":"-","status":"Survived",
       "location":{"start":{"line":20,"column":1},"end":{"line":20,"column":2}}}
    ]},
    "src/untouched.ts": {"language":"typescript","source":"...","mutants":[
      {"id":"3","mutatorName":"ConditionalExpression","replacement":"true","status":"Killed",
       "location":{"start":{"line":10,"column":1},"end":{"line":10,"column":2}}},
      {"id":"4","mutatorName":"ArithmeticOperator","replacement":"-","status":"Survived",
       "location":{"start":{"line":30,"column":1},"end":{"line":30,"column":2}}}
    ]}
  }
}`

// TestFilterReportIsAdapterAgnostic runs the same logical mutant set through
// both real parsers — a package-granular tool and a file-granular one — and
// requires identical attribution. A filter that special-cases one tool's path
// shape produces different kept or dropped counts here.
func TestFilterReportIsAdapterAgnostic(t *testing.T) {
	gr, err := mutation.ParseGremlinsJSON([]byte(gremlinsPayload))
	if err != nil {
		t.Fatal(err)
	}
	st, err := mutation.ParseStrykerJSON([]byte(strykerPayload))
	if err != nil {
		t.Fatal(err)
	}
	scope := scopeOf("pkg/touched.go", "src/touched.ts")

	keptGo, dropGo := FilterReport(gr, scope, "go")
	keptTS, dropTS := FilterReport(st, scope, "typescript")

	for _, tc := range []struct {
		lang    string
		kept    *mutation.Report
		dropped []OutOfScopeMutant
	}{
		{"go", keptGo, dropGo},
		{"typescript", keptTS, dropTS},
	} {
		if tc.kept.Killed != 1 || tc.kept.Survived != 1 || tc.kept.Score != 0.5 {
			t.Errorf("%s kept: killed=%d survived=%d score=%v want 1/1/0.50",
				tc.lang, tc.kept.Killed, tc.kept.Survived, tc.kept.Score)
		}
		if len(tc.dropped) != 1 {
			t.Errorf("%s dropped %d want 1: %+v", tc.lang, len(tc.dropped), tc.dropped)
		}
	}

	// The same set, this time through a multi-leg RunScoped, proving the
	// file-granular leg takes the same path when it runs alongside another.
	tests, err := RunScoped("p", []string{"pkg/touched.go", "src/touched.ts"},
		[]mutation.Adapter{
			&fakeAdapter{name: "gremlins", supportsExt: []string{".go"}, report: gr},
			&fakeAdapter{name: "stryker", supportsExt: []string{".ts"}, report: st},
		}, scope)
	if err != nil {
		t.Fatal(err)
	}
	if len(tests.Languages) != 2 {
		t.Fatalf("expected two legs: %+v", tests.Languages)
	}
	for _, lr := range tests.Languages {
		if lr.Mutation.Killed != 1 || lr.Mutation.Survived != 1 {
			t.Errorf("%s leg: killed=%d survived=%d want 1/1",
				lr.Tool, lr.Mutation.Killed, lr.Mutation.Survived)
		}
	}
	if len(tests.OutOfScope) != 2 {
		t.Errorf("both legs' dropped survivors collect at top level: %+v", tests.OutOfScope)
	}
	langs := map[string]bool{}
	for _, d := range tests.OutOfScope {
		langs[d.Language] = true
	}
	if !langs["go"] || !langs["typescript"] {
		t.Errorf("dropped survivors lost their language: %+v", tests.OutOfScope)
	}
}

// TestRunScopedFailedLegSurvivesFiltering: a leg whose adapter errored has no
// report at all. It must still be recorded — the record-and-continue contract
// — and must not nil-panic the filter on the way through.
func TestRunScopedFailedLegSurvivesFiltering(t *testing.T) {
	broken := &fakeAdapter{name: "stryker", supportsExt: []string{".ts"}, err: errAdapterBoom}
	ok := &fakeAdapter{name: "gremlins", supportsExt: []string{".go"},
		report: reportOf("gremlins", map[string]mutation.FileStat{"a.go": {Killed: 1}})}

	tests, err := RunScoped("p", []string{"a.ts", "a.go"},
		[]mutation.Adapter{broken, ok}, scopeOf("a.go"))
	if err != nil {
		t.Fatal(err)
	}
	if len(tests.Languages) != 2 {
		t.Fatalf("a failed leg must still be recorded: %+v", tests.Languages)
	}
	for _, lr := range tests.Languages {
		if lr.Tool == "stryker" && lr.Error == "" {
			t.Error("failed leg lost its error")
		}
		if lr.Tool == "gremlins" && lr.Mutation.Killed != 1 {
			t.Errorf("healthy leg was discarded: %+v", lr.Mutation)
		}
	}
}
