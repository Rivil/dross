package runsummary

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// jestPin is the exact jest the jest fixtures were captured with, installed
// with --ignore-scripts in a scratch directory outside the repo.
const jestPin = "30.5.2"

// vitestHeader heads every vitest fixture: the ts-project lockfile's vitest,
// installed with `npm ci --ignore-scripts` in a scratch copy.
const vitestHeader = "# captured: vitest@4.1.11 "

// feedJS runs a fixture through a Segment — splitter, ANSI and CR stripping —
// into p alone, without the fixture's header line.
func feedJS(t *testing.T, name string, p Parser) Parser {
	t.Helper()
	s := &Segment{parsers: []Parser{p}}
	for _, l := range fixtureLines(t, filepath.Join("testdata", "js", name)) {
		s.Write([]byte(l + "\n"))
	}
	s.Close()
	return p
}

func TestVitestCounts(t *testing.T) {
	p := feedJS(t, "vitest_red.txt", &vitest{})
	if !p.Claims() {
		t.Fatal("vitest_red.txt is not claimed")
	}
	r := p.Result()
	if got := strings.Join(r.Counts, " · "); got != "Test Files 1 failed | 1 passed (2) · Tests 1 failed | 4 passed (5)" {
		t.Fatalf("counts %q, want the runner's own two summary lines", got)
	}
	if r.Failures.Total != 1 {
		t.Fatalf("%d failures %q, want 1", r.Failures.Total, names(r))
	}
	f := failureNamed(t, r, "src/math.test.ts > add > negatives")
	joined := strings.Join(f.Lines, "\n")
	for _, want := range []string{"AssertionError: expected 1 to be -3", "- Expected", "+ Received", "- -3", "+ 1"} {
		if !strings.Contains(joined, want) {
			t.Errorf("the negatives block lacks %q:\n%s", want, joined)
		}
	}
	g := feedJS(t, "vitest_green.txt", &vitest{}).(*vitest)
	if !g.Claims() || g.passed != 5 || g.tests != "Tests 5 passed (5)" || g.Result().Failures.Total != 0 {
		t.Fatalf("green: claimed=%v passed=%d tests=%q; want 5, matching its \"Tests 5 passed\" line", g.Claims(), g.passed, g.tests)
	}
}

func TestVitestLoadError(t *testing.T) {
	r := feedJS(t, "vitest_load_error.txt", &vitest{}).Result()
	f := failureNamed(t, r, "src/broken.test.ts")
	joined := strings.Join(f.Lines, "\n")
	if !strings.Contains(joined, "Transform failed") || !strings.Contains(joined, "[PARSE_ERROR] Unexpected token") {
		t.Fatalf("the load error block lacks the transform/syntax error:\n%s", joined)
	}
	if r.Failures.Total != 1 {
		t.Fatalf("%d failures, want the suite that failed to load (every test passed)", r.Failures.Total)
	}
}

func TestJestCounts(t *testing.T) {
	p := feedJS(t, "jest_red.txt", &jest{})
	if !p.Claims() {
		t.Fatal("jest_red.txt is not claimed")
	}
	r := p.Result()
	if got := strings.Join(r.Counts, " · "); got != "Test Suites: 1 failed, 1 passed, 2 total · Tests: 1 failed, 3 passed, 4 total" {
		t.Fatalf("counts %q, want the runner's own two summary lines", got)
	}
	if r.Failures.Total != 1 {
		t.Fatalf("%d failures %q, want the one ● block", r.Failures.Total, names(r))
	}
	f := failureNamed(t, r, "math.test.js › add › negatives")
	if joined := strings.Join(f.Lines, "\n"); !strings.Contains(joined, "Expected: -3") || !strings.Contains(joined, "Received: 1") {
		t.Fatalf("the ● block lacks its expectation:\n%s", joined)
	}
}

func TestJestSuiteFailed(t *testing.T) {
	r := feedJS(t, "jest_suite_failed.txt", &jest{}).Result()
	f := failureNamed(t, r, "broken.test.js › Test suite failed to run")
	if !strings.Contains(strings.Join(f.Lines, "\n"), "Cannot find module './does-not-exist'") {
		t.Fatalf("the suite failure lacks its cause: %q", f.Lines)
	}
	if r.Failures.Total != 1 {
		t.Fatalf("%d failures, want the suite that failed to run (every test passed)", r.Failures.Total)
	}
}

// TestJestConsoleNotAFailure: a passing file's logged output prints under
// "● Console" in a human's (non-agent) run; it is not a failure.
func TestJestConsoleNotAFailure(t *testing.T) {
	r := feedJS(t, "jest_console.txt", &jest{}).Result()
	if got := strings.Join(names(r), " | "); got != "math.test.js › add › negatives" {
		t.Fatalf("failures %q, want only the failing test — never a Console block", got)
	}
}

// TestVitestUnhandledErrors: each error under "Unhandled Errors" is a failure
// carrying its own error, never the section's preamble, and the run's
// "Errors" summary line is counted.
func TestVitestUnhandledErrors(t *testing.T) {
	p := feedJS(t, "vitest_unhandled.txt", &vitest{})
	r := p.Result()
	if !p.Claims() || r.Failures.Total != 1 {
		t.Fatalf("claimed=%v failures=%q, want one unhandled-error failure", p.Claims(), names(r))
	}
	f := failureNamed(t, r, "Unhandled Rejection")
	joined := strings.Join(f.Lines, "\n")
	if !strings.Contains(joined, "Error: leaked rejection") || strings.Contains(joined, "Vitest caught") {
		t.Fatalf("the unhandled block = %q, want its own error and not the preamble", f.Lines)
	}
	if !strings.Contains(strings.Join(r.Counts, " · "), "Errors 1 error") {
		t.Fatalf("counts %q lack the Errors line", r.Counts)
	}
	out := Render(Outcome{Exit: 1, Ran: true}, []*Segment{segmentOf(t, "vitest_unhandled.txt", &vitest{})}, &Budget{}, logPath, time.Second)
	if strings.HasPrefix(out, "PASS") {
		t.Fatalf("an unhandled error rendered green:\n%s", out)
	}
}

// TestVitestMergedErrors: vitest prints one shared error under several FAIL
// headers; each header is a failure carrying that error.
func TestVitestMergedErrors(t *testing.T) {
	r := feedJS(t, "vitest_merged.txt", &vitest{}).Result()
	if r.Failures.Total != 2 {
		t.Fatalf("%d failures %q, want case 1 and case 2", r.Failures.Total, names(r))
	}
	for _, n := range []string{"src/same.test.ts > case 1", "src/same.test.ts > case 2"} {
		if f := failureNamed(t, r, n); !strings.Contains(strings.Join(f.Lines, "\n"), "Error: same failure") {
			t.Errorf("%s lacks the shared error: %q", n, f.Lines)
		}
	}
}

// TestSharedErrorBounded: a thousand FAIL headers over one error keep at most
// MaxBlocks names and still count every one.
func TestSharedErrorBounded(t *testing.T) {
	v := &vitest{}
	for i := 0; i < 1000; i++ {
		v.Feed(" FAIL  src/x.test.ts > case " + strings.Repeat("n", 300))
	}
	v.Feed("Error: boom")
	v.Feed("⎯⎯⎯⎯[1/1000]⎯")
	if v.cur != nil {
		t.Fatal("the block did not close")
	}
	r := v.Result()
	if r.Failures.Total != 1000 || len(r.Failures.Kept) != MaxBlocks {
		t.Fatalf("total=%d kept=%d, want 1000 counted and %d kept", r.Failures.Total, len(r.Failures.Kept), MaxBlocks)
	}
}

// TestVitestNoTests: when every file fails to load, vitest prints "Tests  no
// tests"; the run is still claimed and the load errors are its failures.
func TestVitestNoTests(t *testing.T) {
	p := feedJS(t, "vitest_no_tests.txt", &vitest{})
	r := p.Result()
	if !p.Claims() || !strings.Contains(strings.Join(r.Counts, " · "), "Tests no tests") {
		t.Fatalf("claimed=%v counts=%q, want the no-tests run claimed", p.Claims(), r.Counts)
	}
	f := failureNamed(t, r, "only/broken.test.ts")
	if !strings.Contains(strings.Join(f.Lines, "\n"), "Transform failed") {
		t.Fatalf("the load error is missing: %q", f.Lines)
	}
}

// segmentOf is a closed Segment fed a fixture through p alone.
func segmentOf(t *testing.T, name string, p Parser) *Segment {
	t.Helper()
	s := &Segment{parsers: []Parser{p}}
	for _, l := range fixtureLines(t, filepath.Join("testdata", "js", name)) {
		s.Write([]byte(l + "\n"))
	}
	s.Close()
	return s
}

func TestJSForcedColour(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("testdata", "js", "vitest_red_color.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "\x1b[") {
		t.Fatal("vitest_red_color.txt holds no ANSI sequence — the comparison proves nothing")
	}
	plain := feedJS(t, "vitest_red.txt", &vitest{}).Result()
	colour := feedJS(t, "vitest_red_color.txt", &vitest{}).Result()
	if !reflect.DeepEqual(plain, colour) {
		t.Fatalf("the coloured run parses differently:\nplain  %+v\ncolour %+v", plain, colour)
	}
}

func TestJSTruncatedFallsBack(t *testing.T) {
	v := &vitest{}
	for _, l := range fixtureLines(t, filepath.Join("testdata", "js", "vitest_red.txt")) {
		if vitestTests.MatchString(l) {
			break
		}
		v.Feed(l)
	}
	if v.Claims() {
		t.Fatal("vitest output cut before its Tests line is claimed")
	}
	j := &jest{}
	for _, l := range fixtureLines(t, filepath.Join("testdata", "js", "jest_red.txt")) {
		if jestTests.MatchString(l) {
			break
		}
		j.Feed(l)
	}
	if j.Claims() {
		t.Fatal("jest output cut before its Tests: line is claimed")
	}
}

func TestJSDoesNotClaimGo(t *testing.T) {
	paths, _ := filepath.Glob(filepath.Join("testdata", "gotest", "*.txt"))
	if len(paths) == 0 {
		t.Fatal("no go fixtures — nothing to check")
	}
	for _, p := range paths {
		for _, parser := range []Parser{&vitest{}, &jest{}} {
			for _, l := range fixtureLines(t, p) {
				parser.Feed(clean(l))
			}
			if parser.Claims() {
				t.Errorf("%T claims %s", parser, p)
			}
		}
	}
	// And across runners: neither claims the other's output.
	if feedJS(t, "jest_red.txt", &vitest{}).Claims() || feedJS(t, "vitest_red.txt", &jest{}).Claims() {
		t.Error("vitest and jest claim each other's output")
	}
}

func TestJSFixturesCaptured(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join("testdata", "js", "*.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) < 6 {
		t.Fatalf("%d js fixtures, want the 6 these parsers are tested against", len(paths))
	}
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		want := vitestHeader
		if strings.HasPrefix(filepath.Base(p), "jest_") {
			want = "# captured: jest@" + jestPin + " "
		}
		if !strings.HasPrefix(string(b), want) {
			t.Errorf("%s does not start with %q", p, want)
		}
	}
}
