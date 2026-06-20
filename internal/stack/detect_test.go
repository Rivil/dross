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
