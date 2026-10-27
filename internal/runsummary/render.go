package runsummary

import (
	"fmt"
	"strings"
	"time"
)

// MoreFailingPrefix follows "+N " on the line that closes a cut failure list:
// "+4 more failing — see <log>". The execute and quick prompts quote it.
const MoreFailingPrefix = "more failing — see"

// MoreLinesPrefix follows "… N " on the line that closes a cut failure block:
// "… 20 more lines — see <log>".
const MoreLinesPrefix = "more lines — see"

// Outcome is how the run ended.
type Outcome struct {
	Exit int
	// Ran is false when the run did not happen — the host was unreachable or
	// the tree never fully arrived — so no count can describe it.
	Ran bool
}

// Render summarises segs. It prints green only when the run happened, exited
// 0, every segment was claimed by exactly one parser and none reported a
// failure. Reported failures print red, at most b's remaining full blocks.
// Anything else prints the fallback: the exit status, the last TailLines
// lines and the log path. Every printed line is cut to LineCut bytes.
func Render(o Outcome, segs []*Segment, b *Budget, logPath string, wall time.Duration) string {
	var w lines
	if !o.Ran {
		fallback(&w, o, "the run did not happen", lastOf(segs), logPath)
		return w.String()
	}
	results := make([]Result, 0, len(segs))
	failing := 0
	for _, s := range segs {
		r, c := s.result()
		switch c {
		case unclaimed:
			fallback(&w, o, "output not recognised", s, logPath)
			return w.String()
		case ambiguous:
			fallback(&w, o, "output claimed by more than one runner", s, logPath)
			return w.String()
		}
		results = append(results, r)
		failing += r.Failures.Total
	}
	if len(results) == 0 {
		fallback(&w, o, "no output", nil, logPath)
		return w.String()
	}
	if failing == 0 {
		if o.Exit != 0 {
			fallback(&w, o, "no failing test reported", lastOf(segs), logPath)
			return w.String()
		}
		w.add("PASS · " + counts(results) + " · " + clock(wall))
		w.add("log: " + logPath)
		return w.String()
	}

	w.add(fmt.Sprintf("FAIL (exit %d) · %s · %d failing · %s", o.Exit, counts(results), failing, clock(wall)))
	more := 0
	for _, r := range results {
		more += r.Failures.Total - len(r.Failures.Kept)
		for _, f := range r.Failures.Kept {
			if !b.take() {
				more++
				continue
			}
			w.add("✗ " + f.Name)
			for _, l := range f.Lines {
				w.add(l)
			}
			if f.More > 0 {
				w.add(fmt.Sprintf("… %d %s %s", f.More, MoreLinesPrefix, logPath))
			}
		}
	}
	if more > 0 {
		w.add(fmt.Sprintf("+%d %s %s", more, MoreFailingPrefix, logPath))
	}
	w.add("log: " + logPath)
	return w.String()
}

// fallback prints the exit status, why there is no summary, and s's tail.
func fallback(w *lines, o Outcome, why string, s *Segment, logPath string) {
	var tail []string
	if s != nil {
		tail = s.Tail()
	}
	w.add(fmt.Sprintf("exit %d · %s — last %d lines:", o.Exit, why, len(tail)))
	for _, l := range tail {
		w.add(l)
	}
	w.add("log: " + logPath)
}

func lastOf(segs []*Segment) *Segment {
	if len(segs) == 0 {
		return nil
	}
	return segs[len(segs)-1]
}

// counts joins each segment's runner and count phrases.
func counts(rs []Result) string {
	parts := make([]string, 0, len(rs))
	for _, r := range rs {
		c := strings.Join(r.Counts, ", ")
		if r.Runner != "" {
			c = r.Runner + ": " + c
		}
		parts = append(parts, c)
	}
	return strings.Join(parts, " · ")
}

// clock formats a wall-clock time: whole seconds from a minute up ("1m12s"),
// tenths below it ("3.2s").
func clock(d time.Duration) string {
	if d >= time.Minute {
		return d.Round(time.Second).String()
	}
	return d.Round(100 * time.Millisecond).String()
}

// lines accumulates printed lines, each cut to LineCut.
type lines struct{ b strings.Builder }

func (l *lines) add(s string) {
	l.b.WriteString(Cut(s))
	l.b.WriteByte('\n')
}

func (l *lines) String() string { return l.b.String() }
