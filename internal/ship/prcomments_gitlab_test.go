package ship

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/Rivil/dross/internal/hostallow"
)

const glMR = "/api/v4/projects/acme%2Fwidgets/merge_requests/7"

// gitlabThread is a scripted GitLab API over an MR !7.
type gitlabThread struct {
	mr     string
	notes  []map[string]any
	users  map[string]string // /users/<id> -> answer; missing -> 404
	status map[string]int    // path or path?page=N -> status
	hits   atomic.Int32
	mu     sync.Mutex
	paths  []string
}

func (g *gitlabThread) serve(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g.hits.Add(1)
		g.mu.Lock()
		g.paths = append(g.paths, r.URL.EscapedPath())
		g.mu.Unlock()
		if r.Header.Get("PRIVATE-TOKEN") != "gl-secret" {
			http.Error(w, "no auth", http.StatusUnauthorized)
			return
		}
		key := r.URL.EscapedPath()
		if p := r.URL.Query().Get("page"); p != "" {
			key += "?page=" + p
		}
		if code, ok := g.status[key]; ok {
			w.WriteHeader(code)
			_, _ = w.Write([]byte(`{"message":"boom"}`))
			return
		}
		switch path := r.URL.EscapedPath(); {
		case path == glMR:
			_, _ = w.Write([]byte(g.mr))
		case path == glMR+"/notes":
			page, _ := strconv.Atoi(r.URL.Query().Get("page"))
			per, _ := strconv.Atoi(r.URL.Query().Get("per_page"))
			lo := min((page-1)*per, len(g.notes))
			b, _ := json.Marshal(g.notes[lo:min(lo+per, len(g.notes))])
			_, _ = w.Write(b)
		case strings.HasPrefix(path, "/api/v4/users/"):
			if a, ok := g.users[strings.TrimPrefix(path, "/api/v4")]; ok {
				_, _ = w.Write([]byte(a))
				return
			}
			http.NotFound(w, r)
		case path == "/api/v4/user":
			_, _ = w.Write([]byte(`{"id":7,"username":"rivil"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func (g *gitlabThread) count(prefix string) int {
	g.mu.Lock()
	defer g.mu.Unlock()
	n := 0
	for _, p := range g.paths {
		if strings.HasPrefix(p, prefix) {
			n++
		}
	}
	return n
}

func gitlabOpts(t *testing.T, apiBase string) OpenOpts {
	t.Setenv("GL_TOKEN", "gl-secret")
	return OpenOpts{
		Provider: "gitlab", URL: "https://gitlab.example/acme/widgets", APIBase: apiBase + "/api/v4",
		AuthEnv: "GL_TOKEN", Hosts: hostallow.Derive(apiBase, nil),
	}
}

const glMRJSON = `{"state":"opened","web_url":"https://gitlab.example/acme/widgets/-/merge_requests/7","source_branch":"phase/x","source_project_id":1,"target_project_id":1}`

func glNote(id, author int, body string) map[string]any {
	return map[string]any{"id": id, "body": body, "system": false, "author": map[string]any{"id": author, "username": fmt.Sprintf("u%d", author)}}
}

func glDiffNote(id, author int, newPath string, newLine any, oldPath string, oldLine any) map[string]any {
	n := glNote(id, author, "on a line")
	n["type"] = "DiffNote"
	n["position"] = map[string]any{"new_path": newPath, "new_line": newLine, "old_path": oldPath, "old_line": oldLine}
	return n
}

var glHuman = map[string]string{"/users/5": `{"id":5,"bot":false}`, "/users/6": `{"id":6,"bot":true}`}

func TestGitLabPRCommentKinds(t *testing.T) {
	g := &gitlabThread{mr: glMRJSON, users: glHuman, notes: []map[string]any{
		glNote(1, 5, "why?"),
		glDiffNote(2, 5, "internal/x.go", 42, "internal/x.go", 40),
		glDiffNote(3, 6, "internal/y.go", nil, "internal/old.go", 9),
	}}
	got, err := ListPRComments(gitlabOpts(t, g.serve(t).URL), 7)
	if err != nil {
		t.Fatal(err)
	}
	want := []PRComment{
		{ID: "1", Kind: CommentConversation, Author: CommentAuthor{ID: "5", Login: "u5", Bot: AuthorHuman},
			URL: "https://gitlab.example/acme/widgets/-/merge_requests/7#note_1", Body: "why?"},
		{ID: "2", Kind: CommentInline, Author: CommentAuthor{ID: "5", Login: "u5", Bot: AuthorHuman},
			Path: "internal/x.go", Line: 42, URL: "https://gitlab.example/acme/widgets/-/merge_requests/7#note_2", Body: "on a line"},
		{ID: "3", Kind: CommentInline, Author: CommentAuthor{ID: "6", Login: "u6", Bot: AuthorBot},
			Path: "internal/old.go", Line: 9, URL: "https://gitlab.example/acme/widgets/-/merge_requests/7#note_3", Body: "on a line"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("note %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestGitLabPRCommentURL(t *testing.T) {
	g := &gitlabThread{mr: glMRJSON, users: glHuman, notes: []map[string]any{glNote(11, 5, "a"), glNote(12, 5, "b")}}
	got, err := ListPRComments(gitlabOpts(t, g.serve(t).URL), 7)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d notes, want 2", len(got))
	}
	for _, c := range got {
		if c.URL != "https://gitlab.example/acme/widgets/-/merge_requests/7#note_"+c.ID {
			t.Errorf("note %s url %q", c.ID, c.URL)
		}
	}
	g = &gitlabThread{mr: `{"state":"opened","source_branch":"phase/x"}`, notes: []map[string]any{glNote(1, 5, "a")}}
	if got, err := ListPRComments(gitlabOpts(t, g.serve(t).URL), 7); err == nil || got != nil {
		t.Errorf("an MR with no web_url: %v, %v; want (nil, err)", got, err)
	}
}

// TestGitLabDiffNoteSides: the old side is used only for a note on a removed
// line; a file-level note (no line on either side) on a renamed file keeps
// the file's current name.
func TestGitLabDiffNoteSides(t *testing.T) {
	g := &gitlabThread{mr: glMRJSON, users: glHuman, notes: []map[string]any{
		glDiffNote(1, 5, "renamed.go", nil, "original.go", nil),
		glDiffNote(2, 5, "", nil, "deleted.go", nil),
		glDiffNote(3, 5, "kept.go", 7, "kept.go", 6),
	}}
	got, err := ListPRComments(gitlabOpts(t, g.serve(t).URL), 7)
	if err != nil {
		t.Fatal(err)
	}
	want := []struct {
		path string
		line int
	}{{"renamed.go", 0}, {"deleted.go", 0}, {"kept.go", 7}}
	if len(got) != len(want) {
		t.Fatalf("got %d notes, want %d", len(got), len(want))
	}
	for i, w := range want {
		if got[i].Kind != CommentInline || got[i].Path != w.path || got[i].Line != w.line {
			t.Errorf("note %s = %s:%d (%s), want inline %s:%d", got[i].ID, got[i].Path, got[i].Line, got[i].Kind, w.path, w.line)
		}
	}
}

func TestGitLabSystemNotesDropped(t *testing.T) {
	sys := glNote(1, 5, "added 1 commit")
	sys["system"] = true
	g := &gitlabThread{mr: glMRJSON, users: glHuman, notes: []map[string]any{sys, glNote(2, 5, "real")}}
	got, err := ListPRComments(gitlabOpts(t, g.serve(t).URL), 7)
	if err != nil || len(got) != 1 || got[0].ID != "2" {
		t.Errorf("got %+v, %v; want only note 2", got, err)
	}
}

func TestGitLabPRCommentPaging(t *testing.T) {
	var notes []map[string]any
	for i := 1; i <= gitlabNotePage+3; i++ {
		notes = append(notes, glNote(i, 5, "n"))
	}
	g := &gitlabThread{mr: glMRJSON, users: glHuman, notes: notes}
	got, err := ListPRComments(gitlabOpts(t, g.serve(t).URL), 7)
	if err != nil || len(got) != gitlabNotePage+3 {
		t.Fatalf("got %d, %v; want %d", len(got), err, gitlabNotePage+3)
	}
	g = &gitlabThread{mr: glMRJSON, users: glHuman, notes: notes, status: map[string]int{glMR + "/notes?page=2": 500}}
	if got, err := ListPRComments(gitlabOpts(t, g.serve(t).URL), 7); err == nil || got != nil {
		t.Errorf("a 500 on page 2: %d comments, %v; want (nil, err)", len(got), err)
	}

	prev := prThreadMaxPages
	prThreadMaxPages = 1
	t.Cleanup(func() { prThreadMaxPages = prev })
	g = &gitlabThread{mr: glMRJSON, users: glHuman, notes: notes}
	if got, err := ListPRComments(gitlabOpts(t, g.serve(t).URL), 7); err == nil || !strings.Contains(err.Error(), "may be truncated") || got != nil {
		t.Errorf("a run of full pages: %d, %v; want a may-be-truncated refusal", len(got), err)
	}
}

func TestGitLabAuthorBotLookup(t *testing.T) {
	g := &gitlabThread{mr: glMRJSON, users: glHuman, notes: []map[string]any{
		glNote(1, 5, "a"), glNote(2, 6, "b"), glNote(3, 5, "c"), glNote(4, 6, "d"),
	}}
	got, err := ListPRComments(gitlabOpts(t, g.serve(t).URL), 7)
	if err != nil {
		t.Fatal(err)
	}
	if n := g.count("/api/v4/users/"); n != 2 {
		t.Errorf("%d /users/:id calls for 2 authors, want 2", n)
	}
	for _, c := range got {
		want := map[string]AuthorClass{"5": AuthorHuman, "6": AuthorBot}[c.Author.ID]
		if c.Author.Bot != want {
			t.Errorf("note %s by %s reads %q, want %q", c.ID, c.Author.ID, c.Author.Bot, want)
		}
	}

	g = &gitlabThread{mr: glMRJSON, users: map[string]string{"/users/5": `{"id":5,"bot":false}`}, notes: []map[string]any{glNote(1, 5, "a"), glNote(2, 6, "b")}}
	got, err = ListPRComments(gitlabOpts(t, g.serve(t).URL), 7)
	if err != nil || len(got) != 2 || got[1].Author.Bot != AuthorUnknown || got[0].Author.Bot != AuthorHuman {
		t.Errorf("a 404 on /users/6: %+v, %v; want the list with 6 unknown", got, err)
	}
}

func TestGitLabHostRefused(t *testing.T) {
	g := &gitlabThread{mr: glMRJSON}
	srv := g.serve(t)
	t.Setenv("GL_TOKEN", "gl-secret")
	opts := OpenOpts{Provider: "gitlab", URL: "https://gitlab.example/acme/widgets", APIBase: srv.URL + "/api/v4",
		AuthEnv: "GL_TOKEN", Hosts: hostallow.Derive("https://gitlab.example", nil)}
	_, e1 := ListPRComments(opts, 7)
	_, e2 := PRHeadOf(opts, 7)
	_, e3 := AuthenticatedUser(opts)
	for i, err := range []error{e1, e2, e3} {
		if !errors.Is(err, hostallow.ErrRefused) {
			t.Errorf("call %d: %v, want a host refusal", i+1, err)
		}
	}
	if n := g.hits.Load(); n != 0 {
		t.Errorf("the off-allowlist server saw %d requests", n)
	}
}

func TestGitLabScrubsToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("forbidden: PRIVATE-TOKEN " + r.Header.Get("PRIVATE-TOKEN")))
	}))
	t.Cleanup(srv.Close)
	opts := gitlabOpts(t, srv.URL)
	_, e1 := ListPRComments(opts, 7)
	_, e2 := PRHeadOf(opts, 7)
	_, e3 := AuthenticatedUser(opts)
	for i, err := range []error{e1, e2, e3} {
		if err == nil || strings.Contains(err.Error(), "gl-secret") || !strings.Contains(err.Error(), "[redacted $GL_TOKEN]") {
			t.Errorf("call %d error %v, want the token scrubbed", i+1, err)
		}
	}
}

func TestGitLabHeadAndCaller(t *testing.T) {
	for _, tc := range []struct {
		name, mr    string
		cross, open bool
	}{
		{"same project", glMRJSON, false, true},
		{"fork", `{"state":"opened","web_url":"u","source_branch":"phase/x","source_project_id":2,"target_project_id":1}`, true, true},
		{"merged", `{"state":"merged","web_url":"u","source_branch":"phase/x","source_project_id":1,"target_project_id":1}`, false, false},
		{"closed", `{"state":"closed","web_url":"u","source_branch":"phase/x","source_project_id":1,"target_project_id":1}`, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := &gitlabThread{mr: tc.mr}
			head, err := PRHeadOf(gitlabOpts(t, g.serve(t).URL), 7)
			if err != nil {
				t.Fatal(err)
			}
			if head.Ref != "phase/x" || head.CrossRepo != tc.cross || head.Open != tc.open {
				t.Errorf("head = %+v, want cross=%v open=%v", head, tc.cross, tc.open)
			}
		})
	}
	g := &gitlabThread{mr: `{"state":"opened"}`}
	if head, err := PRHeadOf(gitlabOpts(t, g.serve(t).URL), 7); err == nil || head != (PRHead{}) {
		t.Errorf("an MR with no source branch: %+v, %v", head, err)
	}
	g = &gitlabThread{mr: glMRJSON}
	acct, err := AuthenticatedUser(gitlabOpts(t, g.serve(t).URL))
	if err != nil || acct != (Account{ID: "7", Login: "rivil"}) {
		t.Errorf("account = %+v, %v", acct, err)
	}
}
