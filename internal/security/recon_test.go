package security

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
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

func TestSecurityReconDelegatesToStack(t *testing.T) {
	// Polyglot fixture plus a non-Go-only fixture — both must match stack.Detect-
	// Languages exactly, proving recon owns no second ext->lang map.
	for _, files := range [][]string{
		{"main.go", "app.py"}, // polyglot
		{"app.py", "lib.rb"},  // non-Go only
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
			t.Errorf("security recon DetectLanguages=%v, stack.DetectLanguages=%v — recon is not delegating", got, want)
		}
	}
}

func TestNoDuplicateExtLangMap(t *testing.T) {
	// A standalone ext->lang map literal must not reappear in either recon source —
	// the whole point of the de-dup is one canonical map in internal/stack.
	for _, path := range []string{"recon.go", filepath.Join("..", "quality", "recon.go")} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		src := string(data)
		for _, marker := range []string{"extLang = map[string]string", `".swift": "swift"`} {
			if strings.Contains(src, marker) {
				t.Errorf("%s still carries a standalone ext->lang map (found %q) — delegate to stack instead", path, marker)
			}
		}
	}
}
