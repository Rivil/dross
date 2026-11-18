package facts

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func write(t *testing.T, root, rel, body string) {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir,
		"-c", "user.name=t", "-c", "user.email=t@example.com", "-c", "commit.gpgsign=false"}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func contains(files []string, f string) bool {
	for _, x := range files {
		if x == f {
			return true
		}
	}
	return false
}

// TestTreeRespectsGitignoreAndNestedRepos: inside git, the walk lists tracked
// and untracked-but-not-ignored files, and never a gitignored file or anything
// inside a nested repository (a worktree copy of the repo).
func TestTreeRespectsGitignoreAndNestedRepos(t *testing.T) {
	root := t.TempDir()
	git(t, root, "init", "-q")
	write(t, root, ".gitignore", "dist/\n")
	write(t, root, "tracked.go", "package x\n")
	git(t, root, "add", ".")
	git(t, root, "commit", "-q", "-m", "base")

	write(t, root, "new.go", "package x\n")       // untracked, not ignored
	write(t, root, "dist/a.go", "package dist\n") // ignored
	nested := filepath.Join(root, ".claude", "worktrees", "w")
	write(t, nested, "tracked.go", "package x\n")
	git(t, nested, "init", "-q")
	write(t, nested, "inner.go", "package x\n")

	files, err := Tree(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{".gitignore", "tracked.go", "new.go"} {
		if !contains(files, want) {
			t.Errorf("walk lacks %s: %v", want, files)
		}
	}
	for _, f := range files {
		switch {
		case f == "dist/a.go":
			t.Errorf("walk includes gitignored %s", f)
		case len(f) >= len(".claude/") && f[:len(".claude/")] == ".claude/":
			t.Errorf("walk descends into the nested repo: %s", f)
		}
	}
}

// TestTreeFallbackOutsideGit: when git cannot list the tree, the WalkDir
// fallback lists regular files and skips .git and node_modules anywhere.
func TestTreeFallbackOutsideGit(t *testing.T) {
	prev := lsFiles
	t.Cleanup(func() { lsFiles = prev })
	lsFiles = func(string) (string, error) { return "", errors.New("not a git repository") }

	root := t.TempDir()
	write(t, root, "src/a.go", "package a\n")
	write(t, root, "README.md", "# r\n")
	write(t, root, ".git/config", "[core]\n")
	write(t, root, "node_modules/dep/index.js", "x\n")
	write(t, root, "web/node_modules/dep/index.js", "x\n")

	files, err := Tree(root)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"README.md", "src/a.go"}
	if len(files) != len(want) || files[0] != want[0] || files[1] != want[1] {
		t.Errorf("fallback walk = %v, want %v", files, want)
	}
}

// TestTreeRealNonGitDir: a plain temp dir (no repo) still walks via the
// fallback with no error.
func TestTreeRealNonGitDir(t *testing.T) {
	root := t.TempDir()
	write(t, root, "a.go", "package a\n")
	files, err := Tree(root)
	if err != nil {
		t.Fatal(err)
	}
	if !contains(files, "a.go") {
		t.Errorf("non-git walk = %v, want a.go", files)
	}
}
