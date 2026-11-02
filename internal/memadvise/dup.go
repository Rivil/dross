package memadvise

import (
	"path"
	"strings"
	"unicode"
)

// shingleWords is the window a duplicate is measured in (locked dup_threshold):
// long enough that a passing mention of a doc's phrase doesn't count, short
// enough that a restatement with its paragraphs reordered still does.
const shingleWords = 8

// DocCandidates keeps the tracked paths a memory may duplicate: the root
// CLAUDE.md, ARCHITECTURE.md and README*, and any .md under docs/. Paths are
// repo-relative and slash-separated, as `git ls-files` prints them.
func DocCandidates(tracked []string) []string {
	var out []string
	for _, p := range tracked {
		switch {
		case p == "CLAUDE.md", p == "ARCHITECTURE.md",
			!strings.Contains(p, "/") && strings.HasPrefix(p, "README"),
			strings.HasPrefix(p, "docs/") && path.Ext(p) == ".md":
			out = append(out, p)
		}
	}
	return out
}

// Docs is a set of tracked docs shingled once, so every memory is measured
// against them without re-reading or re-splitting a doc per memory.
type Docs struct {
	paths []string
	sets  []map[string]struct{}
}

// NewDocs shingles each doc. paths fixes the order ties resolve in;
// content maps each path to its text.
func NewDocs(paths []string, content map[string]string) *Docs {
	d := &Docs{}
	for _, p := range paths {
		set := map[string]struct{}{}
		for _, s := range shingles(content[p]) {
			set[s] = struct{}{}
		}
		d.paths = append(d.paths, p)
		d.sets = append(d.sets, set)
	}
	return d
}

// Duplicates returns the one doc holding at least half of body's distinct
// shingles. Coverage split across several docs doesn't add up: a memory that
// restates half of one doc is redundant, one that quotes a little of each is a
// summary. A body too short for a single shingle never matches.
func (d *Docs) Duplicates(body string) (doc string, ok bool) {
	set := map[string]struct{}{}
	for _, s := range shingles(body) {
		set[s] = struct{}{}
	}
	if len(set) == 0 || d == nil {
		return "", false
	}
	best := 0
	for i, docSet := range d.sets {
		hit := 0
		for s := range set {
			if _, in := docSet[s]; in {
				hit++
			}
		}
		// 2*hit >= total is the ≥50% threshold without a float boundary.
		if 2*hit >= len(set) && hit > best {
			best, doc, ok = hit, d.paths[i], true
		}
	}
	return doc, ok
}

// shingles normalises text — lowercase, letters and digits only, whitespace
// collapsed — and returns its overlapping shingleWords-word windows.
func shingles(text string) []string {
	words := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	if len(words) < shingleWords {
		return nil
	}
	out := make([]string, 0, len(words)-shingleWords+1)
	for i := 0; i+shingleWords <= len(words); i++ {
		out = append(out, strings.Join(words[i:i+shingleWords], " "))
	}
	return out
}
