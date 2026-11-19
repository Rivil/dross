package facts

import (
	"errors"
	"fmt"
	"io/fs"
	"path"
	"strings"
	"syscall"

	"github.com/Rivil/dross/internal/gitrun"
	"github.com/Rivil/dross/internal/pathfence"
)

// ErrMissing is wrapped by every ContainCitation refusal that means "this
// citation names no file in the tree": a path that is not there, and a path
// that escapes the root or is absolute, which cannot name a file in it either.
var ErrMissing = errors.New("cited path does not exist")

// citationArtifact is what a refused citation's error names as its source. A
// citation comes from spec or plan prose; the caller labels which field.
const citationArtifact = "citation"

// statCitation is pathfence.Stat behind a seam, so a test can count the
// filesystem calls a citation costs: an escaping one must cost none.
var statCitation = pathfence.Stat

// lsIgnored lists the gitignored files under root whose base name is name,
// NUL-separated. The glob pathspec keeps git from printing every file in an
// ignored tree (node_modules) only for all but one name to be thrown away.
var lsIgnored = func(root, name string) (string, error) {
	return gitrun.Raw(root, "ls-files", "-z", "--others", "--ignored", "--exclude-standard",
		"--", ":(glob)**/"+globEscape(name))
}

// ContainCitation checks one cited path against the tree at root and returns
// it contained. Every refusal that means "not in this tree" wraps ErrMissing;
// any other error is a failure to look, not a finding.
//
// A SLASHED path is repo-relative: it is contained under root (so `../x` and
// an absolute path are missing without a filesystem call), cleaned (so
// `./internal/x.go` is `internal/x.go`), and checked on disk through
// pathfence.Stat — so a gitignored, machine-local file still resolves.
//
// A BARE basename resolves when any file Tree lists has that name, or failing
// that any file git lists as ignored does. The result is the first match in
// sorted order. Existence only: the ignored files are never added to Tree, so
// they never feed the symbol index.
func ContainCitation(root, raw string) (pathfence.Contained, error) {
	return containCitation(root, raw, func() ([]string, error) { return Tree(root) })
}

// containCitation is ContainCitation over a caller's tree listing, so a caller
// that has already walked the tree (a Resolver) does not walk it again per
// citation. tree is called only for a bare basename.
func containCitation(root, raw string, tree func() ([]string, error)) (pathfence.Contained, error) {
	c, err := pathfence.Contain(root, citationArtifact, raw)
	if err != nil {
		return pathfence.Contained{}, fmt.Errorf("%w: %w", ErrMissing, err)
	}
	if c.Rel() == "." {
		// "./" or "a/.." — the root itself, which names no file.
		return pathfence.Contained{}, fmt.Errorf("%w: %q names the root, not a file", ErrMissing, raw)
	}
	if strings.ContainsAny(raw, `/\`) {
		return statContained(c, raw)
	}
	return containBasename(root, c.Rel(), raw, tree)
}

// statContained resolves a slashed citation on disk. A missing file, or a
// path through something that is not a directory, is missing; any other stat
// error is returned as it stands.
func statContained(c pathfence.Contained, raw string) (pathfence.Contained, error) {
	if _, err := statCitation(c); err != nil {
		if errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTDIR) {
			return pathfence.Contained{}, fmt.Errorf("%w: %s", ErrMissing, raw)
		}
		return pathfence.Contained{}, err
	}
	return c, nil
}

// containBasename resolves a bare file name against the tree listing, then
// against git's ignored files. Outside git (no repository, no git binary) the
// ignored lookup has nothing to add — Tree's fallback walk has already listed
// every file, ignored or not — so its failure there is no match. Inside a
// repository a failed lookup is a failure to look, returned as an error: a
// present ignored file must never be reported missing because git refused.
func containBasename(root, name, raw string, tree func() ([]string, error)) (pathfence.Contained, error) {
	files, err := tree()
	if err != nil {
		return pathfence.Contained{}, err
	}
	if f, ok := firstNamed(files, name); ok {
		return pathfence.Contain(root, citationArtifact, f)
	}
	out, err := lsIgnored(root, name)
	switch {
	case err == nil:
		if f, ok := firstNamed(fromLsFiles(out), name); ok {
			return pathfence.Contain(root, citationArtifact, f)
		}
	case !notGitTree(err):
		return pathfence.Contained{}, fmt.Errorf("facts: listing ignored files named %s failed in %s (%w)", raw, root, err)
	}
	return pathfence.Contained{}, fmt.Errorf("%w: no file named %s", ErrMissing, raw)
}

// firstNamed returns the first file in files whose base name is name. files
// is sorted, so the answer does not depend on listing order.
func firstNamed(files []string, name string) (string, bool) {
	for _, f := range files {
		if path.Base(f) == name {
			return f, true
		}
	}
	return "", false
}

// globEscape backslash-escapes the characters git's glob pathspec magic
// treats specially, so a name is matched literally.
func globEscape(name string) string {
	var b strings.Builder
	for _, r := range name {
		if strings.ContainsRune(`*?[]\`, r) {
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}
