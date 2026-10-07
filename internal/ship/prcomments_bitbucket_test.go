package ship

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Rivil/dross/internal/hostallow"
)

const bbPullPath = "/2.0/repositories/acme/widgets/pullrequests/7"

// bbThread is a scripted Bitbucket Cloud API over PR #7.
type bbThread struct {
	pull     string
	comments []map[string]any
	next     string         // the `next` URL every non-final page carries
	status   map[string]int // path or path?page=N -> status
	hits     atomic.Int32
}

func (b *bbThread) serve(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b.hits.Add(1)
		want := "Basic " + base64.StdEncoding.EncodeToString([]byte("rivil:bb-secret"))
		if r.Header.Get("Authorization") != want {
			http.Error(w, "no auth", http.StatusUnauthorized)
			return
		}
		key := r.URL.Path
		if p := r.URL.Query().Get("page"); p != "" {
			key += "?page=" + p
		}
		if code, ok := b.status[key]; ok {
			w.WriteHeader(code)
			_, _ = w.Write([]byte(`{"error":{"message":"boom"}}`))
			return
		}
		switch r.URL.Path {
		case bbPullPath:
			_, _ = w.Write([]byte(b.pull))
		case bbPullPath + "/comments":
			page, _ := strconv.Atoi(r.URL.Query().Get("page"))
			per, _ := strconv.Atoi(r.URL.Query().Get("pagelen"))
			lo := min((page-1)*per, len(b.comments))
			hi := min(lo+per, len(b.comments))
			body := map[string]any{"values": b.comments[lo:hi]}
			if hi < len(b.comments) {
				body["next"] = b.next
			}
			out, _ := json.Marshal(body)
			_, _ = w.Write(out)
		case "/2.0/user":
			_, _ = w.Write([]byte(`{"uuid":"{caller}","nickname":"rivil","display_name":"rivil","type":"user"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	if b.next == "" {
		b.next = srv.URL + bbPullPath + "/comments?page=99"
	}
	return srv
}

func bbOpts(t *testing.T, apiBase string) OpenOpts {
	t.Setenv("BB_TOKEN", "bb-secret")
	return OpenOpts{
		Provider: "bitbucket", URL: "https://bitbucket.org/acme/widgets", APIBase: apiBase + "/2.0",
		AuthEnv: "BB_TOKEN", AuthUser: "rivil", Hosts: hostallow.Derive(apiBase, nil),
	}
}

func bbC(id int, uuid, name, typ, body string) map[string]any {
	return map[string]any{
		"id": id, "content": map[string]any{"raw": body},
		"user":  map[string]any{"uuid": uuid, "nickname": name, "display_name": name, "type": typ},
		"links": map[string]any{"html": map[string]any{"href": fmt.Sprintf("https://bitbucket.org/acme/widgets/pull-requests/7#comment-%d", id)}},
	}
}

func bbInlineC(id int, path string, to, from any) map[string]any {
	c := bbC(id, "{u2}", "bob", "user", "on a line")
	c["inline"] = map[string]any{"path": path, "to": to, "from": from}
	return c
}

func TestBitbucketPRCommentKinds(t *testing.T) {
	b := &bbThread{comments: []map[string]any{
		bbC(1, "{u1}", "alice", "user", "why?"),
		bbInlineC(2, "internal/x.go", 42, nil),
		bbInlineC(3, "internal/y.go", nil, 9),
	}}
	got, err := ListPRComments(bbOpts(t, b.serve(t).URL), 7)
	if err != nil {
		t.Fatal(err)
	}
	want := []PRComment{
		{ID: "1", Kind: CommentConversation, Author: CommentAuthor{ID: "{u1}", Login: "alice", Bot: AuthorHuman},
			URL: "https://bitbucket.org/acme/widgets/pull-requests/7#comment-1", Body: "why?"},
		{ID: "2", Kind: CommentInline, Author: CommentAuthor{ID: "{u2}", Login: "bob", Bot: AuthorHuman},
			Path: "internal/x.go", Line: 42, URL: "https://bitbucket.org/acme/widgets/pull-requests/7#comment-2", Body: "on a line"},
		{ID: "3", Kind: CommentInline, Author: CommentAuthor{ID: "{u2}", Login: "bob", Bot: AuthorHuman},
			Path: "internal/y.go", Line: 9, URL: "https://bitbucket.org/acme/widgets/pull-requests/7#comment-3", Body: "on a line"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("comment %d = %+v, want %+v", i, got[i], want[i])
		}
		if got[i].Kind == CommentReview {
			t.Errorf("comment %d has the review kind Bitbucket does not have", i)
		}
	}
}

func TestBitbucketDeletedDropped(t *testing.T) {
	gone := bbC(2, "{u1}", "alice", "user", "")
	gone["deleted"] = true
	b := &bbThread{comments: []map[string]any{bbC(1, "{u1}", "alice", "user", "kept"), gone}}
	got, err := ListPRComments(bbOpts(t, b.serve(t).URL), 7)
	if err != nil || len(got) != 1 || got[0].ID != "1" {
		t.Errorf("got %+v, %v; want only comment 1", got, err)
	}
}

func TestBitbucketNeverFollowsNext(t *testing.T) {
	var elsewhere atomic.Int32
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		elsewhere.Add(1)
		_, _ = w.Write([]byte(`{"values":[]}`))
	}))
	t.Cleanup(other.Close)
	var comments []map[string]any
	for i := 1; i <= bbCommentPage+5; i++ {
		comments = append(comments, bbC(i, "{u1}", "alice", "user", "c"))
	}
	b := &bbThread{comments: comments, next: other.URL + "/steal?page=2"}
	got, err := ListPRComments(bbOpts(t, b.serve(t).URL), 7)
	if err != nil || len(got) != bbCommentPage+5 {
		t.Fatalf("got %d, %v; want every page via page=", len(got), err)
	}
	if n := elsewhere.Load(); n != 0 {
		t.Errorf("the host named in `next` saw %d requests", n)
	}
}

func TestBitbucketPRCommentPaging(t *testing.T) {
	var comments []map[string]any
	for i := 1; i <= bbCommentPage+1; i++ {
		comments = append(comments, bbC(i, "{u1}", "alice", "user", "c"))
	}
	b := &bbThread{comments: comments}
	if got, err := ListPRComments(bbOpts(t, b.serve(t).URL), 7); err != nil || len(got) != bbCommentPage+1 {
		t.Fatalf("got %d, %v; want %d", len(got), err, bbCommentPage+1)
	}
	b = &bbThread{comments: comments, status: map[string]int{bbPullPath + "/comments?page=2": 500}}
	if got, err := ListPRComments(bbOpts(t, b.serve(t).URL), 7); err == nil || got != nil {
		t.Errorf("a 500 on page 2: %d comments, %v; want (nil, err)", len(got), err)
	}
	prev := prThreadMaxPages
	prThreadMaxPages = 1
	t.Cleanup(func() { prThreadMaxPages = prev })
	b = &bbThread{comments: comments}
	if got, err := ListPRComments(bbOpts(t, b.serve(t).URL), 7); err == nil || !strings.Contains(err.Error(), "may be truncated") || got != nil {
		t.Errorf("more pages than allowed: %d, %v; want a may-be-truncated refusal", len(got), err)
	}
}

func TestBitbucketAuthorFlag(t *testing.T) {
	b := &bbThread{comments: []map[string]any{
		bbC(1, "{app}", "pipelines", "app_user", "built"),
		bbC(2, "{u1}", "alice", "user", "hi"),
		bbC(3, "{t}", "team", "team", "?"),
		{"id": 4, "content": map[string]any{"raw": "ghost"}, "user": nil},
	}}
	got, err := ListPRComments(bbOpts(t, b.serve(t).URL), 7)
	if err != nil {
		t.Fatal(err)
	}
	want := []AuthorClass{AuthorBot, AuthorHuman, AuthorUnknown, AuthorUnknown}
	for i, w := range want {
		if got[i].Author.Bot != w {
			t.Errorf("comment %s: %q, want %q", got[i].ID, got[i].Author.Bot, w)
		}
	}
}

func TestBitbucketHostRefused(t *testing.T) {
	b := &bbThread{}
	srv := b.serve(t)
	t.Setenv("BB_TOKEN", "bb-secret")
	opts := OpenOpts{Provider: "bitbucket", URL: "https://bitbucket.org/acme/widgets", APIBase: srv.URL + "/2.0",
		AuthEnv: "BB_TOKEN", AuthUser: "rivil", Hosts: hostallow.Derive("https://api.bitbucket.org", nil)}
	_, e1 := ListPRComments(opts, 7)
	_, e2 := PRHeadOf(opts, 7)
	_, e3 := AuthenticatedUser(opts)
	for i, err := range []error{e1, e2, e3} {
		if !errors.Is(err, hostallow.ErrRefused) {
			t.Errorf("call %d: %v, want a host refusal", i+1, err)
		}
	}
	if n := b.hits.Load(); n != 0 {
		t.Errorf("the off-allowlist server saw %d requests", n)
	}
}

func TestBitbucketScrubsToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, tok, _ := r.BasicAuth()
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("forbidden for token " + tok))
	}))
	t.Cleanup(srv.Close)
	opts := bbOpts(t, srv.URL)
	_, e1 := ListPRComments(opts, 7)
	_, e2 := PRHeadOf(opts, 7)
	_, e3 := AuthenticatedUser(opts)
	for i, err := range []error{e1, e2, e3} {
		if err == nil || strings.Contains(err.Error(), "bb-secret") || !strings.Contains(err.Error(), "[redacted $BB_TOKEN]") {
			t.Errorf("call %d error %v, want the token scrubbed", i+1, err)
		}
	}
}

func TestBitbucketHeadAndCaller(t *testing.T) {
	b := &bbThread{comments: []map[string]any{
		bbC(1, "{caller}", "rivil", "user", "mine"),
		bbC(2, "{impostor}", "rivil", "user", "same display name"),
	}}
	srv := b.serve(t)
	opts := bbOpts(t, srv.URL)
	acct, err := AuthenticatedUser(opts)
	if err != nil || acct.ID != "{caller}" || acct.Login != "rivil" {
		t.Fatalf("account = %+v, %v", acct, err)
	}
	got, err := ListPRComments(opts, 7)
	if err != nil {
		t.Fatal(err)
	}
	var mine []string
	for _, c := range got {
		if c.Author.ID == acct.ID {
			mine = append(mine, c.ID)
		}
	}
	if strings.Join(mine, ",") != "1" {
		t.Errorf("comments matched to the caller: %v, want only 1 (uuid, never display_name)", mine)
	}

	for _, tc := range []struct {
		name, pull  string
		cross, open bool
	}{
		{"same repo", `{"state":"OPEN","links":{"html":{"href":"https://bitbucket.org/acme/widgets/pull-requests/7"}},"source":{"branch":{"name":"phase/x"},"repository":{"full_name":"acme/widgets"}},"destination":{"repository":{"full_name":"acme/widgets"}}}`, false, true},
		{"fork", `{"state":"OPEN","source":{"branch":{"name":"phase/x"},"repository":{"full_name":"mallory/widgets"}},"destination":{"repository":{"full_name":"acme/widgets"}}}`, true, true},
		{"merged", `{"state":"MERGED","source":{"branch":{"name":"phase/x"},"repository":{"full_name":"acme/widgets"}},"destination":{"repository":{"full_name":"acme/widgets"}}}`, false, false},
		{"declined", `{"state":"DECLINED","source":{"branch":{"name":"phase/x"},"repository":{"full_name":"acme/widgets"}},"destination":{"repository":{"full_name":"acme/widgets"}}}`, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := &bbThread{pull: tc.pull}
			head, err := PRHeadOf(bbOpts(t, b.serve(t).URL), 7)
			if err != nil {
				t.Fatal(err)
			}
			if head.Ref != "phase/x" || head.CrossRepo != tc.cross || head.Open != tc.open {
				t.Errorf("head = %+v, want cross=%v open=%v", head, tc.cross, tc.open)
			}
		})
	}
	b2 := &bbThread{pull: `{"state":"OPEN","source":{"branch":{"name":""}}}`}
	if head, err := PRHeadOf(bbOpts(t, b2.serve(t).URL), 7); err == nil || head != (PRHead{}) {
		t.Errorf("a PR with no source branch: %+v, %v", head, err)
	}
}
