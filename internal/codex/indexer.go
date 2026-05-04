// Package codex provides polyglot code insight for execution and
// verification. For a given target file, it produces:
//
//   - Symbols defined in the file (functions, types, methods, exports)
//   - Cross-file references (which other files mention these symbols)
//   - Sibling files in the same dir
//   - Recent git activity for the parent dir
//
// # Languages
//
// Go is handled by the stdlib parser (go/ast) — no external
// dependency. Everything else routes through ast-grep
// (https://ast-grep.github.io) with per-language patterns:
//
//   - TypeScript (.ts), TSX (.tsx)
//   - Svelte (.svelte)
//   - C# (.cs)
//   - GDScript (.gd)
//
// When ast-grep isn't on PATH, the non-Go indexers gracefully return
// nil Symbols and the rest of the index (siblings, git log) still
// populates. Install via `brew install ast-grep`, `cargo install
// ast-grep`, or whatever's right for your platform.
//
// HTML/CSS are not symbol-bearing in any useful sense; files of those
// types still get sibling + git-log enrichment but no Symbols.
//
// # Adding more languages
//
// Add a constructor in languages.go: pick the ast-grep language id,
// list the file extensions, and write patterns for each kind of
// symbol you want surfaced. Patterns use ast-grep metavariables
// ($NAME captures, $_ wildcards, $$$ multi-token wildcards). Then
// register the new indexer in allIndexers().
//
// If ast-grep's coverage of a language is too thin for your use case,
// the alternative is CGO tree-sitter via github.com/smacker/
// go-tree-sitter. That breaks the pure-Go binary distribution and
// complicates GoReleaser cross-compile, so it stays a last resort.
//
// # CLI surface
//
// `dross codex <file>` prints a compact rendering. The Result struct
// is the canonical format for programmatic consumers (e.g. verify's
// criterion-to-test mapping, the /dross-review panel).
package codex
