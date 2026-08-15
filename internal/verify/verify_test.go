package verify

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/mutation"
	"github.com/Rivil/dross/internal/remote"
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
	blockingCount, noteCount := 0, 0
	for _, f := range v.Findings {
		switch f.Severity {
		case "BLOCKING":
			blockingCount++
		case "NOTE":
			noteCount++
		}
	}
	// The in-scope survivor is BLOCKING, not a FLAG: an unclassified survivor
	// inside the phase's own diff is the mutation leg's fail lever.
	if blockingCount != 1 || noteCount != 1 {
		t.Errorf("expected 1 BLOCKING (surviving mutant) + 1 NOTE (skip); got blocking=%d notes=%d",
			blockingCount, noteCount)
	}
	if v.Summary.UnclassifiedInScope != 1 {
		t.Errorf("UnclassifiedInScope = %d, want 1", v.Summary.UnclassifiedInScope)
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

// TestFilterReportDropsTestdataEntirely: a fixture under testdata/ leaves
// through NEITHER exit. Not kept — the phase does not answer for a file Go
// excludes from `./...` and never compiles into the binary. And not dropped
// either: an OutOfScopeMutant is backlog, so routing it there would resurface
// it as an unclassified FLAG for someone to accept or route, and there is
// nothing to accept. Both halves are asserted because dropping it from
// Surviving alone still leaves it re-listed one findings-section down, which is
// the drain-don't-relist rule with extra steps.
//
// The scope is asked to CONTAIN the fixture, so the exclusion cannot pass by
// accident: without the rule this survivor is in scope and BLOCKING, which is
// precisely how this repo's own ceiling fixture failed its phase.
func TestFilterReportDropsTestdataEntirely(t *testing.T) {
	r := reportOf("gremlins",
		map[string]mutation.FileStat{
			"internal/mutation/gremlins.go":                 {Killed: 1, Survived: 1},
			"internal/mutation/testdata/ceiling/ceiling.go": {Survived: 3},
		},
		mutation.Mutant{File: "internal/mutation/gremlins.go", Line: 4, Op: "CONDITIONALS_NEGATION"},
		mutation.Mutant{File: "internal/mutation/testdata/ceiling/ceiling.go", Line: 20, Op: "CONDITIONALS_BOUNDARY"},
	)
	s := scopeOf("internal/mutation/gremlins.go", "internal/mutation/testdata/ceiling/ceiling.go")

	kept, dropped := FilterReport(r, s, "go")

	if len(kept.Surviving) != 1 || kept.Surviving[0].File != "internal/mutation/gremlins.go" {
		t.Errorf("a testdata fixture was kept as this phase's debt: %+v", kept.Surviving)
	}
	if len(dropped) != 0 {
		t.Errorf("a testdata fixture was relisted as out-of-scope backlog: %+v", dropped)
	}
	// The stat row goes with the survivor: a denominator counting mutants the
	// survivor list has dropped is the same inconsistency one layer down. With
	// the fixture's 3 survived rolled in, the score would be 1/5 = 0.20.
	if _, ok := kept.Files["internal/mutation/testdata/ceiling/ceiling.go"]; ok {
		t.Errorf("the fixture's stat row survived filtering: %+v", kept.Files)
	}
	if kept.Survived != 1 || kept.Killed != 1 || kept.Score != 0.5 {
		t.Errorf("score computed over the fixture: killed=%d survived=%d score=%v, want 1/1/0.5",
			kept.Killed, kept.Survived, kept.Score)
	}
}

// TestIsTestdataPathIsSegmentScoped pins the rule's narrowness at the shared
// implementation both `dross verify` and `dross survivor drain` now call. A
// substring match would silently exclude ordinary packages from the gate.
func TestIsTestdataPathIsSegmentScoped(t *testing.T) {
	for _, tc := range []struct {
		file string
		want bool
	}{
		{"internal/mutation/testdata/ceiling/ceiling.go", true},
		{"testdata/x.go", true},
		{"internal/testdata/deep/nested/y.go", true},
		{"internal/cmd/doctor.go", false},
		{"internal/testdatabase/store.go", false},
		{"internal/mytestdata.go", false},
	} {
		if got := IsTestdataPath(tc.file); got != tc.want {
			t.Errorf("IsTestdataPath(%q) = %v, want %v", tc.file, got, tc.want)
		}
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

// --- persistence, status and the single NOTE --------------------------------

// recordingAdapter remembers what it was handed, so a change that narrows what
// gets MUTATED (rather than what gets ATTRIBUTED) is caught.
type recordingAdapter struct {
	fakeAdapter
	got []string
}

func (r *recordingAdapter) Run(files []string) (*mutation.Report, error) {
	r.got = append([]string(nil), files...)
	return r.fakeAdapter.Run(files)
}

// TestRunScopedPersistsOutOfScopeSurvivors: filtered survivors appear in the
// top-level list and NOWHERE in the per-language surviving lists. Both halves
// are asserted — an implementation that copies rather than moves them would
// pass the first and fail the second.
func TestRunScopedPersistsOutOfScopeSurvivors(t *testing.T) {
	rep := reportOf("gremlins", map[string]mutation.FileStat{
		"a.go": {Survived: 1},
		"b.go": {Survived: 1},
	},
		mutation.Mutant{File: "a.go", Line: 1, Op: "CONDITIONALS_NEGATION"},
		mutation.Mutant{File: "b.go", Line: 2, Op: "ARITHMETIC_BASE"},
	)
	tests, err := RunScoped("p", []string{"a.go"},
		[]mutation.Adapter{&fakeAdapter{name: "gremlins", supportsExt: []string{".go"}, report: rep}},
		scopeOf("a.go"))
	if err != nil {
		t.Fatal(err)
	}

	want := []OutOfScopeMutant{{File: "b.go", Line: 2, Op: "ARITHMETIC_BASE", Language: "go"}}
	if !reflect.DeepEqual(tests.OutOfScope, want) {
		t.Errorf("out_of_scope:\n got %+v\nwant %+v", tests.OutOfScope, want)
	}
	for _, lr := range tests.Languages {
		for _, m := range lr.Mutation.Surviving {
			if m.File == "b.go" {
				t.Errorf("filtered survivor still listed under languages[].mutation.surviving: %+v", m)
			}
		}
	}
}

// TestRunScopedKeepsLanguageOnDroppedSurvivors: a merged list must still say
// which adapter produced each entry, or a two-language repo's filtered set is
// unattributable.
func TestRunScopedKeepsLanguageOnDroppedSurvivors(t *testing.T) {
	goRep := reportOf("gremlins", map[string]mutation.FileStat{"x.go": {Survived: 1}},
		mutation.Mutant{File: "x.go", Line: 1})
	tsRep := reportOf("stryker", map[string]mutation.FileStat{"x.ts": {Survived: 1}},
		mutation.Mutant{File: "x.ts", Line: 1})

	tests, err := RunScoped("p", []string{"x.go", "x.ts"}, []mutation.Adapter{
		&fakeAdapter{name: "gremlins", supportsExt: []string{".go"}, report: goRep},
		&fakeAdapter{name: "stryker", supportsExt: []string{".ts"}, report: tsRep},
	}, scopeOf("nothing-here.go"))
	if err != nil {
		t.Fatal(err)
	}

	byLang := map[string]string{}
	for _, d := range tests.OutOfScope {
		byLang[d.Language] = d.File
	}
	if byLang["go"] != "x.go" || byLang["typescript"] != "x.ts" {
		t.Errorf("language attribution lost: %+v", tests.OutOfScope)
	}
}

// TestTestsScopeRoundTrip: the scope block must survive a write/reload. A
// struct missing json tags writes fine and comes back empty, so the reload is
// the half that actually pins it.
func TestTestsScopeRoundTrip(t *testing.T) {
	scope := NewScope(ScopeInput{
		Root:     "/repo",
		Base:     "abc123def456",
		Git:      []string{"a.go"},
		Recorded: []string{"b.go"},
		Hunks:    map[string][]Range{"a.go": {{Start: 10, End: 12}}},
	})
	orig := &Tests{Phase: "p", Scope: scope, OutOfScope: []OutOfScopeMutant{
		{File: "z.go", Line: 3, Op: "ARITHMETIC_BASE", Language: "go"},
	}}

	path := filepath.Join(t.TempDir(), "tests.json")
	if err := orig.Save(path); err != nil {
		t.Fatal(err)
	}
	got, err := LoadTests(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Scope == nil {
		t.Fatal("scope block did not survive the reload")
	}
	if !reflect.DeepEqual(got.Scope.Files, []string{"a.go", "b.go"}) {
		t.Errorf("files: %v", got.Scope.Files)
	}
	if got.Scope.Base != "abc123def456" {
		t.Errorf("base: %q", got.Scope.Base)
	}
	if got.Scope.Source != SourceUnion {
		t.Errorf("source: %q", got.Scope.Source)
	}
	if !reflect.DeepEqual(got.Scope.Hunks["a.go"], []Range{{10, 12}}) {
		t.Errorf("hunks: %+v", got.Scope.Hunks)
	}
	// The lookup index is not persisted; a reloaded scope must still answer.
	if !got.Scope.Contains("a.go") {
		t.Error("reloaded scope cannot answer Contains")
	}
	if !reflect.DeepEqual(got.OutOfScope, orig.OutOfScope) {
		t.Errorf("out_of_scope: %+v", got.OutOfScope)
	}
}

// TestTestsDegradedScopeIsReadableAfterTheFact: the reason a scope was partial
// is the difference between "this run measured little" and "this run measured
// little BECAUSE the base was missing". Only the second is actionable.
func TestTestsDegradedScopeIsReadableAfterTheFact(t *testing.T) {
	scope := NewScope(ScopeInput{
		Recorded: []string{"a.go"},
		Degraded: []string{"changes.json records no base branch"},
	})
	path := filepath.Join(t.TempDir(), "tests.json")
	if err := (&Tests{Phase: "p", Scope: scope}).Save(path); err != nil {
		t.Fatal(err)
	}
	got, err := LoadTests(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Scope.Source != SourceChangesOnly {
		t.Errorf("source: %q want %q", got.Scope.Source, SourceChangesOnly)
	}
	joined := strings.Join(got.Scope.Degraded, "\n")
	if !strings.Contains(joined, "no base branch") {
		t.Errorf("degraded reason lost: %v", got.Scope.Degraded)
	}
}

// TestSkeletonScoresFromFilteredReport: the skeleton must read the FILTERED
// report. A Skeleton still reading the package-wide numbers reports 7/7 here
// where the phase's own slice is 2/2.
func TestSkeletonScoresFromFilteredReport(t *testing.T) {
	rep := reportOf("gremlins", map[string]mutation.FileStat{
		"a.go":         {Killed: 2},
		"untouched.go": {Killed: 5},
	})
	tests, err := RunScoped("p", []string{"a.go"},
		[]mutation.Adapter{&fakeAdapter{name: "gremlins", supportsExt: []string{".go"}, report: rep}},
		scopeOf("a.go"))
	if err != nil {
		t.Fatal(err)
	}
	v := Skeleton(tests, []string{"c-1"})

	if v.Summary.MutantsInScope != 2 {
		t.Errorf("mutants_in_scope = %d want 2", v.Summary.MutantsInScope)
	}
	if v.Summary.MutantsKilled != 2 {
		t.Errorf("mutants_killed = %d want 2 (not 7)", v.Summary.MutantsKilled)
	}
	if v.Summary.MutationScore != 1.0 {
		t.Errorf("score = %v want 1.00", v.Summary.MutationScore)
	}
	if v.Summary.MutationStatus != MutationMeasured {
		t.Errorf("status = %q want %q", v.Summary.MutationStatus, MutationMeasured)
	}
}

// TestSkeletonOutOfScopeStatus: when every mutant landed outside the phase,
// the status is its own value. All three alternatives are asserted against,
// because collapsing it into unmeasurable or measured-0.00 is exactly how a
// vacuous run gets to look like a settled one.
func TestSkeletonOutOfScopeStatus(t *testing.T) {
	rep := reportOf("gremlins", map[string]mutation.FileStat{"untouched.go": {Killed: 3, Survived: 1}},
		mutation.Mutant{File: "untouched.go", Line: 2})
	tests, err := RunScoped("p", []string{"a.go"},
		[]mutation.Adapter{&fakeAdapter{name: "gremlins", supportsExt: []string{".go"}, report: rep}},
		scopeOf("a.go"))
	if err != nil {
		t.Fatal(err)
	}
	v := Skeleton(tests, []string{"c-1"})

	if v.Summary.MutationStatus != MutationOutOfScope {
		t.Errorf("status = %q want %q", v.Summary.MutationStatus, MutationOutOfScope)
	}
	if v.Summary.MutationStatus == MutationMeasured || v.Summary.MutationStatus == MutationUnmeasurable {
		t.Error("out-of-scope must not collapse into measured or unmeasurable")
	}
	if v.Summary.MutantsInScope != 0 {
		t.Errorf("mutants_in_scope = %d want 0", v.Summary.MutantsInScope)
	}
	if v.Summary.MutationScore != 0 {
		t.Errorf("score = %v want 0 (and read via the status, not the number)", v.Summary.MutationScore)
	}
}

// TestSkeletonOneNoteNoFlagFlood: seven filtered survivors plus one real one
// produce exactly ONE NOTE carrying the count, and exactly ONE FLAG — the
// phase's own. Counts are asserted rather than presence: a loop over the
// unfiltered set yields eight FLAGs and still "contains" the right one.
func TestSkeletonOneNoteNoFlagFlood(t *testing.T) {
	rows := map[string]mutation.FileStat{"touched.go": {Survived: 1}, "other.go": {Survived: 7}}
	surviving := []mutation.Mutant{{File: "touched.go", Line: 1, Op: "X"}}
	for i := 0; i < 7; i++ {
		surviving = append(surviving, mutation.Mutant{File: "other.go", Line: i + 1, Op: "Y"})
	}
	rep := reportOf("gremlins", rows, surviving...)

	tests, err := RunScoped("p", []string{"touched.go"},
		[]mutation.Adapter{&fakeAdapter{name: "gremlins", supportsExt: []string{".go"}, report: rep}},
		scopeOf("touched.go"))
	if err != nil {
		t.Fatal(err)
	}
	v := Skeleton(tests, []string{"c-1"})

	var notes, blocking []Finding
	for _, f := range v.Findings {
		switch f.Severity {
		case "NOTE":
			notes = append(notes, f)
		case "BLOCKING":
			blocking = append(blocking, f)
		}
	}
	if len(blocking) != 1 {
		t.Errorf("expected exactly 1 BLOCKING (the phase's own survivor), got %d: %+v", len(blocking), blocking)
	}
	if len(notes) != 1 {
		t.Fatalf("expected exactly 1 NOTE, got %d: %+v", len(notes), notes)
	}
	if !strings.Contains(notes[0].Text, "7") {
		t.Errorf("the NOTE must carry the filtered count: %q", notes[0].Text)
	}

	// Nothing filtered → no such NOTE at all, so a clean run stays clean.
	clean := reportOf("gremlins", map[string]mutation.FileStat{"touched.go": {Killed: 1}})
	cleanTests, err := RunScoped("p", []string{"touched.go"},
		[]mutation.Adapter{&fakeAdapter{name: "gremlins", supportsExt: []string{".go"}, report: clean}},
		scopeOf("touched.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range Skeleton(cleanTests, nil).Findings {
		if strings.Contains(f.Text, "out-of-scope survivor") {
			t.Errorf("no survivors were filtered; NOTE should be absent: %q", f.Text)
		}
	}
}

// TestSkeletonDegradedScopeRaisesNoFlag: a missing base is a bookkeeping gap.
// It has to be visible, but failing the phase for it would punish the wrong
// thing entirely.
func TestSkeletonDegradedScopeRaisesNoFlag(t *testing.T) {
	scope := NewScope(ScopeInput{
		Recorded: []string{"a.go"},
		Degraded: []string{"could not resolve merge-base of \"base\" and HEAD"},
	})
	rep := reportOf("gremlins", map[string]mutation.FileStat{"a.go": {Killed: 1}})
	tests, err := RunScoped("p", []string{"a.go"},
		[]mutation.Adapter{&fakeAdapter{name: "gremlins", supportsExt: []string{".go"}, report: rep}},
		scope)
	if err != nil {
		t.Fatal(err)
	}

	v := Skeleton(tests, nil)
	var sawReason bool
	for _, f := range v.Findings {
		if f.Severity == "FLAG" {
			t.Errorf("a degraded scope must not FLAG: %q", f.Text)
		}
		if strings.Contains(f.Text, "merge-base") {
			sawReason = true
		}
	}
	if !sawReason {
		t.Errorf("the degradation reason must reach verify.toml: %+v", v.Findings)
	}
}

// TestSkeletonSampleSizeSignal: mutants_in_scope reaches verify.toml's
// [summary] and survives the round-trip. It is the small_denominator_gate
// lock's substitute for moving the threshold — a 0.67 over 3 has to read as a
// small sample rather than as a near-miss.
func TestSkeletonSampleSizeSignal(t *testing.T) {
	rep := reportOf("gremlins", map[string]mutation.FileStat{"a.go": {Killed: 2, Survived: 1}})
	tests, err := RunScoped("p", []string{"a.go"},
		[]mutation.Adapter{&fakeAdapter{name: "gremlins", supportsExt: []string{".go"}, report: rep}},
		scopeOf("a.go"))
	if err != nil {
		t.Fatal(err)
	}
	v := Skeleton(tests, nil)
	if v.Summary.MutantsInScope != 3 {
		t.Fatalf("mutants_in_scope = %d want 3", v.Summary.MutantsInScope)
	}

	path := filepath.Join(t.TempDir(), "verify.toml")
	if err := v.Save(path); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "mutants_in_scope = 3") {
		t.Errorf("verify.toml [summary] missing the sample size:\n%s", body)
	}
}

// TestRunScopedDoesNotNarrowAdapterInvocation: scoping filters what is
// ATTRIBUTED, never what is MUTATED. An implementation that passed the scope
// down to the adapter would change which mutants exist — a different, and
// much quieter, way to make survivors disappear.
func TestRunScopedDoesNotNarrowAdapterInvocation(t *testing.T) {
	rec := &recordingAdapter{fakeAdapter: fakeAdapter{
		name:        "gremlins",
		supportsExt: []string{".go"},
		report:      reportOf("gremlins", map[string]mutation.FileStat{"a.go": {Killed: 1}}),
	}}
	files := []string{"a.go", "b.go"}

	if _, err := RunScoped("p", files, []mutation.Adapter{rec}, scopeOf("a.go")); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(rec.got, files) {
		t.Errorf("adapter was handed %v, want the full list %v", rec.got, files)
	}
}

// lifecycleTests builds a Tests with one in-scope survivor and three
// out-of-scope ones, classified with the given acceptance/routing maps.
func lifecycleTests(t *testing.T, accepted, routed map[string]string) *Tests {
	t.Helper()
	tests := &Tests{
		Phase: "p",
		Languages: []LanguageRun{{
			Name: "go", Tool: "gremlins",
			Mutation: &mutation.Report{
				Killed: 3, Survived: 4, Score: 3.0 / 7.0,
				Surviving: []mutation.Mutant{{File: "in.go", Line: 10, Op: "OP"}},
			},
		}},
		OutOfScope: []OutOfScopeMutant{
			{File: "routed.go", Line: 20, Op: "OP", Language: "go"},
			{File: "accept.go", Line: 30, Op: "OP", Language: "go"},
			{File: "bare.go", Line: 40, Op: "OP", Language: "go"},
		},
	}
	ApplyLifecycle(tests, accepted, routed, fixtureIdentifier())
	return tests
}

func findingsBySeverity(v *Verify, severity string) []Finding {
	var out []Finding
	for _, f := range v.Findings {
		if f.Severity == severity {
			out = append(out, f)
		}
	}
	return out
}

// TestSkeletonAcceptanceSuppressesExactlyOneFlag is c-2 at the report surface,
// and it pins the suppression to the acceptance itself: with the acceptance the
// survivor is silent, and deleting ONLY that acceptance brings back exactly one
// FLAG naming it. A suppression that isn't attributable to a recorded reason is
// indistinguishable from a survivor being dropped.
func TestSkeletonAcceptanceSuppressesExactlyOneFlag(t *testing.T) {
	ti := fixtureIdentifier()
	acceptKey := keyOf(t, ti, "accept.go", 30, "OP")
	routing := map[string]string{keyOf(t, ti, "routed.go", 20, "OP"): "board-sync-truth"}

	withAcceptance := Skeleton(lifecycleTests(t, map[string]string{acceptKey: "ceiling"}, routing), []string{"c-1"})
	without := Skeleton(lifecycleTests(t, nil, routing), []string{"c-1"})

	for _, f := range withAcceptance.Findings {
		if strings.Contains(f.Text, "accept.go") {
			t.Errorf("accepted survivor still reported: %+v", f)
		}
	}

	var named []Finding
	for _, f := range findingsBySeverity(without, "FLAG") {
		if strings.Contains(f.Text, "accept.go") {
			named = append(named, f)
		}
	}
	if len(named) != 1 {
		t.Fatalf("deleting the acceptance produced %d FLAGs for accept.go, want exactly 1: %+v", len(named), named)
	}
}

// TestSkeletonInScopeSurvivorRendersItsNote: a note that never reaches
// verify.toml explains nothing. The case that matters is the ambiguous
// acceptance — the survivor re-emits as in-diff, and its FLAG is the only place
// the user learns why the acceptance they recorded did not silence it. Without
// the note the FLAG is indistinguishable from a survivor nobody ever accepted.
func TestSkeletonInScopeSurvivorRendersItsNote(t *testing.T) {
	ti := fixtureIdentifier()
	ambiguous := &Tests{
		Phase: "p",
		Languages: []LanguageRun{{
			Name: "go", Tool: "gremlins",
			Mutation: &mutation.Report{
				Killed: 3, Survived: 1, Score: 0.75,
				Surviving: []mutation.Mutant{{File: "dup.go", Line: 50, Op: "OP"}},
			},
		}},
	}
	ApplyLifecycle(ambiguous, map[string]string{keyOf(t, ti, "dup.go", 50, "OP"): "ceiling"}, nil, ti)

	blocking := findingsBySeverity(Skeleton(ambiguous, []string{"c-1"}), "BLOCKING")
	var named []Finding
	for _, f := range blocking {
		if strings.Contains(f.Text, "dup.go:50") {
			named = append(named, f)
		}
	}
	if len(named) != 1 {
		t.Fatalf("want exactly 1 BLOCKING for the re-emitted survivor, got %d: %+v", len(named), named)
	}
	if !strings.Contains(named[0].Text, "— accepted key is ambiguous") {
		t.Errorf("BLOCKING = %q, must carry the survivor's note saying why the acceptance was withheld", named[0].Text)
	}

	// The other half: a survivor with no note renders with no dangling
	// separator, so the note is genuinely conditional rather than always-on.
	plain := findingsBySeverity(Skeleton(lifecycleTests(t, nil, nil), []string{"c-1"}), "BLOCKING")
	for _, f := range plain {
		if strings.Contains(f.Text, "in.go:10") && strings.Contains(f.Text, "—") {
			t.Errorf("note-less survivor rendered a separator with nothing after it: %q", f.Text)
		}
	}
}

// TestSkeletonAcceptanceDoesNotTouchTheScore: an acceptance suppresses a
// finding, never a count. If accepted survivors folded out of the denominator, a
// phase could raise its mutation score by declaring its survivors acceptable.
func TestSkeletonAcceptanceDoesNotTouchTheScore(t *testing.T) {
	ti := fixtureIdentifier()
	acceptKey := keyOf(t, ti, "in.go", 10, "OP")

	before := Skeleton(lifecycleTests(t, nil, nil), []string{"c-1"})
	after := Skeleton(lifecycleTests(t, map[string]string{acceptKey: "ceiling"}, nil), []string{"c-1"})

	if before.Summary.MutantsSurvived != after.Summary.MutantsSurvived {
		t.Errorf("mutants_survived changed from %d to %d after an acceptance",
			before.Summary.MutantsSurvived, after.Summary.MutantsSurvived)
	}
	if before.Summary.MutationScore != after.Summary.MutationScore {
		t.Errorf("mutation score changed from %v to %v after an acceptance",
			before.Summary.MutationScore, after.Summary.MutationScore)
	}
}

// TestSkeletonOutOfScopeIsLifecycleAware is c-7: an out-of-scope survivor gets
// the same treatment as an in-scope one. Unclassified means one actionable FLAG
// naming both escapes; routed means a NOTE naming the destination; accepted
// means silence — and the one-line filtered NOTE counts only what is left.
func TestSkeletonOutOfScopeIsLifecycleAware(t *testing.T) {
	ti := fixtureIdentifier()
	v := Skeleton(lifecycleTests(t,
		map[string]string{keyOf(t, ti, "accept.go", 30, "OP"): "ceiling"},
		map[string]string{keyOf(t, ti, "routed.go", 20, "OP"): "board-sync-truth"},
	), []string{"c-1"})

	var bareFlags, routedNotes, filteredNotes []Finding
	for _, f := range v.Findings {
		switch {
		case f.Severity == "FLAG" && strings.Contains(f.Text, "bare.go"):
			bareFlags = append(bareFlags, f)
		case f.Severity == "NOTE" && strings.Contains(f.Text, "routed.go"):
			routedNotes = append(routedNotes, f)
		case strings.Contains(f.Text, "out-of-scope survivor(s)"):
			filteredNotes = append(filteredNotes, f)
		}
		if strings.Contains(f.Text, "accept.go") {
			t.Errorf("accepted out-of-scope survivor still reported: %+v", f)
		}
	}

	if len(bareFlags) != 1 {
		t.Errorf("unrouted, unaccepted out-of-scope survivor produced %d FLAGs, want 1: %+v", len(bareFlags), bareFlags)
	} else {
		for _, escape := range []string{"dross survivor accept", "dross survivor route"} {
			if !strings.Contains(bareFlags[0].Text, escape) {
				t.Errorf("unclassified FLAG must name %q: %q", escape, bareFlags[0].Text)
			}
		}
	}
	if len(routedNotes) != 1 || !strings.Contains(routedNotes[0].Text, "board-sync-truth") {
		t.Errorf("routed out-of-scope survivor should be one NOTE naming its destination, got %+v", routedNotes)
	}
	if len(filteredNotes) != 1 {
		t.Fatalf("want exactly 1 filtered-count NOTE, got %d: %+v", len(filteredNotes), filteredNotes)
	}
	// 3 out-of-scope entries, 1 accepted → the remainder is 2, not 3.
	if !strings.Contains(filteredNotes[0].Text, "filtered 2 out-of-scope") {
		t.Errorf("filtered NOTE must count the post-lifecycle remainder, got %q", filteredNotes[0].Text)
	}
}

// TestSkeletonUnclassifiedNoteFallsAwayWhenDrained: once every out-of-scope
// survivor is accepted, the one-line NOTE disappears entirely. That is the
// ratchet — a drained backlog reads as drained, not as "0 remaining".
func TestSkeletonUnclassifiedNoteFallsAwayWhenDrained(t *testing.T) {
	ti := fixtureIdentifier()
	accepted := map[string]string{
		keyOf(t, ti, "routed.go", 20, "OP"): "r",
		keyOf(t, ti, "accept.go", 30, "OP"): "a",
		keyOf(t, ti, "bare.go", 40, "OP"):   "b",
	}
	v := Skeleton(lifecycleTests(t, accepted, nil), []string{"c-1"})
	for _, f := range v.Findings {
		if strings.Contains(f.Text, "out-of-scope") {
			t.Errorf("fully drained out-of-scope set still reported: %+v", f)
		}
	}
}

// inScopeTests builds a Tests whose only survivor is in-scope, at the given
// file:line, with the given score.
func inScopeTests(t *testing.T, file string, line int, score float64, accepted, routed map[string]string) *Tests {
	t.Helper()
	tests := &Tests{
		Phase: "p",
		Languages: []LanguageRun{{
			Name: "go", Tool: "gremlins",
			Mutation: &mutation.Report{
				Killed: 19, Survived: 1, Score: score,
				Surviving: []mutation.Mutant{{File: file, Line: line, Op: "OP"}},
			},
		}},
	}
	ApplyLifecycle(tests, accepted, routed, fixtureIdentifier())
	return tests
}

// TestUnclassifiedInScopeCountsOnlyUndisposedSurvivors is c-6's core. The
// mutation leg's fail lever is an absolute count of in-scope survivors with no
// disposition — not a ratio. A disposition is a reason (accepted) or a
// destination (routed); anything else is debt this phase is leaving behind.
func TestUnclassifiedInScopeCountsOnlyUndisposedSurvivors(t *testing.T) {
	ti := fixtureIdentifier()
	key := keyOf(t, ti, "in.go", 10, "OP")

	cases := []struct {
		name         string
		accepted     map[string]string
		routed       map[string]string
		wantCount    int
		wantBlocking int
		wantNote     int
	}{
		{
			name:         "undisposed in-diff survivor blocks",
			wantCount:    1,
			wantBlocking: 1,
		},
		{
			name:      "accepted survivor is silent",
			accepted:  map[string]string{key: "unreachable through the CLI"},
			wantCount: 0,
		},
		{
			name:      "routed survivor keeps its NOTE and does not block",
			routed:    map[string]string{key: "later-phase"},
			wantCount: 0,
			wantNote:  1,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v := Skeleton(inScopeTests(t, "in.go", 10, 0.95, tc.accepted, tc.routed), []string{"c-1"})

			if v.Summary.UnclassifiedInScope != tc.wantCount {
				t.Errorf("UnclassifiedInScope = %d, want %d", v.Summary.UnclassifiedInScope, tc.wantCount)
			}
			blocking := findingsBySeverity(v, "BLOCKING")
			if len(blocking) != tc.wantBlocking {
				t.Fatalf("got %d BLOCKING findings, want %d: %+v", len(blocking), tc.wantBlocking, blocking)
			}
			if tc.wantBlocking == 1 && !strings.Contains(blocking[0].Text, "in.go:10 (OP)") {
				t.Errorf("BLOCKING = %q, must name file:line (op) so the survivor is addressable", blocking[0].Text)
			}
			if got := len(findingsBySeverity(v, "NOTE")); got != tc.wantNote {
				t.Errorf("got %d NOTE findings, want %d", got, tc.wantNote)
			}
		})
	}
}

// TestHighScoreCannotBuyAPass is why the lever moved off the ratio. A phase
// that adds a pile of killed mutants can bury a live one and still clear any
// cutoff — 0.95 here, comfortably over the old 0.80. The absolute count is
// immune to that arithmetic.
func TestHighScoreCannotBuyAPass(t *testing.T) {
	v := Skeleton(inScopeTests(t, "in.go", 10, 0.95, nil, nil), []string{"c-1"})

	if v.Summary.MutationScore < 0.9 {
		t.Fatalf("fixture premise broken: score = %v, want a comfortably passing one", v.Summary.MutationScore)
	}
	if v.Summary.UnclassifiedInScope != 1 {
		t.Errorf("UnclassifiedInScope = %d, want 1", v.Summary.UnclassifiedInScope)
	}
	if got := findingsBySeverity(v, "BLOCKING"); len(got) != 1 {
		t.Errorf("a 0.95 score suppressed the BLOCKING finding: %+v", got)
	}
}

// TestUnresolvedInDiffIdentityStillBlocks: a survivor whose identity could not
// be resolved is still in this phase's diff and still undisposed. If an
// unreadable key let it fall out of the count, the cheapest way to pass the
// gate would be to make the survivor unidentifiable.
func TestUnresolvedInDiffIdentityStillBlocks(t *testing.T) {
	// "unknown.go" is absent from fixtureIdentifier, so Identify errors.
	v := Skeleton(inScopeTests(t, "unknown.go", 99, 0.95, nil, nil), []string{"c-1"})

	if v.Summary.UnclassifiedInScope != 1 {
		t.Errorf("UnclassifiedInScope = %d, want 1 — an unresolvable key must not escape the count", v.Summary.UnclassifiedInScope)
	}
	blocking := findingsBySeverity(v, "BLOCKING")
	if len(blocking) != 1 {
		t.Fatalf("got %d BLOCKING findings, want 1: %+v", len(blocking), blocking)
	}
	if !strings.Contains(blocking[0].Text, "unknown.go:99") {
		t.Errorf("BLOCKING = %q, must still name the survivor", blocking[0].Text)
	}
}

// TestUnclassifiedInScopeExcludesOutOfScope: the standing backlog lives outside
// the diff. Counting it here would fail every phase in this repo for debt it
// did not create — and would make the gate impossible to ever go green under,
// which is the fastest way to get a gate disabled.
func TestUnclassifiedInScopeExcludesOutOfScope(t *testing.T) {
	// lifecycleTests has one in-scope survivor plus three out-of-scope ones,
	// none of them accepted or routed.
	v := Skeleton(lifecycleTests(t, nil, nil), []string{"c-1"})

	if v.Summary.UnclassifiedInScope != 1 {
		t.Errorf("UnclassifiedInScope = %d, want 1 (the in-scope survivor only)", v.Summary.UnclassifiedInScope)
	}
	// The out-of-scope ones are still individually visible for draining —
	// excluded from the gate is not excluded from the report.
	flags := findingsBySeverity(v, "FLAG")
	if len(flags) != 3 {
		t.Errorf("got %d FLAG findings for out-of-scope survivors, want 3: %+v", len(flags), flags)
	}
	for _, f := range flags {
		if strings.Contains(f.Text, "in.go:10") {
			t.Errorf("the in-scope survivor was demoted to a FLAG: %q", f.Text)
		}
	}
}

// --- c-4: a remote leg that never ran is BLOCKING ---------------------------

// TestRemoteTransportFailureIsBlocking.
//
// A failing adapter and an unreachable mutation host look identical from
// verify's side — both are "the adapter returned an error" — but they mean
// opposite things about what this run knows. A misconfigured stryker is a
// problem with the tool: the FLAG is read and fixed. An unreachable host means
// nothing was measured for that language at all, and the absence of survivors
// is the absence of evidence rather than a clean result. A phase must not be
// verifiable past a leg that never ran.
func TestRemoteTransportFailureIsBlocking(t *testing.T) {
	transport := fmt.Errorf("remote rsync on helicon exited 255: %w", remote.ErrTransport)
	broken := &fakeAdapter{name: "gremlins", supportsExt: []string{".go"}, err: transport}

	tests, err := RunScoped("p", []string{"a.go"}, []mutation.Adapter{broken}, scopeOf("a.go"))
	if err != nil {
		t.Fatal(err)
	}
	if len(tests.Languages) != 1 {
		t.Fatalf("want one recorded leg, got %+v", tests.Languages)
	}
	if !tests.Languages[0].RemoteTransport {
		t.Fatal("the transport class was not captured while the error value was live — errors.Is cannot be re-run against the stored string")
	}
	// The leg still names the files it did not measure. An empty Files list
	// would make the report look like there was nothing to measure.
	if len(tests.Languages[0].Files) != 1 || tests.Languages[0].Files[0] != "a.go" {
		t.Errorf("the failed leg lost its Files: %+v", tests.Languages[0])
	}

	v := Skeleton(tests, []string{"c-1"})
	blocking := findingsBySeverity(v, "BLOCKING")
	if len(blocking) != 1 {
		t.Fatalf("want 1 BLOCKING for a leg that never ran, got %d: %+v", len(blocking), v.Findings)
	}
	if !strings.Contains(blocking[0].Text, "helicon") {
		t.Errorf("the BLOCKING finding does not name the host: %q", blocking[0].Text)
	}
	if len(findingsBySeverity(v, "FLAG")) != 0 {
		t.Errorf("the transport failure was ALSO flagged: %+v", v.Findings)
	}
}

// TestPlainAdapterFailureStaysAFlag is the regression half: the escalation must
// not swallow the existing behaviour. A stryker that is merely misconfigured is
// still a FLAG.
func TestPlainAdapterFailureStaysAFlag(t *testing.T) {
	broken := &fakeAdapter{name: "stryker", supportsExt: []string{".ts"}, err: errAdapterBoom}

	tests, err := RunScoped("p", []string{"a.ts"}, []mutation.Adapter{broken}, scopeOf("a.ts"))
	if err != nil {
		t.Fatal(err)
	}
	if tests.Languages[0].RemoteTransport {
		t.Error("a plain adapter error was classified as a transport failure")
	}

	v := Skeleton(tests, []string{"c-1"})
	if flags := findingsBySeverity(v, "FLAG"); len(flags) != 1 {
		t.Fatalf("want 1 FLAG for a plain adapter failure, got %d: %+v", len(flags), v.Findings)
	}
	if len(findingsBySeverity(v, "BLOCKING")) != 0 {
		t.Errorf("a plain adapter failure was escalated: %+v", v.Findings)
	}
}

// TestRemoteTransportFailurePreservesRecordAndContinue: escalating one leg must
// not discard another's finished report. The gremlins host being unreachable
// says nothing about the stryker run that already completed.
func TestRemoteTransportFailurePreservesRecordAndContinue(t *testing.T) {
	transport := fmt.Errorf("remote ssh on helicon exited 255: %w", remote.ErrTransport)
	broken := &fakeAdapter{name: "gremlins", supportsExt: []string{".go"}, err: transport}
	ok := &fakeAdapter{name: "stryker", supportsExt: []string{".ts"},
		report: reportOf("stryker", map[string]mutation.FileStat{"a.ts": {Killed: 1}})}

	tests, err := RunScoped("p", []string{"a.go", "a.ts"},
		[]mutation.Adapter{broken, ok}, scopeOf("a.go", "a.ts"))
	if err != nil {
		t.Fatal(err)
	}
	if len(tests.Languages) != 2 {
		t.Fatalf("want both legs recorded, got %+v", tests.Languages)
	}
	for _, lr := range tests.Languages {
		switch lr.Tool {
		case "gremlins":
			if !lr.RemoteTransport || lr.Error == "" {
				t.Errorf("the failed leg lost its classification: %+v", lr)
			}
		case "stryker":
			if lr.Mutation == nil || lr.Mutation.Killed != 1 {
				t.Errorf("the finished leg was discarded by the other's failure: %+v", lr.Mutation)
			}
			if lr.RemoteTransport {
				t.Errorf("the healthy leg was marked as a transport failure: %+v", lr)
			}
		}
	}

	v := Skeleton(tests, []string{"c-1"})
	if len(findingsBySeverity(v, "BLOCKING")) != 1 {
		t.Errorf("want exactly the one BLOCKING for the unreachable leg: %+v", v.Findings)
	}
}
