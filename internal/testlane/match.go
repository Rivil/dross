// Package testlane decides which test lanes a set of file paths belongs to.
//
// A lane is a named list of globs paired with a command (project.TestLane).
// Given the paths a task touched, `dross test --files` needs to know which
// lanes to run — and, just as importantly, which paths it could not place.
// That decision is pure: no filesystem, no config loading, no spawning. It
// lives here so it can be tested exhaustively against strings, and so the
// command layer is left holding only policy.
//
// Three properties are load-bearing:
//
//   - **A miss is a result, not a silence.** Every in-tree path that matches no
//     lane comes back in Unmatched. A selector that quietly dropped them would
//     let a file set report green having measured nothing, which is the exact
//     failure `dross test --files` exists to prevent.
//   - **Out-of-tree is its own category, not a kind of miss.** An absolute path
//     or one escaping the repo with `..` is a caller mistake — bad argv. A path
//     that is in the tree and matches nothing is a lane-configuration gap.
//     Collapsing the two sends a user to edit project.toml over a typo in their
//     own command line, so they are reported separately and the caller decides
//     what each one means.
//   - **Lane order is declaration order.** Indices come back ascending and
//     deduplicated, so array position confers no precedence and a path caught
//     by two globs of one lane never runs that lane's command twice.
//
// Glob syntax is matched segment-wise over filepath.Match, with `**` added as a
// whole segment meaning zero or more path segments. Deliberately hand-rolled:
// the shipped binary is a single static artifact with no runtime dependencies,
// and this is a page of code against a module dependency.
package testlane

import (
	"path"
	"path/filepath"
	"strings"
)

// Selection is what Select found: which lanes to run, and every path it could
// not place, split by whose fault the miss is.
//
// The zero Selection means "nothing to run and nothing to report", which is
// what an empty path set produces.
type Selection struct {
	// Lanes are indices into the globs argument, ascending and deduplicated.
	Lanes []int
	// Unmatched are in-tree paths matching no lane, as the caller wrote them.
	Unmatched []string
	// OutOfTree are paths naming something outside the repo — absolute, or
	// escaping upward through `..` — as the caller wrote them.
	OutOfTree []string
	// Matched records, per lane index, the paths that put that lane in Lanes,
	// in NORMALIZED repo-relative form. It is keyed only for lanes Lanes also
	// carries, so a caller ranging over it can never resurrect a lane the
	// selection excluded.
	//
	// Normalized rather than caller-spelled, which is the opposite of
	// Unmatched and OutOfTree and deliberate: those two are read by a human
	// looking for the path they typed, while these are appended to a runner's
	// command line, where `./internal/a.go` and `internal/a.go` must not
	// derive two different selectors.
	Matched map[int][]string
}

// Select resolves paths against lanes. globs[i] is lane i's match list.
//
// Paths are reported back in the caller's own spelling, not the normalized
// form: a user who typed `./internal/a.go` should read `./internal/a.go` in the
// refusal, or they will go looking for a path they never wrote.
func Select(globs [][]string, paths []string) Selection {
	var sel Selection
	hit := make([]bool, len(globs))
	seen := map[string]bool{}

	for _, raw := range paths {
		norm, inTree := normalize(raw)
		// Deduplicated by normalized form, so `a.go` and `./a.go` in one
		// argv do not produce the same complaint twice.
		if seen[norm] {
			continue
		}
		seen[norm] = true

		if !inTree {
			sel.OutOfTree = append(sel.OutOfTree, raw)
			continue
		}
		matched := false
		for i, lane := range globs {
			for _, g := range lane {
				if matchGlob(g, norm) {
					hit[i] = true
					matched = true
					if sel.Matched == nil {
						sel.Matched = map[int][]string{}
					}
					// Appended inside the same one-hit break as hit[i], so
					// the path is recorded once per LANE however many of
					// that lane's globs would have caught it — the same
					// rule that keeps the lane's command from running twice.
					sel.Matched[i] = append(sel.Matched[i], norm)
					// One hit settles this lane: a second glob in the
					// same lane matching the same path must not enqueue
					// the lane's command a second time.
					break
				}
			}
		}
		if !matched {
			sel.Unmatched = append(sel.Unmatched, raw)
		}
	}

	for i, h := range hit {
		if h {
			sel.Lanes = append(sel.Lanes, i)
		}
	}
	return sel
}

// normalize reduces one caller-supplied path to the repo-relative slash form
// the globs are written in, and reports whether it stayed inside the tree.
//
// Cleaning happens BEFORE the escape check, so `internal/../../x` is caught as
// an escape rather than passing as a path with a harmless-looking `..` in the
// middle of it.
func normalize(p string) (norm string, inTree bool) {
	s := filepath.ToSlash(strings.TrimSpace(p))
	if s == "" {
		// Not a path at all. In-tree so it surfaces in Unmatched rather
		// than being reported as an escape, which it is not.
		return "", true
	}
	// filepath.IsAbs as well as the leading slash: on Windows a drive-letter
	// path is absolute without one, and an absolute path is out of the tree
	// wherever it came from.
	if filepath.IsAbs(p) || strings.HasPrefix(s, "/") {
		return path.Clean(s), false
	}
	c := path.Clean(s)
	if c == ".." || strings.HasPrefix(c, "../") {
		return c, false
	}
	return c, true
}

// matchGlob reports whether one lane pattern matches one normalized path.
//
// A trailing slash means the directory and everything beneath it: `docs/` is
// the shorthand a user reaches for and it would otherwise match nothing at all,
// since no file path ends in a separator.
//
// A pattern that does not compile matches nothing. It is reported as a
// project.toml problem by `dross validate`, which is where a broken lane
// belongs — returning an error from every match call would push that reporting
// into every caller for a fault none of them can fix.
func matchGlob(pattern, p string) bool {
	pattern = filepath.ToSlash(strings.TrimSpace(pattern))
	if pattern == "" {
		return false
	}
	if strings.HasSuffix(pattern, "/") {
		pattern += "**"
	}
	return matchSegments(strings.Split(pattern, "/"), strings.Split(p, "/"))
}

// matchSegments is the segment-wise walk. `**` is the only construct
// filepath.Match cannot express: it consumes zero or more whole segments, which
// is why it is resolved here and every other segment is delegated.
func matchSegments(pat, name []string) bool {
	if len(pat) == 0 {
		return len(name) == 0
	}
	if pat[0] == "**" {
		// Zero segments first: `internal/**/*.go` has to match
		// internal/x.go, or `**` would mean "at least one directory" and
		// every lane written with it would silently skip the top level.
		for i := 0; i <= len(name); i++ {
			if matchSegments(pat[1:], name[i:]) {
				return true
			}
		}
		return false
	}
	if len(name) == 0 {
		return false
	}
	ok, err := filepath.Match(pat[0], name[0])
	if err != nil || !ok {
		return false
	}
	return matchSegments(pat[1:], name[1:])
}
