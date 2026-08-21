package cmd

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/forge"
	"github.com/Rivil/dross/internal/reaplog"
)

// undoYT models the one thing the apply fake does not need to: a card's
// resolved flag FOLLOWS its column. Writing "Verified" resolves it; writing
// "In Review" back un-resolves it. Without that, an undo could appear to
// succeed while leaving every card resolved.
type undoYT struct {
	state    map[string]string
	labels   map[string][]string
	refuse   map[string]bool
	wrote    map[string][]string
	requests int
}

// resolvedColumns are the columns this fake treats as done — the two the state
// map points the lane terminals at.
var resolvedColumns = map[string]bool{"Verified": true, "Task Done": true}

func newUndoYT() *undoYT {
	return &undoYT{
		state:  map[string]string{},
		labels: map[string][]string{},
		refuse: map[string]bool{},
		wrote:  map[string][]string{},
	}
}

func (f *undoYT) seed(key, state string, labels ...string) {
	f.state[key] = state
	f.labels[key] = labels
}

func (f *undoYT) render(key string) string {
	resolved := "null"
	if resolvedColumns[f.state[key]] {
		resolved = "1700000000000"
	}
	var tags []string
	for _, l := range f.labels[key] {
		tags = append(tags, `{"name":"`+l+`"}`)
	}
	return `{"idReadable":"` + key + `","resolved":` + resolved +
		`,"tags":[` + strings.Join(tags, ",") + `]` +
		`,"customFields":[{"name":"State","value":{"name":"` + f.state[key] + `"}}]}`
}

func (f *undoYT) handler(t *testing.T) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		f.requests++
		switch {
		case r.URL.Path == "/api/issueTags":
			_, _ = io.WriteString(w, `[{"id":"tid-dross","name":"dross"}]`)
		case r.URL.Path == "/api/issues" && r.Method == "GET":
			_, _ = io.WriteString(w, `[]`)
		case strings.HasPrefix(r.URL.Path, "/api/issues/") && r.Method == "GET":
			_, _ = io.WriteString(w, f.render(strings.TrimPrefix(r.URL.Path, "/api/issues/")))
		case strings.HasPrefix(r.URL.Path, "/api/issues/") && r.Method == "POST":
			key := strings.TrimPrefix(r.URL.Path, "/api/issues/")
			var body struct {
				CustomFields []struct {
					Name  string `json:"name"`
					Value struct {
						Name string `json:"name"`
					} `json:"value"`
				} `json:"customFields"`
			}
			raw, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(raw, &body)
			for _, cf := range body.CustomFields {
				if cf.Name != "State" {
					continue
				}
				f.wrote[key] = append(f.wrote[key], cf.Value.Name)
				if f.refuse[key] {
					w.WriteHeader(http.StatusInternalServerError)
					_, _ = io.WriteString(w, `{"error":"boom"}`)
					return
				}
				f.state[key] = cf.Value.Name
			}
			_, _ = io.WriteString(w, `{"idReadable":"`+key+`"}`)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			_, _ = io.WriteString(w, `{}`)
		}
	}
}

// undoRepo scaffolds the all-five-lanes fixture with a DIFFERENT starting
// column per card. That is the fixture's whole point: an undo that hardcodes
// one state, or that restores every card to whatever the first journal entry
// held, passes over a uniform fixture and fails here.
func undoRepo(t *testing.T, f *undoYT) string {
	t.Helper()
	srv := httptest.NewServer(f.handler(t))
	t.Cleanup(srv.Close)
	dir := youtrackBoardRepo(t, srv.URL)
	mustRunSet(t, "board.milestone_mode", "epic")
	mustRunSet(t, "board.state_map.complete", "Verified")
	mustRunSet(t, "board.state_map.task-complete", "Task Done")
	mustWrite(t, filepath.Join(dir, ".dross", "board.json"), strandedBoard)
	writeStrandedFixture(t, dir)
	f.seed("PROJ-1", "In Progress", labelMarker)
	f.seed("PROJ-2", "In Review", labelMarker)
	f.seed("PROJ-7", "Open", labelMarker)
	f.seed("PROJ-20", "Submitted", labelMarker)
	f.seed("PROJ-40", "Open", labelMarker)
	return dir
}

func runReap(t *testing.T, args ...string) error {
	t.Helper()
	var err error
	_ = captureStdout(t, func() {
		_ = captureStderr(t, func() {
			err = runCmd(t, Issue(), append([]string{"reap"}, args...)...)
		})
	})
	return err
}

// TestUndoRestoresTheRecordedPriorState: each card goes back to the exact
// column IT held, not one blanket state.
func TestUndoRestoresTheRecordedPriorState(t *testing.T) {
	f := newUndoYT()
	undoRepo(t, f)

	if err := runReap(t, "--apply"); err != nil {
		t.Fatalf("apply: %v", err)
	}
	for _, k := range []string{"PROJ-1", "PROJ-2", "PROJ-7", "PROJ-20"} {
		if !resolvedColumns[f.state[k]] {
			t.Fatalf("%s was not closed by the apply — the fixture proves nothing", k)
		}
	}

	if err := runReap(t, "--undo"); err != nil {
		t.Fatalf("undo: %v", err)
	}
	for _, tc := range []struct{ key, want string }{
		{"PROJ-1", "In Progress"},
		{"PROJ-2", "In Review"},
		{"PROJ-7", "Open"},
		{"PROJ-20", "Submitted"},
	} {
		if f.state[tc.key] != tc.want {
			t.Errorf("%s restored to %q, want the column it actually held: %q", tc.key, f.state[tc.key], tc.want)
		}
	}
}

// TestUndoRestoresDroppedBoardLinks: the sweep drops the backlog key, so undo
// has to put it back — otherwise the next backlog sync mints a duplicate for an
// item that already has a card.
func TestUndoRestoresDroppedBoardLinks(t *testing.T) {
	f := newUndoYT()
	dir := undoRepo(t, f)

	if err := runReap(t, "--apply"); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if strings.Contains(mustRead(t, filepath.Join(dir, ".dross", "board.json")), "slug:built") {
		t.Fatal("the apply did not drop the backlog link — the fixture proves nothing")
	}

	if err := runReap(t, "--undo"); err != nil {
		t.Fatalf("undo: %v", err)
	}
	bd := mustRead(t, filepath.Join(dir, ".dross", "board.json"))
	if !strings.Contains(bd, "slug:built") || !strings.Contains(bd, "PROJ-20") {
		t.Errorf("the dropped backlog link was not restored:\n%s", bd)
	}
}

// TestUndoRestoresACardMovedSinceTheSweep: there is deliberately no conflict
// check. A card someone moved since the sweep is the card most likely to need
// putting back, and skipping it would make undo quietly partial — "restored"
// over a board that still is not.
func TestUndoRestoresACardMovedSinceTheSweep(t *testing.T) {
	f := newUndoYT()
	undoRepo(t, f)

	if err := runReap(t, "--apply"); err != nil {
		t.Fatalf("apply: %v", err)
	}
	f.state["PROJ-1"] = "Blocked" // someone dragged it after the sweep

	if err := runReap(t, "--undo"); err != nil {
		t.Fatalf("undo: %v", err)
	}
	if f.state["PROJ-1"] != "In Progress" {
		t.Errorf("PROJ-1 is in %q; a card moved since the sweep must still be returned to its journalled column", f.state["PROJ-1"])
	}
}

// TestUndoContinuesPastAFailure mirrors the apply-side guarantee: one refused
// restore must not strand the rest.
func TestUndoContinuesPastAFailure(t *testing.T) {
	f := newUndoYT()
	undoRepo(t, f)

	if err := runReap(t, "--apply"); err != nil {
		t.Fatalf("apply: %v", err)
	}
	f.refuse["PROJ-2"] = true

	var err error
	_ = captureStdout(t, func() {
		errOut := captureStderr(t, func() {
			err = runCmd(t, Issue(), "reap", "--undo")
		})
		if !strings.Contains(errOut, "PROJ-2") {
			t.Errorf("the failing card is not named in the report:\n%s", errOut)
		}
	})
	if err == nil {
		t.Fatal("an undo with a refused restore exited 0")
	}
	for _, tc := range []struct{ key, want string }{
		{"PROJ-1", "In Progress"},
		{"PROJ-7", "Open"},
		{"PROJ-20", "Submitted"},
	} {
		if f.state[tc.key] != tc.want {
			t.Errorf("%s is in %q, want %q — one refused card stranded the rest", tc.key, f.state[tc.key], tc.want)
		}
	}
}

// TestUndoReversesOnlyTheLastRun: the locked undo_shape decision. Two scoped
// applies, and only the second is reversed.
func TestUndoReversesOnlyTheLastRun(t *testing.T) {
	f := newUndoYT()
	undoRepo(t, f)

	if err := runReap(t, "--apply", "--namespace", "phases"); err != nil {
		t.Fatalf("first apply: %v", err)
	}
	if err := runReap(t, "--apply", "--namespace", "tasks"); err != nil {
		t.Fatalf("second apply: %v", err)
	}

	if err := runReap(t, "--undo"); err != nil {
		t.Fatalf("undo: %v", err)
	}
	if f.state["PROJ-2"] != "In Review" {
		t.Errorf("the last run's task card is in %q, want In Review", f.state["PROJ-2"])
	}
	if f.state["PROJ-1"] != "Verified" {
		t.Errorf("the FIRST run's phase card was also reverted (now %q) — undo reverses the last run only", f.state["PROJ-1"])
	}
}

// TestUndoWithNoJournalIsClean: a first --undo on a repo that never applied
// says so, rather than erroring.
func TestUndoWithNoJournalIsClean(t *testing.T) {
	f := newUndoYT()
	dir := undoRepo(t, f)

	var err error
	out := captureStdout(t, func() {
		err = runCmd(t, Issue(), "reap", "--undo")
	})
	if err != nil {
		t.Fatalf("undo with no journal errored: %v", err)
	}
	if !strings.Contains(out, "nothing to undo") {
		t.Errorf("output = %q, want the nothing-to-undo line", out)
	}
	if len(f.wrote) != 0 {
		t.Errorf("undo wrote to %v with no journal to replay", f.wrote)
	}
	if l := loadReapLog(t, dir); len(l.Runs) != 0 {
		t.Errorf("a journal exists after no apply: %+v", l.Runs)
	}
}

// TestUndoRefusesFlagCombinations: --apply and --namespace have no meaning for
// a reversal that replays a recorded run verbatim, and silently dropping them
// would let an operator believe they had scoped an undo they had not.
func TestUndoRefusesFlagCombinations(t *testing.T) {
	f := newUndoYT()
	undoRepo(t, f)
	if err := runReap(t, "--apply"); err != nil {
		t.Fatalf("apply: %v", err)
	}
	before := len(f.wrote["PROJ-1"])

	for _, args := range [][]string{
		{"--undo", "--apply"},
		{"--undo", "--namespace", "phases"},
	} {
		if err := runReap(t, args...); err == nil {
			t.Errorf("reap %v was accepted", args)
		}
	}
	if len(f.wrote["PROJ-1"]) != before {
		t.Error("a refused flag combination still wrote to the board")
	}
}

// TestUndoRefusesOnABackendWithoutStateWriter: GitHub has no column model, so a
// restore is not merely lossy — a card that sat in "In Review" would come back
// as `open`. Refusing by provider name and writing nothing is the honest
// answer, and it is the same shape the no-linker fallback already uses.
func TestUndoRefusesOnABackendWithoutStateWriter(t *testing.T) {
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		t.Errorf("a refused --undo still called the board: %s %s", r.Method, r.URL.Path)
		_, _ = io.WriteString(w, `{}`)
	}))
	t.Cleanup(srv.Close)

	dir := t.TempDir()
	chdir(t, dir)
	t.Setenv("MOCK_TOKEN", "secret")
	if err := runCmd(t, Init()); err != nil {
		t.Fatalf("init: %v", err)
	}
	for k, v := range map[string]string{
		"remote.url":     srv.URL,
		"board.provider": "github",
		"board.base_url": srv.URL,
		"board.auth_env": "MOCK_TOKEN",
		"board.project":  "me/proj",
		"board.enabled":  "true",
	} {
		mustRunSet(t, k, v)
	}
	// A journal is deliberately present: the refusal must come from the
	// capability, not from an empty ledger.
	l := &reaplog.Log{}
	l.Append(reaplog.Run{Cards: []reaplog.Card{{Issue: "1", Class: "Phases", PriorState: "In Review", Outcome: reaplog.OutcomeClosed}}})
	if err := l.Save(reaplog.FilePath(filepath.Join(dir, ".dross"))); err != nil {
		t.Fatal(err)
	}

	var err error
	_ = captureStdout(t, func() {
		err = runCmd(t, Issue(), "reap", "--undo")
	})
	if err == nil {
		t.Fatal("--undo was accepted on a backend with no column model")
	}
	if !strings.Contains(err.Error(), "github") {
		t.Errorf("error %q does not name the provider", err)
	}
	if requests != 0 {
		t.Errorf("%d requests were issued by a refused undo", requests)
	}
}

// TestJournalledPriorStateFallsBackToTheOpenClosedState: a forge or GitLab
// board has no column model, so WorkflowState is empty on every card — and
// those backends ARE StateWriters, so undo really does run against them.
// Journalling WorkflowState alone would record "" and make every restore write
// an empty state and fail its read-back.
func TestJournalledPriorStateFallsBackToTheOpenClosedState(t *testing.T) {
	for _, tc := range []struct {
		name string
		iss  forge.Issue
		want string
	}{
		{"a board with a column model", forge.Issue{WorkflowState: "In Review", State: "open"}, "In Review"},
		{"a flat open/closed board", forge.Issue{State: "open"}, "open"},
	} {
		if got := priorStateOf(&tc.iss); got != tc.want {
			t.Errorf("%s: journalled prior state %q, want %q", tc.name, got, tc.want)
		}
	}
}
