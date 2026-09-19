package verify

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/mutation"
)

// twoLegTests is one ranged stryker leg beside one whole-file gremlins leg —
// the shape a mixed-language phase records.
func twoLegTests() *Tests {
	return &Tests{
		Phase: "p",
		Scope: scopeWithHunks([]string{"src/a.ts", "src/b.ts", "x.go"},
			map[string][]Range{"src/a.ts": {{Start: 10, End: 12}}}),
		Languages: []LanguageRun{
			{
				Name: "typescript", Tool: "stryker",
				Files:     []string{"src/a.ts", "src/b.ts"},
				Mutation:  &mutation.Report{Tool: "stryker", Killed: 3, Survived: 1},
				Ranges:    map[string][]EffectiveRange{"src/a.ts": {{Start: 1, End: 37, Construct: "FunctionDeclaration tally"}}},
				WholeFile: map[string]string{"src/b.ts": WholeFileAbsentFromHunks},
			},
			{
				Name: "go", Tool: "gremlins",
				Files:     []string{"x.go"},
				Mutation:  &mutation.Report{Tool: "gremlins", Killed: 2},
				WholeFile: map[string]string{"x.go": WholeFileNoRangeRunner},
			},
		},
	}
}

func skeletonBytes(t *testing.T, tests *Tests) (string, *Verify) {
	t.Helper()
	path := filepath.Join(t.TempDir(), VerifyFile)
	if err := Skeleton(tests, []string{"c-1"}).Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	v, err := LoadVerify(path)
	if err != nil {
		t.Fatalf("LoadVerify: %v", err)
	}
	return string(raw), v
}

func TestVerifyTomlStatesEffectiveRanges(t *testing.T) {
	body, v := skeletonBytes(t, twoLegTests())
	for _, want := range []string{
		`ranges = ["src/a.ts:1-37 (FunctionDeclaration tally)"]`,
		`"src/b.ts — file-absent-from-hunks"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("verify.toml lacks %s:\n%s", want, body)
		}
	}
	if strings.Contains(body, "pad =") {
		t.Errorf("verify.toml still states a pad:\n%s", body)
	}
	if len(v.Summary.Legs) != 2 {
		t.Fatalf("want two legs, got %+v", v.Summary.Legs)
	}
	stryker := v.Summary.Legs[0]
	if !reflect.DeepEqual(stryker.Ranges, []string{"src/a.ts:1-37 (FunctionDeclaration tally)"}) {
		t.Errorf("ranges did not round-trip: %v", stryker.Ranges)
	}
	if !reflect.DeepEqual(stryker.WholeFile, []string{"src/b.ts — file-absent-from-hunks"}) {
		t.Errorf("whole_file did not round-trip: %v", stryker.WholeFile)
	}
}

// legBlock cuts the [[summary.leg]] block whose tool line names tool.
func legBlock(t *testing.T, body, tool string) string {
	t.Helper()
	blocks := strings.Split(body, "[[summary.leg]]")
	for _, b := range blocks[1:] {
		if strings.Contains(b, `tool = "`+tool+`"`) {
			return b
		}
	}
	t.Fatalf("no [[summary.leg]] for %s in:\n%s", tool, body)
	return ""
}

func TestGremlinsLegSummaryHasNoRanges(t *testing.T) {
	body, v := skeletonBytes(t, twoLegTests())
	block := legBlock(t, body, "gremlins")
	if strings.Contains(block, "ranges =") {
		t.Errorf("the gremlins leg claims ranges:\n%s", block)
	}
	if !strings.Contains(block, `"x.go — adapter-lacks-range-runner"`) {
		t.Errorf("the gremlins leg does not name its whole-file reason:\n%s", block)
	}
	gremlins := v.Summary.Legs[1]
	if gremlins.Ranges != nil {
		t.Errorf("gremlins leg loaded with ranges=%v", gremlins.Ranges)
	}
	if len(gremlins.WholeFile) != len(twoLegTests().Languages[1].Files) {
		t.Errorf("whole_file names %d files, leg has %d", len(gremlins.WholeFile), len(twoLegTests().Languages[1].Files))
	}
}

// The error leg states its provenance too — a failed ranged leg that forgot
// its ranges would be re-read as a whole-file one.
func TestErrorLegStatesItsRanges(t *testing.T) {
	tests := twoLegTests()
	tests.Languages[0].Mutation = nil
	tests.Languages[0].Error = "stryker exploded"
	body, v := skeletonBytes(t, tests)
	block := legBlock(t, body, "stryker")
	if !strings.Contains(block, `ranges = ["src/a.ts:1-37 (FunctionDeclaration tally)"]`) {
		t.Errorf("the failed stryker leg lost its ranges:\n%s", block)
	}
	if v.Summary.Legs[0].Error == "" || len(v.Summary.Legs[0].Ranges) != 1 {
		t.Errorf("error leg = %+v, want both the error and its range", v.Summary.Legs[0])
	}
}

func TestLegProvenanceIsSorted(t *testing.T) {
	lr := LanguageRun{
		Ranges: map[string][]EffectiveRange{
			"z.ts": {{Start: 1, End: 30, Construct: "ClassDeclaration Z"}},
			"a.ts": {{Start: 5, End: 60, Construct: "FunctionDeclaration a"}, {Start: 100, End: 140, Construct: ConstructHunk}},
		},
		WholeFile: map[string]string{"m.ts": WholeFileMalformedRange, "b.ts": WholeFileAbsentFromHunks},
	}
	ranges, whole := legProvenance(lr)
	if want := []string{"a.ts:100-140 (hunk)", "a.ts:5-60 (FunctionDeclaration a)", "z.ts:1-30 (ClassDeclaration Z)"}; !reflect.DeepEqual(ranges, want) {
		t.Errorf("ranges = %v, want %v", ranges, want)
	}
	if want := []string{"b.ts — file-absent-from-hunks", "m.ts — malformed-range"}; !reflect.DeepEqual(whole, want) {
		t.Errorf("whole = %v, want %v", whole, want)
	}
}
