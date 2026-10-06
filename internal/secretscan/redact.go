package secretscan

import (
	"regexp"
	"sort"
	"strings"
)

// Redact returns s with the value of every credential-shaped match replaced by
// `[redacted <rule>]`, and a Hit for each replacement.
//
// It is for text dross did not write and cannot send back to be fixed by hand —
// a PR comment someone else posted. Report's hit_disposition (refuse, never
// rewrite) is for the user's own artifacts; untrusted third-party text that
// dross persists or prints is scrubbed instead, because nobody is there to fix
// it.
//
// Every match of every rule on every line is replaced, not only the first
// Scan needs to refuse on, and a rule's carve-outs still hold. Only the
// captured value goes: `api_key = "` survives in front of its redaction. A PEM
// private key is replaced whole, from its BEGIN header through its END line,
// or through the end of s when the block never closes.
//
// AllowMarker earns nothing here: the line is redacted like any other and the
// marker itself is neutralised, so text a commenter wrote cannot carry a
// marker that would silence a later scan of whatever dross saves. A Hit holds
// no matched bytes, the same fingerprint Scan reports; its Location is empty.
//
// Redact(Redact(s)) == Redact(s), and ScanString over the result is empty.
// One pass cannot promise that: a carve-out judges a whole candidate value,
// and replacing a secret inside a carved-out value can leave a shorter,
// high-entropy remainder that now matches on its own. So Redact repeats until
// the text stops changing — each pass consumes credential bytes, which the
// `[redacted …]` it writes never supplies — with redactPasses as a backstop.
func Redact(s string) (string, []Hit) {
	out, hits := redactOnce(s)
	for i := 1; i < redactPasses; i++ {
		next, more := redactOnce(out)
		if next == out {
			break
		}
		out, hits = next, append(hits, more...)
	}
	return out, hits
}

// redactPasses bounds Redact's fixed-point loop.
const redactPasses = 8

// redactOnce is one pass over s.
func redactOnce(s string) (string, []Hit) {
	var out strings.Builder
	var hits []Hit
	rest := s
	for line := 1; rest != ""; {
		var raw string
		if i := strings.IndexByte(rest, '\n'); i >= 0 {
			raw, rest = rest[:i+1], rest[i+1:]
		} else {
			raw, rest = rest, ""
		}
		body, eol := cutEOL(raw)
		body = strings.ReplaceAll(body, AllowMarker, neutralisedMarker)

		begin := pemRule().Regex.FindStringIndex(body)
		if begin == nil {
			redacted, h := redactLine(body, line)
			out.WriteString(redacted)
			out.WriteString(eol)
			hits = append(hits, h...)
			line++
			continue
		}

		before, h := redactLine(body[:begin[0]], line)
		out.WriteString(before)
		hits = append(hits, h...)
		out.WriteString(redaction(pemRuleName))
		block := body[begin[1]:] + eol + rest
		end := pemEnd.FindStringIndex(block)
		if end == nil {
			hits = append(hits, pemHit(line, begin[1]-begin[0]+len(block)))
			return out.String(), hits
		}
		hits = append(hits, pemHit(line, begin[1]-begin[0]+end[1]))
		// What follows the END marker on its line is text like any other, and
		// is read on as the rest of that line.
		line += strings.Count(block[:end[1]], "\n")
		rest = block[end[1]:]
	}
	return out.String(), hits
}

// neutralisedMarker is what AllowMarker becomes in redacted text.
const neutralisedMarker = "[allow-marker removed]"

const pemRuleName = "pem-private-key"

// pemEnd closes the block a pem-private-key header opens.
var pemEnd = regexp.MustCompile(`-----END (?:[A-Z]+ )?PRIVATE KEY(?: BLOCK)?-----`)

func pemRule() Rule {
	for _, r := range rules {
		if r.Name == pemRuleName {
			return r
		}
	}
	panic("secretscan: no " + pemRuleName + " rule")
}

func pemHit(line, length int) Hit {
	r := pemRule()
	return Hit{Rule: r.Name, Line: line, Length: length, Prefix: r.Prefix}
}

func redaction(rule string) string { return "[redacted " + rule + "]" }

// cutEOL splits a line from its LF or CRLF ending, so the ending survives
// redaction byte for byte.
func cutEOL(raw string) (body, eol string) {
	switch {
	case strings.HasSuffix(raw, "\r\n"):
		return raw[:len(raw)-2], "\r\n"
	case strings.HasSuffix(raw, "\n"):
		return raw[:len(raw)-1], "\n"
	}
	return raw, ""
}

// span is one value to replace: body[start:end], found by rules[rule].
type span struct{ start, end, rule int }

// redactLine replaces every non-PEM match on one line. Where two rules claim
// overlapping bytes — `token = ghp_…` is both a github-token and a key-context
// value — the union goes, named for the match that starts first, with report
// order breaking a tie.
func redactLine(body string, line int) (string, []Hit) {
	mask := candidates([]byte(body))
	var spans []span
	for i, r := range rules {
		if mask&(1<<i) == 0 || r.Name == pemRuleName {
			continue
		}
		for _, m := range r.Regex.FindAllStringSubmatchIndex(body, -1) {
			if r.carveOut != nil && r.carveOut(body[m[2]:m[3]]) {
				continue
			}
			spans = append(spans, span{m[2], m[3], i})
		}
	}
	if len(spans) == 0 {
		return body, nil
	}
	sort.Slice(spans, func(a, b int) bool {
		if spans[a].start != spans[b].start {
			return spans[a].start < spans[b].start
		}
		return spans[a].rule < spans[b].rule
	})
	merged := []span{spans[0]}
	for _, sp := range spans[1:] {
		last := &merged[len(merged)-1]
		if sp.start < last.end {
			last.end = max(last.end, sp.end)
			continue
		}
		merged = append(merged, sp)
	}

	var b strings.Builder
	hits := make([]Hit, 0, len(merged))
	at := 0
	for _, sp := range merged {
		r := rules[sp.rule]
		b.WriteString(body[at:sp.start])
		b.WriteString(redaction(r.Name))
		at = sp.end
		hits = append(hits, Hit{Rule: r.Name, Line: line, Length: sp.end - sp.start, Prefix: r.Prefix})
	}
	b.WriteString(body[at:])
	return b.String(), hits
}
