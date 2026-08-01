package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestReadmeDocumentsInstallAndUpdate is c-5's guard: the README must document the
// supported install (curl|sh of install.sh) and update (`dross update`) flows. If the
// installer entrypoint or the update command is renamed without updating the docs,
// these greps fail.
func TestReadmeDocumentsInstallAndUpdate(t *testing.T) {
	root := repoRootFromTest(t)
	b, err := os.ReadFile(filepath.Join(root, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	readme := string(b)

	// The curl|sh one-liner: raw githubusercontent URL for Rivil/dross's install.sh, piped to sh.
	if !strings.Contains(readme, "raw.githubusercontent.com/Rivil/dross") {
		t.Error("README missing the raw.githubusercontent.com/Rivil/dross install URL")
	}
	if !strings.Contains(readme, "install.sh | sh") {
		t.Error("README missing the `install.sh | sh` one-liner (installer entrypoint renamed?)")
	}
	if !strings.Contains(readme, "curl -fsSL") {
		t.Error("README curl one-liner missing the -fsSL flags")
	}

	// The self-update command must be documented.
	if !strings.Contains(readme, "dross update") {
		t.Error("README does not document `dross update`")
	}
}

// TestReadmeDocumentsBaseTruthSurfaces (c-6) is the same grep-needle guard for
// the surfaces this milestone's base-truth work added: the machine-local store
// and completion's explicit base. Both change how a user recovers from a
// refusal, so an undocumented one is a support burden, not a cosmetic gap.
func TestReadmeDocumentsBaseTruthSurfaces(t *testing.T) {
	root := repoRootFromTest(t)
	b, err := os.ReadFile(filepath.Join(root, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	readme := string(b)

	if !strings.Contains(readme, "dross local") {
		t.Error("README does not document `dross local` (the machine-local store)")
	}
	if !strings.Contains(readme, "quick_base") {
		t.Error("README does not name `quick_base`, the store's first key")
	}
	// The completion flag, in the phase row that describes `complete`.
	phaseRow := ""
	for _, line := range strings.Split(readme, "\n") {
		if strings.HasPrefix(line, "| `dross phase {") {
			phaseRow = line
		}
	}
	if phaseRow == "" {
		t.Fatal("README no longer has a `dross phase {...}` command row to check")
	}
	if !strings.Contains(phaseRow, "--base") {
		t.Errorf("the phase row must document complete's --base escape hatch: %s", phaseRow)
	}
	if !strings.Contains(phaseRow, "--recover") {
		t.Errorf("the phase row must document complete's --recover behaviour: %s", phaseRow)
	}
}
