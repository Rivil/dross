package verify

import (
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/mutation"
)

// The funnel: every persisted leg-error string comes from the carrier.
//
// This is what makes the c-4 walker's assignment arm checkable at all. The
// stored field has to stay a Go string — LegError's unexported field cannot
// marshal — so the named type lives on the DERIVED value, and the guard keys on
// the CALL rather than on the field's type. Revert this assignment to
// err.Error() and the walker in internal/cmd goes red.

// TestRecordLegErrorReturnsANamedType. A bare `string` return would leave the
// walker nothing to accept: every raw literal and every fmt.Sprintf looks
// exactly like a carrier call once the type is gone.
func TestRecordLegErrorReturnsANamedType(t *testing.T) {
	ft := reflect.TypeOf(mutation.RecordLegError)
	if ft.NumOut() != 1 {
		t.Fatalf("RecordLegError returns %d values, want 1", ft.NumOut())
	}
	out := ft.Out(0)
	if out.Kind() == reflect.String && out.PkgPath() == "" {
		t.Fatal("RecordLegError returns a bare string — the walker's accepted-RHS arm has nothing to key on")
	}
	if out.Name() == "" || out.PkgPath() == "" {
		t.Errorf("RecordLegError's return %s is unnamed; the carrier must be a named type", out)
	}
	// And it still yields a plain string for the field.
	if got := reflect.TypeOf(mutation.RecordLegError(errors.New("x")).String()); got.Kind() != reflect.String {
		t.Errorf("carrier.String() is %s, want string", got)
	}
}

// TestLegErrorFieldsStayStrings. Changing either field's Go type breaks
// serialization, and it must break HERE — with a message naming the field —
// rather than downstream inside encoding/json or the toml encoder.
func TestLegErrorFieldsStayStrings(t *testing.T) {
	for _, tc := range []struct {
		typ   reflect.Type
		field string
	}{
		{reflect.TypeOf(LanguageRun{}), "Error"},
		{reflect.TypeOf(LegSummary{}), "Error"},
	} {
		f, ok := tc.typ.FieldByName(tc.field)
		if !ok {
			t.Errorf("%s has no field %s", tc.typ, tc.field)
			continue
		}
		if f.Type.Kind() != reflect.String || f.Type.PkgPath() != "" {
			t.Errorf("%s.%s is %s, want a plain string — the carrier's unexported field cannot marshal, so the type lives on the derived value",
				tc.typ, tc.field, f.Type)
		}
	}
}

// TestLegErrorRoundTripsThroughBothArtifacts. LegSummary.Error is populated from
// LanguageRun.Error, so the two files must agree; a leg whose error vanished
// from verify.toml reads as a clean run.
func TestLegErrorRoundTripsThroughBothArtifacts(t *testing.T) {
	const boom = "stryker failed with exit status 3; 42 bytes of tool output observed"

	tests := &Tests{
		Phase: "p",
		Languages: []LanguageRun{
			{Name: "typescript", Tool: "stryker", Files: []string{"x.ts"}, Error: boom},
		},
	}
	dir := t.TempDir()
	testsPath := filepath.Join(dir, TestsFile)
	if err := tests.Save(testsPath); err != nil {
		t.Fatal(err)
	}
	back, err := LoadTests(testsPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(back.Languages) != 1 || back.Languages[0].Error != boom {
		t.Fatalf("LanguageRun.Error did not round-trip through %s: %+v", TestsFile, back.Languages)
	}

	v := Skeleton(back, []string{"c-1"})
	verifyPath := filepath.Join(dir, VerifyFile)
	if err := v.Save(verifyPath); err != nil {
		t.Fatal(err)
	}
	vback, err := LoadVerify(verifyPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(vback.Summary.Legs) != 1 {
		t.Fatalf("want one leg in %s, got %+v", VerifyFile, vback.Summary.Legs)
	}
	if got := vback.Summary.Legs[0].Error; got != boom {
		t.Errorf("LegSummary.Error = %q, want %q — a leg whose error vanished from %s reads as a clean run", got, boom, VerifyFile)
	}
}

// TestCarrierDoesNotSwallowDrossAuthoredDiagnostics. The funnel must not rewrite
// what it carries. An adapter that failed on dross's OWN refusal — an argfence
// fence, an unreadable report — has to keep saying so, or the funnel would erase
// exactly the diagnostics this phase never meant to touch. It is also what lets
// stryker's checkInstrumented keep its dropped-path prose intact.
func TestCarrierDoesNotSwallowDrossAuthoredDiagnostics(t *testing.T) {
	const refusal = "stryker: refusing argument \"--mutate=$(rm -rf /)\": shell metacharacters"

	stry := &fakeAdapter{
		name:        "stryker",
		supportsExt: []string{".ts"},
		err:         errors.New(refusal),
	}
	got, err := Run("p", []string{"x.ts"}, []mutation.Adapter{stry})
	if err != nil {
		t.Fatalf("an adapter failure must not fail the whole run: %v", err)
	}
	if len(got.Languages) != 1 {
		t.Fatalf("want one recorded leg, got %+v", got.Languages)
	}
	if !strings.Contains(got.Languages[0].Error, "refusing argument") {
		t.Errorf("the carrier rewrote a dross-authored refusal: %q", got.Languages[0].Error)
	}
	if got.Languages[0].Error != refusal {
		t.Errorf("LanguageRun.Error = %q, want the refusal verbatim %q", got.Languages[0].Error, refusal)
	}
}
