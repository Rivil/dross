package project

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

// loadFixture returns the lossless fixture as a Project plus an independent
// copy to mutate, so every test diffs a struct against its own unchanged
// twin and any op it sees is one its mutation caused.
func loadFixture(t *testing.T) (base, mutated *Project) {
	t.Helper()
	path := filepath.Join("testdata", "lossless.toml")
	base, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	mutated, err = Load(path)
	if err != nil {
		t.Fatal(err)
	}
	return base, mutated
}

func mustDiff(t *testing.T, old, new *Project) []op {
	t.Helper()
	ops, err := diff(old, new)
	if err != nil {
		t.Fatalf("diff: %v", err)
	}
	return ops
}

func opStrings(ops []op) []string {
	out := make([]string, len(ops))
	for i, o := range ops {
		out[i] = o.String()
	}
	return out
}

func TestDiffNoChangeIsNoOps(t *testing.T) {
	base, twin := loadFixture(t)
	if ops := mustDiff(t, base, twin); len(ops) != 0 {
		t.Errorf("fixture diffed against itself: %v", opStrings(ops))
	}
	// And the config dross itself ships.
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 6; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			break
		}
		dir = filepath.Dir(dir)
	}
	path := filepath.Join(dir, ".dross", "project.toml")
	if _, err := os.Stat(path); err != nil {
		t.Skipf("no repo project.toml at %s", path)
	}
	p, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if ops := mustDiff(t, p, p); len(ops) != 0 {
		t.Errorf("repo project.toml diffed against itself: %v", opStrings(ops))
	}
}

func TestDiffScalarChangeIsOneOp(t *testing.T) {
	base, m := loadFixture(t)
	m.Project.Version = "1.7.11.0"
	ops := mustDiff(t, base, m)
	want := []op{{kind: opKey, table: []string{"project"}, elem: -1, key: "version", value: "1.7.11.0"}}
	if !reflect.DeepEqual(ops, want) {
		t.Errorf("ops = %#v, want %#v", ops, want)
	}
}

func TestDiffOmitemptyZeroIsDelete(t *testing.T) {
	base, m := loadFixture(t)
	base.Stack.Profile, m.Stack.Profile = "go", ""
	ops := mustDiff(t, base, m)
	want := []op{{kind: opKey, table: []string{"stack"}, elem: -1, key: "profile"}}
	if !reflect.DeepEqual(ops, want) {
		t.Errorf("ops = %#v, want a single delete", ops)
	}
}

func TestDiffRequiredZeroIsSetLiteral(t *testing.T) {
	base, m := loadFixture(t)
	m.Repo.Layout = ""
	m.Repo.SquashMerge = false
	ops := mustDiff(t, base, m)
	want := []op{
		{kind: opKey, table: []string{"repo"}, elem: -1, key: "layout", value: ""},
		{kind: opKey, table: []string{"repo"}, elem: -1, key: "squash_merge", value: false},
	}
	if !reflect.DeepEqual(ops, want) {
		t.Errorf("ops = %#v, want %#v", ops, want)
	}
}

func TestDiffMapDeleteIsPerKey(t *testing.T) {
	base, m := loadFixture(t)
	base.Board.StateMap["verified"] = "Done"
	delete(m.Board.StateMap, "verified")
	m.Runtime.Services = map[string]Service{"db": {URL: "postgres://x", Admin: "psql"}}
	ops := mustDiff(t, base, m)
	want := []string{
		"set runtime.services.db.url",
		"set runtime.services.db.admin",
		"delete board.state_map.verified",
	}
	if got := opStrings(ops); !reflect.DeepEqual(got, want) {
		t.Errorf("ops = %v, want %v", got, want)
	}
	for _, o := range ops {
		if _, isMap := o.value.(map[string]any); isMap {
			t.Errorf("%s carries a whole table as its value", o)
		}
	}
}

func TestDiffArrayOfTablesEditInPlace(t *testing.T) {
	base, m := loadFixture(t)
	m.Runtime.TestLane[1].Command = "go test -race ./internal/project/..."
	ops := mustDiff(t, base, m)
	want := []op{{kind: opKey, table: []string{"runtime", "test_lane"}, elem: 1, key: "command", value: "go test -race ./internal/project/..."}}
	if !reflect.DeepEqual(ops, want) {
		t.Errorf("ops = %#v, want %#v", ops, want)
	}
}

func TestDiffArrayOfTablesShrinkRemovesOnlyThatElement(t *testing.T) {
	base, m := loadFixture(t)
	m.Runtime.TestLane = append(m.Runtime.TestLane[:1], m.Runtime.TestLane[2:]...)
	ops := mustDiff(t, base, m)
	want := []op{{kind: opDeleteElem, table: []string{"runtime", "test_lane"}, elem: 1}}
	if !reflect.DeepEqual(ops, want) {
		t.Errorf("ops = %#v, want %#v", ops, want)
	}
}

func TestDiffArrayOfTablesGrowAppends(t *testing.T) {
	base, m := loadFixture(t)
	m.Runtime.TestLane = append(m.Runtime.TestLane, TestLane{Name: "verify", Match: []string{"internal/verify/**"}, Command: "go test ./internal/verify/..."})
	ops := mustDiff(t, base, m)
	if len(ops) != 1 || ops[0].kind != opAppendElem || !reflect.DeepEqual(ops[0].table, []string{"runtime", "test_lane"}) {
		t.Fatalf("ops = %#v, want one append", ops)
	}
	b, ok := ops[0].value.(block)
	if !ok {
		t.Fatalf("append value is %T, want block", ops[0].value)
	}
	var keys []string
	for _, e := range b {
		keys = append(keys, e.key)
	}
	// The encoder's field order, not the map's.
	if want := []string{"name", "match", "command"}; !reflect.DeepEqual(keys, want) {
		t.Errorf("block keys = %v, want %v", keys, want)
	}
}

// populate fills every field of v with a non-zero value derived from its
// path: two elements per slice, two entries per map, so every array-of-
// tables has two blocks and every map two keys. Reflection, not a literal,
// so a field added tomorrow is covered the day it lands.
func populate(v reflect.Value, seed string) {
	switch v.Kind() {
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			if f := v.Field(i); f.CanSet() {
				populate(f, seed+"."+v.Type().Field(i).Name)
			}
		}
	case reflect.String:
		v.SetString(seed)
	case reflect.Bool:
		v.SetBool(true)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		v.SetInt(int64(len(seed)))
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		v.SetUint(uint64(len(seed)))
	case reflect.Float32, reflect.Float64:
		v.SetFloat(1.5)
	case reflect.Slice:
		s := reflect.MakeSlice(v.Type(), 2, 2)
		for i := 0; i < 2; i++ {
			populate(s.Index(i), seed+"["+string(rune('0'+i))+"]")
		}
		v.Set(s)
	case reflect.Map:
		m := reflect.MakeMap(v.Type())
		for _, k := range []string{"a", "b"} {
			e := reflect.New(v.Type().Elem()).Elem()
			populate(e, seed+"."+k)
			m.SetMapIndex(reflect.ValueOf(k).Convert(v.Type().Key()), e)
		}
		v.Set(m)
	case reflect.Pointer:
		p := reflect.New(v.Type().Elem())
		populate(p.Elem(), seed)
		v.Set(p)
	}
}

// TestDiffCoversEveryProjectValueKind walks the generic tree of a fully
// populated Project in both directions and applies the ops, so a field of a
// shape the differ cannot classify — or the patcher cannot splice — fails
// here rather than surfacing as a dropped write in some future command.
func TestDiffCoversEveryProjectValueKind(t *testing.T) {
	full := &Project{}
	populate(reflect.ValueOf(full).Elem(), "p")
	empty := &Project{}

	for _, tc := range []struct {
		name     string
		from, to *Project
	}{
		{"empty→full", empty, full},
		{"full→empty", full, empty},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ops := mustDiff(t, tc.from, tc.to)
			if len(ops) == 0 {
				t.Fatal("no ops between empty and fully-populated")
			}
			src, err := encodeCanonical(tc.from)
			if err != nil {
				t.Fatal(err)
			}
			out, err := apply(src, ops)
			if err != nil {
				t.Fatalf("apply: %v", err)
			}
			// Canonical comparison: decode the patched text as a Project and
			// re-encode it, so an empty [paths] header the patcher dropped
			// and one the encoder writes compare equal — they load the same.
			var got Project
			if _, err := toml.Decode(string(out), &got); err != nil {
				t.Fatalf("patched document does not decode: %v\n%s", err, out)
			}
			gotSrc, err := encodeCanonical(&got)
			if err != nil {
				t.Fatal(err)
			}
			wantSrc, err := encodeCanonical(tc.to)
			if err != nil {
				t.Fatal(err)
			}
			if string(gotSrc) != string(wantSrc) {
				t.Errorf("patched document differs from target\n--- patched ---\n%s\n--- target ---\n%s", out, wantSrc)
			}
		})
	}
}

// TestDiffUsesOneEncoderHelper pins the differ's encoder hand-off: the tree
// conversion goes through encodeCanonical and nothing else in the file
// touches the encoder, so t-3 can fold the package down to one call site.
func TestDiffUsesOneEncoderHelper(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "patch_diff.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}
		ast.Inspect(fn, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			pkg, ok := sel.X.(*ast.Ident)
			if !ok || pkg.Name != "toml" {
				return true
			}
			if (sel.Sel.Name == "Marshal" || sel.Sel.Name == "NewEncoder") && fn.Name.Name != "encodeCanonical" {
				t.Errorf("%s: toml.%s called in %s, not encodeCanonical", fset.Position(call.Pos()), sel.Sel.Name, fn.Name.Name)
			}
			return true
		})
	}
	// And encodeCanonical itself defers to the package helper rather than
	// configuring an encoder of its own.
	src, err := os.ReadFile("patch_diff.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(src), "return encodeFresh(p)") {
		t.Error("encodeCanonical no longer routes through encodeFresh")
	}
}
