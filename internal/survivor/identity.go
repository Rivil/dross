// Package survivor provides the durable, cross-run identity and acceptance
// registry for surviving mutants. A survivor's key is derived from its file,
// its mutation operator, and a hash of the mutated line's *normalized source
// text* — never its line number, which drifts whenever an unrelated edit shifts
// code. That is what lets an acceptance recorded in one phase still suppress
// the same survivor several phases later, while a genuinely new survivor at the
// accepted one's old position keeps re-emitting.
package survivor

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// ErrSubjectGone reports that the subject a survivor key would be derived from
// no longer exists: the file is missing, the line index is past EOF, or the
// line has no source text at all. Callers get an empty key alongside it — an
// identity is never invented for a subject that isn't there, because a
// constructible key for a vanished subject is exactly how a stale acceptance
// would go on silently suppressing nothing forever (c-5).
var ErrSubjectGone = errors.New("survivor subject no longer exists")

// Resolved is one survivor's resolved identity.
type Resolved struct {
	// Key is the opaque cross-run identity. Empty when resolution failed.
	Key string

	// Text is the normalized source text the key was derived from. Recorded
	// alongside the key so staleness detection can re-look for the subject
	// without re-deriving it from a line number that has since moved.
	Text string

	// Ambiguous is true when Text occurs on more than one line of the file,
	// making the key non-unique within that file. An ambiguous acceptance
	// must not suppress — the survivor re-emits with a note instead. The
	// locked survivor_identity decision picks that direction deliberately: a
	// lapsed acceptance is noise, a false suppression hides a real bug.
	Ambiguous bool

	// Occurrences is how many lines in the file share Text. Always >= 1 on a
	// successful resolve.
	Occurrences int
}

// Normalize reduces a source line to the text the key hashes. Leading
// indentation, trailing whitespace, and a CRLF line terminator's carriage
// return all collapse away, so a gofmt re-indent or a checkout with different
// line endings does not change a survivor's identity.
func Normalize(line string) string {
	return strings.TrimSpace(line)
}

// KeyFor derives the opaque identity from the three components that define a
// survivor: its file, its mutation operator, and the normalized text of the
// mutated line. The line number is deliberately absent — that is the whole
// point of the key (c-4).
//
// Fields are joined with a NUL separator so no concatenation of (file, op,
// text) can be mistaken for a different split. An empty text yields an empty
// key: there is no subject to identify.
func KeyFor(file, op, text string) string {
	if text == "" {
		return ""
	}
	joined := strings.Join([]string{normalizePath(file), op, text}, "\x00")
	sum := sha256.Sum256([]byte(joined))
	// 64-bit hex key: short enough to read and hand-edit in survivors.toml,
	// with ample collision headroom for one repo's survivor set.
	return hex.EncodeToString(sum[:8])
}

// Resolve reads path from the working tree and resolves the survivor at the
// 1-based line for the given operator.
//
// A missing file yields ErrSubjectGone. Any other read failure is returned as
// itself — an unreadable file is a problem to report, not a subject that has
// gone away.
func Resolve(path string, line int, op string) (Resolved, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Resolved{}, ErrSubjectGone
		}
		return Resolved{}, err
	}
	return ResolveSource(src, path, line, op)
}

// ResolveSource is Resolve over source bytes already in hand. Kept separate so
// classification and staleness can resolve without re-reading the file, and so
// the identity rules are testable without touching disk.
func ResolveSource(src []byte, file string, line int, op string) (Resolved, error) {
	lines := splitLines(src)
	if line < 1 || line > len(lines) {
		return Resolved{}, ErrSubjectGone
	}
	text := Normalize(lines[line-1])
	if text == "" {
		return Resolved{}, ErrSubjectGone
	}
	n := countOccurrences(lines, text)
	return Resolved{
		Key:         KeyFor(file, op, text),
		Text:        text,
		Ambiguous:   n > 1,
		Occurrences: n,
	}, nil
}

// Occurrences counts how many lines of src normalize to text. Zero means the
// subject is no longer present in the file — the signal staleness detection
// reads. An empty text never matches anything.
func Occurrences(src []byte, text string) int {
	if text == "" {
		return 0
	}
	return countOccurrences(splitLines(src), text)
}

func countOccurrences(lines []string, text string) int {
	n := 0
	for _, l := range lines {
		if Normalize(l) == text {
			n++
		}
	}
	return n
}

// splitLines splits source into lines without allocating a trailing empty line
// for a file that ends in a newline — that phantom line would otherwise be a
// resolvable index one past the real end of the file.
func splitLines(src []byte) []string {
	s := strings.TrimSuffix(string(src), "\n")
	s = strings.TrimSuffix(s, "\r")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

// normalizePath collapses ./ , // , and trailing-slash forms to one canonical
// forward-slash path so equivalent spellings key identically. An empty path
// stays empty — filepath.Clean would turn "" into ".", which must not pollute
// the key.
func normalizePath(p string) string {
	if p == "" {
		return ""
	}
	return filepath.ToSlash(filepath.Clean(p))
}
