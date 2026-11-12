package cmd

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/gatestate"
	"github.com/Rivil/dross/internal/treefp"
)

// captureGreen swaps the green recorder's note stream for the test.
func captureGreen(t *testing.T) *strings.Builder {
	t.Helper()
	var b strings.Builder
	orig := greenStderr
	greenStderr = &b
	t.Cleanup(func() { greenStderr = orig })
	return &b
}

// repoDirHere is the fixture's repo root (the directory holding .dross).
func repoDirHere(t *testing.T) string {
	t.Helper()
	root, err := FindRoot()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Dir(root)
}

// asGitRepo turns the cwd fixture into a git work tree holding a.go.
func asGitRepo(t *testing.T) string {
	t.Helper()
	dir := repoDirHere(t)
	gitInit(t, dir, "")
	mustWrite(t, filepath.Join(dir, "a.go"), "package a\n")
	return dir
}

// greenFixture is a consented git repo whose runtime.test_command is set.
func greenFixture(t *testing.T) string {
	t.Helper()
	testFixture(t, "go test ./...")
	trustFixture(t)
	return asGitRepo(t)
}

func green(t *testing.T, dir string) *gatestate.Green {
	t.Helper()
	g, err := gatestate.LoadGreen(dir)
	if err != nil {
		t.Fatal(err)
	}
	return g
}

// candidateAfterAddAll is the tree the commit gate would judge after
// `git add -A`.
func candidateAfterAddAll(t *testing.T, dir string) string {
	t.Helper()
	mustGit(t, dir, "add", "-A")
	c, err := treefp.Candidate(dir, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	return c.Tree
}

// TestGreenMatchesCandidate: the recorder and the commit side fingerprint
// the same tree the same way.
func TestGreenMatchesCandidate(t *testing.T) {
	dir := greenFixture(t)
	notes := captureGreen(t)
	installSpawnRecorder(t, nil)

	if err := runCmd(t, Test()); err != nil {
		t.Fatalf("dross test: %v", err)
	}
	g := green(t, dir)
	if g == nil {
		t.Fatalf("a green bare run recorded nothing (notes: %q)", notes.String())
	}
	if want := candidateAfterAddAll(t, dir); g.Tree != want {
		t.Errorf("green tree %s != candidate after git add -A %s", g.Tree, want)
	}
	if g.Runner != "local" || notes.Len() != 0 {
		t.Errorf("runner = %q, notes = %q; want local and silence", g.Runner, notes.String())
	}
}

// TestFullRunSourceRecords: both full-run sources (full_run_source) record.
func TestFullRunSourceRecords(t *testing.T) {
	t.Run("test_command with lanes declared", func(t *testing.T) {
		dir := greenFixture(t)
		appendLanes(t, dir, fullRunLanes)
		grantAllLanes(t)
		rec := installSpawnRecorder(t, nil)
		if err := runCmd(t, Test()); err != nil {
			t.Fatal(err)
		}
		if len(rec.lines) != 1 || rec.lines[0] != "go test ./..." {
			t.Fatalf("spawned %q, want only the test_command", rec.lines)
		}
		if g := green(t, dir); g == nil || g.Tree != candidateAfterAddAll(t, dir) {
			t.Errorf("green = %+v, want the candidate tree", g)
		}
	})
	t.Run("lanes-only, every lane green", func(t *testing.T) {
		lanesOnlyFixture(t, fullRunLanes)
		dir := asGitRepo(t)
		grantAllLanes(t)
		perLaneSpawn(t, nil)
		if err := runCmd(t, Test()); err != nil {
			t.Fatal(err)
		}
		if g := green(t, dir); g == nil || g.Tree != candidateAfterAddAll(t, dir) {
			t.Errorf("green = %+v, want the candidate tree", g)
		}
	})
}

// TestPartialRunsRecordNothing (green_run_definition): a run that measured
// less than the whole tree, or measured it red, vouches for nothing.
func TestPartialRunsRecordNothing(t *testing.T) {
	cases := []struct {
		name string
		run  func(t *testing.T) (string, error)
	}{
		{"a selector", func(t *testing.T) (string, error) {
			dir := greenFixture(t)
			installSpawnRecorder(t, nil)
			return dir, runCmd(t, Test(), "./internal/x")
		}},
		{"a red run", func(t *testing.T) (string, error) {
			dir := greenFixture(t)
			installSpawnRecorder(t, errors.New("exit status 1"))
			return dir, runCmd(t, Test())
		}},
		{"lanes-only, one lane red", func(t *testing.T) (string, error) {
			lanesOnlyFixture(t, fullRunLanes)
			dir := asGitRepo(t)
			grantAllLanes(t)
			perLaneSpawn(t, map[string]error{"npm test": errors.New("exit status 1")})
			return dir, runCmd(t, Test())
		}},
		{"lanes-only, one lane refused", func(t *testing.T) (string, error) {
			lanesOnlyFixture(t, fullRunLanes)
			dir := asGitRepo(t)
			grantLane(t, "go")
			perLaneSpawn(t, nil)
			return dir, runCmd(t, Test())
		}},
		{"--files in a laned repo", func(t *testing.T) (string, error) {
			filesFixture(t, fullRunLanes)
			dir := asGitRepo(t)
			grantAllLanes(t)
			touchFile(t, "internal/a/a.go")
			installSpawnRecorder(t, nil)
			return dir, runCmd(t, Test(), "--files", "internal/a/a.go")
		}},
		{"--files on a vouching lane", func(t *testing.T) (string, error) {
			dir, _ := scopedRepo(t, relateLane, "src/a.ts", "src/a.test.ts")
			(&relateSeam{stdout: aToATest}).install(t)
			return dir, runCmd(t, Test(), "--files", "src/a.ts")
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			captureGreen(t)
			dir, _ := c.run(t)
			if g := green(t, dir); g != nil {
				t.Errorf("recorded %+v, want nothing", g)
			}
		})
	}
}

// TestLaneLessFilesRecords: in a repo with no lanes, --files runs the whole
// suite unchanged (bare_test_run) — execute.md's gate form — so it records.
func TestLaneLessFilesRecords(t *testing.T) {
	dir := greenFixture(t)
	captureGreen(t)
	installSpawnRecorder(t, nil)
	if err := runCmd(t, Test(), "--files", "a.go"); err != nil {
		t.Fatal(err)
	}
	if g := green(t, dir); g == nil {
		t.Error("a green lane-less --files run recorded nothing")
	}
}

// TestTreeChangedDuringRunRecordsNothing: a suite that writes into the tree
// went green on a tree that no longer exists.
func TestTreeChangedDuringRunRecordsNothing(t *testing.T) {
	dir := greenFixture(t)
	notes := captureGreen(t)
	orig := spawnLocal
	t.Cleanup(func() { spawnLocal = orig })
	spawnLocal = func(d, _ string, _, _ io.Writer) error {
		return os.WriteFile(filepath.Join(d, "coverage.out"), []byte("x"), 0o644)
	}

	if err := runCmd(t, Test()); err != nil {
		t.Fatalf("the run's own verdict changed: %v", err)
	}
	if g := green(t, dir); g != nil {
		t.Errorf("recorded %+v for a tree that moved mid-run", g)
	}
	lines := strings.Split(strings.TrimSpace(notes.String()), "\n")
	if len(lines) != 1 || !strings.Contains(lines[0], "coverage.out") {
		t.Errorf("notes = %q, want exactly one line naming coverage.out", notes.String())
	}
}

// TestGreenLifecycle: red on the recorded tree clears it; a run that never
// happened leaves it.
func TestGreenLifecycle(t *testing.T) {
	t.Run("green then red", func(t *testing.T) {
		dir := greenFixture(t)
		captureGreen(t)
		installSpawnRecorder(t, nil)
		if err := runCmd(t, Test()); err != nil || green(t, dir) == nil {
			t.Fatalf("setup green: %v", err)
		}
		installSpawnRecorder(t, errors.New("exit status 1"))
		if err := runCmd(t, Test()); ExitCode(err) != exitSuiteFailed {
			t.Fatalf("exit = %d, want red", ExitCode(err))
		}
		if g := green(t, dir); g != nil {
			t.Errorf("a red run on the recorded tree left %+v", g)
		}
	})
	for _, c := range []struct {
		name string
		err  error
		code int
	}{
		{"unreachable", errors.New("connection refused"), exitTransport},
		{"incomplete transfer", fakeExit{code: 23}, exitPartial},
	} {
		t.Run(c.name, func(t *testing.T) {
			grantedTestFixture(t, "go test ./...")
			dir := asGitRepo(t)
			captureGreen(t)
			installSpawnRecorder(t, nil)
			if err := runCmd(t, Test(), "--local"); err != nil || green(t, dir) == nil {
				t.Fatalf("setup green: %v", err)
			}
			before := green(t, dir)
			installRemoteRecorder(t, c.err)
			if err := runCmd(t, Test()); ExitCode(err) != c.code {
				t.Fatalf("exit = %d (%v), want %d", ExitCode(err), err, c.code)
			}
			if g := green(t, dir); g == nil || g.Tree != before.Tree {
				t.Errorf("a run that never happened changed the record: %+v -> %+v", before, g)
			}
		})
	}
}

// TestFullRedClearsScopedOnItsTree: a full run is the stronger measurement, so a
// red from one sinks a scoped record on that tree as it sinks a full green.
func TestFullRedClearsScopedOnItsTree(t *testing.T) {
	t.Run("on the tree it ran on", func(t *testing.T) {
		dir, _ := scopedRepo(t, relateLane, "src/a.ts")
		seedScopedHere(t, dir)
		installSpawnRecorder(t, errors.New("exit status 1"))
		if err := runCmd(t, Test()); ExitCode(err) != exitSuiteFailed {
			t.Fatalf("exit = %d, want red", ExitCode(err))
		}
		if b := scopedBytes(t, dir); b != nil {
			t.Errorf("a full red on the recorded tree left %s", b)
		}
	})
	t.Run("on another tree", func(t *testing.T) {
		dir, _ := scopedRepo(t, relateLane, "src/a.ts")
		before := seedScoped(t, dir, "another-tree")
		installSpawnRecorder(t, errors.New("exit status 1"))
		if err := runCmd(t, Test()); ExitCode(err) != exitSuiteFailed {
			t.Fatalf("exit = %d, want red", ExitCode(err))
		}
		if got := scopedBytes(t, dir); !bytes.Equal(got, before) {
			t.Errorf("a full red changed another tree's record:\n%s", got)
		}
	})
	t.Run("beside a green record", func(t *testing.T) {
		dir, _ := scopedRepo(t, relateLane, "src/a.ts")
		seedScopedHere(t, dir)
		tree, err := treefp.WorkingTree(dir)
		if err != nil {
			t.Fatal(err)
		}
		if err := gatestate.SaveGreen(dir, gatestate.Green{Tree: tree, Runner: "local"}); err != nil {
			t.Fatal(err)
		}
		installSpawnRecorder(t, errors.New("exit status 1"))
		if err := runCmd(t, Test()); ExitCode(err) != exitSuiteFailed {
			t.Fatalf("exit = %d, want red", ExitCode(err))
		}
		if b := scopedBytes(t, dir); b != nil {
			t.Errorf("the scoped record survived: %s", b)
		}
		if g := green(t, dir); g != nil {
			t.Errorf("the full green survived: %+v", g)
		}
	})
}

// TestFullRunThatNeverHappenedLeavesScoped: an unreachable host measured nothing
// about the tree, so the scoped record stands as the full green does.
func TestFullRunThatNeverHappenedLeavesScoped(t *testing.T) {
	relateHostFixture(t, relateLane, "src/a.ts")
	dir := asGitRepo(t)
	captureGreen(t)
	before := seedScopedHere(t, dir)
	installRemoteRecorder(t, errors.New("connection refused"))
	if err := runCmd(t, Test()); ExitCode(err) != exitTransport {
		t.Fatalf("exit = %d (%v), want %d", ExitCode(err), err, exitTransport)
	}
	if got := scopedBytes(t, dir); !bytes.Equal(got, before) {
		t.Errorf("a run that never happened changed the record:\n%s", got)
	}
}

// TestScopedRunsLeaveGreenAlone: a scoped run measured part of the tree, so it has
// nothing to say to the full record in either direction.
func TestScopedRunsLeaveGreenAlone(t *testing.T) {
	seedGreen := func(t *testing.T, dir string) []byte {
		t.Helper()
		tree, err := treefp.WorkingTree(dir)
		if err != nil {
			t.Fatal(err)
		}
		if err := gatestate.SaveGreen(dir, gatestate.Green{Tree: tree, Runner: "local"}); err != nil {
			t.Fatal(err)
		}
		b, err := os.ReadFile(gatestate.Path(dir, gatestate.GreenFile))
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	for _, tc := range []struct {
		name string
		seam relateSeam
		want int
	}{
		{"a scoped green", relateSeam{stdout: aToATest}, 0},
		{"a scoped red", relateSeam{stdout: aToATest, laneErr: fakeExit{code: 1}}, exitSuiteFailed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir, _ := scopedRepo(t, relateLane, "src/a.ts", "src/a.test.ts")
			before := seedGreen(t, dir)
			seam := tc.seam
			seam.install(t)

			if err := runCmd(t, Test(), "--files", "src/a.ts"); ExitCode(err) != tc.want {
				t.Fatalf("exit = %d (%v), want %d", ExitCode(err), err, tc.want)
			}
			got, err := os.ReadFile(gatestate.Path(dir, gatestate.GreenFile))
			if err != nil {
				t.Fatalf("green.json is gone: %v", err)
			}
			if !bytes.Equal(got, before) {
				t.Errorf("green.json changed:\n%s\nwas:\n%s", got, before)
			}
		})
	}
}

// TestRemoteGreenMatchesLocal: where the suite ran does not change what tree
// it vouches for.
func TestRemoteGreenMatchesLocal(t *testing.T) {
	grantedTestFixture(t, "go test ./...")
	dir := asGitRepo(t)
	captureGreen(t)
	installRemoteRecorder(t, nil)
	if err := runCmd(t, Test()); err != nil {
		t.Fatal(err)
	}
	remoteGreen := green(t, dir)
	if remoteGreen == nil || remoteGreen.Runner != "helicon" {
		t.Fatalf("remote green = %+v, want one recorded on helicon", remoteGreen)
	}
	installSpawnRecorder(t, nil)
	if err := runCmd(t, Test(), "--local"); err != nil {
		t.Fatal(err)
	}
	if local := green(t, dir); local == nil || local.Tree != remoteGreen.Tree {
		t.Errorf("local green %+v != remote green %+v", local, remoteGreen)
	}
}

// TestRecorderFailureOnlyWarns: a fingerprint that cannot be taken costs one
// warning and changes nothing about the run.
func TestRecorderFailureOnlyWarns(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs an index the test cannot read")
	}
	dir := greenFixture(t)
	mustGit(t, dir, "add", "a.go")
	index := filepath.Join(dir, ".git", "index")
	if err := os.Chmod(index, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(index, 0o644) })
	notes := captureGreen(t)
	installSpawnRecorder(t, nil)

	var out string
	err := runCmdCapturing(t, &out, Test())
	if err != nil {
		t.Errorf("a recorder failure changed the exit: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(notes.String()), "\n")
	if len(lines) != 1 || !strings.HasPrefix(lines[0], "warning:") {
		t.Errorf("notes = %q, want exactly one warning line", notes.String())
	}
	if strings.Contains(out, "warning") {
		t.Errorf("the warning leaked into stdout: %q", out)
	}
}
