package codex

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// Languages is the set of languages with first-class indexer support.
// Files with extensions outside this set still get sibling listing and
// git-log neighbour data, just no symbol extraction.
//
// v1 ships Go-only. The package doc has the implementation sketch for
// adding TS/Svelte/C#/GDScript/HTML/CSS adapters when they're needed.
var Languages = map[string]string{
	".go": "go",
}

// Indexer extracts symbols + intra-file references for one language.
// Each language has its own implementation (currently just Go via
// go/ast). The Dispatch helper picks the right one for a file.
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

	indexers := []Indexer{&GoIndexer{}}

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

	// Stable output ordering for testability.
	sort.Slice(res.Symbols, func(i, j int) bool {
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
}

// ErrUnsupportedLanguage is returned when no Indexer matches a file's
// extension. Callers can ignore this — a file without symbol support
// still gets siblings + git log.
var ErrUnsupportedLanguage = errors.New("codex: no indexer for this language")
