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

// TestStubMinimalShapeLoads pins the stub_profile_shape decision: a detection-only
// stub carrying only id + title + [signals].exts (no [runtime], [[tools]] or
// [loadout]) must Decode and Validate clean. A Validate regression that began
// demanding a runtime block or an analyzer would reject a bare stub and fail here.
func TestStubMinimalShapeLoads(t *testing.T) {
	const stub = `id = "ruby"
title = "Ruby"
[signals]
  exts = [".rb"]
`
	p, err := Decode([]byte(stub))
	if err != nil {
		t.Fatalf("bare stub must Decode+Validate clean, got: %v", err)
	}
	if p.ID != "ruby" || len(p.Signals.Exts) != 1 || p.Signals.Exts[0] != ".rb" {
		t.Fatalf("stub fields lost: %+v", p)
	}
	// The optional blocks must genuinely be absent — a stub is not silently filled in.
	if len(p.Tools) != 0 || len(p.Packages) != 0 {
		t.Errorf("bare stub gained tools/packages it never declared: tools=%v packages=%v", p.Tools, p.Packages)
	}
}

// TestEmbeddedProfilesAllLoad is the blast-radius guard: Embedded() decodes AND
// validates every shipped <id>.toml, so a single malformed profile (missing id, a
// tools entry with no bin, broken TOML) errors the whole set rather than being
// silently skipped. Shipping a bad stub fails the entire suite here.
func TestEmbeddedProfilesAllLoad(t *testing.T) {
	profiles, err := Embedded()
	if err != nil {
		t.Fatalf("a malformed shipped profile breaks the whole embedded set: %v", err)
	}
	// Every loaded profile must satisfy the same invariants Decode enforces.
	for _, p := range profiles {
		if err := p.Validate(); err != nil {
			t.Errorf("embedded profile %q fails Validate: %v", p.ID, err)
		}
	}
	// The 7 detection-only stubs shipped this phase must all be present.
	ids := map[string]bool{}
	for _, p := range profiles {
		ids[p.ID] = true
	}
	for _, want := range []string{"ruby", "rust", "java", "c", "cpp", "php", "swift"} {
		if !ids[want] {
			t.Errorf("embedded set missing stub profile %q", want)
		}
	}
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

// TestTerraformFilePatternMatch drives the match cases off the SHIPPED terraform
// profile's Signals (not a hardcoded list), so a dropped/renamed pattern in
// terraform.toml changes what matches here. One row per locked pattern.
func TestTerraformFilePatternMatch(t *testing.T) {
	emb, err := Embedded()
	if err != nil {
		t.Fatalf("Embedded: %v", err)
	}
	tf := ByID(emb, "terraform")
	if tf == nil {
		t.Fatal("embedded profiles must include the terraform profile")
	}
	s := tf.Signals
	cases := []struct {
		name string
		want bool
	}{
		// MUST match — one row per locked pattern, plus a case-insensitive guard.
		{"main.tf", true},             // *.tf
		{"vars.tf.json", true},        // *.tf.json
		{"prod.tfvars", true},         // *.tfvars
		{"secrets.tfvars.json", true}, // *.tfvars.json
		{"packer.hcl", true},          // *.hcl
		{"Main.TF", true},             // case-insensitive
		// MUST NOT match — false-positive guards.
		{"main.go", false},
		{"README.md", false},
		{"notfile.tfx", false},   // *.tf must not match a longer ext
		{"main.tfstate", false},  // *.tf must not match *.tfstate
		{"module.tf.bak", false}, // a backup file is not a TF source
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

// TestContentMatchAllSemantics pins All=AND / Any=OR. If All were treated as OR, a
// candidate carrying only one of two required tokens would wrongly match.
func TestContentMatchAllSemantics(t *testing.T) {
	all := ContentMatch{All: []string{"apiVersion", "kind"}}
	if !all.Matches([]byte("apiVersion: apps/v1\nkind: Deployment\n")) {
		t.Error("All match: both tokens present must match")
	}
	if all.Matches([]byte("apiVersion: apps/v1\n")) {
		t.Error("All is AND, not OR — only one of two required tokens must NOT match")
	}

	any := ContentMatch{Any: []string{"AWSTemplateFormatVersion", "Resources"}}
	if !any.Matches([]byte("Resources:\n  Bucket:\n")) {
		t.Error("Any match: one token present must match (OR)")
	}
	if any.Matches([]byte("nothing relevant here\n")) {
		t.Error("Any match: no token present must NOT match")
	}
}

// TestContentMatchCaseSensitive pins case-sensitive matching: a lowercase
// `resources:` (common in unrelated YAML) must not satisfy a CloudFormation
// `Resources` token, or every YAML repo would false-positive.
func TestContentMatchCaseSensitive(t *testing.T) {
	c := ContentMatch{Any: []string{"Resources"}}
	if c.Matches([]byte("resources:\n  - x\n")) {
		t.Error("content match must be case-sensitive — lowercase `resources:` must not satisfy `Resources`")
	}
	if !c.Matches([]byte("Resources:\n")) {
		t.Error("exact-case `Resources` must match")
	}
}

// TestContentMatchDecodeRoundTrip pins the [signals.content] toml tag: a profile's
// content gate must survive decode and an encode/decode round-trip (a dropped tag
// would silently disable content confirmation).
func TestContentMatchDecodeRoundTrip(t *testing.T) {
	const src = `id = "k8s-like"
[signals]
  file_patterns = ["*.yaml", "*.json"]
  [signals.content]
    all = ["apiVersion", "kind"]
`
	p, err := Decode([]byte(src))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	want := []string{"apiVersion", "kind"}
	if got := p.Signals.Content.All; !equalStrs(got, want) {
		t.Fatalf("content.all decode: got %v, want %v (wrong toml tag?)", got, want)
	}
	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(p); err != nil {
		t.Fatalf("encode: %v", err)
	}
	rt, err := Decode(buf.Bytes())
	if err != nil {
		t.Fatalf("decode round-trip: %v", err)
	}
	if got := rt.Signals.Content.All; !equalStrs(got, want) {
		t.Fatalf("content.all lost on round-trip: got %v, want %v", got, want)
	}
}
