package profile

import (
	"path/filepath"
	"testing"
)

const sampleGSD = `# Developer Profile

## Communication

**Rating:** terse-direct | **Confidence:** HIGH

**Directive:** Keep responses action-oriented and concise.

## Decisions

**Rating:** fast-intuitive | **Confidence:** HIGH

**Directive:** Present options as comparison tables.
`

func TestParseGSDProfile(t *testing.T) {
	p := parseGSDProfile(sampleGSD)
	if len(p.Dimensions) != 2 {
		t.Fatalf("expected 2 dimensions, got %d: %+v", len(p.Dimensions), p.Dimensions)
	}
	comm, ok := p.Dimensions["communication"]
	if !ok {
		t.Fatalf("communication dimension missing: keys=%v", keysOf(p.Dimensions))
	}
	if comm.Rating != "terse-direct" {
		t.Errorf("rating: got %q want terse-direct", comm.Rating)
	}
	if comm.Confidence != High {
		t.Errorf("confidence: got %q want high", comm.Confidence)
	}
	if comm.Directive == "" {
		t.Error("directive should not be empty")
	}
}

func TestNormaliseDim(t *testing.T) {
	cases := map[string]string{
		"Communication Style":   "communication",
		"UX Philosophy":         "ux",
		"Frustration Triggers":  "frustration",
		"Vendor Choices":        "vendor_choices",
	}
	for in, want := range cases {
		if got := normaliseDim(in); got != want {
			t.Errorf("normaliseDim(%q): got %q want %q", in, got, want)
		}
	}
}

func TestRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "profile.toml")

	original := &Profile{
		Source: "test",
		Dimensions: map[string]Dimension{
			"communication": {Rating: "terse-direct", Confidence: High, Directive: "be brief"},
			"decisions":     {Rating: "fast-intuitive", Confidence: Medium, Directive: "tables"},
		},
		UserOverrides: map[string]string{"learning_mode": "off"},
	}
	if err := original.SaveFile(path); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Dimensions) != 2 {
		t.Fatalf("dimension count: %d", len(loaded.Dimensions))
	}
	if loaded.Dimensions["communication"].Rating != "terse-direct" {
		t.Error("communication rating drifted")
	}
}

func TestLoadMissingReturnsEmpty(t *testing.T) {
	p, err := LoadFile(filepath.Join(t.TempDir(), "nope.toml"))
	if err != nil {
		t.Fatalf("missing file should be ok: %v", err)
	}
	if p.Dimensions == nil {
		t.Error("Dimensions should be initialised even for missing file")
	}
}

func TestMergeProjectOverridesGlobal(t *testing.T) {
	g := &Profile{Dimensions: map[string]Dimension{
		"communication": {Rating: "verbose", Confidence: High, Directive: "be thorough"},
		"decisions":     {Rating: "deliberate", Confidence: High, Directive: "ponder"},
	}}
	p := &Profile{Dimensions: map[string]Dimension{
		"communication": {Rating: "terse-direct", Confidence: High, Directive: "override"},
	}}
	out := Merge(g, p)
	if out.Dimensions["communication"].Rating != "terse-direct" {
		t.Error("project should override global on overlap")
	}
	if out.Dimensions["decisions"].Rating != "deliberate" {
		t.Error("non-overridden global dim should remain")
	}
}

func keysOf(m map[string]Dimension) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
