package mutation

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// tsFixtureDir is the committed TypeScript project the end-to-end mutation run
// measures. Kept here so both this file and the e2e test resolve it one way.
const tsFixtureDir = "testdata/ts-project"

// TestFixtureIsAWellFormedProject: the fixture only earns its keep if it is a
// project the real toolchain would accept. A broken one degrades the e2e test
// into a tautology — the tool errors, the test skips or fails for the wrong
// reason, and nothing about the adapter was learned.
func TestFixtureIsAWellFormedProject(t *testing.T) {
	var pkg struct {
		Name    string            `json:"name"`
		Private bool              `json:"private"`
		Scripts map[string]string `json:"scripts"`
		DevDeps map[string]string `json:"devDependencies"`
	}
	readJSON(t, filepath.Join(tsFixtureDir, "package.json"), &pkg)

	if !pkg.Private {
		t.Error("the fixture must be private — it is testdata, not something to publish")
	}
	if pkg.Scripts["test"] == "" {
		t.Error("no test script: Stryker's vitest runner needs a suite to run per mutant")
	}
	// Pinned exactly, matching the adapter's own strykerPin. A floating version
	// makes the e2e result depend on whatever the registry served that day.
	for _, dep := range []string{"@stryker-mutator/core", "@stryker-mutator/vitest-runner", "vitest"} {
		v, ok := pkg.DevDeps[dep]
		if !ok {
			t.Errorf("devDependencies is missing %s", dep)
			continue
		}
		if strings.ContainsAny(v, "^~*") {
			t.Errorf("%s is pinned loosely (%q) — a floating version makes the end-to-end result depend on the day", dep, v)
		}
	}

	var conf struct {
		TestRunner string   `json:"testRunner"`
		Mutate     []string `json:"mutate"`
		Reporters  []string `json:"reporters"`
		JSON       struct {
			FileName string `json:"fileName"`
		} `json:"jsonReporter"`
	}
	readJSON(t, filepath.Join(tsFixtureDir, "stryker.conf.json"), &conf)

	if conf.TestRunner != "vitest" {
		t.Errorf("testRunner = %q, want vitest — the fixture's suite is vitest", conf.TestRunner)
	}
	if !containsStr(conf.Reporters, "json") {
		t.Error("the json reporter is not configured — the adapter reads the JSON report, so without it there is nothing to parse")
	}
	if conf.JSON.FileName == "" {
		t.Error("no jsonReporter.fileName — the adapter and the tool must agree on the report path, which is exactly what this fixture exists to prove")
	}

	// The mutate globs must actually resolve. A glob matching nothing produces
	// a clean, empty, meaningless report.
	matched := 0
	for _, g := range conf.Mutate {
		if strings.HasPrefix(g, "!") {
			continue
		}
		hits, err := filepath.Glob(filepath.Join(tsFixtureDir, filepath.FromSlash(strings.ReplaceAll(g, "**/", ""))))
		if err != nil {
			t.Fatalf("bad glob %q: %v", g, err)
		}
		matched += len(hits)
	}
	if matched == 0 {
		t.Error("no mutate glob resolves to a file on disk — the run would produce an empty report and report success")
	}
}

// TestFixtureTestCoversTheSource: an uncovered fixture makes every mutant
// survive, which turns the e2e assertion into noise — a report full of
// survivors says nothing about whether the tool ran correctly.
//
// The fixture is written so BOTH outcomes appear: covered functions whose
// mutants die, and one deliberately uncovered function whose mutants live.
func TestFixtureTestCoversTheSource(t *testing.T) {
	src := readFileString(t, filepath.Join(tsFixtureDir, "src", "tally.ts"))
	test := readFileString(t, filepath.Join(tsFixtureDir, "src", "tally.test.ts"))

	if !strings.Contains(test, "./tally") {
		t.Fatal("the test file does not import the module under test — nothing would be covered")
	}
	for _, fn := range []string{"tally", "classify"} {
		if !strings.Contains(src, "export function "+fn) {
			t.Errorf("the fixture lost its %s export", fn)
		}
		if !strings.Contains(test, fn+"(") {
			t.Errorf("%s is no longer exercised by the test — its mutants would all survive", fn)
		}
	}
	// The deliberately-uncovered one. Without it the report can only ever show
	// killed mutants, and "does the report distinguish killed from survived?"
	// goes untested.
	if !strings.Contains(src, "export function describe") {
		t.Error("the fixture lost its deliberately-uncovered function — at least one surviving mutant is what proves the report distinguishes the two")
	}
	if strings.Contains(test, "describe(") {
		t.Error("the uncovered function is now covered — the fixture must keep one function whose mutants survive")
	}

	// long.ts is the construct-range fixture; both of its functions are
	// covered so the whole-file e2e run keeps its score and the construct
	// proof measures generation, not coverage.
	longSrc := readFileString(t, filepath.Join(tsFixtureDir, "src", "long.ts"))
	longTest := readFileString(t, filepath.Join(tsFixtureDir, "src", "long.test.ts"))
	if !strings.Contains(longTest, "./long") {
		t.Fatal("long.test.ts does not import ./long — nothing would be covered")
	}
	for _, fn := range []string{"big", "small"} {
		if !strings.Contains(longSrc, "export function "+fn) {
			t.Errorf("long.ts lost its %s export", fn)
		}
		if !strings.Contains(longTest, fn+"(") {
			t.Errorf("%s is no longer exercised by long.test.ts", fn)
		}
	}
}

// TestDeepFixtureStaysDeep: the construct proof only means something while
// the edited line sits further from big's first line than the retired pad
// reached. A fixture that shrank under that would pass the proof vacuously.
func TestDeepFixtureStaysDeep(t *testing.T) {
	lines := strings.Split(readFileString(t, filepath.Join(tsFixtureDir, "src", "long.ts")), "\n")
	find := func(needle string) int {
		for i, l := range lines {
			if strings.Contains(l, needle) {
				return i + 1
			}
		}
		t.Fatalf("long.ts has no line containing %q", needle)
		return 0
	}
	bigStart := find("export function big(")
	edit := find("dross:deep-edit")
	smallStart := find("export function small<")
	if smallStart-bigStart < 40 {
		t.Errorf("big spans fewer than 40 lines (%d..%d) — the fixture is no longer long", bigStart, smallStart)
	}
	if edit-bigStart < 26 {
		t.Errorf("the deep edit (line %d) is within 25 lines of big's start (line %d) — the retired pad would have reached it", edit, bigStart)
	}
	if !strings.Contains(lines[edit-1], "total = 1000") {
		t.Errorf("the deep-edit line is no longer a mutable statement: %q", lines[edit-1])
	}
}

func readJSON(t *testing.T, path string, v any) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if err := json.Unmarshal(b, v); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
}

func readFileString(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}

func containsStr(hay []string, needle string) bool {
	for _, h := range hay {
		if h == needle {
			return true
		}
	}
	return false
}
