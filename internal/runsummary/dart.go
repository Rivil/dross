package runsummary

import (
	"regexp"
	"strings"
)

func init() {
	Register(func() Parser { return &dart{} })
}

// dart parses `flutter test` / `dart test` output from both the compact and
// the expanded reporter (reporter_formats). Every progress line starts with
// the runner's own counter, "00:04 +18 ~1 -2: "; compact redraws it in place
// with carriage returns, which Segment's clean already reduces to the last
// frame. Its counts are the final counter, verbatim. Each "[E]" progress line
// is one failure, named without the counter or the marker ("loading <file>"
// becomes the file), holding the lines under it until the next progress line.
// A widget test prints its "══╡ EXCEPTION CAUGHT" block before its [E] line,
// so a block printed while no failure is open is held and becomes the start
// of the next failure — if the next progress line is that [E] line; any other
// progress line drops it, so it never lands on a later test's failure. A
// block printed under an open failure, before the next progress line — a
// teardown exception after the test's [E] — is that failure's. A progress
// line that repeats the last one apart from its clock is the runner redrawing
// as a second ticks over, not a new event: it changes nothing, so a held or
// open block survives it. It claims a segment only on the runner's terminal
// line — "All tests passed!", "Some tests failed." or "No tests ran." — never
// on a running counter.
type dart struct {
	counter string // the terminal line's counter, once printed
	fs      Failures
	cur     *blockLines
	pending *blockLines // an exception block waiting for its [E] line
	exc     *blockLines // the block an open "══╡ EXCEPTION CAUGHT" block goes to
	last    string      // the last progress line's counter and text, clock dropped
}

var (
	dartProgress = regexp.MustCompile(`^\d+:\d+(?::\d+)? (\+\d+(?: ~\d+)?(?: -\d+)?): (.*?)\s*$`)
	dartTerminal = regexp.MustCompile(`^(?:All tests passed!|Some tests failed\.|No tests ran\.)$`)
)

func (d *dart) Feed(line string) {
	if m := dartProgress.FindStringSubmatch(line); m != nil {
		event := m[1] + ": " + m[2]
		if event == d.last {
			return // a clock tick redrawing the same line
		}
		d.last = event
		d.close()
		d.exc = nil
		rest := m[2]
		if !strings.HasSuffix(rest, "[E]") {
			// The held block's [E] line did not follow: it is no failure's.
			d.pending = nil
			if dartTerminal.MatchString(rest) {
				d.counter = m[1]
			}
			return
		}
		name := strings.TrimSpace(strings.TrimSuffix(rest, "[E]"))
		name = strings.TrimPrefix(name, "loading ")
		if d.pending != nil {
			d.cur, d.pending = d.pending, nil
			d.cur.f.Name = name
		} else {
			d.cur = &blockLines{f: Failure{Name: name}}
		}
		return
	}
	switch {
	case strings.HasPrefix(line, "══╡ EXCEPTION CAUGHT"):
		d.exc = d.cur
		if d.exc == nil {
			if d.pending == nil {
				d.pending = &blockLines{}
			}
			d.exc = d.pending
		}
		d.exc.line(line)
	case d.exc != nil:
		d.exc.line(line)
		if strings.Trim(line, "═") == "" && line != "" {
			d.exc = nil
		}
	case strings.HasPrefix(line, "To run this test again:"):
		// A pointer to the runner, not the failure's own output.
	case d.cur != nil:
		d.cur.line(line)
	}
}

func (d *dart) close() {
	if d.cur != nil {
		d.cur.addTo(&d.fs)
		d.cur = nil
	}
}

func (d *dart) Claims() bool { return d.counter != "" }

func (d *dart) Result() Result {
	d.close()
	return Result{Runner: "dart test", Counts: []string{d.counter}, Failures: d.fs}
}
