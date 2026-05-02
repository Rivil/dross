package ship

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Rivil/dross/internal/changes"
)

// helper: run a shell command in a dir and fail the test on error.
func mustGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	full := append([]string{"-C", dir}, args...)
	out, err := exec.Command("git", full...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, string(out))
	}
	return strings.TrimSpace(string(out))
}

// makeRepo creates a tmp git repo with a baseline commit.
// Returns the repo dir.
func makeRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	mustGit(t, dir, "init", "-q", "-b", "main")
	mustGit(t, dir, "config", "user.email", "test@example.com")
	mustGit(t, dir, "config", "user.name", "Test")
	mustGit(t, dir, "config", "commit.gpgsign", "false")
	mustGit(t, dir, "config", "tag.gpgsign", "false")

	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustGit(t, dir, "add", "README.md")
	mustGit(t, dir, "commit", "-q", "-m", "init")
	return dir
}

func writeFile(t *testing.T, dir, rel, body string) {
	t.Helper()
	full := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestFilterSquashDropsDrossDir(t *testing.T) {
	dir := makeRepo(t)

	// Phase commits — each touches code AND .dross/.
	writeFile(t, dir, "src/a.ts", "export const a = 1\n")
	writeFile(t, dir, ".dross/phases/01-x/spec.toml", `[phase]
id = "01-x"`)
	mustGit(t, dir, "add", ".")
	mustGit(t, dir, "commit", "-q", "-m", "feat: add a")
	sha1 := mustGit(t, dir, "rev-parse", "HEAD")

	writeFile(t, dir, "src/b.ts", "export const b = 2\n")
	writeFile(t, dir, ".dross/phases/01-x/changes.json", `{"phase":"01-x","tasks":{}}`)
	mustGit(t, dir, "add", ".")
	mustGit(t, dir, "commit", "-q", "-m", "feat: add b")
	sha2 := mustGit(t, dir, "rev-parse", "HEAD")

	// Build a changes record naming the phase commits.
	c := &changes.Changes{
		Phase: "01-x",
		Tasks: map[string]changes.TaskRecord{
			"t1": {Commit: sha1, CompletedAt: time.Now()},
			"t2": {Commit: sha2, CompletedAt: time.Now()},
		},
	}

	branch, sha, err := FilterSquash(c, FilterOpts{
		RepoDir: dir,
		PhaseID: "01-x",
		Message: "phase 01-x: a + b",
	})
	if err != nil {
		t.Fatalf("FilterSquash: %v", err)
	}
	if branch != "pr/01-x" {
		t.Errorf("branch name: got %q want pr/01-x", branch)
	}
	if sha == "" {
		t.Errorf("expected commit SHA, got empty")
	}

	// User should be back on main after FilterSquash returns.
	cur := mustGit(t, dir, "symbolic-ref", "--short", "HEAD")
	if cur != "main" {
		t.Errorf("expected main after FilterSquash, on %q", cur)
	}

	// pr/01-x branch should have src/a.ts + src/b.ts but NO .dross/.
	mustGit(t, dir, "checkout", "-q", "pr/01-x")
	defer mustGit(t, dir, "checkout", "-q", "main")

	for _, want := range []string{"src/a.ts", "src/b.ts", "README.md"} {
		if _, err := os.Stat(filepath.Join(dir, want)); err != nil {
			t.Errorf("expected %s on pr branch: %v", want, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, ".dross")); !os.IsNotExist(err) {
		t.Errorf(".dross/ should be absent on pr branch, err=%v", err)
	}

	// Should be a single commit on top of base (init).
	count := mustGit(t, dir, "rev-list", "--count", "main..HEAD")
	if count != "1" {
		t.Errorf("expected 1 commit on pr branch, got %s", count)
	}
}

func TestFilterSquashRefusesExistingBranchWithoutForce(t *testing.T) {
	dir := makeRepo(t)
	writeFile(t, dir, "src/a.ts", "x\n")
	writeFile(t, dir, ".dross/phases/01/spec.toml", "x")
	mustGit(t, dir, "add", ".")
	mustGit(t, dir, "commit", "-q", "-m", "feat")
	sha := mustGit(t, dir, "rev-parse", "HEAD")

	c := &changes.Changes{Tasks: map[string]changes.TaskRecord{"t1": {Commit: sha}}}
	if _, _, err := FilterSquash(c, FilterOpts{RepoDir: dir, PhaseID: "01", Message: "p"}); err != nil {
		t.Fatalf("first squash: %v", err)
	}
	// Second call without Force should fail.
	_, _, err := FilterSquash(c, FilterOpts{RepoDir: dir, PhaseID: "01", Message: "p"})
	if err == nil {
		t.Error("expected error on existing branch without Force")
	}
	// With Force it should overwrite.
	if _, _, err := FilterSquash(c, FilterOpts{RepoDir: dir, PhaseID: "01", Message: "p2", Force: true}); err != nil {
		t.Fatalf("force overwrite: %v", err)
	}
}

func TestFilterSquashErrsWithoutCommits(t *testing.T) {
	dir := makeRepo(t)
	c := &changes.Changes{Tasks: map[string]changes.TaskRecord{}}
	_, _, err := FilterSquash(c, FilterOpts{RepoDir: dir, PhaseID: "01", Message: "p"})
	if err == nil {
		t.Error("expected error when no commits in changes.json")
	}
}
