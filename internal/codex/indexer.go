// Package codex provides polyglot code insight for execution and verification.
//
// # Goal
//
// For a given target file (or task), produce:
//   - Symbols defined in the file (functions, types, classes)
//   - Cross-file references (callers/callees)
//   - Sibling files in the same dir + similar exported names
//   - Recent neighbour activity from git log
//
// # Status: stub
//
// Returns empty results so dependent commands can wire up. The shape of
// Result is intentionally stable so callers can rely on it across
// implementations.
//
// # Implementation sketch (not yet built)
//
// The next attempt should follow this rough plan; deviating freely is
// fine if a better path emerges:
//
//  1. Pick a tree-sitter Go binding. The two viable options at time
//     of writing:
//       - github.com/smacker/go-tree-sitter — long-lived, stable, but
//         CGO-heavy and grammar selection is per-language module
//       - github.com/tree-sitter/go-tree-sitter — newer official Go
//         binding from the tree-sitter org; uses C bindings via cgo
//     Either works; smacker's is the path of least surprise for the
//     existing dross stack.
//
//  2. Vendor or import grammar modules. Per language:
//       - ts/tsx       → github.com/smacker/go-tree-sitter/typescript
//       - svelte       → community-maintained; may need to embed .so
//                        or build from tree-sitter-svelte source
//       - go           → github.com/smacker/go-tree-sitter/golang
//       - c#           → github.com/smacker/go-tree-sitter/csharp
//       - gdscript     → no official Go binding; either build from the
//                        tree-sitter-gdscript C source or fall back to
//                        regex extraction for symbols
//       - html, css    → github.com/smacker/go-tree-sitter/html,
//                        .../css — symbols are minimal here, mostly
//                        just IDs/classes; consider a stub return
//
//  3. Write per-language symbol queries. Tree-sitter supports `.scm`
//     query files; each language needs a query that captures top-level
//     declarations (functions, types, classes, exports). Store under
//     internal/codex/queries/<language>.scm so they're easy to tune.
//
//  4. Cross-file reference scan. Naive approach: for each symbol, grep
//     `<paths.source>` for the symbol name and report files where it
//     appears outside the defining file. Refinement: per-language
//     parsing to distinguish definitions from uses (avoids false
//     positives on common names like "New" or "id").
//
//  5. Sibling files: trivial — list dirent of file's parent dir.
//
//  6. Recent neighbour activity: `git log --pretty=format:%h\ %s -5
//     -- <dir>` for the file's parent directory, sanitized.
//
//  7. CLI surface: `dross codex <target>` (already wired) prints a
//     compact rendering. The Result struct is the canonical format
//     for programmatic consumers (verify's criterion mapping, the
//     subagent review panel).
//
// # Why this isn't shipped yet
//
// Each language adds CGO build complexity, vendored grammars bloat the
// binary, and the dogfood projects so far have been single-language
// (Go, TS) where ripgrep + AST-grep cover most of the value. When a
// real cross-language project drives the need, this is the doc to read
// before starting.
package codex

// Symbol represents a top-level definition in a source file.
type Symbol struct {
	Name string
	Kind string // function | type | class | var | const
	File string
	Line int
}

// Result is what `dross codex <target>` prints to stdout for the LLM to read.
type Result struct {
	TargetFiles  []string
	Symbols      []Symbol
	Callers      []Symbol // best-effort cross-file references
	Siblings     []string // files in same dir
	RecentLog    []string // git log lines for the touched dirs
}

// Index is a stub. Returns empty results.
func Index(targetFiles []string) (*Result, error) {
	return &Result{TargetFiles: targetFiles}, nil
}
