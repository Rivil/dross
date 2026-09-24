package remote

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

var (
	// ErrHostBusy is the lock being held by another run when the policy was
	// not to wait for it. The error carries the holder (see BusyError) so
	// the refusal can name who has the host.
	ErrHostBusy = errors.New("remote host is busy")
	// ErrLockTool is the host lacking flock. Nothing was locked and nothing
	// should be measured there until it is installed.
	ErrLockTool = errors.New("remote host lacks the lock tool")
)

// BusyError is ErrHostBusy with the holder attached.
type BusyError struct {
	Host   string
	Holder Holder
}

func (e *BusyError) Error() string {
	if e.Holder.IsZero() {
		return fmt.Sprintf("host %s is busy: the host lock is held (holder not yet recorded): %v", e.Host, ErrHostBusy)
	}
	return fmt.Sprintf("host %s is busy: held by %s since %s: %v",
		e.Host, e.Holder.Name(), e.Holder.Since.UTC().Format(time.RFC3339), ErrHostBusy)
}

func (e *BusyError) Unwrap() error { return ErrHostBusy }

// HoldOutcome is what an Acquire ended with.
type HoldOutcome int

const (
	// Held: the lock is ours, on fd 9 of the session's shell, until Release.
	Held HoldOutcome = iota + 1
	// Alongside: a bounded wait expired and the caller runs beside the
	// holder with nothing held. The session is kept so Release is uniform.
	Alongside
)

// HoldEvents is where an Acquire reports what it is waiting on.
type HoldEvents struct {
	// Log receives the one "waiting on" line and the heartbeats. nil
	// discards them.
	Log io.Writer
	// HeartbeatEvery is the interval between "still waiting" lines while the
	// lock is held by someone else. Zero means no heartbeats.
	HeartbeatEvery time.Duration
}

// holdCommandFn builds the local process a hold session runs in. It is the
// package's one exec seam, reused rather than duplicated: the argv is
// SSHArgs(t) and the audit entry that accepts buildCommand covers this use.
// Swapped in tests for a stand-in that speaks the lock protocol.
var holdCommandFn = func(argv []string) *exec.Cmd { return buildCommand(argv, "") }

// holdKeepalive is how often the local side writes a newline into the hold
// session's stdin. The host-side loop reads with a timeout (see HoldScript)
// of a few of these, so a laptop that vanishes — lid closed, network gone —
// frees the host within that lease rather than after TCP's own ~2h
// keepalive gives up. A var so a test can shrink the lease.
var holdKeepalive = 30 * time.Second

// holdLease is the host-side read timeout, in seconds. Four keepalives
// deep: one missed write is a hiccup, four is a laptop that is gone.
const holdLease = 120

// holderLull is how long Acquire waits for a holder record to finish
// arriving after a `lock=waiting` or `lock=busy` line. The record is
// printed by the same shell immediately after the line and before it
// blocks in flock, so the lines arrive together; the lull only bounds the
// empty-record case, where there is nothing to wait for.
var holderLull = 300 * time.Millisecond

// HoldScript renders what the hold session's shell runs: the lock prelude,
// then a loop that holds the shell — and with it fd 9 — open for as long as
// its stdin delivers a byte every couple of minutes.
//
// No preamble, no cd: the lock is taken before the workdir is synced and may
// be taken for a workdir that does not exist yet. No setsid, no nohup: the
// session is MEANT to die with the ssh connection — that is how a holder
// that dies releases the host with no cleanup (c-4).
func HoldScript(t Target) (string, error) {
	if err := t.Validate(); err != nil {
		return "", err
	}
	lock, err := LockPrelude(t.Lock)
	if err != nil {
		return "", err
	}
	return lock + fmt.Sprintf("while read -t %d -r _; do :; done\n", holdLease), nil
}

// WaitLine renders the one line a waiter prints about who it waits on.
func WaitLine(h Holder) string {
	return fmt.Sprintf("waiting on %s since %s", h.Name(), h.Since.UTC().Format(time.RFC3339))
}

// Hold is a live hold session: an ssh whose remote shell holds fd 9.
type Hold struct {
	// Outcome is Held or Alongside.
	Outcome HoldOutcome
	// Other is the holder this session waited on (Held after a wait) or is
	// running beside (Alongside). Zero when the lock was free at once.
	Other Holder

	host     string
	cmd      *exec.Cmd
	stdin    io.WriteCloser
	stderr   *boundedBuffer
	events   chan lockEvent
	done     chan struct{}
	waitErr  error
	stopKA   chan struct{}
	kaOnce   sync.Once
	released atomic.Bool
}

// Acquire opens a hold session on t and drives the lock protocol to an
// outcome. On Held or Alongside the session stays open and the caller MUST
// Release it; on any error no process is left behind.
//
// The script goes over stdin, never argv: the holder record names a project
// and a phase, and the argv of every process on the host is readable by
// every user on it. Stdin is then KEPT open — the session's lifetime is the
// lock's lifetime, and a local process that dies takes the lock with it.
func Acquire(t Target, ev HoldEvents) (*Hold, error) {
	argv, err := SSHArgs(t)
	if err != nil {
		return nil, err
	}
	script, err := HoldScript(t)
	if err != nil {
		return nil, err
	}
	cmd := holdCommandFn(argv)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("hold session on %s: %w: %v", t.Host, ErrTransport, err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("hold session on %s: %w: %v", t.Host, ErrTransport, err)
	}
	h := &Hold{
		host:   t.Host,
		cmd:    cmd,
		stdin:  stdin,
		stderr: &boundedBuffer{limit: 4096},
		events: make(chan lockEvent, 16),
		done:   make(chan struct{}),
		stopKA: make(chan struct{}),
	}
	cmd.Stderr = h.stderr
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("hold session on %s: %w: %v", t.Host, ErrTransport, err)
	}
	// One goroutine owns the pipe: it reads to EOF and only then Waits,
	// which is the order os/exec requires for a StdoutPipe. A second turns
	// the raw lines into protocol events, so drive never sees a half-read
	// holder record.
	raw := make(chan string, 64)
	go func() {
		sc := bufio.NewScanner(stdout)
		for sc.Scan() {
			raw <- sc.Text()
		}
		close(raw)
		h.waitErr = cmd.Wait()
		close(h.done)
	}()
	go assembleLockEvents(raw, h.events)
	if _, err := io.WriteString(stdin, script); err != nil {
		h.teardown()
		return nil, fmt.Errorf("hold session on %s: %w: %v", t.Host, ErrTransport, err)
	}
	go h.keepalive(holdKeepalive)

	if err := h.drive(t, ev); err != nil {
		h.teardown()
		return nil, err
	}
	return h, nil
}

// lockEvent is one protocol line, with the holder record that followed it
// (for waiting and busy) already collected.
type lockEvent struct {
	State  string // noflock, waiting, acquired, busy, error…
	Holder Holder
}

// assembleLockEvents turns the session's raw stdout lines into events. A
// waiting or busy line is held back until its holder record has arrived: the
// record is printed by the same shell immediately after the line and before
// it blocks in flock, so the lines arrive together, and the lull only bounds
// the empty-record case, where there is nothing to wait for. Any other line
// — a login banner, the tool's own chatter — is ignored.
func assembleLockEvents(raw <-chan string, out chan<- lockEvent) {
	defer close(out)
	var pending *lockEvent
	var rec []string
	var lull <-chan time.Time
	flush := func() {
		if pending == nil {
			return
		}
		pending.Holder = parseHolderLines(rec)
		out <- *pending
		pending, rec, lull = nil, nil, nil
	}
	for {
		select {
		case line, ok := <-raw:
			if !ok {
				flush()
				return
			}
			line = strings.TrimSpace(line)
			if pending != nil && strings.HasPrefix(line, "holder.") {
				rec = append(rec, line)
				if strings.HasPrefix(line, "holder.since=") {
					// since= is the record's last key (LockPrelude pins the
					// order), so the record is complete: report it now
					// rather than after the lull.
					flush()
				}
				continue
			}
			k, v, isKV := strings.Cut(line, "=")
			if !isKV || k != "lock" {
				continue
			}
			flush()
			switch v {
			case "waiting", "busy":
				pending = &lockEvent{State: v}
				lull = time.After(holderLull)
			default:
				out <- lockEvent{State: v}
			}
		case <-lull:
			flush()
		}
	}
}

// parseHolderLines reads a collected record. A garbled one is not a reason
// to fail the hold; the waiter just cannot name who it waits on.
func parseHolderLines(rec []string) Holder {
	h, _, err := ParseHolder(strings.Join(rec, "\n"))
	if err != nil {
		return Holder{}
	}
	return h
}

// drive reads protocol events until the session reports an outcome.
func (h *Hold) drive(t Target, ev HoldEvents) error {
	logf := func(format string, args ...any) {
		if ev.Log != nil {
			fmt.Fprintf(ev.Log, format+"\n", args...)
		}
	}
	var waitingSince time.Time
	var hb <-chan time.Time
	for {
		select {
		case e, ok := <-h.events:
			if !ok {
				return h.endedEarly()
			}
			switch {
			case e.State == "noflock":
				return fmt.Errorf("%s is not installed on %s — the host lock needs it; run dross doctor: %w",
					LockTool, h.host, ErrLockTool)
			case e.State == "acquired":
				h.Outcome = Held
				return nil
			case e.State == "waiting":
				h.Other = e.Holder
				if h.Other.IsZero() {
					logf("waiting on the host lock on %s (holder not yet recorded)", h.host)
				} else {
					logf("%s", WaitLine(h.Other))
				}
				waitingSince = time.Now()
				if ev.HeartbeatEvery > 0 {
					tk := time.NewTicker(ev.HeartbeatEvery)
					defer tk.Stop()
					hb = tk.C
				}
			case e.State == "busy":
				if h.Other.IsZero() {
					h.Other = e.Holder
				}
				if !t.Lock.Wait.Forever && t.Lock.Wait.Max == 0 {
					return &BusyError{Host: h.host, Holder: h.Other}
				}
				h.Outcome = Alongside
				return nil
			case strings.HasPrefix(e.State, "error"):
				return fmt.Errorf("host lock on %s: %s: %w", h.host, e.State, ErrRemoteCommand)
			}
		case <-hb:
			who := "the host lock"
			if !h.Other.IsZero() {
				who = h.Other.Name()
			}
			logf("still waiting on %s for %s — %s elapsed", h.host, who,
				time.Since(waitingSince).Round(time.Second))
		}
	}
}

// endedEarly classifies a session that exited before reporting an outcome.
func (h *Hold) endedEarly() error {
	<-h.done
	if h.waitErr == nil {
		return fmt.Errorf("hold session on %s exited without answering: %w", h.host, ErrRemoteCommand)
	}
	var ee *exec.ExitError
	if errors.As(h.waitErr, &ee) {
		err := Classify("ssh", h.host, ee.ExitCode())
		h.printStderr()
		return err
	}
	return fmt.Errorf("hold session on %s: %w: %v", h.host, ErrTransport, h.waitErr)
}

// keepalive writes a newline into the session every holdKeepalive until
// stopped. A failed write means the session is gone; Lost reports that.
func (h *Hold) keepalive(every time.Duration) {
	tk := time.NewTicker(every)
	defer tk.Stop()
	for {
		select {
		case <-h.stopKA:
			return
		case <-h.done:
			return
		case <-tk.C:
			if _, err := io.WriteString(h.stdin, "\n"); err != nil {
				return
			}
		}
	}
}

func (h *Hold) stopKeepalive() { h.kaOnce.Do(func() { close(h.stopKA) }) }

// Lost reports whether the session ended for any reason other than Release.
// A caller holding a measurement must check it before recording: a lock
// that was lost mid-run means another leg may have been sharing the host.
func (h *Hold) Lost() bool {
	select {
	case <-h.done:
		return !h.released.Load()
	default:
		return false
	}
}

// Release ends the session: the keepalive stops, stdin closes, the host's
// read loop sees EOF and the shell exits — closing fd 9, which is the
// release. Then the local process is reaped. Idempotent.
//
// The order matters: closing stdin BEFORE Wait is what lets the shell end;
// a Wait first would block on a loop that is waiting for the very EOF this
// call has not yet delivered.
func (h *Hold) Release() error {
	if h.released.Swap(true) {
		return nil
	}
	lostBefore := false
	select {
	case <-h.done:
		lostBefore = true
	default:
	}
	h.stopKeepalive()
	h.stdin.Close()
	<-h.done
	if lostBefore {
		return fmt.Errorf("hold session on %s ended before release: %v", h.host, h.exitReason())
	}
	if h.waitErr != nil {
		return fmt.Errorf("hold session on %s: %v", h.host, h.exitReason())
	}
	return nil
}

func (h *Hold) exitReason() string {
	if h.waitErr == nil {
		return "exited 0"
	}
	h.printStderr()
	return h.waitErr.Error()
}

// printStderr puts what the session's ssh wrote to stderr where the user is
// looking. It is the transport's own output: it goes to the terminal, never
// into an error that can outlive the run.
func (h *Hold) printStderr() {
	if msg := strings.TrimSpace(h.stderr.String()); msg != "" {
		fmt.Fprintf(diagStderr, "hold session on %s: ssh said:\n%s\n", h.host, msg)
	}
}

// teardown ends a session that never became a Hold: stdin is closed so a
// shell sitting in the read loop exits, and one that does not is killed.
func (h *Hold) teardown() {
	h.released.Store(true)
	h.stopKeepalive()
	h.stdin.Close()
	select {
	case <-h.done:
	case <-time.After(2 * time.Second):
		h.cmd.Process.Kill()
		<-h.done
	}
}

// boundedBuffer keeps the first limit bytes written to it — enough of an
// ssh error to name the cause, never an unbounded capture of a chatty host.
type boundedBuffer struct {
	mu    sync.Mutex
	b     bytes.Buffer
	limit int
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if room := b.limit - b.b.Len(); room > 0 {
		if len(p) > room {
			b.b.Write(p[:room])
		} else {
			b.b.Write(p)
		}
	}
	return len(p), nil
}

func (b *boundedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.String()
}
