package mutation

import (
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
