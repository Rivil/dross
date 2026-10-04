package cmd

import (
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"

	"github.com/Rivil/dross/internal/changes"
	"github.com/Rivil/dross/internal/ship"
	"github.com/Rivil/dross/internal/state"
	"github.com/Rivil/dross/internal/telemetry"
)

// allowBoardHost adds the board fake's address to this machine's allowlist,
// next to whatever is already there (the forge mock, in the ship fixture).
func allowBoardHost(t *testing.T, dir string, f *taskCloseFake) {
	t.Helper()
	var local struct {
		AllowHosts string `toml:"allow_hosts"`
	}
	if _, err := toml.DecodeFile(filepath.Join(dir, ".dross", LocalFile), &local); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	hosts := f.srv.Listener.Addr().String()
	if local.AllowHosts != "" {
		hosts = local.AllowHosts + "," + hosts
	}
	if err := runCmd(t, Local(), "set", "allow_hosts", hosts); err != nil {
		t.Fatal(err)
	}
}

func pointBoardAt(t *testing.T, f *taskCloseFake, on bool, authEnv string) {
	t.Helper()
	f.mu.Lock()
	f.provider = "forgejo"
	f.mu.Unlock()
	for _, kv := range [][2]string{
		{"board.provider", "forgejo"},
		{"board.base_url", f.srv.URL},
		{"board.auth_env", authEnv},
		{"board.project", "me/proj"},
		{"board.enabled", strconv.FormatBool(on)},
	} {
		mustRunSet(t, kv[0], kv[1])
	}
}

// boardCompleteFixture ships phase x — now carrying a two-task plan — through
// the mock forge, squash-merges it on origin, and points [board] at f. It
// stops just before `dross phase complete`.
func boardCompleteFixture(t *testing.T, f *taskCloseFake, on bool, authEnv string) string {
	t.Helper()
	dir := shipFixture(t, "https://forge.example/me/p.git")
	shipMockFlow(t, dir)
	stubPRMerged(t, true)
	t.Setenv("MOCK_TOKEN", "secret")
	allowBoardHost(t, dir, f)
	pointBoardAt(t, f, on, authEnv)
	writePlan(t, dir, "x", "task_seq = 2\n\n[phase]\n  id = \"x\"\n\n[[task]]\n  id = \"t-1\"\n  wave = 1\n  title = \"one\"\n  status = \"done\"\n\n[[task]]\n  id = \"t-2\"\n  wave = 1\n  title = \"two\"\n  status = \"done\"\n")
	gitCommit(t, dir, "test: board config and a plan")

	mustGit(t, dir, "push", "-q", "origin", "main:main")
	mustGit(t, dir, "fetch", "-q", "origin")
	if err := runCmd(t, Ship()); err != nil {
		t.Fatalf("ship: %v", err)
	}
	simulateSquashMerge(t, dir, "phase/x", "main")
	return dir
}

// phaseCards splits the fake's cards for phase id into the phase card and the
// task cards.
func phaseCards(t *testing.T, f *taskCloseFake, id string) (phaseCard string, tasks []string) {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	for key, labels := range f.issues {
		isPhase, isTask := false, false
		for _, l := range labels {
			isPhase = isPhase || l == "dross/phase:"+id
			isTask = isTask || strings.HasPrefix(l, "dross/task:"+id+"/")
		}
		switch {
		case isTask:
			tasks = append(tasks, key)
		case isPhase:
			phaseCard = key
		}
	}
	return phaseCard, tasks
}

func wantCardTerminal(t *testing.T, f *taskCloseFake, key, status string) {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.closed[key] {
		t.Errorf("card %s reads open", key)
	}
	if !slicesHas(f.issues[key], "dross/status:"+status) {
		t.Errorf("card %s labels = %v, want dross/status:%s", key, f.issues[key], status)
	}
}

func wantBoardFinalized(t *testing.T, f *taskCloseFake) {
	t.Helper()
	phaseCard, tasks := phaseCards(t, f, "x")
	if phaseCard == "" || len(tasks) != 2 {
		t.Fatalf("cards: phase %q, tasks %v — want one phase card and two task cards", phaseCard, tasks)
	}
	for _, k := range tasks {
		wantCardTerminal(t, f, k, "task-complete")
	}
	wantCardTerminal(t, f, phaseCard, "complete")
}

// TestCompleteFinalizesTheBoard: `dross phase complete` leaves every card
// closed at its terminal state, a clean tree, and the base level with origin.
func TestCompleteFinalizesTheBoard(t *testing.T) {
	f := newTaskCloseFake(t)
	dir := boardCompleteFixture(t, f, true, "MOCK_TOKEN")
	if err := runCmd(t, Phase(), "complete", "x"); err != nil {
		t.Fatalf("complete: %v", err)
	}
	wantBoardFinalized(t, f)
	if out := mustGit(t, dir, "status", "--porcelain"); out != "" {
		t.Errorf("complete left the tree dirty:\n%s", out)
	}
	if local, origin := mustGit(t, dir, "rev-parse", "main"), mustGit(t, dir, "rev-parse", "origin/main"); local != origin {
		t.Errorf("main %s is not level with origin/main %s", local, origin)
	}
}

// TestCompleteBoardFailureKeepsCompletion: a board that refuses leaves the
// completion standing — record written, status complete, branches gone, the
// completed and branch lines printed, the outcome event recorded — and the
// error names the retry, which then finishes the job.
func TestCompleteBoardFailureKeepsCompletion(t *testing.T) {
	f := newTaskCloseFake(t)
	dir := boardCompleteFixture(t, f, true, "MOCK_TOKEN")
	f.mu.Lock()
	f.refuse["1"] = true // the phase card is created first
	f.mu.Unlock()
	t.Setenv("DROSS_NO_TELEMETRY", "")
	// Telemetry lands under the package-wide test HOME, so count this run's
	// event rather than trusting one some earlier test may have left behind.
	before := phaseCompleteEvents(t)

	var err error
	out := captureStdout(t, func() { err = runCmd(t, Phase(), "complete", "x") })
	if err == nil || !strings.Contains(err.Error(), "dross issue phase finalize x") {
		t.Fatalf("err = %v, want the finalize retry named", err)
	}
	for _, want := range []string{"completed x", "branch:"} {
		if !strings.Contains(out, want) {
			t.Errorf("stdout lacks %q — the board failure replaced the completion narration:\n%s", want, out)
		}
	}
	root := filepath.Join(dir, ".dross")
	s, lerr := state.Load(filepath.Join(root, state.File))
	if lerr != nil || !hasAction(s, "completed x") {
		t.Errorf("the completion record is missing (load err %v)", lerr)
	}
	if ch, cerr := changes.Load(changes.FilePath(root, "x"), "x"); cerr != nil || !ch.Complete() {
		t.Errorf("changes.json does not read complete (err %v)", cerr)
	}
	if _, gerr := gitOut(dir, "rev-parse", "--verify", "--quiet", "refs/heads/phase/x"); gerr == nil {
		t.Error("phase/x still exists locally")
	}
	if remote := mustGit(t, dir, "ls-remote", "--heads", "origin", "phase/x"); remote != "" {
		t.Errorf("phase/x still exists on origin: %s", remote)
	}
	if got := phaseCompleteEvents(t); got != before+1 {
		t.Errorf("phase_complete outcome events %d → %d, want one recorded by this run", before, got)
	}

	f.mu.Lock()
	f.refuse = map[string]bool{}
	f.mu.Unlock()
	if err := runCmd(t, Issue(), "phase", "finalize", "x"); err != nil {
		t.Fatalf("the finalize retry: %v", err)
	}
	wantBoardFinalized(t, f)
}

// TestCompleteBoardOffMakesNoCall: with board sync off, complete builds no
// client, sends nothing, and says nothing about a board.
func TestCompleteBoardOffMakesNoCall(t *testing.T) {
	f := newTaskCloseFake(t)
	boardCompleteFixture(t, f, false, "MOCK_TOKEN")
	var err error
	out := captureStdout(t, func() { err = runCmd(t, Phase(), "complete", "x") })
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	f.mu.Lock()
	requests := f.requests
	f.mu.Unlock()
	if requests != 0 {
		t.Errorf("board sync off, yet complete sent %d board request(s)", requests)
	}
	if strings.Contains(out, "board") {
		t.Errorf("board sync off, yet complete printed a board line:\n%s", out)
	}
}

// TestCompleteRerunIsReadOnly: re-running complete over a finished board only
// reads — no write request, no chore commit, board.json byte-identical.
func TestCompleteRerunIsReadOnly(t *testing.T) {
	f := newTaskCloseFake(t)
	dir := boardCompleteFixture(t, f, true, "MOCK_TOKEN")
	if err := runCmd(t, Phase(), "complete", "x"); err != nil {
		t.Fatalf("complete: %v", err)
	}
	writes := 0
	f.srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writes++
		}
		f.handle(w, r)
	})
	head := mustGit(t, dir, "rev-parse", "HEAD")
	boardPath := filepath.Join(dir, ".dross", "board.json")
	before := mustRead(t, boardPath)

	if err := runCmd(t, Phase(), "complete", "x"); err != nil {
		t.Fatalf("re-running complete: %v", err)
	}
	if writes != 0 {
		t.Errorf("a re-run sent %d write request(s) to the board", writes)
	}
	if got := mustGit(t, dir, "rev-parse", "HEAD"); got != head {
		t.Errorf("a re-run committed: HEAD %s → %s", head, got)
	}
	if mustRead(t, boardPath) != before {
		t.Error("a re-run changed board.json")
	}
}

// TestCompleteUnreachableBoardNamesRetry: a board dross cannot even open — its
// token variable unset — fails complete only after the completion is written.
func TestCompleteUnreachableBoardNamesRetry(t *testing.T) {
	f := newTaskCloseFake(t)
	dir := boardCompleteFixture(t, f, true, "DROSS_TEST_UNSET_BOARD_TOKEN")
	err := runCmd(t, Phase(), "complete", "x")
	if err == nil || !strings.Contains(err.Error(), "dross issue phase finalize x") {
		t.Fatalf("err = %v, want the finalize retry named", err)
	}
	s, lerr := state.Load(filepath.Join(dir, ".dross", state.File))
	if lerr != nil || !hasAction(s, "completed x") {
		t.Errorf("the completion record was not written before the board was opened (load err %v)", lerr)
	}
}

// TestIssuePhaseFinalizeGuards: with board sync off the verb does nothing and
// sends nothing; with it on, it refuses to run off the phase's recorded base.
func TestIssuePhaseFinalizeGuards(t *testing.T) {
	f := newTaskCloseFake(t)
	dir := boardCompleteFixture(t, f, true, "MOCK_TOKEN")
	if err := runCmd(t, Phase(), "complete", "x"); err != nil {
		t.Fatalf("complete: %v", err)
	}

	mustGit(t, dir, "switch", "-q", "-c", "elsewhere")
	err := runCmd(t, Issue(), "phase", "finalize", "x")
	if err == nil || !strings.Contains(err.Error(), "HEAD is on elsewhere") {
		t.Errorf("off the recorded base: err = %v, want a refusal naming the branch", err)
	}

	mustRunSet(t, "board.enabled", "false")
	f.mu.Lock()
	f.requests = 0
	f.mu.Unlock()
	if err := runCmd(t, Issue(), "phase", "finalize", "x"); err != nil {
		t.Errorf("board off: err = %v, want a silent exit 0", err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.requests != 0 {
		t.Errorf("board off, yet finalize sent %d request(s)", f.requests)
	}
}

// TestFinalizeBoardRoutesThroughChorePR: on a protected base, the board.json
// the finalizer writes goes out through the chore-PR path — into the open
// completion-record chore PR, not a second one — and main is never pushed.
func TestFinalizeBoardRoutesThroughChorePR(t *testing.T) {
	f := newTaskCloseFake(t)
	dir, origin := setupMilestoneRepo(t)
	setProtectRemote(t, dir, "github", "")
	t.Setenv("MOCK_TOKEN", "secret")
	allowBoardHost(t, dir, f)
	pointBoardAt(t, f, true, "MOCK_TOKEN")
	writeSpec(t, dir, "p", "[phase]\n  id = \"p\"\n  title = \"P\"\n")
	mustWrite(t, filepath.Join(dir, ".dross", "phases", "p", "changes.json"), `{"phase":"p","status":"complete","tasks":{}}`)
	mustGit(t, dir, "add", ".dross")
	mustGit(t, dir, "commit", "-q", "-m", "phase p complete")
	mustGit(t, dir, "push", "-q", "-u", "origin", "main")
	choreCommit(t, dir, "completion-record.md") // already out in an open chore PR

	log := protectedOrigin(t, origin, false)
	stubBranchRules(t, protectedRules(t))
	s := &choreSeams{open: &ship.OpenResult{Number: 40, URL: "https://github.com/o/r/pull/40"}, autoRes: ship.AutoMergeResult{AutoEnabled: true}}
	stubChoreSeams(t, s)

	if err := finalizeBoard(dir, "p", "main"); err != nil {
		t.Fatalf("finalizeBoard: %v", err)
	}
	if len(s.opened) != 0 {
		t.Errorf("opened %d new chore PR(s), want the open one reused", len(s.opened))
	}
	if got := baseUpdates(t, log); len(got) != 0 {
		t.Errorf("main was offered directly: %v", got)
	}
	if out := mustGit(t, dir, "status", "--porcelain"); out != "" {
		t.Errorf("the tree is dirty after finalizing:\n%s", out)
	}
	if got := originRef(t, origin, "refs/heads/dross-chores/main"); got != mustGit(t, dir, "rev-parse", "main") {
		t.Errorf("dross-chores/main = %q, want local main's tip carrying board.json", got)
	}
	phaseCard, _ := phaseCards(t, f, "p")
	if phaseCard == "" {
		t.Fatal("no phase card was created")
	}
	wantCardTerminal(t, f, phaseCard, "complete")
}

// TestCompleteBoardHTTPErrorKeepsCompletion: a tracker answering 500 to the
// close is a failure like any other — the completion stands and the retry is
// named.
func TestCompleteBoardHTTPErrorKeepsCompletion(t *testing.T) {
	f := newTaskCloseFake(t)
	dir := boardCompleteFixture(t, f, true, "MOCK_TOKEN")
	f.srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPatch {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		f.handle(w, r)
	})
	err := runCmd(t, Phase(), "complete", "x")
	if err == nil || !strings.Contains(err.Error(), "dross issue phase finalize x") {
		t.Fatalf("err = %v, want the finalize retry named", err)
	}
	s, lerr := state.Load(filepath.Join(dir, ".dross", state.File))
	if lerr != nil || !hasAction(s, "completed x") {
		t.Errorf("the completion record is missing after a board 500 (load err %v)", lerr)
	}
}

// TestIssuePhaseFinalizeTakesBase: a phase with no recorded base — completed
// with --base — is finalized by naming the base the same way.
func TestIssuePhaseFinalizeTakesBase(t *testing.T) {
	f := newTaskCloseFake(t)
	dir := boardCompleteFixture(t, f, true, "MOCK_TOKEN")
	if err := runCmd(t, Phase(), "complete", "x"); err != nil {
		t.Fatalf("complete: %v", err)
	}
	// Drop the recorded base: the shape a --base completion leaves behind.
	cpath := filepath.Join(dir, ".dross", "phases", "x", "changes.json")
	mustWrite(t, cpath, strings.Replace(mustRead(t, cpath), `"base": "main"`, `"base": ""`, 1))
	mustGit(t, dir, "commit", "-q", "-am", "test: no recorded base")

	if err := runCmd(t, Issue(), "phase", "finalize", "x"); err == nil {
		t.Fatal("finalize with no recorded base and no --base succeeded")
	}
	if err := runCmd(t, Issue(), "phase", "finalize", "x", "--base", "main"); err != nil {
		t.Fatalf("finalize --base main: %v", err)
	}
}

// phaseCompleteEvents counts the phase_complete outcome events in telemetry.
func phaseCompleteEvents(t *testing.T) int {
	t.Helper()
	evs, err := telemetry.Load(telemetryPath())
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	n := 0
	for _, e := range evs {
		if e.Kind == "outcome" && e.Command == "phase_complete" {
			n++
		}
	}
	return n
}
