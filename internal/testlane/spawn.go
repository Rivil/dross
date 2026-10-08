package testlane

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/Rivil/dross/internal/argfence"
)

// spawn.go holds the spawns that run a repo's own consented lines: the suite
// (`dross test`, local and remote), a runtime slot (`dross run`) and a lane's
// install step. None of them checks consent — every caller in internal/cmd does
// that first, and the exec-consent audit proves each site here is reached only
// through a gated command. Keeping the spawn behind the check in the callee,
// rather than beside it, is what lets internal/cmd hold no os/exec at all.

// ShArgv is the fenced builder for the suite's `sh -c` invocation, labelled
// with runtime.test_command.
//
// sh reads options before -c and honours no end-of-options token, so a command
// line beginning with a dash would be taken as a shell option (`-i`, `-x`)
// rather than as the script — which is why argfence's policy for sh is Reject
// rather than Separator. The line here is the user's own consented
// runtime.test_command and is not a derived value today; the fence is what
// keeps that true the first time a caller passes one.
func ShArgv(line string) ([]string, error) {
	return ShArgvFor("runtime.test_command", line)
}

// ShArgvFor is ShArgv with the field label the refusal should name. `dross run`
// spawns a different [runtime] key per slot, and a fence refusal that always
// blamed test_command would point at the wrong line to edit.
func ShArgvFor(field, line string) ([]string, error) {
	if err := argfence.RejectLeadingDash("sh", field, line); err != nil {
		return nil, err
	}
	return []string{"-c", line}, nil
}

// LookPath is this machine's binary resolver — the local half of the per-lane
// locality decision.
func LookPath(bin string) (string, error) {
	return exec.LookPath(bin)
}

// RunLocal runs one shell command line in dir with a cancellable context,
// streaming its output to the given writers as it arrives.
//
// Streaming rather than capturing is the point. The suite takes minutes; a
// command that prints nothing until it finishes is indistinguishable from a
// hang, and the agent driving it reads the tail as it goes. os/exec writes
// straight through when Stdout is set, so this is buffer-free by construction
// rather than by a flush discipline someone has to maintain.
//
// WaitDelay is what makes a kill actually terminate the call. Killing `sh`
// leaves its children holding the pipe ends, so Wait would block on a copy that
// never ends — the delay closes the descriptors and returns instead.
func RunLocal(ctx context.Context, dir, line string, stdout, stderr io.Writer) error {
	argv, err := ShArgv(line)
	if err != nil {
		return err
	}
	c := exec.CommandContext(ctx, "sh", argv...)
	c.Dir = dir
	c.Stdout = stdout
	c.Stderr = stderr
	c.Stdin = nil
	c.WaitDelay = 5 * time.Second
	return c.Run()
}

// RunSlot runs a `dross run` slot line in dir, streaming straight to this
// process's terminal. It mirrors RunLocal with two deliberate differences:
// stdin is wired for an interactive slot, and the fence is labelled with the
// slot's own field so a refusal names the key the user would edit.
//
// A nil stdin leaves the child's stdin unset. Assigning a nil *os.File to
// Cmd.Stdin would store a non-nil interface holding a nil pointer, which
// os/exec treats as a real reader.
func RunSlot(ctx context.Context, dir, line string, stdin *os.File) error {
	argv, err := ShArgvFor("runtime command", line)
	if err != nil {
		return err
	}
	c := exec.CommandContext(ctx, "sh", argv...)
	c.Dir = dir
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	if stdin != nil {
		c.Stdin = stdin
	}
	// Killing `sh` leaves its children holding the pipe ends, so Wait would
	// block on a copy that never ends. The delay closes the descriptors and
	// returns — without it, Ctrl-C on a dev server hangs the terminal it was
	// meant to give back.
	c.WaitDelay = 5 * time.Second
	return c.Run()
}

// RunRemote spawns a built remote argv — internal/remote's SSHArgs or
// SyncArgs — with script piped to its stdin, streaming output through.
//
// Written as Command(argv[0]) plus an explicit Args assignment rather than a
// `...` spread, for the same reason internal/remote's buildCommand is: the
// subprocess argv audit skips spreads, so the spread form would pass that gate
// by accident. This form is evaluated, and accepted by a named entry with a
// reason in subprocargs_audit_test.go.
func RunRemote(argv []string, stdin string, stdout, stderr io.Writer) error {
	c := exec.Command(argv[0])
	c.Args = argv
	if stdin != "" {
		c.Stdin = strings.NewReader(stdin)
	}
	c.Stdout = stdout
	c.Stderr = stderr
	return c.Run()
}

// RunInstall spawns an install argv on this machine and returns its combined
// output.
//
// Output is captured rather than streamed: an install is short and its whole
// value on failure is the tail, unlike a suite whose silence is
// indistinguishable from a hang. The caller decides where that output goes; it
// must be the user's terminal, never an error, telemetry or a record.
func RunInstall(argv []string) ([]byte, error) {
	if len(argv) == 0 {
		return nil, fmt.Errorf("empty install command")
	}
	return exec.Command(argv[0], argv[1:]...).CombinedOutput()
}
