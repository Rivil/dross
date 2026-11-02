package runsummary

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"
)

// pytestHeader heads every pytest fixture: sunday-rider's test container.
const pytestHeader = "# captured: pytest@9.1.1 docker compose --profile test run --rm test "

const (
	pyParamA = "tests/test_parsing_keywords.py::test_unrecognised_names_the_ignored_clause[2h gravel ride ending at the pub-2h gravel ride, ending at the pub-ending at the pub]"
	pyParamB = "tests/test_parsing_keywords.py::test_unrecognised_names_the_ignored_clause[gravel 90 min in the rain-gravel 90 min, in the rain-in the rain]"
	pyPlain  = "tests/test_parsing_keywords.py::test_comma_form_is_pinned_exactly"
)

func pytestSegment(t *testing.T, name string, ps ...Parser) *Segment {
	t.Helper()
	s := &Segment{parsers: ps}
	for _, l := range fixtureLines(t, filepath.Join("testdata", "pytest", name)) {
		s.Write([]byte(l + "\n"))
	}
	s.Close()
	return s
}

func parsePytest(t *testing.T, name string) Result {
	t.Helper()
	p := &pytest{}
	pytestSegment(t, name, p)
	if !p.Claims() {
		t.Fatalf("%s is not claimed by the pytest parser", name)
	}
	return p.Result()
}

// untimed drops the run's clock from count phrases: two runs never agree on it.
var pyClock = regexp.MustCompile(` in \d+(?:\.\d+)?s$`)

func untimed(cs []string) []string {
	out := make([]string, len(cs))
	for i, c := range cs {
		out[i] = pyClock.ReplaceAllString(c, "")
	}
	return out
}

// TestPytestFormatsAgree: reporter_formats — default and -q give the same
// node ids and count phrases for the same break.
func TestPytestFormatsAgree(t *testing.T) {
	for _, pair := range [][2]string{{"default_red.txt", "quiet_red.txt"}, {"default_green.txt", "quiet_green.txt"}} {
		d, q := parsePytest(t, pair[0]), parsePytest(t, pair[1])
		if !reflect.DeepEqual(names(d), names(q)) || !reflect.DeepEqual(untimed(d.Counts), untimed(q.Counts)) {
			t.Errorf("%s and %s disagree:\ndefault %q %q\nquiet   %q %q", pair[0], pair[1], d.Counts, names(d), q.Counts, names(q))
		}
	}
}

// TestPytestRedNodeIDs: names are whole node ids, the parametrized ids — with
// their spaces and commas — included, and no " - message" tail.
func TestPytestRedNodeIDs(t *testing.T) {
	for _, f := range []string{"default_red.txt", "quiet_red.txt"} {
		r := parsePytest(t, f)
		if got := names(r); !reflect.DeepEqual(got, []string{pyParamA, pyParamB, pyPlain}) || r.Failures.Total != 3 {
			t.Errorf("%s: names %q (total %d)", f, got, r.Failures.Total)
		}
	}
}

// TestPytestRedOwnLines: each failure carries its own section's "E   " lines;
// a parametrized case carries ITS section, not its sibling's.
func TestPytestRedOwnLines(t *testing.T) {
	own := map[string]string{
		pyParamA: "E       AssertionError: ['ride ending at the pub']",
		pyParamB: "E       AssertionError: ['in the rain']",
		pyPlain:  "E       AssertionError: assert ['to the moon'] == ['to the moon!']",
	}
	for _, f := range []string{"default_red.txt", "quiet_red.txt"} {
		r := parsePytest(t, f)
		for name, line := range own {
			fl := failureNamed(t, r, name)
			joined := strings.Join(fl.Lines, "\n")
			if !strings.Contains(joined, line) {
				t.Errorf("%s: %s lacks %q:\n%s", f, name, line, joined)
			}
			for other, ol := range own {
				if other != name && strings.Contains(joined, ol) {
					t.Errorf("%s: %s carries %s's %q", f, name, other, ol)
				}
			}
			if len(fl.Lines) == 0 || !strings.HasPrefix(fl.Lines[0], ">") {
				t.Errorf("%s: %s does not open on its failing line: %q", f, name, fl.Lines)
			}
		}
	}
}

// TestPytestGreenCounts: the count is pytest's own line, "=" padding and the
// h:mm:ss clock stripped.
func TestPytestGreenCounts(t *testing.T) {
	for f, want := range map[string]string{"default_green.txt": "1982 passed in 63.64s", "quiet_green.txt": "1982 passed in 62.91s"} {
		r := parsePytest(t, f)
		if !reflect.DeepEqual(r.Counts, []string{want}) || r.Failures.Total != 0 || r.Runner != "pytest" {
			t.Errorf("%s: runner=%q counts=%q failures=%q, want pytest [%q]", f, r.Runner, r.Counts, names(r), want)
		}
	}
	out := Render(Outcome{Exit: 0, Ran: true}, []*Segment{pytestSegment(t, "quiet_green.txt", &pytest{})}, &Budget{}, logPath, time.Second)
	if !strings.HasPrefix(out, "PASS · pytest: 1982 passed in 62.91s · ") {
		t.Fatalf("green rendered:\n%s", out)
	}
}

// TestPytestClaimsOnlyItsSummary: a wrapper's own lines never forge a claim.
func TestPytestClaimsOnlyItsSummary(t *testing.T) {
	lines := fixtureLines(t, filepath.Join("testdata", "pytest", "quiet_green.txt"))
	p := &pytest{}
	for _, l := range lines {
		if strings.HasPrefix(l, "1982 passed in ") {
			continue
		}
		p.Feed(l)
	}
	if p.Claims() {
		t.Fatalf("quiet_green.txt minus its pytest count line is claimed as %q", p.counts)
	}
	for _, l := range []string{"Found 3 errors in 2 files (checked 88 source files)", "Found 2 errors.", "Found 1 error in 1 file (checked 88 source files)", "Success: no issues found in 88 source files"} {
		p := &pytest{}
		p.Feed(l)
		if p.Claims() {
			t.Errorf("%q alone is claimed", l)
		}
	}
}

// TestPytestGreenUnderRedWrapper: pytest green under a selftest that failed on
// mypy is not a green run — it renders the fallback.
func TestPytestGreenUnderRedWrapper(t *testing.T) {
	seg := NewSegment()
	for _, l := range fixtureLines(t, filepath.Join("testdata", "pytest", "selftest_mypy_red.txt")) {
		seg.Write([]byte(l + "\n"))
	}
	seg.Close()
	out := Render(Outcome{Exit: 1, Ran: true}, []*Segment{seg}, &Budget{}, logPath, time.Second)
	if !strings.HasPrefix(out, "exit 1 · no failing test reported") || strings.Contains(out, "PASS") {
		t.Fatalf("selftest_mypy_red rendered:\n%s", out)
	}
	if !strings.Contains(out, "selftest FAILED: ruff-format mypy") {
		t.Fatalf("the fallback tail lacks the wrapper's verdict:\n%s", out)
	}
}

// TestPytestCollectError: a module that fails to collect is one failure named
// by its path, carrying its error, and the counts keep pytest's "error".
func TestPytestCollectError(t *testing.T) {
	r := parsePytest(t, "collect_error.txt")
	if got := names(r); !reflect.DeepEqual(got, []string{"tests/test_parsing_keywords.py"}) {
		t.Fatalf("names %q, want the module path", got)
	}
	joined := strings.Join(r.Failures.Kept[0].Lines, "\n")
	if !strings.Contains(joined, "E   ModuleNotFoundError: No module named 'sundayride_no_such_module'") {
		t.Fatalf("the collection block lacks its error:\n%s", joined)
	}
	if !reflect.DeepEqual(r.Counts, []string{"1 error in 2.33s"}) {
		t.Fatalf("counts %q, want pytest's 1 error phrase", r.Counts)
	}
}

// TestPytestFrameSeparatorKeepsSection: "_ _ _" between traceback frames does
// not open a section of its own and cut the failure off.
func TestPytestFrameSeparatorKeepsSection(t *testing.T) {
	p := &pytest{}
	for _, l := range []string{
		"=================================== FAILURES ===================================",
		"___________________________________ test_x ____________________________________",
		">       helper()",
		"_ _ _ _ _ _ _ _ _ _ _ _ _ _ _ _ _ _ _ _ _ _ _ _ _ _ _ _ _ _ _ _ _ _ _ _ _ _ _ _",
		">       assert 1 == 2",
		"E       assert 1 == 2",
		"=========================== short test summary info ============================",
		"FAILED tests/test_a.py::test_x - assert 1 == 2",
		"============================== 1 failed in 0.01s ===============================",
	} {
		p.Feed(l)
	}
	r := p.Result()
	if !p.Claims() || !strings.Contains(strings.Join(failureNamed(t, r, "tests/test_a.py::test_x").Lines, "\n"), "E       assert 1 == 2") {
		t.Fatalf("the frame separator cut the section: %+v", r.Failures)
	}
}

// TestPytestFixturesCaptured: every pytest fixture is a live sunday-rider run.
func TestPytestFixturesCaptured(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join("testdata", "pytest", "*.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) < 6 {
		t.Fatalf("%d pytest fixtures, want the 6 this parser is tested against", len(paths))
	}
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		first, _, _ := strings.Cut(string(b), "\n")
		if !strings.HasPrefix(first, pytestHeader) || !strings.Contains(first, " (sunday-rider@") {
			t.Errorf("%s does not start with %q<command> (sunday-rider@<sha>)", p, pytestHeader)
		}
		if !strings.Contains(filepath.Base(p), "green") && !strings.Contains(first, " break: ") {
			t.Errorf("%s is red but its header names no break", p)
		}
	}
}

// TestPytestPlainIDBracketMessage: a plain test's message may hold brackets;
// the id stops at " - " and the failure keeps its section.
func TestPytestPlainIDBracketMessage(t *testing.T) {
	cases := map[string]string{
		"tests/a.py::test_x - assert [1] == [2]":          "tests/a.py::test_x",
		"tests/a.py::test_x - assert a[0] - 1 == 2":       "tests/a.py::test_x",
		"tests/a.py::test_x[a - b] - assert [1] == [2]":   "tests/a.py::test_x[a - b]",
		"tests/a.py::test_x[1-2]":                         "tests/a.py::test_x[1-2]",
		"tests/a.py::TestC::test_y - AssertionError: ...": "tests/a.py::TestC::test_y",
	}
	for in, want := range cases {
		if got := nodeID(in); got != want {
			t.Errorf("nodeID(%q) = %q, want %q", in, got, want)
		}
	}
	p := &pytest{}
	for _, l := range []string{
		"=================================== FAILURES ===================================",
		"____________________________________ test_x ____________________________________",
		">       assert [1] == [2]",
		"E       assert [1] == [2]",
		"=========================== short test summary info ============================",
		"FAILED tests/a.py::test_x - assert [1] == [2]",
		"============================== 1 failed in 0.01s ===============================",
	} {
		p.Feed(l)
	}
	if f := failureNamed(t, p.Result(), "tests/a.py::test_x"); !strings.Contains(strings.Join(f.Lines, "\n"), "E       assert [1] == [2]") {
		t.Fatalf("the bracketed failure lost its section: %q", f.Lines)
	}
}

// TestPytestSameNameTwoModules: two modules' test_x print sections with the
// same header; each failure keeps its own, in print order.
func TestPytestSameNameTwoModules(t *testing.T) {
	p := &pytest{}
	for _, l := range []string{
		"=================================== FAILURES ===================================",
		"____________________________________ test_x ____________________________________",
		"E       assert 'A' == 'a'",
		"____________________________________ test_x ____________________________________",
		"E       assert 'B' == 'b'",
		"=========================== short test summary info ============================",
		"FAILED tests/a.py::test_x - AssertionError",
		"FAILED tests/b.py::test_x - AssertionError",
		"============================== 2 failed in 0.01s ===============================",
	} {
		p.Feed(l)
	}
	r := p.Result()
	for name, want := range map[string]string{"tests/a.py::test_x": "'A'", "tests/b.py::test_x": "'B'"} {
		if joined := strings.Join(failureNamed(t, r, name).Lines, "\n"); !strings.Contains(joined, want) {
			t.Errorf("%s = %q, want its own %s", name, joined, want)
		}
	}
}

// TestPytestSubtestCounts: pytest 9's subtest phrases still claim the run.
func TestPytestSubtestCounts(t *testing.T) {
	p := &pytest{}
	p.Feed("============== 1 failed, 3 passed, 2 subtests passed in 0.12s ===============")
	if !p.Claims() || p.counts != "1 failed, 3 passed, 2 subtests passed in 0.12s" {
		t.Fatalf("claimed=%v counts=%q", p.Claims(), p.counts)
	}
}
