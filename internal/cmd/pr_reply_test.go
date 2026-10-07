package cmd

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Rivil/dross/internal/gate"
	"github.com/Rivil/dross/internal/gatestate"
	"github.com/Rivil/dross/internal/prtriage"
	"github.com/Rivil/dross/internal/ship"
)

// replyForge counts the comment POSTs a fake Forgejo receives and keeps the
// last body; failWith answers each POST with that status instead.
type replyForge struct {
	posts    atomic.Int32
	lastBody string
	failWith int
}

// replyRepo is prRepo on a Forgejo remote served by a fake forge, with PR #12
// on phase/x stubbed and phase x's record rejecting c1 (unposted).
func replyRepo(t *testing.T) (dir string, f *replyForge, s *prStubs) {
	t.Helper()
	dir = prRepo(t)
	f = &replyForge{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/api/v1/repos/me/proj/issues/12/comments" {
			f.posts.Add(1)
			b, _ := io.ReadAll(r.Body)
			f.lastBody = string(b)
			if f.failWith != 0 {
				w.WriteHeader(f.failWith)
				return
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":1}`))
			return
		}
		t.Errorf("unexpected forge request %s %s", r.Method, r.URL.Path)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("MOCK_FORGEJO_TOKEN", "secret")
	for _, set := range [][]string{
		{"set", "remote.provider", "forgejo"},
		{"set", "remote.url", srv.URL + "/me/proj"},
		{"set", "remote.api_base", srv.URL + "/api/v1"},
		{"set", "remote.auth_env", "MOCK_FORGEJO_TOKEN"},
	} {
		if err := runCmd(t, Project(), set...); err != nil {
			t.Fatalf("project %v: %v", set, err)
		}
	}
	mustWrite(t, replyRecordPath(dir), replyEntry("c1", "bounded by maxPages", false))
	gitCommit(t, dir, "chore(dross): forgejo remote and a reject")
	s = &prStubs{head: ship.PRHead{Ref: "phase/x", Open: true}, self: prSelf, comments: []ship.PRComment{
		prComment("1", ship.CommentConversation, "11", "alice", ship.AuthorHuman, "why?"),
	}}
	stubPRThread(t, s)
	return dir, f, s
}

func replyRecordPath(dir string) string {
	return filepath.Join(dir, ".dross", "phases", "x", prtriage.File)
}

func replyEntry(id, reason string, posted bool) string {
	b := `[[resolution]]
id = "` + id + `"
kind = "conversation"
pr = 12
url = "https://forge.example/me/proj/pulls/12#` + id + `"
author = "alice"
verdict = "reject"
reason = "` + reason + `"
at = "x.go:1"
digest = "` + strings.Repeat("ab", 32) + `"
`
	if posted {
		b += "posted = true\n"
	}
	return b + "\n"
}

// runReply runs `dross pr reply 12 <args>` and returns stdout.
func runReply(t *testing.T, args ...string) (string, error) {
	t.Helper()
	c := prRoot()
	var out bytes.Buffer
	c.SetArgs(append([]string{"reply", "12"}, args...))
	c.SetOut(&out)
	c.SetErr(new(bytes.Buffer))
	err := c.Execute()
	return out.String(), err
}

// draft returns the current draft body and its approval label.
func draft(t *testing.T, dir string) (string, string) {
	t.Helper()
	rec, _, err := prtriage.Load(replyRecordPath(dir))
	if err != nil {
		t.Fatal(err)
	}
	body, ok := prtriage.ReplyBody(rec, 12)
	if !ok {
		t.Fatal("no draft")
	}
	return body, gate.ReplyApproveLabel(12, prtriage.ReplyDigest(body))
}

func approve(t *testing.T, dir string, pr int, digest string) {
	t.Helper()
	if err := gatestate.SaveReplyApproval(dir, gatestate.ReplyApproval{PR: pr, Digest: digest, At: time.Now()}); err != nil {
		t.Fatal(err)
	}
}

func TestPRReplyDraftPostsNothing(t *testing.T) {
	dir, f, _ := replyRepo(t)
	before := mustRead(t, replyRecordPath(dir))
	out, err := runReply(t)
	if err != nil {
		t.Fatal(err)
	}
	body, label := draft(t, dir)
	if !strings.Contains(out, body) || !strings.Contains(out, label) {
		t.Errorf("the draft run did not print the draft and the label %q:\n%s", label, out)
	}
	if f.posts.Load() != 0 || mustRead(t, replyRecordPath(dir)) != before {
		t.Error("a draft run posted or wrote")
	}
}

func TestPRReplyRefusesWithoutRecordedApproval(t *testing.T) {
	dir, f, _ := replyRepo(t)
	out, _ := runReply(t)
	_, label := draft(t, dir)
	if !strings.Contains(out, label) {
		t.Fatalf("the draft run did not print the label:\n%s", out)
	}
	_, err := runReply(t, "--post")
	if err == nil || !strings.Contains(err.Error(), label) || !strings.Contains(err.Error(), "dross hooks ensure") {
		t.Errorf("--post with no recorded approval: %v, want a refusal naming the label and `dross hooks ensure`", err)
	}
	if f.posts.Load() != 0 {
		t.Errorf("%d POSTs without an approval", f.posts.Load())
	}
}

func TestPRReplyStaleApproval(t *testing.T) {
	dir, f, _ := replyRepo(t)
	body, _ := draft(t, dir)
	approve(t, dir, 12, prtriage.ReplyDigest(body))
	mustWrite(t, replyRecordPath(dir), replyEntry("c1", "bounded by maxPages", false)+replyEntry("c2", "a new reject", false))
	if _, err := runReply(t, "--post"); err == nil {
		t.Error("an approval of the earlier draft posted the new one")
	}
	body, _ = draft(t, dir)
	approve(t, dir, 13, prtriage.ReplyDigest(body))
	if _, err := runReply(t, "--post"); err == nil {
		t.Error("an approval for PR 13 posted on PR 12")
	}
	if f.posts.Load() != 0 {
		t.Errorf("%d POSTs on stale or foreign approvals", f.posts.Load())
	}
}

func TestPRReplyNothingToPost(t *testing.T) {
	dir, f, _ := replyRepo(t)
	mustWrite(t, replyRecordPath(dir), replyEntry("c1", "bounded", true))
	for _, args := range [][]string{nil, {"--post"}} {
		out, err := runReply(t, args...)
		if err != nil || !strings.Contains(out, "nothing to reply") {
			t.Errorf("%v: %v\n%s", args, err, out)
		}
	}
	if f.posts.Load() != 0 {
		t.Errorf("%d POSTs with nothing to reply", f.posts.Load())
	}
}

func TestPRReplyPostsOnceViaShipComment(t *testing.T) {
	dir, f, _ := replyRepo(t)
	body, _ := draft(t, dir)
	approve(t, dir, 12, prtriage.ReplyDigest(body))
	if _, err := runReply(t, "--post"); err != nil {
		t.Fatal(err)
	}
	var sent struct {
		Body string `json:"body"`
	}
	if err := json.Unmarshal([]byte(f.lastBody), &sent); err != nil {
		t.Fatalf("the POST body is not the JSON comment it should be: %v (%q)", err, f.lastBody)
	}
	if f.posts.Load() != 1 || sent.Body != body {
		t.Fatalf("%d POSTs; posted body %q, want exactly the previewed draft %q", f.posts.Load(), sent.Body, body)
	}
	rec, _, _ := prtriage.Load(replyRecordPath(dir))
	if !rec.Resolution[0].Posted {
		t.Error("the posted reject is not marked posted")
	}
	if a, _ := gatestate.LoadReplyApproval(dir); a != nil {
		t.Errorf("the approval was not spent: %+v", a)
	}
	out, err := runReply(t, "--post")
	if err != nil || !strings.Contains(out, "nothing to reply") || f.posts.Load() != 1 {
		t.Errorf("a second --post: %v, %d POSTs\n%s", err, f.posts.Load(), out)
	}
}

func TestPRReplyPostFailureLeavesUnposted(t *testing.T) {
	dir, f, _ := replyRepo(t)
	f.failWith = http.StatusInternalServerError
	body, _ := draft(t, dir)
	approve(t, dir, 12, prtriage.ReplyDigest(body))
	before := mustRead(t, replyRecordPath(dir))
	if _, err := runReply(t, "--post"); err == nil {
		t.Fatal("a 500 from the forge read as posted")
	}
	if mustRead(t, replyRecordPath(dir)) != before {
		t.Error("a failed post changed the record")
	}
	if a, _ := gatestate.LoadReplyApproval(dir); a == nil {
		t.Error("a failed post spent the approval")
	}
}

func TestPRReplyMarkFailureNamesPosted(t *testing.T) {
	dir, f, _ := replyRepo(t)
	prev := beforeTriageSave
	beforeTriageSave = func(recPath string) { mustWrite(t, recPath, "# changed under us\n") }
	t.Cleanup(func() { beforeTriageSave = prev })
	body, _ := draft(t, dir)
	approve(t, dir, 12, prtriage.ReplyDigest(body))
	_, err := runReply(t, "--post")
	if f.posts.Load() != 1 {
		t.Fatalf("%d POSTs, want the one that succeeded", f.posts.Load())
	}
	if err == nil || !strings.Contains(err.Error(), "c1") || !strings.Contains(err.Error(), "posted") {
		t.Errorf("err = %v, want one naming the posted id c1", err)
	}
}

// TestPRReplyInvalidRecordPostsNothing: a record the post could not be marked
// in refuses before anything reaches the forge.
func TestPRReplyInvalidRecordPostsNothing(t *testing.T) {
	dir, f, _ := replyRepo(t)
	mustWrite(t, replyRecordPath(dir), replyEntry("c1", "bounded", false)+`[[resolution]]
id = "c2"
kind = "conversation"
pr = 12
url = "https://forge.example/me/proj/pulls/12#c2"
author = "bob"
verdict = "accept"
task = "t-99"
at = "x.go:1"
digest = "`+strings.Repeat("ab", 32)+`"
`)
	body, _ := draft(t, dir)
	approve(t, dir, 12, prtriage.ReplyDigest(body))
	if _, err := runReply(t, "--post"); err == nil || !strings.Contains(err.Error(), "invalid") {
		t.Errorf("an invalid record: %v, want a refusal before posting", err)
	}
	if f.posts.Load() != 0 {
		t.Errorf("%d POSTs with a record that cannot be marked", f.posts.Load())
	}
}

// routeEntry is a route resolution of PR 12 parking comment id as deferred
// item abc.
func routeEntry(id string) string {
	return `[[resolution]]
id = "` + id + `"
kind = "conversation"
pr = 12
url = "https://forge.example/me/proj/pulls/12#` + id + `"
author = "bob"
verdict = "route"
deferred = "abc"
target = "later"
at = "x.go:1"
digest = "` + strings.Repeat("ab", 32) + `"
`
}

// TestPRReplyWithRoutedResolution: on a phase whose spec holds the deferred
// item a route names — the normal shape of a real phase — the pre-post check
// reads the spec's ids, so the reply goes out once and only the reject is
// marked posted.
func TestPRReplyWithRoutedResolution(t *testing.T) {
	dir, f, _ := replyRepo(t)
	mustWrite(t, filepath.Join(dir, ".dross", "phases", "x", "spec.toml"), `[phase]
id = "x"
title = "X"

[[deferred]]
id = "abc"
text = "rework paging"
target = "later"
`)
	mustWrite(t, replyRecordPath(dir), replyEntry("c1", "bounded by maxPages", false)+routeEntry("c2"))
	body, _ := draft(t, dir)
	approve(t, dir, 12, prtriage.ReplyDigest(body))
	if _, err := runReply(t, "--post"); err != nil {
		t.Fatalf("a record holding a route to a spec deferred item: %v", err)
	}
	if f.posts.Load() != 1 {
		t.Errorf("%d POSTs, want exactly 1", f.posts.Load())
	}
	rec, _, err := prtriage.Load(replyRecordPath(dir))
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rec.Resolution {
		if want := r.ID == "c1"; r.Posted != want {
			t.Errorf("%s posted = %v, want %v", r.ID, r.Posted, want)
		}
	}
}

// TestPRReplyRefusesBadSpec: a spec.toml that cannot be read stops --post
// before anything is sent, marked or spent.
func TestPRReplyRefusesBadSpec(t *testing.T) {
	dir, f, _ := replyRepo(t)
	mustWrite(t, filepath.Join(dir, ".dross", "phases", "x", "spec.toml"), "[phase\n")
	body, _ := draft(t, dir)
	approve(t, dir, 12, prtriage.ReplyDigest(body))
	recPath, apprPath := replyRecordPath(dir), gatestate.Path(dir, gatestate.ReplyFile)
	rec, appr := readOrEmpty(t, recPath), readOrEmpty(t, apprPath)
	if _, err := runReply(t, "--post"); err == nil || !strings.Contains(err.Error(), "spec.toml") {
		t.Errorf("a malformed spec.toml: %v, want a refusal naming spec.toml", err)
	}
	if f.posts.Load() != 0 {
		t.Errorf("%d POSTs with an unreadable spec.toml", f.posts.Load())
	}
	if readOrEmpty(t, recPath) != rec {
		t.Errorf("%s changed", prtriage.File)
	}
	if appr == "" || readOrEmpty(t, apprPath) != appr {
		t.Error("the approval was spent or never recorded")
	}
}

func TestPRReplyNeedsPhaseBranch(t *testing.T) {
	dir, f, _ := replyRepo(t)
	body, _ := draft(t, dir)
	approve(t, dir, 12, prtriage.ReplyDigest(body))
	mustGit(t, dir, "checkout", "-q", "main")
	if _, err := runReply(t, "--post"); err == nil {
		t.Error("reply ran off the phase branch")
	}
	if f.posts.Load() != 0 {
		t.Errorf("%d POSTs off-branch", f.posts.Load())
	}
}

func TestPRReplyRoundTripExcluded(t *testing.T) {
	dir, _, s := replyRepo(t)
	body, _ := draft(t, dir)
	s.comments = append(s.comments, prComment("99", ship.CommentConversation, prSelf.ID, prSelf.Login, ship.AuthorHuman, body))
	out, err := runPR(t, "comments", "12")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "c99 · ") {
		t.Errorf("the posted reply resurfaced for triage:\n%s", out)
	}
}
