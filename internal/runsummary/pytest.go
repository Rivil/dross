package runsummary

import (
	"regexp"
	"strings"
)

func init() {
	Register(func() Parser { return &pytest{} })
}

// pytest parses pytest's output, default and -q alike (reporter_formats). Its
// count is pytest's own terminal line — "3 failed, 1979 passed in 54.51s",
// with or without the "===" padding, which is stripped along with the
// "(0:01:02)" clock. Each FAILED/ERROR line under the "short test summary
// info" banner is one failure named by its node id; its output is the
// matching "____ name ____" section printed above, from the section's first
// ">" line on, so a long test body never crowds out its "E   " lines; when a
// deep traceback runs that past the bound, the test's own ">" line and the
// innermost frame — where the "E   " lines are — are kept instead. It claims
// a segment only on the count line: a wrapper's own lines — ruff's "Found 2
// errors.", mypy's "Found 3 errors in 2 files" — never forge one.
type pytest struct {
	counts   string
	summary  bool         // under the "short test summary info" banner
	sections []*pySection // in print order, at most MaxBlocks
	cur      *pySection
	fs       Failures
}

// pySection is one "____ name ____" section's output: everything until its
// first ">" line, then everything from that line on. inner is the innermost
// frame so far: it restarts at every ">" run that opens a frame.
type pySection struct {
	name       string
	pre, post  blockLines
	inner      blockLines
	head       string // the first ">" line: the test's own failing line
	frames     int    // ">" runs seen
	marked     bool
	prevMarked bool // the line before was a ">" line
	used       bool // matched to a summary line already
}

func (s *pySection) line(l string) {
	mark := strings.HasPrefix(l, ">")
	if !s.marked && mark {
		s.marked, s.head = true, keep(l)
	}
	if !s.marked {
		s.pre.line(l)
		return
	}
	s.post.line(l)
	// A multi-line statement marks each of its lines: one frame, not several.
	if mark && !s.prevMarked {
		s.frames++
		s.inner = blockLines{}
	}
	s.prevMarked = mark
	s.inner.line(l)
}

// failure is the section from its first ">" line on. When that ran past the
// bound across several frames, the "E   " lines at the end are what was cut:
// it is the test's own failing line, then the innermost frame, and every line
// between them is counted in More.
func (s *pySection) failure() Failure {
	if !s.marked {
		return s.pre.f
	}
	if s.post.f.More == 0 || s.frames < 2 {
		return s.post.f
	}
	var f Failure
	f.Line(s.head)
	for _, l := range s.inner.f.Lines {
		f.Line(l)
	}
	f.More += s.inner.f.More + lineCount(s.post.f) - 1 - lineCount(s.inner.f)
	return f
}

// lineCount is every output line a block saw, kept or counted in More.
func lineCount(f Failure) int { return len(f.Lines) + f.More }

var (
	pyCount  = regexp.MustCompile(`^=*\s*((?:\d+ (?:subtests )?(?:passed|failed|skipped|deselected|xfailed|xpassed|errors?|warnings?|rerun)(?:, )?)+ in \d+(?:\.\d+)?s|no tests ran in \d+(?:\.\d+)?s)(?: \(\d+:\d\d:\d\d\))?\s*=*$`)
	pyBanner = regexp.MustCompile(`^=+ (.+?) =+$`)
	pyHeader = regexp.MustCompile(`^_+ (.+?) _+$`)
	pyResult = regexp.MustCompile(`^(FAILED|ERROR) (\S.*?)\s*$`)
)

func (p *pytest) Feed(line string) {
	if m := pyCount.FindStringSubmatch(line); m != nil {
		p.cur = nil
		p.summary = false
		p.counts = m[1]
		return
	}
	if m := pyBanner.FindStringSubmatch(line); m != nil {
		p.cur = nil
		p.summary = m[1] == "short test summary info"
		return
	}
	if p.summary {
		if m := pyResult.FindStringSubmatch(line); m != nil {
			id := nodeID(m[2])
			f := Failure{Name: id}
			if s := p.section(m[1], id); s != nil {
				f = s.failure()
				f.Name = id
			}
			p.fs.Add(f)
		}
		return
	}
	// "_ _ _ _" between a traceback's frames is not a new section.
	if m := pyHeader.FindStringSubmatch(line); m != nil && strings.Trim(m[1], "_ ") != "" {
		p.cur = nil
		if len(p.sections) < MaxBlocks {
			p.cur = &pySection{name: m[1]}
			p.sections = append(p.sections, p.cur)
		}
		return
	}
	if p.cur != nil {
		p.cur.line(line)
	}
}

// nodeID is a summary line's node id without pytest's " - message" tail. An
// id is parametrized only when its "[" comes before the first " - ": then it
// ends at its closing bracket, whatever the brackets hold. A plain id's
// message may hold brackets of its own ("- assert [1] == [2]").
func nodeID(rest string) string {
	k := strings.Index(rest, " - ")
	if b := strings.IndexByte(rest, '['); b >= 0 && (k < 0 || b < k) {
		if e := strings.Index(rest[b:], "] - "); e >= 0 {
			return rest[:b+e+1]
		}
		return rest
	}
	if k >= 0 {
		return rest[:k]
	}
	return rest
}

// section finds the first unmatched section printed for a summary line of
// kind FAILED or ERROR: a FAILED test's is "Class.test_x[a-b]" for
// "path::Class::test_x[a-b]", an ERROR test's the setup/teardown error form,
// and a module that failed to collect's "ERROR collecting path". The kind
// keeps a test that both FAILED and ERRORed at teardown on two sections, not
// on whichever printed first. Section headers drop the path, so two modules'
// test_x share a name; pytest prints sections and summary lines in the same
// order, and taking the first unmatched one keeps each failure on its own
// section.
func (p *pytest) section(kind, id string) *pySection {
	path, test, ok := strings.Cut(id, "::")
	keys := []string{"ERROR collecting " + path}
	if ok {
		key := strings.ReplaceAll(test, "::", ".")
		keys = []string{key}
		if kind == "ERROR" {
			keys = []string{"ERROR at setup of " + key, "ERROR at teardown of " + key}
		}
	}
	for _, s := range p.sections {
		if s.used {
			continue
		}
		for _, k := range keys {
			if s.name == k {
				s.used = true
				return s
			}
		}
	}
	return nil
}

func (p *pytest) Claims() bool { return p.counts != "" }

func (p *pytest) Result() Result {
	return Result{Runner: "pytest", Counts: []string{p.counts}, Failures: p.fs}
}
