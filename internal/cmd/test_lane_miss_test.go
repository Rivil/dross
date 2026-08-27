package cmd

import (
	"io"
	"path/filepath"
	"strings"
	"testing"
)

// missLanes: a go lane with no empty-exit declaration and a docs lane that
// declares pytest-style 5. Two lanes with DIFFERENT declarations, so a run can
// show one collecting nothing while the other measures something.
const missLanes = `[[runtime.test_lane]]
name = "go"
match = ["internal/**", "main.go"]
command = "go test -count=1"
selector = "go-package"

[[runtime.test_lane]]
name = "docs"
match = ["docs/**"]
command = "pytest"
selector = "dir"
empty_exit = [5]`

// perLaneSpawn installs a local spawn seam that returns a different error
// depending on which command line ran, so one run can hold a green lane and a
// collecting-nothing lane at once.
func perLaneSpawn(t *testing.T, by map[string]error) *spawnRecorder {
	t.Helper()
	rec := &spawnRecorder{}
	orig := spawnLocal
	t.Cleanup(func() { spawnLocal = orig })
	spawnLocal = func(dir, line string, _, _ io.Writer) error {
		_ = rec.spawn(dir, line, io.Discard, io.Discard)
		for prefix, err := range by {
			if strings.HasPrefix(line, prefix) {
				return err
			}
		}
		return nil
	}
	return rec
}

// TestAMissDoesNotFailARunThatMeasuredSomething is c-5's first half. exitRank
// puts exitNothingMeasured above nil, so an implementation that folded the miss
// through worseOutcome fails on the exit code — not on the transcript, which
// would still read correctly.
func TestAMissDoesNotFailARunThatMeasuredSomething(t *testing.T) {
	filesFixture(t, missLanes)
	grantAllLanes(t)
	touchFile(t, "internal/cmd/test.go")
	touchFile(t, "docs/x.md")
	perLaneSpawn(t, map[string]error{"pytest": fakeExit{code: 5}})

	var out string
	err := runCmdCapturing(t, &out, Test(), "--files", "internal/cmd/test.go", "--files", "docs/x.md")
	if got := ExitCode(err); got != 0 {
		t.Fatalf("exit = %d, want 0 — a lane that collected nothing is not a verdict about the code (%v)", got, err)
	}
	if !strings.Contains(out, `"docs"`) {
		t.Errorf("the transcript does not name the missing lane:\n%s", out)
	}
	if !strings.Contains(out, "docs") || !strings.Contains(out, "selector miss") {
		t.Errorf("the transcript does not report a selector miss:\n%s", out)
	}
}

// TestAMissLineNamesTheLaneAndTheSelector: c-5's wording. "collected no tests"
// is only actionable if the reader can see what it was looking in.
func TestAMissLineNamesTheLaneAndTheSelector(t *testing.T) {
	filesFixture(t, missLanes)
	grantAllLanes(t)
	touchFile(t, "docs/guide/x.md")
	perLaneSpawn(t, map[string]error{"pytest": fakeExit{code: 5}})

	var out string
	_ = runCmdCapturing(t, &out, Test(), "--files", "docs/guide/x.md")
	if !strings.Contains(out, `"docs"`) {
		t.Errorf("the miss line does not name the lane:\n%s", out)
	}
	if !strings.Contains(out, "docs/guide") {
		t.Errorf("the miss line does not name the derived selector:\n%s", out)
	}
}

// TestEveryLaneMissingExitsNothingMeasured is c-5's second half, and the
// false-green this whole command exists to prevent: a run where nothing
// collected anything measured exactly as much as one that matched no lane.
// Asserted on the CODE, not the message.
func TestEveryLaneMissingExitsNothingMeasured(t *testing.T) {
	filesFixture(t, missLanes)
	grantAllLanes(t)

	// Neither path exists, so both lanes' selectors filter down to nothing
	// and neither spawns.
	rec := installSpawnRecorder(t, nil)
	err := runCmd(t, Test(), "--files", "internal/gone/x.go", "--files", "docs/gone.md")
	if got := ExitCode(err); got != exitNothingMeasured {
		t.Errorf("exit = %d, want %d (nothing measured)", got, exitNothingMeasured)
	}
	if rec.count() != 0 {
		t.Fatalf("a run where every lane missed spawned %v", rec.lines)
	}
}

// TestOneIntegerApartOppositeVerdicts: the same lane, the same declaration,
// exit 5 versus exit 1. The empty-exit arm must not swallow a real red, which
// is the one way this feature could hide a broken suite.
func TestOneIntegerApartOppositeVerdicts(t *testing.T) {
	t.Run("declared code is a miss", func(t *testing.T) {
		filesFixture(t, missLanes)
		grantAllLanes(t)
		touchFile(t, "docs/x.md")
		perLaneSpawn(t, map[string]error{"pytest": fakeExit{code: 5}})

		if err := runCmd(t, Test(), "--files", "docs/x.md"); ExitCode(err) != exitNothingMeasured {
			t.Errorf("exit = %d, want %d", ExitCode(err), exitNothingMeasured)
		}
	})
	t.Run("one away is a red suite", func(t *testing.T) {
		filesFixture(t, missLanes)
		grantAllLanes(t)
		touchFile(t, "docs/x.md")
		perLaneSpawn(t, map[string]error{"pytest": fakeExit{code: 1}})

		err := runCmd(t, Test(), "--files", "docs/x.md")
		if got := ExitCode(err); got != exitSuiteFailed {
			t.Fatalf("exit = %d, want %d (red suite)", got, exitSuiteFailed)
		}
		if !strings.Contains(err.Error(), `test lane "docs" failed`) {
			t.Errorf("a red lane must say so, got: %v", err)
		}
	})
}

// TestALaneWithNoDeclarationTreatsFiveAsRed is locked empty_detection: without
// a declaration only the empty-selector filter can produce a miss. The go lane
// here declares no empty_exit, and 5 is a perfectly ordinary way for a runner
// to fail.
func TestALaneWithNoDeclarationTreatsFiveAsRed(t *testing.T) {
	filesFixture(t, missLanes)
	grantAllLanes(t)
	touchFile(t, "internal/cmd/test.go")
	perLaneSpawn(t, map[string]error{"go test": fakeExit{code: 5}})

	err := runCmd(t, Test(), "--files", "internal/cmd/test.go")
	if got := ExitCode(err); got != exitSuiteFailed {
		t.Errorf("exit = %d, want %d — an undeclared 5 is a red suite", got, exitSuiteFailed)
	}
}

// TestARemoteMissIsClassifiedTheSameWay: the remote transport wraps the far
// side's status in remote.ExitError, so errors.As has nothing to reach unless
// that type exposes ExitCode. Without it the same runner exiting the same way
// is a miss locally and a red suite on a granted host.
func TestARemoteMissIsClassifiedTheSameWay(t *testing.T) {
	root := grantedTestFixture(t, "go test ./...")
	appendLanes(t, filepath.Dir(root), missLanes)
	grantAllLanes(t)
	touchFile(t, "docs/x.md")

	orig := spawnRemote
	t.Cleanup(func() { spawnRemote = orig })
	spawnRemote = func(argv []string, _ string, _, _ io.Writer) error {
		if len(argv) > 0 && strings.Contains(argv[0], "ssh") {
			return fakeExit{code: 5}
		}
		return nil
	}

	err := runCmd(t, Test(), "--files", "docs/x.md")
	if got := ExitCode(err); got != exitNothingMeasured {
		t.Errorf("exit = %d, want %d — a remote lane's declared empty code must classify as a miss", got, exitNothingMeasured)
	}
}

// TestAMissNeverOutranksARealFailure: the existing exitRank order, preserved.
// A run holding a miss and a red lane is red; a run holding a miss and a
// refused lane is refused. Reporting either as "nothing measured" would send
// the reader to project.toml over a broken test or an ungranted command.
func TestAMissNeverOutranksARealFailure(t *testing.T) {
	t.Run("red wins", func(t *testing.T) {
		filesFixture(t, missLanes)
		grantAllLanes(t)
		touchFile(t, "internal/cmd/test.go")
		touchFile(t, "docs/x.md")
		perLaneSpawn(t, map[string]error{
			"pytest":  fakeExit{code: 5},
			"go test": fakeExit{code: 1},
		})

		err := runCmd(t, Test(), "--files", "internal/cmd/test.go", "--files", "docs/x.md")
		if got := ExitCode(err); got != exitSuiteFailed {
			t.Errorf("exit = %d, want %d (red suite)", got, exitSuiteFailed)
		}
	})
	t.Run("refusal wins", func(t *testing.T) {
		filesFixture(t, missLanes)
		// Only the docs lane is granted; the go lane is refused.
		grantLane(t, "docs")
		touchFile(t, "internal/cmd/test.go")
		touchFile(t, "docs/x.md")
		perLaneSpawn(t, map[string]error{"pytest": fakeExit{code: 5}})

		err := runCmd(t, Test(), "--files", "internal/cmd/test.go", "--files", "docs/x.md")
		if got := ExitCode(err); got != exitLaneRefused {
			t.Errorf("exit = %d, want %d (lane refused)", got, exitLaneRefused)
		}
	})
}

// TestOutputIsNeverScraped is locked empty_detection stated as a refusal to
// infer: a lane that PRINTS "collected 0 items" and exits 0 passed, because the
// only thing dross reads is the status. Matching wording would make dross track
// every framework's phrasing across every version of it.
func TestOutputIsNeverScraped(t *testing.T) {
	filesFixture(t, missLanes)
	grantAllLanes(t)
	touchFile(t, "docs/x.md")

	orig := spawnLocal
	t.Cleanup(func() { spawnLocal = orig })
	spawnLocal = func(_, _ string, stdout io.Writer, _ io.Writer) error {
		_, _ = io.WriteString(stdout, "collected 0 items\n")
		return nil
	}

	var out string
	err := runCmdCapturing(t, &out, Test(), "--files", "docs/x.md")
	if got := ExitCode(err); got != 0 {
		t.Errorf("exit = %d, want 0 — the runner exited 0, whatever it printed", got)
	}
	if strings.Contains(out, "selector miss") {
		t.Errorf("dross read the runner's output:\n%s", out)
	}
}
