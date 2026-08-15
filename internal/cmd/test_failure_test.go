package cmd

import (
	"errors"
	"strings"
	"testing"
)

// fakeExit is a spawn failure carrying an exit status, standing in for
// *exec.ExitError so the classification is reachable without a live host. A
// rule about network failures that could only be exercised over a network is a
// rule nothing checks.
type fakeExit struct{ code int }

func (e fakeExit) Error() string { return "exit status" }
func (e fakeExit) ExitCode() int { return e.code }

// TestTransportFailureIsNotAPass is the criterion, stated at its sharpest.
//
// ssh reserves 255 for "I could not do this" precisely so a dead transport is
// distinguishable from whatever the remote program returned. A run that never
// happened must never exit 0: nothing was measured, and reporting that as a
// clean suite is the false-green every rule in this repo is written against.
func TestTransportFailureIsNotAPass(t *testing.T) {
	grantedTestFixture(t, "go test -count=1")
	installSpawnRecorder(t, nil)
	installRemoteRecorder(t, fakeExit{code: 255})

	err := runCmd(t, Test())
	if err == nil {
		t.Fatal("an unreachable host reported a passing suite")
	}
	if got := ExitCode(err); got != exitTransport {
		t.Errorf("ExitCode = %d, want %d (transport)", got, exitTransport)
	}
	if got := ExitCode(err); got == 0 {
		t.Error("a run that never happened exited 0")
	}
}

// TestTransportAndSuiteFailuresDiffer: a caller reacts to these differently, so
// they must be distinguishable without reading prose — in the exit code — and
// the prose must still say which one it was.
func TestTransportAndSuiteFailuresDiffer(t *testing.T) {
	run := func(t *testing.T, spawnErr error) (int, string) {
		t.Helper()
		grantedTestFixture(t, "go test -count=1")
		installSpawnRecorder(t, nil)
		installRemoteRecorder(t, spawnErr)
		err := runCmd(t, Test())
		if err == nil {
			t.Fatal("the failure did not surface")
		}
		return ExitCode(err), err.Error()
	}

	// 1 is the remote PROGRAM speaking: the suite ran and went red.
	suiteCode, suiteMsg := run(t, fakeExit{code: 1})
	// 255 is ssh saying it could not do this at all.
	transportCode, transportMsg := run(t, fakeExit{code: 255})

	if suiteCode == transportCode {
		t.Errorf("a red suite and an unreachable host both exit %d — a caller cannot tell them apart", suiteCode)
	}
	if suiteCode != exitSuiteFailed {
		t.Errorf("red suite exited %d, want %d", suiteCode, exitSuiteFailed)
	}
	if transportCode != exitTransport {
		t.Errorf("unreachable host exited %d, want %d", transportCode, exitTransport)
	}
	if suiteMsg == transportMsg {
		t.Errorf("both failures produced the same message: %q", suiteMsg)
	}
	if !strings.Contains(suiteMsg, "suite failed") {
		t.Errorf("the red-suite message does not say the suite failed: %q", suiteMsg)
	}
	for _, want := range []string{"reach", "helicon"} {
		if !strings.Contains(transportMsg, want) {
			t.Errorf("the transport message does not mention %q — \"command failed\" sends the reader to the source when the answer is that a machine is down: %q", want, transportMsg)
		}
	}
}

// TestIncompleteSyncFails: the connection held, the tree did not all arrive.
// Whatever ran, ran against an incomplete checkout — which is not a verdict
// either, and must not be reported as one.
func TestIncompleteSyncFails(t *testing.T) {
	for _, code := range []int{23, 24} {
		t.Run(rsyncCodeName(code), func(t *testing.T) {
			grantedTestFixture(t, "go test -count=1")
			installSpawnRecorder(t, nil)
			installRemoteRecorder(t, fakeExit{code: code})

			err := runCmd(t, Test())
			if err == nil {
				t.Fatalf("rsync exit %d was reported as success", code)
			}
			if got := ExitCode(err); got != exitPartial {
				t.Errorf("ExitCode = %d, want %d (partial transfer)", got, exitPartial)
			}
			if !strings.Contains(err.Error(), "transfer") {
				t.Errorf("the message does not name the transfer: %v", err)
			}
		})
	}
}

func rsyncCodeName(code int) string {
	if code == 23 {
		return "partial due to error"
	}
	return "partial due to vanished files"
}

// TestSpawnThatNeverStartedIsTransport: rsync or ssh missing from PATH means
// nothing ran on the remote — a transport failure by any useful definition, not
// a mysterious generic error.
func TestSpawnThatNeverStartedIsTransport(t *testing.T) {
	grantedTestFixture(t, "go test -count=1")
	installSpawnRecorder(t, nil)
	installRemoteRecorder(t, errors.New("exec: \"rsync\": executable file not found in $PATH"))

	err := runCmd(t, Test())
	if err == nil {
		t.Fatal("a spawn that never started reported success")
	}
	if got := ExitCode(err); got != exitTransport {
		t.Errorf("ExitCode = %d, want %d (transport)", got, exitTransport)
	}
}

// TestLocalSuiteFailureIsNotTransport is the same distinction on the local
// path, so --local cannot quietly report a broken runner as broken code or the
// reverse.
func TestLocalSuiteFailureIsNotTransport(t *testing.T) {
	testFixture(t, "go test -count=1")
	trustFixture(t)
	installSpawnRecorder(t, fakeExit{code: 1})

	err := runCmd(t, Test())
	if err == nil {
		t.Fatal("a failing local suite reported success")
	}
	if got := ExitCode(err); got != exitSuiteFailed {
		t.Errorf("ExitCode = %d, want %d", got, exitSuiteFailed)
	}
}
