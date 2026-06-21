package quality

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCatalogCompleteness(t *testing.T) {
	// The core Go analyzers — one per substantive dimension dross can measure
	// deterministically. Each must be present AND flagged Core so a thin toolbelt
	// never reads as a clean "all clear".
	core := map[string]bool{
		"gocyclo": false, "dupl": false, "deadcode": false,
		"errcheck": false, "ineffassign": false,
	}
	for _, a := range Catalog() {
		if _, ok := core[a.Name]; ok {
			core[a.Name] = true
			if !a.Core {
				t.Errorf("analyzer %q is in the core set but not flagged Core", a.Name)
			}
		}
	}
	for name, found := range core {
		if !found {
			t.Errorf("core analyzer %q missing from the catalog", name)
		}
	}
}

func TestCatalogExcludesCosmetic(t *testing.T) {
	// quality_scope is locked: substantive maintainability + risky-lint only, no
	// cosmetic/format/naming rules. Two guards — a dimension outside the
	// substantive allowlist, OR a known cosmetic binary, fails this test. Either
	// way, adding a cosmetic/naming-only lint to the table trips it.
	for _, a := range Catalog() {
		if !IsSubstantive(a.Dimension) {
			t.Errorf("analyzer %q has non-substantive dimension %q (cosmetic excluded by quality_scope)", a.Name, a.Dimension)
		}
		if cosmeticBins[a.Bin] {
			t.Errorf("analyzer %q is a cosmetic/format tool; excluded by quality_scope", a.Name)
		}
	}
}

func TestAnalyzersForAgnosticFallback(t *testing.T) {
	// rust has no dedicated catalog (a stub language) — it must still get the
	// agnostic analyzers, never an empty set.
	got := AnalyzersFor("rust")
	if len(got) == 0 {
		t.Fatal("AnalyzersFor(\"rust\") returned no analyzers; agnostic fallback broken")
	}
	want := map[string]bool{"scc": true, "jscpd": true}
	for _, a := range got {
		if !a.Agnostic() {
			t.Errorf("AnalyzersFor(\"rust\") returned non-agnostic analyzer %q", a.Name)
		}
		if !want[a.Name] {
			t.Errorf("unexpected agnostic analyzer %q for rust", a.Name)
		}
	}
}

func TestAnalyzersForGoIsComplete(t *testing.T) {
	names := map[string]bool{}
	for _, a := range AnalyzersFor("go") {
		names[a.Name] = true
	}
	for _, want := range []string{"gocyclo", "dupl", "deadcode", "errcheck", "ineffassign", "scc", "jscpd"} {
		if !names[want] {
			t.Errorf("AnalyzersFor(\"go\") missing %q", want)
		}
	}
}

func TestDetectMissingHasHint(t *testing.T) {
	allMissing := func(string) (string, error) { return "", errors.New("not found") }
	statuses := Detect(Catalog(), allMissing)
	if len(statuses) == 0 {
		t.Fatal("Detect returned no statuses")
	}
	for _, st := range statuses {
		if st.Installed {
			t.Errorf("analyzer %q reported installed under an all-missing lookup", st.Name)
		}
		if st.Install == "" {
			t.Errorf("missing analyzer %q has no install instruction", st.Name)
		}
	}
}

// findAnalyzer returns a pointer to the analyzer named name in list, or nil.
func findAnalyzer(list []Analyzer, name string) *Analyzer {
	for i := range list {
		if list[i].Name == name {
			return &list[i]
		}
	}
	return nil
}

// TestAnalyzersForLanguageProfiles proves each v0.2 profile surfaces its dedicated
// analyzer on top of the agnostic set (scc, jscpd), with a substantive dimension.
// The list is profile-derived, so dropping (e.g.) dcm from dart.toml fails the
// dart row — demonstrating the c-5 keystone on the quality side.
func TestAnalyzersForLanguageProfiles(t *testing.T) {
	cases := []struct {
		id       string
		analyzer string
	}{
		{"kotlin", "detekt"},
		{"dart", "dcm"},
		{"svelte", "eslint"},
		{"sql", "sqlfluff"},
		{"typescript", "eslint"},
	}
	for _, tc := range cases {
		t.Run(tc.id, func(t *testing.T) {
			got := AnalyzersFor(tc.id)
			names := map[string]bool{}
			for _, a := range got {
				names[a.Name] = true
			}
			for _, want := range []string{tc.analyzer, "scc", "jscpd"} {
				if !names[want] {
					t.Errorf("AnalyzersFor(%q) missing %q", tc.id, want)
				}
			}
			dedicated := findAnalyzer(got, tc.analyzer)
			if dedicated == nil || dedicated.Agnostic() {
				t.Fatalf("AnalyzersFor(%q): want a dedicated (non-agnostic) analyzer %q", tc.id, tc.analyzer)
			}
			if !IsSubstantive(dedicated.Dimension) {
				t.Errorf("%s analyzer %q has non-substantive dimension %q", tc.id, dedicated.Name, dedicated.Dimension)
			}
		})
	}
}

// TestSvelteAndTypescriptEslintInstall pins the install hints apart: both profiles
// name their analyzer "eslint", but svelte must pull in eslint-plugin-svelte and
// typescript must pull in typescript-eslint.
func TestSvelteAndTypescriptEslintInstall(t *testing.T) {
	sv := findAnalyzer(AnalyzersFor("svelte"), "eslint")
	if sv == nil || !strings.Contains(sv.Install, "eslint-plugin-svelte") {
		t.Errorf("svelte eslint Install should name eslint-plugin-svelte, got %+v", sv)
	}
	ts := findAnalyzer(AnalyzersFor("typescript"), "eslint")
	if ts == nil || !strings.Contains(ts.Install, "typescript-eslint") {
		t.Errorf("typescript eslint Install should name typescript-eslint, got %+v", ts)
	}
}

// overrideGoProfile points the user profile dir at a temp HOME holding a go.toml
// that replaces the embedded Go profile, so a test can prove the catalog tracks
// the profile rather than an inline table.
func overrideGoProfile(t *testing.T, body string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".claude", "dross", "profiles")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "go.toml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestAnalyzersTrackProfile(t *testing.T) {
	// Override the Go profile with a single analyzer. If AnalyzersFor still read an
	// inline map, dupl would survive — it must not.
	overrideGoProfile(t, "id = \"go\"\n[[tools]]\n  name = \"gocyclo\"\n  kind = \"analyzer\"\n  dimension = \"complexity\"\n")

	names := map[string]bool{}
	for _, a := range AnalyzersFor("go") {
		names[a.Name] = true
	}
	if !names["gocyclo"] {
		t.Error("profile-declared analyzer gocyclo missing from AnalyzersFor(\"go\")")
	}
	if names["dupl"] {
		t.Error("dupl survived after being removed from the profile — list is not profile-derived")
	}
	if !names["scc"] {
		t.Error("agnostic analyzer scc must remain available")
	}
}
