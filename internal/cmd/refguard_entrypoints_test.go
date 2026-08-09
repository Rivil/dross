package cmd

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/project"
	"github.com/Rivil/dross/internal/state"
)

// The four entry points where a ref is produced from committed config or from
// argv. Each gets its own test rather than one table over validateGitRef,
// because the failure this guards against is not "the helper stopped working" —
// it is "someone added a code path that forgot to call it". A shared table
// passes happily while three of the four call sites are gone.
//
// Every test asserts the refusal AND that nothing moved: the guard is only
// worth anything if it lands before the mutation, and an assertion on the error
// string alone cannot tell the difference.

// hostileRepo builds a repo whose committed .dross/project.toml carries the
// payload, the way a cloned hostile repo would. The value is written straight
// into the file rather than through `dross project set`, because that is how it
// arrives in reality — nobody types it.
func hostileRepo(t *testing.T, mainBranch string) string {
	t.Helper()
	dir := t.TempDir()
	remote := t.TempDir()
	mustGit(t, remote, "init", "-q", "--bare", "-b", "main")
	gitInit(t, dir, remote)
	chdir(t, dir)

	if err := runCmd(t, Init()); err != nil {
		t.Fatalf("init: %v", err)
	}
	root := filepath.Join(dir, ".dross")
	ppath := filepath.Join(root, project.File)
	p, err := project.Load(ppath)
	if err != nil {
		t.Fatal(err)
	}
	p.Repo.GitMainBranch = mainBranch
	if err := p.Save(ppath); err != nil {
		t.Fatal(err)
	}

	mustWrite(t, filepath.Join(dir, "README.md"), "base\n")
	mustGit(t, dir, "add", ".")
	mustGit(t, dir, "commit", "-q", "-m", "chore: baseline")
	return dir
}

// dashPayload is the live shape, not a toy: `git log --output=<file>` writes
// git's output wherever it is pointed, so this exact string in
// [repo].git_main_branch is an arbitrary-file write, not a lookup failure.
const dashPayload = "--output=/tmp/dross-pwned-entrypoints"

func assertRefusedRef(t *testing.T, err error, kind string) {
	t.Helper()
	if err == nil {
		t.Fatal("command accepted a leading-dash ref, want a refusal")
	}
	msg := err.Error()
	if !strings.Contains(msg, "unsafe git ref") {
		t.Fatalf("not the guard's refusal: %v", err)
	}
	if !strings.Contains(msg, kind) {
		t.Errorf("refusal does not name %q: %v", kind, err)
	}
	if !strings.Contains(msg, dashPayload) {
		t.Errorf("refusal does not name the payload: %v", err)
	}
}

// TestPhaseCompleteRefusesDashMainBranch: resolveCompleteBase runs before the
// verify heal, the .dross auto-commit, the fetch and the branch switch, so the
// refusal must leave HEAD and the branch list exactly as it found them.
func TestPhaseCompleteRefusesDashMainBranch(t *testing.T) {
	dir := hostileRepo(t, dashPayload)
	root := filepath.Join(dir, ".dross")

	st, err := state.Load(filepath.Join(root, state.File))
	if err != nil {
		t.Fatal(err)
	}
	st.CurrentPhase = "01-x"
	if err := st.Save(filepath.Join(root, state.File)); err != nil {
		t.Fatal(err)
	}

	headBefore := mustGit(t, dir, "rev-parse", "HEAD")
	branchesBefore := mustGit(t, dir, "branch", "--list")

	assertRefusedRef(t, runCmd(t, Phase(), "complete", "01-x"), "repo.git_main_branch")

	if got := mustGit(t, dir, "rev-parse", "HEAD"); got != headBefore {
		t.Errorf("HEAD moved despite the refusal: %s -> %s", headBefore, got)
	}
	if got := mustGit(t, dir, "branch", "--list"); got != branchesBefore {
		t.Errorf("the branch list changed despite the refusal:\nbefore %q\nafter  %q", branchesBefore, got)
	}
}

// TestPhaseCheckoutRefusesDashArg: reaching the "no local branch" message would
// prove the ref probe already ran, so the guard must precede it. The argument
// is checked unprefixed — "phase/" would make the dash unreachable and the
// check vacuous.
func TestPhaseCheckoutRefusesDashArg(t *testing.T) {
	hostileRepo(t, "main")

	// "--" so cobra hands the payload through as a positional rather than
	// trying to parse it as a flag — the shape a hostile id actually arrives in.
	err := runCmd(t, Phase(), "checkout", "--", dashPayload)
	assertRefusedRef(t, err, "phase id argument")
	if strings.Contains(err.Error(), "no local branch") {
		t.Errorf("git ran before the guard — the refusal is the ref-probe message: %v", err)
	}
}

// TestMilestoneCreateRefusesDashMainBranch: the cut point reaches
// `git branch <new> <base>` as a bare positional, so a refusal that arrives
// late leaves a new ref behind. Asserting on `git branch --list` is what
// catches that; the error string alone would not.
func TestMilestoneCreateRefusesDashMainBranch(t *testing.T) {
	dir := hostileRepo(t, dashPayload)
	before := mustGit(t, dir, "branch", "--list")

	assertRefusedRef(t, runCmd(t, Milestone(), "create", "v9.9"), "repo.git_main_branch")

	if got := mustGit(t, dir, "branch", "--list"); got != before {
		t.Errorf("a branch was created despite the refusal:\nbefore %q\nafter  %q", before, got)
	}
}

// TestShipRecoverRefusesDashMainBranch: `ship recover` ends in a `reset --hard`,
// so this is the entry point where a late refusal is most expensive. Against
// the hostile config it must return the guard error rather than today's
// "must be on <branch>" message, which would prove the branch read already ran.
func TestShipRecoverRefusesDashMainBranch(t *testing.T) {
	dir := hostileRepo(t, dashPayload)
	root := filepath.Join(dir, ".dross")

	st, err := state.Load(filepath.Join(root, state.File))
	if err != nil {
		t.Fatal(err)
	}
	st.CurrentPhase = "01-x"
	if err := st.Save(filepath.Join(root, state.File)); err != nil {
		t.Fatal(err)
	}
	headBefore := mustGit(t, dir, "rev-parse", "HEAD")

	rerr := runCmd(t, Ship(), "recover")
	assertRefusedRef(t, rerr, "repo.git_main_branch")
	if strings.Contains(rerr.Error(), "must be on") {
		t.Errorf("the branch check ran before the guard: %v", rerr)
	}
	if got := mustGit(t, dir, "rev-parse", "HEAD"); got != headBefore {
		t.Errorf("HEAD moved despite the refusal: %s -> %s", headBefore, got)
	}
}
