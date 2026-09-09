package cmd

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/toolfence"
)

// c-4, the enumerating half: every persisted field that may carry tool-derived
// text is DECLARED, and every declaration still names a field that exists.
//
// It lives in internal/cmd rather than in internal/toolfence because
// internal/cmd imports the registry — a test inside the registry package could
// not walk the writers back. That is pathfence's arrangement and the same
// reason applies.
//
// The matching rule is written down here rather than inferred, and it is EXACT
// against the tag vocabulary. A prefix or substring rule would quietly widen the
// guard to fields the registry has no answer for, which reads as more coverage
// while meaning less.

// toolfenceVocabulary are the tag keys that make a text field a candidate sink.
// Exact match, never a prefix: `footnote` is not `note`.
var toolfenceVocabulary = map[string]bool{
	"error": true, "message": true, "note": true, "notes": true, "detail": true,
	"output": true, "stderr": true, "reason": true, "text": true, "degraded": true,
}

// persistedRoots are the schema types that actually reach disk. The walk is
// REACHABILITY from these, not "every struct in the walked packages": an option
// struct with a Reason field is not a sink, and a guard that reported it would
// be loosened rather than answered.
var persistedRoots = []string{"verify.Tests", "verify.Verify", "telemetry.Event"}

// toolfenceRoots are toolfence.Roots() as paths relative to internal/cmd, so
// the registry's own scope statement is what this walk uses.
func toolfenceRoots(t *testing.T) []string {
	t.Helper()
	var out []string
	for _, r := range toolfence.Roots() {
		base := path.Base(r)
		if base == "cmd" {
			out = append(out, ".")
			continue
		}
		out = append(out, "../"+base)
	}
	return out
}

// TestEveryPersistedTextSinkIsDeclared is the undeclared arm, over the live tree.
func TestEveryPersistedTextSinkIsDeclared(t *testing.T) {
	structs, visited := toolfenceStructs(t)
	for dir, n := range visited {
		if n == 0 {
			t.Errorf("the walk visited no non-test file in %s — every assertion over that root is vacuous", dir)
		}
	}
	if len(visited) != 4 {
		t.Fatalf("the walk covered %d roots, want the 4 toolfence.Roots() names", len(visited))
	}

	declared := map[string]toolfence.Field{}
	for _, f := range toolfence.Fields() {
		declared[f.Name()] = f
	}

	reach := reachableStructs(structs, persistedRoots)
	if len(reach) < len(persistedRoots) {
		t.Fatalf("reachability from %v resolved only %d structs — the type walk is not resolving cross-package references", persistedRoots, len(reach))
	}

	found := 0
	for _, name := range sortedKeys(reach) {
		for _, sink := range textSinks(structs[name]) {
			found++
			full := name + "." + sink.field
			if _, ok := declared[full]; !ok {
				t.Errorf("%s (tag %q, %s) is a persisted text field with no toolfence entry — declare it Recorded, NotToolStream or Renderable",
					full, sink.tag, sink.kind)
			}
		}
	}

	// Vacuity floor: a matcher that stopped matching reports nothing and passes.
	if found < 3 {
		t.Errorf("the walk resolved %d persisted text sinks, want at least 3 — it is passing because it found nothing to check", found)
	}
	// The []string arm specifically, because a walk restricted to scalar
	// strings would look healthy while missing Scope.Degraded entirely.
	if !sinkFound(structs, reach, "verify.Scope", "Degraded") {
		t.Error("verify.Scope.Degraded was not seen — the walk is restricted to string-typed fields and misses every slice sink")
	}
}

// TestNoStaleDeclaration is the other direction: an entry naming a field that no
// longer exists is a declaration nothing enforces.
//
// CriterionResult.Notes is the interesting case and it resolves like any other —
// the field exists, it is simply written by the agent editing verify.toml rather
// than by Go code, which is what its Writers record says.
func TestNoStaleDeclaration(t *testing.T) {
	structs, _ := toolfenceStructs(t)

	for _, f := range toolfence.Fields() {
		st, ok := structs[f.Struct]
		if !ok {
			t.Errorf("%s declares struct %s, which no longer exists in the walked roots", f.Name(), f.Struct)
			continue
		}
		if !hasField(st, f.Field) {
			t.Errorf("%s is a stale declaration — %s has no field %s", f.Name(), f.Struct, f.Field)
		}
	}
	if len(toolfence.Fields()) == 0 {
		t.Fatal("the registry is empty, so this arm asserts nothing")
	}
}

// TestSinkMatcherIsCalibrated drives the matcher over a fixture whose answers
// are written down. The live tree is already clean, so a matcher that stopped
// matching would pass there and only fail here.
func TestSinkMatcherIsCalibrated(t *testing.T) {
	structs := parseFixtureStructs(t, "undeclared_sink.go.txt", "verify")

	got := map[string][]string{}
	for name, st := range structs {
		for _, s := range textSinks(st) {
			got[name] = append(got[name], s.field)
		}
	}

	if len(got["verify.ToolRun"]) != 1 || got["verify.ToolRun"][0] != "Stderr" {
		t.Errorf("the matcher did not report the undeclared `stderr` sink: %v", got)
	}
	// EXACT, not prefix: `footnote` must not match `note`, and a `notes`-tagged
	// INT is not a text sink.
	if n := len(got["verify.Annotation"]); n != 0 {
		t.Errorf("the matcher reported %d field(s) on a struct with no vocabulary tag — a prefix regression silently widens the guard: %v", n, got["verify.Annotation"])
	}
	if len(got["verify.Degradation"]) != 1 {
		t.Errorf("the matcher missed the []string `degraded` sink: %v", got)
	}

	// The ARM, not just the matcher: a matched field with no registry entry is
	// what has to be reported. verify.ToolRun is not in the registry, so it is
	// undeclared by construction.
	declared := map[string]bool{}
	for _, f := range toolfence.Fields() {
		declared[f.Name()] = true
	}
	undeclared := 0
	for name, fields := range got {
		for _, field := range fields {
			if !declared[name+"."+field] {
				undeclared++
			}
		}
	}
	if undeclared == 0 {
		t.Error("the undeclared arm reported nothing over a fixture whose sinks are all absent from the registry")
	}
	if declared["verify.ToolRun.Stderr"] {
		t.Error("the fixture's sink is in the real registry, so it cannot demonstrate the undeclared arm")
	}
}

// TestVocabularyIsExact states the rule from both sides on a synthetic struct,
// so the difference between `notes` and `footnote` is a written-down answer
// rather than a property of whatever the tree happens to contain.
func TestVocabularyIsExact(t *testing.T) {
	const src = `package verify

type Synthetic struct {
	Notes    string ` + "`json:\"notes\"`" + `
	Footnote string ` + "`json:\"footnote\"`" + `
	Untagged string
	Reason   string ` + "`json:\"reason\"`" + `
	Skipped  string ` + "`json:\"-\"`" + `
}
`
	structs := parseStructSource(t, "synthetic.go", src, "verify")
	got := map[string]bool{}
	for _, s := range textSinks(structs["verify.Synthetic"]) {
		got[s.field] = true
	}

	if !got["Notes"] {
		t.Error("a field tagged `notes` did not trip the matcher")
	}
	if got["Footnote"] {
		t.Error("a field tagged `footnote` tripped the matcher — the vocabulary is being prefix-matched")
	}
	if !got["Reason"] {
		t.Error("a field tagged `reason` did not trip the matcher")
	}
	if got["Untagged"] {
		t.Error("an untagged field whose lowercased name is outside the vocabulary tripped the matcher")
	}
	if got["Skipped"] {
		t.Error("a field tagged `-` — never serialized — tripped the matcher")
	}

	// The untagged arm, which is why mutation.Mutant is not silently excluded:
	// its fields carry no tags at all.
	const untagged = `package mutation

type Mutantish struct {
	Note    string
	Snippet string
	line    string
}
`
	uns := parseStructSource(t, "untagged.go", untagged, "mutation")
	seen := map[string]bool{}
	for _, s := range textSinks(uns["mutation.Mutantish"]) {
		seen[s.field] = true
	}
	if !seen["Note"] {
		t.Error("an UNTAGGED exported string field named Note was not matched by its lowercased name — mutation.Mutant would be silently excluded")
	}
	if seen["line"] {
		t.Error("an unexported field was matched; it is never serialized")
	}
}

// --- the walk ---------------------------------------------------------------

type textSink struct {
	field string
	tag   string
	kind  string // "string" | "[]string"
}

// toolfenceStructs collects every struct declared in the walked roots, keyed
// "pkg.Name", and reports how many non-test files each root contributed.
func toolfenceStructs(t *testing.T) (map[string]*ast.StructType, map[string]int) {
	t.Helper()
	structs := map[string]*ast.StructType{}
	visited := map[string]int{}

	for _, dir := range toolfenceRoots(t) {
		pkg := path.Base(dir)
		if dir == "." {
			pkg = "cmd"
		}
		visited[dir] = 0
		for _, name := range goFilesIn(t, dir) {
			visited[dir]++
			for k, v := range parseStructs(t, name, pkg) {
				structs[k] = v
			}
		}
	}
	return structs, visited
}

func parseStructs(t *testing.T, path, pkg string) map[string]*ast.StructType {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return parseStructSource(t, path, string(b), pkg)
}

func parseStructSource(t *testing.T, name, src, pkg string) map[string]*ast.StructType {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, name, src, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse %s: %v", name, err)
	}
	out := map[string]*ast.StructType{}
	ast.Inspect(f, func(n ast.Node) bool {
		ts, ok := n.(*ast.TypeSpec)
		if !ok {
			return true
		}
		if st, ok := ts.Type.(*ast.StructType); ok {
			out[pkg+"."+ts.Name.Name] = st
		}
		return true
	})
	return out
}

func parseFixtureStructs(t *testing.T, name, pkg string) map[string]*ast.StructType {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "toolfence", name))
	if err != nil {
		t.Fatal(err)
	}
	return parseStructSource(t, name, string(b), pkg)
}

// reachableStructs walks the type graph out from the persisted roots.
func reachableStructs(structs map[string]*ast.StructType, roots []string) map[string]bool {
	seen := map[string]bool{}
	queue := append([]string(nil), roots...)
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		if seen[cur] {
			continue
		}
		st, ok := structs[cur]
		if !ok {
			continue
		}
		seen[cur] = true
		pkg := cur[:strings.Index(cur, ".")]
		for _, f := range st.Fields.List {
			for _, ref := range typeRefs(f.Type, pkg) {
				if !seen[ref] {
					queue = append(queue, ref)
				}
			}
		}
	}
	return seen
}

// textSinks reports the fields of one struct that match the vocabulary.
func textSinks(st *ast.StructType) []textSink {
	if st == nil {
		return nil
	}
	var out []textSink
	for _, f := range st.Fields.List {
		kind := stringKind(f.Type)
		if kind == "" {
			continue
		}
		tag, ok := serializedKey(f)
		if !ok || !toolfenceVocabulary[tag] {
			continue
		}
		for _, id := range f.Names {
			out = append(out, textSink{field: id.Name, tag: tag, kind: kind})
		}
	}
	return out
}

// stringKind reports "string", "[]string", or "" for anything else.
func stringKind(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.Ident:
		if t.Name == "string" {
			return "string"
		}
	case *ast.ArrayType:
		if id, ok := t.Elt.(*ast.Ident); ok && id.Name == "string" && t.Len == nil {
			return "[]string"
		}
	}
	return ""
}

// serializedKey returns the json/toml key a field is written under, falling back
// to the lowercased field name for an untagged EXPORTED field — which is how
// mutation.Mutant, which carries no tags at all, is not silently excluded.
func serializedKey(f *ast.Field) (string, bool) {
	if len(f.Names) == 0 || !f.Names[0].IsExported() {
		return "", false
	}
	if f.Tag != nil {
		raw := strings.Trim(f.Tag.Value, "`")
		for _, key := range []string{"json", "toml"} {
			marker := key + ":\""
			i := strings.Index(raw, marker)
			if i < 0 {
				continue
			}
			rest := raw[i+len(marker):]
			j := strings.Index(rest, "\"")
			if j < 0 {
				continue
			}
			name := strings.SplitN(rest[:j], ",", 2)[0]
			if name == "-" {
				return "", false
			}
			if name != "" {
				return strings.ToLower(name), true
			}
		}
	}
	return strings.ToLower(f.Names[0].Name), true
}

func typeRefs(e ast.Expr, pkg string) []string {
	switch t := e.(type) {
	case *ast.Ident:
		return []string{pkg + "." + t.Name}
	case *ast.SelectorExpr:
		if id, ok := t.X.(*ast.Ident); ok {
			return []string{id.Name + "." + t.Sel.Name}
		}
	case *ast.StarExpr:
		return typeRefs(t.X, pkg)
	case *ast.ArrayType:
		return typeRefs(t.Elt, pkg)
	case *ast.MapType:
		return append(typeRefs(t.Key, pkg), typeRefs(t.Value, pkg)...)
	}
	return nil
}

func hasField(st *ast.StructType, name string) bool {
	for _, f := range st.Fields.List {
		for _, id := range f.Names {
			if id.Name == name {
				return true
			}
		}
	}
	return false
}

func sinkFound(structs map[string]*ast.StructType, reach map[string]bool, structName, field string) bool {
	if !reach[structName] {
		return false
	}
	for _, s := range textSinks(structs[structName]) {
		if s.field == field {
			return true
		}
	}
	return false
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
