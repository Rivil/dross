package prtriage

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	"github.com/Rivil/dross/internal/secretscan"
)

// ReplyBody drafts the one reply /dross-respond posts on PR pr: ReplyMarker as
// its first line, then one bullet per rejected resolution of that PR not yet
// posted, sorted by id — a link to the comment, its author's login in a code
// span, and the reason. Accepted and routed comments get no reply: agreeing
// says nothing the new task does not. ("", false) when there is nothing to
// post.
//
// Everything in a bullet is someone's text or a hand-editable record, so each
// value is redacted and made one line, and an @ becomes ＠ — the reply
// mentions no one and cannot forge a heading, a bullet or a second marker.
func ReplyBody(rec Record, pr int) (string, bool) {
	var rejects []Resolution
	for _, r := range rec.Resolution {
		if r.PR == pr && r.Verdict == VerdictReject && !r.Posted {
			rejects = append(rejects, r)
		}
	}
	if len(rejects) == 0 {
		return "", false
	}
	sort.Slice(rejects, func(i, j int) bool { return rejects[i].ID < rejects[j].ID })
	var b strings.Builder
	b.WriteString(ReplyMarker + "\n\nNot acted on, with the reason for each:\n\n")
	for _, r := range rejects {
		fmt.Fprintf(&b, "- [%s](%s): `` %s `` — %s\n",
			replyLinkText(r.ID), replyURL(r.URL), replyText(r.Author), replyText(r.Reason))
	}
	return b.String(), true
}

// replyText is a record value as reply prose: redacted, one line, every
// backtick escaped (so no run outlasts the double-backtick code span) and no
// @ that could mention anyone.
func replyText(s string) string {
	red, _ := secretscan.Redact(s)
	return strings.ReplaceAll(OneLine(red), "@", "＠")
}

// replyLinkText is replyText that also cannot close the link text early. The
// link characters — backslash first, so a `\]` cannot become an escaped
// backslash and a live bracket — are escaped in the raw value, before OneLine
// adds its own backslash to each backtick; escaping after would undo those.
func replyLinkText(s string) string {
	red, _ := secretscan.Redact(s)
	esc := strings.NewReplacer(`\`, `\\`, "[", `\[`, "]", `\]`).Replace(red)
	return strings.ReplaceAll(OneLine(esc), "@", "＠")
}

// replyURL is a link destination nothing can break out of: redacted, and
// every byte outside a plain URL's characters percent-encoded — whitespace,
// controls, parentheses, angle brackets, backticks and @ among them.
func replyURL(u string) string {
	red, _ := secretscan.Redact(u)
	var b strings.Builder
	for i := 0; i < len(red); i++ {
		c := red[i]
		switch {
		case 'a' <= c && c <= 'z', 'A' <= c && c <= 'Z', '0' <= c && c <= '9',
			strings.IndexByte("-._~:/?#&=+,;%!$*'", c) >= 0:
			b.WriteByte(c)
		default:
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}

// ReplyDigest is the token a reply approval carries: the sha256 of the exact
// body the human was shown, so an approval spends on that draft and no other.
func ReplyDigest(body string) string {
	sum := sha256.Sum256([]byte(body))
	return hex.EncodeToString(sum[:])
}

// MarkPosted records that the resolutions named by ids went out in a reply.
func MarkPosted(rec *Record, ids []string) {
	posted := make(map[string]bool, len(ids))
	for _, id := range ids {
		posted[id] = true
	}
	for i := range rec.Resolution {
		if posted[rec.Resolution[i].ID] {
			rec.Resolution[i].Posted = true
		}
	}
}
