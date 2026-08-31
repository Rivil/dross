package cmd

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Rivil/dross/internal/remote"
)

// cancelCall is what a teardown asked the host to do.
type cancelCall struct {
	host    string
	runDir  string
	pidFile string
}

func stubCancel(t *testing.T, err error) *[]cancelCall {
	t.Helper()
	var calls []cancelCall
	orig := detachCancel
	t.Cleanup(func() { detachCancel = orig })
	detachCancel = func(tg remote.Target, runDir, pidFile string) error {
		calls = append(calls, cancelCall{host: tg.Host, runDir: runDir, pidFile: pidFile})
		return err
	}
	return &calls
}

// TestStatusNamesEveryDispatchedRun is c-3's read half: a run is never lost by
// forgetting an id, because the phase is the identity and status prints it.
func TestStatusNamesEveryDispatchedRun(t *testing.T) {
	root := chdirDross(t)
	repoDir := filepath.Dir(root)
	for _, r := range []detachedRun{
		{Phase: "remote-run-detach", RunID: "r-1", Host: "helicon", Workdir: "/srv/x",
			RunDir: ".dross-runs/r-1", DispatchedAt: time.Now().UTC(), State: "running"},
		{Phase: "lane-host-affinity", RunID: "r-2", Host: "anachryon", Workdir: "/srv/y",
			RunDir: ".dross-runs/r-2", DispatchedAt: time.Now().UTC(), State: "running"},
	} {
		if err := recordDetachedRun(root, repoDir, r); err != nil {
			t.Fatal(err)
		}
	}
	stubStatus(t, remote.RunStatus{DirExists: true, State: "running"}, nil)

	out := captureStdout(t, func() {
		if err := printDetachedStatus(); err != nil {
			t.Fatalf("status: %v", err)
		}
	})
	for _, want := range []string{"remote-run-detach", "r-1", "helicon", "lane-host-affinity", "r-2", "anachryon"} {
		if !strings.Contains(out, want) {
			t.Errorf("status does not name %q:\n%s", want, out)
		}
	}
}

// TestStatusSaysWhenThereAreNone: an empty listing must say so rather than
// printing nothing, which is indistinguishable from the command failing.
func TestStatusSaysWhenThereAreNone(t *testing.T) {
	chdirDross(t)
	out := captureStdout(t, func() {
		if err := printDetachedStatus(); err != nil {
			t.Fatalf("status: %v", err)
		}
	})
	if !strings.Contains(out, "no detached runs") {
		t.Errorf("an empty listing printed nothing useful:\n%s", out)
	}
}

// TestStatusLabelsARecordedStateAsRecorded is the honesty rule.
//
// When the host does not answer, the recorded state is a dispatch-time guess,
// not an observation. Printing it unqualified would tell a user their run is
// "running" on the strength of a value written before it started — which is
// exactly the case where they most need to know nobody has checked.
func TestStatusLabelsARecordedStateAsRecorded(t *testing.T) {
	root := chdirDross(t)
	if err := recordDetachedRun(root, filepath.Dir(root), detachedRun{
		Phase: "remote-run-detach", RunID: "r-1", Host: "helicon", Workdir: "/srv/x",
		RunDir: ".dross-runs/r-1", DispatchedAt: time.Now().UTC(), State: "running",
	}); err != nil {
		t.Fatal(err)
	}
	stubStatus(t, remote.RunStatus{}, errors.New("no route to host"))

	out := captureStdout(t, func() {
		if err := printDetachedStatus(); err != nil {
			t.Fatalf("status: %v", err)
		}
	})
	if !strings.Contains(out, "recorded") || !strings.Contains(out, "did not answer") {
		t.Errorf("an unverified state was printed as though it were observed:\n%s", out)
	}
}

// TestStatusPointsAFinishedRunAtTheCollectVerb: a finished run that nobody
// collects is a leg already paid for and thrown away.
func TestStatusPointsAFinishedRunAtTheCollectVerb(t *testing.T) {
	root := chdirDross(t)
	if err := recordDetachedRun(root, filepath.Dir(root), detachedRun{
		Phase: "remote-run-detach", RunID: "r-1", Host: "helicon", Workdir: "/srv/x",
		RunDir: ".dross-runs/r-1", DispatchedAt: time.Now().UTC(), State: "running",
	}); err != nil {
		t.Fatal(err)
	}
	stubStatus(t, remote.RunStatus{DirExists: true, State: "finished", HasExit: true, ExitCode: 0}, nil)

	out := captureStdout(t, func() {
		if err := printDetachedStatus(); err != nil {
			t.Fatalf("status: %v", err)
		}
	})
	if !strings.Contains(out, "finished") {
		t.Errorf("a finished run is not reported as finished:\n%s", out)
	}
	if !strings.Contains(out, "dross verify results remote-run-detach") {
		t.Errorf("status does not say how to collect it:\n%s", out)
	}
}

// TestCancelClearsTheRecord is what unblocks a re-dispatch. A cancel that tore
// the run down on the host but left the record would leave the phase refusing
// every future --detach, with nothing running to justify it.
func TestCancelClearsTheRecord(t *testing.T) {
	root := chdirDross(t)
	repoDir := filepath.Dir(root)
	if err := recordDetachedRun(root, repoDir, detachedRun{
		Phase: "remote-run-detach", RunID: "r-1", Host: "helicon", Workdir: "/srv/x",
		RunDir: ".dross-runs/r-1", DispatchedAt: time.Now().UTC(), State: "running",
	}); err != nil {
		t.Fatal(err)
	}
	stubCancel(t, nil)

	if err := cancelDetached("remote-run-detach"); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	got, err := findDetachedRun(root, repoDir, "remote-run-detach")
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Errorf("the record survived the cancel: %+v", got)
	}
}

// TestCancelReachesTheHostThatHoldsTheRun pins both the destination and what
// is torn down. Cancelling on whichever host is preferred today would report a
// run stopped while the real one kept running; removing the directory without
// killing the process would leave gremlins writing into a path that no longer
// exists for the rest of the night.
func TestCancelReachesTheHostThatHoldsTheRun(t *testing.T) {
	root := chdirDross(t)
	if err := recordDetachedRun(root, filepath.Dir(root), detachedRun{
		Phase: "remote-run-detach", RunID: "r-1", Host: "anachryon", Workdir: "/srv/y",
		RunDir: ".dross-runs/r-1", DispatchedAt: time.Now().UTC(), State: "running",
	}); err != nil {
		t.Fatal(err)
	}
	calls := stubCancel(t, nil)

	if err := cancelDetached("remote-run-detach"); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if len(*calls) != 1 {
		t.Fatalf("want exactly one teardown, got %d", len(*calls))
	}
	c := (*calls)[0]
	if c.host != "anachryon" {
		t.Errorf("cancelled on %q, but the run is on anachryon", c.host)
	}
	if c.runDir != ".dross-runs/r-1" {
		t.Errorf("teardown targeted %q", c.runDir)
	}
	if c.pidFile != ".dross-runs/r-1/pid" {
		t.Errorf("teardown read the pid from %q — without it the process group survives", c.pidFile)
	}
}

// TestCancelOfAnUnknownPhaseIsAnError: a mistyped phase id must not report a
// cancellation that never happened while the real run keeps burning the host's
// night.
func TestCancelOfAnUnknownPhaseIsAnError(t *testing.T) {
	chdirDross(t)
	calls := stubCancel(t, nil)

	err := cancelDetached("no-such-phase")
	if err == nil {
		t.Fatal("cancelling a phase with no run reported success")
	}
	if !strings.Contains(err.Error(), "no-such-phase") {
		t.Errorf("the error does not name the phase: %v", err)
	}
	if len(*calls) != 0 {
		t.Errorf("an unknown phase still reached a host: %+v", *calls)
	}
}

// TestCancelAgainstAnUnreachableHostStillClearsButSaysSo is the honest middle
// case. Keeping the record would block a re-dispatch over a host that may
// simply be down; dropping it silently would hide that something may still be
// running there. Both facts are reported.
func TestCancelAgainstAnUnreachableHostStillClearsButSaysSo(t *testing.T) {
	root := chdirDross(t)
	repoDir := filepath.Dir(root)
	if err := recordDetachedRun(root, repoDir, detachedRun{
		Phase: "remote-run-detach", RunID: "r-1", Host: "helicon", Workdir: "/srv/x",
		RunDir: ".dross-runs/r-1", DispatchedAt: time.Now().UTC(), State: "running",
	}); err != nil {
		t.Fatal(err)
	}
	stubCancel(t, errors.New("no route to host"))

	err := cancelDetached("remote-run-detach")
	if err == nil {
		t.Fatal("an unreachable teardown reported clean success")
	}
	if !strings.Contains(err.Error(), "may still be running") {
		t.Errorf("the warning does not say the run may survive: %v", err)
	}
	got, ferr := findDetachedRun(root, repoDir, "remote-run-detach")
	if ferr != nil {
		t.Fatal(ferr)
	}
	if got != nil {
		t.Error("the record was kept, which blocks a re-dispatch over a host that is merely down")
	}
}

// --- cancelLine: the teardown text stubCancel makes invisible ---
//
// Every other cancel test swaps detachCancel wholesale, so they prove the right
// host, runDir and pidFile reach the seam and nothing about what the seam then
// asks the host to do. These assert the command text itself, which needs no ssh
// and so leaves the tests_spawn_no_ssh decision intact.

// TestCancelLineKillsTheProcessGroup is the regression the code comments
// against: setsid made the detached job a group leader, so a kill that
// signalled the bare pid would leave gremlins and its `go test` children
// running — the host's cores held for an hour after the user was told the run
// was cancelled. The `-- -` is what makes the signal reach the group.
func TestCancelLineKillsTheProcessGroup(t *testing.T) {
	line := cancelLine(".dross-runs/r-1", ".dross-runs/r-1/pid")

	if !strings.Contains(line, "kill -- -$(cat ") {
		t.Errorf("cancel line does not kill the process group:\n%s", line)
	}
	// The negation is the load-bearing half: `kill -- -$(...)` contains
	// `kill -$(...)` as a substring, so a positive check alone would still
	// pass with the group marker dropped. Assert the pid-only form is absent.
	if strings.Contains(strings.ReplaceAll(line, "kill -- -$(cat ", ""), "kill -$(cat ") {
		t.Errorf("cancel line signals a bare pid somewhere:\n%s", line)
	}
	if strings.Contains(line, "kill $(cat ") {
		t.Errorf("cancel line signals the pid, not its group:\n%s", line)
	}
}

// TestCancelLineRemovesTheRunDirectory pins the other half of a teardown: the
// record is dropped locally by cancelDetached, so a run dir left on the host
// would be a directory `verify results` could still find and read a stale
// report out of.
func TestCancelLineRemovesTheRunDirectory(t *testing.T) {
	line := cancelLine(".dross-runs/r-1", ".dross-runs/r-1/pid")

	if !strings.Contains(line, "rm -rf ") {
		t.Errorf("cancel line does not remove the run dir:\n%s", line)
	}
	kill := strings.Index(line, "kill")
	rm := strings.Index(line, "rm -rf")
	if kill < 0 || rm < 0 || rm < kill {
		t.Errorf("cancel line removes the run dir before killing the job:\n%s", line)
	}
	// The kill must not abort the line: its failure is expected when the job
	// already exited, and the rm still has to run.
	if !strings.Contains(line, "; rm -rf ") {
		t.Errorf("cancel line chains the rm behind the kill's success:\n%s", line)
	}
}

// TestCancelLineQuotesItsPaths proves both paths go through shellQuoteArg. The
// run dir is dross-generated today, but the workdir it hangs off is user
// config, and an unquoted path in an `rm -rf` is the worst place in the
// codebase for a word split.
func TestCancelLineQuotesItsPaths(t *testing.T) {
	line := cancelLine("/srv/a b/.dross-runs/r-1", "/srv/a b/.dross-runs/r-1/pid")

	for _, want := range []string{
		"'/srv/a b/.dross-runs/r-1/pid'",
		"'/srv/a b/.dross-runs/r-1'",
	} {
		if !strings.Contains(line, want) {
			t.Errorf("cancel line does not quote %s:\n%s", want, line)
		}
	}

	quoted := cancelLine("/srv/x'y", "/srv/x'y/pid")
	if strings.Contains(quoted, `'/srv/x'y`) {
		t.Errorf("a quote in the run dir escapes its quoting:\n%s", quoted)
	}
}

// TestCancelSendsTheCancelLineToTheHost joins the two halves: the extracted
// text is what detachCancel actually hands the transport, so asserting the
// string is asserting the command the host runs. Without this, cancelLine
// could drift into being dead code the tests are happy with.
func TestCancelSendsTheCancelLineToTheHost(t *testing.T) {
	var got []string
	orig := remoteExecFn
	t.Cleanup(func() { remoteExecFn = orig })
	remoteExecFn = func(_ remote.Target, argv []string) (string, error) {
		got = argv
		return "", nil
	}

	if err := detachCancel(remote.Target{Host: "helicon"}, ".dross-runs/r-1", ".dross-runs/r-1/pid"); err != nil {
		t.Fatalf("detachCancel: %v", err)
	}
	want := []string{"bash", "-c", cancelLine(".dross-runs/r-1", ".dross-runs/r-1/pid")}
	if len(got) != len(want) {
		t.Fatalf("argv = %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("argv[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}
