package telemetry

import (
	"strings"
	"testing"
)

// matcherShadows reports whether earlier matcher a makes later matcher b
// unreachable: every token of a is a substring of some token of b, so any
// message that fires b necessarily fires a first. The single-token case
// (most rules) reduces to plain substring containment.
func matcherShadows(a, b []string) bool {
	for _, ta := range a {
		found := false
		for _, tb := range b {
			if strings.Contains(tb, ta) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// TestNoTokenShadowing enforces the taxonomy's central invariant: the
// classRules table is first-match-wins, so an earlier rule's matcher must
// never subsume a later rule's matcher — that later matcher would be
// unreachable and its bucket silently starved. Failing output names both
// buckets and the tokens so the fix (reorder or tighten) is obvious.
func TestNoTokenShadowing(t *testing.T) {
	for i, ri := range classRules {
		for j := i + 1; j < len(classRules); j++ {
			rj := classRules[j]
			for _, ma := range ri.matchers {
				for _, mb := range rj.matchers {
					if matcherShadows(ma, mb) {
						t.Errorf("rule %q matcher %v shadows later rule %q matcher %v — the later matcher is unreachable; reorder or tighten tokens",
							ri.bucket, ma, rj.bucket, mb)
					}
				}
			}
		}
	}
}

// TestMatcherShadowsSelfCheck seeds the guard's own logic with known
// shadowing and non-shadowing pairs, so a weakened matcherShadows (e.g.
// one that stops scanning multi-token matchers) fails here even while
// the live table happens to be clean.
func TestMatcherShadowsSelfCheck(t *testing.T) {
	cases := []struct {
		a, b []string
		want bool
	}{
		// classic single-token shadow: "git " subsumes any longer token containing it
		{[]string{"git "}, []string{"git push failed"}, true},
		// disjoint tokens don't shadow
		{[]string{"network"}, []string{"unknown flag"}, false},
		// multi-token earlier matcher shadows only when EVERY token is subsumed
		{[]string{"carries no", "record"}, []string{"carries no `completed` record"}, true},
		{[]string{"carries no", "record"}, []string{"carries no idea"}, false},
		// later multi-token matcher with one subsumed token IS shadowed by
		// a single-token earlier matcher
		{[]string{"invalid"}, []string{"invalid landmark", "pair"}, true},
	}
	for _, c := range cases {
		if got := matcherShadows(c.a, c.b); got != c.want {
			t.Errorf("matcherShadows(%v, %v) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

// TestClassRulesBucketsUnique pins that each bucket owns exactly one rule —
// a duplicate entry would split a bucket's matchers across the order and
// make precedence reasoning wrong.
func TestClassRulesBucketsUnique(t *testing.T) {
	seen := map[string]bool{}
	for _, r := range classRules {
		if seen[r.bucket] {
			t.Errorf("bucket %q appears twice in classRules", r.bucket)
		}
		seen[r.bucket] = true
		if len(r.matchers) == 0 {
			t.Errorf("bucket %q has no matchers — unreachable bucket", r.bucket)
		}
	}
}
