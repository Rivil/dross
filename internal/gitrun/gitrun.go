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
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
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

// Trim runs a REF-plumbing git command in dir and returns its output trimmed.
func Trim(dir string, args ...string) (string, error) {
	tap(args)
	//dross:exec-exempt the argv is dross's own git plumbing, fenced by the caller's argv builders before it gets here; none of it runs a repo-authored line
	out, err := exec.Command("git", argv(dir, args)...).Output()
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
	tap(args)
	//dross:exec-exempt the argv is dross's own git plumbing, fenced by the caller's argv builders before it gets here; none of it runs a repo-authored line
	out, err := exec.Command("git", argv(dir, args)...).Output()
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
