package testlane

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// relateTree writes each named file under a fresh directory, standing in for the
// repo a relate pass ran in.
func relateTree(t *testing.T, files ...string) string {
	t.Helper()
	dir := t.TempDir()
	for _, f := range files {
		full := filepath.Join(dir, f)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// TestParseRelateRefusesTheWholeOutput: one bad line takes every good line down
// with it, and the refusal names the line by number and by nothing else.
//
// Line 1 is good in every case, so a parser that returned what it had so far
// fails the empty-Relation check rather than passing for the wrong reason. Every
// bad line carries the marker, so a refusal quoting its line fails too.
func TestParseRelateRefusesTheWholeOutput(t *testing.T) {
	const good = "src/a.ts\tsrc/a.test.ts\n"
	repo := relateTree(t, "src/a.test.ts", "src/b.test.ts", "src/dir/x.ts")
	outside := filepath.Join(filepath.Dir(repo), "MARKER.test.ts")
	asked := []string{"src/a.ts", "src/b.ts"}

	for _, tc := range []struct {
		name, line, reason string
	}{
		{"no tab", "src/b.ts MARKER", "is not <path><TAB><test>"},
		{"a third field", "src/b.ts\tsrc/b.test.ts\tMARKER", "is not <path><TAB><test>"},
		{"empty path", "\tsrc/b.test.ts MARKER", "has an empty field"},
		{"empty test", "src/b.ts\t", "has an empty field"},
		{"blank test", "src/b.ts\t  ", "has an empty field"},
		{"a path nobody asked about", "src/MARKER.ts\tsrc/b.test.ts", "nobody asked about"},
		{"a path outside the tree", "../MARKER.ts\tsrc/b.test.ts", "nobody asked about"},
		{"an absolute test", "src/b.ts\t" + outside, "outside the repository"},
		{"a test up and out", "src/b.ts\t../MARKER.test.ts", "outside the repository"},
		{"a test spelled out of the tree", "src/b.ts\tsrc/../../MARKER.test.ts", "outside the repository"},
		{"a test not in the tree", "src/b.ts\tsrc/MARKER.test.ts", "not in the tree"},
		{"a directory for a test", "src/b.ts\tsrc/dir", "not in the tree"},
		{"the root for a test", "src/b.ts\t.", "not in the tree"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseRelate(repo, asked, good+tc.line+"\n")
			if err == nil {
				t.Fatalf("admitted %q: %+v", tc.line, got)
			}
			if !reflect.DeepEqual(got, Relation{}) {
				t.Errorf("a refusal kept part of the output: %+v", got)
			}
			if !strings.Contains(err.Error(), "line 2 ") {
				t.Errorf("the refusal does not name line 2: %v", err)
			}
			if !strings.Contains(err.Error(), tc.reason) {
				t.Errorf("the refusal gives no reason %q: %v", tc.reason, err)
			}
			if strings.Contains(err.Error(), "MARKER") || strings.Contains(err.Error(), "src/") {
				t.Errorf("the refusal carries relate's text: %v", err)
			}
		})
	}
}

// TestParseRelateRefusesABlankLineMidStream: only the newline ending the output
// is forgiven. A blank line inside it is a line, and it has no tab.
func TestParseRelateRefusesABlankLineMidStream(t *testing.T) {
	repo := relateTree(t, "a.test.ts")
	_, err := ParseRelate(repo, []string{"a.ts"}, "a.ts\ta.test.ts\n\na.ts\ta.test.ts\n")
	if err == nil || !strings.Contains(err.Error(), "line 2 ") {
		t.Errorf("err = %v, want a refusal naming line 2", err)
	}
}

// TestParseRelateMatchesAcrossSpelling: `./` on either side names the same file,
// and Covered is always the asked string, whichever side wore it.
func TestParseRelateMatchesAcrossSpelling(t *testing.T) {
	repo := relateTree(t, "src/a.test.ts")
	for _, tc := range []struct {
		name, asked, listed string
	}{
		{"asked with ./", "./src/a.ts", "src/a.ts"},
		{"listed with ./", "src/a.ts", "./src/a.ts"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseRelate(repo, []string{tc.asked}, tc.listed+"\tsrc/a.test.ts\n")
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got.Covered, []string{tc.asked}) {
				t.Errorf("covered = %v, want the asked %q", got.Covered, tc.asked)
			}
			if len(got.Missed) != 0 {
				t.Errorf("missed = %v, want none", got.Missed)
			}
		})
	}
}

// TestParseRelateOmittedPathIsMissed is c-9 at the parser: a path relate left
// out is no refusal and no cover, so one run can hold both kinds.
func TestParseRelateOmittedPathIsMissed(t *testing.T) {
	repo := relateTree(t, "a.test.ts")
	got, err := ParseRelate(repo, []string{"a.ts", "b.svelte"}, "a.ts\ta.test.ts\n")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Covered, []string{"a.ts"}) {
		t.Errorf("covered = %v, want [a.ts]", got.Covered)
	}
	if !reflect.DeepEqual(got.Missed, []string{"b.svelte"}) {
		t.Errorf("missed = %v, want [b.svelte]", got.Missed)
	}
}

// TestParseRelateKeepsAskedOrderAndFirstSeenTests: Covered follows the question
// and Tests the stream, which is why the stream below runs against both.
func TestParseRelateKeepsAskedOrderAndFirstSeenTests(t *testing.T) {
	repo := relateTree(t, "z.test.ts", "a.test.ts")
	got, err := ParseRelate(repo, []string{"a.ts", "b.ts"},
		"b.ts\tz.test.ts\nb.ts\t./a.test.ts\na.ts\tz.test.ts\na.ts\ta.test.ts\n")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Covered, []string{"a.ts", "b.ts"}) {
		t.Errorf("covered = %v, want asked order", got.Covered)
	}
	if !reflect.DeepEqual(got.Tests, []string{"z.test.ts", "a.test.ts"}) {
		t.Errorf("tests = %v, want distinct and first-seen", got.Tests)
	}
}

// TestParseRelateFoldsRepeatedAskedPaths: one file spelled twice is one answer.
func TestParseRelateFoldsRepeatedAskedPaths(t *testing.T) {
	repo := relateTree(t, "a.test.ts")
	got, err := ParseRelate(repo, []string{"a.ts", "./a.ts", "", "b.ts", "./b.ts"}, "a.ts\ta.test.ts\n")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Covered, []string{"a.ts"}) || !reflect.DeepEqual(got.Missed, []string{"b.ts"}) {
		t.Errorf("covered = %v, missed = %v", got.Covered, got.Missed)
	}
}

// TestParseRelateEmptyOutputIsAllMissed: relate saying nothing is an answer, and
// the run decides what it means. A refusal here would read as relate breaking.
func TestParseRelateEmptyOutputIsAllMissed(t *testing.T) {
	got, err := ParseRelate(t.TempDir(), []string{"a.ts"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Covered) != 0 || len(got.Tests) != 0 {
		t.Errorf("got %+v, want nothing covered", got)
	}
	if !reflect.DeepEqual(got.Missed, []string{"a.ts"}) {
		t.Errorf("missed = %v, want [a.ts]", got.Missed)
	}
}

// TestParseRelateOutOfTreeAskedPathIsNeverCovered: the fence on the path field
// reads the asked set, so an escaping entry in it must not open the door.
func TestParseRelateOutOfTreeAskedPathIsNeverCovered(t *testing.T) {
	repo := relateTree(t, "a.test.ts")
	if _, err := ParseRelate(repo, []string{"../a.ts"}, "../a.ts\ta.test.ts\n"); err == nil {
		t.Error("admitted a line for an asked path outside the tree")
	}
	got, err := ParseRelate(repo, []string{"../a.ts"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Missed, []string{"../a.ts"}) {
		t.Errorf("missed = %v, want the escaping path left uncovered", got.Missed)
	}
}

// TestBatchArgsKeepsOrderAndCollapses: the sort Derive applies would lose the
// order relate gave, and a test named twice would run twice.
func TestBatchArgsKeepsOrderAndCollapses(t *testing.T) {
	got := BatchArgs([]string{"z.test.ts", "a.test.ts", "z.test.ts", "-x.test.ts"})
	want := []string{"z.test.ts", "a.test.ts", "./-x.test.ts"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("args = %v, want %v", got, want)
	}
}
