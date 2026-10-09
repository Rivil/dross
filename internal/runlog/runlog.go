// Package runlog is the one sink dross routes a spawned tool's full output
// through: a per-run log file under .dross/logs/, kept out of git by a
// self-ignoring .gitignore, with an optional echo to the human's terminal.
//
// The file is created lazily on the first Write, so a run that is refused
// before anything spawns leaves no log behind and does not churn the window of
// kept logs. A log that cannot be opened or written never fails the run: Write
// always reports success, the bytes go to the chosen terminal instead, one
// warning says so, and Err names the first failure.
//
// The terminal is an enum, never an io.Writer: a Log's only outputs are its
// gitignored file and os.Stdout or os.Stderr, so nothing can be laundered
// through it. The exec-taint gate relies on exactly that.
package runlog

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"sync"
	"time"

	"golang.org/x/term"

	"github.com/Rivil/dross/internal/pathfence"
)

// Dir is where run logs live, relative to the project root. It carries its
// own `*` .gitignore (the locked log_location decision): an unignored log would
// count as a tree change and stale both verify and the green record.
const Dir = ".dross/logs"

// Keep is how many logs the directory holds after a write prunes it (the
// locked log_retention decision).
const Keep = 20

// IgnoreContent is what .dross/logs/.gitignore holds. Anything else there is
// rewritten before a log is created.
const IgnoreContent = "# machine-local dross run logs — never committed\n*\n"

// Kind is which command a log belongs to.
type Kind int

const (
	// KindTest names a `dross test` run: test-<ts>.log.
	KindTest Kind = iota
	// KindVerify names a mutation leg: verify-<phase>-<leg>-<ts>.log.
	KindVerify
)

// Name says which run a log belongs to. Phase and Leg are used only by
// KindVerify, and each must be a single path segment.
type Name struct {
	Kind  Kind
	Phase string
	Leg   string
}

// Terminal is the stream a Log echoes to, and falls back to when its file is
// unavailable.
type Terminal int

const (
	// Stdout is os.Stdout.
	Stdout Terminal = iota
	// Stderr is os.Stderr.
	Stderr
)

// Options are a Log's terminal behaviour.
type Options struct {
	// Terminal is where Echo copies bytes, and where they go when the file is
	// unavailable.
	Terminal Terminal
	// Echo copies every byte to Terminal as well as the file — set it when
	// that terminal is a TTY, so the human keeps live output.
	Echo bool
}

// Log is one run's log. The zero value is not usable; get one from Open.
type Log struct {
	root string
	name Name
	opts Options

	mu     sync.Mutex
	f      io.WriteCloser
	path   string
	err    error
	opened bool // the open was attempted, whatever its outcome
	failed bool // the file is unavailable; bytes go to the terminal
	warned bool // the fallback warning was printed
}

// Seams. Tests swap them; production never does.
var (
	now = time.Now
	// wrapFile wraps the freshly created log file, so a test can make it fail
	// part-way through.
	wrapFile = func(f *os.File) io.WriteCloser { return f }
	// created is told each path runlog creates, in order.
	created = func(string) {}
	// removeFile is the prune's delete.
	removeFile = os.Remove
	// terminal resolves a Terminal at write time, so a caller that swapped
	// os.Stdout sees the bytes where fmt.Println would put them.
	terminal = func(t Terminal) io.Writer {
		if t == Stderr {
			return os.Stderr
		}
		return os.Stdout
	}
	// warnOut is where the one fallback warning goes.
	warnOut = func() io.Writer { return os.Stderr }
	// isTerminal is the TTY probe.
	isTerminal = func(fd uintptr) bool { return term.IsTerminal(int(fd)) }
)

// StdoutIsTerminal reports whether os.Stdout is a terminal right now: a human
// shell, as against an agent's Bash tool or a pipe.
func StdoutIsTerminal() bool { return isTerminal(os.Stdout.Fd()) }

// Open returns the log for one run under root. Nothing touches the disk until
// the first Write.
func Open(root string, name Name, opts Options) *Log {
	return &Log{root: root, name: name, opts: opts}
}

// Write sends p to the log file, and to the terminal when echoing or when the
// file is unavailable. It always returns len(p), nil: a log failure must never
// stop the copy loop of the child process feeding it.
func (l *Log) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.opened {
		l.opened = true
		if err := l.open(); err != nil {
			l.fail(err)
		}
	}
	logged := false
	if l.f != nil {
		if _, err := l.f.Write(p); err != nil {
			l.fail(fmt.Errorf("write %s: %w", l.rel(), err))
		} else {
			logged = true
		}
	}
	if l.opts.Echo || !logged {
		l.toTerminal(p)
	}
	return len(p), nil
}

// Path is the log file's absolute path, or "" before the first Write and
// whenever the file is unavailable.
func (l *Log) Path() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.failed {
		return ""
	}
	return l.path
}

// Err is the first failure the log met, or nil.
func (l *Log) Err() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.err
}

// Close closes the file, if one was created. Path still names it afterwards;
// a later Write goes to the terminal.
func (l *Log) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.f == nil {
		return nil
	}
	err := l.f.Close()
	l.f = nil
	return err
}

// fail records err (the first one wins), marks the file unavailable and prints
// the one warning.
func (l *Log) fail(err error) {
	if l.err == nil {
		l.err = err
	}
	if l.f != nil {
		l.f.Close()
		l.f = nil
	}
	l.failed = true
	if !l.warned {
		l.warned = true
		fmt.Fprintf(warnOut(), "warning: dross could not keep this run's log (%v); its output goes to the terminal instead\n", err)
	}
}

func (l *Log) toTerminal(p []byte) {
	terminal(l.opts.Terminal).Write(p) // a closed terminal has no one left to tell
}

func (l *Log) rel() string {
	if l.path == "" {
		return Dir
	}
	return Dir + "/" + filepath.Base(l.path)
}

// base is the log's file name before any collision suffix, without ".log".
func (l *Log) base() (string, error) {
	ts := now().UTC().Format("20060102T150405.000000000Z")
	switch l.name.Kind {
	case KindTest:
		return "test-" + ts, nil
	case KindVerify:
		if err := pathfence.Segment("log phase", l.name.Phase); err != nil {
			return "", err
		}
		if err := pathfence.Segment("log leg", l.name.Leg); err != nil {
			return "", err
		}
		return "verify-" + l.name.Phase + "-" + l.name.Leg + "-" + ts, nil
	}
	return "", fmt.Errorf("unknown log kind %d", l.name.Kind)
}

// maxCollisions bounds the -N suffix search for one instant.
const maxCollisions = 1000

// open makes the directory, its ignore file and then the log, and prunes.
func (l *Log) open() error {
	base, err := l.base()
	if err != nil {
		return err
	}
	dir := filepath.Join(l.root, filepath.FromSlash(Dir))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", Dir, err)
	}
	if err := ensureIgnored(dir); err != nil {
		return err
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return fmt.Errorf("resolve %s: %w", Dir, err)
	}
	for i := 0; i < maxCollisions; i++ {
		file := base + ".log"
		if i > 0 {
			file = base + "-" + strconv.Itoa(i) + ".log"
		}
		p := filepath.Join(abs, file)
		f, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		if err != nil {
			return fmt.Errorf("create %s/%s: %w", Dir, file, err)
		}
		created(p)
		l.f, l.path = wrapFile(f), p
		prune(abs, p)
		return nil
	}
	return fmt.Errorf("create %s/%s.log: %d names already taken", Dir, base, maxCollisions)
}

// ensureIgnored writes .gitignore unless it already holds exactly
// IgnoreContent, and refuses one that is not a regular file — a directory or a
// symlink there must not stand in for the ignore that keeps logs out of git.
func ensureIgnored(dir string) error {
	p := filepath.Join(dir, ".gitignore")
	info, err := os.Lstat(p)
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return fmt.Errorf("read %s/.gitignore: %w", Dir, err)
	case !info.Mode().IsRegular():
		return fmt.Errorf("%s/.gitignore is not a regular file; it must be the `*` ignore that keeps run logs out of git", Dir)
	default:
		if b, err := os.ReadFile(p); err == nil && string(b) == IgnoreContent {
			return nil
		}
	}
	if err := os.WriteFile(p, []byte(IgnoreContent), 0o644); err != nil {
		return fmt.Errorf("write %s/.gitignore: %w", Dir, err)
	}
	created(p)
	return nil
}

var logName = regexp.MustCompile(`^(test|verify)-.*[.]log$`)

// prune deletes all but the newest Keep logs by mtime, never keep (the file
// just created). It is best-effort: a log that vanished meanwhile is already
// gone, and one that cannot be removed now is retried by the next run's prune
// — neither costs this run its log.
func prune(dir, keep string) {
	des, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	type entry struct {
		path string
		mod  time.Time
	}
	var logs []entry
	for _, d := range des {
		if !d.Type().IsRegular() || !logName.MatchString(d.Name()) {
			continue
		}
		p := filepath.Join(dir, d.Name())
		if p == keep {
			continue
		}
		info, err := d.Info()
		if err != nil {
			continue // removed between the listing and now
		}
		logs = append(logs, entry{p, info.ModTime()})
	}
	sort.Slice(logs, func(i, j int) bool {
		if !logs[i].mod.Equal(logs[j].mod) {
			return logs[i].mod.After(logs[j].mod)
		}
		return logs[i].path > logs[j].path
	})
	for i := Keep - 1; i < len(logs); i++ {
		removeFile(logs[i].path) // ENOENT: someone else pruned it first
	}
}
