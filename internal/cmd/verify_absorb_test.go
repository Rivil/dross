package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/Rivil/dross/internal/mutation"
	"github.com/Rivil/dross/internal/state"
	"github.com/Rivil/dross/internal/verify"
)

// countingBoard points the current repo's [board] at a YouTrack server
// running h, and returns how many requests it has seen. Every uat sync this
// file asserts on is counted here.
func countingBoard(t *testing.T, h http.HandlerFunc) *int {
	t.Helper()
	var mu sync.Mutex
	n := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		n++
		mu.Unlock()
		h(w, r)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("MOCK_TOKEN", "secret")
	mustRunSet(t, "remote.url", srv.URL)
	mustRunSet(t, "board.provider", "youtrack")
	mustRunSet(t, "board.base_url", srv.URL)
	mustRunSet(t, "board.auth_env", "MOCK_TOKEN")
	mustRunSet(t, "board.project", "PROJ")
	mustRunSet(t, "board.enabled", "true")
	return &n
}

func failingBoard(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusInternalServerError)
}

// finishBoardFixture is finishFixture with a spec on disk, so a board sync
// reaches the tracker rather than failing on the missing spec.
func finishBoardFixture(t *testing.T, id string) string {
	t.Helper()
	root := finishFixture(t, id)
	mustWrite(t, filepath.Join(root, "phases", id, "spec.toml"), "[phase]\nid=\""+id+"\"\ntitle=\""+id+"\"\n")
	return root
}

func TestVerifyBoardFailureKeepsResults(t *testing.T) {
	const id = "p"
	root := finishBoardFixture(t, id)
	hits := countingBoard(t, failingBoard)
	var out string
	err := runCmdCapturingInto(t, &out, func() error {
		return finishVerify(root, id, finishSpec(id, "c-1"), finishTests(id), "helicon", nil)
	})
	if err == nil {
		t.Fatal("finishVerify succeeded against a failing board")
	}
	if code := exitCodeOf(err); code != exitVerifyBoardFailed {
		t.Errorf("exit code = %d, want %d — distinct from a mutation-tool failure's 1", code, exitVerifyBoardFailed)
	}
	if !strings.Contains(out, "board: FAILED") || !strings.Contains(out, "--status uat") {
		t.Errorf("output %q does not name the board failure and its retry", out)
	}
	if *hits == 0 {
		t.Error("the board was never called")
	}
	testsPath, verifyPath := verify.FilePaths(root, id)
	raw, rerr := os.ReadFile(testsPath)
	if rerr != nil {
		t.Fatalf("tests.json lost to the board failure: %v", rerr)
	}
	var got verify.Tests
	if err := json.Unmarshal(raw, &got); err != nil || len(got.Languages) != 1 || got.Languages[0].Mutation.Killed != 9 {
		t.Errorf("tests.json does not carry the measured run (err %v): %+v", err, got.Languages)
	}
	v, verr := verify.LoadVerify(verifyPath)
	if verr != nil || v == nil || v.Summary.MutantsKilled != 9 {
		t.Errorf("verify.toml does not carry the measured counts (err %v): %+v", verr, v)
	}
}

func TestVerifyBoardOffPrintsOff(t *testing.T) {
	const id = "p"
	root := finishBoardFixture(t, id)
	var out string
	if err := runCmdCapturingInto(t, &out, func() error {
		return finishVerify(root, id, finishSpec(id, "c-1"), finishTests(id), "helicon", nil)
	}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "board: off") {
		t.Errorf("output %q, want `board: off`", out)
	}
}

// TestVerifyBoardSyncsUAT: a measured run moves the board to uat, once.
func TestVerifyBoardSyncsUAT(t *testing.T) {
	const id = "p"
	root := finishBoardFixture(t, id)
	f := newYTPhaseFake()
	countingBoard(t, f.handler(t))
	var out string
	if err := runCmdCapturingInto(t, &out, func() error {
		return finishVerify(root, id, finishSpec(id, "c-1"), finishTests(id), "helicon", nil)
	}); err != nil {
		t.Fatalf("finishVerify: %v\n%s", err, out)
	}
	if !strings.Contains(out, "board: phase p -> board PROJ-100 (uat)") {
		t.Errorf("output %q, want the uat board line", out)
	}
	if !slicesHas(f.tags["PROJ-100"], "dross/status:uat") {
		t.Errorf("phase issue tags = %v, want the uat status label", f.tags["PROJ-100"])
	}
}

func TestVerifyToolErrorMakesNoBoardRequests(t *testing.T) {
	const id = "busy"
	dir := scopedVerifyRepo(t, id)
	phaseSpec(t, id)
	writeScopeFile(t, dir, "a.go", "package x\n\nfunc A() bool { return 1 > 0 }\n")
	mustGit(t, dir, "commit", "-qam", "phase edits a.go")
	if err := runCmd(t, Changes(), "record", id, "t-1", "--files", "a.go"); err != nil {
		t.Fatal(err)
	}
	mustSetBase(t, id, "base")
	// A held host fails the only leg: RunScoped returns before any artefact
	// or skeleton exists.
	useStubAdapter(t, &busyAdapter{})
	hits := countingBoard(t, failingBoard)
	if err := runCmd(t, Verify(), id); err == nil {
		t.Fatal("verify succeeded with its only leg refused")
	}
	if *hits != 0 {
		t.Errorf("a verify that wrote nothing made %d board requests, want 0", *hits)
	}
	if _, verifyPath := verify.FilePaths(filepath.Join(dir, RootDirName), id); isFile(verifyPath) {
		t.Error("verify.toml exists although no leg ran")
	}
}

func TestVerifyDetachSyncsOnlyAtResults(t *testing.T) {
	dir := detachCmdRepo(t, "detachcmd", mutationTuning{Target: detachTarget()})
	rec := &detachRecorder{}
	rec.install(t)
	hits := countingBoard(t, failingBoard)
	if err := runCmd(t, Verify(), "detachcmd", "--detach"); err != nil {
		t.Fatalf("dross verify --detach: %v", err)
	}
	if *hits != 0 {
		t.Errorf("a detached dispatch made %d board requests, want 0", *hits)
	}
	_ = dir
}

func TestVerifyResultsSyncsUATOnce(t *testing.T) {
	const id = "collect"
	collectRepo(t, id)
	f := newYTPhaseFake()
	countingBoard(t, f.handler(t))
	var out string
	if err := runCmdCapturingInto(t, &out, func() error { return collectDetached(id) }); err != nil {
		t.Fatalf("collectDetached: %v\n%s", err, out)
	}
	if n := countLines(out, "board: phase "+id+" -> board"); n != 1 {
		t.Errorf("collect printed %d uat board lines, want exactly 1:\n%s", n, out)
	}
	if !strings.Contains(out, "(uat)") {
		t.Errorf("collect output %q, want the uat sync", out)
	}
}

// writeFinalVerdict writes a verify.toml for p with the given verdict.
func writeFinalVerdict(t *testing.T, root, id, verdict string) {
	t.Helper()
	_, verifyPath := verify.FilePaths(root, id)
	mustWrite(t, verifyPath, `[verify]
  phase = "`+id+`"
  verdict = "`+verdict+`"

[summary]
  mutation_status = "measured"
  mutation_score = 0.97
  criteria_total = 2
  criteria_covered = 2

[[criterion]]
  id = "c-1"
  status = "covered"

[[criterion]]
  id = "c-2"
  status = "covered"
`)
}

func TestVerifyFinalizeOwnsTheVerifiedStatus(t *testing.T) {
	const id = "p"
	root := finishFixture(t, id)
	statePath := filepath.Join(root, state.File)
	mustRunState(t, "current_phase_status", "complete")

	writeFinalVerdict(t, root, id, "pending")
	before := fileSum(t, statePath)
	if err := runCmd(t, Verify(), "finalize", id); err == nil {
		t.Fatal("finalize accepted a pending verdict")
	}
	if fileSum(t, statePath) != before {
		t.Error("a refused finalize changed state.json")
	}

	writeFinalVerdict(t, root, id, "pass")
	if err := runCmd(t, Verify(), "finalize", id); err != nil {
		t.Fatal(err)
	}
	s, err := state.Load(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if s.CurrentPhaseStatus != "verified" {
		t.Errorf("current_phase_status = %q, want verified", s.CurrentPhaseStatus)
	}
	last := s.History[len(s.History)-1].Action
	if last != "verified p: pass (2/2, mutation 0.97)" {
		t.Errorf("last history entry = %q, want the verified entry", last)
	}
	n := len(s.History)
	if err := runCmd(t, Verify(), "finalize", id); err != nil {
		t.Fatal(err)
	}
	if s2, _ := state.Load(statePath); len(s2.History) != n {
		t.Errorf("a second finalize appended a history entry (%d -> %d)", n, len(s2.History))
	}
}

// TestAutoFinalizeLeavesStatusAlone: ship and phase complete heal through
// finalizeVerify, which must never walk a shipped phase back to verified.
func TestAutoFinalizeLeavesStatusAlone(t *testing.T) {
	const id = "p"
	root := finishFixture(t, id)
	mustRunState(t, "current_phase_status", "shipped")
	writeFinalVerdict(t, root, id, "pass")
	if recorded, _, err := finalizeVerify(root, id); err != nil || !recorded {
		t.Fatalf("finalizeVerify = %v, %v", recorded, err)
	}
	s, err := state.Load(filepath.Join(root, state.File))
	if err != nil {
		t.Fatal(err)
	}
	if s.CurrentPhaseStatus != "shipped" {
		t.Errorf("auto-finalize moved current_phase_status to %q, want shipped", s.CurrentPhaseStatus)
	}
}

// TestVerifyLegRunnerMakesNoBoardRequests: the shared leg runner — what the
// attached path and the remote leg both run — never touches the board; only
// finishVerify, after the artefacts, does.
func TestVerifyLegRunnerMakesNoBoardRequests(t *testing.T) {
	const id = "p"
	finishBoardFixture(t, id)
	hits := countingBoard(t, failingBoard)
	a := &stubMutationAdapter{name: "gremlins", exts: []string{".go"}, report: finishTests(id).Languages[0].Mutation}
	if _, err := verify.RunScoped(id, []string{"internal/cmd/verify.go"}, []mutation.Adapter{a}, nil); err != nil {
		t.Fatalf("RunScoped: %v", err)
	}
	if *hits != 0 {
		t.Errorf("the leg runner made %d board requests, want 0", *hits)
	}
}

// TestVerifyResultsBoardFailureStillCollects: a board failure at collect
// leaves the run collected — artefacts written, record cleared — and exits 16.
func TestVerifyResultsBoardFailureStillCollects(t *testing.T) {
	const id = "collect"
	dir := collectRepo(t, id)
	countingBoard(t, failingBoard)
	err := collectDetached(id)
	if code := exitCodeOf(err); code != exitVerifyBoardFailed {
		t.Fatalf("collect against a failing board = %v (code %d), want exit %d", err, code, exitVerifyBoardFailed)
	}
	root := filepath.Join(dir, RootDirName)
	if rec, ferr := findDetachedRun(root, dir, id); ferr != nil || rec != nil {
		t.Errorf("the detached record survived a collected run: %+v (err %v)", rec, ferr)
	}
	if _, verifyPath := verify.FilePaths(root, id); !isFile(verifyPath) {
		t.Error("verify.toml was not written")
	}
}
