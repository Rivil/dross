package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// taskCloseFake is a flat issue board that tracks each issue's open/closed
// state, so a close and the read-back that verifies it are both expressible.
// Self-contained rather than an extension of fakeForge: the assertions here are
// about state transitions and about calls that must NOT happen, and fakeForge
// models neither.
type taskCloseFake struct {
	mu       sync.Mutex
	requests int
	closes   map[string]int  // issue key -> close writes seen
	closed   map[string]bool // issue key -> what a read-back reports
	// refuse names issues whose read-back keeps reporting open however many
	// times they are closed — a tracker that accepted the write and moved
	// nothing, which is the only failure a verified close can catch.
	refuse map[string]bool
	// provider decides the wire dialect the fake answers in: GitLab reads
	// labels as plain strings and the others as {"name":...} objects, so one
	// shape cannot serve both.
	provider string
	nextID   int
	labelIDs map[string]int
	issues   map[string][]string // issue key -> label names
	srv      *httptest.Server
}

func newTaskCloseFake(t *testing.T) *taskCloseFake {
	t.Helper()
	f := &taskCloseFake{
		closes:   map[string]int{},
		closed:   map[string]bool{},
		refuse:   map[string]bool{},
		labelIDs: map[string]int{},
		issues:   map[string][]string{},
		nextID:   1,
	}
	f.srv = httptest.NewServer(http.HandlerFunc(f.handle))
	t.Cleanup(f.srv.Close)
	return f
}

// count returns how many HTTP requests the fake has seen. The validation tests
// assert this is ZERO: a rejected flag must be rejected before the board is
// ever opened.
func (f *taskCloseFake) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.requests
}

func (f *taskCloseFake) closeCount(key string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.closes[key]
}

func (f *taskCloseFake) labelsOf(key string) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.issues[key]...)
}

func (f *taskCloseFake) handle(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests++
	w.Header().Set("Content-Type", "application/json")

	state := func(key string) string {
		if f.closed[key] {
			return "closed"
		}
		return "open"
	}
	labelObjs := func(key string) any {
		if f.provider == "gitlab" {
			return append([]string(nil), f.issues[key]...)
		}
		out := make([]any, 0, len(f.issues[key]))
		for _, n := range f.issues[key] {
			out = append(out, map[string]any{"name": n})
		}
		return out
	}
	// Labels arrive in three shapes across these backends: forge label IDs,
	// GitHub label names, and GitLab's comma-joined string.
	namesFrom := func(in map[string]any) []string {
		var names []string
		switch raw := in["labels"].(type) {
		case string:
			for _, n := range strings.Split(raw, ",") {
				if n != "" {
					names = append(names, n)
				}
			}
		case []any:
			for _, v := range raw {
				switch id := v.(type) {
				case float64:
					for n, i := range f.labelIDs {
						if i == int(id) {
							names = append(names, n)
						}
					}
				case string:
					names = append(names, id)
				}
			}
		}
		return names
	}
	// issueBody is the wire shape every backend here reads: `number` for the
	// forge/GitHub decoders, `iid` for GitLab's. Emitting both keeps one fake
	// honest for all four — a response carrying only `number` decodes to issue
	// 0 on GitLab, which is how a card silently goes missing.
	issueBody := func(key string, extra map[string]any) map[string]any {
		out := map[string]any{
			"number": mustAtoi(key),
			"iid":    mustAtoi(key),
			"state":  state(key),
			"labels": labelObjs(key),
		}
		for k, v := range extra {
			out[k] = v
		}
		return out
	}

	switch {
	case strings.HasSuffix(r.URL.Path, "/labels") && r.Method == "GET":
		rows := []map[string]any{}
		for name, id := range f.labelIDs {
			rows = append(rows, map[string]any{"id": id, "name": name})
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

	// A whole-set label replace on an existing issue.
	case strings.HasSuffix(r.URL.Path, "/labels") && r.Method == "PUT":
		key := strings.Split(strings.TrimSuffix(r.URL.Path, "/labels"), "/issues/")
		var in map[string]any
		_ = json.NewDecoder(r.Body).Decode(&in)
		if len(key) == 2 {
			f.issues[key[1]] = namesFrom(in)
		}
		_, _ = io.WriteString(w, `{}`)

	case r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/issues"):
		var in map[string]any
		_ = json.NewDecoder(r.Body).Decode(&in)
		key := strconv.Itoa(f.nextID)
		f.nextID++
		f.issues[key] = namesFrom(in)
		_ = json.NewEncoder(w).Encode(issueBody(key, map[string]any{"title": in["title"]}))

	case r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/issues"):
		_, _ = io.WriteString(w, `[]`)

	case (r.Method == "PATCH" || r.Method == "PUT") && strings.Contains(r.URL.Path, "/issues/"):
		key := lastSegment(r.URL.Path)
		var in map[string]any
		_ = json.NewDecoder(r.Body).Decode(&in)
		// `state` is the forge/GitHub spelling; `state_event` is GitLab's.
		st, _ := in["state"].(string)
		ev, _ := in["state_event"].(string)
		if st == "closed" || ev == "close" {
			f.closes[key]++
			f.closed[key] = !f.refuse[key]
		}
		if lbls, ok := in["labels"]; ok && lbls != nil {
			f.issues[key] = namesFrom(in)
		}
		_ = json.NewEncoder(w).Encode(issueBody(key, nil))

	case r.Method == "GET" && strings.Contains(r.URL.Path, "/issues/"):
		key := lastSegment(r.URL.Path)
		_ = json.NewEncoder(w).Encode(issueBody(key, nil))

	default:
		_, _ = io.WriteString(w, `[]`)
	}
}

func mustAtoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}

// taskCloseRepo scaffolds a board-enabled repo for `provider` with a phase, a
// plan of `taskIDs`, and the phase already synced.
func taskCloseRepo(t *testing.T, provider string, f *taskCloseFake, taskIDs ...string) string {
	t.Helper()
	f.mu.Lock()
	f.provider = provider
	f.mu.Unlock()
	dir := flatBoardRepo(t, provider, f.srv.URL)
	// flatBoardRepo seeds a quick link; the task lane wants a clean board.
	mustWrite(t, filepath.Join(dir, ".dross", "board.json"), emptyBoardJSON)
	writeSpec(t, dir, "01-auth", "[phase]\n  id = \"01-auth\"\n  title = \"Auth\"\n")

	var b strings.Builder
	fmt.Fprintf(&b, "task_seq = %d\n\n[phase]\n  id = \"01-auth\"\n", len(taskIDs))
	for i, id := range taskIDs {
		fmt.Fprintf(&b, "\n[[task]]\n  id = %q\n  wave = 1\n  title = \"Task %d\"\n  files = [\"a%d.go\"]\n  status = \"pending\"\n", id, i+1, i+1)
	}
	writePlan(t, dir, "01-auth", b.String())

	if err := runCmd(t, Issue(), "phase-sync", "01-auth"); err != nil {
		t.Fatalf("phase-sync: %v", err)
	}
	return dir
}

// TestTaskSyncValidatesStatusBeforeTouchingTheBoard closes the asymmetry with
// phase-sync. It matters from here on: `task-complete` is a status a human
// types at ship time, and a typo that reaches the board silently is a card that
// never leaves the review column.
//
// Zero requests is the assertion, not just the non-zero exit: validating after
// openBoard would still fail, but only after the run had already written.
func TestTaskSyncValidatesStatusBeforeTouchingTheBoard(t *testing.T) {
	f := newTaskCloseFake(t)
	taskCloseRepo(t, "forgejo", f, "t-1")
	before := f.count()

	err := runCmd(t, Issue(), "task-sync", "01-auth", "--status", "bogus")
	if err == nil {
		t.Fatal("an unknown --status was accepted")
	}
	if !strings.Contains(err.Error(), "task-in-review") {
		t.Errorf("error = %v, want it to name configenum.LifecycleStatuses.List()", err)
	}
	if got := f.count() - before; got != 0 {
		t.Errorf("%d tracker requests made for a rejected --status, want 0", got)
	}
}

// TestTaskCloseRequiresAStatus guards the task_terminal_status decision at the
// flag layer. closeBoardIssue defaults an empty status to `complete` — the
// PHASE lane's terminal state — so letting --close through without --status
// would write the phase lane's state onto every task card.
func TestTaskCloseRequiresAStatus(t *testing.T) {
	f := newTaskCloseFake(t)
	taskCloseRepo(t, "forgejo", f, "t-1")
	before := f.count()

	err := runCmd(t, Issue(), "task-sync", "01-auth", "--close")
	if err == nil {
		t.Fatal("--close without --status was accepted; the phase lane's `complete` would reach the task cards")
	}
	if !strings.Contains(err.Error(), "--status") {
		t.Errorf("error = %v, want it to name --status", err)
	}
	if got := f.count() - before; got != 0 {
		t.Errorf("%d tracker requests made for a rejected flag combination, want 0", got)
	}
}

// TestTaskSyncNormalizesStatus proves validation reassigns rather than merely
// checking: the padded, mixed-case form must reach the label and the state map
// normalized, or the label reads "dross/status: Task-In-Review" and the state
// lookup misses.
func TestTaskSyncNormalizesStatus(t *testing.T) {
	f := newTaskCloseFake(t)
	dir := taskCloseRepo(t, "forgejo", f, "t-1")

	if err := runCmd(t, Issue(), "task-sync", "01-auth", "--status", " Task-In-Review"); err != nil {
		t.Fatalf("task-sync: %v", err)
	}
	bd := loadBoardFile(t, dir)
	key, ok := bd.TaskIssue("01-auth", "t-1")
	if !ok {
		t.Fatal("t-1 was never mirrored")
	}
	labels := f.labelsOf(key)
	if !slicesHas(labels, statusLabel(statusTaskInReview)) {
		t.Errorf("labels = %v, want the normalized %s", labels, statusLabel(statusTaskInReview))
	}
	for _, l := range labels {
		if strings.Contains(l, " Task-In-Review") {
			t.Errorf("labels = %v, want the padded form normalized away", labels)
		}
	}
}

// TestFlatBoardTaskCloseClosesEveryCard is c-2 on the boards that have no
// resolution concept. task-pull refuses these providers by name; copying that
// shape here would strand every task card on forgejo, gitea, gitlab and github
// forever — the exact defect this phase exists to fix (flat_board_close).
func TestFlatBoardTaskCloseClosesEveryCard(t *testing.T) {
	for _, provider := range flatProviders {
		t.Run(provider, func(t *testing.T) {
			f := newTaskCloseFake(t)
			dir := taskCloseRepo(t, provider, f, "t-1", "t-2")

			var errOut string
			out := captureStdout(t, func() {
				errOut = captureStderr(t, func() {
					if err := runCmd(t, Issue(), "task-sync", "01-auth", "--status", "uat", "--close"); err != nil {
						t.Fatalf("task-sync --close on %s: %v", provider, err)
					}
				})
			})
			_ = out
			bd := loadBoardFile(t, dir)
			for _, id := range []string{"t-1", "t-2"} {
				key, ok := bd.TaskIssue("01-auth", id)
				if !ok {
					t.Fatalf("%s was never mirrored", id)
				}
				if got := f.closeCount(key); got != 1 {
					t.Errorf("%s: close writes = %d, want exactly 1", id, got)
				}
			}
			for _, refusal := range []string{"cannot close", "does not support", "unsupported"} {
				if strings.Contains(strings.ToLower(errOut), refusal) {
					t.Errorf("stderr = %q, want no provider refusal on a flat board", errOut)
				}
			}
			if strings.Contains(errOut, "state_map") || strings.Contains(errOut, "no ") && strings.Contains(errOut, "mapping") {
				t.Errorf("stderr = %q, want no state-map complaint — flat boards take no state write", errOut)
			}
		})
	}
}

// TestTaskClosePartialFailureNamesTheTaskAndKeepsGoing is the shape that keeps
// one stuck card from stranding the rest. The middle card's read-back keeps
// reporting open; the command must still attempt the third, fail overall, and
// name the card the tracker refused.
//
// board.json must record no agreement point for the failed card: `task-pull`
// compares against that snapshot, and claiming an agreement the board never
// reached would make the next pull read a phantom move.
func TestTaskClosePartialFailureNamesTheTaskAndKeepsGoing(t *testing.T) {
	f := newTaskCloseFake(t)
	dir := taskCloseRepo(t, "forgejo", f, "t-1", "t-2", "t-3")

	// Mirror the cards first so the refusal can be aimed at t-2's real issue.
	if err := runCmd(t, Issue(), "task-sync", "01-auth"); err != nil {
		t.Fatalf("seed task-sync: %v", err)
	}
	bd := loadBoardFile(t, dir)
	stuck, ok := bd.TaskIssue("01-auth", "t-2")
	if !ok {
		t.Fatal("t-2 was never mirrored")
	}
	f.mu.Lock()
	f.refuse[stuck] = true
	f.mu.Unlock()

	var err error
	_ = captureStdout(t, func() {
		_ = captureStderr(t, func() {
			err = runCmd(t, Issue(), "task-sync", "01-auth", "--status", "uat", "--close")
		})
	})
	if err == nil {
		t.Fatal("a refused close reported overall success")
	}
	if !strings.Contains(err.Error(), "t-2") {
		t.Errorf("error = %v, want it to name the task the tracker refused", err)
	}

	after := loadBoardFile(t, dir)
	third, ok := after.TaskIssue("01-auth", "t-3")
	if !ok {
		t.Fatal("t-3 was never mirrored")
	}
	if got := f.closeCount(third); got != 1 {
		t.Errorf("t-3 close writes = %d, want 1 — a refused card must not stop the ones after it", got)
	}

	failed, _ := after.TaskLinkFor("01-auth", "t-2")
	if failed.Issue != stuck {
		t.Errorf("t-2 link = %q, want the issue key kept so the next run need not re-query by label", failed.Issue)
	}
	if failed.BoardState != "" {
		t.Errorf("t-2 agreement point = %q, want none recorded for a card that never reached the state", failed.BoardState)
	}
	ok3, _ := after.TaskLinkFor("01-auth", "t-3")
	if ok3.BoardState != "uat" {
		t.Errorf("t-3 agreement point = %q, want uat — the cards that DID close keep theirs", ok3.BoardState)
	}
}
