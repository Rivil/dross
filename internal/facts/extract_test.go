package facts

import (
	"reflect"
	"testing"
)

// only returns the single citation Extract finds in text, failing otherwise.
func only(t *testing.T, text string) Citation {
	t.Helper()
	got := Extract(text)
	if len(got) != 1 {
		t.Fatalf("Extract(%q) = %+v, want exactly one citation", text, got)
	}
	return got[0]
}

// TestExtractStripsWrappingPunctuation (c-7): punctuation and wrapping around
// a file:line never become part of the path, and a leading "." survives.
func TestExtractStripsWrappingPunctuation(t *testing.T) {
	for _, text := range []string{
		"see foo.go:12.",
		"(foo.go:12)",
		"`foo.go:12`, then more",
		"foo.go:12;",
		"foo.go:12:",
		"`foo.go:12`",
		"(`foo.go:12`)",
	} {
		c := only(t, text)
		if c.Kind != KindLine || c.Path != "foo.go" || c.Line != 12 || c.Raw != "foo.go:12" {
			t.Errorf("Extract(%q) = %+v, want line citation foo.go:12", text, c)
		}
	}
	for text, want := range map[string]string{
		"edit .dross/x.toml.":   ".dross/x.toml",
		"see (internal/a.go),":  "internal/a.go",
		`"README.md"`:           "README.md",
		"`./internal/x.go`":     "./internal/x.go",
		"and ../outside.go too": "../outside.go",
	} {
		c := only(t, text)
		if c.Kind != KindPath || c.Path != want {
			t.Errorf("Extract(%q) = %+v, want path %q", text, c, want)
		}
	}
}

// TestExtractExcludesNonPaths: abbreviations, versions, URLs, refs, home and
// absolute paths, directories and placeholder paths are never path citations.
func TestExtractExcludesNonPaths(t *testing.T) {
	for _, text := range []string{
		"e.g.", "i.e.", "v1.9", "go1.27.3", "1.9.4.0",
		"https://example.com/a.md", "http://host:8080/x.go",
		"milestone/v1.9", "origin/main", "~/notes.md", "`~/.claude/x.md`",
		"/etc/hosts.txt", "internal/codex/", ".dross/phases/<id>/spec.toml",
		"**/*.go", "{a,b}.go", "x.go:0",
	} {
		if got := Extract(text); len(got) != 0 {
			t.Errorf("Extract(%q) = %+v, want no citation", text, got)
		}
	}
}

// TestExtractSkipsCommandsFlagsAndKeys: backticked commands, flags, TOML keys
// and bare lowercase words never become symbols (locked symbol_shape).
func TestExtractSkipsCommandsFlagsAndKeys(t *testing.T) {
	for _, text := range []string{
		"`dross phase facts`", "`--json`", "`paths.source`",
		"`current_phase_status`", "`facts`", "`Init`",
	} {
		if got := Extract(text); len(got) != 0 {
			t.Errorf("Extract(%q) = %+v, want no citation", text, got)
		}
	}
}

// TestExtractSymbols: real symbol citations are found with "()" stripped, and
// a backticked file name is a path, never a symbol.
func TestExtractSymbols(t *testing.T) {
	for text, want := range map[string]string{
		"`quoted.String()`":        "quoted.String",
		"`phase.Spec`":             "phase.Spec",
		"`cmd.planLockEdgeGate()`": "cmd.planLockEdgeGate",
		"`helper()`":               "helper",
		"`Changes.Record`":         "Changes.Record",
		// Final components that case-fold to a path extension stay symbols.
		"`template.HTML`": "template.HTML",
		"`math.Mod`":      "math.Mod",
		"`x.JS()`":        "x.JS",
	} {
		c := only(t, text)
		if c.Kind != KindSymbol || c.Symbol != want {
			t.Errorf("Extract(%q) = %+v, want symbol %q", text, c, want)
		}
	}
	c := only(t, "`spec.toml`")
	if c.Kind != KindPath || c.Path != "spec.toml" {
		t.Errorf("`spec.toml` = %+v, want a path citation", c)
	}
}

// TestExtractPairsSymbolWithLine (c-2): a file:line adjacent to a backticked
// symbol carries it — in every connector form — and the symbol is not also
// reported standalone. A bare file:line carries no symbol.
func TestExtractPairsSymbolWithLine(t *testing.T) {
	for text, want := range map[string]Citation{
		"`architecture.Resolve` — internal/architecture/links.go:192": {
			Kind: KindLine, Raw: "`architecture.Resolve` — internal/architecture/links.go:192",
			Path: "internal/architecture/links.go", Line: 192, Symbol: "architecture.Resolve",
		},
		"`Init` — x.go:12": {
			Kind: KindLine, Raw: "`Init` — x.go:12", Path: "x.go", Line: 12, Symbol: "Init",
		},
		"`Init` (x.go:12)": {
			Kind: KindLine, Raw: "`Init` — x.go:12", Path: "x.go", Line: 12, Symbol: "Init",
		},
		"`T.M()` at x.go:12.": {
			Kind: KindLine, Raw: "`T.M()` — x.go:12", Path: "x.go", Line: 12, Symbol: "T.M",
		},
		"`Bar` — `foo.go:99`": {
			Kind: KindLine, Raw: "`Bar` — foo.go:99", Path: "foo.go", Line: 99, Symbol: "Bar",
		},
		"`Bar` (`foo.go:99`)": {
			Kind: KindLine, Raw: "`Bar` — foo.go:99", Path: "foo.go", Line: 99, Symbol: "Bar",
		},
	} {
		if c := only(t, text); !reflect.DeepEqual(c, want) {
			t.Errorf("Extract(%q)\n got: %+v\nwant: %+v", text, c, want)
		}
	}

	c := only(t, "see x.go:5 for it")
	if c.Kind != KindLine || c.Symbol != "" || c.Path != "x.go" || c.Line != 5 {
		t.Errorf("bare x.go:5 = %+v, want an unpaired line citation", c)
	}
	// A non-symbol span never pairs: the TOML key is skipped and the line
	// stays bare.
	c = only(t, "`paths.source` — x.go:5")
	if c.Symbol != "" {
		t.Errorf("a TOML key must not pair as a symbol: %+v", c)
	}
}

// TestExtractExtensionSet: common source and config types, and extensionless
// Makefile / Dockerfile, are paths.
func TestExtractExtensionSet(t *testing.T) {
	for _, p := range []string{
		"install.ps1", "fixtures/terraform-c3/main.tf", "web/app.js", "scripts/gen.py",
		"Makefile", "build/Dockerfile", "go.mod", "go.sum", "a/b.svelte", "x.jsonl",
		"cmd/dross/testdata/cli_tree.txt", "golden/out.golden", ".github/workflows/ci.yml",
	} {
		c := only(t, "touch "+p+" now")
		if c.Kind != KindPath || c.Path != p {
			t.Errorf("Extract(%q) = %+v, want path", p, c)
		}
	}
}

// TestExtractMultiWordSpansAndOrder: a multi-word backtick span is scanned for
// paths like prose; citations come back in text order, each once.
func TestExtractMultiWordSpansAndOrder(t *testing.T) {
	got := Extract("run `git add internal/b.go` after a.go, then `phase.Spec`, a.go again")
	want := []Citation{
		{Kind: KindPath, Raw: "internal/b.go", Path: "internal/b.go"},
		{Kind: KindPath, Raw: "a.go", Path: "a.go"},
		{Kind: KindSymbol, Raw: "phase.Spec", Symbol: "phase.Spec"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got  %+v\nwant %+v", got, want)
	}
	if got := Extract("an `unterminated span x.go"); len(got) != 1 || got[0].Path != "x.go" {
		t.Errorf("unmatched backtick: got %+v, want the x.go path", got)
	}
	if got := Extract(""); got != nil {
		t.Errorf("empty text: got %+v, want nil", got)
	}
}
