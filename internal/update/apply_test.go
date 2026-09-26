package update

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"aead.dev/minisign"
)

// apply_test.go holds Apply's end-to-end tests, moved here from internal/cmd with
// the flow itself. sha256hex and genKey are this package's existing helpers.

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

// makeZip packs a single file `name` with the given bytes into a zip archive — the
// windows release format goreleaser emits via format_overrides.
func makeZip(t *testing.T, name string, content []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// trustKey overrides TrustedMinisignKey for the duration of the test and
// restores it via t.Cleanup.
func trustKey(t *testing.T, pubKeyText string) {
	t.Helper()
	orig := TrustedMinisignKey
	TrustedMinisignKey = pubKeyText
	t.Cleanup(func() { TrustedMinisignKey = orig })
}

// releaseServer stands in for api.github.com + the asset host. tag is the release
// tag; the served "dross" binary is `binary`; checksumsBody overrides the served
// checksums.txt (empty -> a correct one for the served tarball). When serveSig is
// true it also generates a throwaway minisign key, signs the EXACT served
// checksums.txt bytes with it, serves the signature at /dl/checksums.txt.minisig,
// and lists that asset in the release JSON. The returned string is the base64
// public-key line of the signing key (feed it to trustKey for a valid-signature run).
func releaseServer(t *testing.T, tag string, binary []byte, checksumsBody string, serveSig bool) (*httptest.Server, string, string) {
	t.Helper()
	assetName, err := AssetName(tag, runtime.GOOS, runtime.GOARCH)
	if err != nil {
		t.Fatal(err)
	}
	tarball := makeTarGz(t, "dross", binary)
	sums := checksumsBody
	if sums == "" {
		sums = fmt.Sprintf("%s  %s\n", sha256hex(tarball), assetName)
	}

	pub, priv := genKey(t)
	sig := minisign.Sign(priv, []byte(sums))

	mux := http.NewServeMux()
	var base string
	mux.HandleFunc("/repos/Rivil/dross/releases/latest", func(w http.ResponseWriter, _ *http.Request) {
		sigAsset := ""
		if serveSig {
			sigAsset = fmt.Sprintf(`,
			{"name":"checksums.txt.minisig","browser_download_url":%q}`, base+"/dl/checksums.txt.minisig")
		}
		fmt.Fprintf(w, `{"tag_name":%q,"assets":[
			{"name":%q,"browser_download_url":%q},
			{"name":"checksums.txt","browser_download_url":%q}%s
		]}`, tag, assetName, base+"/dl/"+assetName, base+"/dl/checksums.txt", sigAsset)
	})
	mux.HandleFunc("/dl/"+assetName, func(w http.ResponseWriter, _ *http.Request) { w.Write(tarball) })
	mux.HandleFunc("/dl/checksums.txt", func(w http.ResponseWriter, _ *http.Request) { io.WriteString(w, sums) })
	mux.HandleFunc("/dl/checksums.txt.minisig", func(w http.ResponseWriter, _ *http.Request) { w.Write(sig) })
	srv := httptest.NewServer(mux)
	base = srv.URL
	return srv, assetName, pub.String()
}

// windowsReleaseServer is the windows analog of releaseServer: it serves a signed
// release whose asset is a .zip named dross_<v>_windows_amd64.zip containing
// dross.exe. This exercises the zip extraction + windows binary-name path without a
// windows host. checksumsBody empty -> a correct checksum for the served zip; when
// non-empty it is served verbatim (still validly signed) so the checksum gate can be
// exercised on the zip path. Returns the server, the asset name, and the signing
// key's public-key line (feed to trustKey for a valid-signature run).
func windowsReleaseServer(t *testing.T, tag string, binaryExe []byte, checksumsBody string) (*httptest.Server, string, string) {
	t.Helper()
	assetName, err := AssetName(tag, "windows", "amd64")
	if err != nil {
		t.Fatal(err)
	}
	zipBytes := makeZip(t, "dross.exe", binaryExe)
	sums := checksumsBody
	if sums == "" {
		sums = fmt.Sprintf("%s  %s\n", sha256hex(zipBytes), assetName)
	}

	pub, priv := genKey(t)
	sig := minisign.Sign(priv, []byte(sums))

	mux := http.NewServeMux()
	var base string
	mux.HandleFunc("/repos/Rivil/dross/releases/latest", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, `{"tag_name":%q,"assets":[
			{"name":%q,"browser_download_url":%q},
			{"name":"checksums.txt","browser_download_url":%q},
			{"name":"checksums.txt.minisig","browser_download_url":%q}
		]}`, tag, assetName, base+"/dl/"+assetName, base+"/dl/checksums.txt", base+"/dl/checksums.txt.minisig")
	})
	mux.HandleFunc("/dl/"+assetName, func(w http.ResponseWriter, _ *http.Request) { w.Write(zipBytes) })
	mux.HandleFunc("/dl/checksums.txt", func(w http.ResponseWriter, _ *http.Request) { io.WriteString(w, sums) })
	mux.HandleFunc("/dl/checksums.txt.minisig", func(w http.ResponseWriter, _ *http.Request) { w.Write(sig) })
	srv := httptest.NewServer(mux)
	base = srv.URL
	return srv, assetName, pub.String()
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

	srv, _, pubKey := releaseServer(t, "v0.6.1", newBin, "", true)
	defer srv.Close()
	trustKey(t, pubKey)

	var out bytes.Buffer
	err := Apply(context.Background(), Options{
		Out: &out, APIBase: srv.URL, HTTP: srv.Client(),
		Version: "0.6.0", Commit: "abc1234", TargetPath: target,
	})
	if err != nil {
		t.Fatalf("Apply: %v\n%s", err, out.String())
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

// TestUpdateAppliesWindowsZip drives the windows self-update path from a non-windows
// host: Apply with GOOS=windows/amd64 downloads a validly-signed .zip, verifies
// signature+checksum, extracts dross.exe, and atomically swaps the target. The resync
// is stubbed (we cannot execute a windows binary here) so the assertion is purely that
// the extracted-and-swapped bytes are the served dross.exe.
func TestUpdateAppliesWindowsZip(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "dross.exe")
	if err := os.WriteFile(target, []byte("OLD-WINDOWS-BINARY"), 0o755); err != nil {
		t.Fatal(err)
	}
	newExe := []byte("MZ\x00\x00 fake-windows-pe dross.exe")

	srv, assetName, pubKey := windowsReleaseServer(t, "v0.6.1", newExe, "")
	defer srv.Close()
	trustKey(t, pubKey)
	if !strings.HasSuffix(assetName, ".zip") {
		t.Fatalf("windows asset %q is not a .zip", assetName)
	}

	var out bytes.Buffer
	resyncCalled := false
	err := Apply(context.Background(), Options{
		Out: &out, APIBase: srv.URL, HTTP: srv.Client(),
		Version: "0.6.0", Commit: "abc1234", TargetPath: target,
		GOOS: "windows", GOARCH: "amd64",
		Resync: func(string) error { resyncCalled = true; return nil },
	})
	if err != nil {
		t.Fatalf("Apply windows: %v\n%s", err, out.String())
	}
	got, _ := os.ReadFile(target)
	if !bytes.Equal(got, newExe) {
		t.Errorf("windows binary not replaced with the extracted dross.exe bytes")
	}
	if !resyncCalled {
		t.Errorf("windows update did not reach the resync step")
	}
}

// TestUpdateRefusesOnBadChecksumWindowsZip proves the checksum gate still fires on the
// zip path: a validly-signed checksums.txt whose hash does NOT match the served zip is
// refused at the checksum stage, before any extraction/swap — no zip bypass.
func TestUpdateRefusesOnBadChecksumWindowsZip(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "dross.exe")
	orig := []byte("ORIGINAL-WINDOWS-BINARY")
	if err := os.WriteFile(target, orig, 0o755); err != nil {
		t.Fatal(err)
	}
	assetName, _ := AssetName("v0.6.1", "windows", "amd64")
	badSums := fmt.Sprintf("%s  %s\n", strings.Repeat("0", 64), assetName)

	srv, _, pubKey := windowsReleaseServer(t, "v0.6.1", []byte("MZ new-exe"), badSums)
	defer srv.Close()
	trustKey(t, pubKey)

	var out bytes.Buffer
	resyncCalled := false
	err := Apply(context.Background(), Options{
		Out: &out, APIBase: srv.URL, HTTP: srv.Client(),
		Version: "0.6.0", Commit: "abc1234", TargetPath: target,
		GOOS: "windows", GOARCH: "amd64",
		Resync: func(string) error { resyncCalled = true; return nil },
	})
	if err == nil {
		t.Fatal("windows bad checksum: want error, got nil")
	}
	if !errors.Is(err, ErrChecksumMismatch) {
		t.Errorf("windows bad checksum: want ErrChecksumMismatch, got %v", err)
	}
	if got, _ := os.ReadFile(target); !bytes.Equal(got, orig) {
		t.Errorf("windows bad checksum: binary was modified despite refusal")
	}
	if resyncCalled {
		t.Errorf("windows bad checksum: re-sync ran despite refusal")
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

	srv, _, _ := releaseServer(t, "v0.9.0", newBinaryScript(marker), "", true)
	defer srv.Close()

	var out bytes.Buffer
	resyncCalled := false
	err := Apply(context.Background(), Options{
		Out: &out, APIBase: srv.URL, HTTP: srv.Client(), Check: true,
		Version: "0.6.0", Commit: "abc1234", TargetPath: target,
		Resync: func(string) error { resyncCalled = true; return nil },
	})
	if err != nil {
		t.Fatalf("Apply --check: %v", err)
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

	// Latest equals the running Version: without --force this would be a no-op.
	srv, _, pubKey := releaseServer(t, "v0.6.0", newBin, "", true)
	defer srv.Close()
	trustKey(t, pubKey)

	var out bytes.Buffer
	err := Apply(context.Background(), Options{
		Out: &out, APIBase: srv.URL, HTTP: srv.Client(), Force: true,
		Version: "0.6.0", Commit: "abc1234", TargetPath: target,
	})
	if err != nil {
		t.Fatalf("Apply --force: %v\n%s", err, out.String())
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
	assetName, _ := AssetName("v0.6.1", runtime.GOOS, runtime.GOARCH)
	// checksums.txt with a hash that does NOT match the served tarball (MITM/corruption).
	// It is still VALIDLY signed, so it passes the outer signature gate and must be
	// refused at the inner checksum stage — proving the checksum check still bites.
	badSums := fmt.Sprintf("%s  %s\n", strings.Repeat("0", 64), assetName)

	srv, _, pubKey := releaseServer(t, "v0.6.1", newBinaryScript(marker), badSums, true)
	defer srv.Close()
	trustKey(t, pubKey)

	var out bytes.Buffer
	resyncCalled := false
	err := Apply(context.Background(), Options{
		Out: &out, APIBase: srv.URL, HTTP: srv.Client(),
		Version: "0.6.0", Commit: "abc1234", TargetPath: target,
		Resync: func(string) error { resyncCalled = true; return nil },
	})
	if err == nil {
		t.Fatal("bad checksum: want error, got nil")
	}
	if !errors.Is(err, ErrChecksumMismatch) {
		t.Errorf("bad checksum: want ErrChecksumMismatch (refused at checksum stage), got %v", err)
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

func TestUpdateRefusesOnMissingSignature(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "dross")
	orig := []byte("ORIGINAL-BINARY")
	if err := os.WriteFile(target, orig, 0o755); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(dir, "resync-marker")

	// serveSig=false: the release lists no checksums.txt.minisig asset (fail-closed).
	srv, _, pubKey := releaseServer(t, "v0.6.1", newBinaryScript(marker), "", false)
	defer srv.Close()
	trustKey(t, pubKey)

	var out bytes.Buffer
	resyncCalled := false
	err := Apply(context.Background(), Options{
		Out: &out, APIBase: srv.URL, HTTP: srv.Client(),
		Version: "0.6.0", Commit: "abc1234", TargetPath: target,
		Resync: func(string) error { resyncCalled = true; return nil },
	})
	if err == nil {
		t.Fatal("missing signature: want error, got nil")
	}
	if !errors.Is(err, ErrNoSignature) {
		t.Errorf("missing signature: want ErrNoSignature, got %v", err)
	}
	if got, _ := os.ReadFile(target); !bytes.Equal(got, orig) {
		t.Errorf("missing signature: binary was modified despite refusal")
	}
	if resyncCalled {
		t.Errorf("missing signature: re-sync ran despite refusal")
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Errorf("missing signature: swap/resync reached end-to-end")
	}
}

func TestUpdateRefusesOnWrongKeySignature(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "dross")
	orig := []byte("ORIGINAL-BINARY")
	if err := os.WriteFile(target, orig, 0o755); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(dir, "resync-marker")

	// The server signs with its own key, but we trust a DIFFERENT key — the .minisig
	// is present and well-formed yet was made by a non-trusted key.
	srv, _, _ := releaseServer(t, "v0.6.1", newBinaryScript(marker), "", true)
	defer srv.Close()
	otherPub, _ := genKey(t)
	trustKey(t, otherPub.String())

	var out bytes.Buffer
	resyncCalled := false
	err := Apply(context.Background(), Options{
		Out: &out, APIBase: srv.URL, HTTP: srv.Client(),
		Version: "0.6.0", Commit: "abc1234", TargetPath: target,
		Resync: func(string) error { resyncCalled = true; return nil },
	})
	if err == nil {
		t.Fatal("wrong-key signature: want error, got nil")
	}
	if !errors.Is(err, ErrSignatureMismatch) {
		t.Errorf("wrong-key signature: want ErrSignatureMismatch, got %v", err)
	}
	if got, _ := os.ReadFile(target); !bytes.Equal(got, orig) {
		t.Errorf("wrong-key signature: binary was modified despite refusal")
	}
	if resyncCalled {
		t.Errorf("wrong-key signature: re-sync ran despite refusal")
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Errorf("wrong-key signature: swap/resync reached end-to-end")
	}
}
