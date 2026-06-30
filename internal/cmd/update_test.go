package cmd

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/update"
)

// makeTarGz packs a single regular file `name` with the given bytes (mode 0755).
func makeTarGz(t *testing.T, name string, content []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(content)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func sha256hex(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

// releaseServer stands in for api.github.com + the asset host. tag is the release
// tag; the served "dross" binary is `binary`; checksumsBody overrides the served
// checksums.txt (empty -> a correct one for the served tarball).
func releaseServer(t *testing.T, tag string, binary []byte, checksumsBody string) (*httptest.Server, string) {
	t.Helper()
	assetName, err := update.AssetName(tag, runtime.GOOS, runtime.GOARCH)
	if err != nil {
		t.Fatal(err)
	}
	tarball := makeTarGz(t, "dross", binary)
	sums := checksumsBody
	if sums == "" {
		sums = fmt.Sprintf("%s  %s\n", sha256hex(tarball), assetName)
	}

	mux := http.NewServeMux()
	var base string
	mux.HandleFunc("/repos/Rivil/dross/releases/latest", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, `{"tag_name":%q,"assets":[
			{"name":%q,"browser_download_url":%q},
			{"name":"checksums.txt","browser_download_url":%q}
		]}`, tag, assetName, base+"/dl/"+assetName, base+"/dl/checksums.txt")
	})
	mux.HandleFunc("/dl/"+assetName, func(w http.ResponseWriter, _ *http.Request) { w.Write(tarball) })
	mux.HandleFunc("/dl/checksums.txt", func(w http.ResponseWriter, _ *http.Request) { io.WriteString(w, sums) })
	srv := httptest.NewServer(mux)
	base = srv.URL
	return srv, assetName
}

// newBinaryScript returns a shell "binary" that, when run as `<self> install`, writes
// a marker file — so a successful run proves the FRESHLY-SWAPPED binary executed the
// asset re-sync, not the old in-process engine.
func newBinaryScript(markerPath string) []byte {
	return []byte("#!/bin/sh\nif [ \"$1\" = install ]; then echo synced-by-new-binary > " + markerPath + "; fi\n")
}

func TestUpdateAppliesAndResyncs(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake binary is unix-only")
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "dross")
	if err := os.WriteFile(target, []byte("OLD-BINARY"), 0o755); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(dir, "resync-marker")
	newBin := newBinaryScript(marker)

	srv, _ := releaseServer(t, "v0.6.1", newBin, "")
	defer srv.Close()

	var out bytes.Buffer
	err := runUpdate(context.Background(), updateOpts{
		out: &out, apiBase: srv.URL, httpClient: srv.Client(),
		version: "0.6.0", commit: "abc1234", targetPath: target,
	})
	if err != nil {
		t.Fatalf("runUpdate: %v\n%s", err, out.String())
	}

	got, _ := os.ReadFile(target)
	if !bytes.Equal(got, newBin) {
		t.Errorf("binary not replaced with the new release bytes")
	}
	// Re-sync ran the NEW (swapped) binary: the marker only the new binary writes exists.
	mb, err := os.ReadFile(marker)
	if err != nil {
		t.Fatalf("re-sync marker not written — new binary was not exec'd for install: %v", err)
	}
	if strings.TrimSpace(string(mb)) != "synced-by-new-binary" {
		t.Errorf("marker content = %q", mb)
	}
}

func TestUpdateCheckNoApply(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "dross")
	orig := []byte("RUNNING-BINARY")
	if err := os.WriteFile(target, orig, 0o755); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(dir, "resync-marker")

	srv, _ := releaseServer(t, "v0.9.0", newBinaryScript(marker), "")
	defer srv.Close()

	var out bytes.Buffer
	resyncCalled := false
	err := runUpdate(context.Background(), updateOpts{
		out: &out, apiBase: srv.URL, httpClient: srv.Client(), check: true,
		version: "0.6.0", commit: "abc1234", targetPath: target,
		resync: func(string) error { resyncCalled = true; return nil },
	})
	if err != nil {
		t.Fatalf("runUpdate --check: %v", err)
	}
	if !strings.Contains(out.String(), "v0.9.0") {
		t.Errorf("--check did not report the available version:\n%s", out.String())
	}
	if got, _ := os.ReadFile(target); !bytes.Equal(got, orig) {
		t.Errorf("--check modified the binary")
	}
	if resyncCalled {
		t.Errorf("--check ran the asset re-sync (should leave assets untouched)")
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Errorf("--check produced a resync marker")
	}
}

func TestUpdateForce(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake binary is unix-only")
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "dross")
	if err := os.WriteFile(target, []byte("OLD"), 0o755); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(dir, "resync-marker")
	newBin := newBinaryScript(marker)

	// Latest equals the running version: without --force this would be a no-op.
	srv, _ := releaseServer(t, "v0.6.0", newBin, "")
	defer srv.Close()

	var out bytes.Buffer
	err := runUpdate(context.Background(), updateOpts{
		out: &out, apiBase: srv.URL, httpClient: srv.Client(), force: true,
		version: "0.6.0", commit: "abc1234", targetPath: target,
	})
	if err != nil {
		t.Fatalf("runUpdate --force: %v\n%s", err, out.String())
	}
	if got, _ := os.ReadFile(target); !bytes.Equal(got, newBin) {
		t.Errorf("--force on equal version did not reinstall")
	}
	if _, err := os.Stat(marker); err != nil {
		t.Errorf("--force did not re-sync: %v", err)
	}
}

func TestUpdateRefusesOnBadChecksum(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "dross")
	orig := []byte("ORIGINAL-BINARY")
	if err := os.WriteFile(target, orig, 0o755); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(dir, "resync-marker")
	assetName, _ := update.AssetName("v0.6.1", runtime.GOOS, runtime.GOARCH)
	// checksums.txt with a hash that does NOT match the served tarball (MITM/corruption).
	badSums := fmt.Sprintf("%s  %s\n", strings.Repeat("0", 64), assetName)

	srv, _ := releaseServer(t, "v0.6.1", newBinaryScript(marker), badSums)
	defer srv.Close()

	var out bytes.Buffer
	resyncCalled := false
	err := runUpdate(context.Background(), updateOpts{
		out: &out, apiBase: srv.URL, httpClient: srv.Client(),
		version: "0.6.0", commit: "abc1234", targetPath: target,
		resync: func(string) error { resyncCalled = true; return nil },
	})
	if err == nil {
		t.Fatal("bad checksum: want error, got nil")
	}
	if got, _ := os.ReadFile(target); !bytes.Equal(got, orig) {
		t.Errorf("bad checksum: binary was modified despite refusal")
	}
	if resyncCalled {
		t.Errorf("bad checksum: re-sync ran despite refusal")
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Errorf("bad checksum: swap/resync reached end-to-end")
	}
}
