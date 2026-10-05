package treefp

import (
	"crypto/sha256"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
)

// git runs a real git in dir for the test's own setup.
func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	c := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := c.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

func write(t *testing.T, dir, rel, body string) {
	t.Helper()
	p := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func initRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	dir := t.TempDir()
	git(t, dir, "init", "-q", "-b", "main")
	for _, kv := range [][2]string{{"user.email", "t@example.com"}, {"user.name", "T"}, {"commit.gpgsign", "false"}, {"gc.auto", "0"}} {
		git(t, dir, "config", kv[0], kv[1])
	}
	return dir
}

// repo has a.go, b.go, a .gitignore for *.log and a tracked .dross/state.json,
// all committed.
func repo(t *testing.T) string {
	t.Helper()
	dir := initRepo(t)
	write(t, dir, "a.go", "package a\n")
	write(t, dir, "b.go", "package b\n")
	write(t, dir, ".gitignore", "*.log\n")
	write(t, dir, ".dross/state.json", "{}\n")
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-q", "-m", "init")
	return dir
}

func mustTree(t *testing.T, dir string) string {
	t.Helper()
	tree, err := WorkingTree(dir)
	if err != nil || tree == "" {
		t.Fatalf("WorkingTree = %q, %v", tree, err)
	}
	return tree
}

func mustMeasured(t *testing.T, dir string) Measured {
	t.Helper()
	m, err := MeasuredTree(dir)
	if err != nil || m.Tree == "" {
		t.Fatalf("MeasuredTree = %+v, %v", m, err)
	}
	return m
}

func mustCandidate(t *testing.T, dir string, adds []Add, all bool) Snapshot {
	t.Helper()
	snap, err := Candidate(dir, adds, all)
	if err != nil || snap.Tree == "" {
		t.Fatalf("Candidate = %+v, %v", snap, err)
	}
	return snap
}

func indexState(t *testing.T, dir string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, ".git", "index"))
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(b)
	return string(sum[:]) + git(t, dir, "diff", "--cached", "--name-only")
}

// TestRealIndexUntouched: every recipe works on a copy. The real index's bytes
// and what it stages are the same before and after.
func TestRealIndexUntouched(t *testing.T) {
	dir := repo(t)
	write(t, dir, "a.go", "package a // staged\n")
	git(t, dir, "add", "a.go")
	write(t, dir, "a.go", "package a // staged, then edited again\n")
	write(t, dir, "b.go", "package b // unstaged\n")
	write(t, dir, "c.go", "package c\n")
	before := indexState(t, dir)

	mustTree(t, dir)
	mustMeasured(t, dir)
	mustCandidate(t, dir, nil, false)
	mustCandidate(t, dir, []Add{{Args: []string{"--", "b.go", "c.go"}}}, true)
	if _, err := ChangedPaths(dir); err != nil {
		t.Fatal(err)
	}
	if after := indexState(t, dir); after != before {
		t.Fatal("the real index changed")
	}

	// A scratch whose index path is empty must refuse, not fall through to
	// the real index.
	s := &scratch{dir: dir}
	if _, err := s.git(dir, "add", "-A"); err == nil {
		t.Fatal("a scratch with no index ran git")
	}
	if after := indexState(t, dir); after != before {
		t.Fatal("an empty IndexFile reached the real index")
	}
}

// TestWorkingTreeAndCandidate: the two recipes agree exactly when the commit
// would hold what the work tree holds.
func TestWorkingTreeAndCandidate(t *testing.T) {
	dir := repo(t)
	write(t, dir, "a.go", "package a // changed\n")
	write(t, dir, "new.go", "package n\n")
	work := mustTree(t, dir)
	if c := mustCandidate(t, dir, nil, false); c.Tree == work {
		t.Error("candidate equals the work tree while the changes are unstaged")
	}
	git(t, dir, "add", "-A")
	if c := mustCandidate(t, dir, nil, false); c.Tree != work {
		t.Errorf("after git add -A, candidate %s != work tree %s", c.Tree, work)
	}

	write(t, dir, "b.go", "package b // left unstaged\n")
	if c, w := mustCandidate(t, dir, nil, false), mustTree(t, dir); c.Tree == w {
		t.Error("candidate equals the work tree with b.go modified and unstaged")
	}
	git(t, dir, "checkout", "--", "b.go")

	// .dross/ is out of both.
	w0, c0 := mustTree(t, dir), mustCandidate(t, dir, nil, false).Tree
	write(t, dir, ".dross/state.json", `{"edited":true}`+"\n")
	if w, c := mustTree(t, dir), mustCandidate(t, dir, []Add{{Args: []string{"-A"}}}, false).Tree; w != w0 || c != c0 {
		t.Errorf("editing .dross/state.json moved a fingerprint: work %s→%s candidate %s→%s", w0, w, c0, c)
	}

	// Untracked-unignored counts; ignored does not.
	write(t, dir, "debug.log", "noise\n")
	if w := mustTree(t, dir); w != w0 {
		t.Error("an ignored file changed the work tree fingerprint")
	}
	write(t, dir, "extra.go", "package e\n")
	if w := mustTree(t, dir); w == w0 {
		t.Error("an untracked unignored file did not change the work tree fingerprint")
	}
	if err := os.Remove(filepath.Join(dir, "extra.go")); err != nil {
		t.Fatal(err)
	}

	if runtime.GOOS != "windows" {
		if err := os.Chmod(filepath.Join(dir, "a.go"), 0o755); err != nil {
			t.Fatal(err)
		}
		if w := mustTree(t, dir); w == w0 {
			t.Error("chmod +x did not change the work tree fingerprint")
		}
	}
}

// TestCandidateMatchesRealAdd: a replayed add stages what the real one does.
func TestCandidateMatchesRealAdd(t *testing.T) {
	dir := repo(t)
	write(t, dir, "a.go", "package a // changed\n")
	write(t, dir, "sub/s.go", "package s\n")
	replayed := mustCandidate(t, dir, []Add{{Args: []string{"--", "a.go"}}, {Dir: filepath.Join(dir, "sub"), Args: []string{"s.go"}}}, false)
	git(t, dir, "add", "a.go")
	git(t, filepath.Join(dir, "sub"), "add", "s.go")
	plain := mustCandidate(t, dir, nil, false)
	if replayed.Tree != plain.Tree {
		t.Errorf("replayed candidate %s != candidate after the real adds %s", replayed.Tree, plain.Tree)
	}
	if want := []string{"a.go", "sub/s.go"}; !reflect.DeepEqual(plain.Changed, want) {
		t.Errorf("Changed = %q, want %q", plain.Changed, want)
	}

	// -a is `git add -u`: modified tracked files, never new ones.
	write(t, dir, "b.go", "package b // changed\n")
	write(t, dir, "untracked.go", "package u\n")
	all := mustCandidate(t, dir, nil, true)
	git(t, dir, "add", "-u")
	if plain := mustCandidate(t, dir, nil, false); all.Tree != plain.Tree {
		t.Errorf("commit -a candidate %s != candidate after git add -u %s", all.Tree, plain.Tree)
	}
}

func TestReplayRefuses(t *testing.T) {
	dir := repo(t)
	for _, args := range [][]string{{"-p"}, {"--patch"}, {"-i"}, {"--interactive"}, {"-e"}, {"-Ap"}} {
		if _, err := Candidate(dir, []Add{{Args: args}}, false); err == nil || !strings.Contains(err.Error(), "interactive") {
			t.Errorf("add %q: err = %v, want an interactive refusal", args, err)
		}
	}
	if _, err := Candidate(dir, []Add{{Args: []string{"--", "-p"}}}, false); err != nil && strings.Contains(err.Error(), "interactive") {
		t.Errorf("a pathspec after -- was read as an option: %v", err)
	}
	other := repo(t)
	if _, err := Candidate(dir, []Add{{Dir: other, Args: []string{"-A"}}}, false); err == nil || !strings.Contains(err.Error(), "different work tree") {
		t.Errorf("an add in another repository: err = %v, want a different-work-tree refusal", err)
	}
}

func TestChangedPaths(t *testing.T) {
	dir := repo(t)
	write(t, dir, ".dross/x", "x\n")
	git(t, dir, "add", ".dross/x")
	if got, err := ChangedPaths(dir); err != nil || !reflect.DeepEqual(got, []string{".dross/x"}) {
		t.Errorf("ChangedPaths = %q, %v; want exactly [.dross/x]", got, err)
	}

	unborn := initRepo(t)
	write(t, unborn, ".dross/x", "x\n")
	write(t, unborn, "loose.go", "package l\n")
	git(t, unborn, "add", ".dross/x")
	if got, err := ChangedPaths(unborn); err != nil || !reflect.DeepEqual(got, []string{".dross/x"}) {
		t.Errorf("unborn ChangedPaths = %q, %v; want exactly [.dross/x]", got, err)
	}
	if got, err := ChangedPaths(filepath.Join(unborn, ".dross")); err != nil || !reflect.DeepEqual(got, []string{".dross/x"}) {
		t.Errorf("unborn ChangedPaths from a subdir = %q, %v; want repo-relative [.dross/x]", got, err)
	}

	// A linked worktree reads its own index, not the main worktree's.
	wt := filepath.Join(t.TempDir(), "wt")
	git(t, dir, "worktree", "add", "-q", wt)
	write(t, wt, "wt.go", "package w\n")
	git(t, wt, "add", "wt.go")
	if got, err := ChangedPaths(wt); err != nil || !reflect.DeepEqual(got, []string{"wt.go"}) {
		t.Errorf("worktree ChangedPaths = %q, %v; want [wt.go]", got, err)
	}
	if got, _ := ChangedPaths(dir); !reflect.DeepEqual(got, []string{".dross/x"}) {
		t.Errorf("main ChangedPaths after the worktree add = %q, want [.dross/x]", got)
	}
}

func TestDiff(t *testing.T) {
	dir := repo(t)
	before := mustTree(t, dir)
	write(t, dir, "a.go", "package a // changed\n")
	write(t, dir, "z.go", "package z\n")
	after := mustTree(t, dir)
	if got, err := Diff(dir, before, after); err != nil || !reflect.DeepEqual(got, []string{"a.go", "z.go"}) {
		t.Errorf("Diff = %q, %v; want [a.go z.go]", got, err)
	}
	if got, err := Diff(dir, before, ""); err == nil {
		t.Errorf("Diff with an empty fingerprint = %q, want an error", got)
	}
}

// TestGitFailureIsAnError: no recipe answers "" with a nil error — an empty
// fingerprint could equal an empty record.
func TestGitFailureIsAnError(t *testing.T) {
	notRepo := t.TempDir()
	if got, err := WorkingTree(notRepo); err == nil || got != "" {
		t.Errorf("WorkingTree outside a repo = %q, %v", got, err)
	}
	if got, err := Candidate(notRepo, nil, false); err == nil || got.Tree != "" {
		t.Errorf("Candidate outside a repo = %+v, %v", got, err)
	}
	if got, err := ChangedPaths(notRepo); err == nil || got != nil {
		t.Errorf("ChangedPaths outside a repo = %q, %v", got, err)
	}
	if got, err := Diff(notRepo, "a", "b"); err == nil || got != nil {
		t.Errorf("Diff outside a repo = %q, %v", got, err)
	}

	// A conflicted index cannot be written as a tree.
	dir := repo(t)
	git(t, dir, "checkout", "-q", "-b", "side")
	write(t, dir, "a.go", "package a // side\n")
	git(t, dir, "commit", "-q", "-am", "side")
	git(t, dir, "checkout", "-q", "main")
	write(t, dir, "a.go", "package a // main\n")
	git(t, dir, "commit", "-q", "-am", "main")
	if out, err := exec.Command("git", "-C", dir, "merge", "-q", "side").CombinedOutput(); err == nil {
		t.Fatalf("the merge did not conflict:\n%s", out)
	}
	if got, err := Candidate(dir, nil, false); err == nil || got.Tree != "" {
		t.Errorf("Candidate on a conflicted index = %+v, %v; want an error", got, err)
	}
}

// TestEveryGitCallIsBounded: a git that hangs is killed at Timeout and the
// recipe reports it.
func TestEveryGitCallIsBounded(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the stub git is a shell script")
	}
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte("#!/bin/sh\nsleep 10\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	prev := Timeout
	Timeout = 500 * time.Millisecond
	t.Cleanup(func() { Timeout = prev })

	start := time.Now()
	if _, err := WorkingTree(t.TempDir()); err == nil {
		t.Error("a hanging git reported success")
	}
	if _, err := Diff(t.TempDir(), "a", "b"); err == nil {
		t.Error("a hanging git diff-tree reported success")
	}
	if _, err := MeasuredTree(t.TempDir()); err == nil {
		t.Error("a hanging git under MeasuredTree reported success")
	}
	if took := time.Since(start); took > 6*time.Second {
		t.Errorf("three hanging calls took %s, want each bounded by Timeout", took)
	}
}

// TestPatchWholeDiff: the review patch is everything `git add -A` would stage
// against HEAD — staged, unstaged, untracked, and edits no task declared —
// with ignored files and .dross/ left out.
func TestPatchWholeDiff(t *testing.T) {
	dir := repo(t)
	write(t, dir, "a.go", "package a // staged\n")
	git(t, dir, "add", "a.go")
	write(t, dir, "b.go", "package b // unstaged\n")
	write(t, dir, "c.go", "package c // untracked\n")
	write(t, dir, "undeclared/d.go", "package d // nobody planned this\n")
	write(t, dir, "x.log", "ignored\n")
	write(t, dir, ".dross/state.json", `{"x":1}`+"\n")

	c, err := Changes(dir)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"a.go", "b.go", "c.go", "undeclared/d.go"}; !reflect.DeepEqual(c.Paths, want) {
		t.Fatalf("Paths = %v, want %v", c.Paths, want)
	}
	p, err := Patch(dir, c, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"+package a // staged", "+package b // unstaged", "+package c // untracked", "+package d // nobody planned this"} {
		if !strings.Contains(p, want) {
			t.Errorf("patch lacks %q:\n%s", want, p)
		}
	}
	for _, bad := range []string{"x.log", ".dross"} {
		if strings.Contains(p, bad) {
			t.Errorf("patch carries %q:\n%s", bad, p)
		}
	}

	ex, err := Patch(dir, c, []string{"b.go"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(ex, "b.go") || !strings.Contains(ex, "a.go") {
		t.Errorf("Patch excluding b.go:\n%s", ex)
	}
}

// TestPatchConfigIndependent: nothing in the repository's config changes the
// patch bytes, colours it, or runs a configured diff driver.
func TestPatchConfigIndependent(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the marker driver is a shell script")
	}
	dir := repo(t)
	write(t, dir, "a.go", "package a\n\nfunc A() {}\n")
	git(t, dir, "mv", "b.go", "renamed.go")
	write(t, dir, "ünï.go", "package u\n")
	c, err := Changes(dir)
	if err != nil {
		t.Fatal(err)
	}
	want, err := Patch(dir, c, nil)
	if err != nil {
		t.Fatal(err)
	}

	marker := filepath.Join(t.TempDir(), "ran")
	driver := filepath.Join(t.TempDir(), "driver.sh")
	if err := os.WriteFile(driver, []byte("#!/bin/sh\ntouch "+marker+"\ncat \"$1\" 2>/dev/null\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, kv := range [][2]string{
		{"color.ui", "always"}, {"color.diff", "always"},
		{"diff.external", driver},
		{"diff.fake.textconv", driver},
		{"diff.noprefix", "true"}, {"diff.mnemonicPrefix", "true"},
		{"diff.renames", "copies"},
		{"core.quotePath", "false"},
	} {
		git(t, dir, "config", kv[0], kv[1])
	}
	write(t, dir, ".git/info/attributes", "*.go diff=fake\n")

	got, err := Patch(dir, c, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("repo config changed the patch:\n--- before\n%s\n--- after\n%s", want, got)
	}
	if strings.Contains(got, "\x1b[") {
		t.Error("the patch carries ANSI colour escapes")
	}
	if _, err := os.Stat(marker); err == nil {
		t.Error("a configured diff driver or textconv filter ran")
	}
}

// TestPatchUnbornHead: before the first commit every file is added against
// the empty tree.
func TestPatchUnbornHead(t *testing.T) {
	dir := initRepo(t)
	write(t, dir, "a.go", "package a\n")
	write(t, dir, "sub/b.go", "package b\n")
	write(t, dir, ".dross/state.json", "{}\n")
	c, err := Changes(dir)
	if err != nil {
		t.Fatal(err)
	}
	empty := strings.TrimSpace(git(t, dir, "hash-object", "-t", "tree", "/dev/null"))
	if c.Base != empty {
		t.Fatalf("Base on an unborn HEAD = %q, want the empty tree %q", c.Base, empty)
	}
	if want := []string{"a.go", "sub/b.go"}; !reflect.DeepEqual(c.Paths, want) {
		t.Fatalf("Paths = %v, want %v", c.Paths, want)
	}
	p, err := Patch(dir, c, nil)
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(p, "new file mode"); n != 2 {
		t.Fatalf("patch shows %d new files, want 2:\n%s", n, p)
	}
	if b, err := Base(dir); err != nil || b != empty {
		t.Fatalf("Base = %q, %v", b, err)
	}
}

// TestDirtyIgnoresDross: only code a commit would record counts.
func TestDirtyIgnoresDross(t *testing.T) {
	dir := repo(t)
	dirty := func() bool {
		t.Helper()
		d, err := Dirty(dir)
		if err != nil {
			t.Fatal(err)
		}
		return d
	}
	if dirty() {
		t.Fatal("a clean tree reads dirty")
	}
	write(t, dir, ".dross/state.json", `{"changed":true}`+"\n")
	write(t, dir, ".dross/gate/review.json", "{}\n")
	write(t, dir, "x.log", "ignored\n")
	if dirty() {
		t.Fatal("a .dross/-only (plus ignored) change reads dirty")
	}
	write(t, dir, "c.go", "package c\n")
	if !dirty() {
		t.Fatal("an untracked unignored file does not read dirty")
	}

	// Base moves only when code is committed: a .dross/-only commit keeps it.
	git(t, dir, "rm", "-q", "--cached", "--ignore-unmatch", "c.go")
	before, err := Base(dir)
	if err != nil {
		t.Fatal(err)
	}
	git(t, dir, "add", ".dross/state.json")
	git(t, dir, "commit", "-q", "-m", "bookkeeping")
	git(t, dir, "commit", "-q", "--allow-empty", "-m", "empty")
	if after, err := Base(dir); err != nil || after != before {
		t.Fatalf("Base moved across .dross/-only and empty commits: %q -> %q (%v)", before, after, err)
	}
}

// measuredRepo is repo plus a committed root ARCHITECTURE.md and README.md.
func measuredRepo(t *testing.T) string {
	t.Helper()
	dir := repo(t)
	write(t, dir, "ARCHITECTURE.md", "# arch\n")
	write(t, dir, "README.md", "# readme\n")
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-q", "-m", "docs")
	return dir
}

// TestMeasuredTreeExemptions: the two exemptions are exactly the root
// .dross/ and the root ARCHITECTURE.md, matched literally — a lookalike is
// measured like any other file.
func TestMeasuredTreeExemptions(t *testing.T) {
	dir := measuredRepo(t)
	base := mustMeasured(t, dir).Tree
	write(t, dir, "ARCHITECTURE.md", "# arch, landmarks merged\n")
	write(t, dir, ".dross/x", "bookkeeping\n")
	if got := mustMeasured(t, dir).Tree; got != base {
		t.Error("an exempt edit (root ARCHITECTURE.md, .dross/x) changed the measured tree")
	}
	for _, rel := range []string{"docs/ARCHITECTURE.md", "ARCHITECTURE.md.bak", "sub/.dross/x", ".drossrc"} {
		t.Run(rel, func(t *testing.T) {
			dir := measuredRepo(t)
			base := mustMeasured(t, dir).Tree
			write(t, dir, rel, "lookalike\n")
			if got := mustMeasured(t, dir).Tree; got == base {
				t.Errorf("%s is not exempt, but adding it left the measured tree unchanged", rel)
			}
		})
	}
}

// TestMeasuredTreeCoversTheWorkTree: every change `git add -A` would stage
// moves the measured tree; an ignored file does not.
func TestMeasuredTreeCoversTheWorkTree(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(t *testing.T, dir string)
	}{
		{"uncommitted tracked edit", func(t *testing.T, dir string) { write(t, dir, "a.go", "package a // edited\n") }},
		{"untracked file", func(t *testing.T, dir string) { write(t, dir, "c.go", "package c\n") }},
		{"deleted tracked file", func(t *testing.T, dir string) {
			if err := os.Remove(filepath.Join(dir, "b.go")); err != nil {
				t.Fatal(err)
			}
		}},
		{"mode change", func(t *testing.T, dir string) {
			if err := os.Chmod(filepath.Join(dir, "a.go"), 0o755); err != nil {
				t.Fatal(err)
			}
		}},
		{"README", func(t *testing.T, dir string) { write(t, dir, "README.md", "# readme, edited\n") }},
		{"CI config", func(t *testing.T, dir string) { write(t, dir, ".github/workflows/ci.yml", "on: push\n") }},
		{"test file", func(t *testing.T, dir string) { write(t, dir, "x_test.go", "package a\n") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.name == "mode change" && runtime.GOOS == "windows" {
				t.Skip("no executable bit")
			}
			dir := measuredRepo(t)
			base := mustMeasured(t, dir).Tree
			tc.change(t, dir)
			if got := mustMeasured(t, dir).Tree; got == base {
				t.Errorf("%s left the measured tree unchanged", tc.name)
			}
		})
	}
	dir := measuredRepo(t)
	base := mustMeasured(t, dir).Tree
	write(t, dir, "run.log", "ignored\n")
	if got := mustMeasured(t, dir).Tree; got != base {
		t.Error("a .gitignore'd file changed the measured tree")
	}
}

// TestWorkingTreeStillCountsArchitecture: the new exemption is MeasuredTree's
// alone. The commit gate and the green record fingerprint through WorkingTree,
// which must still see a root ARCHITECTURE.md edit.
func TestWorkingTreeStillCountsArchitecture(t *testing.T) {
	dir := measuredRepo(t)
	before := mustTree(t, dir)
	write(t, dir, "ARCHITECTURE.md", "# arch, edited\n")
	if after := mustTree(t, dir); after == before {
		t.Error("WorkingTree stopped counting ARCHITECTURE.md")
	}
}

// TestMeasuredTreeIgnoresCommitMoves: the tree is the work tree's, not HEAD's.
// A .dross-only commit, and a commit of exactly the measured edits, move
// Commit and leave Tree alone.
func TestMeasuredTreeIgnoresCommitMoves(t *testing.T) {
	dir := measuredRepo(t)
	m0 := mustMeasured(t, dir)
	if m0.Commit == "" {
		t.Fatal("no commit recorded on a born HEAD")
	}
	write(t, dir, ".dross/state.json", "{\"v\":2}\n")
	git(t, dir, "commit", "-q", "-am", "bookkeeping")
	m1 := mustMeasured(t, dir)
	if m1.Tree != m0.Tree || m1.Commit == m0.Commit {
		t.Errorf("a .dross-only commit: tree %s -> %s, commit %s -> %s; want the tree kept and the commit moved", m0.Tree, m1.Tree, m0.Commit, m1.Commit)
	}
	write(t, dir, "a.go", "package a // measured dirty\n")
	dirty := mustMeasured(t, dir)
	git(t, dir, "commit", "-q", "-am", "commit the measured edit")
	after := mustMeasured(t, dir)
	if after.Tree != dirty.Tree || after.Commit == dirty.Commit {
		t.Errorf("committing exactly the measured edit: tree %s -> %s, commit %s -> %s; want the tree kept and the commit moved", dirty.Tree, after.Tree, dirty.Commit, after.Commit)
	}
}

// TestMeasuredTreeDiffNamesChanges: Diff over two measured trees names exactly
// the non-exempt paths that were added, removed or modified.
func TestMeasuredTreeDiffNamesChanges(t *testing.T) {
	dir := measuredRepo(t)
	before := mustMeasured(t, dir).Tree
	write(t, dir, "a.go", "package a // modified\n")
	write(t, dir, "c.go", "package c\n")
	if err := os.Remove(filepath.Join(dir, "b.go")); err != nil {
		t.Fatal(err)
	}
	write(t, dir, "ARCHITECTURE.md", "# arch, edited\n")
	write(t, dir, ".dross/x", "bookkeeping\n")
	after := mustMeasured(t, dir).Tree
	got, err := Diff(dir, before, after)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"a.go", "b.go", "c.go"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Diff = %v, want %v", got, want)
	}
}

// TestMeasuredTreeEdgeRepos: an unborn HEAD still has a tree and no commit;
// outside a repository there is an error, never an empty fingerprint.
func TestMeasuredTreeEdgeRepos(t *testing.T) {
	dir := initRepo(t)
	write(t, dir, "a.go", "package a\n")
	m, err := MeasuredTree(dir)
	if err != nil || m.Tree == "" || m.Commit != "" {
		t.Errorf("unborn HEAD: MeasuredTree = %+v, %v; want a tree and no commit", m, err)
	}
	if got, err := MeasuredTree(t.TempDir()); err == nil || got.Tree != "" || got.Commit != "" {
		t.Errorf("outside a repo: MeasuredTree = %+v, %v; want an error and nothing recorded", got, err)
	}
}

// TestMeasuredTreeAnchorsAtDrossRoot: with the dross root below the top of the
// repository, the exempt paths are the root's own .dross/ and ARCHITECTURE.md,
// and the top-level files of the same names are measured.
func TestMeasuredTreeAnchorsAtDrossRoot(t *testing.T) {
	top := initRepo(t)
	write(t, top, "svc/a.go", "package a\n")
	write(t, top, "svc/.dross/state.json", "{}\n")
	write(t, top, "svc/ARCHITECTURE.md", "# svc arch\n")
	write(t, top, "ARCHITECTURE.md", "# top arch\n")
	write(t, top, ".dross/state.json", "{}\n")
	git(t, top, "add", "-A")
	git(t, top, "commit", "-q", "-m", "init")
	root := filepath.Join(top, "svc")

	base := mustMeasured(t, root).Tree
	write(t, top, "svc/.dross/x", "bookkeeping\n")
	write(t, top, "svc/ARCHITECTURE.md", "# svc arch, landmarks merged\n")
	if got := mustMeasured(t, root).Tree; got != base {
		t.Error("the dross root's own .dross/ or ARCHITECTURE.md changed the measured tree")
	}
	for _, rel := range []string{"ARCHITECTURE.md", ".dross/x"} {
		before := mustMeasured(t, root).Tree
		write(t, top, rel, "edited at the top\n")
		if got := mustMeasured(t, root).Tree; got == before {
			t.Errorf("top-level %s is outside the dross root but was treated as exempt", rel)
		}
	}
}
