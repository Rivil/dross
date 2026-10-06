package ship

import (
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

// forgejoThread is a scripted Forgejo/Gitea API: paths map to JSON answers,
// and a path missing from it answers 404. hits counts every request.
type forgejoThread struct {
	answers  map[string]string
	status   map[string]int
	hits     atomic.Int32
	paged    bool // honour page=&limit= on the issue-comment list
	maxItems int  // cap every page at this many, as [api] MAX_RESPONSE_ITEMS does; 0 = no cap
	conv     []map[string]any
}

func (f *forgejoThread) serve(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.hits.Add(1)
		if r.Header.Get("Authorization") != "token forge-secret" {
			http.Error(w, "no auth", http.StatusUnauthorized)
			return
		}
		key := r.URL.Path
		if p := r.URL.Query().Get("page"); p != "" {
			key += "?page=" + p
		}
		if code, ok := f.status[key]; ok {
			w.WriteHeader(code)
			_, _ = w.Write([]byte(`{"message":"boom"}`))
			return
		}
		if strings.HasSuffix(r.URL.Path, "/issues/7/comments") && f.conv != nil {
			items := f.conv
			if f.paged {
				page, _ := strconv.Atoi(r.URL.Query().Get("page"))
				limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
				if f.maxItems > 0 {
					limit = min(limit, f.maxItems)
				}
				lo := min((page-1)*limit, len(items))
				items = items[lo:min(lo+limit, len(items))]
			}
			b, _ := json.Marshal(items)
			_, _ = w.Write(b)
			return
		}
		body, ok := f.answers[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func forgejoOpts(t *testing.T, provider, apiBase string) OpenOpts {
	t.Setenv("FORGE_TOKEN", "forge-secret")
	return OpenOpts{
		Provider: provider, URL: "https://forge.example/acme/widgets", APIBase: apiBase + "/api/v1",
		AuthEnv: "FORGE_TOKEN", Hosts: hostallow.Derive(apiBase, nil),
	}
}

func fjComment(id int, login, body string) map[string]any {
	return map[string]any{
		"id": id, "body": body, "html_url": fmt.Sprintf("https://forge.example/acme/widgets/pulls/7#c%d", id),
		"user": map[string]any{"id": id * 10, "login": login},
	}
}

func fjJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

const fjBase = "/api/v1/repos/acme/widgets"

func TestForgejoPRCommentKinds(t *testing.T) {
	for _, provider := range []string{"forgejo", "gitea"} {
		t.Run(provider, func(t *testing.T) {
			inline := fjComment(3, "carol", "rename this")
			inline["path"], inline["position"] = "internal/x.go", 42
			review := fjComment(2, "bob", "two notes")
			review["state"] = "COMMENT"
			f := &forgejoThread{conv: []map[string]any{fjComment(1, "alice", "why?")}, answers: map[string]string{
				fjBase + "/pulls/7/reviews":            fjJSON([]any{review}),
				fjBase + "/pulls/7/reviews/2/comments": fjJSON([]any{inline}),
			}}
			srv := f.serve(t)
			got, err := ListPRComments(forgejoOpts(t, provider, srv.URL), 7)
			if err != nil {
				t.Fatal(err)
			}
			want := []PRComment{
				{ID: "1", Kind: CommentConversation, Author: CommentAuthor{ID: "10", Login: "alice", Bot: AuthorUnknown},
					URL: "https://forge.example/acme/widgets/pulls/7#c1", Body: "why?"},
				{ID: "2", Kind: CommentReview, Author: CommentAuthor{ID: "20", Login: "bob", Bot: AuthorUnknown},
					URL: "https://forge.example/acme/widgets/pulls/7#c2", Body: "two notes"},
				{ID: "3", Kind: CommentInline, Author: CommentAuthor{ID: "30", Login: "carol", Bot: AuthorUnknown},
					Path: "internal/x.go", Line: 42, URL: "https://forge.example/acme/widgets/pulls/7#c3", Body: "rename this"},
			}
			if len(got) != len(want) {
				t.Fatalf("got %d comments, want %d: %+v", len(got), len(want), got)
			}
			for i := range want {
				if got[i] != want[i] {
					t.Errorf("comment %d = %+v, want %+v", i, got[i], want[i])
				}
			}
		})
	}

	t.Run("an old-side line", func(t *testing.T) {
		inline := fjComment(3, "carol", "removed line")
		inline["path"], inline["position"], inline["original_position"] = "a.go", 0, 9
		f := &forgejoThread{conv: []map[string]any{}, answers: map[string]string{
			fjBase + "/pulls/7/reviews":            fjJSON([]any{fjComment(2, "bob", "")}),
			fjBase + "/pulls/7/reviews/2/comments": fjJSON([]any{inline}),
		}}
		got, err := ListPRComments(forgejoOpts(t, "forgejo", f.serve(t).URL), 7)
		if err != nil || len(got) != 1 || got[0].Line != 9 {
			t.Errorf("got %+v, %v; want one inline comment on line 9", got, err)
		}
	})
}

func TestForgejoPRCommentNoise(t *testing.T) {
	answers := map[string]string{
		fjBase + "/pulls/7/reviews": fjJSON([]any{
			fjComment(2, "bob", ""), fjComment(3, "bob", "  \n"),
			map[string]any{"id": 4, "state": "PENDING", "body": "my draft", "user": map[string]any{"id": 1, "login": "x"}},
		}),
	}
	for _, r := range []int{2, 3} {
		answers[fmt.Sprintf("%s/pulls/7/reviews/%d/comments", fjBase, r)] = fjJSON([]any{
			fjComment(r*10+1, "bob", "a"), fjComment(r*10+2, "bob", "b"),
		})
	}
	f := &forgejoThread{conv: []map[string]any{}, answers: answers}
	got, err := ListPRComments(forgejoOpts(t, "forgejo", f.serve(t).URL), 7)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 4 {
		t.Fatalf("got %d comments, want the 4 inline ones: %+v", len(got), got)
	}
	for _, c := range got {
		if c.Kind != CommentInline {
			t.Errorf("comment %s kind %q: an empty or pending review leaked in", c.ID, c.Kind)
		}
	}
}

func TestForgejoPRCommentPaging(t *testing.T) {
	var conv []map[string]any
	for i := 1; i <= forgejoCommentPage+1; i++ {
		conv = append(conv, fjComment(i, "alice", "c"))
	}
	answers := map[string]string{fjBase + "/pulls/7/reviews": "[]"}

	t.Run("a paging server", func(t *testing.T) {
		f := &forgejoThread{conv: conv, paged: true, answers: answers}
		got, err := ListPRComments(forgejoOpts(t, "forgejo", f.serve(t).URL), 7)
		if err != nil || len(got) != forgejoCommentPage+1 {
			t.Fatalf("got %d, %v; want %d", len(got), err, forgejoCommentPage+1)
		}
	})

	t.Run("a server that ignores paging", func(t *testing.T) {
		f := &forgejoThread{conv: conv, answers: answers}
		got, err := ListPRComments(forgejoOpts(t, "gitea", f.serve(t).URL), 7)
		if err != nil || len(got) != forgejoCommentPage+1 {
			t.Fatalf("got %d, %v; want %d with no duplicates", len(got), err, forgejoCommentPage+1)
		}
	})

	t.Run("a server capping pages below the limit", func(t *testing.T) {
		var many []map[string]any
		for i := 1; i <= 70; i++ {
			many = append(many, fjComment(i, "alice", "c"))
		}
		f := &forgejoThread{conv: many, paged: true, maxItems: 30, answers: answers}
		got, err := ListPRComments(forgejoOpts(t, "forgejo", f.serve(t).URL), 7)
		if err != nil || len(got) != 70 {
			t.Fatalf("got %d, %v; want all 70 — a capped page is not the last page", len(got), err)
		}
	})

	t.Run("a 500 on page 2", func(t *testing.T) {
		f := &forgejoThread{conv: conv, paged: true, answers: answers,
			status: map[string]int{fjBase + "/issues/7/comments?page=2": 500}}
		got, err := ListPRComments(forgejoOpts(t, "forgejo", f.serve(t).URL), 7)
		if err == nil || got != nil {
			t.Errorf("got %d comments, %v; want (nil, err), never page 1", len(got), err)
		}
	})

	t.Run("pages that never end", func(t *testing.T) {
		prev := prThreadMaxPages
		prThreadMaxPages = 2
		t.Cleanup(func() { prThreadMaxPages = prev })
		var many []map[string]any
		for i := 1; i <= 3*forgejoCommentPage; i++ {
			many = append(many, fjComment(i, "alice", "c"))
		}
		f := &forgejoThread{conv: many, paged: true, answers: answers}
		got, err := ListPRComments(forgejoOpts(t, "forgejo", f.serve(t).URL), 7)
		if err == nil || !strings.Contains(err.Error(), "may be truncated") || got != nil {
			t.Errorf("got %d, %v; want a may-be-truncated refusal", len(got), err)
		}
	})
}

func TestForgejoAuthorUnknown(t *testing.T) {
	f := &forgejoThread{conv: []map[string]any{
		fjComment(1, "renovate-bot", "bump"), fjComment(2, "dependabot[bot]", "bump"), fjComment(3, "alice", "hi"),
		{"id": 4, "body": "ghost", "html_url": "https://forge.example/x", "user": nil},
	}, answers: map[string]string{fjBase + "/pulls/7/reviews": "[]"}}
	got, err := ListPRComments(forgejoOpts(t, "forgejo", f.serve(t).URL), 7)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range got {
		if c.Author.Bot != AuthorUnknown {
			t.Errorf("%s by %q reads %q, want unknown", c.ID, c.Author.Login, c.Author.Bot)
		}
	}
	if got[3].Author != (CommentAuthor{Bot: AuthorUnknown}) {
		t.Errorf("a null user maps to %+v", got[3].Author)
	}
}

func TestForgejoHostRefused(t *testing.T) {
	f := &forgejoThread{conv: []map[string]any{}}
	srv := f.serve(t)
	t.Setenv("FORGE_TOKEN", "forge-secret")
	opts := OpenOpts{Provider: "forgejo", URL: "https://forge.example/acme/widgets", APIBase: srv.URL + "/api/v1",
		AuthEnv: "FORGE_TOKEN", Hosts: hostallow.Derive("https://forge.example", nil)}
	calls := []error{}
	_, err := ListPRComments(opts, 7)
	calls = append(calls, err)
	_, err = PRHeadOf(opts, 7)
	calls = append(calls, err)
	_, err = AuthenticatedUser(opts)
	calls = append(calls, err)
	for i, err := range calls {
		if !errors.Is(err, hostallow.ErrRefused) {
			t.Errorf("call %d: %v, want a host refusal", i+1, err)
		}
	}
	if n := f.hits.Load(); n != 0 {
		t.Errorf("the off-allowlist server saw %d requests", n)
	}
}

func TestForgejoScrubsToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("forbidden for Authorization: " + r.Header.Get("Authorization")))
	}))
	t.Cleanup(srv.Close)
	opts := forgejoOpts(t, "forgejo", srv.URL)
	_, err1 := ListPRComments(opts, 7)
	_, err2 := PRHeadOf(opts, 7)
	_, err3 := AuthenticatedUser(opts)
	for i, err := range []error{err1, err2, err3} {
		if err == nil || strings.Contains(err.Error(), "forge-secret") || !strings.Contains(err.Error(), "[redacted $FORGE_TOKEN]") {
			t.Errorf("call %d error %v, want the token scrubbed to [redacted $FORGE_TOKEN]", i+1, err)
		}
	}
}

func TestForgejoHeadAndCaller(t *testing.T) {
	for _, tc := range []struct {
		name, pull  string
		cross, open bool
	}{
		{"same repo", `{"state":"open","html_url":"https://forge.example/acme/widgets/pulls/7","head":{"ref":"phase/x","repo":{"full_name":"acme/widgets"}},"base":{"ref":"main","repo":{"full_name":"acme/widgets"}}}`, false, true},
		{"fork", `{"state":"open","head":{"ref":"phase/x","repo":{"full_name":"mallory/widgets"}},"base":{"ref":"main","repo":{"full_name":"acme/widgets"}}}`, true, true},
		{"deleted fork", `{"state":"open","head":{"ref":"phase/x","repo":null},"base":{"ref":"main","repo":{"full_name":"acme/widgets"}}}`, true, true},
		{"closed", `{"state":"closed","head":{"ref":"phase/x","repo":{"full_name":"acme/widgets"}},"base":{"ref":"main","repo":{"full_name":"acme/widgets"}}}`, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &forgejoThread{answers: map[string]string{fjBase + "/pulls/7": tc.pull}}
			head, err := PRHeadOf(forgejoOpts(t, "gitea", f.serve(t).URL), 7)
			if err != nil {
				t.Fatal(err)
			}
			if head.Ref != "phase/x" || head.CrossRepo != tc.cross || head.Open != tc.open {
				t.Errorf("head = %+v, want cross=%v open=%v", head, tc.cross, tc.open)
			}
		})
	}
	t.Run("no head branch", func(t *testing.T) {
		f := &forgejoThread{answers: map[string]string{fjBase + "/pulls/7": `{"state":"open","head":{"ref":""}}`}}
		if head, err := PRHeadOf(forgejoOpts(t, "forgejo", f.serve(t).URL), 7); err == nil || head != (PRHead{}) {
			t.Errorf("got %+v, %v", head, err)
		}
	})
	t.Run("a 404", func(t *testing.T) {
		f := &forgejoThread{answers: map[string]string{}}
		if _, err := PRHeadOf(forgejoOpts(t, "forgejo", f.serve(t).URL), 7); err == nil || !strings.Contains(err.Error(), "404") {
			t.Errorf("err = %v, want the 404", err)
		}
	})

	f := &forgejoThread{answers: map[string]string{"/api/v1/user": `{"id":7,"login":"rivil"}`}}
	acct, err := AuthenticatedUser(forgejoOpts(t, "forgejo", f.serve(t).URL))
	if err != nil || acct != (Account{ID: "7", Login: "rivil"}) {
		t.Errorf("account = %+v, %v", acct, err)
	}
	f = &forgejoThread{answers: map[string]string{"/api/v1/user": `{"id":0}`}}
	if acct, err := AuthenticatedUser(forgejoOpts(t, "forgejo", f.serve(t).URL)); err == nil || acct != (Account{}) {
		t.Errorf("an empty account = %+v, %v", acct, err)
	}
}
