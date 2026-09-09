package mutation

import (
	"bytes"
	"fmt"
	"io"
	"strings"
)

// This file holds the ONE way a mutation adapter records a failed tool
// invocation, and the tee buffer that keeps the tool's own output out of it.
//
// The cut is at error CONSTRUCTION, not at the persistence boundary (spec
// decision cut_point). An adapter never builds an error string containing a
// byte of tool output: it prints the head of that output to stderr at the
// failure point, where the reader is already looking, and returns a record
// carrying only facts ABOUT the output — which tool, what exit status, how many
// bytes went past, and where the full stream went. A scrubber downstream could
// only strip shapes it had been told about; a string that was never built
// cannot leak.

// exitDidNotStart is the exit status recorded when the tool never ran — no
// *exec.ExitError, so there is no status to report. Rendered as prose rather
// than as a number, because "-1" reads as a real exit status the tool chose.
const exitDidNotStart = -1

// capture is how much of the tool's output this run actually saw. It has
// exactly two shapes and no zero value worth trusting: a detached run tees
// nothing, and recording that as `0 bytes` is a structurally false claim that
// the tool was silent. Build one with Observed or NotCaptured.
type capture struct {
	observed bool
	n        int
	why      string
}

// Observed records that n bytes of tool output went past this run. n is the
// TOTAL seen, not the retained prefix — see headBuffer.observed.
func Observed(n int) capture { return capture{observed: true, n: n} }

// NotCaptured records that this run saw no output at all, and why. `why` is
// dross-authored prose (e.g. "this run was detached"), never tool text.
func NotCaptured(why string) capture { return capture{why: why} }

// clause renders the capture half of a record's message.
//
// The observed wording says OBSERVED rather than retained or captured: the tee
// is capped at strykerHeadBytes, so a run that printed 100 KiB retains 64 KiB
// but observed all of it. Reporting the retained figure would understate the
// output every time the cap bit.
func (c capture) clause() string {
	if c.observed {
		return fmt.Sprintf("%d bytes of tool output observed", c.n)
	}
	why := strings.TrimSpace(c.why)
	if why == "" {
		why = "reason not recorded"
	}
	return "no tool output was seen by this run: " + why
}

// toolFailure is the record. Unexported with no exported field, and built only
// through RecordToolFailure, so the sole route by which tool text could enter
// one is a new constructor parameter — which the reflect pin in
// toolfail_test.go fails on.
type toolFailure struct {
	tool string
	exit int
	cap  capture
	err  error
}

// RecordToolFailure builds the record for a tool that ran and failed.
//
// The parameter list is the guarantee: a string (the tool's NAME), an int (its
// exit status) and a capture (a count, or why there is none). There is no
// parameter that could carry the tool's output, and TestRecordToolFailureShape
// fails if one is added.
func RecordToolFailure(tool string, exit int, c capture) *toolFailure {
	return &toolFailure{tool: tool, exit: exit, cap: c}
}

// wrapping preserves an error's identity through the record, so classification
// off the live value — errors.Is against remote.ErrTransport and
// remote.ErrRemoteCommand — keeps firing after the message is replaced.
func (f *toolFailure) wrapping(err error) *toolFailure {
	f.err = err
	return f
}

// Error is fixed prose. Every clause is asserted in toolfail_test.go:
//
//   - the tool name as a bare token, because telemetry's `mutation` bucket
//     keys on the substrings "stryker", "gremlins" and "mutation adapter"
//     (telemetry.go classifier). A rewording that drops it silently pushes
//     every adapter failure into `other`.
//   - the exit status, which is the one number that says what the tool did.
//   - the capture clause.
//   - the stderr pointer: fixed prose, no path. dross cannot know where its own
//     stderr went — nohup, CI capture, a detached remote run — so a recorded
//     path would often be wrong, and writing a log file would re-persist
//     exactly what this phase exists to stop persisting (spec decision
//     stderr_pointer).
func (f *toolFailure) Error() string {
	var b strings.Builder
	b.WriteString(f.tool)
	if f.exit == exitDidNotStart {
		b.WriteString(" did not start")
	} else {
		// "exit status N", matching what exec.ExitError itself renders, so a
		// reader grepping for the familiar phrase finds it.
		fmt.Fprintf(&b, " failed with exit status %d", f.exit)
	}
	b.WriteString("; ")
	b.WriteString(f.cap.clause())
	b.WriteString("; the full output of ")
	b.WriteString(f.tool)
	b.WriteString(" went to this run's stderr, not to any file dross writes")
	return b.String()
}

func (f *toolFailure) Unwrap() error { return f.err }

// LegError is the carrier for a persisted mutation-leg error string. It is a
// NAMED type on purpose: the toolfence walker accepts an assignment to a
// Recorded field only when the right-hand side came from here, and a bare
// `string` gives it nothing to key on.
type LegError struct{ text string }

// RecordLegError carries an adapter error to the one place it is allowed to
// become persisted text.
//
// It does NOT rewrite the error. An adapter that failed on a tool already
// returns a toolFailure whose message carries no tool output; an adapter that
// failed on a dross-authored diagnostic (an argfence refusal, an unreadable
// report) must keep saying so, or the funnel would swallow the diagnostics it
// was never meant to touch.
func RecordLegError(err error) LegError {
	if err == nil {
		return LegError{}
	}
	return LegError{text: err.Error()}
}

// String is the marshalling value. The persisted field stays a Go string —
// LegError's unexported field cannot marshal — so the named type lives on the
// derived value, exactly as pathfence's does.
func (e LegError) String() string { return e.text }

// headBuffer retains the first `limit` bytes written through it and silently
// discards the rest, always reporting a full write so it can sit inside an
// io.MultiWriter without truncating the stream its sibling is rendering.
type headBuffer struct {
	limit int
	buf   bytes.Buffer
	// seen totals EVERY byte written through, not the retained prefix. A run
	// that printed 100 KiB past a 64 KiB cap observed 102400 bytes; reading
	// buf.Len() would report the saturated 65536 and quietly cap the truth at
	// the size of the buffer.
	seen int
}

func (h *headBuffer) Write(p []byte) (int, error) {
	h.seen += len(p)
	if room := h.limit - h.buf.Len(); room > 0 {
		if len(p) <= room {
			h.buf.Write(p)
		} else {
			h.buf.Write(p[:room])
		}
	}
	// len(p), never the amount kept: a short count is an io.ErrShortWrite to
	// io.MultiWriter, which would abort the write to os.Stderr as well and
	// truncate the live output the moment the cap was reached.
	return len(p), nil
}

// observed is the total number of bytes that went past this buffer.
func (h *headBuffer) observed() int { return h.seen }

// contains reports whether the retained head matches s. Used to key the
// adapter's own dross-authored hints off what the tool said, without that text
// reaching an error string.
func (h *headBuffer) contains(s string) bool {
	return strings.Contains(h.buf.String(), s)
}

// printHead writes at most n lines of the retained head to w, indented, under a
// banner naming the tool.
//
// A WRITER, not a string. The head is the live diagnostic — it goes to the
// terminal at the failure point, where the reader is already looking, and
// nowhere else. Returning a string would put the tool's own output back within
// reach of the next error constructor, which is the thing this phase removes.
func (h *headBuffer) printHead(w io.Writer, tool string, n int) {
	text := strings.TrimRight(h.buf.String(), "\n")
	if text == "" {
		fmt.Fprintf(w, "%s produced no output at all — it may not have started.", tool)
		return
	}
	lines := strings.Split(text, "\n")
	truncated := false
	if len(lines) > n {
		lines, truncated = lines[:n], true
	}
	fmt.Fprintf(w, "the head of %s's output, which is where the cause is:\n\n", tool)
	for _, l := range lines {
		fmt.Fprintf(w, "    %s\n", l)
	}
	if truncated {
		fmt.Fprint(w, "    … (output continues above)\n")
	}
}
