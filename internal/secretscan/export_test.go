package secretscan

import (
	"regexp"
	"strings"
)

// Candidates is the prefilter's bitmask for line over the real rule table.
func Candidates(line string) uint64 { return candidates([]byte(line)) }

// CandidatesIn is the prefilter's bitmask for line over a caller's table.
func CandidatesIn(rs []Rule, line string) uint64 { return candidatesIn(rs, []byte(line)) }

// NewRule builds a Rule with explicit needles, for exercising the prefilter
// away from the real table.
func NewRule(name string, re *regexp.Regexp, fold bool, needles ...string) Rule {
	r := Rule{Name: name, Regex: re, fold: fold}
	if len(needles) > 0 {
		r.needles = lits(needles...)
	}
	return r
}

// Needles returns r's prefilter literals.
func Needles(r Rule) []string {
	out := make([]string, len(r.needles))
	for i, n := range r.needles {
		out[i] = string(n)
	}
	return out
}

// ScanUnfiltered is ScanString with every rule a candidate on every line — the
// reference the prefiltered scan must equal.
func ScanUnfiltered(name, s string) []Hit {
	hits, _ := scan(name, strings.NewReader(s), func([]byte) uint64 { return ^uint64(0) })
	return hits
}
