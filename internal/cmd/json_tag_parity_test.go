package cmd

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/defaults"
	"github.com/Rivil/dross/internal/milestone"
	"github.com/Rivil/dross/internal/phase"
	"github.com/Rivil/dross/internal/profile"
	"github.com/Rivil/dross/internal/project"
	"github.com/Rivil/dross/internal/stack"
)

// The `show --json` surface marshals the same structs the toml decoder fills,
// so the two tag sets are one contract: a field with a toml name and no json
// name emits its Go field name instead, and the payload silently stops matching
// the document it claims to be. This file is the gate — every field carrying a
// toml tag must carry an identical json tag.
//
// It walks the schema roots rather than listing fields, so a struct added to
// any of them years from now is covered on the day it lands.

// schemaRoots are the documents `show --json` can emit. Each is walked
// transitively, so nested blocks ([board], [runtime], the task list) are
// covered without being named here.
func schemaRoots() []any {
	return []any{
		project.Project{},
		milestone.Milestone{},
		defaults.Defaults{},
		profile.Profile{},
		stack.Profile{},
		phase.Spec{},
		phase.Plan{},
		phase.Task{},
	}
}

// TestTomlFieldsCarryMatchingJSONTags proves c-5's precondition: every
// toml-named field is json-named identically, omitempty included.
//
// The omitempty half matters as much as the name: a field the toml encoder
// omits when empty but the json encoder emits as null renders a document the
// user never sees in `show`, which is the same mismatch by a different route.
func TestTomlFieldsCarryMatchingJSONTags(t *testing.T) {
	// One seen-set across all roots: the check is type-level, so a struct
	// reachable from two documents (phase.Task, from both Plan and itself)
	// is checked once and reported once.
	seen := map[reflect.Type]bool{}
	for _, root := range schemaRoots() {
		walkStructFields(t, reflect.TypeOf(root), seen, func(owner reflect.Type, f reflect.StructField) {
			tomlTag, ok := f.Tag.Lookup("toml")
			if !ok {
				return
			}
			jsonTag, ok := f.Tag.Lookup("json")
			if !ok {
				t.Errorf("%s.%s has toml:%q and no json tag — `show --json` would emit %q instead of %q",
					owner.String(), f.Name, tomlTag, f.Name, tomlName(tomlTag))
				return
			}
			if jsonTag != tomlTag {
				t.Errorf("%s.%s: toml:%q != json:%q — the two names (or their omitempty) must match exactly",
					owner.String(), f.Name, tomlTag, jsonTag)
			}
		})
	}
}

// tomlName is the name half of a struct tag value, for the error message above.
func tomlName(tag string) string {
	if i := strings.Index(tag, ","); i >= 0 {
		return tag[:i]
	}
	return tag
}

// walkStructFields visits every field of t and of any struct reachable from it
// through pointers, slices, arrays and map values. seen breaks recursive types.
func walkStructFields(t *testing.T, typ reflect.Type, seen map[reflect.Type]bool, visit func(reflect.Type, reflect.StructField)) {
	t.Helper()
	typ = deref(typ)
	if typ.Kind() != reflect.Struct || seen[typ] {
		return
	}
	seen[typ] = true
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		if f.PkgPath != "" { // unexported — neither encoder sees it
			continue
		}
		visit(typ, f)
		walkStructFields(t, f.Type, seen, visit)
	}
}

// deref unwraps pointers, slices, arrays and maps down to the element type the
// encoders actually descend into.
func deref(typ reflect.Type) reflect.Type {
	for {
		switch typ.Kind() {
		case reflect.Ptr, reflect.Slice, reflect.Array:
			typ = typ.Elem()
		case reflect.Map:
			typ = typ.Elem()
		default:
			return typ
		}
	}
}

// TestJSONTagsRenameFieldsToDocumentNames is the behavioural half of the walk
// above: the tags are not just present, they are what the encoder uses.
func TestJSONTagsRenameFieldsToDocumentNames(t *testing.T) {
	raw, err := json.Marshal(phase.Task{TestContract: []string{"x"}})
	if err != nil {
		t.Fatalf("marshal task: %v", err)
	}
	got := string(raw)
	if !strings.Contains(got, `"test_contract"`) {
		t.Errorf("payload has no \"test_contract\" key: %s", got)
	}
	if strings.Contains(got, `"TestContract"`) {
		t.Errorf("payload carries the Go field name TestContract: %s", got)
	}
}

// TestJSONOmitemptyMatchesToml pins the second half of the tag: a field the
// toml document omits when empty is omitted from the payload too, and one that
// is always written is always present.
func TestJSONOmitemptyMatchesToml(t *testing.T) {
	raw, err := json.Marshal(phase.Task{ID: "t-1"})
	if err != nil {
		t.Fatalf("marshal task: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	// Status is toml:"status,omitempty" and left zero here.
	if _, present := got["status"]; present {
		t.Errorf("zero omitempty field \"status\" is present: %s", raw)
	}
	// Wave is toml:"wave" with no omitempty, and is zero here.
	if _, present := got["wave"]; !present {
		t.Errorf("zero non-omitempty field \"wave\" is absent: %s", raw)
	}
}

// TestSkippedTomlFieldsAreSkippedInJSON pins the mirroring rule for the one
// tag shape the schema does not currently use.
//
// No field in the six schema files carries toml:"-" today, so this asserts the
// convention on a stand-in rather than pretending to cover a real field: when
// one is added, mirroring it as json:"-" is what keeps a field the toml
// document deliberately omits out of the payload as well. The parity walk above
// accepts the pair because the two tags are identical.
func TestSkippedTomlFieldsAreSkippedInJSON(t *testing.T) {
	type withSkipped struct {
		Kept    string `toml:"kept" json:"kept"`
		Skipped string `toml:"-" json:"-"`
	}
	raw, err := json.Marshal(withSkipped{Kept: "yes", Skipped: "secret"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(raw), "secret") || strings.Contains(string(raw), "Skipped") {
		t.Errorf(`a toml:"-" field mirrored as json:"-" still reached the payload: %s`, raw)
	}
	if !strings.Contains(string(raw), `"kept"`) {
		t.Errorf("sibling field was dropped too: %s", raw)
	}

	// And the walk treats the pair as matching, so mirroring "-" is not a
	// parity violation the gate would reject.
	typ := reflect.TypeOf(withSkipped{})
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		if got, want := f.Tag.Get("json"), f.Tag.Get("toml"); got != want {
			t.Errorf("%s: toml:%q != json:%q", f.Name, want, got)
		}
	}
}
