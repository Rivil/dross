package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// replayFixture is a repo with a phase whose red proof records replayLine,
// plus a second commit so the pin has somewhere to point. It returns the dross
// root, the repo dir and the commit the replay will be run at.
func replayFixture(t *testing.T, replayLine string) (root, repoDir, sha string) {
	t.Helper()
	repoDir = initWithGit(t)
	root = filepath.Join(repoDir, ".dross")
	sha = mustGit(t, repoDir, "rev-parse", "HEAD")

	phaseID := "proofy"
	if err := os.MkdirAll(filepath.Join(root, "phases", phaseID), 0o755); err != nil {
		t.Fatal(err)
	}
	doc := "fixtures/proof/RUN.md"
	mustWrite(t, filepath.Join(repoDir, doc), "**base commit: `"+sha+"`**\n")
	mustGit(t, repoDir, "add", ".")
	mustGit(t, repoDir, "commit", "-q", "-m", "chore: proof doc")

	args := []string{"red-proof", "set", phaseID, "--sha", sha, "--doc", doc}
	if replayLine != "" {
		args = append(args, "--replay", replayLine)
	}
	if err := runCmd(t, Phase(), args...); err != nil {
		t.Fatalf("red-proof set: %v", err)
	}
	return root, repoDir, sha
}

// countingSpawn replaces the spawn seam, recording every invocation. Used where
// the assertion is about whether a spawn happened at all, or where it happened.
type countingSpawn struct {
	calls []struct{ dir, line string }
	err   error
	out   string
}

func (c *countingSpawn) install(t *testing.T) {
	t.Helper()
	orig := spawnLocalCtx
	spawnLocalCtx = func(_ context.Context, dir, line string, stdout, _ io.Writer) error {
		c.calls = append(c.calls, struct{ dir, line string }{dir, line})
		if c.out != "" {
			_, _ = io.WriteString(stdout, c.out)
		}
		return c.err
	}
	t.Cleanup(func() { spawnLocalCtx = orig })
}

// worktreeEntries returns the linked worktrees `git worktree list` reports.
// The main worktree is listed first and dropped by position rather than by
// comparing paths: on macOS git resolves /var/folders to /private/var/folders,
// so a path comparison would count the main tree as a leak on every run.
func worktreeEntries(t *testing.T, repoDir string) []string {
	t.Helper()
	out, err := gitTrim(repoDir, "worktree", "list", "--porcelain")
	if err != nil {
		t.Fatalf("worktree list: %v", err)
	}
	var got []string
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "worktree ") {
			got = append(got, strings.TrimPrefix(line, "worktree "))
		}
	}
	if len(got) == 0 {
		t.Fatalf("`git worktree list` named no worktree at all:\n%s", out)
	}
	return got[1:]
}

// TestReplayRefusesUnconsented: an ungranted line costs ZERO side effects. The
// line arrives from a tracked file, so a clone proposing it must not be able to
// get it spawned by anything.
func TestReplayRefusesUnconsented(t *testing.T) {
	const line = "exit 3"
	root, repoDir, sha := replayFixture(t, line)
	spawn := &countingSpawn{}
	spawn.install(t)

	_, err := runRedProofReplay(root, repoDir, sha, line)
	if err == nil {
		t.Fatal("an unconsented replay ran")
	}
	if !errors.Is(err, ErrNoReplayConsent) {
		t.Errorf("err = %v, want ErrNoReplayConsent", err)
	}
	if !strings.Contains(err.Error(), "dross trust --replay") {
		t.Errorf("refusal does not name the grant command: %v", err)
	}
	if len(spawn.calls) != 0 {
		t.Errorf("an unconsented replay spawned %d command(s): %+v", len(spawn.calls), spawn.calls)
	}
	if got := worktreeEntries(t, repoDir); len(got) != 0 {
		t.Errorf("an unconsented replay left worktrees: %v", got)
	}
}

// TestReplayRedVsGreen: red and green are different answers. A red proof that
// no longer reproduces at the proposed commit is exactly what the repoint must
// refuse on, so collapsing them would defeat the check.
func TestReplayRedVsGreen(t *testing.T) {
	for _, tc := range []struct {
		name     string
		line     string
		wantRed  bool
		wantCode int
	}{
		{"non-zero exit is red", "exit 3", true, 3},
		{"a different non-zero exit is still red", "exit 1", true, 1},
		{"zero exit is green", "echo still-passes", false, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, repoDir, sha := replayFixture(t, tc.line)
			if err := GrantReplayConsent(root, tc.line); err != nil {
				t.Fatal(err)
			}

			got, err := runRedProofReplay(root, repoDir, sha, tc.line)
			if err != nil {
				t.Fatalf("runRedProofReplay: %v", err)
			}
			if got.Red != tc.wantRed {
				t.Errorf("red = %v, want %v", got.Red, tc.wantRed)
			}
			if got.ExitCode != tc.wantCode {
				t.Errorf("exit code = %d, want %d", got.ExitCode, tc.wantCode)
			}
			if !tc.wantRed && !strings.Contains(got.Tail, "still-passes") {
				t.Errorf("tail did not capture the output: %q", got.Tail)
			}
		})
	}
}

// TestReplayRunsAtProposedCommit: the proof must be run against the commit
// being proposed, not against whatever the operator is standing in. A replay
// run in the working tree would "confirm" a repoint the tree happens to satisfy.
func TestReplayRunsAtProposedCommit(t *testing.T) {
	const line = "exit 3"
	root, repoDir, _ := replayFixture(t, line)
	// Propose the FIRST commit, while the working tree is on the second.
	first := mustGit(t, repoDir, "rev-parse", "HEAD~1")
	if err := GrantReplayConsent(root, line); err != nil {
		t.Fatal(err)
	}

	// HEAD is resolved INSIDE the seam: the worktree is torn down before
	// runRedProofReplay returns, so a rev-parse afterwards would be asking a
	// directory that no longer exists.
	var cwd, head, spawned string
	orig := spawnLocalCtx
	spawnLocalCtx = func(_ context.Context, dir, l string, _, _ io.Writer) error {
		cwd, spawned = dir, l
		head, _ = gitTrim(dir, "rev-parse", "HEAD")
		return nil
	}
	t.Cleanup(func() { spawnLocalCtx = orig })

	if _, err := runRedProofReplay(root, repoDir, first, line); err != nil {
		t.Fatalf("runRedProofReplay: %v", err)
	}

	if cwd == "" {
		t.Fatal("the replay never spawned")
	}
	if strings.HasPrefix(filepath.Clean(cwd), filepath.Clean(repoDir)+string(filepath.Separator)) {
		t.Errorf("replay ran inside the repo work tree (%s)", cwd)
	}
	if head != first {
		t.Errorf("replay cwd is at %q, want the proposed %s", head, first)
	}
	if spawned != line {
		t.Errorf("spawned %q, want the recorded line %q", spawned, line)
	}
}

// TestReplayCleansWorktree: every exit path. A leaked worktree accumulates
// across runs and eventually makes `git worktree add` itself fail, turning a
// repair verb into one that stops working after a while.
func TestReplayCleansWorktree(t *testing.T) {
	for _, tc := range []struct {
		name      string
		line      string
		spawnErr  error
		useSpawn  bool
		wantError bool
	}{
		{name: "red", line: "exit 3"},
		{name: "green", line: "true"},
		{name: "spawn error", line: "exit 3", spawnErr: errors.New("exec: \"sh\": executable file not found"), useSpawn: true, wantError: true},
		{name: "timeout", line: "exit 3", spawnErr: context.DeadlineExceeded, useSpawn: true, wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, repoDir, sha := replayFixture(t, tc.line)
			if err := GrantReplayConsent(root, tc.line); err != nil {
				t.Fatal(err)
			}
			var seen string
			if tc.useSpawn {
				orig := spawnLocalCtx
				spawnLocalCtx = func(_ context.Context, dir, _ string, _, _ io.Writer) error {
					seen = dir
					return tc.spawnErr
				}
				t.Cleanup(func() { spawnLocalCtx = orig })
			}

			_, err := runRedProofReplay(root, repoDir, sha, tc.line)
			if tc.wantError && err == nil {
				t.Errorf("expected an error for %s", tc.name)
			}
			if !tc.wantError && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if got := worktreeEntries(t, repoDir); len(got) != 0 {
				t.Errorf("%s left worktrees behind: %v", tc.name, got)
			}
			if seen != "" {
				if _, statErr := os.Stat(seen); !os.IsNotExist(statErr) {
					t.Errorf("%s left the temp worktree dir %s in place", tc.name, seen)
				}
			}
		})
	}
}

// TestReplayTimeoutIsRefusal: a killed process also exits non-zero. Reading
// that as red would have the tool manufacture the evidence it was asked to
// check for.
func TestReplayTimeoutIsRefusal(t *testing.T) {
	const line = "sleep 30"
	root, repoDir, sha := replayFixture(t, line)
	if err := GrantReplayConsent(root, line); err != nil {
		t.Fatal(err)
	}

	// Drive the real seam through an already-cancelled deadline rather than
	// waiting out redProofReplayTimeout: the assertion is about how a killed
	// run is CLASSIFIED, not about how long it takes to get there.
	orig := spawnLocalCtx
	spawnLocalCtx = func(_ context.Context, dir, l string, stdout, stderr io.Writer) error {
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()
		err := runLocalCommandCtx(ctx, dir, l, stdout, stderr)
		if ctx.Err() != nil {
			// Mirror the real seam: the outer context is what
			// runRedProofReplay inspects, so make the deadline visible.
			return fmt.Errorf("%w: %v", context.DeadlineExceeded, err)
		}
		return err
	}
	t.Cleanup(func() { spawnLocalCtx = orig })

	got, err := runRedProofReplay(root, repoDir, sha, line)
	if err == nil {
		t.Fatalf("a timed-out replay was reported as a result: %+v", got)
	}
	if got.Red {
		t.Error("a timed-out replay was classified red")
	}
	if !strings.Contains(err.Error(), "could not be run") {
		t.Errorf("err = %v, want it to say the replay could not be run", err)
	}
}

// TestSpawnLocalCtxCancels: the new context variant must actually terminate a
// command that outlives its deadline. Without this the timeout above is a
// message rather than a kill, and the hung process outlives dross.
func TestSpawnLocalCtxCancels(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	start := time.Now()
	err := runLocalCommandCtx(ctx, t.TempDir(), "sleep 30", io.Discard, io.Discard)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("a command that outlived its deadline returned nil")
	}
	if elapsed > 20*time.Second {
		t.Errorf("took %s to kill a command with a 100ms deadline", elapsed)
	}
	if ctx.Err() == nil {
		t.Error("the context was not cancelled")
	}

	// The contextless caller keeps its behaviour: same signature, still runs
	// to completion.
	if err := spawnLocal(t.TempDir(), "true", io.Discard, io.Discard); err != nil {
		t.Errorf("spawnLocal regressed: %v", err)
	}
	if err := spawnLocal(t.TempDir(), "exit 7", io.Discard, io.Discard); err == nil {
		t.Error("spawnLocal no longer reports a non-zero exit")
	}
}

// TestTrustGrantsReplayCommand: the grant prints the line BEFORE it trusts it.
// A grant that did not show the command would be a rubber stamp on a line the
// repo chose and nobody read.
func TestTrustGrantsReplayCommand(t *testing.T) {
	const line = "exit 3"
	root, repoDir, sha := replayFixture(t, line)

	var err error
	out := captureStdout(t, func() { err = runCmd(t, Trust(), "--replay", "proofy") })
	if err != nil {
		t.Fatalf("trust --replay: %v", err)
	}
	if !strings.Contains(out, line) {
		t.Errorf("the grant did not print the line it trusted:\n%s", out)
	}

	ok, cerr := ReplayConsented(root, line)
	if cerr != nil {
		t.Fatal(cerr)
	}
	if !ok {
		t.Fatal("the line is still unconsented after a grant")
	}
	if _, err := runRedProofReplay(root, repoDir, sha, line); err != nil {
		t.Errorf("a granted replay still refused: %v", err)
	}
}

// TestTrustTestCommandUnchanged: the new flag is additive. The bare verb still
// means the one thing it always meant.
func TestTrustTestCommandUnchanged(t *testing.T) {
	dir := gatedFixture(t)
	root := filepath.Join(dir, ".dross")

	if err := runCmd(t, Trust()); err != nil {
		t.Fatalf("bare trust: %v", err)
	}
	l, err := loadLocal(localPath(root))
	if err != nil {
		t.Fatal(err)
	}
	if l.TrustedTestCommand == "" {
		t.Error("bare `dross trust` no longer fingerprints runtime.test_command")
	}
	if l.TrustedReplayCommands != "" {
		t.Errorf("bare `dross trust` wrote a replay grant: %q", l.TrustedReplayCommands)
	}
	if err := runCmd(t, Trust(), "some-positional"); err == nil {
		t.Error("`dross trust <positional>` was accepted")
	}
}

// TestReplayTrustNotInLocalKeys: consent must never be grantable by a generic
// key-writer, and must never be committable. Both halves are the same property
// — the repo cannot authorize itself.
func TestReplayTrustNotInLocalKeys(t *testing.T) {
	if _, ok := localKeys["trusted_replay_commands"]; ok {
		t.Error("trusted_replay_commands is in localKeys — `dross local set` can grant consent without showing the command")
	}

	b, err := os.ReadFile(filepath.Join(repoRootForDocs(t), ".gitignore"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), ".dross/local.toml") {
		t.Error(".dross/local.toml is not gitignored — a consent grant could be committed")
	}
}

// TestReplayTailKeepsTheTail: a refusal must carry the END of a long run — the
// part that says why it failed — not the beginning that scrolled past. A
// truncation that kept the head would report a passing preamble as the reason
// a proof did not go red.
func TestReplayTailKeepsTheTail(t *testing.T) {
	const total = redProofReplayTailLines * 3
	var b strings.Builder
	for i := 1; i <= total; i++ {
		fmt.Fprintf(&b, "line-%d\n", i)
	}

	got := strings.Split(lastLines(b.String(), redProofReplayTailLines), "\n")

	if len(got) != redProofReplayTailLines {
		t.Fatalf("returned %d lines, want %d", len(got), redProofReplayTailLines)
	}
	wantFirst := fmt.Sprintf("line-%d", total-redProofReplayTailLines+1)
	if got[0] != wantFirst {
		t.Errorf("first line is %q, want %q — the window is not anchored at the tail", got[0], wantFirst)
	}
	if wantLast := fmt.Sprintf("line-%d", total); got[len(got)-1] != wantLast {
		t.Errorf("last line is %q, want %q — the end of the run was dropped", got[len(got)-1], wantLast)
	}
	for _, line := range got {
		if line == "line-1" {
			t.Error("the head of the run survived truncation")
		}
	}
}

// TestReplayTailShortInputUnchanged: at or under the budget nothing is
// dropped. A truncation that fired early would silently eat short failures,
// which are the ones a reader can actually act on.
func TestReplayTailShortInputUnchanged(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"only newlines", "\n\n", ""},
		{"one line", "boom\n", "boom"},
		{"at the budget", strings.TrimSuffix(strings.Repeat("x\n", redProofReplayTailLines), "\n"), strings.TrimSuffix(strings.Repeat("x\n", redProofReplayTailLines), "\n")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := lastLines(tc.in, redProofReplayTailLines); got != tc.want {
				t.Errorf("lastLines(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestTrustReplayCheckSurfacesError: `--replay <id> --check` has three answers
// and they are not interchangeable. A store it could not READ is not a grant
// that was WITHHELD: reporting the first as the second would send an operator
// to run a grant command that will not fix anything.
func TestTrustReplayCheckSurfacesError(t *testing.T) {
	const line = "exit 3"

	t.Run("unconsented", func(t *testing.T) {
		replayFixture(t, line)
		err := runCmd(t, Trust(), "--replay", "proofy", "--check")
		if err == nil {
			t.Fatal("--check passed on a line this machine never granted")
		}
		if !errors.Is(err, ErrNoReplayConsent) {
			t.Errorf("err = %v, want ErrNoReplayConsent", err)
		}
		if !strings.Contains(err.Error(), "dross trust --replay proofy") {
			t.Errorf("the refusal does not name the grant command: %v", err)
		}
	})

	t.Run("granted", func(t *testing.T) {
		root, _, _ := replayFixture(t, line)
		if err := GrantReplayConsent(root, line); err != nil {
			t.Fatal(err)
		}
		if err := runCmd(t, Trust(), "--replay", "proofy", "--check"); err != nil {
			t.Errorf("--check refused a granted line: %v", err)
		}
	})

	t.Run("unreadable store", func(t *testing.T) {
		root, _, _ := replayFixture(t, line)
		mustWrite(t, localPath(root), "this is = = not toml\n")

		err := runCmd(t, Trust(), "--replay", "proofy", "--check")
		if err == nil {
			t.Fatal("--check passed over a store it could not read")
		}
		if !strings.Contains(err.Error(), LocalFile) {
			t.Errorf("the error does not name the store it failed to read: %v", err)
		}
		if strings.Contains(err.Error(), "Grant it with") {
			t.Errorf("an unreadable store was reported as a withheld grant: %v", err)
		}
	})
}
