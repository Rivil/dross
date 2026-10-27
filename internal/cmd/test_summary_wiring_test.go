package cmd

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Rivil/dross/internal/gatestate"
)

// summaryFixtureDir is runsummary's captured runner output, resolved while the
// test binary still runs from this package's directory.
var summaryFixtureDir = func() string {
	wd, _ := os.Getwd()
	return filepath.Join(wd, "..", "runsummary", "testdata")
}()

// runnerOutput is a captured fixture without its `# captured:` header.
func runnerOutput(t *testing.T, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(summaryFixtureDir, rel))
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if strings.HasPrefix(s, "# captured: ") {
		s = s[strings.Index(s, "\n")+1:]
	}
	return s
}

// spawnWriting makes every local spawn write out and return err.
func spawnWriting(t *testing.T, out string, err error) {
	t.Helper()
	orig := spawnLocal
	t.Cleanup(func() { spawnLocal = orig })
	spawnLocal = func(_, _ string, stdout, _ io.Writer) error {
		io.WriteString(stdout, out)
		return err
	}
}

// spawnByLine makes each local spawn whose line starts with a key write that
// key's output and return its error.
func spawnByLine(t *testing.T, by map[string]struct {
	out string
	err error
}) {
	t.Helper()
	orig := spawnLocal
	t.Cleanup(func() { spawnLocal = orig })
	spawnLocal = func(_, line string, stdout, _ io.Writer) error {
		for prefix, r := range by {
			if strings.HasPrefix(line, prefix) {
				io.WriteString(stdout, r.out)
				return r.err
			}
		}
		return nil
	}
}

// withoutLogLines drops the `log: ` lines from a transcript.
func withoutLogLines(out string) string {
	var keep []string
	for _, l := range strings.Split(out, "\n") {
		if !strings.HasPrefix(l, "log: ") {
			keep = append(keep, l)
		}
	}
	return strings.Join(keep, "\n")
}

func runTestCapturing(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var err error
	out := captureStdout(t, func() { err = runCmd(t, Test(), args...) })
	return out, err
}

func trustedTestFixture(t *testing.T) {
	t.Helper()
	testFixture(t, "go test -count=1 ./...")
	trustFixture(t)
}

func TestGreenRunPrintsSummaryOnly(t *testing.T) {
	trustedTestFixture(t)
	spawnWriting(t, runnerOutput(t, "gotest/green.txt"), nil)
	out, err := runTestCapturing(t)
	if err != nil {
		t.Fatal(err)
	}
	var logs []string
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(l, "log: ") {
			logs = append(logs, l)
		}
	}
	if len(logs) != 2 {
		t.Fatalf("%d `log: ` lines, want exactly two (before the first spawn and at the end):\n%s", len(logs), out)
	}
	for _, l := range logs {
		if !strings.Contains(l, string(filepath.Separator)+filepath.Join(".dross", "logs")+string(filepath.Separator)) {
			t.Errorf("log line %q is not under .dross/logs/", l)
		}
	}
	rest := strings.TrimSpace(withoutLogLines(out))
	if strings.Count(rest, "\n") != 0 || !strings.Contains(rest, "4 ok") || !strings.HasSuffix(rest, "s") {
		t.Errorf("want one counts line ending in the wall-clock, got:\n%s", rest)
	}
	if strings.Contains(rest, "example.com/green") || len(rest) > 200 {
		t.Errorf("green printed a package path or more than 200 bytes (%d):\n%s", len(rest), rest)
	}
}

func TestRedRunPrintsFailures(t *testing.T) {
	trustedTestFixture(t)
	spawnWriting(t, runnerOutput(t, "gotest/red_subtests.txt"), fakeExit{code: 1})
	out, err := runTestCapturing(t)
	if ExitCode(err) != exitSuiteFailed {
		t.Fatalf("exit %d, want %d", ExitCode(err), exitSuiteFailed)
	}
	for _, want := range []string{"TestTable/a", "Add(1, 1) = 3, want 2", "TestTable/b", "Add(2, 2) = 5, want 4", "TestTable/c", "Add(0, 0) = 1, want 0"} {
		if !strings.Contains(out, want) {
			t.Errorf("stdout lacks %q:\n%s", want, out)
		}
	}
}

func TestNonZeroAllOkFallsBack(t *testing.T) {
	trustedTestFixture(t)
	spawnWriting(t, runnerOutput(t, "gotest/green.txt"), fakeExit{code: 1})
	out, err := runTestCapturing(t)
	if ExitCode(err) != exitSuiteFailed {
		t.Fatalf("exit %d, want %d", ExitCode(err), exitSuiteFailed)
	}
	if strings.Contains(out, "PASS") || !strings.Contains(out, "exit 1 ") || !strings.Contains(out, "example.com/green/a") {
		t.Fatalf("an all-ok run that exited 1 must fall back to the exit status and the tail:\n%s", out)
	}
}

func TestTransportTailShown(t *testing.T) {
	t.Run("exit 3", func(t *testing.T) {
		grantedTestFixture(t, "go test ./...")
		orig := spawnRemote
		t.Cleanup(func() { spawnRemote = orig })
		spawnRemote = func(argv []string, _ string, _, stderr io.Writer) error {
			if argv[0] == "rsync" {
				return nil
			}
			io.WriteString(stderr, "ssh: connect to host h port 22: Connection refused\n")
			return fakeExit{code: 255}
		}
		out, err := runTestCapturing(t)
		if ExitCode(err) != exitTransport {
			t.Fatalf("exit %d, want %d", ExitCode(err), exitTransport)
		}
		if !strings.Contains(out, "ssh: connect to host h port 22: Connection refused") || !strings.Contains(out, "log: ") {
			t.Fatalf("exit 3 lacks the failing spawn's line or the log path:\n%s", out)
		}
	})
	t.Run("exit 4", func(t *testing.T) {
		grantedTestFixture(t, "go test ./...")
		orig := spawnRemote
		t.Cleanup(func() { spawnRemote = orig })
		spawnRemote = func(argv []string, _ string, _, stderr io.Writer) error {
			io.WriteString(stderr, "rsync: some files/attrs were not transferred\n")
			return fakeExit{code: 23}
		}
		out, err := runTestCapturing(t)
		if ExitCode(err) != exitPartial {
			t.Fatalf("exit %d, want %d", ExitCode(err), exitPartial)
		}
		if !strings.Contains(out, "rsync: some files/attrs were not transferred") || !strings.Contains(out, "log: ") {
			t.Fatalf("exit 4 lacks the failing spawn's line or the log path:\n%s", out)
		}
	})
	t.Run("exit 7", func(t *testing.T) {
		filesFixture(t, preparedGoLane)
		grantEveryLane(t)
		spawnByLine(t, map[string]struct {
			out string
			err error
		}{"make build": {"make: *** [build] Error 2\n", fakeExit{code: 2}}})
		out, err := runTestCapturing(t, "--files", "internal/a.go")
		if ExitCode(err) != exitPrepareFailed {
			t.Fatalf("exit %d, want %d", ExitCode(err), exitPrepareFailed)
		}
		if !strings.Contains(out, "make: *** [build] Error 2") || !strings.Contains(out, "log: ") {
			t.Fatalf("exit 7 lacks the prepare's line or the log path:\n%s", out)
		}
	})
}

func TestTwoLanesEachParsed(t *testing.T) {
	filesFixture(t, goAndDocsLanes)
	grantAllLanes(t)
	spawnByLine(t, map[string]struct {
		out string
		err error
	}{
		"go test":      {runnerOutput(t, "gotest/red_subtests.txt"), fakeExit{code: 1}},
		"markdownlint": {runnerOutput(t, "js/vitest_green.txt"), nil},
	})
	out, err := runTestCapturing(t, "--files", "internal/a.go", "--files", "docs/x.md")
	if ExitCode(err) != exitSuiteFailed {
		t.Fatalf("exit %d, want %d", ExitCode(err), exitSuiteFailed)
	}
	if !strings.Contains(out, "FAIL (exit 1) · go test") || !strings.Contains(out, "PASS · vitest") {
		t.Fatalf("want a red go-lane summary and a green vitest-lane summary:\n%s", out)
	}
}

func TestParsedLaneCannotMaskUnparsed(t *testing.T) {
	filesFixture(t, goAndDocsLanes)
	grantAllLanes(t)
	spawnByLine(t, map[string]struct {
		out string
		err error
	}{
		"go test":      {runnerOutput(t, "gotest/green.txt"), nil},
		"markdownlint": {"markdownlint: checked 3 files\nMASK-SENTINEL\n", nil},
	})
	out, err := runTestCapturing(t, "--files", "internal/a.go", "--files", "docs/x.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "go test: 4 ok") {
		t.Errorf("the go lane's counts are missing:\n%s", out)
	}
	if !strings.Contains(out, "exit 0 · output not recognised") || !strings.Contains(out, "MASK-SENTINEL") {
		t.Errorf("the unparsed lane's exit status and tail are missing:\n%s", out)
	}
}

// eightFailures is go test output with eight failing tests.
func eightFailures(pkg string) string {
	var b strings.Builder
	for i := 1; i <= 8; i++ {
		fmt.Fprintf(&b, "--- FAIL: Test%s%d (0.00s)\n    x_test.go:%d: bad %d\n", pkg, i, i, i)
	}
	fmt.Fprintf(&b, "FAIL\nFAIL\texample.com/%s\t0.01s\nFAIL\n", pkg)
	return b.String()
}

func TestBudgetAcrossLanes(t *testing.T) {
	filesFixture(t, goAndDocsLanes)
	grantAllLanes(t)
	spawnByLine(t, map[string]struct {
		out string
		err error
	}{
		"go test":      {eightFailures("A"), fakeExit{code: 1}},
		"markdownlint": {eightFailures("B"), fakeExit{code: 1}},
	})
	out, _ := runTestCapturing(t, "--files", "internal/a.go", "--files", "docs/x.md")
	if n := strings.Count(out, "\n✗ "); n != 10 {
		t.Errorf("%d full failure blocks across the two lanes, want 10:\n%s", n, out)
	}
	if !strings.Contains(out, "+6 more failing") {
		t.Errorf("no +6 more-failing line:\n%s", out)
	}
}

func TestSelectorMissNoTail(t *testing.T) {
	filesFixture(t, missLanes)
	grantAllLanes(t)
	touchFile(t, "docs/guide/x.md")
	spawnByLine(t, map[string]struct {
		out string
		err error
	}{"pytest": {"no tests ran\nMISS-SENTINEL\n", fakeExit{code: 5}}})
	out, err := runTestCapturing(t, "--files", "docs/guide/x.md")
	if ExitCode(err) != exitNothingMeasured {
		t.Fatalf("exit %d, want %d", ExitCode(err), exitNothingMeasured)
	}
	if !strings.Contains(out, "selector miss:") || strings.Contains(out, "MISS-SENTINEL") {
		t.Fatalf("a miss must print only its miss line:\n%s", out)
	}
}

func TestSummaryNotInError(t *testing.T) {
	trustedTestFixture(t)
	spawnWriting(t, runnerOutput(t, "gotest/red_subtests.txt"), fakeExit{code: 1})
	_, err := runTestCapturing(t)
	if err == nil {
		t.Fatal("a red run returned nil")
	}
	for _, line := range []string{"Add(1, 1)", "TestTable", "m_test.go"} {
		if strings.Contains(err.Error(), line) {
			t.Errorf("the returned error carries the runner's output (%q): %v", line, err)
		}
	}
}

func TestWallClockFromSeam(t *testing.T) {
	trustedTestFixture(t)
	spawnWriting(t, runnerOutput(t, "gotest/green.txt"), nil)
	start := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	calls := 0
	orig := testNow
	t.Cleanup(func() { testNow = orig })
	testNow = func() time.Time {
		calls++
		if calls == 1 {
			return start
		}
		return start.Add(75 * time.Second)
	}
	out, err := runTestCapturing(t)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "1m15s") {
		t.Fatalf("the summary does not carry the seam's 1m15s:\n%s", out)
	}
}

func TestExitCodesUnchanged(t *testing.T) {
	cases := []struct {
		name  string
		setup func(t *testing.T) []string
		want  int
	}{
		{"0 green", func(t *testing.T) []string {
			greenFixture(t) // a git repo, so the green record has a tree to vouch for
			spawnWriting(t, runnerOutput(t, "gotest/green.txt"), nil)
			return nil
		}, 0},
		{"1 red", func(t *testing.T) []string {
			trustedTestFixture(t)
			spawnWriting(t, runnerOutput(t, "gotest/red_subtests.txt"), fakeExit{code: 1})
			return nil
		}, exitSuiteFailed},
		{"3 unreachable", func(t *testing.T) []string {
			grantedTestFixture(t, "go test ./...")
			orig := spawnRemote
			t.Cleanup(func() { spawnRemote = orig })
			spawnRemote = func(argv []string, _ string, _, _ io.Writer) error {
				if argv[0] == "rsync" {
					return nil
				}
				return fakeExit{code: 255}
			}
			return nil
		}, exitTransport},
		{"4 partial", func(t *testing.T) []string {
			grantedTestFixture(t, "go test ./...")
			orig := spawnRemote
			t.Cleanup(func() { spawnRemote = orig })
			spawnRemote = func([]string, string, io.Writer, io.Writer) error { return fakeExit{code: 23} }
			return nil
		}, exitPartial},
		{"5 nothing measured", func(t *testing.T) []string {
			filesFixture(t, missLanes)
			grantAllLanes(t)
			touchFile(t, "docs/guide/x.md")
			spawnWriting(t, "", fakeExit{code: 5})
			return []string{"--files", "docs/guide/x.md"}
		}, exitNothingMeasured},
		{"6 lane refused", func(t *testing.T) []string {
			filesFixture(t, goAndDocsLanes)
			spawnWriting(t, "", nil)
			return []string{"--files", "internal/a.go"}
		}, exitLaneRefused},
		{"7 prepare failed", func(t *testing.T) []string {
			filesFixture(t, preparedGoLane)
			grantEveryLane(t)
			spawnWriting(t, "", fakeExit{code: 2})
			return []string{"--files", "internal/a.go"}
		}, exitPrepareFailed},
		{"8 toolchain missing", func(t *testing.T) []string {
			filesFixture(t, goAndDocsLanes)
			grantAllLanes(t)
			installLaneLookPath(t, "markdownlint")
			spawnWriting(t, "", nil)
			return []string{"--files", "docs/x.md"}
		}, exitToolchainMissing},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			args := tc.setup(t)
			_, err := runTestCapturing(t, args...)
			if got := ExitCode(err); got != tc.want {
				t.Fatalf("exit %d, want %d (err %v)", got, tc.want, err)
			}
			if tc.want == 0 {
				root, _ := FindRoot()
				g, gerr := gatestate.LoadGreen(filepath.Dir(root))
				if gerr != nil || g == nil {
					t.Fatalf("a green full run in summary mode recorded no green (%v)", gerr)
				}
			}
		})
	}
}
