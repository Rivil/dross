package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The release pipeline must sign checksums.txt with minisign so `dross update`'s
// signature gate (internal/update) has something to verify. These tests pin the
// goreleaser + workflow wiring so a refactor can't silently drop signing — which
// would fail-close every future self-update. YAML has no mutation adapter, so this
// is the reproducible regression guard for milestone criterion c-1.

func readRepoFile(t *testing.T, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(repoRootFromTest(t), rel))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	return string(b)
}

func TestGoreleaserSignsChecksumsWithMinisign(t *testing.T) {
	y := readRepoFile(t, ".goreleaser.yaml")
	for _, want := range []string{
		"signs:",
		"cmd: minisign",
		"artifacts: checksum",          // sign the checksums file, not every archive
		"${artifact}.minisig",          // the signature artifact dross update fetches
		"{{ .Env.MINISIGN_KEY_FILE }}", // key path injected by the workflow
		"{{ .Env.MINISIGN_PASSWORD }}", // password piped via stdin (non-interactive)
	} {
		if !strings.Contains(y, want) {
			t.Errorf(".goreleaser.yaml missing signing directive %q", want)
		}
	}
}

func TestReleaseWorkflowMaterializesKeyAndPassesEnv(t *testing.T) {
	w := readRepoFile(t, ".github/workflows/release.yml")
	for _, want := range []string{
		`"$RUNNER_TEMP/minisign.key"`,        // key written to an ephemeral path only
		"chmod 600",                          // not world-readable
		"${{ secrets.MINISIGN_SECRET_KEY }}", // sourced from a secret, never a literal
		"MINISIGN_PASSWORD: ${{ secrets.MINISIGN_PASSWORD }}",
		"MINISIGN_KEY_FILE: ${{ runner.temp }}/minisign.key",
	} {
		if !strings.Contains(w, want) {
			t.Errorf("release.yml missing signing wiring %q", want)
		}
	}
	// The secret key must never be written into the repo tree, only $RUNNER_TEMP.
	if strings.Contains(w, "> minisign.key") || strings.Contains(w, "> ./minisign.key") {
		t.Error("release.yml writes minisign.key into the working tree; it must stay in $RUNNER_TEMP")
	}
}
