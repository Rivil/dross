package stack

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEmbeddedIncludesGo(t *testing.T) {
	emb, err := Embedded()
	if err != nil {
		t.Fatalf("Embedded: %v", err)
	}
	if ByID(emb, "go") == nil {
		t.Fatal("embedded profiles must include the Go profile")
	}
}

func TestUserDirWinsOnIDCollision(t *testing.T) {
	dir := t.TempDir()
	// A user profile with the same id as the embedded Go profile but a different
	// test command must win.
	writeFile(t, dir, "go.toml", "id = \"go\"\n[runtime.test]\n  run = \"go test -race ./...\"\n")

	merged, err := loadAllFrom(dir)
	if err != nil {
		t.Fatalf("loadAllFrom: %v", err)
	}
	got := ByID(merged, "go")
	if got == nil {
		t.Fatal("go profile missing after merge")
	}
	if got.Runtime.Test.Run != "go test -race ./..." {
		t.Errorf("user dir must win on id collision: got test command %q", got.Runtime.Test.Run)
	}
}

func TestMalformedUserProfileSurfacedNotSwallowed(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "malformed.toml", "id = \nthis is not valid toml [[[\n")

	merged, err := loadAllFrom(dir)
	if err == nil {
		t.Fatal("a malformed user profile must surface an error")
	}
	if !strings.Contains(err.Error(), "malformed.toml") {
		t.Errorf("error must name the offending file, got: %v", err)
	}
	// The embedded Go profile must NOT be silently dropped.
	if ByID(merged, "go") == nil {
		t.Error("embedded Go profile was dropped because of a malformed user file")
	}
}

func TestUserDirAbsentFallsBack(t *testing.T) {
	// A home with no profiles/ subdir at all.
	dir := filepath.Join(t.TempDir(), "claude", "dross", "profiles")
	merged, err := loadAllFrom(dir)
	if err != nil {
		t.Fatalf("absent user dir must not error: %v", err)
	}
	if ByID(merged, "go") == nil {
		t.Fatal("embedded Go profile must remain when the user dir is absent")
	}
}

func TestReadmeDocumentsZeroCodeDropIn(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("profiles", "README.md"))
	if err != nil {
		t.Fatalf("read README: %v", err)
	}
	doc := string(data)
	for _, want := range []string{"single TOML drop-in", "zero code change"} {
		if !strings.Contains(doc, want) {
			t.Errorf("README must document the drop-in: missing %q", want)
		}
	}
}
