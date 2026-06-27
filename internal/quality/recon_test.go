package quality

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/Rivil/dross/internal/stack"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// manifestNames returns the set of analyzer names in the manifest.
func manifestNames(m Manifest) map[string]bool {
	out := map[string]bool{}
	for _, t := range m.Tools {
		out[t.Name] = true
	}
	return out
}

// TestQualityManifestMarkerDocker proves a marker-only repo surfaces the Docker
// analyzer ON TOP of the agnostic set — the analyzer side of closing the blind spot.
func TestQualityManifestMarkerDocker(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "Dockerfile"), "FROM scratch\n")
	allMissing := func(string) (string, error) { return "", errors.New("not found") }

	m, err := BuildManifest(root, allMissing)
	if err != nil {
		t.Fatal(err)
	}
	names := manifestNames(m)
	if !names["hadolint"] {
		t.Errorf("Dockerfile-only repo: quality manifest missing hadolint analyzer (c-3); names=%v", names)
	}
	// The agnostic set must remain — a marker-only repo never loses scc/jscpd.
	for _, want := range []string{"scc", "jscpd"} {
		if !names[want] {
			t.Errorf("marker-only repo lost the agnostic analyzer %q; names=%v", want, names)
		}
	}
}

// TestQualityNoMarkerRegression guards c-6: a marker-less repo surfaces no Docker
// analyzer.
func TestQualityNoMarkerRegression(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "main.go"), "package main")
	allMissing := func(string) (string, error) { return "", errors.New("not found") }

	m, err := BuildManifest(root, allMissing)
	if err != nil {
		t.Fatal(err)
	}
	if manifestNames(m)["hadolint"] {
		t.Error("marker-less repo surfaced the hadolint analyzer — no Docker tools must leak in (c-6)")
	}
}

// TestPythonAnalyzerInManifest is the c-1 analyzer-in-run row: a .py tree must
// surface python's dedicated analyzer (ruff) in the quality manifest, ON TOP of
// the agnostic set — proving a full stack profile's analyzer reaches a real run via
// profile id == language. Dropping ruff from python.toml (or renaming the profile
// id away from "python") loses the ruff row.
func TestPythonAnalyzerInManifest(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "app.py"), "print(1)\n")
	allMissing := func(string) (string, error) { return "", errors.New("not found") }

	m, err := BuildManifest(root, allMissing)
	if err != nil {
		t.Fatal(err)
	}
	names := manifestNames(m)
	if !names["ruff"] {
		t.Errorf(".py repo: quality manifest missing python's ruff analyzer (c-1); names=%v", names)
	}
	// The agnostic set must remain — a python repo never loses scc/jscpd.
	for _, want := range []string{"scc", "jscpd"} {
		if !names[want] {
			t.Errorf("python repo lost the agnostic analyzer %q; names=%v", want, names)
		}
	}
}

func TestDetectLanguages(t *testing.T) {
	// HOME-isolate: DetectLanguages derives ext->lang from LoadAll(), which reads
	// ~/.claude/dross/profiles — a real user overlay could add a language and flip
	// this exact-match assertion (review flag #3).
	t.Setenv("HOME", t.TempDir())
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "main.go"), "package main")
	// A planted .dross/ holding a python file. Detection must NOT descend into
	// .dross/ — if it did it would surface "python" and would be reading planning
	// artifacts, breaking the code-only sweep. So the result must be exactly ["go"].
	writeFile(t, filepath.Join(root, ".dross", "sneaky.py"), "import os")

	langs, err := DetectLanguages(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(langs) != 1 || langs[0] != "go" {
		t.Fatalf("DetectLanguages = %v, want exactly [\"go\"] (python in .dross/ must be skipped, result sorted+deduped)", langs)
	}
}

func TestDetectLanguagesUnknownExt(t *testing.T) {
	t.Setenv("HOME", t.TempDir()) // overlay-independent: see TestDetectLanguages (flag #3)
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "data.xyz"), "blob")
	writeFile(t, filepath.Join(root, "notes.unknownext"), "blob")

	langs, err := DetectLanguages(root)
	if err != nil {
		t.Fatalf("unknown extension crashed detection: %v", err)
	}
	if len(langs) != 0 {
		t.Fatalf("unknown extensions yielded languages: %v", langs)
	}
}

func TestManifestRecordsSkipped(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "main.go"), "package main")
	allMissing := func(string) (string, error) { return "", errors.New("not found") }

	m, err := BuildManifest(root, allMissing)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Ran()) != 0 {
		t.Errorf("Ran() non-empty under an all-missing lookup: %v", m.Ran())
	}
	if len(m.Skipped()) == 0 {
		t.Fatal("manifest omits detected-but-uninstalled analyzers; Skipped() is empty — a thin toolbelt would read 'all clear'")
	}
	names := map[string]bool{}
	for _, s := range m.Skipped() {
		names[s.Name] = true
	}
	for _, want := range []string{"gocyclo", "scc"} {
		if !names[want] {
			t.Errorf("manifest Skipped() missing %q (main.go → go, so the Go core + agnostic tools must appear)", want)
		}
	}
}

func TestQualityReconDelegatesToStack(t *testing.T) {
	// Polyglot plus a non-Go-only fixture — both must match stack.DetectLanguages
	// exactly, proving recon owns no second ext->lang map.
	for _, files := range [][]string{
		{"main.go", "app.py"},
		{"app.py", "lib.rb"},
	} {
		root := t.TempDir()
		for _, f := range files {
			writeFile(t, filepath.Join(root, f), "x")
		}
		got, err := DetectLanguages(root)
		if err != nil {
			t.Fatal(err)
		}
		want, err := stack.DetectLanguages(root)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("quality recon DetectLanguages=%v, stack.DetectLanguages=%v — recon is not delegating", got, want)
		}
	}
}
