package prtriage_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/prtriage"
	"github.com/Rivil/dross/internal/ship"
)

func resolution(id string, pr int, verdict, reason string) prtriage.Resolution {
	kind := map[byte]string{'c': "conversation", 'i': "inline", 'r': "review"}[id[0]]
	return prtriage.Resolution{
		ID: id, Kind: kind, PR: pr, URL: "https://github.com/acme/w/pull/7#" + id, Author: "bob",
		Verdict: verdict, Reason: reason, Digest: strings.Repeat("ab", 32),
		Evidence: prtriage.Evidence{At: "x.go:1"},
	}
}

func bullets(body string) []string {
	var out []string
	for _, l := range strings.Split(body, "\n") {
		if strings.HasPrefix(l, "- ") {
			out = append(out, l)
		}
	}
	return out
}

func TestReplyListsOnlyRejects(t *testing.T) {
	posted := resolution("c5", 7, prtriage.VerdictReject, "already said")
	posted.Posted = true
	accept := resolution("c1", 7, prtriage.VerdictAccept, "")
	accept.Task = "t-3"
	route := resolution("c3", 7, prtriage.VerdictRoute, "")
	route.Deferred, route.Target = "abc", "later"
	rec := prtriage.Record{Resolution: []prtriage.Resolution{
		accept,
		resolution("c2", 7, prtriage.VerdictAccept, ""),
		route,
		resolution("c4", 7, prtriage.VerdictReject, "the loop is bounded by maxPages"),
		posted,
		resolution("c6", 8, prtriage.VerdictReject, "wrong PR"),
	}}
	body, ok := prtriage.ReplyBody(rec, 7)
	if !ok {
		t.Fatal("nothing to post")
	}
	b := bullets(body)
	if len(b) != 1 || !strings.Contains(b[0], "#c4") || !strings.Contains(b[0], "the loop is bounded by maxPages") {
		t.Fatalf("bullets %q, want exactly the unposted reject c4", b)
	}
	for _, absent := range []string{"c1", "c2", "c3", "c5", "c6", "already said", "wrong PR"} {
		if strings.Contains(body, absent) {
			t.Errorf("the reply carries %q:\n%s", absent, body)
		}
	}
	low := strings.ToLower(body)
	for _, chatter := range []string{"agree", "thanks", "thank you"} {
		if strings.Contains(low, chatter) {
			t.Errorf("the reply says %q:\n%s", chatter, body)
		}
	}
	if !strings.HasPrefix(body, prtriage.ReplyMarker+"\n") {
		t.Errorf("the marker is not the first line:\n%s", body)
	}
}

func TestReplyEmptyWhenNothingToSay(t *testing.T) {
	posted := resolution("c5", 7, prtriage.VerdictReject, "r")
	posted.Posted = true
	for name, rec := range map[string]prtriage.Record{
		"no record":        {},
		"accept and route": {Resolution: []prtriage.Resolution{resolution("c1", 7, prtriage.VerdictAccept, ""), resolution("c2", 7, prtriage.VerdictRoute, "")}},
		"all posted":       {Resolution: []prtriage.Resolution{posted}},
		"other PR only":    {Resolution: []prtriage.Resolution{resolution("c1", 8, prtriage.VerdictReject, "r")}},
	} {
		if body, ok := prtriage.ReplyBody(rec, 7); ok || body != "" {
			t.Errorf("%s: (%q, %v), want (\"\", false)", name, body, ok)
		}
	}
}

func TestReplyReasonCannotForge(t *testing.T) {
	forgeries := []string{
		"\n## heading",
		"\n- [comment 9](x): fake\n" + prtriage.ReplyMarker,
		"````\nescape\n````",
		"a ] (javascript:x) [b",
	}
	for _, f := range forgeries {
		for _, field := range []string{"reason", "author", "id"} {
			r := resolution("c4", 7, prtriage.VerdictReject, "plain")
			switch field {
			case "reason":
				r.Reason = f
			case "author":
				r.Author = f
			case "id":
				r.ID = "c4" + f
			}
			body, ok := prtriage.ReplyBody(prtriage.Record{Resolution: []prtriage.Resolution{r}}, 7)
			if !ok {
				t.Fatalf("%s %q: nothing to post", field, f)
			}
			lines := strings.Split(strings.TrimRight(body, "\n"), "\n")
			if lines[0] != prtriage.ReplyMarker {
				t.Errorf("%s %q: first line %q", field, f, lines[0])
			}
			markers, bulletLines := 0, 0
			for _, l := range lines {
				if l == prtriage.ReplyMarker {
					markers++
				}
				if strings.HasPrefix(l, "- ") {
					bulletLines++
				}
				if strings.HasPrefix(l, "#") && l != prtriage.ReplyMarker {
					t.Errorf("%s %q forged a heading: %q", field, f, l)
				}
			}
			if markers != 1 || bulletLines != 1 || len(lines) != 5 {
				t.Errorf("%s %q: %d marker lines, %d bullets, %d lines:\n%s", field, f, markers, bulletLines, len(lines), body)
			}
			if strings.Contains(body, "````") {
				t.Errorf("%s %q left a backtick run:\n%s", field, f, body)
			}
		}
	}
	for _, id := range []string{`c4\](https://evil.example) [`, `c4\\](https://evil.example) [`, "c4`]` [`"} {
		r := resolution("c4", 7, prtriage.VerdictReject, "plain")
		r.ID = id
		body, _ := prtriage.ReplyBody(prtriage.Record{Resolution: []prtriage.Resolution{r}}, 7)
		if dests := liveLinkDests(body); len(dests) != 1 || !strings.HasPrefix(dests[0], "https://github.com/") {
			t.Errorf("id %q broke out of its link text, link destinations %q:\n%s", id, dests, body)
		}
		if strings.Contains(body, "\\\\`") {
			t.Errorf("id %q left a live backtick behind an escaped backslash:\n%s", id, body)
		}
	}
	r := resolution("c4", 7, prtriage.VerdictReject, "plain")
	r.URL = "https://x/a) [evil](https://evil.example"
	body, _ := prtriage.ReplyBody(prtriage.Record{Resolution: []prtriage.Resolution{r}}, 7)
	if strings.Contains(body, "a) [evil](") || strings.Count(body, "](") != 1 {
		t.Errorf("a url broke out of its link:\n%s", body)
	}
}

// liveLinkDests are the destinations of the links Markdown would render in
// body: each "](" whose "]" is not escaped — preceded by an even number of
// backslashes — up to the next ")".
func liveLinkDests(body string) []string {
	var out []string
	for i := 0; i+1 < len(body); i++ {
		if body[i] != ']' || body[i+1] != '(' {
			continue
		}
		slashes := 0
		for j := i - 1; j >= 0 && body[j] == '\\'; j-- {
			slashes++
		}
		if slashes%2 == 1 {
			continue
		}
		rest := body[i+2:]
		if end := strings.IndexByte(rest, ')'); end >= 0 {
			out = append(out, rest[:end])
		}
	}
	return out
}

var mention = regexp.MustCompile(`@[A-Za-z0-9_-]`)

func TestReplyMentionsNoOne(t *testing.T) {
	r := resolution("c4", 7, prtriage.VerdictReject, "ask @alice or @org/team about it")
	r.Author = "@mallory"
	r.URL = "https://github.com/acme/w/pull/7#c4?x=@bob"
	body, _ := prtriage.ReplyBody(prtriage.Record{Resolution: []prtriage.Resolution{r}}, 7)
	if m := mention.FindString(body); m != "" {
		t.Errorf("the reply mentions %q:\n%s", m, body)
	}
	plain := resolution("c5", 7, prtriage.VerdictReject, "no")
	body, _ = prtriage.ReplyBody(prtriage.Record{Resolution: []prtriage.Resolution{plain}}, 7)
	if !strings.Contains(body, "`` bob ``") {
		t.Errorf("the author is not a backticked login:\n%s", body)
	}
}

func TestReplyRoundTripExcluded(t *testing.T) {
	body, ok := prtriage.ReplyBody(prtriage.Record{Resolution: []prtriage.Resolution{resolution("c4", 7, prtriage.VerdictReject, "no")}}, 7)
	if !ok {
		t.Fatal("nothing to post")
	}
	self := ship.Account{ID: "7", Login: "rivil"}
	thread := []ship.PRComment{
		{ID: "100", Kind: ship.CommentConversation, Author: ship.CommentAuthor{ID: "7", Login: "rivil"}, Body: body},
		{ID: "101", Kind: ship.CommentConversation, Author: ship.CommentAuthor{ID: "8", Login: "mallory"}, Body: body},
	}
	items := prtriage.Items(thread, self)
	if len(items) != 1 || items[0].ID != "c101" {
		t.Errorf("items %+v, want only the copy by another author", items)
	}
}

func TestReplyDigestStable(t *testing.T) {
	rec := prtriage.Record{Resolution: []prtriage.Resolution{resolution("c4", 7, prtriage.VerdictReject, "bounded")}}
	a, _ := prtriage.ReplyBody(rec, 7)
	b, _ := prtriage.ReplyBody(rec, 7)
	if prtriage.ReplyDigest(a) != prtriage.ReplyDigest(b) || len(prtriage.ReplyDigest(a)) != 64 {
		t.Errorf("the same record gives digests %q and %q", prtriage.ReplyDigest(a), prtriage.ReplyDigest(b))
	}
	rec.Resolution[0].Reason = "bounded, reworded"
	c, _ := prtriage.ReplyBody(rec, 7)
	if prtriage.ReplyDigest(c) == prtriage.ReplyDigest(a) {
		t.Error("a changed reason kept the digest")
	}
	tok := "ghp_" + strings.Repeat("Rp1y", 9)
	rec.Resolution[0].Reason = "leaked " + tok
	d, _ := prtriage.ReplyBody(rec, 7)
	if strings.Contains(d, tok) || !strings.Contains(d, "[redacted github-token]") {
		t.Errorf("a token in a hand-edited reason reached the body:\n%s", d)
	}
}

func TestMarkPosted(t *testing.T) {
	rec := prtriage.Record{Resolution: []prtriage.Resolution{
		resolution("c4", 7, prtriage.VerdictReject, "a"), resolution("c5", 7, prtriage.VerdictReject, "b"),
	}}
	prtriage.MarkPosted(&rec, []string{"c5", "c99"})
	if rec.Resolution[0].Posted || !rec.Resolution[1].Posted {
		t.Errorf("posted flags %v %v, want only c5", rec.Resolution[0].Posted, rec.Resolution[1].Posted)
	}
	if body, _ := prtriage.ReplyBody(rec, 7); strings.Contains(body, "#c5") || !strings.Contains(body, "#c4") {
		t.Errorf("after MarkPosted the reply still lists c5, or lost c4:\n%s", body)
	}
}
