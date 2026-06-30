package cmd

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/Rivil/dross/assets"
)

// a representative command skill present in assets/commands.
const sampleSkill = "dross-execute"

func skillMDPath(home string) string {
	return filepath.Join(home, ".claude", "skills", sampleSkill, "SKILL.md")
}

// TestInstallCopyMode proves the end-user install: SKILL.md and prompt files are
// REAL files (not symlinks) whose bytes match the embedded FS.
func TestInstallCopyMode(t *testing.T) {
	home := t.TempDir()
	in := &installer{home: home, link: false, out: io.Discard}
	if err := in.run(); err != nil {
		t.Fatalf("install: %v", err)
	}

	skill := skillMDPath(home)
	fi, err := os.Lstat(skill)
	if err != nil {
		t.Fatalf("SKILL.md not created: %v", err)
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		t.Errorf("copy mode: SKILL.md is a symlink, want a real file")
	}
	got, err := os.ReadFile(skill)
	if err != nil {
		t.Fatal(err)
	}
	want, err := assets.FS.ReadFile("commands/" + sampleSkill + ".md")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Errorf("copy mode: SKILL.md bytes differ from embed.FS")
	}

	// A prompt file is a real copy too.
	prompt := filepath.Join(home, ".claude", "dross", "prompts", "execute.md")
	pfi, err := os.Lstat(prompt)
	if err != nil {
		t.Fatalf("prompt not copied: %v", err)
	}
	if pfi.Mode()&os.ModeSymlink != 0 {
		t.Errorf("copy mode: prompt is a symlink, want a real file")
	}
	pgot, _ := os.ReadFile(prompt)
	pwant, _ := assets.FS.ReadFile("prompts/execute.md")
	if string(pgot) != string(pwant) {
		t.Errorf("copy mode: prompt bytes differ from embed.FS")
	}
}

// TestInstallSymlinkMode proves the dev install: SKILL.md is a symlink resolving to
// the absolute source file under assets/commands.
func TestInstallSymlinkMode(t *testing.T) {
	home := t.TempDir()
	src := filepath.Join(repoRootFromTest(t), "assets")
	in := &installer{home: home, link: true, sourceDir: src, out: io.Discard}
	if err := in.run(); err != nil {
		t.Fatalf("install: %v", err)
	}

	skill := skillMDPath(home)
	fi, err := os.Lstat(skill)
	if err != nil {
		t.Fatalf("SKILL.md not created: %v", err)
	}
	if fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("link mode: SKILL.md is not a symlink")
	}
	target, err := os.Readlink(skill)
	if err != nil {
		t.Fatal(err)
	}
	wantTarget := filepath.Join(src, "commands", sampleSkill+".md")
	if target != wantTarget {
		t.Errorf("symlink target = %q, want %q", target, wantTarget)
	}

	// prompts dir is a symlink to the source prompts dir.
	prompts := filepath.Join(home, ".claude", "dross", "prompts")
	plink, err := os.Readlink(prompts)
	if err != nil {
		t.Fatalf("prompts not symlinked: %v", err)
	}
	if plink != filepath.Join(src, "prompts") {
		t.Errorf("prompts symlink = %q, want %q", plink, filepath.Join(src, "prompts"))
	}
}

// TestInstallPrunesStaleDross proves a dross-* skill this version dropped is removed.
func TestInstallPrunesStaleDross(t *testing.T) {
	home := t.TempDir()
	stale := filepath.Join(home, ".claude", "skills", "dross-obsolete")
	if err := os.MkdirAll(stale, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stale, "SKILL.md"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	in := &installer{home: home, link: false, out: io.Discard}
	if err := in.run(); err != nil {
		t.Fatalf("install: %v", err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Errorf("stale dross-obsolete skill not pruned (err=%v)", err)
	}
	// A real shipped skill is still present.
	if _, err := os.Stat(skillMDPath(home)); err != nil {
		t.Errorf("shipped skill missing after prune: %v", err)
	}
}

// TestInstallPrunesStalePrompt proves a dropped prompt file is removed on a copy-mode
// install (asset_sync_policy prunes prompts, not just skills).
func TestInstallPrunesStalePrompt(t *testing.T) {
	home := t.TempDir()
	promptsDir := filepath.Join(home, ".claude", "dross", "prompts")
	if err := os.MkdirAll(promptsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(promptsDir, "removed-last-version.md")
	if err := os.WriteFile(stale, []byte("gone"), 0o644); err != nil {
		t.Fatal(err)
	}
	in := &installer{home: home, link: false, out: io.Discard}
	if err := in.run(); err != nil {
		t.Fatalf("install: %v", err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Errorf("stale prompt not pruned (err=%v)", err)
	}
	// A shipped prompt remains.
	if _, err := os.Stat(filepath.Join(promptsDir, "execute.md")); err != nil {
		t.Errorf("shipped prompt missing after prune: %v", err)
	}
}

// TestInstallSparesNonDross proves a non-dross skill and its files survive install.
func TestInstallSparesNonDross(t *testing.T) {
	home := t.TempDir()
	userSkill := filepath.Join(home, ".claude", "skills", "foo")
	if err := os.MkdirAll(userSkill, 0o755); err != nil {
		t.Fatal(err)
	}
	userFile := filepath.Join(userSkill, "SKILL.md")
	if err := os.WriteFile(userFile, []byte("user content"), 0o644); err != nil {
		t.Fatal(err)
	}
	in := &installer{home: home, link: false, out: io.Discard}
	if err := in.run(); err != nil {
		t.Fatalf("install: %v", err)
	}
	if _, err := os.Stat(userSkill); err != nil {
		t.Errorf("non-dross skill dir was removed: %v", err)
	}
	got, err := os.ReadFile(userFile)
	if err != nil {
		t.Fatalf("non-dross user file removed: %v", err)
	}
	if string(got) != "user content" {
		t.Errorf("non-dross user file mutated: %q", got)
	}
}
