// Package facts checks the factual claims a phase's spec.toml and plan.toml
// make about the code — the file paths, `file:line` citations and symbol names
// they cite — against the live tree, so a plan is never built on a fact that
// stopped being true (or never was).
package facts

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Rivil/dross/internal/gitrun"
)

// lsFiles lists the repo's files the way git sees them: tracked plus
// untracked-but-not-ignored, NUL-separated. A seam so the fallback path can be
// tested without uninstalling git.
var lsFiles = func(root string) (string, error) {
	return gitrun.Raw(root, "ls-files", "-z", "--cached", "--others", "--exclude-standard")
}

// skipDirs are directory names the non-git fallback never descends into.
var skipDirs = map[string]bool{".git": true, "node_modules": true}

// Tree returns every file under root as a sorted, deduplicated list of
// slash-separated root-relative paths. It is the one tree walker the facts
// checks share: the symbol index and the bare-basename lookup see the same set.
//
// Inside a git repo it asks git (`ls-files --cached --others
// --exclude-standard`), so gitignored files are absent and a nested repository
// or worktree — which git lists as a single `dir/` entry and never descends
// into — contributes nothing. Outside a git repo (or with no git binary) it
// falls back to a directory walk that skips .git and node_modules. Any other
// git failure is returned: inside a repo git refused to list (a safe.directory
// refusal, a broken index), the walk would count its ignored files and nested
// worktrees, and a wrong tree is worse than none.
func Tree(root string) ([]string, error) {
	out, err := lsFiles(root)
	if err == nil {
		return fromLsFiles(out), nil
	}
	if !notGitTree(err) {
		return nil, fmt.Errorf("facts: git ls-files failed in %s (%w); run `git status` there to see why", root, err)
	}
	return walkTree(root)
}

// notARepo is the phrase git's fatal error carries when no repository
// encloses the directory it was run in.
const notARepo = "not a git repository"

// notGitTree reports whether a git listing's error means there is no
// repository to ask — git is not installed, or git says the directory is not
// in a repository — so the plain directory walk is the right answer. Git's
// reason is on the stderr its exit error captured; the error's own text is
// checked too, for a failure that carries the reason there. Only the match
// leaves this function, never git's text. A git whose messages are localised
// never matches, so it fails closed: an error, not a walk.
func notGitTree(err error) bool {
	if errors.Is(err, exec.ErrNotFound) {
		return true
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) && bytes.Contains(exit.Stderr, []byte(notARepo)) {
		return true
	}
	return strings.Contains(err.Error(), notARepo)
}

// fromLsFiles parses `ls-files -z` output. An entry ending in "/" is an
// embedded repository git refuses to descend into — not a file — and is dropped.
func fromLsFiles(out string) []string {
	seen := map[string]bool{}
	var files []string
	//dross:taint-cleared git ls-files -z prints NUL-separated repo-relative paths; each one is kept as a path and nothing else of git's output is
	for _, f := range strings.Split(out, "\x00") {
		if f == "" || strings.HasSuffix(f, "/") || seen[f] {
			continue
		}
		seen[f] = true
		files = append(files, f)
	}
	sort.Strings(files)
	return files
}

// walkTree is the non-git fallback: every regular file under root, skipping
// .git and node_modules directories wherever they appear.
func walkTree(root string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if p != root && skipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		files = append(files, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	return files, nil
}
