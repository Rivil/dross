package secretscan

import (
	"math"
	"regexp"
	"strings"
)

// Rule is one credential shape the scanner recognises.
//
// Every Regex carries exactly one capture group — the secret value itself —
// so a hit's length fingerprint measures the value, not the key name or the
// header scheme around it. Prefix is the rule's fixed literal, printed as the
// second half of the fingerprint; it is a property of the rule, never bytes
// copied out of the match, which is what keeps a report echo-free (c-5).
type Rule struct {
	Name   string
	Regex  *regexp.Regexp
	Prefix string

	// carveOut, when set, rejects a candidate value the regex accepted — the
	// one place a rule can say "this shape, but not this instance" without
	// widening the regex into something a reader can no longer check.
	carveOut func(value string) bool
}

// valueClass is the character set a credential value is drawn from. `$`,
// `<`, `>`, `"` and `+` sit outside it on purpose: `$GITHUB_TOKEN`,
// `<your-token>`, `"Bearer "+c.token` and `Bearer <token-from-auth_env>` are
// references to a secret, not the secret, and the class is what tells them
// apart from a pasted value of the same length.
const valueClass = `[A-Za-z0-9+/=_.~@!#%^*-]`

// keyContextMinEntropy is the Shannon floor (bits per byte) a key-context value
// must clear. A value below it — `aaaaaaaaaaaaaaaaaaaab`, `xxxxxxxxxxxxxxxx` —
// is a placeholder, and the entropy_rules decision restricts the judgement to
// the value side of a labelled assignment; no bare line is ever judged.
const keyContextMinEntropy = 3.0

// rules is the fixed table. The order is the report order; no rule is
// consulted from anywhere but the binary (pattern_source decision).
var rules = []Rule{
	{
		Name:   "github-token",
		Regex:  regexp.MustCompile(`\b(gh[pousr]_[A-Za-z0-9]{36}|github_pat_[A-Za-z0-9_]{22,})\b`),
		Prefix: "ghp_",
	},
	{
		Name:   "gitlab-pat",
		Regex:  regexp.MustCompile(`\b(glpat-[A-Za-z0-9_-]{20,})`),
		Prefix: "glpat-",
	},
	{
		Name:   "atlassian-token",
		Regex:  regexp.MustCompile(`\b(ATATT[A-Za-z0-9_=-]{20,})`),
		Prefix: "ATATT",
	},
	{
		Name:   "aws-access-key",
		Regex:  regexp.MustCompile(`\b((?:AKIA|ASIA)[A-Z0-9]{16})\b`),
		Prefix: "AKIA",
		// AKIAIOSFODNN7EXAMPLE is the placeholder every AWS document carries;
		// it is documentation, not a key.
		carveOut: func(v string) bool { return strings.HasSuffix(v, "EXAMPLE") },
	},
	{
		Name:   "slack-token",
		Regex:  regexp.MustCompile(`\b(xox[abprs]-[A-Za-z0-9-]{10,})`),
		Prefix: "xox",
	},
	{
		Name:   "sk-api-key",
		Regex:  regexp.MustCompile(`\b(sk-(?:proj-|ant-)?[A-Za-z0-9_-]{20,})`),
		Prefix: "sk-",
	},
	{
		Name:   "pem-private-key",
		Regex:  regexp.MustCompile(`(-----BEGIN (?:[A-Z]+ )?PRIVATE KEY(?: BLOCK)?-----)`),
		Prefix: "-----BEGIN",
	},
	{
		Name:   "authorization-header",
		Regex:  regexp.MustCompile(`(?i)authorization["']?\s*[:=]\s*["']?(?:basic|bearer)\s+(` + valueClass + `{8,})`),
		Prefix: "Basic|Bearer",
	},
	{
		Name: "key-context",
		Regex: regexp.MustCompile(`(?i)[A-Za-z0-9_.-]*(?:password|passwd|pwd|secret|api[_-]key|access[_-]key|token|authorization)["']?\s*[:=]\s*["']?(` +
			valueClass + `{16,})`),
		Prefix:   "",
		carveOut: func(v string) bool { return entropy(v) < keyContextMinEntropy },
	},
}

// Rules returns the compiled table, in report order.
func Rules() []Rule { return rules }

// entropy is the Shannon entropy of s in bits per byte.
func entropy(s string) float64 {
	if s == "" {
		return 0
	}
	var counts [256]int
	for i := 0; i < len(s); i++ {
		counts[s[i]]++
	}
	n := float64(len(s))
	var h float64
	for _, c := range counts {
		if c == 0 {
			continue
		}
		p := float64(c) / n
		h -= p * math.Log2(p)
	}
	return h
}
