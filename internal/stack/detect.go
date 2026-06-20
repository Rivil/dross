package stack

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Unsupported is the id Detect returns when no profile's signals match. It is an
// explicit sentinel — never an empty string or a guessed default — so callers can
// distinguish "no stack matched" from "matched the empty stack".
const Unsupported = "unsupported"

// Signal weights: a root marker file (go.mod, package.json) is far stronger
// evidence of a stack than a stray source-file extension, so on a polyglot tree
// the file signal wins over an extension-only match.
const (
	fileSignalWeight = 100
	extSignalWeight  = 1
)

// skipDirs are never descended when collecting extension signals — VCS, vendored,
// and build noise would otherwise let a stray dependency file flip detection.
var skipDirs = map[string]bool{
	".git":         true,
	".dross":       true,
	"node_modules": true,
	"vendor":       true,
	"dist":         true,
	"build":        true,
	".idea":        true,
	".vscode":      true,
}

// Detect resolves the strongest-matching profile for the tree at root and returns
// its id, or Unsupported when nothing matches. Detection keys off each profile's
// declared Signals (root marker files + source extensions), not a hardcoded
// language switch — so a new profile becomes selectable purely by being supplied
// here, with no change to this function.
func Detect(root string, profiles []*Profile) string {
	rootFiles := rootFilenames(root)
	exts := extsInTree(root)

	best := Unsupported
	bestScore := 0
	bestPriority := 0
	for _, p := range profiles {
		s := scoreProfile(p, rootFiles, exts)
		if s == 0 {
			continue
		}
		if s > bestScore || (s == bestScore && p.Signals.Priority > bestPriority) {
			best, bestScore, bestPriority = p.ID, s, p.Signals.Priority
		}
	}
	return best
}

// scoreProfile weights a profile's matched signals against the evidence collected
// from the tree.
func scoreProfile(p *Profile, rootFiles, exts map[string]bool) int {
	score := 0
	for _, f := range p.Signals.Files {
		if rootFiles[f] {
			score += fileSignalWeight
		}
	}
	for _, e := range p.Signals.Exts {
		if exts[normalizeExt(e)] {
			score += extSignalWeight
		}
	}
	return score
}

// rootFilenames returns the set of entry names directly under root. A missing or
// unreadable root yields an empty set rather than a panic.
func rootFilenames(root string) map[string]bool {
	out := map[string]bool{}
	entries, err := os.ReadDir(root)
	if err != nil {
		return out
	}
	for _, e := range entries {
		if !e.IsDir() {
			out[e.Name()] = true
		}
	}
	return out
}

// extsInTree returns the set of file extensions present anywhere under root,
// skipping VCS/vendor/build noise. A missing root yields an empty set.
func extsInTree(root string) map[string]bool {
	out := map[string]bool{}
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if path != root && skipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if ext := filepath.Ext(d.Name()); ext != "" {
			out[ext] = true
		}
		return nil
	})
	return out
}

func normalizeExt(e string) string {
	if e == "" {
		return e
	}
	if !strings.HasPrefix(e, ".") {
		return "." + e
	}
	return e
}
