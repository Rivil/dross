package mutation

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// mutateArg pulls the --mutate value out of a runArgs argv.
func mutateArg(t *testing.T, argv []string) string {
	t.Helper()
	for i, a := range argv {
		if a == "--mutate" && i+1 < len(argv) {
			return argv[i+1]
		}
	}
	t.Fatalf("no --mutate in argv: %v", argv)
	return ""
}

// The core of c-3: a phase that edited one line of a large file must not
// instrument the rest of it.
func TestRunArgsEmitsLineRanges(t *testing.T) {
	s := &Stryker{Workdir: "web"}
	args, requested, err := s.runArgs(
		[]string{"web/src/lib/server/recipe.ts"},
		map[string][]Range{"web/src/lib/server/recipe.ts": {{Start: 120, End: 124}}},
	)
	if err != nil {
		t.Fatalf("runArgs: %v", err)
	}
	if got, want := mutateArg(t, args), "src/lib/server/recipe.ts:120-124"; got != want {
		t.Errorf("--mutate = %q, want %q", got, want)
	}
	// requested must stay BARE. It is what checkInstrumented compares report
	// keys against, and the report keys carry no ranges — a ranged `requested`
	// would find every file "missing" and refuse a perfectly good run.
	if got, want := strings.Join(requested, ","), "src/lib/server/recipe.ts"; got != want {
		t.Errorf("requested = %q, want %q (bare)", got, want)
	}
}

func TestRunArgsEmitsEveryHunkOfAFile(t *testing.T) {
	s := &Stryker{Workdir: "web"}
	args, _, err := s.runArgs(
		[]string{"web/src/a.ts"},
		map[string][]Range{"web/src/a.ts": {{Start: 3, End: 3}, {Start: 40, End: 52}}},
	)
	if err != nil {
		t.Fatalf("runArgs: %v", err)
	}
	if got, want := mutateArg(t, args), "src/a.ts:3-3,src/a.ts:40-52"; got != want {
		t.Errorf("--mutate = %q, want %q", got, want)
	}
}

// The fail-open limb, and the reason RangeRunner documents it as a contract:
// a file the caller knows nothing about must be mutated WHOLE, not skipped.
func TestRunArgsMutatesAFileWithNoRangesWhole(t *testing.T) {
	s := &Stryker{Workdir: "web"}
	args, _, err := s.runArgs(
		[]string{"web/src/a.ts", "web/src/b.ts"},
		map[string][]Range{"web/src/a.ts": {{Start: 1, End: 2}}},
	)
	if err != nil {
		t.Fatalf("runArgs: %v", err)
	}
	got := mutateArg(t, args)
	if !strings.Contains(got, "src/a.ts:1-2") {
		t.Errorf("--mutate = %q, want the narrowed file", got)
	}
	if !strings.Contains(got, "src/b.ts") || strings.Contains(got, "src/b.ts:") {
		t.Errorf("--mutate = %q, want src/b.ts whole", got)
	}
}

func TestRunArgsWithNoRangesIsUnchanged(t *testing.T) {
	s := &Stryker{Workdir: "web"}
	with, _, err := s.runArgs([]string{"web/src/a.ts"}, nil)
	if err != nil {
		t.Fatalf("runArgs: %v", err)
	}
	without, _, err := s.runArgs([]string{"web/src/a.ts"}, map[string][]Range{})
	if err != nil {
		t.Fatalf("runArgs: %v", err)
	}
	if mutateArg(t, with) != "src/a.ts" || mutateArg(t, without) != "src/a.ts" {
		t.Errorf("nil and empty ranges must both mean whole-file: %q / %q",
			mutateArg(t, with), mutateArg(t, without))
	}
}

// ORDER IS LOAD-BEARING. escapeGlobMeta rewrites "[" and "]" into bracket
// EXPRESSIONS; a range glued on before that would sit inside the string it
// inspects. Six real route files vanished from a run on 2026-08-26 when this
// escaping was absent, and narrowing must not undo the fix.
func TestRunArgsAppendsTheRangeAfterEscaping(t *testing.T) {
	s := &Stryker{Workdir: "web"}
	path := "web/src/routes/recipes/[id]/+page.server.ts"
	args, requested, err := s.runArgs(path0(path), map[string][]Range{
		path: {{Start: 10, End: 12}},
	})
	if err != nil {
		t.Fatalf("runArgs: %v", err)
	}
	got := mutateArg(t, args)
	// The escaped form is what escapeGlobMeta already produces for this path:
	// "[" and "]" become bracket expressions, and the leading "+" becomes an
	// extglob group so minimatch does not read it as a quantifier. The
	// assertion is that the range is appended to THAT, untouched by it.
	want := "src/routes/recipes/[[]id[]]/@(+page.server.ts):10-12"
	if got != want {
		t.Errorf("--mutate = %q, want %q", got, want)
	}
	if got, want := requested[0], "src/routes/recipes/[id]/+page.server.ts"; got != want {
		t.Errorf("requested = %q, want the real path %q", got, want)
	}
}

func path0(p string) []string { return []string{p} }

// Stryker reads a range on a .svelte against each block's own lines, so its
// ranges stay off the argv, whatever case the extension is in. The .ts beside it
// keeps its spec: only the .svelte is narrowed from the report.
func TestRunArgsKeepsSvelteRangesOffTheArgv(t *testing.T) {
	s := &Stryker{Workdir: "web"}
	args, requested, err := s.runArgs(
		[]string{"web/src/Widget.svelte", "web/src/Loud.SVELTE", "web/src/a.ts"},
		map[string][]Range{
			"web/src/Widget.svelte": {{Start: 9, End: 12}, {Start: 21, End: 23}},
			"web/src/Loud.SVELTE":   {{Start: 2, End: 3}},
			"web/src/a.ts":          {{Start: 4, End: 5}},
		},
	)
	if err != nil {
		t.Fatalf("runArgs: %v", err)
	}
	if got, want := mutateArg(t, args), "src/Widget.svelte,src/Loud.SVELTE,src/a.ts:4-5"; got != want {
		t.Errorf("--mutate = %q, want %q", got, want)
	}
	if got, want := strings.Join(requested, ","), "src/Widget.svelte,src/Loud.SVELTE,src/a.ts"; got != want {
		t.Errorf("requested = %q, want %q (bare)", got, want)
	}
}

// svelteRanges is what parseRanged will narrow, so it must hold exactly the
// files whose ranges runArgs held back, under the path the report comes back in.
func TestSvelteRangesHoldsOnlyWhatTheArgvLeftOut(t *testing.T) {
	good := []Range{{Start: 9, End: 12}}
	got := svelteRanges(
		[]string{"src/Widget.svelte", "src/Bad.svelte", "src/Whole.svelte", "src/a.ts"},
		"web",
		map[string][]Range{
			"web/src/Widget.svelte": good,
			"web/src/Bad.svelte":    {{Start: 3, End: 5}, {Start: 0, End: 9}},
			"web/src/a.ts":          {{Start: 4, End: 5}},
		},
	)
	if want := map[string][]Range{"src/Widget.svelte": good}; !reflect.DeepEqual(got, want) {
		t.Errorf("svelteRanges = %v, want %v", got, want)
	}
}

// strykerReportOf writes a report holding one NoCoverage mutant per [start, end]
// line pair, under key.
func strykerReportOf(t *testing.T, key string, spans ...[2]int) []byte {
	t.Helper()
	f := strykerFile{Mutants: []strykerMutant{}}
	for _, sp := range spans {
		f.Mutants = append(f.Mutants, strykerMutant{
			MutatorName: "ArithmeticOperator",
			Status:      "NoCoverage",
			Location:    strykerLocation{Start: strykerPos{Line: sp[0]}, End: strykerPos{Line: sp[1]}},
		})
	}
	b, err := json.Marshal(strykerReport{Files: map[string]strykerFile{key: f}})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func survivingLines(r *Report) []int {
	lines := []int{}
	for _, m := range r.Surviving {
		lines = append(lines, m.Line)
	}
	slices.Sort(lines)
	return lines
}

// The rule is Stryker's own for a --mutate range: both ends count, and a mutant
// reaching past either edge is out.
func TestParseRangedKeepsOnlyMutantsWhollyInsideARange(t *testing.T) {
	const file = "src/Widget.svelte"
	ranges := map[string][]Range{file: {{Start: 9, End: 12}, {Start: 21, End: 23}}}
	for _, tc := range []struct {
		name string
		span [2]int
		kept bool
	}{
		{"on the first line", [2]int{9, 9}, true},
		{"on the last line", [2]int{12, 12}, true},
		{"filling the range", [2]int{9, 12}, true},
		{"in the second range", [2]int{22, 22}, true},
		{"one line before", [2]int{8, 8}, false},
		{"one line after", [2]int{13, 13}, false},
		{"reaching in from before", [2]int{8, 10}, false},
		{"reaching out past the end", [2]int{11, 13}, false},
		{"between the ranges", [2]int{15, 15}, false},
		{"spanning both ranges", [2]int{9, 23}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			report, err := (&Stryker{}).parseRanged(strykerReportOf(t, file, tc.span), []string{file}, ranges)
			if err != nil {
				t.Fatalf("parseRanged: %v", err)
			}
			if got := report.Survived == 1; got != tc.kept {
				t.Errorf("mutant %v kept = %v, want %v", tc.span, got, tc.kept)
			}
			if got := report.Files[file].NotCovered == 1; got != tc.kept {
				t.Errorf("mutant %v in the file's row = %v, want %v", tc.span, got, tc.kept)
			}
		})
	}
}

// Stryker has already ranged a .ts, and a .svelte with no ranges ran whole: the
// report is theirs as it stands, and only the ranged .svelte beside them narrows.
func TestParseRangedLeavesEverythingElseAlone(t *testing.T) {
	const widget, other, script = "src/Widget.svelte", "src/Other.svelte", "src/a.ts"
	mutants := []strykerMutant{
		{Status: "NoCoverage", Location: strykerLocation{Start: strykerPos{Line: 3}, End: strykerPos{Line: 3}}},
		{Status: "NoCoverage", Location: strykerLocation{Start: strykerPos{Line: 40}, End: strykerPos{Line: 40}}},
	}
	data, err := json.Marshal(strykerReport{Files: map[string]strykerFile{
		widget: {Mutants: mutants}, other: {Mutants: mutants}, script: {Mutants: mutants},
	}})
	if err != nil {
		t.Fatal(err)
	}
	report, err := (&Stryker{}).parseRanged(data, []string{widget, other, script}, map[string][]Range{
		widget: {{Start: 3, End: 3}},
		script: {{Start: 3, End: 3}},
	})
	if err != nil {
		t.Fatalf("parseRanged: %v", err)
	}
	for file, want := range map[string]int{widget: 1, other: 2, script: 2} {
		if got := report.Files[file].Survived; got != want {
			t.Errorf("%s: %d mutants, want %d", file, got, want)
		}
	}
}

// Nothing to narrow is ParseStrykerJSON unchanged.
func TestParseRangedWithNothingHeldBackParsesTheReportAsIs(t *testing.T) {
	const file = "src/Widget.svelte"
	report, err := (&Stryker{}).parseRanged(strykerReportOf(t, file, [2]int{3, 3}, [2]int{40, 40}), []string{file}, nil)
	if err != nil {
		t.Fatalf("parseRanged: %v", err)
	}
	if got := survivingLines(report); !reflect.DeepEqual(got, []int{3, 40}) {
		t.Errorf("lines = %v, want both", got)
	}
}

func TestParseRangedRefusesAnUndecodableReport(t *testing.T) {
	const file = "src/Widget.svelte"
	_, err := (&Stryker{}).parseRanged([]byte("{"), []string{file}, map[string][]Range{file: {{Start: 1, End: 2}}})
	if err == nil || !strings.Contains(err.Error(), "decode stryker report") {
		t.Errorf("err = %v, want the decode failure", err)
	}
}

// Keys come back workdir-relative or absolute, and the range map is
// repo-relative. All of them must meet, or a .svelte silently reports whole.
func TestParseRangedFindsTheFileWhateverTheReportKey(t *testing.T) {
	for _, tc := range []struct {
		name, key string
	}{
		{"workdir-relative", "src/Widget.svelte"},
		{"absolute", "/srv/repo/web/src/Widget.svelte"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &Stryker{Workdir: "web"}
			report, err := s.parseRanged(
				strykerReportOf(t, tc.key, [2]int{3, 3}, [2]int{40, 40}),
				[]string{"src/Widget.svelte"},
				map[string][]Range{"web/src/Widget.svelte": {{Start: 3, End: 3}}},
			)
			if err != nil {
				t.Fatalf("parseRanged: %v", err)
			}
			if got := survivingLines(report); !reflect.DeepEqual(got, []int{3}) {
				t.Errorf("lines = %v, want [3]", got)
			}
		})
	}
}

// Under absolute keys two requested paths can both be suffixes of one key, and
// the key belongs to the longer. A suffix counts only on a path boundary.
func TestRequestedForMatchesOnAPathBoundary(t *testing.T) {
	requested := []string{"Widget.svelte", "src/Widget.svelte", "packages/x/src/Widget.svelte"}
	for _, tc := range []struct{ key, want string }{
		{"src/Widget.svelte", "src/Widget.svelte"},
		{"/srv/repo/src/Widget.svelte", "src/Widget.svelte"},
		{"/srv/repo/packages/x/src/Widget.svelte", "packages/x/src/Widget.svelte"},
		{"/srv/repo/Widget.svelte", "Widget.svelte"},
		{"/srv/repo/src/MyWidget.svelte", ""},
		{"src/Other.svelte", ""},
	} {
		if got := requestedFor(tc.key, requested); got != tc.want {
			t.Errorf("requestedFor(%q) = %q, want %q", tc.key, got, tc.want)
		}
	}
}

// The file requested whole must keep every mutant even though the ranged file's
// path is a suffix of its key, while the ranged file still narrows.
func TestParseRangedGivesAWholeFileNoOtherFilesRanges(t *testing.T) {
	const ranged, whole = "src/Widget.svelte", "packages/x/src/Widget.svelte"
	const rangedKey, wholeKey = "/srv/repo/" + ranged, "/srv/repo/" + whole
	mutants := []strykerMutant{
		{Status: "NoCoverage", Location: strykerLocation{Start: strykerPos{Line: 3}, End: strykerPos{Line: 3}}},
		{Status: "NoCoverage", Location: strykerLocation{Start: strykerPos{Line: 40}, End: strykerPos{Line: 40}}},
	}
	data, err := json.Marshal(strykerReport{Files: map[string]strykerFile{
		rangedKey: {Mutants: mutants}, wholeKey: {Mutants: mutants},
	}})
	if err != nil {
		t.Fatal(err)
	}
	report, err := (&Stryker{}).parseRanged(data, []string{ranged, whole}, map[string][]Range{ranged: {{Start: 3, End: 3}}})
	if err != nil {
		t.Fatalf("parseRanged: %v", err)
	}
	if got := report.Files[rangedKey].Survived; got != 1 {
		t.Errorf("%s kept %d mutants, want 1", ranged, got)
	}
	if got := report.Files[wholeKey].Survived; got != 2 {
		t.Errorf("%s kept %d mutants, want its 2", whole, got)
	}
}

// A style-only hunk on an instrumented .svelte: every mutant falls outside, the
// report is empty, and that is a fact to name, not a drop to refuse.
func TestAStyleOnlyHunkOnAnInstrumentedSvelteReadsAsNoMutants(t *testing.T) {
	const file = "src/Widget.svelte"
	s := &Stryker{}
	ranges := map[string][]Range{file: {{Start: 33, End: 33}}}
	data := strykerReportOf(t, file, [2]int{10, 10}, [2]int{22, 22})
	out, report, err := captureStderr(t, func() (*Report, error) {
		r, err := s.parseRanged(data, []string{file}, ranges)
		if err != nil {
			return nil, err
		}
		narrowed := narrowedSet([]string{file}, "", ranges)
		return r, s.checkInstrumented(data, []string{file}, narrowed, &headBuffer{limit: 1 << 10})
	})
	if err != nil {
		t.Fatalf("a style-only hunk must not refuse the run: %v", err)
	}
	if report.Survived != 0 || len(report.Surviving) != 0 || len(report.Files) != 0 {
		t.Errorf("report = %+v, want no mutants at all", report)
	}
	if want := "1 narrowed file(s) contributed no mutants in their changed lines: " + file; !strings.Contains(out, want) {
		t.Errorf("stderr lacks %q:\n%s", want, out)
	}
}

// A file with no mutants to begin with has nothing the ranges took away, so
// parseRanged stays quiet and checkInstrumented's own branch speaks for it.
func TestParseRangedSaysNothingOfAFileThatHadNoMutants(t *testing.T) {
	const file = "src/Widget.svelte"
	out, _, err := captureStderr(t, func() (*Report, error) {
		return (&Stryker{}).parseRanged(strykerReportOf(t, file), []string{file}, map[string][]Range{file: {{Start: 1, End: 1}}})
	})
	if err != nil {
		t.Fatalf("parseRanged: %v", err)
	}
	if out != "" {
		t.Errorf("stderr = %q, want nothing", out)
	}
}

// --reuse-report reads the same whole-file report a live run does, so it must
// narrow the same way, and leave the tool's own report as it found it.
func TestReuseReportNarrowsASvelteFromTheReport(t *testing.T) {
	root := t.TempDir()
	const file = "src/Widget.svelte"
	s := &Stryker{ProjectRoot: root, ReuseReport: true}
	data := strykerReportOf(t, file, [2]int{10, 10}, [2]int{16, 16})
	if err := os.MkdirAll(filepath.Dir(s.reportPath()), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.reportPath(), data, 0o644); err != nil {
		t.Fatal(err)
	}
	report, err := s.RunRanges([]string{file}, map[string][]Range{file: {{Start: 9, End: 12}}})
	if err != nil {
		t.Fatalf("RunRanges: %v", err)
	}
	if got := survivingLines(report); !reflect.DeepEqual(got, []int{10}) {
		t.Errorf("lines = %v, want [10]", got)
	}
	if got := readFileString(t, s.reportPath()); got != string(data) {
		t.Errorf("the report on disk changed:\n%s", got)
	}
}

// A malformed range would select nothing at all, which reads on the report
// exactly like a file with no mutable lines. Falling back to the whole file
// keeps a bad input from quietly measuring less than it claims.
func TestRunArgsFallsBackOnAMalformedRange(t *testing.T) {
	s := &Stryker{Workdir: "web"}
	for _, bad := range []Range{{Start: 0, End: 5}, {Start: 9, End: 2}, {Start: -3, End: -1}} {
		args, _, err := s.runArgs([]string{"web/src/a.ts"}, map[string][]Range{
			"web/src/a.ts": {bad},
		})
		if err != nil {
			t.Fatalf("runArgs: %v", err)
		}
		if got := mutateArg(t, args); got != "src/a.ts" {
			t.Errorf("range %v: --mutate = %q, want the whole file", bad, got)
		}
	}
}

// A file with one good range and one bad one must fall back to the WHOLE file
// and emit nothing else. Emitting the good range alongside the fallback puts
// two contradictory specs for the same file in one argv.
func TestRunArgsFallsBackWholesaleWhenAnyRangeIsMalformed(t *testing.T) {
	s := &Stryker{Workdir: "web"}
	args, _, err := s.runArgs([]string{"web/src/a.ts"}, map[string][]Range{
		"web/src/a.ts": {{Start: 3, End: 5}, {Start: 0, End: 9}},
	})
	if err != nil {
		t.Fatalf("runArgs: %v", err)
	}
	if got := mutateArg(t, args); got != "src/a.ts" {
		t.Errorf("--mutate = %q, want the whole file and nothing else", got)
	}
}

func TestRunArgsRangeKeysAreRepoRelativeWithoutAWorkdir(t *testing.T) {
	s := &Stryker{}
	args, _, err := s.runArgs([]string{"src/a.ts"}, map[string][]Range{
		"src/a.ts": {{Start: 7, End: 9}},
	})
	if err != nil {
		t.Fatalf("runArgs: %v", err)
	}
	if got, want := mutateArg(t, args), "src/a.ts:7-9"; got != want {
		t.Errorf("--mutate = %q, want %q", got, want)
	}
}

func TestNarrowedSet(t *testing.T) {
	ranges := map[string][]Range{"web/src/a.ts": {{Start: 1, End: 1}}}
	got := narrowedSet([]string{"src/a.ts", "src/b.ts"}, "web", ranges)
	if !got["src/a.ts"] || got["src/b.ts"] {
		t.Errorf("narrowedSet = %v, want only src/a.ts", got)
	}
	if narrowedSet([]string{"src/a.ts"}, "web", nil) != nil {
		t.Error("no ranges must produce no narrowed set, so the guard keeps today's strictness")
	}
}

const reportWithOnlyA = `{"files":{"src/a.ts":{"mutants":[]}}}`

// A narrowed file can legitimately contribute zero mutants — a hunk that only
// touched comments or imports. Absence must not refuse the run.
func TestCheckInstrumentedToleratesANarrowedFileWithNoMutants(t *testing.T) {
	s := &Stryker{Workdir: "web"}
	head := &headBuffer{limit: 1 << 10}
	err := s.checkInstrumented(
		[]byte(reportWithOnlyA),
		[]string{"src/a.ts", "src/b.ts"},
		map[string]bool{"src/b.ts": true},
		head,
	)
	if err != nil {
		t.Errorf("narrowed file with no mutants must not refuse the run: %v", err)
	}
}

// A .svelte with nothing mutable is the sole file of its run and places no
// mutant anywhere, so stryker's report has no files at all: its rows are built
// from mutant results. That is the file's own fact, not a drop, and the narrowed
// branch is the one that must say so.
func TestCheckInstrumentedToleratesAStyleOnlySvelteHunk(t *testing.T) {
	s := &Stryker{}
	const file = "src/Widget.svelte"
	narrowed := narrowedSet([]string{file}, "", map[string][]Range{file: {{Start: 33, End: 33}}})
	head := &headBuffer{limit: 1 << 10}
	out, _, err := captureStderr(t, func() (*Report, error) {
		return nil, s.checkInstrumented([]byte(`{"files":{}}`), []string{file}, narrowed, head)
	})
	if err != nil {
		t.Fatalf("a style-only hunk must not refuse the run: %v", err)
	}
	if want := "1 narrowed file(s) contributed no mutants in their changed lines: " + file; !strings.Contains(out, want) {
		t.Errorf("stderr lacks %q:\n%s", want, out)
	}
}

// ...but only while stryker itself did not say it dropped something. That
// warning is what a failed bracket expansion produces, and it must still
// refuse even when the file was narrowed.
func TestCheckInstrumentedStillRefusesWhenStrykerWarnedItDropped(t *testing.T) {
	s := &Stryker{Workdir: "web"}
	head := &headBuffer{limit: 1 << 10}
	head.Write([]byte("some banner\n" + strykerDropWarningText + "\nmore\n"))
	err := s.checkInstrumented(
		[]byte(reportWithOnlyA),
		[]string{"src/a.ts", "src/b.ts"},
		map[string]bool{"src/b.ts": true},
		head,
	)
	if err == nil {
		t.Error("a drop stryker warned about must refuse, narrowed or not")
	}
}

// An unnarrowed file can ALSO contribute zero mutants — a declarations-only
// .d.ts, a .svelte whose script is imports and a Props interface — and stryker
// omits it from the report's files entirely. On 2026-09-18 that refused a
// complete 7h48m run over 181 files for two such files. Without stryker's own
// drop warning in the head, absence is tolerated and named on stderr.
func TestCheckInstrumentedToleratesAWholeFileWithNoMutants(t *testing.T) {
	s := &Stryker{Workdir: "web"}
	head := &headBuffer{limit: 1 << 10}
	out, _, err := captureStderr(t, func() (*Report, error) {
		return nil, s.checkInstrumented(
			[]byte(reportWithOnlyA),
			[]string{"src/a.ts", "src/app.d.ts", "src/b.ts"},
			map[string]bool{},
			head,
		)
	})
	if err != nil {
		t.Fatalf("whole files with no mutants must not refuse the run without stryker's warning: %v", err)
	}
	if want := "2 whole file(s) contributed no mutants"; !strings.Contains(out, want) {
		t.Errorf("stderr lacks %q:\n%s", want, out)
	}
	for _, f := range []string{"src/app.d.ts", "src/b.ts"} {
		if !strings.Contains(out, f) {
			t.Errorf("stderr must name %s:\n%s", f, out)
		}
	}
}

// The 2026-08-26 fault — a --mutate glob that resolved to nothing — still
// refuses for an unnarrowed file, because stryker warns about it and the
// warning is what the guard keys on now.
func TestCheckInstrumentedStillRefusesAnUnnarrowedDropStrykerWarnedAbout(t *testing.T) {
	s := &Stryker{Workdir: "web"}
	head := &headBuffer{limit: 1 << 10}
	head.Write([]byte("some banner\n" + strykerDropWarningText + "\nmore\n"))
	err := s.checkInstrumented(
		[]byte(reportWithOnlyA),
		[]string{"src/a.ts", "src/b.ts"},
		map[string]bool{},
		head,
	)
	if err == nil {
		t.Fatal("an unnarrowed file absent from the report must refuse when stryker warned it dropped a glob")
	}
	if !strings.Contains(err.Error(), "src/b.ts") || !strings.Contains(err.Error(), strykerDropWarningText) {
		t.Errorf("refusal must name the file and point at stryker's warning: %v", err)
	}
}

// The adapter must satisfy the optional interface, or RunScoped silently keeps
// calling the whole-file path and the whole change measures nothing.
func TestStrykerImplementsRangeRunner(t *testing.T) {
	var _ RangeRunner = (*Stryker)(nil)
}

// The other side of the same contract: gremlins mutates Go packages and takes
// no line scope, so it must NOT satisfy RangeRunner. If it ever grows a
// RunRanges method, RunScoped would start narrowing a leg that cannot honour
// the narrowing — a run claiming ranges it never applied.
func TestGremlinsDoesNotImplementRangeRunner(t *testing.T) {
	if _, ok := any(&Gremlins{}).(RangeRunner); ok {
		t.Fatal("Gremlins implements RangeRunner; gremlins must run whole files")
	}
}
