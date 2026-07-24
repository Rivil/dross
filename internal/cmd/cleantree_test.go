package cmd

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestUnderDross pins the prefix guard: only .dross itself and paths inside it
// qualify — string-prefix siblings must not.
func TestUnderDross(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{".dross", true},
		{".dross/", true},
		{".dross/handoff.md", true},
		{".dross/phases/x/plan.toml", true},
		{".drosszz/x", false},
		{"notes-.dross.txt", false},
		{"src/tag.ts", false},
		{"dross", false},
	}
	for _, c := range cases {
		if got := underDross(c.path); got != c.want {
			t.Errorf("underDross(%q) = %v, want %v", c.path, got, c.want)
		}
	}
}

// TestPorcelainPaths pins line parsing: rename lines yield both sides, plain
// lines one path, quoted paths are unquoted.
func TestPorcelainPaths(t *testing.T) {
	cases := []struct {
		line string
		want []string
	}{
		{" M .dross/state.json", []string{".dross/state.json"}},
		{"?? notes-.dross.txt", []string{"notes-.dross.txt"}},
		{"R  .dross/a.md -> .dross/b.md", []string{".dross/a.md", ".dross/b.md"}},
		{"R  .dross/a.md -> escaped.md", []string{".dross/a.md", "escaped.md"}},
		{`?? "we\tird.txt"`, []string{"we\tird.txt"}},
		{"", nil},
	}
	for _, c := range cases {
		got := porcelainPaths(c.line)
		if len(got) != len(c.want) {
			t.Errorf("porcelainPaths(%q) = %v, want %v", c.line, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("porcelainPaths(%q)[%d] = %q, want %q", c.line, i, got[i], c.want[i])
			}
		}
	}
}

func commitCount(t *testing.T, dir string) string {
	t.Helper()
	return mustGit(t, dir, "rev-list", "--count", "HEAD")
}

// Only .dross/handoff.md dirty → exactly one chore(dross): commit and the
// gate proceeds (nil error, clean tree after).
func TestAutoCommitDrossOnlyDirt(t *testing.T) {
	dir := initWithGit(t)
	mustWrite(t, filepath.Join(dir, ".dross", "handoff.md"), "# handoff\n")
	before := commitCount(t, dir)

	committed, err := autoCommitDrossDirt(dir, "testing")
	if err != nil {
		t.Fatalf("expected .dross-only dirt to auto-commit, got: %v", err)
	}
	if !committed {
		t.Error("expected committed=true for .dross-only dirt")
	}
	after := commitCount(t, dir)
	if before == after {
		t.Error("expected exactly one new commit, count unchanged")
	}
	msg := mustGit(t, dir, "log", "-1", "--format=%s")
	if !strings.HasPrefix(msg, "chore(dross):") {
		t.Errorf("auto-commit must use the chore(dross) convention, got: %q", msg)
	}
	if st := mustGit(t, dir, "status", "--porcelain"); st != "" {
		t.Errorf("tree should be clean after auto-commit, got: %q", st)
	}
}

// .dross/ + README.md both dirty → dirtyTreeError with zero commits and
// nothing staged (no partial commit of the .dross half).
func TestAutoCommitMixedDirtRefuses(t *testing.T) {
	dir := initWithGit(t)
	mustWrite(t, filepath.Join(dir, ".dross", "handoff.md"), "# handoff\n")
	mustWrite(t, filepath.Join(dir, "README.md"), "changed\n")
	before := commitCount(t, dir)

	committed, err := autoCommitDrossDirt(dir, "testing")
	if err == nil {
		t.Fatal("expected refusal when non-.dross paths are dirty")
	}
	if committed {
		t.Error("committed must be false on refusal")
	}
	if !strings.Contains(err.Error(), "working tree is dirty") {
		t.Errorf("refusal must be the dirtyTreeError shape: %v", err)
	}
	if !strings.Contains(err.Error(), "README.md") {
		t.Errorf("refusal should list the offending path: %v", err)
	}
	if after := commitCount(t, dir); after != before {
		t.Errorf("refusal must create zero commits: %s -> %s", before, after)
	}
	// Nothing staged — not even the .dross half.
	if err := gitNoOut(dir, "diff", "--cached", "--quiet"); err != nil {
		t.Error("refusal must stage nothing, but the index has staged changes")
	}
}

// A rename staying inside .dross/ parses both sides as under .dross and
// proceeds.
func TestAutoCommitRenameWithinDross(t *testing.T) {
	dir := initWithGit(t)
	mustWrite(t, filepath.Join(dir, ".dross", "a.md"), "a\n")
	mustGit(t, dir, "add", ".dross/a.md")
	mustGit(t, dir, "commit", "-q", "-m", "chore: seed a.md")
	mustGit(t, dir, "mv", ".dross/a.md", ".dross/b.md")

	committed, err := autoCommitDrossDirt(dir, "testing")
	if err != nil {
		t.Fatalf("rename within .dross should auto-commit, got: %v", err)
	}
	if !committed {
		t.Error("expected committed=true for a .dross-internal rename")
	}
}

// A rename leaving .dross/ has one side outside — refuse.
func TestAutoCommitRenameLeavingDrossRefuses(t *testing.T) {
	dir := initWithGit(t)
	mustWrite(t, filepath.Join(dir, ".dross", "a.md"), "a\n")
	mustGit(t, dir, "add", ".dross/a.md")
	mustGit(t, dir, "commit", "-q", "-m", "chore: seed a.md")
	mustGit(t, dir, "mv", ".dross/a.md", "escaped.md")
	before := commitCount(t, dir)

	if _, err := autoCommitDrossDirt(dir, "testing"); err == nil {
		t.Fatal("rename leaving .dross must refuse")
	}
	if after := commitCount(t, dir); after != before {
		t.Errorf("refusal must create zero commits: %s -> %s", before, after)
	}
}

// String-prefix siblings of .dross are NOT under it — refuse.
func TestAutoCommitPrefixSiblingsRefuse(t *testing.T) {
	cases := []string{
		filepath.Join(".drosszz", "x.txt"),
		"notes-.dross.txt",
	}
	for _, rel := range cases {
		t.Run(rel, func(t *testing.T) {
			dir := initWithGit(t)
			mustWrite(t, filepath.Join(dir, rel), "x\n")
			before := commitCount(t, dir)

			if _, err := autoCommitDrossDirt(dir, "testing"); err == nil {
				t.Fatalf("%s must not count as under .dross", rel)
			}
			if after := commitCount(t, dir); after != before {
				t.Errorf("refusal must create zero commits: %s -> %s", before, after)
			}
		})
	}
}

// Clean tree → no-op: zero commits, committed=false.
func TestAutoCommitCleanTreeNoop(t *testing.T) {
	dir := initWithGit(t)
	before := commitCount(t, dir)

	committed, err := autoCommitDrossDirt(dir, "testing")
	if err != nil {
		t.Fatalf("clean tree must be a no-op, got: %v", err)
	}
	if committed {
		t.Error("committed must be false on a clean tree")
	}
	if after := commitCount(t, dir); after != before {
		t.Errorf("clean tree must produce zero commits: %s -> %s", before, after)
	}
}
