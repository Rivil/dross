package cmd

import (
	"bufio"
	"bytes"
	"fmt"
	"go/ast"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// The WANT corpus harness. A source scan's fixtures carry their expected
// answers inline: a line ending in `// WANT <scan>` must produce a finding from
// that scan, and a line without one must not. The answer key sits next to the
// code it judges, so a fixture edit that moves a line moves its expectation too.
//
// The comparison runs both ways — a MISSED WANT and an UNEXPECTED finding are
// each a failure — and the harness counts annotations twice, once parsed and
// once as raw text, so a parser that silently dropped one cannot agree with
// itself.

// wantMarker introduces an expected finding. The scan name follows it,
// separated by whitespace. Every occurrence must be well-formed: a bare or
// glued marker is an error, never a line that silently asserts nothing.
const wantMarker = "// WANT"

// corpusHit is one finding a scan reported, reduced to where it points.
type corpusHit struct {
	File string // the fixture source's Name, as positions print it
	Line int
	Msg  string
}

// corpusWant is one parsed annotation.
type corpusWant struct {
	File string
	Line int
	Scan string
}

func (w corpusWant) at() string { return fmt.Sprintf("%s:%d", w.File, w.Line) }

// parseWants reads every `// WANT <scan>` annotation in the sources.
func parseWants(srcs []fixtureSource) ([]corpusWant, error) {
	var out []corpusWant
	for _, s := range srcs {
		sc := bufio.NewScanner(bytes.NewReader(s.Src))
		for line := 1; sc.Scan(); line++ {
			_, rest, ok := strings.Cut(sc.Text(), wantMarker)
			if !ok {
				continue
			}
			fields := strings.Fields(rest)
			if len(fields) != 1 || !(strings.HasPrefix(rest, " ") || strings.HasPrefix(rest, "\t")) {
				return nil, fmt.Errorf("%s:%d: %q must be %s followed by exactly one scan name", s.Name, line, strings.TrimSpace(wantMarker+rest), wantMarker)
			}
			out = append(out, corpusWant{File: s.Name, Line: line, Scan: fields[0]})
		}
		if err := sc.Err(); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// rawWantCount is the independent count: the marker as text, however the
// parser treats it.
func rawWantCount(srcs []fixtureSource) int {
	n := 0
	for _, s := range srcs {
		n += bytes.Count(s.Src, []byte(wantMarker))
	}
	return n
}

// wantCountProblem is the parsed-versus-raw guard: a parser that dropped an
// annotation disagrees with the text it read.
func wantCountProblem(parsed, raw int) string {
	if parsed == raw {
		return ""
	}
	return fmt.Sprintf("parsed %d WANT annotations but the sources hold %d — the parser is dropping some", parsed, raw)
}

// checkCorpus compares one scan's findings against the annotations for that
// scan and returns every disagreement. Annotations for other scans are
// ignored; several findings on one line count as that line's one finding.
func checkCorpus(srcs []fixtureSource, scan string, hits []corpusHit) []string {
	wants, err := parseWants(srcs)
	if err != nil {
		return []string{err.Error()}
	}
	var problems []string
	if p := wantCountProblem(len(wants), rawWantCount(srcs)); p != "" {
		problems = append(problems, p)
	}
	want := map[string]bool{}
	for _, w := range wants {
		if w.Scan == scan {
			want[w.at()] = true
		}
	}
	got := map[string][]string{}
	for _, h := range hits {
		at := fmt.Sprintf("%s:%d", h.File, h.Line)
		got[at] = append(got[at], h.Msg)
	}
	for at := range want {
		if _, ok := got[at]; !ok {
			problems = append(problems, fmt.Sprintf("MISSED WANT %s (%s): the scan reported nothing on this line", at, scan))
		}
	}
	for at, msgs := range got {
		if !want[at] {
			problems = append(problems, fmt.Sprintf("UNEXPECTED finding %s (%s): %s", at, scan, strings.Join(msgs, " | ")))
		}
	}
	sort.Strings(problems)
	return problems
}

// assertCorpus fails t with every disagreement between the scan and the
// fixture's annotations, and requires the fixture to hold at least one
// annotation for the scan — a corpus with no expectations proves nothing.
func assertCorpus(t *testing.T, fx *ssaFixture, scan string, hits []corpusHit) {
	t.Helper()
	wants, err := parseWants(fx.Srcs)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, w := range wants {
		if w.Scan == scan {
			n++
		}
	}
	if n == 0 {
		t.Errorf("the fixture holds no %s %s annotation — nothing is asserted", wantMarker, scan)
	}
	for _, p := range checkCorpus(fx.Srcs, scan, hits) {
		t.Error(p)
	}
}

// corpusFiles lists a corpus directory's fixture files, sorted. An empty
// directory is an error: a corpus that lost its files would otherwise assert
// nothing and pass.
func corpusFiles(dir string) ([]string, error) {
	paths, err := filepath.Glob(filepath.Join(dir, "*.go.txt"))
	if err != nil {
		return nil, err
	}
	var out []string
	for _, p := range paths {
		if !isFixtureTestFile(filepath.Base(p)) {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("corpus %s holds no .go.txt fixtures", dir)
	}
	sort.Strings(out)
	return out, nil
}

// stubFlagScan is the harness's own scanner: every call to a function named
// flag is a finding. It exists so the harness can be tested apart from any
// real scan.
func stubFlagScan(fx *ssaFixture) []corpusHit {
	var hits []corpusHit
	for _, p := range fx.Pkgs {
		for _, f := range p.Syntax {
			ast.Inspect(f, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				if id, ok := call.Fun.(*ast.Ident); ok && id.Name == "flag" {
					pos := fx.Fset.Position(call.Pos())
					hits = append(hits, corpusHit{File: pos.Filename, Line: pos.Line, Msg: "flag() called"})
				}
				return true
			})
		}
	}
	return hits
}

// TestCorpusHarnessReportsBothDirections: want_mismatch.go.txt holds one
// matched annotation, one finding without a WANT and one WANT without a
// finding. Both mismatches must come back, and nothing else.
func TestCorpusHarnessReportsBothDirections(t *testing.T) {
	fx := loadFixture(t, fixturePath("ssa_fixture", "want_mismatch.go.txt"))
	problems := checkCorpus(fx.Srcs, "stub", stubFlagScan(fx))
	want := []string{
		"MISSED WANT want_mismatch.go.txt:12 (stub)",
		"UNEXPECTED finding want_mismatch.go.txt:11 (stub)",
	}
	if len(problems) != len(want) {
		t.Fatalf("got %d problems, want %d:\n%s", len(problems), len(want), strings.Join(problems, "\n"))
	}
	for i, w := range want {
		if !strings.HasPrefix(problems[i], w) {
			t.Errorf("problem %d = %q, want it to start %q", i, problems[i], w)
		}
	}
	// Annotations for another scan are not this scan's expectations.
	if p := checkCorpus(fx.Srcs, "other", nil); len(p) != 0 {
		t.Errorf("a scan with no annotations and no findings reported %v", p)
	}
}

// TestCorpusHarnessCountsAnnotationsTwice: a parse that loses an annotation —
// here, the last one — disagrees with the raw count and is reported.
func TestCorpusHarnessCountsAnnotationsTwice(t *testing.T) {
	srcs := []fixtureSource{{Name: "x.go.txt", Src: []byte("package x\n\nvar a = 1 // WANT stub\nvar b = 2 // WANT stub")}}
	wants, err := parseWants(srcs)
	if err != nil {
		t.Fatal(err)
	}
	if len(wants) != 2 || rawWantCount(srcs) != 2 {
		t.Fatalf("parsed %d / raw %d annotations, want 2 / 2 — the last line has no newline", len(wants), rawWantCount(srcs))
	}
	// An annotation the parser cannot read is an error, not a skip.
	for _, line := range []string{"// WANT", "// WANTstub", "// WANT stub other"} {
		bad := []fixtureSource{{Name: "y.go.txt", Src: []byte("package y\n\nvar a = 1 " + line + "\n")}}
		if p := checkCorpus(bad, "stub", nil); len(p) != 1 || !strings.Contains(p[0], "y.go.txt:3") {
			t.Errorf("annotation %q gave %v, want one error naming y.go.txt:3", line, p)
		}
	}
	// The raw-versus-parsed guard itself.
	if p := wantCountProblem(1, 2); !strings.Contains(p, "parsed 1") || !strings.Contains(p, "hold 2") {
		t.Errorf("wantCountProblem(1, 2) = %q, want it to name both counts", p)
	}
	if p := wantCountProblem(2, 2); p != "" {
		t.Errorf("wantCountProblem(2, 2) = %q, want none", p)
	}
}

// TestEmptyCorpusFails: a corpus directory with no fixtures is an error.
func TestEmptyCorpusFails(t *testing.T) {
	dir := t.TempDir()
	if _, err := corpusFiles(dir); err == nil {
		t.Error("an empty corpus directory listed without error")
	}
	mustWrite(t, filepath.Join(dir, "only_test.go.txt"), "package x\n")
	if _, err := corpusFiles(dir); err == nil {
		t.Error("a corpus holding only a _test.go.txt listed without error")
	}
	files, err := corpusFiles(filepath.Join("testdata", "ssa_fixture"))
	if err != nil || len(files) != 5 {
		t.Errorf("corpusFiles(testdata/ssa_fixture) = %d files, %v; want 5", len(files), err)
	}
}
