package cmd

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/Rivil/dross/internal/board"
)

// fakeForge is a minimal forge REST tracker: it records what dross asked it to
// do, which is what these tests assert about. A backend rather than a stubbed
// client on purpose — the thing under test is the sequence of calls the sync
// makes, and stubbing the client would assert the code against itself.
type fakeForge struct {
	mu      sync.Mutex
	created []map[string]any
	updated map[string]map[string]any
	nextID  int
	byLabel map[string][]string // label name -> issue keys
	issues  map[string]map[string]any
	// labelIDs is name -> id. The forge REST API models labels as entities and
	// takes IDs on an issue, so the fake has to resolve them back to names or
	// nothing downstream can tell what an issue is tagged with — which is
	// exactly how dross finds an existing issue on a re-sync.
	labelIDs map[string]int
	srv      *httptest.Server
}

func newFakeForge(t *testing.T) *fakeForge {
	t.Helper()
	f := &fakeForge{
		updated:  map[string]map[string]any{},
		byLabel:  map[string][]string{},
		issues:   map[string]map[string]any{},
		labelIDs: map[string]int{},
		nextID:   1,
	}
	f.srv = httptest.NewServer(http.HandlerFunc(f.handle))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeForge) handle(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")

	switch {
	// Labels exist as first-class entities on a forge; the client ensures each
	// one before it can put it on an issue.
	case strings.HasSuffix(r.URL.Path, "/labels") && r.Method == "GET":
		var rows []map[string]any
		for name, id := range f.labelIDs {
			rows = append(rows, map[string]any{"id": id, "name": name})
		}
		if rows == nil {
			rows = []map[string]any{}
		}
		_ = json.NewEncoder(w).Encode(rows)
	case strings.HasSuffix(r.URL.Path, "/labels") && r.Method == "POST":
		var in map[string]any
		_ = json.NewDecoder(r.Body).Decode(&in)
		name, _ := in["name"].(string)
		id := len(f.labelIDs) + 1
		f.labelIDs[name] = id
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{"id": id, "name": name})
	case strings.Contains(r.URL.Path, "/labels") && r.Method == "PUT":
		_, _ = io.WriteString(w, `{}`)

	case r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/issues"):
		var in map[string]any
		_ = json.NewDecoder(r.Body).Decode(&in)
		f.created = append(f.created, in)
		key := itoaFake(f.nextID)
		f.nextID++
		names := f.labelNames(in)
		iss := map[string]any{"number": f.nextID - 1, "title": in["title"], "body": in["body"], "labels": namesToObjs(names)}
		f.issues[key] = iss
		for _, l := range names {
			f.byLabel[l] = append(f.byLabel[l], key)
		}
		_ = json.NewEncoder(w).Encode(iss)

	case r.Method == "GET" && strings.Contains(r.URL.Path, "/issues/"):
		key := lastSegment(r.URL.Path)
		iss, ok := f.issues[key]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, `{}`)
			return
		}
		_ = json.NewEncoder(w).Encode(iss)

	case (r.Method == "PATCH" || r.Method == "PUT") && strings.Contains(r.URL.Path, "/issues/"):
		key := lastSegment(r.URL.Path)
		var in map[string]any
		_ = json.NewDecoder(r.Body).Decode(&in)
		f.updated[key] = in
		iss := f.issues[key]
		if iss == nil {
			iss = map[string]any{"number": 1}
		}
		if t, ok := in["title"]; ok {
			iss["title"] = t
		}
		if b, ok := in["body"]; ok {
			iss["body"] = b
		}
		iss["labels"] = namesToObjs(f.labelNames(in))
		f.issues[key] = iss
		_ = json.NewEncoder(w).Encode(iss)

	case r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/issues"):
		want := r.URL.Query().Get("labels")
		var out []map[string]any
		for _, key := range f.byLabel[want] {
			out = append(out, f.issues[key])
		}
		if out == nil {
			out = []map[string]any{}
		}
		_ = json.NewEncoder(w).Encode(out)

	default:
		_, _ = io.WriteString(w, `[]`)
	}
}

func (f *fakeForge) createdTitles() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, c := range f.created {
		if s, ok := c["title"].(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// labelNames resolves whatever shape the request used back to names: forge
// REST sends integer IDs, GitLab a comma-joined string. Without this the fake
// cannot say what an issue is tagged with, and the label lookup that finds an
// existing issue on a re-sync would never match.
func (f *fakeForge) labelNames(in map[string]any) []string {
	byID := map[int]string{}
	for name, id := range f.labelIDs {
		byID[id] = name
	}
	var out []string
	switch raw := in["labels"].(type) {
	case []any:
		for _, l := range raw {
			switch v := l.(type) {
			case string:
				out = append(out, v)
			case float64:
				if name, ok := byID[int(v)]; ok {
					out = append(out, name)
				}
			}
		}
	case string:
		for _, s := range strings.Split(raw, ",") {
			if s != "" {
				out = append(out, s)
			}
		}
	}
	return out
}

func namesToObjs(names []string) []any {
	out := make([]any, 0, len(names))
	for _, s := range names {
		out = append(out, map[string]any{"name": s})
	}
	return out
}

func lastSegment(p string) string {
	i := strings.LastIndex(p, "/")
	return p[i+1:]
}

func itoaFake(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

// taskSyncRepo is a board-enabled repo with a phase, a two-task plan, and the
// phase already synced — the state the execute loop is in when it calls
// task-sync.
func taskSyncRepo(t *testing.T, f *fakeForge) string {
	t.Helper()
	dir := boardRepo(t, f.srv.URL, true)
	writeSpec(t, dir, "01-auth", "[phase]\n  id = \"01-auth\"\n  title = \"Auth\"\n")
	writePlan(t, dir, "01-auth", `task_seq = 2

[phase]
  id = "01-auth"

[[task]]
  id = "t-1"
  wave = 1
  title = "First task"
  files = ["a.go"]
  status = "pending"

[[task]]
  id = "t-2"
  wave = 1
  title = "Second task"
  files = ["b.go"]
  status = "pending"
`)
	if err := runCmd(t, Issue(), "phase-sync", "01-auth"); err != nil {
		t.Fatalf("phase-sync: %v", err)
	}
	return dir
}

func loadBoardFile(t *testing.T, dir string) *board.Board {
	t.Helper()
	b, err := board.Load(filepath.Join(dir, ".dross", "board.json"))
	if err != nil {
		t.Fatalf("load board.json: %v", err)
	}
	return b
}

// TestTaskSyncIsIdempotent is the property that makes this safe to call from
// the execute loop, which runs it at every task edge. A second sync must update
// rather than duplicate.
func TestTaskSyncIsIdempotent(t *testing.T) {
	f := newFakeForge(t)
	dir := taskSyncRepo(t, f)

	for i := 0; i < 2; i++ {
		if err := runCmd(t, Issue(), "task-sync", "01-auth"); err != nil {
			t.Fatalf("task-sync run %d: %v", i+1, err)
		}
	}

	taskIssues := 0
	for _, title := range f.createdTitles() {
		if strings.Contains(title, "01-auth/t-") {
			taskIssues++
		}
	}
	if taskIssues != 2 {
		t.Errorf("created %d task issues over two syncs, want 2 — a re-run duplicated instead of updating", taskIssues)
	}

	b := loadBoardFile(t, dir)
	if len(b.Tasks) != 2 {
		t.Errorf("board.json holds %d task mappings, want 2: %v", len(b.Tasks), b.Tasks)
	}
	if _, ok := b.TaskIssue("01-auth", "t-1"); !ok {
		t.Error("t-1 has no recorded issue")
	}
}

// TestTaskSyncScopesToOneTask: the execute loop syncs one task at a time, and
// rewriting the other seven on every commit would make every task's issue churn
// on work it had nothing to do with.
func TestTaskSyncScopesToOneTask(t *testing.T) {
	f := newFakeForge(t)
	taskSyncRepo(t, f)

	if err := runCmd(t, Issue(), "task-sync", "01-auth", "t-1"); err != nil {
		t.Fatalf("task-sync: %v", err)
	}

	titles := f.createdTitles()
	for _, title := range titles {
		if strings.Contains(title, "t-2") {
			t.Errorf("syncing t-1 also created an issue for t-2: %v", titles)
		}
	}
	found := false
	for _, title := range titles {
		if strings.Contains(title, "01-auth/t-1") {
			found = true
		}
	}
	if !found {
		t.Errorf("no issue was created for the named task: %v", titles)
	}
}

// TestTaskIssueIsRelatedToItsPhase: an unrelated task issue is a loose card
// nobody can trace back. Where the tracker cannot express a link, the body must
// still name the phase issue.
func TestTaskIssueIsRelatedToItsPhase(t *testing.T) {
	f := newFakeForge(t)
	dir := taskSyncRepo(t, f)

	if err := runCmd(t, Issue(), "task-sync", "01-auth"); err != nil {
		t.Fatalf("task-sync: %v", err)
	}

	b := loadBoardFile(t, dir)
	parent, ok := b.PhaseIssue("01-auth")
	if !ok {
		t.Fatal("the phase has no issue — the fixture is wrong")
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.created {
		title, _ := c["title"].(string)
		if !strings.Contains(title, "01-auth/t-") {
			continue
		}
		body, _ := c["body"].(string)
		if !strings.Contains(body, parent) {
			t.Errorf("task issue %q does not name its phase issue %s in the body:\n%s", title, parent, body)
		}
		if !containsStr(f.labelNames(c), phaseLabel("01-auth")) {
			t.Errorf("task issue %q does not carry the phase label (labels: %v)", title, f.labelNames(c))
		}
	}
}

// TestNoLinkerWarnsOnceAndContinues: the forge REST backend has no issue-link
// primitive. An eight-task phase must not print eight identical warnings — that
// is how a warning becomes scrollback — and every task issue must still exist.
func TestNoLinkerWarnsOnceAndContinues(t *testing.T) {
	f := newFakeForge(t)
	taskSyncRepo(t, f)

	stderr := captureStderr(t, func() {
		_ = captureStdout(t, func() {
			if err := runCmd(t, Issue(), "task-sync", "01-auth"); err != nil {
				t.Fatalf("task-sync: %v", err)
			}
		})
	})

	if n := strings.Count(stderr, "cannot relate issues"); n > 1 {
		t.Errorf("the capability gap warned %d times for a 2-task phase, want at most 1:\n%s", n, stderr)
	}
	created := 0
	for _, title := range f.createdTitles() {
		if strings.Contains(title, "01-auth/t-") {
			created++
		}
	}
	if created != 2 {
		t.Errorf("created %d task issues, want 2 — a capability gap must not stop the sync", created)
	}
}

// TestTaskSyncRequiresAPhaseIssue: a task issue with no parent to relate to is
// a loose card. Refusing names the command that fixes it rather than creating
// orphans.
func TestTaskSyncRequiresAPhaseIssue(t *testing.T) {
	f := newFakeForge(t)
	dir := boardRepo(t, f.srv.URL, true)
	writeSpec(t, dir, "01-auth", "[phase]\n  id = \"01-auth\"\n  title = \"Auth\"\n")
	writePlan(t, dir, "01-auth", "task_seq = 1\n\n[phase]\n  id = \"01-auth\"\n\n[[task]]\n  id = \"t-1\"\n  wave = 1\n  title = \"T\"\n  status = \"pending\"\n")

	err := runCmd(t, Issue(), "task-sync", "01-auth")
	if err == nil {
		t.Fatal("task-sync created task issues with no phase issue to relate them to")
	}
	if !strings.Contains(err.Error(), "phase-sync") {
		t.Errorf("the refusal does not name the command that fixes it: %v", err)
	}
}

// TestTaskSyncIsANoOpWhenBoardSyncIsOff: every `dross issue` verb is, which is
// what lets the loop prompts call them unconditionally. A prompt-side condition
// would be a second place for that rule to drift.
func TestTaskSyncIsANoOpWhenBoardSyncIsOff(t *testing.T) {
	f := newFakeForge(t)
	dir := boardRepo(t, f.srv.URL, false) // not enabled
	writeSpec(t, dir, "01-auth", "[phase]\n  id = \"01-auth\"\n  title = \"Auth\"\n")

	if err := runCmd(t, Issue(), "task-sync", "01-auth"); err != nil {
		t.Fatalf("task-sync with board sync off must be a silent no-op: %v", err)
	}
	if len(f.createdTitles()) != 0 {
		t.Errorf("a disabled board still created issues: %v", f.createdTitles())
	}
}

// seedIssue puts an issue on the fake directly, tagged with the given labels —
// the state of a tracker that already holds work dross did not create in this
// run. Callers use it to build situations a sync cannot reach on its own.
func (f *fakeForge) seedIssue(num int, labels ...string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := strconv.Itoa(num)
	f.issues[key] = map[string]any{"number": num, "title": "seeded " + key, "body": "", "labels": namesToObjs(labels)}
	for _, l := range labels {
		f.byLabel[l] = append(f.byLabel[l], key)
		// The tracker knows a label as an entity; a query naming one it has
		// never heard of is dropped before it is sent, so a seeded issue whose
		// labels are not in the index could never be found by label.
		if _, ok := f.labelIDs[l]; !ok {
			f.labelIDs[l] = len(f.labelIDs) + 1
		}
	}
}

// dropTaskCache rewrites board.json with the phase mapping only, which is the
// state of a fresh clone: the tracker still holds the task issues, the local
// cache does not know about them.
func dropTaskCache(t *testing.T, dir string) {
	t.Helper()
	b := loadBoardFile(t, dir)
	parent, ok := b.PhaseIssue("01-auth")
	if !ok {
		t.Fatal("the phase has no issue — the fixture is wrong")
	}
	mustWrite(t, filepath.Join(dir, ".dross", "board.json"),
		`{"phases":{"01-auth":`+strconv.Quote(parent)+`},"tasks":{},"quicks":{},"milestones":{},"backlog":{},"dismissed":[]}`)
}

// TestTaskResolvedByLabelIsSilentAndReused is the re-clone path: with no cache
// entry, the label lookup finds the one existing issue. One match is the normal
// case and must be silent — a warning here would fire on every fresh clone —
// and it must update that issue rather than create a second.
func TestTaskResolvedByLabelIsSilentAndReused(t *testing.T) {
	f := newFakeForge(t)
	dir := taskSyncRepo(t, f)

	stderr := captureStderr(t, func() {
		_ = captureStdout(t, func() {
			if err := runCmd(t, Issue(), "task-sync", "01-auth", "t-1"); err != nil {
				t.Fatalf("first task-sync: %v", err)
			}
		})
	})
	before := len(f.createdTitles())
	dropTaskCache(t, dir)

	stderr += captureStderr(t, func() {
		_ = captureStdout(t, func() {
			if err := runCmd(t, Issue(), "task-sync", "01-auth", "t-1"); err != nil {
				t.Fatalf("second task-sync: %v", err)
			}
		})
	})

	if strings.Contains(stderr, "leaving the rest") {
		t.Errorf("a single label match warned about duplicates:\n%s", stderr)
	}
	if got := len(f.createdTitles()); got != before {
		t.Errorf("created %d issues, want %d — the label lookup did not reuse the existing issue", got, before)
	}
	if n, ok := loadBoardFile(t, dir).TaskIssue("01-auth", "t-1"); !ok || n == "" {
		t.Errorf("t-1 mapping = %q,%v — the re-resolved issue was not written back to the cache", n, ok)
	}
}

// TestDuplicateTaskIssuesWarnAndUpdateTheFirst: two issues carrying one task's
// label is a tracker dross cannot silently pick between. It names all of them,
// updates the lowest-sorting one deterministically, and leaves the rest alone
// rather than merging or deleting anything.
func TestDuplicateTaskIssuesWarnAndUpdateTheFirst(t *testing.T) {
	f := newFakeForge(t)
	dir := taskSyncRepo(t, f)
	label := taskLabel("01-auth", "t-1")
	f.seedIssue(81, labelMarker, phaseLabel("01-auth"), label)
	f.seedIssue(82, labelMarker, phaseLabel("01-auth"), label)

	stderr := captureStderr(t, func() {
		_ = captureStdout(t, func() {
			if err := runCmd(t, Issue(), "task-sync", "01-auth", "t-1"); err != nil {
				t.Fatalf("task-sync: %v", err)
			}
		})
	})

	if !strings.Contains(stderr, "2 issues carry") {
		t.Errorf("stderr = %q, want the duplicate count named", stderr)
	}
	for _, key := range []string{"81", "82"} {
		if !strings.Contains(stderr, key) {
			t.Errorf("stderr = %q, want %s named so the duplicate can be found", stderr, key)
		}
	}
	if _, ok := f.updated["81"]; !ok {
		t.Errorf("updated = %v, want the lowest-sorting duplicate 81 updated", f.updated)
	}
	if _, ok := f.updated["82"]; ok {
		t.Errorf("updated = %v, want 82 left alone", f.updated)
	}
	if n, _ := loadBoardFile(t, dir).TaskIssue("01-auth", "t-1"); n != "81" {
		t.Errorf("cached mapping = %q, want 81 — the pick must be recorded, not re-decided every sync", n)
	}
}
