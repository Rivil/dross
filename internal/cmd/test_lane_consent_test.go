package cmd

import (
	"errors"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/project"
)

// grantLane consents to one lane's currently-declared command.
func grantLane(t *testing.T, name string) {
	t.Helper()
	root, err := FindRoot()
	if err != nil {
		t.Fatal(err)
	}
	p, err := project.Load(filepath.Join(root, project.File))
	if err != nil {
		t.Fatal(err)
	}
	lane, err := findLane(p, name)
	if err != nil {
		t.Fatal(err)
	}
	if err := GrantLaneConsent(root, lane.Name, lane.Command); err != nil {
		t.Fatal(err)
	}
}

const goCmd = "go test -count=1 ./..."
const docsCmd = "markdownlint docs"

// TestFilesRunsOnlyMatchedLanes is c-3 stated as an absence: the docs lane's
// command must appear in NONE of the recorded lines. Asserting only that the go
// lane ran would be satisfied by a loop that ran everything.
func TestFilesRunsOnlyMatchedLanes(t *testing.T) {
	filesFixture(t, goAndDocsLanes)
	grantAllLanes(t)
	rec := installSpawnRecorder(t, nil)

	if err := runCmd(t, Test(), "--files", "internal/cmd/test.go"); err != nil {
		t.Fatalf("dross test --files: %v", err)
	}
	if rec.count() != 1 {
		t.Fatalf("want exactly 1 spawn, got %d: %v", rec.count(), rec.lines)
	}
	if rec.lines[0] != goCmd {
		t.Errorf("spawned %q, want the go lane's line byte-for-byte", rec.lines[0])
	}
	for _, line := range rec.lines {
		if strings.Contains(line, docsCmd) {
			t.Errorf("the docs lane's command was spawned for a Go-only file set: %q", line)
		}
	}
}

// TestLaneLineIsByteIdentical: the fingerprint this lane's grant checks covers
// the configured string exactly. A run that appended the --files paths as a
// selector would be approving one command and executing another — the same
// mismatch the whole consent binding exists to prevent.
func TestLaneLineIsByteIdentical(t *testing.T) {
	filesFixture(t, goAndDocsLanes)
	grantAllLanes(t)
	rec := installSpawnRecorder(t, nil)

	if err := runCmd(t, Test(), "--files", "internal/cmd/test.go", "--files", "main.go"); err != nil {
		t.Fatal(err)
	}
	if rec.count() != 1 || rec.lines[0] != goCmd {
		t.Fatalf("spawned %v, want exactly [%q]", rec.lines, goCmd)
	}
}

// TestLaneHeaderPrecedesLaneOutput is c-4: the header has to come BEFORE the
// lane's own output, or a transcript cannot attribute a result to the runner
// that produced it — which is the one thing the header is for.
func TestLaneHeaderPrecedesLaneOutput(t *testing.T) {
	filesFixture(t, goAndDocsLanes)
	grantAllLanes(t)

	// A spawn seam that writes a sentinel where the lane's output would go.
	orig := spawnLocal
	t.Cleanup(func() { spawnLocal = orig })
	spawnLocal = func(_, _ string, stdout io.Writer, _ io.Writer) error {
		_, _ = io.WriteString(stdout, "LANE-OUTPUT-SENTINEL\n")
		return nil
	}

	var out string
	if err := runCmdCapturing(t, &out, Test(), "--files", "internal/cmd/test.go"); err != nil {
		t.Fatal(err)
	}
	header := strings.Index(out, "lane go: "+goCmd)
	sentinel := strings.Index(out, "LANE-OUTPUT-SENTINEL")
	if header < 0 {
		t.Fatalf("no lane header in the transcript:\n%s", out)
	}
	if sentinel < 0 {
		t.Fatalf("the lane's output is missing:\n%s", out)
	}
	if header >= sentinel {
		t.Errorf("the header trails the lane's output — the transcript cannot attribute the result:\n%s", out)
	}
}

// TestUngrantedLaneRefusesOnlyItself is the locked lane_consent decision at the
// run site: one ungranted lane must not block the lanes that ARE granted, and
// must not run either. The exit code says a lane was refused, not that the code
// is broken.
func TestUngrantedLaneRefusesOnlyItself(t *testing.T) {
	filesFixture(t, goAndDocsLanes)
	grantLane(t, "go")
	rec := installSpawnRecorder(t, nil)

	var out string
	err := runCmdCapturing(t, &out, Test(), "--files", "internal/a.go", "--files", "docs/x.md")
	if err == nil {
		t.Fatal("a run with an ungranted lane reported success")
	}
	if got := ExitCode(err); got != exitLaneRefused {
		t.Errorf("exit = %d, want %d (lane refused)", got, exitLaneRefused)
	}
	if rec.count() != 1 || rec.lines[0] != goCmd {
		t.Fatalf("spawned %v, want only the granted lane's line", rec.lines)
	}
	if !strings.Contains(out, "docs") {
		t.Errorf("the refusal does not name the ungranted lane:\n%s", out)
	}
}

// TestStaleLaneRefusalIsDistinct: a lane whose command was REWRITTEN since it
// was trusted is the case the binding exists for. Reported as a routine first
// run, it reads as "you have not got round to trusting this yet" rather than
// "this line is not the line you approved".
func TestStaleLaneRefusalIsDistinct(t *testing.T) {
	filesFixture(t, goAndDocsLanes)
	root, err := FindRoot()
	if err != nil {
		t.Fatal(err)
	}
	// Granted for a command the lane no longer declares.
	if err := GrantLaneConsent(root, "go", "go test -race ./..."); err != nil {
		t.Fatal(err)
	}
	rec := installSpawnRecorder(t, nil)

	var out string
	rerr := runCmdCapturing(t, &out, Test(), "--files", "internal/a.go")
	if rerr == nil {
		t.Fatal("a stale lane ran")
	}
	if !strings.Contains(out, "CHANGED") {
		t.Errorf("the stale refusal is indistinguishable from a first run:\n%s", out)
	}
	if rec.count() != 0 {
		t.Errorf("the stale lane spawned %d command(s)", rec.count())
	}
}

// TestAllLanesRefusedIsNotGreen: a run where nothing was allowed to execute
// measured exactly as much as one that matched nothing. Exiting 0 there is the
// false-green the whole exit-code contract exists to prevent.
func TestAllLanesRefusedIsNotGreen(t *testing.T) {
	filesFixture(t, goAndDocsLanes)
	rec := installSpawnRecorder(t, nil)

	err := runCmd(t, Test(), "--files", "internal/a.go", "--files", "docs/x.md")
	if err == nil {
		t.Fatal("a run where every lane was refused reported success")
	}
	if got := ExitCode(err); got != exitLaneRefused {
		t.Errorf("exit = %d, want %d", got, exitLaneRefused)
	}
	if rec.count() != 0 {
		t.Errorf("a fully-refused run spawned %d command(s)", rec.count())
	}
}

// TestRedLaneFailsTheRunAndRunsTheRest is the locked multi_lane decision: every
// matched lane runs, and a red result anywhere fails the gate. Stopping at the
// first red would hide the second lane's state; letting the second lane's green
// win would hide the first lane's failure.
func TestRedLaneFailsTheRunAndRunsTheRest(t *testing.T) {
	filesFixture(t, goAndDocsLanes)
	grantAllLanes(t)

	// The first lane in declaration order goes red; the second is fine.
	var lines []string
	orig := spawnLocal
	t.Cleanup(func() { spawnLocal = orig })
	spawnLocal = func(_, line string, _, _ io.Writer) error {
		lines = append(lines, line)
		if line == goCmd {
			return errors.New("exit status 1")
		}
		return nil
	}

	// A file set hitting both lanes.
	err := runCmd(t, Test(), "--files", "internal/a.go", "--files", "docs/x.md")
	if err == nil {
		t.Fatal("a red lane did not fail the run")
	}
	if got := ExitCode(err); got != exitSuiteFailed {
		t.Errorf("exit = %d, want %d (red suite)", got, exitSuiteFailed)
	}
	if len(lines) != 2 {
		t.Fatalf("ran %v, want both lanes despite the first going red", lines)
	}
	if lines[0] != goCmd || lines[1] != docsCmd {
		t.Errorf("lanes ran out of declaration order: %v", lines)
	}
}

// TestRedBeatsRefusedInExitPrecedence: with a broken suite AND an ungranted
// lane, the run must report the broken suite. Reporting the consent problem
// would send the user to `dross trust --lane`, after which the run goes red for
// the first time and they have learnt nothing they could not have known now.
func TestRedBeatsRefusedInExitPrecedence(t *testing.T) {
	filesFixture(t, goAndDocsLanes)
	grantLane(t, "go") // docs stays ungranted
	installSpawnRecorder(t, errors.New("exit status 1"))

	err := runCmd(t, Test(), "--files", "internal/a.go", "--files", "docs/x.md")
	if err == nil {
		t.Fatal("a red lane alongside a refused one reported success")
	}
	if got := ExitCode(err); got != exitSuiteFailed {
		t.Errorf("exit = %d, want %d (red), not %d (refused)", got, exitSuiteFailed, exitLaneRefused)
	}
}

// TestExitPrecedenceIsTotal drives every co-occurring pair through one table
// rather than sampling three of them, so "worst outcome wins" is pinned as an
// order instead of inferred from examples.
//
// The order — transport > partial > prepare > red > refused > nothing-measured
// — is about how badly each outcome misleads a caller deciding whether to
// commit, not about how annoying it is.
func TestExitPrecedenceIsTotal(t *testing.T) {
	order := []int{exitTransport, exitPartial, exitPrepareFailed, exitSuiteFailed, exitLaneRefused, exitNothingMeasured}
	tagged := func(code int) error {
		return &ExitCodeError{Code: code, Err: errors.New("outcome")}
	}
	for i, worse := range order {
		// Success loses to every failure, in both argument positions.
		if got := ExitCode(worseOutcome(nil, tagged(worse))); got != worse {
			t.Errorf("worseOutcome(nil, %d) = %d", worse, got)
		}
		if got := ExitCode(worseOutcome(tagged(worse), nil)); got != worse {
			t.Errorf("worseOutcome(%d, nil) = %d", worse, got)
		}
		for _, weaker := range order[i+1:] {
			if got := ExitCode(worseOutcome(tagged(worse), tagged(weaker))); got != worse {
				t.Errorf("worseOutcome(%d, %d) = %d, want %d", worse, weaker, got, worse)
			}
			// Order of arrival must not change the verdict: lanes run in
			// declaration order, and a lane's position in project.toml is
			// not an argument about what the run means.
			if got := ExitCode(worseOutcome(tagged(weaker), tagged(worse))); got != worse {
				t.Errorf("worseOutcome(%d, %d) = %d, want %d", weaker, worse, got, worse)
			}
		}
	}
	if worseOutcome(nil, nil) != nil {
		t.Error("two successes must stay a success")
	}
}

// TestPrepareOutranksRedAndUnderranksPartial asserts the ranks THEMSELVES, not
// just the pairwise verdicts above.
//
// exitRank's unknown-code default is literally `return 3` — exitSuiteFailed's
// own rank. A code left out of the switch therefore does not rank low, it TIES
// with a red suite, and worseOutcome resolves a tie to whichever error arrived
// first. That is order-dependent and silent: the pairwise table can pass while
// the rank is only accidentally right. Reading the ranks catches the omission
// directly.
func TestPrepareOutranksRedAndUnderranksPartial(t *testing.T) {
	if exitRank(exitPrepareFailed) <= exitRank(exitSuiteFailed) {
		t.Errorf("rank(prepare)=%d must be strictly above rank(red)=%d — a bootstrap that failed measured nothing about the code",
			exitRank(exitPrepareFailed), exitRank(exitSuiteFailed))
	}
	if exitRank(exitPrepareFailed) >= exitRank(exitPartial) {
		t.Errorf("rank(prepare)=%d must be strictly below rank(partial)=%d — an incomplete tree invalidates every lane, a failed prepare only its own",
			exitRank(exitPrepareFailed), exitRank(exitPartial))
	}
}

// TestRedLaneWithNoPrepareStillExitsOne: exit 7 must be unreachable in a repo
// that declares no prepare. The new code is an addition to the taxonomy, not a
// re-tagging of the failures already in it — a red suite is still a red suite
// everywhere the feature is not in use, which is every repo written before
// this phase.
func TestRedLaneWithNoPrepareStillExitsOne(t *testing.T) {
	filesFixture(t, goAndDocsLanes)
	grantAllLanes(t)
	installSpawnRecorder(t, errors.New("exit status 1"))

	err := runCmd(t, Test(), "--files", "internal/a.go")
	if err == nil {
		t.Fatal("a red lane reported success")
	}
	if got := ExitCode(err); got != exitSuiteFailed {
		t.Errorf("exit = %d, want %d (red suite) — a repo declaring no prepare can never produce %d",
			got, exitSuiteFailed, exitPrepareFailed)
	}
}

// TestLaneArgfenceBlamesTheLane: `sh` reads options before -c and honours no
// end-of-options token, so a command line starting with a dash would be taken
// as a shell option. The refusal has to name the lane key, because sending the
// user to edit runtime.test_command — a line that may be perfectly fine — is a
// fix applied to the wrong place.
func TestLaneArgfenceBlamesTheLane(t *testing.T) {
	filesFixture(t, `[[runtime.test_lane]]
name = "go"
match = ["internal/**"]
command = "-x go test ./..."`)
	grantAllLanes(t)
	rec := installSpawnRecorder(t, nil)

	err := runCmd(t, Test(), "--files", "internal/a.go")
	if err == nil {
		t.Fatal("a lane command beginning with a dash was accepted")
	}
	if !strings.Contains(err.Error(), "runtime.test_lane[go]") {
		t.Errorf("the fence refusal does not name the lane key: %v", err)
	}
	if strings.Contains(err.Error(), "runtime.test_command") {
		t.Errorf("the fence refusal blames runtime.test_command: %v", err)
	}
	if rec.count() != 0 {
		t.Errorf("the fenced lane spawned %d command(s)", rec.count())
	}
}

// TestArgfenceRefusesBeforeAnyLaneRuns: the fence is checked for every matched
// lane up front. Checked inside the loop, a malformed lane declared second
// would be discovered with the first lane already executed — and the fence
// exists precisely to stop a line reaching a shell.
func TestArgfenceRefusesBeforeAnyLaneRuns(t *testing.T) {
	filesFixture(t, `[[runtime.test_lane]]
name = "go"
match = ["internal/**"]
command = "go test ./..."

[[runtime.test_lane]]
name = "bad"
match = ["internal/**"]
command = "-x true"`)
	grantAllLanes(t)
	rec := installSpawnRecorder(t, nil)

	if err := runCmd(t, Test(), "--files", "internal/a.go"); err == nil {
		t.Fatal("a malformed lane was accepted")
	}
	if rec.count() != 0 {
		t.Errorf("the earlier lane ran before the fence refused: %v", rec.lines)
	}
}

// TestLanesOnlyRepoRunsWithNoTestCommand is the repo shape lanes most exist to
// serve. With the whole-suite gate stacked on top of the per-lane grants, this
// repo would refuse every lane run at a gate guarding a command it never
// spawns — so the per-lane check REPLACES it on this path rather than adding
// to it.
func TestLanesOnlyRepoRunsWithNoTestCommand(t *testing.T) {
	dir := laneFixture(t) // no runtime.test_command at all
	appendLanes(t, dir, goAndDocsLanes)
	grantAllLanes(t)
	rec := installSpawnRecorder(t, nil)

	if err := runCmd(t, Test(), "--files", "internal/a.go"); err != nil {
		t.Fatalf("a lanes-only repo must run its granted lanes: %v", err)
	}
	if rec.count() != 1 || rec.lines[0] != goCmd {
		t.Errorf("spawned %v, want the go lane", rec.lines)
	}
}

// TestBareTestStillRequiresTheWholeSuiteGrant is the other side of the same
// coin: moving the gate off the lane path must not move it off the bare path,
// where runtime.test_command is still what gets spawned.
func TestBareTestStillRequiresTheWholeSuiteGrant(t *testing.T) {
	dir := laneFixture(t)
	mustRunSet(t, "runtime.test_command", "go test ./...")
	appendLanes(t, dir, goAndDocsLanes)
	grantAllLanes(t) // lanes granted, the whole suite is NOT
	rec := installSpawnRecorder(t, nil)

	if err := runCmd(t, Test()); err == nil {
		t.Fatal("a bare `dross test` ran without the whole-suite grant")
	}
	if rec.count() != 0 {
		t.Errorf("the refusal spawned %d command(s)", rec.count())
	}
}

// TestDoctorConsentIsLaneAware: after lanes gained their own grants, "no
// runtime.test_command, so the loop commands refuse" is false in exactly the
// repo shape lanes serve. A doctor that kept saying it would send that user to
// configure something that is not broken.
func TestDoctorConsentIsLaneAware(t *testing.T) {
	dir := laneFixture(t) // no runtime.test_command
	appendLanes(t, dir, goAndDocsLanes)
	grantAllLanes(t)

	var out string
	_ = runCmdCapturing(t, &out, Doctor())
	if strings.Contains(out, "the loop commands refuse") {
		t.Errorf("doctor still claims the loop refuses in a lanes-only repo:\n%s", out)
	}
	if !strings.Contains(out, "--files") {
		t.Errorf("doctor does not say the lanes are still runnable:\n%s", out)
	}

	// A repo with no lanes keeps the original advice: there really is nothing
	// to run there.
	laneFixture(t)
	var bare string
	_ = runCmdCapturing(t, &bare, Doctor())
	if !strings.Contains(bare, "the loop commands refuse") {
		t.Errorf("a lane-less repo lost the original advice:\n%s", bare)
	}
}

// TestMultiLaneSyncsOnce: the tree is pushed once for the whole run, not once
// per lane. Fused, the wall-clock cost of lanes would scale with the number of
// lanes rather than with the code they cover — and a re-sync between lanes
// could push edits made while an earlier lane was running, so two lanes in one
// run would measure two different trees.
func TestMultiLaneSyncsOnce(t *testing.T) {
	root := grantedTestFixture(t, "go test ./...")
	appendLanes(t, filepath.Dir(root), goAndDocsLanes)
	grantAllLanes(t)
	rem := installRemoteRecorder(t, nil)
	local := installSpawnRecorder(t, nil)

	if err := runCmd(t, Test(), "--files", "internal/a.go", "--files", "docs/x.md"); err != nil {
		t.Fatalf("dross test --files: %v", err)
	}
	if n := local.count(); n != 0 {
		t.Errorf("%d lane(s) ran locally despite a granted host", n)
	}
	var rsyncs, sshs int
	for _, bin := range rem.bins() {
		switch {
		case strings.Contains(bin, "rsync"):
			rsyncs++
		case strings.Contains(bin, "ssh"):
			sshs++
		}
	}
	if rsyncs != 1 {
		t.Errorf("synced %d time(s), want exactly 1 for the whole run", rsyncs)
	}
	if sshs != 2 {
		t.Errorf("ran %d lane(s) over ssh, want 2", sshs)
	}
}

// TestRemoteLaneTransportFailureExitsThree: a host that died mid-run measured
// nothing, and a lane run reported as a red suite would send the user hunting a
// bug in code that was never tested. The lane path must classify the failure
// the same way the whole-suite path does.
func TestRemoteLaneTransportFailureExitsThree(t *testing.T) {
	root := grantedTestFixture(t, "go test ./...")
	appendLanes(t, filepath.Dir(root), goAndDocsLanes)
	grantAllLanes(t)

	// The sync lands; the far-side run does not come back.
	orig := spawnRemote
	t.Cleanup(func() { spawnRemote = orig })
	spawnRemote = func(argv []string, _ string, _, _ io.Writer) error {
		if len(argv) > 0 && strings.Contains(argv[0], "ssh") {
			return errors.New("connection closed by remote host")
		}
		return nil
	}

	err := runCmd(t, Test(), "--files", "internal/a.go")
	if err == nil {
		t.Fatal("an unreachable host reported a passing lane")
	}
	if got := ExitCode(err); got != exitTransport {
		t.Errorf("exit = %d, want %d (transport) — never %d, which reads as a red suite", got, exitTransport, exitSuiteFailed)
	}
}
