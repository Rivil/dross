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

// TestGoreleaserBuildsWindows pins the windows build target so a refactor can't drop
// it — without it there is no windows archive for `dross update` / install.ps1 to fetch.
func TestGoreleaserBuildsWindows(t *testing.T) {
	y := readRepoFile(t, ".goreleaser.yaml")
	for _, want := range []string{
		"- windows", // windows in the builds.goos matrix
		"- amd64",   // windows still built for both arches
		"- arm64",
	} {
		if !strings.Contains(y, want) {
			t.Errorf(".goreleaser.yaml missing build matrix entry %q", want)
		}
	}
}

// TestGoreleaserWindowsZipOverride pins the windows→zip archive override. dross update's
// AssetName expects a .zip on windows; a tar.gz here would 404 every windows self-update.
func TestGoreleaserWindowsZipOverride(t *testing.T) {
	y := readRepoFile(t, ".goreleaser.yaml")
	for _, want := range []string{
		"format_overrides:",
		"goos: windows",
		"formats: [zip]",
	} {
		if !strings.Contains(y, want) {
			t.Errorf(".goreleaser.yaml missing windows zip override directive %q", want)
		}
	}
}

// TestGoreleaserBrewsTap pins the Homebrew tap publish: the formula pushes to the
// Rivil/homebrew-dross tap using HOMEBREW_TAP_GITHUB_TOKEN — never a literal token.
func TestGoreleaserBrewsTap(t *testing.T) {
	y := readRepoFile(t, ".goreleaser.yaml")
	for _, want := range []string{
		"brews:",
		"name: homebrew-dross",                 // the external tap repo
		"owner: Rivil",                         // tap owner
		"{{ .Env.HOMEBREW_TAP_GITHUB_TOKEN }}", // token from env, not a literal
		`bin.install "dross"`,                  // install stanza
	} {
		if !strings.Contains(y, want) {
			t.Errorf(".goreleaser.yaml missing brews/tap directive %q", want)
		}
	}
	// The tap token must never be a hard-coded literal.
	if strings.Contains(y, "token: ghp_") || strings.Contains(y, "token: github_pat_") {
		t.Error(".goreleaser.yaml embeds a literal Homebrew tap token; it must come from HOMEBREW_TAP_GITHUB_TOKEN")
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
