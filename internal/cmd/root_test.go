package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

// TestRootCover_MustWriteFile targets root.go:52 CONDITIONALS_NEGATION.
// The success case asserts the file is actually written: the mutant
// (err == nil) would return early before WriteFile, leaving no file.
// The error case forces MkdirAll to fail (parent path is a regular
// file) and asserts a non-nil error is returned.
func TestRootCover_MustWriteFile(t *testing.T) {
	t.Run("success writes file", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "nested", "out.txt")
		want := []byte("payload")
		if err := MustWriteFile(path, want); err != nil {
			t.Fatalf("MustWriteFile returned error: %v", err)
		}
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("file was not written: %v", err)
		}
		if string(got) != string(want) {
			t.Fatalf("content = %q, want %q", got, want)
		}
	})

	t.Run("mkdirall failure returns error", func(t *testing.T) {
		dir := t.TempDir()
		// Create a regular file, then try to write under it so that
		// MkdirAll(filepath.Dir(path)) must fail.
		blocker := filepath.Join(dir, "blocker")
		if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(blocker, "child", "out.txt")
		if err := MustWriteFile(path, []byte("data")); err == nil {
			t.Fatalf("expected error when parent is a file, got nil")
		}
	})
}
