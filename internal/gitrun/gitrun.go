// Package gitrun is the one way dross spawns git. Every git invocation under
// internal/ goes through one of its four verbs, and each verb decides what
// happens to git's output — which is the whole of what makes a git call safe
// or not to quote:
//
//   - Trim runs REF plumbing and returns its output trimmed: rev-parse,
//     symbolic-ref, merge-base, rev-list, for-each-ref over refname/objectname
//     atoms, ls-remote without --get-url, bare --version — verb and options
//     pinned by TestGitTrimRunsRefVerbsOnly. What those print is a ref name git
//     validated, an object id, a count or git's version, so its one
//     taint-cleared marker is true for every caller.
//   - Raw runs a CONTENT read — a log, a diff, a listing, a status — and
//     returns it untouched; Read is Raw trimmed. Neither is marked: a commit
//     subject or a patch line is the repo's text, so each caller marks the line
//     where it slices a SHA, a branch or a path out.
//   - Run runs git for its effect. On failure git's own output goes to Stderr,
//     never into the returned error, which can reach telemetry or a record.
//   - Quiet runs git for its exit status alone and discards all output.
//
// The package is a leaf — it imports nothing from this module — so consent and
// remote can spawn git through it without an import cycle. The argv is always
// the caller's own plumbing, fenced by its argv builders before it gets here;
// none of it runs a repo-authored line.
package gitrun

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Stderr is where a failed Run's own output goes: this process's stderr.
// Tests swap it to capture what a user would see.
var Stderr io.Writer = os.Stderr

// ArgvRecorder, when set, receives every argv dross hands to git (without the
// leading -C <dir>). It is nil in production and costs one nil check; tests
// install a recorder and assert on ORDERING — that a config-derived positional
// never precedes its separator — which a builder test alone cannot prove about
// a call site that quietly went back to a bare literal list.
var ArgvRecorder func([]string)

func tap(args []string) {
	if ArgvRecorder != nil {
		ArgvRecorder(args)
	}
}

// argv is git's full argument list for args run in dir.
func argv(dir string, args []string) []string {
	return append([]string{"-C", dir}, args...)
}

// Options tunes one TrimWith or RawWith call. The zero value is what Trim and
// Raw use: no deadline, git's own locking, no stdin.
type Options struct {
	// Timeout bounds the call; git is killed when it expires. Zero means none.
	Timeout time.Duration
	// NoOptionalLocks runs `git --no-optional-locks`, for a read that must
	// never contend with the user's own git on the same repo (the status line
	// renders on every prompt).
	NoOptionalLocks bool
	// Stdin is fed to git's standard input — a diff for patch-id.
	Stdin io.Reader
}

// context is the call's deadline under o, and the cancel that releases it.
func (o Options) context() (context.Context, context.CancelFunc) {
	if o.Timeout > 0 {
		return context.WithTimeout(context.Background(), o.Timeout)
	}
	return context.Background(), func() {}
}

// fullArgv is git's argument list under o.
func (o Options) fullArgv(dir string, args []string) []string {
	if o.NoOptionalLocks {
		return append([]string{"--no-optional-locks"}, argv(dir, args)...)
	}
	return argv(dir, args)
}

// prepared applies o's stdin and kill delay to c and returns it, so the spawn
// and its Output stay one expression — one line for the audits to key on.
//
// WaitDelay is what makes a timeout actually return. Killing git can leave a
// child holding the output pipe, and Output would block until it closed; the
// delay closes the descriptors and returns instead.
func (o Options) prepared(c *exec.Cmd) *exec.Cmd {
	if o.Stdin != nil {
		c.Stdin = o.Stdin
	}
	if o.Timeout > 0 {
		c.WaitDelay = 250 * time.Millisecond
	}
	return c
}

// Trim runs a REF-plumbing git command in dir and returns its output trimmed.
func Trim(dir string, args ...string) (string, error) {
	return TrimWith(Options{}, dir, args...)
}

// TrimWith is Trim under o — the one Trim spawn. Plain functions rather than
// methods on Options: the taint scan follows a function's return back to its
// callers, and the pointer wrapper SSA synthesises for a value method has
// none, so a method would read as output returned out of the program.
func TrimWith(o Options, dir string, args ...string) (string, error) {
	tap(args)
	ctx, cancel := o.context()
	defer cancel()
	//dross:exec-exempt the argv is dross's own git plumbing, fenced by the caller's argv builders before it gets here; none of it runs a repo-authored line
	out, err := o.prepared(exec.CommandContext(ctx, "git", o.fullArgv(dir, args)...)).Output()
	if err != nil {
		return "", err
	}
	//dross:taint-cleared Trim runs only pinned ref invocations (TestGitTrimRunsRefVerbsOnly): it prints ref names, object ids, counts or git's version, never content
	return strings.TrimSpace(string(out)), nil
}

// Raw runs a git command whose output is CONTENT and returns it untrimmed —
// a porcelain status's leading status column and a NUL-separated listing's
// last entry both survive.
func Raw(dir string, args ...string) (string, error) {
	return RawWith(Options{}, dir, args...)
}

// RawWith is Raw under o — the one Raw spawn.
func RawWith(o Options, dir string, args ...string) (string, error) {
	tap(args)
	ctx, cancel := o.context()
	defer cancel()
	//dross:exec-exempt the argv is dross's own git plumbing, fenced by the caller's argv builders before it gets here; none of it runs a repo-authored line
	out, err := o.prepared(exec.CommandContext(ctx, "git", o.fullArgv(dir, args)...)).Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// Read is Raw with surrounding whitespace trimmed.
func Read(dir string, args ...string) (string, error) {
	out, err := Raw(dir, args...)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// Run runs a git command for its effect. On failure git's own output — which
// can quote file contents, remote responses and hook output — goes to Stderr,
// where the user is already looking, and the caller gets git's exit status
// only: an error is never the terminal, it can reach telemetry or a persisted
// record.
func Run(dir string, args ...string) error {
	tap(args)
	//dross:exec-exempt the argv is dross's own git plumbing, fenced by the caller's argv builders before it gets here; none of it runs a repo-authored line
	out, err := exec.Command("git", argv(dir, args)...).CombinedOutput()
	if err != nil && len(out) > 0 {
		fmt.Fprintf(Stderr, "git %s:\n%s\n", verb(args), bytes.TrimRight(out, "\n"))
	}
	return err
}

// Quiet runs a git command for its exit status alone, discarding its output —
// a ref-exists probe, an ancestry check.
func Quiet(dir string, args ...string) error {
	tap(args)
	//dross:exec-exempt the argv is dross's own git plumbing, fenced by the caller's argv builders before it gets here; none of it runs a repo-authored line
	return exec.Command("git", argv(dir, args)...).Run()
}

// ShortSHA returns the short HEAD sha for the repo at dir, or "nogit" if it
// can't be read (not a git repo, no commits). Best-effort — never errors, so a
// missing repo degrades to a stable "nogit" rather than failing a run. The
// security, quality and techdebt scans name their run directories with it.
func ShortSHA(dir string) string {
	out, err := Trim(dir, "rev-parse", "--short", "HEAD")
	if err != nil {
		return "nogit"
	}
	return NormalizeSHA(out)
}

// NormalizeSHA trims git's output and falls back to "nogit" when it is empty —
// git can exit 0 and print nothing, and an empty run-id component would let
// two runs share a directory name.
func NormalizeSHA(out string) string {
	sha := strings.TrimSpace(out)
	if sha == "" {
		return "nogit"
	}
	return sha
}

// ExitCode is the exit status an error from one of these verbs carries: 0 for
// nil, git's status when git ran and exited non-zero, and -1 when git did not
// run at all — so a caller reading "exit 1 means no" cannot mistake a missing
// binary for an answer.
func ExitCode(err error) int {
	if err == nil {
		return 0
	}
	var exit interface{ ExitCode() int }
	if errors.As(err, &exit) {
		return exit.ExitCode()
	}
	return -1
}

// verb is the subcommand of a git argv, for labelling its output.
func verb(args []string) string {
	for _, a := range args {
		if !strings.HasPrefix(a, "-") {
			return a
		}
	}
	return "command"
}
