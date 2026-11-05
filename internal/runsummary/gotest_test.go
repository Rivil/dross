package runsummary

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// capturedHeader is the first line of every testdata/gotest fixture: what
// produced it. Fixtures are real `go test` output, never written by hand.
const capturedHeader = "# captured: go1.27.1 go test -count=1 "

// fixtureLines reads a captured fixture, without its header line.
func fixtureLines(t *testing.T, path string) []string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")
	if strings.HasPrefix(lines[0], "# captured: ") {
		lines = lines[1:]
	}
	return lines
}

func parseGo(t *testing.T, name string) (*goTest, Result) {
	t.Helper()
	g := &goTest{}
	for _, l := range fixtureLines(t, filepath.Join("testdata", "gotest", name)) {
		g.Feed(l)
	}
	if !g.Claims() {
		t.Fatalf("%s is not claimed by the go test parser", name)
	}
	return g, g.Result()
}

func names(r Result) []string {
	var out []string
	for _, f := range r.Failures.Kept {
		out = append(out, f.Name)
	}
	return out
}

func failureNamed(t *testing.T, r Result, name string) Failure {
	t.Helper()
	for _, f := range r.Failures.Kept {
		if f.Name == name {
			return f
		}
	}
	t.Fatalf("no failure named %q in %q", name, names(r))
	return Failure{}
}

func TestGoGreenCounts(t *testing.T) {
	g, r := parseGo(t, "green.txt")
	if g.ok != 4 || g.noTestFiles != 2 || g.noTests != 1 || g.failed != 0 || r.Failures.Total != 0 {
		t.Fatalf("ok=%d no-test-files=%d no-tests-to-run=%d FAIL=%d failures=%d; want 4, 2, 1, 0, 0",
			g.ok, g.noTestFiles, g.noTests, g.failed, r.Failures.Total)
	}
	if got := strings.Join(r.Counts, ", "); got != "4 ok, 2 no test files, 1 no tests to run" {
		t.Fatalf("counts %q", got)
	}
}

func TestGoSubtestsAreLeaves(t *testing.T) {
	_, r := parseGo(t, "red_subtests.txt")
	if got := strings.Join(names(r), " "); got != "TestAdd TestTable/a TestTable/b TestTable/c" {
		t.Fatalf("failures %q, want the leaves TestAdd and TestTable/a, /b, /c — never the parent TestTable", got)
	}
}

func TestGoRedNamesOwnLines(t *testing.T) {
	_, r := parseGo(t, "red_subtests.txt")
	add := failureNamed(t, r, "TestAdd")
	joined := strings.Join(add.Lines, "\n")
	if !strings.Contains(joined, "m_test.go:7: Add(1, 2) = 4, want 3") {
		t.Fatalf("TestAdd's block lacks its own assertion line:\n%s", joined)
	}
	if strings.Contains(joined, "m_test.go:17") || strings.Contains(joined, "m_test.go:19") {
		t.Fatalf("TestTable's lines bled into TestAdd's block:\n%s", joined)
	}
	b := failureNamed(t, r, "TestTable/b")
	if len(b.Lines) != 2 || !strings.Contains(b.Lines[1], "Add(2, 2) = 5, want 4") {
		t.Fatalf("TestTable/b's block = %q, want its own two lines", b.Lines)
	}
}

func TestGoPanicAttributed(t *testing.T) {
	_, r := parseGo(t, "panic.txt")
	boom := failureNamed(t, r, "TestBoom")
	if len(boom.Lines) == 0 || !strings.HasPrefix(boom.Lines[0], "panic: boom") {
		t.Fatalf("TestBoom's block does not start with the panic: %q", boom.Lines)
	}
	joined := strings.Join(boom.Lines, "\n")
	if !strings.Contains(joined, "testing.tRunner.func1.2(") {
		t.Fatalf("TestBoom's block lacks the first stack frame:\n%s", joined)
	}
	if r.Failures.Total != 1 {
		t.Fatalf("%d failures, want TestBoom alone (the package is not a second one)", r.Failures.Total)
	}
}

func TestGoTimeout(t *testing.T) {
	_, r := parseGo(t, "timeout.txt")
	f := failureNamed(t, r, "example.com/timeout/s")
	if len(f.Lines) == 0 || f.Lines[0] != "panic: test timed out after 2s" {
		t.Fatalf("the package failure does not start with the timeout: %q", f.Lines)
	}
	if !strings.Contains(strings.Join(f.Lines, "\n"), "TestSlow") {
		t.Fatalf("the timeout block does not name the running test:\n%s", strings.Join(f.Lines, "\n"))
	}
	// In u, TestEarly failed before TestHang hung: the timeout is still the
	// package's own failure, and TestEarly's block holds only its own line.
	u := failureNamed(t, r, "example.com/timeout/u")
	if len(u.Lines) == 0 || u.Lines[0] != "panic: test timed out after 2s" || !strings.Contains(strings.Join(u.Lines, "\n"), "TestHang") {
		t.Fatalf("u's timeout is not its own package failure naming TestHang: %q", u.Lines)
	}
	early := failureNamed(t, r, "TestEarly")
	if strings.Contains(strings.Join(early.Lines, "\n"), "panic:") {
		t.Fatalf("the timeout bled into TestEarly's block: %q", early.Lines)
	}
}

func TestGoBuildFailed(t *testing.T) {
	g, r := parseGo(t, "build_failed.txt")
	if g.failed != 3 || g.ok != 1 {
		t.Fatalf("FAIL=%d ok=%d, want every [build failed] package (3) counted and 1 ok", g.failed, g.ok)
	}
	if !strings.Contains(strings.Join(r.Counts, ", "), "3 FAIL") {
		t.Fatalf("counts %q lack the failed packages", r.Counts)
	}
	withParsers(t, func() Parser { return &goTest{} })
	s := NewSegment()
	for _, l := range fixtureLines(t, filepath.Join("testdata", "gotest", "build_failed.txt")) {
		s.Write([]byte(l + "\n"))
	}
	s.Close()
	out := Render(Outcome{Exit: 1, Ran: true}, []*Segment{s}, &Budget{}, logPath, time.Second)
	if n := strings.Count(out, "undefined: foo"); n != 1 {
		t.Fatalf("\"undefined: foo\" printed %d times, want once:\n%s", n, out)
	}
}

func TestGoSetupAndVet(t *testing.T) {
	_, r := parseGo(t, "setup_and_vet.txt")
	if r.Failures.Total != 3 {
		t.Fatalf("%d failures %q, want the setup failure, the test-file compile error and the vet error", r.Failures.Total, names(r))
	}
	for name, want := range map[string]string{
		"example.com/setup/s":                            "no required module provides package example.com/setup/nope",
		"example.com/setup/c [example.com/setup/c.test]": "undefined: missing",
		"example.com/setup/v":                            "fmt.Printf format %d has arg",
	} {
		f := failureNamed(t, r, name)
		if !strings.Contains(strings.Join(f.Lines, "\n"), want) {
			t.Errorf("%s's block lacks %q: %q", name, want, f.Lines)
		}
	}
}

// TestGoVerboseNotClaimed: -v puts a test's lines before its --- FAIL, so this
// parser would print empty blocks; the output takes the fallback instead.
func TestGoVerboseNotClaimed(t *testing.T) {
	g := &goTest{}
	for _, l := range []string{"=== RUN   TestAdd", "    m_test.go:7: bad", "--- FAIL: TestAdd (0.00s)", "FAIL", "FAIL\texample.com/m\t0.1s"} {
		g.Feed(l)
	}
	if g.Claims() {
		t.Fatal("the go test parser claims -v output")
	}
}

func TestGoClaimsOnlyGo(t *testing.T) {
	foreign := map[string]string{
		"make":   "make -C web build\nok  done\nFAIL: nothing\nok\n",
		"vitest": " ✓ src/math.test.ts (4 tests) 3ms\n\n Test Files  1 passed (1)\n      Tests  4 passed (4)\n   Duration  412ms\n",
	}
	js, _ := filepath.Glob(filepath.Join("testdata", "js", "*.txt"))
	for _, p := range js {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		foreign[p] = string(b)
	}
	for name, text := range foreign {
		g := &goTest{}
		for _, l := range strings.Split(text, "\n") {
			g.Feed(l)
		}
		if g.Claims() {
			t.Errorf("the go test parser claims %s output", name)
		}
	}
}

func TestGoFixturesCaptured(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join("testdata", "gotest", "*.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) < 10 {
		t.Fatalf("%d gotest fixtures, want at least the 10 this parser is tested against", len(paths))
	}
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(string(b), capturedHeader) {
			t.Errorf("%s does not start with %q", p, capturedHeader)
		}
	}
}

// TestGoBlockDoesNotAbsorbNextPackage: a "# dep" build block printed before
// another package's TestMain output keeps only its compile lines; the
// TestMain package's failure holds its own output.
func TestGoBlockDoesNotAbsorbNextPackage(t *testing.T) {
	g, r := parseGo(t, "build_then_testmain.txt")
	dep := failureNamed(t, r, "example.com/orphan/dep")
	if got := strings.Join(dep.Lines, "\n"); got != "dep/dep.go:3:9: undefined: foo" {
		t.Fatalf("the dep block = %q, want only its compile line", got)
	}
	a := failureNamed(t, r, "example.com/orphan/a")
	if !strings.Contains(strings.Join(a.Lines, "\n"), "TESTMAIN-MARK: fixture database unreachable") {
		t.Fatalf("the TestMain package's failure = %q, want its own output", a.Lines)
	}
	if r.Failures.Total != 2 || g.failed != 3 {
		t.Fatalf("failures %q, FAIL=%d; want dep and a, and 3 FAIL packages", names(r), g.failed)
	}
}

// TestGoHashHeadingNotABuildBlock: a test printing "# Results" opens no build
// failure — red names only the failing test, green renders PASS.
func TestGoHashHeadingNotABuildBlock(t *testing.T) {
	_, r := parseGo(t, "hash_heading.txt")
	if got := names(r); len(got) != 1 || got[0] != "TestReport" {
		t.Fatalf("failures %q, want only TestReport", got)
	}
	if !strings.Contains(strings.Join(failureNamed(t, r, "TestReport").Lines, "\n"), "HEADING-MARK") {
		t.Fatalf("TestReport lacks its own output: %q", failureNamed(t, r, "TestReport").Lines)
	}
	withParsers(t, func() Parser { return &goTest{} })
	s := NewSegment()
	for _, l := range fixtureLines(t, filepath.Join("testdata", "gotest", "hash_heading_green.txt")) {
		s.Write([]byte(l + "\n"))
	}
	s.Close()
	out := Render(Outcome{Exit: 0, Ran: true}, []*Segment{s}, &Budget{}, logPath, time.Second)
	if !strings.HasPrefix(out, "PASS · go test: 1 ok") {
		t.Fatalf("the green heading run rendered:\n%s", out)
	}
}

// TestGoOneWordBuildStillReported: a real build error in a one-word module is
// still one build failure, and its package is counted FAIL.
func TestGoOneWordBuildStillReported(t *testing.T) {
	g, r := parseGo(t, "build_failed_oneword.txt")
	if r.Failures.Total != 1 || g.failed != 1 {
		t.Fatalf("failures %q, FAIL=%d; want the one build failure", names(r), g.failed)
	}
	f := failureNamed(t, r, "oneword [oneword.test]")
	if !strings.Contains(strings.Join(f.Lines, "\n"), "undefined: foo") {
		t.Fatalf("the oneword block lacks its compile error: %q", f.Lines)
	}
}

// TestGoOneWordVetBlock: a one-word module's vet failure prints "# oneword"
// then vet's own "# [oneword]" — the shape setup_and_vet.txt captured for a
// dotted path — and is one build failure named oneword, even with another
// package's result line before its [build failed] one.
func TestGoOneWordVetBlock(t *testing.T) {
	g, r := feedGo(
		"# oneword",
		"# [oneword]",
		"./x_test.go:9:14: fmt.Printf format %d has arg \"x\" of wrong type string",
		"ok  \toneword/ok\t0.221s",
		"FAIL\toneword [build failed]",
		"FAIL",
	)
	if r.Failures.Total != 1 || g.failed != 1 || g.ok != 1 {
		t.Fatalf("failures %q, FAIL=%d ok=%d; want the one vet failure", names(r), g.failed, g.ok)
	}
	f := r.Failures.Kept[0]
	if f.Name != "oneword" || !strings.Contains(strings.Join(f.Lines, "\n"), "wrong type string") {
		t.Fatalf("failure %q = %q, want oneword with its vet diagnostic", f.Name, f.Lines)
	}
}

// feedGo runs lines through a fresh go test parser.
func feedGo(lines ...string) (*goTest, Result) {
	g := &goTest{}
	for _, l := range lines {
		g.Feed(l)
	}
	return g, g.Result()
}

// TestGoBlockKeepsNonDiagnosticErrors: a build or setup block whose lines are
// not file:line shaped — an import cycle, a linker/cgo failure — keeps its
// message rather than losing it at its package's FAIL line.
func TestGoBlockKeepsNonDiagnosticErrors(t *testing.T) {
	_, r := feedGo(
		"# example.com/cyc/a",
		"package example.com/cyc/a",
		"\timports example.com/cyc/b",
		"\timports example.com/cyc/a: import cycle not allowed",
		"FAIL\texample.com/cyc/a [setup failed]",
		"# example.com/cgo/c",
		"/usr/local/go/pkg/tool/darwin_arm64/link: running clang failed: exit status 1",
		"clang: error: linker command failed with exit code 1",
		"FAIL\texample.com/cgo/c [build failed]",
		"FAIL",
	)
	for name, want := range map[string]string{
		"example.com/cyc/a": "import cycle not allowed",
		"example.com/cgo/c": "clang: error: linker command failed",
	} {
		if got := strings.Join(failureNamed(t, r, name).Lines, "\n"); !strings.Contains(got, want) {
			t.Errorf("%s = %q, want %q kept", name, got, want)
		}
	}
	if r.Failures.Total != 2 {
		t.Fatalf("failures %q, want the two blocks only", names(r))
	}
}

// TestGoHeadingThenIndentedLine: "# Results" followed by an indented row is a
// test's output, not a build block.
func TestGoHeadingThenIndentedLine(t *testing.T) {
	_, r := feedGo(
		"# Results",
		"  row 1 ok",
		"--- FAIL: TestReport (0.00s)",
		"    h_test.go:13: mismatch",
		"FAIL",
		"FAIL\texample.com/heading/h\t0.05s",
		"FAIL",
	)
	if got := names(r); len(got) != 1 || got[0] != "TestReport" {
		t.Fatalf("failures %q, want only TestReport", got)
	}
}
