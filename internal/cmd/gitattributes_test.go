package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnsureDrossGitattributesCreates(t *testing.T) {
	dir := t.TempDir()
	if err := ensureDrossGitattributes(dir); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	body, err := os.ReadFile(filepath.Join(dir, ".gitattributes"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(string(body), drossGitattributesLine) {
		t.Errorf("missing line %q in:\n%s", drossGitattributesLine, body)
	}
}

func TestEnsureDrossGitattributesIdempotent(t *testing.T) {
	dir := t.TempDir()
	if err := ensureDrossGitattributes(dir); err != nil {
		t.Fatalf("first ensure: %v", err)
	}
	if err := ensureDrossGitattributes(dir); err != nil {
		t.Fatalf("second ensure: %v", err)
	}
	body, _ := os.ReadFile(filepath.Join(dir, ".gitattributes"))
	if got := strings.Count(string(body), drossGitattributesLine); got != 1 {
		t.Errorf("expected line once, got %d times:\n%s", got, body)
	}
}

func TestEnsureDrossGitattributesAppends(t *testing.T) {
	dir := t.TempDir()
	pre := "*.png binary\n"
	if err := os.WriteFile(filepath.Join(dir, ".gitattributes"), []byte(pre), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ensureDrossGitattributes(dir); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	body, _ := os.ReadFile(filepath.Join(dir, ".gitattributes"))
	if !strings.Contains(string(body), pre) {
		t.Errorf("pre-existing content was lost:\n%s", body)
	}
	if !strings.Contains(string(body), drossGitattributesLine) {
		t.Errorf("did not append our line:\n%s", body)
	}
}

func TestEnsureDrossGitattributesAppendsWithoutTrailingNewline(t *testing.T) {
	dir := t.TempDir()
	// Deliberately missing trailing newline — common when files are
	// hand-edited. We should not merge our line onto the previous one.
	pre := "*.png binary"
	if err := os.WriteFile(filepath.Join(dir, ".gitattributes"), []byte(pre), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ensureDrossGitattributes(dir); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	body, _ := os.ReadFile(filepath.Join(dir, ".gitattributes"))
	lines := strings.Split(strings.TrimRight(string(body), "\n"), "\n")
	if len(lines) != 2 {
		t.Errorf("expected 2 lines, got %d:\n%s", len(lines), body)
	}
}

func TestEnsureDrossGitattributesSkipsWhenLinePresent(t *testing.T) {
	dir := t.TempDir()
	pre := "# notes\n" + drossGitattributesLine + "\n*.png binary\n"
	if err := os.WriteFile(filepath.Join(dir, ".gitattributes"), []byte(pre), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ensureDrossGitattributes(dir); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	body, _ := os.ReadFile(filepath.Join(dir, ".gitattributes"))
	if string(body) != pre {
		t.Errorf("body should be unchanged.\nexpected:\n%s\ngot:\n%s", pre, body)
	}
}
