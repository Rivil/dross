package runsummary

import (
	"fmt"
	"regexp"
	"strings"
)

func init() { Register(func() Parser { return &goTest{} }) }

// goTest parses plain `go test` output (not -v, not -json): one result line per
// package, and each failing test's `--- FAIL:` line followed by its own
// indented output.
//
// A failure is a LEAF `--- FAIL`: a parent whose subtests failed is not one
// itself, and its own lines are dropped. A package that FAILs with no failing
// test of its own — a timeout panic, a TestMain exit — becomes a failure named
// after the package, carrying the lines it printed. Each `# pkg` block (a
// compile, vet or setup error) is one failure; the `[build failed]` and
// `[setup failed]` packages it causes are counted, not repeated.
type goTest struct {
	ok, failed, noTestFiles, noTests int
	results                          int // package result lines seen

	fs          Failures
	stack       []*goFrame
	panic       bool     // a panic is being attached to the innermost open test
	timedOut    bool     // the orphan lines hold the package's timeout panic
	verbose     bool     // -v output: a test's lines precede its --- FAIL
	orphan      *Failure // lines since the last result line that belong to no test
	block       *Failure // the `# pkg` block being read
	blocks      map[string]bool
	sinceResult int // failures added since the last package result line
}

type goFrame struct {
	f      Failure
	indent int
	parent bool // a subtest failed beneath it: not a leaf
}

var (
	goOK       = regexp.MustCompile(`^ok  \t(\S+)\t`)
	goNoFiles  = regexp.MustCompile(`^\?   \t(\S+)\t\[no test files\]`)
	goFail     = regexp.MustCompile(`^FAIL\t(\S+)(?:\t| \[)`)
	goTestFail = regexp.MustCompile(`^( *)--- FAIL: (\S+) \(`)
	goBlock    = regexp.MustCompile(`^# ([^\s\[]+)(?: \[[^\]]+\])?$`)
)

func (g *goTest) Feed(line string) {
	switch {
	case goOK.MatchString(line):
		g.result()
		g.ok++
		if strings.HasSuffix(line, "[no tests to run]") {
			g.ok--
			g.noTests++
		}
	case goNoFiles.MatchString(line):
		g.result()
		g.noTestFiles++
	case goFail.MatchString(line):
		pkg := goFail.FindStringSubmatch(line)[1]
		g.closeTests()
		g.closeBlock()
		if (g.sinceResult == 0 || g.timedOut) && !g.blocks[pkg] && g.orphan != nil && len(g.orphan.Lines) > 0 {
			g.orphan.Name = pkg
			g.add(*g.orphan)
		}
		g.result()
		g.failed++
	case goBlock.MatchString(line):
		g.closeTests()
		g.closeBlock()
		if g.blocks == nil {
			g.blocks = map[string]bool{}
		}
		g.blocks[goBlock.FindStringSubmatch(line)[1]] = true
		g.block = &Failure{Name: strings.TrimPrefix(line, "# ")}
	case goTestFail.MatchString(line):
		g.closeBlock()
		m := goTestFail.FindStringSubmatch(line)
		g.openTest(m[2], len(m[1]))
	case line == "FAIL" || line == "PASS":
		g.closeTests()
		g.closeBlock()
	default:
		g.text(line)
	}
}

// text places a line that is not a result or a header.
func (g *goTest) text(line string) {
	if strings.HasPrefix(line, "=== RUN ") {
		g.verbose = true
	}
	// A timeout belongs to the package, never to a test that failed before the
	// hang: it starts the package's own failure.
	if strings.HasPrefix(line, "panic: test timed out") {
		g.closeTests()
		g.closeBlock()
		g.orphan, g.timedOut = &Failure{}, true
	}
	if g.block != nil {
		g.block.Line(line)
		return
	}
	if n := len(g.stack); n > 0 {
		top := g.stack[n-1]
		if !g.panic && strings.HasPrefix(line, "panic: ") {
			g.panic = true
		}
		if g.panic || indentOf(line) > top.indent {
			top.f.Line(line)
			return
		}
		g.closeTests()
	}
	if g.orphan == nil {
		g.orphan = &Failure{}
	}
	g.orphan.Line(line)
}

func indentOf(s string) int { return len(s) - len(strings.TrimLeft(s, " \t")) }

// openTest starts a `--- FAIL:` at indent, closing the frames it is not
// nested in and marking the one it is.
func (g *goTest) openTest(name string, indent int) {
	for n := len(g.stack); n > 0 && g.stack[n-1].indent >= indent; n = len(g.stack) {
		g.pop()
	}
	if n := len(g.stack); n > 0 {
		g.stack[n-1].parent = true
	}
	g.panic = false
	g.stack = append(g.stack, &goFrame{f: Failure{Name: name}, indent: indent})
}

func (g *goTest) pop() {
	n := len(g.stack)
	top := g.stack[n-1]
	g.stack = g.stack[:n-1]
	if !top.parent {
		g.add(top.f)
	}
}

func (g *goTest) closeTests() {
	for len(g.stack) > 0 {
		g.pop()
	}
	g.panic = false
}

func (g *goTest) closeBlock() {
	if g.block != nil {
		g.add(*g.block)
		g.block = nil
	}
}

func (g *goTest) add(f Failure) {
	g.fs.Add(f)
	g.sinceResult++
}

// result marks a package result line: what came before it is settled.
func (g *goTest) result() {
	g.closeTests()
	g.closeBlock()
	g.results++
	g.sinceResult = 0
	g.orphan, g.timedOut = nil, false
}

// Claims is true once a package result line in go test's own tab-separated
// shape was seen — "ok  done" from make is not one — and the output is not
// -v, whose failure blocks this parser would leave empty (parser_coverage:
// anything else takes the fallback).
func (g *goTest) Claims() bool { return g.results > 0 && !g.verbose }

func (g *goTest) Result() Result {
	g.closeTests()
	g.closeBlock()
	counts := []string{fmt.Sprintf("%d ok", g.ok)}
	for _, c := range []struct {
		n     int
		label string
	}{{g.failed, "FAIL"}, {g.noTestFiles, "no test files"}, {g.noTests, "no tests to run"}} {
		if c.n > 0 {
			counts = append(counts, fmt.Sprintf("%d %s", c.n, c.label))
		}
	}
	return Result{Runner: "go test", Counts: counts, Failures: g.fs}
}
