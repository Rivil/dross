package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Rivil/dross/internal/pincheck"
)

// The coverage sweep is deliberately NOT pincheck's scanner. It is a cruder,
// broader pass over the same tree — any version-shaped `@X.Y.Z` token where a
// pin could live — and every hit must be a site cmd/pincheck checks. A pin the
// scanner cannot parse (a line continuation, a new Go-source const, a nested
// composite action) shows up here as a hit with no site, and fails the build
// instead of silently never being checked. Phase run-block-pin-currency,
// criterion c-1.

// versionAt is an `@` followed by an X.Y.Z version, with an optional v.
var versionAt = regexp.MustCompile(`@v?\d+\.\d+\.\d+`)

// sweepHit is one place a pin was found. A zero line matches any site in the
// file — how a node-version-file reference is expressed: the pin is whatever
// line of the version file carries it.
type sweepHit struct {
	file string
	line int
	what string
}

func (h sweepHit) String() string { return fmt.Sprintf("%s:%d %s", h.file, h.line, h.what) }

// sweep walks root for every place a pin can hide.
func sweep(t *testing.T, root string) []sweepHit {
	t.Helper()
	var hits []sweepHit
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", "testdata", "vendor":
				return filepath.SkipDir
			}
			return nil
		}
		switch {
		case rel == "go.mod":
			hits = append(hits, sweepGoMod(t, rel, p)...)
		case strings.HasPrefix(rel, ".github/workflows/") && (strings.HasSuffix(rel, ".yml") || strings.HasSuffix(rel, ".yaml")):
			hits = append(hits, sweepYAML(t, rel, p, false)...)
		case strings.HasPrefix(rel, ".github/actions/") && (d.Name() == "action.yml" || d.Name() == "action.yaml"):
			hits = append(hits, sweepYAML(t, rel, p, true)...)
		case strings.HasSuffix(rel, ".go") && !strings.HasSuffix(rel, "_test.go"):
			hits = append(hits, sweepGoConsts(t, rel, p)...)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return hits
}

// sweepGoMod finds the toolchain line — and only it: `go` and `require` lines
// are Dependabot's (require) or a language floor (go), not pins this check owns.
func sweepGoMod(t *testing.T, rel, path string) []sweepHit {
	var hits []sweepHit
	for i, line := range readLines(t, path) {
		if f := strings.Fields(line); len(f) >= 2 && f[0] == "toolchain" {
			hits = append(hits, sweepHit{rel, i + 1, "toolchain " + f[1]})
		}
	}
	return hits
}

// sweepYAML finds, in one workflow or composite action: every version token on
// a run: line or inside any block scalar (every non-uses line, for an action),
// every node-version / node-version-file key, and goreleaser-action's version.
// uses: lines and comments are skipped — an action's SHA pin and its `# vX.Y.Z`
// comment are Dependabot's.
func sweepYAML(t *testing.T, rel, path string, action bool) []sweepHit {
	var (
		hits        []sweepHit
		blockDepth  = -1
		releaserDep = -1
	)
	for i, raw := range readLines(t, path) {
		trimmed := strings.TrimSpace(raw)
		indent := len(raw) - len(strings.TrimLeft(raw, " "))
		inBlock := blockDepth >= 0 && (trimmed == "" || indent > blockDepth)
		if !inBlock {
			blockDepth = -1
		}
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		code := trimmed
		if j := strings.Index(code, " #"); j >= 0 {
			code = code[:j]
		}
		key := strings.TrimPrefix(code, "- ")
		if strings.HasPrefix(key, "uses:") {
			if strings.Contains(key, "goreleaser/goreleaser-action@") {
				releaserDep = indent
			} else if indent <= releaserDep {
				releaserDep = -1
			}
			continue
		}
		if releaserDep >= 0 && strings.HasPrefix(code, "- ") && indent <= releaserDep {
			releaserDep = -1
		}
		if !inBlock {
			switch {
			case strings.HasPrefix(key, "node-version-file:"):
				hits = append(hits, sweepHit{strings.Trim(strings.TrimSpace(strings.TrimPrefix(key, "node-version-file:")), `'"`), 0, "node-version-file"})
			case strings.HasPrefix(key, "node-version:"):
				hits = append(hits, sweepHit{rel, i + 1, "node-version"})
			case releaserDep >= 0 && strings.HasPrefix(key, "version:"):
				hits = append(hits, sweepHit{rel, i + 1, "goreleaser-action version"})
			}
			if v := strings.TrimSpace(key[strings.Index(key, ":")+1:]); strings.Contains(key, ":") && (v == "|" || v == ">" || strings.HasPrefix(v, "|") || strings.HasPrefix(v, ">")) {
				blockDepth = indent
				if strings.HasPrefix(code, "- ") {
					blockDepth += 2
				}
			}
		}
		if (inBlock || action || strings.HasPrefix(key, "run:")) && versionAt.MatchString(code) {
			hits = append(hits, sweepHit{rel, i + 1, versionAt.FindString(code)})
		}
	}
	return hits
}

// sweepGoConsts finds every package-level string const whose value carries a
// version token — the shape gremlinsPin and strykerPin take.
func sweepGoConsts(t *testing.T, rel, path string) []sweepHit {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse %s: %v", rel, err)
	}
	var hits []sweepHit
	for _, decl := range f.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.CONST {
			continue
		}
		for _, spec := range gd.Specs {
			for i, v := range spec.(*ast.ValueSpec).Values {
				lit, ok := v.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					continue
				}
				if s, err := strconv.Unquote(lit.Value); err == nil && versionAt.MatchString(s) {
					hits = append(hits, sweepHit{rel, fset.Position(lit.Pos()).Line, "const " + spec.(*ast.ValueSpec).Names[i].Name})
				}
			}
		}
	}
	return hits
}

func readLines(t *testing.T, path string) []string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(string(b), "\n")
}

// uncovered returns the hits no site accounts for.
func uncovered(hits []sweepHit, sites []pincheck.Site) []sweepHit {
	var out []sweepHit
	for _, h := range hits {
		found := false
		for _, s := range sites {
			if s.File == h.file && (h.line == 0 || s.Line == h.line) {
				found = true
				break
			}
		}
		if !found {
			out = append(out, h)
		}
	}
	return out
}

func TestEveryPinSiteIsChecked(t *testing.T) {
	root := filepath.Join("..", "..")
	hits := sweep(t, root)
	sites, err := allSites(root)
	if err != nil {
		t.Fatalf("allSites: %v", err)
	}
	for _, h := range uncovered(hits, sites) {
		t.Errorf("pin at %s is not a site cmd/pincheck checks — extend the scanner or sourcePins", h)
	}

	// The sweep itself must see the pins it exists to guard, and nothing it
	// was told to leave alone.
	seen := map[string]bool{}
	for _, h := range hits {
		seen[h.what] = true
		if h.file == "go.mod" && !strings.HasPrefix(h.what, "toolchain") {
			t.Errorf("sweep read a go.mod line that is not the toolchain: %s", h)
		}
		if strings.Contains(h.what, "uses") {
			t.Errorf("sweep read a uses: line: %s", h)
		}
	}
	for _, want := range []string{"const gremlinsPin", "const strykerPin"} {
		if !seen[want] {
			t.Errorf("sweep did not find %s — it is not looking where the pins are", want)
		}
	}
	if len(hits) < 5 {
		t.Errorf("sweep found %d pins in the live repo, want at least 5 (toolchain, node, govulncheck, goreleaser, gremlins, stryker): %v", len(hits), hits)
	}

	t.Run("fixture", func(t *testing.T) {
		base := map[string]string{
			"go.mod":                           "module example.com/m\n\ngo 1.27.0\n\ntoolchain go1.27.1\n\nrequire golang.org/x/mod v0.41.0\n",
			"internal/cmd/remote_bootstrap.go": "package cmd\n\nconst gremlinsPin = \"github.com/go-gremlins/gremlins/cmd/gremlins@v0.6.0\"\n",
			"internal/mutation/stryker.go":     "package mutation\n\nconst strykerPin = \"@stryker-mutator/core@9.6.1\"\n",
			".github/actions/tool/action.yml":  "runs:\n  using: composite\n  steps:\n    - shell: bash\n      run: go install golang.org/x/vuln/cmd/govulncheck@v1.8.0\n",
		}
		check := func(extra map[string]string) []sweepHit {
			files := map[string]string{}
			for k, v := range base {
				files[k] = v
			}
			for k, v := range extra {
				files[k] = v
			}
			root := writeTree(t, files)
			sites, err := allSites(root)
			if err != nil {
				t.Fatalf("allSites: %v", err)
			}
			return uncovered(sweep(t, root), sites)
		}
		if u := check(nil); len(u) != 0 {
			t.Fatalf("baseline fixture: uncovered %v, want none", u)
		}
		continued := map[string]string{".github/workflows/ci.yml": "jobs:\n  a:\n    steps:\n      - run: |\n          go install \\\n            example.com/tool/cmd/x@v1.2.3\n"}
		if u := check(continued); len(u) != 1 || u[0].line != 6 {
			t.Errorf("a go install the scanner cannot parse: uncovered %v, want the one at ci.yml:6", u)
		}
		newConst := map[string]string{"internal/foo/foo.go": "package foo\n\nconst fooPin = \"example.com/x@v1.2.3\"\n"}
		if u := check(newConst); len(u) != 1 || u[0].file != "internal/foo/foo.go" {
			t.Errorf("a new Go-source pin: uncovered %v, want internal/foo/foo.go", u)
		}
	})
}

func writeTree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, body := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// stubResolver answers from a table keyed by site name; a missing name is an
// unreachable upstream (the shape a 503 takes once the resolver has read it).
type stubResolver map[string][]pincheck.Release

func (s stubResolver) Releases(_ context.Context, site pincheck.Site) ([]pincheck.Release, error) {
	rels, ok := s[site.Name]
	if !ok {
		return nil, errors.New("GET https://proxy.example/x/@v/list: 503 Service Unavailable")
	}
	return rels, nil
}

var runNow = time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)

func old(v string) pincheck.Release {
	return pincheck.Release{Version: v, Published: runNow.Add(-30 * 24 * time.Hour)}
}

// runFixture is a tree with the three pins every dross checkout carries:
// the toolchain, gremlinsPin and strykerPin, plus any extra files.
func runFixture(t *testing.T, extra map[string]string) string {
	files := map[string]string{
		"go.mod":                           "module example.com/m\n\ngo 1.27.0\n\ntoolchain go1.27.1\n",
		"internal/cmd/remote_bootstrap.go": "package cmd\n\nconst gremlinsPin = \"github.com/go-gremlins/gremlins/cmd/gremlins@v0.6.0\"\n",
		"internal/mutation/stryker.go":     "package mutation\n\nconst strykerPin = \"@stryker-mutator/core@9.6.1\"\n",
	}
	for k, v := range extra {
		files[k] = v
	}
	return writeTree(t, files)
}

func currentUpstream() stubResolver {
	return stubResolver{
		"go": {old("go1.27.1")},
		"github.com/go-gremlins/gremlins/cmd/gremlins": {old("v0.5.1"), old("v0.6.0")},
		"@stryker-mutator/core":                        {old("9.6.1")},
	}
}

func runWith(t *testing.T, root string, r stubResolver, env map[string]string) (int, string) {
	t.Helper()
	var out, errb bytes.Buffer
	code := run(root, nil, &out, &errb, deps{resolver: r, now: runNow, getenv: func(k string) string { return env[k] }})
	return code, out.String() + errb.String()
}

func TestPincheckExitCodes(t *testing.T) {
	root := runFixture(t, nil)

	if code, out := runWith(t, root, currentUpstream(), nil); code != 0 {
		t.Errorf("every pin current: exit %d, want 0\n%s", code, out)
	}

	stale := currentUpstream()
	stale["go"] = []pincheck.Release{old("go1.27.1"), old("go1.27.2")}
	if code, out := runWith(t, root, stale, nil); code == 0 || !strings.Contains(out, "go1.27.1 → go1.27.2") {
		t.Errorf("one stale pin: exit %d, want non-zero naming it\n%s", code, out)
	}

	info := currentUpstream()
	info["@stryker-mutator/core"] = []pincheck.Release{old("9.6.1"), old("10.0.0")}
	if code, out := runWith(t, root, info, nil); code != 0 || !strings.Contains(out, "info") || !strings.Contains(out, "10.0.0") {
		t.Errorf("info only (a newer major): exit %d, want 0 with the info line\n%s", code, out)
	}

	unreachable := currentUpstream()
	delete(unreachable, "github.com/go-gremlins/gremlins/cmd/gremlins")
	if code, out := runWith(t, root, unreachable, nil); code == 0 || !strings.Contains(out, "unknown") || !strings.Contains(out, "503") {
		t.Errorf("one upstream 503: exit %d, want non-zero with the pin shown unknown\n%s", code, out)
	}

	if code, _ := runWith(t, root, currentUpstream(), nil); code != 0 {
		t.Fatal("sanity: rerun over the current upstream must still pass")
	}
	var out, errb bytes.Buffer
	if code := run(root, []string{"extra"}, &out, &errb, deps{resolver: currentUpstream(), now: runNow, getenv: func(string) string { return "" }}); code != 2 {
		t.Errorf("unexpected argument: exit %d, want 2", code)
	}
	if code, _ := runWith(t, t.TempDir(), currentUpstream(), nil); code != 2 {
		t.Errorf("a tree missing the source pins: exit %d, want 2 — a renamed const must not leave the check quietly", code)
	}
}

func TestPincheckWritesStaleOutput(t *testing.T) {
	readOut := func(p string) string {
		b, err := os.ReadFile(p)
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			t.Fatal(err)
		}
		return string(b)
	}

	stale := currentUpstream()
	stale["go"] = []pincheck.Release{old("go1.27.1"), old("go1.27.2")}
	out := filepath.Join(t.TempDir(), "github_output")
	if code, _ := runWith(t, runFixture(t, nil), stale, map[string]string{"GITHUB_OUTPUT": out}); code == 0 || readOut(out) != "stale=true\n" {
		t.Errorf("stale pin: exit %d, GITHUB_OUTPUT %q, want non-zero and stale=true", code, readOut(out))
	}

	unknownOnly := currentUpstream()
	delete(unknownOnly, "go")
	out = filepath.Join(t.TempDir(), "github_output")
	if code, _ := runWith(t, runFixture(t, nil), unknownOnly, map[string]string{"GITHUB_OUTPUT": out}); code == 0 || readOut(out) != "stale=false\n" {
		t.Errorf("unknown only: exit %d, GITHUB_OUTPUT %q, want non-zero and stale=false", code, readOut(out))
	}

	unpinnedOnly := runFixture(t, map[string]string{".github/workflows/ci.yml": "jobs:\n  a:\n    steps:\n      - run: go install example.com/tool/cmd/x@latest\n"})
	out = filepath.Join(t.TempDir(), "github_output")
	if code, _ := runWith(t, unpinnedOnly, currentUpstream(), map[string]string{"GITHUB_OUTPUT": out}); code == 0 || readOut(out) != "stale=false\n" {
		t.Errorf("unpinned only: exit %d, GITHUB_OUTPUT %q, want non-zero and stale=false", code, readOut(out))
	}

	// Unset: the verdict stands and nothing is written — not into the tree,
	// and not to an empty path (which would fail the run with exit 2).
	root := runFixture(t, nil)
	before := treeListing(t, root)
	if code, out := runWith(t, root, stale, nil); code != 1 {
		t.Errorf("GITHUB_OUTPUT unset, stale pin: exit %d, want 1\n%s", code, out)
	}
	if after := treeListing(t, root); after != before {
		t.Errorf("GITHUB_OUTPUT unset, yet the tree changed:\nbefore %s\nafter  %s", before, after)
	}
	// Appends, never truncates: the workflow may have written other outputs.
	out = filepath.Join(t.TempDir(), "github_output")
	if err := os.WriteFile(out, []byte("earlier=1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runWith(t, runFixture(t, nil), currentUpstream(), map[string]string{"GITHUB_OUTPUT": out})
	if got := readOut(out); got != "earlier=1\nstale=false\n" {
		t.Errorf("GITHUB_OUTPUT = %q, want the earlier line kept and stale=false appended", got)
	}
}

// treeListing is every file path under root, one per line.
func treeListing(t *testing.T, root string) string {
	t.Helper()
	var paths []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			paths = append(paths, p)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return strings.Join(paths, "\n")
}

// TestPincheckBumpCommand drives `pincheck bump` through run: a stale pin is
// rewritten on disk and printed, an npm pin is reported skipped and left alone,
// a current tree prints no outcome, and the exit code is 0 whenever the
// rewrite completed. Phase run-block-pin-currency, criterion c-10.
func TestPincheckBumpCommand(t *testing.T) {
	stale := currentUpstream()
	stale["go"] = []pincheck.Release{old("go1.27.1"), old("go1.27.2")}
	stale["@stryker-mutator/core"] = []pincheck.Release{old("9.6.1"), old("9.6.2")}

	root := runFixture(t, nil)
	var out, errb bytes.Buffer
	code := run(root, []string{"bump"}, &out, &errb, deps{resolver: stale, now: runNow, getenv: func(string) string { return "" }})
	if code != 0 {
		t.Fatalf("bump: exit %d, want 0\n%s%s", code, out.String(), errb.String())
	}
	for _, want := range []string{"bumped", "go.mod:5 go go1.27.1 → go1.27.2", "skipped", "@stryker-mutator/core", "1 pin(s) bumped, 1 left"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("bump output lacks %q:\n%s", want, out.String())
		}
	}
	if gomod, _ := os.ReadFile(filepath.Join(root, "go.mod")); !strings.Contains(string(gomod), "toolchain go1.27.2\n") {
		t.Errorf("go.mod not rewritten:\n%s", gomod)
	}
	if src, _ := os.ReadFile(filepath.Join(root, "internal/mutation/stryker.go")); !strings.Contains(string(src), "@9.6.1\"") {
		t.Errorf("strykerPin was touched:\n%s", src)
	}
	if strings.Contains(out.String(), "stale=") {
		t.Error("bump wrote the check's stale= signal")
	}

	// A current tree: no outcome lines, still exit 0.
	out.Reset()
	if code := run(runFixture(t, nil), []string{"bump"}, &out, &errb, deps{resolver: currentUpstream(), now: runNow, getenv: func(string) string { return "" }}); code != 0 {
		t.Errorf("bump over a current tree: exit %d, want 0", code)
	}
	if strings.Contains(out.String(), "bumped ") || !strings.Contains(out.String(), "0 pin(s) bumped, 0 left") {
		t.Errorf("bump over a current tree printed:\n%s", out.String())
	}

	if code := run(root, []string{"bump", "extra"}, &out, &errb, deps{resolver: stale, now: runNow, getenv: func(string) string { return "" }}); code != 2 {
		t.Errorf("bump with an extra argument: exit %d, want 2", code)
	}
}
