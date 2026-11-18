package codex

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// Indexer extracts symbols for one language. Each language has its
// own implementation; allIndexers() registers them all and Index()
// dispatches by file extension. Go uses stdlib go/ast (no external
// dependency); the rest shell out to ast-grep when it's on PATH and
// fall back to empty Symbols when it isn't.
type Indexer interface {
	Name() string
	Supports(file string) bool
	Symbols(file string) ([]Symbol, error)
}

// Index builds a Result for the given target files. Each file is
// classified by extension, symbol-extracted if a matching Indexer
// exists, then enriched with sibling listings and recent git activity
// for the file's parent directory.
//
// Errors from individual files are collected onto the Result; one
// unparseable file doesn't kill the whole index. The error return is
// reserved for catastrophic failures (e.g. cwd unreadable).
func Index(targetFiles []string) (*Result, error) {
	res := &Result{TargetFiles: targetFiles}
	if len(targetFiles) == 0 {
		return res, nil
	}

	indexers := allIndexers()

	dirSeen := map[string]bool{}
	for _, f := range targetFiles {
		if a, err := filepath.Abs(f); err == nil {
			f = a
		}
		// Symbols
		if idx := dispatch(indexers, f); idx != nil {
			syms, err := idx.Symbols(f)
			if err != nil {
				res.Errors = append(res.Errors, fmt.Sprintf("%s: %v", f, err))
			}
			res.Symbols = append(res.Symbols, syms...)
		}

		// Siblings + git log: dedup per parent dir so multi-file
		// targets in the same dir don't repeat.
		dir := filepath.Dir(f)
		if dirSeen[dir] {
			continue
		}
		dirSeen[dir] = true

		sibs, err := siblings(dir, f)
		if err == nil {
			res.Siblings = append(res.Siblings, sibs...)
		}

		recent, err := recentLog(dir)
		if err == nil {
			res.RecentLog = append(res.RecentLog, recent...)
		}
	}

	// Cross-file references: for each symbol we found, look for its
	// name in other source files. Grep-based; cheap; false-positive-
	// prone for common names. Refinement (per-language ref parsing)
	// is documented in the package doc.
	res.Callers = findCallers(targetFiles, res.Symbols)

	// Stable output ordering for testability. SliceStable keeps symbols that
	// share a line (`var a, b int`) in declaration order, matching Symbols.
	sort.SliceStable(res.Symbols, func(i, j int) bool {
		if res.Symbols[i].File != res.Symbols[j].File {
			return res.Symbols[i].File < res.Symbols[j].File
		}
		return res.Symbols[i].Line < res.Symbols[j].Line
	})
	sort.Strings(res.Siblings)
	return res, nil
}

func dispatch(indexers []Indexer, file string) Indexer {
	for _, idx := range indexers {
		if idx.Supports(file) {
			return idx
		}
	}
	return nil
}

// SupportsFile reports whether codex has an indexer for this file's language
// (decided by extension). It routes through the same allIndexers() registry
// that Index dispatches on, so a newly registered language is recognised here
// with no change. Consumers use it to tell "a language codex can't index"
// (skip) apart from "a symbol that's genuinely gone" (unresolved).
//
// Note: this reflects *language* coverage, not tool availability — for an
// ast-grep-backed language it returns true even when the ast-grep binary is
// absent (in which case Symbols() is empty). Go (stdlib parser) always works.
func SupportsFile(file string) bool {
	return dispatch(allIndexers(), file) != nil
}

// CanIndex reports whether codex can actually extract symbols from this file
// right now: its language has an indexer (SupportsFile) AND that indexer can
// run. Go always can (stdlib parser); an ast-grep-backed language can only
// when the ast-grep binary is on PATH. Unlike SupportsFile it reflects tool
// availability, so a consumer can tell "no symbols because none exist" apart
// from "no symbols because the tool to find them is missing".
func CanIndex(file string) bool {
	idx := dispatch(allIndexers(), file)
	if idx == nil {
		return false
	}
	if _, isGo := idx.(*GoIndexer); isGo {
		return true
	}
	return astGrepAvailable()
}

// Symbols extracts the top-level symbols of one file through the same
// indexer dispatch Index uses, and nothing else: no sibling listing, no
// cross-file caller grep, no git log. It is the cheap per-file entry point for
// a consumer that indexes a whole tree. The file path is made absolute and the
// result ordered by line, so Symbols(f) equals Index([]string{f}).Symbols.
//
// A file in a language with no indexer returns ErrUnsupportedLanguage; a file
// its indexer cannot parse returns that indexer's error.
func Symbols(file string) ([]Symbol, error) {
	// Mirrors Index exactly so Symbols(f) == Index([f]).Symbols holds on every
	// path: filepath.Abs fails only when the working directory is unreadable,
	// and then Index keeps the path as given too. The indexer still opens the
	// file through that path, so an unreadable target surfaces as its error.
	if a, err := filepath.Abs(file); err == nil {
		file = a
	}
	idx := dispatch(allIndexers(), file)
	if idx == nil {
		return nil, ErrUnsupportedLanguage
	}
	syms, err := idx.Symbols(file)
	if err != nil {
		return nil, err
	}
	sort.SliceStable(syms, func(i, j int) bool { return syms[i].Line < syms[j].Line })
	return syms, nil
}

// siblings returns the names of files in dir, excluding the target
// file itself and any subdirectories. Used to surface "what else lives
// here" context to the LLM.
func siblings(dir, target string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	targetName := filepath.Base(target)
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if e.Name() == targetName {
			continue
		}
		out = append(out, filepath.Join(dir, e.Name()))
	}
	return out, nil
}

// Result is what `dross codex <target>` prints to stdout for the LLM
// to read. Schema is stable across implementations (Go-only today,
// multi-language later).
type Result struct {
	TargetFiles []string
	Symbols     []Symbol // top-level definitions in target files
	Callers     []Symbol // best-effort cross-file references
	Siblings    []string // files in same dir
	RecentLog   []string // git log lines for the touched dirs
	Errors      []string // per-file extraction failures (non-fatal)
}

// Symbol represents a top-level definition in a source file.
type Symbol struct {
	Name string
	Kind string // function | type | method | var | const
	File string
	Line int
	// Package is the declaring package's name from the file's package clause
	// (Go only; "" for languages with no package concept). Name is unchanged
	// by it — a method stays "T.M" — so a qualified citation `pkg.T.M` matches
	// on Package + Name.
	Package string
}

// ErrUnsupportedLanguage is returned when no Indexer matches a file's
// extension. Callers can ignore this — a file without symbol support
// still gets siblings + git log.
var ErrUnsupportedLanguage = errors.New("codex: no indexer for this language")
