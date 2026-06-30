package architecture

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/Rivil/dross/internal/codex"
)

// This file is the shared parse + symbol-resolve core for ARCHITECTURE.md,
// consumed by both the advisory `dross doctor` stale-link section and the
// `dross architecture check [--fix]` repair command. There is one parser and
// one resolver — neither command grows its own.
//
// Resolution routes through codex's existing language dispatch
// (codex.SupportsFile / codex.Index) rather than a hard-coded Go path, so a
// bullet in a language codex can't index is classified Skipped (not Unresolved)
// and a non-Go repo's healthy links are never reported as stale.

// Entry is one ARCHITECTURE.md feature entry: a heading, its one-line
// description, its symbol-link bullets, and the inline provenance breadcrumb.
type Entry struct {
	Heading    string
	OneLine    string
	Links      []SymbolLink
	Provenance string
}

// SymbolLink is one `Symbol — file:line` bullet. Raw keeps the original bullet
// text verbatim so a repair can rewrite only the line number byte-for-byte.
// Line is 0 when the bullet carries no parseable line.
type SymbolLink struct {
	Symbol string
	File   string
	Line   int
	Raw    string
}

// Status is the outcome of resolving a SymbolLink against the live code.
type Status int

const (
	StatusOK         Status = iota // symbol found exactly where the bullet says
	StatusMoved                    // symbol found, but at a different line
	StatusUnresolved               // symbol not found (deleted/renamed)
	StatusAmbiguous                // multiple same-named symbols — never guess
	StatusSkipped                  // no line to check, or a language codex can't index
)

func (s Status) String() string {
	switch s {
	case StatusOK:
		return "ok"
	case StatusMoved:
		return "moved"
	case StatusUnresolved:
		return "unresolved"
	case StatusAmbiguous:
		return "ambiguous"
	case StatusSkipped:
		return "skipped"
	default:
		return "unknown"
	}
}

// Resolution pairs a link with its resolved Status. NewLine is set only for
// StatusMoved — the line the symbol now lives at.
type Resolution struct {
	Link    SymbolLink
	Status  Status
	NewLine int
}

// headingRe matches a level-3 entry heading. The skeleton's template example
// uses a `<…>` placeholder heading, which is not a real entry — callers skip it.
var headingRe = regexp.MustCompile(`^###\s+(.+?)\s*$`)

// locRe extracts the last `path.ext:line` occurrence from a bullet's location
// part. It tolerates surrounding backticks and trailing text after the line.
var locRe = regexp.MustCompile(`([A-Za-z0-9_./\-]+\.[A-Za-z0-9_]+):(\d+)`)

// fileOnlyRe matches a `path.ext` with no line (some bullets point at a file).
var fileOnlyRe = regexp.MustCompile(`([A-Za-z0-9_./\-]+\.[A-Za-z0-9_]+)`)

// ParseDoc parses ARCHITECTURE.md content into feature entries. The skeleton's
// `### <Feature name …>` template placeholder is not a real entry and is
// skipped (along with its placeholder bullets).
func ParseDoc(content string) []Entry {
	var entries []Entry
	var cur *Entry
	skipping := false

	flush := func() {
		if cur != nil {
			entries = append(entries, *cur)
		}
		cur = nil
	}

	for _, line := range strings.Split(content, "\n") {
		if m := headingRe.FindStringSubmatch(line); m != nil {
			flush()
			heading := m[1]
			// Placeholder heading from the embedded template — not an entry.
			if strings.HasPrefix(heading, "<") {
				skipping = true
				continue
			}
			skipping = false
			cur = &Entry{Heading: heading}
			continue
		}
		if skipping || cur == nil {
			continue
		}
		trimmed := strings.TrimSpace(line)
		switch {
		case trimmed == "":
			// blank — separator, ignore
		case strings.HasPrefix(trimmed, "- "):
			if link, ok := parseBullet(line); ok {
				cur.Links = append(cur.Links, link)
			}
		case strings.HasPrefix(trimmed, "_") && strings.HasSuffix(trimmed, "_"):
			cur.Provenance = trimmed
		case cur.OneLine == "" && len(cur.Links) == 0:
			cur.OneLine = trimmed
		default:
			// extra prose paragraph — not part of the structured entry
		}
	}
	flush()
	return entries
}

// parseBullet parses one `- Symbol — file:line` bullet. Returns ok=false for a
// bullet with no recognisable location at all.
func parseBullet(raw string) (SymbolLink, bool) {
	body := strings.TrimSpace(raw)
	body = strings.TrimPrefix(body, "- ")

	// Split symbol | location on the LAST em-dash so a symbol containing a dash
	// doesn't get cut early.
	sep := strings.LastIndex(body, "—")
	var symbolPart, locPart string
	if sep >= 0 {
		symbolPart = body[:sep]
		locPart = body[sep+len("—"):]
	} else {
		// No separator — treat the whole thing as a location-bearing bullet.
		locPart = body
	}

	link := SymbolLink{Symbol: cleanSymbol(symbolPart), Raw: raw}
	if m := locRe.FindStringSubmatch(locPart); m != nil {
		link.File = m[1]
		link.Line, _ = strconv.Atoi(m[2])
		return link, true
	}
	if m := fileOnlyRe.FindStringSubmatch(locPart); m != nil {
		// File but no line — keep it (resolver will Skip), so the bullet still
		// counts as a parsed link with File set for display.
		link.File = m[1]
		return link, true
	}
	return SymbolLink{}, false
}

// cleanSymbol reduces a bullet's symbol part to the primary symbol name:
// strips backticks, then takes the text up to the first annotation/multi-symbol
// marker ('(' , '/', '+'). `architecture.EntryTemplate` stays whole;
// `Init (seeds skeleton)` → `Init`; `quality.Catalog / quality.Detect` →
// `quality.Catalog`.
func cleanSymbol(s string) string {
	s = strings.ReplaceAll(s, "`", "")
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, "(/+"); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

// Resolve classifies one SymbolLink against the live code.
func Resolve(link SymbolLink) Resolution {
	// No line to check, or a language codex can't index → Skipped, never a
	// false "stale" warning.
	if link.Line == 0 || link.File == "" || !codex.SupportsFile(link.File) {
		return Resolution{Link: link, Status: StatusSkipped}
	}
	return resolveAgainst(link, fileSymbols(link.File))
}

// resolveAgainst classifies a link given the symbols extracted from its file.
// Split out so ResolveAll can index each file once and reuse the result.
func resolveAgainst(link SymbolLink, syms []codex.Symbol) Resolution {
	matches := matchSymbols(syms, link.Symbol)
	switch len(matches) {
	case 0:
		return Resolution{Link: link, Status: StatusUnresolved}
	case 1:
		if matches[0].Line == link.Line {
			return Resolution{Link: link, Status: StatusOK}
		}
		return Resolution{Link: link, Status: StatusMoved, NewLine: matches[0].Line}
	default:
		return Resolution{Link: link, Status: StatusAmbiguous}
	}
}

// ResolveAll parses content and resolves every link in every entry, indexing
// each referenced file at most once.
func ResolveAll(content string) []Resolution {
	cache := map[string][]codex.Symbol{}
	var out []Resolution
	for _, e := range ParseDoc(content) {
		for _, link := range e.Links {
			if link.Line == 0 || link.File == "" || !codex.SupportsFile(link.File) {
				out = append(out, Resolution{Link: link, Status: StatusSkipped})
				continue
			}
			syms, ok := cache[link.File]
			if !ok {
				syms = fileSymbols(link.File)
				cache[link.File] = syms
			}
			out = append(out, resolveAgainst(link, syms))
		}
	}
	return out
}

// fileSymbols returns the top-level symbols codex finds in one file.
func fileSymbols(file string) []codex.Symbol {
	res, err := codex.Index([]string{file})
	if err != nil || res == nil {
		return nil
	}
	return res.Symbols
}

// matchSymbols finds the symbols matching a doc symbol name. It first tries an
// exact name match (e.g. the method `Changes.Record`); failing that it matches
// on the last dotted component (so the package-qualified `architecture.EntryTemplate`
// matches codex's bare `EntryTemplate`). Two matches of the same base name are
// returned both — the caller treats that as Ambiguous rather than guessing.
func matchSymbols(syms []codex.Symbol, docSymbol string) []codex.Symbol {
	var exact []codex.Symbol
	for _, s := range syms {
		if s.Name == docSymbol {
			exact = append(exact, s)
		}
	}
	if len(exact) > 0 {
		return exact
	}
	base := lastComponent(docSymbol)
	var byBase []codex.Symbol
	for _, s := range syms {
		if lastComponent(s.Name) == base {
			byBase = append(byBase, s)
		}
	}
	return byBase
}

func lastComponent(name string) string {
	if i := strings.LastIndex(name, "."); i >= 0 {
		return name[i+1:]
	}
	return name
}
