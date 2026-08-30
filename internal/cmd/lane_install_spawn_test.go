package cmd

import (
	"strings"
	"testing"
)

// The install EXECUTION layer: what actually happens on a machine, as opposed
// to lane_install_test.go's decision layer, which stops before anything is
// spawned.
//
// These exercise the real runInstallLocally rather than the localInstallFn seam
// every other install test replaces. Without them the function has no coverage
// at all: every fixture in test_lane_install_test.go and
// lane_install_residue_test.go stubs the seam to record an argv, so the body
// behind it — the guard, the spawn, the error wrap — is asserted by nothing. A
// local install broken in either branch would satisfy the entire install suite,
// which is exactly what verify found (c-6 weak).
//
// `sh` stands in for a real install command: same shape, no network, and no
// dependency on anything the machine might not have. external_cli_audit_test.go
// exempts the POSIX utilities for this purpose — the seam tests are how a spawn
// is simulated here.

// TestLocalInstallRefusesEmptyArgv covers the guard at lane_install.go:216.
//
// Negating it hands argv[0] to exec.Command on an empty slice, which panics
// rather than returning the named error — so the caller reporting a failed
// install would instead take the process down.
func TestLocalInstallRefusesEmptyArgv(t *testing.T) {
	out, err := runInstallLocally(nil)
	if err == nil {
		t.Fatal("an empty install command was accepted")
	}
	if got := err.Error(); !strings.Contains(got, "empty install command") {
		t.Errorf("error does not name the empty command, got %q", got)
	}
	if out != "" {
		t.Errorf("a command that never ran produced output %q", out)
	}
}

// TestLocalInstallWrapsFailureWithTheBinary covers the error wrap at
// lane_install.go:220, and both halves of it.
//
// The wrap is what puts the binary's name on a failure whose own message is
// only ever "exit status N". Dropping it leaves the operator an exit code and
// no subject. The output assertion is the second half: a failing install
// returns its tail ALONGSIDE the error, and that tail is the whole diagnostic
// value of capturing output rather than streaming it.
func TestLocalInstallWrapsFailureWithTheBinary(t *testing.T) {
	out, err := runInstallLocally([]string{"sh", "-c", "echo the tail; exit 3"})
	if err == nil {
		t.Fatal("a command that exited 3 was reported as a successful install")
	}
	if got := err.Error(); !strings.HasPrefix(got, "sh: ") {
		t.Errorf("error does not lead with the binary that failed, got %q", got)
	}
	if !strings.Contains(out, "the tail") {
		t.Errorf("a failed install dropped its output, got %q", out)
	}
}

// TestLocalInstallCapturesOutputOnSuccess is the arm that makes the other two
// mean anything: a function that returned an error unconditionally would pass
// both of them.
//
// It also pins CombinedOutput itself — swapping it for Run() would return an
// empty string on every successful install, and nothing else in the suite would
// notice, since the seam's stub supplies its own output.
func TestLocalInstallCapturesOutputOnSuccess(t *testing.T) {
	out, err := runInstallLocally([]string{"sh", "-c", "echo installed pnpm"})
	if err != nil {
		t.Fatalf("runInstallLocally: %v", err)
	}
	if !strings.Contains(out, "installed pnpm") {
		t.Errorf("the install's output was not captured, got %q", out)
	}
}
