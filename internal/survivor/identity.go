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
	"strconv"
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

	// Ambiguous reports that the key could not be made unique within its file.
	//
	// It is now always false: when Text repeats, the key is scoped by the
	// enclosing declaration and the occurrence ordinal within it, which is
	// unique by construction. The field is kept because callers branch on it
	// and because the guarantee it names — an acceptance never suppresses a
	// survivor it cannot be attributed to — is still the contract; scoping is
	// how that guarantee is now met instead of by withholding.
	Ambiguous bool

	// Occurrences is how many lines in the file share Text. Always >= 1 on a
	// successful resolve. Greater than 1 means the key is scoped.
	Occurrences int

	// Scope is the enclosing top-level declaration a scoped key was derived
	// against, and Ordinal is which occurrence of Text within it this is
	// (1-based). Both empty/zero for an unscoped key.
	//
	// They are recorded so a reader of survivors.toml can see WHY two entries
	// with the same file, op and text are different survivors.
	Scope   string
	Ordinal int
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

// ResolveAt resolves the survivor at rel (a repo-relative path) by reading
// root/rel, but derives the key from rel itself.
//
// Keeping the two apart is load-bearing: the key must be stable across working
// directories, so it is derived from the path the store records, never from the
// absolute path a particular caller happened to read. Resolving with an
// absolute path and storing a relative one produces a key nothing can ever
// match again.
func ResolveAt(root, rel string, line int, op string) (Resolved, error) {
	src, err := os.ReadFile(filepath.Join(root, rel))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Resolved{}, ErrSubjectGone
		}
		return Resolved{}, err
	}
	return ResolveSource(src, rel, line, op)
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
	if n == 1 {
		// The common case, and the one every key recorded before scoping
		// existed used. Left byte-identical so those acceptances keep
		// resolving — scoping is additive, not a re-keying.
		return Resolved{
			Key:         KeyFor(file, op, text),
			Text:        text,
			Occurrences: n,
		}, nil
	}
	scope, ordinal := scopeOf(lines, line, text)
	return Resolved{
		Key:         keyForScoped(file, op, text, scope, ordinal),
		Text:        text,
		Occurrences: n,
		Scope:       scope,
		Ordinal:     ordinal,
	}, nil
}

// scopeOf locates the survivor at the 1-based line within the file: the nearest
// preceding top-level declaration, and which occurrence of text this line is
// INSIDE that declaration.
//
// Scoping to the declaration rather than to the whole file is what keeps the
// ordinal stable: an edit elsewhere in the file cannot renumber it, so only a
// change inside the same function can, and that is a change to the very code
// the acceptance is about.
func scopeOf(lines []string, line int, text string) (scope string, ordinal int) {
	start := 0
	for i := line - 1; i >= 0; i-- {
		if isTopLevelDecl(lines[i]) {
			scope, start = Normalize(lines[i]), i
			break
		}
	}
	for i := start; i < line; i++ {
		if Normalize(lines[i]) == text {
			ordinal++
		}
	}
	return scope, ordinal
}

// isTopLevelDecl reports whether a line opens a top-level declaration — an
// unindented `func`, `type`, `var` or `const`. Deliberately textual rather than
// parsed: identity must work on any language's source, and the only property
// needed is a stable, recognisable anchor above the survivor.
func isTopLevelDecl(line string) bool {
	if line == "" || line[0] == ' ' || line[0] == '\t' {
		return false
	}
	for _, kw := range []string{"func ", "func(", "type ", "var ", "const "} {
		if strings.HasPrefix(line, kw) {
			return true
		}
	}
	return false
}

// keyForScoped derives the identity of a survivor whose line text repeats in
// its file, adding the enclosing declaration and the occurrence ordinal within
// it. Both ride in the hashed payload behind the same NUL separator, so a
// scoped key can never collide with an unscoped one.
func keyForScoped(file, op, text, scope string, ordinal int) string {
	if text == "" {
		return ""
	}
	joined := strings.Join([]string{
		normalizePath(file), op, text, scope, strconv.Itoa(ordinal),
	}, "\x00")
	sum := sha256.Sum256([]byte(joined))
	return hex.EncodeToString(sum[:8])
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
