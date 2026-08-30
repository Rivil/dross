package cmd

// Per-lane locality at the run site: which machine each lane actually reached,
// what the transcript said about it, and what the run exited with.
//
// laneLocality's own tests are about the decision; these are about the wiring —
// that the decision is applied per lane rather than per run, that a lane which
// never spawned is not laundered into a red suite, and that a run with nothing
// going remote does not pay for the transfer.

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/remote"
)

// installLaneLookPath replaces this machine's resolver: every binary resolves
// except the ones named.
//
// A fixture that stubs the spawn seam and resolves binaries for real is half a
// machine — it refuses a lane whose command it is simultaneously pretending to
// run — so filesFixture installs the permissive form and tests whose subject IS
// local absence name what is missing.
func installLaneLookPath(t *testing.T, absent ...string) {
	t.Helper()
	gone := map[string]bool{}
	for _, bin := range absent {
		gone[bin] = true
	}
	orig := laneLookPath
	t.Cleanup(func() { laneLookPath = orig })
	laneLookPath = func(bin string) (string, error) {
		if gone[bin] {
			return "", exec.ErrNotFound
		}
		return "/usr/bin/" + bin, nil
	}
}

// goAndWebLanes is the two-toolchain fixture every split test needs: one lane
// the host can run and one it cannot.
const goAndWebLanes = `[[runtime.test_lane]]
name = "go"
match = ["internal/**"]
command = "go test -count=1 ./..."

[[runtime.test_lane]]
name = "web"
match = ["web/**"]
command = "pnpm test"`

// TestEveryLaneFallingBackSkipsTheTransfer is c-4's cost half. A run where
// nothing is going to the host has no use for the tree there, and rsyncing the
// repo anyway is exactly the price the pre-sync probe was added to avoid.
func TestEveryLaneFallingBackSkipsTheTransfer(t *testing.T) {
	grantedLaneFixture(t, goAndWebLanes)
	installLaneLookPath(t)
	log := &runLog{}
	log.probeSeam(t, []string{"go", "pnpm"}, nil)
	log.spawnSeam(t)
	local := installSpawnRecorder(t, nil)

	if err := runCmd(t, Test(), "--files", "internal/a.go", "--files", "web/app.ts"); err != nil {
		t.Fatalf("dross test --files: %v", err)
	}
	if i := log.indexOf("rsync"); i >= 0 {
		t.Errorf("the tree was pushed to a host no lane was going to use: %v", log.events)
	}
	if local.count() != 2 {
		t.Errorf("%d lane(s) ran locally, want 2 — both fell back", local.count())
	}
}

// TestOneRemoteLaneStillGetsTheTransfer: the skip must be keyed on "nothing is
// going there", not on "something fell back". A remote lane running against a
// tree that was never pushed measures the previous run's code — a green about
// something else, which is worse than any wasted transfer.
func TestOneRemoteLaneStillGetsTheTransfer(t *testing.T) {
	grantedLaneFixture(t, goAndWebLanes)
	installLaneLookPath(t)
	log := &runLog{}
	log.probeSeam(t, []string{"pnpm"}, nil)
	log.spawnSeam(t)
	installSpawnRecorder(t, nil)

	if err := runCmd(t, Test(), "--files", "internal/a.go", "--files", "web/app.ts"); err != nil {
		t.Fatalf("dross test --files: %v", err)
	}
	n := 0
	for _, e := range log.events {
		if e == "rsync" {
			n++
		}
	}
	if n != 1 {
		t.Errorf("the tree was pushed %d time(s), want exactly 1: %v", n, log.events)
	}
}

// TestLocalityIsDecidedPerLane is c-3 at the run site: one invocation, one
// probe answer, two destinations. Both lanes report their own suite result.
func TestLocalityIsDecidedPerLane(t *testing.T) {
	grantedLaneFixture(t, goAndWebLanes)
	installLaneLookPath(t)
	log := &runLog{}
	log.probeSeam(t, []string{"pnpm"}, nil)
	log.spawnSeam(t)
	local := installSpawnRecorder(t, nil)

	if err := runCmd(t, Test(), "--files", "internal/a.go", "--files", "web/app.ts"); err != nil {
		t.Fatalf("dross test --files: %v", err)
	}
	if local.count() != 1 || local.lines[0] != "pnpm test" {
		t.Errorf("locally spawned %v, want only the web lane's line", local.lines)
	}
	ssh := 0
	for _, e := range log.events {
		if e == "ssh" {
			ssh++
		}
	}
	if ssh != 1 {
		t.Errorf("%d remote command(s) ran, want exactly 1 — the go lane's: %v", ssh, log.events)
	}
}

// TestFallbackLinePrecedesThatLanesOutput is c-2, asserted BY INDEX. A line
// that merely appears is satisfied by one printed after the lane's header and
// its bootstrap, at which point the transcript no longer reads as a sequence
// and the fallback looks like a consequence of the run rather than its cause.
func TestFallbackLinePrecedesThatLanesOutput(t *testing.T) {
	grantedLaneFixture(t, `[[runtime.test_lane]]
name = "web"
match = ["web/**"]
command = "pnpm test"
prepare = "pnpm install"`)
	// Re-granted through laneConsentLine: grantAllLanes fingerprints the
	// command alone, and one grant covers command AND prepare, so a lane with a
	// prepare comes out of that fixture STALE.
	grantLane(t, "web")
	installLaneLookPath(t)
	log := &runLog{}
	log.probeSeam(t, []string{"pnpm"}, nil)
	log.spawnSeam(t)
	installSpawnRecorder(t, nil)

	var out string
	if err := runCmdCapturing(t, &out, Test(), "--files", "web/app.ts"); err != nil {
		t.Fatalf("dross test --files: %v", err)
	}
	fallback := strings.Index(out, "lane web fallback:")
	prepare := strings.Index(out, "lane web prepare:")
	header := strings.Index(out, "lane web: pnpm test")
	if fallback < 0 {
		t.Fatalf("no fallback line in the transcript:\n%s", out)
	}
	if prepare < 0 || header < 0 {
		t.Fatalf("the lane did not run:\n%s", out)
	}
	if fallback > prepare || fallback > header {
		t.Errorf("the fallback line trails the lane's own output:\n%s", out)
	}
	if !strings.Contains(out, "helicon") || !strings.Contains(out, "pnpm") {
		t.Errorf("the fallback line does not name the host and the binary:\n%s", out)
	}
}

// TestFallenBackLaneReportsItsOwnResult is c-1. The whole point of the fallback
// is that the lane produces a verdict about the code: a red LOCAL suite must
// arrive as exit 1 naming that lane, not as a transport failure and not as a
// remote `command not found`.
func TestFallenBackLaneReportsItsOwnResult(t *testing.T) {
	grantedLaneFixture(t, goAndWebLanes)
	installLaneLookPath(t)
	log := &runLog{}
	log.probeSeam(t, []string{"pnpm"}, nil)
	log.spawnSeam(t)
	installSpawnRecorder(t, fakeExit{code: 1})

	var out string
	err := runCmdCapturing(t, &out, Test(), "--files", "web/app.ts")
	if err == nil {
		t.Fatal("a red local suite reported success")
	}
	if got := ExitCode(err); got != exitSuiteFailed {
		t.Errorf("exit = %d, want %d — the lane ran here and went red", got, exitSuiteFailed)
	}
	if !strings.Contains(err.Error(), "web") {
		t.Errorf("the failure does not name the lane: %v", err)
	}
	if strings.Contains(out, "command not found") {
		t.Errorf("the lane was still spawned on the host:\n%s", out)
	}
}

// TestNeitherHostLaneNeverSpawns is locked local_absence at the run site: the
// lane reaches no spawn seam at all, and the run exits 8 rather than the 1 a
// `pnpm: command not found` would have produced.
func TestNeitherHostLaneNeverSpawns(t *testing.T) {
	grantedLaneFixture(t, goAndWebLanes)
	installLaneLookPath(t, "pnpm")
	log := &runLog{}
	log.probeSeam(t, []string{"pnpm"}, nil)
	log.spawnSeam(t)
	local := installSpawnRecorder(t, nil)

	var out string
	err := runCmdCapturing(t, &out, Test(), "--files", "web/app.ts")
	if err == nil {
		t.Fatal("a lane no machine can run reported success")
	}
	if got := ExitCode(err); got != exitToolchainMissing {
		t.Errorf("exit = %d, want %d — a missing binary is not a red suite", got, exitToolchainMissing)
	}
	if !strings.Contains(err.Error(), "neither") {
		t.Errorf("the refusal does not say both hosts lack it: %v", err)
	}
	if local.count() != 0 {
		t.Errorf("the lane was spawned locally anyway: %v", local.lines)
	}
	for _, e := range log.events {
		if e == "ssh" {
			t.Errorf("the lane was spawned remotely anyway: %v", log.events)
		}
	}
	if strings.Contains(out, "selector miss") {
		t.Errorf("a lane that could not run was reported as collecting no tests:\n%s", out)
	}
}

// TestUngrantedRunStillRefusesAMissingBinary is c-9: no remote in play at all.
// A lane whose tool is absent here must take the toolchain code rather than
// spawn and come back as a red suite, while its neighbour runs exactly as it
// always did.
func TestUngrantedRunStillRefusesAMissingBinary(t *testing.T) {
	filesFixture(t, goAndWebLanes)
	grantAllLanes(t)
	installLaneLookPath(t, "pnpm")
	log := &runLog{}
	log.probeSeam(t, nil, nil)
	local := installSpawnRecorder(t, nil)

	err := runCmd(t, Test(), "--files", "internal/a.go", "--files", "web/app.ts")
	if err == nil {
		t.Fatal("a lane with no local binary reported success")
	}
	if got := ExitCode(err); got != exitToolchainMissing {
		t.Errorf("exit = %d, want %d", got, exitToolchainMissing)
	}
	if strings.Contains(err.Error(), "neither") {
		t.Errorf("a run with no remote claims two hosts: %v", err)
	}
	if local.count() != 1 || local.lines[0] != "go test -count=1 ./..." {
		t.Errorf("locally spawned %v, want only the go lane's line", local.lines)
	}
	if log.probes != 0 {
		t.Errorf("an ungranted run opened %d probe(s)", log.probes)
	}
}

// TestFallbackIsNeverSticky is c-6. Nothing is written, so a host that gains
// the binary goes back to running that lane remotely on the next invocation
// with no user action — asserted on the config bytes AND on the second run's
// destination, because either alone is satisfiable by the wrong implementation.
func TestFallbackIsNeverSticky(t *testing.T) {
	grantedLaneFixture(t, goAndWebLanes)
	installLaneLookPath(t)
	root, err := FindRoot()
	if err != nil {
		t.Fatal(err)
	}
	read := func(name string) string {
		b, rerr := os.ReadFile(filepath.Join(root, name))
		if rerr != nil {
			return ""
		}
		return string(b)
	}
	beforeProject, beforeLocal := read("project.toml"), read("local.toml")

	first := &runLog{}
	first.probeSeam(t, []string{"pnpm"}, nil)
	first.spawnSeam(t)
	installSpawnRecorder(t, nil)
	if err := runCmd(t, Test(), "--files", "web/app.ts"); err != nil {
		t.Fatalf("first run: %v", err)
	}
	if got := read("project.toml"); got != beforeProject {
		t.Errorf("a fallback rewrote project.toml:\n%s", got)
	}
	if got := read("local.toml"); got != beforeLocal {
		t.Errorf("a fallback rewrote local.toml:\n%s", got)
	}

	// Second run, same repo, host now has pnpm. Nothing is done in between.
	second := &runLog{}
	second.probeSeam(t, nil, nil)
	second.spawnSeam(t)
	local := installSpawnRecorder(t, nil)
	var out string
	if err := runCmdCapturing(t, &out, Test(), "--files", "web/app.ts"); err != nil {
		t.Fatalf("second run: %v", err)
	}
	if local.count() != 0 {
		t.Errorf("the lane stayed local after the host gained the binary: %v", local.lines)
	}
	if second.indexOf("ssh") < 0 {
		t.Errorf("the lane never reached the host on the second run: %v", second.events)
	}
	if strings.Contains(out, "fallback:") {
		t.Errorf("the second run still announced a fallback:\n%s", out)
	}
}

// TestUnreachableHostSpawnsEveryLaneLocally is the c-5 half t-2 could only
// assert on a return value: the whole run comes home, the transport line is
// unchanged, and NOT ONE lane prints a toolchain line for a host that never
// answered.
func TestUnreachableHostSpawnsEveryLaneLocally(t *testing.T) {
	grantedLaneFixture(t, goAndWebLanes)
	installLaneLookPath(t)
	log := &runLog{}
	log.probeSeam(t, nil, remoteTransportErr())
	log.spawnSeam(t)
	local := installSpawnRecorder(t, nil)

	var out string
	if err := runCmdCapturing(t, &out, Test(), "--files", "internal/a.go", "--files", "web/app.ts"); err != nil {
		t.Fatalf("an unreachable host must fall back, not fail: %v", err)
	}
	if local.count() != 2 {
		t.Errorf("%d lane(s) ran locally, want 2 — the whole run comes home", local.count())
	}
	if !strings.Contains(out, "could not reach helicon") {
		t.Errorf("the transport fallback lost its wording:\n%s", out)
	}
	if strings.Contains(out, "fallback:") {
		t.Errorf("a lane blamed a toolchain on a host that never answered:\n%s", out)
	}
	if log.indexOf("rsync") >= 0 {
		t.Errorf("the tree was pushed to a host that could not be reached: %v", log.events)
	}
}

// remoteTransportErr is what the probe returns for a host that never answered.
func remoteTransportErr() error {
	return &transportErr{}
}

type transportErr struct{}

func (e *transportErr) Error() string { return "dial tcp: connection refused" }
func (e *transportErr) Unwrap() error { return remote.ErrTransport }

// interface guards: the seams these tests install must keep matching the
// production ones, or a signature change would leave the tests compiling
// against a seam nothing uses.
var (
	_ func(string, string, io.Writer, io.Writer) error = spawnLocal
	_ func(string) (string, error)                     = laneLookPath
)

// TestTheInstallOfferReachesTheTranscript: the offer is only worth anything if
// it lands in the output the user is actually reading when the fallback is
// paid for. laneFallbackLine's own tests prove the string; this proves the
// string survives the run site — a decision assembled correctly and never
// printed is c-1 unmet.
func TestTheInstallOfferReachesTheTranscript(t *testing.T) {
	grantedLaneFixture(t, goAndWebLanes)
	installLaneLookPath(t)
	log := &runLog{}
	log.probeSeam(t, []string{"pnpm"}, nil)
	log.spawnSeam(t)
	installSpawnRecorder(t, nil)

	var out string
	if err := runCmdCapturing(t, &out, Test(), "--files", "web/app.ts"); err != nil {
		t.Fatalf("dross test --files: %v", err)
	}
	if !strings.Contains(out, "dross test lane install web") {
		t.Errorf("the fallback offered no remedy in the transcript:\n%s", out)
	}
	// The lane that did NOT fall back must not be offered an install: an offer
	// attached to a lane that ran fine reads as a problem the user has to go
	// and fix.
	if strings.Contains(out, "dross test lane install go") {
		t.Errorf("a lane that did not fall back was offered an install:\n%s", out)
	}
}
