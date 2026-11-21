package facts

import (
	"errors"
	"io/fs"
	"testing"

	"github.com/Rivil/dross/internal/pathfence"
)

// countStats swaps the stat seam for one that counts the paths it is asked
// about, and still stats them for real.
func countStats(t *testing.T) map[string]int {
	t.Helper()
	prev := statCitation
	t.Cleanup(func() { statCitation = prev })
	calls := map[string]int{}
	statCitation = func(c pathfence.Contained) (fs.FileInfo, error) {
		calls[c.Rel()]++
		return prev(c)
	}
	return calls
}

func mustResolve(t *testing.T, root, raw, wantRel string) pathfence.Contained {
	t.Helper()
	c, err := ContainCitation(root, raw)
	if err != nil {
		t.Fatalf("ContainCitation(%q): %v, want it to resolve to %s", raw, err, wantRel)
	}
	if c.Rel() != wantRel {
		t.Errorf("ContainCitation(%q).Rel() = %q, want %q", raw, c.Rel(), wantRel)
	}
	return c
}

func mustMiss(t *testing.T, root, raw string) {
	t.Helper()
	c, err := ContainCitation(root, raw)
	if !errors.Is(err, ErrMissing) {
		t.Fatalf("ContainCitation(%q) = %q, %v; want ErrMissing", raw, c.Rel(), err)
	}
}

// TestContainCitationEscapeCostsNoStat: a citation that leaves the root, or is
// absolute, is missing before anything touches the filesystem; an in-tree one
// does reach the stat seam, so the zero count is not a seam nobody calls.
func TestContainCitationEscapeCostsNoStat(t *testing.T) {
	root := t.TempDir()
	write(t, root, "internal/cmd/state.go", "package cmd\n")
	calls := countStats(t)

	for _, raw := range []string{"../../etc/passwd", "/etc/passwd", "a/../../x.go"} {
		mustMiss(t, root, raw)
	}
	if len(calls) != 0 {
		t.Errorf("escaping citations reached the stat seam: %v", calls)
	}

	mustResolve(t, root, "internal/cmd/state.go", "internal/cmd/state.go")
	if calls["internal/cmd/state.go"] != 1 {
		t.Errorf("an in-tree slashed citation must be stat'd once, got %v", calls)
	}
}

// TestContainCitationResolution: a slashed path is checked on disk and
// cleaned; a bare basename is looked up by name anywhere in the walked tree.
func TestContainCitationResolution(t *testing.T) {
	root := t.TempDir()
	git(t, root, "init", "-q")
	write(t, root, "internal/cmd/state.go", "package cmd\n")
	write(t, root, "internal/x.go", "package internal\n")
	write(t, root, ".dross/phases/x/spec.toml", "[phase]\n")

	mustResolve(t, root, "internal/cmd/state.go", "internal/cmd/state.go")
	mustMiss(t, root, "nosuch.go")
	mustResolve(t, root, "spec.toml", ".dross/phases/x/spec.toml")

	dotted := mustResolve(t, root, "./internal/x.go", "internal/x.go")
	plain := mustResolve(t, root, "internal/x.go", "internal/x.go")
	if dotted != plain {
		t.Errorf("./internal/x.go = %+v, internal/x.go = %+v; want one path", dotted, plain)
	}

	for _, raw := range []string{
		"internal/nosuch.go",   // slashed, absent
		"internal/x.go/y.go",   // through a file, not a directory
		"./",                   // the root itself
		"internal/cmd/state.g", // a near miss is not fuzzy-matched
	} {
		mustMiss(t, root, raw)
	}
}

// TestContainCitationBareMatchIsSorted: a basename several files share
// resolves to the first in sorted order, whatever order the listing came in.
func TestContainCitationBareMatchIsSorted(t *testing.T) {
	root := t.TempDir()
	c, err := containCitation(root, "a.go", func() ([]string, error) {
		return []string{"b/a.go", "z/a.go"}, nil
	})
	if err != nil || c.Rel() != "b/a.go" {
		t.Errorf("bare a.go = %q, %v; want b/a.go", c.Rel(), err)
	}
}

// TestContainCitationTreeErrorIsNotAFinding: a walk that fails is a failure to
// look, never reported as the citation being missing.
func TestContainCitationTreeErrorIsNotAFinding(t *testing.T) {
	boom := errors.New("walk failed")
	_, err := containCitation(t.TempDir(), "a.go", func() ([]string, error) { return nil, boom })
	if !errors.Is(err, boom) || errors.Is(err, ErrMissing) {
		t.Errorf("err = %v; want the walk error and not ErrMissing", err)
	}
}

// TestContainCitationIgnoredBasename: a gitignored file that exists on disk is
// found by its bare name — including one inside an ignored directory — while
// the symbol-index walk still never lists it.
func TestContainCitationIgnoredBasename(t *testing.T) {
	root := t.TempDir()
	git(t, root, "init", "-q")
	write(t, root, ".gitignore", ".dross/state.json\nnode_modules/\n*.log\n")
	write(t, root, "main.go", "package main\n")
	git(t, root, "add", ".")
	git(t, root, "commit", "-q", "-m", "base")
	write(t, root, ".dross/state.json", "{}\n")
	write(t, root, "node_modules/dep/lib.js", "x\n")
	write(t, root, "x[1].log", "x\n")

	mustResolve(t, root, "state.json", ".dross/state.json")
	mustResolve(t, root, "lib.js", "node_modules/dep/lib.js")
	// Glob characters in the name are matched literally, not as a class.
	mustResolve(t, root, "x[1].log", "x[1].log")
	mustMiss(t, root, "nosuch.json")

	files, err := Tree(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, ignored := range []string{".dross/state.json", "node_modules/dep/lib.js", "x[1].log"} {
		if contains(files, ignored) {
			t.Errorf("the symbol-index walk lists ignored %s: %v", ignored, files)
		}
	}
}

// TestGlobEscape pins the escaped set.
func TestGlobEscape(t *testing.T) {
	if got, want := globEscape(`a*b?[c]\d.go`), `a\*b\?\[c\]\\d.go`; got != want {
		t.Errorf("globEscape = %q, want %q", got, want)
	}
}

// stubIgnored swaps the ignored-file lookup for one that fails with err.
func stubIgnored(t *testing.T, err error) {
	t.Helper()
	prev := lsIgnored
	t.Cleanup(func() { lsIgnored = prev })
	lsIgnored = func(string, string) (string, error) { return "", err }
}

// TestContainCitationIgnoredLookupFailure: inside a repository a failed
// ignored-file lookup is a failure to look, never ErrMissing — the file may be
// there; outside git (git says no repository) a bare name it cannot find is
// still missing.
func TestContainCitationIgnoredLookupFailure(t *testing.T) {
	tree := func() ([]string, error) { return []string{"main.go"}, nil }

	boom := errors.New("exit status 128")
	stubIgnored(t, boom)
	_, err := containCitation(t.TempDir(), "state.json", tree)
	if err == nil || errors.Is(err, ErrMissing) || !errors.Is(err, boom) {
		t.Errorf("lookup failure inside git: err = %v; want the git error and not ErrMissing", err)
	}

	stubIgnored(t, errors.New("fatal: not a git repository"))
	if _, err := containCitation(t.TempDir(), "state.json", tree); !errors.Is(err, ErrMissing) {
		t.Errorf("lookup outside git: err = %v; want ErrMissing", err)
	}
}
