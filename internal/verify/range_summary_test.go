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
				Ranges:    map[string][]EffectiveRange{"src/a.ts": {{Start: 1, End: 37, Pad: 25}}},
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
		"pad = 25",
		`ranges = ["src/a.ts:1-37"]`,
		`"src/b.ts — file-absent-from-hunks"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("verify.toml lacks %s:\n%s", want, body)
		}
	}
	if len(v.Summary.Legs) != 2 {
		t.Fatalf("want two legs, got %+v", v.Summary.Legs)
	}
	stryker := v.Summary.Legs[0]
	if stryker.Pad != 25 {
		t.Errorf("pad did not round-trip: %d", stryker.Pad)
	}
	if !reflect.DeepEqual(stryker.Ranges, []string{"src/a.ts:1-37"}) {
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

func TestGremlinsLegHasNoPad(t *testing.T) {
	body, v := skeletonBytes(t, twoLegTests())
	block := legBlock(t, body, "gremlins")
	for _, banned := range []string{"pad =", "ranges ="} {
		if strings.Contains(block, banned) {
			t.Errorf("the gremlins leg claims %q:\n%s", banned, block)
		}
	}
	if !strings.Contains(block, `"x.go — adapter-lacks-range-runner"`) {
		t.Errorf("the gremlins leg does not name its whole-file reason:\n%s", block)
	}
	gremlins := v.Summary.Legs[1]
	if gremlins.Pad != 0 || gremlins.Ranges != nil {
		t.Errorf("gremlins leg loaded with pad=%d ranges=%v", gremlins.Pad, gremlins.Ranges)
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
	if !strings.Contains(block, `ranges = ["src/a.ts:1-37"]`) {
		t.Errorf("the failed stryker leg lost its ranges:\n%s", block)
	}
	if v.Summary.Legs[0].Error == "" || v.Summary.Legs[0].Pad != 25 {
		t.Errorf("error leg = %+v, want both the error and pad 25", v.Summary.Legs[0])
	}
}

func TestLegProvenanceIsSorted(t *testing.T) {
	lr := LanguageRun{
		Ranges: map[string][]EffectiveRange{
			"z.ts": {{Start: 1, End: 30, Pad: 25}},
			"a.ts": {{Start: 5, End: 60, Pad: 25}, {Start: 100, End: 140, Pad: 25}},
		},
		WholeFile: map[string]string{"m.ts": WholeFileMalformedRange, "b.ts": WholeFileAbsentFromHunks},
	}
	pad, ranges, whole := legProvenance(lr)
	if pad != 25 {
		t.Errorf("pad = %d", pad)
	}
	if want := []string{"a.ts:100-140", "a.ts:5-60", "z.ts:1-30"}; !reflect.DeepEqual(ranges, want) {
		t.Errorf("ranges = %v, want %v", ranges, want)
	}
	if want := []string{"b.ts — file-absent-from-hunks", "m.ts — malformed-range"}; !reflect.DeepEqual(whole, want) {
		t.Errorf("whole = %v, want %v", whole, want)
	}
}
