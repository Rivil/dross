package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"

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
	c := &cobra.Command{
		Use:   "statusline",
		Short: "Render the Claude Code status line (reads status JSON on stdin)",
		Args:  cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			runStatusline(c.InOrStdin(), c.OutOrStdout(), os.Getenv, time.Now(), statuslineGitBranch, stdinDeadline)
			return nil
		},
	}
	c.AddCommand(statuslineEnable(), statuslineDisable())
	return c
}

// statuslineEnable registers `dross statusline enable` — wire the status line into
// ~/.claude/settings.json (the same wiring `dross install --statusline` performs).
func statuslineEnable() *cobra.Command {
	return &cobra.Command{
		Use:   "enable",
		Short: "Wire the dross statusline into ~/.claude/settings.json",
		Args:  cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			home, err := os.UserHomeDir()
			if err != nil {
				return fmt.Errorf("resolve home: %w", err)
			}
			bin, err := resolveStatuslineBinary()
			if err != nil {
				return err
			}
			return enableStatuslineIn(statuslineSettingsPath(home, os.Getenv), bin, interactiveConfirm(c), c.OutOrStdout())
		},
	}
}

// statuslineDisable registers `dross statusline disable` — un-wire dross's status
// line, leaving any other settings.json keys and any foreign statusLine untouched.
func statuslineDisable() *cobra.Command {
	return &cobra.Command{
		Use:   "disable",
		Short: "Un-wire the dross statusline from ~/.claude/settings.json",
		Args:  cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			home, err := os.UserHomeDir()
			if err != nil {
				return fmt.Errorf("resolve home: %w", err)
			}
			bin, err := resolveStatuslineBinary()
			if err != nil {
				return err
			}
			return disableStatuslineIn(statuslineSettingsPath(home, os.Getenv), bin, c.OutOrStdout())
		},
	}
}

// statuslineCommand is the settings.json statusLine.command for the installed binary
// at binPath: the ABSOLUTE path (quoted to tolerate spaces) plus the statusline verb,
// per the command_form decision — never a bare `dross` relying on PATH.
func statuslineCommand(binPath string) string {
	return fmt.Sprintf("%q statusline", binPath)
}

// resolveStatuslineBinary returns the absolute path of the running dross binary,
// resolving symlinks so settings.json points at the real installed file.
func resolveStatuslineBinary() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("resolve binary path: %w", err)
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	return exe, nil
}

// statuslineSettingsPath is ~/.claude/settings.json, honoring CLAUDE_CONFIG_DIR.
func statuslineSettingsPath(home string, env func(string) string) string {
	if cfg := env("CLAUDE_CONFIG_DIR"); cfg != "" {
		return filepath.Join(cfg, "settings.json")
	}
	return filepath.Join(home, ".claude", "settings.json")
}

// enableStatuslineIn wires the settings.json at path to invoke binPath's statusline.
// It JSON-merges (preserving every other key), is idempotent, and refuses to
// overwrite a DIFFERENT existing statusLine.command unless confirm approves it. The
// write is atomic (temp + rename) so a crash never leaves a half-written settings.json.
func enableStatuslineIn(path, binPath string, confirm func(existing string) bool, out io.Writer) error {
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read settings.json: %w", err)
	}
	command := statuslineCommand(binPath)
	merged, err := statusline.MergeStatusline(data, command, false)
	if errors.Is(err, statusline.ErrStatusLineClobber) {
		existing := existingStatusLineCommand(data)
		if confirm == nil || !confirm(existing) {
			return fmt.Errorf("settings.json already has a different statusLine.command (%s); not overwriting", existing)
		}
		merged, err = statusline.MergeStatusline(data, command, true)
	}
	if err != nil {
		return err
	}
	if err := atomicWriteFile(path, merged); err != nil {
		return err
	}
	fmt.Fprintf(out, "statusLine wired: %s\n", command)
	return nil
}

// disableStatuslineIn removes dross's statusLine entry from the settings.json at
// path, preserving all other keys. It is a no-op when the file is absent or its
// statusLine is not dross's (never removes a foreign status line).
func disableStatuslineIn(path, binPath string, out io.Writer) error {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		fmt.Fprintln(out, "statusLine not set by dross; nothing to disable")
		return nil
	}
	if err != nil {
		return fmt.Errorf("read settings.json: %w", err)
	}
	result, err := statusline.RemoveStatusline(data, statuslineCommand(binPath))
	if err != nil {
		return err
	}
	if err := atomicWriteFile(path, result); err != nil {
		return err
	}
	fmt.Fprintln(out, "statusLine unwired")
	return nil
}

// existingStatusLineCommand extracts statusLine.command from raw settings (or "").
func existingStatusLineCommand(data []byte) string {
	var v struct {
		StatusLine struct {
			Command string `json:"command"`
		} `json:"statusLine"`
	}
	_ = json.Unmarshal(data, &v)
	return v.StatusLine.Command
}

// interactiveConfirm returns a consent callback that prompts on a TTY and refuses
// (returns false) when input is not interactive — so a non-interactive install never
// silently clobbers a foreign statusLine.
func interactiveConfirm(c *cobra.Command) func(string) bool {
	return func(existing string) bool {
		if !term.IsTerminal(int(os.Stdin.Fd())) {
			return false
		}
		fmt.Fprintf(c.OutOrStdout(), "settings.json already has a statusLine.command:\n  %s\nOverwrite it with dross? [y/N] ", existing)
		var resp string
		_, _ = fmt.Fscanln(os.Stdin, &resp)
		resp = strings.ToLower(strings.TrimSpace(resp))
		return resp == "y" || resp == "yes"
	}
}

// atomicWriteFile writes data to path via a temp file in the same directory then
// renames over it, creating parent directories as needed.
func atomicWriteFile(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, ".settings-*")
	if err != nil {
		return fmt.Errorf("stage settings.json: %w", err)
	}
	tmpName := tmp.Name()
	defer func() {
		if _, statErr := os.Stat(tmpName); statErr == nil {
			_ = os.Remove(tmpName)
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write settings.json: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close settings.json: %w", err)
	}
	return os.Rename(tmpName, path)
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
