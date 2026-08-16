package cmd

import (
	"fmt"
	"strings"
)

// redProofAbbrevMin is the shortest abbreviation a red-proof doc may spell a
// pinned commit as, and so the shortest hex run this rewriter will touch. Git's
// own default abbreviation floor is 7; below that a hex run is far more likely
// to be an ordinary word than a commit.
const redProofAbbrevMin = 7

// redProofRewriteDoc rewrites every occurrence of the OLD pinned commit in a
// red-proof doc to newSHA, and nothing else.
//
// The locked doc_rewrite_scope decision is what this function implements: the
// replacement is keyed to the old pin's *value*, not to "anything shaped like a
// SHA". fixtures/hostile-config-c5/RUN.md carries the pin three times — a
// `BASE=` shell var, the canonical `base commit:` line, and a `git worktree add
// --detach` recipe — and separately names an unrelated commit in a note about a
// previous repoint. Rewriting only the canonical line would leave the replay
// recipe pointing at a commit nobody can check out: doctor green, doc broken.
// Rewriting every 40-hex run would silently falsify the note.
//
// An occurrence may be the full SHA or any abbreviation of it at least
// redProofAbbrevMin long; it is replaced by newSHA truncated to the same width,
// so a doc that abbreviates stays abbreviated. Matching is full-prefix
// equality — a hex run that shares the pin's first six characters and diverges
// at the seventh is a different commit and is left alone.
//
// count == 0 is an error, never a silent success: a caller that reported a
// rewrite which did not happen would record a pin the doc still contradicts.
func redProofRewriteDoc(doc, body, oldSHA, newSHA string) (string, int, error) {
	old := strings.ToLower(strings.TrimSpace(oldSHA))
	repl := strings.ToLower(strings.TrimSpace(newSHA))
	if !isHexRun(old) || len(old) < redProofAbbrevMin {
		return "", 0, fmt.Errorf("%s: old pin %q is not a commit sha of at least %d hex characters", doc, oldSHA, redProofAbbrevMin)
	}
	if !isHexRun(repl) || len(repl) < len(old) {
		return "", 0, fmt.Errorf("%s: new pin %q is not a commit sha at least as long as the old pin (%d characters)", doc, newSHA, len(old))
	}

	var out strings.Builder
	out.Grow(len(body))
	count := 0
	for i := 0; i < len(body); {
		if !isAlnumByte(body[i]) {
			out.WriteByte(body[i])
			i++
			continue
		}
		j := i
		for j < len(body) && isAlnumByte(body[j]) {
			j++
		}
		tok := body[i:j]
		if isPinOccurrence(tok, old) {
			out.WriteString(repl[:len(tok)])
			count++
		} else {
			out.WriteString(tok)
		}
		i = j
	}
	if count == 0 {
		return "", 0, fmt.Errorf("%s: no occurrence of the pinned commit %s to rewrite", doc, old)
	}
	return out.String(), count, nil
}

// isPinOccurrence reports whether tok is the pinned commit written out — the
// full value or an abbreviation of it. The length bound in both directions is
// the point: a run longer than the pin cannot be an abbreviation of it, and a
// run shorter than redProofAbbrevMin is not treated as a commit at all.
func isPinOccurrence(tok, old string) bool {
	if len(tok) < redProofAbbrevMin || len(tok) > len(old) {
		return false
	}
	if !isHexRun(tok) {
		return false
	}
	return strings.EqualFold(old[:len(tok)], tok)
}

func isHexRun(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if !isHexByte(s[i]) {
			return false
		}
	}
	return true
}

func isHexByte(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F'
}

// isAlnumByte defines the token boundary. Scanning alphanumeric runs rather
// than hex runs is what keeps a SHA-shaped substring of a longer identifier
// from being rewritten out of the middle of it.
func isAlnumByte(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}
