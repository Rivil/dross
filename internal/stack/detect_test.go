package stack

import (
	"os"
	"path/filepath"
	"strings"
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
	isolateHome(t)
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
	isolateHome(t)
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

// isolateHome points HOME at an empty temp dir so DetectLanguages — which now
// derives ext->lang from LoadAll() (embedded built-ins overlaid by
// ~/.claude/dross/profiles) — sees no user overlay and resolves to the embedded set
// alone. Without this, a contributor who happens to have a profile dropped under
// their real ~/.claude/dross/profiles could flip an exact-match assertion (review
// flag #3). Tests asserting only "includes X" don't need it; exact-set tests do.
func isolateHome(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
}

// TestDetectLanguagesNoRegression is the c-2 no-regression keystone: every language
// the deleted extLang map used to carry must still be derivable from the profile
// set. A tree with one file per legacy extension must yield all 16 legacy language
// ids; breaking any single profile's exts (or its derivation) drops exactly that
// language and fails its row. It runs against detectLanguagesFrom over Embedded() —
// no filesystem overlay, no HOME dependence — so the assertion is deterministic.
func TestDetectLanguagesNoRegression(t *testing.T) {
	profiles, err := Embedded()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	legacyExts := []string{
		".go", ".py", ".js", ".jsx", ".ts", ".tsx", ".rb", ".rs", ".java",
		".kt", ".dart", ".svelte", ".sql", ".c", ".h", ".cc", ".cpp", ".cs",
		".php", ".swift",
	}
	for i, ext := range legacyExts {
		writeFile(t, dir, "f"+string(rune('a'+i))+ext, "x\n")
	}

	langs, err := detectLanguagesFrom(dir, profiles)
	if err != nil {
		t.Fatal(err)
	}
	// The 16 distinct languages the old hardcoded extLang map resolved.
	for _, want := range []string{
		"go", "python", "javascript", "typescript", "ruby", "rust", "java",
		"kotlin", "dart", "svelte", "sql", "c", "cpp", "csharp", "php", "swift",
	} {
		if !containsLang(langs, want) {
			t.Errorf("DetectLanguages = %v, want it to include %q — a legacy language stopped resolving", langs, want)
		}
	}
}

// TestTsNotHijackedBySvelte pins the amended ext_clash_resolution: .ts is claimed by
// BOTH typescript@4 and svelte@6, and DetectLanguages unions them — so a tree with a
// .ts file yields BOTH languages. A winner-take-all derivation would keep only svelte
// (priority 6 > 4) and silently drop typescript, regressing c-2.
func TestTsNotHijackedBySvelte(t *testing.T) {
	profiles, err := Embedded()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeFile(t, dir, "index.ts", "export {}\n")

	langs, err := detectLanguagesFrom(dir, profiles)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"typescript", "svelte"} {
		if !containsLang(langs, want) {
			t.Errorf("DetectLanguages on a .ts tree = %v, want it to include %q — a shared ext must union, not winner-take-all", langs, want)
		}
	}
}

// TestExtLangForUnit exercises the ext->lang derivation as a pure function over a
// profile slice — no filesystem. It proves a shared extension unions to every
// claiming profile's id (sorted, de-duplicated) and a sole-claim extension maps to
// exactly one. A tie-break/clash regression in extLangFor fails here without a temp
// dir.
func TestExtLangForUnit(t *testing.T) {
	profiles := []*Profile{
		{ID: "typescript", Signals: Signals{Exts: []string{".ts", ".tsx"}}},
		{ID: "svelte", Signals: Signals{Exts: []string{".svelte", ".ts", ".tsx"}}},
		{ID: "go", Signals: Signals{Exts: []string{".go"}}},
	}
	m := extLangFor(profiles)

	if got, want := m[".ts"], []string{"svelte", "typescript"}; !equalStrs(got, want) {
		t.Errorf("extLangFor[.ts] = %v, want %v (shared ext must union, sorted)", got, want)
	}
	if got, want := m[".svelte"], []string{"svelte"}; !equalStrs(got, want) {
		t.Errorf("extLangFor[.svelte] = %v, want %v (sole claim)", got, want)
	}
	if got, want := m[".go"], []string{"go"}; !equalStrs(got, want) {
		t.Errorf("extLangFor[.go] = %v, want %v", got, want)
	}
	if _, ok := m[".unknown"]; ok {
		t.Error("extLangFor invented a mapping for an unclaimed extension")
	}
}

// TestNoExtLangMapInDetect is the t-3 contract #4 guard: the hardcoded ext->lang map
// literal must not survive (or be reintroduced) in detect.go — the mapping is
// single-sourced from the loaded profiles via extLangFor. Mirrors the
// TestNoDuplicateExtLangMap idiom in the recon packages (reading source via
// os.ReadFile).
func TestNoExtLangMapInDetect(t *testing.T) {
	data, err := os.ReadFile("detect.go")
	if err != nil {
		t.Fatalf("read detect.go: %v", err)
	}
	src := string(data)
	for _, marker := range []string{"extLang = map[string]string", `".swift": "swift"`} {
		if strings.Contains(src, marker) {
			t.Errorf("detect.go still carries a standalone ext->lang map (found %q) — derive it from profiles via extLangFor instead", marker)
		}
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
// has no signal for: a fixture of genuinely-unmapped extensions matches none of the
// shipped profiles. (Originally a .rb fixture — but shipping ruby.toml makes .rb
// legitimately detect "ruby", so the fixture was swapped to .xyz/.txt, which no
// profile claims, to keep the never-false-match guard meaningful.)
func TestDetectUnsupportedFixture(t *testing.T) {
	profiles, err := Embedded()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeFile(t, dir, "data.xyz", "blob\n")
	writeFile(t, dir, "README.txt", "hi\n")

	if got := Detect(dir, profiles); got != Unsupported {
		t.Fatalf("unsupported fixture detected as %q, want %q", got, Unsupported)
	}
}

// TestStubExtAliasesDetect proves the multi-extension aliases the old extLang map
// carried survive as stub-profile signals: c.toml claims both .c and .h, cpp.toml
// both .cc and .cpp. Each is asserted independently through the real embedded set,
// so dropping .h from c.toml (or .cc from cpp.toml) makes that extension's tree
// fall through to Unsupported and fails its row.
func TestStubExtAliasesDetect(t *testing.T) {
	profiles, err := Embedded()
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		file string
		want string
	}{
		{"main.c", "c"},
		{"header.h", "c"},
		{"app.cc", "cpp"},
		{"app.cpp", "cpp"},
		{"lib.rb", "ruby"},
		{"main.rs", "rust"},
		{"App.java", "java"},
		{"index.php", "php"},
		{"main.swift", "swift"},
	}
	for _, tc := range cases {
		t.Run(tc.file, func(t *testing.T) {
			dir := t.TempDir()
			writeFile(t, dir, tc.file, "x\n")
			if got := Detect(dir, profiles); got != tc.want {
				t.Fatalf("Detect on %s = %q, want %q (stub ext alias lost)", tc.file, got, tc.want)
			}
		})
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

// TestDetectFullProfilesResolve proves each of the three full non-Go profiles
// shipped this phase (python, javascript, csharp) is selectable by its declared
// signals through the real embedded set — Detect needs only the TOML, no detect.go
// change (c-1). Deleting any <id>.toml, or renaming javascript's id to "node",
// drops its row.
func TestDetectFullProfilesResolve(t *testing.T) {
	profiles, err := Embedded()
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		id    string
		setup func(dir string)
	}{
		{"python", func(d string) {
			writeFile(t, d, "pyproject.toml", "[project]\nname = \"x\"\n")
			writeFile(t, d, "app.py", "print(1)\n")
		}},
		{"javascript", func(d string) {
			writeFile(t, d, "package.json", `{"name":"x"}`)
			writeFile(t, d, "index.js", "export default 1\n")
		}},
		{"csharp", func(d string) {
			writeFile(t, d, "Program.cs", "class P {}\n")
		}},
	}
	for _, tc := range cases {
		t.Run(tc.id, func(t *testing.T) {
			dir := t.TempDir()
			tc.setup(dir)
			if got := Detect(dir, profiles); got != tc.id {
				t.Fatalf("Detect = %q, want %q (profile must resolve by data alone)", got, tc.id)
			}
		})
	}
}

// TestJsNotHijackedByTs is the TS-hijack guard (t-1 contract #3): on a TypeScript
// project tree the typescript profile must win over javascript. The realistic case
// (package.json + tsconfig.json + index.ts) wins on score (tsconfig+ .ts = 101 >
// package.json = 100); the source-less scaffold case (package.json + tsconfig.json,
// a 100-100 tie) wins only because javascript's priority (3) sits below
// typescript's (4). Raising javascript's priority to >= 4, or letting it claim
// tsconfig.json, fails the tie case.
func TestJsNotHijackedByTs(t *testing.T) {
	profiles, err := Embedded()
	if err != nil {
		t.Fatal(err)
	}
	t.Run("ts project with sources", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, dir, "package.json", `{"name":"x"}`)
		writeFile(t, dir, "tsconfig.json", "{}\n")
		writeFile(t, dir, "index.ts", "export {}\n")
		if got := Detect(dir, profiles); got != "typescript" {
			t.Fatalf("Detect = %q, want \"typescript\" — a TS project must not be mislabelled javascript", got)
		}
	})
	t.Run("source-less scaffold (priority tie-break)", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, dir, "package.json", `{"name":"x"}`)
		writeFile(t, dir, "tsconfig.json", "{}\n")
		if got := Detect(dir, profiles); got != "typescript" {
			t.Fatalf("Detect = %q, want \"typescript\" — package.json/tsconfig.json tie must break to typescript (javascript priority must stay < typescript)", got)
		}
	})
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

// TestNoDockerHardcode proves marker detection is purely data-driven: the
// mechanism source (detect.go) and both recon paths must carry no literal
// "docker"/"Dockerfile" — the docker stack lives entirely in docker.toml, and a
// special-case slipped into the mechanism code would trip this. Mirrors the
// TestNoDuplicateExtLangMap idiom (reading source via os.ReadFile). Note it reads
// the production .go files, not _test.go files, so test fixtures may name Docker.
func TestNoDockerHardcode(t *testing.T) {
	paths := []string{
		"detect.go",
		filepath.Join("..", "security", "recon.go"),
		filepath.Join("..", "quality", "recon.go"),
	}
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		src := strings.ToLower(string(data))
		if strings.Contains(src, "docker") {
			t.Errorf("%s contains a literal \"docker\"/\"Dockerfile\" — marker detection must be data-driven (move it to docker.toml), not hardcoded in mechanism code", path)
		}
	}
}

// zzzDropInProfile is a brand-new profile id no embedded TOML ships: a unique .zzz
// extension plus one dedicated analyzer. Dropping it under the user overlay must make
// "zzz" both detectable and recon-visible with zero Go change — the c-3 keystone.
const zzzDropInProfile = `id = "zzz"
title = "Zzz"
[signals]
  exts = [".zzz"]
[[tools]]
  name = "zzzlint"
  kind = "analyzer"
  dimension = "complexity"
  core = true
  install = "echo install zzzlint"
`

// dropInOverlay points HOME at a fresh temp dir, writes profile TOMLs into the user
// overlay (~/.claude/dross/profiles), and returns the overlay dir. This is the real
// drop-in path a user takes — no embedded change, no recompile.
func dropInOverlay(t *testing.T, files map[string]string) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	profDir := filepath.Join(home, ".claude", "dross", "profiles")
	if err := os.MkdirAll(profDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, content := range files {
		writeFile(t, profDir, name, content)
	}
	return profDir
}

// TestDropInProfileDetectable is the c-3 keystone (detection half): a brand-new
// profile dropped under ~/.claude/dross/profiles makes its language detectable with
// zero edit to detect.go — DetectLanguages derives the new .zzz->zzz mapping purely
// from the loaded profile set. Reverting t-3's profile-derived DetectLanguages (back
// to a hardcoded map) breaks this, since .zzz was never in any map literal.
func TestDropInProfileDetectable(t *testing.T) {
	dropInOverlay(t, map[string]string{"zzz.toml": zzzDropInProfile})

	code := t.TempDir()
	writeFile(t, code, "widget.zzz", "blob\n")

	langs, err := DetectLanguages(code)
	if err != nil {
		t.Fatal(err)
	}
	if !containsLang(langs, "zzz") {
		t.Fatalf("DetectLanguages = %v, want it to include %q — a drop-in profile must be detectable with no code change", langs, "zzz")
	}
}

// TestDropInMalformedDoesNotCrash is the never-crash drop-in seam: a malformed TOML
// dropped beside valid profiles must not crash detection — LoadAll still returns the
// merged embedded set, so DetectLanguages yields the embedded languages rather than
// erroring out on a bad user file.
func TestDropInMalformedDoesNotCrash(t *testing.T) {
	dropInOverlay(t, map[string]string{
		"zzz.toml":    zzzDropInProfile,
		"broken.toml": "id = \nthis is not valid toml [[[\n",
	})

	code := t.TempDir()
	writeFile(t, code, "main.go", "package main\n")

	langs, err := DetectLanguages(code)
	if err != nil {
		t.Fatalf("a malformed drop-in must not crash detection, got: %v", err)
	}
	if !containsLang(langs, "go") {
		t.Fatalf("DetectLanguages = %v, want the embedded set to survive a malformed overlay (include %q)", langs, "go")
	}
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
