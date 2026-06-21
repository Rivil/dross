package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// makeInstallToTemp runs `make install` against overridable temp BIN/SKILLS/
// PROMPTS dirs and returns the temp PROMPTS_DIR. It isolates the install from
// the developer's real ~/.claude so the test never clobbers a live setup.
func makeInstallToTemp(t *testing.T) (root, promptsDir string) {
	t.Helper()
	if _, err := exec.LookPath("make"); err != nil {
		t.Skip("make not on PATH — skipping install symlink test")
	}
	root = repoRootFromTest(t)
	tmp := t.TempDir()
	promptsDir = filepath.Join(tmp, "dross", "prompts")
	cmd := exec.Command("make", "install",
		"BIN_DIR="+filepath.Join(tmp, "bin"),
		"SKILLS_DIR="+filepath.Join(tmp, "skills"),
		"PROMPTS_DIR="+promptsDir,
	)
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("make install failed: %v\n%s", err, out)
	}
	return root, promptsDir
}

// TestMakeInstallLinksInteractionSnippet proves c-2's delivery half: after
// `make install`, the snippet resolves through the installed prompts dir and its
// content matches source. If install stops linking the prompts dir (or somehow
// excludes the new file), the readlink/content check fails.
func TestMakeInstallLinksInteractionSnippet(t *testing.T) {
	root, promptsDir := makeInstallToTemp(t)

	linked := filepath.Join(promptsDir, "_interaction.md")
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

// TestMakeDoctorCountsInteractionSnippet proves doctor sees the new partial: its
// prompt section must report _interaction.md as present, not missing. Exit code
// is ignored (doctor also checks the developer's PATH/binary, which the temp
// install can't satisfy) — only the prompt-section content is asserted.
func TestMakeDoctorCountsInteractionSnippet(t *testing.T) {
	root, promptsDir := makeInstallToTemp(t)

	cmd := exec.Command("make", "doctor",
		"BIN_DIR="+filepath.Join(filepath.Dir(filepath.Dir(promptsDir)), "bin"),
		"SKILLS_DIR="+filepath.Join(filepath.Dir(filepath.Dir(promptsDir)), "skills"),
		"PROMPTS_DIR="+promptsDir,
	)
	cmd.Dir = root
	out, _ := cmd.CombinedOutput() // non-zero exit expected (PATH/binary checks)
	doc := string(out)

	if strings.Contains(doc, "_interaction.md missing") {
		t.Errorf("doctor reports _interaction.md missing — install didn't link it:\n%s", doc)
	}
	if !strings.Contains(doc, "prompts present") {
		t.Errorf("doctor output missing the prompts-present summary:\n%s", doc)
	}
}
