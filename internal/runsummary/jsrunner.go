package runsummary

import (
	"regexp"
	"strconv"
	"strings"
)

func init() {
	Register(func() Parser { return &vitest{} })
	Register(func() Parser { return &jest{} })
}

// blockLines collects one failure's output, dropping blank lines at its edges
// so a block never opens or closes on padding. names holds further failures
// that share the block: vitest prints identical errors once, under all their
// FAIL headers.
type blockLines struct {
	f      Failure
	names  []string
	shared int // headers past MaxBlocks: counted, never kept
	blanks int
}

// share adds one more FAIL header to the block, keeping at most MaxBlocks
// names — a throwing beforeEach can put a whole file under one error.
func (b *blockLines) share(name string) {
	if len(b.names) < MaxBlocks {
		b.names = append(b.names, keep(name))
		return
	}
	b.shared++
}

// empty reports whether no output line has reached the block yet.
func (b *blockLines) empty() bool { return len(b.f.Lines) == 0 && b.f.More == 0 }

// addTo records the block's failure, once per name that shares it.
func (b *blockLines) addTo(fs *Failures) {
	fs.Add(b.f)
	for _, n := range b.names {
		f := b.f
		f.Name = n
		fs.Add(f)
	}
	fs.Total += b.shared
}

func (b *blockLines) line(s string) {
	if strings.TrimSpace(s) == "" {
		if len(b.f.Lines) > 0 || b.f.More > 0 {
			b.blanks++
		}
		return
	}
	for ; b.blanks > 0; b.blanks-- {
		b.f.Line("")
	}
	b.f.Line(s)
}

// vitest parses `vitest run`'s default reporter. Its counts are the runner's
// own "Test Files", "Tests" and "Errors" summary lines; each `FAIL` block after
// a "Failed Tests" or "Failed Suites" banner is one failure (a suite that failed
// to load included), several FAIL headers over one shared error are one failure
// each, and each error under the "Unhandled Errors" banner is one more. It
// claims a segment only once the "Tests" summary line has printed: a run cut
// short is not summarised.
type vitest struct {
	files, tests, errors string
	passed               int
	unhandled            bool // inside the "Unhandled Errors" section
	fs                   Failures
	cur                  *blockLines
}

var (
	vitestFiles = regexp.MustCompile(`^\s*Test Files\s+(\d.*\(\d+\))\s*$`)
	vitestTests = regexp.MustCompile(`^\s*Tests\s+(no tests|\d.*\(\d+\))\s*$`)
	vitestErrs  = regexp.MustCompile(`^\s*Errors\s+(\d+ errors?)\s*$`)
	vitestFail  = regexp.MustCompile(`^\s*FAIL\s+(.+?)\s*$`)
	passedRE    = regexp.MustCompile(`(\d+) passed`)
)

func (v *vitest) Feed(line string) {
	switch {
	case vitestFiles.MatchString(line):
		v.close()
		v.files = "Test Files " + vitestFiles.FindStringSubmatch(line)[1]
	case vitestTests.MatchString(line):
		v.close()
		v.tests = "Tests " + vitestTests.FindStringSubmatch(line)[1]
		if m := passedRE.FindStringSubmatch(v.tests); m != nil {
			v.passed, _ = strconv.Atoi(m[1])
		}
	case vitestErrs.MatchString(line):
		v.close()
		v.errors = "Errors " + vitestErrs.FindStringSubmatch(line)[1]
	case strings.Contains(line, "⎯"):
		// A banner or a block's footer ends the block. Inside the "Unhandled
		// Errors" section each typed banner — "Unhandled Rejection",
		// "Uncaught Exception", "Unhandled Error" — opens one of its own; the
		// section's preamble belongs to none.
		v.close()
		switch title := strings.TrimSpace(strings.Trim(line, "⎯ ")); {
		case title == "Unhandled Errors":
			v.unhandled = true
		case v.unhandled && title != "" && !strings.HasPrefix(title, "["):
			v.cur = &blockLines{f: Failure{Name: title}}
		}
	case vitestFail.MatchString(line):
		name := vitestFail.FindStringSubmatch(line)[1]
		if i := strings.Index(name, " [ "); i > 0 {
			name = name[:i] // a suite: "src/x.test.ts [ src/x.test.ts ]"
		}
		if v.cur != nil && v.cur.empty() {
			v.cur.share(name) // one error, several headers
			return
		}
		v.close()
		v.cur = &blockLines{f: Failure{Name: name}}
	default:
		if v.cur != nil {
			v.cur.line(line)
		}
	}
}

func (v *vitest) close() {
	if v.cur != nil {
		v.cur.addTo(&v.fs)
		v.cur = nil
	}
}

func (v *vitest) Claims() bool { return v.tests != "" }

func (v *vitest) Result() Result {
	v.close()
	var counts []string
	for _, c := range []string{v.files, v.tests, v.errors} {
		if c != "" {
			counts = append(counts, c)
		}
	}
	return Result{Runner: "vitest", Counts: counts, Failures: v.fs}
}

// jest parses jest's default reporter. Its counts are the "Test Suites:" and
// "Tests:" summary lines; each `●` block is one failure, named after its
// file, "Test suite failed to run" included. A `● Console` block is a file's
// logged output, not a failure, and the "Summary of all failing tests" recap
// repeats blocks already counted: both are skipped. It claims a segment only
// once the "Tests:" summary line has printed.
type jest struct {
	suites, tests string
	file          string
	recap         bool
	fs            Failures
	cur           *blockLines
}

var (
	jestSuites = regexp.MustCompile(`^Test Suites:\s+(.*\d+ total)\s*$`)
	jestTests  = regexp.MustCompile(`^Tests:\s+(.*\d+ total)\s*$`)
	jestFile   = regexp.MustCompile(`^\s*(?:FAIL|PASS)\s+(\S+)`)
	jestBullet = regexp.MustCompile(`^\s+● (.+?)\s*$`)
)

func (j *jest) Feed(line string) {
	switch {
	case jestSuites.MatchString(line):
		j.close()
		j.suites = "Test Suites: " + jestSuites.FindStringSubmatch(line)[1]
	case jestTests.MatchString(line):
		j.close()
		j.tests = "Tests: " + jestTests.FindStringSubmatch(line)[1]
	case strings.HasPrefix(line, "Summary of all failing tests"):
		j.close()
		j.recap = true
	case jestFile.MatchString(line):
		j.close()
		j.file = strings.TrimPrefix(jestFile.FindStringSubmatch(line)[1], "./")
	case jestBullet.MatchString(line):
		j.close()
		name := jestBullet.FindStringSubmatch(line)[1]
		if j.recap || name == "Console" {
			return
		}
		if j.file != "" {
			name = j.file + " › " + name
		}
		j.cur = &blockLines{f: Failure{Name: name}}
	default:
		if j.cur != nil {
			j.cur.line(line)
		}
	}
}

func (j *jest) close() {
	if j.cur != nil {
		j.cur.addTo(&j.fs)
		j.cur = nil
	}
}

func (j *jest) Claims() bool { return j.tests != "" }

func (j *jest) Result() Result {
	j.close()
	var counts []string
	for _, c := range []string{j.suites, j.tests} {
		if c != "" {
			counts = append(counts, c)
		}
	}
	return Result{Runner: "jest", Counts: counts, Failures: j.fs}
}
