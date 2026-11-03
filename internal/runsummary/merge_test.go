package runsummary

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"
)

// fixtureSegment is a closed Segment fed every fixture in paths, in order,
// through a fresh instance of every registered parser.
func fixtureSegment(t *testing.T, paths ...string) *Segment {
	t.Helper()
	s := NewSegment()
	for _, p := range paths {
		for _, l := range fixtureLines(t, p) {
			s.Write([]byte(l + "\n"))
		}
	}
	s.Close()
	return s
}

// claimers names the parsers that claim a closed segment, in registry order.
func claimers(s *Segment) []string {
	var out []string
	for _, p := range s.parsers {
		if p.Claims() {
			out = append(out, fmt.Sprintf("%T", p))
		}
	}
	return out
}

// TestMixedRedBothRunners: t-2's flutter red and t-6's vitest red fed into one
// segment print the union of both runners' failures, and the FAIL line
// carries both runners' counts.
func TestMixedRedBothRunners(t *testing.T) {
	s := fixtureSegment(t, filepath.Join("testdata", "dart", "compact_red.txt"), filepath.Join("testdata", "js", "vitest_console_fail.txt"))
	out := Render(Outcome{Exit: 1, Ran: true}, []*Segment{s}, &Budget{}, logPath, time.Second)
	head := outLines(out)[0]
	if !strings.HasPrefix(head, "FAIL (exit 1) · ") || !strings.Contains(head, "dart test: +20 -2") || !strings.Contains(head, "vitest: ") || !strings.Contains(head, "· 3 failing ·") {
		t.Fatalf("header %q, want both runners' counts and 3 failing", head)
	}
	for _, n := range []string{dartFoldName, dartShellName, "default  src/lib/zz-dross-fixture.test.ts > dross fixture > plain check"} {
		if !hasLine(out, "✗ "+n) {
			t.Errorf("no ✗ %s in:\n%s", n, out)
		}
	}
}

// TestMixedSharesMaxBlocks: two runners' failures share the one MaxBlocks
// budget — never MaxBlocks each.
func TestMixedSharesMaxBlocks(t *testing.T) {
	withParsers(t, newFake, newFake)
	out := Render(Outcome{Exit: 1, Ran: true}, []*Segment{segment(failures(8, 1, 10))}, &Budget{}, logPath, time.Second)
	if n := strings.Count(out, "\n✗ "); n != MaxBlocks {
		t.Fatalf("%d ✗ blocks, want exactly %d:\n%s", n, MaxBlocks, out)
	}
	if !hasLine(out, "+6 "+MoreFailingPrefix+" "+logPath) {
		t.Fatalf("no +6 more-failing line:\n%s", out)
	}
}

// TestMixedHeaderFits: a header past LineCut sheds count phrases, never a
// runner's name.
func TestMixedHeaderFits(t *testing.T) {
	long := func(runner string) Result {
		var cs []string
		for i := 0; i < 6; i++ {
			cs = append(cs, fmt.Sprintf("Phrase %d %s", i, strings.Repeat("c", 30)))
		}
		return Result{Runner: runner, Counts: cs}
	}
	h := header("FAIL (exit 1) · ", []Result{long("dart test"), long("vitest")}, " · 12 failing · 1m12s")
	if len(h) > LineCut || !strings.Contains(h, "dart test") || !strings.Contains(h, "vitest") || !strings.HasSuffix(h, " · 12 failing · 1m12s") {
		t.Fatalf("header (%d bytes) %q, want ≤ %d with both runners and the tail", len(h), h, LineCut)
	}
	short := []Result{{Runner: "dart test", Counts: []string{"+22"}}, {Runner: "vitest", Counts: []string{"Tests 3 passed (3)"}}}
	if h := header("PASS · ", short, " · 1.0s"); h != "PASS · dart test: +22 · vitest: Tests 3 passed (3) · 1.0s" {
		t.Fatalf("a header that fits changed: %q", h)
	}
}

// TestNoCrossClaims: every runner's fixtures are claimed by exactly its own
// parser — strict claims are what makes merging safe — and only the mixed
// fixtures by two: flutter test's and vitest's. Result() is read twice on
// every claimer, the mixed ones included, and must not change: a merged
// segment reads each claiming parser once more.
func TestNoCrossClaims(t *testing.T) {
	own := map[string]map[string]bool{
		"gotest": {"*runsummary.goTest": true},
		"js":     {"*runsummary.vitest": true, "*runsummary.jest": true},
		"gradle": {"*runsummary.gradle": true},
		"dart":   {"*runsummary.dart": true},
		"pytest": {"*runsummary.pytest": true},
	}
	for dir, mine := range own {
		paths, _ := filepath.Glob(filepath.Join("testdata", dir, "*.txt"))
		if len(paths) == 0 {
			t.Fatalf("no %s fixtures", dir)
		}
		for _, p := range paths {
			s := fixtureSegment(t, p)
			got := claimers(s)
			if len(got) != 1 || !mine[got[0]] {
				t.Errorf("%s is claimed by %q, want exactly one of %v", p, got, keys(mine))
			}
			resultTwice(t, s, p)
		}
	}
	mixed, _ := filepath.Glob(filepath.Join("testdata", "mixed", "*.txt"))
	if len(mixed) == 0 {
		t.Fatal("no mixed fixtures")
	}
	want := []string{"*runsummary.dart", "*runsummary.vitest"}
	for _, p := range mixed {
		s := fixtureSegment(t, p)
		got := claimers(s)
		sort.Strings(got)
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s is claimed by %q, want exactly %q", p, got, want)
		}
		resultTwice(t, s, p)
	}
}

// resultTwice fails when a claiming parser's Result() changes on a second
// read.
func resultTwice(t *testing.T, s *Segment, path string) {
	t.Helper()
	for _, pr := range s.parsers {
		if pr.Claims() && !reflect.DeepEqual(pr.Result(), pr.Result()) {
			t.Errorf("%T.Result() changes when read twice on %s", pr, path)
		}
	}
}

func keys(m map[string]bool) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}

// mixedHeader heads every mixed fixture: feastahead's own bare test command,
// run locally (helicon has no flutter).
const mixedHeader = "# captured: flutter@3.41.6+vitest@4.1.12 bash scripts/dross-test.sh (feastahead@"

func TestMixedFixturesCaptured(t *testing.T) {
	paths, _ := filepath.Glob(filepath.Join("testdata", "mixed", "*.txt"))
	if len(paths) < 2 {
		t.Fatalf("%d mixed fixtures, want the 2 the merge is tested against", len(paths))
	}
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(string(b), mixedHeader) {
			t.Errorf("%s does not start with %q", p, mixedHeader)
		}
	}
}

// registryRunners is the Runner label of each registered parser, in registry
// order — the order a merged header prints them in.
func registryRunners() []string {
	var out []string
	for _, p := range parsers() {
		out = append(out, p.Result().Runner)
	}
	return out
}

// TestMixedGreen: feastahead's two-lane spawn at exit 0 is one PASS line with
// both runners' counts, in registry order.
func TestMixedGreen(t *testing.T) {
	s := fixtureSegment(t, filepath.Join("testdata", "mixed", "feastahead_green.txt"))
	out := Render(Outcome{Exit: 0, Ran: true}, []*Segment{s}, &Budget{}, logPath, time.Second)
	head := outLines(out)[0]
	if !strings.HasPrefix(head, "PASS · ") || !strings.Contains(head, "dart test: +7") || !strings.Contains(head, "vitest: Test Files 1 passed (1), Tests 13 passed (13)") {
		t.Fatalf("header %q, want PASS with both runners' counts", head)
	}
	var order []string
	for _, r := range registryRunners() {
		if i := strings.Index(head, r+": "); i >= 0 {
			order = append(order, r)
		}
	}
	if len(order) != 2 || strings.Index(head, order[0]+": ") > strings.Index(head, order[1]+": ") {
		t.Fatalf("header %q does not print its runners in registry order %q", head, registryRunners())
	}
}

// TestMixedWebRed: flutter green then vitest red in one spawn prints the dart
// test counts and the vitest failure.
func TestMixedWebRed(t *testing.T) {
	s := fixtureSegment(t, filepath.Join("testdata", "mixed", "feastahead_web_red.txt"))
	out := Render(Outcome{Exit: 1, Ran: true}, []*Segment{s}, &Budget{}, logPath, time.Second)
	head := outLines(out)[0]
	if !strings.HasPrefix(head, "FAIL (exit 1) · ") || !strings.Contains(head, "dart test: +7") || !strings.Contains(head, "· 1 failing ·") {
		t.Fatalf("header %q, want FAIL with the dart test counts and 1 failing", head)
	}
	if !hasLine(out, "✗ |default| src/lib/zz-dross-fixture.test.ts > dross mixed red") {
		t.Fatalf("the vitest failure is missing:\n%s", out)
	}
}

// TestReadmeNamesSummaryRunners: the README's `dross test` row names every
// runner runsummary's registry recognises — the list is the registry's, so a
// parser registered without a README mention fails here — and says what a run
// with several runners prints. Checked inside the summary passage only, so a
// runner named somewhere else in the long row cannot stand in for the list.
func TestReadmeNamesSummaryRunners(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	row := ""
	for _, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(line, "| `dross test [selector...]` |") {
			row = line
		}
	}
	if row == "" {
		t.Fatal("README has no `dross test [selector...]` command row")
	}
	_, passage, ok := strings.Cut(row, "when the output is not recognised —")
	if !ok {
		t.Fatalf("the dross test row no longer describes the unrecognised-output fallback: %s", row)
	}
	passage, _, _ = strings.Cut(passage, "A summary never changes the exit status")
	runners := registryRunners()
	if len(runners) < 6 {
		t.Fatalf("the registry holds %d runners %q, want at least the six this phase ships", len(runners), runners)
	}
	for _, runner := range runners {
		if runner == "" || !strings.Contains(passage, runner) {
			t.Errorf("the dross test row's recognised runners do not name %q: %s", runner, passage)
		}
	}
	lower := strings.ToLower(passage)
	for _, want := range []string{"several runners in one run", "ten-block budget"} {
		if !strings.Contains(lower, want) {
			t.Errorf("the dross test row does not say %q (merged runners): %s", want, passage)
		}
	}
}
