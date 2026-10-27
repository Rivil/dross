// Package runsummary turns a test run's output into a short summary: the
// runner's own counts on green, each failing test with its own output on red,
// and the exit status plus the last lines when the output is not recognised.
//
// Every spawn of a run is its own Segment. A Segment splits what it is fed
// into lines, keeps a bounded tail, and feeds every line to one instance of
// each registered Parser; afterwards the single parser that claims the output
// supplies the Result. Nothing here grows with the size of the output: lines
// are cut, the tail is a ring, and a parser keeps at most MaxBlocks failures in
// full and only counts the rest.
package runsummary

import (
	"regexp"
	"strings"
	"sync"
	"unicode/utf8"
)

// Bounds. The failure_bound decision fixes MaxBlocks, BlockLines and
// BlockBytes; the rest keep a mass break inside the agent's Bash window.
const (
	// MaxLine is where the splitter cuts a line that has no newline yet; the
	// rest of that line is dropped (the run log keeps it).
	MaxLine = 64 << 10
	// LineCut is the most bytes of any one line that is printed or kept.
	LineCut = 240
	// TailLines is how many trailing lines the fallback prints.
	TailLines = 40
	// MaxBlocks is how many failures a run prints in full.
	MaxBlocks = 10
	// BlockLines and BlockBytes bound one failure's printed output, whichever
	// is reached first. The name and marker lines sit outside both.
	BlockLines = 30
	BlockBytes = 2800
)

// Parser recognises one runner's output. A fresh instance sees every line of
// one segment through Feed, in order; Claims then says whether the segment was
// this runner's output, and Result is only read when it was.
type Parser interface {
	Feed(line string)
	Claims() bool
	Result() Result
}

// Result is one segment's summary, as its runner reported it.
type Result struct {
	// Runner names the runner on the summary line: "go test", "vitest".
	Runner string
	// Counts are the runner's own count phrases, in its order: "4 ok",
	// "Tests 1 failed | 4 passed (5)".
	Counts []string
	// Failures are the failing tests, build failures included.
	Failures Failures
}

// Failure is one failing test or build-failed package, with as much of its
// own output as fits one printed block.
type Failure struct {
	Name  string
	Lines []string
	// More counts the output lines past the bound, which the log still has.
	More  int
	bytes int
}

// Line attaches one output line, keeping it only while the block is under
// BlockLines lines and BlockBytes bytes; past either it is counted in More.
func (f *Failure) Line(s string) {
	s = keep(s)
	if f.More > 0 || len(f.Lines) >= BlockLines || f.bytes+len(s)+1 > BlockBytes {
		f.More++
		return
	}
	f.Lines = append(f.Lines, s)
	f.bytes += len(s) + 1
}

// Failures is a segment's failing tests: the first MaxBlocks in full, and a
// count of all of them.
type Failures struct {
	Kept  []Failure
	Total int
}

// Add records one failure, keeping it only while fewer than MaxBlocks are kept.
func (fs *Failures) Add(f Failure) {
	fs.Total++
	if len(fs.Kept) < MaxBlocks {
		f.Name = keep(f.Name)
		fs.Kept = append(fs.Kept, f)
	}
}

// Cut shortens s to at most LineCut bytes without splitting a UTF-8 sequence.
func Cut(s string) string {
	if len(s) <= LineCut {
		return s
	}
	i := LineCut
	for i > 0 && !utf8.RuneStart(s[i]) {
		i--
	}
	return s[:i]
}

// keep is Cut for a line that is retained: a copy, so the kept bytes do not
// pin the whole line (up to MaxLine) they were cut from.
func keep(s string) string { return strings.Clone(Cut(s)) }

var (
	regMu    sync.Mutex
	registry []func() Parser
)

// Register adds a parser constructor; each runner's parser calls it from
// init(). Every Segment gets one fresh instance of each.
func Register(newParser func() Parser) {
	regMu.Lock()
	defer regMu.Unlock()
	registry = append(registry, newParser)
}

func parsers() []Parser {
	regMu.Lock()
	defer regMu.Unlock()
	out := make([]Parser, 0, len(registry))
	for _, n := range registry {
		out = append(out, n())
	}
	return out
}

// Segment is one spawn's output. It is an io.Writer; Close flushes a final
// line that had no newline.
type Segment struct {
	mu       sync.Mutex
	parsers  []Parser
	partial  []byte
	dropping bool // past MaxLine in a line that has not ended yet
	tail     [TailLines]string
	next     int // the ring slot the next line goes in
	lines    int // lines seen, capped at TailLines
	closed   bool
}

// NewSegment starts a segment fed to a fresh instance of every registered
// parser.
func NewSegment() *Segment { return &Segment{parsers: parsers()} }

// Write splits p into lines. It never fails: a summary must not stop the copy
// loop of the child feeding it.
func (s *Segment) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := len(p)
	if s.closed {
		return n, nil
	}
	for len(p) > 0 {
		i := indexNL(p)
		if i < 0 {
			s.buffer(p)
			break
		}
		s.buffer(p[:i])
		s.emit()
		p = p[i+1:]
	}
	return n, nil
}

func indexNL(p []byte) int {
	for i, c := range p {
		if c == '\n' {
			return i
		}
	}
	return -1
}

// buffer adds to the line in progress, up to MaxLine; the rest is dropped.
func (s *Segment) buffer(p []byte) {
	if s.dropping {
		return
	}
	room := MaxLine - len(s.partial)
	if len(p) > room {
		s.partial = append(s.partial, p[:room]...)
		s.dropping = true
		return
	}
	s.partial = append(s.partial, p...)
}

// emit hands the line in progress to the tail and the parsers.
func (s *Segment) emit() {
	line := clean(string(s.partial))
	s.partial, s.dropping = s.partial[:0], false
	s.tail[s.next] = keep(line)
	s.next = (s.next + 1) % TailLines
	if s.lines < TailLines {
		s.lines++
	}
	for _, p := range s.parsers {
		p.Feed(line)
	}
}

// Close flushes a final unterminated line. Writes after Close are dropped.
func (s *Segment) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	if len(s.partial) > 0 || s.dropping {
		s.emit()
	}
	s.closed = true
}

var sgr = regexp.MustCompile("\x1b\\[[0-9;]*m")

// clean strips ANSI colour codes and carriage returns. Where a CR sits inside
// the line (a progress redraw), only what follows the last one is kept — what
// a terminal would have shown.
func clean(line string) string {
	if strings.IndexByte(line, 0x1b) >= 0 {
		line = sgr.ReplaceAllString(line, "")
	}
	line = strings.TrimSuffix(line, "\r")
	if i := strings.LastIndexByte(line, '\r'); i >= 0 {
		line = line[i+1:]
	}
	return line
}

// Tail is the last TailLines lines, oldest first, each cut to LineCut.
func (s *Segment) Tail() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, s.lines)
	start := (s.next - s.lines + TailLines) % TailLines
	for i := 0; i < s.lines; i++ {
		out = append(out, s.tail[(start+i)%TailLines])
	}
	return out
}

// Retained is the bytes the segment itself holds: the line in progress and
// the tail ring.
func (s *Segment) Retained() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := cap(s.partial)
	for _, l := range s.tail {
		n += len(l)
	}
	return n
}

// claim is how a segment's output was recognised.
type claim int

const (
	claimed claim = iota
	unclaimed
	ambiguous
)

// result is the one claiming parser's Result. Two claims are as good as none:
// dross never guesses between runners.
func (s *Segment) result() (Result, claim) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var got []Parser
	for _, p := range s.parsers {
		if p.Claims() {
			got = append(got, p)
		}
	}
	switch len(got) {
	case 0:
		return Result{}, unclaimed
	case 1:
		return got[0].Result(), claimed
	}
	return Result{}, ambiguous
}

// Budget spreads MaxBlocks full failure blocks across every render of one run,
// so a run with several lanes still prints at most MaxBlocks in full.
type Budget struct {
	mu    sync.Mutex
	shown int
}

// take grants one full block, or reports the budget spent.
func (b *Budget) take() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.shown >= MaxBlocks {
		return false
	}
	b.shown++
	return true
}
