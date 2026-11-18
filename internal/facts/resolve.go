package facts

import (
	"fmt"
	"path"
	"path/filepath"
	"strings"

	"github.com/Rivil/dross/internal/architecture"
	"github.com/Rivil/dross/internal/codex"
)

// Seams over codex's language checks, so a test can simulate a missing
// ast-grep binary without touching codex's own state.
var (
	canIndex     = codex.CanIndex
	supportsFile = codex.SupportsFile
	fileSymbols  = codex.Symbols
)

// unindexedSourceExts are source languages codex has no indexer for at all.
// A tree holding one (outside testdata/fixtures) cannot have its symbols
// checked completely.
var unindexedSourceExts = map[string]bool{
	".js": true, ".jsx": true, ".mjs": true, ".cjs": true, ".py": true,
	".rs": true, ".kt": true, ".dart": true, ".java": true,
}

// StrictMatch is the facts Matcher (locked symbol_shape): a cited name matches
// a symbol only when every qualifier matches — the bare Name ("Init",
// "Changes.Record") or the package-qualified "pkg.Name" ("phase.Spec",
// "pkg.T.M"). There is no last-component fallback, so `quoted.String` never
// resolves against some other type's String method.
func StrictMatch(syms []codex.Symbol, cited string) []codex.Symbol {
	var out []codex.Symbol
	for _, s := range syms {
		if s.Name == cited || (s.Package != "" && s.Package+"."+s.Name == cited) {
			out = append(out, s)
		}
	}
	return out
}

// Resolved is the outcome of checking one line or symbol citation. Status
// reuses the ARCHITECTURE.md link check's classification; NewLine is set only
// for StatusMoved.
type Resolved struct {
	Status  architecture.Status
	NewLine int
}

// treeIndex is every symbol in the tree, keyed both ways. Symbol.File is
// repo-relative and slash-separated.
type treeIndex struct {
	syms        []codex.Symbol
	byFile      map[string][]codex.Symbol
	degraded    bool
	degradedWhy string
}

// buildIndex indexes files under root. A seam so a test can count builds.
var buildIndex = buildTreeIndex

// Resolver resolves citations against one tree. The file list and the symbol
// index are both built lazily, at most once: a spec citing no symbol and no
// paired file:line never parses a single file.
type Resolver struct {
	root    string
	files   []string
	fileErr error
	listed  bool
	idx     *treeIndex
}

// NewResolver returns a Resolver for the tree at root.
func NewResolver(root string) *Resolver { return &Resolver{root: root} }

// Files is the shared tree walk (Tree), computed once.
func (r *Resolver) Files() ([]string, error) {
	if !r.listed {
		r.files, r.fileErr = Tree(r.root)
		r.listed = true
	}
	return r.files, r.fileErr
}

// Degraded reports whether the symbol index, if built, could not cover every
// source file — and why. An unbuilt index is never degraded.
func (r *Resolver) Degraded() (bool, string) {
	if r.idx == nil {
		return false, ""
	}
	return r.idx.degraded, r.idx.degradedWhy
}

func (r *Resolver) index() (*treeIndex, error) {
	if r.idx != nil {
		return r.idx, nil
	}
	files, err := r.Files()
	if err != nil {
		return nil, err
	}
	r.idx = buildIndex(r.root, files)
	return r.idx, nil
}

// Line resolves a file:line citation that carries a paired symbol. An
// unpaired file:line has nothing to classify here (its path existence is
// checked separately) and is Skipped. A bare basename shared by several files
// is Ambiguous, never guessed; one naming no file is Unresolved.
func (r *Resolver) Line(c Citation) (Resolved, error) {
	if c.Kind != KindLine {
		return Resolved{}, fmt.Errorf("facts: Line given a %s citation %q", c.Kind, c.Raw)
	}
	if c.Symbol == "" {
		return Resolved{Status: architecture.StatusSkipped}, nil
	}
	files, err := r.Files()
	if err != nil {
		return Resolved{}, err
	}
	cands := locate(files, c.Path)
	switch {
	case len(cands) == 0:
		return Resolved{Status: architecture.StatusUnresolved}, nil
	case len(cands) > 1:
		return Resolved{Status: architecture.StatusAmbiguous}, nil
	}
	file := cands[0]
	if !canIndex(file) {
		return Resolved{Status: architecture.StatusSkipped}, nil
	}
	idx, err := r.index()
	if err != nil {
		return Resolved{}, err
	}
	link := architecture.SymbolLink{Symbol: c.Symbol, File: file, Line: c.Line}
	res := architecture.Classify(link, idx.byFile[file], StrictMatch)
	return Resolved{Status: res.Status, NewLine: res.NewLine}, nil
}

// Symbol resolves a standalone symbol citation against the whole tree. One
// match is OK, several Ambiguous. No match is Unresolved — unless the index is
// degraded, when the symbol may live in a file codex could not read, and it is
// Skipped instead.
func (r *Resolver) Symbol(c Citation) (Resolved, error) {
	if c.Kind != KindSymbol {
		return Resolved{}, fmt.Errorf("facts: Symbol given a %s citation %q", c.Kind, c.Raw)
	}
	idx, err := r.index()
	if err != nil {
		return Resolved{}, err
	}
	switch n := len(StrictMatch(idx.syms, c.Symbol)); {
	case n == 1:
		return Resolved{Status: architecture.StatusOK}, nil
	case n > 1:
		return Resolved{Status: architecture.StatusAmbiguous}, nil
	case idx.degraded:
		return Resolved{Status: architecture.StatusSkipped}, nil
	default:
		return Resolved{Status: architecture.StatusUnresolved}, nil
	}
}

// locate maps a cited path to the tree files it names: a slashed path names
// itself (cleaned), a bare basename names every file with that base name.
func locate(files []string, cited string) []string {
	p := path.Clean(cited)
	var out []string
	if strings.Contains(cited, "/") {
		for _, f := range files {
			if f == p {
				return []string{f}
			}
		}
		return nil
	}
	for _, f := range files {
		if path.Base(f) == p {
			out = append(out, f)
		}
	}
	return out
}

// inFixtureDir reports whether a repo-relative path sits under a testdata/ or
// fixtures/ directory — sample inputs, not the project's own source.
func inFixtureDir(f string) bool {
	for _, part := range strings.Split(path.Dir(f), "/") {
		if part == "testdata" || part == "fixtures" {
			return true
		}
	}
	return false
}

// buildTreeIndex indexes every file codex can read. A file that fails to
// parse is skipped, never fatal. The index is degraded when a source file
// outside testdata/fixtures is one codex cannot index: a supported language
// whose tool is missing, or a language with no indexer at all.
func buildTreeIndex(root string, files []string) *treeIndex {
	idx := &treeIndex{byFile: map[string][]codex.Symbol{}}
	for _, f := range files {
		if !inFixtureDir(f) && !idx.degraded {
			ext := strings.ToLower(path.Ext(f))
			switch {
			case supportsFile(f) && !canIndex(f):
				idx.degraded = true
				idx.degradedWhy = fmt.Sprintf("%s: its language indexer (ast-grep) is not installed", f)
			case unindexedSourceExts[ext]:
				idx.degraded = true
				idx.degradedWhy = fmt.Sprintf("%s: codex has no indexer for %s files", f, ext)
			}
		}
		if !canIndex(f) {
			continue
		}
		syms, err := fileSymbols(filepath.Join(root, filepath.FromSlash(f)))
		if err != nil {
			// Unparsable: never fatal. A broken fixture is expected; a broken
			// project file may hold a cited symbol, so the index can no longer
			// call an unmatched symbol gone.
			if !inFixtureDir(f) && !idx.degraded {
				idx.degraded = true
				idx.degradedWhy = fmt.Sprintf("%s: could not be parsed", f)
			}
			continue
		}
		for i := range syms {
			syms[i].File = f
		}
		idx.syms = append(idx.syms, syms...)
		idx.byFile[f] = syms
	}
	return idx
}
