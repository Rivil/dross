package cmd

import (
	"path/filepath"
	"reflect"
	"testing"
)

// parityGoFile is the one Go fixture the link-check parity pins resolve
// against. Line numbers matter: Foo lives at :5, Bar at :7, and two Fetch
// methods (on A and B) make a bare `Fetch` ambiguous.
const parityGoFile = "package pkg\n" + // 1
	"\n" + // 2
	"// Foo is documented here.\n" + // 3
	"\n" + // 4
	"func Foo() {}\n" + // 5
	"\n" + // 6
	"func Bar() {}\n" + // 7
	"\n" + // 8
	"type A struct{}\n" + // 9
	"\n" + // 10
	"func (A) Fetch() {}\n" + // 11
	"\n" + // 12
	"type B struct{}\n" + // 13
	"\n" + // 14
	"func (B) Fetch() {}\n" // 15

// parityDoc carries one bullet per link-check outcome:
//   - Foo cited at :3, lives at :5           → moved
//   - Gone cited at :9, no such symbol       → unresolved
//   - Fetch: two methods share the base name → ambiguous
//   - tool.py with no line, unindexed lang   → skipped
//   - pkg.Bar resolves ONLY via the lastComponent fallback (codex names the
//     func "Bar", never "pkg.Bar") → ok, so it must stay silent
//   - Bar cited at :7, an exact name match   → ok, silent without the fallback
const parityDoc = "### Feature\n\none line.\n\n" +
	"- `Foo` — `x.go:3`\n" +
	"- `Gone` — `x.go:9`\n" +
	"- `Fetch` — `x.go:11`\n" +
	"- `Script` — `tool.py`\n" +
	"- `pkg.Bar` — `x.go:7`\n" +
	"- `Bar` — `x.go:7`\n" +
	"\n_x_\n"

// parityRepo builds a dross repo holding the fixture Go file and ARCHITECTURE.md.
func parityRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	gitInit(t, dir, "https://github.com/Rivil/dross.git")
	chdir(t, dir)
	if err := runCmd(t, Init()); err != nil {
		t.Fatalf("init: %v", err)
	}
	mustWrite(t, filepath.Join(dir, "x.go"), parityGoFile)
	mustWrite(t, filepath.Join(dir, "tool.py"), "def script():\n    pass\n")
	mustWrite(t, filepath.Join(dir, "ARCHITECTURE.md"), parityDoc)
	return dir
}

// TestArchitectureLinkWarningsParity pins doctor's architecture-link section
// for the parity fixture, written green on the pre-refactor resolver so a
// shared classifier can never change what doctor reports (spec-fact-drift c-8).
// Exactly the moved and the unresolved bullet warn; ambiguous, skipped and the
// fallback-resolved `pkg.Bar` stay silent.
func TestArchitectureLinkWarningsParity(t *testing.T) {
	dir := parityRepo(t)

	warnings, present := architectureLinkWarnings(dir)
	if !present {
		t.Fatal("expected present=true when ARCHITECTURE.md exists")
	}
	want := []string{
		"Foo — x.go:3 moved to line 5",
		"Gone — x.go:9 no longer resolves",
	}
	if !reflect.DeepEqual(warnings, want) {
		t.Errorf("doctor architecture-link warnings drifted.\n got: %q\nwant: %q", warnings, want)
	}
}

// TestArchitectureCheckOutputParity pins `dross architecture check` stdout for
// the parity fixture, byte for byte — the per-bullet lines and the
// "N moved, N unresolved, N ambiguous" summary. The `pkg.Bar` bullet prints
// nothing only while the architecture path keeps its lastComponent fallback;
// dropping it adds an unresolved line and bumps the summary.
func TestArchitectureCheckOutputParity(t *testing.T) {
	parityRepo(t)

	var runErr error
	out := captureStdout(t, func() {
		runErr = runCmd(t, Architecture(), "check")
	})
	if runErr != nil {
		t.Fatalf("architecture check: %v", runErr)
	}
	want := "  moved      Foo — x.go:3 → :5\n" +
		"  unresolved Gone — x.go:9 (no such symbol — left as-is)\n" +
		"  ambiguous  Fetch — x.go:11 (multiple matches — left as-is)\n" +
		"1 moved, 1 unresolved, 1 ambiguous. Run with --fix to repoint moved links.\n"
	if out != want {
		t.Errorf("`dross architecture check` output drifted.\n got: %q\nwant: %q", out, want)
	}
}
