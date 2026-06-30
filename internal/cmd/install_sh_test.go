package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// installShPreconditions skips when the host can't run the POSIX installer end to
// end: install.sh is sh + curl/wget based, so Windows and a missing sh/curl can't
// exercise it.
func installShPreconditions(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("install.sh is a POSIX sh script; not exercised on windows")
	}
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not on PATH")
	}
	if _, err := exec.LookPath("curl"); err != nil {
		t.Skip("curl not on PATH (install.sh download path)")
	}
}

// installShFixture builds a release fixture for install.sh: a fake `dross` binary
// (a shell script that records its install invocation) packed as the platform
// tarball, plus a checksums.txt. checksumOverride replaces the (otherwise correct)
// checksums.txt body to drive the mismatch case. Returns the serve dir, the
// version to pass via DROSS_VERSION, and the marker path the fake writes on install.
func installShFixture(t *testing.T, checksumOverride string) (serveDir, version, marker string) {
	t.Helper()
	version = "0.6.0"
	serveDir = t.TempDir()
	marker = filepath.Join(t.TempDir(), "install-ran")

	// Fake dross: on `dross install`, write the marker — proves install.sh handed
	// off to the freshly-installed binary.
	script := []byte("#!/bin/sh\nif [ \"$1\" = install ]; then echo ran > " + marker + "; fi\n")
	tarball := makeTarGz(t, "dross", script)

	asset := fmt.Sprintf("dross_%s_%s_%s.tar.gz", version, runtime.GOOS, runtime.GOARCH)
	if err := os.WriteFile(filepath.Join(serveDir, asset), tarball, 0o644); err != nil {
		t.Fatal(err)
	}
	sums := checksumOverride
	if sums == "" {
		sums = fmt.Sprintf("%s  %s\n", sha256hex(tarball), asset)
	}
	if err := os.WriteFile(filepath.Join(serveDir, "checksums.txt"), []byte(sums), 0o644); err != nil {
		t.Fatal(err)
	}
	return serveDir, version, marker
}

// runInstallSh runs `sh install.sh` against the given asset base URL, isolated to a
// temp HOME + bin dir, and returns the bin dir, the combined output, and the error.
func runInstallSh(t *testing.T, baseURL, version string) (binDir string, out []byte, err error) {
	t.Helper()
	root := repoRootFromTest(t)
	home := t.TempDir()
	binDir = filepath.Join(home, "bin")
	cmd := exec.Command("sh", filepath.Join(root, "install.sh"))
	cmd.Dir = t.TempDir() // neutral cwd, no assets/ checkout
	cmd.Env = append(os.Environ(),
		"HOME="+home,
		"DROSS_BIN_DIR="+binDir,
		"DROSS_INSTALL_BASE="+baseURL,
		"DROSS_VERSION=v"+version,
	)
	out, err = cmd.CombinedOutput()
	return binDir, out, err
}

// TestInstallShInstalls proves the happy path: install.sh downloads + verifies the
// platform tarball, places dross on the bin path, and runs `dross install`.
func TestInstallShInstalls(t *testing.T) {
	installShPreconditions(t)
	serveDir, version, marker := installShFixture(t, "")

	binDir, out, err := runInstallSh(t, "file://"+serveDir, version)
	if err != nil {
		t.Fatalf("install.sh failed: %v\n%s", err, out)
	}
	bin := filepath.Join(binDir, "dross")
	info, err := os.Stat(bin)
	if err != nil {
		t.Fatalf("dross not installed at %s: %v\n%s", bin, err, out)
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Errorf("installed dross is not executable: %v", info.Mode())
	}
	if _, err := os.Stat(marker); err != nil {
		t.Errorf("`dross install` was not run by install.sh (marker missing): %v\n%s", err, out)
	}
}

// TestInstallSh404LeavesNoBinary proves the partial-write guard: an unreachable
// download exits non-zero and leaves NO binary on the bin path (staging in temp,
// moving only after a verified download).
func TestInstallSh404LeavesNoBinary(t *testing.T) {
	installShPreconditions(t)
	// A base URL whose assets don't exist.
	missing := filepath.Join(t.TempDir(), "does-not-exist")

	binDir, out, err := runInstallSh(t, "file://"+missing, "0.6.0")
	if err == nil {
		t.Fatalf("install.sh should fail on unreachable download, got success:\n%s", out)
	}
	if _, statErr := os.Stat(filepath.Join(binDir, "dross")); !os.IsNotExist(statErr) {
		t.Errorf("partial/binary left on bin path after failed download (err=%v)", statErr)
	}
}

// TestInstallShChecksumMismatch proves install.sh refuses a tampered/corrupt
// download: a checksums.txt not matching the tarball aborts with no binary installed.
func TestInstallShChecksumMismatch(t *testing.T) {
	installShPreconditions(t)
	asset := fmt.Sprintf("dross_0.6.0_%s_%s.tar.gz", runtime.GOOS, runtime.GOARCH)
	badSums := fmt.Sprintf("%s  %s\n", "0000000000000000000000000000000000000000000000000000000000000000", asset)
	serveDir, version, marker := installShFixture(t, badSums)

	binDir, out, err := runInstallSh(t, "file://"+serveDir, version)
	if err == nil {
		t.Fatalf("install.sh should fail on checksum mismatch, got success:\n%s", out)
	}
	if _, statErr := os.Stat(filepath.Join(binDir, "dross")); !os.IsNotExist(statErr) {
		t.Errorf("binary installed despite checksum mismatch (err=%v)", statErr)
	}
	if _, statErr := os.Stat(marker); !os.IsNotExist(statErr) {
		t.Errorf("`dross install` ran despite checksum mismatch")
	}
}
