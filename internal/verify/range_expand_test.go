package verify

import (
	"math/rand"
	"reflect"
	"testing"

	"github.com/Rivil/dross/internal/mutation"
)

func fn(start, end int, name string) mutation.Construct {
	return mutation.Construct{Start: start, End: end, Kind: "FunctionDeclaration", Name: name}
}

func eff(start, end int, label string) EffectiveRange {
	return EffectiveRange{Start: start, End: end, Construct: label}
}

// The phase's core claim: a changed line maps to its enclosing construct's
// FULL span, not to the hunk and not to a padded window around it.
func TestExpandWidensToTheEnclosingConstruct(t *testing.T) {
	got := expandToConstructs([]Range{{40, 41}}, []mutation.Construct{fn(10, 60, "run")})
	want := []EffectiveRange{eff(10, 60, "FunctionDeclaration run")}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expand = %v, want %v", got, want)
	}
}

func TestExpandSplitsAHunkAcrossConstructs(t *testing.T) {
	got := expandToConstructs([]Range{{18, 23}}, []mutation.Construct{fn(1, 20, "a"), fn(21, 40, "b")})
	want := []EffectiveRange{eff(1, 20, "FunctionDeclaration a"), eff(21, 40, "FunctionDeclaration b")}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expand = %v, want %v (two constructs, in order, never merged)", got, want)
	}
}

func TestExpandDedupesHunksInOneConstruct(t *testing.T) {
	got := expandToConstructs([]Range{{12, 12}, {30, 31}}, []mutation.Construct{fn(10, 60, "run")})
	if len(got) != 1 {
		t.Fatalf("two hunks in one construct produced %d ranges, want 1: %v", len(got), got)
	}
	if got[0] != eff(10, 60, "FunctionDeclaration run") {
		t.Fatalf("range = %v", got[0])
	}
}

func TestExpandKeepsUncoveredLinesAsTheBareHunk(t *testing.T) {
	cs := []mutation.Construct{fn(10, 60, "run")}
	for _, tc := range []struct {
		name string
		hunk Range
	}{
		{"before the first construct", Range{5, 7}},
		{"past the last construct", Range{70, 72}},
	} {
		got := expandToConstructs([]Range{tc.hunk}, cs)
		want := []EffectiveRange{eff(tc.hunk.Start, tc.hunk.End, ConstructHunk)}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s: expand = %v, want %v", tc.name, got, want)
		}
	}
}

// A straddling hunk keeps its uncovered tail as a bare range, and no two
// output ranges ever overlap — pinned by a property check over random
// layouts, since the interesting cases are the ones nobody enumerated.
func TestExpandOutputNeverOverlaps(t *testing.T) {
	got := expandToConstructs([]Range{{18, 26}}, []mutation.Construct{fn(1, 20, "a")})
	want := []EffectiveRange{eff(1, 20, "FunctionDeclaration a"), eff(21, 26, ConstructHunk)}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("straddle: expand = %v, want %v", got, want)
	}

	rng := rand.New(rand.NewSource(1))
	for iter := 0; iter < 200; iter++ {
		var cs []mutation.Construct
		line := 1 + rng.Intn(5)
		for line < 200 {
			end := line + rng.Intn(30)
			cs = append(cs, fn(line, end, "f"))
			line = end + 1 + rng.Intn(6)
		}
		var hunks []Range
		for n := 1 + rng.Intn(6); n > 0; n-- {
			s := 1 + rng.Intn(220)
			hunks = append(hunks, Range{s, s + rng.Intn(15)})
		}
		out := expandToConstructs(hunks, cs)
		for i := 1; i < len(out); i++ {
			if out[i].Start <= out[i-1].End {
				t.Fatalf("iter %d: ranges %v and %v overlap (hunks %v, constructs %v)", iter, out[i-1], out[i], hunks, cs)
			}
		}
		for _, h := range hunks {
			for l := h.Start; l <= h.End; l++ {
				n := 0
				for _, r := range out {
					if l >= r.Start && l <= r.End {
						n++
					}
				}
				if n != 1 {
					t.Fatalf("iter %d: hunk line %d lies in %d output ranges, want exactly 1 (out %v)", iter, l, n, out)
				}
			}
		}
	}
}

func TestExpandWithNoConstructsIsTheRawHunks(t *testing.T) {
	got := expandToConstructs([]Range{{5, 7}, {30, 31}}, nil)
	want := []EffectiveRange{eff(5, 7, ConstructHunk), eff(30, 31, ConstructHunk)}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expand = %v, want the hunks verbatim %v", got, want)
	}
	if expandToConstructs(nil, nil) != nil {
		t.Fatal("no hunks must expand to nil")
	}
}

func TestLabelOmitsAMissingName(t *testing.T) {
	if got := (mutation.Construct{Kind: "ExpressionStatement"}).Label(); got != "ExpressionStatement" {
		t.Fatalf("Label() = %q, want the bare kind with no trailing space", got)
	}
	if got := fn(1, 2, "run").Label(); got != "FunctionDeclaration run" {
		t.Fatalf("Label() = %q", got)
	}
}
