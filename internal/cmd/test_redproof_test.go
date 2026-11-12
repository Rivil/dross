package cmd

import (
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Rivil/dross/internal/gatestate"
	"github.com/Rivil/dross/internal/project"
	"github.com/Rivil/dross/internal/runsummary"
	"github.com/Rivil/dross/internal/treefp"
)

// checksLane runs each named shell test with sh, from the tree it is spawned in.
const checksLane = `[[runtime.test_lane]]
name = "checks"
match = ["checks/**"]
command = "sh"
selector = "path"`

// fixTest is the test the fixtures' task added: green only on a src.txt that
// holds the fix.
const fixTest = "checks/fix_test.sh"

// fixScript is fixTest's body. It reports in pytest's shape, naming itself,
// because a red-proof counts a red only when a parser can name the failing test
// and the name points at a named file.
const fixScript = `if grep -q fixed src.txt; then echo '1 passed in 0.01s'; exit 0; fi
echo '=== short test summary info ==='
echo 'FAILED checks/fix_test.sh::test_fix - src.txt is not fixed'
echo '1 failed in 0.01s'
exit 1
`

// redProofRepo commits a repo whose src.txt reads base, then leaves the working
// tree holding the fix and the new test that greps for it — the shape a writer
// returns: fix and test uncommitted, on top of HEAD.
func redProofRepo(t *testing.T, base string) string {
	t.Helper()
	dir := repoDirHere(t)
	gitInit(t, dir, "")
	mustWrite(t, filepath.Join(dir, "src.txt"), base+"\n")
	mustGit(t, dir, "add", "-A")
	mustGit(t, dir, "commit", "-qm", "base")
	mustWrite(t, filepath.Join(dir, "src.txt"), "fixed\n")
	mustWrite(t, filepath.Join(dir, filepath.FromSlash(fixTest)), fixScript)
	return dir
}

// redProofLaneFixture is redProofRepo in a repo with checksLane granted.
func redProofLaneFixture(t *testing.T, base string) string {
	t.Helper()
	dir := laneFixture(t)
	appendLanes(t, dir, checksLane)
	installLaneLookPath(t)
	grantLaneLines(t)
	return redProofRepo(t, base)
}

// realSpawnIn keeps the local spawn real and records the directory each line
// ran in. The proof is about which code a line saw, so it has to run.
func realSpawnIn(t *testing.T) *[]string {
	t.Helper()
	var dirs []string
	orig := spawnLocal
	t.Cleanup(func() { spawnLocal = orig })
	spawnLocal = func(dir, line string, stdout, stderr io.Writer) error {
		dirs = append(dirs, dir)
		return runLocalCommand(dir, line, stdout, stderr)
	}
	return &dirs
}

// redProof runs `dross test --red-proof` over files and returns what it printed.
func redProof(t *testing.T, files ...string) (string, error) {
	t.Helper()
	args := []string{"--red-proof"}
	for _, f := range files {
		args = append(args, "--files", f)
	}
	var err error
	out := captureStdout(t, func() { err = runCmd(t, Test(), args...) })
	return out, err
}

// TestRedProofRunsAtBase: the test fails on HEAD's src.txt and passes on the
// working tree's, so only a run against HEAD can come back red. A red-proof
// that ran on the working tree reports not proven and fails here.
func TestRedProofRunsAtBase(t *testing.T) {
	dir := redProofLaneFixture(t, "bug")
	dirs := realSpawnIn(t)

	// The control: on the working tree the test is green.
	if err := runCmd(t, Test(), "--files", fixTest); err != nil {
		t.Fatalf("fixture: the test is not green on the working tree: %v", err)
	}
	*dirs = nil

	out, err := redProof(t, fixTest)
	if err != nil {
		t.Fatalf("a test red at base did not prove: exit %d: %v\n%s", ExitCode(err), err, out)
	}
	if !strings.Contains(out, "red-proof: proven (red at base)") {
		t.Errorf("no proven line:\n%s", out)
	}
	if len(*dirs) != 1 {
		t.Fatalf("want one spawn, got %d: %v", len(*dirs), *dirs)
	}
	ran := (*dirs)[0]
	if ran == dir || !strings.Contains(ran, "dross-redproof-") {
		t.Errorf("the test ran in %q, want a scratch copy of HEAD, never the working tree %q", ran, dir)
	}
	if _, err := os.Stat(ran); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the scratch copy %s outlived the run (stat: %v)", ran, err)
	}
}

// TestRedProofGreenIsNotProven: a test that passes at base says nothing about
// the fix, so it is not proven and the exit is non-zero.
func TestRedProofGreenIsNotProven(t *testing.T) {
	redProofLaneFixture(t, "fixed") // HEAD already passes the test
	realSpawnIn(t)

	out, err := redProof(t, fixTest)
	if got := ExitCode(err); got != exitSuiteFailed {
		t.Fatalf("exit = %d (%v), want %d: a green at base is no proof\n%s", got, err, exitSuiteFailed, out)
	}
	if !strings.Contains(out, "red-proof: not proven (green at base)") {
		t.Errorf("no not-proven line:\n%s", out)
	}
	if strings.Contains(out, "red-proof: proven") {
		t.Errorf("a green at base printed proven:\n%s", out)
	}
}

// TestRedProofNotRunIsNoProof: a lane that never ran — here ungranted — keeps
// its own exit and is reported as not run, never as proven.
func TestRedProofNotRunIsNoProof(t *testing.T) {
	dir := laneFixture(t)
	appendLanes(t, dir, checksLane)
	installLaneLookPath(t)
	redProofRepo(t, "bug")
	rec := installSpawnRecorder(t, nil)

	out, err := redProof(t, fixTest)
	if got := ExitCode(err); got != exitLaneRefused {
		t.Fatalf("exit = %d (%v), want %d", got, err, exitLaneRefused)
	}
	if !strings.Contains(out, fmt.Sprintf("red-proof: not run (exit %d)", exitLaneRefused)) {
		t.Errorf("no not-run line:\n%s", out)
	}
	if rec.count() != 0 {
		t.Errorf("an ungranted lane spawned: %v", rec.lines)
	}
}

// exitStatus stands in for an exited process: the shape remoteFailure and the
// verdict read through exitCoder.
type exitStatus int

func (e exitStatus) Error() string { return fmt.Sprintf("exit status %d", int(e)) }
func (e exitStatus) ExitCode() int { return int(e) }

// failedIn is one runner's parsed result with the named tests failing.
func failedIn(runner string, names ...string) runsummary.Result {
	var fs runsummary.Failures
	for _, n := range names {
		fs.Add(runsummary.Failure{Name: n})
	}
	return runsummary.Result{Runner: runner, Failures: fs}
}

// TestRedProofVerdict pins the mapping from the run's outcome to the proof's.
func TestRedProofVerdict(t *testing.T) {
	red := func(code int) error {
		return &ExitCodeError{Code: exitSuiteFailed, Err: fmt.Errorf("test lane failed: %w", exitStatus(code))}
	}
	named := redProofNamed{files: []string{"pkg/fix_test.go"}, goFuncs: map[string]bool{"TestFix": true}}
	for _, tc := range []struct {
		name   string
		run    error
		parsed []runsummary.Result
		line   string
		exit   int
	}{
		{"green", nil, nil, "red-proof: not proven (green at base)", exitSuiteFailed},
		{"red from a named test", red(1), []runsummary.Result{failedIn("go test", "TestOther", "TestFix/case")}, "red-proof: proven (red at base)", 0},
		{"red from a build failure", red(1), []runsummary.Result{failedIn("go test", "ex.com/m/pkg [ex.com/m/pkg.test]")},
			"red-proof: not proven (red at base, but not from the named tests: ex.com/m/pkg [ex.com/m/pkg.test])", exitSuiteFailed},
		{"red from another test", red(1), []runsummary.Result{failedIn("go test", "TestOther")},
			"red-proof: not proven (red at base, but not from the named tests: TestOther)", exitSuiteFailed},
		{"red no parser named", red(1), nil,
			"red-proof: not proven (red at base, but not from the named tests: no failing test could be read from the output)", exitSuiteFailed},
		{"red from an unmapped runner", red(1), []runsummary.Result{failedIn("dart test", "TestFix")},
			"red-proof: not proven (red at base, but not from the named tests: TestFix)", exitSuiteFailed},
		{"runner not found", red(127), []runsummary.Result{failedIn("go test", "TestFix")}, "red-proof: not run (exit 8)", exitToolchainMissing},
		{"runner not executable", red(126), nil, "red-proof: not run (exit 8)", exitToolchainMissing},
		{"bad file set", &ExitCodeError{Code: exitBadFileSet, Err: errors.New("x")}, nil, "red-proof: not run (exit 2)", exitBadFileSet},
		{"transport", &ExitCodeError{Code: exitTransport, Err: errors.New("x")}, nil, "red-proof: not run (exit 3)", exitTransport},
		{"partial", &ExitCodeError{Code: exitPartial, Err: errors.New("x")}, nil, "red-proof: not run (exit 4)", exitPartial},
		{"nothing measured", &ExitCodeError{Code: exitNothingMeasured, Err: errors.New("x")}, nil, "red-proof: not run (exit 5)", exitNothingMeasured},
		{"refused", &ExitCodeError{Code: exitLaneRefused, Err: errors.New("x")}, nil, "red-proof: not run (exit 6)", exitLaneRefused},
		{"prepare", &ExitCodeError{Code: exitPrepareFailed, Err: errors.New("x")}, nil, "red-proof: not run (exit 7)", exitPrepareFailed},
		{"toolchain", &ExitCodeError{Code: exitToolchainMissing, Err: errors.New("x")}, nil, "red-proof: not run (exit 8)", exitToolchainMissing},
		// Untagged, it would exit 1 — this mode's "ran green".
		{"untagged error", errors.New("malformed lane"), nil, "red-proof: not run (exit 7)", exitPrepareFailed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			line, err := redProofVerdict(tc.run, tc.parsed, named)
			if line != tc.line || ExitCode(err) != tc.exit {
				t.Errorf("verdict = %q, exit %d; want %q, exit %d", line, ExitCode(err), tc.line, tc.exit)
			}
		})
	}
}

// fakeRunner stands in for the suite at base: every local spawn prints out and
// exits code, so a test can hand the red-proof any runner's report.
func fakeRunner(t *testing.T, out string, code int) {
	t.Helper()
	orig := spawnLocal
	t.Cleanup(func() { spawnLocal = orig })
	spawnLocal = func(_, _ string, stdout, _ io.Writer) error {
		io.WriteString(stdout, out)
		if code != 0 {
			return exitStatus(code)
		}
		return nil
	}
}

// goFixTest is a Go test the task added, declaring TestFix, which calls a Fix
// HEAD does not have.
const goFixTest = "checks/fix_test.go"

func writeGoFixTest(t *testing.T, dir string) {
	t.Helper()
	mustWrite(t, filepath.Join(dir, filepath.FromSlash(goFixTest)),
		"package checks\n\nimport \"testing\"\n\nfunc TestFix(t *testing.T) {\n\tif Fix() != \"fixed\" {\n\t\tt.Fatal(\"not fixed\")\n\t}\n}\n")
}

// TestRedProofBuildFailureNotProven: the new test calls what HEAD lacks, so at
// base its package does not build. That red is the test not compiling, not the
// test failing without the fix, and it proves nothing.
func TestRedProofBuildFailureNotProven(t *testing.T) {
	dir := redProofLaneFixture(t, "bug")
	writeGoFixTest(t, dir)
	fakeRunner(t, "# ex.com/m/checks [ex.com/m/checks.test]\n"+
		"checks/fix_test.go:6:5: undefined: Fix\n"+
		"FAIL\tex.com/m/checks [build failed]\n"+
		"FAIL\n", 1)

	out, err := redProof(t, goFixTest)
	if got := ExitCode(err); got != exitSuiteFailed {
		t.Fatalf("exit = %d (%v), want %d: a build failure at base is no proof\n%s", got, err, exitSuiteFailed, out)
	}
	if want := "red-proof: not proven (red at base, but not from the named tests: ex.com/m/checks [ex.com/m/checks.test])"; !strings.Contains(out, want) {
		t.Errorf("want %q in:\n%s", want, out)
	}
	if strings.Contains(out, "red-proof: proven") {
		t.Errorf("a build failure printed proven:\n%s", out)
	}
}

// TestRedProofUnnamedRedNotProven: a whole-package run at base where only a
// test the task did not name fails is red for a reason that is not the fix.
// The same run with the named test's subtest failing is proof, so the line
// between the two is the attribution and nothing else.
func TestRedProofUnnamedRedNotProven(t *testing.T) {
	for _, tc := range []struct {
		name, failing, line string
		exit                int
	}{
		{"only an un-named test",
			"--- FAIL: TestOther (0.00s)\n    other_test.go:9: boom\n",
			"red-proof: not proven (red at base, but not from the named tests: TestOther)", exitSuiteFailed},
		{"the named test's subtest",
			"--- FAIL: TestFix (0.00s)\n    --- FAIL: TestFix/case (0.00s)\n        fix_test.go:7: not fixed\n",
			"red-proof: proven (red at base)", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := redProofLaneFixture(t, "bug")
			writeGoFixTest(t, dir)
			fakeRunner(t, tc.failing+"FAIL\nFAIL\tex.com/m/checks\t0.01s\nFAIL\n", 1)

			out, err := redProof(t, goFixTest)
			if got := ExitCode(err); got != tc.exit || !strings.Contains(out, tc.line) {
				t.Errorf("exit %d (%v), want %d and %q in:\n%s", got, err, tc.exit, tc.line, out)
			}
		})
	}
}

// TestRedProofOnTerminalProven: on a terminal a plain run builds no summary
// segments, since every byte streams live. A red-proof judges by the parsed
// failures, so it must build them there too: without them a red from the named
// test reads as one no failing test could be read from, and is not proven.
func TestRedProofOnTerminalProven(t *testing.T) {
	dir := redProofLaneFixture(t, "bug")
	writeGoFixTest(t, dir)
	origTTY := testStdoutIsTTY
	t.Cleanup(func() { testStdoutIsTTY = origTTY })
	testStdoutIsTTY = func() bool { return true }
	fakeRunner(t, "--- FAIL: TestFix (0.00s)\n    fix_test.go:7: not fixed\nFAIL\nFAIL\tex.com/m/checks\t0.01s\nFAIL\n", 1)

	out, err := redProof(t, goFixTest)
	if err != nil {
		t.Fatalf("a red from the named test on a terminal did not prove: exit %d: %v\n%s", ExitCode(err), err, out)
	}
	if !strings.Contains(out, "red-proof: proven (red at base)") {
		t.Errorf("no proven line on a terminal:\n%s", out)
	}
	// The run streamed, as any terminal run does: the runner's own lines
	// reached stdout rather than a summary standing in for them.
	if !strings.Contains(out, "--- FAIL: TestFix") {
		t.Errorf("the run did not stream on a terminal:\n%s", out)
	}
}

// TestRedProofHelpNamesNamedTests: the help states the rule the verdict
// applies. A red at base proves only when it is the named tests'; any red is
// not, and the help must not say it is.
func TestRedProofHelpNamesNamedTests(t *testing.T) {
	c := Test()
	long := strings.Join(strings.Fields(c.Long), " ")
	for _, want := range []string{
		"belongs to one of the named test files",
		"a build failure",
		"a red only in tests you did not name",
		"red at base, but not from the named tests: <first failing>",
	} {
		if !strings.Contains(long, want) {
			t.Errorf("long help lacks %q:\n%s", want, c.Long)
		}
	}
	if strings.Contains(long, "Red there is proof the tests need the fix (exit 0); green") {
		t.Errorf("long help still says any red is proof:\n%s", c.Long)
	}
	usage := c.Flags().Lookup("red-proof").Usage
	if !strings.Contains(usage, "only a red from the named tests exits 0") {
		t.Errorf("--red-proof usage = %q, want it to name the named tests", usage)
	}
}

// TestRedProofOwns: each runner's real report, parsed as a run's would be, and
// which of its failures are the named tests'. Paths a runner prints from below
// the repo's top still match; a file that failed to load never does.
func TestRedProofOwns(t *testing.T) {
	for _, tc := range []struct {
		runner, out string
		files       []string
		want        map[string]bool // every parsed failure, and whether it is owned
	}{
		{"go test", "--- FAIL: TestFix (0.00s)\n    --- FAIL: TestFix/case (0.00s)\n        fix_test.go:7: no\n" +
			"--- FAIL: TestOther (0.00s)\n    other_test.go:9: boom\nFAIL\nFAIL\tex.com/m/checks\t0.01s\n" +
			"# ex.com/m/b [ex.com/m/b.test]\nb/x_test.go:3:2: undefined: Y\nFAIL\tex.com/m/b [build failed]\nFAIL\n",
			[]string{goFixTest},
			map[string]bool{"TestFix/case": true, "TestOther": false, "ex.com/m/b [ex.com/m/b.test]": false}},
		{"pytest", "=== short test summary info ===\nFAILED tests/test_api.py::test_x - assert 1\n" +
			"FAILED tests/test_other.py::test_y - assert 2\nERROR tests/test_broken.py - ImportError\n" +
			"=== 2 failed, 1 error in 0.10s ===\n",
			[]string{"api/tests/test_api.py", "api/tests/test_broken.py"},
			map[string]bool{"tests/test_api.py::test_x": true, "tests/test_other.py::test_y": false, "tests/test_broken.py": false}},
		{"jest", "FAIL ./math.test.js\n  ● add › negatives\n\n    boom\n\nFAIL ./broken.test.js\n  ● Test suite failed to run\n\n    gone\n\n" +
			"Test Suites: 2 failed, 2 total\nTests:       1 failed, 1 total\n",
			[]string{"web/math.test.js", "web/broken.test.js"},
			map[string]bool{"math.test.js › add › negatives": true, "broken.test.js › Test suite failed to run": false}},
		{"vitest", "⎯⎯⎯ Failed Suites 1 ⎯⎯⎯\n\n FAIL  src/broken.test.ts [ src/broken.test.ts ]\nError: parse\n⎯⎯⎯[1/3]⎯\n\n" +
			"⎯⎯⎯ Failed Tests 2 ⎯⎯⎯\n\n FAIL  |default| src/math.test.ts > add > negatives\nAssertionError: no\n⎯⎯⎯[2/3]⎯\n\n" +
			" FAIL  src/other.test.ts > sub > x\nAssertionError: no\n⎯⎯⎯[3/3]⎯\n\n" +
			" Test Files  3 failed (3)\n      Tests  2 failed (2)\n",
			[]string{"web/src/math.test.ts", "web/src/broken.test.ts"},
			map[string]bool{"src/broken.test.ts": false, "|default| src/math.test.ts > add > negatives": true, "src/other.test.ts > sub > x": false}},
		{"dart test", "00:01 +0 -1: test/fix_test.dart: fix [E]\n  boom\n00:01 +0 -1: Some tests failed.\n",
			[]string{"test/fix_test.dart"}, nil},
	} {
		t.Run(tc.runner, func(t *testing.T) {
			seg := runsummary.NewSegment()
			io.WriteString(seg, tc.out)
			seg.Close()
			named := redProofNamed{files: tc.files, goFuncs: map[string]bool{}}
			if tc.runner == "go test" {
				dir := t.TempDir()
				writeGoFixTest(t, dir)
				named = namedTests(dir, tc.files)
			}
			got := map[string]bool{}
			for _, r := range seg.Results() {
				if r.Runner != tc.runner {
					continue
				}
				for _, f := range r.Failures.Kept {
					got[f.Name] = named.owns(r.Runner, f.Name)
				}
			}
			if tc.want == nil {
				// An unmapped runner: whatever it reported, none of it is owned.
				if len(got) == 0 {
					t.Fatalf("%s's report parsed to no failure, so nothing was checked", tc.runner)
				}
				for name, owned := range got {
					if owned {
						t.Errorf("%s's %q was attributed; the runner has no mapping", tc.runner, name)
					}
				}
				return
			}
			if !maps.Equal(got, tc.want) {
				t.Errorf("parsed and owned %v, want %v", got, tc.want)
			}
		})
	}
}

// TestRedProofScratchHasGitAtHead: the scratch copy is a git work tree at HEAD,
// so a test that calls git runs as it would in the working tree. Here the named
// test passes at base only if git answers there, so the run is green — not
// proven — and without the .git it would be red. The spawn also reads the copy
// directly: HEAD is the repo's, and HEAD's files are unchanged.
func TestRedProofScratchHasGitAtHead(t *testing.T) {
	dir := redProofLaneFixture(t, "fixed")
	mustWrite(t, filepath.Join(dir, filepath.FromSlash(fixTest)),
		"git rev-parse --verify -q HEAD >/dev/null && grep -q fixed src.txt && { echo '1 passed in 0.01s'; exit 0; }\n"+
			"echo '=== short test summary info ==='\n"+
			"echo 'FAILED checks/fix_test.sh::test_fix - no git, or src.txt is not fixed'\n"+
			"echo '1 failed in 0.01s'\nexit 1\n")
	head := mustGit(t, dir, "rev-parse", "HEAD")

	var sawHead, diff, status string
	orig := spawnLocal
	t.Cleanup(func() { spawnLocal = orig })
	spawnLocal = func(at, line string, stdout, stderr io.Writer) error {
		git := func(args ...string) string {
			out, err := exec.Command("git", append([]string{"-C", at}, args...)...).CombinedOutput()
			if err != nil {
				return fmt.Sprintf("error: %v: %s", err, out)
			}
			return strings.TrimSpace(string(out))
		}
		sawHead = git("rev-parse", "HEAD")
		diff = git("diff", "--name-only", "HEAD", "--", ".")
		status = git("status", "--porcelain", "--", "checks")
		return runLocalCommand(at, line, stdout, stderr)
	}

	out, err := redProof(t, fixTest)
	if got := ExitCode(err); got != exitSuiteFailed || !strings.Contains(out, "red-proof: not proven (green at base)") {
		t.Errorf("exit %d (%v), want the git-calling test green at base:\n%s", got, err, out)
	}
	if sawHead != head {
		t.Errorf("the copy's HEAD is %q, want the repo's %s", sawHead, head)
	}
	if diff != "" {
		t.Errorf("the copy's tracked files differ from HEAD: %q", diff)
	}
	if status != "?? checks/" && status != "?? "+fixTest {
		t.Errorf("the laid-over test reads %q in the copy's status, want it untracked as in the working tree", status)
	}
}

// gateSnapshot is everything a red-proof must leave as it found it.
type gateSnapshot struct {
	tree, green, scoped, index, status string
}

func snapshotGate(t *testing.T, dir string) gateSnapshot {
	t.Helper()
	tree, err := treefp.WorkingTree(dir)
	if err != nil {
		t.Fatal(err)
	}
	return gateSnapshot{
		tree:   tree,
		green:  mustRead(t, gatestate.Path(dir, gatestate.GreenFile)),
		scoped: mustRead(t, gatestate.Path(dir, gatestate.ScopedFile)),
		index:  mustGit(t, dir, "ls-files", "--stage"),
		status: mustGit(t, dir, "status", "--porcelain", "--", ".", ":(exclude).dross"),
	}
}

// TestRedProofLeavesTreeAndRecords: proven or not, a red-proof writes nothing a
// commit is judged by. The records stand on the working tree's own fingerprint,
// so a run that reached either recorder would clear them on a red and rewrite
// green.json on a green. A staged change shows a read-tree into the real index.
func TestRedProofLeavesTreeAndRecords(t *testing.T) {
	for _, base := range []string{"bug", "fixed"} {
		t.Run("HEAD reads "+base, func(t *testing.T) {
			dir := redProofLaneFixture(t, base)
			mustGit(t, dir, "add", "src.txt")
			fp, err := treefp.WorkingTree(dir)
			if err != nil {
				t.Fatal(err)
			}
			at := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
			if err := gatestate.SaveGreen(dir, gatestate.Green{Tree: fp, At: at, Runner: "local"}); err != nil {
				t.Fatal(err)
			}
			if err := gatestate.SaveScoped(dir, gatestate.Scoped{Tree: fp, Covered: []string{"src.txt"}, Lanes: []string{"lane"}, At: at, Runner: "local"}); err != nil {
				t.Fatal(err)
			}
			before := snapshotGate(t, dir)
			realSpawnIn(t)

			out, err := redProof(t, fixTest)
			if c := ExitCode(err); c != 0 && c != exitSuiteFailed {
				t.Fatalf("the red-proof did not run: exit %d: %v\n%s", c, err, out)
			}
			if after := snapshotGate(t, dir); after != before {
				t.Errorf("the red-proof moved what a commit is judged by:\nbefore %+v\nafter  %+v", before, after)
			}
		})
	}
}

// TestRedProofRefusesSourcePaths: laying source over HEAD would run the fix
// itself, so any non-test path refuses the whole set before anything spawns.
func TestRedProofRefusesSourcePaths(t *testing.T) {
	redProofLaneFixture(t, "bug")
	mustWrite(t, filepath.Join(repoDirHere(t), "checks", "helper.sh"), "echo\n")
	rec := installSpawnRecorder(t, nil)

	for _, tc := range []struct {
		name  string
		files []string
		names string
	}{
		{"source alone", []string{"src.txt"}, "src.txt"},
		{"source beside a test", []string{fixTest, "src.txt"}, "src.txt"},
		{"a non-test in a lane's tree", []string{"checks/helper.sh"}, "checks/helper.sh"},
		{"no files", nil, "--files"},
		{"a test that is not there", []string{"checks/gone_test.sh"}, "checks/gone_test.sh"},
		{"outside the repo", []string{"../x_test.sh"}, "../x_test.sh"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, err := redProof(t, tc.files...)
			if got := ExitCode(err); got != exitBadFileSet {
				t.Fatalf("exit = %d (%v), want %d", got, err, exitBadFileSet)
			}
			if !strings.Contains(err.Error(), tc.names) {
				t.Errorf("refusal does not name %q: %v", tc.names, err)
			}
			if strings.Contains(out, "red-proof: proven") {
				t.Errorf("a refused set printed proven:\n%s", out)
			}
		})
	}
	if rec.count() != 0 {
		t.Errorf("a refused red-proof spawned %d line(s): %v", rec.count(), rec.lines)
	}
}

// TestIsTestPath: names that are tests by their runner's convention, and the
// source beside them that is not.
func TestIsTestPath(t *testing.T) {
	for p, want := range map[string]bool{
		"internal/cmd/test_redproof_test.go": true,
		"src/a.test.ts":                      true,
		"src/a.spec.tsx":                     true,
		"src/a.test.svelte.ts":               true,
		"src/__tests__/a.ts":                 true,
		"src/__snapshots__/a.test.ts.snap":   true,
		"internal/x/testdata/fixture.json":   true,
		"tests/test_api.py":                  true,
		"pkg/test_api.py":                    true,
		"pkg/conftest.py":                    true,
		"lib/foo_spec.rb":                    true,
		"app/src/test/kotlin/FooTest.kt":     true,
		"app/FooTests.java":                  true,
		"lib/foo_test.dart":                  true,
		"internal/cmd/test_redproof.go":      false,
		"src/a.ts":                           false,
		"src/test.ts":                        false,
		"spec.md":                            false,
		"Makefile":                           false,
		"pkg/testing.py":                     false,
		"src/latest.ts":                      false,
		"app/Contest.kt":                     false,
		"internal/testdatabase/store.go":     false,
	} {
		if got := isTestPath(p); got != want {
			t.Errorf("isTestPath(%q) = %v, want %v", p, got, want)
		}
	}
}

// TestUnderAny: an ignored entry beneath an already-linked directory is skipped,
// so MkdirAll never follows the link into the working tree. Beneath means a
// path separator after the dir: a sibling sharing its prefix, or the dir
// itself, is not under it.
func TestUnderAny(t *testing.T) {
	linked := []string{"node_modules", "web/.svelte-kit"}
	for name, want := range map[string]bool{
		"node_modules/vitest":      true,
		"web/.svelte-kit/output/x": true,
		"node_modules":             false,
		"node_modules_extra/x":     false,
		"web/.svelte-kitchen/x":    false,
		"web/node_modules/vitest":  false,
		"dist":                     false,
	} {
		if got := underAny(name, linked); got != want {
			t.Errorf("underAny(%q, %q) = %v, want %v", name, linked, got, want)
		}
	}
	if underAny("node_modules/x", nil) {
		t.Error("underAny with no linked dirs reported a match")
	}
}

// TestRedProofLanesSkipRelate: a vouching lane's tests are the named paths
// themselves, so its line is built from them and relate never spawns.
func TestRedProofLanesSkipRelate(t *testing.T) {
	lane := project.TestLane{Name: "web", Command: "npx vitest run", Selector: "path", Vouch: true, Relate: "node scripts/relate.mjs"}
	got := redProofLanes([]plannedLane{{
		matchedLane: matchedLane{lane: lane},
		Relate:      "node scripts/relate.mjs 'src/a.test.ts'",
		Asked:       []string{"src/a.test.ts"},
	}})
	if len(got) != 1 || got[0].Relate != "" || got[0].ScopedToNothing || got[0].Line != batchOfATest {
		t.Errorf("planned %+v, want the batch line %q and no relate", got, batchOfATest)
	}
}

// TestRedProofPushSendsItsGit: on a granted host the scratch copy is pushed in
// place of the working tree, its own .git at HEAD with it and no rule keeping
// the host's, which would otherwise sit beside HEAD's files describing another
// tree. Its ignored paths are linked in, so the push must exclude them, or a
// link would replace the host's node_modules. What it sends is HEAD's code with
// the test laid over it.
func TestRedProofPushSendsItsGit(t *testing.T) {
	grantedTestFixture(t, "go test -count=1 ./...")
	dir := repoDirHere(t)
	// Appended: init's own rules keep .dross/local.toml untracked, and a
	// committed one is refused before anything runs.
	ignore := filepath.Join(dir, ".gitignore")
	kept, _ := os.ReadFile(ignore)
	mustWrite(t, ignore, string(kept)+"\nnode_modules/\n")
	redProofRepo(t, "bug")
	mustWrite(t, filepath.Join(dir, "node_modules", "dep", "index.js"), "x\n")
	installSpawnRecorder(t, nil)

	head := mustGit(t, dir, "rev-parse", "HEAD")
	var syncs [][]string
	var src, test, sentHead string
	var linked, gitDir bool
	rem := installRemoteRecorder(t, nil)
	record := spawnRemote
	spawnRemote = func(argv []string, stdin string, stdout, stderr io.Writer) error {
		if argv[0] == "rsync" {
			syncs = append(syncs, append([]string(nil), argv...))
			from := strings.TrimSuffix(argv[len(argv)-2], "/")
			src = mustRead(t, filepath.Join(from, "src.txt"))
			test = mustRead(t, filepath.Join(from, filepath.FromSlash(fixTest)))
			if fi, err := os.Lstat(filepath.Join(from, "node_modules")); err == nil {
				linked = fi.Mode()&os.ModeSymlink != 0
			}
			fi, err := os.Lstat(filepath.Join(from, ".git"))
			gitDir = err == nil && fi.IsDir()
			if out, err := exec.Command("git", "-C", from, "rev-parse", "HEAD").Output(); err == nil {
				sentHead = strings.TrimSpace(string(out))
			}
		}
		return record(argv, stdin, stdout, stderr)
	}

	if _, err := redProof(t, fixTest); ExitCode(err) != exitSuiteFailed {
		t.Fatalf("exit = %d (%v), want %d from the faked green suite", ExitCode(err), err, exitSuiteFailed)
	}
	if len(syncs) != 1 {
		t.Fatalf("want one push, got %d", len(syncs))
	}
	argv := syncs[0]
	if from := argv[len(argv)-2]; from == dir+"/" || !strings.Contains(from, "dross-redproof-") {
		t.Errorf("pushed %q, want the scratch copy, never the working tree", from)
	}
	if slices.Contains(argv, "--filter=P /.git") {
		t.Errorf("the push keeps the host's .git beside HEAD's files: %q", argv)
	}
	if !gitDir || sentHead != head {
		t.Errorf("pushed .git directory %v at HEAD %q, want the copy's own .git at the repo's %s", gitDir, sentHead, head)
	}
	if src != "bug\n" || test != fixScript {
		t.Errorf("pushed src.txt %q and test %q, want HEAD's src.txt and the working tree's test", src, test)
	}
	if ex := slices.Index(argv, "--exclude=/node_modules"); !linked || ex < 0 || ex >= len(argv)-2 {
		t.Errorf("node_modules linked %v, argv %q — want it linked and excluded, by a pattern a symlink matches, ahead of the paths", linked, argv)
	}
	if bins := rem.bins(); len(bins) != 2 || bins[1] != "ssh" {
		t.Errorf("remote spawns %v, want the push then the suite over ssh", bins)
	}
}
