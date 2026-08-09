package verify

import (
	"reflect"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/mutation"
)

// TestScopeNormalisationCollapsesSpellings pins the premise the whole filter
// rests on: the four shapes a path arrives in — changes.json's repo-relative
// form, a "./" prefixed one, an absolute one, and a Windows-produced report's
// backslashes — are ONE file. If they don't collapse, a mutant in a touched
// file reads as out-of-scope and the phase's own survivor is filtered away.
func TestScopeNormalisationCollapsesSpellings(t *testing.T) {
	spellings := []string{
		"./internal/a.go",
		"internal/a.go",
		"/repo/internal/a.go",
		`internal\a.go`,
	}
	s := NewScope(ScopeInput{Root: "/repo", Git: spellings})

	if want := []string{"internal/a.go"}; !reflect.DeepEqual(s.Files, want) {
		t.Errorf("spellings did not collapse: got %v want %v", s.Files, want)
	}
	for _, p := range spellings {
		if !s.Contains(p) {
			t.Errorf("Contains(%q) = false", p)
		}
	}
}

// TestScopeContainsAfterReindex proves Contains survives a scope whose lookup
// index was never built — the state a Scope decoded from tests.json is in.
func TestScopeContainsAfterReindex(t *testing.T) {
	s := &Scope{Root: "/repo", Files: []string{"internal/a.go"}}
	if !s.Contains("./internal/a.go") {
		t.Error("a scope with no prebuilt index must still answer Contains")
	}
	if s.Contains("internal/b.go") {
		t.Error("Contains matched a file not in the set")
	}
}

// TestParseHunksArithmetic pins the new-side range maths. The exclusive-end
// off-by-one is the specific failure being nailed down: `+11,3` covers lines
// 11, 12 AND 13, so a mutant on 13 is in-hunk. An implementation that computes
// start+count gets 14 and quietly reads line 13 as untouched.
func TestParseHunksArithmetic(t *testing.T) {
	cases := []struct {
		name   string
		header string
		want   []Range
	}{
		{"omitted count means one line", "@@ -1,0 +5 @@", []Range{{5, 5}}},
		{"three lines from seven", "@@ -3,2 +7,3 @@", []Range{{7, 9}}},
		{"inclusive end includes 13", "@@ -10,0 +11,3 @@", []Range{{11, 13}}},
		{"single line with explicit count", "@@ -1,1 +1,1 @@", []Range{{1, 1}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			hunks, degraded := ParseHunks("+++ b/a.go\n" + tc.header + " func x()\n")
			if len(degraded) != 0 {
				t.Fatalf("unexpected degraded entries: %v", degraded)
			}
			if !reflect.DeepEqual(hunks["a.go"], tc.want) {
				t.Errorf("ranges: got %v want %v", hunks["a.go"], tc.want)
			}
		})
	}
}

// TestParseHunksPureDeletion: a hunk that removes lines and adds none has no
// new-side extent, so it contributes no range. The file is still in scope
// (the file set is separate) — its mutants simply classify as inherited
// instead of crashing on, or matching, a zero-length range.
func TestParseHunksPureDeletion(t *testing.T) {
	hunks, degraded := ParseHunks("+++ b/a.go\n@@ -4,2 +6,0 @@\n")
	if len(degraded) != 0 {
		t.Fatalf("a pure deletion is not a degradation: %v", degraded)
	}
	if len(hunks["a.go"]) != 0 {
		t.Fatalf("deletion contributed a range: %v", hunks["a.go"])
	}

	s := NewScope(ScopeInput{Git: []string{"a.go"}, Hunks: hunks})
	if !s.Contains("a.go") {
		t.Error("a file whose only change was a deletion is still in scope")
	}
	if got := s.Origin("a.go", 6); got != mutation.OriginInherited {
		t.Errorf("origin over a deletion-only file = %q want %q", got, mutation.OriginInherited)
	}
}

// TestScopeOriginDefaultsToProximity: a line inside a file the phase touched
// but outside every changed range is INHERITED, not in-hunk. Defaulting the
// other way would promote every survivor in a hunk-less (degraded) run to the
// strongest evidence tier — exactly when the evidence is weakest.
func TestScopeOriginDefaultsToProximity(t *testing.T) {
	s := NewScope(ScopeInput{
		Git:   []string{"a.go"},
		Hunks: map[string][]Range{"a.go": {{Start: 10, End: 12}}},
	})
	if got := s.Origin("a.go", 11); got != mutation.OriginInHunk {
		t.Errorf("line inside the hunk = %q want %q", got, mutation.OriginInHunk)
	}
	for _, line := range []int{9, 13, 400} {
		if got := s.Origin("a.go", line); got != mutation.OriginInherited {
			t.Errorf("line %d outside every range = %q want %q", line, got, mutation.OriginInherited)
		}
	}
	// No hunk data at all is the same conservative answer, not a crash.
	bare := NewScope(ScopeInput{Git: []string{"a.go"}})
	if got := bare.Origin("a.go", 1); got != mutation.OriginInherited {
		t.Errorf("origin with no hunk data = %q want %q", got, mutation.OriginInherited)
	}
}

// TestParseHunksMalformedHeaderDegrades: one unreadable header must cost one
// hunk, not the whole diff. The ranges parsed before it are kept and the
// degradation is recorded naming the file it happened in.
func TestParseHunksMalformedHeaderDegrades(t *testing.T) {
	diff := strings.Join([]string{
		"+++ b/a.go",
		"@@ -1,0 +5,2 @@",
		"@@ this is not a hunk header @@",
		"+++ b/b.go",
		"@@ -1,0 +3,1 @@",
	}, "\n")

	hunks, degraded := ParseHunks(diff)
	if !reflect.DeepEqual(hunks["a.go"], []Range{{5, 6}}) {
		t.Errorf("ranges before the bad header were discarded: %v", hunks["a.go"])
	}
	if !reflect.DeepEqual(hunks["b.go"], []Range{{3, 3}}) {
		t.Errorf("parsing stopped at the bad header: %v", hunks["b.go"])
	}
	if len(degraded) != 1 {
		t.Fatalf("expected exactly one degraded entry, got %v", degraded)
	}
	if !strings.Contains(degraded[0], "a.go") {
		t.Errorf("degraded entry must name the file it happened in: %q", degraded[0])
	}
}

// TestNewScopeUnionFailsOpen: the two sides are unioned, never intersected.
// An intersecting build returns the narrower set — and the narrowest possible
// scope is the one that gates nothing, which is the vacuous pass this whole
// phase exists to make unreachable.
func TestNewScopeUnionFailsOpen(t *testing.T) {
	s := NewScope(ScopeInput{
		Git:      []string{"a.go"},
		Recorded: []string{"a.go", "b.go"},
	})
	if want := []string{"a.go", "b.go"}; !reflect.DeepEqual(s.Files, want) {
		t.Errorf("union: got %v want %v (deduped and sorted)", s.Files, want)
	}
	if s.Source != SourceUnion {
		t.Errorf("source = %q want %q", s.Source, SourceUnion)
	}
	if len(s.Degraded) != 0 {
		t.Errorf("a complete union is not degraded: %v", s.Degraded)
	}
}

// TestNewScopeSourceProvenance walks the three one-sided outcomes. Only the
// changes-only case is degraded: git alone is a complete view of what changed,
// while changes.json alone is only as wide as what someone remembered to
// record.
func TestNewScopeSourceProvenance(t *testing.T) {
	git := NewScope(ScopeInput{Git: []string{"a.go"}})
	if git.Source != SourceGitOnly {
		t.Errorf("git-only source = %q", git.Source)
	}
	if len(git.Degraded) != 0 {
		t.Errorf("git-only is complete, not degraded: %v", git.Degraded)
	}

	rec := NewScope(ScopeInput{Recorded: []string{"a.go", "b.go"}})
	if rec.Source != SourceChangesOnly {
		t.Errorf("changes-only source = %q", rec.Source)
	}
	if want := []string{"a.go", "b.go"}; !reflect.DeepEqual(rec.Files, want) {
		t.Errorf("an empty git side must still yield the recorded files: %v", rec.Files)
	}
	if len(rec.Degraded) != 1 || !strings.Contains(rec.Degraded[0], "changes.json") {
		t.Errorf("changes-only must record why it is partial: %v", rec.Degraded)
	}

	none := NewScope(ScopeInput{})
	if none.Source != SourceNone {
		t.Errorf("empty source = %q", none.Source)
	}
	if !none.Empty() || len(none.Degraded) != 1 {
		t.Errorf("empty scope: files=%v degraded=%v", none.Files, none.Degraded)
	}
}

// TestNewScopeRejectsPathEscape: a changes.json entry naming something outside
// the repo is recorded and dropped, never matched. Silently keeping it would
// let a path traversal widen what counts as "the phase's own code".
func TestNewScopeRejectsPathEscape(t *testing.T) {
	s := NewScope(ScopeInput{
		Root:     "/repo",
		Git:      []string{"a.go"},
		Recorded: []string{"../../etc/passwd", "/etc/passwd"},
	})
	if want := []string{"a.go"}; !reflect.DeepEqual(s.Files, want) {
		t.Errorf("escaping paths entered the scope: %v", s.Files)
	}
	for _, p := range []string{"../../etc/passwd", "/etc/passwd"} {
		if s.Contains(p) {
			t.Errorf("Contains(%q) = true", p)
		}
	}
	if len(s.Degraded) != 2 {
		t.Fatalf("both rejections must be recorded: %v", s.Degraded)
	}
	joined := strings.Join(s.Degraded, "\n")
	if !strings.Contains(joined, "etc/passwd") {
		t.Errorf("degraded entries must name the rejected path: %v", s.Degraded)
	}
	// Recorded contributed nothing usable, so the scope is git-only.
	if s.Source != SourceGitOnly {
		t.Errorf("source = %q want %q — a rejected path is not a contribution", s.Source, SourceGitOnly)
	}
}

// TestNormalizePathTable covers the shapes the callers actually produce,
// including the ones that must be refused.
func TestNormalizePathTable(t *testing.T) {
	cases := []struct {
		root, in string
		want     string
		ok       bool
	}{
		{"/repo", "internal/a.go", "internal/a.go", true},
		{"/repo", "./internal/a.go", "internal/a.go", true},
		{"/repo", "/repo/internal/a.go", "internal/a.go", true},
		{"/repo", `internal\a.go`, "internal/a.go", true},
		{"/repo/", "/repo/a.go", "a.go", true},
		{"", "./a.go", "a.go", true},
		{"/repo", "internal/../internal/a.go", "internal/a.go", true},
		{"/repo", "/other/a.go", "", false},
		{"/repo", "../a.go", "", false},
		{"/repo", "..", "", false},
		{"/repo", ".", "", false},
		{"/repo", "   ", "", false},
		{"/repo", "/repo", "", false},
	}
	for _, tc := range cases {
		got, ok := NormalizePath(tc.root, tc.in)
		if got != tc.want || ok != tc.ok {
			t.Errorf("NormalizePath(%q, %q) = (%q, %v) want (%q, %v)",
				tc.root, tc.in, got, ok, tc.want, tc.ok)
		}
	}
}

// TestParseHunksIgnoresDeletedFileSide: `+++ /dev/null` has no new side, so
// its hunks belong to no file rather than to whatever file came before it.
func TestParseHunksIgnoresDeletedFileSide(t *testing.T) {
	diff := "+++ b/a.go\n@@ -1,0 +1,2 @@\n+++ /dev/null\n@@ -1,2 +0,0 @@\n"
	hunks, degraded := ParseHunks(diff)
	if !reflect.DeepEqual(hunks["a.go"], []Range{{1, 2}}) {
		t.Errorf("a.go ranges: %v", hunks["a.go"])
	}
	if len(hunks) != 1 {
		t.Errorf("deleted-file hunks must not attach anywhere: %v", hunks)
	}
	if len(degraded) != 0 {
		t.Errorf("a deletion is not a degradation: %v", degraded)
	}
}

// TestParseHunksDequotesPaths: git quotes paths carrying non-ASCII bytes.
// Left quoted, the key never matches a mutant reported under the real name.
func TestParseHunksDequotesPaths(t *testing.T) {
	hunks, _ := ParseHunks("+++ \"b/internal/na\\303\\257ve file.go\"\n@@ -1,0 +2,1 @@\n")
	if _, ok := hunks["internal/naïve file.go"]; !ok {
		t.Errorf("quoted path not decoded: %v", hunks)
	}
}
