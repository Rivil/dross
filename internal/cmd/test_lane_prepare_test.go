package cmd

// The run-site half of lane prepare: where each lane's bootstrap line spawns,
// in what order, and on which transport.
//
// Every assertion here is about the RECORDED spawns rather than about a return
// value, because the whole claim is about sequence and locality — "it ran" is
// satisfied by an implementation that ran it in the wrong place, at the wrong
// time, or twice.

import (
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/project"
)

// preparedGoLane is the single-lane shape the ordering assertions need.
const preparedGoLane = `[[runtime.test_lane]]
name = "go"
match = ["internal/**", "main.go"]
command = "go test -count=1 ./..."
prepare = "make build"`

// preparedTwoLanes is the interleaving shape: two lanes, each with its own
// bootstrap, so "per lane" can be told apart from "batched up front".
const preparedTwoLanes = `[[runtime.test_lane]]
name = "go"
match = ["internal/**", "main.go"]
command = "go test -count=1 ./..."
prepare = "make build"

[[runtime.test_lane]]
name = "docs"
match = ["docs/", "README.md"]
command = "markdownlint docs"
prepare = "npm ci"`

// remoteLaneFixture is a granted host plus the given lane blocks, with every
// declared lane consented to as declared — prepare included.
func remoteLaneFixture(t *testing.T, lanes string) string {
	t.Helper()
	root := grantedTestFixture(t, "go test ./...")
	appendLanes(t, filepath.Dir(root), lanes)
	grantEveryLane(t)
	return root
}

// grantEveryLane consents to each declared lane's full consent line. It is not
// grantAllLanes: that one grants Fingerprint(lane.Command), which is a lane's
// consent line only while the lane declares no prepare.
func grantEveryLane(t *testing.T) {
	t.Helper()
	root, err := FindRoot()
	if err != nil {
		t.Fatal(err)
	}
	p, err := project.Load(filepath.Join(root, project.File))
	if err != nil {
		t.Fatal(err)
	}
	for _, lane := range p.Runtime.TestLane {
		if err := GrantLaneConsent(root, lane.Name, laneConsentLine(lane)); err != nil {
			t.Fatalf("grant lane %s: %v", lane.Name, err)
		}
	}
}

// remoteScripts is the ssh-carried scripts in spawn order, with the rsync legs
// dropped — the sequence a reader of the transcript would see run on the host.
func remoteScripts(rec *remoteRecorder) []string {
	rec.mu.Lock()
	defer rec.mu.Unlock()
	var out []string
	for i, argv := range rec.argvs {
		if len(argv) > 0 && strings.Contains(argv[0], "ssh") {
			out = append(out, rec.scripts[i])
		}
	}
	return out
}

// TestRemotePrepareRunsAfterTheSyncAndBeforeTheCommand pins the whole remote
// sequence by INDEX, not by presence.
//
// The sync has to come first or the prepare bootstraps a tree that is about to
// be overwritten; the command has to come last or the suite runs against a host
// that was never prepared. Both failures produce a run that looks like it
// worked.
func TestRemotePrepareRunsAfterTheSyncAndBeforeTheCommand(t *testing.T) {
	remoteLaneFixture(t, preparedGoLane)
	rem := installRemoteRecorder(t, nil)
	local := installSpawnRecorder(t, nil)

	if err := runCmd(t, Test(), "--files", "internal/a.go"); err != nil {
		t.Fatalf("dross test --files: %v", err)
	}
	if n := local.count(); n != 0 {
		t.Errorf("%d command(s) ran locally despite a granted host", n)
	}

	bins := rem.bins()
	if len(bins) != 3 {
		t.Fatalf("spawned %v remotely, want exactly [rsync ssh ssh]", bins)
	}
	if !strings.Contains(bins[0], "rsync") {
		t.Errorf("the first remote spawn is %q, want the tree sync — a prepare hoisted above it bootstraps a tree about to be replaced", bins[0])
	}
	scripts := remoteScripts(rem)
	if len(scripts) != 2 {
		t.Fatalf("ran %d ssh leg(s), want the prepare and the command", len(scripts))
	}
	if !strings.Contains(scripts[0], "make build") {
		t.Errorf("the first ssh leg is not the prepare: %q", scripts[0])
	}
	if !strings.Contains(scripts[1], "go test -count=1 ./...") {
		t.Errorf("the second ssh leg is not the command: %q", scripts[1])
	}
}

// TestPreparesInterleavePerLane: the sequence is [prep-go, go, prep-docs,
// docs], never [prep-go, prep-docs, go, docs].
//
// Batching every prepare up front is the natural optimisation and it is wrong:
// a lane's bootstrap would then run while an unrelated lane's suite is still
// mutating the tree it prepared, and a failed bootstrap would be discovered
// with nothing yet attributable to the lane that declared it.
func TestPreparesInterleavePerLane(t *testing.T) {
	filesFixture(t, preparedTwoLanes)
	grantEveryLane(t)
	rec := installSpawnRecorder(t, nil)

	if err := runCmd(t, Test(), "--files", "internal/a.go", "--files", "docs/x.md"); err != nil {
		t.Fatalf("dross test --files: %v", err)
	}
	want := []string{"make build", "go test -count=1 ./...", "npm ci", "markdownlint docs"}
	if len(rec.lines) != len(want) {
		t.Fatalf("spawned %v, want %v", rec.lines, want)
	}
	for i, w := range want {
		if rec.lines[i] != w {
			t.Errorf("spawn[%d] = %q, want %q — the sequence is %v", i, rec.lines[i], w, rec.lines)
		}
	}
}

// TestIdenticalPreparesAreNotDeduplicated is the locked prepare_scope decision.
//
// Idempotence is the declared contract of a prepare, so the repeat is the no-op
// the user promised. A dedup cache would make a lane's spawn set depend on
// which neighbours happened to match this file set — so the same lane would
// bootstrap or not depending on what else was edited, which is precisely the
// coupling the per-lane consent key was designed to avoid.
func TestIdenticalPreparesAreNotDeduplicated(t *testing.T) {
	filesFixture(t, `[[runtime.test_lane]]
name = "go"
match = ["internal/**"]
command = "go test -count=1 ./..."
prepare = "make build"

[[runtime.test_lane]]
name = "docs"
match = ["docs/"]
command = "markdownlint docs"
prepare = "make build"`)
	grantEveryLane(t)
	rec := installSpawnRecorder(t, nil)

	if err := runCmd(t, Test(), "--files", "internal/a.go", "--files", "docs/x.md"); err != nil {
		t.Fatalf("dross test --files: %v", err)
	}
	n := 0
	for _, line := range rec.lines {
		if line == "make build" {
			n++
		}
	}
	if n != 2 {
		t.Errorf("the shared prepare ran %d time(s), want 2 — one per lane: %v", n, rec.lines)
	}
}

// TestPrepareLinePrecedesTheLaneHeaderAndItsOutput asserts the transcript by
// BYTE OFFSET, the same way TestLaneHeaderPrecedesLaneOutput does.
//
// Presence is not the claim. A transcript that named the bootstrap after the
// suite it bootstrapped cannot be read as a sequence, and the reader who has to
// work out what ran first is exactly the reader this line exists for.
func TestPrepareLinePrecedesTheLaneHeaderAndItsOutput(t *testing.T) {
	filesFixture(t, preparedGoLane)
	grantEveryLane(t)

	// A sentinel where the COMMAND's output would go, and nothing for the
	// prepare: what is under test is where the lane's own output starts.
	orig := spawnLocal
	t.Cleanup(func() { spawnLocal = orig })
	spawnLocal = func(_, line string, stdout io.Writer, _ io.Writer) error {
		if strings.HasPrefix(line, "go test") {
			_, _ = io.WriteString(stdout, "LANE-OUTPUT-SENTINEL\n")
		}
		return nil
	}

	var out string
	if err := runCmdCapturing(t, &out, Test(), "--files", "internal/a.go"); err != nil {
		t.Fatal(err)
	}
	prepare := strings.Index(out, "lane go prepare: make build")
	header := strings.Index(out, "lane go: go test -count=1 ./...")
	sentinel := strings.Index(out, "LANE-OUTPUT-SENTINEL")
	for name, idx := range map[string]int{"prepare line": prepare, "lane header": header, "lane output": sentinel} {
		if idx < 0 {
			t.Fatalf("the transcript has no %s:\n%s", name, out)
		}
	}
	if !(prepare < header && header < sentinel) {
		t.Errorf("transcript out of order (prepare=%d header=%d output=%d):\n%s", prepare, header, sentinel, out)
	}
}

// TestPrepareRunsOnBothTransports is c-5: the same lane produces the same
// prepare invocation under --local as it does on a granted host.
//
// Both halves matter. A prepare wired only into the remote leg fails the local
// half, and one rebuilt per transport — a line assembled twice rather than
// carried — fails the equality even while both halves "ran a prepare".
func TestPrepareRunsOnBothTransports(t *testing.T) {
	var remoteLine string
	t.Run("granted host", func(t *testing.T) {
		remoteLaneFixture(t, preparedGoLane)
		rem := installRemoteRecorder(t, nil)
		local := installSpawnRecorder(t, nil)

		if err := runCmd(t, Test(), "--files", "internal/a.go"); err != nil {
			t.Fatal(err)
		}
		if local.count() != 0 {
			t.Errorf("a granted run spawned %d command(s) locally", local.count())
		}
		scripts := remoteScripts(rem)
		if len(scripts) != 2 {
			t.Fatalf("ran %d ssh leg(s), want the prepare and the command", len(scripts))
		}
		remoteLine = scripts[0]
	})

	t.Run("--local against the same fixture", func(t *testing.T) {
		remoteLaneFixture(t, preparedGoLane)
		rem := installRemoteRecorder(t, nil)
		local := installSpawnRecorder(t, nil)

		if err := runCmd(t, Test(), "--local", "--files", "internal/a.go"); err != nil {
			t.Fatal(err)
		}
		if n := len(rem.bins()); n != 0 {
			t.Errorf("--local reached the host %d time(s)", n)
		}
		want := []string{"make build", "go test -count=1 ./..."}
		if len(local.lines) != 2 || local.lines[0] != want[0] || local.lines[1] != want[1] {
			t.Fatalf("--local spawned %v, want %v", local.lines, want)
		}
		if remoteLine != "" && !strings.Contains(remoteLine, local.lines[0]) {
			t.Errorf("the prepare differs by transport: remote script %q does not carry the local line %q", remoteLine, local.lines[0])
		}
	})
}

// TestSkippedLanesNeverPrepare is the locked prepare_selector_miss decision,
// on both routes a lane can be skipped before it spawns.
//
// No spawn, no bootstrap. Preparing for a lane that will not run spends cold
// host time on nothing and puts a line in the transcript for a run that never
// happened — which is worse than useless, because a reader takes it as evidence
// the lane ran.
func TestSkippedLanesNeverPrepare(t *testing.T) {
	t.Run("selector filtered to nothing", func(t *testing.T) {
		// The lane declares a selector, and internal/a.go is not on disk in
		// the fixture, so every path that selected it is gone.
		filesFixture(t, `[[runtime.test_lane]]
name = "go"
match = ["internal/**"]
command = "go test -count=1"
prepare = "make build"
selector = "path"`)
		grantEveryLane(t)
		rec := installSpawnRecorder(t, nil)

		err := runCmd(t, Test(), "--files", "internal/a.go")
		if err == nil {
			t.Fatal("a run that measured nothing reported success")
		}
		if rec.count() != 0 {
			t.Errorf("a lane that never spawned still bootstrapped: %v", rec.lines)
		}
	})

	t.Run("consent refused", func(t *testing.T) {
		filesFixture(t, preparedTwoLanes)
		grantLane(t, "go") // docs stays ungranted
		rec := installSpawnRecorder(t, nil)

		err := runCmd(t, Test(), "--files", "internal/a.go", "--files", "docs/x.md")
		if err == nil {
			t.Fatal("a run with an ungranted lane reported success")
		}
		for _, line := range rec.lines {
			if line == "npm ci" || line == "markdownlint docs" {
				t.Errorf("the refused lane reached the runner: %v", rec.lines)
			}
		}
		if len(rec.lines) != 2 {
			t.Errorf("spawned %v, want only the granted lane's prepare and command", rec.lines)
		}
	})
}

// TestPrepareArgfenceRefusesBeforeAnyLaneRuns: `sh` reads options before -c and
// honours no end-of-options token, so a prepare beginning with a dash would be
// taken as a shell option. It goes through the SAME up-front sweep the command
// does.
//
// Two lanes, with the malformed prepare on the SECOND: a fence moved inside the
// run loop passes a single-lane test while leaving the earlier lane already
// run — and the point of a fence is that nothing ran before it.
func TestPrepareArgfenceRefusesBeforeAnyLaneRuns(t *testing.T) {
	filesFixture(t, `[[runtime.test_lane]]
name = "first"
match = ["internal/**"]
command = "go test -count=1 ./..."

[[runtime.test_lane]]
name = "go"
match = ["docs/"]
command = "markdownlint docs"
prepare = "-x make build"`)
	grantEveryLane(t)
	rec := installSpawnRecorder(t, nil)

	err := runCmd(t, Test(), "--files", "internal/a.go", "--files", "docs/x.md")
	if err == nil {
		t.Fatal("a prepare beginning with a dash was accepted")
	}
	if !strings.Contains(err.Error(), "runtime.test_lane[go]") {
		t.Errorf("the fence refusal does not name the lane key: %v", err)
	}
	if rec.count() != 0 {
		t.Errorf("the fence let %d command(s) run before refusing: %v", rec.count(), rec.lines)
	}
}

// TestLaneWithNoPrepareSpawnsExactlyOnce is c-1's "a lane declaring none spawns
// exactly what it spawns today", asserted on the COUNT on both seams.
//
// An unconditional spawn that turns an absent prepare into `sh -c ""` satisfies
// every other contract in this phase and fails only here: it costs a process
// per lane per run, and it puts a line in the transcript for a bootstrap the
// user never declared.
func TestLaneWithNoPrepareSpawnsExactlyOnce(t *testing.T) {
	t.Run("local", func(t *testing.T) {
		filesFixture(t, goAndDocsLanes)
		grantEveryLane(t)
		rec := installSpawnRecorder(t, nil)

		if err := runCmd(t, Test(), "--files", "internal/a.go"); err != nil {
			t.Fatal(err)
		}
		if len(rec.lines) != 1 || rec.lines[0] != goCmd {
			t.Errorf("spawned %v, want exactly [%q]", rec.lines, goCmd)
		}
	})

	t.Run("remote", func(t *testing.T) {
		remoteLaneFixture(t, goAndDocsLanes)
		rem := installRemoteRecorder(t, nil)

		if err := runCmd(t, Test(), "--files", "internal/a.go"); err != nil {
			t.Fatal(err)
		}
		scripts := remoteScripts(rem)
		if len(scripts) != 1 {
			t.Fatalf("ran %d ssh leg(s), want exactly the command: %v", len(scripts), scripts)
		}
		if !strings.Contains(scripts[0], goCmd) {
			t.Errorf("the single ssh leg is not the lane's command: %q", scripts[0])
		}
	})
}

// --- t-5: a failed prepare fails its own lane and spares the rest ---

// TestFailedPrepareSkipsItsOwnLaneAndSparesTheRest is c-3's core claim, with
// both halves asserted against the same recorder.
//
// A `return` instead of a `continue` kills the run and fails the goCmd assert;
// a missing skip runs the suite the bootstrap was supposed to prepare and fails
// the docsCmd assert. Only one implementation satisfies both.
func TestFailedPrepareSkipsItsOwnLaneAndSparesTheRest(t *testing.T) {
	filesFixture(t, preparedTwoLanes)
	grantEveryLane(t)
	rec := installPrepareFailure(t, "npm ci")

	err := runCmd(t, Test(), "--files", "internal/a.go", "--files", "docs/x.md")
	if err == nil {
		t.Fatal("a failed prepare reported success")
	}
	if got := ExitCode(err); got != exitPrepareFailed {
		t.Errorf("exit = %d, want %d — a bootstrap that failed measured nothing about the code", got, exitPrepareFailed)
	}
	for _, line := range rec.lines {
		if line == "markdownlint docs" {
			t.Errorf("the lane whose prepare failed still ran its suite: %v", rec.lines)
		}
	}
	var ranGo bool
	for _, line := range rec.lines {
		if line == goCmd {
			ranGo = true
		}
	}
	if !ranGo {
		t.Errorf("one lane's failed prepare stopped an unrelated lane: %v", rec.lines)
	}
	if !strings.Contains(err.Error(), "docs") || !strings.Contains(err.Error(), "npm ci") {
		t.Errorf("the failure names neither the lane nor the line: %v", err)
	}
	if strings.Contains(err.Error(), `test lane "docs" failed`) {
		t.Errorf("a failed bootstrap reads as a failed suite: %v", err)
	}
}

// installPrepareFailure records every local spawn and fails exactly the named
// line, so a test can make ONE lane's bootstrap fail while everything else
// behaves normally.
func installPrepareFailure(t *testing.T, failing string) *spawnRecorder {
	t.Helper()
	rec := &spawnRecorder{}
	orig := spawnLocal
	t.Cleanup(func() { spawnLocal = orig })
	spawnLocal = func(dir, line string, stdout, stderr io.Writer) error {
		if line == failing {
			rec.mu.Lock()
			rec.lines = append(rec.lines, line)
			rec.mu.Unlock()
			return fakeExit{code: 1}
		}
		return rec.spawn(dir, line, stdout, stderr)
	}
	return rec
}

// TestPrepareFailureOutranksARedSuite proves the result was FOLDED through
// worseOutcome rather than left to whichever lane happened to run last.
//
// With the go lane red and the docs lane's bootstrap broken, an implementation
// that returned the last lane's error reports 1 — and the user goes looking for
// the bug in a lane that was never even bootstrapped.
func TestPrepareFailureOutranksARedSuite(t *testing.T) {
	filesFixture(t, preparedTwoLanes)
	grantEveryLane(t)

	orig := spawnLocal
	t.Cleanup(func() { spawnLocal = orig })
	spawnLocal = func(_, line string, _, _ io.Writer) error {
		if line == goCmd || line == "npm ci" {
			return fakeExit{code: 1}
		}
		return nil
	}

	err := runCmd(t, Test(), "--files", "internal/a.go", "--files", "docs/x.md")
	if err == nil {
		t.Fatal("a red lane beside a broken bootstrap reported success")
	}
	if got := ExitCode(err); got != exitPrepareFailed {
		t.Errorf("exit = %d, want %d — a red suite must not mask a lane that never ran", got, exitPrepareFailed)
	}
}

// TestEveryPrepareFailedIsNotNothingMeasured: prepare failures are counted
// APART from selector misses.
//
// Folded in with the misses, `misses == len(runnable)` fires and the run
// reports exitNothingMeasured — which ranks LAST, so a run where nothing was
// bootstrapped would be reported as one that merely collected no tests. The
// two send the reader to different files.
func TestEveryPrepareFailedIsNotNothingMeasured(t *testing.T) {
	filesFixture(t, preparedTwoLanes)
	grantEveryLane(t)

	orig := spawnLocal
	t.Cleanup(func() { spawnLocal = orig })
	spawnLocal = func(_, line string, _, _ io.Writer) error {
		if line == "make build" || line == "npm ci" {
			return fakeExit{code: 1}
		}
		return nil
	}

	err := runCmd(t, Test(), "--files", "internal/a.go", "--files", "docs/x.md")
	if err == nil {
		t.Fatal("a run where every bootstrap failed reported success")
	}
	if got := ExitCode(err); got != exitPrepareFailed {
		t.Errorf("exit = %d, want %d, never %d (nothing measured)", got, exitPrepareFailed, exitNothingMeasured)
	}
}

// TestPrepareFailureIsClassifiedOnBothTransports: runOneLane's local arm
// hard-codes exitSuiteFailed and the remote arm takes its code from
// remoteFailure, which spends exitSuiteFailed on a non-zero command. A re-tag
// wired into only one of them ships a prepare failure still reporting 1 on the
// other — which is the collision exit 7 was added to prevent.
func TestPrepareFailureIsClassifiedOnBothTransports(t *testing.T) {
	t.Run("local", func(t *testing.T) {
		filesFixture(t, preparedGoLane)
		grantEveryLane(t)
		installPrepareFailure(t, "make build")

		err := runCmd(t, Test(), "--local", "--files", "internal/a.go")
		if got := ExitCode(err); got != exitPrepareFailed {
			t.Errorf("exit = %d, want %d — the local arm still reports a bootstrap failure as a red suite", got, exitPrepareFailed)
		}
	})

	t.Run("over ssh", func(t *testing.T) {
		remoteLaneFixture(t, preparedGoLane)
		rem := installRemoteRecorder(t, nil)
		orig := spawnRemote
		t.Cleanup(func() { spawnRemote = orig })
		spawnRemote = func(argv []string, script string, stdout, stderr io.Writer) error {
			if err := rem.spawn(argv, script, stdout, stderr); err != nil {
				return err
			}
			if strings.Contains(script, "make build") {
				return fakeExit{code: 1}
			}
			return nil
		}

		err := runCmd(t, Test(), "--files", "internal/a.go")
		if got := ExitCode(err); got != exitPrepareFailed {
			t.Fatalf("exit = %d, want %d — the remote arm reports a bootstrap failure as a red suite", got, exitPrepareFailed)
		}
		// And the suite it was preparing never reached the host.
		for _, script := range remoteScripts(rem) {
			if strings.Contains(script, "go test -count=1 ./...") {
				t.Errorf("the lane's tests ran on a host whose bootstrap failed: %v", remoteScripts(rem))
			}
		}
	})
}

// TestTransportFailureDuringAPrepareKeepsItsOwnCode: the re-tag must not
// swallow the transport family.
//
// An unreachable host and an incomplete transfer are facts about the RUN, not
// about the prepare. Relabelled as a bootstrap failure they would send the
// reader to edit a `make build` line that is perfectly fine, while the machine
// that is actually down goes unmentioned.
func TestTransportFailureDuringAPrepareKeepsItsOwnCode(t *testing.T) {
	t.Run("ssh 255 stays exit 3", func(t *testing.T) {
		remoteLaneFixture(t, preparedGoLane)
		orig := spawnRemote
		t.Cleanup(func() { spawnRemote = orig })
		spawnRemote = func(argv []string, _ string, _, _ io.Writer) error {
			if len(argv) > 0 && strings.Contains(argv[0], "ssh") {
				return fakeExit{code: 255}
			}
			return nil
		}

		err := runCmd(t, Test(), "--files", "internal/a.go")
		if got := ExitCode(err); got != exitTransport {
			t.Errorf("exit = %d, want %d (transport) — a host that died is not a broken bootstrap", got, exitTransport)
		}
	})

	// The sync precedes every lane, so an incomplete transfer is reached
	// before any prepare exists to blame — which is exactly the property
	// worth pinning: whatever the re-tag does, it must not be able to reach
	// backwards over the one failure that invalidates the whole tree.
	t.Run("incomplete rsync stays exit 4", func(t *testing.T) {
		remoteLaneFixture(t, preparedGoLane)
		orig := spawnRemote
		t.Cleanup(func() { spawnRemote = orig })
		spawnRemote = func(argv []string, _ string, _, _ io.Writer) error {
			if len(argv) > 0 && strings.Contains(argv[0], "rsync") {
				return fakeExit{code: 23}
			}
			return nil
		}

		err := runCmd(t, Test(), "--files", "internal/a.go")
		if got := ExitCode(err); got != exitPartial {
			t.Errorf("exit = %d, want %d (partial) — an incomplete tree is not a broken bootstrap", got, exitPartial)
		}
	})
}
