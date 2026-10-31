package memadvise

import (
	"regexp"
	"strings"
	"time"
)

// A time-bound clause is the part of an index hook that stops being true on
// its own: a deadline, or a pointer to the next phase. Once it has passed, the
// clause misleads every turn that loads the index — but the memory around it
// is usually still worth keeping, so an expired clause is reworded out, never
// a reason to delete (c-8).
//
// A date only binds when a deadline word sits directly before it. A bare date
// is history ("RESOLVED 2026-09-19", "shipped 2026-10-04") and stays true.
var (
	dateClauseRe = regexp.MustCompile(`(?i)\b(?:until|by|before|due|expires|till|deadline):?\s+(\d{4}-\d{2}-\d{2})\b`)
	nextClauseRe = regexp.MustCompile(`(?i)\bnext:?\s+(?:phase\s+|/dross-[a-z-]+\s+)?([a-z0-9](?:[a-z0-9-]*[a-z0-9])?)`)
)

// clauseEnds are where a clause stops: the hook's own separators, or the close
// of a parenthetical it opened inside.
var clauseEnds = []string{";", ",", " — ", " – ", " · ", ")"}

// joinSeps are the separators trimmed from beside a removed clause, so the
// remaining text never ends or starts on a dangling one.
var joinSeps = []string{";", ",", "—", "–", "·"}

// NextSlugs returns the phase slugs hook's "next" clauses name, lowercased as
// slugs are, so a caller can build Expired's known set by asking only about
// those.
func NextSlugs(hook string) []string {
	var out []string
	for _, m := range nextClauseRe.FindAllStringSubmatch(hook, -1) {
		out = append(out, strings.ToLower(m[1]))
	}
	return out
}

// Expired finds the first time-bound clause in hook that has expired — a
// deadline strictly before today's date, or a "next" naming a phase known
// holds as scaffolded or shipped — and returns hook with only that clause and
// its separator removed. A hook that would be left empty is not reported: the
// clause is all it says, and the only fix for that is a delete, which an
// expired clause never earns.
//
// known is a set, not a callback: a func value here is a dispatch the
// exec-consent audit cannot resolve to a callee.
func Expired(hook string, today time.Time, known map[string]bool) (reworded string, expired bool) {
	todayStr := today.Format(time.DateOnly)
	type span struct{ start, end int }
	var hits []span
	for _, m := range dateClauseRe.FindAllStringSubmatchIndex(hook, -1) {
		date := hook[m[2]:m[3]]
		if _, err := time.Parse(time.DateOnly, date); err == nil && date < todayStr {
			hits = append(hits, span{m[0], m[1]})
		}
	}
	for _, m := range nextClauseRe.FindAllStringSubmatchIndex(hook, -1) {
		if known[strings.ToLower(hook[m[2]:m[3]])] {
			hits = append(hits, span{m[0], m[1]})
		}
	}
	if len(hits) == 0 {
		return hook, false
	}
	first := hits[0]
	for _, h := range hits[1:] {
		if h.start < first.start {
			first = h
		}
	}
	out := removeClause(hook, first.start, first.end)
	if strings.TrimSpace(out) == "" {
		return hook, false
	}
	return out, true
}

// removeClause cuts hook[start:] up to the next clause end, plus the one
// separator that joined it to the text before (or, at the start, after) it.
func removeClause(hook string, start, end int) string {
	rest := hook[end:]
	stop := len(rest)
	for _, sep := range clauseEnds {
		if i := strings.Index(rest, sep); i >= 0 && i < stop {
			stop = i
		}
	}
	prefix := strings.TrimRight(hook[:start], " ")
	suffix := rest[stop:]
	switch {
	case strings.HasSuffix(prefix, "(") && strings.HasPrefix(suffix, ")"):
		prefix = strings.TrimRight(strings.TrimSuffix(prefix, "("), " ")
		suffix = suffix[1:]
	case prefix == "" || strings.HasSuffix(prefix, "("):
		// Nothing before the clause to join to: the separator after it goes.
		suffix = strings.TrimLeft(suffix, " ")
		for _, sep := range joinSeps {
			if strings.HasPrefix(suffix, sep) {
				suffix = strings.TrimLeft(strings.TrimPrefix(suffix, sep), " ")
				break
			}
		}
	default:
		for _, sep := range joinSeps {
			if strings.HasSuffix(prefix, sep) {
				prefix = strings.TrimRight(strings.TrimSuffix(prefix, sep), " ")
				break
			}
		}
	}
	return prefix + suffix
}
