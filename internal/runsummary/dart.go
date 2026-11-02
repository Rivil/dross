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
// so that block is held and becomes the start of the next failure. It claims
// a segment only on the runner's terminal line — "All tests passed!", "Some
// tests failed." or "No tests ran." — never on a running counter.
type dart struct {
	counter string // the terminal line's counter, once printed
	fs      Failures
	cur     *blockLines
	pending *blockLines // an exception block waiting for its [E] line
	inExc   bool        // inside a "══╡ EXCEPTION CAUGHT" block
}

var (
	dartProgress = regexp.MustCompile(`^\d+:\d+(?::\d+)? (\+\d+(?: ~\d+)?(?: -\d+)?): (.*?)\s*$`)
	dartTerminal = regexp.MustCompile(`^(?:All tests passed!|Some tests failed\.|No tests ran\.)$`)
)

func (d *dart) Feed(line string) {
	if m := dartProgress.FindStringSubmatch(line); m != nil {
		d.close()
		d.inExc = false
		rest := m[2]
		switch {
		case dartTerminal.MatchString(rest):
			d.counter = m[1]
		case strings.HasSuffix(rest, "[E]"):
			name := strings.TrimSpace(strings.TrimSuffix(rest, "[E]"))
			name = strings.TrimPrefix(name, "loading ")
			if d.pending != nil {
				d.cur, d.pending = d.pending, nil
				d.cur.f.Name = name
			} else {
				d.cur = &blockLines{f: Failure{Name: name}}
			}
		}
		return
	}
	switch {
	case strings.HasPrefix(line, "══╡ EXCEPTION CAUGHT"):
		d.close()
		d.inExc = true
		if d.pending == nil {
			d.pending = &blockLines{}
		}
		d.pending.line(line)
	case d.inExc:
		d.pending.line(line)
		if strings.Trim(line, "═") == "" && line != "" {
			d.inExc = false
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
