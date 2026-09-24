package remote

import (
	"bytes"
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"
)

// standIn swaps holdCommandFn for a local bash running body in place of the
// ssh session. body reads the same stdin the real session would (the script,
// then keepalive newlines) and speaks the lock protocol on stdout. Returns
// the argv the seam received and the commands it built.
type standIn struct {
	mu   sync.Mutex
	argv [][]string
	cmds []*exec.Cmd
}

func useStandIn(t *testing.T, body string) *standIn {
	t.Helper()
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("no bash on PATH")
	}
	s := &standIn{}
	orig := holdCommandFn
	holdCommandFn = func(argv []string) *exec.Cmd {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.argv = append(s.argv, argv)
		c := exec.Command("bash", "-c", body)
		s.cmds = append(s.cmds, c)
		return c
	}
	t.Cleanup(func() { holdCommandFn = orig })
	return s
}

// readLoop is the tail every stand-in ends with: consume stdin until EOF.
const readLoop = "while read -r _; do :; done"

// waitingThen prints a waiting line with the canonical holder record, then
// the given continuation.
func waitingThen(rest string) string {
	return "printf 'lock=waiting\\n'; " +
		"printf 'holder.project=dross\\nholder.phase=p\\nholder.run=r-1\\nholder.pid=7\\nholder.user=u\\nholder.since=1700000000\\n'; " + rest
}

func waitTarget(wait WaitPolicy) Target {
	tt := lockedTarget()
	tt.Lock.Wait = wait
	return tt
}

func exited(h *Hold, within time.Duration) bool {
	select {
	case <-h.done:
		return true
	case <-time.After(within):
		return false
	}
}

// --- lifetime -----------------------------------------------------------------

// TestHoldSessionDiesWithItsStdin is c-4 at the session level. The hold is
// an ssh whose remote shell reads stdin; the lock lives exactly as long as
// that shell, and the shell lives exactly as long as stdin. Release closes
// stdin; a dead local process closes it too, with nobody calling anything.
func TestHoldSessionDiesWithItsStdin(t *testing.T) {
	script, err := HoldScript(lockedTarget())
	if err != nil {
		t.Fatalf("HoldScript = %v", err)
	}
	for _, bad := range []string{"setsid", "nohup", "cd "} {
		if strings.Contains(script, bad) {
			t.Errorf("HoldScript carries %q — the session must die with its connection:\n%s", bad, script)
		}
	}
	if !strings.HasSuffix(script, "while read -t 120 -r _; do :; done\n") {
		t.Errorf("HoldScript does not end in the lease loop:\n%s", script)
	}

	useStandIn(t, "printf 'lock=acquired\\n'; "+readLoop)
	h, err := Acquire(lockedTarget(), HoldEvents{})
	if err != nil {
		t.Fatalf("Acquire = %v", err)
	}
	if h.Outcome != Held {
		t.Fatalf("Outcome = %v, want Held", h.Outcome)
	}
	if exited(h, 300*time.Millisecond) {
		t.Fatal("the session exited before Release")
	}
	if err := h.Release(); err != nil {
		t.Errorf("Release = %v", err)
	}
	if !exited(h, time.Second) {
		t.Error("the session did not exit within 1s of Release")
	}

	// A local process that dies closes the pipe without a Release: the
	// stand-in must see EOF and exit, and Lost must say so.
	h2, err := Acquire(lockedTarget(), HoldEvents{})
	if err != nil {
		t.Fatalf("Acquire = %v", err)
	}
	h2.stopKeepalive()
	h2.stdin.Close()
	if !exited(h2, time.Second) {
		t.Fatal("the session did not exit on EOF with no Release")
	}
	if !h2.Lost() {
		t.Error("Lost() is false after the session died without Release")
	}
	h2.Release()
}

// TestVanishedLaptopFreesWithinTheLease: a laptop that suspends stops
// writing keepalives but does not close the connection. The host-side read
// timeout is what frees the host then — within the lease, not after TCP's
// own ~2h keepalive.
func TestVanishedLaptopFreesWithinTheLease(t *testing.T) {
	orig := holdKeepalive
	holdKeepalive = 10 * time.Millisecond
	t.Cleanup(func() { holdKeepalive = orig })
	useStandIn(t, "printf 'lock=acquired\\n'; while read -t 1 -r _; do :; done")

	h, err := Acquire(lockedTarget(), HoldEvents{})
	if err != nil {
		t.Fatalf("Acquire = %v", err)
	}
	if exited(h, 1500*time.Millisecond) {
		t.Fatal("the session exited while keepalives were flowing")
	}
	h.stopKeepalive()
	if !exited(h, 2500*time.Millisecond) {
		t.Fatal("the session outlived the lease after the keepalives stopped")
	}
	h.Release()
}

// TestReleaseClosesStdinThenWaits: a Wait before the close would block on a
// loop that is waiting for the EOF the close delivers.
func TestReleaseClosesStdinThenWaits(t *testing.T) {
	useStandIn(t, "printf 'lock=acquired\\n'; cat >/dev/null")
	h, err := Acquire(lockedTarget(), HoldEvents{})
	if err != nil {
		t.Fatalf("Acquire = %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- h.Release() }()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Release = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Release did not return within 1s — it waited before closing stdin")
	}
	if h.Release() != nil {
		t.Error("a second Release is not a no-op")
	}
}

// --- the protocol -------------------------------------------------------------

// TestAcquirePrintsTheHolderOnce is c-3 for an attached leg: the waiter names
// what it waits on, once.
func TestAcquirePrintsTheHolderOnce(t *testing.T) {
	useStandIn(t, waitingThen("sleep 0.2; printf 'lock=acquired\\n'; "+readLoop))
	var log bytes.Buffer
	h, err := Acquire(lockedTarget(), HoldEvents{Log: &log})
	if err != nil {
		t.Fatalf("Acquire = %v", err)
	}
	defer h.Release()
	if h.Outcome != Held || h.Other.RunID != "r-1" {
		t.Errorf("Outcome = %v, Other = %+v", h.Outcome, h.Other)
	}
	want := "waiting on dross/p run r-1 (pid 7) since 2023-11-14T22:13:20Z\n"
	if log.String() != want {
		t.Errorf("log = %q, want exactly %q", log.String(), want)
	}
}

// TestAcquireHeartbeatsWhileWaiting: the locked attached_busy_policy is one
// holder line then a heartbeat every few minutes — and nothing after the
// lock is ours.
func TestAcquireHeartbeatsWhileWaiting(t *testing.T) {
	useStandIn(t, waitingThen("sleep 0.3; printf 'lock=acquired\\n'; "+readLoop))
	var log bytes.Buffer
	h, err := Acquire(lockedTarget(), HoldEvents{Log: &log, HeartbeatEvery: 50 * time.Millisecond})
	if err != nil {
		t.Fatalf("Acquire = %v", err)
	}
	defer h.Release()
	at := log.String()
	beats := 0
	for _, l := range strings.Split(strings.TrimSpace(at), "\n") {
		if strings.HasPrefix(l, "still waiting") {
			beats++
			for _, want := range []string{"helicon", "r-1", "elapsed"} {
				if !strings.Contains(l, want) {
					t.Errorf("heartbeat lacks %q: %s", want, l)
				}
			}
		}
	}
	if beats < 3 {
		t.Errorf("want >= 3 heartbeats over 300ms at 50ms, got %d:\n%s", beats, at)
	}
	time.Sleep(200 * time.Millisecond)
	if log.String() != at {
		t.Errorf("heartbeats continued after acquisition:\n%s", log.String())
	}
}

// TestAcquireEmptyRecordIsNotBlank: a holder that has not written its record
// yet is a fact worth stating, not a blank to print.
func TestAcquireEmptyRecordIsNotBlank(t *testing.T) {
	useStandIn(t, "printf 'lock=waiting\\n'; sleep 0.5; printf 'lock=acquired\\n'; "+readLoop)
	var log bytes.Buffer
	h, err := Acquire(lockedTarget(), HoldEvents{Log: &log})
	if err != nil {
		t.Fatalf("Acquire = %v", err)
	}
	defer h.Release()
	if !strings.Contains(log.String(), "holder not yet recorded") || !strings.Contains(log.String(), "helicon") {
		t.Errorf("log = %q", log.String())
	}
}

// TestAcquireExpiredRunsAlongside: under a bounded policy an expired wait is
// an outcome, not an error — the suite runs beside the holder and Release
// stays uniform. Under the zero policy the same answer is a refusal that
// leaves no process behind.
func TestAcquireExpiredRunsAlongside(t *testing.T) {
	s := useStandIn(t, waitingThen("printf 'lock=busy\\n'; "+readLoop))
	h, err := Acquire(waitTarget(WaitPolicy{Max: time.Second}), HoldEvents{})
	if err != nil {
		t.Fatalf("Acquire = %v", err)
	}
	if h.Outcome != Alongside || h.Other.RunID != "r-1" || h.Other.PID != 7 {
		t.Errorf("Outcome = %v, Other = %+v", h.Outcome, h.Other)
	}
	if h.Lost() {
		t.Error("Lost() is true while the stand-in still reads stdin")
	}
	if err := h.Release(); err != nil {
		t.Errorf("Release = %v", err)
	}

	_, err = Acquire(waitTarget(WaitPolicy{}), HoldEvents{})
	if !errors.Is(err, ErrHostBusy) {
		t.Fatalf("zero policy: Acquire = %v, want ErrHostBusy", err)
	}
	if !strings.Contains(err.Error(), "dross/p run r-1") {
		t.Errorf("the refusal does not name the holder: %v", err)
	}
	var be *BusyError
	if !errors.As(err, &be) || be.Holder.RunID != "r-1" {
		t.Errorf("the holder does not ride on the error: %v", err)
	}
	s.mu.Lock()
	last := s.cmds[len(s.cmds)-1]
	s.mu.Unlock()
	if last.ProcessState == nil || !last.ProcessState.Exited() {
		t.Error("a refused Acquire left its session running")
	}
}

// TestMissingFlockIsNamed: a host without the tool is an error that names
// the tool and the fix — not a silent unlocked run.
func TestMissingFlockIsNamed(t *testing.T) {
	useStandIn(t, "printf 'lock=noflock\\n'; "+readLoop)
	_, err := Acquire(lockedTarget(), HoldEvents{})
	if !errors.Is(err, ErrLockTool) {
		t.Fatalf("Acquire = %v, want ErrLockTool", err)
	}
	for _, want := range []string{"flock", "dross doctor", "helicon"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error lacks %q: %v", want, err)
		}
	}
}

// TestEmptyAnswerIsNotAnAcquisition: a session that said nothing did not
// lock anything, and a transport failure is never a busy host.
func TestEmptyAnswerIsNotAnAcquisition(t *testing.T) {
	useStandIn(t, "exit 0")
	_, err := Acquire(lockedTarget(), HoldEvents{})
	if !errors.Is(err, ErrRemoteCommand) {
		t.Errorf("silent exit 0: Acquire = %v, want ErrRemoteCommand", err)
	}
	useStandIn(t, "echo 'ssh: connect refused' >&2; exit 255")
	var stderr strings.Builder
	prev := diagStderr
	diagStderr = &stderr
	defer func() { diagStderr = prev }()
	_, err = Acquire(lockedTarget(), HoldEvents{})
	if !errors.Is(err, ErrTransport) || errors.Is(err, ErrHostBusy) {
		t.Errorf("exit 255: Acquire = %v, want ErrTransport and never ErrHostBusy", err)
	}
	// ssh's own words reach the user on stderr, never the error, which can
	// outlive the run.
	if !strings.Contains(stderr.String(), "connect refused") {
		t.Errorf("ssh's stderr did not reach the terminal: %q", stderr.String())
	}
	if strings.Contains(err.Error(), "connect refused") {
		t.Errorf("ssh's stderr is carried on the error: %v", err)
	}
}

// TestLostFlipsWhenTheSessionDies: a hold that ends early is a measurement
// that may have been shared; the caller must be able to see it.
func TestLostFlipsWhenTheSessionDies(t *testing.T) {
	useStandIn(t, "printf 'lock=acquired\\n'; sleep 0.2; exit 3")
	h, err := Acquire(lockedTarget(), HoldEvents{})
	if err != nil {
		t.Fatalf("Acquire = %v", err)
	}
	if h.Lost() {
		t.Error("Lost() before the session ended")
	}
	if !exited(h, 2*time.Second) {
		t.Fatal("the stand-in did not exit")
	}
	if !h.Lost() {
		t.Error("Lost() is false after the session died")
	}
	err = h.Release()
	if err == nil || !strings.Contains(err.Error(), "helicon") {
		t.Errorf("Release after a lost session = %v, want an error naming the host", err)
	}
}

// TestHoldGoesOverStdin: the argv is the same four words every session
// uses; the script and the holder's identity never reach the host's ps.
func TestHoldGoesOverStdin(t *testing.T) {
	s := useStandIn(t, "printf 'lock=acquired\\n'; "+readLoop)
	tt := lockedTarget()
	h, err := Acquire(tt, HoldEvents{})
	if err != nil {
		t.Fatalf("Acquire = %v", err)
	}
	defer h.Release()
	want, _ := SSHArgs(tt)
	if len(s.argv) != 1 || strings.Join(s.argv[0], " ") != strings.Join(want, " ") {
		t.Errorf("argv = %v, want %v", s.argv, want)
	}
	joined := strings.Join(s.argv[0], " ")
	for _, leak := range []string{"flock", "dross-host.lock", "r-1", "project="} {
		if strings.Contains(joined, leak) {
			t.Errorf("argv carries %q: %v", leak, s.argv[0])
		}
	}
}

// --- against a real flock ----------------------------------------------------

func useRealBash(t *testing.T) {
	t.Helper()
	needFlock(t)
	orig := holdCommandFn
	holdCommandFn = func(argv []string) *exec.Cmd { return exec.Command("bash", "-s") }
	t.Cleanup(func() { holdCommandFn = orig })
}

// TestTwoHoldsSerializeOnARealFlock proves c-1 and c-2 end to end: two
// holds from two workdirs on one host, the second waits and names the
// first, and gets the lock within 2s of the first's Release.
func TestTwoHoldsSerializeOnARealFlock(t *testing.T) {
	useRealBash(t)
	useTempLock(t)
	a := Target{Host: host, Workdir: "/srv/a", Lock: LockSpec{Holder: Holder{Project: "alpha", Phase: "p", RunID: "r-1"}, Wait: Forever}}
	b := Target{Host: host, Workdir: "/srv/b", Lock: LockSpec{Holder: Holder{Project: "beta", Phase: "q", RunID: "r-2"}, Wait: Forever}}

	first, err := Acquire(a, HoldEvents{})
	if err != nil {
		t.Fatalf("first Acquire = %v", err)
	}
	if first.Outcome != Held {
		t.Fatalf("first Outcome = %v", first.Outcome)
	}
	var log lockedBuffer
	type result struct {
		h   *Hold
		err error
	}
	got := make(chan result, 1)
	go func() {
		h, err := Acquire(b, HoldEvents{Log: &log})
		got <- result{h, err}
	}()
	select {
	case r := <-got:
		t.Fatalf("the second Acquire returned while the first held the lock: %+v %v", r.h, r.err)
	case <-time.After(700 * time.Millisecond):
	}
	if !strings.Contains(log.String(), "waiting on alpha/p run r-1") {
		t.Errorf("the waiter does not name the holder: %q", log.String())
	}
	if err := first.Release(); err != nil {
		t.Errorf("first Release = %v", err)
	}
	select {
	case r := <-got:
		if r.err != nil || r.h.Outcome != Held || r.h.Other.RunID != "r-1" {
			t.Fatalf("second Acquire = %+v, %v", r.h, r.err)
		}
		r.h.Release()
	case <-time.After(2 * time.Second):
		t.Fatal("the second Acquire did not return within 2s of the first Release")
	}
}

// TestAKilledHolderReleasesWithNoCleanup proves c-4: the first holder is
// SIGKILLed, its record stays in the file (stale content), and the next
// Acquire is Held at once with nobody clearing anything (no stale lock).
func TestAKilledHolderReleasesWithNoCleanup(t *testing.T) {
	useRealBash(t)
	path := useTempLock(t)
	first, err := Acquire(lockedTarget(), HoldEvents{})
	if err != nil {
		t.Fatalf("first Acquire = %v", err)
	}
	if err := first.cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	if !exited(first, 2*time.Second) {
		t.Fatal("the killed session did not exit")
	}
	rec, _ := os.ReadFile(path)
	if !strings.Contains(string(rec), "run=r-1") {
		t.Errorf("the killed holder's record was cleaned up — nothing should have:\n%s", rec)
	}
	tt := lockedTarget()
	tt.Lock.Holder.RunID = "r-2"
	var log lockedBuffer
	done := make(chan *Hold, 1)
	go func() {
		h, err := Acquire(tt, HoldEvents{Log: &log})
		if err != nil {
			t.Errorf("second Acquire = %v", err)
		}
		done <- h
	}()
	select {
	case h := <-done:
		if h != nil {
			if h.Outcome != Held {
				t.Errorf("second Outcome = %v", h.Outcome)
			}
			h.Release()
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the second Acquire did not return within 2s of the first being killed")
	}
	if strings.Contains(log.String(), "waiting") {
		t.Errorf("the second holder waited on a dead one: %q", log.String())
	}
	first.Release()
}

// lockedBuffer is a bytes.Buffer safe to read while another goroutine logs.
type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

// TestHoldTransportOutputGoesToStderr: what ssh writes to stderr is the
// transport's own output. It reaches the user on stderr; the error carries the
// classification only, and stays an ErrTransport.
func TestHoldTransportOutputGoesToStderr(t *testing.T) {
	useStandIn(t, "echo CANARY-SSH >&2; exit 255")
	var stderr strings.Builder
	prev := diagStderr
	diagStderr = &stderr
	defer func() { diagStderr = prev }()

	_, err := Acquire(lockedTarget(), HoldEvents{})
	if err == nil {
		t.Fatal("a failed hold session returned no error")
	}
	if strings.Contains(err.Error(), "CANARY-SSH") {
		t.Errorf("ssh's output reached the error: %v", err)
	}
	if !strings.Contains(stderr.String(), "CANARY-SSH") {
		t.Errorf("stderr = %q, want ssh's output there", stderr.String())
	}
	if !errors.Is(err, ErrTransport) {
		t.Errorf("err = %v, want it to stay an ErrTransport", err)
	}
}

// TestUnreadableHolderFieldIsFixedProse: a holder record's pid that is not a
// number names the field in the error; the raw value goes to stderr.
func TestUnreadableHolderFieldIsFixedProse(t *testing.T) {
	var stderr strings.Builder
	prev := diagStderr
	diagStderr = &stderr
	defer func() { diagStderr = prev }()

	_, _, err := ParseHolder("holder.run=r1\nholder.pid=CANARY-PID")
	if err == nil || !strings.Contains(err.Error(), "unreadable holder pid") {
		t.Fatalf("ParseHolder = %v, want an unreadable holder pid error", err)
	}
	if strings.Contains(err.Error(), "CANARY-PID") {
		t.Errorf("the raw field reached the error: %v", err)
	}
	if !strings.Contains(stderr.String(), "CANARY-PID") {
		t.Errorf("stderr = %q, want the raw field there", stderr.String())
	}
}

// TestMalformedHolderYieldsNoHolder: a field that fails the protocol-token
// shape check is an error with no Holder — the marker clears only what passed.
func TestMalformedHolderYieldsNoHolder(t *testing.T) {
	prev := diagStderr
	diagStderr = io.Discard
	defer func() { diagStderr = prev }()
	for _, record := range []string{
		"holder.run=r1\nholder.project=" + strings.Repeat("x", maxProtocolToken+1),
		"holder.run=r1\nholder.user=bad\x07bell",
		"holder.run=bad\x1b[31mred",
	} {
		h, ok, err := ParseHolder(record)
		if err == nil || ok || !h.IsZero() {
			t.Errorf("ParseHolder(%q) = %+v, %v, %v — want an error and no Holder", record, h, ok, err)
		}
	}
	if h, ok, err := ParseHolder("holder.run=r1\nholder.project=My App\nholder.phase=auth\nholder.pid=7"); err != nil || !ok || h.Project != "My App" {
		t.Errorf("a well-formed record = %+v, %v, %v", h, ok, err)
	}
}
