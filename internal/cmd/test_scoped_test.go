package cmd

// The relate run of a vouching lane, end to end through Test(). Every assertion
// is on what spawned and on what the transcript said, because a relate run that
// reports the right exit from the wrong spawns is the false green this exists to
// prevent.

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Rivil/dross/internal/consent"
	"github.com/Rivil/dross/internal/gatestate"
	"github.com/Rivil/dross/internal/project"
	"github.com/Rivil/dross/internal/testlane"
	"github.com/Rivil/dross/internal/treefp"
)

// The two lines relateLane spawns for a run over src/a.ts that relate maps to
// src/a.test.ts.
const (
	relateOverA  = "node scripts/relate.mjs 'src/a.ts'"
	batchOfATest = "npx vitest run 'src/a.test.ts'"
	aToATest     = "src/a.ts\tsrc/a.test.ts\n"
)

// relateWideLane is relateLane matching build config and docs too, so a path the
// run has to set aside reaches the lane in the first place.
const relateWideLane = `[[runtime.test_lane]]
name = "web"
match = ["src/**", "docs/**", "package.json"]
command = "npx vitest run"
selector = "path"
vouch = true
relate = "node scripts/relate.mjs"`

// goNeighbourLane is a plain lane to run beside a vouching one.
const goNeighbourLane = `

[[runtime.test_lane]]
name = "go"
match = ["internal/**"]
command = "go test -count=1"
selector = "go-package"`

// goNeighbourLine is what goNeighbourLane spawns for internal/cmd/here.go.
const goNeighbourLine = "go test -count=1 './internal/cmd/...'"

// relateSeam answers the spawn seams as a vouching lane's tools would: a relate
// line prints stdout and stderr and returns err, and a batched line prints
// laneOut, calls onBatch and returns laneErr. Anything else but a prepare returns
// otherErr.
type relateSeam struct {
	stdout, stderr string
	err            error
	laneOut        string
	laneErr        error
	onBatch        func()
	otherErr       error
	// lines are the local spawns, and events the remote ones by leg, in order.
	lines  []string
	events []string
}

// answer plays one spawned line, in whichever leg it turned out to be.
func (s *relateSeam) answer(line string, stdout, stderr io.Writer) (string, error) {
	switch {
	case strings.Contains(line, "scripts/relate.mjs"):
		io.WriteString(stdout, s.stdout)
		io.WriteString(stderr, s.stderr)
		return "relate", s.err
	case strings.Contains(line, "npx vitest run"):
		io.WriteString(stdout, s.laneOut)
		if s.onBatch != nil {
			s.onBatch()
		}
		return "batch", s.laneErr
	case strings.Contains(line, "npm ci"):
		return "prepare", nil
	}
	return "other", s.otherErr
}

// install takes over the local spawn seam.
func (s *relateSeam) install(t *testing.T) *relateSeam {
	t.Helper()
	orig := spawnLocal
	t.Cleanup(func() { spawnLocal = orig })
	spawnLocal = func(_, line string, stdout, stderr io.Writer) error {
		s.lines = append(s.lines, line)
		_, err := s.answer(line, stdout, stderr)
		return err
	}
	return s
}

// installRemote takes over the remote spawn seam, naming each leg in events.
func (s *relateSeam) installRemote(t *testing.T) *relateSeam {
	t.Helper()
	orig := spawnRemote
	t.Cleanup(func() { spawnRemote = orig })
	spawnRemote = func(argv []string, script string, stdout, stderr io.Writer) error {
		if strings.Contains(argv[0], "rsync") {
			s.events = append(s.events, "sync")
			return nil
		}
		leg, err := s.answer(script, stdout, stderr)
		s.events = append(s.events, leg)
		return err
	}
	return s
}

// relateFixture is a repo whose lanes are all granted, with every named file on
// disk.
func relateFixture(t *testing.T, lanes string, files ...string) {
	t.Helper()
	filesFixture(t, lanes)
	grantLaneLines(t)
	for _, f := range files {
		touchFile(t, f)
	}
}

// relateHostFixture is relateFixture with a host granted.
func relateHostFixture(t *testing.T, lanes string, files ...string) {
	t.Helper()
	grantedLaneFixture(t, lanes)
	grantLaneLines(t)
	for _, f := range files {
		touchFile(t, f)
	}
}

// uncoveredLine is the transcript's one `uncovered:` line, or empty.
func uncoveredLine(t *testing.T, out string) string {
	t.Helper()
	var found []string
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(l, "uncovered:") {
			found = append(found, l)
		}
	}
	if len(found) > 1 {
		t.Fatalf("%d `uncovered:` lines, want at most one:\n%s", len(found), out)
	}
	return strings.Join(found, "")
}

// TestRelateOmissionRunsTheBatchAndNamesThePath: a path with no test costs the
// run nothing, and is still in the transcript. A run that went quiet about it
// would read as covering everything it was asked.
func TestRelateOmissionRunsTheBatchAndNamesThePath(t *testing.T) {
	relateFixture(t, relateLane, "src/a.ts", "src/b.svelte", "src/a.test.ts")
	seam := (&relateSeam{stdout: aToATest}).install(t)

	out, err := runTestCapturing(t, "--files", "src/a.ts", "--files", "src/b.svelte")
	if err != nil {
		t.Fatalf("dross test --files: %v", err)
	}
	want := []string{"node scripts/relate.mjs 'src/a.ts' 'src/b.svelte'", batchOfATest}
	if !reflect.DeepEqual(seam.lines, want) {
		t.Errorf("spawned %q, want %q", seam.lines, want)
	}
	if got := uncoveredLine(t, out); got != "uncovered: src/b.svelte" {
		t.Errorf("uncovered line = %q:\n%s", got, out)
	}
}

// TestManyPathsSpawnOneRelateAndOneBatch: three paths, one relate, one lane
// command. A test shared by two paths is on the line once.
func TestManyPathsSpawnOneRelateAndOneBatch(t *testing.T) {
	relateFixture(t, relateLane, "src/a.ts", "src/b.ts", "src/c.ts", "src/a.test.ts", "src/c.test.ts")
	seam := (&relateSeam{stdout: "src/a.ts\tsrc/a.test.ts\nsrc/b.ts\tsrc/a.test.ts\nsrc/c.ts\tsrc/c.test.ts\n"}).install(t)

	out, err := runTestCapturing(t, "--files", "src/a.ts", "--files", "src/b.ts", "--files", "src/c.ts")
	if err != nil {
		t.Fatalf("dross test --files: %v", err)
	}
	want := []string{
		"node scripts/relate.mjs 'src/a.ts' 'src/b.ts' 'src/c.ts'",
		"npx vitest run 'src/a.test.ts' 'src/c.test.ts'",
	}
	if !reflect.DeepEqual(seam.lines, want) {
		t.Errorf("spawned %q, want %q", seam.lines, want)
	}
	if got := uncoveredLine(t, out); got != "" {
		t.Errorf("every path has a test, and the transcript says %q", got)
	}
}

// TestNothingRelatedExitsFiveAndNeverEscalates is zero_test_scoped_run: the lane
// command with nothing appended is its whole suite, and test_command is the
// repo's, so either one spawning here is the surprise full run the decision
// forbids.
func TestNothingRelatedExitsFiveAndNeverEscalates(t *testing.T) {
	relateFixture(t, relateLane, "src/a.ts")
	seam := (&relateSeam{}).install(t)

	out, err := runTestCapturing(t, "--files", "src/a.ts")
	if ExitCode(err) != exitNothingMeasured {
		t.Fatalf("exit = %d (%v), want %d", ExitCode(err), err, exitNothingMeasured)
	}
	if want := []string{relateOverA}; !reflect.DeepEqual(seam.lines, want) {
		t.Errorf("spawned %q, want only relate", seam.lines)
	}
	if got := uncoveredLine(t, out); got != "uncovered: src/a.ts" {
		t.Errorf("uncovered line = %q:\n%s", got, out)
	}
	if !strings.Contains(out, `selector miss: lane "web"`) {
		t.Errorf("the transcript does not name the lane that related nothing:\n%s", out)
	}
}

// TestFailedRelateTakesThePrepareExit: relate's failures all mean its answer
// cannot be trusted, which says nothing about whether the paths have tests. So
// each is the prepare exit, the lane command stays unspawned, and the transcript
// holds no miss and no gap. The last two cases are the shape checks a miss would
// most easily swallow.
func TestFailedRelateTakesThePrepareExit(t *testing.T) {
	for _, tc := range []struct {
		name, stdout string
		err          error
		tail         string
	}{
		{name: "non-zero exit", stdout: "RELATE-TAIL\n", err: fakeExit{code: 2}, tail: "RELATE-TAIL"},
		{name: "malformed line", stdout: "RELATE-MALFORMED\n", tail: "RELATE-MALFORMED"},
		{name: "a test absent from the tree", stdout: "src/a.ts\tsrc/gone.test.ts\n", tail: "src/gone.test.ts"},
		{name: "a path nobody asked about", stdout: "src/z.ts\tsrc/a.test.ts\n", tail: "src/z.ts"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			relateFixture(t, relateLane, "src/a.ts", "src/z.ts", "src/a.test.ts")
			seam := (&relateSeam{stdout: tc.stdout, err: tc.err}).install(t)

			out, err := runTestCapturing(t, "--files", "src/a.ts")
			if ExitCode(err) != exitPrepareFailed {
				t.Fatalf("exit = %d (%v), want %d", ExitCode(err), err, exitPrepareFailed)
			}
			if want := []string{relateOverA}; !reflect.DeepEqual(seam.lines, want) {
				t.Errorf("spawned %q, want only relate", seam.lines)
			}
			if strings.Contains(out, "selector miss") || strings.Contains(out, "uncovered:") {
				t.Errorf("a failed relate reads as a miss:\n%s", out)
			}
			if !strings.Contains(err.Error(), "relate failed") || !strings.Contains(err.Error(), `"web"`) {
				t.Errorf("the failure does not name relate and the lane: %v", err)
			}
			if tc.err == nil && strings.Contains(err.Error(), tc.tail) {
				t.Errorf("the refusal carries relate's text: %v", err)
			}
			// The step shown is relate's own, whether its spawn failed or its
			// output did: without its tail the reader has only a line number.
			if fb := fallbackOf(t, out, exitPrepareFailed); !strings.Contains(fb, tc.tail) {
				t.Errorf("the summary does not show relate's tail:\n%s", fb)
			}
		})
	}
}

// TestFailedRelateLeavesItsNeighbourRunning: the lane is out and the run is not,
// and the failure still outranks the neighbour's green.
func TestFailedRelateLeavesItsNeighbourRunning(t *testing.T) {
	relateFixture(t, relateLane+goNeighbourLane, "src/a.ts", "internal/cmd/here.go")
	seam := (&relateSeam{err: fakeExit{code: 2}}).install(t)

	err := runCmd(t, Test(), "--files", "src/a.ts", "--files", "internal/cmd/here.go")
	if ExitCode(err) != exitPrepareFailed {
		t.Fatalf("exit = %d (%v), want %d", ExitCode(err), err, exitPrepareFailed)
	}
	if want := []string{relateOverA, goNeighbourLine}; !reflect.DeepEqual(seam.lines, want) {
		t.Errorf("spawned %q, want %q", seam.lines, want)
	}
}

// TestOnlyRelatesStdoutIsRead: neither the lane command's output nor relate's
// stderr can cover a path, however much either looks like an answer.
func TestOnlyRelatesStdoutIsRead(t *testing.T) {
	relateFixture(t, relateLane, "src/a.ts", "src/b.svelte", "src/a.test.ts", "src/b.test.ts")
	shaped := "dross-covered\tsrc/b.svelte\nsrc/b.svelte\tsrc/b.test.ts\n"
	seam := (&relateSeam{stdout: aToATest, stderr: shaped, laneOut: shaped}).install(t)

	out, err := runTestCapturing(t, "--files", "src/a.ts", "--files", "src/b.svelte")
	if err != nil {
		t.Fatalf("dross test --files: %v", err)
	}
	want := []string{"node scripts/relate.mjs 'src/a.ts' 'src/b.svelte'", batchOfATest}
	if !reflect.DeepEqual(seam.lines, want) {
		t.Errorf("spawned %q, want %q — output other than relate's stdout changed the run", seam.lines, want)
	}
	if got := uncoveredLine(t, out); got != "uncovered: src/b.svelte" {
		t.Errorf("uncovered line = %q — output other than relate's stdout covered a path:\n%s", got, out)
	}
}

// TestRemoteRelateFailuresKeepTheirFamily: ssh's own failure is the transport's
// on both legs and never a miss, with the batched leg declaring 255 as an
// empty_exit to prove it. Relate's own non-zero status is still relate's fault
// over ssh, which is the arm remoteFailure would otherwise call a red suite.
func TestRemoteRelateFailuresKeepTheirFamily(t *testing.T) {
	for _, tc := range []struct {
		name       string
		seam       relateSeam
		want       int
		wantEvents []string
	}{
		{"ssh fails on relate", relateSeam{stdout: aToATest, err: fakeExit{code: 255}}, exitTransport, []string{"sync", "relate"}},
		{"ssh fails on the batched line", relateSeam{stdout: aToATest, laneErr: fakeExit{code: 255}}, exitTransport, []string{"sync", "relate", "batch"}},
		{"relate exits non-zero on the host", relateSeam{stdout: aToATest, err: fakeExit{code: 2}}, exitPrepareFailed, []string{"sync", "relate"}},
		{"relate prints a malformed line on the host", relateSeam{stdout: "RELATE-MALFORMED\n"}, exitPrepareFailed, []string{"sync", "relate"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			relateHostFixture(t, relateLane+"\nempty_exit = [255]", "src/a.ts", "src/a.test.ts")
			seam := tc.seam
			seam.installRemote(t)
			local := installSpawnRecorder(t, nil)

			out, err := runTestCapturing(t, "--files", "src/a.ts")
			if ExitCode(err) != tc.want {
				t.Fatalf("exit = %d (%v), want %d", ExitCode(err), err, tc.want)
			}
			if !reflect.DeepEqual(seam.events, tc.wantEvents) {
				t.Errorf("legs = %v, want %v", seam.events, tc.wantEvents)
			}
			if strings.Contains(out, "selector miss") {
				t.Errorf("a failure read as a miss:\n%s", out)
			}
			if local.count() != 0 {
				t.Errorf("%d command(s) ran locally: %q", local.count(), local.lines)
			}
		})
	}
}

// TestNonVouchingLaneRunsTodaysLine: a lane that does not vouch spawns the line
// it always did, alone and beside a vouching lane, and a repo with no vouching
// lane has none of this in its transcript.
func TestNonVouchingLaneRunsTodaysLine(t *testing.T) {
	t.Run("alone", func(t *testing.T) {
		relateFixture(t, goAndDocsLanes, "internal/cmd/here.go")
		seam := (&relateSeam{}).install(t)

		out, err := runTestCapturing(t, "--files", "internal/cmd/here.go")
		if err != nil {
			t.Fatalf("dross test --files: %v", err)
		}
		if want := []string{"go test -count=1 ./..."}; !reflect.DeepEqual(seam.lines, want) {
			t.Errorf("spawned %q, want %q", seam.lines, want)
		}
		if strings.Contains(out, "uncovered:") || strings.Contains(out, "relate") {
			t.Errorf("a non-vouching run talks about relate:\n%s", out)
		}
	})

	t.Run("beside a vouching lane", func(t *testing.T) {
		relateFixture(t, relateLane+goNeighbourLane, "src/a.ts", "src/a.test.ts", "internal/cmd/here.go")
		seam := (&relateSeam{stdout: aToATest}).install(t)

		if err := runCmd(t, Test(), "--files", "src/a.ts", "--files", "internal/cmd/here.go"); err != nil {
			t.Fatalf("dross test --files: %v", err)
		}
		if want := []string{relateOverA, batchOfATest, goNeighbourLine}; !reflect.DeepEqual(seam.lines, want) {
			t.Errorf("spawned %q, want %q", seam.lines, want)
		}
	})
}

// TestRelateRunsBetweenThePrepareAndTheBatch pins the sequence by index on both
// transports. Relate reads the tree the prepare built and the sync pushed, so
// hoisted above either it answers about something else.
func TestRelateRunsBetweenThePrepareAndTheBatch(t *testing.T) {
	prepared := relateLane + "\nprepare = \"npm ci\""

	t.Run("local", func(t *testing.T) {
		relateFixture(t, prepared, "src/a.ts", "src/a.test.ts")
		seam := (&relateSeam{stdout: aToATest}).install(t)

		if err := runCmd(t, Test(), "--files", "src/a.ts"); err != nil {
			t.Fatalf("dross test --files: %v", err)
		}
		if want := []string{"npm ci", relateOverA, batchOfATest}; !reflect.DeepEqual(seam.lines, want) {
			t.Errorf("spawned %q, want %q", seam.lines, want)
		}
	})

	t.Run("on the host", func(t *testing.T) {
		relateHostFixture(t, prepared, "src/a.ts", "src/a.test.ts")
		seam := (&relateSeam{stdout: aToATest}).installRemote(t)
		local := installSpawnRecorder(t, nil)

		if err := runCmd(t, Test(), "--files", "src/a.ts"); err != nil {
			t.Fatalf("dross test --files: %v", err)
		}
		if want := []string{"sync", "prepare", "relate", "batch"}; !reflect.DeepEqual(seam.events, want) {
			t.Errorf("legs = %v, want %v", seam.events, want)
		}
		if local.count() != 0 {
			t.Errorf("%d command(s) ran locally despite a granted host: %q", local.count(), local.lines)
		}
	})
}

// TestRelateFollowsTheLaneToItsSite: a lane the host cannot run comes home, and
// relate comes with it. Relate left on the host would answer about a tree this
// run never pushed.
func TestRelateFollowsTheLaneToItsSite(t *testing.T) {
	relateHostFixture(t, relateLane, "src/a.ts", "src/a.test.ts")
	log := &runLog{}
	log.probeSeam(t, []string{"npx"}, nil)
	seam := (&relateSeam{stdout: aToATest}).installRemote(t).install(t)

	if err := runCmd(t, Test(), "--files", "src/a.ts"); err != nil {
		t.Fatalf("dross test --files: %v", err)
	}
	if want := []string{relateOverA, batchOfATest}; !reflect.DeepEqual(seam.lines, want) {
		t.Errorf("spawned %q locally, want %q", seam.lines, want)
	}
	if len(seam.events) != 0 {
		t.Errorf("the lane ran here and these legs reached the host: %v", seam.events)
	}
}

// TestRelateIsLoggedAndNeverSummarised: the summary is the batched line's alone,
// and the log still holds what relate said. Left in the parsers' view, relate's
// map is a segment no parser claims, and the run's summary falls back to a tail.
func TestRelateIsLoggedAndNeverSummarised(t *testing.T) {
	relateFixture(t, relateLane, "src/a.ts", "src/a.test.ts")
	(&relateSeam{
		stdout:  aToATest,
		stderr:  "--- FAIL: TestGhostOfRelate (0.00s)\n",
		laneOut: runnerOutput(t, "gotest/green.txt"),
	}).install(t)

	out, err := runTestCapturing(t, "--files", "src/a.ts")
	if err != nil {
		t.Fatalf("dross test --files: %v", err)
	}
	if !strings.Contains(out, "4 ok") {
		t.Errorf("the summary is not the batched line's:\n%s", out)
	}
	if strings.Contains(out, "not recognised") || strings.Contains(out, "TestGhostOfRelate") {
		t.Errorf("relate's output reached the summary:\n%s", out)
	}
	log := onlyLog(t)
	for _, want := range []string{"$ " + relateOverA, aToATest, "TestGhostOfRelate"} {
		if !strings.Contains(log, want) {
			t.Errorf("the run log lacks %q:\n%s", want, log)
		}
	}
}

// TestRelateMarkerClearsTheParseCallAlone: one marker in the file, on the parse
// call and naming the shape check. On the stream or its writer it would clear
// everything read from them. The second half is the marker's reason to exist:
// taken away, relate's stream escapes.
func TestRelateMarkerClearsTheParseCallAlone(t *testing.T) {
	src, err := os.ReadFile("test_scoped.go")
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(src), "\n")
	var at []int
	for i, l := range lines {
		if strings.Contains(l, taintClearedMarker) {
			at = append(at, i)
		}
	}
	if len(at) != 1 {
		t.Fatalf("test_scoped.go carries %d taint markers, want exactly 1", len(at))
	}
	if !strings.Contains(lines[at[0]], "shape check") {
		t.Errorf("the marker's prose does not name the shape check: %s", lines[at[0]])
	}
	if !strings.Contains(lines[at[0]+1], "testlane.ParseRelate(") {
		t.Errorf("the marker binds %q, want the parse call", lines[at[0]+1])
	}

	found := false
	for _, m := range taintMarkersIn(t, "internal/cmd") {
		if filepath.Base(m.file) != "test_scoped.go" {
			continue
		}
		found = true
		taint, _ := execTaintScan(viewWithoutComment(liveView(t), m.file, m.line))
		if len(taint) == 0 {
			t.Error("removing relate's marker leaves no finding: its stream no longer reaches the parse call")
		}
	}
	if !found {
		t.Error("the taint scan sees no marker in test_scoped.go")
	}
}

// TestUngrantedRelateSpawnsNothing: relate is a repo's own text, so it stands
// behind the lane's grant with the prepare and the command. An edit to relate
// alone stales the grant.
func TestUngrantedRelateSpawnsNothing(t *testing.T) {
	prepared := relateLane + "\nprepare = \"npm ci\""

	t.Run("untrusted", func(t *testing.T) {
		filesFixture(t, prepared)
		touchFile(t, "src/a.ts")
		seam := (&relateSeam{stdout: aToATest}).install(t)

		err := runCmd(t, Test(), "--files", "src/a.ts")
		if ExitCode(err) != exitLaneRefused {
			t.Fatalf("exit = %d (%v), want %d", ExitCode(err), err, exitLaneRefused)
		}
		if len(seam.lines) != 0 {
			t.Errorf("an ungranted lane spawned %q", seam.lines)
		}
	})

	t.Run("stale", func(t *testing.T) {
		relateFixture(t, prepared, "src/a.ts")
		root, err := FindRoot()
		if err != nil {
			t.Fatal(err)
		}
		file := filepath.Join(root, project.File)
		body, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		edited := strings.Replace(string(body), "node scripts/relate.mjs", "node scripts/elsewhere.mjs", 1)
		if edited == string(body) {
			t.Fatal("the fixture holds no relate line to edit")
		}
		if err := os.WriteFile(file, []byte(edited), 0o644); err != nil {
			t.Fatal(err)
		}
		seam := (&relateSeam{stdout: aToATest}).install(t)

		err = runCmd(t, Test(), "--files", "src/a.ts")
		if ExitCode(err) != exitLaneRefused {
			t.Fatalf("exit = %d (%v), want %d", ExitCode(err), err, exitLaneRefused)
		}
		if len(seam.lines) != 0 {
			t.Errorf("a stale lane spawned %q", seam.lines)
		}
	})
}

// TestBuildConfigNeverReachesRelate: the gate refuses a config path whatever ran,
// so it stays off relate's argv, while the rest of the run relates and runs. An
// exempt glob does not move it, because the fixed list beats exempt at the gate.
func TestBuildConfigNeverReachesRelate(t *testing.T) {
	relateFixture(t, relateWideLane, "src/a.ts", "src/vite.config.ts", "package.json", "src/a.test.ts")
	mustRunSet(t, "runtime.test_exempt", "*.json")
	seam := (&relateSeam{stdout: aToATest}).install(t)

	out, err := runTestCapturing(t, "--files", "src/a.ts", "--files", "src/vite.config.ts", "--files", "package.json")
	if err != nil {
		t.Fatalf("dross test --files: %v", err)
	}
	if want := []string{relateOverA, batchOfATest}; !reflect.DeepEqual(seam.lines, want) {
		t.Errorf("spawned %q, want %q", seam.lines, want)
	}
	if got := uncoveredLine(t, out); got != "uncovered: src/vite.config.ts package.json" {
		t.Errorf("uncovered line = %q:\n%s", got, out)
	}
}

// TestDeletedBuildConfigStillPrintsAsUncovered: the gate refuses a deleted config
// file too, so the transcript names it beside the run that went ahead.
func TestDeletedBuildConfigStillPrintsAsUncovered(t *testing.T) {
	relateFixture(t, relateWideLane, "src/a.ts", "src/a.test.ts")
	seam := (&relateSeam{stdout: aToATest}).install(t)

	out, err := runTestCapturing(t, "--files", "src/a.ts", "--files", "package.json")
	if err != nil {
		t.Fatalf("dross test --files: %v", err)
	}
	if want := []string{relateOverA, batchOfATest}; !reflect.DeepEqual(seam.lines, want) {
		t.Errorf("spawned %q, want %q", seam.lines, want)
	}
	if got := uncoveredLine(t, out); got != "uncovered: package.json" {
		t.Errorf("uncovered line = %q:\n%s", got, out)
	}
}

// TestDeletedPathsBesideOnlyConfigPayForNothing: a deleted source file leaves
// nothing to ask about, so the config beside it still stops the lane at the door.
// With no config in hand the same lane is a deleted-paths miss, which stays in
// the run and is told as one.
func TestDeletedPathsBesideOnlyConfigPayForNothing(t *testing.T) {
	t.Run("config beside a deleted path", func(t *testing.T) {
		relateFixture(t, relateWideLane, "package.json")
		seam := (&relateSeam{}).install(t)

		out, err := runTestCapturing(t, "--files", "package.json", "--files", "src/gone.ts")
		if ExitCode(err) != exitNothingMeasured {
			t.Fatalf("exit = %d (%v), want %d", ExitCode(err), err, exitNothingMeasured)
		}
		if len(seam.lines) != 0 {
			t.Errorf("spawned %q, want nothing", seam.lines)
		}
		if !strings.Contains(out, "build config is all that is left") {
			t.Errorf("the transcript does not say why the lane did not run:\n%s", out)
		}
	})

	t.Run("deleted paths alone", func(t *testing.T) {
		relateFixture(t, relateWideLane)
		seam := (&relateSeam{}).install(t)

		out, err := runTestCapturing(t, "--files", "src/gone.ts")
		if ExitCode(err) != exitNothingMeasured {
			t.Fatalf("exit = %d (%v), want %d", ExitCode(err), err, exitNothingMeasured)
		}
		if len(seam.lines) != 0 {
			t.Errorf("spawned %q, want nothing", seam.lines)
		}
		if !strings.Contains(out, "every path matching it is gone") {
			t.Errorf("the deleted-paths miss went untold:\n%s", out)
		}
	})
}

// TestRelateCannotAnswerForWithheldConfig: a path off the relate line is a path
// nobody asked about, so relate naming it is the same refusal as any other.
func TestRelateCannotAnswerForWithheldConfig(t *testing.T) {
	relateFixture(t, relateWideLane, "src/a.ts", "package.json", "src/a.test.ts")
	seam := (&relateSeam{stdout: aToATest + "package.json\tsrc/a.test.ts\n"}).install(t)

	err := runCmd(t, Test(), "--files", "src/a.ts", "--files", "package.json")
	if ExitCode(err) != exitPrepareFailed {
		t.Fatalf("exit = %d (%v), want %d", ExitCode(err), err, exitPrepareFailed)
	}
	if want := []string{relateOverA}; !reflect.DeepEqual(seam.lines, want) {
		t.Errorf("spawned %q, want only relate", seam.lines)
	}
}

// TestConfigOnlyLanePaysForNothing: with nothing else selecting it the lane has
// no question to ask, so no prepare, no relate, no probe and no sync, and the run
// measured nothing.
func TestConfigOnlyLanePaysForNothing(t *testing.T) {
	relateHostFixture(t, relateWideLane+"\nprepare = \"npm ci\"", "package.json")
	log := &runLog{}
	log.probeSeam(t, nil, nil)
	seam := (&relateSeam{}).installRemote(t).install(t)

	out, err := runTestCapturing(t, "--files", "package.json")
	if ExitCode(err) != exitNothingMeasured {
		t.Fatalf("exit = %d (%v), want %d", ExitCode(err), err, exitNothingMeasured)
	}
	if len(seam.lines) != 0 || len(seam.events) != 0 || log.probes != 0 {
		t.Errorf("a config-only run paid: local %q, host %v, %d probe(s)", seam.lines, seam.events, log.probes)
	}
	if got := uncoveredLine(t, out); got != "uncovered: package.json" {
		t.Errorf("uncovered line = %q:\n%s", got, out)
	}
	if !strings.Contains(out, `selector miss: lane "web"`) {
		t.Errorf("the transcript does not say why the lane did not run:\n%s", out)
	}
}

// TestConfigOnlyLaneBesideAnother: beside a lane that measured something it is
// information, it outranks no refusal, and it cannot make a run that related
// nothing read as anything else.
func TestConfigOnlyLaneBesideAnother(t *testing.T) {
	t.Run("a green neighbour stands", func(t *testing.T) {
		relateFixture(t, relateWideLane+goNeighbourLane, "package.json", "internal/cmd/here.go")
		seam := (&relateSeam{}).install(t)

		out, err := runTestCapturing(t, "--files", "package.json", "--files", "internal/cmd/here.go")
		if err != nil {
			t.Fatalf("dross test --files: %v", err)
		}
		if want := []string{goNeighbourLine}; !reflect.DeepEqual(seam.lines, want) {
			t.Errorf("spawned %q, want %q", seam.lines, want)
		}
		if got := uncoveredLine(t, out); got != "uncovered: package.json" {
			t.Errorf("uncovered line = %q:\n%s", got, out)
		}
	})

	t.Run("a refused neighbour keeps its exit", func(t *testing.T) {
		filesFixture(t, relateWideLane+goNeighbourLane)
		root, err := FindRoot()
		if err != nil {
			t.Fatal(err)
		}
		if err := GrantLaneConsent(root, "web", vouchingWebGrant(t)); err != nil {
			t.Fatal(err)
		}
		touchFile(t, "package.json")
		touchFile(t, "internal/cmd/here.go")
		seam := (&relateSeam{}).install(t)

		err = runCmd(t, Test(), "--files", "package.json", "--files", "internal/cmd/here.go")
		if ExitCode(err) != exitLaneRefused {
			t.Fatalf("exit = %d (%v), want %d", ExitCode(err), err, exitLaneRefused)
		}
		if len(seam.lines) != 0 {
			t.Errorf("spawned %q, want nothing", seam.lines)
		}
	})

	t.Run("a neighbour that also related nothing is nothing measured", func(t *testing.T) {
		relateFixture(t, relateWideLane+`

[[runtime.test_lane]]
name = "app"
match = ["app/**"]
command = "npx vitest run --project app"
selector = "path"
vouch = true
relate = "node scripts/relate.mjs --project app"`, "package.json", "app/x.ts")
		seam := (&relateSeam{}).install(t)

		err := runCmd(t, Test(), "--files", "package.json", "--files", "app/x.ts")
		if ExitCode(err) != exitNothingMeasured {
			t.Fatalf("exit = %d (%v), want %d", ExitCode(err), err, exitNothingMeasured)
		}
		if want := []string{"node scripts/relate.mjs --project app 'app/x.ts'"}; !reflect.DeepEqual(seam.lines, want) {
			t.Errorf("spawned %q, want %q", seam.lines, want)
		}
	})
}

// vouchingWebGrant is the consent line of the fixture's "web" lane.
func vouchingWebGrant(t *testing.T) string {
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
		if lane.Name == "web" {
			return laneConsentLine(lane)
		}
	}
	t.Fatal("the fixture declares no web lane")
	return ""
}

// TestExemptPathIsNoGap: relate has no test for a doc and never will, and a path
// the repo declared as needing none is not a gap. The omission beside it still
// is, and an exempt-only run says nothing at all.
func TestExemptPathIsNoGap(t *testing.T) {
	t.Run("beside a real gap", func(t *testing.T) {
		relateFixture(t, relateWideLane, "src/a.ts", "src/b.ts", "docs/x.md", "src/a.test.ts")
		mustRunSet(t, "runtime.test_exempt", "docs/**")
		(&relateSeam{stdout: aToATest}).install(t)

		out, err := runTestCapturing(t, "--files", "src/a.ts", "--files", "src/b.ts", "--files", "docs/x.md")
		if err != nil {
			t.Fatalf("dross test --files: %v", err)
		}
		if got := uncoveredLine(t, out); got != "uncovered: src/b.ts" {
			t.Errorf("uncovered line = %q:\n%s", got, out)
		}
	})

	t.Run("alone", func(t *testing.T) {
		relateFixture(t, relateWideLane, "docs/x.md")
		mustRunSet(t, "runtime.test_exempt", "docs/**")
		(&relateSeam{}).install(t)

		out, err := runTestCapturing(t, "--files", "docs/x.md")
		if ExitCode(err) != exitNothingMeasured {
			t.Fatalf("exit = %d (%v), want %d", ExitCode(err), err, exitNothingMeasured)
		}
		if got := uncoveredLine(t, out); got != "" {
			t.Errorf("an exempt path reads as a gap: %q", got)
		}
	})
}

// TestPathCoveredByASecondLaneIsNoGap: one lane's omission is that lane's, and
// the transcript speaks for the run. Named per lane, this path would read as
// uncovered with its test having just passed.
func TestPathCoveredByASecondLaneIsNoGap(t *testing.T) {
	relateFixture(t, relateLane+`

[[runtime.test_lane]]
name = "shared"
match = ["src/**"]
command = "npx vitest run --project shared"
selector = "path"
vouch = true
relate = "node scripts/relate.mjs --project shared"`, "src/a.ts", "src/a.test.ts")
	orig := spawnLocal
	t.Cleanup(func() { spawnLocal = orig })
	spawnLocal = func(_, line string, stdout, _ io.Writer) error {
		if strings.HasPrefix(line, "node scripts/relate.mjs --project shared") {
			io.WriteString(stdout, aToATest)
		}
		return nil
	}

	out, err := runTestCapturing(t, "--files", "src/a.ts")
	if err != nil {
		t.Fatalf("dross test --files: %v", err)
	}
	if got := uncoveredLine(t, out); got != "" {
		t.Errorf("a path the second lane covered reads as a gap: %q", got)
	}
}

// TestNothingRelatedBesideAGreenLaneIsAMiss: a lane with no test for its paths is
// no verdict on the code, so it must not fail a neighbour's green. It is the
// path-level rule at lane level, and the transcript still names the paths.
func TestNothingRelatedBesideAGreenLaneIsAMiss(t *testing.T) {
	relateFixture(t, relateLane+goNeighbourLane, "src/a.ts", "internal/cmd/here.go")
	seam := (&relateSeam{}).install(t)

	out, err := runTestCapturing(t, "--files", "src/a.ts", "--files", "internal/cmd/here.go")
	if err != nil {
		t.Fatalf("dross test --files: %v", err)
	}
	if want := []string{relateOverA, goNeighbourLine}; !reflect.DeepEqual(seam.lines, want) {
		t.Errorf("spawned %q, want %q", seam.lines, want)
	}
	if got := uncoveredLine(t, out); got != "uncovered: src/a.ts" {
		t.Errorf("uncovered line = %q:\n%s", got, out)
	}
}

// TestBatchedLineFailsLikeAnyLaneLine: past relate the batched line is the lane's
// own, so a red is a red, and a declared empty_exit is a miss.
func TestBatchedLineFailsLikeAnyLaneLine(t *testing.T) {
	t.Run("red", func(t *testing.T) {
		relateFixture(t, relateLane, "src/a.ts", "src/a.test.ts")
		(&relateSeam{stdout: aToATest, laneErr: fakeExit{code: 1}}).install(t)

		err := runCmd(t, Test(), "--files", "src/a.ts")
		if ExitCode(err) != exitSuiteFailed {
			t.Fatalf("exit = %d (%v), want %d", ExitCode(err), err, exitSuiteFailed)
		}
		if !strings.Contains(err.Error(), `"web"`) {
			t.Errorf("the red does not name its lane: %v", err)
		}
	})

	t.Run("a declared empty_exit", func(t *testing.T) {
		relateFixture(t, relateLane+"\nempty_exit = [4]", "src/a.ts", "src/a.test.ts")
		(&relateSeam{stdout: aToATest, laneErr: fakeExit{code: 4}}).install(t)

		out, err := runTestCapturing(t, "--files", "src/a.ts")
		if ExitCode(err) != exitNothingMeasured {
			t.Fatalf("exit = %d (%v), want %d", ExitCode(err), err, exitNothingMeasured)
		}
		if !strings.Contains(out, "src/a.test.ts (exit 4)") {
			t.Errorf("the miss does not name the tests it looked in:\n%s", out)
		}
	})
}

// lintLane is a plain lane over the same paths relateLane matches, so one path
// can sit under a lane that vouches and a lane that does not.
const lintLane = `

[[runtime.test_lane]]
name = "lint"
match = ["src/**"]
command = "npx eslint"`

// appLane is a second vouching lane, over paths of its own.
const appLane = `

[[runtime.test_lane]]
name = "app"
match = ["app/**"]
command = "npx vitest run --project app"
selector = "path"
vouch = true
relate = "node scripts/relate.mjs --project app"`

// scopedRepo is relateFixture in a git work tree, with the recorder's notes
// captured. The tree a run fingerprints is the one this leaves.
func scopedRepo(t *testing.T, lanes string, files ...string) (string, *strings.Builder) {
	t.Helper()
	relateFixture(t, lanes, files...)
	dir := asGitRepo(t)
	return dir, captureGreen(t)
}

// scopedRecordOf is the record on disk, or nil.
func scopedRecordOf(t *testing.T, dir string) *gatestate.Scoped {
	t.Helper()
	s, err := gatestate.LoadScoped(dir)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// scopedBytes is the record file itself, nil when there is none. A run that
// leaves a record leaves it byte for byte, which a loaded record cannot show.
func scopedBytes(t *testing.T, dir string) []byte {
	t.Helper()
	b, err := os.ReadFile(gatestate.Path(dir, gatestate.ScopedFile))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// seedScoped writes a record on tree, covering a path no fixture run names, so
// finding it after a run can only mean the run left it alone.
func seedScoped(t *testing.T, dir, tree string) []byte {
	t.Helper()
	if err := gatestate.SaveScoped(dir, gatestate.Scoped{
		Tree:    tree,
		Covered: []string{"src/seeded.ts"},
		Lanes:   []string{consent.Fingerprint("seeded")},
		At:      time.Now().UTC(),
		Runner:  "local",
	}); err != nil {
		t.Fatal(err)
	}
	return scopedBytes(t, dir)
}

// seedScopedHere is seedScoped on the tree a run would measure now.
func seedScopedHere(t *testing.T, dir string) []byte {
	t.Helper()
	tree, err := treefp.WorkingTree(dir)
	if err != nil {
		t.Fatal(err)
	}
	return seedScoped(t, dir, tree)
}

// scopedGreenLines are the transcript's `scoped green:` lines.
func scopedGreenLines(out string) []string {
	var found []string
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(l, "scoped green:") {
			found = append(found, l)
		}
	}
	return found
}

// assertNoRecord: no scoped.json, and a transcript that does not claim one.
func assertNoRecord(t *testing.T, dir, out string) {
	t.Helper()
	if b := scopedBytes(t, dir); b != nil {
		t.Errorf("recorded %s, want nothing", b)
	}
	if got := scopedGreenLines(out); len(got) != 0 {
		t.Errorf("a run that recorded nothing says %q", got)
	}
}

// laneFingerprint is what a record names the fixture's lane by.
func laneFingerprint(t *testing.T, name string) string {
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
		if lane.Name == name {
			return consent.Fingerprint(laneConsentLine(lane))
		}
	}
	t.Fatalf("the fixture declares no lane %q", name)
	return ""
}

// TestScopedGreenCoversOnlyWhatRelateListed is c-9: a path with no test stays out
// of the record however green the run beside it was. The record says what ran and
// where, and the run says it recorded, once.
func TestScopedGreenCoversOnlyWhatRelateListed(t *testing.T) {
	dir, notes := scopedRepo(t, relateLane, "src/a.ts", "src/b.svelte", "src/a.test.ts")
	(&relateSeam{stdout: aToATest}).install(t)

	out, err := runTestCapturing(t, "--files", "src/a.ts", "--files", "src/b.svelte")
	if err != nil {
		t.Fatalf("dross test --files: %v", err)
	}
	rec := scopedRecordOf(t, dir)
	if rec == nil {
		t.Fatalf("a green relate run recorded nothing (notes: %q)", notes.String())
	}
	if want := []string{"src/a.ts"}; !reflect.DeepEqual(rec.Covered, want) {
		t.Errorf("covered = %q, want %q", rec.Covered, want)
	}
	if want := []string{laneFingerprint(t, "web")}; !reflect.DeepEqual(rec.Lanes, want) {
		t.Errorf("lanes = %q, want %q", rec.Lanes, want)
	}
	if rec.Runner != "local" || rec.At.IsZero() {
		t.Errorf("runner = %q, at = %v; want local and a time", rec.Runner, rec.At)
	}
	if got, want := scopedGreenLines(out), []string{"scoped green: 1 covered"}; !reflect.DeepEqual(got, want) {
		t.Errorf("scoped green lines = %q, want %q:\n%s", got, want, out)
	}
	if got := uncoveredLine(t, out); got != "uncovered: src/b.svelte" {
		t.Errorf("uncovered line = %q:\n%s", got, out)
	}
}

// TestScopedGreenCountsDistinctPaths: two lanes reaching one path cover one path,
// and each lane that did is on the record.
func TestScopedGreenCountsDistinctPaths(t *testing.T) {
	dir, _ := scopedRepo(t, relateLane+`

[[runtime.test_lane]]
name = "shared"
match = ["src/**"]
command = "npx vitest run --project shared"
selector = "path"
vouch = true
relate = "node scripts/relate.mjs --project shared"`, "src/a.ts", "src/a.test.ts")
	(&relateSeam{stdout: aToATest}).install(t)

	out, err := runTestCapturing(t, "--files", "src/a.ts")
	if err != nil {
		t.Fatalf("dross test --files: %v", err)
	}
	rec := scopedRecordOf(t, dir)
	if rec == nil {
		t.Fatal("recorded nothing")
	}
	if want := []string{"src/a.ts"}; !reflect.DeepEqual(rec.Covered, want) {
		t.Errorf("covered = %q, want %q", rec.Covered, want)
	}
	if want := []string{laneFingerprint(t, "web"), laneFingerprint(t, "shared")}; !reflect.DeepEqual(rec.Lanes, want) {
		t.Errorf("lanes = %q, want %q", rec.Lanes, want)
	}
	if got, want := scopedGreenLines(out), []string{"scoped green: 1 covered"}; !reflect.DeepEqual(got, want) {
		t.Errorf("scoped green lines = %q, want %q", got, want)
	}
}

// TestScopedGreenNamesTheHostItRanOn: a host is where the batch ran, so the record
// says so, and the tree it vouches for is the one here.
func TestScopedGreenNamesTheHostItRanOn(t *testing.T) {
	relateHostFixture(t, relateLane, "src/a.ts", "src/a.test.ts")
	dir := asGitRepo(t)
	captureGreen(t)
	(&relateSeam{stdout: aToATest}).installRemote(t)

	if err := runCmd(t, Test(), "--files", "src/a.ts"); err != nil {
		t.Fatalf("dross test --files: %v", err)
	}
	rec := scopedRecordOf(t, dir)
	if rec == nil || rec.Runner != "helicon" {
		t.Fatalf("record = %+v, want one recorded on helicon", rec)
	}
}

// TestScopedGreenMatchesCandidate: the recorder and the commit side fingerprint
// the same tree the same way.
func TestScopedGreenMatchesCandidate(t *testing.T) {
	dir, _ := scopedRepo(t, relateLane, "src/a.ts", "src/a.test.ts")
	(&relateSeam{stdout: aToATest}).install(t)

	if err := runCmd(t, Test(), "--files", "src/a.ts"); err != nil {
		t.Fatalf("dross test --files: %v", err)
	}
	rec := scopedRecordOf(t, dir)
	if rec == nil {
		t.Fatal("recorded nothing")
	}
	if want := candidateAfterAddAll(t, dir); rec.Tree != want {
		t.Errorf("scoped tree %s != candidate after git add -A %s", rec.Tree, want)
	}
}

// TestRefusedRunsRecordNothing is zero_test_scoped_run: none of these measured a
// path, so none of them may vouch for one.
func TestRefusedRunsRecordNothing(t *testing.T) {
	local := func(seam relateSeam) func(t *testing.T) string {
		return func(t *testing.T) string {
			dir, _ := scopedRepo(t, relateLane, "src/a.ts", "src/a.test.ts")
			seam.install(t)
			return dir
		}
	}
	host := func(seam relateSeam) func(t *testing.T) string {
		return func(t *testing.T) string {
			relateHostFixture(t, relateLane, "src/a.ts", "src/a.test.ts")
			dir := asGitRepo(t)
			captureGreen(t)
			seam.installRemote(t)
			installSpawnRecorder(t, nil)
			return dir
		}
	}
	for _, tc := range []struct {
		name    string
		arrange func(t *testing.T) string
		want    int
	}{
		{"relate lists nothing", local(relateSeam{}), exitNothingMeasured},
		{"relate exits non-zero", local(relateSeam{stdout: aToATest, err: fakeExit{code: 2}}), exitPrepareFailed},
		{"a malformed line", local(relateSeam{stdout: "RELATE-MALFORMED\n"}), exitPrepareFailed},
		{"a named test absent from the tree", local(relateSeam{stdout: "src/a.ts\tsrc/gone.test.ts\n"}), exitPrepareFailed},
		{"transport fails on relate", host(relateSeam{stdout: aToATest, err: fakeExit{code: 255}}), exitTransport},
		{"transport fails on the batched line", host(relateSeam{stdout: aToATest, laneErr: fakeExit{code: 255}}), exitTransport},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := tc.arrange(t)
			out, err := runTestCapturing(t, "--files", "src/a.ts")
			if ExitCode(err) != tc.want {
				t.Fatalf("exit = %d (%v), want %d", ExitCode(err), err, tc.want)
			}
			assertNoRecord(t, dir, out)
		})
	}
}

// TestRunsThatMeasuredNothingLeaveTheRecord: none of these is a verdict on the
// tree, so a record standing on it stands, byte for byte.
func TestRunsThatMeasuredNothingLeaveTheRecord(t *testing.T) {
	onHost := func(t *testing.T) string {
		t.Helper()
		relateHostFixture(t, relateLane, "src/a.ts", "src/a.test.ts")
		dir := asGitRepo(t)
		captureGreen(t)
		return dir
	}
	for _, tc := range []struct {
		name    string
		arrange func(t *testing.T) string
		want    int
	}{
		{"exit 3, the sync never reached the host", func(t *testing.T) string {
			dir := onHost(t)
			installRemoteRecorder(t, fakeExit{code: 255})
			return dir
		}, exitTransport},
		{"exit 3 on the batched line", func(t *testing.T) string {
			dir := onHost(t)
			(&relateSeam{stdout: aToATest, laneErr: fakeExit{code: 255}}).installRemote(t)
			return dir
		}, exitTransport},
		{"exit 4, an incomplete transfer", func(t *testing.T) string {
			dir := onHost(t)
			installRemoteRecorder(t, fakeExit{code: 23})
			return dir
		}, exitPartial},
		{"exit 5, nothing related", func(t *testing.T) string {
			dir, _ := scopedRepo(t, relateLane, "src/a.ts")
			(&relateSeam{}).install(t)
			return dir
		}, exitNothingMeasured},
		{"exit 6, an ungranted lane", func(t *testing.T) string {
			filesFixture(t, relateLane)
			touchFile(t, "src/a.ts")
			dir := asGitRepo(t)
			captureGreen(t)
			(&relateSeam{stdout: aToATest}).install(t)
			return dir
		}, exitLaneRefused},
		{"exit 7, a failed relate", func(t *testing.T) string {
			dir, _ := scopedRepo(t, relateLane, "src/a.ts")
			(&relateSeam{err: fakeExit{code: 2}}).install(t)
			return dir
		}, exitPrepareFailed},
		{"exit 8, no toolchain anywhere", func(t *testing.T) string {
			dir, _ := scopedRepo(t, relateLane, "src/a.ts")
			installLaneLookPath(t, "npx")
			(&relateSeam{stdout: aToATest}).install(t)
			return dir
		}, exitToolchainMissing},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := tc.arrange(t)
			before := seedScopedHere(t, dir)
			out, err := runTestCapturing(t, "--files", "src/a.ts")
			if ExitCode(err) != tc.want {
				t.Fatalf("exit = %d (%v), want %d", ExitCode(err), err, tc.want)
			}
			if got := scopedBytes(t, dir); !bytes.Equal(got, before) {
				t.Errorf("the record changed:\n%s\nwas:\n%s", got, before)
			}
			if got := scopedGreenLines(out); len(got) != 0 {
				t.Errorf("a run that recorded nothing says %q", got)
			}
		})
	}
}

// TestBatchThatCollectedNothingRecordsNothing: a declared empty_exit is a miss,
// which is no verdict either way, so it records nothing and clears nothing.
func TestBatchThatCollectedNothingRecordsNothing(t *testing.T) {
	t.Run("no record to begin with", func(t *testing.T) {
		dir, _ := scopedRepo(t, relateLane+"\nempty_exit = [4]", "src/a.ts", "src/a.test.ts")
		(&relateSeam{stdout: aToATest, laneErr: fakeExit{code: 4}}).install(t)

		out, err := runTestCapturing(t, "--files", "src/a.ts")
		if ExitCode(err) != exitNothingMeasured {
			t.Fatalf("exit = %d (%v), want %d", ExitCode(err), err, exitNothingMeasured)
		}
		assertNoRecord(t, dir, out)
	})

	t.Run("a record on the tree stands", func(t *testing.T) {
		dir, _ := scopedRepo(t, relateLane+"\nempty_exit = [4]", "src/a.ts", "src/a.test.ts")
		before := seedScopedHere(t, dir)
		(&relateSeam{stdout: aToATest, laneErr: fakeExit{code: 4}}).install(t)

		if err := runCmd(t, Test(), "--files", "src/a.ts"); ExitCode(err) != exitNothingMeasured {
			t.Fatalf("exit = %d (%v), want %d", ExitCode(err), err, exitNothingMeasured)
		}
		if got := scopedBytes(t, dir); !bytes.Equal(got, before) {
			t.Errorf("a miss changed the record:\n%s", got)
		}
	})
}

// TestEmptyBatchBesideAGreenLaneCoversNothing: relate mapped src/a.ts, and its
// batch then collected nothing beside a neighbour that passed. The run is green
// and records, and what relate promised for src/a.ts counts for nothing.
func TestEmptyBatchBesideAGreenLaneCoversNothing(t *testing.T) {
	dir, _ := scopedRepo(t, relateLane+"\nempty_exit = [4]"+appLane,
		"src/a.ts", "src/a.test.ts", "app/x.ts", "app/x.test.ts")
	orig := spawnLocal
	t.Cleanup(func() { spawnLocal = orig })
	spawnLocal = func(_, line string, stdout, _ io.Writer) error {
		switch {
		case strings.HasPrefix(line, "node scripts/relate.mjs --project app"):
			io.WriteString(stdout, "app/x.ts\tapp/x.test.ts\n")
		case strings.HasPrefix(line, "node scripts/relate.mjs"):
			io.WriteString(stdout, aToATest)
		case line == batchOfATest:
			return fakeExit{code: 4}
		}
		return nil
	}

	out, err := runTestCapturing(t, "--files", "src/a.ts", "--files", "app/x.ts")
	if err != nil {
		t.Fatalf("dross test --files: %v", err)
	}
	rec := scopedRecordOf(t, dir)
	if rec == nil {
		t.Fatal("the neighbour's green recorded nothing")
	}
	if want := []string{"app/x.ts"}; !reflect.DeepEqual(rec.Covered, want) {
		t.Errorf("covered = %q, want %q", rec.Covered, want)
	}
	if want := []string{laneFingerprint(t, "app")}; !reflect.DeepEqual(rec.Lanes, want) {
		t.Errorf("lanes = %q, want only the lane that measured something", rec.Lanes)
	}
	if got := uncoveredLine(t, out); got != "uncovered: src/a.ts" {
		t.Errorf("uncovered line = %q:\n%s", got, out)
	}
}

// TestRedBatchClearsTheRecordOnItsTree: a red says the tree is broken, whatever an
// earlier run said about it. A code empty_exit does not declare is red like any
// other, so a lane with none declared cannot pass an empty batch off as a miss.
func TestRedBatchClearsTheRecordOnItsTree(t *testing.T) {
	for _, tc := range []struct {
		name, lane string
		code       int
	}{
		{"no empty_exit declared, exit 1", relateLane, 1},
		{"no empty_exit declared, a runner's own code", relateLane, 4},
		{"a code empty_exit does not declare", relateLane + "\nempty_exit = [4]", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir, _ := scopedRepo(t, tc.lane, "src/a.ts", "src/a.test.ts")
			seedScopedHere(t, dir)
			(&relateSeam{stdout: aToATest, laneErr: fakeExit{code: tc.code}}).install(t)

			out, err := runTestCapturing(t, "--files", "src/a.ts")
			if ExitCode(err) != exitSuiteFailed {
				t.Fatalf("exit = %d (%v), want %d", ExitCode(err), err, exitSuiteFailed)
			}
			assertNoRecord(t, dir, out)
		})
	}
}

// TestRedLeavesARecordOnAnotherTree: that record is about code this run did not
// measure.
func TestRedLeavesARecordOnAnotherTree(t *testing.T) {
	dir, _ := scopedRepo(t, relateLane, "src/a.ts", "src/a.test.ts")
	before := seedScoped(t, dir, "another-tree")
	(&relateSeam{stdout: aToATest, laneErr: fakeExit{code: 1}}).install(t)

	if err := runCmd(t, Test(), "--files", "src/a.ts"); ExitCode(err) != exitSuiteFailed {
		t.Fatalf("exit = %d (%v), want %d", ExitCode(err), err, exitSuiteFailed)
	}
	if got := scopedBytes(t, dir); !bytes.Equal(got, before) {
		t.Errorf("a red on this tree touched another tree's record:\n%s", got)
	}
}

// TestRedFromAnotherLaneClearsTheRecord: the vouching lane passed and the lane
// beside it, over the same path, did not. That path is broken, so nothing vouches
// for it.
func TestRedFromAnotherLaneClearsTheRecord(t *testing.T) {
	dir, _ := scopedRepo(t, relateLane+lintLane, "src/a.ts", "src/a.test.ts")
	seedScopedHere(t, dir)
	seam := (&relateSeam{stdout: aToATest, otherErr: fakeExit{code: 1}}).install(t)

	out, err := runTestCapturing(t, "--files", "src/a.ts")
	if ExitCode(err) != exitSuiteFailed {
		t.Fatalf("exit = %d (%v), want %d", ExitCode(err), err, exitSuiteFailed)
	}
	if want := []string{relateOverA, batchOfATest, "npx eslint"}; !reflect.DeepEqual(seam.lines, want) {
		t.Fatalf("spawned %q, want %q", seam.lines, want)
	}
	assertNoRecord(t, dir, out)
}

// TestRedOutrankedByAFailedRelateStillClears: the exit is the relate failure's,
// and the red it outranks is still a red on this tree.
func TestRedOutrankedByAFailedRelateStillClears(t *testing.T) {
	dir, _ := scopedRepo(t, relateLane+goNeighbourLane, "src/a.ts", "internal/cmd/here.go")
	seedScopedHere(t, dir)
	(&relateSeam{err: fakeExit{code: 2}, otherErr: fakeExit{code: 1}}).install(t)

	out, err := runTestCapturing(t, "--files", "src/a.ts", "--files", "internal/cmd/here.go")
	if ExitCode(err) != exitPrepareFailed {
		t.Fatalf("exit = %d (%v), want %d", ExitCode(err), err, exitPrepareFailed)
	}
	assertNoRecord(t, dir, out)
}

// TestRunThatSpawnedNothingRecordsNothing: nothing ran, so there is no green of
// either kind, however the run came to spawn nothing.
func TestRunThatSpawnedNothingRecordsNothing(t *testing.T) {
	for _, tc := range []struct {
		name    string
		arrange func(t *testing.T) string
		want    int
	}{
		{"only build config was left", func(t *testing.T) string {
			dir, _ := scopedRepo(t, relateWideLane, "package.json")
			return dir
		}, exitNothingMeasured},
		{"every lane was refused", func(t *testing.T) string {
			filesFixture(t, relateWideLane)
			touchFile(t, "package.json")
			dir := asGitRepo(t)
			captureGreen(t)
			return dir
		}, exitLaneRefused},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := tc.arrange(t)
			seam := (&relateSeam{}).install(t)

			out, err := runTestCapturing(t, "--files", "package.json")
			if ExitCode(err) != tc.want {
				t.Fatalf("exit = %d (%v), want %d", ExitCode(err), err, tc.want)
			}
			if len(seam.lines) != 0 {
				t.Fatalf("spawned %q, want nothing", seam.lines)
			}
			assertNoRecord(t, dir, out)
			if g := green(t, dir); g != nil {
				t.Errorf("recorded a full green %+v", g)
			}
		})
	}
}

// TestNonVouchingLaneCoversNothing: its green is real and it measured its path,
// and it still has no say in the record, which a gate would sink if it named the
// lane.
func TestNonVouchingLaneCoversNothing(t *testing.T) {
	dir, _ := scopedRepo(t, relateLane+goNeighbourLane, "src/a.ts", "src/a.test.ts", "internal/cmd/here.go")
	(&relateSeam{stdout: aToATest}).install(t)

	if err := runCmd(t, Test(), "--files", "src/a.ts", "--files", "internal/cmd/here.go"); err != nil {
		t.Fatalf("dross test --files: %v", err)
	}
	rec := scopedRecordOf(t, dir)
	if rec == nil {
		t.Fatal("recorded nothing")
	}
	if want := []string{"src/a.ts"}; !reflect.DeepEqual(rec.Covered, want) {
		t.Errorf("covered = %q, want %q", rec.Covered, want)
	}
	if want := []string{laneFingerprint(t, "web")}; !reflect.DeepEqual(rec.Lanes, want) {
		t.Errorf("lanes = %q, want only the vouching lane", rec.Lanes)
	}
}

// TestGreenNonVouchingLaneAloneRecordsNothing: a vouching lane is declared and
// this run never reached it, so nothing vouched and the record stands.
func TestGreenNonVouchingLaneAloneRecordsNothing(t *testing.T) {
	dir, _ := scopedRepo(t, relateLane+goNeighbourLane, "internal/cmd/here.go")
	before := seedScopedHere(t, dir)
	(&relateSeam{}).install(t)

	out, err := runTestCapturing(t, "--files", "internal/cmd/here.go")
	if err != nil {
		t.Fatalf("dross test --files: %v", err)
	}
	if got := scopedBytes(t, dir); !bytes.Equal(got, before) {
		t.Errorf("the record changed:\n%s", got)
	}
	if got := scopedGreenLines(out); len(got) != 0 {
		t.Errorf("a run that recorded nothing says %q", got)
	}
}

// TestNoVouchingLaneNoRecord: a repo that never opted in gets no record, and the
// run's transcript and notes hold nothing of this.
func TestNoVouchingLaneNoRecord(t *testing.T) {
	dir, notes := scopedRepo(t, goAndDocsLanes, "internal/cmd/here.go")
	(&relateSeam{}).install(t)

	out, err := runTestCapturing(t, "--files", "internal/cmd/here.go")
	if err != nil {
		t.Fatalf("dross test --files: %v", err)
	}
	assertNoRecord(t, dir, out)
	if notes.Len() != 0 {
		t.Errorf("notes = %q, want silence", notes.String())
	}
}

// TestRedInANonVouchingRepoLeavesAStrayRecord: a repo that never opted in has no
// recorder running, so a stray record is left to the gate, which ignores it.
func TestRedInANonVouchingRepoLeavesAStrayRecord(t *testing.T) {
	dir, _ := scopedRepo(t, goAndDocsLanes, "internal/cmd/here.go")
	before := seedScopedHere(t, dir)
	(&relateSeam{otherErr: fakeExit{code: 1}}).install(t)

	if err := runCmd(t, Test(), "--files", "internal/cmd/here.go"); ExitCode(err) != exitSuiteFailed {
		t.Fatalf("exit = %d (%v), want %d", ExitCode(err), err, exitSuiteFailed)
	}
	if got := scopedBytes(t, dir); !bytes.Equal(got, before) {
		t.Errorf("the record changed:\n%s", got)
	}
}

// TestMidRunWriteRecordsNothing: the tree that went green no longer exists, and
// the note is the full recorder's own, naming what moved.
func TestMidRunWriteRecordsNothing(t *testing.T) {
	dir, notes := scopedRepo(t, relateLane, "src/a.ts", "src/a.test.ts")
	(&relateSeam{stdout: aToATest, onBatch: func() {
		mustWrite(t, filepath.Join(dir, "coverage.out"), "x")
	}}).install(t)

	out, err := runTestCapturing(t, "--files", "src/a.ts")
	if err != nil {
		t.Fatalf("the run's own verdict changed: %v", err)
	}
	assertNoRecord(t, dir, out)
	want := "dross test: green, but not recorded for the commit gate — these paths changed while the suite ran: coverage.out\n"
	if notes.String() != want {
		t.Errorf("notes = %q, want %q", notes.String(), want)
	}
}

// TestDeletionStaysOutOfCovered: a deleted path has no test to pass, and it is on
// no relate line, so the run beside it covers only what is left.
func TestDeletionStaysOutOfCovered(t *testing.T) {
	t.Run("beside a live path", func(t *testing.T) {
		dir, _ := scopedRepo(t, relateLane, "src/a.ts", "src/a.test.ts")
		(&relateSeam{stdout: aToATest}).install(t)

		if err := runCmd(t, Test(), "--files", "src/a.ts", "--files", "src/gone.ts"); err != nil {
			t.Fatalf("dross test --files: %v", err)
		}
		rec := scopedRecordOf(t, dir)
		if rec == nil {
			t.Fatal("recorded nothing")
		}
		if want := []string{"src/a.ts"}; !reflect.DeepEqual(rec.Covered, want) {
			t.Errorf("covered = %q, want %q", rec.Covered, want)
		}
	})

	t.Run("relate cannot answer for it", func(t *testing.T) {
		dir, _ := scopedRepo(t, relateLane, "src/a.ts", "src/a.test.ts")
		(&relateSeam{stdout: aToATest + "src/gone.ts\tsrc/a.test.ts\n"}).install(t)

		out, err := runTestCapturing(t, "--files", "src/a.ts", "--files", "src/gone.ts")
		if ExitCode(err) != exitPrepareFailed {
			t.Fatalf("exit = %d (%v), want %d", ExitCode(err), err, exitPrepareFailed)
		}
		assertNoRecord(t, dir, out)
	})
}

// TestSecondRunReplacesTheRecord: runs never union. The first run's path has to
// be asked again, on the same tree, to stay covered.
func TestSecondRunReplacesTheRecord(t *testing.T) {
	dir, _ := scopedRepo(t, relateLane, "src/a.ts", "src/b.ts", "src/a.test.ts", "src/b.test.ts")
	(&relateSeam{stdout: aToATest}).install(t)
	if err := runCmd(t, Test(), "--files", "src/a.ts"); err != nil {
		t.Fatalf("first run: %v", err)
	}
	first := scopedRecordOf(t, dir)
	if first == nil {
		t.Fatal("the first run recorded nothing")
	}

	(&relateSeam{stdout: "src/b.ts\tsrc/b.test.ts\n"}).install(t)
	if err := runCmd(t, Test(), "--files", "src/b.ts"); err != nil {
		t.Fatalf("second run: %v", err)
	}
	rec := scopedRecordOf(t, dir)
	if rec == nil || rec.Tree != first.Tree {
		t.Fatalf("record = %+v, want one on the first run's tree", rec)
	}
	if want := []string{"src/b.ts"}; !reflect.DeepEqual(rec.Covered, want) {
		t.Errorf("covered = %q, want %q", rec.Covered, want)
	}
}

// TestUnrunnableLaneContributesNothing: a lane with no LaneLine has no grant to
// stand on, and consent.Fingerprint would still hash its empty line. The run
// cannot get here past consent, so settle is asked directly.
func TestUnrunnableLaneContributesNothing(t *testing.T) {
	lane := project.TestLane{Name: "web", Command: "npx vitest\x00run", Vouch: true, Relate: "node scripts/relate.mjs"}
	if line := laneConsentLine(lane); line != "" {
		t.Fatalf("the fixture lane has a line: %q", line)
	}
	s := &scopedRun{}
	rel := testlane.Relation{Covered: []string{"src/a.ts"}, Tests: []string{"src/a.test.ts"}}
	s.note(rel)
	s.settle(lane, rel, nil)
	if len(s.lanes) != 0 || len(s.covered) != 0 {
		t.Errorf("lanes = %q, covered = %q; want nothing from an unrunnable lane", s.lanes, s.covered)
	}
}

// TestUntaggedFailureIsNoRed: an error with no exit tag exits 1 like a red, and
// says nothing about the code, so it clears nothing.
func TestUntaggedFailureIsNoRed(t *testing.T) {
	s := &scopedRun{}
	s.settle(project.TestLane{Name: "web", Command: "npx vitest run"}, testlane.Relation{}, os.ErrInvalid)
	if s.red {
		t.Error("an untagged error read as a red")
	}
	s.settle(project.TestLane{Name: "web", Command: "npx vitest run"}, testlane.Relation{}, &ExitCodeError{Code: exitSuiteFailed, Err: os.ErrInvalid})
	if !s.red {
		t.Error("a red did not read as one")
	}
	s.settle(project.TestLane{Name: "web", Command: "npx vitest run"}, testlane.Relation{}, nil)
	if !s.red {
		t.Error("a later green took the red back")
	}
}
