package debugsession

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Rivil/dross/internal/testlane"
)

// MinSignals is how many Resolution signals a fixed close needs (the locked
// close_gates decision). The CLI counts them; judging that they are
// independent is the prompt's job.
const MinSignals = 2

// The header lines a close adds beside the rewritten status line.
const (
	ClosedKey = "closed"
	ReasonKey = "reason"
)

// FixedCloseGaps names every reason a `--fixed` close must be refused, all at
// once so one refusal says everything that is missing. Every Problem is a gap:
// a malformed session's counts cannot be trusted, so a duplicated Resolution
// never adds up to two signals. No gap quotes the session's own text.
func FixedCloseGaps(s Session) []string {
	var gaps []string
	for _, p := range s.Problems {
		gaps = append(gaps, "the session is malformed: "+p)
	}
	if s.Signals < MinSignals {
		gaps = append(gaps, fmt.Sprintf("Resolution lists %d of %d signals", s.Signals, MinSignals))
	}
	if len(s.Prevention) == 0 {
		gaps = append(gaps, "Prevention is empty")
	}
	return gaps
}

// CloseText closes a session's text: it rewrites the header status line to
// to — resolved or abandoned — and adds a `closed:` line after it, plus a
// `reason:` line when abandoned. Every byte from the first `## ` heading on
// is left as it was, and the status line keeps its line ending. On error it
// returns raw unchanged.
func CloseText(raw []byte, to State, reason string, now time.Time) ([]byte, error) {
	if to != StateResolved && to != StateAbandoned {
		return raw, fmt.Errorf("invalid close state %q: want %s or %s", to, StateResolved, StateAbandoned)
	}
	if to == StateAbandoned {
		// The line check runs before trimming: a trailing \r is still a
		// second line to anything that splits on it.
		switch {
		case strings.ContainsAny(reason, "\r\n"):
			return raw, errors.New("invalid abandon reason: it must be a single line")
		case strings.Contains(reason, "<!--"):
			// A comment opened in the header would hide every section below it.
			return raw, errors.New("invalid abandon reason: it must not open an <!-- comment")
		case strings.TrimSpace(reason) == "":
			return raw, errors.New("an abandon reason is required")
		}
	}
	reason = strings.TrimSpace(reason)

	// Find the status line exactly as Parse does — outside fences and
	// comments, above the first `## ` — so the line rewritten is the line read.
	p := parser{seen: map[string]int{}, signals: map[string]bool{}}
	lines := strings.Split(string(raw), "\n")
	for i, line := range lines {
		p.line(i+1, strings.TrimSuffix(line, "\r"))
	}
	switch len(p.statusLines) {
	case 0:
		return raw, fmt.Errorf("invalid session: its header has no %q line to close", StatusKey+":")
	case 1:
	default:
		return raw, fmt.Errorf("invalid session: its header carries %d %q lines; fix it to one before closing", len(p.statusLines), StatusKey+":")
	}
	if v := State(p.statusValue); v == StateResolved || v == StateAbandoned {
		return raw, fmt.Errorf("invalid close: the session is already %s", v)
	}

	at := p.statusLines[0] - 1
	eol := ""
	if strings.HasSuffix(lines[at], "\r") {
		eol = "\r"
	}
	head := []string{
		fmt.Sprintf("%s: %s%s", StatusKey, to, eol),
		fmt.Sprintf("%s: %s%s", ClosedKey, now.UTC().Format(time.RFC3339), eol),
	}
	if to == StateAbandoned {
		head = append(head, fmt.Sprintf("%s: %s%s", ReasonKey, reason, eol))
	}
	out := []byte(strings.Join(append(append(append([]string{}, lines[:at]...), head...), lines[at+1:]...), "\n"))
	// A success must read closed. The rewrite replaces the whole raw line, so a
	// comment marker sharing it would be lost; refuse rather than write a
	// session that still reads open.
	if Parse(out).Status != to {
		return raw, fmt.Errorf("invalid session: its %q line shares a line with a comment marker; put it on its own line before closing", StatusKey+":")
	}
	return out, nil
}

// RuleText flattens a Prevention section into one rule line: the lines Parse
// kept (comments and fenced blocks already dropped), list markers stripped,
// whitespace runs collapsed, joined with "; ". Rules are one-line MUST-FOLLOWs,
// so the result never holds a line break.
func RuleText(s Session) string {
	var parts []string
	for _, line := range s.Prevention {
		if item, ok := listItem(line); ok {
			line = item
		}
		if line = strings.Join(strings.Fields(line), " "); line != "" {
			parts = append(parts, line)
		}
	}
	return strings.Join(parts, "; ")
}

// RuleAddCommand is the ready-to-run `dross rule add` line carrying the
// session's Prevention, or "" when it has none. The text sits behind `--`, so
// one starting with a dash is never read as a flag, and is single-quoted for a
// POSIX shell. Printing it is all a close does: adding a rule is the user's
// call (the locked prevention_rule decision).
func RuleAddCommand(s Session) string {
	text := RuleText(s)
	if text == "" {
		return ""
	}
	return "dross rule add -- " + testlane.ShellQuote(text)
}
