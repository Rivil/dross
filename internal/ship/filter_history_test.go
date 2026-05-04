package ship

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Rivil/dross/internal/changes"
)

// TestFilterPreserveHistoryProducesPerCommitBranch builds a phase with
// three commits — two touch real source + .dross/, one is .dross/-only —
// and asserts the resulting branch has exactly two commits in the
// expected order, all .dross/-free.
//
// Mirrors TestFilterSquashDropsDrossDir: .dross/ is committed normally
// (no gitignore in the fixture) so the cherry-pick/strip path operates
// on tracked files. The gitignored variant is exercised by
// TestFilterSquashPreservesGitignoredDross — the algorithm here
// doesn't care.
func TestFilterPreserveHistoryProducesPerCommitBranch(t *testing.T) {
	dir := makeRepo(t)

	// Commit 1 — code + planning artefacts. Force author so we can
	// assert authorship is preserved across the cherry-pick.
	writeFile(t, dir, "src/a.ts", "export const a = 1\n")
	writeFile(t, dir, ".dross/phases/01-x/spec.toml", `id = "01-x"`)
	mustGit(t, dir, "add", "src/a.ts", ".dross/phases/01-x/spec.toml")
	mustGit(t, dir, "-c", "user.email=alice@example.com", "-c", "user.name=Alice",
		"commit", "-q", "-m", "feat: add a")
	sha1 := mustGit(t, dir, "rev-parse", "HEAD")

	// Commit 2 — .dross/-only (should be skipped in the preserved
	// history because nothing remains after stripping).
	writeFile(t, dir, ".dross/phases/01-x/plan.toml", `id = "01-x"`)
	mustGit(t, dir, "add", ".dross/phases/01-x/plan.toml")
	mustGit(t, dir, "commit", "-q", "-m", "chore(plan): wip")
	sha2 := mustGit(t, dir, "rev-parse", "HEAD")

	// Commit 3 — code change with a different author.
	writeFile(t, dir, "src/b.ts", "export const b = 2\n")
	mustGit(t, dir, "add", "src/b.ts")
	mustGit(t, dir, "-c", "user.email=bob@example.com", "-c", "user.name=Bob",
		"commit", "-q", "-m", "feat: add b")
	sha3 := mustGit(t, dir, "rev-parse", "HEAD")

	c := &changes.Changes{
		Phase: "01-x",
		Tasks: map[string]changes.TaskRecord{
			"t1": {Commit: sha1, CompletedAt: time.Now()},
			"t2": {Commit: sha2, CompletedAt: time.Now()},
			"t3": {Commit: sha3, CompletedAt: time.Now()},
		},
	}

	branch, tipSHA, err := FilterPreserveHistory(c, FilterOpts{
		RepoDir: dir,
		PhaseID: "01-x",
	})
	if err != nil {
		t.Fatalf("FilterPreserveHistory: %v", err)
	}
	if branch != "pr/01-x" || tipSHA == "" {
		t.Fatalf("unexpected return: branch=%q sha=%q", branch, tipSHA)
	}

	// Should be exactly 2 commits on the new branch (commit 2 dropped).
	count := mustGit(t, dir, "rev-list", "--count", "main..pr/01-x")
	if count != "2" {
		t.Errorf("expected 2 commits on pr/01-x, got %s", count)
	}
	_, _, _ = sha1, sha2, sha3 // referenced for debug; keep for future failures

	// Authors preserved from source commits (Alice on t1, Bob on t3).
	authors := mustGit(t, dir, "log", "--format=%an", "main..pr/01-x")
	if !strings.Contains(authors, "Alice") || !strings.Contains(authors, "Bob") {
		t.Errorf("source authors should survive: %q", authors)
	}

	// Tree on the new branch contains code, no .dross/.
	prTree := mustGit(t, dir, "ls-tree", "-r", "--name-only", "pr/01-x")
	if !strings.Contains(prTree, "src/a.ts") || !strings.Contains(prTree, "src/b.ts") {
		t.Errorf("pr branch missing expected source: %s", prTree)
	}
	if strings.Contains(prTree, ".dross") {
		t.Errorf("pr branch must not contain .dross/: %s", prTree)
	}

	// User's working tree untouched — .dross/ still here, still on main.
	if _, err := os.Stat(filepath.Join(dir, ".dross/phases/01-x/spec.toml")); err != nil {
		t.Errorf("user lost .dross/spec.toml: %v", err)
	}
	if cur := mustGit(t, dir, "symbolic-ref", "--short", "HEAD"); cur != "main" {
		t.Errorf("user's HEAD moved off main: %q", cur)
	}
}

// TestFilterPreserveHistoryErrsOnAllDrossOnlyCommits guards against a
// silent no-op when every phase commit was .dross/-only.
func TestFilterPreserveHistoryErrsOnAllDrossOnlyCommits(t *testing.T) {
	dir := makeRepo(t)

	writeFile(t, dir, ".dross/phases/01/spec.toml", "x")
	mustGit(t, dir, "add", ".dross/phases/01/spec.toml")
	mustGit(t, dir, "commit", "-q", "-m", "chore(spec): wip")
	sha := mustGit(t, dir, "rev-parse", "HEAD")

	c := &changes.Changes{Tasks: map[string]changes.TaskRecord{"t1": {Commit: sha}}}
	_, _, err := FilterPreserveHistory(c, FilterOpts{RepoDir: dir, PhaseID: "01"})
	if err == nil {
		t.Fatal("expected error when every phase commit was .dross/-only")
	}
	if !strings.Contains(err.Error(), "no commits") && !strings.Contains(err.Error(), "produced no") {
		t.Errorf("unexpected error message: %v", err)
	}
}

// TestFilterPreserveHistoryRefusesExistingBranchWithoutForce mirrors
// the squash version's behaviour.
func TestFilterPreserveHistoryRefusesExistingBranchWithoutForce(t *testing.T) {
	dir := makeRepo(t)
	writeFile(t, dir, "src/a.ts", "x\n")
	mustGit(t, dir, "add", "src/a.ts")
	mustGit(t, dir, "commit", "-q", "-m", "feat: add a")
	sha := mustGit(t, dir, "rev-parse", "HEAD")

	c := &changes.Changes{Tasks: map[string]changes.TaskRecord{"t1": {Commit: sha}}}
	if _, _, err := FilterPreserveHistory(c, FilterOpts{RepoDir: dir, PhaseID: "01"}); err != nil {
		t.Fatalf("first call: %v", err)
	}
	if _, _, err := FilterPreserveHistory(c, FilterOpts{RepoDir: dir, PhaseID: "01"}); err == nil {
		t.Error("expected error on existing branch without Force")
	}
	if _, _, err := FilterPreserveHistory(c, FilterOpts{RepoDir: dir, PhaseID: "01", Force: true}); err != nil {
		t.Fatalf("force overwrite: %v", err)
	}
}
