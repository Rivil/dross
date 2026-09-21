package remote

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// HostLockPath is the one file every dross on a host locks before it measures
// there. It is a system-wide path on purpose — not under any workdir — so a
// leg dispatched from repo A waits on a leg in flight from repo B, and a leg
// started by user A waits on one held by user B.
//
// /tmp is the only directory that is sticky and world-writable on every host
// dross reaches (/run/lock is root:lock on Fedora), which is what the cross-
// user requirement needs. The tmpfiles sweep that keeps run directories OUT of
// /tmp does not apply here: a lock file carries no result, and a swept lock is
// simply recreated by the next leg.
//
// A var rather than a const so the real-flock tests can point it at a temp
// file. The suite itself runs under the production lock on the remote gate,
// so a test that locked the production path would deadlock against the leg
// that is running it.
var HostLockPath = "/tmp/dross-host.lock"

// LockTool is the host program the lock needs. It is probed like any other
// host tool: a host without it is reported by doctor, and a mutation leg
// refuses to measure there rather than measuring unlocked.
const LockTool = "flock"

// lockBusyCode is what flock is told to exit with when the lock is held by
// someone else. 75 is EX_TEMPFAIL — chosen because it is not a code flock
// produces on its own (a usage error is 1, a missing file 1), so "busy" is
// never confused with "flock did not understand its arguments".
const lockBusyCode = 75

// Holder is who holds — or is waiting on — the host lock. The holder writes
// it into the lock file once the lock is held; waiters, `verify status` and
// doctor read it from there. One file is both the exclusion and the
// explanation, which is the locked holder_record decision.
type Holder struct {
	// Project is the project.toml name of the repo the leg was dispatched from.
	Project string
	// Phase is the phase under verification. Empty for a suite run or a
	// survivor drain, which have no phase.
	Phase string
	// RunID names the run: a detached run's id, or the id minted for an
	// attached leg. It is the one field a holder must have — a lock held by
	// nobody nameable is one a waiter cannot explain.
	RunID string
	// PID is the holding process on the host, as the host reported it.
	PID int
	// User is the unix user on the host that took the lock.
	User string
	// Since is when the lock was taken, on the host's clock.
	Since time.Time
}

// IsZero reports whether the holder carries no identity at all — the record
// was absent or empty. Distinguished from a filled holder so a busy lock whose
// holder has not yet written its record reads as "held, not yet named" rather
// than as held by a blank.
func (h Holder) IsZero() bool {
	return h.Project == "" && h.Phase == "" && h.RunID == "" && h.PID == 0 && h.User == "" && h.Since.IsZero()
}

// Name renders the holder the way every message names it:
// `<project>/<phase> run <run> (pid N)`, with the phase omitted when there is
// none. One renderer so a waiter's line, doctor's line and the status line
// agree on what a holder is called.
func (h Holder) Name() string {
	who := h.Project
	if h.Phase != "" {
		who += "/" + h.Phase
	}
	if who == "" {
		who = "(unnamed)"
	}
	return fmt.Sprintf("%s run %s (pid %d)", who, h.RunID, h.PID)
}

// validate refuses a holder whose identity would break the record's line
// protocol. The record is one field per line; a newline inside a value would
// end the line early and start a forged one, so it is refused before any
// text is built rather than quoted and hoped for.
func (h Holder) validate() error {
	for _, f := range []struct{ name, v string }{
		{"project", h.Project}, {"phase", h.Phase}, {"run id", h.RunID},
	} {
		if strings.ContainsAny(f.v, "\r\n") {
			return fmt.Errorf("lock holder %s %q contains a newline: %w", f.name, f.v, ErrUnsafeTarget)
		}
	}
	return nil
}

// WaitPolicy is how long an acquisition waits for a held lock.
//
// The zero value waits not at all (`flock -n`): the lock is taken if free and
// reported busy otherwise. Forever blocks until the holder releases — the
// policy of an attached mutation leg, per the locked attached_busy_policy.
// Max bounds the wait — the policy of a suite run, which waits a little for
// a leg about to finish and then runs alongside rather than holding a task
// gate for the length of a mutation leg.
type WaitPolicy struct {
	Forever bool
	Max     time.Duration
}

// Forever is the unbounded policy, named so a call site reads as what it
// means.
var Forever = WaitPolicy{Forever: true}

// LockSpec is everything the lock prelude needs: who is taking the lock and
// how long they will wait for it.
type LockSpec struct {
	Holder Holder
	Wait   WaitPolicy
}

// lockPath is the quoted lock path as every script spells it.
func lockPath() string { return shellQuote(HostLockPath) }

// LockPrelude renders the shell fragment that takes the host lock. It is the
// ONLY text that ever opens, creates, chmods or writes the lock path; every
// caller composes it rather than spelling any part of it again.
//
// The fragment speaks a small line protocol on stdout, which the hold session
// and the detached job's log both read:
//
//	lock=noflock            flock is not on the host; nothing was taken
//	lock=waiting            the lock is held; holder.<k>=<v> lines follow
//	lock=acquired           the lock is held by THIS shell on fd 9
//	lock=busy               a bounded or zero wait expired; holder lines follow
//	lock=error <why>        the lock could not be taken
//
// and leaves the outcome in the shell variable `__lock` for the caller's text
// to branch on. It never exits the shell itself: after `lock=busy` the
// caller's text runs with nothing held — that is the suite's run-alongside
// path, and a prelude that exited would turn it into a lost session.
//
// Why each line is shaped the way it is — every one of these is a property
// the cross-user requirement or the crash-safe release depends on:
//
//   - The create is guarded by `[ -e ]` and followed by `chmod 0666` on the
//     same line. The first leg on a host creates the file world-writable;
//     every later leg finds it and never tries.
//   - The lock is taken on a READ-ONLY open, `exec 9<PATH`. Linux's
//     fs.protected_regular refuses an O_CREAT open of another user's file in
//     a sticky world-writable directory, so any `>`-shaped open of the path
//     would fail for user B with EACCES. A read-only open is not O_CREAT and
//     succeeds for anyone; flock(2) does not care about the open mode.
//   - After acquisition the path is checked against fd 9 with `-ef`. If the
//     file was removed and recreated between the open and the lock, the lock
//     is on a dead inode and excludes nobody; the open is retried a bounded
//     number of times and then refused.
//   - The holder record is written through `dd conv=nocreat` fed by a pipe —
//     never a `>` redirection (O_CREAT again) and never a dd reading stdin,
//     which in the hold session is the keepalive stream and in the detached
//     job is /dev/null. A failed write is ignored: the exclusion is the flock
//     on fd 9, and it holds whether or not the record landed.
//   - Nothing releases by hand. There is no `flock -u`, no close of fd 9, no
//     rm. The lock lives exactly as long as the fd, which is exactly as long
//     as the process — a holder that dies releases the host with no cleanup
//     step and no stale state for anyone to clear.
func LockPrelude(spec LockSpec) (string, error) {
	if err := spec.Holder.validate(); err != nil {
		return "", err
	}
	if spec.Wait.Max < 0 {
		return "", fmt.Errorf("lock wait %v is negative: %w", spec.Wait.Max, ErrUnsafeTarget)
	}
	p := lockPath()
	h := spec.Holder

	var b strings.Builder
	b.WriteString("__lock=\n")
	b.WriteString("if ! command -v " + LockTool + " >/dev/null 2>&1; then __lock=noflock; printf 'lock=noflock\\n'; else\n")
	// Create guard and chmod on ONE line, so a reader can see they travel
	// together. The stderr redirect covers the race where two first legs both
	// pass the guard: the second's `: >` fails under protected_regular, and
	// its `&&` skips the chmod.
	b.WriteString("[ -e " + p + " ] || { : > " + p + " && chmod 0666 " + p + "; } 2>/dev/null\n")
	b.WriteString("__w=0; __i=0\n")
	b.WriteString("while [ -z \"$__lock\" ]; do\n")
	// A failed exec-redirection returns 1 without exiting a non-posix bash;
	// the guard makes that explicit rather than relying on it.
	b.WriteString("if ! exec 9<" + p + "; then __lock=error; printf 'lock=error open\\n'; break; fi\n")
	b.WriteString("flock -n -E 75 9; __r=$?\n")
	b.WriteString("if [ \"$__r\" -eq 75 ]; then\n")
	switch {
	case spec.Wait.Forever:
		b.WriteString(waitingLines(p))
		b.WriteString("flock -x -E 75 9; __r=$?\n")
		b.WriteString("if [ \"$__r\" -ne 0 ]; then __lock=error; printf 'lock=error flock %s\\n' \"$__r\"; break; fi\n")
	case spec.Wait.Max > 0:
		b.WriteString(waitingLines(p))
		secs := int64(spec.Wait.Max / time.Second)
		if secs < 1 {
			secs = 1
		}
		b.WriteString("flock -w " + strconv.FormatInt(secs, 10) + " -E 75 9; __r=$?\n")
		b.WriteString("if [ \"$__r\" -eq 75 ]; then " + busyLines(p) + "; break; fi\n")
		b.WriteString("if [ \"$__r\" -ne 0 ]; then __lock=error; printf 'lock=error flock %s\\n' \"$__r\"; break; fi\n")
	default:
		b.WriteString(busyLines(p) + "; break\n")
	}
	b.WriteString("elif [ \"$__r\" -ne 0 ]; then __lock=error; printf 'lock=error flock %s\\n' \"$__r\"; break; fi\n")
	// Held. Make sure it is the file at the path that is held, not an inode
	// someone unlinked out from under the open.
	b.WriteString("if [ " + p + " -ef /dev/fd/9 ]; then __lock=acquired; " +
		"elif [ \"$__i\" -lt 3 ]; then __i=$((__i + 1)); " +
		"else __lock=error; printf 'lock=error replaced\\n'; break; fi\n")
	b.WriteString("done\n")
	b.WriteString("if [ \"$__lock\" = acquired ]; then\n")
	b.WriteString("printf '%s\\n' project=" + shellQuote(h.Project) +
		" phase=" + shellQuote(h.Phase) +
		" run=" + shellQuote(h.RunID) +
		" pid=$$ user=\"$(id -un)\" since=\"$(date +%s)\"" +
		" | dd of=" + p + " conv=nocreat status=none 2>/dev/null\n")
	b.WriteString("printf 'lock=acquired\\n'\n")
	b.WriteString("fi\n")
	b.WriteString("fi\n")
	return b.String(), nil
}

// waitingLines announces the wait once — a re-open after an inode check
// must not announce it again — and names the holder.
func waitingLines(p string) string {
	return "if [ \"$__w\" -eq 0 ]; then __w=1; printf 'lock=waiting\\n'; " + holderLines(p) + "; fi\n"
}

// busyLines is the expired-wait report: the caller's text runs next with
// nothing held.
func busyLines(p string) string {
	return "__lock=busy; printf 'lock=busy\\n'; " + holderLines(p)
}

// holderLines emits the record as `holder.<k>=<v>` lines. awk rather than
// sed because awk always terminates its output with a newline, so a record
// that lost its trailing newline cannot glue itself onto the next protocol
// line. Bounded, because the file is world-writable and could hold anything.
func holderLines(p string) string {
	return "awk 'NR <= 16 { print \"holder.\" $0 }' " + p + " 2>/dev/null"
}

// LockStatusScript renders the fragment that reports the lock's state without
// taking it for longer than a probe: tool presence, then `lock=free` or
// `lock=busy` with the holder's record.
//
// It PROBES with `flock -n` rather than reading the record and trusting it.
// A record is stale the moment its writer dies — that is the whole design —
// so a status derived from the record alone would report a crashed run as
// still holding the host. The probe asks the kernel, which is the only thing
// that knows.
//
// An absent file — a host no leg has ever run on — is free, and is said to be
// before any open is attempted. The probe never creates the file: creation is
// the prelude's job, and a status read that left a file behind would be
// creating it with whatever mode the probing user's umask gave it.
func LockStatusScript() string {
	p := lockPath()
	var b strings.Builder
	b.WriteString("if ! command -v " + LockTool + " >/dev/null 2>&1; then printf 'tool=no\\n'; else\n")
	b.WriteString("printf 'tool=yes\\n'\n")
	b.WriteString("if [ -e " + p + " ]; then\n")
	b.WriteString("if exec 9<" + p + "; then\n")
	b.WriteString("flock -n -E 75 9; __r=$?\n")
	b.WriteString("if [ \"$__r\" -eq 0 ]; then printf 'lock=free\\n'; " +
		"elif [ \"$__r\" -eq 75 ]; then printf 'lock=busy\\n'; " + holderLines(p) + "; " +
		"else printf 'lock=error flock %s\\n' \"$__r\"; fi\n")
	b.WriteString("else printf 'lock=error open\\n'; fi\n")
	b.WriteString("else printf 'lock=free\\n'; fi\n")
	b.WriteString("fi\n")
	return b.String()
}

// LockStatus is what LockStatusScript reported.
type LockStatus struct {
	// ToolMissing means flock is not on the host. Nothing else is known: a
	// host that cannot lock cannot be asked whether it is locked.
	ToolMissing bool
	// Held is the kernel's answer: something holds the lock right now.
	Held bool
	// Holder is the record found beside a held lock. It may be zero when the
	// lock is held but the holder has not written its record yet; it is
	// ALWAYS zero when the lock is free, whatever stale record the file holds.
	Holder Holder
}

// ParseLockStatus reads LockStatusScript's output, or the tail of a
// StatusScript's output where the same lines appear.
//
// Unknown lines are ignored, as ParseStatus ignores them: a login shell's
// banner is not an error. Output with neither a tool= nor a lock= line IS an
// error — it means the probe never ran, and reporting that as "free" would
// be reporting the absence of an answer as an answer.
func ParseLockStatus(out string) (LockStatus, error) {
	var s LockStatus
	seen := false
	var record []string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		switch {
		case k == "tool":
			seen = true
			if v == "no" {
				s.ToolMissing = true
			}
		case k == "lock":
			seen = true
			switch {
			case v == "free":
				s.Held = false
			case v == "busy":
				s.Held = true
			case v == "noflock":
				s.ToolMissing = true
			case strings.HasPrefix(v, "error"):
				return LockStatus{}, fmt.Errorf("remote: host lock probe failed: %s", v)
			}
		case strings.HasPrefix(k, "holder."):
			record = append(record, line)
		}
	}
	if !seen {
		return LockStatus{}, fmt.Errorf("remote: no lock status lines in output")
	}
	if s.ToolMissing {
		return LockStatus{ToolMissing: true}, nil
	}
	if s.Held {
		h, _, err := ParseHolder(strings.Join(record, "\n"))
		if err != nil {
			return LockStatus{}, err
		}
		s.Holder = h
	}
	return s, nil
}

// ParseHolder reads a holder record: the lock file's own `k=v` lines, or the
// same lines carrying the `holder.` prefix the scripts add on the wire. ok is
// false for an empty record — a lock file that exists but has never been
// written, or a busy lock whose holder has not recorded itself yet.
//
// A non-empty record that names no run, or whose pid or since is not an
// integer, is an error rather than a partial holder: the file is world-
// writable, and a waiter that printed half a forged record as its reason
// would be repeating whatever was put there.
func ParseHolder(record string) (Holder, bool, error) {
	var h Holder
	seen := false
	for _, line := range strings.Split(record, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		line = strings.TrimPrefix(line, "holder.")
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		seen = true
		switch k {
		case "project":
			h.Project = v
		case "phase":
			h.Phase = v
		case "run":
			h.RunID = v
		case "user":
			h.User = v
		case "pid":
			n, err := strconv.Atoi(v)
			if err != nil {
				return Holder{}, false, fmt.Errorf("remote: unreadable holder pid %q: %w", v, err)
			}
			h.PID = n
		case "since":
			n, err := strconv.ParseInt(v, 10, 64)
			if err != nil {
				return Holder{}, false, fmt.Errorf("remote: unreadable holder since %q: %w", v, err)
			}
			h.Since = time.Unix(n, 0)
		}
	}
	if !seen {
		return Holder{}, false, nil
	}
	if h.RunID == "" {
		return Holder{}, false, fmt.Errorf("remote: holder record names no run")
	}
	return h, true, nil
}
