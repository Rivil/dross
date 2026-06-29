package stack

import (
	"io"
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

// extLangFor derives the ext->languages map by UNION over every profile's declared
// Signals.Exts: each extension maps to the id of every profile that claims it, so a
// shared extension (e.g. .ts in both svelte@6 and typescript@4) yields BOTH
// languages — no profile's language is ever dropped (the amended ext_clash_resolution
// decision). This is a pure function of the profile slice — no filesystem — so the
// derivation is unit-testable in isolation. There is no hardcoded ext->lang map: the
// mapping is single-sourced from the loaded profiles, so adding a profile extends
// language detection with zero code change here.
func extLangFor(profiles []*Profile) map[string][]string {
	m := map[string][]string{}
	for _, p := range profiles {
		for _, e := range p.Signals.Exts {
			ext := normalizeExt(e)
			if !langListHas(m[ext], p.ID) {
				m[ext] = append(m[ext], p.ID)
			}
		}
	}
	for ext := range m {
		sort.Strings(m[ext])
	}
	return m
}

// langListHas reports whether langs already contains id (keeps extLangFor's per-ext
// language list de-duplicated even if two profiles share an id).
func langListHas(langs []string, id string) bool {
	for _, l := range langs {
		if l == id {
			return true
		}
	}
	return false
}

// detectLanguagesFrom walks the tree at root and returns the sorted, de-duplicated
// set of languages found, deriving ext->lang from the supplied profiles (via
// extLangFor) rather than a hardcoded map. It skips VCS/vendor/build noise
// (including .dross, keeping audit sweeps context-free) and silently ignores files
// with extensions no profile claims. Parameterizing the profile slice keeps the
// core filesystem-walk testable without a real user overlay; DetectLanguages is the
// production wrapper that supplies the loaded set.
func detectLanguagesFrom(root string, profiles []*Profile) ([]string, error) {
	extToLangs := extLangFor(profiles)
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
		for _, lang := range extToLangs[filepath.Ext(d.Name())] {
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

// DetectLanguages walks the tree at root and returns the sorted, de-duplicated set
// of languages found, mapped from file extensions. It skips VCS/vendor/build noise
// (including .dross, keeping audit sweeps context-free) and silently ignores files
// with extensions no profile claims. Unlike Detect (which resolves a single best
// profile), this returns every language present — what the security/quality tool
// sweeps need.
//
// The ext->lang mapping is derived from the loaded profile set (embedded built-ins
// overlaid by ~/.claude/dross/profiles), so dropping in a profile extends language
// detection with no code change (c-2/c-3). A malformed user profile is tolerated:
// LoadAll still returns the merged embedded set, so detection never crashes on a bad
// drop-in — only a total failure to load any profile surfaces as an error.
func DetectLanguages(root string) ([]string, error) {
	profiles, err := LoadAll()
	if len(profiles) == 0 && err != nil {
		return nil, err
	}
	return detectLanguagesFrom(root, profiles)
}

// MarkerProfiles returns the sorted ids of every profile whose Signals.FilePatterns
// match a filename anywhere in the tree at root. Unlike Detect (winner-take-all over
// a profile's Files/Exts), this is additive and pattern-driven: a marker-file stack is
// surfaced ON TOP of any source languages rather than instead of them, so the security
// and quality manifests can union its tools in. The walk reuses skipDirs to ignore
// VCS/vendor/build noise and descends the whole subtree, so a marker file in a
// subdirectory is still caught. A missing or unreadable root yields an empty slice.
//
// This deliberately touches neither Detect nor DetectLanguages — it is a separate,
// data-driven seam (the marker_detection_additive decision).
func MarkerProfiles(root string, profiles []*Profile) []string {
	matched := map[string]bool{}
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
		name := d.Name()
		for _, p := range profiles {
			if matched[p.ID] || !p.Signals.MatchesFile(name) {
				continue
			}
			// Pure-glob fast path: a profile with no content requirements matches on
			// filename alone and its candidate body is never read.
			if p.Signals.Content.IsZero() {
				matched[p.ID] = true
				continue
			}
			// Content-gated: confirm the glob candidate's body against the profile's
			// tokens. An unreadable candidate is skipped (ok=false), never fatal.
			if body, ok := readCapped(path, contentSniffCap); ok && p.Signals.Content.Matches(body) {
				matched[p.ID] = true
			}
		}
		return nil
	})
	out := make([]string, 0, len(matched))
	for id := range matched {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// contentSniffCap bounds how many bytes a content gate reads from a candidate, so a
// huge file can't blow up memory and a marker planted past the cap is treated as
// absent. 64 KiB comfortably covers a real manifest/template header.
const contentSniffCap = 64 * 1024

// readCapped returns up to limit bytes from the file at path. It returns ok=false
// when the file cannot be opened or read, so a content-gated profile is simply
// skipped on an unreadable or vanished candidate rather than panicking. A short read
// (file smaller than limit) is normal and returned as-is.
func readCapped(path string, limit int) ([]byte, bool) {
	f, err := os.Open(path)
	if err != nil {
		return nil, false
	}
	defer f.Close()
	buf := make([]byte, limit)
	n, err := io.ReadFull(f, buf)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return nil, false
	}
	return buf[:n], true
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
