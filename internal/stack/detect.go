package stack

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
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

// extLang maps source-file extensions to languages. Unknown extensions are
// ignored — detection never crashes on an extension it doesn't recognise. This is
// the single canonical map: security and quality recon both delegate to
// DetectLanguages so the ext->lang mapping can never drift across copies.
var extLang = map[string]string{
	".go":    "go",
	".py":    "python",
	".js":    "javascript",
	".jsx":   "javascript",
	".ts":    "typescript",
	".tsx":   "typescript",
	".rb":    "ruby",
	".rs":    "rust",
	".java":  "java",
	".kt":    "kotlin",
	".c":     "c",
	".h":     "c",
	".cc":    "cpp",
	".cpp":   "cpp",
	".cs":    "csharp",
	".php":   "php",
	".swift": "swift",
}

// DetectLanguages walks the tree at root and returns the sorted, de-duplicated set
// of languages found, mapped from file extensions. It skips VCS/vendor/build noise
// (including .dross, keeping audit sweeps context-free) and silently ignores files
// with unknown extensions. Unlike Detect (which resolves a single best profile),
// this returns every language present — what the security/quality tool sweeps need.
func DetectLanguages(root string) ([]string, error) {
	set := map[string]bool{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != root && skipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if lang, ok := extLang[filepath.Ext(d.Name())]; ok {
			set[lang] = true
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(set))
	for l := range set {
		out = append(out, l)
	}
	sort.Strings(out)
	return out, nil
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
