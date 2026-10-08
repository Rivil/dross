// Package prtriage triages the review comments on a phase's PR: it lists them
// as untrusted data, records a verdict for each, and drafts the one reply that
// carries the rejections back.
package prtriage

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// FenceInfo is the info string every fenced comment body opens with, so a
// reader — human or agent — can tell the fenced text is someone else's.
const FenceInfo = "untrusted-comment"

// Fence wraps an untrusted body in a backtick fence one longer than the
// longest backtick run anywhere in it (at least three), so no line of the body
// can close the fence early. CRLF becomes LF, every control character but tab
// and newline is shown as a visible escape (see escapeRune), and the closer
// always sits on its own line, also after a body with no trailing newline.
// The result has no trailing newline.
func Fence(body string) string {
	text := escapeControls(strings.ReplaceAll(body, "\r\n", "\n"), false)
	if text != "" && !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	fence := strings.Repeat("`", max(3, longestRun(text, '`')+1))
	return fence + FenceInfo + "\n" + text + fence
}

// OneLine renders an untrusted value that sits outside any fence — an author,
// a path, a URL — so it can neither span lines nor open a fence: newline, CR
// and tab are escaped like every other control, and each backtick is
// backslash-escaped, so no two ever touch.
func OneLine(s string) string {
	return strings.ReplaceAll(escapeControls(s, true), "`", "\\`")
}

// longestRun is the length of the longest run of c in s.
func longestRun(s string, c byte) int {
	longest, run := 0, 0
	for i := 0; i < len(s); i++ {
		if s[i] != c {
			run = 0
			continue
		}
		run++
		longest = max(longest, run)
	}
	return longest
}

// escapeControls replaces each rune escapeRune names with its escape. Inside
// a fence tab and newline are kept; on one line they are escaped too.
func escapeControls(s string, oneLine bool) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && size == 1 {
			fmt.Fprintf(&b, `\x%02x`, s[i])
			i++
			continue
		}
		i += size
		if !oneLine && (r == '\n' || r == '\t') {
			b.WriteRune(r)
			continue
		}
		if esc, ok := escapeRune(r); ok {
			b.WriteString(esc)
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// escapeRune is the visible form of a rune that would otherwise act on the
// terminal or reorder what a reader sees: C0 controls (ESC starts a terminal
// sequence, a lone CR rewinds the line), DEL, C1 controls, and the bidi
// embedding, override and isolate controls behind "Trojan Source" spoofing.
func escapeRune(r rune) (string, bool) {
	switch {
	case r == '\n':
		return `\n`, true
	case r == '\t':
		return `\t`, true
	case r == '\r':
		return `\r`, true
	case r < 0x20 || r == 0x7f:
		return fmt.Sprintf(`\x%02x`, r), true
	case r >= 0x80 && r <= 0x9f,
		r >= 0x202a && r <= 0x202e,
		r >= 0x2066 && r <= 0x2069:
		return fmt.Sprintf(`\u%04x`, r), true
	}
	return "", false
}
