package codex

import (
	"errors"
	"os/exec"
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
		"function $NAME($$$ARGS) { $$$ }":        {mkMatch(file, "parseToken", 12)},
		"export function $NAME($$$ARGS) { $$$ }": {mkMatch(file, "loadConfig", 30)},
		"export const $NAME = ($$$) => $$$":      {mkMatch(file, "withRetry", 45)},
		"class $NAME { $$$ }":                    {mkMatch(file, "ApiClient", 60)},
		"interface $NAME { $$$ }":                {mkMatch(file, "RequestOpts", 5)},
		"export type $NAME = $$$":                {mkMatch(file, "Outcome", 8)},
		"export enum $NAME { $$$ }":              {mkMatch(file, "Status", 10)},
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

// TestRealRunAstGrepFnHandlesEveryOutputShape covers the REAL runAstGrepFn
// body. Every other test in this package substitutes it wholesale, so its four
// branches never ran — the invoker that actually talks to ast-grep was the one
// piece of this package with no coverage at all.
//
// Only the process seam (astGrepOutput) is faked, so the decode path under test
// is the shipped one.
func TestRealRunAstGrepFnHandlesEveryOutputShape(t *testing.T) {
	fakeOutput := func(t *testing.T, out []byte, err error) {
		t.Helper()
		prev := astGrepOutput
		astGrepOutput = func([]string) ([]byte, error) { return out, err }
		t.Cleanup(func() { astGrepOutput = prev })
	}

	t.Run("a spawn failure is wrapped, not swallowed", func(t *testing.T) {
		fakeOutput(t, nil, errors.New("exec: no such file"))
		got, err := runAstGrepFn("a.ts", "ts", "function $NAME($$$) { $$$ }")
		if err == nil {
			t.Fatal("a failing ast-grep run returned no error")
		}
		if !strings.Contains(err.Error(), "ast-grep run:") {
			t.Errorf("err = %q, want it wrapped with the run context", err)
		}
		if got != nil {
			t.Errorf("matches returned alongside an error: %+v", got)
		}
	})

	t.Run("empty output is no matches, not an error", func(t *testing.T) {
		// ast-grep prints nothing when a pattern matches nothing. Treating
		// that as a decode failure would make every clean scan an error.
		for _, body := range []string{"", "   ", "\n\t\n"} {
			fakeOutput(t, []byte(body), nil)
			got, err := runAstGrepFn("a.ts", "ts", "class $NAME { $$$ }")
			if err != nil {
				t.Errorf("empty output %q errored: %v", body, err)
			}
			if got != nil {
				t.Errorf("empty output %q yielded matches: %+v", body, got)
			}
		}
	})

	t.Run("malformed JSON is a decode error", func(t *testing.T) {
		fakeOutput(t, []byte("{not an array"), nil)
		_, err := runAstGrepFn("a.ts", "ts", "class $NAME { $$$ }")
		if err == nil {
			t.Fatal("malformed ast-grep JSON returned no error")
		}
		if !strings.Contains(err.Error(), "decode ast-grep JSON") {
			t.Errorf("err = %q, want the decode context", err)
		}
	})

	t.Run("a valid array is decoded", func(t *testing.T) {
		fakeOutput(t, []byte(`[{"file":"src/a.ts","range":{"start":{"line":11}},`+
			`"metaVariables":{"single":{"NAME":{"text":"parseToken"}}}}]`), nil)
		got, err := runAstGrepFn("src/a.ts", "ts", "function $NAME($$$) { $$$ }")
		if err != nil {
			t.Fatalf("runAstGrepFn: %v", err)
		}
		if len(got) != 1 {
			t.Fatalf("decoded %d matches, want 1: %+v", len(got), got)
		}
		if got[0].File != "src/a.ts" {
			t.Errorf("File = %q", got[0].File)
		}
		if name := got[0].MetaVars.Single["NAME"].Text; name != "parseToken" {
			t.Errorf("NAME = %q, want parseToken", name)
		}
	})
}

// TestAstGrepAvailableAsksLookPath covers the real availability closure, which
// every other test replaces with a constant. The answer depends on whether
// ast-grep is installed here, so it is asserted against exec.LookPath rather
// than against a fixed value — the point is that the closure consults the PATH
// at all, not what this machine happens to have.
func TestAstGrepAvailableAsksLookPath(t *testing.T) {
	_, lookErr := exec.LookPath("ast-grep")
	if got, want := astGrepAvailable(), lookErr == nil; got != want {
		t.Errorf("astGrepAvailable() = %v, but exec.LookPath says %v", got, want)
	}
}
