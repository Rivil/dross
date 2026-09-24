package cmd

import (
	"fmt"
	"go/token"
	"sort"
	"strings"
	"testing"

	"golang.org/x/tools/go/ssa"
)

// The //dross:taint-cleared marker: an in-source declaration, at a
// CONVERSION, that the value produced there no longer carries the stream — a
// SHA or branch name sliced out of output, a count's prose rendering. Same
// grammar as //dross:exec-exempt (the marker_grammar lock; directive_test.go
// holds the one parser), and the same position rule: it binds the line
// IMMEDIATELY below it.
//
// It clears only the tainted values DEFINED on that line, and through them
// their downstream uses. The raw stream on the next line is untouched, and a
// marker can never sit on the stream itself: the spawn's output, the writer
// plugged into Cmd.Stdout, a pipe. Those are sources, not conversions, and a
// marker there would clear everything read from them — so it is a finding of
// its own ("marker on source") and clears nothing.
//
// A marker that clears nothing is a finding too ("orphan marker"): marking
// every line of a file must fail, not pass.

// taintMarker is one parsed marker and what the scan made of it.
type taintMarker struct {
	directive
	file  string
	line  int // the marker's own line
	bound int // the line it binds
	used  bool
}

// execTaintScan is the exec-output policy with markers honoured: the taint
// findings, plus a finding for every marker that is malformed, sits on a
// source, or clears nothing.
func execTaintScan(v *srcView) (taint []taintFinding, markers []taintFinding) {
	byLine := map[string]*taintMarker{}
	var all []*taintMarker
	for _, p := range v.Pkgs {
		for _, f := range p.Syntax {
			file := v.Fset.Position(f.Pos()).Filename
			for bound, d := range directiveMarkers(v.Fset, f, taintClearedMarker) {
				m := &taintMarker{directive: d, file: file, line: v.Fset.Position(d.Pos).Line, bound: bound}
				byLine[fmt.Sprintf("%s:%d", file, bound)] = m
				all = append(all, m)
			}
		}
	}

	pol := execTaintPolicy()
	seeding := false
	seed := pol.Seed
	pol.Seed = func(e *taintEngine, fn *ssa.Function) {
		// No marker clears a SOURCE: while the stream is being introduced,
		// the hook answers no.
		seeding = true
		seed(e, fn)
		seeding = false
	}
	pol.ClearAt = func(pos token.Position) bool {
		if seeding {
			return false
		}
		m := byLine[fmt.Sprintf("%s:%d", pos.Filename, pos.Line)]
		if m == nil || directiveVerdict(map[int]directive{m.bound: m.directive}, m.bound) != "valid" {
			return false
		}
		m.used = true
		return true
	}
	taint = runTaint(v, pol)

	origins := map[string]bool{}
	for _, f := range taint {
		for _, o := range f.Origins {
			origins[fmt.Sprintf("%s:%d", o.Filename, o.Line)] = true
		}
	}
	for _, m := range all {
		at := token.Position{Filename: m.file, Line: m.bound}
		switch verdict := directiveVerdict(map[int]directive{m.bound: m.directive}, m.bound); {
		case verdict == "reasonless":
			markers = append(markers, taintFinding{Escape: at, What: fmt.Sprintf(
				"is bound by a reasonless %s marker at line %d; state what the converted value is and why it no longer carries the stream",
				taintClearedMarker, m.line)})
		case verdict == "short":
			markers = append(markers, taintFinding{Escape: at, What: fmt.Sprintf(
				"is bound by a %s marker at line %d whose reason is %d characters; state in at least %d what the converted value is",
				taintClearedMarker, m.line, len([]rune(m.Reason)), directiveMinReason)})
		case origins[fmt.Sprintf("%s:%d", m.file, m.bound)]:
			markers = append(markers, taintFinding{Escape: at, What: fmt.Sprintf(
				"is a SOURCE line: the %s marker at line %d sits on the stream itself, which no marker clears — "+
					"move it to the line that converts the stream into a value that no longer carries it", taintClearedMarker, m.line)})
		case !m.used:
			markers = append(markers, taintFinding{Escape: at, What: fmt.Sprintf(
				"is bound by an orphan %s marker at line %d: this line defines no tainted value, so the marker clears nothing — delete it",
				taintClearedMarker, m.line)})
		}
	}
	sort.Slice(markers, func(i, j int) bool {
		a, b := markers[i].Escape, markers[j].Escape
		if a.Filename != b.Filename {
			return a.Filename < b.Filename
		}
		return a.Line < b.Line
	})
	return taint, markers
}

// TestTaintMarkerCorpus: position binding, the directive's name, orphans,
// sideways leaks and markers on sources, each pinned in markers.go.txt.
func TestTaintMarkerCorpus(t *testing.T) {
	fx := loadFixture(t, fixturePath("taint", "markers.go.txt"))
	taint, markers := execTaintScan(fx.srcView)
	assertCorpus(t, fx, "taint", taintHits(taint))
	assertCorpus(t, fx, "marker", taintHits(markers))
	for _, m := range markers {
		if !strings.Contains(m.What, taintClearedMarker) {
			t.Errorf("a marker finding does not name the directive: %s", m.What)
		}
	}
}

// TestTaintMarkerGrammar: a marker with no reason, or one too short to say
// anything, is a finding and clears nothing — the same grammar exec-exempt
// holds its prose to.
func TestTaintMarkerGrammar(t *testing.T) {
	for _, tc := range []struct{ marker, want string }{
		{taintClearedMarker, "reasonless"},
		{taintClearedMarker + " a sha", "whose reason is 5 characters"},
	} {
		src := "package g\n\nimport (\n\t\"errors\"\n\t\"os/exec\"\n\t\"strings\"\n)\n\n" +
			"func f(c *exec.Cmd) error {\n\tout, _ := c.Output()\n\t" + tc.marker + "\n" +
			"\tsha := strings.TrimSpace(string(out))\n\treturn errors.New(sha)\n}\n"
		fx, err := typecheckFixture(liveImportable(t), []fixtureSource{{Name: "g.go", Src: []byte(src)}})
		if err != nil {
			t.Fatal(err)
		}
		taint, markers := execTaintScan(fx.srcView)
		if len(markers) != 1 || !strings.Contains(markers[0].What, tc.want) {
			t.Errorf("marker %q: marker findings = %v, want one saying %q", tc.marker, markers, tc.want)
		}
		if len(taint) != 1 || taint[0].Escape.Line != 13 {
			t.Errorf("marker %q cleared the conversion anyway: taint findings = %v", tc.marker, taint)
		}
	}
}
