package codex

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

// AstGrepIndexer is a generic Indexer that shells out to `ast-grep`
// (https://ast-grep.github.io) for languages where Go's stdlib doesn't
// have a parser. One AstGrepIndexer per language; each carries the
// list of patterns that capture symbol-level declarations.
//
// If ast-grep isn't on PATH, Symbols() returns nil with no error so
// the rest of the index (siblings, recent log) still populates.
// Same for individual pattern errors — they're swallowed so a bad
// pattern doesn't drop legitimate matches from the same file.
type AstGrepIndexer struct {
	// LangName is the ast-grep language identifier (e.g. "ts", "tsx",
	// "svelte", "csharp", "gdscript").
	LangName string
	// Exts are the file extensions this indexer handles. Matched
	// case-insensitively.
	Exts []string
	// Patterns are the symbol-extraction queries to run.
	Patterns []AstGrepPattern
}

// AstGrepPattern is one query: an ast-grep pattern string, the kind
// label to report on matches, and the metavariable name that captures
// the symbol's identifier (e.g. "NAME" for a pattern like
// `function $NAME($_) { $$$ }`).
type AstGrepPattern struct {
	Kind    string
	Pattern string
	NameVar string // metavar capturing the symbol name; defaults to "NAME"
}

func (a *AstGrepIndexer) Name() string { return "ast-grep:" + a.LangName }

func (a *AstGrepIndexer) Supports(file string) bool {
	ext := strings.ToLower(filepath.Ext(file))
	for _, e := range a.Exts {
		if strings.ToLower(e) == ext {
			return true
		}
	}
	return false
}

func (a *AstGrepIndexer) Symbols(file string) ([]Symbol, error) {
	if !astGrepAvailable() {
		return nil, nil
	}
	var out []Symbol
	for _, p := range a.Patterns {
		matches, err := runAstGrep(file, a.LangName, p.Pattern)
		if err != nil {
			// Bad pattern, parse failure, version mismatch — none of
			// these should fail the whole index. Skip silently.
			continue
		}
		nameVar := p.NameVar
		if nameVar == "" {
			nameVar = "NAME"
		}
		for _, m := range matches {
			name := m.MetaVar(nameVar)
			if name == "" {
				continue
			}
			out = append(out, Symbol{
				Name: name,
				Kind: p.Kind,
				File: file,
				Line: m.Line(),
			})
		}
	}
	return out, nil
}

// astGrepMatch is the subset of ast-grep's JSON match output we use.
// Schema: ast-grep uses a stable JSON shape — see `ast-grep --help` for
// the latest. We're tolerant: missing fields don't fail the whole run.
type astGrepMatch struct {
	File     string `json:"file"`
	Range    struct {
		Start struct {
			Line   int `json:"line"`
			Column int `json:"column"`
		} `json:"start"`
	} `json:"range"`
	MetaVars struct {
		Single map[string]struct {
			Text string `json:"text"`
		} `json:"single"`
	} `json:"metaVariables"`
}

// Line returns the 1-indexed line of the match's start. ast-grep
// reports 0-indexed; we add 1 to match every other tool dross
// produces (verify, codex symbols, etc.).
func (m astGrepMatch) Line() int { return m.Range.Start.Line + 1 }

// MetaVar returns the captured text for `$<name>`, empty if not bound.
func (m astGrepMatch) MetaVar(name string) string {
	if m.MetaVars.Single == nil {
		return ""
	}
	v, ok := m.MetaVars.Single[name]
	if !ok {
		return ""
	}
	return v.Text
}

// astGrepAvailableFn is the binary-presence check, overridable from
// tests so the suite can simulate "ast-grep present" without actually
// invoking it.
var astGrepAvailableFn = func() bool {
	_, err := exec.LookPath("ast-grep")
	return err == nil
}

func astGrepAvailable() bool { return astGrepAvailableFn() }

// runAstGrepFn is the actual ast-grep invoker. Overridable from tests
// so we can exercise pattern handling without depending on the real
// binary being installed.
var runAstGrepFn = func(file, lang, pattern string) ([]astGrepMatch, error) {
	cmd := exec.Command("ast-grep",
		"run",
		"--lang", lang,
		"--pattern", pattern,
		"--json=compact",
		file,
	)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("ast-grep run: %w", err)
	}
	// `--json=compact` emits a single JSON array per file scanned.
	out = []byte(strings.TrimSpace(string(out)))
	if len(out) == 0 {
		return nil, nil
	}
	var raw []astGrepMatch
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, fmt.Errorf("decode ast-grep JSON: %w", err)
	}
	return raw, nil
}

func runAstGrep(file, lang, pattern string) ([]astGrepMatch, error) {
	return runAstGrepFn(file, lang, pattern)
}
