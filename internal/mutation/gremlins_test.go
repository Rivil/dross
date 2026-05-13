package mutation

import (
	"math"
	"os"
	"path/filepath"
	"reflect"
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
		"main.go":            true,
		"internal/x/y.go":    true,
		"src/api.GO":         true, // case-insensitive
		"src/api.ts":         false,
		"src/Button.tsx":     false,
		"static/index.html":  false,
		"main_test.go":       true, // gremlins handles tests itself but file is .go
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

func TestPackagesFromFiles(t *testing.T) {
	cases := []struct {
		in   []string
		want []string
	}{
		{
			in:   []string{"internal/api/tags.go", "internal/db/users.go"},
			want: []string{"./internal/api", "./internal/db"},
		},
		{
			// Same package gets deduped
			in:   []string{"internal/api/tags.go", "internal/api/users.go"},
			want: []string{"./internal/api"},
		},
		{
			// Files at repo root → ./...
			in:   []string{"main.go"},
			want: []string{"./..."},
		},
		{
			// Mix: root + subdir
			in:   []string{"main.go", "internal/x/y.go"},
			want: []string{"./...", "./internal/x"},
		},
	}
	for _, tc := range cases {
		got := packagesFromFiles(tc.in)
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("packagesFromFiles(%v) = %v want %v", tc.in, got, tc.want)
		}
	}
}

// TestGremlinsBuildUnleashArgsDefault asserts the default
// --timeout-coefficient override (30) is applied when the project
// hasn't set its own. Gremlins' built-in default (~3) is too tight
// for fast Go test suites; see DefaultTimeoutCoefficient comment.
func TestGremlinsBuildUnleashArgsDefault(t *testing.T) {
	g := &Gremlins{}
	args := g.buildUnleashArgs("reports/gremlins/output.json", []string{"./internal/api"})
	want := []string{
		"gremlins", "unleash",
		"--output", "reports/gremlins/output.json",
		"--timeout-coefficient", "30",
		"./internal/api",
	}
	if !reflect.DeepEqual(args, want) {
		t.Errorf("default args:\n got %v\nwant %v", args, want)
	}
}

// TestGremlinsBuildUnleashArgsOverride asserts a project-set
// TimeoutCoefficient flows through to the flag.
func TestGremlinsBuildUnleashArgsOverride(t *testing.T) {
	g := &Gremlins{TimeoutCoefficient: 60}
	args := g.buildUnleashArgs("reports/gremlins/output.json", []string{"./..."})
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
