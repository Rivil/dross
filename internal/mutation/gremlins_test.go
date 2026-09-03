package mutation

import (
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

// fixtureGremlins is the gremlins testdata/normal_output.json payload,
// fetched from the canonical repo. Counts: 4 killed, 3 lived, 3 not
// covered, 2 not viable, 0 timeout — total 12 mutations across 3 files.
const fixtureGremlins = `{
  "go_module": "example.com/go/module",
  "test_efficacy": 57.14285714285714,
  "mutations_coverage": 70,
  "mutants_total": 9,
  "mutants_killed": 4,
  "mutants_lived": 3,
  "mutants_not_viable": 2,
  "mutants_not_covered": 3,
  "elapsed_time": 142.123,
  "files": [
    {
      "file_name": "file1.go",
      "mutations": [
        {"line":10,"column":3,"type":"CONDITIONALS_NEGATION","status":"KILLED"},
        {"line":20,"column":8,"type":"ARITHMETIC_BASE","status":"LIVED"},
        {"line":40,"column":7,"type":"INCREMENT_DECREMENT","status":"NOT COVERED"},
        {"line":10,"column":8,"type":"INVERT_ASSIGNMENTS","status":"NOT VIABLE"}
      ]
    },
    {
      "file_name": "file2.go",
      "mutations": [
        {"line":20,"column":3,"type":"INVERT_LOOPCTRL","status":"NOT COVERED"},
        {"line":44,"column":17,"type":"INCREMENT_DECREMENT","status":"KILLED"},
        {"line":500,"column":3,"type":"CONDITIONALS_BOUNDARY","status":"NOT COVERED"},
        {"line":100,"column":3,"type":"INVERT_BITWISE","status":"LIVED"},
        {"line":120,"column":3,"type":"INVERT_BITWISE_ASSIGNMENTS","status":"KILLED"}
      ]
    },
    {
      "file_name": "file3.go",
      "mutations": [
        {"line":5,"column":4,"type":"INVERT_LOGICAL","status":"KILLED"},
        {"line":15,"column":2,"type":"INVERT_NEGATIVES","status":"LIVED"},
        {"line":30,"column":1,"type":"REMOVE_SELF_ASSIGNMENTS","status":"NOT VIABLE"}
      ]
    }
  ]
}`

// fixtureGremlinsPerFile is the minimal shape the per-file attribution
// contract is stated over: one file with a kill and a live one, another with
// a single NOT COVERED mutant.
const fixtureGremlinsPerFile = `{
  "go_module": "example.com/go/module",
  "files": [
    {
      "file_name": "file1.go",
      "mutations": [
        {"line":10,"column":3,"type":"CONDITIONALS_NEGATION","status":"KILLED"},
        {"line":20,"column":8,"type":"ARITHMETIC_BASE","status":"LIVED"}
      ]
    },
    {
      "file_name": "file2.go",
      "mutations": [
        {"line":5,"column":1,"type":"INVERT_LOOPCTRL","status":"NOT COVERED"}
      ]
    }
  ]
}`

// assertPerFileMatchesAggregate pins the drift invariant every adapter owes
// diff scoping: each mutant counted in an aggregate is counted in exactly one
// per-file row. An adapter that bumps a counter and forgets the row fails
// here, at the parse, rather than three layers downstream as a mutant that
// silently leaves the denominator when the report is split by file.
func assertPerFileMatchesAggregate(t *testing.T, r *Report) {
	t.Helper()
	var sum FileStat
	for _, s := range r.Files {
		sum = sum.plus(s)
	}
	want := FileStat{
		Killed:     r.Killed,
		Survived:   r.Survived,
		Timeout:    r.Timeout,
		Errors:     r.Errors,
		NotCovered: r.NotCovered,
	}
	if sum != want {
		t.Errorf("per-file rows drift from aggregates:\n sum  %+v\n want %+v\n rows %+v", sum, want, r.Files)
	}
}

// TestParseGremlinsJSONPerFileAttribution pins which file each status lands
// in, and that adding the per-file table left the aggregates alone.
func TestParseGremlinsJSONPerFileAttribution(t *testing.T) {
	r, err := ParseGremlinsJSON([]byte(fixtureGremlinsPerFile))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]FileStat{
		"file1.go": {Killed: 1, Survived: 1},
		"file2.go": {Survived: 1, NotCovered: 1},
	}
	if !reflect.DeepEqual(r.Files, want) {
		t.Errorf("per-file rows:\n got %+v\nwant %+v", r.Files, want)
	}
	if r.Killed != 1 || r.Survived != 2 || r.NotCovered != 1 {
		t.Errorf("aggregates changed: killed=%d survived=%d notcovered=%d", r.Killed, r.Survived, r.NotCovered)
	}
	assertPerFileMatchesAggregate(t, r)
}

// TestParseGremlinsJSONPerFileNoDrift runs the drift invariant over the
// fuller fixtures — including NOT VIABLE (uncounted) and the unknown-status
// path, which must contribute to a row exactly when it contributes to a total.
func TestParseGremlinsJSONPerFileNoDrift(t *testing.T) {
	for name, payload := range map[string]string{
		"normal output":      fixtureGremlins,
		"timeout and future": fixtureGremlinsTimeoutAndUnknown,
	} {
		t.Run(name, func(t *testing.T) {
			r, err := ParseGremlinsJSON([]byte(payload))
			if err != nil {
				t.Fatal(err)
			}
			assertPerFileMatchesAggregate(t, r)
		})
	}
}

// fakeGremlins swaps the process seam for one that drops payload at the
// --output path instead of running gremlins, so Run's per-package loop can be
// exercised end to end without the binary. Returns the restore func.
func fakeGremlins(t *testing.T, payload []byte) func() {
	t.Helper()
	orig := gremlinsBuildCmd
	gremlinsBuildCmd = func(g *Gremlins, args []string) *exec.Cmd {
		for i, a := range args {
			if a == "--output" && i+1 < len(args) {
				if err := os.WriteFile(filepath.Join(g.ProjectRoot, args[i+1]), payload, 0o644); err != nil {
					t.Fatalf("write fake gremlins report: %v", err)
				}
			}
		}
		return exec.Command("true")
	}
	return func() { gremlinsBuildCmd = orig }
}

// TestGremlinsRunRePrefixesPackagePaths pins the re-prefix at the only seam
// that knows the package: Run's per-package loop. ParseGremlinsJSON takes
// bytes and no package argument, so it cannot express this at all.
//
// The fixture is a real gremlins payload (copied to testdata/, tracked —
// reports/ is gitignored runtime output) which names its files by bare
// basename. Left un-prefixed, a repo-relative change set matches none of them
// and the entire Go leg filters out as out-of-scope: a vacuous 0/0 that reads
// like a clean run.
func TestGremlinsRunRePrefixesPackagePaths(t *testing.T) {
	payload, err := os.ReadFile(filepath.Join("testdata", "gremlins_pkg_report.json"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	// Guard the premise: if the fixture ever gets replaced with a
	// repo-relative one, this test would pass without proving anything.
	if !strings.Contains(string(payload), `"file_name":"argfence.go"`) {
		t.Fatalf("fixture must carry bare basenames, got: %s", payload)
	}
	defer fakeGremlins(t, payload)()

	g := &Gremlins{ProjectRoot: t.TempDir()}
	rep, err := g.Run([]string{"internal/argfence/argfence.go"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(rep.Surviving) != 1 || rep.Surviving[0].File != "internal/argfence/policy.go" {
		t.Errorf("surviving path not repo-relative: %+v", rep.Surviving)
	}
	want := map[string]FileStat{
		"internal/argfence/policy.go":   {Survived: 1, NotCovered: 1},
		"internal/argfence/argfence.go": {Killed: 2},
	}
	if !reflect.DeepEqual(rep.Files, want) {
		t.Errorf("per-file keys not repo-relative:\n got %+v\nwant %+v", rep.Files, want)
	}
}

// fixtureGremlinsBareBasename is a per-package payload naming a file that
// exists under more than one directory — the collision the re-prefix prevents.
const fixtureGremlinsBareBasename = `{
  "go_module": "example.com/go/module",
  "files": [
    {
      "file_name": "x.go",
      "mutations": [
        {"line":1,"column":1,"type":"CONDITIONALS_NEGATION","status":"KILLED"},
        {"line":2,"column":1,"type":"ARITHMETIC_BASE","status":"LIVED"}
      ]
    }
  ]
}`

// TestGremlinsRunKeepsPackagesDistinct: two packages each reporting a bare
// `x.go` must land as two rows, not one summed entry — otherwise a survivor
// in an untouched package's x.go would be indistinguishable from one in the
// touched package's.
func TestGremlinsRunKeepsPackagesDistinct(t *testing.T) {
	defer fakeGremlins(t, []byte(fixtureGremlinsBareBasename))()

	g := &Gremlins{ProjectRoot: t.TempDir()}
	rep, err := g.Run([]string{"a/x.go", "b/x.go"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	want := map[string]FileStat{
		"a/x.go": {Killed: 1, Survived: 1},
		"b/x.go": {Killed: 1, Survived: 1},
	}
	if !reflect.DeepEqual(rep.Files, want) {
		t.Errorf("packages collapsed into one row:\n got %+v\nwant %+v", rep.Files, want)
	}
	assertPerFileMatchesAggregate(t, rep)
}

// TestMergeIntoSumsRepeatedFile is the other half of the distinctness
// contract: the same repo-relative path reported by two legs is ONE row.
func TestMergeIntoSumsRepeatedFile(t *testing.T) {
	dst := &Report{Tool: "gremlins"}
	mergeInto(dst, &Report{Killed: 2, Files: map[string]FileStat{"internal/x/a.go": {Killed: 2}}})
	mergeInto(dst, &Report{Survived: 1, Files: map[string]FileStat{"internal/x/a.go": {Survived: 1}}})
	want := map[string]FileStat{"internal/x/a.go": {Killed: 2, Survived: 1}}
	if !reflect.DeepEqual(dst.Files, want) {
		t.Errorf("repeated file must sum into one row:\n got %+v\nwant %+v", dst.Files, want)
	}
	assertPerFileMatchesAggregate(t, dst)
}

// TestRePrefixGremlinsFilesIdempotent: the module root has no prefix to add,
// and an already-prefixed path must not be prefixed twice.
func TestRePrefixGremlinsFilesIdempotent(t *testing.T) {
	root := &Report{Surviving: []Mutant{{File: "main.go"}}, Files: map[string]FileStat{"main.go": {Killed: 1}}}
	RePrefixGremlinsFiles(root, ".")
	if root.Surviving[0].File != "main.go" || root.Files["main.go"].Killed != 1 {
		t.Errorf("module root must be a no-op: %+v %+v", root.Surviving, root.Files)
	}

	r := &Report{Surviving: []Mutant{{File: "a.go"}}, Files: map[string]FileStat{"a.go": {Killed: 1}}}
	RePrefixGremlinsFiles(r, "./internal/x")
	RePrefixGremlinsFiles(r, "./internal/x")
	if r.Surviving[0].File != "internal/x/a.go" {
		t.Errorf("double-prefixed surviving path: %q", r.Surviving[0].File)
	}
	if _, ok := r.Files["internal/x/a.go"]; !ok || len(r.Files) != 1 {
		t.Errorf("double-prefixed per-file key: %+v", r.Files)
	}
}

func TestParseGremlinsJSONCounts(t *testing.T) {
	r, err := ParseGremlinsJSON([]byte(fixtureGremlins))
	if err != nil {
		t.Fatal(err)
	}
	if r.Tool != "gremlins" {
		t.Errorf("tool: %q", r.Tool)
	}
	// 4 KILLED
	if r.Killed != 4 {
		t.Errorf("killed: %d want 4", r.Killed)
	}
	// 3 LIVED + 3 NOT COVERED → survived = 6
	if r.Survived != 6 {
		t.Errorf("survived: %d want 6 (LIVED + NOT COVERED rolled up)", r.Survived)
	}
	// 3 NOT COVERED tracked as a subset of Survived for diagnostic surfacing.
	if r.NotCovered != 3 {
		t.Errorf("not_covered: %d want 3", r.NotCovered)
	}
	if r.Timeout != 0 {
		t.Errorf("timeout: %d want 0", r.Timeout)
	}
	// 2 NOT VIABLE → not counted
	if r.Errors != 0 {
		t.Errorf("errors: %d want 0 (NOT VIABLE not counted)", r.Errors)
	}
	// score = 4 / (4 + 6 + 0) = 0.4
	if math.Abs(r.Score-0.4) > 1e-9 {
		t.Errorf("score: %v want 0.4", r.Score)
	}
}

func TestParseGremlinsJSONDivergesFromGremlinsEfficacy(t *testing.T) {
	// gremlins' own test_efficacy in the fixture is 57.14% (4/7 — only
	// killed vs killed+lived; ignores NOT COVERED). Dross score is
	// 40% because we treat NOT COVERED as survived. Document that
	// divergence with a test so future-me notices if it changes.
	r, err := ParseGremlinsJSON([]byte(fixtureGremlins))
	if err != nil {
		t.Fatal(err)
	}
	if r.Score >= 0.57 {
		t.Errorf("expected score (%v) to be lower than gremlins' 57%% efficacy — dross penalises NOT COVERED", r.Score)
	}
}

func TestParseGremlinsJSONSurvivingMutants(t *testing.T) {
	r, err := ParseGremlinsJSON([]byte(fixtureGremlins))
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Surviving) != 6 {
		t.Fatalf("expected 6 surviving mutants, got %d", len(r.Surviving))
	}
	// First surviving in order: file1.go line 20 LIVED ARITHMETIC_BASE
	first := r.Surviving[0]
	if first.File != "file1.go" || first.Line != 20 || first.Op != "ARITHMETIC_BASE" {
		t.Errorf("first survivor wrong: %+v", first)
	}
}

const fixtureGremlinsTimeoutAndUnknown = `{
  "go_module": "x",
  "files": [
    {"file_name":"x.go","mutations":[
      {"line":1,"column":1,"type":"X","status":"TIMED OUT"},
      {"line":2,"column":1,"type":"X","status":"SKIPPED"},
      {"line":3,"column":1,"type":"X","status":"RUNNABLE"},
      {"line":4,"column":1,"type":"X","status":"FUTURE_STATUS_VALUE"}
    ]}
  ]
}`

func TestParseGremlinsJSONTimeoutAndUnknown(t *testing.T) {
	r, err := ParseGremlinsJSON([]byte(fixtureGremlinsTimeoutAndUnknown))
	if err != nil {
		t.Fatal(err)
	}
	if r.Timeout != 1 {
		t.Errorf("timeout: %d want 1", r.Timeout)
	}
	// SKIPPED + RUNNABLE not counted; unknown future status counts as error
	if r.Errors != 1 {
		t.Errorf("errors: %d want 1 (unknown status)", r.Errors)
	}
	if r.Killed+r.Survived != 0 {
		t.Errorf("nothing should be killed or survived: k=%d s=%d", r.Killed, r.Survived)
	}
	// score = 0 / (0 + 0 + 1) = 0 (only timeout, no killed)
	if r.Score != 0 {
		t.Errorf("score with only timeouts: %v want 0", r.Score)
	}
}

func TestParseGremlinsJSONEmpty(t *testing.T) {
	r, err := ParseGremlinsJSON([]byte(`{"go_module":"x","files":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	if r.Killed != 0 || r.Survived != 0 || r.Score != 0 {
		t.Errorf("empty report should be all zero: %+v", r)
	}
}

func TestParseGremlinsJSONMalformed(t *testing.T) {
	if _, err := ParseGremlinsJSON([]byte(`not json`)); err == nil {
		t.Fatal("expected error for malformed json")
	}
}

func TestGremlinsSupports(t *testing.T) {
	g := &Gremlins{}
	cases := map[string]bool{
		"main.go":           true,
		"internal/x/y.go":   true,
		"src/api.GO":        true, // case-insensitive
		"src/api.ts":        false,
		"src/Button.tsx":    false,
		"static/index.html": false,
		"main_test.go":      true, // gremlins handles tests itself but file is .go
	}
	for file, want := range cases {
		if got := g.Supports(file); got != want {
			t.Errorf("Supports(%q): got %v want %v", file, got, want)
		}
	}
}

func TestGremlinsName(t *testing.T) {
	if (&Gremlins{}).Name() != "gremlins" {
		t.Error("name should be 'gremlins'")
	}
}

// TestPackagesFromFiles pins the per-package derivation: one concrete
// package path per unique directory, deduped and sorted. Gremlins is
// invoked once per package (NOT over a collapsed `./<ancestor>/...`
// path) — a broad recursive scope makes gremlins gather empty coverage
// and report nothing, which used to hard-fail the whole verify.
func TestPackagesFromFiles(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want []string
	}{
		{
			name: "sibling packages each get their own path",
			in:   []string{"internal/api/tags.go", "internal/db/users.go"},
			want: []string{"./internal/api", "./internal/db"},
		},
		{
			name: "same package dedupes to one path",
			in:   []string{"internal/api/tags.go", "internal/api/users.go"},
			want: []string{"./internal/api"},
		},
		{
			name: "single subpackage",
			in:   []string{"internal/x/y.go"},
			want: []string{"./internal/x"},
		},
		{
			name: "file at module root maps to .",
			in:   []string{"main.go"},
			want: []string{"."},
		},
		{
			name: "root + subdir keep both, sorted",
			in:   []string{"main.go", "internal/x/y.go"},
			want: []string{".", "./internal/x"},
		},
		{
			name: "cross-top-level keep both, sorted",
			in:   []string{"internal/x/y.go", "cmd/server/main.go"},
			want: []string{"./cmd/server", "./internal/x"},
		},
		{
			name: "deep packages preserved separately",
			in:   []string{"internal/svc/api/x.go", "internal/svc/db/y.go"},
			want: []string{"./internal/svc/api", "./internal/svc/db"},
		},
		{
			name: "empty input returns nil",
			in:   nil,
			want: nil,
		},
		// --- Go-less directories yield no package ---
		//
		// Gremlins appends `/...` to every package argument, so "." is not the
		// root package — it is the whole module. A changed README.md deriving
		// "." is what made a phase touching 15 files mutate all 196 of them.
		{
			name: "a root file that is not Go derives no root package",
			in:   []string{"README.md"},
			want: nil,
		},
		{
			name: "a directory holding no Go is dropped",
			in:   []string{"assets/prompts/verify.md"},
			want: nil,
		},
		{
			name: "the phase's own shape: Go-less dirs dropped, Go dirs kept",
			in: []string{
				"README.md",
				"assets/prompts/verify.md",
				"internal/cmd/verify.go",
				"internal/remote/remote.go",
			},
			want: []string{"./internal/cmd", "./internal/remote"},
		},
		// The guard against over-reach: the filter is per FILE, but the
		// decision is per DIRECTORY. A package must survive a non-Go file
		// sitting beside its sources, whatever order they arrive in.
		{
			name: "a non-Go file beside Go does not drop its package",
			in:   []string{"internal/cmd/verify.go", "internal/cmd/testdata.json"},
			want: []string{"./internal/cmd"},
		},
		{
			name: "the non-Go file arriving first does not drop it either",
			in:   []string{"internal/cmd/testdata.json", "internal/cmd/verify.go"},
			want: []string{"./internal/cmd"},
		},
		{
			name: "a real root package is still derived from a root .go file",
			in:   []string{"README.md", "main.go"},
			want: []string{"."},
		},
		// A task that only adds tests still owes its package a run: those
		// tests are precisely what kill its mutants.
		{
			name: "test sources count as Go",
			in:   []string{"internal/cmd/verify_status_test.go"},
			want: []string{"./internal/cmd"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := packagesFromFiles(tc.in)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("packagesFromFiles(%v) = %v want %v", tc.in, got, tc.want)
			}
		})
	}
}

// TestHasCoverage pins the predicate that decides whether a per-package
// report contributed usable coverage. A report with only NOT COVERED
// mutants (gremlins' coverage blind spot) must be treated as having no
// coverage so it's excluded from the merged score.
func TestHasCoverage(t *testing.T) {
	cases := []struct {
		name string
		r    *Report
		want bool
	}{
		{"has kills", &Report{Killed: 2}, true},
		{"has a LIVED (covered) survivor", &Report{Survived: 1, NotCovered: 0}, true},
		{"only a timeout", &Report{Timeout: 1}, true},
		{"all NOT COVERED → no usable coverage", &Report{Survived: 5, NotCovered: 5}, false},
		{"empty report", &Report{}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := hasCoverage(tc.r); got != tc.want {
				t.Errorf("hasCoverage(%+v) = %v want %v", tc.r, got, tc.want)
			}
		})
	}
}

// TestMergeInto pins the accumulation + score recompute used to combine
// per-package reports into one.
func TestMergeInto(t *testing.T) {
	dst := &Report{Tool: "gremlins"}
	mergeInto(dst, &Report{Killed: 2, Surviving: []Mutant{{File: "a.go", Line: 1}}})
	mergeInto(dst, &Report{Killed: 1, Survived: 1, NotCovered: 1, Surviving: []Mutant{{File: "b.go", Line: 2}}})
	if dst.Killed != 3 || dst.Survived != 1 || dst.NotCovered != 1 {
		t.Fatalf("counts: killed=%d survived=%d notcovered=%d", dst.Killed, dst.Survived, dst.NotCovered)
	}
	if len(dst.Surviving) != 2 {
		t.Errorf("surviving mutants not concatenated: %d", len(dst.Surviving))
	}
	// score = killed / (killed + survived + timeout) = 3 / (3 + 1 + 0) = 0.75
	if math.Abs(dst.Score-0.75) > 1e-9 {
		t.Errorf("merged score: %v want 0.75", dst.Score)
	}
}

// TestGremlinsRunToleratesMissingReport pins the resilience contract: a
// package whose gremlins run writes no report must NOT hard-fail the
// whole run. It is excluded and the adapter returns a (possibly empty)
// Report with nil error, so verify records `unmeasurable` and falls back
// to coverage-based judgement rather than aborting. Uses `true` as a
// stand-in for gremlins (exits 0, writes nothing).
func TestGremlinsRunToleratesMissingReport(t *testing.T) {
	g := &Gremlins{Prefix: "true", ProjectRoot: t.TempDir()}
	rep, err := g.Run([]string{"internal/x/y.go"})
	if err != nil {
		t.Fatalf("missing report must not be fatal: %v", err)
	}
	if rep == nil {
		t.Fatal("expected a non-nil (empty) report")
	}
	if rep.Killed != 0 || rep.Survived != 0 {
		t.Errorf("expected empty report, got %+v", rep)
	}
}

// TestGremlinsBuildUnleashArgsDefault asserts the default
// --timeout-coefficient override (30) is applied when the project
// hasn't set its own. Gremlins' built-in default (~3) is too tight
// for fast Go test suites; see DefaultTimeoutCoefficient comment.
func TestGremlinsBuildUnleashArgsDefault(t *testing.T) {
	g := &Gremlins{}
	args, err := g.buildUnleashArgs("reports/gremlins/output.json", []string{"./internal/api"})
	if err != nil {
		t.Fatalf("buildUnleashArgs: %v", err)
	}
	want := []string{
		"gremlins", "unleash",
		"--output", "reports/gremlins/output.json",
		"--timeout-coefficient", "30",
		"--workers", strconv.Itoa(defaultWorkers()),
		"--test-cpu", "1",
		"./internal/api",
	}
	if !reflect.DeepEqual(args, want) {
		t.Errorf("default args:\n got %v\nwant %v", args, want)
	}
}

// TestGremlinsBuildUnleashArgsWorkers asserts explicit Workers/TestCPU
// flow through to the flags, and that the defaults (NumCPU/2, 1) apply
// when unset — the parallelism that avoids the oversubscription timeouts.
func TestGremlinsBuildUnleashArgsWorkers(t *testing.T) {
	args, err := (&Gremlins{Workers: 4, TestCPU: 2}).buildUnleashArgs("r.json", []string{"./x"})
	if err != nil {
		t.Fatalf("buildUnleashArgs: %v", err)
	}
	if !argHasFlag(args, "--workers", "4") {
		t.Errorf("expected --workers 4, got %v", args)
	}
	if !argHasFlag(args, "--test-cpu", "2") {
		t.Errorf("expected --test-cpu 2, got %v", args)
	}

	def, derr := (&Gremlins{}).buildUnleashArgs("r.json", []string{"./x"})
	if derr != nil {
		t.Fatalf("buildUnleashArgs: %v", derr)
	}
	if !argHasFlag(def, "--workers", strconv.Itoa(defaultWorkers())) {
		t.Errorf("expected default --workers %d, got %v", defaultWorkers(), def)
	}
	if !argHasFlag(def, "--test-cpu", "1") {
		t.Errorf("expected default --test-cpu 1, got %v", def)
	}
}

// argHasFlag reports whether args contains flag immediately followed by val.
func argHasFlag(args []string, flag, val string) bool {
	for i, a := range args {
		if a == flag {
			return i+1 < len(args) && args[i+1] == val
		}
	}
	return false
}

// TestGremlinsBuildUnleashArgsOverride asserts a project-set
// TimeoutCoefficient flows through to the flag.
func TestGremlinsBuildUnleashArgsOverride(t *testing.T) {
	g := &Gremlins{TimeoutCoefficient: 60}
	args, err := g.buildUnleashArgs("reports/gremlins/output.json", []string{"./..."})
	if err != nil {
		t.Fatalf("buildUnleashArgs: %v", err)
	}
	for i, a := range args {
		if a == "--timeout-coefficient" {
			if i+1 >= len(args) || args[i+1] != "60" {
				t.Fatalf("expected --timeout-coefficient 60, got %v", args)
			}
			return
		}
	}
	t.Fatalf("--timeout-coefficient flag missing: %v", args)
}

// TestGremlinsRunCreatesReportDir asserts the adapter pre-creates the
// `reports/gremlins/` directory before invoking gremlins. Gremlins
// itself won't create parent dirs for --output; without this, fresh
// projects fail their first verify with "did not write a report".
// Uses /usr/bin/true as a stand-in for gremlins so the test stays
// hermetic — we don't need a real mutation run, just confirmation that
// the path was prepared.
func TestGremlinsRunCreatesReportDir(t *testing.T) {
	root := t.TempDir()
	g := &Gremlins{
		Prefix:      "true", // swallow the gremlins exec, exit 0
		ProjectRoot: root,
	}
	// Run will fail at the report-read step (true doesn't write a JSON);
	// we don't care — we only care that the dir exists afterwards.
	_, _ = g.Run([]string{"main.go"})

	dir := filepath.Join(root, "reports", "gremlins")
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("reports/gremlins/ should exist: %v", err)
	}
	if !info.IsDir() {
		t.Fatal("reports/gremlins should be a directory")
	}
}

func TestGremlinsDispatch(t *testing.T) {
	adapters := []Adapter{&Stryker{}, &Gremlins{}}
	if got := Dispatch("main.go", adapters); got == nil || got.Name() != "gremlins" {
		t.Errorf("expected gremlins for .go; got %v", got)
	}
	if got := Dispatch("api.ts", adapters); got == nil || got.Name() != "stryker" {
		t.Errorf("expected stryker for .ts; got %v", got)
	}
	if got := Dispatch("project.godot", adapters); got != nil {
		t.Errorf("expected nil for unsupported ext; got %v", got)
	}
}

// fakeGremlinsPerPackage substitutes the process seam with one that writes a
// DIFFERENT payload per package, keyed by the package path Run passes ("./a").
// A package absent from the map gets no report at all — the missing-report case.
// The single-payload fakeGremlins cannot express any of the skip kinds, because
// every package would hit the same one.
func fakeGremlinsPerPackage(t *testing.T, byPkg map[string]string) func() {
	t.Helper()
	orig := gremlinsBuildCmd
	gremlinsBuildCmd = func(g *Gremlins, args []string) *exec.Cmd {
		var out, pkg string
		for i, a := range args {
			if a == "--output" && i+1 < len(args) {
				out = args[i+1]
			}
			if strings.HasPrefix(a, "./") || a == "." {
				pkg = a
			}
		}
		if payload, ok := byPkg[pkg]; ok && out != "" {
			if err := os.WriteFile(filepath.Join(g.ProjectRoot, out), []byte(payload), 0o644); err != nil {
				t.Fatalf("write fake gremlins report for %s: %v", pkg, err)
			}
		}
		return exec.Command("true")
	}
	return func() { gremlinsBuildCmd = orig }
}

// gremlinsAllNotCovered is a parseable report whose every mutant is NOT COVERED
// — the coverage blind spot. It parses, so the package IS measured; it just
// contributes nothing usable to a score.
const gremlinsAllNotCovered = `{
  "go_module": "example.com/go/module",
  "files": [
    {
      "file_name": "b.go",
      "mutations": [
        {"line":7,"column":1,"type":"CONDITIONALS_NEGATION","status":"NOT COVERED"},
        {"line":9,"column":1,"type":"CONDITIONALS_BOUNDARY","status":"NOT COVERED"}
      ]
    }
  ]
}`

// gremlinsCovered is an ordinary report with real killed and lived mutants.
const gremlinsCovered = `{
  "go_module": "example.com/go/module",
  "files": [
    {
      "file_name": "c.go",
      "mutations": [
        {"line":3,"column":1,"type":"CONDITIONALS_NEGATION","status":"KILLED"},
        {"line":5,"column":1,"type":"CONDITIONALS_NEGATION","status":"LIVED"}
      ]
    }
  ]
}`

// unmeasuredFor returns the entry for pkg, or a zero value if the package was
// not excluded at all.
func unmeasuredFor(t *testing.T, g *Gremlins, pkg string) Unmeasured {
	t.Helper()
	for _, u := range g.Unmeasured {
		if u.Package == pkg {
			return u
		}
	}
	return Unmeasured{}
}

// TestGremlinsUnmeasuredDistinguishesSkipKinds is c-4's collection half, and
// the fact the drain gates on. "No report at all" and "a report with zero
// covered mutants" both drop out of the score, but they mean opposite things:
// the first is no evidence (fatal for a drain), the second is evidence of
// survivors nobody has killed (classify them). Collapsing the two into one
// prose line — which is all Run used to emit — makes an unmeasured package
// indistinguishable from a clean one.
func TestGremlinsUnmeasuredDistinguishesSkipKinds(t *testing.T) {
	defer fakeGremlinsPerPackage(t, map[string]string{
		"./a": "{not json at all",
		"./b": gremlinsAllNotCovered,
		"./c": gremlinsCovered,
		// "./d" is absent: gremlins writes nothing for it.
	})()

	g := &Gremlins{ProjectRoot: t.TempDir()}
	rep, err := g.Run([]string{"a/x.go", "b/x.go", "c/x.go", "d/x.go"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if len(g.Unmeasured) != 3 {
		t.Fatalf("Unmeasured holds %d entries, want 3 (a, b, d):\n%+v", len(g.Unmeasured), g.Unmeasured)
	}

	// The malformed report: exact message, including every concatenation.
	a := unmeasuredFor(t, g, "./a")
	wantA := "./a (unreadable report: decode gremlins report: invalid character 'n' looking for beginning of object key string)"
	if a.Message != wantA {
		t.Errorf("malformed-report message:\n got %q\nwant %q", a.Message, wantA)
	}
	if a.Kind != UnmeasuredUnreadable {
		t.Errorf("malformed report Kind = %q, want %q", a.Kind, UnmeasuredUnreadable)
	}

	// The zero-coverage report: exact message, and marked uncovered — NOT
	// missing. The drain must classify this package's survivors, not fail on it.
	b := unmeasuredFor(t, g, "./b")
	wantB := "./b (zero covered mutants — coverage blind spot)"
	if b.Message != wantB {
		t.Errorf("zero-coverage message:\n got %q\nwant %q", b.Message, wantB)
	}
	if b.Kind != UnmeasuredUncovered {
		t.Errorf("zero-coverage Kind = %q, want %q", b.Kind, UnmeasuredUncovered)
	}

	// The absent report: marked missing, distinguishable from ./b as DATA and
	// not only by its prose.
	d := unmeasuredFor(t, g, "./d")
	if d.Kind != UnmeasuredMissing {
		t.Errorf("absent report Kind = %q, want %q", d.Kind, UnmeasuredMissing)
	}
	if d.Kind == b.Kind {
		t.Error("a package with no report is indistinguishable from one with zero covered mutants")
	}

	// The good package is excluded from neither, and its rows do reach the merge.
	if got := unmeasuredFor(t, g, "./c"); got.Kind != "" {
		t.Errorf("a readable, covered package was reported unmeasured: %+v", got)
	}
	if _, ok := rep.Files["c/c.go"]; !ok {
		t.Errorf("the covered package's rows did not reach the merge: %+v", rep.Files)
	}

	// And the zero-coverage package contributes no rows to the MERGED report —
	// the exclusion is scoring-only. Its raw report is still on disk for the
	// drain to classify from.
	if _, ok := rep.Files["b/b.go"]; ok {
		t.Errorf("zero-coverage package leaked rows into the merged score: %+v", rep.Files)
	}
	raw := filepath.Join(g.ProjectRoot, "reports", "gremlins", "b.json")
	if _, err := os.Stat(raw); err != nil {
		t.Errorf("zero-coverage package's raw report must survive for the drain to read: %v", err)
	}
}

// TestGremlinsUnmeasuredIsPerRun: the field describes the most recent Run, not
// an accumulation. A stale entry from a prior run would make the drain fail on
// a package that has since been measured.
func TestGremlinsUnmeasuredIsPerRun(t *testing.T) {
	restore := fakeGremlinsPerPackage(t, map[string]string{})
	g := &Gremlins{ProjectRoot: t.TempDir()}
	if _, err := g.Run([]string{"a/x.go"}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(g.Unmeasured) != 1 {
		t.Fatalf("first run: Unmeasured = %+v, want 1 entry", g.Unmeasured)
	}
	restore()

	defer fakeGremlinsPerPackage(t, map[string]string{"./a": gremlinsCovered})()
	if _, err := g.Run([]string{"a/x.go"}); err != nil {
		t.Fatalf("second Run: %v", err)
	}
	if len(g.Unmeasured) != 0 {
		t.Errorf("a measured re-run left stale exclusions behind: %+v", g.Unmeasured)
	}
}

// TestMissingReportIsSkipNotError: a missing report is a skip INSIDE the
// adapter — Run returns a nil error and an empty report. Turning absence fatal
// here would abort every verify whose repo has one uncoverable package; the
// drain is where absence becomes fatal, because that is the caller that needs
// evidence rather than a best-effort score.
func TestMissingReportIsSkipNotError(t *testing.T) {
	defer fakeGremlinsPerPackage(t, map[string]string{})()

	g := &Gremlins{ProjectRoot: t.TempDir()}
	rep, err := g.Run([]string{"a/x.go"})
	if err != nil {
		t.Fatalf("a missing report must not be an adapter error: %v", err)
	}
	if rep == nil {
		t.Fatal("expected a non-nil (empty) report")
	}
	if len(rep.Surviving) != 0 || rep.Killed != 0 {
		t.Errorf("expected an empty report, got %+v", rep)
	}
	if len(g.Unmeasured) != 1 || g.Unmeasured[0].Kind != UnmeasuredMissing {
		t.Fatalf("the skip must be recorded as missing: %+v", g.Unmeasured)
	}
	// And the reason is still printed — the field supplements stderr, it does
	// not replace it.
	if got := g.Unmeasured[0].String(); got != g.Unmeasured[0].Message {
		t.Errorf("String() = %q, want the printed message %q", got, g.Unmeasured[0].Message)
	}
}

// TestCollectMatchesRunForTheSameReports is the equivalence that makes c-2 an
// assertion instead of a claim.
//
// Run and Collect are two entry points to the same merge: Run reaches it after
// spawning the tool and fetching each report, Collect reaches it hours later
// against reports already on disk. They share every helper — ParseGremlinsJSON,
// RePrefixGremlinsFiles, DropInapplicable, hasCoverage, mergeInto — but sharing
// helpers is not the same as agreeing, and nothing else in the suite would fail
// if one of them grew a step the other lacked.
//
// A detached verdict that differed from an attached one would be invisible:
// both produce a plausible verify.toml, and the difference would only surface
// as two runs of the same phase disagreeing for no stated reason.
func TestCollectMatchesRunForTheSameReports(t *testing.T) {
	root := t.TempDir()
	restore := fakeGremlins(t, []byte(fixtureGremlins))
	defer restore()

	files := []string{"pkga/x.go", "pkgb/y.go"}

	// Run spawns (faked) and leaves the per-package reports on disk.
	viaRun := &Gremlins{ProjectRoot: root}
	fromRun, err := viaRun.Run(files)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Collect reads exactly those reports, with no spawn at all.
	viaCollect := &Gremlins{ProjectRoot: root}
	steps, err := viaCollect.DetachSteps(files)
	if err != nil {
		t.Fatalf("DetachSteps: %v", err)
	}
	fromCollect, err := viaCollect.Collect(steps, "helicon")
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}

	for _, f := range []struct {
		name      string
		got, want int
	}{
		{"Killed", fromCollect.Killed, fromRun.Killed},
		{"Survived", fromCollect.Survived, fromRun.Survived},
		{"Timeout", fromCollect.Timeout, fromRun.Timeout},
		{"NotCovered", fromCollect.NotCovered, fromRun.NotCovered},
		{"Errors", fromCollect.Errors, fromRun.Errors},
		{"len(Surviving)", len(fromCollect.Surviving), len(fromRun.Surviving)},
	} {
		if f.got != f.want {
			t.Errorf("%s: Collect=%d Run=%d — the two paths disagree, so a detached "+
				"verdict is not the attached one", f.name, f.got, f.want)
		}
	}
	if fromCollect.Score != fromRun.Score {
		t.Errorf("Score: Collect=%v Run=%v", fromCollect.Score, fromRun.Score)
	}

	// The survivor rows must match by identity, not merely in count: a path
	// that re-prefixed differently would keep the count and change every path.
	byKey := func(r *Report) map[string]int {
		m := map[string]int{}
		for _, s := range r.Surviving {
			m[fmt.Sprintf("%s:%d:%s", s.File, s.Line, s.Op)]++
		}
		return m
	}
	if !reflect.DeepEqual(byKey(fromCollect), byKey(fromRun)) {
		t.Errorf("surviving rows differ:\n Collect=%v\n Run=%v", byKey(fromCollect), byKey(fromRun))
	}
}

// TestCollectSkipsAMissingReportRatherThanFailing mirrors Run's reading: a
// package gremlins gathered no covered mutants for writes nothing, and that is
// an exclusion, not an error. Failing here would turn every phase containing
// one such package into an uncollectable detached run.
func TestCollectSkipsAMissingReportRatherThanFailing(t *testing.T) {
	root := t.TempDir()
	g := &Gremlins{ProjectRoot: root}
	steps, err := g.DetachSteps([]string{"pkga/x.go"})
	if err != nil {
		t.Fatal(err)
	}
	rep, err := g.Collect(steps, "helicon")
	if err != nil {
		t.Fatalf("Collect failed on an absent report: %v", err)
	}
	if rep.Killed+rep.Survived != 0 {
		t.Errorf("an absent report contributed counts: %+v", rep)
	}
	if len(g.Unmeasured) != 1 || g.Unmeasured[0].Kind != UnmeasuredMissing {
		t.Errorf("the absent report was not recorded as unmeasured: %+v", g.Unmeasured)
	}
}

// writeStepExit records code as the exit file for step s, the way the detached
// sequence does on the host.
func writeStepExit(t *testing.T, root string, s PackageStep, code string) {
	t.Helper()
	if s.ExitRel == "" {
		t.Fatal("step carries no ExitRel — the detached sequence has nowhere to record a code")
	}
	p := filepath.Join(root, s.ExitRel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(code+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// writeStepReport drops payload at step s's report path, creating the report
// directory the way the host's `mkdir -p` does. Not folded into writeStepExit:
// a test that writes only one of the pair is asserting something about the
// other's absence.
func writeStepReport(t *testing.T, root string, s PackageStep, payload string) {
	t.Helper()
	p := filepath.Join(root, s.ReportRel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(payload), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestCollectRefusesAPackageThatFailedBeforeMeasuring is the false-green this
// whole seam exists to refuse, at the package level.
//
// The attached loop has refused this since reportlessExitFatal landed — written
// after a real host failed `go test ./internal/cmd`, leaving gremlins unable to
// gather coverage, exiting 1 and writing nothing, while the run reported a
// clean 0.95 with the package holding most of the phase's code unmeasured. The
// detached path reproduced it exactly, because a missing report was read as
// "no covered mutants" with no code consulted.
//
// The second package measures cleanly, which is the point: the failure must
// bite even when the rest of the run is fine. A guard that only fired when
// EVERYTHING failed is the run-level one that was already there and already
// insufficient.
func TestCollectRefusesAPackageThatFailedBeforeMeasuring(t *testing.T) {
	root := t.TempDir()
	g := &Gremlins{ProjectRoot: root}
	steps, err := g.DetachSteps([]string{"pkga/x.go", "pkgb/y.go"})
	if err != nil {
		t.Fatal(err)
	}
	if len(steps) != 2 {
		t.Fatalf("want two steps, got %d", len(steps))
	}
	// First failed before writing anything; second measured fine.
	writeStepExit(t, root, steps[0], "1")
	writeStepReport(t, root, steps[1], fixtureGremlins)
	writeStepExit(t, root, steps[1], "0")

	rep, err := g.Collect(steps, "helicon")
	if err == nil {
		t.Fatal("a package that exited 1 without writing a report was collected as merely unmeasured")
	}
	if rep != nil {
		t.Errorf("a refused collect still returned a report: %+v", rep)
	}
	// The message has to name what failed and where, or the user cannot act:
	// the package, the code, and the host that actually measured it.
	for _, want := range []string{steps[0].Package, "exited 1", "helicon"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error does not mention %q: %v", want, err)
		}
	}
}

// TestCollectStillSkipsAZeroExitWithNoReport is the over-reach guard. Gremlins
// exiting 0 without a report means it found no covered mutants — a real and
// benign answer, and the state TestCollectSkipsAMissingReportRatherThanFailing
// pins. Turning that into a failure would make every phase containing such a
// package uncollectable.
func TestCollectStillSkipsAZeroExitWithNoReport(t *testing.T) {
	root := t.TempDir()
	g := &Gremlins{ProjectRoot: root}
	steps, err := g.DetachSteps([]string{"pkga/x.go"})
	if err != nil {
		t.Fatal(err)
	}
	writeStepExit(t, root, steps[0], "0")

	rep, err := g.Collect(steps, "helicon")
	if err != nil {
		t.Fatalf("a clean exit with no report should be a skip, not a failure: %v", err)
	}
	if rep.Killed+rep.Survived != 0 {
		t.Errorf("an absent report contributed counts: %+v", rep)
	}
	if len(g.Unmeasured) != 1 || g.Unmeasured[0].Kind != UnmeasuredMissing {
		t.Errorf("the absent report was not recorded as unmeasured: %+v", g.Unmeasured)
	}
}

// TestCollectTreatsAnUnrecordedExitAsASkip covers the states where no code can
// be trusted: the step never reached the line that writes one (killed
// mid-package, host rebooted), or what it wrote is not a number.
//
// None of those is evidence of failure, and inventing a 0 would be evidence of
// success the run never produced. They stay skips — the reading that held
// before exit files existed — with the run-level guard as the backstop.
func TestCollectTreatsAnUnrecordedExitAsASkip(t *testing.T) {
	for _, tc := range []struct {
		name string
		code string // "" means write no file at all
	}{
		{name: "no exit file at all", code: ""},
		{name: "an empty exit file", code: ""},
		{name: "a non-numeric exit file", code: "killed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			g := &Gremlins{ProjectRoot: root}
			steps, err := g.DetachSteps([]string{"pkga/x.go"})
			if err != nil {
				t.Fatal(err)
			}
			if tc.name != "no exit file at all" {
				writeStepExit(t, root, steps[0], tc.code)
			}
			if _, err := g.Collect(steps, "helicon"); err != nil {
				t.Errorf("an unrecorded exit should stay a skip: %v", err)
			}
			if len(g.Unmeasured) != 1 || g.Unmeasured[0].Kind != UnmeasuredMissing {
				t.Errorf("not recorded as unmeasured: %+v", g.Unmeasured)
			}
		})
	}
}

// TestCollectToleratesAFailedExitWhenTheReportLanded: the rule is about a
// package that failed BEFORE measuring anything. Gremlins exits non-zero
// whenever mutants survive, which is a successful measurement with bad results
// — refusing that would make every run with a survivor uncollectable.
func TestCollectToleratesAFailedExitWhenTheReportLanded(t *testing.T) {
	root := t.TempDir()
	g := &Gremlins{ProjectRoot: root}
	steps, err := g.DetachSteps([]string{"pkga/x.go"})
	if err != nil {
		t.Fatal(err)
	}
	writeStepReport(t, root, steps[0], fixtureGremlins)
	writeStepExit(t, root, steps[0], "1")

	rep, err := g.Collect(steps, "helicon")
	if err != nil {
		t.Fatalf("a non-zero exit WITH a report is a measurement, not a failure: %v", err)
	}
	if rep.Killed == 0 {
		t.Errorf("the report that landed was not merged: %+v", rep)
	}
}

// TestDetachStepsRecordsAnExitPathBesideEachReport: the code and the report
// share a stem and a directory, so they are cleared, fetched and read as a
// pair. A step with no ExitRel is one the sequence cannot record a code for,
// and every package that produced no report would then be indistinguishable
// from one that failed.
func TestDetachStepsRecordsAnExitPathBesideEachReport(t *testing.T) {
	g := &Gremlins{ProjectRoot: t.TempDir()}
	steps, err := g.DetachSteps([]string{"internal/cmd/a.go", "internal/remote/c.go"})
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range steps {
		if s.ExitRel == "" {
			t.Fatalf("step %s records no exit path", s.Package)
		}
		want := filepath.Join("reports", "gremlins", filepath.Base(GremlinsExitPath(g.ProjectRoot, s.Package)))
		if s.ExitRel != want {
			t.Errorf("step %s writes its code to %q, want %q", s.Package, s.ExitRel, want)
		}
		if filepath.Dir(s.ExitRel) != filepath.Dir(s.ReportRel) {
			t.Errorf("step %s splits its code (%s) from its report (%s)", s.Package, s.ExitRel, s.ReportRel)
		}
		if strings.TrimSuffix(s.ExitRel, ".exit") != strings.TrimSuffix(s.ReportRel, ".json") {
			t.Errorf("step %s's code and report do not share a stem: %s vs %s", s.Package, s.ExitRel, s.ReportRel)
		}
	}
	if steps[0].ExitRel == steps[1].ExitRel {
		t.Errorf("two packages record their codes to the same file: %s", steps[0].ExitRel)
	}
}

// TestDetachStepsDerivesTheSamePackagesRunDoes: the detached dispatch and the
// attached run must mutate the same set. A second derivation here would drift
// the moment packagesFromFiles changed, and the detached run would quietly
// measure a different set of packages while reporting through the same
// artefacts.
func TestDetachStepsDerivesTheSamePackagesRunDoes(t *testing.T) {
	g := &Gremlins{ProjectRoot: t.TempDir()}
	files := []string{"internal/cmd/a.go", "internal/cmd/b.go", "internal/remote/c.go"}

	steps, err := g.DetachSteps(files)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, s := range steps {
		got = append(got, s.Package)
	}
	want := packagesFromFiles(files)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("DetachSteps packages = %v, Run would use %v", got, want)
	}
	// And each step's report path must be the one Run writes, or a detached
	// run's reports land where nothing collects them.
	for _, s := range steps {
		wantRel := filepath.Join("reports", "gremlins", filepath.Base(GremlinsReportPath(g.ProjectRoot, s.Package)))
		if s.ReportRel != wantRel {
			t.Errorf("step %s writes %q, Run reads %q", s.Package, s.ReportRel, wantRel)
		}
	}
}

// TestCollectClassifiesAnUnreadableReport covers the branch between "no report"
// and "a good report" — a file that exists and does not parse.
//
// The three outcomes call for opposite handling and are deliberately distinct
// as DATA rather than as prose: missing means nothing is known, unreadable
// means nothing is known but something is wrong, and a parsed report with no
// coverage means the package WAS measured. A collect that merged a malformed
// file, or that treated it as fatal, would lose that distinction — and the
// detached path reads reports written hours earlier on another machine, which
// is exactly where a truncated file shows up.
func TestCollectClassifiesAnUnreadableReport(t *testing.T) {
	root := t.TempDir()
	g := &Gremlins{ProjectRoot: root}
	steps, err := g.DetachSteps([]string{"pkga/x.go"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "reports", "gremlins"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Truncated mid-object, which is what a half-written or half-fetched
	// report actually looks like.
	if err := os.WriteFile(filepath.Join(root, steps[0].ReportRel), []byte(`{"mutants_killed": 4, "files": [`), 0o644); err != nil {
		t.Fatal(err)
	}

	rep, err := g.Collect(steps, "helicon")
	if err != nil {
		t.Fatalf("an unreadable report should be recorded, not fatal: %v", err)
	}
	if rep.Killed+rep.Survived+rep.Timeout != 0 {
		t.Errorf("an unreadable report contributed counts: %+v", rep)
	}
	if len(g.Unmeasured) != 1 {
		t.Fatalf("want 1 unmeasured entry, got %d: %+v", len(g.Unmeasured), g.Unmeasured)
	}
	if g.Unmeasured[0].Kind != UnmeasuredUnreadable {
		t.Errorf("kind = %q, want %q — a malformed report is not the same as an absent one, "+
			"and a drain treats the two differently", g.Unmeasured[0].Kind, UnmeasuredUnreadable)
	}
	if !strings.Contains(g.Unmeasured[0].Message, "unreadable") {
		t.Errorf("the message does not say why: %q", g.Unmeasured[0].Message)
	}
}
