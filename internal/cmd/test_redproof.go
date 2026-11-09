package cmd

// `dross test --red-proof` — showing that a task's tests fail without its fix.
//
// A solo run has nobody who can show it. The writer holds the fix in the
// working tree and may not stash it, and the main loop may not edit the task's
// files at all, so "revert the fix and watch the test go red" has no one to do
// it. This answers the question without reverting anything: the named tests
// run against a scratch copy of HEAD — the code as it stood before the task —
// with only those test files laid over it from the working tree.
//
// Red there is the proof, and exits 0: the tests need the working tree's fix to
// pass. Green is not, and exits 1 — this mode's ordinary failure — because a
// test that passes without the fix says nothing about it. A run that never
// happened keeps its own exit in the 2–8 band and is never read as either.
//
// The scratch copy is built from HEAD's tree through a scratch index and
// checkout-index, never as a git worktree: a worktree's .git is a FILE pointing
// back into this repo, and pushing it to a granted host would replace the
// host's .git directory with a dangling pointer. The working tree, the real
// index, green.json and scoped.json are never written — a red-proof measures a
// tree no commit will ever record, so nothing it learns may vouch for one.

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/Rivil/dross/internal/gitrun"
	"github.com/Rivil/dross/internal/pathfence"
	"github.com/Rivil/dross/internal/project"
	"github.com/Rivil/dross/internal/remote"
	"github.com/Rivil/dross/internal/verify"
)

// runRedProof is `dross test --red-proof --files <tests>`: the run, then its
// verdict, printed as one `red-proof:` line a writer's report can quote.
func runRedProof(tr *testRun, root, repoDir string, proj *project.Project, files []string, local bool, wait time.Duration) error {
	runErr := redProofRun(tr, root, repoDir, proj, files, local, wait)
	tr.finish(runErr)
	line, err := redProofVerdict(runErr)
	Print(line)
	return err
}

// redProofRun runs the named tests at base the way a --files run would run them
// on the working tree: the lanes they hit, on the same site, through the same
// grants — with no recorder anywhere on the path.
func redProofRun(tr *testRun, root, repoDir string, proj *project.Project, files []string, local bool, wait time.Duration) error {
	tests, err := redProofTests(repoDir, files)
	if err != nil {
		return err
	}
	// A lane-less repo's --files run is its whole suite, so its red-proof is
	// too. The consent gate comes before the scratch copy is built, as it comes
	// before any spawn on the ordinary path.
	laneless := len(proj.Runtime.TestLane) == 0
	if laneless {
		if err := requireExecConsent(); err != nil {
			// Tagged as the refused lane it amounts to: untagged, it would exit
			// 1, which in this mode says the tests ran and passed.
			return &ExitCodeError{Code: exitLaneRefused, Err: err}
		}
	}
	scratch, err := buildRedProofTree(repoDir, tests)
	if err != nil {
		return err
	}
	defer scratch.close()
	tr.say("base: HEAD %s, with %s laid over it from the working tree\n", short(scratch.head), strings.Join(tests, " "))
	tree := scratch.runTree()
	if laneless {
		line := testCommandLine(proj.Runtime.TestCommand, nil)
		return runTest(tr, root, repoDir, tree, proj.Project.Name, line, local, wait, nil)
	}
	plan := lanePlan(scratch.dir, proj, tests)
	if err := refuseLanePlan(tr, proj, plan); err != nil {
		return err
	}
	// A throwaway scopedRun and no sites: what the lanes measured here is about
	// HEAD's code, and nothing may carry it to a record.
	return runPlannedLanes(tr, root, repoDir, tree, proj, redProofLanes(plan.Lanes), local, wait, nil, &scopedRun{})
}

// redProofVerdict turns the run's outcome into the proof's: the line to print,
// and the error the command exits with.
//
// Only a red suite proves anything, and it exits 0, because a proven red is
// this mode's success. A red whose runner was never found (126 or 127 from the
// shell) is not a test failing: in a scratch tree that lacks something the
// working tree had, it is the likeliest red of all, and reading it as proof
// would be the tool manufacturing the evidence it was asked to check for.
func redProofVerdict(runErr error) (string, error) {
	if isRed(runErr) {
		var ec exitCoder
		if errors.As(runErr, &ec) && (ec.ExitCode() == 126 || ec.ExitCode() == 127) {
			err := &ExitCodeError{Code: exitToolchainMissing, Err: fmt.Errorf(
				"the runner was not found at base (shell exit %d), so no test ran: %w", ec.ExitCode(), runErr)}
			return fmt.Sprintf("red-proof: not run (exit %d)", exitToolchainMissing), err
		}
		return "red-proof: proven (red at base)", nil
	}
	if runErr == nil {
		return "red-proof: not proven (green at base)", &ExitCodeError{Code: exitSuiteFailed, Err: errors.New(
			"not proven: the named tests pass at HEAD without the working tree's fix, so they do not show the fix is needed")}
	}
	// An untagged error — a malformed lane, an argv the transport refused —
	// would exit 1, which in this mode is a claim that the tests ran green. It
	// is tagged as the run that did not happen.
	var tagged *ExitCodeError
	if !errors.As(runErr, &tagged) {
		runErr = &ExitCodeError{Code: exitPrepareFailed, Err: fmt.Errorf("the red-proof did not run: %w", runErr)}
	}
	return fmt.Sprintf("red-proof: not run (exit %d)", ExitCode(runErr)), runErr
}

// redProofLanes runs each vouching lane straight on the tests it was handed.
//
// A vouching lane's relate exists to find the tests that reach a changed source
// path. Here every path already IS a test, so its line is built from them
// directly, exactly as it would be from relate's answer. With no relate in the
// run, no relate output can reach the throwaway scopedRun either.
func redProofLanes(lanes []plannedLane) []plannedLane {
	out := make([]plannedLane, 0, len(lanes))
	for _, pl := range lanes {
		if pl.Relate != "" {
			var ok bool
			pl.Line, pl.Selector, ok = laneBatchLine(pl.lane, pl.Asked)
			pl.Relate, pl.ScopedToNothing = "", !ok
		}
		out = append(out, pl)
	}
	return out
}

// redProofTests checks the --files set and returns it as normalized
// repo-relative test paths.
//
// Every path must be a test file present in the working tree. A source path is
// refused rather than skipped: laying it over HEAD would run the fix itself, and
// a red-proof that carried the fix would prove nothing while still saying
// "proven" about the paths that were tests.
func redProofTests(repoDir string, files []string) ([]string, error) {
	if len(files) == 0 {
		return nil, &ExitCodeError{Code: exitBadFileSet, Err: errors.New(
			"--red-proof needs the tests to prove: --files <test path> (repeatable), one per test file the task added or changed")}
	}
	var tests, outside, source, missing []string
	seen := map[string]bool{}
	for _, raw := range files {
		norm, in := pathfence.InTree(raw)
		if !in || norm == "" || norm == "." {
			outside = append(outside, raw)
			continue
		}
		if seen[norm] {
			continue
		}
		seen[norm] = true
		if !isTestPath(norm) {
			source = append(source, raw)
			continue
		}
		// Lstat, not Stat: a symlinked test would be laid over HEAD as the
		// link, pointing at whatever the working tree holds.
		if info, err := os.Lstat(filepath.Join(repoDir, filepath.FromSlash(norm))); err != nil || !info.Mode().IsRegular() {
			missing = append(missing, raw)
			continue
		}
		tests = append(tests, norm)
	}
	switch {
	case len(outside) > 0:
		return nil, &ExitCodeError{Code: exitBadFileSet, Err: fmt.Errorf(
			"refusing to red-proof: these paths are not inside this repository — %s", strings.Join(outside, " "))}
	case len(source) > 0:
		return nil, &ExitCodeError{Code: exitBadFileSet, Err: fmt.Errorf(
			"refusing to red-proof: these are not test files — %s\n\n"+
				"A red-proof lays only tests over HEAD's code. Laying source over it would run the\n"+
				"fix itself, which proves nothing. Name the test files the task added or changed.",
			strings.Join(source, " "))}
	case len(missing) > 0:
		return nil, &ExitCodeError{Code: exitBadFileSet, Err: fmt.Errorf(
			"refusing to red-proof: these are not regular files in the working tree — %s\n\n"+
				"A red-proof lays the working tree's copy of each test over HEAD, so a deleted\n"+
				"test, a directory or a symlink has nothing of its own to lay.",
			strings.Join(missing, " "))}
	}
	return tests, nil
}

// testPathDirs are directories whose contents are test-only by the convention of
// every runner dross lanes front: a fixture or helper there is no product code.
// Go's fixture directory is verify.IsTestdataPath's, so the rule has one home.
var testPathDirs = map[string]bool{"__tests__": true, "test": true, "tests": true}

// isTestPath reports whether a normalized repo-relative path is a test file by
// name: Go's _test.go and its kin in other languages (foo_test.dart, foo_spec.rb),
// pytest's test_*.py and conftest.py, the JS family's foo.test.ts and
// foo.spec.tsx (any extension, snapshots included), JVM FooTest and FooTests,
// and anything under a test-only directory.
//
// It reads names only. Whether the file holds a test is the runner's question;
// this one only has to keep source off the scratch tree.
func isTestPath(norm string) bool {
	if verify.IsTestdataPath(norm) {
		return true
	}
	segs := strings.Split(norm, "/")
	for _, d := range segs[:len(segs)-1] {
		if testPathDirs[d] {
			return true
		}
	}
	base := segs[len(segs)-1]
	ext := path.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	switch {
	case strings.HasSuffix(stem, "_test"), strings.HasSuffix(stem, "_spec"):
		return true
	case ext == ".py" && (strings.HasPrefix(base, "test_") || base == "conftest.py"):
		return true
	case (ext == ".kt" || ext == ".java" || ext == ".scala") && (strings.HasSuffix(stem, "Test") || strings.HasSuffix(stem, "Tests")):
		return true
	}
	// The middle parts only: a file called test.ts or spec.md is not one.
	parts := strings.Split(base, ".")
	if len(parts) < 3 {
		return false
	}
	for _, p := range parts[1 : len(parts)-1] {
		if p == "test" || p == "spec" {
			return true
		}
	}
	return false
}

// redProofTree is the scratch copy a red-proof runs on.
type redProofTree struct {
	// base holds everything below and is removed whole by close.
	base string
	// dir is the dross root inside the copy — what lines spawn in and what is
	// pushed — which is a subdirectory of it when .dross sits below the git
	// work tree's top.
	dir string
	// head is the commit the copy is of.
	head string
	// ignored are the paths the working tree ignores, repo-relative, so a push
	// neither sends one nor deletes the host's own.
	ignored []string
}

// buildRedProofTree checks HEAD's tree out into a scratch directory and lays
// the named tests over it from the working tree.
//
// Every failure is exitPrepareFailed: the tree to run on could not be built, so
// nothing ran, and that must never read as a red suite.
func buildRedProofTree(repoDir string, tests []string) (_ *redProofTree, err error) {
	head, err := gitrun.Trim(repoDir, gitRefArgs("rev-parse", []string{"--verify", "--quiet"}, "HEAD^{commit}")...)
	if err != nil || head == "" {
		return nil, redProofNotBuilt(errors.New("HEAD names no commit, so there is no base to prove against"))
	}
	out, err := gitrun.Read(repoDir, "rev-parse", "--show-prefix")
	if err != nil {
		return nil, redProofNotBuilt(fmt.Errorf("could not place the dross root in its git work tree: %w", err))
	}
	//dross:taint-cleared rev-parse --show-prefix prints where the dross root sits inside the git work tree, a directory git derives from the cwd and never file content
	prefix := filepath.FromSlash(strings.TrimSuffix(out, "/"))
	if prefix != "" && !filepath.IsLocal(prefix) {
		return nil, redProofNotBuilt(fmt.Errorf("the dross root's place in its git work tree is not a relative path: %q", prefix))
	}

	base, err := os.MkdirTemp("", "dross-redproof-")
	if err != nil {
		return nil, redProofNotBuilt(err)
	}
	s := &redProofTree{base: base, head: head}
	defer func() {
		if err != nil {
			s.close()
		}
	}()

	// A scratch index, never the real one: read-tree into the index the user
	// stages into would replace what they staged with HEAD. The tests go into
	// it from the working tree, over HEAD's entries where they have one, so the
	// one checkout writes HEAD and the overlay together — modes included, and
	// with no copy of a file made by hand. checkout-index run from the dross
	// root writes only the entries below it, each at its full path under the
	// prefix, which is why dir is top joined with the prefix.
	top := filepath.Join(base, "tree")
	opts := gitrun.Options{IndexFile: filepath.Join(base, "index")}
	if _, err = gitrun.RawWith(opts, repoDir, gitRefArgs("read-tree", nil, head)...); err != nil {
		return nil, redProofNotBuilt(fmt.Errorf("could not read HEAD's tree: %w", err))
	}
	if _, err = gitrun.RawWith(opts, repoDir, gitPathArgs("update-index", []string{"--add"}, tests...)...); err != nil {
		return nil, redProofNotBuilt(fmt.Errorf("could not lay the tests over HEAD: %w", err))
	}
	if _, err = gitrun.RawWith(opts, repoDir, "checkout-index", "--all", "--force", "--prefix="+top+"/"); err != nil {
		return nil, redProofNotBuilt(fmt.Errorf("could not check HEAD's tree out: %w", err))
	}
	s.dir = filepath.Join(top, prefix)
	if err = os.MkdirAll(s.dir, 0o755); err != nil {
		return nil, redProofNotBuilt(err)
	}
	if err = s.linkIgnored(repoDir); err != nil {
		return nil, redProofNotBuilt(err)
	}
	return s, nil
}

// redProofNotBuilt tags a failure to build the scratch tree.
func redProofNotBuilt(err error) error {
	return &ExitCodeError{Code: exitPrepareFailed, Err: fmt.Errorf(
		"red-proof could not build HEAD's tree to run on, so nothing ran: %w", err)}
}

// linkIgnored gives the copy the working tree's ignored paths — node_modules, a
// virtualenv, build output — as symlinks, and records them for the push.
//
// HEAD's tree holds only what git tracks, and a runner whose dependencies are
// missing exits non-zero exactly like a failing test: the false proof this mode
// most has to avoid. Linked, a local run sees what a run in the working tree
// sees. A granted host keeps its own copies across every sync, and excluding
// the same paths keeps the links off the wire and the host's copies safe from
// --delete.
//
// .dross/ is excluded but never linked: dross's own records are not the suite's
// environment, and a link would let a run write through into green.json.
func (s *redProofTree) linkIgnored(repoDir string) error {
	abs, err := filepath.Abs(repoDir)
	if err != nil {
		return err
	}
	out, err := gitrun.Raw(repoDir, "ls-files",
		"--others", "--ignored", "--exclude-standard",
		"--directory", "--no-empty-directory", "-z")
	if err != nil {
		return fmt.Errorf("listing ignored paths in %s: %w", repoDir, err)
	}
	var linked []string
	//dross:taint-cleared git ls-files -z --others --ignored prints NUL-separated paths the repo ignores; each entry becomes one symlink and one anchored rsync exclude, and nothing else of git's output is kept
	for _, p := range strings.Split(out, "\x00") {
		name, in := pathfence.InTree(strings.TrimSuffix(p, "/"))
		if name == "" || !in || name == "." {
			continue
		}
		s.ignored = append(s.ignored, name)
		if name == RootDirName || strings.HasPrefix(name, RootDirName+"/") || underAny(name, linked) {
			continue
		}
		link := filepath.Join(s.dir, filepath.FromSlash(name))
		if _, err := os.Lstat(link); err == nil {
			continue
		}
		// Recorded before the link exists: a later entry beneath it must never
		// reach MkdirAll, which would follow the link into the working tree.
		linked = append(linked, name)
		if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
			return err
		}
		if err := os.Symlink(filepath.Join(abs, filepath.FromSlash(name)), link); err != nil {
			return err
		}
	}
	return nil
}

// underAny reports whether name lies beneath one of dirs.
func underAny(name string, dirs []string) bool {
	for _, d := range dirs {
		if strings.HasPrefix(name, d+"/") {
			return true
		}
	}
	return false
}

// runTree is the copy as a run's tree: lines spawn in it, and a push sends it
// with the red-proof's filters.
func (s *redProofTree) runTree() runTree {
	return runTree{dir: s.dir, sync: func(tr *testRun, t remote.Target) error {
		return syncTreeTo(tr, t, s.dir, redProofSyncFilters(s.ignored)...)
	}}
}

// redProofSyncFilters are the rules a push of the copy adds to SyncArgs' own.
//
// The copy has no .git, and an ordinary sync sends the working tree's .git on
// purpose (see remote.SyncArgs), so without a protect rule --delete would remove
// the host's. The excludes stand in for the git-derived list SyncArgs cannot
// build for a directory that is not a work tree. Each is anchored at the root,
// so a bare name cannot match elsewhere, and carries no trailing slash: in the
// copy a linked directory is a symlink, which a directory-only pattern would
// let through to replace the host's directory.
func redProofSyncFilters(ignored []string) []string {
	out := []string{"--filter=P /.git"}
	for _, name := range ignored {
		out = append(out, "--exclude=/"+name)
	}
	return out
}

// close removes the copy. The links in it go with it; what they point at stays.
func (s *redProofTree) close() {
	_ = os.RemoveAll(s.base)
}
