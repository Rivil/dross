// Package treefp fingerprints the tree a commit would record, and the tree
// `git add -A` would stage, without touching the index the user stages into.
//
// A fingerprint is the id of the git tree object written from a scratch copy
// of the real index once it has been staged into and .dross/ taken out. Two
// recipes share it. WorkingTree stages everything `git add -A` would, so a
// green `dross test` records the tree the suite actually ran against.
// Candidate replays the `git add`s chained ahead of a commit (and -a), so the
// commit gate judges the tree `git commit` will actually record. The two are
// equal exactly when the commit holds the tested tree, give or take .dross/ —
// the bookkeeping dross itself writes between a test run and its commit.
//
// The scratch index is a byte copy, so stat data, skip-worktree and
// intent-to-add bits carry over and only changed files are rehashed. It lives
// in the OS temp dir and is removed after each call. The blobs and trees the
// recipes write land in the repository's object store as loose objects, which
// git's gc prunes like any other. Every git call carries Timeout.
package treefp

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Rivil/dross/internal/gitrun"
)

// Timeout bounds each git call. The commit gate runs inside a hook, and a
// wedged git there would hold the tool call it guards.
var Timeout = 30 * time.Second

// Add is one `git add` replayed onto a candidate: its arguments after `add`,
// run in Dir ("" for the directory the candidate is taken in).
type Add struct {
	Dir  string
	Args []string
}

// Snapshot is the commit a candidate would make.
type Snapshot struct {
	// Tree is the fingerprint.
	Tree string
	// Changed lists every path whose staged entry differs from HEAD — every
	// staged path on an unborn HEAD — .dross/ included, in git's order.
	Changed []string
}

// errNoIndex guards the one mistake that would turn a scratch run into a
// write to the user's own index: an empty IndexFile means the real one.
var errNoIndex = errors.New("treefp: no scratch index — refusing to run git against the real index")

// WorkingTree fingerprints the work tree in dir as `git add -A` would stage
// it: tracked and untracked-unignored files, modes included, .dross/ out.
func WorkingTree(dir string) (string, error) {
	s, err := open(dir)
	if err != nil {
		return "", err
	}
	defer s.close()
	if _, err := s.git(s.dir, "add", "-A"); err != nil {
		return "", err
	}
	return s.tree()
}

// Candidate takes the commit `git commit` in dir would make after the adds
// ran in order, with all set for `git commit -a`.
func Candidate(dir string, adds []Add, all bool) (Snapshot, error) {
	s, err := open(dir)
	if err != nil {
		return Snapshot{}, err
	}
	defer s.close()
	for _, a := range adds {
		if err := s.replay(a); err != nil {
			return Snapshot{}, err
		}
	}
	if all {
		if _, err := s.git(s.dir, "add", "-u"); err != nil {
			return Snapshot{}, err
		}
	}
	changed, err := s.changed()
	if err != nil {
		return Snapshot{}, err
	}
	tree, err := s.tree()
	if err != nil {
		return Snapshot{}, err
	}
	return Snapshot{Tree: tree, Changed: changed}, nil
}

// ChangedPaths lists the paths the real index in dir stages against HEAD.
func ChangedPaths(dir string) ([]string, error) {
	s, err := open(dir)
	if err != nil {
		return nil, err
	}
	defer s.close()
	return s.changed()
}

// Diff lists the paths whose entries differ between two fingerprints taken in
// the repository at dir.
func Diff(dir, a, b string) ([]string, error) {
	if a == "" || b == "" {
		return nil, fmt.Errorf("treefp: diff needs two fingerprints, got %q and %q", a, b)
	}
	out, err := gitrun.RawWith(gitrun.Options{Timeout: Timeout}, dir, "diff-tree", "-r", "--name-only", "-z", "--end-of-options", a, b)
	if err != nil {
		return nil, fmt.Errorf("git diff-tree: %w", err)
	}
	//dross:taint-cleared git diff-tree --name-only -z prints NUL-separated repo-relative paths; each one is kept as a path and nothing else of git's output is
	return splitNUL(out), nil
}

// Base fingerprints HEAD's tree with .dross/ taken out — the empty tree on an
// unborn HEAD. It is the side a review diff compares the work tree against,
// and moves only when code (not bookkeeping) is committed.
func Base(dir string) (string, error) {
	s, err := open(dir)
	if err != nil {
		return "", err
	}
	defer s.close()
	return s.base()
}

// Change is the work tree's difference from HEAD, .dross/ out on both sides.
type Change struct {
	// Base is HEAD's fingerprint (Base).
	Base string
	// Tree is the work tree's fingerprint — what WorkingTree returns.
	Tree string
	// Paths lists every path whose entry differs between the two.
	Paths []string
}

// Changes takes both fingerprints in one scratch index and lists what differs.
func Changes(dir string) (Change, error) {
	s, err := open(dir)
	if err != nil {
		return Change{}, err
	}
	defer s.close()
	if _, err := s.git(s.dir, "add", "-A"); err != nil {
		return Change{}, err
	}
	tree, err := s.tree()
	if err != nil {
		return Change{}, err
	}
	base, err := s.base()
	if err != nil {
		return Change{}, err
	}
	c := Change{Base: base, Tree: tree}
	if base != tree {
		if c.Paths, err = Diff(dir, base, tree); err != nil {
			return Change{}, err
		}
	}
	return c, nil
}

// Dirty reports whether the work tree differs from HEAD outside .dross/:
// whether there is code a commit would record.
func Dirty(dir string) (bool, error) {
	c, err := Changes(dir)
	if err != nil {
		return false, err
	}
	return c.Base != c.Tree, nil
}

// patchArgs pin every knob a repository's config could turn on a patch: no
// colour, no external diff driver, no textconv filter, no rename pairing,
// standard a/ b/ prefixes, three lines of context, quoted non-ASCII paths. Two
// machines — or the review verb and its recorder — render the same bytes.
var patchArgs = []string{
	"-c", "core.quotePath=true",
	"diff-tree", "-p", "--no-color", "--no-ext-diff", "--no-textconv", "--no-renames",
	"--src-prefix=a/", "--dst-prefix=b/", "-U3",
}

// Patch renders the diff between c.Base and c.Tree, leaving out the content
// of each path in exclude (matched literally, from the top of the tree).
func Patch(dir string, c Change, exclude []string) (string, error) {
	if c.Base == "" || c.Tree == "" {
		return "", fmt.Errorf("treefp: patch needs two fingerprints, got %q and %q", c.Base, c.Tree)
	}
	if c.Base == c.Tree {
		return "", nil
	}
	args := append(append([]string{}, patchArgs...), "--end-of-options", c.Base, c.Tree)
	if len(exclude) > 0 {
		args = append(args, "--")
		for _, p := range exclude {
			args = append(args, ":(top,exclude,literal)"+p)
		}
	}
	out, err := gitrun.RawWith(gitrun.Options{Timeout: Timeout}, dir, args...)
	if err != nil {
		return "", fmt.Errorf("git diff-tree -p: %w", err)
	}
	return out, nil
}

// base loads HEAD (or nothing, on an unborn HEAD) into the scratch index and
// writes it, .dross/ out.
func (s *scratch) base() (string, error) {
	args := []string{"read-tree", "HEAD"}
	if s.unborn {
		args = []string{"read-tree", "--empty"}
	}
	if _, err := s.git(s.dir, args...); err != nil {
		return "", err
	}
	return s.tree()
}

// scratch is a copy of one repository's real index.
type scratch struct {
	dir    string // where the candidate is taken
	real   string // the real index the copy came from
	tmp    string // the temp dir holding the copy
	index  string // the copy
	unborn bool   // HEAD names no commit yet
}

func open(dir string) (*scratch, error) {
	real, err := realIndex(dir)
	if err != nil {
		return nil, err
	}
	tmp, err := os.MkdirTemp("", "dross-treefp-")
	if err != nil {
		return nil, fmt.Errorf("treefp: scratch dir: %w", err)
	}
	s := &scratch{dir: dir, real: real, tmp: tmp, index: filepath.Join(tmp, "index")}
	if err := copyIndex(real, s.index); err != nil {
		s.close()
		return nil, err
	}
	_, err = gitrun.RawWith(gitrun.Options{Timeout: Timeout}, dir, "rev-parse", "--verify", "-q", "HEAD^{commit}")
	switch {
	case err == nil:
	case gitrun.ExitCode(err) == 1:
		s.unborn = true
	default:
		s.close()
		return nil, fmt.Errorf("git rev-parse HEAD: %w", err)
	}
	return s, nil
}

func (s *scratch) close() { os.RemoveAll(s.tmp) }

// realIndex is the index git uses for the work tree at dir — a linked
// worktree's own, not the main worktree's.
func realIndex(dir string) (string, error) {
	out, err := gitrun.RawWith(gitrun.Options{Timeout: Timeout}, dir, "rev-parse", "--git-path", "index")
	if err != nil {
		return "", fmt.Errorf("locate the git index for %s: %w", dir, err)
	}
	//dross:taint-cleared git rev-parse --git-path index prints the one path of the index file; it is only ever opened for reading or compared, never shown
	p := strings.TrimSpace(out)
	if p == "" {
		return "", fmt.Errorf("locate the git index for %s: git printed no path", dir)
	}
	if !filepath.IsAbs(p) {
		p = filepath.Join(dir, p)
	}
	return filepath.Clean(p), nil
}

// copyIndex copies the real index byte for byte. A repository with nothing
// staged yet has none, and the scratch starts empty.
func copyIndex(from, to string) error {
	b, err := os.ReadFile(from)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("treefp: read the git index: %w", err)
	}
	if err := os.WriteFile(to, b, 0o600); err != nil {
		return fmt.Errorf("treefp: copy the git index: %w", err)
	}
	return nil
}

// git runs one git command in dir against the scratch index.
func (s *scratch) git(dir string, args ...string) (string, error) {
	if s.index == "" {
		return "", errNoIndex
	}
	out, err := gitrun.RawWith(gitrun.Options{Timeout: Timeout, IndexFile: s.index}, dir, args...)
	if err != nil {
		return "", fmt.Errorf("git %s: %w", args[0], err)
	}
	return out, nil
}

// interactive are the `git add` options that wait for a human — a replay of
// one would hang the hook until its timeout.
var interactive = map[string]bool{"--patch": true, "--interactive": true, "--edit": true}

// replay stages one earlier `git add` into the scratch index. It must run in
// the same work tree: an add in another repository stages into another index.
func (s *scratch) replay(a Add) error {
	for _, arg := range a.Args {
		if arg == "--" {
			break
		}
		if interactive[arg] || (len(arg) > 1 && arg[0] == '-' && arg[1] != '-' && strings.ContainsAny(arg[1:], "pie")) {
			return fmt.Errorf("treefp: `git add %s` is interactive and cannot be replayed", strings.Join(a.Args, " "))
		}
	}
	dir := a.Dir
	if dir == "" {
		dir = s.dir
	}
	if dir != s.dir {
		other, err := realIndex(dir)
		if err != nil {
			return err
		}
		if !sameIndex(other, s.real) {
			return fmt.Errorf("treefp: the git add in %s stages into a different work tree than the commit in %s", dir, s.dir)
		}
	}
	// The replayed arguments are the agent's own `git add` options and
	// pathspecs, which git must read as such — they cannot be fenced as
	// values. Interactive forms are refused above.
	_, err := s.git(dir, append([]string{"add"}, a.Args...)...)
	return err
}

func sameIndex(a, b string) bool {
	if a == b {
		return true
	}
	da, errA := filepath.EvalSymlinks(filepath.Dir(a))
	db, errB := filepath.EvalSymlinks(filepath.Dir(b))
	return errA == nil && errB == nil && filepath.Base(a) == filepath.Base(b) && da == db
}

// changed lists the staged paths that differ from HEAD.
func (s *scratch) changed() ([]string, error) {
	var out string
	var err error
	if s.unborn {
		out, err = s.git(s.dir, "ls-files", "-z", "--full-name", "--", ":/")
	} else {
		out, err = s.git(s.dir, "diff-index", "--cached", "--name-only", "-z", "HEAD")
	}
	if err != nil {
		return nil, err
	}
	//dross:taint-cleared git diff-index --name-only -z / ls-files -z print NUL-separated repo-relative paths; each one is kept as a path and nothing else of git's output is
	return splitNUL(out), nil
}

// tree takes .dross/ out of the scratch index and writes it as a tree.
func (s *scratch) tree() (string, error) {
	if _, err := s.git(s.dir, "rm", "-r", "--cached", "-q", "--ignore-unmatch", "--", ":/.dross"); err != nil {
		return "", err
	}
	out, err := s.git(s.dir, "write-tree")
	if err != nil {
		return "", err
	}
	//dross:taint-cleared git write-tree prints one tree object id and nothing else
	tree := strings.TrimSpace(out)
	if tree == "" {
		return "", errors.New("git write-tree printed no tree id")
	}
	return tree, nil
}

func splitNUL(out string) []string {
	var paths []string
	for _, p := range strings.Split(out, "\x00") {
		if p != "" {
			paths = append(paths, p)
		}
	}
	return paths
}
