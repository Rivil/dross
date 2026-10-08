package toolfence

import (
	"path"
	"strings"
	"testing"
)

// TestValidateNamesEveryMalformedShape drives Validate over synthetic entries,
// one bad shape at a time. Asserting only that the REAL registry validates
// clean would pass identically against a Validate that returned nil
// unconditionally.
func TestValidateNamesEveryMalformedShape(t *testing.T) {
	good := Field{
		Struct: "verify.Thing", Field: "Error", Tag: "error", Artifact: "tests.json",
		Recorded: &Recorded{Carrier: "mutation.RecordLegError"},
	}

	cases := []struct {
		name  string
		in    []Field
		wants []string
	}{
		{
			name:  "no disposition",
			in:    []Field{{Struct: "verify.Thing", Field: "Error", Tag: "error", Artifact: "tests.json"}},
			wants: []string{"no disposition"},
		},
		{
			name: "two dispositions",
			in: []Field{{
				Struct: "verify.Thing", Field: "Error", Tag: "error", Artifact: "tests.json",
				Recorded:      &Recorded{Carrier: "mutation.RecordLegError"},
				NotToolStream: &NotToolStream{Why: "because", Writers: []string{"somewhere.go"}},
			}},
			wants: []string{"dispositions set"},
		},
		{
			name: "three dispositions",
			in: []Field{{
				Struct: "verify.Thing", Field: "Error", Tag: "error", Artifact: "tests.json",
				Recorded:      &Recorded{Carrier: "mutation.RecordLegError"},
				NotToolStream: &NotToolStream{Why: "because", Writers: []string{"somewhere.go"}},
				Renderable:    &Renderable{Why: "because", SourcedFrom: "verify.Thing.Error"},
			}},
			wants: []string{"dispositions set"},
		},
		{
			name: "recorded with blank carrier",
			in: []Field{{
				Struct: "verify.Thing", Field: "Error", Tag: "error", Artifact: "tests.json",
				Recorded: &Recorded{Carrier: "   "},
			}},
			wants: []string{"no Carrier"},
		},
		{
			name: "not-tool-stream with empty why",
			in: []Field{{
				Struct: "verify.Thing", Field: "Note", Tag: "note", Artifact: "tests.json",
				NotToolStream: &NotToolStream{Why: " ", Writers: []string{"somewhere.go"}},
			}},
			wants: []string{"no Why"},
		},
		{
			name: "not-tool-stream naming no writer",
			in: []Field{{
				Struct: "verify.Thing", Field: "Note", Tag: "note", Artifact: "tests.json",
				NotToolStream: &NotToolStream{Why: "dross prose"},
			}},
			wants: []string{"no Writer"},
		},
		{
			name: "renderable with no sourced-from",
			in: []Field{{
				Struct: "verify.Thing", Field: "Text", Tag: "text", Artifact: "verify.toml",
				Renderable: &Renderable{Why: "derived"},
			}},
			wants: []string{"no SourcedFrom"},
		},
		{
			name:  "renderable with no why",
			in:    []Field{{Struct: "verify.Thing", Field: "Text", Tag: "text", Artifact: "verify.toml", Renderable: &Renderable{SourcedFrom: "verify.Thing.Error"}}},
			wants: []string{"no Why"},
		},
		{
			name:  "duplicated name",
			in:    []Field{good, good},
			wants: []string{"declared twice"},
		},
		{
			name:  "missing coordinates",
			in:    []Field{{Recorded: &Recorded{Carrier: "mutation.RecordLegError"}}},
			wants: []string{"empty Struct", "empty Field", "empty Tag", "empty Artifact"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			errs := Validate(tc.in)
			if len(errs) == 0 {
				t.Fatalf("Validate accepted a malformed registry (%s)", tc.name)
			}
			joined := joinErrs(errs)
			for _, want := range tc.wants {
				if !strings.Contains(joined, want) {
					t.Errorf("Validate did not report %q; got: %s", want, joined)
				}
			}
		})
	}

	if errs := Validate(Fields()); len(errs) != 0 {
		t.Errorf("the real registry is malformed: %s", joinErrs(errs))
	}
	if errs := Validate([]Field{good}); len(errs) != 0 {
		t.Errorf("Validate rejected a well-formed entry: %s", joinErrs(errs))
	}
}

// TestRegistryPinsEveryDeclaredField is the by-name pin. Each named field is a
// sink that has to stay declared; the count check is what stops a NEW entry
// being added without a reader deciding what its disposition is.
func TestRegistryPinsEveryDeclaredField(t *testing.T) {
	type want struct {
		name        string
		disposition string
	}
	wants := []want{
		{"verify.LanguageRun.Error", "recorded"},
		{"verify.LegSummary.Error", "recorded"},
		{"verify.Finding.Text", "renderable"},
		{"telemetry.Event.ErrorDetail", "not-tool-stream"},
		{"verify.SkippedFile.Reason", "not-tool-stream"},
		{"verify.CriterionResult.Notes", "not-tool-stream"},
		{"verify.OutOfScopeMutant.Note", "not-tool-stream"},
		{"verify.Scope.Degraded", "not-tool-stream"},
		{"mutation.Mutant.Snippet", "not-tool-stream"},
		{"mutation.Mutant.Note", "not-tool-stream"},
	}

	got := map[string]string{}
	for _, f := range Fields() {
		got[f.Name()] = disposition(f)
	}

	for _, w := range wants {
		d, ok := got[w.name]
		if !ok {
			t.Errorf("%s is no longer declared — deleting a sink's entry silently un-fences it", w.name)
			continue
		}
		if d != w.disposition {
			t.Errorf("%s is declared %s, want %s", w.name, d, w.disposition)
		}
	}

	// The count, so a new entry cannot slip in unpinned above.
	if len(Fields()) != len(wants) {
		t.Errorf("registry holds %d entries, the pin above names %d — add the new field to this test with its disposition, or remove the entry", len(Fields()), len(wants))
	}
}

// TestRegistryStaysInsideTheWalkedRoots asserts the package doc's scope
// statement instead of trusting it. A field declared from a package the walker
// never visits is a declaration nothing enforces.
func TestRegistryStaysInsideTheWalkedRoots(t *testing.T) {
	roots := Roots()
	if len(roots) != 4 {
		t.Fatalf("Roots() names %d roots, want the 4 the doc comment states", len(roots))
	}
	inScope := map[string]bool{}
	for _, r := range roots {
		inScope[path.Base(r)] = true
	}
	for _, want := range []string{"cmd", "verify", "mutation", "telemetry"} {
		if !inScope[want] {
			t.Errorf("Roots() no longer covers internal/%s", want)
		}
	}

	for _, f := range Fields() {
		if pkg := f.Package(); !inScope[pkg] {
			t.Errorf("%s is declared from package %q, which is outside the walked roots %v — either widen Roots and the package doc comment, or the entry is unenforceable", f.Name(), pkg, roots)
		}
	}
}

func disposition(f Field) string {
	switch {
	case f.Recorded != nil:
		return "recorded"
	case f.NotToolStream != nil:
		return "not-tool-stream"
	case f.Renderable != nil:
		return "renderable"
	}
	return "none"
}

func joinErrs(errs []error) string {
	parts := make([]string, 0, len(errs))
	for _, e := range errs {
		parts = append(parts, e.Error())
	}
	return strings.Join(parts, "; ")
}
