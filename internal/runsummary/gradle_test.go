package runsummary

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

// gradleHeader heads every gradle fixture: equerry's test_command, run in
// equerry under openjdk@21 (gradle 8.13 rejects the default JDK 26).
const gradleHeader = "# captured: gradle@8.13 ./gradlew testDebugUnitTest (equerry@"

// gradleSegment is a closed Segment fed a gradle fixture through ps.
func gradleSegment(t *testing.T, name string, ps ...Parser) *Segment {
	t.Helper()
	s := &Segment{parsers: ps}
	for _, l := range fixtureLines(t, filepath.Join("testdata", "gradle", name)) {
		s.Write([]byte(l + "\n"))
	}
	s.Close()
	return s
}

func parseGradle(t *testing.T, name string) Result {
	t.Helper()
	g := &gradle{}
	gradleSegment(t, name, g)
	if !g.Claims() {
		t.Fatalf("%s is not claimed by the gradle parser", name)
	}
	return g.Result()
}

// TestGradleGreenCounts: gradle_green_counts — green is gradle's own outcome
// and task lines, never a made-up test count.
func TestGradleGreenCounts(t *testing.T) {
	r := parseGradle(t, "green.txt")
	want := []string{"BUILD SUCCESSFUL", "37 actionable tasks: 1 executed, 36 up-to-date"}
	if !reflect.DeepEqual(r.Counts, want) {
		t.Fatalf("counts %q, want %q", r.Counts, want)
	}
	for _, c := range r.Counts {
		if strings.Contains(c, "test") {
			t.Errorf("green count %q names tests gradle never counted", c)
		}
	}
	if r.Failures.Total != 0 || r.Runner != "gradle" {
		t.Fatalf("runner=%q failures=%q, want gradle with none", r.Runner, names(r))
	}
	out := Render(Outcome{Exit: 0, Ran: true}, []*Segment{gradleSegment(t, "green.txt", &gradle{})}, &Budget{}, logPath, time.Second)
	if !strings.HasPrefix(out, "PASS · gradle: BUILD SUCCESSFUL, 37 actionable tasks") {
		t.Fatalf("green rendered:\n%s", out)
	}
}

// TestGradleRedNames: each "Class > method FAILED" is one failure named
// Class.method, a backtick name with spaces whole; the task's own FAILED line
// and the epilogue open none.
func TestGradleRedNames(t *testing.T) {
	r := parseGradle(t, "red.txt")
	want := []string{
		"DefaultAssistantDetectorTest.not default when a different OEM component holds the slot",
		"SpeakChunkerTest.sentence_mode_full_sequence_matches_the_expected_utterances",
	}
	if got := names(r); !reflect.DeepEqual(got, want) {
		t.Fatalf("names %q, want %q", got, want)
	}
	if r.Failures.Total != 2 {
		t.Fatalf("%d failures, want 2", r.Failures.Total)
	}
}

// TestGradleNestedClassName: a JUnit 5 @Nested class prints one more " > "
// per level; every level joins the name with a dot, as a plain class's does.
func TestGradleNestedClassName(t *testing.T) {
	g := &gradle{}
	for _, l := range []string{
		"OuterTest > Inner > method() FAILED",
		"    org.opentest4j.AssertionFailedError at OuterTest.kt:12",
		"",
		"OuterTest > Inner > Deeper > other() FAILED",
		"    org.opentest4j.AssertionFailedError at OuterTest.kt:20",
		"",
		"2 tests completed, 2 failed",
		"",
		"BUILD FAILED in 2s",
	} {
		g.Feed(l)
	}
	if !g.Claims() {
		t.Fatal("the run is not claimed")
	}
	r := g.Result()
	if got, want := names(r), []string{"OuterTest.Inner.method()", "OuterTest.Inner.Deeper.other()"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("names %q, want %q", got, want)
	}
	if !strings.Contains(strings.Join(failureNamed(t, r, "OuterTest.Inner.method()").Lines, "\n"), "OuterTest.kt:12") {
		t.Fatalf("the nested failure lost its own lines: %q", r.Failures.Kept[0].Lines)
	}
}

// TestGradleRedOwnLines: a block holds its own assertion line and none of the
// other test's, nor the console noise printed between them.
func TestGradleRedOwnLines(t *testing.T) {
	r := parseGradle(t, "red.txt")
	own := map[string]string{
		"DefaultAssistantDetectorTest.not default when a different OEM component holds the slot": "AssertionError at DefaultAssistantDetectorTest.kt:43",
		"SpeakChunkerTest.sentence_mode_full_sequence_matches_the_expected_utterances":           "AssertionError at SpeakChunkerTest.kt:29",
	}
	for name, line := range own {
		joined := strings.Join(failureNamed(t, r, name).Lines, "\n")
		if !strings.Contains(joined, line) {
			t.Errorf("%s lacks its own %q:\n%s", name, line, joined)
		}
		for other, ol := range own {
			if other != name && strings.Contains(joined, ol) {
				t.Errorf("%s carries %s's %q", name, other, ol)
			}
		}
		for _, noise := range []string{"Failed to create image decoder", "> Task", "What went wrong"} {
			if strings.Contains(joined, noise) {
				t.Errorf("%s carries %q:\n%s", name, noise, joined)
			}
		}
	}
}

// TestGradleRedCounts: gradle's own red count is kept verbatim.
func TestGradleRedCounts(t *testing.T) {
	r := parseGradle(t, "red.txt")
	joined := strings.Join(r.Counts, " | ")
	for _, want := range []string{"BUILD FAILED", "324 tests completed, 2 failed", "37 actionable tasks: 4 executed, 33 up-to-date"} {
		if !strings.Contains(joined, want) {
			t.Errorf("counts %q lack %q", r.Counts, want)
		}
	}
	out := Render(Outcome{Exit: 1, Ran: true}, []*Segment{gradleSegment(t, "red.txt", &gradle{})}, &Budget{}, logPath, time.Second)
	if !strings.HasPrefix(out, "FAIL (exit 1) · gradle: BUILD FAILED, 324 tests completed, 2 failed") || !strings.Contains(out, "· 2 failing ·") {
		t.Fatalf("red rendered:\n%s", out)
	}
}

// TestGradleCompileFailedNeverGreen: a compile break is BUILD FAILED with no
// failing test — never green, never "0 failing"; the fallback tail shows why.
func TestGradleCompileFailedNeverGreen(t *testing.T) {
	seg := NewSegment()
	for _, l := range fixtureLines(t, filepath.Join("testdata", "gradle", "compile_failed.txt")) {
		seg.Write([]byte(l + "\n"))
	}
	seg.Close()
	out := Render(Outcome{Exit: 1, Ran: true}, []*Segment{seg}, &Budget{}, logPath, time.Second)
	if !strings.HasPrefix(out, "exit 1 · no failing test reported") {
		t.Fatalf("compile break rendered:\n%s", out)
	}
	if !strings.Contains(out, "What went wrong") || !strings.Contains(out, "Unresolved reference 'undefinedSymbol'") {
		t.Fatalf("the fallback tail lacks gradle's reason:\n%s", out)
	}
	for _, l := range outLines(out) {
		if strings.HasPrefix(l, "PASS") || strings.HasPrefix(l, "FAIL (") {
			t.Fatalf("compile break printed %q", l)
		}
	}
}

// TestGradleTruncatedNotClaimed: output cut before gradle's BUILD line is not
// a run gradle finished, so it is not summarised.
func TestGradleTruncatedNotClaimed(t *testing.T) {
	g := &gradle{}
	for _, l := range fixtureLines(t, filepath.Join("testdata", "gradle", "red.txt")) {
		if strings.HasPrefix(l, "BUILD FAILED") {
			break
		}
		g.Feed(l)
	}
	if g.Claims() {
		t.Fatal("gradle output cut before BUILD FAILED is claimed")
	}
}

// TestGradleReadsNoFiles: gradle_green_counts — the parser reads the stream
// only, never JUnit XML or anything else on disk.
func TestGradleReadsNoFiles(t *testing.T) {
	f, err := parser.ParseFile(token.NewFileSet(), "gradle.go", nil, parser.ImportsOnly)
	if err != nil {
		t.Fatal(err)
	}
	banned := map[string]bool{"os": true, "io/fs": true, "path/filepath": true, "encoding/xml": true}
	for _, imp := range f.Imports {
		p, _ := strconv.Unquote(imp.Path.Value)
		if banned[p] {
			t.Errorf("gradle.go imports %s", p)
		}
	}
}

// TestGradleFixturesCaptured: every gradle fixture is a live equerry run, and
// a red one names the break that produced it.
func TestGradleFixturesCaptured(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join("testdata", "gradle", "*.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) < 3 {
		t.Fatalf("%d gradle fixtures, want the 3 this parser is tested against", len(paths))
	}
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		first, _, _ := strings.Cut(string(b), "\n")
		if !strings.HasPrefix(first, gradleHeader) || !strings.Contains(first, ")") {
			t.Errorf("%s does not start with %q<sha>)", p, gradleHeader)
		}
		if filepath.Base(p) != "green.txt" && !strings.Contains(first, " break: ") {
			t.Errorf("%s is red but its header names no break", p)
		}
	}
}
