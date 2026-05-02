package changes

import (
	"os"
	"path/filepath"
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
