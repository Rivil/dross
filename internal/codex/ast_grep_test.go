package codex

import (
	"errors"
	"strings"
	"testing"
)

// fakeAstGrep installs a substitute runAstGrepFn that returns the
// supplied matches keyed by pattern. Restores the original on test
// cleanup. Also forces astGrepAvailableFn to true so Symbols() doesn't
// bail on the LookPath check.
func fakeAstGrep(t *testing.T, byPattern map[string][]astGrepMatch) {
	t.Helper()
	prevAvail := astGrepAvailableFn
	prevRun := runAstGrepFn
	astGrepAvailableFn = func() bool { return true }
	runAstGrepFn = func(file, lang, pattern string) ([]astGrepMatch, error) {
		return byPattern[pattern], nil
	}
	t.Cleanup(func() {
		astGrepAvailableFn = prevAvail
		runAstGrepFn = prevRun
	})
}

func mkMatch(file, name string, line int) astGrepMatch {
	m := astGrepMatch{File: file}
	m.Range.Start.Line = line - 1 // ast-grep is 0-indexed; Line() adds 1
	m.MetaVars.Single = map[string]struct {
		Text string `json:"text"`
	}{
		"NAME": {Text: name},
	}
	return m
}

func TestAstGrepIndexerSkipsWhenBinaryAbsent(t *testing.T) {
	prev := astGrepAvailableFn
	astGrepAvailableFn = func() bool { return false }
	defer func() { astGrepAvailableFn = prev }()

	idx := TypeScriptIndexer()
	syms, err := idx.Symbols("anything.ts")
	if err != nil {
		t.Errorf("missing ast-grep should not error: %v", err)
	}
	if syms != nil {
		t.Errorf("expected nil symbols when binary absent, got %v", syms)
	}
}

func TestAstGrepIndexerExtractsTypeScriptSymbols(t *testing.T) {
	const file = "src/api.ts"
	fakeAstGrep(t, map[string][]astGrepMatch{
		"function $NAME($$$ARGS) { $$$ }":            {mkMatch(file, "parseToken", 12)},
		"export function $NAME($$$ARGS) { $$$ }":     {mkMatch(file, "loadConfig", 30)},
		"export const $NAME = ($$$) => $$$":          {mkMatch(file, "withRetry", 45)},
		"class $NAME { $$$ }":                        {mkMatch(file, "ApiClient", 60)},
		"interface $NAME { $$$ }":                    {mkMatch(file, "RequestOpts", 5)},
		"export type $NAME = $$$":                    {mkMatch(file, "Outcome", 8)},
		"export enum $NAME { $$$ }":                  {mkMatch(file, "Status", 10)},
	})

	syms, err := TypeScriptIndexer().Symbols(file)
	if err != nil {
		t.Fatal(err)
	}

	got := map[string]string{}
	for _, s := range syms {
		got[s.Name] = s.Kind
	}
	want := map[string]string{
		"parseToken":  "function",
		"loadConfig":  "function",
		"withRetry":   "function",
		"ApiClient":   "class",
		"RequestOpts": "interface",
		"Outcome":     "type",
		"Status":      "enum",
	}
	for name, kind := range want {
		if got[name] != kind {
			t.Errorf("symbol %q: got kind %q want %q", name, got[name], kind)
		}
	}
}

func TestAstGrepIndexerSwallowsPerPatternErrors(t *testing.T) {
	prevAvail := astGrepAvailableFn
	prevRun := runAstGrepFn
	astGrepAvailableFn = func() bool { return true }
	runAstGrepFn = func(file, lang, pattern string) ([]astGrepMatch, error) {
		// First pattern fails, second succeeds — second's matches
		// should still come through.
		if strings.Contains(pattern, "function $NAME($$$ARGS) { $$$ }") {
			return nil, errors.New("simulated ast-grep failure")
		}
		if pattern == "interface $NAME { $$$ }" {
			return []astGrepMatch{mkMatch("a.ts", "OnlyOne", 3)}, nil
		}
		return nil, nil
	}
	defer func() {
		astGrepAvailableFn = prevAvail
		runAstGrepFn = prevRun
	}()

	syms, err := TypeScriptIndexer().Symbols("a.ts")
	if err != nil {
		t.Fatalf("indexer should swallow per-pattern errors: %v", err)
	}
	if len(syms) != 1 || syms[0].Name != "OnlyOne" || syms[0].Kind != "interface" {
		t.Errorf("expected the surviving match to come through: %+v", syms)
	}
}

func TestAstGrepSupports(t *testing.T) {
	cases := []struct {
		idx      *AstGrepIndexer
		supports []string
		rejects  []string
	}{
		{TypeScriptIndexer(), []string{"a.ts", "deep/path.TS"}, []string{"a.tsx", "a.go", "a.js"}},
		{TSXIndexer(), []string{"a.tsx", "deep/x.TSX"}, []string{"a.ts", "a.go"}},
		{SvelteIndexer(), []string{"App.svelte", "x/y.SVELTE"}, []string{"a.ts", "a.go"}},
		{CSharpIndexer(), []string{"Foo.cs", "src/Bar.CS"}, []string{"a.fs", "a.cshtml"}},
		{GDScriptIndexer(), []string{"player.gd", "x/y.GD"}, []string{"a.gdshader", "a.cs"}},
	}
	for _, c := range cases {
		for _, ok := range c.supports {
			if !c.idx.Supports(ok) {
				t.Errorf("%s should support %q", c.idx.Name(), ok)
			}
		}
		for _, no := range c.rejects {
			if c.idx.Supports(no) {
				t.Errorf("%s should not support %q", c.idx.Name(), no)
			}
		}
	}
}

func TestAllIndexersIncludesEveryLanguage(t *testing.T) {
	names := map[string]bool{}
	for _, idx := range allIndexers() {
		names[idx.Name()] = true
	}
	for _, want := range []string{"go", "ast-grep:ts", "ast-grep:tsx", "ast-grep:svelte", "ast-grep:csharp", "ast-grep:gdscript"} {
		if !names[want] {
			t.Errorf("allIndexers missing %q (got %v)", want, names)
		}
	}
}

// TestIndexUsesAstGrepWhenAvailable plumbs the fake through Index's
// dispatch so we know Index() actually calls the AstGrep indexer for
// non-Go files when ast-grep is "available".
func TestIndexUsesAstGrepWhenAvailable(t *testing.T) {
	dir := makeFixture(t)
	tsFile := writeFile(t, dir, "lib/util.ts", "// fixture body — ast-grep is faked\n")

	fakeAstGrep(t, map[string][]astGrepMatch{
		"function $NAME($$$ARGS) { $$$ }": {mkMatch(tsFile, "doStuff", 12)},
	})

	res, err := Index([]string{tsFile})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, s := range res.Symbols {
		if s.Name == "doStuff" && s.Kind == "function" {
			found = true
		}
	}
	if !found {
		t.Errorf("Index didn't pick up the ast-grep fake: %+v", res.Symbols)
	}
}
