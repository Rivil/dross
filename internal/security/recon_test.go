package security

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

func TestDetectLanguagesContextFree(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "main.go"), "package main")
	// A planted .dross/ holding a python file + planning artifacts. Detection must
	// NOT descend into .dross/ — if it did it would surface "python" and would be
	// reading planning artifacts, violating context-free.
	writeFile(t, filepath.Join(root, ".dross", "evil.py"), "import os")
	writeFile(t, filepath.Join(root, ".dross", "rules.toml"), "[[rule]]\ntext = 'x'")

	langs, err := DetectLanguages(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range langs {
		if l == "python" {
			t.Fatalf("detection descended into .dross/ (found python) — audit is not context-free; langs=%v", langs)
		}
	}
	if !contains(langs, "go") {
		t.Fatalf("expected go detected from main.go, got %v", langs)
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

func TestBuildManifestRecordsMissing(t *testing.T) {
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
		t.Fatal("manifest omits detected-but-uninstalled scanners; Skipped() is empty")
	}
	names := map[string]bool{}
	for _, s := range m.Skipped() {
		names[s.Name] = true
	}
	for _, want := range []string{"govulncheck", "gitleaks"} {
		if !names[want] {
			t.Errorf("manifest Skipped() missing %q (main.go → go, so the Go core + agnostic tools must appear)", want)
		}
	}
}

func contains(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}
