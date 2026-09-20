// Package secretscan is the embedded credential detector behind dross's publish
// and artifact gates.
//
// It recognises a fixed set of credential shapes — provider-prefixed tokens,
// PEM private-key headers, Authorization header values, and labelled
// password/token/secret assignments — and reports each hit as a fingerprint
// (rule, file:line, length, the rule's fixed prefix) that never contains the
// matched value. A gate that refuses on a hit and prints the report is safe to
// run in front of any sink: the report itself cannot be the leak.
//
// The ruleset is compiled in (pattern_source decision): the verdict does not
// depend on what is on PATH, and every rule is testable offline against a
// corpus built at runtime. Whole-tree secret scanning stays gitleaks' job via
// `dross secure`; this package covers the artifacts dross persists and the
// bodies it publishes (scan_scope decision).
package secretscan

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"
)

// AllowMarker, present anywhere on a line, silences every rule for that line
// and that line only (allowlist_route decision). It sits next to the evidence
// so the exemption is reviewable in the same diff that added the shape.
const AllowMarker = "dross:allow-secret"

// Hit is one credential-shaped value the scanner found.
//
// It is a fingerprint, not an excerpt: Length is the value's byte length and
// Prefix is the matching rule's fixed literal. Nothing in a Hit is copied out
// of the scanned text, so a Hit can be printed, logged, or wrapped into an
// error without becoming the thing it warns about.
type Hit struct {
	Rule     string
	Location string
	Line     int
	Length   int
	Prefix   string
}

// String renders the fingerprint: `<rule> at <location>:<line> (len=<n>, prefix=<fixed>)`.
func (h Hit) String() string {
	return fmt.Sprintf("%s at %s:%d (len=%d, prefix=%s)", h.Rule, h.Location, h.Line, h.Length, h.Prefix)
}

// Report renders hits one per line followed by the single remedy line. The
// remedy names AllowMarker because the user fixes the artifact by hand — a
// hit is never scrubbed or rewritten on their behalf (hit_disposition
// decision).
func Report(hits []Hit) string {
	var b strings.Builder
	for _, h := range hits {
		b.WriteString(h.String())
		b.WriteByte('\n')
	}
	fmt.Fprintf(&b, "secret-shaped value found: fix the line by hand, or append %s to it to accept the shape", AllowMarker)
	return b.String()
}

// ErrHit is the error a gate returns when the scan found something. Callers
// test for it with errors.As and read Hits for the structured fingerprints;
// its Error text is Report(Hits), so wrapping it is as echo-free as printing
// it.
type ErrHit struct {
	Hits []Hit
}

func (e *ErrHit) Error() string { return Report(e.Hits) }

// AsErrHit unwraps err to the *ErrHit inside it, if any.
func AsErrHit(err error) (*ErrHit, bool) {
	var e *ErrHit
	if errors.As(err, &e) {
		return e, true
	}
	return nil, false
}

// Scan reads r line by line and returns every hit, with name as each hit's
// Location and the 1-based line number as its Line.
//
// Lines are read with bufio.Reader.ReadBytes rather than bufio.Scanner: a
// minified JSON artifact or a one-line PR body is one line of arbitrary
// length, and a scanner with the default 64 KB ceiling would refuse it — or,
// worse, be tempted to skip it. CRLF is tolerated. A line carrying AllowMarker
// yields nothing.
func Scan(name string, r io.Reader) ([]Hit, error) {
	br := bufio.NewReader(r)
	var hits []Hit
	for n := 1; ; n++ {
		line, err := br.ReadBytes('\n')
		if len(line) > 0 {
			hits = append(hits, scanLine(name, n, line)...)
		}
		if err == io.EOF {
			return hits, nil
		}
		if err != nil {
			return nil, fmt.Errorf("secret scan %s: %w", name, err)
		}
	}
}

// ScanString is Scan over an in-memory string.
func ScanString(name, s string) []Hit {
	hits, _ := Scan(name, strings.NewReader(s))
	return hits
}

func scanLine(name string, n int, line []byte) []Hit {
	line = bytes.TrimRight(line, "\r\n")
	if bytes.Contains(line, []byte(AllowMarker)) {
		return nil
	}
	var hits []Hit
	for _, rule := range rules {
		for _, m := range rule.Regex.FindAllSubmatchIndex(line, -1) {
			value := string(line[m[2]:m[3]])
			if rule.carveOut != nil && rule.carveOut(value) {
				continue
			}
			hits = append(hits, Hit{
				Rule:     rule.Name,
				Location: name,
				Line:     n,
				Length:   len(value),
				Prefix:   rule.Prefix,
			})
			break // one fingerprint per rule per line is enough to refuse on
		}
	}
	return hits
}
