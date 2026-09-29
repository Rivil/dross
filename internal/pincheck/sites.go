// Package pincheck finds the version pins Dependabot cannot reach and checks
// each one against its upstream's release list.
//
// Dependabot bumps `uses:` refs, go.mod requirements and npm lockfiles. It does
// not bump a `go install pkg@vX.Y.Z` inside a workflow `run:` block, the Node
// version setup-node installs, go.mod's `toolchain` line, goreleaser-action's
// `version:`, or a pin written into Go source. Those sites only move when a
// human remembers them; this package is what remembers.
//
// The scanner is line-based on purpose, like the workflow sweeps in
// internal/cmd (action_pins_test.go, workflow_run_expressions_test.go): the
// repo's stack decision is a single static binary with no YAML dependency.
package pincheck

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/mod/semver"
)

// Kind is what sort of pin a Site is. It picks the upstream a pin is checked
// against and the line rule its staleness is judged on.
type Kind string

const (
	// KindGoInstall is a `go install <pkg>@<version>` in a run: block, or a
	// Go-source const holding the same `<pkg>@<version>` shape.
	KindGoInstall Kind = "go-install"
	// KindNode is the Node version setup-node installs.
	KindNode Kind = "node"
	// KindGoToolchain is go.mod's `toolchain` directive.
	KindGoToolchain Kind = "go-toolchain"
	// KindGoreleaserAction is goreleaser-action's `version:` input.
	KindGoreleaserAction Kind = "goreleaser-action"
	// KindNPM is an npm package pin held in Go source (strykerPin).
	KindNPM Kind = "npm"
)

// Site is one pin: where it is written, what it pins, and to which version.
//
// A site that names no exact release — a range, `latest`, `lts/*`, a `${{ }}`
// expression, a missing version — is still a site: Pinned is false, Version is
// empty and Reason says what was written instead. The strict check fails on it,
// so an unpinned site cannot hide by being unparseable.
type Site struct {
	File    string // repo-relative, slash-separated
	Line    int    // 1-based line the version is written on
	Kind    Kind
	Name    string // package path, "node", "go", or the goreleaser distribution
	Version string // the exact pinned version; empty when !Pinned
	Pinned  bool
	Reason  string // why the site is not an exact pin; empty when Pinned
}

var (
	// exactRelease is a plain X.Y.Z with an optional leading v — the only
	// shape a Node or goreleaser pin may take.
	exactRelease = regexp.MustCompile(`^v?\d+\.\d+\.\d+$`)
	// exactToolchain is a released Go toolchain name. An rc build is exact
	// but not a release line pincheck can judge, so it reports unpinned.
	exactToolchain = regexp.MustCompile(`^go\d+\.\d+\.\d+$`)
	// yamlKey is a plain mapping key at the start of a line's body.
	yamlKey = regexp.MustCompile(`^([A-Za-z0-9_.\-]+):(?:\s+(.*))?$`)
	// goInstall finds a `go install` invocation in a line of shell.
	goInstall = regexp.MustCompile(`(?:^|[\s;&|(])go\s+install\s+([^;&|)]*)`)
)

// Scan walks the generic pin sites under root: run-block `go install` pins in
// .github/workflows/*.{yml,yaml} and .github/actions/*/action.{yml,yaml},
// setup-node's node version (inline, or the file node-version-file: names),
// goreleaser-action's version, and go.mod's toolchain. Sites come back sorted by
// file then line; a version file two workflows both name is one site.
func Scan(root string) ([]Site, error) {
	var yamls []string
	for _, pattern := range []string{
		".github/workflows/*.yml", ".github/workflows/*.yaml",
		".github/actions/*/action.yml", ".github/actions/*/action.yaml",
	} {
		m, err := filepath.Glob(filepath.Join(root, filepath.FromSlash(pattern)))
		if err != nil {
			return nil, fmt.Errorf("pincheck: glob %s: %w", pattern, err)
		}
		yamls = append(yamls, m...)
	}

	var sites []Site
	for _, p := range yamls {
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return nil, fmt.Errorf("pincheck: %w", err)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return nil, fmt.Errorf("pincheck: read %s: %w", rel, err)
		}
		found, refs := yamlSites(filepath.ToSlash(rel), string(b))
		sites = append(sites, found...)
		for _, ref := range refs {
			s, err := nodeVersionFileSite(root, ref)
			if err != nil {
				return nil, err
			}
			sites = append(sites, s)
		}
	}

	gomod, err := os.ReadFile(filepath.Join(root, "go.mod"))
	switch {
	case err == nil:
		sites = append(sites, toolchainSites(string(gomod))...)
	case !errors.Is(err, fs.ErrNotExist):
		return nil, fmt.Errorf("pincheck: read go.mod: %w", err)
	}

	return dedupe(sites), nil
}

// dedupe drops repeated sites and sorts the rest by file, line, kind, name.
func dedupe(sites []Site) []Site {
	sort.SliceStable(sites, func(i, j int) bool {
		a, b := sites[i], sites[j]
		if a.File != b.File {
			return a.File < b.File
		}
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		return a.Name < b.Name
	})
	out := make([]Site, 0, len(sites))
	for i, s := range sites {
		if i > 0 && s == sites[i-1] {
			continue
		}
		out = append(out, s)
	}
	return out
}

// yline is one YAML line, split just far enough to find keys and run blocks.
type yline struct {
	n         int    // 1-based line number
	indent    int    // column of the first non-space character
	item      bool   // the line opens a list item (`- `)
	keyIndent int    // column the key starts at
	key       string // mapping key; "" when the line is not `key:` shaped
	value     string // the key's value, YAML comment stripped
	skip      bool   // blank or comment-only
	inBlock   bool   // a content line of a block scalar
	blockKey  string // the key whose block scalar this line belongs to
	text      string // the raw line, trimmed
}

// parseYAMLLines splits a workflow or action into ylines. A block scalar
// (`key: |`, `key: >-`, …) runs through every following line that is blank or
// indented past its key, and those lines are content, never keys — a
// `name: go install x@v1` inside a run block is shell, and one outside it is a
// name.
func parseYAMLLines(content string) []yline {
	var (
		out        []yline
		inBlock    bool
		blockDepth int
		blockKey   string
	)
	for i, raw := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(raw)
		indent := len(raw) - len(strings.TrimLeft(raw, " "))
		l := yline{n: i + 1, indent: indent, text: trimmed}
		if inBlock {
			if trimmed == "" || indent > blockDepth {
				l.inBlock, l.blockKey = true, blockKey
				out = append(out, l)
				continue
			}
			inBlock = false
		}
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			l.skip = true
			out = append(out, l)
			continue
		}
		body := trimmed
		l.keyIndent = indent
		if body == "-" || strings.HasPrefix(body, "- ") {
			l.item = true
			rest := strings.TrimLeft(body[1:], " ")
			l.keyIndent = indent + len(body) - len(rest)
			body = rest
		}
		if m := yamlKey.FindStringSubmatch(body); m != nil {
			l.key = m[1]
			l.value = strings.TrimSpace(stripYAMLComment(m[2]))
			if isBlockScalarIndicator(l.value) {
				inBlock, blockDepth, blockKey = true, l.keyIndent, l.key
			}
		}
		out = append(out, l)
	}
	return out
}

// stripYAMLComment drops a trailing ` # …` comment. YAML needs whitespace
// before a `#` for it to open a comment, so a `#` inside a scalar stays.
func stripYAMLComment(s string) string {
	if strings.HasPrefix(s, "#") {
		return ""
	}
	if i := strings.Index(s, " #"); i >= 0 {
		return s[:i]
	}
	return s
}

// isBlockScalarIndicator reports whether a value opens a YAML block scalar:
// `|` or `>` followed by optional chomping/indentation indicators.
func isBlockScalarIndicator(v string) bool {
	if v == "" || (v[0] != '|' && v[0] != '>') {
		return false
	}
	return strings.Trim(v[1:], "+-0123456789") == ""
}

// unquote strips one pair of matching surrounding quotes.
func unquote(v string) string {
	if len(v) >= 2 && (v[0] == '\'' || v[0] == '"') && v[len(v)-1] == v[0] {
		return v[1 : len(v)-1]
	}
	return v
}

// versionFileRef is a setup-node `node-version-file:` still to be resolved: the
// workflow line that names it, and the path it names.
type versionFileRef struct {
	file string
	line int
	path string
}

// yamlSites returns the pins written in one workflow or composite action, and
// the node-version-file references it makes.
func yamlSites(rel, content string) ([]Site, []versionFileRef) {
	lines := parseYAMLLines(content)
	var (
		sites []Site
		refs  []versionFileRef
	)
	for i, l := range lines {
		switch {
		case l.inBlock && l.blockKey == "run":
			sites = append(sites, goInstallSites(rel, l.n, l.text)...)
		case l.skip || l.inBlock:
		case l.key == "run" && !isBlockScalarIndicator(l.value):
			sites = append(sites, goInstallSites(rel, l.n, l.value)...)
		case l.key == "uses":
			action := unquote(l.value)
			switch {
			case strings.HasPrefix(action, "actions/setup-node@"):
				s, ref := setupNodeSite(rel, lines, i)
				if ref != nil {
					refs = append(refs, *ref)
				} else {
					sites = append(sites, s)
				}
			case strings.HasPrefix(action, "goreleaser/goreleaser-action@"):
				sites = append(sites, goreleaserSite(rel, lines, i))
			}
		}
	}
	return sites, refs
}

// goInstallSites returns a site per `pkg@version` argument of every `go
// install` on one line of shell. A shell comment is not an install.
func goInstallSites(rel string, n int, shell string) []Site {
	shell = unquote(strings.TrimSpace(stripYAMLComment(strings.TrimSpace(shell))))
	var sites []Site
	for _, m := range goInstall.FindAllStringSubmatch(shell, -1) {
		for _, arg := range strings.Fields(m[1]) {
			arg = unquote(arg)
			if strings.HasPrefix(arg, "-") || !strings.Contains(arg, "@") {
				continue
			}
			pkg, ver, _ := strings.Cut(arg, "@")
			s := Site{File: rel, Line: n, Kind: KindGoInstall, Name: pkg}
			if exactModuleVersion(ver) {
				s.Version, s.Pinned = ver, true
			} else {
				s.Reason = fmt.Sprintf("go install %s@%s is not an exact module version", pkg, ver)
			}
			sites = append(sites, s)
		}
	}
	return sites
}

// exactModuleVersion reports whether v names one module version rather than a
// query: `v1.2.3`, a pre-release or a pseudo-version, but not `v1`, `latest`,
// a branch or a range.
func exactModuleVersion(v string) bool {
	bare, _, _ := strings.Cut(v, "+")
	return semver.IsValid(bare) && semver.Canonical(bare) == bare
}

// stepWith returns the `with:` inputs of the step that holds lines[at], keyed
// by input name. ok is false when no enclosing list item is found.
func stepWith(lines []yline, at int) (with map[string]yline, ok bool) {
	start := at
	if !lines[at].item {
		// Walk up through the step's earlier keys to the `- ` that opens it:
		// a list item whose key sits at the same column as this one.
		start = -1
		for j := at - 1; j >= 0; j-- {
			l := lines[j]
			if l.skip || l.inBlock || l.keyIndent > lines[at].keyIndent {
				continue
			}
			if l.item && l.keyIndent == lines[at].keyIndent {
				start = j
			}
			if l.item || l.keyIndent < lines[at].keyIndent {
				break
			}
		}
		if start < 0 {
			return nil, false
		}
	}
	d, keyDepth := lines[start].indent, lines[start].keyIndent
	with = map[string]yline{}
	top := ""
	for j := start; j < len(lines); j++ {
		l := lines[j]
		if l.skip || l.inBlock {
			continue
		}
		if j > start && l.indent <= d {
			break
		}
		if l.keyIndent == keyDepth {
			top = l.key
			continue
		}
		if top == "with" && l.key != "" {
			with[l.key] = l
		}
	}
	return with, true
}

// setupNodeSite returns the Node pin of the setup-node step at lines[at]: an
// inline node-version site, or a reference to the file node-version-file names.
func setupNodeSite(rel string, lines []yline, at int) (Site, *versionFileRef) {
	s := Site{File: rel, Line: lines[at].n, Kind: KindNode, Name: "node"}
	with, ok := stepWith(lines, at)
	if !ok {
		s.Reason = "setup-node step could not be delimited"
		return s, nil
	}
	if f, ok := with["node-version-file"]; ok {
		return Site{}, &versionFileRef{file: rel, line: f.n, path: unquote(f.value)}
	}
	v, ok := with["node-version"]
	if !ok {
		s.Reason = "setup-node has no node-version or node-version-file — it uses whatever Node the runner carries"
		return s, nil
	}
	s.Line = v.n
	return nodeVersion(s, unquote(v.value), "node-version: "+v.value), nil
}

// nodeVersion fills s from a written Node version: an exact X.Y.Z (a leading v
// is stripped) is a pin; anything else is unpinned with what was written.
func nodeVersion(s Site, value, written string) Site {
	if exactRelease.MatchString(value) {
		s.Version, s.Pinned = strings.TrimPrefix(value, "v"), true
		return s
	}
	s.Reason = fmt.Sprintf("%s is not an exact X.Y.Z Node version", written)
	return s
}

// nodeVersionFileSite resolves a node-version-file reference against root, in
// the formats setup-node reads: `.nvmrc`/`.node-version` (a bare version, v
// optional), `.tool-versions` (`nodejs X.Y.Z`), and `package.json`, which holds
// no exact pin and so is unpinned. The site sits on the version file's line,
// since that is where a bump edits; a reference that cannot be resolved sits on
// the workflow line that makes it.
func nodeVersionFileSite(root string, ref versionFileRef) (Site, error) {
	atRef := Site{File: ref.file, Line: ref.line, Kind: KindNode, Name: "node"}
	clean := path.Clean(ref.path)
	if !filepath.IsLocal(filepath.FromSlash(clean)) {
		atRef.Reason = fmt.Sprintf("node-version-file: %s is outside the repository", ref.path)
		return atRef, nil
	}
	if path.Base(clean) == "package.json" {
		atRef.Reason = fmt.Sprintf("node-version-file: %s — package.json carries a range, not an exact pin", ref.path)
		return atRef, nil
	}
	b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(clean)))
	if errors.Is(err, fs.ErrNotExist) {
		atRef.Reason = fmt.Sprintf("node-version-file: %s does not exist", ref.path)
		return atRef, nil
	}
	if err != nil {
		return Site{}, fmt.Errorf("pincheck: read %s: %w", clean, err)
	}
	inFile := Site{File: clean, Kind: KindNode, Name: "node"}
	toolVersions := path.Base(clean) == ".tool-versions"
	for i, raw := range strings.Split(string(b), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		inFile.Line = i + 1
		if !toolVersions {
			return nodeVersion(inFile, line, clean+" holds "+strconv.Quote(line)), nil
		}
		f := strings.Fields(stripYAMLComment(line))
		if len(f) >= 2 && (f[0] == "nodejs" || f[0] == "node") {
			return nodeVersion(inFile, f[1], clean+" holds "+strconv.Quote(line)), nil
		}
	}
	atRef.Reason = fmt.Sprintf("node-version-file: %s names no Node version", ref.path)
	return atRef, nil
}

// goreleaserSite returns the pin of the goreleaser-action step at lines[at].
// Its Name is the distribution (goreleaser unless the step says otherwise),
// since that picks the upstream the version is checked against.
func goreleaserSite(rel string, lines []yline, at int) Site {
	s := Site{File: rel, Line: lines[at].n, Kind: KindGoreleaserAction, Name: "goreleaser"}
	with, ok := stepWith(lines, at)
	if !ok {
		s.Reason = "goreleaser-action step could not be delimited"
		return s
	}
	if d, ok := with["distribution"]; ok && unquote(d.value) != "" {
		s.Name = unquote(d.value)
	}
	v, ok := with["version"]
	if !ok {
		s.Reason = "goreleaser-action has no version: — it installs the latest release"
		return s
	}
	s.Line = v.n
	if val := unquote(v.value); exactRelease.MatchString(val) {
		s.Version, s.Pinned = val, true
		return s
	}
	s.Reason = fmt.Sprintf("version: %s is not an exact goreleaser release", v.value)
	return s
}

// toolchainSites returns go.mod's toolchain directive as a site, if it has one.
func toolchainSites(gomod string) []Site {
	for i, raw := range strings.Split(gomod, "\n") {
		code, _, _ := strings.Cut(raw, "//")
		f := strings.Fields(code)
		if len(f) != 2 || f[0] != "toolchain" {
			continue
		}
		s := Site{File: "go.mod", Line: i + 1, Kind: KindGoToolchain, Name: "go"}
		if exactToolchain.MatchString(f[1]) {
			s.Version, s.Pinned = f[1], true
		} else {
			s.Reason = fmt.Sprintf("toolchain %s is not a released go1.X.Y toolchain", f[1])
		}
		return []Site{s}
	}
	return nil
}

// GoConst returns the value of the package-level string const name declared in
// the Go file at file, and the 1-based line its value is written on. It is how
// the dross-only pins held in source (gremlinsPin, strykerPin) become sites. A
// missing const, or one whose value is not a single string literal, is an
// error.
func GoConst(file, name string) (value string, line int, err error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, file, nil, parser.SkipObjectResolution)
	if err != nil {
		return "", 0, fmt.Errorf("pincheck: parse %s: %w", file, err)
	}
	for _, decl := range f.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.CONST {
			continue
		}
		for _, spec := range gd.Specs {
			vs := spec.(*ast.ValueSpec)
			for i, id := range vs.Names {
				if id.Name != name {
					continue
				}
				if i >= len(vs.Values) {
					return "", 0, fmt.Errorf("pincheck: %s: const %s has no value of its own", file, name)
				}
				lit, ok := vs.Values[i].(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					return "", 0, fmt.Errorf("pincheck: %s: const %s is not a string literal", file, name)
				}
				v, err := strconv.Unquote(lit.Value)
				if err != nil {
					return "", 0, fmt.Errorf("pincheck: %s: const %s: %w", file, name, err)
				}
				return v, fset.Position(lit.Pos()).Line, nil
			}
		}
	}
	return "", 0, fmt.Errorf("pincheck: %s declares no const %s", file, name)
}
