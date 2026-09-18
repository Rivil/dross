package verify

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/mutation"
)

// compactJSON strips MarshalIndent's whitespace so a substring assertion
// reads the record's bytes, not its pretty-printing.
func compactJSON(t *testing.T, raw []byte) string {
	t.Helper()
	var buf bytes.Buffer
	if err := json.Compact(&buf, raw); err != nil {
		t.Fatalf("compact: %v", err)
	}
	return buf.String()
}

func saveAndRead(t *testing.T, tests *Tests) ([]byte, *Tests) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "tests.json")
	if err := tests.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadTests(path)
	if err != nil {
		t.Fatalf("LoadTests: %v", err)
	}
	return raw, loaded
}

// Raw and effective side by side: the hunk under scope, the padded range
// under the leg. That adjacency is what makes the claimed scope provable.
func TestLegRecordsEffectiveRangesWithPad(t *testing.T) {
	a := &rangingAdapter{name: "stryker"}
	scope := scopeWithHunks([]string{"src/a.ts"}, map[string][]Range{"src/a.ts": {{Start: 10, End: 12}}})
	tests, err := RunScoped("p", []string{"src/a.ts"}, []mutation.Adapter{a}, scope)
	if err != nil {
		t.Fatalf("RunScoped: %v", err)
	}
	raw, loaded := saveAndRead(t, tests)
	body := compactJSON(t, raw)
	for _, want := range []string{
		`"ranges":{"src/a.ts":[{"start":1,"end":37,"pad":25}]}`,
		`"hunks":{"src/a.ts":[{"start":10,"end":12}]}`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("tests.json lacks %s:\n%s", want, body)
		}
	}
	if !reflect.DeepEqual(loaded.Languages[0].Ranges, tests.Languages[0].Ranges) {
		t.Errorf("Ranges did not round-trip: %v vs %v", loaded.Languages[0].Ranges, tests.Languages[0].Ranges)
	}
	if !reflect.DeepEqual(loaded.Languages[0].WholeFile, tests.Languages[0].WholeFile) {
		t.Errorf("WholeFile did not round-trip: %v vs %v", loaded.Languages[0].WholeFile, tests.Languages[0].WholeFile)
	}
}

// failingRanger takes the ranged arm and then fails — the shape of a stryker
// that was told its ranges and died.
type failingRanger struct {
	rangingAdapter
}

func (f *failingRanger) RunRanges(files []string, ranges map[string][]mutation.Range) (*mutation.Report, error) {
	f.rangingAdapter.RunRanges(files, ranges)
	return nil, errors.New("stryker exploded")
}

func TestFailedLegKeepsItsProvenance(t *testing.T) {
	a := &failingRanger{rangingAdapter{name: "stryker"}}
	files := []string{"src/a.ts", "src/b.ts"}
	scope := scopeWithHunks(files, map[string][]Range{"src/a.ts": {{Start: 10, End: 12}}})
	tests, err := RunScoped("p", files, []mutation.Adapter{a}, scope)
	if err != nil {
		t.Fatalf("RunScoped must record-and-continue, got %v", err)
	}
	lr := tests.Languages[0]
	if lr.Error == "" {
		t.Fatal("the leg's error was not recorded")
	}
	if len(lr.Ranges["src/a.ts"]) != 1 {
		t.Errorf("a failed ranged leg lost its ranges: %v", lr.Ranges)
	}
	if lr.WholeFile["src/b.ts"] != WholeFileAbsentFromHunks {
		t.Errorf("a failed leg lost its whole_file reasons: %v", lr.WholeFile)
	}
}

func TestNilScopeRecordsNoProvenanceAndDoesNotPanic(t *testing.T) {
	adapters := []mutation.Adapter{&rangingAdapter{name: "stryker"}}
	tests, err := Run("p", []string{"src/a.ts"}, adapters)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	for _, lr := range tests.Languages {
		if lr.Ranges != nil || lr.WholeFile != nil {
			t.Errorf("an unscoped run recorded provenance: ranges=%v whole_file=%v", lr.Ranges, lr.WholeFile)
		}
	}
	raw, _ := saveAndRead(t, tests)
	body := compactJSON(t, raw)
	for _, key := range []string{`"ranges"`, `"whole_file"`} {
		if strings.Contains(body, key) {
			t.Errorf("unscoped tests.json carries %s:\n%s", key, body)
		}
	}
}

// extRanger is a RangeRunner that claims only one extension, so two of them
// can share a run without the first swallowing every file.
type extRanger struct {
	rangingAdapter
	ext string
}

func (e *extRanger) Supports(file string) bool { return strings.HasSuffix(file, e.ext) }

func TestDegradedLinesAreDeduped(t *testing.T) {
	ts := &extRanger{rangingAdapter{name: "stryker"}, ".ts"}
	cs := &extRanger{rangingAdapter{name: "stryker-net"}, ".cs"}
	files := []string{"a.ts", "b.cs"}
	scope := scopeWithHunks(files, nil) // hunk-less: each capable adapter degrades once

	if _, err := RunScoped("p", files, []mutation.Adapter{ts, cs}, scope); err != nil {
		t.Fatalf("RunScoped: %v", err)
	}
	// And again over the same scope: the lines are already there.
	if _, err := RunScoped("p", files, []mutation.Adapter{ts, cs}, scope); err != nil {
		t.Fatalf("RunScoped: %v", err)
	}
	seen := map[string]int{}
	for _, l := range scope.Degraded {
		seen[l]++
	}
	for l, n := range seen {
		if n != 1 {
			t.Errorf("degraded line repeated %d times: %s", n, l)
		}
	}
	var named int
	for _, l := range scope.Degraded {
		if strings.HasPrefix(l, "stryker:") || strings.HasPrefix(l, "stryker-net:") {
			named++
		}
	}
	if named != 2 {
		t.Errorf("want one degraded line per adapter, got %v", scope.Degraded)
	}
}

// A gremlins leg ranged nothing, and its record must say so by ABSENCE of the
// key — an empty `ranges` object would read as "ranged, zero files".
func TestGremlinsLegHasNoRanges(t *testing.T) {
	a := &plainAdapter{name: "gremlins"}
	files := []string{"a.go"}
	scope := scopeWithHunks(files, map[string][]Range{"a.go": {{Start: 3, End: 4}}})
	tests, err := RunScoped("p", files, []mutation.Adapter{a}, scope)
	if err != nil {
		t.Fatalf("RunScoped: %v", err)
	}
	raw, _ := saveAndRead(t, tests)
	body := compactJSON(t, raw)
	if strings.Contains(body, `"ranges"`) {
		t.Errorf("a gremlins leg emitted a ranges key:\n%s", body)
	}
	if !strings.Contains(body, `"whole_file":{"a.go":"adapter-lacks-range-runner"}`) {
		t.Errorf("the gremlins leg does not name its whole-file reason:\n%s", body)
	}
}
