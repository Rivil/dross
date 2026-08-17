package cmd

import (
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

// spawnRecorder replaces the local-spawn seam and records every command line it
// was asked to run, so a test can assert that nothing was spawned — which is
// the only way to prove a refusal happened BEFORE the suite ran rather than
// after.
type spawnRecorder struct {
	mu    sync.Mutex
	lines []string
	dirs  []string
	err   error
}

func (r *spawnRecorder) spawn(dir, line string, _, _ io.Writer) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lines = append(r.lines, line)
	r.dirs = append(r.dirs, dir)
	return r.err
}

func (r *spawnRecorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.lines)
}

// installSpawnRecorder swaps in the recorder for the length of one test.
func installSpawnRecorder(t *testing.T, err error) *spawnRecorder {
	t.Helper()
	rec := &spawnRecorder{err: err}
	orig := spawnLocal
	t.Cleanup(func() { spawnLocal = orig })
	spawnLocal = rec.spawn
	return rec
}

// testFixture is a repo with a runtime.test_command set. Consent is NOT
// granted — each test decides, because the refusal path is behaviour under
// test rather than scaffolding to get past.
func testFixture(t *testing.T, testCmd string) {
	t.Helper()
	dir := t.TempDir()
	chdir(t, dir)
	if err := runCmd(t, Init()); err != nil {
		t.Fatalf("init: %v", err)
	}
	mustRunSet(t, "project.name", "test-app")
	mustRunSet(t, "runtime.mode", "native")
	if testCmd != "" {
		mustRunSet(t, "runtime.test_command", testCmd)
	}
}

// TestTestRefusesWithoutTrust: the consent gate covers the command that
// actually spawns the suite, and it refuses before spawning.
//
// Asserting the spawn count is the whole test. A refusal returned AFTER the
// suite ran would still surface an error, and would still have run the code it
// was refusing to authorize.
func TestTestRefusesWithoutTrust(t *testing.T) {
	testFixture(t, "echo ran")
	rec := installSpawnRecorder(t, nil)

	if err := runCmd(t, Test()); err == nil {
		t.Fatal("dross test ran without consent")
	}
	if n := rec.count(); n != 0 {
		t.Errorf("the refusal spawned %d command(s) — it must refuse before running anything", n)
	}
}

// TestTestRefusesOnStaleTrust: consent binds to the command. Changing
// runtime.test_command after granting must lapse it, and the refusal must say
// so rather than reporting a routine first run — the second is the attack the
// binding exists for.
func TestTestRefusesOnStaleTrust(t *testing.T) {
	testFixture(t, "echo original")
	trustFixture(t)
	mustRunSet(t, "runtime.test_command", "echo swapped")

	rec := installSpawnRecorder(t, nil)
	err := runCmd(t, Test())
	if err == nil {
		t.Fatal("dross test ran a command consent had never been granted for")
	}
	if rec.count() != 0 {
		t.Errorf("the stale-consent refusal still spawned something")
	}
	if !strings.Contains(err.Error(), "swapped") {
		t.Errorf("the refusal does not name the command it is refusing: %v", err)
	}
}

// TestTestRefusesWithNoCommand: a blank runtime.test_command is not a free
// pass. If it were, emptying the field would be the way around the gate.
func TestTestRefusesWithNoCommand(t *testing.T) {
	testFixture(t, "")
	rec := installSpawnRecorder(t, nil)

	if err := runCmd(t, Test()); err == nil {
		t.Fatal("dross test accepted a blank runtime.test_command")
	}
	if rec.count() != 0 {
		t.Errorf("a blank command still spawned something")
	}
}

// TestTestRunsTheConsentedCommand: with consent, the line spawned is byte
// identical to what `dross trust` showed the user. Anything else means the gate
// approved one command and the runner ran another.
func TestTestRunsTheConsentedCommand(t *testing.T) {
	testFixture(t, "go test -count=1 ./...")
	trustFixture(t)
	rec := installSpawnRecorder(t, nil)

	if err := runCmd(t, Test()); err != nil {
		t.Fatalf("dross test: %v", err)
	}
	if rec.count() != 1 {
		t.Fatalf("spawned %d commands, want 1", rec.count())
	}
	if rec.lines[0] != "go test -count=1 ./..." {
		t.Errorf("spawned %q, want the consented command verbatim", rec.lines[0])
	}
}

// TestTestExitsWithTheSuiteStatus: a red suite must make `dross test` fail,
// with the exit code that says "your code is broken" rather than "the run never
// happened". Every caller gates a commit on this.
func TestTestExitsWithTheSuiteStatus(t *testing.T) {
	testFixture(t, "exit 1")
	trustFixture(t)
	installSpawnRecorder(t, errors.New("exit status 1"))

	err := runCmd(t, Test())
	if err == nil {
		t.Fatal("a failing suite reported success")
	}
	if got := ExitCode(err); got != exitSuiteFailed {
		t.Errorf("ExitCode = %d, want %d (suite failed)", got, exitSuiteFailed)
	}
}

// TestTestAppendsSelector: the selector reaches the spawned line, and an absent
// selector leaves the line exactly as consented.
func TestTestAppendsSelector(t *testing.T) {
	t.Run("with selector", func(t *testing.T) {
		testFixture(t, "go test -count=1")
		trustFixture(t)
		rec := installSpawnRecorder(t, nil)

		if err := runCmd(t, Test(), "./internal/cmd/..."); err != nil {
			t.Fatalf("dross test: %v", err)
		}
		if !strings.Contains(rec.lines[0], "./internal/cmd/...") {
			t.Errorf("the selector did not reach the spawned line: %q", rec.lines[0])
		}
	})

	t.Run("without selector", func(t *testing.T) {
		testFixture(t, "go test -count=1")
		trustFixture(t)
		rec := installSpawnRecorder(t, nil)

		if err := runCmd(t, Test()); err != nil {
			t.Fatalf("dross test: %v", err)
		}
		if rec.lines[0] != "go test -count=1" {
			t.Errorf("no selector changed the line to %q — it must stay the consented command", rec.lines[0])
		}
	})
}

// TestSelectorIsQuoted: the selector comes from an agent's argv, not from the
// user's consented text, so it is quoted before it reaches `sh -c`.
func TestSelectorIsQuoted(t *testing.T) {
	line := testCommandLine("go test", []string{"; rm -rf /"})
	if strings.Contains(line, "; rm -rf /") && !strings.Contains(line, `'; rm -rf /'`) {
		t.Errorf("a selector reached the shell unquoted: %q", line)
	}
}

// stampWriter records the wall-clock time of each Write, so a test can tell
// streamed output from output flushed once at the end.
type stampWriter struct {
	mu    sync.Mutex
	times []time.Time
	body  strings.Builder
}

func (w *stampWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.times = append(w.times, time.Now())
	w.body.Write(p)
	return len(p), nil
}

// TestTestStreamsOutput: the suite takes minutes, and a command that prints
// nothing until it finishes is indistinguishable from a hang — both to a human
// and to the agent reading the tail as it goes.
//
// The spawned script writes, sleeps, then writes again. If output were captured
// and flushed at the end, both writes would land together at the finish; the
// gap between the first and last write is what separates the two.
func TestTestStreamsOutput(t *testing.T) {
	var w stampWriter
	start := time.Now()
	if err := runLocalCommand(t.TempDir(), "echo first; sleep 0.4; echo second", &w, &w); err != nil {
		t.Fatalf("runLocalCommand: %v", err)
	}
	elapsed := time.Since(start)

	if got := w.body.String(); !strings.Contains(got, "first") || !strings.Contains(got, "second") {
		t.Fatalf("output did not arrive: %q", got)
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.times) < 2 {
		t.Fatalf("output landed in %d write(s) — a buffered run writes once at the end", len(w.times))
	}
	gap := w.times[len(w.times)-1].Sub(w.times[0])
	if gap < 200*time.Millisecond {
		t.Errorf("first and last write were %v apart within a %v run — output was buffered, not streamed", gap, elapsed)
	}
}

// TestExitCodeDefaultsToOne: tagging is opt-in. Every command that returned a
// plain error before still exits 1.
func TestExitCodeDefaultsToOne(t *testing.T) {
	if got := ExitCode(nil); got != 0 {
		t.Errorf("ExitCode(nil) = %d, want 0", got)
	}
	if got := ExitCode(errors.New("plain")); got != 1 {
		t.Errorf("ExitCode(plain) = %d, want 1 — untagged errors must keep the old behaviour", got)
	}
	wrapped := &ExitCodeError{Code: exitTransport, Err: errors.New("unreachable")}
	if got := ExitCode(wrapped); got != exitTransport {
		t.Errorf("ExitCode(tagged) = %d, want %d", got, exitTransport)
	}
}
