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
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Rivil/dross/internal/argfence"

	"github.com/Rivil/dross/internal/project"
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
	argv, err := shArgv(line)
	if err != nil {
		return err
	}
	c := exec.Command("sh", argv...)
	c.Dir = dir
	c.Stdout = stdout
	c.Stderr = stderr
	c.Stdin = nil
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
			return runTest(filepath.Dir(root), line, local)
		},
	}
	c.Flags().BoolVar(&local, "local", false, "run on this machine even when a remote is granted")
	return c
}

// runTest executes one test run. The transport choice lands here in t-4; for
// now every run is local.
func runTest(repoDir, line string, _ bool) error {
	if err := spawnLocal(repoDir, line, os.Stdout, os.Stderr); err != nil {
		return &ExitCodeError{Code: exitSuiteFailed, Err: fmt.Errorf("test suite failed: %w", err)}
	}
	return nil
}
