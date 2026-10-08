package techdebt

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// abs joins each repo-relative path onto repoDir, the form trackedFiles hands
// to Filter.
func abs(repoDir string, rels ...string) []string {
	out := make([]string, 0, len(rels))
	for _, r := range rels {
		out = append(out, filepath.Join(repoDir, filepath.FromSlash(r)))
	}
	return out
}

func rels(repoDir string, paths []string) []string {
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		out = append(out, relSlash(repoDir, p))
	}
	return out
}

// TestFilterTrailingSlashIsPrefix: a trailing "/" is a directory prefix, not a
// glob — handing it straight to path.Match keeps the two nested files.
func TestFilterTrailingSlashIsPrefix(t *testing.T) {
	repo := t.TempDir()
	in := abs(repo, "internal/techdebt/scan.go", "internal/techdebt/deep/nested.go", "internal/techdebtx/a.go", "internal/techdebt.go")
	got, err := Filter(repo, in, []string{"internal/techdebt/"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"internal/techdebtx/a.go", "internal/techdebt.go"}
	if !reflect.DeepEqual(rels(repo, got), want) {
		t.Fatalf("survivors = %v, want %v", rels(repo, got), want)
	}
}

// TestFilterGlobIsRepoRelative: globs match the repo-relative path (and, for a
// bare pattern, the base name) — never the absolute path, which starts with
// the tempdir and would defeat "internal/*/scan.go".
func TestFilterGlobIsRepoRelative(t *testing.T) {
	repo := t.TempDir()
	cases := []struct {
		pattern string
		in      []string
		want    []string
	}{
		{"*.golden", []string{"docs/a.golden", "a.golden", "docs/a.md"}, []string{"docs/a.md"}},
		{"docs/*.md", []string{"docs/a.md", "docs/sub/a.md"}, []string{"docs/sub/a.md"}},
		{"internal/*/scan.go", []string{"internal/techdebt/scan.go", "internal/techdebt/run.go"}, []string{"internal/techdebt/run.go"}},
	}
	for _, c := range cases {
		got, err := Filter(repo, abs(repo, c.in...), []string{c.pattern})
		if err != nil {
			t.Fatalf("%s: %v", c.pattern, err)
		}
		if !reflect.DeepEqual(rels(repo, got), c.want) {
			t.Errorf("%s: survivors = %v, want %v", c.pattern, rels(repo, got), c.want)
		}
	}
}

// TestFilterBadPatternErrors: a pattern that does not compile is an error
// naming the entry, with a nil slice — never the unfiltered input.
func TestFilterBadPatternErrors(t *testing.T) {
	repo := t.TempDir()
	got, err := Filter(repo, abs(repo, "a.go"), []string{"[abc"})
	if err == nil {
		t.Fatal("want an error for pattern [abc")
	}
	if !strings.Contains(err.Error(), "[abc") {
		t.Fatalf("error %q must name the entry", err)
	}
	if got != nil {
		t.Fatalf("paths = %v, want nil on error", got)
	}
}

// TestFilterEmptyIsIdentity: nil excludes return the input unchanged.
func TestFilterEmptyIsIdentity(t *testing.T) {
	repo := t.TempDir()
	in := abs(repo, "a.go", "b/c.go")
	got, err := Filter(repo, in, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, in) {
		t.Fatalf("got %v, want input unchanged %v", got, in)
	}
}
