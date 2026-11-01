package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/memadvise"
)

// memoryDocText is a tracked doc the fixture's dup.md restates.
var memoryDocText = strings.Repeat("dross keeps phase state in toml files under the dross dir and commits them. ", 4)

// memoryThreeIndex holds the fixture's three suggestions: a duplicate delete
// and two expired-clause rewords.
const memoryThreeIndex = "- [Dup](dup.md) — restates the readme\n" +
	"- [A](a.md) — alpha; until 2026-01-01\n" +
	"- [B](b.md) — beta; until 2026-01-01\n"

// memoryApplyFixture is a git repo with a tracked README.md and a memory dir
// carrying delete-dup, reword-a and reword-b. It returns (repo, memory dir).
func memoryApplyFixture(t *testing.T) (string, string) {
	t.Helper()
	dir := debugNudgeFixture(t)
	mustGit(t, dir, "init", "-q")
	mustWrite(t, filepath.Join(dir, "README.md"), memoryDocText)
	mustGit(t, dir, "add", "README.md")
	mem := memoryDir(t, dir)
	mustWrite(t, filepath.Join(mem, memadvise.IndexName), memoryThreeIndex)
	mustWrite(t, filepath.Join(mem, "dup.md"), memoryDocText)
	mustWrite(t, filepath.Join(mem, "a.md"), "alpha body\n")
	mustWrite(t, filepath.Join(mem, "b.md"), "beta body\n")
	return dir, mem
}

// memoryRun runs `dross memory <args>` on a fresh tree: apply's --yes lives in
// its closure, so a reused tree would carry it into the next call.
func memoryRun(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var err error
	out := captureStdout(t, func() { err = runCmd(t, Memory(), args...) })
	return out, err
}

func memoryFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func noSnapshots(t *testing.T, repo string) {
	t.Helper()
	if _, err := os.Stat(filepath.Join(repo, filepath.FromSlash(memadvise.SnapshotsDir))); !os.IsNotExist(err) {
		t.Errorf("a refused apply took a snapshot (%v)", err)
	}
}

func TestApplyRefusesWithoutConfirm(t *testing.T) {
	repo, mem := memoryApplyFixture(t)
	before := memDirHash(t, mem)
	_, err := memoryRun(t, "apply", "delete-dup")
	if err == nil || !strings.Contains(err.Error(), "--yes") {
		t.Fatalf("apply without --yes = %v, want a refusal naming --yes", err)
	}
	if memDirHash(t, mem) != before {
		t.Error("apply without --yes changed the memory dir")
	}
	noSnapshots(t, repo)
}

func TestApplyUnknownID(t *testing.T) {
	repo, mem := memoryApplyFixture(t)
	before := memDirHash(t, mem)
	for _, id := range []string{"delete-nope", "reword-dup", "delete-a"} {
		if _, err := memoryRun(t, "apply", id, "--yes"); err == nil || !strings.Contains(err.Error(), "not a current suggestion") {
			t.Errorf("apply %s = %v, want an unknown-id refusal", id, err)
		}
	}
	if memDirHash(t, mem) != before {
		t.Error("an unknown id changed the memory dir")
	}
	noSnapshots(t, repo)
}

func TestApplyOrphanRefused(t *testing.T) {
	repo, mem := memoryApplyFixture(t)
	mustWrite(t, filepath.Join(mem, "lost.md"), "nobody indexes me\n")
	before := memDirHash(t, mem)
	_, err := memoryRun(t, "apply", "delete-lost", "--yes")
	if err == nil || !strings.Contains(err.Error(), "report-only") || !strings.Contains(err.Error(), "index lost.md") ||
		!strings.Contains(err.Error(), "by hand") {
		t.Fatalf("apply on an orphan = %v, want the index-or-remove-by-hand refusal", err)
	}
	if memDirHash(t, mem) != before {
		t.Error("a refused orphan apply changed the memory dir")
	}
	noSnapshots(t, repo)
}

func TestApplyChangesExactlyOne(t *testing.T) {
	_, mem := memoryApplyFixture(t)
	dupBefore, aBefore := memoryFile(t, filepath.Join(mem, "dup.md")), memoryFile(t, filepath.Join(mem, "a.md"))
	if _, err := memoryRun(t, "apply", "reword-b", "--yes"); err != nil {
		t.Fatal(err)
	}
	if memoryFile(t, filepath.Join(mem, "dup.md")) != dupBefore || memoryFile(t, filepath.Join(mem, "a.md")) != aBefore {
		t.Error("applying reword-b changed another memory")
	}
	want := strings.Replace(memoryThreeIndex, "beta; until 2026-01-01", "beta", 1)
	if got := memoryFile(t, filepath.Join(mem, memadvise.IndexName)); got != want {
		t.Errorf("MEMORY.md = %q, want %q", got, want)
	}
}

func TestApplyEndToEnd(t *testing.T) {
	repo, mem := memoryApplyFixture(t)
	out, err := memoryRun(t, "apply", "delete-dup", "--yes")
	if err != nil {
		t.Fatalf("apply: %v\n%s", err, out)
	}
	if _, err := os.Lstat(filepath.Join(mem, "dup.md")); !os.IsNotExist(err) {
		t.Errorf("dup.md was not removed: %v", err)
	}
	for _, f := range []string{"a.md", "b.md"} {
		if _, err := os.Stat(filepath.Join(mem, f)); err != nil {
			t.Errorf("%s was removed too: %v", f, err)
		}
	}
	var snap string
	for _, l := range strings.Split(out, "\n") {
		if rest, ok := strings.CutPrefix(l, "snapshot: "); ok {
			snap = rest
		}
	}
	if !strings.HasPrefix(snap, memadvise.SnapshotsDir+"/") {
		t.Fatalf("no snapshot path under %s in:\n%s", memadvise.SnapshotsDir, out)
	}
	if got := memoryFile(t, filepath.Join(repo, filepath.FromSlash(snap), "dup.md")); got != memoryDocText {
		t.Error("the printed snapshot does not hold the deleted memory")
	}
	if !strings.Contains(out, "applied delete-dup") || !strings.Contains(out, "recheck: 0 dead index link(s), 0 orphan(s)") {
		t.Errorf("apply output lacks the result or the recheck:\n%s", out)
	}
}

func TestAdviseListsAllReadOnly(t *testing.T) {
	dir := debugNudgeFixture(t)
	mem := fiveItemMemory(t, dir)
	before := memDirHash(t, mem)
	out, err := memoryRun(t, "advise")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range memoryFixtureTokens {
		if !strings.Contains(out, "  delete-"+f.stem+"  ") {
			t.Errorf("advise lacks delete-%s:\n%s", f.stem, out)
		}
	}
	if strings.Contains(out, "more — dross memory advise") {
		t.Errorf("advise truncated its list:\n%s", out)
	}
	if !strings.Contains(out, "assistant turns") {
		t.Errorf("advise lacks the share line:\n%s", out)
	}
	if memDirHash(t, mem) != before {
		t.Error("advise changed the memory dir")
	}
}

func TestAdviseMissingDirSilent(t *testing.T) {
	debugNudgeFixture(t)
	out, err := memoryRun(t, "advise")
	if err != nil || out != "" {
		t.Errorf("advise with no memory dir = (%q, %v), want silence and exit 0", out, err)
	}
}

func TestAdviseSurfacesCountError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads a chmod-000 dir")
	}
	dir := debugNudgeFixture(t)
	fiveItemMemory(t, dir)
	projects := filepath.Join(os.Getenv("CLAUDE_CONFIG_DIR"), "projects")
	other := filepath.Join(projects, "-another-project")
	mustWrite(t, filepath.Join(other, "s.jsonl"), "{}\n")
	if err := os.Chmod(other, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(other, 0o755) })
	_, err := memoryRun(t, "advise")
	if err == nil || !strings.Contains(err.Error(), projects) {
		t.Errorf("advise over an unreadable transcripts dir = %v, want an error naming %s", err, projects)
	}
	// The second run reuses the cache the first one saved; a cache hit must
	// not pass the partial count off as complete.
	_, err = memoryRun(t, "advise")
	if err == nil || !strings.Contains(err.Error(), projects) {
		t.Errorf("advise on a cached partial count = %v, want the same error naming %s", err, projects)
	}
}

// TestAdviseReportsCacheWriteError: a turn cache that cannot be written still
// prints the advice, and advise names the failure instead of exiting 0.
func TestAdviseReportsCacheWriteError(t *testing.T) {
	dir := debugNudgeFixture(t)
	fiveItemMemory(t, dir)
	mustWrite(t, filepath.Join(dir, filepath.FromSlash(memadvise.StateDir)), "not a dir\n")
	out, err := memoryRun(t, "advise")
	if !strings.Contains(out, "  delete-"+memoryFixtureTokens[0].stem+"  ") {
		t.Errorf("advise printed no suggestions on an unwritable cache:\n%s", out)
	}
	if err == nil || !strings.Contains(err.Error(), "could not be cached") || !strings.Contains(err.Error(), memadvise.StateDir) {
		t.Errorf("advise on an unwritable cache = %v, want an error naming the cache write and %s", err, memadvise.StateDir)
	}
}

// TestMemoryCommandRegistered: Memory() must be on the real root, or `dross
// memory` — and the status line pointing at it — leads nowhere.
func TestMemoryCommandRegistered(t *testing.T) {
	b, err := os.ReadFile(filepath.Join(repoRootFromTest(t), "cmd", "dross", "main.go"))
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	if !strings.Contains(string(b), "cmd.Memory()") {
		t.Error("cmd.Memory() is not registered in cmd/dross/main.go root.AddCommand")
	}
}
