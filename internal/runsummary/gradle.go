package runsummary

import (
	"regexp"
	"strings"
)

func init() {
	Register(func() Parser { return &gradle{} })
}

// gradle parses a Gradle test task's plain console output, as gradle prints it
// off a terminal. Its counts are gradle's own: the build outcome ("BUILD
// SUCCESSFUL"), the "N actionable tasks: …" line and, when gradle printed one,
// "N tests completed, M failed". Without testLogging gradle prints no count on
// green, and none is made up: no JUnit XML is read and no init script injected
// (gradle_green_counts). Each unindented "Class > method FAILED" header is one
// failure named Class.method — a JUnit 5 nested class's "Outer > Inner >
// method() FAILED" is Outer.Inner.method() — holding the indented lines under
// it; the "> Task … FAILED" line and the "* What went wrong:" epilogue open
// none. It claims a segment only once a "BUILD … in" line has printed: a run
// cut short is not summarised. A compile break prints BUILD FAILED with no
// failing test, which Render turns into the fallback tail.
type gradle struct {
	outcome string
	tasks   string
	tests   []string
	fs      Failures
	cur     *blockLines
}

var (
	gradleOutcome = regexp.MustCompile(`^(BUILD (?:SUCCESSFUL|FAILED)) in \d`)
	gradleTasks   = regexp.MustCompile(`^\d+ actionable tasks?: .*\S`)
	gradleTests   = regexp.MustCompile(`^\d+ tests? completed(?:, \d+ (?:failed|skipped))*\s*$`)
	gradleFailed  = regexp.MustCompile(`^(\S.*?) > (.+) FAILED\s*$`)
)

func (g *gradle) Feed(line string) {
	if g.cur != nil && (line == "" || line[0] == ' ' || line[0] == '\t') {
		g.cur.line(line)
		return
	}
	g.close()
	switch {
	case gradleOutcome.MatchString(line):
		// A later BUILD SUCCESSFUL in the same segment never hides an earlier
		// failed build.
		if g.outcome != "BUILD FAILED" {
			g.outcome = gradleOutcome.FindStringSubmatch(line)[1]
		}
	case gradleTasks.MatchString(line):
		g.tasks = keep(strings.TrimSpace(line))
	case gradleTests.MatchString(line):
		if len(g.tests) < MaxBlocks {
			g.tests = append(g.tests, keep(strings.TrimSpace(line)))
		}
	case strings.HasPrefix(line, "> "):
		// "> Task :app:testDebugUnitTest FAILED", "> There were failing tests".
	case gradleFailed.MatchString(line):
		m := gradleFailed.FindStringSubmatch(line)
		g.cur = &blockLines{f: Failure{Name: m[1] + "." + strings.ReplaceAll(m[2], " > ", ".")}}
	}
}

func (g *gradle) close() {
	if g.cur != nil {
		g.cur.addTo(&g.fs)
		g.cur = nil
	}
}

func (g *gradle) Claims() bool { return g.outcome != "" }

func (g *gradle) Result() Result {
	g.close()
	counts := []string{g.outcome}
	counts = append(counts, g.tests...)
	if g.tasks != "" {
		counts = append(counts, g.tasks)
	}
	return Result{Runner: "gradle", Counts: counts, Failures: g.fs}
}
