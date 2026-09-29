package verify

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// detachHelperEnv switches TestDetachHelperProcess from a no-op into the child
// RunDetachArgv spawns: this test binary, re-executed.
const detachHelperEnv = "DROSS_DETACH_HELPER_EXIT"

const detachSentinel = "detach-child-stdout-sentinel"

// TestDetachHelperProcess is not a test: it is the child process for
// TestDetachArgvRoutesChildStdoutToStderr. Unset, it returns at once.
func TestDetachHelperProcess(t *testing.T) {
	code := os.Getenv(detachHelperEnv)
	if code == "" {
		return
	}
	fmt.Fprint(os.Stdout, detachSentinel)
	n, _ := strconv.Atoi(code)
	os.Exit(n)
}

// captureStdio runs fn with os.Stdout and os.Stderr each swapped for a pipe and
// returns what landed on each. Both are process-global, so a test using this
// must not call t.Parallel.
func captureStdio(t *testing.T, fn func()) (stdout, stderr string) {
	t.Helper()
	origOut, origErr := os.Stdout, os.Stderr
	t.Cleanup(func() { os.Stdout, os.Stderr = origOut, origErr })
	drain := func(r *os.File) <-chan string {
		ch := make(chan string, 1)
		go func() {
			b, _ := io.ReadAll(r)
			_ = r.Close()
			ch <- string(b)
		}()
		return ch
	}
	ro, wo, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	re, we, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	outCh, errCh := drain(ro), drain(re)
	func() {
		defer func() {
			os.Stdout, os.Stderr = origOut, origErr
			_ = wo.Close()
			_ = we.Close()
		}()
		os.Stdout, os.Stderr = wo, we
		fn()
	}()
	return <-outCh, <-errCh
}

// TestDetachArgvRoutesChildStdoutToStderr: a detached verify's transport output
// belongs on stderr, where the user watching the run is looking — stdout is
// the channel a caller may be parsing. A failing child's exit status survives
// as an *exec.ExitError, and an argv naming no program is an error.
func TestDetachArgvRoutesChildStdoutToStderr(t *testing.T) {
	helper := []string{os.Args[0], "-test.run=^TestDetachHelperProcess$"}

	t.Setenv(detachHelperEnv, "0")
	var runErr error
	stdout, stderr := captureStdio(t, func() { runErr = RunDetachArgv(helper) })
	if runErr != nil {
		t.Fatalf("RunDetachArgv on an exit-0 child: %v", runErr)
	}
	if !strings.Contains(stderr, detachSentinel) {
		t.Errorf("the child's stdout did not reach os.Stderr; stderr = %q", stderr)
	}
	if strings.Contains(stdout, detachSentinel) {
		t.Errorf("the child's stdout leaked onto os.Stdout: %q", stdout)
	}

	t.Setenv(detachHelperEnv, "3")
	_, _ = captureStdio(t, func() { runErr = RunDetachArgv(helper) })
	var exit *exec.ExitError
	if !errors.As(runErr, &exit) || exit.ExitCode() != 3 {
		t.Errorf("an exit-3 child gave %v, want *exec.ExitError with ExitCode 3", runErr)
	}

	if err := RunDetachArgv([]string{filepath.Join(t.TempDir(), "no-such-program")}); err == nil {
		t.Error("an argv naming a nonexistent program returned nil")
	}
}
