package quality

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
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

func TestDetectLanguages(t *testing.T) {
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
