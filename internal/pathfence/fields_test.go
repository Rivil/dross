// This file is the EXTERNAL test package on purpose.
//
// Its stored-field assertion reflects over internal/changes, internal/verify,
// internal/phase and internal/project. Once those packages import pathfence —
// internal/verify and internal/phase both will — a `package pathfence` test
// importing them back is an import cycle and will not build. Go permits an
// EXTERNAL test package to import packages that import the package under test;
// an internal one does not. Written as `package pathfence` this compiles green
// today and breaks the moment a consumer is wired up, which is the worst
// possible discovery order.
package pathfence_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/changes"
	"github.com/Rivil/dross/internal/pathfence"
	"github.com/Rivil/dross/internal/phase"
	"github.com/Rivil/dross/internal/project"
	"github.com/Rivil/dross/internal/verify"
)

func TestRegistryIsWellFormed(t *testing.T) {
	for _, err := range pathfence.Validate(pathfence.Fields()) {
		t.Error(err)
	}
}

// TestValidateRejectsMalformedEntries feeds Validate synthetic bad entries.
// Without this, TestRegistryIsWellFormed would pass just as happily against a
// Validate that returned nil unconditionally.
func TestValidateRejectsMalformedEntries(t *testing.T) {
	ok := func() pathfence.Field {
		return pathfence.Field{
			Struct: "pkg.T", Field: "F", Tag: "files", Artifact: "a.json",
			NotConsumed: &pathfence.NotConsumedBy{Why: "because", Readers: []string{"somewhere"}},
		}
	}

	for _, tc := range []struct {
		name string
		in   []pathfence.Field
		want string
	}{
		{"no disposition", []pathfence.Field{func() pathfence.Field {
			f := ok()
			f.NotConsumed = nil
			return f
		}()}, "no disposition"},

		{"both dispositions", []pathfence.Field{func() pathfence.Field {
			f := ok()
			f.Consumed = &pathfence.ConsumedBy{Carrier: "x"}
			return f
		}()}, "both dispositions"},

		{"consumed without carrier", []pathfence.Field{func() pathfence.Field {
			f := ok()
			f.NotConsumed = nil
			f.Consumed = &pathfence.ConsumedBy{Carrier: "  "}
			return f
		}()}, "no Carrier"},

		{"not-consumed without why", []pathfence.Field{func() pathfence.Field {
			f := ok()
			f.NotConsumed.Why = ""
			return f
		}()}, "no Why"},

		{"not-consumed without readers", []pathfence.Field{func() pathfence.Field {
			f := ok()
			f.NotConsumed.Readers = nil
			return f
		}()}, "naming no reader"},

		{"empty artifact", []pathfence.Field{func() pathfence.Field {
			f := ok()
			f.Artifact = ""
			return f
		}()}, "empty Artifact"},

		{"duplicate", []pathfence.Field{ok(), ok()}, "declared twice"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			errs := pathfence.Validate(tc.in)
			if len(errs) == 0 {
				t.Fatalf("Validate accepted a %s entry", tc.name)
			}
			var joined []string
			for _, e := range errs {
				joined = append(joined, e.Error())
			}
			if !strings.Contains(strings.Join(joined, "\n"), tc.want) {
				t.Errorf("want an error naming %q, got:\n%s", tc.want, strings.Join(joined, "\n"))
			}
		})
	}
}

// TestRegistryCoversEveryKnownFieldByName pins the set by NAME, so deleting a
// declaration to make a later guard pass fails here first.
func TestRegistryCoversEveryKnownFieldByName(t *testing.T) {
	want := []string{
		"changes.TaskRecord.Files",
		"changes.RedProof.Doc",
		"verify.Scope.Files",
		"phase.Task.Files",
		"project.Env.Files",
		"project.TestLane.Match",
		"project.Paths.Source",
		"project.Paths.Tests",
		"project.Paths.E2E",
		"project.Paths.Migrations",
		"project.Paths.Schemas",
		"project.Paths.I18n",
		"project.Paths.Public",
		// path-shaped by tag only — see below
		"verify.Scope.Source",
		"verify.LanguageRun.Files",
		"verify.CriterionResult.Tests",
	}

	have := map[string]bool{}
	for _, f := range pathfence.Fields() {
		have[f.Name()] = true
	}
	for _, w := range want {
		if !have[w] {
			t.Errorf("registry is missing %s", w)
		}
	}
	if len(pathfence.Fields()) != len(want) {
		t.Errorf("registry has %d entries, the named set has %d — a new entry must be "+
			"added to this list so it is pinned by name, not merely counted",
			len(pathfence.Fields()), len(want))
	}
}

// TestMisleadingTagsAreDeclaredNotSpecialCased keeps the three by-tag-only
// fields out of t-9's walker as carve-outs. Declaring them puts the judgement
// somewhere a reader can audit.
func TestMisleadingTagsAreDeclaredNotSpecialCased(t *testing.T) {
	for _, name := range []string{
		"verify.Scope.Source",
		"verify.LanguageRun.Files",
		"verify.CriterionResult.Tests",
	} {
		f := find(t, name)
		if f.NotConsumed == nil {
			t.Errorf("%s must be declared not-consumed", name)
			continue
		}
		if len(f.NotConsumed.Why) < 40 {
			t.Errorf("%s: Why is too thin to justify a misleading tag: %q", name, f.NotConsumed.Why)
		}
	}
}

// TestTaskFilesIsAWriteTimeGateOnly pins the reasoning that makes
// phase.Task.Files not-consumed: ValidatePlan gates it on WRITE and opens
// nothing, so it says nothing about a reader.
func TestTaskFilesIsAWriteTimeGateOnly(t *testing.T) {
	f := find(t, "phase.Task.Files")
	if f.NotConsumed == nil {
		t.Fatal("phase.Task.Files must be not-consumed")
	}
	if !strings.Contains(f.NotConsumed.Why, "ValidatePlan") {
		t.Errorf("Why must name ValidatePlan as the write-time gate, got %q", f.NotConsumed.Why)
	}
	joined := strings.Join(f.NotConsumed.Readers, " ")
	for _, reader := range []string{"task.go", "issue_task.go"} {
		if !strings.Contains(joined, reader) {
			t.Errorf("Readers must name the print-only reader %s, got %v", reader, f.NotConsumed.Readers)
		}
	}
}

// TestConsumedEntriesCarryACarrier asserts DECLARATION only. That the named
// carrier is really typed pathfence.Contained is a claim about internal/cmd
// symbols, asserted from a package cmd test — this package cannot import
// internal/cmd without a cycle, and the symbols are unexported there anyway.
func TestConsumedEntriesCarryACarrier(t *testing.T) {
	want := map[string]string{
		"changes.TaskRecord.Files": "containScope",
		"verify.Scope.Files":       "containScope",
		"changes.RedProof.Doc":     "redProofPin.Doc",
	}
	for name, carrier := range want {
		f := find(t, name)
		if f.Consumed == nil {
			t.Errorf("%s must be declared consumed", name)
			continue
		}
		if f.Consumed.Carrier != carrier {
			t.Errorf("%s: Carrier = %q, want %q", name, f.Consumed.Carrier, carrier)
		}
	}
}

// TestStoredFieldsStayStringTyped blocks the obvious wrong fix — typing a
// stored field as Contained — which would silently break changes.json
// round-tripping, since the unexported fields cannot marshal. It fails here
// rather than at a user's next verify.
//
// This is the assertion that forces the external test package: it imports the
// four schema packages, and two of them will import pathfence.
func TestStoredFieldsStayStringTyped(t *testing.T) {
	for _, tc := range []struct {
		name string
		typ  reflect.Type
		fld  string
	}{
		{"changes.TaskRecord.Files", reflect.TypeOf(changes.TaskRecord{}), "Files"},
		{"changes.RedProof.Doc", reflect.TypeOf(changes.RedProof{}), "Doc"},
		{"verify.Scope.Files", reflect.TypeOf(verify.Scope{}), "Files"},
		{"phase.Task.Files", reflect.TypeOf(phase.Task{}), "Files"},
		{"project.Env.Files", reflect.TypeOf(project.Env{}), "Files"},
		{"project.Paths.Source", reflect.TypeOf(project.Paths{}), "Source"},
		{"project.TestLane.Match", reflect.TypeOf(project.TestLane{}), "Match"},
	} {
		sf, ok := tc.typ.FieldByName(tc.fld)
		if !ok {
			t.Errorf("%s: no such field — the registry carries a stale declaration", tc.name)
			continue
		}
		switch sf.Type.Kind() {
		case reflect.String:
		case reflect.Slice:
			if sf.Type.Elem().Kind() != reflect.String {
				t.Errorf("%s: []%s, want []string", tc.name, sf.Type.Elem())
			}
		default:
			t.Errorf("%s: %s, want string or []string — a stored field typed Contained "+
				"cannot marshal and would break round-tripping silently", tc.name, sf.Type)
		}
	}
}

func find(t *testing.T, name string) pathfence.Field {
	t.Helper()
	for _, f := range pathfence.Fields() {
		if f.Name() == name {
			return f
		}
	}
	t.Fatalf("registry has no entry %s", name)
	return pathfence.Field{}
}
