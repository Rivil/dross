package stack

import (
	"bytes"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

// allShapes is a profile TOML exercising every locked schema_extensibility shape:
// a package-manager variant, two commands in one runtime slot, an availability-
// gated (optional) tool, and a per-OS tool name.
const allShapes = `
id = "demo"
title = "Demo"

[[package_managers]]
  name = "pnpm"
  bin = "pnpm"
  lockfile = "pnpm-lock.yaml"

[runtime.test]
  [[runtime.test.variants]]
    run = "pnpm test"
    bin = "pnpm"
  [[runtime.test.variants]]
    run = "npm test"
    bin = "npm"

[[tools]]
  name = "semgrep"
  kind = "scanner"
  optional = true

[[tools]]
  name = "ripgrep"
  kind = "analyzer"
  [tools.bin_by_os]
    darwin = "rg"
    linux = "rg-linux"
`

func TestProfileSchemaShapes(t *testing.T) {
	p, err := Decode([]byte(allShapes))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}

	assertShapes := func(t *testing.T, p *Profile) {
		t.Helper()
		// 1. package-manager variant
		if len(p.Packages) != 1 || p.Packages[0].Name != "pnpm" || p.Packages[0].Lockfile != "pnpm-lock.yaml" {
			t.Errorf("package-manager variant lost: %+v", p.Packages)
		}
		// 2. two commands in one runtime slot
		if got := len(p.Runtime.Test.Variants); got != 2 {
			t.Errorf("multi-command slot lost: want 2 variants, got %d", got)
		}
		// 3. availability-gated (optional) tool
		opt := findTool(p, "semgrep")
		if opt == nil || !opt.Optional {
			t.Errorf("availability-gated tool lost: %+v", opt)
		}
		// 4. per-OS tool name
		rg := findTool(p, "ripgrep")
		if rg == nil || rg.BinByOS["darwin"] != "rg" || rg.BinByOS["linux"] != "rg-linux" {
			t.Errorf("per-OS tool name lost: %+v", rg)
		}
	}

	assertShapes(t, p)

	// Round-trip: encode then decode and assert every shape still survives, so a
	// dropped struct tag fails here too.
	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(p); err != nil {
		t.Fatalf("encode: %v", err)
	}
	rt, err := Decode(buf.Bytes())
	if err != nil {
		t.Fatalf("decode round-trip: %v", err)
	}
	assertShapes(t, rt)
}

func TestProfileRejectsEmptyID(t *testing.T) {
	_, err := Decode([]byte(`id = ""` + "\n[runtime.test]\n  run = \"go test ./...\"\n"))
	if err == nil {
		t.Fatal("expected error for empty id, got nil")
	}
	if !strings.Contains(err.Error(), "id") {
		t.Errorf("error should mention id, got: %v", err)
	}
}

func TestProfileToolRequiresBin(t *testing.T) {
	// A tool with no name, no bin, and no per-OS binary cannot be looked up on PATH.
	_, err := Decode([]byte("id = \"demo\"\n[[tools]]\n  kind = \"scanner\"\n  optional = true\n"))
	if err == nil {
		t.Fatal("expected error for tool with no bin, got nil")
	}
	if !strings.Contains(err.Error(), "bin") {
		t.Errorf("error should mention bin, got: %v", err)
	}
}

// dockerFilePatterns is the marker pattern set the docker profile (t-3) will ship.
// Pinning it here keeps the matcher's contract honest independent of the TOML file.
var dockerFilePatterns = []string{
	"Dockerfile", "Dockerfile.*", "*.dockerfile",
	"docker-compose*.yml", "docker-compose*.yaml",
	"compose*.yml", "compose*.yaml",
}

func TestFilePatternMatch(t *testing.T) {
	s := Signals{FilePatterns: dockerFilePatterns}
	cases := []struct {
		name string
		want bool
	}{
		// MUST match — including case variants (capital D) and suffixed/prefixed forms.
		{"Dockerfile", true},
		{"Dockerfile.dev", true},
		{"app.Dockerfile", true},
		{"app.dockerfile", true},
		{"docker-compose-prod.yaml", true},
		{"compose.override.yml", true},
		{"compose.yaml", true},
		// MUST NOT match — guards against an over-broad (substring) glob.
		{"notes.txt", false},
		{"README.md", false},
		{"mydockerfile.go", false},
		{"compose.go", false},
	}
	for _, c := range cases {
		if got := s.MatchesFile(c.name); got != c.want {
			t.Errorf("MatchesFile(%q) = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestFilePatternNoBraceExpansion(t *testing.T) {
	// .yml and .yaml are distinct patterns — there is no brace expansion. A
	// *.yml-only pattern must NOT match a .yaml file; collapsing them breaks this.
	ymlOnly := Signals{FilePatterns: []string{"compose*.yml"}}
	if ymlOnly.MatchesFile("compose.yaml") {
		t.Error(`"compose*.yml" must not match "compose.yaml" — .yml/.yaml are separate patterns`)
	}
	if !ymlOnly.MatchesFile("compose.yml") {
		t.Error(`"compose*.yml" should match "compose.yml"`)
	}
}

func TestFilePatternsDecodeRoundTrip(t *testing.T) {
	const src = `id = "demo"
[signals]
  file_patterns = ["Dockerfile", "compose*.yaml"]
`
	p, err := Decode([]byte(src))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	want := []string{"Dockerfile", "compose*.yaml"}
	if got := p.Signals.FilePatterns; !equalStrs(got, want) {
		t.Fatalf("file_patterns decode: got %v, want %v (wrong toml tag?)", got, want)
	}
	// Round-trip: encode then decode and assert the patterns survive.
	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(p); err != nil {
		t.Fatalf("encode: %v", err)
	}
	rt, err := Decode(buf.Bytes())
	if err != nil {
		t.Fatalf("decode round-trip: %v", err)
	}
	if got := rt.Signals.FilePatterns; !equalStrs(got, want) {
		t.Fatalf("file_patterns lost on round-trip: got %v, want %v", got, want)
	}
}

func equalStrs(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func findTool(p *Profile, name string) *Tool {
	for i := range p.Tools {
		if p.Tools[i].Name == name {
			return &p.Tools[i]
		}
	}
	return nil
}
