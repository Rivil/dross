package architecture

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func writeGo(t *testing.T, dir, name, body string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestParseDoc(t *testing.T) {
	doc := `# Architecture

intro prose

### <Feature name — a user-facing capability, not a module or a phase>

<One line: what this capability does.>

- Symbol.Name — path/to/file.ext:line

_placeholder_

### Real feature

Does a thing.

- ` + "`Alpha`" + ` — ` + "`fix/a.go:3`" + `
- ` + "`Beta`" + ` (helper) — ` + "`fix/a.go:5`" + ` (note)
- ` + "`findings.Fingerprint`" + ` — ` + "`fix/b.go`" + `

_introduced x · abc123_
`
	entries := ParseDoc(doc)
	if len(entries) != 1 {
		t.Fatalf("expected 1 real entry (placeholder skipped), got %d: %+v", len(entries), entries)
	}
	e := entries[0]
	if e.Heading != "Real feature" {
		t.Errorf("heading: %q", e.Heading)
	}
	if e.OneLine != "Does a thing." {
		t.Errorf("one-line: %q", e.OneLine)
	}
	if e.Provenance != "_introduced x · abc123_" {
		t.Errorf("provenance: %q", e.Provenance)
	}
	if len(e.Links) != 3 {
		t.Fatalf("expected 3 links, got %d: %+v", len(e.Links), e.Links)
	}
	// Backticked symbol + file:line.
	if e.Links[0].Symbol != "Alpha" || e.Links[0].File != "fix/a.go" || e.Links[0].Line != 3 {
		t.Errorf("link0: %+v", e.Links[0])
	}
	// Parenthetical stripped from symbol; trailing text after :line tolerated.
	if e.Links[1].Symbol != "Beta" || e.Links[1].File != "fix/a.go" || e.Links[1].Line != 5 {
		t.Errorf("link1 (em-dash + trailing text): %+v", e.Links[1])
	}
	// File with no line → Line 0 (resolver will Skip it).
	if e.Links[2].File != "fix/b.go" || e.Links[2].Line != 0 {
		t.Errorf("link2 (no line): %+v", e.Links[2])
	}
}

func TestResolveMovedAndOK(t *testing.T) {
	dir := t.TempDir()
	// Alpha at line 3, Beta at line 5.
	f := writeGo(t, dir, "a.go", "package fix\n\nfunc Alpha() {}\n\nfunc Beta() {}\n")

	moved := Resolve(SymbolLink{Symbol: "Alpha", File: f, Line: 99})
	if moved.Status != StatusMoved || moved.NewLine != 3 {
		t.Errorf("expected Moved{3}, got %v new=%d", moved.Status, moved.NewLine)
	}
	ok := Resolve(SymbolLink{Symbol: "Beta", File: f, Line: 5})
	if ok.Status != StatusOK {
		t.Errorf("expected OK for a symbol at its stated line, got %v", ok.Status)
	}
}

func TestResolveUnresolved(t *testing.T) {
	dir := t.TempDir()
	f := writeGo(t, dir, "a.go", "package fix\n\nfunc Alpha() {}\n")
	r := Resolve(SymbolLink{Symbol: "Gamma", File: f, Line: 3})
	if r.Status != StatusUnresolved {
		t.Errorf("expected Unresolved for a deleted/renamed symbol, got %v", r.Status)
	}
}

func TestResolveAmbiguous(t *testing.T) {
	dir := t.TempDir()
	// Two methods share the bare name Dup on different receivers.
	f := writeGo(t, dir, "a.go",
		"package fix\n\ntype A struct{}\n\nfunc (A) Dup() {}\n\ntype B struct{}\n\nfunc (B) Dup() {}\n")
	amb := Resolve(SymbolLink{Symbol: "Dup", File: f, Line: 5})
	if amb.Status != StatusAmbiguous {
		t.Errorf("expected Ambiguous for a duplicate bare name, got %v", amb.Status)
	}
	// The fully-qualified receiver.method disambiguates → exact match.
	exact := Resolve(SymbolLink{Symbol: "A.Dup", File: f, Line: 5})
	if exact.Status != StatusOK {
		t.Errorf("expected OK for the exact A.Dup at line 5, got %v new=%d", exact.Status, exact.NewLine)
	}
}

func TestResolveSkipped(t *testing.T) {
	dir := t.TempDir()
	f := writeGo(t, dir, "a.go", "package fix\n\nfunc Alpha() {}\n")
	// No line → can't check movement → Skipped.
	if r := Resolve(SymbolLink{Symbol: "Alpha", File: f, Line: 0}); r.Status != StatusSkipped {
		t.Errorf("expected Skipped for a bullet with no line, got %v", r.Status)
	}
	// A language codex can't index → Skipped, NOT Unresolved (the whole point
	// of routing through codex.SupportsFile).
	if r := Resolve(SymbolLink{Symbol: "thing", File: "docs/guide.md", Line: 10}); r.Status != StatusSkipped {
		t.Errorf("expected Skipped for an unindexable language, got %v", r.Status)
	}
}

func TestResolveAllIn(t *testing.T) {
	dir := t.TempDir()
	writeGo(t, dir, "a.go", "package fix\n\nfunc Alpha() {}\n") // Alpha at line 3
	doc := "### Feature\n\nline.\n\n- `Alpha` — `a.go:99`\n\n_x_\n"

	// Resolved against the repo root, the dir-relative file is found → Moved.
	in := ResolveAllIn(doc, dir)
	if len(in) != 1 || in[0].Status != StatusMoved || in[0].NewLine != 3 {
		t.Fatalf("ResolveAllIn should resolve relative to baseDir: %+v", in)
	}
	// The resolution keeps the original repo-relative path for display.
	if in[0].Link.File != "a.go" {
		t.Errorf("resolution should keep the repo-relative path, got %q", in[0].Link.File)
	}
	// Without the base dir, the dir-relative path can't be found from the test
	// cwd → Unresolved (proves the baseDir join is doing real work).
	out := ResolveAll(doc)
	if len(out) != 1 || out[0].Status != StatusUnresolved {
		t.Errorf("ResolveAll (cwd-relative) should not find a dir-relative file: %+v", out)
	}
}

func TestResolveAll(t *testing.T) {
	dir := t.TempDir()
	f := writeGo(t, dir, "a.go", "package fix\n\nfunc Alpha() {}\n\nfunc Beta() {}\n")
	// Alpha is stale (says 99, lives at 3); Beta is fine; the .md link skips.
	doc := fmt.Sprintf("### Feature\n\nline.\n\n- `Alpha` — `%s:99`\n- `Beta` — `%s:5`\n- `Doc` — `notes.md:1`\n\n_x_\n", f, f)
	res := ResolveAll(doc)
	if len(res) != 3 {
		t.Fatalf("expected 3 resolutions, got %d", len(res))
	}
	if res[0].Status != StatusMoved || res[0].NewLine != 3 {
		t.Errorf("Alpha: expected Moved{3}, got %v new=%d", res[0].Status, res[0].NewLine)
	}
	if res[1].Status != StatusOK {
		t.Errorf("Beta: expected OK, got %v", res[1].Status)
	}
	if res[2].Status != StatusSkipped {
		t.Errorf("md link: expected Skipped, got %v", res[2].Status)
	}
}
