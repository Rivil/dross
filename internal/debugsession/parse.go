// Package debugsession reads and writes the .dross/debug/<slug>.md session
// files that /dross-debug keeps: a header carrying the session's status line,
// then seven fixed `## ` sections the agent maintains with Edit.
//
// The CLI reads only the header status line, the fixed headings and the list
// items under them (the locked session_format decision); everything else in a
// session is narrative for the agent. Counting is CommonMark-faithful where it
// matters: fenced blocks (``` and ~~~) and <!-- --> comments are invisible, and
// only column-0 list items count, so quoted command output — or a quoted
// session — can never trip the re-plan stop or fill the close gates.
package debugsession

import (
	"fmt"
	"strings"
)

// State is a session's state as `dross debug list` and `dross status` report
// it. A header carries open, resolved or abandoned; needs-replan is derived.
type State string

const (
	StateOpen        State = "open"
	StateNeedsReplan State = "needs-replan"
	StateResolved    State = "resolved"
	StateAbandoned   State = "abandoned"
)

// StatusKey is the header line's key: `status: <state>`.
const StatusKey = "status"

// The seven fixed section headings, each written as `## <heading>`.
const (
	HeadingSymptom       = "Symptom"
	HeadingHypotheses    = "Hypotheses"
	HeadingCurrentTheory = "Current theory"
	HeadingNextProbe     = "Next probe"
	HeadingFixAttempts   = "Fix attempts"
	HeadingResolution    = "Resolution"
	HeadingPrevention    = "Prevention"
)

// Headings returns the fixed section headings in template order. It is a fresh
// slice each call, so a caller can never reorder the template for the next one.
func Headings() []string {
	return []string{
		HeadingSymptom,
		HeadingHypotheses,
		HeadingCurrentTheory,
		HeadingNextProbe,
		HeadingFixAttempts,
		HeadingResolution,
		HeadingPrevention,
	}
}

// The Fix attempts markers. Each opens a list item (`- [failed] …`); case and
// the spaces inside the brackets are ignored, so `[ Failed ]` counts.
const (
	MarkerFailed = "[failed]"
	MarkerReplan = "[replan]"
)

// ReplanThreshold is how many failed fix attempts since the last re-plan entry
// put an open session in needs-replan (the locked replan_threshold decision).
const ReplanThreshold = 3

// Session is what the CLI reads from a session file. It carries no toml or
// json tags: nothing serialises it, and it holds no paths.
type Session struct {
	// Status is the header's status: open, resolved or abandoned. A missing,
	// duplicated or unrecognised status line reads open (with a Problem), so a
	// mangled session never drops out of the nudges.
	Status State
	// Hypotheses counts the non-empty list items under Hypotheses.
	Hypotheses int
	// FailedSinceReplan counts the [failed] items under Fix attempts below the
	// last [replan] item — the number the threshold reads.
	FailedSinceReplan int
	// Signals counts the distinct non-empty list items under Resolution;
	// items identical once whitespace is normalised count once.
	Signals int
	// Prevention holds the Prevention section's visible lines, trimmed, with
	// comments, fenced blocks, blank lines and empty list items dropped.
	Prevention []string
	// Problems names everything malformed about the file: line faults in
	// reading order, then the unclosed fence or comment, the missing headings
	// and the status line.
	Problems []string
}

// State is the session's reported state. A closed header wins over any
// failure count; an open one reads needs-replan at the threshold.
func (s Session) State() State {
	switch s.Status {
	case StateResolved, StateAbandoned:
		return s.Status
	}
	if s.FailedSinceReplan >= ReplanThreshold {
		return StateNeedsReplan
	}
	return StateOpen
}

// Parse reads a session file. It never fails: whatever it cannot trust is
// named in Problems. A missing, duplicated or unrecognised status line reads
// open, so the session stays in the nudges; a closed header stays closed
// whatever faults its body has, because the header is the session's word on
// whether it is over.
func Parse(b []byte) Session {
	p := parser{seen: map[string]int{}, signals: map[string]bool{}}
	text := strings.ReplaceAll(string(b), "\r\n", "\n")
	for i, line := range strings.Split(text, "\n") {
		p.line(i+1, line)
	}
	return p.finish()
}

type parser struct {
	fence       string // the open fence's opening run (``` or ~~~~…); "" outside one
	fenceLine   int
	inComment   bool
	commentLine int
	inBody      bool   // a `## ` heading has been seen; the header is over
	section     string // the fixed heading whose items count here; "" when none does
	seen        map[string]int
	statusLines []int
	statusValue string
	signals     map[string]bool
	s           Session
}

func (p *parser) problem(format string, args ...any) {
	p.s.Problems = append(p.s.Problems, fmt.Sprintf(format, args...))
}

func (p *parser) line(n int, raw string) {
	if p.fence != "" {
		if closesFence(raw, p.fence) {
			p.fence = ""
		}
		return
	}
	// A fence opener is judged before comments are stripped: a `<!--` in its
	// info string is part of the fence line, not the start of a comment.
	if !p.inComment {
		if run := opensFence(raw); run != "" {
			p.fence, p.fenceLine = run, n
			return
		}
	}
	vis := raw
	if p.inComment || strings.Contains(raw, "<!--") {
		vis = p.stripComments(n, raw)
	}
	if level, title, ok := heading(vis); ok {
		p.heading(n, level, title)
		return
	}
	if !p.inBody {
		p.header(n, vis)
		return
	}
	p.body(vis)
}

// stripComments returns the parts of s outside <!-- --> comments, carrying an
// open comment across lines.
func (p *parser) stripComments(n int, s string) string {
	var b strings.Builder
	for s != "" {
		if p.inComment {
			i := strings.Index(s, "-->")
			if i < 0 {
				break
			}
			s, p.inComment = s[i+len("-->"):], false
			continue
		}
		i := commentStart(s)
		if i < 0 {
			b.WriteString(s)
			break
		}
		b.WriteString(s[:i])
		s, p.inComment, p.commentLine = s[i+len("<!--"):], true, n
	}
	return b.String()
}

// commentStart returns the index of the first `<!--` in s outside an inline
// code span, or -1: an item quoting `<!--` must not hide the lines after it.
// A backtick run with no closing run of the same length is literal text.
func commentStart(s string) int {
	for i := 0; i < len(s); {
		switch {
		case s[i] == '`':
			n := len(s[i:]) - len(strings.TrimLeft(s[i:], "`"))
			if j := backtickRun(s[i+n:], n); j >= 0 {
				i += n + j + n
			} else {
				i += n
			}
		case strings.HasPrefix(s[i:], "<!--"):
			return i
		default:
			i++
		}
	}
	return -1
}

// backtickRun returns the index of the first run of exactly n backticks in s,
// or -1.
func backtickRun(s string, n int) int {
	for i := 0; i < len(s); {
		if s[i] != '`' {
			i++
			continue
		}
		m := len(s[i:]) - len(strings.TrimLeft(s[i:], "`"))
		if m == n {
			return i
		}
		i += m
	}
	return -1
}

// opensFence returns the opening run of a ``` or ~~~ fence on line s — at any
// indent, or straight after a list marker — or "". A backtick run whose info
// string holds a backtick is inline code, not a fence (CommonMark).
func opensFence(s string) string {
	s = strings.TrimLeft(s, " \t")
	if item, ok := listItem(s); ok {
		s = item
	}
	for _, c := range "`~" {
		n := len(s) - len(strings.TrimLeft(s, string(c)))
		if n < 3 {
			continue
		}
		if c == '`' && strings.ContainsRune(s[n:], '`') {
			return ""
		}
		return s[:n]
	}
	return ""
}

// closesFence reports whether raw closes a fence opened with run: the same
// character, at least as many of it, and nothing else on the line.
func closesFence(raw, run string) bool {
	t := strings.TrimSpace(raw)
	return len(t) >= len(run) && strings.Trim(t, run[:1]) == ""
}

// heading parses a column-0 ATX heading: one to six `#` then a space or the
// end of the line.
func heading(s string) (level int, title string, ok bool) {
	level = len(s) - len(strings.TrimLeft(s, "#"))
	if level == 0 || level > 6 {
		return 0, "", false
	}
	rest := s[level:]
	if rest != "" && rest[0] != ' ' && rest[0] != '\t' {
		return 0, "", false
	}
	return level, strings.TrimSpace(rest), true
}

// fixedHeading matches a heading title against the fixed set, ignoring case
// and runs of whitespace, and returns the canonical spelling.
func fixedHeading(title string) (string, bool) {
	norm := strings.Join(strings.Fields(title), " ")
	for _, h := range Headings() {
		if strings.EqualFold(norm, h) {
			return h, true
		}
	}
	return "", false
}

func (p *parser) heading(n, level int, title string) {
	fixed, isFixed := fixedHeading(title)
	if level != 2 {
		// Only `## ` headings open and close sections; any other level leaves
		// the enclosing section counting, so `### Round one` under Fix attempts
		// is a subsection of it. A fixed name at the wrong level is a mistake
		// worth naming — its items count under whatever section encloses it.
		if isFixed {
			p.problem("line %d: %q is a level-%d heading; want \"## %s\"", n, fixed, level, fixed)
		}
		return
	}
	p.inBody = true
	p.section = ""
	if !isFixed {
		return
	}
	if first, dup := p.seen[fixed]; dup {
		p.problem("line %d: duplicate \"## %s\" heading (first on line %d); only the first counts", n, fixed, first)
		return
	}
	p.seen[fixed] = n
	p.section = fixed
}

func (p *parser) header(n int, vis string) {
	key, val, ok := strings.Cut(vis, ":")
	if !ok || !strings.EqualFold(key, StatusKey) {
		return
	}
	p.statusLines = append(p.statusLines, n)
	p.statusValue = strings.ToLower(strings.TrimSpace(val))
}

func (p *parser) body(vis string) {
	if p.section == HeadingPrevention {
		t := strings.TrimSpace(vis)
		if item, isItem := listItem(t); t == "" || (isItem && item == "") {
			return
		}
		p.s.Prevention = append(p.s.Prevention, t)
		return
	}
	item, ok := listItem(vis)
	if !ok || item == "" {
		return
	}
	switch p.section {
	case HeadingHypotheses:
		p.s.Hypotheses++
	case HeadingFixAttempts:
		switch marker(item) {
		case MarkerFailed:
			p.s.FailedSinceReplan++
		case MarkerReplan:
			p.s.FailedSinceReplan = 0
		}
	case HeadingResolution:
		key := strings.Join(strings.Fields(item), " ")
		if !p.signals[key] {
			p.signals[key] = true
			p.s.Signals++
		}
	}
}

// listItem parses a column-0 list item — a `-`, `*` or `+` bullet, or a `1.` /
// `1)` ordered marker — and returns its trimmed text.
func listItem(s string) (string, bool) {
	var rest string
	switch {
	case s == "":
		return "", false
	case strings.ContainsRune("-*+", rune(s[0])):
		rest = s[1:]
	default:
		digits := len(s) - len(strings.TrimLeft(s, "0123456789"))
		if digits == 0 || digits > 9 || digits == len(s) || (s[digits] != '.' && s[digits] != ')') {
			return "", false
		}
		rest = s[digits+1:]
	}
	if rest != "" && rest[0] != ' ' && rest[0] != '\t' {
		return "", false
	}
	return strings.TrimSpace(rest), true
}

// marker returns the canonical Fix attempts marker an item opens with, or "".
func marker(item string) string {
	if !strings.HasPrefix(item, "[") {
		return ""
	}
	inner, _, ok := strings.Cut(item[1:], "]")
	if !ok {
		return ""
	}
	return "[" + strings.ToLower(strings.TrimSpace(inner)) + "]"
}

func (p *parser) finish() Session {
	if p.fence != "" {
		p.problem("line %d: %s fence is never closed", p.fenceLine, p.fence)
	}
	if p.inComment {
		p.problem("line %d: <!-- comment is never closed", p.commentLine)
	}
	for _, h := range Headings() {
		if _, ok := p.seen[h]; !ok {
			p.problem("missing \"## %s\" heading", h)
		}
	}
	p.s.Status = StateOpen
	switch len(p.statusLines) {
	case 0:
		p.problem("header has no %q line; reading open", StatusKey+":")
	case 1:
		switch v := State(p.statusValue); v {
		case StateOpen, StateResolved, StateAbandoned:
			p.s.Status = v
		default:
			p.problem("line %d: header status %q is not %s, %s or %s; reading open",
				p.statusLines[0], p.statusValue, StateOpen, StateResolved, StateAbandoned)
		}
	default:
		p.problem("header carries %d %q lines (lines %s); reading open", len(p.statusLines), StatusKey+":", joinInts(p.statusLines))
	}
	return p.s
}

func joinInts(ns []int) string {
	parts := make([]string, len(ns))
	for i, n := range ns {
		parts[i] = fmt.Sprint(n)
	}
	return strings.Join(parts, ", ")
}
