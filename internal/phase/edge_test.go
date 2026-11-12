package phase

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// edgeSpec is a spec with criteria c-1..c-3 and the given edge records.
func edgeSpec(edges ...Edge) *Spec {
	return &Spec{
		Phase: SpecPhase{ID: "edgy", Title: "Edgy"},
		Criteria: []Criterion{
			{ID: "c-1", Text: "one"},
			{ID: "c-2", Text: "two"},
			{ID: "c-3", Text: "three"},
		},
		Edges: edges,
	}
}

var allClassNames = []string{"boundary", "empty", "encoding", "ordering", "precision", "idempotency", "concurrency", "adjacency"}

// TestEdgeProblemsTable pins every rule EdgeProblems enforces, in both
// directions: each malformed record yields a problem naming its edges[i] and
// what is wrong, and each well-formed one yields nothing.
func TestEdgeProblemsTable(t *testing.T) {
	known := map[string]bool{"abc": true, "": true}
	cases := []struct {
		name  string
		edges []Edge
		known map[string]bool
		// want lists substrings the single problem line must hold; nil means
		// the spec must come back clean.
		want []string
		// not lists substrings the problem line must NOT hold.
		not []string
	}{
		{"unknown class", []Edge{{Class: "nullness", Outcome: "dismissed", Reason: "n/a"}}, known,
			append([]string{"edges[0] (nullness)", `unknown class "nullness"`}, allClassNames...), nil},
		{"class is case-sensitive", []Edge{{Class: "Boundary", Outcome: "dismissed", Reason: "n/a"}}, known,
			append([]string{"edges[0] (Boundary)", `unknown class "Boundary"`}, allClassNames...), nil},
		{"class is not trimmed", []Edge{{Class: " boundary", Outcome: "dismissed", Reason: "n/a"}}, known,
			append([]string{"edges[0] ( boundary)", `unknown class " boundary"`}, allClassNames...), nil},
		{"empty class", []Edge{{Class: "", Outcome: "dismissed", Reason: "n/a"}}, known,
			append([]string{"edges[0] ()", `unknown class ""`}, allClassNames...), nil},

		{"outcome typo", []Edge{{Class: "boundary", Outcome: "maybe", Reason: "x"}}, known,
			[]string{"edges[0] (boundary)", `unknown outcome "maybe"`, "covered", "dismissed", "deferred"}, nil},
		{"outcome near-miss", []Edge{{Class: "boundary", Outcome: "dismiss", Reason: "x"}}, known,
			[]string{"edges[0] (boundary)", `unknown outcome "dismiss"`}, nil},
		{"empty outcome", []Edge{{Class: "boundary", Outcome: "", Reason: "x"}}, known,
			[]string{"edges[0] (boundary)", `unknown outcome ""`}, nil},

		{"covered with no criteria", []Edge{{Class: "boundary", Outcome: "covered", Criteria: []string{}}}, known,
			[]string{"edges[0] (boundary)", "covered names no criterion"}, nil},
		{"covered naming a missing criterion", []Edge{{Class: "boundary", Outcome: "covered", Criteria: []string{"c-1", "c-9"}}}, known,
			[]string{"edges[0] (boundary)", `"c-9"`}, []string{"c-1"}},
		{"covered naming an empty criterion id", []Edge{{Class: "boundary", Outcome: "covered", Criteria: []string{""}}}, known,
			[]string{"edges[0] (boundary)", `covered names ""`}, nil},

		{"dismissed with no reason", []Edge{{Class: "empty", Outcome: "dismissed", Reason: ""}}, known,
			[]string{"edges[0] (empty)", "dismissed with no reason"}, nil},
		{"dismissed with a blank reason", []Edge{{Class: "empty", Outcome: "dismissed", Reason: "   "}}, known,
			[]string{"edges[0] (empty)", "dismissed with no reason"}, nil},

		{"deferred to an unknown id", []Edge{{Class: "ordering", Outcome: "deferred", Deferred: "abc"}}, map[string]bool{"zzz": true},
			[]string{"edges[0] (ordering)", `"abc"`}, nil},
		{"deferred to a known id", []Edge{{Class: "ordering", Outcome: "deferred", Deferred: "abc"}}, map[string]bool{"abc": true},
			nil, nil},
		{"deferred to the empty id", []Edge{{Class: "ordering", Outcome: "deferred", Deferred: ""}}, map[string]bool{"": true},
			[]string{"edges[0] (ordering)", "deferred names no [[deferred]] id"}, nil},
		{"deferred with a nil known set", []Edge{{Class: "ordering", Outcome: "deferred", Deferred: "abc"}}, nil,
			[]string{"edges[0] (ordering)", `"abc"`}, nil},

		{"duplicate class", []Edge{
			{Class: "empty", Outcome: "dismissed", Reason: "a"},
			{Class: "empty", Outcome: "dismissed", Reason: "b"},
		}, known, []string{"edges[1] (empty)", "duplicate", "edges[0]"}, nil},

		{"one well-formed record per outcome", []Edge{
			{Class: "boundary", Outcome: "covered", Criteria: []string{"c-1", "c-3"}},
			{Class: "empty", Outcome: "dismissed", Reason: "no empty input reaches this"},
			{Class: "encoding", Outcome: "deferred", Deferred: "abc"},
		}, known, nil, nil},
		{"dismissed carrying criteria", []Edge{
			{Class: "precision", Outcome: "dismissed", Reason: "integers only", Criteria: []string{"c-9"}},
		}, known, nil, nil},
		{"no records", nil, known, nil, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := EdgeProblems(edgeSpec(tc.edges...), tc.known)
			if tc.want == nil {
				if got != nil {
					t.Fatalf("want nil, got %q", got)
				}
				return
			}
			if len(got) != 1 {
				t.Fatalf("want exactly one problem line, got %d: %q", len(got), got)
			}
			for _, w := range tc.want {
				if !strings.Contains(got[0], w) {
					t.Errorf("problem %q does not contain %q", got[0], w)
				}
			}
			for _, n := range tc.not {
				if strings.Contains(got[0], n) {
					t.Errorf("problem %q names %q, which is not a defect", got[0], n)
				}
			}
		})
	}
}

// TestEdgeProblemsOneLinePerRecord: a record with two defects is one line
// naming both, so callers can count lines as unresolved records.
func TestEdgeProblemsOneLinePerRecord(t *testing.T) {
	got := EdgeProblems(edgeSpec(Edge{Class: "latency", Outcome: "dismissed"}), nil)
	if len(got) != 1 {
		t.Fatalf("want one line, got %q", got)
	}
	for _, w := range []string{"unknown class", "dismissed with no reason"} {
		if !strings.Contains(got[0], w) {
			t.Errorf("line %q is missing defect %q", got[0], w)
		}
	}
}

// TestEdgeProblemsRecordOrder: problems come out in record order, every call —
// an order resting on map iteration would shuffle across repeated calls.
func TestEdgeProblemsRecordOrder(t *testing.T) {
	spec := edgeSpec(
		Edge{Class: "precision", Outcome: "dismissed"},
		Edge{Class: "boundary", Outcome: "covered", Criteria: []string{"c-1"}},
		Edge{Class: "adjacency", Outcome: "nope"},
		Edge{Class: "concurrency", Outcome: "deferred", Deferred: "gone"},
	)
	want := []string{"edges[0] (precision)", "edges[2] (adjacency)", "edges[3] (concurrency)"}
	for call := 0; call < 20; call++ {
		got := EdgeProblems(spec, nil)
		if len(got) != len(want) {
			t.Fatalf("call %d: want %d problems, got %q", call, len(want), got)
		}
		for i, w := range want {
			if !strings.HasPrefix(got[i], w+":") {
				t.Fatalf("call %d: problem %d is %q, want it to start with %q", call, i, got[i], w)
			}
		}
	}
}

// TestEdgeClassesExact pins the class set, its order and its hints: the spec
// prompt proposes from this list, so a drift here changes what gets probed.
func TestEdgeClassesExact(t *testing.T) {
	var names []string
	for _, c := range EdgeClasses {
		names = append(names, c.Name)
		if strings.TrimSpace(c.Hint) == "" {
			t.Errorf("class %q has no hint", c.Name)
		}
	}
	if !reflect.DeepEqual(names, allClassNames) {
		t.Errorf("EdgeClasses = %q, want %q", names, allClassNames)
	}
	if want := []string{"covered", "dismissed", "deferred"}; !reflect.DeepEqual(EdgeOutcomes, want) {
		t.Errorf("EdgeOutcomes = %q, want %q", EdgeOutcomes, want)
	}
}

// TestEdgeStatusPartitions: a class is resolved, unresolved or open by its
// records, each list in EdgeClasses order; a reasonless dismissal is not
// resolved, and a spec with no records leaves every class open.
func TestEdgeStatusPartitions(t *testing.T) {
	spec := edgeSpec(
		Edge{Class: "encoding", Outcome: "dismissed", Reason: ""},
		Edge{Class: "empty", Outcome: "dismissed", Reason: "never empty"},
		Edge{Class: "boundary", Outcome: "covered", Criteria: []string{"c-2"}},
		Edge{Class: "latency", Outcome: "dismissed", Reason: "unknown class"},
	)
	resolved, unresolved, open := EdgeStatus(spec, nil)
	if want := []string{"boundary", "empty"}; !reflect.DeepEqual(resolved, want) {
		t.Errorf("resolved = %q, want %q", resolved, want)
	}
	if want := []string{"encoding"}; !reflect.DeepEqual(unresolved, want) {
		t.Errorf("unresolved = %q, want %q", unresolved, want)
	}
	if want := []string{"ordering", "precision", "idempotency", "concurrency", "adjacency"}; !reflect.DeepEqual(open, want) {
		t.Errorf("open = %q, want %q", open, want)
	}

	resolved, unresolved, open = EdgeStatus(edgeSpec(), nil)
	if resolved == nil || unresolved == nil || len(resolved) != 0 || len(unresolved) != 0 {
		t.Errorf("legacy spec: resolved = %#v, unresolved = %#v, want both empty and non-nil", resolved, unresolved)
	}
	if !reflect.DeepEqual(open, allClassNames) {
		t.Errorf("legacy spec: open = %q, want all eight %q", open, allClassNames)
	}
}

// TestEdgeStatusDuplicateIsUnresolved: a class recorded twice is unresolved,
// matching the duplicate line EdgeProblems reports for it.
func TestEdgeStatusDuplicateIsUnresolved(t *testing.T) {
	spec := edgeSpec(
		Edge{Class: "empty", Outcome: "dismissed", Reason: "a"},
		Edge{Class: "empty", Outcome: "dismissed", Reason: "b"},
	)
	resolved, unresolved, _ := EdgeStatus(spec, nil)
	if len(resolved) != 0 || !reflect.DeepEqual(unresolved, []string{"empty"}) {
		t.Errorf("resolved = %q, unresolved = %q, want empty unresolved", resolved, unresolved)
	}
}

// TestEdgeRoundTrip: every Edge field survives Save then LoadSpec.
func TestEdgeRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "spec.toml")
	original := edgeSpec(
		Edge{Class: "boundary", Outcome: "covered", Criteria: []string{"c-1", "c-2"}},
		Edge{Class: "empty", Outcome: "dismissed", Reason: "input is never empty", Criteria: []string{"c-3"}},
		Edge{Class: "ordering", Outcome: "deferred", Deferred: "0123456789abcdef"},
	)
	original.Deferred = []Deferred{{ID: "0123456789abcdef", Text: "ordering later"}}
	if err := original.Save(path); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadSpec(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(original, loaded) {
		t.Errorf("edge round-trip drift:\norig: %+v\nload: %+v", original.Edges, loaded.Edges)
	}
}

// TestEdgesOmitEmpty: a spec with no edge records writes no `edges` key —
// neither for a nil list nor a non-nil empty one — and a legacy spec re-saved
// through LoadSpec stays byte-identical.
func TestEdgesOmitEmpty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "spec.toml")

	spec := edgeSpec()
	spec.Edges = []Edge{}
	if err := spec.Save(path); err != nil {
		t.Fatal(err)
	}
	if raw := readFile(t, path); strings.Contains(raw, "edges") {
		t.Errorf("an empty Edges list wrote an edges key:\n%s", raw)
	}

	legacy := filepath.Join(dir, "legacy.toml")
	if err := edgeSpec().Save(legacy); err != nil {
		t.Fatal(err)
	}
	first := readFile(t, legacy)
	reloaded, err := LoadSpec(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if err := reloaded.Save(legacy); err != nil {
		t.Fatal(err)
	}
	if second := readFile(t, legacy); second != first {
		t.Errorf("legacy spec changed on re-save:\n--- first\n%s\n--- second\n%s", first, second)
	}
	if strings.Contains(first, "edges") {
		t.Errorf("legacy spec save wrote an edges key:\n%s", first)
	}

	// Guard against the checks above passing vacuously: a spec WITH a record
	// must write the table.
	if err := edgeSpec(Edge{Class: "empty", Outcome: "dismissed", Reason: "x"}).Save(path); err != nil {
		t.Fatal(err)
	}
	if raw := readFile(t, path); !strings.Contains(raw, "[[edges]]") {
		t.Errorf("a spec with a record wrote no [[edges]] table:\n%s", raw)
	}
}

// TestEdgeCorpusIsClean: every spec this repo carries — all written before
// [[edges]] existed, or since with resolved records — passes the rules (c-5).
func TestEdgeCorpusIsClean(t *testing.T) {
	specs, err := filepath.Glob(filepath.Join("..", "..", ".dross", "phases", "*", "spec.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(specs) == 0 {
		t.Fatal("the glob matched no spec.toml — it is not reaching .dross/phases")
	}
	known := map[string]bool{}
	loaded := map[string]*Spec{}
	for _, path := range specs {
		spec, err := LoadSpec(path)
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		loaded[path] = spec
		for _, d := range spec.Deferred {
			known[d.ID] = true
		}
	}
	for path, spec := range loaded {
		if got := EdgeProblems(spec, known); got != nil {
			t.Errorf("%s: %q", path, got)
		}
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
