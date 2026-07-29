package cmd

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// fixedLookup resolves from a map and errors on anything else, so the renderer
// can be exercised without a config surface behind it.
func fixedLookup(m map[string]any) dotLookup {
	return func(path string) (any, error) {
		v, ok := m[path]
		if !ok {
			return nil, errors.New("unknown field: " + path)
		}
		return v, nil
	}
}

func TestRenderMultiGetSinglePathIsBare(t *testing.T) {
	lookup := fixedLookup(map[string]any{"current_phase": "cli-surface-sweep"})
	out := captureStdout(t, func() {
		if err := renderMultiGet([]string{"current_phase"}, lookup); err != nil {
			t.Errorf("render: %v", err)
		}
	})
	// Byte-identical, asserted against a literal — multi_get_shape locks
	// this so every existing prompt keeps working.
	if out != "cli-surface-sweep\n" {
		t.Errorf("single-path output = %q, want %q", out, "cli-surface-sweep\n")
	}
}

func TestRenderMultiGetSingleListIsOnePerLine(t *testing.T) {
	lookup := fixedLookup(map[string]any{"scope.non_goals": []string{"a", "b"}})
	out := captureStdout(t, func() {
		if err := renderMultiGet([]string{"scope.non_goals"}, lookup); err != nil {
			t.Errorf("render: %v", err)
		}
	})
	if out != "a\nb\n" {
		t.Errorf("single list output = %q, want %q", out, "a\nb\n")
	}
}

func TestRenderMultiGetKeyedObjectInArgumentOrder(t *testing.T) {
	lookup := fixedLookup(map[string]any{
		"zzz": "last-alphabetically",
		"aaa": "first-alphabetically",
		"mid": []string{"x", "y"},
	})
	out := captureStdout(t, func() {
		if err := renderMultiGet([]string{"zzz", "mid", "aaa"}, lookup); err != nil {
			t.Errorf("render: %v", err)
		}
	})
	var got map[string]json.RawMessage
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("multi-path output is not one JSON object: %v\ngot: %q", err, out)
	}
	if len(got) != 3 {
		t.Errorf("want 3 keys, got %d: %v", len(got), got)
	}
	// Argument order, not map/sorted order — a script must not depend on
	// map iteration.
	if i, j := strings.Index(out, `"zzz"`), strings.Index(out, `"aaa"`); i > j {
		t.Errorf("keys are not in argument order:\n%s", out)
	}
	// A list serialises as a JSON array, not CSV or newline-joined.
	if string(got["mid"]) != `["x","y"]` {
		t.Errorf("list key = %s, want [\"x\",\"y\"]", got["mid"])
	}
}

func TestRenderMultiGetUnknownSecondPathWritesNothing(t *testing.T) {
	lookup := fixedLookup(map[string]any{"good": "v"})
	var err error
	out := captureStdout(t, func() {
		err = renderMultiGet([]string{"good", "nope"}, lookup)
	})
	if err == nil {
		t.Fatal("want an error when the second path is unknown")
	}
	if !strings.Contains(err.Error(), "nope") {
		t.Errorf("error = %q, want it to name nope", err)
	}
	if out != "" {
		t.Errorf("partial output written before the failure: %q", out)
	}
}

func TestRenderMultiGetDedupsRepeatedPath(t *testing.T) {
	lookup := fixedLookup(map[string]any{"a": "1", "b": "2"})
	out := captureStdout(t, func() {
		if err := renderMultiGet([]string{"a", "b", "a"}, lookup); err != nil {
			t.Errorf("render: %v", err)
		}
	})
	if n := strings.Count(out, `"a":`); n != 1 {
		t.Errorf("path a appears %d times, want 1:\n%s", n, out)
	}
	var got map[string]string
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("not valid JSON: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("want 2 keys, got %v", got)
	}
}

func TestRenderMultiGetEmptyListIsArrayNotNull(t *testing.T) {
	lookup := fixedLookup(map[string]any{"empty": []string(nil), "other": "x"})
	out := captureStdout(t, func() {
		if err := renderMultiGet([]string{"empty", "other"}, lookup); err != nil {
			t.Errorf("render: %v", err)
		}
	})
	if !strings.Contains(out, `"empty":[]`) {
		t.Errorf("nil list did not serialise as []:\n%s", out)
	}
}
