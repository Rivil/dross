// Package codex provides polyglot code insight for execution and
// verification. For a given target file, it produces:
//
//   - Symbols defined in the file (functions, types, methods, exports)
//   - Cross-file references (which other files mention these symbols)
//   - Sibling files in the same dir
//   - Recent git activity for the parent dir
//
// # v1: Go-only via go/ast
//
// The Go indexer uses Go's stdlib parser — no CGO, no new dependency,
// works wherever dross compiles. Other languages return empty Symbols
// today but still get sibling + git-log enrichment.
//
// # Adding more languages
//
// Implement the Indexer interface in a new file (one per language) and
// add it to the slice in codex.Index. Two viable architectures for
// non-stdlib languages:
//
//  1. Shell out to ast-grep (preferred) — clean, multi-language,
//     consistent with how dross already shells to gh / stryker /
//     gremlins. Add an ast-grep adapter that takes a language name +
//     a query pattern; one adapter covers TS/Svelte/C#/GDScript/HTML/
//     CSS in principle. Cost: another binary on the user's PATH.
//
//  2. CGO tree-sitter bindings — github.com/smacker/go-tree-sitter
//     plus per-language grammar modules. Strongest analysis but
//     breaks the pure-Go binary distribution and complicates
//     GoReleaser cross-compile. Defer until ast-grep proves
//     insufficient for a real need.
//
// # CLI surface
//
// `dross codex <file>` prints a compact rendering. The Result struct
// is the canonical format for programmatic consumers (e.g. verify's
// criterion-to-test mapping, the /dross-review panel).
package codex
