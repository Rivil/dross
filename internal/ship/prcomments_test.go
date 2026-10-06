package ship

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
)

var prThreadOpts = OpenOpts{Provider: "github", URL: "https://github.com/acme/widgets"}

// jsonOf marshals v for a scripted gh answer, failing the test on error.
func jsonOf(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// ghComment is a scripted thread record. A nil line encodes as JSON null.
func ghComment(id int, login, typ, body string) map[string]any {
	return map[string]any{
		"id":       id,
		"user":     map[string]any{"login": login, "id": id * 10, "type": typ},
		"body":     body,
		"html_url": fmt.Sprintf("https://github.com/acme/widgets/pull/7#c%d", id),
	}
}

func TestGitHubPRCommentKinds(t *testing.T) {
	inline := ghComment(2, "bob", "User", "nit: rename")
	inline["path"] = "internal/x.go"
	inline["line"] = nil
	inline["original_line"] = 42
	review := ghComment(3, "carol", "User", "looks close")
	review["state"] = "COMMENTED"
	calls := scriptAPI(t,
		apiReply{stdout: jsonOf(t, []any{ghComment(1, "alice", "User", "why this?")})},
		apiReply{stdout: jsonOf(t, []any{inline})},
		apiReply{stdout: jsonOf(t, []any{review})},
	)
	got, err := ListPRComments(prThreadOpts, 7)
	if err != nil {
		t.Fatal(err)
	}
	want := []PRComment{
		{ID: "1", Kind: CommentConversation, Author: CommentAuthor{ID: "10", Login: "alice", Bot: AuthorHuman},
			URL: "https://github.com/acme/widgets/pull/7#c1", Body: "why this?"},
		{ID: "2", Kind: CommentInline, Author: CommentAuthor{ID: "20", Login: "bob", Bot: AuthorHuman},
			Path: "internal/x.go", Line: 42, URL: "https://github.com/acme/widgets/pull/7#c2", Body: "nit: rename"},
		{ID: "3", Kind: CommentReview, Author: CommentAuthor{ID: "30", Login: "carol", Bot: AuthorHuman},
			URL: "https://github.com/acme/widgets/pull/7#c3", Body: "looks close"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d comments, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("comment %d = %+v, want %+v", i, got[i], want[i])
		}
	}
	if len(*calls) != 3 {
		t.Fatalf("gh called %d times, want 3", len(*calls))
	}

	t.Run("line wins over original_line", func(t *testing.T) {
		inline := ghComment(4, "bob", "User", "here")
		inline["path"] = "a.go"
		inline["line"] = 9
		inline["original_line"] = 3
		scriptAPI(t, apiReply{stdout: "[]"}, apiReply{stdout: jsonOf(t, []any{inline})}, apiReply{stdout: "[]"})
		got, err := ListPRComments(prThreadOpts, 7)
		if err != nil || len(got) != 1 || got[0].Line != 9 || got[0].Path != "a.go" {
			t.Errorf("got %+v, %v; want one inline comment at a.go:9", got, err)
		}
	})

	t.Run("a conversation comment carries no path", func(t *testing.T) {
		conv := ghComment(5, "alice", "User", "hm")
		conv["path"] = "smuggled.go"
		conv["line"] = 1
		scriptAPI(t, apiReply{stdout: jsonOf(t, []any{conv})}, apiReply{stdout: "[]"}, apiReply{stdout: "[]"})
		got, err := ListPRComments(prThreadOpts, 7)
		if err != nil || len(got) != 1 || got[0].Path != "" || got[0].Line != 0 {
			t.Errorf("got %+v, %v; want a conversation comment with no path:line", got, err)
		}
	})
}

func TestGitHubPRCommentAuthorFlag(t *testing.T) {
	ghost := ghComment(5, "", "", "orphaned")
	ghost["user"] = nil
	scriptAPI(t,
		apiReply{stdout: jsonOf(t, []any{
			ghComment(1, "dependabot[bot]", "Bot", "bump"),
			ghComment(2, "alice", "User", "hi"),
			ghComment(3, "renovate[bot]", "User", "not really a bot"),
			ghComment(4, "acme", "Organization", "?"),
			ghost,
		})},
		apiReply{stdout: "[]"},
		apiReply{stdout: "[]"},
	)
	got, err := ListPRComments(prThreadOpts, 7)
	if err != nil {
		t.Fatal(err)
	}
	want := []AuthorClass{AuthorBot, AuthorHuman, AuthorHuman, AuthorUnknown, AuthorUnknown}
	if len(got) != len(want) {
		t.Fatalf("got %d comments, want %d", len(got), len(want))
	}
	for i, w := range want {
		if got[i].Author.Bot != w {
			t.Errorf("comment %s by %q: bot flag %q, want %q", got[i].ID, got[i].Author.Login, got[i].Author.Bot, w)
		}
	}
	if a := got[4].Author; a.ID != "" || a.Login != "" {
		t.Errorf("a null user maps to %+v, want an empty author", a)
	}
}

func TestGitHubPRCommentsPaginated(t *testing.T) {
	page := func(from, n int) string {
		var cs []any
		for i := from; i < from+n; i++ {
			cs = append(cs, ghComment(i, "alice", "User", "c"))
		}
		return jsonOf(t, cs)
	}
	calls := scriptAPI(t,
		apiReply{stdout: page(1, prThreadPageSize)},
		apiReply{stdout: page(1+prThreadPageSize, 1)},
		apiReply{stdout: "[]"},
		apiReply{stdout: "[]"},
	)
	got, err := ListPRComments(prThreadOpts, 7)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != prThreadPageSize+1 {
		t.Fatalf("got %d comments over two pages, want %d", len(got), prThreadPageSize+1)
	}
	for i, wantPage := range []string{"page=1", "page=2"} {
		ep := endpointOf(t, (*calls)[i].argv)
		if !strings.HasPrefix(ep, "repos/acme/widgets/issues/7/comments?") ||
			!strings.Contains(ep, "per_page=100") || !strings.HasSuffix(ep, wantPage) {
			t.Errorf("call %d endpoint = %q, want issues/7/comments at per_page=100 and %s", i+1, ep, wantPage)
		}
	}

	t.Run("a run of full pages is refused, not truncated", func(t *testing.T) {
		prev := prThreadMaxPages
		prThreadMaxPages = 2
		t.Cleanup(func() { prThreadMaxPages = prev })
		scriptAPI(t, apiReply{stdout: page(1, prThreadPageSize)}, apiReply{stdout: page(101, prThreadPageSize)})
		got, err := ListPRComments(prThreadOpts, 7)
		if err == nil || !strings.Contains(err.Error(), "may be truncated") {
			t.Fatalf("err = %v, want a may-be-truncated refusal", err)
		}
		if got != nil {
			t.Errorf("a truncated read returned %d comments, want nil", len(got))
		}
	})
}

func TestGitHubReviewSummaryFilter(t *testing.T) {
	review := func(id int, state, body string) map[string]any {
		r := ghComment(id, "carol", "User", body)
		r["state"] = state
		return r
	}
	scriptAPI(t,
		apiReply{stdout: "[]"},
		apiReply{stdout: "[]"},
		apiReply{stdout: jsonOf(t, []any{
			review(1, "APPROVED", ""),
			review(2, "PENDING", "draft I never sent"),
			review(3, "COMMENTED", "two things"),
			review(4, "APPROVED", "  \n "),
			review(5, "CHANGES_REQUESTED", "fix the loop"),
		})},
	)
	got, err := ListPRComments(prThreadOpts, 7)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, c := range got {
		if c.Kind != CommentReview {
			t.Errorf("comment %s has kind %q, want review", c.ID, c.Kind)
		}
		ids = append(ids, c.ID)
	}
	if strings.Join(ids, ",") != "3,5" {
		t.Errorf("kept reviews %v, want [3 5]", ids)
	}
}

func TestGitHubPRCommentsFailClosed(t *testing.T) {
	const ghProse = "gh: secret-looking upstream body (HTTP 500)"

	t.Run("a failed second list returns nothing", func(t *testing.T) {
		scriptAPI(t,
			apiReply{stdout: jsonOf(t, []any{ghComment(1, "alice", "User", "first list")})},
			apiReply{stderr: ghProse, exit: 1},
		)
		got, err := ListPRComments(prThreadOpts, 7)
		if got != nil {
			t.Errorf("got %d comments from a failed read, want nil", len(got))
		}
		var apiErr *ghAPIError
		if !errors.As(err, &apiErr) || apiErr.status != 500 {
			t.Fatalf("err = %v, want ghAPI's HTTP 500 error", err)
		}
		if strings.Contains(err.Error(), "secret-looking") {
			t.Errorf("err %q carries gh's own output", err)
		}
	})

	t.Run("garbage JSON", func(t *testing.T) {
		scriptAPI(t, apiReply{stdout: "<html>not json"})
		if got, err := ListPRComments(prThreadOpts, 7); err == nil || got != nil {
			t.Errorf("got %v, %v; want (nil, err)", got, err)
		}
	})

	t.Run("a null list", func(t *testing.T) {
		scriptAPI(t, apiReply{stdout: "null"})
		if got, err := ListPRComments(prThreadOpts, 7); err == nil || got != nil {
			t.Errorf("got %v, %v; want (nil, err)", got, err)
		}
	})

	t.Run("a record without an id", func(t *testing.T) {
		scriptAPI(t, apiReply{stdout: `[{"body":"anonymous"}]`})
		if got, err := ListPRComments(prThreadOpts, 7); err == nil || got != nil {
			t.Errorf("got %v, %v; want (nil, err)", got, err)
		}
	})

	t.Run("a 404 on the PR", func(t *testing.T) {
		scriptAPI(t, apiReply{stderr: "gh: Not Found (HTTP 404)", exit: 1})
		head, err := PRHeadOf(prThreadOpts, 7)
		var apiErr *ghAPIError
		if !errors.As(err, &apiErr) || apiErr.status != 404 || head != (PRHead{}) {
			t.Errorf("got %+v, %v; want an empty head and ghAPI's 404", head, err)
		}
	})

	t.Run("a PR with no head branch", func(t *testing.T) {
		scriptAPI(t, apiReply{stdout: `{"state":"open","head":{"ref":""}}`})
		if head, err := PRHeadOf(prThreadOpts, 7); err == nil || head != (PRHead{}) {
			t.Errorf("got %+v, %v; want (PRHead{}, err)", head, err)
		}
	})

	t.Run("garbage JSON for the PR", func(t *testing.T) {
		scriptAPI(t, apiReply{stdout: "]["})
		if head, err := PRHeadOf(prThreadOpts, 7); err == nil || head != (PRHead{}) {
			t.Errorf("got %+v, %v; want (PRHead{}, err)", head, err)
		}
	})

	for name, reply := range map[string]apiReply{
		"a failed user read":      {stderr: "gh: Bad credentials (HTTP 401)", exit: 1},
		"a user with no login":    {stdout: `{"id":7}`},
		"a user with no id":       {stdout: `{"login":"rivil"}`},
		"garbage JSON for a user": {stdout: "nope"},
	} {
		t.Run(name, func(t *testing.T) {
			scriptAPI(t, reply)
			if acct, err := AuthenticatedUser(prThreadOpts); err == nil || acct != (Account{}) {
				t.Errorf("got %+v, %v; want (Account{}, err)", acct, err)
			}
		})
	}
}

func TestGitHubPRCommentsArgv(t *testing.T) {
	calls := scriptAPI(t, apiReply{stdout: "[]"}, apiReply{stdout: "[]"}, apiReply{stdout: "[]"})
	if _, err := ListPRComments(prThreadOpts, 7); err != nil {
		t.Fatal(err)
	}
	wantPrefix := []string{
		"repos/acme/widgets/issues/7/comments?",
		"repos/acme/widgets/pulls/7/comments?",
		"repos/acme/widgets/pulls/7/reviews?",
	}
	for i, c := range *calls {
		if ep := endpointOf(t, c.argv); !strings.HasPrefix(ep, wantPrefix[i]) {
			t.Errorf("call %d endpoint = %q, want prefix %q", i+1, ep, wantPrefix[i])
		}
		if m := methodOf(c.argv); m != "GET" {
			t.Errorf("call %d method = %q, want GET", i+1, m)
		}
		if c.argv[0] != "api" {
			t.Errorf("call %d argv = %v, want a gh api call", i+1, c.argv)
		}
	}

	calls = scriptAPI(t, apiReply{stdout: `{"state":"open","head":{"ref":"phase/x"}}`}, apiReply{stdout: `{"login":"rivil","id":7}`})
	if _, err := PRHeadOf(prThreadOpts, 12); err != nil {
		t.Fatal(err)
	}
	if _, err := AuthenticatedUser(prThreadOpts); err != nil {
		t.Fatal(err)
	}
	if ep := endpointOf(t, (*calls)[0].argv); ep != "repos/acme/widgets/pulls/12" {
		t.Errorf("head endpoint = %q", ep)
	}
	if ep := endpointOf(t, (*calls)[1].argv); ep != "user" {
		t.Errorf("caller endpoint = %q", ep)
	}

	t.Run("a non-positive PR number never spawns", func(t *testing.T) {
		calls := scriptAPI(t)
		for _, n := range []int{0, -1} {
			if _, err := ListPRComments(prThreadOpts, n); err == nil {
				t.Errorf("ListPRComments(%d) accepted", n)
			}
			if _, err := PRHeadOf(prThreadOpts, n); err == nil {
				t.Errorf("PRHeadOf(%d) accepted", n)
			}
		}
		if len(*calls) != 0 {
			t.Errorf("gh called %d times", len(*calls))
		}
	})

	t.Run("an unknown provider never spawns", func(t *testing.T) {
		calls := scriptAPI(t)
		opts := OpenOpts{Provider: "svn", URL: prThreadOpts.URL}
		if _, err := ListPRComments(opts, 7); err == nil || !strings.Contains(err.Error(), `"svn"`) {
			t.Errorf("ListPRComments err = %v, want an unsupported-provider error", err)
		}
		if _, err := PRHeadOf(opts, 7); err == nil {
			t.Error("PRHeadOf accepted provider svn")
		}
		if _, err := AuthenticatedUser(opts); err == nil {
			t.Error("AuthenticatedUser accepted provider svn")
		}
		if len(*calls) != 0 {
			t.Errorf("gh called %d times", len(*calls))
		}
	})

	t.Run("the other forges never spawn gh", func(t *testing.T) {
		calls := scriptAPI(t)
		for _, p := range []string{"forgejo", "gitea", "gitlab", "bitbucket", "GitLab"} {
			opts := OpenOpts{Provider: p, URL: "https://code.example/acme/widgets"}
			if _, err := ListPRComments(opts, 7); err == nil {
				t.Errorf("%s: ListPRComments with no api_base or token succeeded", p)
			}
			if _, err := PRHeadOf(opts, 7); err == nil {
				t.Errorf("%s: PRHeadOf with no api_base or token succeeded", p)
			}
			if _, err := AuthenticatedUser(opts); err == nil {
				t.Errorf("%s: AuthenticatedUser with no api_base or token succeeded", p)
			}
		}
		if len(*calls) != 0 {
			t.Errorf("gh called %d times for a non-GitHub provider", len(*calls))
		}
	})

	t.Run("a bad remote url never spawns", func(t *testing.T) {
		calls := scriptAPI(t)
		opts := OpenOpts{Provider: "github", URL: "https://github.com/-evil/x"}
		if _, err := ListPRComments(opts, 7); err == nil {
			t.Error("ListPRComments accepted an owner GitHub would not")
		}
		if _, err := PRHeadOf(opts, 7); err == nil {
			t.Error("PRHeadOf accepted an owner GitHub would not")
		}
		if _, err := AuthenticatedUser(opts); err == nil {
			t.Error("AuthenticatedUser accepted an owner GitHub would not")
		}
		if len(*calls) != 0 {
			t.Errorf("gh called %d times", len(*calls))
		}
	})
}

func TestGitHubPRHeadAndCaller(t *testing.T) {
	cases := []struct {
		name  string
		pull  string
		cross bool
		open  bool
	}{
		{"same repo, open", `{"state":"open","html_url":"https://github.com/acme/widgets/pull/7","head":{"ref":"phase/x","repo":{"full_name":"acme/widgets"}},"base":{"ref":"main","repo":{"full_name":"acme/widgets"}}}`, false, true},
		{"case differs, same repo", `{"state":"open","head":{"ref":"phase/x","repo":{"full_name":"Acme/Widgets"}},"base":{"ref":"main","repo":{"full_name":"acme/widgets"}}}`, false, true},
		{"fork head", `{"state":"open","head":{"ref":"phase/x","repo":{"full_name":"mallory/widgets"}},"base":{"ref":"main","repo":{"full_name":"acme/widgets"}}}`, true, true},
		{"deleted fork", `{"state":"open","head":{"ref":"phase/x","repo":null},"base":{"ref":"main","repo":{"full_name":"acme/widgets"}}}`, true, true},
		{"nameless head repo", `{"state":"open","head":{"ref":"phase/x","repo":{"full_name":""}},"base":{"ref":"main","repo":{"full_name":""}}}`, true, true},
		{"closed", `{"state":"closed","head":{"ref":"phase/x","repo":{"full_name":"acme/widgets"}},"base":{"ref":"main","repo":{"full_name":"acme/widgets"}}}`, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			scriptAPI(t, apiReply{stdout: tc.pull})
			head, err := PRHeadOf(prThreadOpts, 7)
			if err != nil {
				t.Fatal(err)
			}
			if head.Ref != "phase/x" || head.CrossRepo != tc.cross || head.Open != tc.open {
				t.Errorf("head = %+v, want ref phase/x cross=%v open=%v", head, tc.cross, tc.open)
			}
		})
	}

	scriptAPI(t, apiReply{stdout: cases[0].pull})
	if head, _ := PRHeadOf(prThreadOpts, 7); head.URL != "https://github.com/acme/widgets/pull/7" {
		t.Errorf("head url = %q", head.URL)
	}

	scriptAPI(t, apiReply{stdout: `{"login":"rivil","id":7,"type":"User"}`})
	acct, err := AuthenticatedUser(prThreadOpts)
	if err != nil {
		t.Fatal(err)
	}
	if acct != (Account{ID: "7", Login: "rivil"}) {
		t.Errorf("account = %+v, want {ID:7 Login:rivil}", acct)
	}
}
