package cmd

// The bare-preview file set.
//
// Every assertion here is about a path that must survive collection, because
// the failure mode is silent in both directions: a path dropped here previews
// nothing and reads as "no lane matches", and a path collected in the wrong
// SHAPE (a collapsed directory, a still-quoted string) matches no glob and
// reads the same way.

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// worktreeFixture is a real git repo with one commit, so a staged deletion has
// something to delete.
func worktreeFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	gitInit(t, dir, "git@example.com:x/y.git")
	if err := os.WriteFile(filepath.Join(dir, "base.go"), []byte("package x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustGit(t, dir, "add", "base.go")
	mustGit(t, dir, "commit", "-q", "-m", "base")
	return dir
}

// writeUnder creates a file and its parents inside the fixture.
func writeUnder(t *testing.T, dir, rel string) {
	t.Helper()
	full := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte("package x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mustWorktreeFiles(t *testing.T, dir string) []string {
	t.Helper()
	got, err := worktreeChangedFiles(dir)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func hasPath(got []string, want string) bool {
	for _, g := range got {
		if g == want {
			return true
		}
	}
	return false
}

// TestWorkingTreeFilesExpandsUntrackedDirectories is why this function does not
// reuse gitStatusRaw.
//
// git's default collapses a wholly untracked directory to one `dir/` entry, and
// a directory previews NOTHING: every lane glob is written against files, so a
// brand-new package would resolve to no lane at all and read as "no lane
// matches" — the one answer that is indistinguishable from a real miss.
func TestWorkingTreeFilesExpandsUntrackedDirectories(t *testing.T) {
	dir := worktreeFixture(t)
	writeUnder(t, dir, "internal/new/a.go")
	writeUnder(t, dir, "internal/new/b.go")

	got := mustWorktreeFiles(t, dir)
	for _, want := range []string{"internal/new/a.go", "internal/new/b.go"} {
		if !hasPath(got, want) {
			t.Errorf("%s missing from %v — the collapsed `dir/` form previews nothing", want, got)
		}
	}
	if hasPath(got, "internal/new/") {
		t.Errorf("the set carries a directory entry: %v", got)
	}
}

// TestDeletedFileIsStillInTheSet: a deletion is collected, not filtered.
//
// The deleted path is precisely what c-2 has to be able to name as dropped at
// derivation. Filtering it here would leave the derivation with nothing to
// report and the line quietly shorter, which is the case a user previewing
// deleted work is most likely to be asking about.
func TestDeletedFileIsStillInTheSet(t *testing.T) {
	dir := worktreeFixture(t)
	mustGit(t, dir, "rm", "-q", "base.go")

	got := mustWorktreeFiles(t, dir)
	if !hasPath(got, "base.go") {
		t.Errorf("the staged deletion is missing from %v", got)
	}
}

// TestQuotedPathIsUnquoted: git quotes a path with a space, and a quoted string
// matches no glob.
//
// The set has to be what `--files` would have been handed — the path itself, not
// git's rendering of it — or the two invocations of the same preview disagree
// about which lanes a file hits.
func TestQuotedPathIsUnquoted(t *testing.T) {
	dir := worktreeFixture(t)
	writeUnder(t, dir, "internal/a b.go")

	got := mustWorktreeFiles(t, dir)
	if !hasPath(got, "internal/a b.go") {
		t.Errorf("the spaced path is missing or still quoted: %v", got)
	}
}

// TestRenameContributesOnlyTheDestination: porcelainPaths returns both sides of
// a rename, and only one of them is a file the user has in hand.
//
// The source no longer exists, so keeping it would make every rename report a
// phantom dropped path — a finding about a file the user deliberately moved,
// printed as if something had gone missing.
func TestRenameContributesOnlyTheDestination(t *testing.T) {
	got := worktreeFilesFromStatus("R  old.go -> new.go")
	if !reflect.DeepEqual(got, []string{"new.go"}) {
		t.Errorf("got %v, want [new.go] — the rename source is not a path in hand", got)
	}
}

// TestCleanTreeReturnsAnEmptySetNotAnError: nothing in hand is an answer, not a
// fault.
//
// The caller prints "0 files" and exits 0 (locked preview_exit_status). An error
// here would make the most ordinary invocation of a describing verb fail.
func TestCleanTreeReturnsAnEmptySetNotAnError(t *testing.T) {
	dir := worktreeFixture(t)

	got, err := worktreeChangedFiles(dir)
	if err != nil {
		t.Fatalf("a clean tree returned an error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("a clean tree returned %v", got)
	}
}
