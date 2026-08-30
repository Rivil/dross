package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Rivil/dross/internal/remote"
	"github.com/Rivil/dross/internal/verify"
)

// resultsFixture records a detached run for phaseID and returns the dross root.
func resultsFixture(t *testing.T, phaseID string) string {
	t.Helper()
	root := chdirDross(t)
	rec := detachedRun{
		Phase:        phaseID,
		RunID:        "r-20260830-2201",
		Host:         "helicon",
		Workdir:      "/var/lib/buildcache/src/dross",
		RunDir:       ".dross-runs/r-20260830-2201",
		DispatchedAt: time.Now().UTC().Add(-time.Hour),
		State:        "running",
	}
	if err := recordDetachedRun(root, filepath.Dir(root), rec); err != nil {
		t.Fatal(err)
	}
	return root
}

// stubStatus swaps the host status read, capturing which host was asked.
func stubStatus(t *testing.T, st remote.RunStatus, err error) *[]string {
	t.Helper()
	var asked []string
	orig := detachStatus
	t.Cleanup(func() { detachStatus = orig })
	detachStatus = func(tg remote.Target, runDir string) (remote.RunStatus, error) {
		asked = append(asked, tg.Host)
		return st, err
	}
	return &asked
}

// exitCodeOf pulls the code out of an ExitCodeError, or -1 when the error is
// not one. The code is the contract — a caller polls on it rather than parsing
// prose — so the assertions below check it rather than the message.
func exitCodeOf(err error) int {
	var ec *ExitCodeError
	if errors.As(err, &ec) {
		return ec.Code
	}
	return -1
}

// assertNoArtefacts is the half that matters most for every non-finished
// state: a verify.toml written from a run that has not finished carries a score
// computed over whichever packages happened to be done, and looks exactly like
// a complete one.
func assertNoArtefacts(t *testing.T, root, phaseID string) {
	t.Helper()
	testsPath, verifyPath := verify.FilePaths(root, phaseID)
	for _, p := range []string{testsPath, verifyPath} {
		if _, err := os.Stat(p); err == nil {
			t.Errorf("%s was written for a run that did not finish", filepath.Base(p))
		}
	}
}

// TestResultsOnAnUnreachableHostIsNotAVerdict is c-6 at the fetch end.
//
// The run may well be finishing perfectly on a machine this laptop cannot
// currently see. Reporting that as a failed or empty run would write a verdict
// about code from an observation about a network.
func TestResultsOnAnUnreachableHostIsNotAVerdict(t *testing.T) {
	const id = "remote-run-detach"
	root := resultsFixture(t, id)
	stubStatus(t, remote.RunStatus{}, errors.New("ssh: connect to host helicon port 22: No route to host"))

	err := collectDetached(id)
	if err == nil {
		t.Fatal("an unreachable host reported success")
	}
	if got := exitCodeOf(err); got != exitResultsUnreachable {
		t.Errorf("exit code = %d, want %d (unreachable)", got, exitResultsUnreachable)
	}
	if !strings.Contains(err.Error(), "not failed") {
		t.Errorf("the message does not distinguish unknown from failed: %v", err)
	}
	assertNoArtefacts(t, root, id)
}

// TestResultsOnAStillRunningRunWritesNothing: the run is alive and will finish.
// Collecting it now would publish a partial score under this phase's name.
func TestResultsOnAStillRunningRunWritesNothing(t *testing.T) {
	const id = "remote-run-detach"
	root := resultsFixture(t, id)
	stubStatus(t, remote.RunStatus{DirExists: true, State: "running", PID: 42}, nil)

	err := collectDetached(id)
	if err == nil {
		t.Fatal("a still-running run reported success")
	}
	if got := exitCodeOf(err); got != exitResultsRunning {
		t.Errorf("exit code = %d, want %d (running)", got, exitResultsRunning)
	}
	assertNoArtefacts(t, root, id)

	// The record must survive: a fetch that cleared it would strand a run
	// still burning an hour of host time with nothing left pointing at it.
	got, err := findDetachedRun(root, filepath.Dir(root), id)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Error("polling a running run cleared its record")
	}
}

// TestResultsOnAScheduledRunSaysScheduled is the c-4 read: a user checking at
// midnight on a run set for 02:00 must be told it is waiting, not that it is
// running — otherwise the only way to tell is to notice it never finishes.
func TestResultsOnAScheduledRunSaysScheduled(t *testing.T) {
	const id = "remote-run-detach"
	root := chdirDross(t)
	at := time.Now().Add(2 * time.Hour).UTC().Truncate(time.Second)
	if err := recordDetachedRun(root, filepath.Dir(root), detachedRun{
		Phase: id, RunID: "r-1", Host: "helicon", Workdir: "/srv/x",
		RunDir: ".dross-runs/r-1", DispatchedAt: time.Now().UTC(),
		ScheduledFor: at, State: "scheduled",
	}); err != nil {
		t.Fatal(err)
	}
	stubStatus(t, remote.RunStatus{DirExists: true, State: "scheduled"}, nil)

	err := collectDetached(id)
	if got := exitCodeOf(err); got != exitResultsScheduled {
		t.Errorf("exit code = %d, want %d (scheduled): %v", got, exitResultsScheduled, err)
	}
	if err == nil || !strings.Contains(err.Error(), "scheduled for") {
		t.Errorf("the message does not say when it starts: %v", err)
	}
	assertNoArtefacts(t, root, id)
}

// TestResultsOnAVanishedRunSaysSo is the other half of c-6: "the run has
// written nothing yet" and "the run directory is gone" are both an absent exit
// file, and only the directory check tells them apart. Reported as gone rather
// than as still-running, or a user polls forever on a run that no longer exists.
func TestResultsOnAVanishedRunSaysSo(t *testing.T) {
	const id = "remote-run-detach"
	root := resultsFixture(t, id)
	stubStatus(t, remote.RunStatus{DirExists: false}, nil)

	err := collectDetached(id)
	if got := exitCodeOf(err); got != exitResultsGone {
		t.Errorf("exit code = %d, want %d (gone): %v", got, exitResultsGone, err)
	}
	if err == nil || !strings.Contains(err.Error(), ".dross-runs/r-20260830-2201") {
		t.Errorf("the message does not name the directory that vanished: %v", err)
	}
	assertNoArtefacts(t, root, id)
}

// TestResultsReadsTheRecordedHostNotTodaysGrant is c-5's fetch half, and the
// reason the record stores a host at all.
//
// A pool reordered, a grant edited, or a host that came back up between
// dispatch and now must not redirect the fetch. Collecting from a machine that
// measured nothing yields an empty report; collecting from one holding another
// run's report yields a wrong one that looks right.
func TestResultsReadsTheRecordedHostNotTodaysGrant(t *testing.T) {
	const id = "remote-run-detach"
	root := chdirDross(t)
	if err := recordDetachedRun(root, filepath.Dir(root), detachedRun{
		Phase: id, RunID: "r-1", Host: "anachryon", Workdir: "/srv/x",
		RunDir: ".dross-runs/r-1", DispatchedAt: time.Now().UTC(), State: "running",
	}); err != nil {
		t.Fatal(err)
	}
	// The grant on disk names a DIFFERENT host, which is what a re-resolution
	// would pick up.
	l, err := loadLocal(localPath(root))
	if err != nil {
		t.Fatal(err)
	}
	l.RemoteHost, l.RemoteWorkdir = "helicon", "/var/lib/buildcache/src/dross"
	if err := l.save(localPath(root)); err != nil {
		t.Fatal(err)
	}

	asked := stubStatus(t, remote.RunStatus{DirExists: true, State: "running"}, nil)
	_ = collectDetached(id)

	if len(*asked) != 1 {
		t.Fatalf("want exactly one host asked, got %v", *asked)
	}
	if (*asked)[0] != "anachryon" {
		t.Errorf("the fetch asked %q, but the run was measured on anachryon — "+
			"re-resolving the grant collects from a machine that measured nothing", (*asked)[0])
	}
}

// TestResultsWithNoRecordedRunSaysHowToStartOne: a phase nobody dispatched is a
// mistake worth naming, not an empty success.
func TestResultsWithNoRecordedRunSaysHowToStartOne(t *testing.T) {
	chdirDross(t)
	err := collectDetached("remote-run-detach")
	if err == nil {
		t.Fatal("collecting a phase with no detached run reported success")
	}
	if !strings.Contains(err.Error(), "--detach") {
		t.Errorf("the error does not say how to start one: %v", err)
	}
}
