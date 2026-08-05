package cmd

import (
	"strings"
	"sync"
	"testing"
)

// recordGitArgv installs a recorder on the exec seam for the duration of a test
// and returns an accessor for what git was actually asked to run.
//
// The assertion these tests make is about ORDERING, not about the builders.
// gitargs_test.go already proves gitRefArgs emits a separator; what it cannot
// prove is that a call site still uses it rather than having quietly gone back
// to a bare literal list — which is the regression this phase exists to
// prevent, and the only thing an argv recording can see.
func recordGitArgv(t *testing.T) func() [][]string {
	t.Helper()
	var mu sync.Mutex
	var seen [][]string
	prev := gitArgvRecorder
	gitArgvRecorder = func(args []string) {
		mu.Lock()
		defer mu.Unlock()
		cp := append([]string(nil), args...)
		seen = append(seen, cp)
	}
	t.Cleanup(func() { gitArgvRecorder = prev })
	return func() [][]string {
		mu.Lock()
		defer mu.Unlock()
		out := make([][]string, len(seen))
		copy(out, seen)
		return out
	}
}

func idx(argv []string, want string) int {
	for i, a := range argv {
		if a == want {
			return i
		}
	}
	return -1
}

// assertSeparatedBefore checks that every argv whose first element is sub
// places value after a separator. Which separator depends on the position:
// --end-of-options for refs, -- for pathspecs.
func assertSeparatedBefore(t *testing.T, argvs [][]string, sub, value, sep string) {
	t.Helper()
	found := 0
	for _, argv := range argvs {
		if len(argv) == 0 || argv[0] != sub {
			continue
		}
		v := idx(argv, value)
		if v < 0 {
			continue
		}
		found++
		s := idx(argv, sep)
		if s < 0 {
			t.Errorf("`git %s` carries %q with no %s separator at all: %q", sub, value, sep, argv)
			continue
		}
		if v < s {
			t.Errorf("`git %s` emits %q at index %d, BEFORE its %s separator at %d: %q",
				sub, value, v, sep, s, argv)
		}
	}
	if found == 0 {
		t.Errorf("no `git %s` invocation carried %q — the assertion never ran:\n%s",
			sub, value, formatArgvs(argvs))
	}
}

func formatArgvs(argvs [][]string) string {
	var b strings.Builder
	for _, a := range argvs {
		b.WriteString("  git " + strings.Join(a, " ") + "\n")
	}
	return b.String()
}

// TestPhaseCompleteArgvCarriesSeparator drives a real `dross phase complete`
// and asserts on the argv it produced. The phase branch name is
// config/phase-id-derived, so every positional carrying it must sit behind a
// separator — including the two that delete refs, where a misparse is not a
// failed lookup but a different command.
func TestPhaseCompleteArgvCarriesSeparator(t *testing.T) {
	dir := hostileRepo(t, "main")
	seen := recordGitArgv(t)

	// A phase branch to complete. The command will refuse somewhere down the
	// line (no PR, no origin record) — that is fine and deliberate: the
	// assertion is on the argv produced along the way, not on the verdict.
	mustGit(t, dir, "checkout", "-q", "-b", "phase/01-x")
	mustWrite(t, dir+"/.dross/phases/01-x/spec.toml", "[phase]\nid = \"01-x\"\n")
	mustWrite(t, dir+"/.dross/phases/01-x/changes.json", `{"phase":"01-x","base":"main"}`)
	mustGit(t, dir, "add", ".")
	mustGit(t, dir, "commit", "-q", "-m", "feat: phase work")

	_ = runCmd(t, Phase(), "complete", "01-x")

	argvs := seen()
	if len(argvs) == 0 {
		t.Fatal("the recorder saw no git invocations — the seam is not wired")
	}
	// The ancestry probe is the one config-derived ref pair this flow reaches
	// before refusing; both operands are phase-id-derived.
	assertSeparatedBefore(t, argvs, "merge-base", "origin/phase/01-x", "--end-of-options")
	assertSeparatedBefore(t, argvs, "merge-base", "origin/main", "--end-of-options")
}

// TestPhaseCreateArgvCarriesSeparator covers the rev-parse ref probe, which
// `phase complete` refuses before reaching. Split out rather than asserted
// hopefully inside the other test: an assertion whose argv never appears is a
// test that silently stops testing.
func TestPhaseCreateArgvCarriesSeparator(t *testing.T) {
	hostileRepo(t, "main")
	seen := recordGitArgv(t)

	if err := runCmd(t, Phase(), "create", "auth middleware"); err != nil {
		t.Fatalf("phase create: %v", err)
	}

	argvs := seen()
	found := false
	for _, argv := range argvs {
		if len(argv) > 0 && argv[0] == "rev-parse" {
			for _, a := range argv {
				if strings.HasPrefix(a, "refs/heads/phase/") {
					found = true
					assertSeparatedBefore(t, [][]string{argv}, "rev-parse", a, "--end-of-options")
				}
			}
		}
	}
	if !found {
		t.Errorf("no rev-parse of a phase ref was recorded:\n%s", formatArgvs(argvs))
	}
}

// TestGuardLiveStateRefPathSeparator covers the two probes inside the guard
// itself. The cat-file one is the interesting shape: "<ref>:<path>" is a single
// positional, and a caller-derived ref is still a flag to git even with a
// ":path" glued to it.
func TestGuardLiveStateRefPathSeparator(t *testing.T) {
	dir, legacy := legacyStateBranchFixture(t)
	seen := recordGitArgv(t)

	if err := guardLiveState(dir, legacy); err == nil {
		t.Fatal("precondition: the legacy branch must trip the guard")
	}

	argvs := seen()
	assertSeparatedBefore(t, argvs, "cat-file", legacy+":.dross/state.json", "--end-of-options")
	// The ls-files probe is a PATHSPEC, so it takes "--" — not the ref
	// separator. Using the wrong one here would look like a fix and change what
	// git is being asked.
	assertSeparatedBefore(t, argvs, "ls-files", ".dross/state.json", "--")
}

// TestSwitchHelpersArgvCarriesSeparator pins the four guarded switch helpers.
// They are driven directly rather than through a command, because each is a
// distinct call site and a command exercises at most one of them.
func TestSwitchHelpersArgvCarriesSeparator(t *testing.T) {
	dir, _ := legacyStateBranchFixture(t)
	mustGit(t, dir, "checkout", "-q", "-b", "target")
	mustGit(t, dir, "checkout", "-q", "main")

	t.Run("checkoutBranch", func(t *testing.T) {
		seen := recordGitArgv(t)
		_ = checkoutBranch(dir, "target")
		assertSeparatedBefore(t, seen(), "checkout", "target", "--end-of-options")
	})
	t.Run("checkoutBranchNew", func(t *testing.T) {
		seen := recordGitArgv(t)
		_ = checkoutBranchNew(dir, "fresh-branch", "main")
		// The BASE is the positional; the new name is -b's argument and cannot
		// be fenced by a separator (one placed in front of it would become the
		// branch name). validateGitRef is what guards that half.
		assertSeparatedBefore(t, seen(), "checkout", "main", "--end-of-options")
	})
	t.Run("guardedFF", func(t *testing.T) {
		seen := recordGitArgv(t)
		_, _ = guardedFF(dir, "target")
		assertSeparatedBefore(t, seen(), "merge", "target", "--end-of-options")
	})
	t.Run("guardedResetHard", func(t *testing.T) {
		seen := recordGitArgv(t)
		_, _ = guardedResetHard(dir, "target")
		assertSeparatedBefore(t, seen(), "reset", "target", "--end-of-options")
	})
}
