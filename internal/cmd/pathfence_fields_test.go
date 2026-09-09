package cmd

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/pathfence"
)

// The enumerating guard behind c-6.
//
// artifact_scope (locked) puts EVERY path-shaped field in the .dross schemas in
// scope, not merely the ones something opens today — scoping to today's
// consumers would leave the same bug one field over the moment a consumer is
// added. pathfence.Fields() is that scope written down; this file is what keeps
// the writing honest, by walking the schema packages and comparing in BOTH
// directions:
//
//   - a path-shaped field with no registry entry fails, so adding one without
//     declaring it is a red test rather than a silent gap;
//   - a registry entry naming a struct or field that no longer exists fails as
//     a stale declaration, so a field renamed out from under a NotConsumed
//     entry cannot leave a promise about something that is gone.

// schemaDirs are the packages whose structs are serialized into .dross files.
// Relative to internal/cmd, which is where this test runs.
var schemaDirs = []string{"../project", "../phase", "../changes", "../verify"}

// pathShapedTags is the tag vocabulary the walker treats as path-shaped. It is
// deliberately WIDER than the set of real paths — "public", "tests" and
// "source" all collide with non-path fields — because a walker that guessed
// narrowly would silently stop finding things. The over-reach is resolved by
// declaring those fields NotConsumed with a reason, where a reader can audit
// the judgement, rather than by carve-outs buried in this walk.
var pathShapedTags = map[string]bool{
	"files": true, "doc": true, "path": true, "source": true, "tests": true,
	"e2e": true, "migrations": true, "schemas": true, "i18n": true,
	"public": true, "match": true,
}

// TestEveryPathShapedFieldIsDeclared is the two-way check.
func TestEveryPathShapedFieldIsDeclared(t *testing.T) {
	found := walkSchemaFields(t)

	declared := map[string]string{}
	for _, f := range pathfence.Fields() {
		declared[f.Name()] = f.Tag
	}

	for name, tag := range found {
		if _, ok := declared[name]; !ok {
			t.Errorf("undeclared path-shaped field %s (toml/json key %q): add it to "+
				"pathfence.Fields() as Consumed (naming its carrier) or NotConsumed "+
				"(saying what it is and who reads it)", name, tag)
		}
	}
	for name, tag := range declared {
		gotTag, ok := found[name]
		if !ok {
			t.Errorf("stale declaration %s (tag %q): pathfence.Fields() names a struct "+
				"or field that no longer exists, so its promise is about nothing", name, tag)
			continue
		}
		if gotTag != tag {
			t.Errorf("declaration %s records tag %q but the struct tag is %q", name, tag, gotTag)
		}
	}
}

// TestWalkerFindsANonEmptySet is the vacuity guard, mirroring
// TestGuardsSeeNonEmptySets. A walker that silently stopped matching would make
// the two-way check above pass by finding nothing on one side and reporting
// every entry as stale on the other — this fails first and says why.
func TestWalkerFindsANonEmptySet(t *testing.T) {
	found := walkSchemaFields(t)
	if len(found) < 10 {
		names := make([]string, 0, len(found))
		for n := range found {
			names = append(names, n)
		}
		sort.Strings(names)
		t.Fatalf("the walker found only %d path-shaped fields (%v) — it has stopped "+
			"seeing the schemas, and every assertion resting on it is now vacuous",
			len(found), names)
	}
}

// TestWalkerSelfTest drives the walker over an in-test source fixture, so a
// walker that stopped finding fields fails on something whose answer is written
// down here rather than passing vacuously against the real tree.
//
// It pins four rules at once:
//
//   - a path-shaped tag on a string field is FOUND;
//   - a tag outside the vocabulary is NOT;
//   - tag OPTIONS are stripped — `toml:"files,omitempty"` is found exactly as
//     `toml:"files"` is, which matters because every field the walker has to
//     find in internal/project carries an option, and a naive whole-value match
//     would find none of them;
//   - the TYPE test is real — a bool tagged `toml:"public"` is not reported,
//     which is the rule that drops project.Remote.Public without a carve-out.
func TestWalkerSelfTest(t *testing.T) {
	const src = `package fixture

type Pin struct {
	Doc     string   ` + "`toml:\"doc\"`" + `
	Replay  string   ` + "`toml:\"replay\"`" + `
	Files   []string ` + "`toml:\"files,omitempty\"`" + `
	Plain   []string ` + "`toml:\"tests\"`" + `
	Public  bool     ` + "`toml:\"public\"`" + `
	Count   int      ` + "`toml:\"files\"`" + `
	Nested  struct{} ` + "`toml:\"source\"`" + `
	Skipped string
	JSONed  string   ` + "`json:\"path,omitempty\"`" + `
}
`
	got := pathShapedFieldsInSrc(t, src)
	want := map[string]string{
		"fixture.Pin.Doc":    "doc",
		"fixture.Pin.Files":  "files",
		"fixture.Pin.Plain":  "tests",
		"fixture.Pin.JSONed": "path",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("walker self-test:\n got %v\nwant %v", got, want)
	}
}

// walkSchemaFields runs the walker over the real schema packages.
func walkSchemaFields(t *testing.T) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, dir := range schemaDirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("read %s: %v", dir, err)
		}
		for _, e := range entries {
			name := e.Name()
			if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
				continue
			}
			src, err := os.ReadFile(filepath.Join(dir, name))
			if err != nil {
				t.Fatal(err)
			}
			for k, v := range pathShapedFieldsInSrc(t, string(src)) {
				out[k] = v
			}
		}
	}
	return out
}

// pathShapedFieldsInSrc parses one Go source and returns every path-shaped
// field in it, keyed "<pkg>.<Type>.<Field>" with the tag key as the value.
//
// A field qualifies on two independent tests: its toml (or json) tag's FIRST
// component is in pathShapedTags, and its type is string or []string. The type
// test is what drops a bool tagged `toml:"public"`, and it is asserted directly
// in the self-test rather than left to be true by accident.
func pathShapedFieldsInSrc(t *testing.T, src string) map[string]string {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "src.go", src, 0)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	pkg := f.Name.Name
	out := map[string]string{}

	ast.Inspect(f, func(n ast.Node) bool {
		ts, ok := n.(*ast.TypeSpec)
		if !ok {
			return true
		}
		st, ok := ts.Type.(*ast.StructType)
		if !ok {
			return true
		}
		for _, field := range st.Fields.List {
			if field.Tag == nil || len(field.Names) == 0 {
				continue // embedded, or untagged: not a serialized key
			}
			key, ok := tagKey(t, field.Tag.Value)
			if !ok || !pathShapedTags[key] || !isPathType(field.Type) {
				continue
			}
			for _, id := range field.Names {
				out[pkg+"."+ts.Name.Name+"."+id.Name] = key
			}
		}
		return true
	})
	return out
}

// tagKey pulls the toml key (falling back to json) out of a raw struct tag,
// lowercased with its options stripped. It reports false for "-" and for a tag
// carrying neither key.
func tagKey(t *testing.T, raw string) (string, bool) {
	t.Helper()
	unquoted, err := strconv.Unquote(raw)
	if err != nil {
		t.Fatalf("unquote struct tag %s: %v", raw, err)
	}
	tag := reflect.StructTag(unquoted)
	v := tag.Get("toml")
	if v == "" {
		v = tag.Get("json")
	}
	// The split is load-bearing: every field the walker must find in
	// internal/project is tagged with an option (`toml:"files,omitempty"` and
	// siblings), so a whole-value match would find none of them and leave the
	// vacuity guard as the only witness.
	key := strings.ToLower(strings.TrimSpace(strings.Split(v, ",")[0]))
	if key == "" || key == "-" {
		return "", false
	}
	return key, true
}

// isPathType reports whether a field's type can hold a path: string or
// []string. Anything else — a bool, an int, a struct — is path-shaped by tag
// alone and is not a path.
func isPathType(e ast.Expr) bool {
	switch v := e.(type) {
	case *ast.Ident:
		return v.Name == "string"
	case *ast.ArrayType:
		if v.Len != nil {
			return false
		}
		id, ok := v.Elt.(*ast.Ident)
		return ok && id.Name == "string"
	}
	return false
}
