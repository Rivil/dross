package memadvise

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSlugReplacesEveryNonAlnum(t *testing.T) {
	cases := map[string]string{
		"/Users/a/.claude/x": "-Users-a--claude-x",
		"/srv/my_repo":       "-srv-my-repo",
		"/srv/my repo":       "-srv-my-repo",
		"/srv/Repo9":         "-srv-Repo9",
	}
	for in, want := range cases {
		if got := Slug(in); got != want {
			t.Errorf("Slug(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSlugResolvesSymlinks(t *testing.T) {
	real, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(real, "repo")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(real, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if got, want := Slug(link), Slug(target); got != want {
		t.Errorf("Slug(link) = %q, want the target's slug %q", got, want)
	}

	// The case the decision was written for: macOS reaches /private/tmp
	// through /tmp, and Claude Code files the project under the real path.
	if dest, err := os.Readlink("/tmp"); err == nil && strings.TrimSuffix(dest, "/") == "private/tmp" {
		if got := Slug("/tmp"); !strings.HasPrefix(got, "-private-tmp") {
			t.Errorf("Slug(/tmp) = %q, want the -private-tmp prefix", got)
		}
	}
}

// memDir creates <claude>/projects/<slug of repo>/memory and returns it.
func memDir(t *testing.T, claude, repo string) string {
	t.Helper()
	dir := filepath.Join(claude, "projects", Slug(repo), "memory")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestLocateHonorsClaudeConfigDir(t *testing.T) {
	repo := t.TempDir()
	cfg := t.TempDir()
	home := t.TempDir()
	want := memDir(t, cfg, repo)
	// A memory dir under ~/.claude too: falling back to it would return ok.
	memDir(t, filepath.Join(home, ".claude"), repo)

	t.Setenv("CLAUDE_CONFIG_DIR", cfg)
	t.Setenv("HOME", home)
	got, ok, err := Locate(repo)
	if err != nil || !ok || got != want {
		t.Fatalf("Locate = (%q, %v, %v), want (%q, true, nil)", got, ok, err, want)
	}

	// Only ~/.claude has the dir: with the override set it must not be found,
	// which it would be if ~/.claude were consulted at all.
	if err := os.RemoveAll(want); err != nil {
		t.Fatal(err)
	}
	got, ok, err = Locate(repo)
	if err != nil || ok {
		t.Fatalf("Locate with only ~/.claude populated = (%q, %v, %v), want ok=false", got, ok, err)
	}
	if !strings.HasPrefix(got, cfg) {
		t.Errorf("Locate path %q is not under CLAUDE_CONFIG_DIR %q", got, cfg)
	}

	// And unset, ~/.claude is the fallback.
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	if got, ok, err := Locate(repo); err != nil || !ok || !strings.HasPrefix(got, filepath.Join(home, ".claude")) {
		t.Errorf("Locate without override = (%q, %v, %v), want ok under ~/.claude", got, ok, err)
	}
}

func TestLocateMissingIsSilent(t *testing.T) {
	repo := t.TempDir()

	check := func(name string) {
		t.Helper()
		if dir, ok, err := Locate(repo); ok || err != nil {
			t.Errorf("%s: Locate = (%q, %v, %v), want ok=false, nil error", name, dir, ok, err)
		}
	}

	cfg := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", cfg)
	check("missing dir")

	project := filepath.Join(cfg, "projects", Slug(repo))
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, "memory"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	check("regular file at memory/")

	if err := os.Remove(filepath.Join(project, "memory")); err != nil {
		t.Fatal(err)
	}
	elsewhere := t.TempDir()
	if err := os.Symlink(elsewhere, filepath.Join(project, "memory")); err != nil {
		t.Fatal(err)
	}
	check("symlinked memory/")

	// A file where the project dir should be: ENOTDIR, still just absent.
	cfg2 := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", cfg2)
	if err := os.MkdirAll(filepath.Join(cfg2, "projects"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfg2, "projects", Slug(repo)), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	check("file in place of the project dir")
}
