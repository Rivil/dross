package testlane

import (
	"bytes"
	"context"
	"errors"
	"os"
	"runtime"
	"strings"
	"testing"
)

// `sh` stands in for the repo's own lines here: same shape, no network, no
// dependency on a toolchain the machine might lack.

func skipOnWindows(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("sh-driven spawn tests are unix-only")
	}
}

// TestRunSlotWithNilStdinLeavesStdinUnset: a non-interactive slot passes a nil
// *os.File. Stored into Cmd.Stdin it would be a non-nil interface holding a nil
// pointer, which os/exec hands to the child as a real descriptor; left unset,
// the child reads /dev/null and `cat` exits 0 at once.
func TestRunSlotWithNilStdinLeavesStdinUnset(t *testing.T) {
	skipOnWindows(t)
	var stdin *os.File
	if err := RunSlot(context.Background(), t.TempDir(), "cat", stdin); err != nil {
		t.Fatalf("`cat` with a nil stdin failed: %v", err)
	}
}

// TestRunSlotReportsFailureAndFence: a failing slot line is an error, and a
// leading-dash line is refused before anything spawns, naming the slot field.
func TestRunSlotReportsFailureAndFence(t *testing.T) {
	skipOnWindows(t)
	if err := RunSlot(context.Background(), t.TempDir(), "exit 3", nil); err == nil {
		t.Error("a slot that exited 3 reported success")
	}
	err := RunSlot(context.Background(), t.TempDir(), "-i", nil)
	if err == nil || !strings.Contains(err.Error(), "runtime command") {
		t.Errorf("a leading-dash slot line gave %v, want a refusal naming \"runtime command\"", err)
	}
}

// TestRunLocalExitStatusIsReadableByInterface: the red-proof replay reads a red
// run's exit status through ExitCode() alone, so the status must survive the
// spawn living in this package.
func TestRunLocalExitStatusIsReadableByInterface(t *testing.T) {
	skipOnWindows(t)
	var out bytes.Buffer
	err := RunLocal(context.Background(), t.TempDir(), "echo red; exit 3", &out, &out)
	var exit interface{ ExitCode() int }
	if !errors.As(err, &exit) || exit.ExitCode() != 3 {
		t.Fatalf("err = %v, want one whose ExitCode() is 3", err)
	}
	if strings.TrimSpace(out.String()) != "red" {
		t.Errorf("output = %q, want it streamed to the writer", out.String())
	}
	if err := RunLocal(context.Background(), t.TempDir(), "true", &out, &out); err != nil {
		t.Errorf("a green line failed: %v", err)
	}
}

// TestShArgvForNamesTheField: the fence refuses a leading dash naming the field
// the caller labelled, and ShArgv labels runtime.test_command.
func TestShArgvForNamesTheField(t *testing.T) {
	if _, err := ShArgvFor("test_lane.install", "-c evil"); err == nil || !strings.Contains(err.Error(), "test_lane.install") {
		t.Errorf("ShArgvFor gave %v, want a refusal naming test_lane.install", err)
	}
	if _, err := ShArgv("-x"); err == nil || !strings.Contains(err.Error(), "runtime.test_command") {
		t.Errorf("ShArgv gave %v, want a refusal naming runtime.test_command", err)
	}
	argv, err := ShArgv("go test ./...")
	if err != nil || len(argv) != 2 || argv[0] != "-c" || argv[1] != "go test ./..." {
		t.Errorf("ShArgv = %q, %v; want [-c <line>]", argv, err)
	}
}

// TestRunRemotePipesStdin: the remote seam's script reaches the child's stdin,
// and an empty script leaves stdin unset.
func TestRunRemotePipesStdin(t *testing.T) {
	skipOnWindows(t)
	var out bytes.Buffer
	if err := RunRemote([]string{"sh", "-c", "cat"}, "the script\n", &out, &out); err != nil {
		t.Fatal(err)
	}
	if out.String() != "the script\n" {
		t.Errorf("stdout = %q, want the piped script", out.String())
	}
	out.Reset()
	if err := RunRemote([]string{"sh", "-c", "cat; echo done"}, "", &out, &out); err != nil {
		t.Fatal(err)
	}
	if out.String() != "done\n" {
		t.Errorf("stdout = %q, want only \"done\" — an empty script must not be piped", out.String())
	}
}

// TestRunInstallCapturesCombinedOutput: an install's output comes back to the
// caller on success and failure alike, and an empty argv is an error rather
// than a panic on argv[0].
func TestRunInstallCapturesCombinedOutput(t *testing.T) {
	skipOnWindows(t)
	if _, err := RunInstall(nil); err == nil || !strings.Contains(err.Error(), "empty install command") {
		t.Errorf("an empty argv gave %v", err)
	}
	out, err := RunInstall([]string{"sh", "-c", "echo out; echo err >&2"})
	if err != nil || !strings.Contains(string(out), "out") || !strings.Contains(string(out), "err") {
		t.Errorf("RunInstall = %q, %v; want both streams captured", out, err)
	}
	out, err = RunInstall([]string{"sh", "-c", "echo the tail; exit 2"})
	if err == nil || !strings.Contains(string(out), "the tail") {
		t.Errorf("a failing install gave %q, %v; want its output and an error", out, err)
	}
}
