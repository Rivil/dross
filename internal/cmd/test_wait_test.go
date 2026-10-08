package cmd

import (
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Rivil/dross/internal/remote"
)

// init parks the suite's host-lock seam on a silent fake for the whole
// package, before any test runs.
//
// `dross test` now takes the host lock before its sync, and the real seam is
// an ssh. Two dozen tests in this package run the command against a granted
// "helicon" with the spawn seams faked; every one of them would otherwise
// reach a live Acquire — and on the remote gate, where the suite itself runs
// under the production lock, that is a deadlock rather than a slow test.
// Tests about the lock install their own recording fake over this one. An
// init in a _test.go file rather than an edit to the package's TestMain: the
// seam is this feature's, and this file is where it is explained.
func init() {
	testHold = func(remote.Target, remote.HoldEvents) (*suiteHold, error) {
		return &suiteHold{Outcome: remote.Held, Release: func() error { return nil }}, nil
	}
	suiteWarn = func(string) {}
}

// holdRecorder records the lock seam's calls in sequence with the remote
// spawns, so the order [warn, hold, sync, ssh, release] is one list.
type holdRecorder struct {
	mu       sync.Mutex
	order    []string
	targets  []remote.Target
	warnings []string
	// answer is what each Acquire returns; err wins when set.
	answer *suiteHold
	err    error
	rem    *remoteRecorder
}

func (h *holdRecorder) note(s string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.order = append(h.order, s)
}

func (h *holdRecorder) sequence() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.order...)
}

// installHoldRecorder wires the lock seam, the warning seam and the remote
// spawn seam into one ordered recording. remoteErr is what the spawns
// return.
func installHoldRecorder(t *testing.T, answer *suiteHold, err error, remoteErr error) *holdRecorder {
	t.Helper()
	h := &holdRecorder{answer: answer, err: err}
	if h.answer == nil {
		h.answer = &suiteHold{Outcome: remote.Held}
	}
	origHold, origWarn, origSpawn := testHold, suiteWarn, spawnRemote
	t.Cleanup(func() { testHold, suiteWarn, spawnRemote = origHold, origWarn, origSpawn })
	h.rem = &remoteRecorder{err: remoteErr}
	testHold = func(tg remote.Target, ev remote.HoldEvents) (*suiteHold, error) {
		h.mu.Lock()
		h.targets = append(h.targets, tg)
		h.mu.Unlock()
		h.note("hold")
		if h.err != nil {
			return nil, h.err
		}
		out := *h.answer
		out.Release = func() error { h.note("release"); return nil }
		return &out, nil
	}
	suiteWarn = func(msg string) {
		h.mu.Lock()
		h.warnings = append(h.warnings, msg)
		h.mu.Unlock()
		h.note("warn")
	}
	spawnRemote = func(argv []string, stdin string, stdout, stderr io.Writer) error {
		switch argv[0] {
		case "rsync":
			h.note("sync")
		default:
			h.note("ssh")
		}
		return h.rem.spawn(argv, stdin, stdout, stderr)
	}
	return h
}

func recordedHolder() remote.Holder {
	return remote.Holder{Project: "dross", Phase: "remote-host-mutex", RunID: "r-1", PID: 4242,
		Since: time.Date(2026, 9, 21, 9, 0, 0, 0, time.UTC)}
}

// TestTestWaitDefaultsToTenMinutes: the cap reaches the seam as a bounded
// policy — never Forever — and --wait overrides it.
func TestTestWaitDefaultsToTenMinutes(t *testing.T) {
	grantedTestFixture(t, "go test ./...")
	installSpawnRecorder(t, nil)
	h := installHoldRecorder(t, nil, nil, nil)
	if err := runCmd(t, Test()); err != nil {
		t.Fatalf("dross test: %v", err)
	}
	if len(h.targets) != 1 {
		t.Fatalf("hold called %d times, want 1", len(h.targets))
	}
	if w := h.targets[0].Lock.Wait; w.Max != 10*time.Minute || w.Forever {
		t.Errorf("default wait = %+v, want Max 10m, Forever false", w)
	}
	if hd := h.targets[0].Lock.Holder; hd.Project != "test-app" || hd.Phase != "" || !strings.HasPrefix(hd.RunID, "t-") {
		t.Errorf("holder = %+v, want project test-app, no phase, a t- run id", hd)
	}

	h2 := installHoldRecorder(t, nil, nil, nil)
	if err := runCmd(t, Test(), "--wait", "30s"); err != nil {
		t.Fatalf("dross test --wait 30s: %v", err)
	}
	if w := h2.targets[0].Lock.Wait; w.Max != 30*time.Second || w.Forever {
		t.Errorf("--wait 30s reached the seam as %+v", w)
	}
}

// TestWaitFlagValidation: a cap that does not parse is refused before any
// spawn, naming the flag.
func TestWaitFlagValidation(t *testing.T) {
	grantedTestFixture(t, "go test ./...")
	local := installSpawnRecorder(t, nil)
	for _, bad := range []string{"abc", "-1s"} {
		h := installHoldRecorder(t, nil, nil, nil)
		err := runCmd(t, Test(), "--wait", bad)
		if err == nil || !strings.Contains(err.Error(), "--wait") {
			t.Errorf("--wait %s: err = %v, want a refusal naming the flag", bad, err)
		}
		if len(h.sequence()) != 0 || local.count() != 0 {
			t.Errorf("--wait %s still spawned: %v", bad, h.sequence())
		}
	}
}

// TestWaitZeroSpawnsAtOnce is c-6's escape: --wait 0 is the zero policy
// (flock -n), and a busy host under it — the real Acquire's ErrHostBusy —
// still spawns the suite at once, naming the holder, with the suite's own
// exit code and never an error exit.
func TestWaitZeroSpawnsAtOnce(t *testing.T) {
	grantedTestFixture(t, "go test ./...")
	installSpawnRecorder(t, nil)
	h := installHoldRecorder(t, nil, nil, nil)
	if err := runCmd(t, Test(), "--wait", "0"); err != nil {
		t.Fatalf("dross test --wait 0: %v", err)
	}
	if w := h.targets[0].Lock.Wait; w.Max != 0 || w.Forever {
		t.Errorf("--wait 0 reached the seam as %+v, want the zero policy", w)
	}
	script, err := remote.HoldScript(h.targets[0])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(script, "flock -n") || strings.Contains(script, "flock -w") {
		t.Errorf("the zero policy's HoldScript is not flock -n:\n%s", script)
	}

	busy := &remote.BusyError{Host: "helicon", Holder: recordedHolder()}
	h2 := installHoldRecorder(t, nil, busy, nil)
	if err := runCmd(t, Test(), "--wait", "0"); err != nil {
		t.Fatalf("a busy host under --wait 0 became an error exit: %v", err)
	}
	if got := h2.sequence(); strings.Join(got, " ") != "hold warn sync ssh" {
		t.Errorf("sequence = %v, want [hold warn sync ssh] (spawn at once, nothing to release)", got)
	}
	if len(h2.warnings) != 1 || !strings.Contains(h2.warnings[0], "r-1") || !strings.Contains(h2.warnings[0], "running alongside") {
		t.Errorf("warnings = %q, want one naming r-1 and running alongside", h2.warnings)
	}
}

// TestTestNeverWaitsForever: no cap, however large, reaches the seam as the
// unbounded policy, and a busy answer never becomes an exit code.
func TestTestNeverWaitsForever(t *testing.T) {
	grantedTestFixture(t, "go test ./...")
	installSpawnRecorder(t, nil)
	h := installHoldRecorder(t, nil, nil, nil)
	if err := runCmd(t, Test(), "--wait", "9000h"); err != nil {
		t.Fatalf("dross test: %v", err)
	}
	if w := h.targets[0].Lock.Wait; w.Forever {
		t.Errorf("a huge cap reached the seam as Forever: %+v", w)
	}
	h2 := installHoldRecorder(t, nil, &remote.BusyError{Host: "helicon"}, nil)
	if err := runCmd(t, Test()); err != nil {
		t.Errorf("ErrHostBusy surfaced as an exit: %v", err)
	}
	if !strings.Contains(strings.Join(h2.sequence(), " "), "ssh") {
		t.Errorf("a busy answer did not spawn the suite: %v", h2.sequence())
	}
}

// TestInFlightWarningPrecedesTheHold: the warning describes the wait that
// follows, so it must be recorded before the hold — [warn hold sync ssh
// release] with a detached record in flight on this host.
func TestInFlightWarningPrecedesTheHold(t *testing.T) {
	root := grantedTestFixture(t, "go test ./...")
	installSpawnRecorder(t, nil)
	if err := recordDetachedRun(root, filepath.Dir(root), detachedRun{
		Phase: "p", RunID: "r-7", Host: "helicon", Workdir: "/srv/dross",
		RunDir: ".dross-runs/r-7", DispatchedAt: time.Now().UTC(), State: "running",
	}); err != nil {
		t.Fatal(err)
	}
	h := installHoldRecorder(t, nil, nil, nil)
	if err := runCmd(t, Test()); err != nil {
		t.Fatalf("dross test: %v", err)
	}
	if got := strings.Join(h.sequence(), " "); got != "warn hold sync ssh release" {
		t.Errorf("sequence = %v, want [warn hold sync ssh release]", h.sequence())
	}
	if len(h.warnings) != 1 || !strings.Contains(h.warnings[0], "r-7") {
		t.Errorf("warnings = %q", h.warnings)
	}
}

// TestExpiredWaitSpawnsAnywayNamingTheHolder: an Alongside answer spawns the
// suite, prints exactly one line naming the wait, the holder and the
// sharing, and exits with the suite's own code — 0 on a green fake.
func TestExpiredWaitSpawnsAnywayNamingTheHolder(t *testing.T) {
	grantedTestFixture(t, "go test ./...")
	installSpawnRecorder(t, nil)
	h := installHoldRecorder(t, &suiteHold{Outcome: remote.Alongside, Other: recordedHolder()}, nil, nil)
	if err := runCmd(t, Test()); err != nil {
		t.Fatalf("an expired wait became an error exit: %v", err)
	}
	if got := strings.Join(h.sequence(), " "); got != "hold warn sync ssh release" {
		t.Errorf("sequence = %v, want [hold warn sync ssh release]", h.sequence())
	}
	if len(h.warnings) != 1 {
		t.Fatalf("warnings = %q, want exactly one", h.warnings)
	}
	for _, want := range []string{"waited 10m", "r-1", "running alongside"} {
		if !strings.Contains(h.warnings[0], want) {
			t.Errorf("the alongside line lacks %q: %s", want, h.warnings[0])
		}
	}
}

// TestTestHoldsAcrossSyncAndSuite: one hold per host, before that host's
// sync, released after the last lane — on the bare run and on a two-lane
// run alike.
func TestTestHoldsAcrossSyncAndSuite(t *testing.T) {
	grantedTestFixture(t, "go test ./...")
	installSpawnRecorder(t, nil)
	h := installHoldRecorder(t, nil, nil, nil)
	if err := runCmd(t, Test()); err != nil {
		t.Fatalf("dross test: %v", err)
	}
	if got := strings.Join(h.sequence(), " "); got != "hold sync ssh release" {
		t.Errorf("bare run sequence = %v, want [hold sync ssh release]", h.sequence())
	}

	// Two lanes on the one host: one hold, one sync, two ssh, one release.
	grantedLaneFixture(t, goAndDocsLanes)
	installSpawnRecorder(t, nil)
	h2 := installHoldRecorder(t, nil, nil, nil)
	if err := runCmd(t, Test(), "--files", "internal/cmd/test.go", "--files", "README.md"); err != nil {
		t.Fatalf("dross test --files: %v", err)
	}
	if got := strings.Join(h2.sequence(), " "); got != "hold sync ssh ssh release" {
		t.Errorf("two-lane sequence = %v, want [hold sync ssh ssh release]", h2.sequence())
	}
}

// TestSuiteFailureStillReleases: a red lane and a failed spawn both release
// the hold — N releases for N holds.
func TestSuiteFailureStillReleases(t *testing.T) {
	grantedTestFixture(t, "go test ./...")
	installSpawnRecorder(t, nil)
	h := installHoldRecorder(t, nil, nil, fakeExit{code: 1})
	if err := runCmd(t, Test()); ExitCode(err) != exitSuiteFailed {
		t.Fatalf("a red suite exited %d: %v", ExitCode(err), err)
	}
	holds, releases := count(h.sequence(), "hold"), count(h.sequence(), "release")
	if holds != 1 || releases != 1 {
		t.Errorf("holds %d, releases %d — want 1 and 1: %v", holds, releases, h.sequence())
	}
	h2 := installHoldRecorder(t, nil, nil, fakeExit{code: 255})
	if err := runCmd(t, Test()); ExitCode(err) != exitTransport {
		t.Fatalf("a dead spawn exited %d: %v", ExitCode(err), err)
	}
	if count(h2.sequence(), "release") != count(h2.sequence(), "hold") {
		t.Errorf("releases != holds after a failed spawn: %v", h2.sequence())
	}
}

func count(seq []string, word string) int {
	n := 0
	for _, s := range seq {
		if s == word {
			n++
		}
	}
	return n
}

// TestMissingFlockWarnsAndRuns: a host without flock gets one warning naming
// the host and the bootstrap verb, and the suite runs unlocked with its own
// exit code.
func TestMissingFlockWarnsAndRuns(t *testing.T) {
	grantedTestFixture(t, "go test ./...")
	installSpawnRecorder(t, nil)
	noTool := fmt.Errorf("flock is not installed on helicon — the host lock needs it; run dross doctor: %w", remote.ErrLockTool)
	h := installHoldRecorder(t, nil, noTool, nil)
	if err := runCmd(t, Test()); err != nil {
		t.Fatalf("a flock-less host became an error exit: %v", err)
	}
	if got := strings.Join(h.sequence(), " "); got != "hold warn sync ssh" {
		t.Errorf("sequence = %v, want [hold warn sync ssh]", h.sequence())
	}
	if len(h.warnings) != 1 || !strings.Contains(h.warnings[0], "helicon") || !strings.Contains(h.warnings[0], "dross remote bootstrap") {
		t.Errorf("warnings = %q", h.warnings)
	}
	// And a transport failure taking the lock is exit 3, not a silent run.
	h3 := installHoldRecorder(t, nil, remote.Classify("ssh", "helicon", 255), nil)
	if err := runCmd(t, Test()); ExitCode(err) != exitTransport {
		t.Errorf("a transport failure on the lock exited %d: %v", ExitCode(err), err)
	}
	if strings.Contains(strings.Join(h3.sequence(), " "), "ssh") {
		t.Errorf("the suite ran on a host the lock could not reach: %v", h3.sequence())
	}
}

// TestInFlightWarningSaysWait: the wording describes the wait, except under
// --wait 0 where there is none and the old "will compete" line stands.
func TestInFlightWarningSaysWait(t *testing.T) {
	here := remote.Target{Host: "helicon", Workdir: "/srv/dross"}
	runs := []detachedRun{{Phase: "p", RunID: "r-4", Host: "helicon", Workdir: "/srv/dross", State: "running"}}
	w := inFlightRunWarning(runs, here, defaultTestWait)
	if !strings.Contains(w, "will wait up to 10m") || strings.Contains(w, "will compete") {
		t.Errorf("warning under the default cap: %q", w)
	}
	w0 := inFlightRunWarning(runs, here, 0)
	if !strings.Contains(w0, "will compete") || strings.Contains(w0, "will wait") {
		t.Errorf("warning under --wait 0: %q", w0)
	}
}

// TestLocalTestTakesNoLock: --local never touches the seam — there is no
// host to lock.
func TestLocalTestTakesNoLock(t *testing.T) {
	grantedTestFixture(t, "go test ./...")
	installSpawnRecorder(t, nil)
	h := installHoldRecorder(t, nil, errors.New("must not be called"), nil)
	if err := runCmd(t, Test(), "--local"); err != nil {
		t.Fatalf("dross test --local: %v", err)
	}
	if len(h.sequence()) != 0 {
		t.Errorf("--local reached the lock seam: %v", h.sequence())
	}
}
