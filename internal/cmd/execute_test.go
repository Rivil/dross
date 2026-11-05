package cmd

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/Rivil/dross/internal/board"
	"github.com/Rivil/dross/internal/boardsync"
	"github.com/Rivil/dross/internal/gatestate"
	"github.com/Rivil/dross/internal/phase"
	"github.com/Rivil/dross/internal/state"
)

// executeFixture is a dross repo with phase p planned.
func executeFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	chdir(t, dir)
	if err := runCmd(t, Init()); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(dir, ".dross", "phases", "p", "plan.toml"), "[phase]\n  id = \"p\"\n")
	return repoDirHere(t)
}

func executeRecord(t *testing.T, dir string) (*gatestate.Execute, []byte) {
	t.Helper()
	rec, err := gatestate.LoadExecute(dir)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(gatestate.Path(dir, gatestate.ExecuteFile))
	return rec, b
}

func TestExecuteBeginRecordsMode(t *testing.T) {
	dir := executeFixture(t)
	if err := runCmd(t, Execute(), "begin", "p"); err != nil {
		t.Fatal(err)
	}
	if rec, _ := executeRecord(t, dir); rec == nil || rec.Mode != "pair" || rec.Phase != "p" {
		t.Fatalf("begin p recorded %+v, want pair for p", rec)
	}
	if err := runCmd(t, Execute(), "begin", "p", "--solo"); err != nil {
		t.Fatal(err)
	}
	rec, before := executeRecord(t, dir)
	if rec == nil || rec.Mode != "solo" {
		t.Fatalf("begin p --solo recorded %+v, want solo", rec)
	}
	var out string
	if err := runCmdCapturing(t, &out, Execute(), "begin", "p", "--solo"); err != nil {
		t.Fatal(err)
	}
	if _, after := executeRecord(t, dir); string(after) != string(before) {
		t.Errorf("a same-mode re-run rewrote execute.json:\n%s\n->\n%s", before, after)
	}
	if !strings.Contains(out, "solo") {
		t.Errorf("begin printed %q, want the recorded mode", out)
	}
}

func TestExecuteBeginPreconditions(t *testing.T) {
	dir := executeFixture(t)
	mustWrite(t, filepath.Join(dir, ".dross", "phases", "unplanned", "spec.toml"), "[phase]\n")
	for _, id := range []string{"nope", "unplanned", "../p"} {
		if err := runCmd(t, Execute(), "begin", id); err == nil {
			t.Errorf("begin %s succeeded", id)
		}
	}
	if _, err := os.Stat(gatestate.Path(dir, gatestate.ExecuteFile)); !os.IsNotExist(err) {
		t.Errorf("a refused begin wrote execute.json (err=%v)", err)
	}
}

// TestExecuteBeginLeavesStateAlone: begin writes version and nothing else of
// state.json — never the phase, its status or the milestone.
func TestExecuteBeginLeavesStateAlone(t *testing.T) {
	dir := executeFixture(t)
	writeBeginMilestone(t, dir, "v1.8", "a", "p")
	mustRunState(t, "current_milestone", "v1.8")
	mustRunState(t, "current_phase", "other")
	mustRunState(t, "current_phase_status", "planned")
	mustRunState(t, "version", "1.8.1.3")
	before := loadStateAt(t, dir)
	for _, args := range [][]string{{"begin", "p"}, {"begin", "p", "--solo"}} {
		if err := runCmd(t, Execute(), args...); err != nil {
			t.Fatal(err)
		}
	}
	after := loadStateAt(t, dir)
	if after.Version != "1.8.2.0" {
		t.Errorf("version = %q, want 1.8.2.0", after.Version)
	}
	after.Version = before.Version
	a, _ := json.Marshal(after)
	b, _ := json.Marshal(before)
	if string(a) != string(b) {
		t.Errorf("dross execute begin changed more than version in state.json:\n%s\n->\n%s", b, a)
	}
}

func mustRunState(t *testing.T, field, value string) {
	t.Helper()
	if err := runCmd(t, State(), "set", field, value); err != nil {
		t.Fatalf("state set %s=%s: %v", field, value, err)
	}
}

func loadStateAt(t *testing.T, dir string) *state.State {
	t.Helper()
	s, err := state.Load(filepath.Join(dir, ".dross", "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// writeBeginMilestone writes milestone version with the given phases array.
func writeBeginMilestone(t *testing.T, dir, version string, phases ...string) {
	t.Helper()
	q := make([]string, len(phases))
	for i, p := range phases {
		q[i] = strconv.Quote(p)
	}
	mustWrite(t, filepath.Join(dir, ".dross", "milestones", version+".toml"),
		"phases = ["+strings.Join(q, ", ")+"]\n\n[milestone]\n  version = \""+version+"\"\n")
}

// beginOut runs `execute begin` with args and returns its stdout and error.
func beginOut(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var out string
	err := runCmdCapturing(t, &out, Execute(), append([]string{"begin"}, args...)...)
	return out, err
}

func countLines(out, prefix string) int {
	n := 0
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(l, prefix) {
			n++
		}
	}
	return n
}

func TestBeginBumpsVersion(t *testing.T) {
	dir := executeFixture(t)
	writeBeginMilestone(t, dir, "v1.8", "a", "p")
	mustRunState(t, "current_milestone", "v1.8")
	mustRunState(t, "version", "1.8.1.3")
	out, err := beginOut(t, "p")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "version: 1.8.1.3 -> 1.8.2.0") || countLines(out, "version:") != 1 {
		t.Errorf("begin printed %q, want one `version: 1.8.1.3 -> 1.8.2.0` line", out)
	}
	if v := loadStateAt(t, dir).Version; v != "1.8.2.0" {
		t.Errorf("state.json version = %q, want 1.8.2.0", v)
	}
	proj, err := os.ReadFile(filepath.Join(dir, ".dross", "project.toml"))
	if err != nil || !strings.Contains(string(proj), `version = "1.8.2.0"`) {
		t.Errorf("project.toml does not carry 1.8.2.0 (err %v):\n%s", err, proj)
	}
}

func TestBeginResumeKeepsInternal(t *testing.T) {
	dir := executeFixture(t)
	writeBeginMilestone(t, dir, "v1.8", "a", "p")
	mustRunState(t, "current_milestone", "v1.8")
	mustRunState(t, "version", "1.8.2.1")
	projPath := filepath.Join(dir, ".dross", "project.toml")
	before := fileSum(t, projPath)
	out, err := beginOut(t, "p")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "version: 1.8.2.1 unchanged") || countLines(out, "version:") != 1 {
		t.Errorf("begin printed %q, want `version: 1.8.2.1 unchanged`", out)
	}
	if fileSum(t, projPath) != before {
		t.Error("a resumed begin rewrote project.toml")
	}
	if v := loadStateAt(t, dir).Version; v != "1.8.2.1" {
		t.Errorf("state.json version = %q, want 1.8.2.1 kept", v)
	}
}

func TestBeginVersionSkipsWithoutAMilestone(t *testing.T) {
	for name, c := range map[string]struct {
		milestone string
		phases    []string
		want      string
	}{
		"no current milestone": {"", nil, "version: skipped (no current milestone)"},
		"phase not in array":   {"v1.8", []string{"a"}, "version: skipped (phase p is not in milestone v1.8)"},
		"non-vX.Y milestone":   {"2026-q1", []string{"p"}, `version: skipped (milestone "2026-q1" is not vX.Y)`},
	} {
		t.Run(name, func(t *testing.T) {
			dir := executeFixture(t)
			if c.milestone != "" {
				writeBeginMilestone(t, dir, c.milestone, c.phases...)
				mustRunState(t, "current_milestone", c.milestone)
			}
			mustRunState(t, "version", "1.8.1.3")
			projPath := filepath.Join(dir, ".dross", "project.toml")
			statePath := filepath.Join(dir, ".dross", "state.json")
			p0, s0 := fileSum(t, projPath), fileSum(t, statePath)
			out, err := beginOut(t, "p")
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(out, c.want) || countLines(out, "version:") != 1 {
				t.Errorf("begin printed %q, want %q", out, c.want)
			}
			if fileSum(t, projPath) != p0 || fileSum(t, statePath) != s0 {
				t.Error("a skipped version still wrote project.toml or state.json")
			}
		})
	}
}

// beginBoardServer is a YouTrack fake that counts every request it sees.
func beginBoardServer(t *testing.T, f *ytPhaseFake) (*httptest.Server, *int) {
	t.Helper()
	var mu sync.Mutex
	n := 0
	h := f.handler(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		n++
		mu.Unlock()
		h(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv, &n
}

// beginBoardRepo is a YouTrack-backed repo with phase p (spec + plan) in
// milestone v1.8 at version 1.8.1.3.
func beginBoardRepo(t *testing.T, srvURL, planBody string) string {
	t.Helper()
	dir := youtrackBoardRepo(t, srvURL)
	writeSpec(t, dir, "p", "[phase]\nid=\"p\"\ntitle=\"p\"\n")
	mustWrite(t, filepath.Join(dir, ".dross", "phases", "p", "plan.toml"), planBody)
	writeBeginMilestone(t, dir, "v1.8", "a", "p")
	mustRunState(t, "current_milestone", "v1.8")
	mustRunState(t, "version", "1.8.1.3")
	return dir
}

const beginPlan = `[phase]
  id = "p"

[[task]]
  id = "t-2"
  wave = 1
  title = "second"
  status = "pending"
`

func TestBeginRefusalsHaveNoSideEffects(t *testing.T) {
	f := newYTPhaseFake()
	srv, hits := beginBoardServer(t, f)
	dir := beginBoardRepo(t, srv.URL, beginPlan)
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir()) // no reviewer installed: --solo must refuse
	projPath := filepath.Join(dir, ".dross", "project.toml")
	statePath := filepath.Join(dir, ".dross", "state.json")
	p0, s0 := fileSum(t, projPath), fileSum(t, statePath)
	for _, args := range [][]string{{"nope"}, {"../p"}, {"p", "--solo"}} {
		out, err := beginOut(t, args...)
		if err == nil {
			t.Errorf("begin %v succeeded", args)
		}
		if strings.Contains(out, "version:") {
			t.Errorf("refused begin %v printed a version line: %q", args, out)
		}
	}
	if fileSum(t, projPath) != p0 || fileSum(t, statePath) != s0 {
		t.Error("a refused begin wrote project.toml or state.json")
	}
	if *hits != 0 {
		t.Errorf("a refused begin made %d board requests, want 0", *hits)
	}
}

// syncedBoard records t-2 as last agreed at pending on PROJ-2, whose card the
// fake reports in board status boardStatus.
func syncedBoard(t *testing.T, dir string, f *ytPhaseFake, boardStatus string) {
	t.Helper()
	bp := filepath.Join(dir, ".dross", board.File)
	bd, err := board.Load(bp)
	if err != nil {
		t.Fatal(err)
	}
	bd.SetTaskSynced("p", "t-2", "PROJ-2", phase.StatusPending, "")
	if err := bd.Save(bp); err != nil {
		t.Fatal(err)
	}
	f.addIssue("PROJ-2", "t-2", boardsync.StatusLabel(boardStatus))
}

func TestBeginSyncsBoardAndReportsMoves(t *testing.T) {
	f := newYTPhaseFake()
	srv, _ := beginBoardServer(t, f)
	dir := beginBoardRepo(t, srv.URL, beginPlan)
	syncedBoard(t, dir, f, boardsync.StatusTaskComplete)
	planPath := filepath.Join(dir, ".dross", "phases", "p", "plan.toml")
	before := fileSum(t, planPath)
	out, err := beginOut(t, "p")
	if err != nil {
		t.Fatalf("begin: %v\n%s", err, out)
	}
	if !strings.Contains(out, "board: phase p -> board PROJ-100 (in-progress)") {
		t.Errorf("begin printed %q, want the in-progress board line", out)
	}
	if !slicesHas(f.tags["PROJ-100"], boardsync.StatusLabel("in-progress")) {
		t.Errorf("the phase issue carries %v, want the in-progress status label", f.tags["PROJ-100"])
	}
	if !strings.Contains(out, "pull: t-2 pending -> done") || !strings.Contains(out, "--apply") {
		t.Errorf("begin printed %q, want the t-2 move reported with the apply command", out)
	}
	if fileSum(t, planPath) != before {
		t.Error("begin applied a board move: plan.toml changed")
	}
}

func TestBeginReportsAConflictWithoutApplying(t *testing.T) {
	f := newYTPhaseFake()
	srv, _ := beginBoardServer(t, f)
	dir := beginBoardRepo(t, srv.URL, strings.Replace(beginPlan, `"pending"`, `"in_progress"`, 1))
	syncedBoard(t, dir, f, boardsync.StatusTaskComplete)
	planPath := filepath.Join(dir, ".dross", "phases", "p", "plan.toml")
	before := fileSum(t, planPath)
	out, err := beginOut(t, "p")
	if err != nil {
		t.Fatalf("begin: %v\n%s", err, out)
	}
	if !strings.Contains(out, "pull: t-2 CONFLICT") {
		t.Errorf("begin printed %q, want the t-2 conflict named", out)
	}
	if fileSum(t, planPath) != before {
		t.Error("begin wrote plan.toml on a conflict")
	}
}

func TestBeginBoardFailureKeepsTheModeRecord(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	dir := beginBoardRepo(t, srv.URL, beginPlan)
	out, err := beginOut(t, "p")
	if err == nil {
		t.Fatalf("begin succeeded against a failing board:\n%s", out)
	}
	if !strings.Contains(out, "board: FAILED") || countLines(out, "version:") != 1 {
		t.Errorf("begin printed %q, want `board: FAILED` after one version line", out)
	}
	if rec, _ := executeRecord(t, dir); rec == nil || rec.Phase != "p" || rec.Mode != "pair" {
		t.Errorf("execute.json = %+v, want the pair record kept despite the board failure", rec)
	}
}

func TestBeginBoardOffMakesNoRequests(t *testing.T) {
	f := newYTPhaseFake()
	srv, hits := beginBoardServer(t, f)
	beginBoardRepo(t, srv.URL, beginPlan)
	mustRunSet(t, "board.enabled", "false")
	out, err := beginOut(t, "p")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "board: off") || countLines(out, "version:") != 1 {
		t.Errorf("begin printed %q, want `board: off`", out)
	}
	if *hits != 0 {
		t.Errorf("board off made %d requests, want 0", *hits)
	}
}

// TestBeginPullIsNAOnAnOpenClosedBoard: a board with no workflow state is a
// configuration fact, not a failure.
func TestBeginPullIsNAOnAnOpenClosedBoard(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet:
			_, _ = io.WriteString(w, "[]")
		case strings.HasSuffix(r.URL.Path, "/labels"):
			_, _ = io.WriteString(w, `{"id":1,"name":"x"}`)
		default:
			_, _ = io.WriteString(w, `{"number":1,"title":"p — p","state":"open","labels":[]}`)
		}
	}))
	t.Cleanup(srv.Close)
	dir := boardRepo(t, srv.URL, true)
	writeSpec(t, dir, "p", "[phase]\nid=\"p\"\ntitle=\"p\"\n")
	mustWrite(t, filepath.Join(dir, ".dross", "phases", "p", "plan.toml"), beginPlan)
	out, err := beginOut(t, "p")
	if err != nil {
		t.Fatalf("begin on a forgejo board: %v\n%s", err, out)
	}
	if !strings.Contains(out, "pull: n/a") {
		t.Errorf("begin printed %q, want `pull: n/a`", out)
	}
}
