package cmd

// `dross test` — the one place dross runs this repo's test suite.
//
// Until this command, dross never ran `runtime.test_command` at all. It hashed
// it for consent (trust.go) and the prompts told the AGENT to type it into
// Bash. That left the consent gate covering a command nothing executed, and —
// the reason this phase exists — left no execution site to point at another
// machine. You cannot delegate a run that no code performs.
//
// So this is deliberately the whole surface: consent, selector, transport and
// exit status resolve here, and execute.md / quick.md / verify.md call this
// instead of interpolating the raw command. One site to gate, one site to
// delegate.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/Rivil/dross/internal/argfence"

	"github.com/Rivil/dross/internal/project"
	"github.com/Rivil/dross/internal/remote"
)

// Exit codes. They are a contract, not an implementation detail: a caller
// deciding whether to commit reacts differently to "your code is broken" and
// "the run never happened", and collapsing the two is how a dead transport
// gets read as a clean suite.
//
// The suite's OWN exit status is deliberately not propagated. A suite is free
// to exit 3, and a raw passthrough would make its status collide with the
// transport band below — after which the codes would mean nothing. Pass/fail
// is what a caller gates on; the detail is in the message.
const (
	// exitSuiteFailed: the suite ran and went red. Ordinary failure.
	exitSuiteFailed = 1
	// exitTransport: the run never happened — host unreachable, ssh refused,
	// stream died. Nothing was measured.
	exitTransport = 3
	// exitPartial: the connection held but the transfer did not complete, so
	// what ran (if anything) ran against an incomplete tree.
	exitPartial = 4
)

// ExitCodeError carries the process exit status a failure should produce.
// main.go reads it through ExitCode; anything without one exits 1 as before.
type ExitCodeError struct {
	Code int
	Err  error
}

func (e *ExitCodeError) Error() string { return e.Err.Error() }
func (e *ExitCodeError) Unwrap() error { return e.Err }

// ExitCode maps an error to the process exit status. A nil error is 0, an
// untagged error is 1 — the behaviour every other command already had.
func ExitCode(err error) int {
	if err == nil {
		return 0
	}
	var ec *ExitCodeError
	if errors.As(err, &ec) {
		return ec.Code
	}
	return 1
}

// spawnLocal is the local-execution seam. Tests replace it to record the argv
// without running anything; production never reassigns it.
var spawnLocal = runLocalCommand

// runLocalCommand runs one shell command line in dir, streaming its output to
// the given writers as it arrives.
//
// Streaming rather than capturing is the point. The suite takes minutes; a
// command that prints nothing until it finishes is indistinguishable from a
// hang, and the agent driving it reads the tail as it goes. os/exec writes
// straight through when Stdout is set, so this is buffer-free by construction
// rather than by a flush discipline someone has to maintain.
func runLocalCommand(dir, line string, stdout, stderr io.Writer) error {
	return runLocalCommandCtx(context.Background(), dir, line, stdout, stderr)
}

// spawnLocalCtx is the same seam with a deadline. Tests replace it to record
// the argv and the cwd without running anything; production never reassigns it.
var spawnLocalCtx = runLocalCommandCtx

// runLocalCommandCtx is runLocalCommand with a cancellable context, added for
// the red-proof replay: an unbounded spawn there would turn a hung proof into a
// hung repoint, and a repoint that never returns is worse than one that refuses.
//
// WaitDelay is what makes the kill actually terminate the call. Killing `sh`
// leaves its children holding the pipe ends, so Wait would block on a copy that
// never ends — the delay closes the descriptors and returns instead.
func runLocalCommandCtx(ctx context.Context, dir, line string, stdout, stderr io.Writer) error {
	argv, err := shArgv(line)
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

// shArgv is the fenced builder for an `sh -c` invocation, in the same shape
// every other spawn site in this repo uses: the fence lives in the builder and
// the caller spreads the result.
//
// sh reads options before -c and honours no end-of-options token, so a command
// line beginning with a dash would be taken as a shell option (`-i`, `-x`)
// rather than as the script — which is why argfence's policy for sh is Reject
// rather than Separator. The line here is the user's own consented
// runtime.test_command and is not a derived value today; the fence is what
// keeps that true the first time a caller passes one.
func shArgv(line string) ([]string, error) {
	if err := argfence.RejectLeadingDash("sh", "runtime.test_command", line); err != nil {
		return nil, err
	}
	return []string{"-c", line}, nil
}

// testCommandLine appends a package/path selector to the consented command.
//
// With no selector the line is byte-identical to runtime.test_command — the
// string the user consented to. That identity matters: `dross test` with no
// arguments must be exactly what `dross trust` showed them, or the gate is
// approving one command and running another.
//
// Selector arguments are shell-quoted because the line goes to `sh -c`. The
// consented command is the user's own text and stays verbatim; the arguments
// come from an agent's argv and do not.
func testCommandLine(base string, selector []string) string {
	line := strings.TrimSpace(base)
	for _, s := range selector {
		line += " " + shellQuoteArg(s)
	}
	return line
}

// shellQuoteArg single-quotes an argument for `sh -c`, the same allowlist-free
// approach internal/remote uses: wrap in single quotes and escape any embedded
// single quote, which is total rather than a metacharacter blocklist.
func shellQuoteArg(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// Test registers `dross test`.
func Test() *cobra.Command {
	var local bool
	c := &cobra.Command{
		Use:   "test [selector...]",
		Short: "Run this repo's test suite",
		Long: "Runs runtime.test_command — the command `dross trust` consented to, and\n" +
			"nothing else. Trailing arguments are appended as a package/path selector,\n" +
			"so a targeted re-run after a fix costs a package rather than the suite.\n\n" +
			"Output streams as it arrives and the exit status reports the suite, not\n" +
			"the runner.",
		SilenceUsage: true,
		RunE: func(_ *cobra.Command, args []string) error {
			// First, before any I/O: a refusal that had already spawned the
			// suite would have done the thing it was refusing to authorize.
			if err := requireExecConsent(); err != nil {
				return err
			}
			root, err := FindRoot()
			if err != nil {
				return err
			}
			proj, err := project.Load(filepath.Join(root, project.File))
			if err != nil {
				return err
			}
			line := testCommandLine(proj.Runtime.TestCommand, args)
			return runTest(root, filepath.Dir(root), line, local)
		},
	}
	c.Flags().BoolVar(&local, "local", false, "run on this machine even when a remote is granted")
	return c
}

// spawnRemote is the remote-execution seam: a fully-built argv plus the script
// piped to its stdin, streamed the same way a local run is. Tests replace it to
// record what would have been spawned without touching a network.
//
// It is separate from internal/remote's own Exec because that one CAPTURES
// stdout — right for a probe, wrong for a suite whose output is the thing the
// caller is reading. internal/remote stays the argv builder; execution and
// streaming live here.
var spawnRemote = runRemoteCommand

// runRemoteCommand spawns a built remote argv, streaming output through.
//
// Written as Command(argv[0]) plus an explicit Args assignment rather than a
// `...` spread, for the same reason internal/remote's buildCommand is: the
// subprocess argv audit skips spreads, so the spread form would pass that gate
// by accident. This form is evaluated, and accepted by a named entry with a
// reason in subprocargs_audit_test.go.
func runRemoteCommand(argv []string, stdin string, stdout, stderr io.Writer) error {
	c := exec.Command(argv[0])
	c.Args = argv
	if stdin != "" {
		c.Stdin = strings.NewReader(stdin)
	}
	c.Stdout = stdout
	c.Stderr = stderr
	return c.Run()
}

// testTarget resolves which machine this run happens on. A nil target means
// here.
//
// Remote is the default whenever a grant exists (locked remote_by_default): the
// point of granting a host is that runs leave the laptop, and an opt-in flag
// would leave the grant unused except when someone remembered it. --local is
// the escape for a remote that is down or a tree mid-edit.
func testTarget(root, repoDir string, local bool) (*remote.Target, error) {
	if local {
		return nil, nil
	}
	return readRemoteGrant(root, repoDir)
}

// runTest executes one test run, here or on the granted host.
func runTest(root, repoDir, line string, local bool) error {
	target, err := testTarget(root, repoDir, local)
	if err != nil {
		return err
	}
	if target == nil {
		if err := spawnLocal(repoDir, line, os.Stdout, os.Stderr); err != nil {
			return &ExitCodeError{Code: exitSuiteFailed, Err: fmt.Errorf("test suite failed: %w", err)}
		}
		return nil
	}
	return runTestRemotely(*target, repoDir, line)
}

// runTestRemotely pushes the tree, then runs the suite over ssh.
//
// The order is the correctness property, not a sequence of steps: running
// before the sync measures whatever the remote tree happened to hold, which is
// the previous run's code. Both argv builders validate the target and return an
// error INSTEAD of an argv, so an unsafe host never reaches a shell.
func runTestRemotely(t remote.Target, repoDir, line string) error {
	root, err := filepath.Abs(repoDir)
	if err != nil {
		return err
	}
	sync, err := remote.SyncArgs(t, root)
	if err != nil {
		return err
	}
	if err := spawnRemote(sync, "", os.Stdout, os.Stderr); err != nil {
		return remoteFailure("rsync", t.Host, err)
	}

	ssh, err := remote.SSHArgs(t)
	if err != nil {
		return err
	}
	// The consented line runs under a shell on the far side too, so the
	// selector and the command mean there exactly what they mean here.
	script, err := remote.Script(t, []string{"sh", "-c", line})
	if err != nil {
		return err
	}
	if err := spawnRemote(ssh, script, os.Stdout, os.Stderr); err != nil {
		return remoteFailure("ssh", t.Host, err)
	}
	return nil
}

// exitCoder is what both *exec.ExitError and a test double expose: the status
// the spawned process ended with. Matched as an interface rather than as the
// concrete type so this classification is reachable from a test without a live
// host — a rule about network failures that can only be exercised over a
// network is a rule nothing checks.
type exitCoder interface{ ExitCode() int }

// remoteFailure turns a spawn failure into a tagged error, keeping the two
// outcomes apart all the way to the exit code.
//
// They mean opposite things to whoever is deciding whether to commit. A red
// suite is information ABOUT THE CODE. An unreachable host is information about
// nothing — the run did not happen, nothing was measured, and treating it as a
// verdict is the false-green this whole seam exists to prevent.
//
// The message names which leg failed and on what host, because "command
// failed" sends the reader to the source when the answer is that a machine is
// down.
func remoteFailure(bin, host string, err error) error {
	var ec exitCoder
	if errors.As(err, &ec) {
		classified := remote.Classify(bin, host, ec.ExitCode())
		switch {
		case errors.Is(classified, remote.ErrPartial):
			// The connection held but the tree did not all arrive, so whatever
			// ran, ran against an incomplete checkout. Not a verdict either.
			return &ExitCodeError{Code: exitPartial, Err: fmt.Errorf("incomplete transfer to %s — the suite did not run against the full tree: %w", host, classified)}
		case errors.Is(classified, remote.ErrRemoteCommand):
			// The remote program spoke: this is the suite, and it is red.
			return &ExitCodeError{Code: exitSuiteFailed, Err: fmt.Errorf("test suite failed on %s: %w", host, classified)}
		default:
			return &ExitCodeError{Code: exitTransport, Err: fmt.Errorf("could not reach %s — the suite did not run: %w", host, classified)}
		}
	}
	// The local binary is missing or could not start: nothing ran on the
	// remote, which is a transport failure by any useful definition.
	return &ExitCodeError{Code: exitTransport, Err: fmt.Errorf("could not reach %s: remote %s did not start: %w: %v", host, bin, remote.ErrTransport, err)}
}
