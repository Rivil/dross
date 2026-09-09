// Package pathfence is the one containment check for paths dross loads from
// tracked artifacts.
//
// The problem it solves: `.dross/` files are committed, hand-editable, and read
// on every machine that clones the repo. A changes.json whose task `files`
// names "../x.go", or a red_proof.doc pointing at "../../victim.md", is a path
// the repo supplied and dross then opens. Before this package there were two
// independent ..-prefix tests — containedPath in internal/cmd and the --doc
// escape test in red-proof set — guarding two of the sites and none of the
// others, so the same bug lived one field over.
//
// The guarantee is carried by a TYPE, not by a hand-maintained list of guarded
// call sites. Contained holds an unexported field, so no package outside
// pathfence can construct one except through Contain: a consumer that skips the
// check has no value to pass and fails to build. The I/O seam takes a Contained
// rather than a string, which is what makes the type load-bearing rather than
// decorative — os.ReadFile takes a string and always will, so the guarantee is
// "a site that opens a tracked path through the seam cannot have skipped the
// check", with a residual AST scan covering the sites that reach for os.*
// directly.
//
// LEXICAL ONLY. The check compares cleaned path strings and never calls
// filepath.EvalSymlinks, so a lexically-contained path that is a symlink
// pointing outside the root is ACCEPTED (see the test named for the
// symlink_resolution lock). This is deliberate: lexical checking is
// deterministic and works on paths that do not exist yet, which the write path
// needs, whereas EvalSymlinks requires the target to exist, is still
// TOCTOU-racy, and would not close the hole it appears to. The threat here is a
// mistyped or hand-edited artifact, not an attacker who can already write
// symlinks into the tree.
package pathfence

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// ErrAbsolute is returned for an absolute path. It is distinct from ErrEscapes
// on purpose: an absolute path never escapes (filepath.Join would silently
// re-root it), so refusing it is a DIAGNOSABILITY choice, not a security one. A
// tracked artifact is read on every machine that clones the repo, so an
// absolute path is one machine's layout and is always a mistake — re-rooting it
// turns that mistake into a confusing file-not-found instead of naming the real
// problem.
var ErrAbsolute = errors.New("absolute path in tracked artifact")

// ErrEscapes is returned for a path that resolves outside its root.
var ErrEscapes = errors.New("path escapes its root")

// Contained is a path that has been through Contain. Both fields are
// unexported, so a composite literal outside this package cannot set either
// one and there is no way to mint a Contained without the check.
//
// It carries BOTH forms because callers need both and conflating them causes
// real bugs. See String and Rel.
type Contained struct {
	rel string // root-relative, slash-separated — for humans and for storage
	abs string // joined, OS-form — the only form that can be opened
}

// String returns the joined OS-form path: the form that can be handed to any
// func(path string) which actually opens the file, such as phase.saveTOML
// beneath WriteScaffoldSpec.
//
// It is deliberately NOT the relative form. An earlier design made String
// relative to serve display consumers, and that broke every boundary where a
// Contained has to reach code which is not itself being retyped — a type whose
// only accessor yields an unopenable string forces either an absolute-accessor
// escape hatch or an unbounded retyping cascade. Two named accessors cost one
// method and end the problem.
//
// Reaching os.* with this value is banned by the residual scan even though it
// would work: the seam is the audited way in.
func (c Contained) String() string { return c.abs }

// Rel returns the path relative to the root it was contained against, in slash
// form, for display, error messages and serialized path lists.
//
// Never hand this to something that opens a file: it resolves against the
// process working directory, not the root, so it silently reads or writes the
// wrong path. The residual scan bans it inside an os.* argument for exactly
// that reason.
func (c Contained) Rel() string { return c.rel }

// Contain refuses p unless it stays inside root, and returns the joined result.
//
// artifact names the file p was loaded from ("changes.json", "run directory")
// and appears in the error, so a hand-edited artifact can be fixed from the
// message alone: the message names the offending path, the artifact it came
// from, and the root it escaped.
//
// It never touches the filesystem — root need not exist.
func Contain(root, artifact, p string) (Contained, error) {
	raw := strings.TrimSpace(p)
	if raw == "" {
		return Contained{}, fmt.Errorf("%s: empty path (root %s): %w", artifact, root, ErrEscapes)
	}
	slashed := toSlash(raw)
	if path.IsAbs(slashed) {
		return Contained{}, fmt.Errorf(
			"%s: %q is an absolute path, but %s records paths relative to %s: %w",
			artifact, p, artifact, root, ErrAbsolute)
	}
	clean := path.Clean(slashed)
	if escapes(clean) {
		return Contained{}, fmt.Errorf(
			"%s: %q resolves outside %s: %w", artifact, p, root, ErrEscapes)
	}
	return Contained{
		rel: clean,
		abs: filepath.Join(root, filepath.FromSlash(clean)),
	}, nil
}

// InTree classifies p WITHOUT a root: it reports whether p is lexically
// in-tree, and returns the cleaned form either way.
//
// It takes no root because its two consumers disagree about root handling —
// verify strips one, testlane has none — so pathfence owns only the lexical
// ..-and-absolute test and each caller keeps its own root policy at its own
// call site. For the same reason it takes NO position on "" or ".": both are
// reported in-tree here, and each caller applies its own rule visibly.
//
// The cleaned path is returned on the false branch too, not "": testlane
// buckets that string, so returning "" would silently change what lands in its
// escaped bucket.
func InTree(p string) (string, bool) {
	raw := strings.TrimSpace(p)
	if raw == "" {
		// Not Clean("") — that is ".", which would be a position on the empty
		// path. The caller's own policy decides what "" means.
		return "", true
	}
	slashed := toSlash(raw)
	if path.IsAbs(slashed) {
		return path.Clean(slashed), false
	}
	clean := path.Clean(slashed)
	return clean, !escapes(clean)
}

// Segment refuses id unless it is a single path segment, so a value used as one
// component of a path cannot introduce a separator or a traversal. label names
// what is being checked ("run id") and appears in the error.
func Segment(label, id string) error {
	trimmed := strings.TrimSpace(id)
	if trimmed == "" {
		return fmt.Errorf("empty %s", label)
	}
	if strings.ContainsAny(trimmed, `/\`) {
		return fmt.Errorf("%s %q contains a path separator, but it must be a single segment", label, id)
	}
	if trimmed == "." || trimmed == ".." {
		return fmt.Errorf("%s %q is a path traversal, not a name", label, id)
	}
	return nil
}

// ReadFile reads the contained path.
func ReadFile(c Contained) ([]byte, error) {
	if err := c.check(); err != nil {
		return nil, err
	}
	return os.ReadFile(c.abs)
}

// WriteFile writes the contained path.
func WriteFile(c Contained, data []byte, perm fs.FileMode) error {
	if err := c.check(); err != nil {
		return err
	}
	return os.WriteFile(c.abs, data, perm)
}

// Stat stats the contained path.
func Stat(c Contained) (fs.FileInfo, error) {
	if err := c.check(); err != nil {
		return nil, err
	}
	return os.Stat(c.abs)
}

// check refuses the zero Contained.
//
// The unexported field stops a caller CONSTRUCTING a path it did not check, but
// Go permits an empty composite literal outside the package — `Contained{}` and
// `[]Contained{{}, {}}` both compile anywhere. So the seam refuses the zero
// value rather than operating on an empty path, which would resolve against the
// process working directory. This is the runtime half of a guarantee the type
// system carries everywhere else.
func (c Contained) check() error {
	if c.abs == "" {
		return fmt.Errorf("uninitialised pathfence.Contained: %w", ErrEscapes)
	}
	return nil
}

// toSlash folds separators UNCONDITIONALLY rather than via filepath.ToSlash.
//
// filepath.ToSlash is the identity on darwin, which would leave the rule
// untested on the machine the suite runs on: `a\b/../c` would clean to "c"
// there (path.Clean sees `a\b` as one segment, which ".." then pops) and to
// "a/c" on Windows. Folding first makes the answer "a/c" everywhere. This
// mirrors verify.NormalizePath, which folds unconditionally for the same
// reason.
func toSlash(p string) string { return strings.ReplaceAll(p, `\`, "/") }

// escapes reports whether a CLEANED slash path leaves its root. The separator
// test matters: "..foo", "a/..b" and "a/b.." are ordinary names, and a bare
// HasPrefix(p, "..") would refuse all three.
func escapes(clean string) bool {
	return clean == ".." || strings.HasPrefix(clean, "../")
}
