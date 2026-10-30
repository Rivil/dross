package memadvise

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"syscall"
)

// IndexName is the index Claude Code loads into every turn.
const IndexName = "MEMORY.md"

// maxFileBytes caps a single read. A memory is a paragraph; anything this big is
// not one, and reading it whole would make status pay for someone's dump.
const maxFileBytes = 1 << 20

// IndexLine is one `- [Title](file.md) — hook` line of MEMORY.md.
type IndexLine struct {
	LineNo int    // 0-based line number in MEMORY.md
	Raw    string // the line as written, without its line ending
	Title  string
	Target string // the link target exactly as written
	Hook   string // the text after the separator; "" when there is none
}

// Memory is one direct-child memory file.
type Memory struct {
	File        string // base name, e.g. dross_facts.md
	Name        string // frontmatter name: — the slug [[links]] resolve by
	Description string
	Body        string // everything after the frontmatter
	Raw         []byte // the whole file, frontmatter included
	Links       []string
}

// Corpus is a memory dir as read: the index and every readable memory.
type Corpus struct {
	Dir      string
	HasIndex bool   // false when MEMORY.md is missing or unreadable
	Index    []byte // MEMORY.md as read
	Lines    []IndexLine
	Memories map[string]*Memory // by File
}

// Integrity is what is wrong with how the index and the memories hang together.
type Integrity struct {
	// DeadLinks are index lines whose target is not a readable memory here:
	// missing, outside the dir, a symlink, or not .md.
	DeadLinks []IndexLine
	// Orphans are memories no index line points at, sorted.
	Orphans []string
	// Inbound maps a uniquely-declared name: slug to the distinct files that
	// [[link]] it, sorted, self-links excluded.
	Inbound map[string][]string
	// Dangling maps a [[slug]] no memory declares to the files linking it.
	Dangling map[string][]string
	// DuplicateNames maps a slug declared by more than one file to those files.
	// A duplicated slug resolves to neither, so neither gets inbound credit.
	DuplicateNames map[string][]string
}

var (
	indexLineRe = regexp.MustCompile(`^\s*[-*+]\s+\[([^\]]*)\]\(([^)\s]*)\)(.*)$`)
	hookSepRe   = regexp.MustCompile(`^\s*(?:—|–|-{1,2})\s*`)
	wikiLinkRe  = regexp.MustCompile(`\[\[([^\[\]\n]+)\]\]`)
	fenceRe     = regexp.MustCompile("(?ms)^\\s*```.*?^\\s*```[^\\n]*$")
	// RE2 has no backreferences, so the two span widths memories use are
	// spelled out, the double first so it is not read as two empty singles.
	codeSpanRe = regexp.MustCompile("``[^`]*``|`[^`\\n]*`")
)

// Load reads dir's MEMORY.md and its direct-child *.md memories. Symlinks,
// FIFOs, subdirectories and files over the size cap are skipped, never
// followed or opened in a way that can block. A missing MEMORY.md leaves
// HasIndex false; only a failure to list dir itself is an error.
func Load(dir string) (*Corpus, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read memory dir: %w", err)
	}
	c := &Corpus{Dir: dir, Memories: map[string]*Memory{}}
	for _, e := range entries {
		name := e.Name()
		if !e.Type().IsRegular() || filepath.Ext(name) != ".md" {
			continue
		}
		data, ok := readRegular(filepath.Join(dir, name))
		if !ok {
			continue
		}
		if name == IndexName {
			c.HasIndex, c.Index = true, data
			continue
		}
		c.Memories[name] = parseMemory(name, data)
	}
	if c.HasIndex {
		c.Lines = parseIndex(c.Index)
	}
	return c, nil
}

// readRegular reads path when it is still a regular file once opened. The open
// is non-blocking so a FIFO swapped in after the listing returns at once.
func readRegular(path string) ([]byte, bool) {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, false
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > maxFileBytes {
		return nil, false
	}
	data, err := io.ReadAll(io.LimitReader(f, maxFileBytes+1))
	if err != nil && !errors.Is(err, io.EOF) || len(data) > maxFileBytes {
		return nil, false
	}
	return data, true
}

// SplitLines splits on \n, dropping a \r before it, and reports whether the
// text ended with a newline. Apply uses the same split to rewrite one line.
func SplitLines(b []byte) (lines []string, trailingNL bool) {
	s := string(b)
	trailingNL = strings.HasSuffix(s, "\n")
	s = strings.TrimSuffix(s, "\n")
	if s == "" && !trailingNL {
		return nil, false
	}
	for _, l := range strings.Split(s, "\n") {
		lines = append(lines, strings.TrimSuffix(l, "\r"))
	}
	return lines, trailingNL
}

func parseIndex(b []byte) []IndexLine {
	lines, _ := SplitLines(b)
	var out []IndexLine
	for i, l := range lines {
		m := indexLineRe.FindStringSubmatch(l)
		if m == nil {
			continue
		}
		hook := ""
		if rest := m[3]; strings.TrimSpace(rest) != "" {
			hook = strings.TrimSpace(hookSepRe.ReplaceAllString(rest, ""))
		}
		out = append(out, IndexLine{LineNo: i, Raw: l, Title: m[1], Target: m[2], Hook: hook})
	}
	return out
}

func parseMemory(file string, data []byte) *Memory {
	m := &Memory{File: file, Raw: data}
	front, body, ok := splitFrontmatter(string(data))
	if ok {
		for _, l := range strings.Split(front, "\n") {
			l = strings.TrimSuffix(l, "\r")
			if l == "" || l[0] == ' ' || l[0] == '\t' {
				continue // nested keys (metadata:) carry nothing the advisor reads
			}
			key, val, found := strings.Cut(l, ":")
			if !found {
				continue
			}
			val = unquote(strings.TrimSpace(val))
			switch strings.TrimSpace(key) {
			case "name":
				m.Name = val
			case "description":
				m.Description = val
			}
		}
	}
	m.Body = body
	m.Links = links(body)
	return m
}

// splitFrontmatter separates a leading `---` block from the body, tolerating
// CRLF. Without one, the whole text is the body.
func splitFrontmatter(s string) (front, body string, ok bool) {
	first, rest, found := strings.Cut(s, "\n")
	if !found || strings.TrimSuffix(first, "\r") != "---" {
		return "", s, false
	}
	for off := 0; off <= len(rest); {
		line, after, more := strings.Cut(rest[off:], "\n")
		if strings.TrimSuffix(line, "\r") == "---" {
			if !more {
				return rest[:off], "", true
			}
			return rest[:off], after, true
		}
		if !more {
			break
		}
		off += len(line) + 1
	}
	return "", s, false
}

func unquote(v string) string {
	if len(v) >= 2 && (v[0] == '"' && v[len(v)-1] == '"' || v[0] == '\'' && v[len(v)-1] == '\'') {
		return v[1 : len(v)-1]
	}
	return v
}

// links returns the distinct [[slug]]s in body, with fenced blocks and inline
// code removed first: a link quoted as an example is not a link.
func links(body string) []string {
	stripped := codeSpanRe.ReplaceAllString(fenceRe.ReplaceAllString(body, ""), "")
	seen := map[string]bool{}
	var out []string
	for _, m := range wikiLinkRe.FindAllStringSubmatch(stripped, -1) {
		slug := strings.TrimSpace(m[1])
		if slug != "" && !seen[slug] {
			seen[slug] = true
			out = append(out, slug)
		}
	}
	return out
}

// Resolve returns the memory an index target names, or nil when the target is
// dead. Only a bare direct-child *.md that Load read counts: a path with a
// separator, "..", or an absolute path is dead without being opened.
func (c *Corpus) Resolve(target string) *Memory {
	if target == "" || strings.ContainsAny(target, `/\`) || filepath.IsAbs(target) ||
		filepath.Ext(target) != ".md" || target == IndexName {
		return nil
	}
	return c.Memories[target]
}

// Files returns the memory file names in sorted order.
func (c *Corpus) Files() []string {
	out := make([]string, 0, len(c.Memories))
	for f := range c.Memories {
		out = append(out, f)
	}
	sort.Strings(out)
	return out
}

// Integrity reports dead index links, orphans, inbound and dangling [[links]],
// and duplicated name: slugs.
func (c *Corpus) Integrity() Integrity {
	in := Integrity{
		Inbound:        map[string][]string{},
		Dangling:       map[string][]string{},
		DuplicateNames: map[string][]string{},
	}

	indexed := map[string]bool{}
	for _, l := range c.Lines {
		if m := c.Resolve(l.Target); m != nil {
			indexed[m.File] = true
		} else {
			in.DeadLinks = append(in.DeadLinks, l)
		}
	}

	declared := map[string][]string{}
	for _, f := range c.Files() {
		if !indexed[f] {
			in.Orphans = append(in.Orphans, f)
		}
		if n := c.Memories[f].Name; n != "" {
			declared[n] = append(declared[n], f)
		}
	}
	for slug, files := range declared {
		if len(files) > 1 {
			in.DuplicateNames[slug] = files
		}
	}

	for _, f := range c.Files() {
		m := c.Memories[f]
		for _, slug := range m.Links {
			if slug == m.Name {
				continue // a self-link is not an inbound reference
			}
			switch files := declared[slug]; len(files) {
			case 0:
				in.Dangling[slug] = append(in.Dangling[slug], f)
			case 1:
				in.Inbound[slug] = append(in.Inbound[slug], f)
			}
		}
	}
	return in
}
