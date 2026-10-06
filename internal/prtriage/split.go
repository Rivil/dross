package prtriage

import (
	"regexp"
	"strings"
)

// ReviewHeader opens every comment /dross-review posts (assets/prompts/review.md §4).
const ReviewHeader = "## /dross-review"

// Finding is one finding of a /dross-review comment. N counts from 1 in the
// order the comment lists them; Loc is the first backticked path:line in it,
// or empty; Text is the finding's own lines, LF-separated.
type Finding struct {
	N        int
	Severity string
	Loc      string
	Text     string
}

// findingLine is a finding bullet: column 0, a bold severity, an em dash.
var findingLine = regexp.MustCompile(`^- \*\*(BLOCKING|FLAG|NOTE)\*\* — `)

// findingLoc is a backticked path:line or path:line-line.
var findingLoc = regexp.MustCompile("`([^`\\s]+:[0-9]+(?:-[0-9]+)?)`")

// headingLine is an ATX heading, which ends the finding above it.
var headingLine = regexp.MustCompile(`^#{1,6}(?:[ \t]|$)`)

// SplitReview splits a /dross-review comment into its findings. It splits
// only a comment whose first non-blank line starts with ReviewHeader; a
// finding is a column-0 bullet outside any ``` or ~~~ fence, and runs to the
// next finding, a heading or the `---` footer. A comment with no findings is
// (nil, false), so it is triaged whole. Who wrote the comment is the caller's
// question, not this one's.
func SplitReview(body string) ([]Finding, bool) {
	lines := strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n")
	if !startsWithHeader(lines) {
		return nil, false
	}
	var out []Finding
	var cur []string
	flush := func() {
		if cur == nil {
			return
		}
		text := strings.TrimRight(strings.Join(cur, "\n"), "\n \t")
		f := Finding{N: len(out) + 1, Severity: findingLine.FindStringSubmatch(cur[0])[1], Text: text}
		if m := findingLoc.FindStringSubmatch(text); m != nil {
			f.Loc = m[1]
		}
		out = append(out, f)
		cur = nil
	}
	var fence fenceState
	for _, line := range lines {
		if fence.open() {
			fence.feed(line)
			if cur != nil {
				cur = append(cur, line)
			}
			continue
		}
		switch {
		case findingLine.MatchString(line):
			flush()
			cur = []string{line}
		case headingLine.MatchString(line), line == "---":
			flush()
		default:
			fence.feed(line)
			if cur != nil {
				cur = append(cur, line)
			}
		}
	}
	flush()
	if len(out) == 0 {
		return nil, false
	}
	return out, true
}

func startsWithHeader(lines []string) bool {
	for _, l := range lines {
		if strings.TrimSpace(l) == "" {
			continue
		}
		return strings.HasPrefix(l, ReviewHeader)
	}
	return false
}

// fenceState tracks a CommonMark fenced code block: an opener of three or more
// backticks or tildes indented at most three spaces (a backtick opener's info
// may hold no backtick, so "```snippet```" is inline code, not an opener), and
// a closer of the same character at least as long, followed by blanks only.
type fenceState struct {
	char byte
	n    int
}

func (f *fenceState) open() bool { return f.n > 0 }

func (f *fenceState) feed(line string) {
	trimmed := strings.TrimLeft(line, " ")
	if len(line)-len(trimmed) > 3 || trimmed == "" {
		return
	}
	c := trimmed[0]
	if c != '`' && c != '~' {
		return
	}
	run := len(trimmed) - len(strings.TrimLeft(trimmed, string(c)))
	if run < 3 {
		return
	}
	rest := trimmed[run:]
	if f.open() {
		if c == f.char && run >= f.n && strings.Trim(rest, " \t") == "" {
			*f = fenceState{}
		}
		return
	}
	if c == '`' && strings.Contains(rest, "`") {
		return
	}
	*f = fenceState{char: c, n: run}
}
