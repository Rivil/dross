package cmd

import (
	"context"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/Rivil/dross/internal/statusline"
)

// stdinDeadline bounds the stdin read so the status line never hangs Claude Code's
// prompt if the pipe never closes (matching the reference's 3s/exit-0 behavior).
const stdinDeadline = 3 * time.Second

// Statusline registers `dross statusline` — the native Claude Code status line. It
// reads the status JSON from stdin (bounded, so it never hangs), resolves the render
// inputs (todos, dross state, peer jobs, git branch), and writes the rendered line
// to stdout. It NEVER returns a non-nil error or non-zero exit: any parse/FS error
// yields empty or partial output, mirroring the reference's silent-fail contract —
// a broken status line must never break the prompt.
func Statusline() *cobra.Command {
	return &cobra.Command{
		Use:   "statusline",
		Short: "Render the Claude Code status line (reads status JSON on stdin)",
		Args:  cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			runStatusline(c.InOrStdin(), c.OutOrStdout(), os.Getenv, time.Now(), statuslineGitBranch, stdinDeadline)
			return nil
		},
	}
}

// runStatusline is the testable core: read stdin (bounded by timeout), gather, render,
// write. Every failure mode is swallowed — it returns without writing rather than
// surfacing an error, so the status line degrades silently.
func runStatusline(stdin io.Reader, out io.Writer, env func(string) string, now time.Time, gitBranch func(string) string, timeout time.Duration) {
	data, ok := readBounded(stdin, timeout)
	if !ok {
		return // timed out or read error — emit nothing
	}
	in, err := statusline.Gather(data, env, now, gitBranch)
	if err != nil {
		return // malformed stdin JSON — silent fail
	}
	_, _ = out.Write(statusline.Render(in))
}

// readBounded reads all of r, returning (data, true) on success or (nil, false) if r
// errors or does not finish within timeout — so a stuck pipe can never block forever.
func readBounded(r io.Reader, timeout time.Duration) ([]byte, bool) {
	type result struct {
		data []byte
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		b, err := io.ReadAll(r)
		ch <- result{b, err}
	}()
	select {
	case res := <-ch:
		if res.err != nil {
			return nil, false
		}
		return res.data, true
	case <-time.After(timeout):
		return nil, false
	}
}

// statuslineGitBranch returns dir's git branch (short SHA on detached HEAD, "" outside
// a repo or on any error), using --no-optional-locks and a short timeout so it never
// stalls or mutates the repo from the prompt subprocess.
func statuslineGitBranch(dir string) string {
	if b, err := gitBranchTrim(dir, "symbolic-ref", "--short", "HEAD"); err == nil && b != "" {
		return b
	}
	if b, err := gitBranchTrim(dir, "rev-parse", "--short", "HEAD"); err == nil {
		return b
	}
	return ""
}

func gitBranchTrim(dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	full := append([]string{"--no-optional-locks", "-C", dir}, args...)
	out, err := exec.CommandContext(ctx, "git", full...).Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
