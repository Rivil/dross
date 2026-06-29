package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// makeInstallToTemp runs `make install` with HOME pointed at a temp dir and returns
// the repo root and that temp HOME. Because `make install` now delegates the
// skills/prompts sync to `dross install` (which writes under $HOME/.claude), driving
// it through HOME isolates the install from the developer's real ~/.claude.
func makeInstallToTemp(t *testing.T) (root, home string) {
	t.Helper()
	if _, err := exec.LookPath("make"); err != nil {
		t.Skip("make not on PATH — skipping make install test")
	}
	root = repoRootFromTest(t)
	home = t.TempDir()
	cmd := exec.Command("make", "install")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "HOME="+home)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("make install failed: %v\n%s", err, out)
	}
	return root, home
}

// TestMakeInstallDelegates proves the install_unification decision: `make install`
// shells out to `dross install` rather than running its own ln/loop. After install,
// EVERY assets/commands/dross-*.md (not just _interaction.md) must have a
// skills/dross-<name>/SKILL.md symlink resolving to its source file — which only the
// delegated `dross install --link` produces.
func TestMakeInstallDelegates(t *testing.T) {
	root, home := makeInstallToTemp(t)
	skillsDir := filepath.Join(home, ".claude", "skills")

	cmds, err := filepath.Glob(filepath.Join(root, "assets", "commands", "dross-*.md"))
	if err != nil {
		t.Fatal(err)
	}
	if len(cmds) == 0 {
		t.Fatal("no assets/commands/dross-*.md found — test wiring broken")
	}
	for _, src := range cmds {
		name := strings.TrimSuffix(filepath.Base(src), ".md")
		skill := filepath.Join(skillsDir, name, "SKILL.md")
		fi, err := os.Lstat(skill)
		if err != nil {
			t.Errorf("%s: SKILL.md not installed: %v", name, err)
			continue
		}
		if fi.Mode()&os.ModeSymlink == 0 {
			t.Errorf("%s: SKILL.md is not a symlink (make ran its own copy, not dross install --link)", name)
			continue
		}
		target, err := os.Readlink(skill)
		if err != nil {
			t.Errorf("%s: readlink: %v", name, err)
			continue
		}
		if target != src {
			t.Errorf("%s: symlink → %q, want %q", name, target, src)
		}
	}
}

// TestMakeInstallLinksInteractionSnippet proves c-2's delivery half: after
// `make install`, the snippet resolves through the installed prompts dir and its
// content matches source. If install stops linking the prompts dir (or somehow
// excludes the underscore-prefixed file), the read/content check fails.
func TestMakeInstallLinksInteractionSnippet(t *testing.T) {
	root, home := makeInstallToTemp(t)

	linked := filepath.Join(home, ".claude", "dross", "prompts", "_interaction.md")
	got, err := os.ReadFile(linked)
	if err != nil {
		t.Fatalf("installed snippet not readable at %s: %v", linked, err)
	}
	src, err := os.ReadFile(filepath.Join(root, "assets", "prompts", "_interaction.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(src) {
		t.Error("installed _interaction.md content does not match source — install linked the wrong thing")
	}
}

// TestMakeDoctorCountsInteractionSnippet proves doctor sees the installed prompts: its
// prompt section must report _interaction.md as present, not missing. Exit code is
// ignored (doctor also checks the developer's PATH/binary, which the temp install
// can't satisfy) — only the prompt-section content is asserted.
func TestMakeDoctorCountsInteractionSnippet(t *testing.T) {
	root, home := makeInstallToTemp(t)

	cmd := exec.Command("make", "doctor")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "HOME="+home)
	out, _ := cmd.CombinedOutput() // non-zero exit expected (PATH/binary checks)
	doc := string(out)

	if strings.Contains(doc, "_interaction.md missing") {
		t.Errorf("doctor reports _interaction.md missing — install didn't link it:\n%s", doc)
	}
	if !strings.Contains(doc, "prompts present") {
		t.Errorf("doctor output missing the prompts-present summary:\n%s", doc)
	}
}
