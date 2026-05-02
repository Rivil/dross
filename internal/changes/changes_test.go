package changes

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestLoadMissingReturnsEmpty(t *testing.T) {
	c, err := Load(filepath.Join(t.TempDir(), "nope.json"), "01-foo")
	if err != nil {
		t.Fatalf("missing file should be ok: %v", err)
	}
	if c.Phase != "01-foo" {
		t.Errorf("phase id should default from arg, got %q", c.Phase)
	}
	if c.Tasks == nil {
		t.Error("Tasks should be initialised even for missing file")
	}
}

func TestRecordAndRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "changes.json")

	c := New("01-meal-tagging")
	c.Record("t-1", []string{"db/schema.ts", "db/migrations/0042.sql"}, "abc1234", "")
	c.Record("t-2", []string{"src/api/tags.ts"}, "def5678", "tagged with helper notes")

	if err := c.Save(path); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := Load(path, "01-meal-tagging")
	if err != nil {
		t.Fatal(err)
	}
	if got.Phase != "01-meal-tagging" {
		t.Errorf("phase: got %q", got.Phase)
	}
	if len(got.Tasks) != 2 {
		t.Fatalf("tasks: %d", len(got.Tasks))
	}
	if got.Tasks["t-1"].Commit != "abc1234" {
		t.Errorf("t-1 commit: %q", got.Tasks["t-1"].Commit)
	}
	if len(got.Tasks["t-1"].Files) != 2 {
		t.Errorf("t-1 files: %v", got.Tasks["t-1"].Files)
	}
	if got.Tasks["t-2"].Notes != "tagged with helper notes" {
		t.Errorf("t-2 notes: %q", got.Tasks["t-2"].Notes)
	}
	if got.Tasks["t-1"].CompletedAt.IsZero() {
		t.Error("CompletedAt should be set")
	}
}

func TestRecordOverwritesOnRerun(t *testing.T) {
	c := New("p")
	c.Record("t-1", []string{"a.ts"}, "old", "")
	first := c.Tasks["t-1"].CompletedAt
	time.Sleep(2 * time.Millisecond)
	c.Record("t-1", []string{"a.ts", "b.ts"}, "new", "")
	if c.Tasks["t-1"].Commit != "new" {
		t.Errorf("expected overwrite, got commit %q", c.Tasks["t-1"].Commit)
	}
	if !c.Tasks["t-1"].CompletedAt.After(first) {
		t.Error("CompletedAt should advance on rerun")
	}
}

func TestSaveCreatesParentDir(t *testing.T) {
	dir := t.TempDir()
	deep := filepath.Join(dir, "a", "b", "c", "changes.json")
	c := New("p")
	c.Record("t-1", []string{"x"}, "", "")
	if err := c.Save(deep); err != nil {
		t.Fatalf("save: %v", err)
	}
	if _, err := os.Stat(deep); err != nil {
		t.Errorf("file not at deep path: %v", err)
	}
}

func TestFilePath(t *testing.T) {
	got := FilePath(".dross", "01-foo")
	want := filepath.Join(".dross", "phases", "01-foo", "changes.json")
	if got != want {
		t.Errorf("FilePath: got %q, want %q", got, want)
	}
}

func TestLoadMalformedJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "changes.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path, "p"); err == nil {
		t.Error("expected unmarshal error for malformed JSON, got nil")
	}
}

func TestLoadReadErrorWhenPathIsDir(t *testing.T) {
	// ReadFile on a directory returns an error that is not fs.ErrNotExist,
	// exercising the generic-error branch in Load.
	dir := t.TempDir()
	if _, err := Load(dir, "p"); err == nil {
		t.Error("expected read error when path is a directory, got nil")
	}
}

func TestLoadAppliesDefaultsForLegacyShape(t *testing.T) {
	// JSON with no "phase" or "tasks" keys should still produce a usable Changes.
	dir := t.TempDir()
	path := filepath.Join(dir, "changes.json")
	if err := os.WriteFile(path, []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := Load(path, "01-foo")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if c.Phase != "01-foo" {
		t.Errorf("phase default not applied: %q", c.Phase)
	}
	if c.Tasks == nil {
		t.Error("Tasks nil after load — default not applied")
	}
}

func TestSaveFailsWhenParentIsAFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission semantics differ on windows")
	}
	dir := t.TempDir()
	blocker := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Save target sits "inside" a regular file → MkdirAll must fail.
	target := filepath.Join(blocker, "sub", "changes.json")
	c := New("p")
	if err := c.Save(target); err == nil {
		t.Error("expected MkdirAll to fail when parent path is a file")
	}
}

func TestRecordInitialisesNilTaskMap(t *testing.T) {
	c := &Changes{Phase: "p"} // no New(), Tasks is nil
	c.Record("t-1", []string{"a"}, "abc", "note")
	if len(c.Tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(c.Tasks))
	}
	if c.Tasks["t-1"].Notes != "note" {
		t.Errorf("notes round-trip: %q", c.Tasks["t-1"].Notes)
	}
}
