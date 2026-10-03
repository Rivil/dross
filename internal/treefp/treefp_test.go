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
	write(t, dir, "b.go", "package b // unstaged\n")
	write(t, dir, "c.go", "package c\n")
	before := indexState(t, dir)

	mustTree(t, dir)
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
	if took := time.Since(start); took > 4*time.Second {
		t.Errorf("two hanging calls took %s, want each bounded by Timeout", took)
	}
}
