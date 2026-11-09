package cmd

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Rivil/dross/internal/gatestate"
	"github.com/Rivil/dross/internal/project"
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
	mustWrite(t, filepath.Join(dir, filepath.FromSlash(fixTest)), "grep -q fixed src.txt\n")
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

// TestRedProofVerdict pins the mapping from the run's outcome to the proof's.
func TestRedProofVerdict(t *testing.T) {
	red := func(code int) error {
		return &ExitCodeError{Code: exitSuiteFailed, Err: fmt.Errorf("test lane failed: %w", exitStatus(code))}
	}
	for _, tc := range []struct {
		name string
		run  error
		line string
		exit int
	}{
		{"green", nil, "red-proof: not proven (green at base)", exitSuiteFailed},
		{"red", red(1), "red-proof: proven (red at base)", 0},
		{"runner not found", red(127), "red-proof: not run (exit 8)", exitToolchainMissing},
		{"runner not executable", red(126), "red-proof: not run (exit 8)", exitToolchainMissing},
		{"bad file set", &ExitCodeError{Code: exitBadFileSet, Err: errors.New("x")}, "red-proof: not run (exit 2)", exitBadFileSet},
		{"transport", &ExitCodeError{Code: exitTransport, Err: errors.New("x")}, "red-proof: not run (exit 3)", exitTransport},
		{"partial", &ExitCodeError{Code: exitPartial, Err: errors.New("x")}, "red-proof: not run (exit 4)", exitPartial},
		{"nothing measured", &ExitCodeError{Code: exitNothingMeasured, Err: errors.New("x")}, "red-proof: not run (exit 5)", exitNothingMeasured},
		{"refused", &ExitCodeError{Code: exitLaneRefused, Err: errors.New("x")}, "red-proof: not run (exit 6)", exitLaneRefused},
		{"prepare", &ExitCodeError{Code: exitPrepareFailed, Err: errors.New("x")}, "red-proof: not run (exit 7)", exitPrepareFailed},
		{"toolchain", &ExitCodeError{Code: exitToolchainMissing, Err: errors.New("x")}, "red-proof: not run (exit 8)", exitToolchainMissing},
		// Untagged, it would exit 1 — this mode's "ran green".
		{"untagged error", errors.New("malformed lane"), "red-proof: not run (exit 7)", exitPrepareFailed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			line, err := redProofVerdict(tc.run)
			if line != tc.line || ExitCode(err) != tc.exit {
				t.Errorf("verdict = %q, exit %d; want %q, exit %d", line, ExitCode(err), tc.line, tc.exit)
			}
		})
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

// TestRedProofPushKeepsHostGit: on a granted host the scratch copy is pushed in
// place of the working tree. It has no .git, so the push must protect the
// host's; its ignored paths are linked in, so the push must exclude them, or a
// link would replace the host's node_modules. What it sends is HEAD's code with
// the test laid over it.
func TestRedProofPushKeepsHostGit(t *testing.T) {
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

	var syncs [][]string
	var src, test string
	var linked bool
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
	keep := slices.Index(argv, "--filter=P /.git")
	if keep < 0 || keep >= len(argv)-2 {
		t.Errorf("the push does not protect the host's .git ahead of its paths: %q", argv)
	}
	if src != "bug\n" || test != "grep -q fixed src.txt\n" {
		t.Errorf("pushed src.txt %q and test %q, want HEAD's src.txt and the working tree's test", src, test)
	}
	if ex := slices.Index(argv, "--exclude=/node_modules"); !linked || ex < 0 || ex >= len(argv)-2 {
		t.Errorf("node_modules linked %v, argv %q — want it linked and excluded, by a pattern a symlink matches, ahead of the paths", linked, argv)
	}
	if bins := rem.bins(); len(bins) != 2 || bins[1] != "ssh" {
		t.Errorf("remote spawns %v, want the push then the suite over ssh", bins)
	}
}
