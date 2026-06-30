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
