package boardsync

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/Rivil/dross/internal/board"
	"github.com/Rivil/dross/internal/forge"
	"github.com/Rivil/dross/internal/project"
)

// The doubles below are shared by the package-local tests that close the
// cross-package blind spot: boardsync used to be exercised only through
// internal/cmd's end-to-end tests, which gremlins — running per package —
// never attributes here. Each double adds exactly one thing to the one below
// it, so a test picks the narrowest board that can express its case:
//
//	fakeBoard   (sync_test.go)  an in-memory tracker that records writes
//	faultBoard  per-key failures, a refused close, a scripted milestone
//	stateBoard  faultBoard + forge.StateWriter
//	linkBoard   faultBoard + forge.IssueLinker
//
// faultBoard deliberately satisfies neither optional capability, so a test of
// a capability gate cannot pass because the double happened to offer it.

// milestoneCall is one EnsureMilestone request as the board received it.
type milestoneCall struct{ Title, Body string }

// faultBoard wraps fakeBoard with injectable failures. Every fault is keyed by
// issue key so one card's failure never bleeds into its siblings — except
// failCreate, which is keyed by the input's Title because a create has no key
// yet. calls logs every method invocation, failed ones included, as
// "<Method> <key>", so a test can assert an attempt that left no trace in
// fakeBoard's success-only slices.
type faultBoard struct {
	*fakeBoard
	failGet     map[string]error
	nilGet      map[string]bool
	failUpdate  map[string]error
	failClose   map[string]error
	failCreate  map[string]error
	refuseClose map[string]bool // CloseIssue returns nil and the issue stays open
	listErr     error

	milestoneID  string
	milestoneErr error
	milestones   []milestoneCall

	calls []string
}

func newFaultBoard() *faultBoard {
	return &faultBoard{
		fakeBoard:   newFakeBoard(),
		failGet:     map[string]error{},
		nilGet:      map[string]bool{},
		failUpdate:  map[string]error{},
		failClose:   map[string]error{},
		failCreate:  map[string]error{},
		refuseClose: map[string]bool{},
		milestoneID: "7",
	}
}

// seed places an issue on the board under its own key and advances the key
// counter past it, so a later create never collides with a seeded key. State
// defaults to "open".
func (f *faultBoard) seed(iss forge.Issue) {
	if iss.State == "" {
		iss.State = "open"
	}
	if i := strings.LastIndex(iss.Key, "-"); i >= 0 {
		if n, err := strconv.Atoi(iss.Key[i+1:]); err == nil {
			iss.Number = n
			if n > f.next {
				f.next = n
			}
		}
	}
	iss.Labels = append([]string(nil), iss.Labels...)
	f.issues[iss.Key] = &iss
}

func (f *faultBoard) record(method, key string) { f.calls = append(f.calls, method+" "+key) }

// callsOf returns the logged invocations of one method, in order.
func (f *faultBoard) callsOf(method string) []string {
	var out []string
	for _, c := range f.calls {
		if strings.HasPrefix(c, method+" ") {
			out = append(out, c)
		}
	}
	return out
}

func (f *faultBoard) EnsureMilestone(title, body string) (string, error) {
	f.record("EnsureMilestone", title)
	f.milestones = append(f.milestones, milestoneCall{Title: title, Body: body})
	if f.milestoneErr != nil {
		return "", f.milestoneErr
	}
	return f.milestoneID, nil
}

func (f *faultBoard) CreateIssue(in forge.IssueInput) (*forge.Issue, error) {
	f.record("CreateIssue", in.Title)
	if err := f.failCreate[in.Title]; err != nil {
		return nil, err
	}
	return f.fakeBoard.CreateIssue(in)
}

func (f *faultBoard) GetIssue(key string) (*forge.Issue, error) {
	f.record("GetIssue", key)
	if err := f.failGet[key]; err != nil {
		return nil, err
	}
	if f.nilGet[key] {
		return nil, nil
	}
	return f.fakeBoard.GetIssue(key)
}

func (f *faultBoard) UpdateIssue(key string, patch forge.IssuePatch) (*forge.Issue, error) {
	f.record("UpdateIssue", key)
	if err := f.failUpdate[key]; err != nil {
		return nil, err
	}
	return f.fakeBoard.UpdateIssue(key, patch)
}

func (f *faultBoard) CloseIssue(key string) error {
	f.record("CloseIssue", key)
	if err := f.failClose[key]; err != nil {
		return err
	}
	if f.refuseClose[key] {
		return nil
	}
	return f.fakeBoard.CloseIssue(key)
}

func (f *faultBoard) ListIssues(filter forge.IssueFilter) ([]forge.Issue, error) {
	f.record("ListIssues", strings.Join(filter.Labels, ","))
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.fakeBoard.ListIssues(filter)
}

// stateWrite is one successful SetStateRaw.
type stateWrite struct{ Key, State string }

// stateBoard is a faultBoard that can write a literal tracker state — the
// capability reap --undo gates on. refuseState makes SetStateRaw fail for a key
// without writing, the way a workflow that refuses a transition does.
type stateBoard struct {
	*faultBoard
	refuseState map[string]error
	states      []stateWrite
}

func newStateBoard() *stateBoard {
	return &stateBoard{faultBoard: newFaultBoard(), refuseState: map[string]error{}}
}

func (s *stateBoard) SetStateRaw(key, state string) error {
	s.record("SetStateRaw", key)
	if err := s.refuseState[key]; err != nil {
		return err
	}
	iss, ok := s.issues[key]
	if !ok {
		return os.ErrNotExist
	}
	iss.State = state
	s.states = append(s.states, stateWrite{Key: key, State: state})
	return nil
}

// linkBoard is a faultBoard that can relate two issues. linkErr makes every
// LinkIssues fail after recording the attempt.
type linkBoard struct {
	*faultBoard
	linkErr error
	links   [][2]string
}

func newLinkBoard() *linkBoard { return &linkBoard{faultBoard: newFaultBoard()} }

func (l *linkBoard) LinkIssues(from, to string) error {
	l.record("LinkIssues", from+"->"+to)
	if l.linkErr != nil {
		return l.linkErr
	}
	l.links = append(l.links, [2]string{from, to})
	return nil
}

var (
	_ forge.StateWriter = (*stateBoard)(nil)
	_ forge.IssueLinker = (*linkBoard)(nil)
)

// boardCtx returns a Ctx over client rooted in a fresh TempDir, with an empty
// board.json path and narration captured in the returned buffer. The provider
// name is "fakeboard" so a refusal that names the provider is unambiguous.
func boardCtx(t *testing.T, client forge.BoardClient) (*Ctx, *bytes.Buffer) {
	t.Helper()
	root := filepath.Join(t.TempDir(), ".dross")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	out := new(bytes.Buffer)
	p := &project.Project{}
	p.Board.Provider = "fakeboard"
	return &Ctx{
		Client:    client,
		Board:     board.New(),
		Proj:      p,
		Root:      root,
		BoardPath: filepath.Join(root, board.File),
		Out:       out,
	}, out
}

// captureStderr runs fn with os.Stderr swapped for a pipe and returns what fn
// wrote. The original stream is restored when fn returns or panics, and again
// in t.Cleanup as a backstop. Because os.Stderr is process-global, a test that
// calls this must never call t.Parallel.
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	orig := os.Stderr
	t.Cleanup(func() { os.Stderr = orig })
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	// Drain concurrently: a writer that fills the pipe buffer before fn
	// returns would otherwise block forever.
	done := make(chan string, 1)
	go func() {
		var b bytes.Buffer
		_, _ = io.Copy(&b, r)
		_ = r.Close()
		done <- b.String()
	}()
	func() {
		defer func() {
			os.Stderr = orig
			_ = w.Close()
		}()
		os.Stderr = w
		fn()
	}()
	return <-done
}

// trackedCall is one request the scripted tracker received.
type trackedCall struct{ Method, Path, Body string }

type trackedRoute struct {
	status int
	bodies []string
	hits   int
}

// scriptedTracker is an httptest stand-in for a real tracker that answers only
// what the test scripted. Routes are keyed "<METHOD> <path>" with the query
// stripped. A scripted route serves its bodies in order and repeats the last;
// a failed route answers 500. Anything unscripted is answered 404 AND recorded
// as a problem, and so is a scripted route nobody called — a fixture that
// scripts a request the code under test never makes is asserting nothing.
type scriptedTracker struct {
	mu         sync.Mutex
	routes     map[string]*trackedRoute
	order      []string
	calls      []trackedCall
	unscripted []string
}

func newScriptedTracker() *scriptedTracker {
	return &scriptedTracker{routes: map[string]*trackedRoute{}}
}

// strictTracker is a scriptedTracker whose problems fail t at cleanup.
func strictTracker(t *testing.T) *scriptedTracker {
	t.Helper()
	s := newScriptedTracker()
	t.Cleanup(func() {
		for _, p := range s.problems() {
			t.Error(p)
		}
	})
	return s
}

func (s *scriptedTracker) add(method, path string, r *trackedRoute) *scriptedTracker {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := method + " " + path
	if _, ok := s.routes[key]; !ok {
		s.order = append(s.order, key)
	}
	s.routes[key] = r
	return s
}

// on scripts a route. With no bodies it answers `{}`.
func (s *scriptedTracker) on(method, path string, bodies ...string) *scriptedTracker {
	if len(bodies) == 0 {
		bodies = []string{`{}`}
	}
	return s.add(method, path, &trackedRoute{status: http.StatusOK, bodies: bodies})
}

// fail scripts a route that answers 500.
func (s *scriptedTracker) fail(method, path string) *scriptedTracker {
	return s.add(method, path, &trackedRoute{status: http.StatusInternalServerError, bodies: []string{`{"error":"scripted failure"}`}})
}

func (s *scriptedTracker) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, trackedCall{Method: r.Method, Path: r.URL.Path, Body: string(body)})
	route, ok := s.routes[r.Method+" "+r.URL.Path]
	if !ok {
		s.unscripted = append(s.unscripted, r.Method+" "+r.URL.Path)
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"error":"unscripted"}`)
		return
	}
	i := route.hits
	if i >= len(route.bodies) {
		i = len(route.bodies) - 1
	}
	route.hits++
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(route.status)
	_, _ = io.WriteString(w, route.bodies[i])
}

// callsTo returns the requests one route received, in order.
func (s *scriptedTracker) callsTo(method, path string) []trackedCall {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []trackedCall
	for _, c := range s.calls {
		if c.Method == method && c.Path == path {
			out = append(out, c)
		}
	}
	return out
}

// problems names every unscripted request and every scripted route that was
// never hit.
func (s *scriptedTracker) problems() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []string
	for _, u := range s.unscripted {
		out = append(out, "tracker: unscripted request "+u)
	}
	for _, key := range s.order {
		if s.routes[key].hits == 0 {
			out = append(out, "tracker: scripted route never hit: "+key)
		}
	}
	return out
}

// ytRepoMode is ytRepo with the milestone mode as a parameter: a repo dir whose
// .dross root holds boardJSON, and a Ctx over a YouTrack client pointed at h.
func ytRepoMode(t *testing.T, h http.Handler, boardJSON, mode string) (string, *Ctx) {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	t.Setenv("MOCK_TOKEN", "secret")
	dir := t.TempDir()
	root := filepath.Join(dir, ".dross")
	mustWrite(t, filepath.Join(root, board.File), boardJSON)
	p := &project.Project{}
	p.Remote.URL = srv.URL
	p.Board = project.Board{Enabled: true, Provider: "youtrack", BaseURL: srv.URL, AuthEnv: "MOCK_TOKEN", Project: "PROJ", MilestoneMode: mode}
	return dir, repoCtx(t, p, root)
}

// jiraRepo is the Jira counterpart of ytRepoMode.
func jiraRepo(t *testing.T, h http.Handler, boardJSON string) (string, *Ctx) {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	t.Setenv("MOCK_TOKEN", "secret")
	dir := t.TempDir()
	root := filepath.Join(dir, ".dross")
	mustWrite(t, filepath.Join(root, board.File), boardJSON)
	p := &project.Project{}
	p.Remote.URL = srv.URL
	p.Board = project.Board{Enabled: true, Provider: "jira", BaseURL: srv.URL, AuthEnv: "MOCK_TOKEN", AuthUser: "dev@example.com", Project: "PROJ"}
	return dir, repoCtx(t, p, root)
}

// repoCtx resolves the board client for p through Config — the allowlist
// derived from [remote].url, exactly as cmd's openBoard does — and loads the
// board.json under root.
func repoCtx(t *testing.T, p *project.Project, root string) *Ctx {
	t.Helper()
	client, err := forge.NewBoard(Config(p.Board, p.Remote.URL, nil))
	if err != nil {
		t.Fatalf("forge.NewBoard: %v", err)
	}
	bd, err := board.Load(filepath.Join(root, board.File))
	if err != nil {
		t.Fatal(err)
	}
	return &Ctx{Client: client, Board: bd, Proj: p, Root: root, BoardPath: filepath.Join(root, board.File), Out: new(bytes.Buffer)}
}

// --- self-tests: a double that silently stops doing its job turns every test
// built on it vacuous, so each guarantee the doubles make is pinned here. ---

// TestFaultBoardInjectsPerKey: a fault on one key fails that key only.
func TestFaultBoardInjectsPerKey(t *testing.T) {
	f := newFaultBoard()
	f.seed(forge.Issue{Key: "K", Labels: []string{"a"}})
	f.seed(forge.Issue{Key: "L", Labels: []string{"a"}})
	boom := errors.New("boom")
	f.failUpdate["K"] = boom
	relabel := []string{"b"}

	if _, err := f.UpdateIssue("K", forge.IssuePatch{Labels: &relabel}); !errors.Is(err, boom) {
		t.Errorf("UpdateIssue(K) err = %v, want the injected error", err)
	}
	if got := f.issues["K"].Labels; !reflect.DeepEqual(got, []string{"a"}) {
		t.Errorf("a failed update touched K's labels: %v", got)
	}
	if _, err := f.UpdateIssue("L", forge.IssuePatch{Labels: &relabel}); err != nil {
		t.Errorf("K's fault leaked onto L: %v", err)
	}
	if got := f.issues["L"].Labels; !reflect.DeepEqual(got, []string{"b"}) {
		t.Errorf("L's labels = %v, want [b]", got)
	}
	if got := f.callsOf("UpdateIssue"); !reflect.DeepEqual(got, []string{"UpdateIssue K", "UpdateIssue L"}) {
		t.Errorf("calls = %v, want the failed attempt logged too", got)
	}

	f.refuseClose["K"] = true
	if err := f.CloseIssue("K"); err != nil {
		t.Errorf("a refused close must report success, got %v", err)
	}
	if iss, _ := f.GetIssue("K"); iss.State != "open" {
		t.Errorf("a refused close left State %q, want open", iss.State)
	}

	f.failGet["L"] = boom
	f.nilGet["M"] = true
	if _, err := f.GetIssue("L"); !errors.Is(err, boom) {
		t.Errorf("GetIssue(L) err = %v", err)
	}
	if iss, err := f.GetIssue("M"); iss != nil || err != nil {
		t.Errorf("nilGet gave (%v, %v), want (nil, nil)", iss, err)
	}
	f.failCreate["bad"] = boom
	if _, err := f.CreateIssue(forge.IssueInput{Title: "bad"}); !errors.Is(err, boom) {
		t.Errorf("CreateIssue(bad) err = %v", err)
	}
	if iss, err := f.CreateIssue(forge.IssueInput{Title: "good"}); err != nil || iss.Key == "K" || iss.Key == "L" {
		t.Errorf("CreateIssue(good) = %v, %v; want a fresh key", iss, err)
	}

	f.milestoneErr = boom
	if _, err := f.EnsureMilestone("v1", "body"); !errors.Is(err, boom) {
		t.Errorf("EnsureMilestone err = %v", err)
	}
	if !reflect.DeepEqual(f.milestones, []milestoneCall{{Title: "v1", Body: "body"}}) {
		t.Errorf("milestone calls = %v", f.milestones)
	}
}

// TestDoubleCapabilitiesAreExact: each double offers exactly the optional
// capability it is named for. faultBoard offering StateWriter would let an
// undo refusal test pass vacuously.
func TestDoubleCapabilitiesAreExact(t *testing.T) {
	for _, tc := range []struct {
		name         string
		client       forge.BoardClient
		state, links bool
	}{
		{"faultBoard", newFaultBoard(), false, false},
		{"stateBoard", newStateBoard(), true, false},
		{"linkBoard", newLinkBoard(), false, true},
	} {
		_, isState := tc.client.(forge.StateWriter)
		_, isLink := tc.client.(forge.IssueLinker)
		if isState != tc.state || isLink != tc.links {
			t.Errorf("%s: StateWriter=%v IssueLinker=%v, want %v/%v", tc.name, isState, isLink, tc.state, tc.links)
		}
	}

	s := newStateBoard()
	s.seed(forge.Issue{Key: "A", State: "closed"})
	s.refuseState["B"] = errors.New("refused")
	if err := s.SetStateRaw("A", "In Review"); err != nil || s.issues["A"].State != "In Review" {
		t.Errorf("SetStateRaw(A) = %v, state %q", err, s.issues["A"].State)
	}
	if err := s.SetStateRaw("B", "open"); err == nil {
		t.Error("a refused SetStateRaw reported success")
	}
	if !reflect.DeepEqual(s.states, []stateWrite{{Key: "A", State: "In Review"}}) {
		t.Errorf("states = %v, want only the successful write", s.states)
	}

	l := newLinkBoard()
	if err := l.LinkIssues("A", "B"); err != nil || !reflect.DeepEqual(l.links, [][2]string{{"A", "B"}}) {
		t.Errorf("LinkIssues = %v, links %v", err, l.links)
	}
	l.linkErr = errors.New("no link type")
	if err := l.LinkIssues("A", "C"); err == nil || len(l.links) != 1 {
		t.Errorf("a failing LinkIssues = %v, links %v", err, l.links)
	}
}

// TestCaptureStderrRestoresTheStream: the capture returns exactly what fn
// wrote and puts the original stream back — including when fn panics.
func TestCaptureStderrRestoresTheStream(t *testing.T) {
	orig := os.Stderr
	big := strings.Repeat("x", 200_000) // larger than any pipe buffer
	got := captureStderr(t, func() {
		fmt.Fprint(os.Stderr, "hello\n")
		fmt.Fprint(os.Stderr, big)
	})
	if got != "hello\n"+big {
		t.Errorf("captured %d bytes, want %d", len(got), len("hello\n")+len(big))
	}
	if os.Stderr != orig {
		t.Fatal("os.Stderr was not restored after a normal return")
	}

	func() {
		defer func() {
			if recover() == nil {
				t.Error("the panic inside fn did not propagate")
			}
		}()
		captureStderr(t, func() { panic("boom") })
	}()
	if os.Stderr != orig {
		t.Fatal("os.Stderr was not restored after a panic inside fn")
	}
}

// TestScriptedTrackerReportsUnscriptedAndUnusedRoutes: the tracker's strictness
// is the whole reason to use it over a permissive handler, so both halves —
// an unscripted request and a scripted route nobody called — must be reported.
func TestScriptedTrackerReportsUnscriptedAndUnusedRoutes(t *testing.T) {
	tr := newScriptedTracker()
	tr.on("GET", "/api/issues/PROJ-1", `{"idReadable":"PROJ-1","summary":"one"}`)
	tr.on("GET", "/api/agiles", `[]`) // scripted, never called
	tr.fail("POST", "/api/issues")
	_, ctx := ytRepoMode(t, tr, `{}`, "epic")

	iss, err := ctx.Client.GetIssue("PROJ-1")
	if err != nil || iss.Key != "PROJ-1" {
		t.Fatalf("scripted GET = %v, %v", iss, err)
	}
	if _, err := ctx.Client.CreateIssue(forge.IssueInput{Title: "new card", Body: "b"}); err == nil {
		t.Error("a scripted 500 on POST /api/issues did not surface as a client error")
	}
	if _, err := ctx.Client.GetIssue("PROJ-2"); err == nil {
		t.Error("an unscripted GET succeeded")
	}

	posts := tr.callsTo("POST", "/api/issues")
	if len(posts) != 1 || !strings.Contains(posts[0].Body, `"summary":"new card"`) {
		t.Errorf("POST /api/issues bodies = %v, want the create body recorded", posts)
	}

	got := tr.problems()
	sort.Strings(got)
	want := []string{
		"tracker: scripted route never hit: GET /api/agiles",
		"tracker: unscripted request GET /api/issues/PROJ-2",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("problems = %q\nwant       %q", got, want)
	}

	// The Jira builder resolves a real JiraClient against the same tracker.
	jt := newScriptedTracker()
	jt.on("GET", "/rest/api/3/issue/PROJ-9", `{"key":"PROJ-9","fields":{"summary":"nine"}}`)
	_, jctx := jiraRepo(t, jt, `{}`)
	if _, ok := jctx.Client.(*forge.JiraClient); !ok {
		t.Fatalf("jiraRepo client is %T", jctx.Client)
	}
	if iss, err := jctx.Client.GetIssue("PROJ-9"); err != nil || iss.Key != "PROJ-9" {
		t.Errorf("Jira GET = %v, %v", iss, err)
	}
	if p := jt.problems(); len(p) != 0 {
		t.Errorf("jira tracker problems = %v", p)
	}
}
