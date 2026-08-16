package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// liveRedProofDoc is fixtures/hostile-config-c5/RUN.md — the one red-proof doc
// this repo actually ships. Tests read it rather than a hand-written sample so
// the rewriter is exercised against real prose: three pin occurrences in three
// different syntaxes, plus an unrelated commit named in a note.
const liveRedProofDoc = "fixtures/hostile-config-c5/RUN.md"

// syntheticSHA is deliberately not a commit anything in this repo names, and
// shares no 7-character prefix with any SHA the fixture carries.
const syntheticSHA = "0123456789abcdef0123456789abcdef01234567"

// readLiveRunMD returns the shipped doc's bytes plus the pin it currently
// carries. The pin is READ, never spelled: this phase's own first repoint
// rewrites it, and a test naming the literal would go red at that repair.
func readLiveRunMD(t *testing.T) (body, pin string) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(repoRootForDocs(t), liveRedProofDoc))
	if err != nil {
		t.Fatalf("read %s: %v", liveRedProofDoc, err)
	}
	body = string(b)
	pin = redProofSHA(body)
	if pin == "" {
		t.Fatalf("%s carries no `base commit:` pin — the rewriter has nothing to key on", liveRedProofDoc)
	}
	return body, pin
}

// hexTokens returns every maximal alphanumeric run in s that is pure hex and at
// least n characters wide — used to find the doc's unrelated SHAs without
// naming them.
func hexTokens(s string, n int) []string {
	var out []string
	for i := 0; i < len(s); {
		if !isAlnumByte(s[i]) {
			i++
			continue
		}
		j := i
		for j < len(s) && isAlnumByte(s[j]) {
			j++
		}
		if tok := s[i:j]; len(tok) >= n && isHexRun(tok) {
			out = append(out, tok)
		}
		i = j
	}
	return out
}

// TestRewriteDocByteFaithful: the rewrite moves the pin spans and NOTHING else.
// A rewriter that reflowed a line, normalised a fence or dropped trailing
// whitespace would leave doctor green while corrupting a doc a human follows.
func TestRewriteDocByteFaithful(t *testing.T) {
	body, pin := readLiveRunMD(t)

	got, count, err := redProofRewriteDoc(liveRedProofDoc, body, pin, syntheticSHA)
	if err != nil {
		t.Fatalf("redProofRewriteDoc: %v", err)
	}
	if count != 3 {
		t.Errorf("rewrote %d occurrences, want 3 (BASE=, base commit:, worktree add --detach)", count)
	}
	// Every occurrence in the live doc is full-width, so a plain literal
	// substitution is the exact expected output — any other difference is the
	// rewriter touching bytes it had no business touching.
	if want := strings.ReplaceAll(body, pin, syntheticSHA); got != want {
		t.Errorf("rewrite is not byte-faithful:\n%s", firstDiff(want, got))
	}
	for _, marker := range []string{"BASE=", "base commit:", "worktree add --detach"} {
		if !lineWithBoth(got, marker, syntheticSHA) {
			t.Errorf("no line containing %q carries the new sha — that pin span was missed", marker)
		}
	}
}

// TestRewriteDocLeavesUnrelatedSHA: the doc names a commit that is not the pin
// (the note recording a previous repoint). Keying on "any 40-hex run" would
// falsify it.
func TestRewriteDocLeavesUnrelatedSHA(t *testing.T) {
	body, pin := readLiveRunMD(t)

	var unrelated []string
	for _, tok := range hexTokens(body, 40) {
		if !strings.EqualFold(tok, pin) {
			unrelated = append(unrelated, tok)
		}
	}
	if len(unrelated) == 0 {
		t.Fatalf("%s no longer names an unrelated commit — this test has nothing to protect", liveRedProofDoc)
	}

	got, _, err := redProofRewriteDoc(liveRedProofDoc, body, pin, syntheticSHA)
	if err != nil {
		t.Fatalf("redProofRewriteDoc: %v", err)
	}
	for _, tok := range unrelated {
		if !strings.Contains(got, tok) {
			t.Errorf("unrelated commit %s was rewritten", tok)
		}
	}
}

// TestRewriteDocNearMissPrefix: full-prefix equality, not "close enough". A hex
// run sharing the pin's first six characters is a different commit.
func TestRewriteDocNearMissPrefix(t *testing.T) {
	const pin = "a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4"
	// Same first six characters, diverging at the seventh.
	near := pin[:6] + "ff" + pin[8:12]
	if strings.HasPrefix(pin, near) {
		t.Fatalf("test bug: %q is still a prefix of the pin", near)
	}
	body := "base commit: `" + pin + "`\nnot the pin: " + near + "\n"

	got, count, err := redProofRewriteDoc("near.md", body, pin, syntheticSHA)
	if err != nil {
		t.Fatalf("redProofRewriteDoc: %v", err)
	}
	if count != 1 {
		t.Errorf("rewrote %d occurrences, want 1", count)
	}
	if !strings.Contains(got, near) {
		t.Errorf("near-miss %s was rewritten:\n%s", near, got)
	}
}

// TestRewriteDocPreservesWidth: an abbreviated occurrence comes back
// abbreviated to the same width. Expanding it would reflow the prose around it
// and, in a shell recipe, change the line a reader copies.
func TestRewriteDocPreservesWidth(t *testing.T) {
	const pin = "a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4"
	for _, width := range []int{7, 8, 12, 40} {
		abbrev := pin[:width]
		body := "base commit: `" + pin + "`\nrecipe: git show " + abbrev + "\n"

		got, _, err := redProofRewriteDoc("width.md", body, pin, syntheticSHA)
		if err != nil {
			t.Fatalf("width %d: redProofRewriteDoc: %v", width, err)
		}
		want := "recipe: git show " + syntheticSHA[:width] + "\n"
		if !strings.Contains(got, want) {
			t.Errorf("width %d: want line %q, got:\n%s", width, strings.TrimSpace(want), got)
		}
		if strings.Contains(got, "git show "+syntheticSHA+"\n") && width != 40 {
			t.Errorf("width %d: abbreviation was expanded to the full sha", width)
		}
	}
}

// TestRewriteDocNoOccurrence: zero rewrites is an error naming the doc. A nil
// error here would let a caller record a pin the doc never learned about.
func TestRewriteDocNoOccurrence(t *testing.T) {
	const pin = "a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4"
	got, count, err := redProofRewriteDoc("silent.md", "no commit named here\n", pin, syntheticSHA)
	if err == nil {
		t.Fatalf("no error for a doc with zero occurrences (count=%d, body=%q)", count, got)
	}
	if count != 0 {
		t.Errorf("count = %d, want 0", count)
	}
	if !strings.Contains(err.Error(), "silent.md") {
		t.Errorf("error does not name the doc: %v", err)
	}
}

// TestRewriteDocReparses: the whole reason this function exists is the
// record/doc cross-check, so the rewritten bytes must satisfy it.
func TestRewriteDocReparses(t *testing.T) {
	body, pin := readLiveRunMD(t)
	got, _, err := redProofRewriteDoc(liveRedProofDoc, body, pin, syntheticSHA)
	if err != nil {
		t.Fatalf("redProofRewriteDoc: %v", err)
	}
	if reparsed := redProofSHA(got); reparsed != syntheticSHA {
		t.Errorf("redProofSHA of the rewritten doc = %q, want %q", reparsed, syntheticSHA)
	}
}

// TestRewriteDocLiveFixtureRoundTrips: the shipped doc stays rewritable. Out
// and back must be byte-identical — no SHA value named, so this survives every
// future repoint of the live pin.
func TestRewriteDocLiveFixtureRoundTrips(t *testing.T) {
	body, pin := readLiveRunMD(t)

	out, n, err := redProofRewriteDoc(liveRedProofDoc, body, pin, syntheticSHA)
	if err != nil {
		t.Fatalf("rewrite out: %v", err)
	}
	back, m, err := redProofRewriteDoc(liveRedProofDoc, out, syntheticSHA, pin)
	if err != nil {
		t.Fatalf("rewrite back: %v", err)
	}
	if n != m {
		t.Errorf("rewrote %d occurrences out but %d back", n, m)
	}
	if back != body {
		t.Errorf("round trip is not byte-identical:\n%s", firstDiff(body, back))
	}
}

// lineWithBoth reports whether some line of s contains both substrings.
func lineWithBoth(s, a, b string) bool {
	for _, line := range strings.Split(s, "\n") {
		if strings.Contains(line, a) && strings.Contains(line, b) {
			return true
		}
	}
	return false
}

// firstDiff renders the first differing line, so a byte-faithfulness failure
// points at the corruption instead of dumping two copies of a long doc.
func firstDiff(want, got string) string {
	w := strings.Split(want, "\n")
	g := strings.Split(got, "\n")
	for i := 0; i < len(w) && i < len(g); i++ {
		if w[i] != g[i] {
			return "line " + itoa(i+1) + ":\n  want: " + w[i] + "\n  got:  " + g[i]
		}
	}
	if len(w) != len(g) {
		return "line count: want " + itoa(len(w)) + ", got " + itoa(len(g))
	}
	return "(no line differs — trailing bytes?)"
}
