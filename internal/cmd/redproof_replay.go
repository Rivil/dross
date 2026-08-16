package cmd

// Running a red proof's recorded replay at a commit, so a repoint can be
// CHECKED rather than hoped.
//
// A repoint moves a pin from a commit that has rotted to the phase's fork
// point. Reachability says the new commit exists; it says nothing about whether
// the proof still reproduces there. Where a replay command is recorded, this
// runs it at the proposed commit and the repoint refuses unless it goes red —
// the proof is a claim about a commit, and moving the claim without re-testing
// it is how a red proof quietly becomes a green one.
//
// The consent gate is not optional. The replay line arrives from changes.json,
// which is TRACKED: a cloned repo proposes it, so spawning it is code execution
// the repo chose. That is the same threat trust.go was built for, and this uses
// the same shape — a fingerprint in the gitignored local.toml, granted by a verb
// that prints the line first.
//
// Absent consent is NOT a refusal to repair. Per the locked replay_consent_states
// decision the repoint proceeds and reports itself unverified, because refusing
// would make a rotted pin unrepairable on any fresh clone until the operator
// granted a command they may never want to run. An error while RUNNING is
// different and does refuse: an error is not evidence the proof went red.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// redProofReplayTimeout bounds one replay. A red proof's replay is a test run,
// not a build farm; anything past this is hung, and a hung replay must not
// become a hung repoint.
const redProofReplayTimeout = 20 * time.Minute

// redProofReplayTailLines is how much of the output a refusal or a report
// carries. The tail is the part that says why.
const redProofReplayTailLines = 40

// replayResult is what a replay that actually RAN produced. A result is only
// ever returned with a nil error: every not-run case is an error, so a caller
// cannot mistake "did not run" for "ran green".
type replayResult struct {
	Red      bool
	ExitCode int
	Tail     string
}

// runRedProofReplay checks out sha in a detached worktree and runs line there.
//
// Red is any non-zero exit — the proof failing is the proof working. Green is
// exit 0, which for a red proof means it no longer reproduces at that commit.
// Everything else (no consent, worktree failure, spawn failure, timeout) is an
// error, and callers distinguish the no-consent case with errors.Is against
// ErrNoReplayConsent.
func runRedProofReplay(root, repoDir, sha, line string) (replayResult, error) {
	line = strings.TrimSpace(line)
	if line == "" {
		return replayResult{}, fmt.Errorf("no replay command recorded")
	}
	// FIRST, before any worktree exists and before anything is spawned: an
	// ungranted line must cost zero side effects.
	ok, err := ReplayConsented(root, line)
	if err != nil {
		return replayResult{}, err
	}
	if !ok {
		return replayResult{}, fmt.Errorf("%w:\n\n    %s\n\nGrant it with `dross trust --replay <phase-id>`", ErrNoReplayConsent, line)
	}

	base, err := os.MkdirTemp("", "dross-redproof-replay-")
	if err != nil {
		return replayResult{}, fmt.Errorf("replay could not be run: %w", err)
	}
	// The worktree path must not exist yet, so it is a child of the temp dir
	// rather than the temp dir itself. Outside the repo work tree on purpose:
	// running the proof inside it would test the tree the operator is standing
	// in, not the commit being proposed.
	wt := filepath.Join(base, "wt")
	defer func() { _ = os.RemoveAll(base) }()

	if out, err := gitCombined(repoDir, gitRefArgs("worktree", []string{"add", "--detach"}, wt, sha)...); err != nil {
		return replayResult{}, fmt.Errorf("replay could not be run: could not check out %s in a worktree: %v: %s", short(sha), err, strings.TrimSpace(out))
	}
	// Registered AFTER the add succeeded and so it runs BEFORE the RemoveAll
	// above (defers unwind last-first): removing the directory first would
	// leave git's worktree admin data behind, and the next run inherits a
	// prunable stale entry.
	defer func() {
		_, _ = gitCombined(repoDir, gitRefArgs("worktree", []string{"remove", "--force"}, wt)...)
		_, _ = gitCombined(repoDir, "worktree", "prune")
	}()

	ctx, cancel := context.WithTimeout(context.Background(), redProofReplayTimeout)
	defer cancel()

	var buf bytes.Buffer
	runErr := spawnLocalCtx(ctx, wt, line, &buf, &buf)
	tail := lastLines(buf.String(), redProofReplayTailLines)

	// Checked before the exit status is read: a killed process also exits
	// non-zero, and reporting a timeout as a red proof would be the tool
	// manufacturing the evidence it was asked to check for.
	if ctx.Err() != nil || errors.Is(runErr, context.DeadlineExceeded) {
		return replayResult{}, fmt.Errorf("replay could not be run: timed out after %s — a hung replay is not evidence the proof went red:\n%s", redProofReplayTimeout, tail)
	}
	var exitErr *exec.ExitError
	if errors.As(runErr, &exitErr) {
		return replayResult{Red: true, ExitCode: exitErr.ExitCode(), Tail: tail}, nil
	}
	if runErr != nil {
		return replayResult{}, fmt.Errorf("replay could not be run: %w", runErr)
	}
	return replayResult{Red: false, ExitCode: 0, Tail: tail}, nil
}

// lastLines returns the final n lines of s, so a refusal carries the part of a
// long test run that explains it rather than the part that scrolled past.
func lastLines(s string, n int) string {
	s = strings.TrimRight(s, "\n")
	if s == "" {
		return ""
	}
	lines := strings.Split(s, "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}
