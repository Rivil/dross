package memadvise

import "regexp"

// A guard phrase marks a memory that exists to stop a mistake from being made
// twice: a rule (NEVER, ASK), a trap (DECOY), a reopened decision (REOPENED,
// don't re-raise, don't revert). Deleting one because it also looks stale or
// duplicated is exactly the regression it guards against, so no delete is ever
// suggested — or applied — for a guarded memory (c-4).
//
// The capitalised words are matched case-sensitively and as whole words: in
// prose "ask" and "never" are ordinary verbs, and TASK or NEVERTHELESS are not
// the marker. Status words (DEAD, done, RESOLVED, DECLINED) are deliberately
// absent — on their own they say nothing about whether the memory still earns
// its index line.
var (
	guardWordRe   = regexp.MustCompile(`\b(?:NEVER|ASK|DECOY|REOPENED)\b`)
	guardPhraseRe = regexp.MustCompile(`(?i)\bdon['’]t\s+(?:re-raise|revert)\b`)
)

// Guarded reports whether raw — a memory's whole file, frontmatter included —
// carries a guard phrase.
func Guarded(raw []byte) bool {
	return guardWordRe.Match(raw) || guardPhraseRe.Match(raw)
}
