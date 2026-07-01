package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestProfileCover_showGlobalScope exercises profile.go:30 (err == nil, false
// side) and the scope switch: a valid global profile is loaded and printed.
// A negated `if err != nil` would return early with no output, dropping "terse".
func TestProfileCover_showGlobalScope(t *testing.T) {
	chdir(t, t.TempDir())
	home := t.TempDir()
	t.Setenv("HOME", home)
	mustWrite(t, filepath.Join(home, ".claude", "dross", "profile.toml"),
		"[dimensions]\n[dimensions.communication]\nrating = \"terse\"\nconfidence = \"high\"\ndirective = \"be brief\"\n")

	var err error
	out := captureStdout(t, func() {
		err = runCmd(t, Profile(), "show", "--scope", "global")
	})
	if err != nil {
		t.Fatalf("show global: %v", err)
	}
	if !strings.Contains(out, "terse") {
		t.Errorf("show global output missing loaded rating:\n%s", out)
	}
}

// TestProfileCover_showGlobalMalformed exercises profile.go:30 (err != nil, true
// side): a malformed global profile.toml makes loadProfile("global") return a
// decode error which show must propagate. Negating the guard would fall through
// with a nil profile.
func TestProfileCover_showGlobalMalformed(t *testing.T) {
	chdir(t, t.TempDir())
	home := t.TempDir()
	t.Setenv("HOME", home)
	mustWrite(t, filepath.Join(home, ".claude", "dross", "profile.toml"),
		"= \"broken, no key\"\n")

	var err error
	_ = captureStdout(t, func() {
		err = runCmd(t, Profile(), "show", "--scope", "global")
	})
	if err == nil {
		t.Fatal("expected show to propagate a decode error from a malformed global profile")
	}
}

// TestProfileCover_showProjectKeepsLoaded exercises profile.go:34 (pp == nil is
// false): inside a repo the project profile is non-nil and must NOT be replaced
// by the empty fallback. A negated `if pp == nil` would overwrite the loaded
// profile with an empty one, dropping "projval" from the printed output.
func TestProfileCover_showProjectKeepsLoaded(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	t.Setenv("HOME", t.TempDir()) // empty global
	mustWrite(t, filepath.Join(dir, ".dross", "profile.toml"),
		"[dimensions]\n[dimensions.testdim]\nrating = \"projval\"\n")

	var err error
	out := captureStdout(t, func() {
		err = runCmd(t, Profile(), "show", "--scope", "project")
	})
	if err != nil {
		t.Fatalf("show project: %v", err)
	}
	if !strings.Contains(out, "projval") {
		t.Errorf("show project dropped the loaded project profile:\n%s", out)
	}
}

// TestProfileCover_showProjectOutsideRepo exercises profile.go:34 (pp == nil is
// true): with no .dross found, loadProfile("project") returns nil and the empty
// fallback must be substituted so encoding still succeeds.
func TestProfileCover_showProjectOutsideRepo(t *testing.T) {
	chdir(t, t.TempDir()) // no .dross anywhere up the tree
	t.Setenv("HOME", t.TempDir())

	var err error
	_ = captureStdout(t, func() {
		err = runCmd(t, Profile(), "show", "--scope", "project")
	})
	if err != nil {
		t.Fatalf("show project outside repo should succeed via empty fallback: %v", err)
	}
}

// TestProfileCover_loadProfileGlobalHomeError exercises profile.go:79 (err != nil,
// true side): with HOME unset GlobalDir() fails and loadProfile("global") must
// return (nil, err). Negating the guard falls through to LoadFile on a relative
// path (no HOME dependency), which succeeds with an empty profile.
func TestProfileCover_loadProfileGlobalHomeError(t *testing.T) {
	chdir(t, t.TempDir())
	t.Setenv("HOME", "")

	p, err := loadProfile("global")
	if err == nil {
		t.Fatal("expected loadProfile(global) to error when HOME is unset")
	}
	if p != nil {
		t.Errorf("expected nil profile on GlobalDir error, got %v", p)
	}
}

// TestProfileCover_loadProfileProjectNoRoot exercises profile.go:85 (err != nil,
// true side): outside any repo FindRoot() fails and loadProfile("project") must
// return (nil, err). Negating the guard falls through to LoadFile on a relative
// path, which succeeds with an empty profile.
func TestProfileCover_loadProfileProjectNoRoot(t *testing.T) {
	chdir(t, t.TempDir()) // no .dross up the tree

	p, err := loadProfile("project")
	if err == nil {
		t.Fatal("expected loadProfile(project) to error with no .dross root")
	}
	if p != nil {
		t.Errorf("expected nil profile on FindRoot error, got %v", p)
	}
}

// TestProfileCover_seedSuccess exercises profile.go:66 (err == nil, false side):
// with no GSD profile present SeedFromGSD returns nil and seed prints "seeded".
// Negating the guard would return before Printf, suppressing the confirmation.
func TestProfileCover_seedSuccess(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	var err error
	out := captureStdout(t, func() {
		err = runCmd(t, Profile(), "seed")
	})
	if err != nil {
		t.Fatalf("seed with no GSD profile should succeed: %v", err)
	}
	if !strings.Contains(out, "seeded") {
		t.Errorf("seed should print a confirmation:\n%s", out)
	}
}

// TestProfileCover_seedReadError exercises profile.go:66 (err != nil, true side):
// making USER-PROFILE.md a directory forces a non-NotExist read error out of
// SeedFromGSD, which seed must propagate. Negating the guard would swallow it and
// print "seeded" instead.
func TestProfileCover_seedReadError(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	// A directory where SeedFromGSD expects a readable file → EISDIR on read.
	if err := os.MkdirAll(filepath.Join(home, ".claude", "get-shit-done", "USER-PROFILE.md"), 0o755); err != nil {
		t.Fatal(err)
	}

	var err error
	_ = captureStdout(t, func() {
		err = runCmd(t, Profile(), "seed")
	})
	if err == nil {
		t.Fatal("expected seed to propagate a read error when USER-PROFILE.md is a directory")
	}
}
