package survivor

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/mutation"
)

// TestNormalizationInvariantSpellings pins that whitespace and line-ending
// spelling do not change identity: the same logical source line indented
// differently, with trailing whitespace, or CRLF-terminated must hash to one
// key. If normalization regresses, a gofmt pass or a CRLF checkout silently
// invalidates every acceptance in the store.
func TestNormalizationInvariantSpellings(t *testing.T) {
	const canonical = "if x > 0 {"
	want := KeyFor("a.go", "OP", Normalize(canonical))
	variants := []string{
		"\tif x > 0 {",
		"        if x > 0 {",
		"if x > 0 {   ",
		"if x > 0 {\r",
		"\t if x > 0 {\t\r",
	}
	for _, v := range variants {
		if got := KeyFor("a.go", "OP", Normalize(v)); got != want {
			t.Errorf("KeyFor(Normalize(%q)) = %s, want %s — whitespace/line-ending spelling must not change identity", v, got, want)
		}
	}
}

// TestOpIsPartOfKey guards against dropping the operator from the key. Two
// different mutation ops on one line are two different survivors; collapsing
// them means accepting one silently accepts the other.
func TestOpIsPartOfKey(t *testing.T) {
	a := KeyFor("a.go", "CONDITIONALS_NEGATION", "if x > 0 {")
	b := KeyFor("a.go", "CONDITIONALS_BOUNDARY", "if x > 0 {")
	if a == b {
		t.Fatalf("two ops on the same line collided on key %s — op must be part of the key", a)
	}
}

// TestFileIsPartOfKey guards the other half of the composite: the same source
// text in two different files is two survivors.
func TestFileIsPartOfKey(t *testing.T) {
	a := KeyFor("a.go", "OP", "return nil")
	b := KeyFor("b.go", "OP", "return nil")
	if a == b {
		t.Fatalf("same text in two files collided on key %s — file must be part of the key", a)
	}
}

// TestPathSpellingDoesNotChangeKey pins path normalization: equivalent
// spellings of one path must not fork a survivor into several identities.
func TestPathSpellingDoesNotChangeKey(t *testing.T) {
	want := KeyFor("internal/x.go", "OP", "return nil")
	for _, p := range []string{"./internal/x.go", "internal//x.go", "internal/x.go/", "internal/./x.go"} {
		if got := KeyFor(p, "OP", "return nil"); got != want {
			t.Errorf("KeyFor(%q, ...) = %s, want %s — path spelling must not change identity", p, got, want)
		}
	}
}

// TestKeyStableAcrossLineDrift is the core of c-4: a survivor whose target text
// moves down the file is still the same survivor. If a line number ever leaks
// into the hash, this fails and every unrelated edit above a survivor lapses
// its acceptance.
func TestKeyStableAcrossLineDrift(t *testing.T) {
	const target = "\tif limit > 0 {"
	early := buildFixture(t, 40, target, 100)
	late := buildFixture(t, 88, target, 100)

	got40, err := ResolveSource(early, "a.go", 40, "OP")
	if err != nil {
		t.Fatalf("ResolveSource at line 40: %v", err)
	}
	got88, err := ResolveSource(late, "a.go", 88, "OP")
	if err != nil {
		t.Fatalf("ResolveSource at line 88: %v", err)
	}
	if got40.Key != got88.Key {
		t.Fatalf("key changed when text moved from line 40 to 88: %s vs %s — line number must not be in the hash", got40.Key, got88.Key)
	}
	if got40.Key == "" {
		t.Fatal("resolved an empty key for a present subject")
	}
}

// TestKeyIsTextDerived is the false-suppression half of c-4: holding
// file+line+op fixed while the line's source text changes must produce a
// different key, so a genuinely new survivor at an accepted survivor's old
// position is not mistaken for it.
func TestKeyIsTextDerived(t *testing.T) {
	before := buildFixture(t, 40, "\tif limit > 0 {", 100)
	after := buildFixture(t, 40, "\tif limit >= 0 {", 100)

	a, err := ResolveSource(before, "a.go", 40, "OP")
	if err != nil {
		t.Fatalf("ResolveSource before: %v", err)
	}
	b, err := ResolveSource(after, "a.go", 40, "OP")
	if err != nil {
		t.Fatalf("ResolveSource after: %v", err)
	}
	if a.Key == b.Key {
		t.Fatalf("replacing the line's source text at a fixed file+line+op kept key %s — the key must be text-derived, not positional", a.Key)
	}
}

// TestAmbiguityDetection pins the locked survivor_identity ambiguity rule:
// identical normalized text on two lines is ambiguous, with the occurrence
// count reported so the caller can say why suppression was withheld.
func TestAmbiguityDetection(t *testing.T) {
	src := []byte(strings.Join([]string{
		"package p",
		"",
		"\treturn nil",
		"x := 1",
		"    return nil",
	}, "\n"))

	got, err := ResolveSource(src, "a.go", 3, "OP")
	if err != nil {
		t.Fatalf("ResolveSource: %v", err)
	}
	if !got.Ambiguous {
		t.Errorf("Ambiguous = false, want true — identical normalized text on two lines is ambiguous")
	}
	if got.Occurrences != 2 {
		t.Errorf("Occurrences = %d, want 2", got.Occurrences)
	}

	unique, err := ResolveSource(src, "a.go", 4, "OP")
	if err != nil {
		t.Fatalf("ResolveSource unique line: %v", err)
	}
	if unique.Ambiguous || unique.Occurrences != 1 {
		t.Errorf("unique line reported Ambiguous=%v Occurrences=%d, want false/1", unique.Ambiguous, unique.Occurrences)
	}
}

// TestSubjectGoneGuards pins that no key is ever constructed for a subject that
// isn't there — a deleted file, a line index past EOF, or a blank line. A
// constructible key for a vanished subject is how a stale acceptance would
// suppress forever.
func TestSubjectGoneGuards(t *testing.T) {
	src := []byte("package p\n\n\treturn nil\n")

	cases := []struct {
		name string
		run  func() (Resolved, error)
	}{
		{"deleted file", func() (Resolved, error) {
			return Resolve(filepath.Join(t.TempDir(), "gone.go"), 1, "OP")
		}},
		{"line past EOF", func() (Resolved, error) { return ResolveSource(src, "a.go", 99, "OP") }},
		{"line zero", func() (Resolved, error) { return ResolveSource(src, "a.go", 0, "OP") }},
		{"blank line", func() (Resolved, error) { return ResolveSource(src, "a.go", 2, "OP") }},
		{"trailing phantom line", func() (Resolved, error) { return ResolveSource(src, "a.go", 4, "OP") }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.run()
			if !errors.Is(err, ErrSubjectGone) {
				t.Fatalf("err = %v, want ErrSubjectGone", err)
			}
			if got.Key != "" {
				t.Errorf("Key = %q, want empty — no identity for an absent subject", got.Key)
			}
		})
	}
}

// TestResolveReadErrorIsNotSubjectGone: an unreadable file is a problem to
// report, not a subject that went away. Collapsing the two would let a
// permissions blip silently mark live acceptances stale.
func TestResolveReadErrorIsNotSubjectGone(t *testing.T) {
	dir := t.TempDir()
	// A directory reads as an error that is not ErrNotExist.
	_, err := Resolve(dir, 1, "OP")
	if err == nil {
		t.Fatal("Resolve on a directory returned no error")
	}
	if errors.Is(err, ErrSubjectGone) {
		t.Fatalf("read failure reported as ErrSubjectGone: %v", err)
	}
}

// TestResolveReadsWorkingTree covers the disk path end to end, so the on-disk
// resolver cannot drift from the pure one.
func TestResolveReadsWorkingTree(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.go")
	src := buildFixture(t, 12, "\tif limit > 0 {", 20)
	if err := os.WriteFile(path, src, 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := Resolve(path, 12, "OP")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	want, err := ResolveSource(src, path, 12, "OP")
	if err != nil {
		t.Fatalf("ResolveSource: %v", err)
	}
	if got != want {
		t.Errorf("Resolve = %+v, ResolveSource = %+v — the disk path must not diverge from the pure one", got, want)
	}
	if got.Text != "if limit > 0 {" {
		t.Errorf("Text = %q, want the normalized line text", got.Text)
	}
}

// TestOccurrencesCountsNormalizedMatches pins the helper staleness detection
// reads: zero means the subject is gone from the file, regardless of where it
// used to sit. An empty text must never match.
func TestOccurrencesCountsNormalizedMatches(t *testing.T) {
	src := []byte("\treturn nil\nx := 1\n   return nil   \n")
	if n := Occurrences(src, "return nil"); n != 2 {
		t.Errorf("Occurrences = %d, want 2", n)
	}
	if n := Occurrences(src, "return err"); n != 0 {
		t.Errorf("Occurrences of absent text = %d, want 0", n)
	}
	if n := Occurrences(src, ""); n != 0 {
		t.Errorf("Occurrences of empty text = %d, want 0 — empty text must match nothing", n)
	}
}

// TestKeyForEmptyTextIsEmpty pins the guard at the pure-function boundary too:
// no caller can mint an identity out of nothing by bypassing Resolve.
func TestKeyForEmptyTextIsEmpty(t *testing.T) {
	if got := KeyFor("a.go", "OP", ""); got != "" {
		t.Errorf("KeyFor with empty text = %q, want empty", got)
	}
}

// TestMutantCarriesLifecycleFields pins that the lifecycle fields landed on
// mutation.Mutant — the type tests.json serialises under
// languages[].mutation.surviving — rather than on a verify-side wrapper that
// would never reach the persisted run.
func TestMutantCarriesLifecycleFields(t *testing.T) {
	in := mutation.Mutant{
		File:      "internal/x.go",
		Line:      42,
		Op:        "CONDITIONALS_NEGATION",
		Origin:    mutation.OriginInHunk,
		Key:       "deadbeefdeadbeef",
		Lifecycle: "accepted",
		Note:      "switch-case attribution ceiling",
	}
	raw, err := json.Marshal(&mutation.Report{Surviving: []mutation.Mutant{in}})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out mutation.Report
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(out.Surviving) != 1 {
		t.Fatalf("round-tripped %d survivors, want 1", len(out.Surviving))
	}
	if got := out.Surviving[0]; got != in {
		t.Errorf("round-trip = %+v, want %+v — Key/Lifecycle/Note must survive tests.json", got, in)
	}
}

// TestMutantWireForm pins the serialised key names. mutation.Mutant carries no
// json tags, so its fields marshal capitalised; that is the deliberate,
// asserted shape here, not an accident to be "fixed" into snake_case without
// noticing that every already-written tests.json disagrees.
func TestMutantWireForm(t *testing.T) {
	raw, err := json.Marshal(mutation.Mutant{Key: "k", Lifecycle: "routed", Note: "n"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, want := range []string{"Key", "Lifecycle", "Note"} {
		if _, ok := fields[want]; !ok {
			t.Errorf("mutation.Mutant did not serialise field %q; got keys %v", want, keysOf(fields))
		}
	}
}

func keysOf(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// buildFixture returns a total-line source file whose 1-based `at` line holds
// target and whose every other line is distinct filler, so occurrence counts
// stay unambiguous.
func buildFixture(t *testing.T, at int, target string, total int) []byte {
	t.Helper()
	if at < 1 || at > total {
		t.Fatalf("bad fixture: line %d outside 1..%d", at, total)
	}
	lines := make([]string, total)
	for i := range lines {
		n := strconv.Itoa(i)
		lines[i] = "\tfiller" + n + " := " + n
	}
	lines[at-1] = target
	return []byte(strings.Join(lines, "\n") + "\n")
}
