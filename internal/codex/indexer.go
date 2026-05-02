// Package codex provides polyglot code insight for execution and verification.
//
// v1 plan: tree-sitter-backed indexer that for a given target file (or task)
// produces:
//   - Symbols defined in the file (functions, types, classes)
//   - Cross-file references (callers/callees)
//   - Sibling files in the same dir + similar exported names
//   - Recent neighbour activity from git log
//
// v0 status: stub. Returns empty results so dependent commands can wire up.
// Real implementation will use github.com/smacker/go-tree-sitter with grammars
// for ts/tsx/svelte/go/csharp/gdscript/html/css.
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
