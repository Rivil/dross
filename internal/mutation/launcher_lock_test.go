package mutation

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Rivil/dross/internal/remote"
)

// TestMain parks the host-lock seam on a silent fake for the whole package.
//
// A remote run now takes the host lock before its push, and the real seam is
// an ssh. Every test that fakes launcherCommand would otherwise reach a live
// Acquire against helicon — and on the remote gate, where the suite itself
// runs under the production lock, that is a deadlock rather than a slow test.
// Tests about the lock install their own recording fake over this one.
func TestMain(m *testing.M) {
	launcherHold = func(remote.Target, remote.HoldEvents) (hostHold, error) { return &fakeHold{}, nil }
	launcherLog = io.Discard
	os.Exit(m.Run())
}

// fakeHold is a hold whose fate the test controls.
type fakeHold struct {
	lost     bool
	releases int
	relErr   error
	onRel    func()
}

func (f *fakeHold) Lost() bool { return f.lost }
func (f *fakeHold) Release() error {
	f.releases++
	if f.onRel != nil {
		f.onRel()
	}
	return f.relErr
}

// recordHold swaps the lock seam for one that tags "hold" and "release" into
// rec, in sequence with the recorded argv. err, when set, is what Acquire
// returns — and nothing is then held to release.
func recordHold(t *testing.T, rec *[][]string, err error) *fakeHold {
	t.Helper()
	fh := &fakeHold{}
	fh.onRel = func() { *rec = append(*rec, []string{"release"}) }
	orig := launcherHold
	launcherHold = func(remote.Target, remote.HoldEvents) (hostHold, error) {
		*rec = append(*rec, []string{"hold"})
		if err != nil {
			return nil, err
		}
		return fh, nil
	}
	t.Cleanup(func() { launcherHold = orig })
	return fh
}

// TestRemoteRunHoldsOnceAcrossEveryPackage is the locked lock_granularity
// decision executed: one acquisition per run, before the push, held across
// every package, released after the last.
func TestRemoteRunHoldsOnceAcrossEveryPackage(t *testing.T) {
	failLocalSpawns(t)
	root := t.TempDir()
	rec := recordRemote(t, func(argv []string) {
		if isFetch(argv) {
			os.WriteFile(argv[3], []byte(fixtureGremlinsBareBasename), 0o644)
		}
	})
	fh := recordHold(t, rec, nil)
	g := &Gremlins{ProjectRoot: root, Remote: helicon("/srv/dross")}
	if _, err := g.Run([]string{"internal/a/a.go", "internal/b/b.go"}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if fh.releases != 1 {
		t.Errorf("released %d times, want 1", fh.releases)
	}
	want := []string{"hold", "push", "rm", "run", "fetch", "rm", "run", "fetch", "release"}
	if got := kinds(*rec); !reflect.DeepEqual(got, want) {
		t.Errorf("sequence = %v, want %v", got, want)
	}
}

// TestCloseReleasesIdempotently: a local run has nothing to release, a second
// Close releases nothing twice, and a failed scratch wipe never skips the
// release — a held lock nobody is using is the starvation this exists to end.
func TestCloseReleasesIdempotently(t *testing.T) {
	var rec [][]string
	fh := recordHold(t, &rec, nil)

	local, err := newLauncher("gremlins", "", nil, t.TempDir(), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := local.Close(); err != nil {
		t.Errorf("local Close = %v", err)
	}
	if fh.releases != 0 {
		t.Errorf("a local launcher released a hold %d times", fh.releases)
	}

	l, err := newLauncher("gremlins", "", helicon("/srv/dross"), t.TempDir(), "", []string{"GOCACHE"})
	if err != nil {
		t.Fatal(err)
	}
	// Take the hold without pushing (the push is not the point here).
	if err := l.ensureHeld(); err != nil {
		t.Fatal(err)
	}
	// The scratch wipe fails: rm exits non-zero.
	orig := launcherCommand
	launcherCommand = func(argv []string, stdin string) *exec.Cmd { return exec.Command("false") }
	t.Cleanup(func() { launcherCommand = orig })
	var stderr bytes.Buffer
	captureStderrInto(t, &stderr, func() {
		err = l.Close()
	})
	if err == nil || !errors.Is(err, remote.ErrRemoteCommand) {
		t.Errorf("Close with a failing wipe = %v, want the rm error returned", err)
	}
	if fh.releases != 1 {
		t.Errorf("a failing scratch wipe released the hold %d times, want 1", fh.releases)
	}
	if err := l.Close(); err != nil {
		t.Errorf("second Close = %v", err)
	}
	if fh.releases != 1 {
		t.Errorf("a second Close released again (%d)", fh.releases)
	}
}

// captureStderrInto redirects os.Stderr for fn.
func captureStderrInto(t *testing.T, into *bytes.Buffer, fn func()) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	orig := os.Stderr
	os.Stderr = w
	done := make(chan struct{})
	go func() { io.Copy(into, r); close(done) }()
	fn()
	w.Close()
	os.Stderr = orig
	<-done
}

// TestBusyRefusalSpawnsNothing: a host held under the zero policy is a
// refusal before any process — no push, no rm — and the adapter's error
// keeps its identity for verify to map.
func TestBusyRefusalSpawnsNothing(t *testing.T) {
	failLocalSpawns(t)
	rec := recordRemote(t, nil)
	busy := &remote.BusyError{Host: "helicon", Holder: remote.Holder{Project: "other", Phase: "q", RunID: "r-9", PID: 4}}
	recordHold(t, rec, busy)
	g := &Gremlins{ProjectRoot: t.TempDir(), Remote: helicon("/srv/dross")}
	_, err := g.Run([]string{"internal/a/a.go"})
	if !errors.Is(err, remote.ErrHostBusy) {
		t.Fatalf("Run = %v, want ErrHostBusy", err)
	}
	if got := kinds(*rec); !reflect.DeepEqual(got, []string{"hold"}) {
		t.Errorf("a refused run still spawned: %v", got)
	}
}

// TestUnlockableHostIsNeverMeasuredOn: no flock, no measurement — and the
// error says which host and what to run.
func TestUnlockableHostIsNeverMeasuredOn(t *testing.T) {
	failLocalSpawns(t)
	rec := recordRemote(t, nil)
	noTool := fmt.Errorf("flock is not installed on helicon — the host lock needs it; run dross doctor: %w", remote.ErrLockTool)
	recordHold(t, rec, noTool)
	g := &Gremlins{ProjectRoot: t.TempDir(), Remote: helicon("/srv/dross")}
	_, err := g.Run([]string{"internal/a/a.go"})
	if !errors.Is(err, remote.ErrLockTool) {
		t.Fatalf("Run = %v, want ErrLockTool", err)
	}
	for _, want := range []string{"helicon", "dross doctor"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error lacks %q: %v", want, err)
		}
	}
	if got := kinds(*rec); !reflect.DeepEqual(got, []string{"hold"}) {
		t.Errorf("an unlockable host was pushed to or run on: %v", got)
	}
}

// TestLauncherRefusesAnAnonymousRemoteRun is the no-bypass rule at
// construction, for every adapter: a remote target must name the run it
// holds the host under.
func TestLauncherRefusesAnAnonymousRemoteRun(t *testing.T) {
	for _, adapter := range []string{"gremlins", "stryker", "stryker-net"} {
		anon := &remote.Target{Host: "helicon", Workdir: "/srv/dross"}
		_, err := newLauncher(adapter, "", anon, t.TempDir(), "", nil)
		if err == nil || !strings.Contains(err.Error(), "holder") {
			t.Errorf("%s: an anonymous remote target was accepted (%v)", adapter, err)
		}
		named := &remote.Target{Host: "helicon", Workdir: "/srv/dross", Lock: remote.LockSpec{Holder: remote.Holder{RunID: "r-1"}}}
		if _, err := newLauncher(adapter, "", named, t.TempDir(), "", nil); err != nil {
			t.Errorf("%s: a named remote target was refused: %v", adapter, err)
		}
	}
	if _, err := newLauncher("gremlins", "", nil, t.TempDir(), "", nil); err != nil {
		t.Errorf("a local launcher was refused over the holder: %v", err)
	}
}

// TestLauncherRoutesWaitLinesToItsLog: the hold is taken on the run's own
// target with the launcher's log and heartbeat, so a waiting attached leg
// reaches the terminal (c-3).
func TestLauncherRoutesWaitLinesToItsLog(t *testing.T) {
	var buf bytes.Buffer
	origLog, origHB := launcherLog, launcherHeartbeat
	launcherLog = &buf
	t.Cleanup(func() { launcherLog, launcherHeartbeat = origLog, origHB })

	var gotT remote.Target
	var gotEv remote.HoldEvents
	orig := launcherHold
	launcherHold = func(tt remote.Target, ev remote.HoldEvents) (hostHold, error) {
		gotT, gotEv = tt, ev
		fmt.Fprintln(ev.Log, remote.WaitLine(remote.Holder{Project: "other", Phase: "q", RunID: "r-9", PID: 4}))
		return &fakeHold{}, nil
	}
	t.Cleanup(func() { launcherHold = orig })

	target := helicon("/srv/dross")
	target.Env = []remote.EnvVar{{Name: "X", Value: "1"}}
	l, err := newLauncher("gremlins", "", target, t.TempDir(), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := l.ensureHeld(); err != nil {
		t.Fatal(err)
	}
	gotT.Env = nil
	wantT := *target
	wantT.Env = nil
	if !reflect.DeepEqual(gotT, wantT) {
		t.Errorf("hold target = %+v, want the run's target %+v", gotT, wantT)
	}
	if gotEv.Log != &buf {
		t.Error("the hold's Log is not launcherLog")
	}
	if gotEv.HeartbeatEvery != launcherHeartbeat || launcherHeartbeat <= 0 {
		t.Errorf("HeartbeatEvery = %v, want launcherHeartbeat (%v, > 0)", gotEv.HeartbeatEvery, launcherHeartbeat)
	}
	if !strings.Contains(buf.String(), "waiting on other/q run r-9") {
		t.Errorf("the waiting line did not reach the log: %q", buf.String())
	}
	if launcherHeartbeat < time.Minute {
		t.Errorf("launcherHeartbeat = %v, want every few minutes", launcherHeartbeat)
	}
}

// TestLostHoldRefusesToRecord: a hold lost after package 1 ends the run
// before package 2, naming the host and the loss, with the dead session's
// exit reason on the error rather than swallowed.
func TestLostHoldRefusesToRecord(t *testing.T) {
	failLocalSpawns(t)
	var rec [][]string
	fh := recordHold(t, &rec, nil)
	fh.relErr = errors.New("hold session on helicon ended before release: exit status 255")
	runs := 0
	orig := launcherCommand
	launcherCommand = func(argv []string, stdin string) *exec.Cmd {
		cp := append(append([]string(nil), argv...), stdin)
		rec = append(rec, cp)
		if argv[0] == "ssh" && !strings.Contains(stdin, "'rm' '-rf'") {
			runs++
			if runs == 1 {
				// The session dies while package 1's tool is running.
				fh.lost = true
			}
		}
		return exec.Command("true")
	}
	t.Cleanup(func() { launcherCommand = orig })

	g := &Gremlins{ProjectRoot: t.TempDir(), Remote: helicon("/srv/dross")}
	_, err := g.Run([]string{"internal/a/a.go", "internal/b/b.go"})
	if err == nil {
		t.Fatal("a run whose hold was lost returned a report")
	}
	for _, want := range []string{"helicon", "lock lost", "exit status 255"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error lacks %q: %v", want, err)
		}
	}
	if runs != 1 {
		t.Errorf("package 2 ran after the hold was lost (%d runs)", runs)
	}
	if fh.releases != 1 {
		t.Errorf("the dead session was released %d times, want 1", fh.releases)
	}
	if _, err := os.Stat(filepath.Join(g.ProjectRoot, "reports")); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
}
