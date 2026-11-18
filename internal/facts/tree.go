// Package facts checks the factual claims a phase's spec.toml and plan.toml
// make about the code — the file paths, `file:line` citations and symbol names
// they cite — against the live tree, so a plan is never built on a fact that
// stopped being true (or never was).
package facts

import (
	"io/fs"
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
// falls back to a directory walk that skips .git and node_modules.
func Tree(root string) ([]string, error) {
	if out, err := lsFiles(root); err == nil {
		return fromLsFiles(out), nil
	}
	return walkTree(root)
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
