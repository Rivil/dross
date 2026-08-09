package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/project"
	"github.com/Rivil/dross/internal/state"
)

// TestRepairStateRefusesOutputInjection is the live vector, tested by its
// SIDE EFFECT rather than by its message.
//
// `git log <mainBranch> …` with a mainBranch of "--output=<path>" is not a
// lookup that fails — git's diff options accept --output=<file> on log, so it
// writes the command's output there. Cloning a repo and running `dross repair`
// was enough to write a file of the repo author's choosing.
//
// The assertion that matters is that the file does not exist afterwards. An
// error-string assertion alone would still pass if the guard moved to after the
// exec, which is exactly when the file gets written.
func TestRepairStateRefusesOutputInjection(t *testing.T) {
	sentinel := filepath.Join(t.TempDir(), "dross-pwned")
	dir := hostileRepo(t, "--output="+sentinel)

	// Sanity: the payload really is in the committed config repair will read.
	p, err := project.Load(filepath.Join(dir, ".dross", project.File))
	if err != nil {
		t.Fatal(err)
	}
	if p.Repo.GitMainBranch != "--output="+sentinel {
		t.Fatalf("fixture did not carry the payload: %q", p.Repo.GitMainBranch)
	}

	// The verdict is irrelevant — repair may well refuse for several reasons.
	// What must hold is that nothing was written.
	_ = runCmd(t, Repair())

	if _, err := os.Stat(sentinel); err == nil {
		t.Fatalf("`dross repair` wrote %s from committed config — the injection landed", sentinel)
	} else if !os.IsNotExist(err) {
		t.Fatalf("stat %s: %v", sentinel, err)
	}
}

// TestRepairFilesRefusesDashRef: `checkout <ref> -- <path>` restores files, so
// a ref read as a flag here is a write, not a failed read.
func TestRepairFilesRefusesDashRef(t *testing.T) {
	dir := t.TempDir()
	err := restorePathFromRef(dir, "-x", ".dross/")
	if err == nil {
		t.Fatal("an option-shaped ref was accepted")
	}
	if !strings.Contains(err.Error(), "unsafe git ref") {
		t.Errorf("not the guard's refusal: %v", err)
	}
	if strings.Contains(err.Error(), "not a git repository") {
		t.Errorf("git ran before the guard: %v", err)
	}
}

// TestPhaseRenameArgvCarriesSeparator: `branch -m <old> <new>` carries two
// slug-derived names, both positionals, both behind the separator.
func TestPhaseRenameArgvCarriesSeparator(t *testing.T) {
	dir := hostileRepo(t, "main")
	if err := runCmd(t, Phase(), "create", "auth middleware"); err != nil {
		t.Fatalf("phase create: %v", err)
	}
	st, err := state.Load(filepath.Join(dir, ".dross", state.File))
	if err != nil {
		t.Fatal(err)
	}
	oldID := st.CurrentPhase

	// Lifecycle commands resolve the phases array through the current
	// milestone. It is written directly rather than via `milestone create`,
	// which cuts its branch from main BEFORE the .dross commit lands — so the
	// phase's own checkout would take the toml straight back out again.
	mustWrite(t, filepath.Join(dir, ".dross", "milestones", "v9.9.toml"),
		// phases is a TOP-LEVEL key, so it precedes the table header — after
		// it, TOML would absorb it into [milestone] and the array reads empty.
		"phases = [\""+oldID+"\"]\n\n[milestone]\n  version = \"v9.9\"\n")
	st.CurrentMilestone = "v9.9"
	if err := st.Save(filepath.Join(dir, ".dross", state.File)); err != nil {
		t.Fatal(err)
	}

	seen := recordGitArgv(t)
	if err := runCmd(t, Phase(), "rename", oldID, "renamed-thing"); err != nil {
		t.Fatalf("phase rename: %v", err)
	}

	argvs := seen()
	found := false
	for _, argv := range argvs {
		if len(argv) > 1 && argv[0] == "branch" && idx(argv, "-m") >= 0 {
			found = true
			sep := idx(argv, "--end-of-options")
			if sep < 0 {
				t.Errorf("`git branch -m` carries no separator: %q", argv)
				continue
			}
			// -m itself is an option and must stay ahead of the fence; both
			// names are positionals and must sit behind it.
			if m := idx(argv, "-m"); m > sep {
				t.Errorf("-m is behind the separator, so git reads it as a branch name: %q", argv)
			}
			if len(argv) < sep+3 {
				t.Errorf("expected two names after the separator: %q", argv)
			}
		}
	}
	if !found {
		t.Errorf("no `git branch -m` invocation was recorded:\n%s", formatArgvs(argvs))
	}
}

// TestTopologyArgvCarriesSeparator: the `<Main>..<Work>` range is reached by
// `dross status` and by `dross phase complete`, and both of its ends come from
// config or from the current branch.
func TestTopologyArgvCarriesSeparator(t *testing.T) {
	dir := hostileRepo(t, "main")
	mustGit(t, dir, "checkout", "-q", "-b", "phase/01-x")
	mustGit(t, dir, "commit", "-q", "--allow-empty", "-m", "feat: work")

	seen := recordGitArgv(t)
	// workOverride is passed explicitly: with no milestone active the inferred
	// work base IS main, so OnMain short-circuits the range and the assertion
	// would never run.
	if _, err := branchTopology(dir, filepath.Join(dir, ".dross"), "phase/01-x"); err != nil {
		t.Fatalf("branchTopology: %v", err)
	}

	argvs := seen()
	found := false
	for _, argv := range argvs {
		if len(argv) > 0 && argv[0] != "rev-list" {
			continue
		}
		for _, a := range argv {
			if strings.Contains(a, "..") {
				found = true
				assertSeparatedBefore(t, [][]string{argv}, "rev-list", a, "--end-of-options")
			}
		}
	}
	if !found {
		t.Errorf("no rev-list range was recorded:\n%s", formatArgvs(argvs))
	}
}

// TestRemainingGitCallsCarrySeparator sweeps the argv produced by the commands
// that reach the rest of the rewritten sites. It asserts a package-wide
// property rather than one site at a time: no invocation may place a
// non-literal-looking positional ahead of its separator.
//
// The value it adds over the per-site tests is coverage of sites nobody thought
// to name — which is the failure mode both plan reviews of this phase hit, each
// finding call sites the inventory had missed.
func TestRemainingGitCallsCarrySeparator(t *testing.T) {
	dir := hostileRepo(t, "main")
	mustGit(t, dir, "checkout", "-q", "-b", "phase/01-x")
	mustWrite(t, filepath.Join(dir, ".dross", "phases", "01-x", "spec.toml"), "[phase]\nid = \"01-x\"\n")
	mustGit(t, dir, "add", ".")
	mustGit(t, dir, "commit", "-q", "-m", "feat: work")

	seen := recordGitArgv(t)
	// Every command that reaches a rewritten site. Verdicts are ignored —
	// several will legitimately refuse — because the subject is the argv.
	_ = runCmd(t, Status())
	_ = runCmd(t, Repair())
	_ = runCmd(t, Doctor())
	_ = runCmd(t, Milestone(), "create", "v9.9")
	_ = runCmd(t, Phase(), "complete", "01-x")

	argvs := seen()
	if len(argvs) == 0 {
		t.Fatal("no git invocations recorded — the seam is not wired")
	}

	// Subcommands whose positionals are refs or paths dross derives. `fetch
	// origin`, `status --porcelain` and friends take only literals and are not
	// listed: demanding a separator there would be noise, and noise is what
	// gets an audit deleted.
	derived := map[string]bool{
		"branch": true, "merge-base": true, "rev-list": true,
		"ls-remote": true, "cat-file": true, "ls-tree": true, "merge": true,
		"reset": true, "diff-tree": true,
	}
	for _, argv := range argvs {
		if len(argv) == 0 || !derived[argv[0]] {
			continue
		}
		if idx(argv, "--end-of-options") < 0 && idx(argv, "--") < 0 {
			t.Errorf("`git %s` was built without a separator: %q", argv[0], argv)
		}
	}
}
