package remote

import (
	"bufio"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// holder is the identity most lock tests stamp; RunID is the one field a
// holder must carry.
func holder() Holder { return Holder{Project: "dross", Phase: "p", RunID: "r-1"} }

func prelude(t *testing.T, wait WaitPolicy) string {
	t.Helper()
	s, err := LockPrelude(LockSpec{Holder: holder(), Wait: wait})
	if err != nil {
		t.Fatalf("LockPrelude = %v", err)
	}
	return s
}

// useTempLock points HostLockPath at a file under a temp dir for the length
// of one test. The production path is NEVER locked by the suite: on the
// remote gate the suite itself runs under that lock, and a test that took it
// would wait on the leg that is running it.
func useTempLock(t *testing.T) string {
	t.Helper()
	old := HostLockPath
	HostLockPath = filepath.Join(t.TempDir(), "host.lock")
	t.Cleanup(func() { HostLockPath = old })
	return HostLockPath
}

// lockLines returns the lines of s that touch the lock path at all.
func lockLines(s string) []string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if strings.Contains(l, HostLockPath) {
			out = append(out, l)
		}
	}
	return out
}

// --- the fragment's shape ---------------------------------------------------

// TestLockPreludeParsesAsBash is the cheapest possible check that the text is
// a script at all — every other test here reads it as a string.
func TestLockPreludeParsesAsBash(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("no bash on PATH")
	}
	for name, wait := range map[string]WaitPolicy{"forever": Forever, "bounded": {Max: 10 * time.Minute}, "zero": {}} {
		cmd := exec.Command(bash, "-n")
		cmd.Stdin = strings.NewReader(prelude(t, wait))
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Errorf("%s prelude does not parse: %v\n%s", name, err, out)
		}
	}
	cmd := exec.Command(bash, "-n")
	cmd.Stdin = strings.NewReader(LockStatusScript())
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Errorf("status script does not parse: %v\n%s", err, out)
	}
}

// TestLockPreludeOpensWithoutCreate is the cross-user property (c-7) at the
// text level. Linux's fs.protected_regular refuses an O_CREAT open of
// another user's file in a sticky world-writable directory, so a `flock
// <path>` (which opens O_CREAT) or any `>`-shaped redirection onto the path
// fails for user B with EACCES. The only open that works for everyone is a
// read-only one — and flock(2) does not care about the open mode.
func TestLockPreludeOpensWithoutCreate(t *testing.T) {
	p := shellQuote(HostLockPath)
	for name, wait := range map[string]WaitPolicy{"forever": Forever, "bounded": {Max: time.Minute}, "zero": {}} {
		s := prelude(t, wait)
		if strings.Contains(s, "flock "+p) || strings.Contains(s, "flock -n "+p) || strings.Contains(s, "flock -x "+p) {
			t.Errorf("%s: flock is given the PATH (an O_CREAT open):\n%s", name, s)
		}
		if !strings.Contains(s, "exec 9<"+p) {
			t.Errorf("%s: the lock is not taken on a read-only fd 9 open:\n%s", name, s)
		}
		for _, l := range lockLines(s) {
			if strings.HasPrefix(strings.TrimSpace(l), "[ -e "+p+" ] ||") {
				continue // the create guard is the one permitted create
			}
			for _, bad := range []string{"> " + p, ">> " + p, "<> " + p, ">" + p, ">>" + p, "<>" + p} {
				if strings.Contains(l, bad) {
					t.Errorf("%s: the lock path is opened with O_CREAT outside the create guard:\n%s", name, l)
				}
			}
		}
	}
}

// TestLockPreludeCreatesWorldWritable pins the guarded create: the first leg
// on a host creates the file for everyone, and every later leg finds it.
func TestLockPreludeCreatesWorldWritable(t *testing.T) {
	p := shellQuote(HostLockPath)
	s := prelude(t, Forever)
	var create string
	for _, l := range strings.Split(s, "\n") {
		if strings.Contains(l, "[ -e "+p+" ] ||") {
			create = l
		}
	}
	if create == "" {
		t.Fatalf("no `[ -e PATH ] ||` create guard:\n%s", s)
	}
	if !strings.Contains(create, "chmod 0666 "+p) {
		t.Errorf("the create is not followed on the same line by chmod 0666:\n%s", create)
	}
	if strings.Index(create, ": > "+p) > strings.Index(create, "chmod 0666") {
		t.Errorf("the chmod precedes the create:\n%s", create)
	}
}

// TestLockPreludeWritesRecordWithoutCreate is the holder-record write.
// `printf > PATH` would be O_CREAT (refused for user B, see above); a dd
// reading stdin would eat the hold session's keepalive stream — the shell's
// stdin IS that stream — and hang; and a record write reachable on the
// expired path of a bounded wait would overwrite the real holder's record
// with the identity of a process that holds nothing.
func TestLockPreludeWritesRecordWithoutCreate(t *testing.T) {
	p := shellQuote(HostLockPath)
	for name, wait := range map[string]WaitPolicy{"forever": Forever, "bounded": {Max: time.Minute}, "zero": {}} {
		s := prelude(t, wait)
		var write string
		for _, l := range strings.Split(s, "\n") {
			if strings.Contains(l, "dd of="+p) {
				write = l
			}
		}
		if write == "" {
			t.Fatalf("%s: no dd write of the record:\n%s", name, s)
		}
		if !strings.Contains(write, "| dd of="+p+" conv=nocreat status=none") {
			t.Errorf("%s: the record is not piped into `dd of=PATH conv=nocreat status=none`:\n%s", name, write)
		}
		if strings.Contains(write, "if=") {
			t.Errorf("%s: dd carries an if= (it must read the pipe):\n%s", name, write)
		}
		if strings.Contains(s, "printf") && regexp.MustCompile(`printf[^\n]*> `+regexp.QuoteMeta(p)).MatchString(s) {
			t.Errorf("%s: the record is written with a > redirection:\n%s", name, s)
		}
		// The write is guarded by the acquired outcome, so an expired wait
		// (busy) never reaches it.
		if !strings.Contains(s, "if [ \"$__lock\" = acquired ]; then\n"+write) {
			t.Errorf("%s: the record write is not guarded by the acquired outcome:\n%s", name, s)
		}
	}
}

// TestLockPreludeBusyDoesNotExit: after `lock=busy` under a bounded policy
// the caller's text runs with nothing held. A prelude that exited would make
// the hold session's Release see a dead process and report a lost session
// for what is the suite's ordinary run-alongside path.
func TestLockPreludeBusyDoesNotExit(t *testing.T) {
	for name, wait := range map[string]WaitPolicy{"forever": Forever, "bounded": {Max: time.Minute}, "zero": {}} {
		s := prelude(t, wait)
		if regexp.MustCompile(`(^|[;\s])exit\b`).MatchString(s) {
			t.Errorf("%s: the prelude exits the shell:\n%s", name, s)
		}
	}
	s := prelude(t, WaitPolicy{Max: time.Minute})
	if !strings.Contains(s, "__lock=busy; printf 'lock=busy\\n'") {
		t.Errorf("bounded prelude does not report busy and leave the outcome in __lock:\n%s", s)
	}
}

// TestLockPreludeWaitForms pins the three flock forms against the three
// policies, and that busy is read from the exit code flock was TOLD to use.
// A usage error is 1; reading anything but 75 as busy would make a flock
// that did not understand its arguments look like a held host.
func TestLockPreludeWaitForms(t *testing.T) {
	cases := []struct {
		name string
		wait WaitPolicy
		want string
		not  []string
	}{
		{"forever", Forever, "flock -x -E 75 9", []string{"flock -w"}},
		{"bounded", WaitPolicy{Max: 10 * time.Minute}, "flock -w 600 -E 75 9", []string{"flock -x"}},
		{"zero", WaitPolicy{}, "flock -n -E 75 9", []string{"flock -w", "flock -x"}},
	}
	for _, c := range cases {
		s := prelude(t, c.wait)
		if !strings.Contains(s, c.want) {
			t.Errorf("%s: missing %q:\n%s", c.name, c.want, s)
		}
		for _, n := range c.not {
			if strings.Contains(s, n) {
				t.Errorf("%s: carries %q:\n%s", c.name, n, s)
			}
		}
		if !strings.Contains(s, `-eq 75 ]`) {
			t.Errorf("%s: busy is not detected by `$? -eq 75`:\n%s", c.name, s)
		}
		if strings.Contains(s, "-ne 0 ]; then __lock=busy") {
			t.Errorf("%s: a non-zero flock is read as busy:\n%s", c.name, s)
		}
	}
	if _, err := LockPrelude(LockSpec{Holder: holder(), Wait: WaitPolicy{Max: -time.Second}}); !errors.Is(err, ErrUnsafeTarget) {
		t.Errorf("a negative wait was accepted: %v", err)
	}
}

// TestLockPreludeChecksTheInode: a lock on an inode someone unlinked out
// from under the open excludes nobody. The check is bounded so a host where
// the file keeps vanishing refuses rather than spinning.
func TestLockPreludeChecksTheInode(t *testing.T) {
	p := shellQuote(HostLockPath)
	s := prelude(t, Forever)
	if !strings.Contains(s, "[ "+p+" -ef /dev/fd/9 ]") {
		t.Errorf("no inode check of the path against fd 9:\n%s", s)
	}
	if !strings.Contains(s, `[ "$__i" -lt 3 ]`) {
		t.Errorf("the re-open loop is not bounded at 3:\n%s", s)
	}
	if !strings.Contains(s, "printf 'lock=error replaced\\n'") {
		t.Errorf("an exhausted re-open does not report `lock=error replaced`:\n%s", s)
	}
}

// TestLockPreludeGatesOnFlock: a host without flock must be reported before
// any part of the lock protocol runs there, and reported as a fact rather
// than a shell death — the caller decides what a missing tool means.
func TestLockPreludeGatesOnFlock(t *testing.T) {
	s := prelude(t, Forever)
	gate := strings.Index(s, "command -v flock")
	if gate < 0 {
		t.Fatalf("no `command -v flock` gate:\n%s", s)
	}
	first := strings.Index(s, HostLockPath)
	if first >= 0 && first < gate {
		t.Errorf("the lock path is touched before the flock gate:\n%s", s)
	}
	if !strings.Contains(s, "printf 'lock=noflock\\n'") {
		t.Errorf("a missing flock is not reported as lock=noflock:\n%s", s)
	}
	if strings.Contains(s, "|| exit") {
		t.Errorf("the gate exits the shell:\n%s", s)
	}
}

// TestLockPreludeNeverReleasesByHand is c-4 at the text level: the lock's
// lifetime is fd 9's, and fd 9's is the process's. Any explicit release is a
// second way to release — one that runs only on the paths someone wrote it
// on, which is never the path where the holder died.
func TestLockPreludeNeverReleasesByHand(t *testing.T) {
	p := shellQuote(HostLockPath)
	for name, wait := range map[string]WaitPolicy{"forever": Forever, "bounded": {Max: time.Minute}, "zero": {}} {
		s := prelude(t, wait)
		for _, bad := range []string{"rm " + p, "rm -f " + p, "unlink " + p, "truncate -s0 " + p, "truncate -s 0 " + p, "flock -u", "9<&-", "9>&-"} {
			if strings.Contains(s, bad) {
				t.Errorf("%s: the prelude releases by hand with %q:\n%s", name, bad, s)
			}
		}
	}
}

// TestLockPreludeQuotesTheHolder pins the record's exact lines and that every
// literal crosses through shellQuote. The record is read by every waiter on
// the host, so its key order is a contract; the quoting is what keeps a
// project named `a'b` from ending the word early.
func TestLockPreludeQuotesTheHolder(t *testing.T) {
	s, err := LockPrelude(LockSpec{Holder: Holder{Project: "a'b", Phase: "ph", RunID: "r-9"}, Wait: Forever})
	if err != nil {
		t.Fatalf("LockPrelude = %v", err)
	}
	want := `printf '%s\n' project='a'\''b' phase='ph' run='r-9' pid=$$ user="$(id -un)" since="$(date +%s)" | dd of=`
	if !strings.Contains(s, want) {
		t.Errorf("record lines are not the pinned form:\nwant %s\n%s", want, s)
	}
	// An empty phase (a suite run, a drain) still emits its line, so the key
	// order is stable for every reader.
	s2 := prelude(t, Forever)
	if !strings.Contains(s2, " phase='p' ") {
		t.Errorf("phase line missing:\n%s", s2)
	}
	s3, _ := LockPrelude(LockSpec{Holder: Holder{Project: "dross", RunID: "t-1"}, Wait: Forever})
	if !strings.Contains(s3, " phase='' ") {
		t.Errorf("an empty phase drops its line:\n%s", s3)
	}
}

// TestHolderNewlineRefused: the record is one field per line, so a newline
// inside a value ends the line early and starts a forged one. Refused before
// any text exists — and through Target.Validate, so no builder can be
// reached with one.
func TestHolderNewlineRefused(t *testing.T) {
	for _, h := range []Holder{
		{Project: "a\nb", RunID: "r"},
		{Phase: "a\rb", RunID: "r"},
		{RunID: "r\nproject=x"},
	} {
		if _, err := LockPrelude(LockSpec{Holder: h, Wait: Forever}); !errors.Is(err, ErrUnsafeTarget) {
			t.Errorf("holder %+v: LockPrelude = %v, want ErrUnsafeTarget", h, err)
		}
		tt := target()
		tt.Lock.Holder = h
		if err := tt.Validate(); !errors.Is(err, ErrUnsafeTarget) {
			t.Errorf("holder %+v: Validate = %v, want ErrUnsafeTarget", h, err)
		}
	}
	tt := target()
	tt.Lock.Holder = holder()
	if err := tt.Validate(); err != nil {
		t.Errorf("a plain holder was refused: %v", err)
	}
}

// TestLockPathIgnoresWorkdir is c-2: the exclusion spans projects and
// workdirs, so the lock's location must not depend on either. Two targets in
// different workdirs open and write the SAME file.
func TestLockPathIgnoresWorkdir(t *testing.T) {
	a := Target{Host: host, Workdir: "/srv/a", Lock: LockSpec{Holder: Holder{Project: "alpha", RunID: "r-a"}, Wait: Forever}}
	b := Target{Host: host, Workdir: "/srv/b", Lock: LockSpec{Holder: Holder{Project: "beta", RunID: "r-b"}, Wait: Forever}}
	sa, err := LockPrelude(a.Lock)
	if err != nil {
		t.Fatal(err)
	}
	sb, err := LockPrelude(b.Lock)
	if err != nil {
		t.Fatal(err)
	}
	pick := func(s, marker string) string {
		for _, l := range strings.Split(s, "\n") {
			if strings.Contains(l, marker) {
				return l
			}
		}
		return ""
	}
	if oa, ob := pick(sa, "exec 9<"), pick(sb, "exec 9<"); oa == "" || oa != ob {
		t.Errorf("lock-open lines differ by workdir:\n%s\n%s", oa, ob)
	}
	wa, wb := pick(sa, "dd of="), pick(sb, "dd of=")
	if wa == "" || wa[strings.Index(wa, "| dd"):] != wb[strings.Index(wb, "| dd"):] {
		t.Errorf("record-write targets differ by workdir:\n%s\n%s", wa, wb)
	}
	for _, s := range []string{sa, sb} {
		if strings.Contains(s, "/srv/a") || strings.Contains(s, "/srv/b") {
			t.Errorf("the workdir leaked into the lock fragment:\n%s", s)
		}
	}
}

// TestLockPreludeHasNoCd: the lock is taken before the workdir is synced and
// may be taken for a workdir that does not exist yet.
func TestLockPreludeHasNoCd(t *testing.T) {
	s := prelude(t, Forever)
	if strings.Contains(s, "cd ") || strings.Contains(s, "export ") {
		t.Errorf("the prelude carries a cd or an export:\n%s", s)
	}
}

// --- the status script -------------------------------------------------------

// TestLockStatusScriptAbsentFileIsFree: a host no leg has ever run on has no
// lock file. `exec 9<` on a missing file kills a posix shell; the guard says
// free before any open is attempted.
func TestLockStatusScriptAbsentFileIsFree(t *testing.T) {
	p := shellQuote(HostLockPath)
	s := LockStatusScript()
	guard := strings.Index(s, "[ -e "+p+" ]")
	open := strings.Index(s, "exec 9<"+p)
	if guard < 0 || open < 0 || guard > open {
		t.Errorf("the open is not guarded by `[ -e PATH ]`:\n%s", s)
	}
	if !strings.Contains(s, "else printf 'lock=free\\n'; fi") {
		t.Errorf("an absent file is not reported free:\n%s", s)
	}
	st, err := ParseLockStatus("tool=yes\nlock=free\n")
	if err != nil {
		t.Fatalf("ParseLockStatus = %v", err)
	}
	if st.Held || !st.Holder.IsZero() || st.ToolMissing {
		t.Errorf("free lock parsed as %+v", st)
	}
}

// TestLockStatusScriptProbesNeverReads: the record is stale the moment its
// writer dies, so a status that trusted it would report a crashed run as
// holding the host. Only the kernel knows; ask it.
func TestLockStatusScriptProbesNeverReads(t *testing.T) {
	p := shellQuote(HostLockPath)
	s := LockStatusScript()
	for _, want := range []string{"command -v flock", "flock -n -E 75 9", "printf 'lock=free\\n'", "printf 'lock=busy\\n'", "printf 'tool=no\\n'", "printf 'tool=yes\\n'"} {
		if !strings.Contains(s, want) {
			t.Errorf("status script lacks %q:\n%s", want, s)
		}
	}
	if !strings.Contains(s, "-eq 75 ]; then printf 'lock=busy\\n'; "+holderLines(p)) {
		t.Errorf("holder lines are emitted other than on 75:\n%s", s)
	}
	for _, bad := range []string{"chmod", ": > " + p, "> " + p, "cat " + p} {
		if strings.Contains(s, bad) {
			t.Errorf("status script carries %q:\n%s", bad, s)
		}
	}
}

// --- parsers ---------------------------------------------------------------

func TestParseHolder(t *testing.T) {
	if h, ok, err := ParseHolder(""); err != nil || ok || !h.IsZero() {
		t.Errorf("ParseHolder(\"\") = %+v, %v, %v; want zero, false, nil", h, ok, err)
	}
	if _, _, err := ParseHolder("project=dross\nphase=p\npid=7\n"); err == nil {
		t.Error("a record with no run= was accepted")
	}
	if _, _, err := ParseHolder("run=r-1\npid=seven\n"); err == nil {
		t.Error("a non-integer pid was accepted")
	}
	if _, _, err := ParseHolder("run=r-1\nsince=abc\n"); err == nil {
		t.Error("a non-integer since was accepted")
	}
	for _, rec := range []string{
		"project=dross\nphase=p\nrun=r-1\npid=7\nuser=u\nsince=1700000000\n",
		"holder.project=dross\nholder.phase=p\nholder.run=r-1\nholder.pid=7\nholder.user=u\nholder.since=1700000000\n",
	} {
		h, ok, err := ParseHolder(rec)
		if err != nil || !ok {
			t.Fatalf("ParseHolder = %+v, %v, %v", h, ok, err)
		}
		want := Holder{Project: "dross", Phase: "p", RunID: "r-1", PID: 7, User: "u", Since: time.Unix(1700000000, 0)}
		if h != want {
			t.Errorf("ParseHolder = %+v, want %+v", h, want)
		}
		if got := h.Since.UTC().Format(time.RFC3339); got != "2023-11-14T22:13:20Z" {
			t.Errorf("Since renders as %s", got)
		}
	}
	if got := (Holder{Project: "dross", Phase: "p", RunID: "r-1", PID: 7}).Name(); got != "dross/p run r-1 (pid 7)" {
		t.Errorf("Name = %q", got)
	}
	if got := (Holder{Project: "dross", RunID: "t-1", PID: 7}).Name(); got != "dross run t-1 (pid 7)" {
		t.Errorf("Name without phase = %q", got)
	}
}

func TestParseLockStatus(t *testing.T) {
	st, err := ParseLockStatus("tool=no\n")
	if err != nil || !st.ToolMissing {
		t.Errorf("tool=no parsed as %+v, %v", st, err)
	}
	// A stale record beside a FREE lock is exactly what a crashed holder
	// leaves; it must not become a holder.
	st, err = ParseLockStatus("tool=yes\nlock=free\nholder.run=r-old\nholder.pid=1\n")
	if err != nil || st.Held || !st.Holder.IsZero() {
		t.Errorf("free lock with a stale record parsed as %+v, %v", st, err)
	}
	st, err = ParseLockStatus("banner\ntool=yes\nlock=busy\nholder.project=dross\nholder.phase=p\nholder.run=r-1\nholder.pid=7\nholder.user=u\nholder.since=1700000000\n")
	if err != nil || !st.Held {
		t.Fatalf("busy lock parsed as %+v, %v", st, err)
	}
	if want := (Holder{Project: "dross", Phase: "p", RunID: "r-1", PID: 7, User: "u", Since: time.Unix(1700000000, 0)}); st.Holder != want {
		t.Errorf("holder = %+v, want %+v", st.Holder, want)
	}
	st, err = ParseLockStatus("tool=yes\nlock=busy\n")
	if err != nil || !st.Held || !st.Holder.IsZero() {
		t.Errorf("busy with an empty record parsed as %+v, %v — must be Held with a zero Holder", st, err)
	}
	if _, err := ParseLockStatus("nothing here\n"); err == nil {
		t.Error("output with no tool=/lock= line was accepted")
	}
	if _, err := ParseLockStatus("tool=yes\nlock=busy\nholder.run=r\nholder.since=abc\n"); err == nil || !strings.Contains(err.Error(), "since") {
		t.Errorf("a bad since did not fail naming the key: %v", err)
	}
	if _, err := ParseLockStatus("tool=yes\nlock=error replaced\n"); err == nil {
		t.Error("lock=error was accepted")
	}
}

// --- against a real flock ----------------------------------------------------

// needFlock skips where the real thing is absent (macOS has no flock(1)); the
// remote gate runs on a host that has it.
func needFlock(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("flock"); err != nil {
		t.Skip("flock not on PATH")
	}
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash not on PATH")
	}
	return bash
}

// lockChild runs the real fragment in a `bash -s` child whose stdin stays
// open (the hold session's shape) and streams its stdout lines.
type lockChild struct {
	cmd   *exec.Cmd
	stdin io.WriteCloser
	lines chan string
}

func startLockChild(t *testing.T, bash, script string) *lockChild {
	t.Helper()
	cmd := exec.Command(bash, "-s")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	c := &lockChild{cmd: cmd, stdin: stdin, lines: make(chan string, 64)}
	go func() {
		sc := bufio.NewScanner(stdout)
		for sc.Scan() {
			c.lines <- sc.Text()
		}
		close(c.lines)
	}()
	// The script, then a read loop that holds the shell — and fd 9 — open
	// for as long as stdin is.
	if _, err := io.WriteString(stdin, script+"while read -r _; do :; done\n"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		stdin.Close()
		cmd.Process.Kill()
		cmd.Wait()
	})
	return c
}

// expect waits for a line with the given prefix, failing on timeout or EOF.
func (c *lockChild) expect(t *testing.T, prefix string, within time.Duration) string {
	t.Helper()
	deadline := time.After(within)
	for {
		select {
		case l, ok := <-c.lines:
			if !ok {
				t.Fatalf("child exited before printing %q", prefix)
			}
			if strings.HasPrefix(l, prefix) {
				return l
			}
		case <-deadline:
			t.Fatalf("no %q line within %v", prefix, within)
		}
	}
}

// TestRealFlockSerializesAndReleasesOnKill is c-1 and c-4 against the kernel.
// Two shells run the real fragment; the second reports waiting while the
// first is up, and acquires within a second of the first being SIGKILLed —
// with no cleanup by anyone.
func TestRealFlockSerializesAndReleasesOnKill(t *testing.T) {
	bash := needFlock(t)
	path := useTempLock(t)
	first := startLockChild(t, bash, prelude(t, Forever))
	first.expect(t, "lock=acquired", 5*time.Second)
	rec, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	h, ok, err := ParseHolder(string(rec))
	if err != nil || !ok || h.RunID != "r-1" || h.PID != first.cmd.Process.Pid {
		t.Fatalf("record after acquire = %+v, %v, %v (pid %d):\n%s", h, ok, err, first.cmd.Process.Pid, rec)
	}
	s2, err := LockPrelude(LockSpec{Holder: Holder{Project: "other", Phase: "q", RunID: "r-2"}, Wait: Forever})
	if err != nil {
		t.Fatal(err)
	}
	second := startLockChild(t, bash, s2)
	second.expect(t, "lock=waiting", 5*time.Second)
	if got := second.expect(t, "holder.run=", 2*time.Second); got != "holder.run=r-1" {
		t.Errorf("the waiter names %q, want the first holder", got)
	}
	select {
	case l := <-second.lines:
		if strings.HasPrefix(l, "lock=") {
			t.Fatalf("the second shell printed %q while the first held the lock", l)
		}
	case <-time.After(300 * time.Millisecond):
	}
	if err := first.cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	first.cmd.Wait()
	second.expect(t, "lock=acquired", time.Second)
	rec, _ = os.ReadFile(path)
	if h, _, _ := ParseHolder(string(rec)); h.RunID != "r-2" {
		t.Errorf("the second holder did not record itself:\n%s", rec)
	}
}

// TestFailedRecordWriteStillAcquires: the exclusion is the flock on the
// read-only fd; the record is a courtesy. A file whose creator died before
// its chmod (0644, another user's) refuses user B's write — B still holds
// the lock and says so, and prints no holder lines for a record it did not
// write.
func TestFailedRecordWriteStillAcquires(t *testing.T) {
	bash := needFlock(t)
	path := useTempLock(t)
	if err := os.WriteFile(path, []byte("run=stale\n"), 0o444); err != nil {
		t.Fatal(err)
	}
	c := startLockChild(t, bash, prelude(t, Forever))
	got := c.expect(t, "lock=", 5*time.Second)
	if got != "lock=acquired" {
		t.Fatalf("first lock line = %q, want lock=acquired", got)
	}
	select {
	case l := <-c.lines:
		t.Errorf("unexpected line after acquire: %q", l)
	case <-time.After(200 * time.Millisecond):
	}
	if os.Getuid() != 0 {
		rec, _ := os.ReadFile(path)
		if string(rec) != "run=stale\n" {
			t.Errorf("a read-only record was rewritten:\n%s", rec)
		}
	}
}

// TestRealBoundedWaitRunsAlongside: an expired bounded wait reports busy,
// names the holder and leaves the shell running for the caller's text.
func TestRealBoundedWaitRunsAlongside(t *testing.T) {
	bash := needFlock(t)
	useTempLock(t)
	first := startLockChild(t, bash, prelude(t, Forever))
	first.expect(t, "lock=acquired", 5*time.Second)
	s2, _ := LockPrelude(LockSpec{Holder: Holder{Project: "other", RunID: "t-2"}, Wait: WaitPolicy{Max: time.Second}})
	second := startLockChild(t, bash, s2+"printf 'after=%s\\n' \"$__lock\"\n")
	second.expect(t, "lock=waiting", 5*time.Second)
	second.expect(t, "lock=busy", 5*time.Second)
	if got := second.expect(t, "after=", 2*time.Second); got != "after=busy" {
		t.Errorf("the caller's text saw %q", got)
	}
	// And the zero policy never waits.
	s3, _ := LockPrelude(LockSpec{Holder: Holder{Project: "other", RunID: "t-3"}})
	third := startLockChild(t, bash, s3+"printf 'after=%s\\n' \"$__lock\"\n")
	if got := third.expect(t, "lock=", time.Second); got != "lock=busy" {
		t.Errorf("zero policy first lock line = %q", got)
	}
	third.expect(t, "after=busy", time.Second)
}
