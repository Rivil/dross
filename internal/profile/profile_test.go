package profile

import (
	"os"
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
		"Communication Style":  "communication",
		"UX Philosophy":        "ux",
		"Frustration Triggers": "frustration",
		"Vendor Choices":       "vendor_choices",
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

// TestSeedFromGSDBranches covers all four arms of the seeding path. Three of
// them never ran before, and the middle one matters most: a machine with no GSD
// profile is the COMMON case, and it must be a silent no-op rather than an
// error — `dross init` calls this unconditionally, so turning "no profile
// here" into a failure would break every greenfield bootstrap.
func TestSeedFromGSDBranches(t *testing.T) {
	gsdPath := func(home string) string {
		return filepath.Join(home, ".claude", "get-shit-done", "USER-PROFILE.md")
	}

	t.Run("no GSD profile is a silent no-op", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		dest := filepath.Join(t.TempDir(), "profile.toml")

		if err := SeedFromGSD(dest); err != nil {
			t.Fatalf("a missing GSD profile must not be an error: %v", err)
		}
		if _, err := os.Stat(dest); !os.IsNotExist(err) {
			t.Errorf("nothing to seed from, yet a profile was written (stat err = %v)", err)
		}
	})

	t.Run("an existing GSD profile is parsed and written", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		src := gsdPath(home)
		if err := os.MkdirAll(filepath.Dir(src), 0o755); err != nil {
			t.Fatal(err)
		}
		body := "## Communication\n\n**Rating:** terse-direct | **Confidence:** high\n\n**Directive:** be terse\n"
		if err := os.WriteFile(src, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		dest := filepath.Join(t.TempDir(), "profile.toml")

		if err := SeedFromGSD(dest); err != nil {
			t.Fatalf("SeedFromGSD: %v", err)
		}
		p, err := LoadFile(dest)
		if err != nil {
			t.Fatal(err)
		}
		if p.Source != "seeded_from_gsd" {
			t.Errorf("Source = %q, want the seeding provenance", p.Source)
		}
		if p.Dimensions["communication"].Rating != "terse-direct" {
			t.Errorf("the GSD profile was not parsed into dimensions: %+v", p.Dimensions)
		}
	})

	t.Run("an unreadable GSD profile is an error, not a no-op", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		// A directory where the profile should be: the read fails with
		// something that is NOT fs.ErrNotExist, so it must surface rather than
		// be swallowed by the "no profile here" arm.
		if err := os.MkdirAll(gsdPath(home), 0o755); err != nil {
			t.Fatal(err)
		}
		dest := filepath.Join(t.TempDir(), "profile.toml")

		if err := SeedFromGSD(dest); err == nil {
			t.Error("an unreadable GSD profile was treated as absent")
		}
		if _, err := os.Stat(dest); !os.IsNotExist(err) {
			t.Errorf("a failed seed wrote a profile anyway (stat err = %v)", err)
		}
	})

	t.Run("an unresolvable home is an error", func(t *testing.T) {
		t.Setenv("HOME", "")
		dest := filepath.Join(t.TempDir(), "profile.toml")

		if err := SeedFromGSD(dest); err == nil {
			t.Error("SeedFromGSD returned nil with no resolvable home directory")
		}
	})
}
