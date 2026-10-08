package prtriage

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/Rivil/dross/internal/secretscan"
	"github.com/Rivil/dross/internal/ship"
)

// Item is one thing to triage: a whole comment, or one finding of a
// /dross-review comment the caller's own account posted. ID qualifies the
// forge id by kind — c, i or r — because the forge numbers each kind
// separately, and a finding adds #<n>. Text is the untrusted body (or
// finding); Digest is its sha256, how a later run notices an edit.
type Item struct {
	ID     string
	Kind   ship.CommentKind
	Author ship.CommentAuthor
	Loc    string // path:line of an inline comment, or a finding's own locator
	URL    string
	Text   string
	Digest string
}

// kindLetter is the id prefix of each comment kind.
var kindLetter = map[ship.CommentKind]string{
	ship.CommentConversation: "c",
	ship.CommentInline:       "i",
	ship.CommentReview:       "r",
}

// Items turns a PR's comments into triage items. self is the account dross
// acts as. A comment with an empty body is dropped, and so is dross's own
// reply — its first non-blank line is ReplyMarker AND self wrote it; either
// alone is not enough, since anyone can copy the marker. A /dross-review
// comment is split into one item per finding only when self wrote it: any
// other author's comment stays whole, so nothing they wrote around a copied
// header can hide outside the findings.
func Items(comments []ship.PRComment, self ship.Account) []Item {
	var out []Item
	for _, c := range comments {
		if strings.TrimSpace(c.Body) == "" {
			continue
		}
		own := self.ID != "" && c.Author.ID == self.ID
		if own && firstLine(c.Body) == ReplyMarker {
			continue
		}
		letter, ok := kindLetter[c.Kind]
		if !ok {
			continue
		}
		base := Item{ID: letter + c.ID, Kind: c.Kind, Author: c.Author, URL: c.URL}
		if c.Kind == ship.CommentInline && c.Path != "" {
			base.Loc = c.Path
			if c.Line > 0 {
				base.Loc += ":" + strconv.Itoa(c.Line)
			}
		}
		if own {
			if findings, ok := SplitReview(c.Body); ok {
				for _, f := range findings {
					it := base
					it.ID = fmt.Sprintf("%s#%d", base.ID, f.N)
					if f.Loc != "" {
						it.Loc = f.Loc
					}
					it.Text = f.Text
					it.Digest = Digest(f.Text)
					out = append(out, it)
				}
				continue
			}
		}
		base.Text = c.Body
		base.Digest = Digest(c.Body)
		out = append(out, base)
	}
	return out
}

// firstLine is s's first non-blank line, CRLF tolerated.
func firstLine(s string) string {
	for _, l := range strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n") {
		if strings.TrimSpace(l) != "" {
			return strings.TrimRight(l, " \t")
		}
	}
	return ""
}

// Digest is the sha256 of text with CRLF folded to LF, so a client that
// re-saves a comment with other line endings has not edited it.
func Digest(text string) string {
	sum := sha256.Sum256([]byte(strings.ReplaceAll(text, "\r\n", "\n")))
	return hex.EncodeToString(sum[:])
}

// Status is where it stands against rec: "untriaged", "edited (was <verdict>)"
// when its text changed since it was resolved, or the verdict it holds.
func Status(it Item, rec Record) string {
	r, ok := rec.Find(it.ID)
	switch {
	case !ok:
		return "untriaged"
	case r.Digest != it.Digest:
		return "edited (was " + r.Verdict + ")"
	}
	switch r.Verdict {
	case VerdictAccept:
		return "accepted"
	case VerdictReject:
		return "rejected"
	case VerdictRoute:
		return "routed"
	}
	return r.Verdict
}

// Pending is what still needs a verdict: items never resolved, and items
// whose text changed since. It is the one definition of untriaged. A record
// entry whose comment is gone is ignored.
func Pending(items []Item, rec Record) []Item {
	var out []Item
	for _, it := range items {
		if r, ok := rec.Find(it.ID); !ok || r.Digest != it.Digest {
			out = append(out, it)
		}
	}
	return out
}

// ResolutionFrom is the resolution skeleton for it — where it is, who wrote
// it, and the digest it was seen at. The text is never carried over.
func ResolutionFrom(it Item) Resolution {
	return Resolution{
		ID:     it.ID,
		Kind:   string(it.Kind),
		URL:    it.URL,
		Author: it.Author.Login,
		Digest: it.Digest,
	}
}

// Render writes each item as one metadata line and its fenced text. The
// metadata line is redacted and made one line as a whole; the text is
// redacted, then fenced as untrusted data.
func Render(w io.Writer, items []Item, rec Record) error {
	for _, it := range items {
		fields := []string{it.ID, string(it.Kind), "@" + it.Author.Login, string(it.Author.Bot)}
		if it.Loc != "" {
			fields = append(fields, it.Loc)
		}
		fields = append(fields, Status(it, rec), "seen="+it.Digest)
		meta, _ := secretscan.Redact(strings.Join(fields, " · "))
		body, _ := secretscan.Redact(it.Text)
		if _, err := fmt.Fprintf(w, "%s\n%s\n\n", OneLine(meta), Fence(body)); err != nil {
			return err
		}
	}
	return nil
}
