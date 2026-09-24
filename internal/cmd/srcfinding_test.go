package cmd

import (
	"bufio"
	"bytes"
	"fmt"
	"go/token"
	"regexp"
	"strings"
	"testing"
)

// What a tracing scan's finding must say (c-6). A finding is only useful if the
// person reading a red gate can act on it without re-running the scan in their
// head: WHERE the value escapes, WHERE it came from — the spawn or the field
// read, which can sit packages away — and WHAT to do. Each is asserted as its
// own fact, so a message that loses one says which one it lost.

// execTaintMessage is how a gate reports an exec-output finding.
func execTaintMessage(f taintFinding) string {
	return fmt.Sprintf("%s — %s", f, execTaintRemedy)
}

// messageFact is one thing a finding's message must hold.
type messageFact struct {
	name string
	re   *regexp.Regexp
}

// positionFact matches file:line as a whole position, never as the prefix of a
// longer line number.
func positionFact(name string, p token.Position) messageFact {
	return messageFact{name, regexp.MustCompile(regexp.QuoteMeta(fmt.Sprintf("%s:%d", p.Filename, p.Line)) + `\b`)}
}

// textFact matches a literal phrase.
func textFact(name, text string) messageFact {
	return messageFact{name, regexp.MustCompile(regexp.QuoteMeta(text))}
}

// missingFacts names every fact msg does not hold.
func missingFacts(msg string, facts []messageFact) []string {
	var out []string
	for _, f := range facts {
		if !f.re.MatchString(msg) {
			out = append(out, f.name)
		}
	}
	return out
}

// anchorLine is the position of the line in a fixture source carrying
// "// anchor: <name>". section is the source's name suffix: the txtar section
// ("spawn.go"), or the file itself.
func anchorLine(t *testing.T, fx *ssaFixture, section, name string) token.Position {
	t.Helper()
	want := "// anchor: " + name
	for _, s := range fx.Srcs {
		if !strings.HasSuffix(s.Name, section) {
			continue
		}
		sc := bufio.NewScanner(bytes.NewReader(s.Src))
		for line := 1; sc.Scan(); line++ {
			if strings.HasSuffix(strings.TrimSpace(sc.Text()), want) {
				return token.Position{Filename: s.Name, Line: line}
			}
		}
	}
	t.Fatalf("no %q in fixture source %s", want, section)
	return token.Position{}
}

// assertFacts checks every fact, and proves each one is load-bearing: the
// message with that fact's text cut out must be reported as missing exactly
// that fact.
func assertFacts(t *testing.T, msg string, facts []messageFact) {
	t.Helper()
	for _, name := range missingFacts(msg, facts) {
		t.Errorf("the finding lost its %s: %s", name, msg)
	}
	for _, f := range facts {
		cut := f.re.ReplaceAllString(msg, "")
		if got := missingFacts(cut, facts); len(got) != 1 || got[0] != f.name {
			t.Errorf("cutting the %s out of the message reported %v missing, want exactly [%s]", f.name, got, f.name)
		}
	}
}

// TestTaintFindingNamesEscapeOriginRemedy: the exec-output finding holds the
// escape's file:line, BOTH spawns' file:line in the package two away — a phi
// of two spawns must not collapse to the first — and the remedy.
func TestTaintFindingNamesEscapeOriginRemedy(t *testing.T) {
	fx := loadFixture(t, fixturePath("taint", "one_violation.go.txt"))
	fs := runTaint(fx.srcView, execTaintPolicy())
	if len(fs) != 1 {
		t.Fatalf("the single-violation fixture yields %d findings, want 1: %v", len(fs), fs)
	}
	escape := anchorLine(t, fx, "escape.go", "escape")
	if fs[0].Escape.Filename != escape.Filename || fs[0].Escape.Line != escape.Line {
		t.Errorf("the finding escapes at %s:%d, want %s:%d", fs[0].Escape.Filename, fs[0].Escape.Line, escape.Filename, escape.Line)
	}
	assertFacts(t, execTaintMessage(fs[0]), []messageFact{
		positionFact("escape position", escape),
		positionFact("first spawn's position", anchorLine(t, fx, "spawn.go", "spawn-short")),
		positionFact("second spawn's position", anchorLine(t, fx, "spawn.go", "spawn-long")),
		textFact("remedy's stderr half", "print it to os.Stderr"),
		textFact("remedy's marker half", "or mark the conversion with "+taintClearedMarker+" <reason>"),
	})
}

// TestPathFindingNamesEscapeFieldRemedy: the path-field finding holds the
// escape's file:line, the field it read, and the remedy.
func TestPathFindingNamesEscapeFieldRemedy(t *testing.T) {
	fx := loadFixture(t, fixturePath("pathtaint", "one_violation.go.txt"))
	res := pathTaintScan(fx.srcView, livePathSources(t))
	if len(res.Findings) != 1 {
		t.Fatalf("the single-violation fixture yields %d findings, want 1: %v", len(res.Findings), res.Findings)
	}
	assertFacts(t, res.Findings[0].String(), []messageFact{
		positionFact("escape position", anchorLine(t, fx, "one_violation.go.txt", "escape")),
		textFact("field name", "changes.TaskRecord.Files"),
		textFact("remedy", "construct a pathfence.Contained via pathfence.Contain"),
	})
}
