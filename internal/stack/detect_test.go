package stack

import (
	"os"
	"path/filepath"
	"testing"
)

func goProfile() *Profile {
	return &Profile{ID: "go", Signals: Signals{Files: []string{"go.mod"}, Exts: []string{".go"}, Priority: 10}}
}

func nodeProfile() *Profile {
	return &Profile{ID: "node", Signals: Signals{Files: []string{"package.json"}, Exts: []string{".js", ".ts"}, Priority: 5}}
}

func pythonProfile() *Profile {
	return &Profile{ID: "python", Signals: Signals{Exts: []string{".py"}}}
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDetectNonGoFixtureUnsupported(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "package.json", `{"name":"x"}`)

	got := Detect(dir, []*Profile{goProfile()})
	if got == "go" {
		t.Fatal("package.json-only fixture must not detect as go")
	}
	if got == "" {
		t.Fatal("detection must return an explicit sentinel, not empty")
	}
	if got != Unsupported {
		t.Fatalf("want %q, got %q", Unsupported, got)
	}
}

func TestDetect_GoRepoMatchesGo(t *testing.T) {
	root := repoRoot(t)
	got := Detect(root, []*Profile{goProfile(), nodeProfile(), pythonProfile()})
	if got != "go" {
		t.Fatalf("this repo (go.mod at %s) should detect as go, got %q", root, got)
	}
}

func TestDetectPolyglotPrefersSignal(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "go.mod", "module x\n")
	writeFile(t, dir, "stray.py", "print(1)\n")

	got := Detect(dir, []*Profile{goProfile(), pythonProfile()})
	if got != "go" {
		t.Fatalf("go.mod marker should beat a stray .py extension, got %q", got)
	}
}

func TestDetect_SecondProfileSelected(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "package.json", `{"name":"x"}`)

	// The node profile is just data passed alongside go; selection is by signal,
	// not by any go-specific code path.
	got := Detect(dir, []*Profile{goProfile(), nodeProfile()})
	if got != "node" {
		t.Fatalf("a second profile must be selectable by its declared signals, got %q", got)
	}
}

// containsLang reports whether want is in the DetectLanguages result.
func containsLang(langs []string, want string) bool {
	for _, l := range langs {
		if l == want {
			return true
		}
	}
	return false
}

// TestDetectLanguagesNewExts proves the extLang additions for the four v0.2
// stacks resolve. Each language is asserted independently, so deleting any one
// extLang line drops that language and fails its check.
func TestDetectLanguagesNewExts(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "main.dart", "void main() {}\n")
	writeFile(t, dir, "App.svelte", "<script></script>\n")
	writeFile(t, dir, "schema.sql", "select 1;\n")

	langs, err := DetectLanguages(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"dart", "svelte", "sql"} {
		if !containsLang(langs, want) {
			t.Errorf("DetectLanguages = %v, want it to include %q", langs, want)
		}
	}
}

// TestDetectLanguagesKotlinRegression guards the pre-existing .kt->kotlin
// mapping against a careless rewrite of the extLang map.
func TestDetectLanguagesKotlinRegression(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "Main.kt", "fun main() {}\n")

	langs, err := DetectLanguages(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !containsLang(langs, "kotlin") {
		t.Errorf("DetectLanguages = %v, want it to include %q", langs, "kotlin")
	}
}

// TestDetectResolvesEmbeddedProfiles proves each shipped profile is selectable by
// its declared signals through the real embedded set — the c-5 keystone: a stack
// becomes detectable purely by its TOML, with no detect.go change. Deleting any
// <id>.toml drops it from Embedded() and fails that row.
func TestDetectResolvesEmbeddedProfiles(t *testing.T) {
	profiles, err := Embedded()
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		id    string
		setup func(dir string)
	}{
		{"kotlin", func(d string) {
			writeFile(t, d, "build.gradle.kts", "")
			writeFile(t, d, "Main.kt", "fun main() {}\n")
		}},
		{"dart", func(d string) {
			writeFile(t, d, "pubspec.yaml", "name: x\n")
			writeFile(t, d, "main.dart", "void main() {}\n")
		}},
		{"svelte", func(d string) {
			writeFile(t, d, "svelte.config.js", "export default {}\n")
			writeFile(t, d, "App.svelte", "<script></script>\n")
		}},
		{"sql", func(d string) {
			writeFile(t, d, "schema.sql", "select 1;\n")
		}},
		{"typescript", func(d string) {
			writeFile(t, d, "tsconfig.json", "{}\n")
			writeFile(t, d, "index.ts", "export {}\n")
		}},
	}
	for _, tc := range cases {
		t.Run(tc.id, func(t *testing.T) {
			dir := t.TempDir()
			tc.setup(dir)
			if got := Detect(dir, profiles); got != tc.id {
				t.Fatalf("Detect = %q, want %q", got, tc.id)
			}
		})
	}
}

// TestDetectUnsupportedFixture guards against a profile false-matching a repo it
// has no signal for: a Ruby/text fixture matches none of the shipped profiles.
func TestDetectUnsupportedFixture(t *testing.T) {
	profiles, err := Embedded()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeFile(t, dir, "foo.rb", "puts 1\n")
	writeFile(t, dir, "README.txt", "hi\n")

	if got := Detect(dir, profiles); got != Unsupported {
		t.Fatalf("unsupported fixture detected as %q, want %q", got, Unsupported)
	}
}

// TestDetectPolyglotSQLDoesNotHijackGo proves a root marker file outweighs a
// stray extension: a .sql file beside go.mod still detects "go", so SQL (an
// extension-only profile) can't hijack a Go repo.
func TestDetectPolyglotSQLDoesNotHijackGo(t *testing.T) {
	profiles, err := Embedded()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeFile(t, dir, "go.mod", "module x\n")
	writeFile(t, dir, "schema.sql", "select 1;\n")

	if got := Detect(dir, profiles); got != "go" {
		t.Fatalf("go.mod marker should beat a .sql extension, got %q", got)
	}
}

// TestStackCLISurfaceListsAndLoadsProfiles exercises the path `dross stack list`
// and `dross stack show <id>` delegate to: LoadAll() enumerates the profiles and
// ByID resolves a single one. Removing an <id>.toml drops it from the list and
// fails its row.
func TestStackCLISurfaceListsAndLoadsProfiles(t *testing.T) {
	all, err := LoadAll()
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"kotlin", "dart", "svelte", "sql", "typescript"} {
		if ByID(all, id) == nil {
			t.Errorf("LoadAll()/ByID: profile %q missing — `dross stack list`/`show %s` would not surface it", id, id)
		}
	}
}

// TestMarkerProfiles exercises the additive marker seam. It injects in-memory
// profiles (MarkerProfiles is parameterized) so it tests the mechanism independent
// of which profiles ship embedded: a marker-patterned profile is surfaced, ANY
// pattern profile is surfaced (no special-casing), subdirectory markers are caught
// by the subtree walk, and a marker-less tree surfaces nothing.
func TestMarkerProfiles(t *testing.T) {
	markerProfile := &Profile{ID: "marker", Signals: Signals{FilePatterns: dockerFilePatterns}}
	widgetProfile := &Profile{ID: "widget", Signals: Signals{FilePatterns: []string{"*.widget"}}}
	// goProfile has Files/Exts but NO FilePatterns — it must never surface here.
	profiles := []*Profile{goProfile(), markerProfile, widgetProfile}

	t.Run("marker-only repo surfaces the marker profile (c-1)", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, dir, "Dockerfile", "FROM scratch\n")
		if got := MarkerProfiles(dir, profiles); !containsLang(got, "marker") {
			t.Fatalf("MarkerProfiles = %v, want it to include %q", got, "marker")
		}
	})

	t.Run("data-driven, not special-cased (c-5 keystone)", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, dir, "x.widget", "")
		if got := MarkerProfiles(dir, profiles); !containsLang(got, "widget") {
			t.Fatalf("MarkerProfiles = %v, want %q — any FilePatterns profile must surface, proving no hardcode", got, "widget")
		}
	})

	t.Run("subdir marker is caught by the subtree walk", func(t *testing.T) {
		dir := t.TempDir()
		sub := filepath.Join(dir, "services", "api")
		if err := os.MkdirAll(sub, 0o755); err != nil {
			t.Fatal(err)
		}
		writeFile(t, sub, "Dockerfile.dev", "FROM scratch\n")
		if got := MarkerProfiles(dir, profiles); !containsLang(got, "marker") {
			t.Fatalf("MarkerProfiles = %v, want %q — a subdir marker must be detected (subtree, not root-only)", got, "marker")
		}
	})

	t.Run("marker-less repo surfaces nothing (c-6)", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, dir, "main.go", "package main\n")
		writeFile(t, dir, "go.mod", "module x\n")
		if got := MarkerProfiles(dir, profiles); len(got) != 0 {
			t.Fatalf("MarkerProfiles = %v, want empty — a marker-less repo must surface nothing", got)
		}
	})
}

// repoRoot walks up from the test working directory to the nearest go.mod.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no go.mod found walking up from test dir")
		}
		dir = parent
	}
}
