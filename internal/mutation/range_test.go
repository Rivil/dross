package mutation

import (
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

// An unnarrowed file missing from the report is the 2026-08-26 fault, and it
// refuses exactly as it did before.
func TestCheckInstrumentedStillRefusesAnUnnarrowedDrop(t *testing.T) {
	s := &Stryker{Workdir: "web"}
	head := &headBuffer{limit: 1 << 10}
	err := s.checkInstrumented(
		[]byte(reportWithOnlyA),
		[]string{"src/a.ts", "src/b.ts"},
		map[string]bool{},
		head,
	)
	if err == nil {
		t.Error("an unnarrowed file absent from the report must refuse")
	}
}

// The adapter must satisfy the optional interface, or RunScoped silently keeps
// calling the whole-file path and the whole change measures nothing.
func TestStrykerImplementsRangeRunner(t *testing.T) {
	var _ RangeRunner = (*Stryker)(nil)
}
