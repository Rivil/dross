package milestone

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "v1.0.toml")

	original := &Milestone{
		Milestone: Meta{
			Version: "v1.0",
			Title:   "First release",
			Status:  "active",
			Started: "2026-05-02",
		},
		Scope: Scope{
			SuccessCriteria: []string{"users can sign up", "meal CRUD works"},
			NonGoals:        []string{"realtime collab"},
		},
		Phases: []string{"01-auth", "02-meals", "03-tagging"},
	}
	if err := original.Save(path); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(original, loaded) {
		t.Errorf("round-trip drift:\norig: %+v\nload: %+v", original, loaded)
	}
}

func TestList(t *testing.T) {
	root := t.TempDir()
	mDir := filepath.Join(root, "milestones")
	_ = os.MkdirAll(mDir, 0o755)
	for _, v := range []string{"v0.1.toml", "v1.0.toml", "v2.0.toml"} {
		_ = os.WriteFile(filepath.Join(mDir, v), []byte(""), 0o644)
	}
	// Subdirectory should be ignored.
	_ = os.MkdirAll(filepath.Join(mDir, "v1.0"), 0o755)
	// Non-toml should be ignored.
	_ = os.WriteFile(filepath.Join(mDir, "notes.md"), []byte(""), 0o644)

	got, err := List(root)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"v0.1", "v1.0", "v2.0"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v want %v", got, want)
	}
}

func TestListEmpty(t *testing.T) {
	got, err := List(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Errorf("expected nil, got %v", got)
	}
}

func TestFilePath(t *testing.T) {
	got := FilePath(".dross", "v1.0")
	want := filepath.Join(".dross", "milestones", "v1.0.toml")
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
}
