package runsummary

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// dartHeader heads every dart fixture: feastahead/phone, run locally (helicon
// has no flutter) under FVM's 3.41.6 — 3.44 cannot compile lucide_icons.
const dartHeader = "# captured: flutter@3.41.6 flutter test --reporter "

const (
	dartFoldName  = "/Users/rivil/Development/feastahead/phone/test/core/util/text_fold_test.dart: foldDiacritics (uat-session-7-fixes t-5, S7-02/S7-13) folds Latin accents and lowercases"
	dartShellName = "/Users/rivil/Development/feastahead/phone/test/shared/widgets/app_shell_i18n_test.dart: AppShell renders nav labels in English"
)

// dartSegment is a closed Segment fed a dart fixture — splitter, CR redraws
// and all — through ps.
func dartSegment(t *testing.T, name string, ps ...Parser) *Segment {
	t.Helper()
	s := &Segment{parsers: ps}
	for _, l := range fixtureLines(t, filepath.Join("testdata", "dart", name)) {
		s.Write([]byte(l + "\n"))
	}
	s.Close()
	return s
}

func parseDart(t *testing.T, name string) Result {
	t.Helper()
	d := &dart{}
	dartSegment(t, name, d)
	if !d.Claims() {
		t.Fatalf("%s is not claimed by the dart parser", name)
	}
	return d.Result()
}

// TestDartReportersAgree: reporter_formats — compact (CR redraws) and
// expanded give the same failures and counts for the same run.
func TestDartReportersAgree(t *testing.T) {
	for _, pair := range [][2]string{{"compact_red.txt", "expanded_red.txt"}, {"compact_green.txt", "expanded_green.txt"}} {
		c, e := parseDart(t, pair[0]), parseDart(t, pair[1])
		if !reflect.DeepEqual(names(c), names(e)) || !reflect.DeepEqual(c.Counts, e.Counts) || c.Failures.Total != e.Failures.Total {
			t.Errorf("%s and %s disagree:\ncompact  %q %q\nexpanded %q %q", pair[0], pair[1], c.Counts, names(c), e.Counts, names(e))
		}
	}
}

// TestDartGreenCounts: the counts are the runner's own final counter.
func TestDartGreenCounts(t *testing.T) {
	for _, f := range []string{"compact_green.txt", "expanded_green.txt"} {
		r := parseDart(t, f)
		if !reflect.DeepEqual(r.Counts, []string{"+22"}) || r.Failures.Total != 0 || r.Runner != "dart test" {
			t.Errorf("%s: runner=%q counts=%q failures=%q, want dart test +22 and none", f, r.Runner, r.Counts, names(r))
		}
	}
	out := Render(Outcome{Exit: 0, Ran: true}, []*Segment{dartSegment(t, "compact_green.txt", &dart{})}, &Budget{}, logPath, time.Second)
	if !strings.HasPrefix(out, "PASS · dart test: +22 · ") {
		t.Fatalf("green rendered:\n%s", out)
	}
	if r := parseDart(t, "expanded_red.txt"); !reflect.DeepEqual(r.Counts, []string{"+20 -2"}) {
		t.Fatalf("red counts %q, want the final counter +20 -2", r.Counts)
	}
}

// TestDartRedNames: names carry neither the counter nor the [E] marker.
func TestDartRedNames(t *testing.T) {
	for _, f := range []string{"compact_red.txt", "expanded_red.txt"} {
		r := parseDart(t, f)
		if got := names(r); !reflect.DeepEqual(got, []string{dartFoldName, dartShellName}) {
			t.Errorf("%s: names %q", f, got)
		}
		for _, n := range names(r) {
			if strings.Contains(n, "[E]") || dartProgress.MatchString(n) || strings.Contains(n, "+1") {
				t.Errorf("%s: name %q keeps runner noise", f, n)
			}
		}
	}
}

// TestDartRedOwnLines: each block holds its own Expected:/Actual: lines — the
// widget test's from the exception block printed before its [E] line.
func TestDartRedOwnLines(t *testing.T) {
	own := map[string][]string{
		dartFoldName:  {"Expected: 'sauteedX'", "Actual: 'sauteed'"},
		dartShellName: {"Expected: exactly one matching candidate", `Found 0 widgets with text "Homeward"`, "Test failed. See exception logs above."},
	}
	for _, f := range []string{"compact_red.txt", "expanded_red.txt"} {
		r := parseDart(t, f)
		for name, wants := range own {
			joined := strings.Join(failureNamed(t, r, name).Lines, "\n")
			for _, w := range wants {
				if !strings.Contains(joined, w) {
					t.Errorf("%s: %s lacks %q:\n%s", f, name, w, joined)
				}
			}
			for other, ows := range own {
				if other != name && strings.Contains(joined, ows[0]) {
					t.Errorf("%s: %s carries %s's %q", f, name, other, ows[0])
				}
			}
			if strings.Contains(joined, "To run this test again") {
				t.Errorf("%s: %s carries the rerun pointer", f, name)
			}
		}
	}
}

// TestDartBlockEnds: the next progress line closes a block — it never carries
// a following test's progress line.
func TestDartBlockEnds(t *testing.T) {
	for _, f := range []string{"compact_red.txt", "expanded_red.txt"} {
		r := parseDart(t, f)
		for _, fl := range r.Failures.Kept {
			for _, l := range fl.Lines {
				if dartProgress.MatchString(l) {
					t.Errorf("%s: %s holds the progress line %q", f, fl.Name, l)
				}
			}
		}
		if len(failureNamed(t, r, dartFoldName).Lines) > 8 {
			t.Errorf("%s: the fold block ran past its stack: %q", f, failureNamed(t, r, dartFoldName).Lines)
		}
	}
}

// TestDartLoadError: a file that fails to load is one failure named after it.
func TestDartLoadError(t *testing.T) {
	r := parseDart(t, "load_error.txt")
	want := "/Users/rivil/Development/feastahead/phone/test/core/util/text_fold_test.dart"
	if got := names(r); !reflect.DeepEqual(got, []string{want}) || r.Failures.Total != 1 {
		t.Fatalf("names %q total %d, want the one file", got, r.Failures.Total)
	}
	if joined := strings.Join(r.Failures.Kept[0].Lines, "\n"); !strings.Contains(joined, "Error: Expected ';' after this.") {
		t.Fatalf("the load block lacks the compiler error:\n%s", joined)
	}
}

// TestDartLongExceptionCut: an exception block longer than BlockLines is cut
// with More > 0, never dropped. The live widget block, padded with more stack
// frames, stands in for a deep widget tree.
func TestDartLongExceptionCut(t *testing.T) {
	d := &dart{}
	for _, l := range fixtureLines(t, filepath.Join("testdata", "dart", "expanded_red.txt")) {
		l = clean(l)
		if strings.HasPrefix(l, "<asynchronous suspension>") {
			for i := 0; i < BlockLines; i++ {
				d.Feed("#9      Element.rebuild (package:flutter/src/widgets/framework.dart:5000:7)")
			}
		}
		d.Feed(l)
	}
	if !d.Claims() {
		t.Fatal("the padded run is not claimed")
	}
	f := failureNamed(t, d.Result(), dartShellName)
	if f.More == 0 || len(f.Lines) == 0 || !strings.HasPrefix(f.Lines[0], "══╡ EXCEPTION CAUGHT") {
		t.Fatalf("lines=%d more=%d first=%q, want a cut block opening on the exception", len(f.Lines), f.More, f.Lines)
	}
}

// TestDartTruncatedNotClaimed: a running counter is not a finished run.
func TestDartTruncatedNotClaimed(t *testing.T) {
	for _, f := range []string{"compact_red.txt", "expanded_red.txt"} {
		lines := fixtureLines(t, filepath.Join("testdata", "dart", f))
		d := &dart{}
		dartSegment(t, f, d) // sanity: the whole fixture is claimed
		if !d.Claims() {
			t.Fatalf("%s is not claimed whole", f)
		}
		cut := &dart{}
		s := &Segment{parsers: []Parser{cut}}
		for _, l := range lines[:len(lines)-1] {
			s.Write([]byte(l + "\n"))
		}
		// The terminal line's last frames, minus the terminal one.
		last := lines[len(lines)-1]
		if i := strings.LastIndexByte(last, '\r'); i >= 0 {
			s.Write([]byte(last[:i] + "\n"))
		}
		s.Close()
		if cut.Claims() {
			t.Errorf("%s cut before its terminal line is claimed", f)
		}
	}
}

// TestDartFixturesCaptured: every dart fixture is a live feastahead run.
func TestDartFixturesCaptured(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join("testdata", "dart", "*.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) < 5 {
		t.Fatalf("%d dart fixtures, want the 5 this parser is tested against", len(paths))
	}
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		first, _, _ := strings.Cut(string(b), "\n")
		r := strings.TrimSuffix(strings.SplitN(filepath.Base(p), "_", 2)[0], ".txt")
		if r == "load" {
			r = "compact"
		}
		if !strings.HasPrefix(first, dartHeader+r+" (feastahead@") {
			t.Errorf("%s does not start with %q", p, dartHeader+r+" (feastahead@")
		}
		if !strings.Contains(filepath.Base(p), "green") && !strings.Contains(first, " break: ") {
			t.Errorf("%s is red but its header names no break", p)
		}
	}
}
