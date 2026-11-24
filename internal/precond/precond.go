// Package precond evaluates a task's preconditions: the commands a plan says
// must pass before the task may start.
//
// It never checks consent. Every caller does that first, against the phase's
// precondition grant; this package only runs what it is handed, in order, and
// renders the one verdict text that `dross task status … in_progress` and
// `dross task check` both print.
package precond

import (
	"context"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/Rivil/dross/internal/phase"
	"github.com/Rivil/dross/internal/testlane"
)

// field is the label a fence refusal names, so it points at the plan key to
// edit.
const field = "task.preconditions.run"

const (
	// stdoutCap bounds the stdout kept for parsing a measurement. A
	// measurement is one number; anything past the cap is unparseable by
	// definition, so keeping more would only cost memory.
	stdoutCap = 4096
	// tailLines and tailBytes bound the output a refusal quotes.
	tailLines = 20
	tailBytes = 4096
	// quoteCap bounds how much of an unparseable stdout the refusal quotes.
	quoteCap = 200
)

// Result is the outcome of one precondition.
type Result struct {
	// Index is the precondition's 1-based position in the task.
	Index int
	Pre   phase.Precondition
	// Passed is true only when the command exited 0 and, if bounded, printed
	// a number inside its bounds.
	Passed bool
	// Reason says why it failed: "exit 3", "timed out after 200ms",
	// "measured 7, bound max 5", an unparseable measurement, or a spawn
	// failure. Empty on a pass.
	Reason string
	// Measured is the parsed measurement of a bounded precondition, when one
	// parsed.
	Measured *float64
	// Tail is the last lines of stdout and stderr together, in arrival order.
	Tail []string
}

// Verdict is the outcome of a task's preconditions, evaluated in order and
// stopped at the first failure.
type Verdict struct {
	// Total is how many preconditions the task has.
	Total int
	// Checked holds the results of those that ran, in order. On a failure
	// the failing one is last and those after it never ran.
	Checked []Result
}

// OK reports whether every precondition passed.
func (v *Verdict) OK() bool {
	_, failed := v.Failure()
	return !failed
}

// Failure returns the failing result, if any.
func (v *Verdict) Failure() (Result, bool) {
	for _, r := range v.Checked {
		if !r.Passed {
			return r, true
		}
	}
	return Result{}, false
}

// Render writes the verdict as the user reads it to w — the one text both
// `dross task status … in_progress` and `dross task check` print. A failure
// names the precondition, its run line, its why, the reason and its output
// tail.
//
// It writes rather than returns: the tail and an unparseable quote are a
// spawned command's output, which goes to the terminal and never into a
// string a caller could fold into an error or a record.
func (v *Verdict) Render(w io.Writer) {
	if v.Total == 0 {
		fmt.Fprintln(w, "no preconditions")
		return
	}
	if f, failed := v.Failure(); failed {
		fmt.Fprintf(w, "precondition %d of %d failed: %s\n", f.Index, v.Total, f.Reason)
		fmt.Fprintf(w, "  run: %s\n", f.Pre.Run)
		if f.Pre.Why != "" {
			fmt.Fprintf(w, "  why: %s\n", f.Pre.Why)
		}
		if len(f.Tail) == 0 {
			fmt.Fprintln(w, "  output: (none)")
			return
		}
		fmt.Fprintf(w, "  output (last %d lines):\n", len(f.Tail))
		for _, l := range f.Tail {
			fmt.Fprintf(w, "  | %s\n", l)
		}
		return
	}
	fmt.Fprintf(w, "preconditions passed (%d of %d)\n", len(v.Checked), v.Total)
	for _, r := range v.Checked {
		detail := "exit 0"
		if r.Measured != nil {
			detail = "measured " + formatNum(*r.Measured) + boundsText(r.Pre)
		}
		fmt.Fprintf(w, "  %d: %s — %s\n", r.Index, r.Pre.Run, detail)
	}
}

// Evaluate runs pres in order at repoDir, each under its own timeout, and
// stops at the first failure. It never returns a pass it did not observe: a
// spawn failure, a kill, a cancelled ctx or an invalid timeout each fail.
func Evaluate(ctx context.Context, repoDir string, pres []phase.Precondition) Verdict {
	v := Verdict{Total: len(pres)}
	for i, p := range pres {
		r := evaluateOne(ctx, repoDir, i+1, p)
		v.Checked = append(v.Checked, r)
		if !r.Passed {
			break
		}
	}
	return v
}

func evaluateOne(ctx context.Context, repoDir string, index int, p phase.Precondition) Result {
	r := Result{Index: index, Pre: p}
	timeout, err := p.EffectiveTimeout()
	if err != nil {
		// A malformed timeout is a plan defect the shape check reports; it
		// never runs under some other limit.
		r.Reason = "not run: " + err.Error()
		return r
	}
	tctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	tail := &tailWriter{}
	out := &capWriter{max: stdoutCap}
	runErr := testlane.RunLine(tctx, repoDir, field, p.Run,
		io.MultiWriter(out, tail), tail)
	r.Tail = tail.lines()

	switch {
	case ctx.Err() != nil:
		r.Reason = "cancelled: " + ctx.Err().Error()
		return r
	case errors.Is(tctx.Err(), context.DeadlineExceeded):
		r.Reason = "timed out after " + timeout.String()
		return r
	case runErr != nil:
		var exit interface{ ExitCode() int }
		if errors.As(runErr, &exit) {
			if code := exit.ExitCode(); code >= 0 {
				r.Reason = fmt.Sprintf("exit %d", code)
				return r
			}
			r.Reason = "terminated: " + runErr.Error()
			return r
		}
		// No exit status at all: the command never ran (a refused line, a
		// missing directory, no sh). Never read as exit 0.
		r.Reason = "did not run: " + runErr.Error()
		return r
	}

	if p.Max == nil && p.Min == nil {
		r.Passed = true
		return r
	}
	if out.over {
		r.Reason = fmt.Sprintf("unparseable measurement: stdout exceeded %d bytes, starting %s", stdoutCap, quote(out.buf))
		return r
	}
	n, ok := parseMeasurement(string(out.buf))
	if !ok {
		r.Reason = "unparseable measurement: stdout was " + quote(out.buf)
		return r
	}
	r.Measured = &n
	if p.Max != nil && n > *p.Max {
		r.Reason = fmt.Sprintf("measured %s, bound max %s", formatNum(n), formatNum(*p.Max))
		return r
	}
	if p.Min != nil && n < *p.Min {
		r.Reason = fmt.Sprintf("measured %s, bound min %s", formatNum(n), formatNum(*p.Min))
		return r
	}
	r.Passed = true
	return r
}

// decimal is the only measurement shape accepted: an optionally signed
// decimal with an optional exponent. strconv.ParseFloat alone would also take
// NaN, Inf, hex floats and underscores, none of which is a measurement.
var decimal = regexp.MustCompile(`^[+-]?(\d+(\.\d*)?|\.\d+)([eE][+-]?\d+)?$`)

// parseMeasurement reads stdout as exactly one number, surrounding whitespace
// aside. Anything else — empty, units, several lines, a percentage — is
// unparseable, never coerced to 0.
func parseMeasurement(s string) (float64, bool) {
	s = strings.TrimSpace(s)
	if !decimal.MatchString(s) {
		return 0, false
	}
	n, err := strconv.ParseFloat(s, 64)
	if err != nil {
		// Out of float64 range: not a usable measurement.
		return 0, false
	}
	return n, true
}

func formatNum(v float64) string {
	return strconv.FormatFloat(v, 'g', -1, 64)
}

func boundsText(p phase.Precondition) string {
	var parts []string
	if p.Min != nil {
		parts = append(parts, "min "+formatNum(*p.Min))
	}
	if p.Max != nil {
		parts = append(parts, "max "+formatNum(*p.Max))
	}
	return " (" + strings.Join(parts, ", ") + ")"
}

func quote(b []byte) string {
	if len(b) > quoteCap {
		return strconv.Quote(string(b[:quoteCap])) + "…"
	}
	return strconv.Quote(string(b))
}

// capWriter keeps the first max bytes and notes whether more arrived. It
// always reports a full write, so a child printing past the cap is never
// stopped by a short write mid-measurement.
type capWriter struct {
	max  int
	buf  []byte
	over bool
}

func (w *capWriter) Write(p []byte) (int, error) {
	room := w.max - len(w.buf)
	if len(p) > room {
		w.over = true
		if room > 0 {
			w.buf = append(w.buf, p[:room]...)
		}
		return len(p), nil
	}
	w.buf = append(w.buf, p...)
	return len(p), nil
}

// tailWriter keeps the last tailBytes of everything written to it. stdout and
// stderr both feed it from separate copy goroutines, hence the lock.
type tailWriter struct {
	mu  sync.Mutex
	buf []byte
}

func (w *tailWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(p) >= tailBytes {
		w.buf = append(w.buf[:0], p[len(p)-tailBytes:]...)
		return len(p), nil
	}
	w.buf = append(w.buf, p...)
	if over := len(w.buf) - tailBytes; over > 0 {
		w.buf = append([]byte(nil), w.buf[over:]...)
	}
	return len(p), nil
}

// lines is the kept output as at most tailLines lines.
func (w *tailWriter) lines() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	s := strings.TrimRight(string(w.buf), "\n")
	if s == "" {
		return nil
	}
	ls := strings.Split(s, "\n")
	if len(ls) > tailLines {
		ls = ls[len(ls)-tailLines:]
	}
	return ls
}
