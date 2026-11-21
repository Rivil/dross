package facts

import (
	"path"
	"regexp"
	"strconv"
	"strings"
)

// Kind is what a Citation claims about the tree.
type Kind string

const (
	// KindPath cites a file: it must exist.
	KindPath Kind = "path"
	// KindLine cites `file:line`: the file must exist and, when a symbol is
	// paired with it, that symbol must live at the line.
	KindLine Kind = "line"
	// KindSymbol cites a code symbol (`pkg.Name`, `T.M()`): it must exist.
	KindSymbol Kind = "symbol"
)

// Citation is one factual claim extracted from spec or plan prose.
//
// Raw is the citation in a canonical display form with every wrapping
// character removed: "foo.go", "foo.go:12", "`Init` — x.go:3" for a paired
// line, "quoted.String()" for a symbol. Path is the cited file exactly as
// written (slashed means repo-relative, otherwise a bare basename) and is set
// for path and line citations. Line is set for line citations. Symbol is the
// cited name with any trailing "()" stripped — on a symbol citation, and on a
// line citation paired with an adjacent backticked symbol.
type Citation struct {
	Kind   Kind
	Raw    string
	Path   string
	Line   int
	Symbol string
}

// pathExts are the file extensions a token must carry to be read as a path:
// every language codex indexes, the other common source languages, and the
// config/doc types plans cite. A dotted token with any other final component
// (`e.g.`, `v1.9`, `go1.27.3`, `paths.source`) is not a path.
var pathExts = map[string]bool{
	// codex-indexed
	".go": true, ".ts": true, ".tsx": true, ".svelte": true, ".cs": true, ".gd": true,
	// other source
	".js": true, ".jsx": true, ".mjs": true, ".cjs": true, ".py": true, ".rs": true,
	".kt": true, ".dart": true, ".java": true, ".sql": true, ".tf": true, ".ps1": true,
	".css": true, ".html": true, ".sh": true,
	// data, config and docs
	".golden": true, ".jsonl": true, ".toml": true, ".md": true, ".json": true,
	".yml": true, ".yaml": true, ".txt": true, ".mod": true, ".sum": true,
}

// pathBasenames are extensionless file names read as paths.
var pathBasenames = map[string]bool{"Makefile": true, "Dockerfile": true}

// pathChars is the whole-token shape of a path: no spaces, globs, braces,
// placeholders or schemes can appear in it.
var pathChars = regexp.MustCompile(`^[A-Za-z0-9_.\-/]+$`)

// lineRe splits a `file:line` token. A trailing `-N` range or `:N` column is
// accepted and dropped; the citation is the first line.
var lineRe = regexp.MustCompile(`^(.+?):(\d+)(?:[-:]\d+)?$`)

// symbolRe is the locked symbol_shape: `Ident(.Ident)*` with optional `()`.
var symbolRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*(?:\.[A-Za-z_][A-Za-z0-9_]*)*(?:\(\))?$`)

// bareIdentRe is a single capitalised identifier (optionally call-shaped). It
// pairs with an adjacent file:line (`Init` — x.go:12) but is never a
// standalone symbol citation.
var bareIdentRe = regexp.MustCompile(`^[A-Z][A-Za-z0-9_]*(?:\(\))?$`)

// connectors may sit between a paired symbol and its file:line.
var connectors = map[string]bool{"—": true, "–": true, "-": true, "at": true, "(": true}

// token is one unit of text: a backtick span's content, or a maximal run of
// non-space characters outside backticks.
type token struct {
	text       string
	backticked bool
}

// Extract returns every citation in text, in order of appearance, each once.
func Extract(text string) []Citation {
	toks := tokenize(text)
	var out []Citation
	seen := map[string]bool{}
	add := func(c Citation) {
		key := string(c.Kind) + "\x00" + c.Raw
		if seen[key] {
			return
		}
		seen[key] = true
		out = append(out, c)
	}

	// paired marks backticked tokens consumed as a line citation's symbol, so
	// they are not also reported as standalone symbols.
	paired := map[int]bool{}
	type pending struct {
		at int
		c  Citation
	}
	var cites []pending

	for i, tk := range toks {
		if c, ok := pathCitation(tk.text); ok {
			if c.Kind == KindLine {
				if j, sym, ok := pairedSymbol(toks, i); ok {
					paired[j] = true
					c.Symbol = strings.TrimSuffix(sym, "()")
					c.Raw = "`" + sym + "` — " + c.Raw
				}
			}
			cites = append(cites, pending{i, c})
			continue
		}
		if tk.backticked {
			if sym, ok := standaloneSymbol(tk.text); ok {
				cites = append(cites, pending{i, Citation{Kind: KindSymbol, Raw: tk.text, Symbol: sym}})
			}
		}
	}
	for _, p := range cites {
		if p.c.Kind == KindSymbol && paired[p.at] {
			continue
		}
		add(p.c)
	}
	return out
}

// tokenize splits text into backtick spans and whitespace-separated prose
// words. An unmatched trailing backtick is treated as a separator.
func tokenize(text string) []token {
	var toks []token
	prose := func(s string) {
		for _, f := range strings.Fields(s) {
			toks = append(toks, token{text: f})
		}
	}
	for {
		open := strings.IndexByte(text, '`')
		if open < 0 {
			prose(text)
			return toks
		}
		closing := strings.IndexByte(text[open+1:], '`')
		if closing < 0 {
			prose(text[:open])
			prose(text[open+1:])
			return toks
		}
		prose(text[:open])
		span := strings.TrimSpace(text[open+1 : open+1+closing])
		switch {
		case span == "":
		case strings.ContainsAny(span, " \t\n"):
			// A multi-word span (`dross phase facts`, `git add x.go`) is a
			// command or phrase, never one symbol — but a path inside it is
			// still a path, so its words are scanned like prose.
			prose(span)
		default:
			toks = append(toks, token{text: span, backticked: true})
		}
		text = text[open+1+closing+1:]
	}
}

// trimWrap strips wrapping punctuation from a token: leading brackets and
// quotes, trailing sentence punctuation, brackets and quotes. A leading "." is
// part of the path (`.dross/x.toml`) and survives.
func trimWrap(s string) string {
	for {
		prev := s
		s = strings.TrimLeft(s, "([\"'“‘")
		s = strings.TrimRight(s, ".,;:)]\"'”’!?")
		if s == prev {
			return s
		}
	}
}

// pathCitation reads a token as a path or file:line citation.
func pathCitation(raw string) (Citation, bool) {
	s := trimWrap(raw)
	if m := lineRe.FindStringSubmatch(s); m != nil {
		if !isPath(m[1]) {
			return Citation{}, false
		}
		n, err := strconv.Atoi(m[2])
		if err != nil || n == 0 {
			return Citation{}, false
		}
		return Citation{Kind: KindLine, Raw: m[1] + ":" + m[2], Path: m[1], Line: n}, true
	}
	if !isPath(s) {
		return Citation{}, false
	}
	return Citation{Kind: KindPath, Raw: s, Path: s}, true
}

// isPath reports whether s is shaped like a file path a plan would cite.
// Absolute paths, home paths, URLs, globs, placeholders and directories are
// not; neither is a dotted word whose last component is no known extension.
func isPath(s string) bool {
	switch {
	case s == "",
		!pathChars.MatchString(s), // rejects < * { ~ : and spaces
		strings.HasPrefix(s, "/"),
		strings.HasSuffix(s, "/"):
		return false
	}
	base := path.Base(s)
	if pathBasenames[base] {
		return true
	}
	// Case-sensitive on purpose: a symbol's final component can case-fold to
	// an extension (`template.HTML`, `math.Mod`) and must stay a symbol.
	ext := path.Ext(base)
	// A basename that is all extension (".toml") names no file.
	if ext == "" || ext == base {
		return false
	}
	return pathExts[ext]
}

// standaloneSymbol applies the locked symbol_shape to a backtick span: a
// single `Ident(.Ident)*` token that is dotted or call-shaped. A dotted token
// whose last component is lowercase and not call-shaped is a TOML key
// (`paths.source`, `spec.toml`) and is skipped, as are bare words, commands
// and flags.
func standaloneSymbol(span string) (string, bool) {
	if !symbolRe.MatchString(span) {
		return "", false
	}
	call := strings.HasSuffix(span, "()")
	name := strings.TrimSuffix(span, "()")
	dotted := strings.Contains(name, ".")
	if !dotted && !call {
		return "", false
	}
	if dotted && !call {
		last := name[strings.LastIndex(name, ".")+1:]
		if last[0] < 'A' || last[0] > 'Z' {
			return "", false
		}
	}
	return name, true
}

// pairedSymbol looks back from the file:line token at i for an adjacent
// backticked symbol — `Sym` — f:N, `Sym` (f:N), `Sym` at f:N — skipping at
// most one connector word. It returns the symbol token's index and its text
// as written.
func pairedSymbol(toks []token, i int) (int, string, bool) {
	j := i - 1
	if j >= 0 && !toks[j].backticked && connectors[toks[j].text] {
		j--
	}
	if j < 0 || !toks[j].backticked {
		return 0, "", false
	}
	text := toks[j].text
	if _, ok := standaloneSymbol(text); ok || bareIdentRe.MatchString(text) {
		return j, text, true
	}
	return 0, "", false
}
