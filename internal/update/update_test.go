package update

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestAssetName pins the release tarball name to the .goreleaser name_template for
// every supported platform and rejects unsupported ones. A regression dropping the
// arch segment or mangling the os fails here.
func TestAssetName(t *testing.T) {
	cases := []struct {
		os, arch, want string
	}{
		{"darwin", "arm64", "dross_0.6.0_darwin_arm64.tar.gz"},
		{"darwin", "amd64", "dross_0.6.0_darwin_amd64.tar.gz"},
		{"linux", "arm64", "dross_0.6.0_linux_arm64.tar.gz"},
		{"linux", "amd64", "dross_0.6.0_linux_amd64.tar.gz"},
	}
	for _, c := range cases {
		got, err := AssetName("v0.6.0", c.os, c.arch)
		if err != nil {
			t.Fatalf("AssetName(%s/%s): unexpected error %v", c.os, c.arch, err)
		}
		if got != c.want {
			t.Errorf("AssetName(%s/%s) = %q, want %q", c.os, c.arch, got, c.want)
		}
	}
	// Leading "v" stripped (matches goreleaser .Version).
	if got, _ := AssetName("0.6.0", "darwin", "arm64"); got != "dross_0.6.0_darwin_arm64.tar.gz" {
		t.Errorf("AssetName without v-prefix = %q", got)
	}
	// Unsupported platform must error, not fabricate a name.
	if _, err := AssetName("v0.6.0", "windows", "amd64"); !errors.Is(err, ErrUnsupportedPlatform) {
		t.Errorf("windows/amd64: want ErrUnsupportedPlatform, got %v", err)
	}
	if _, err := AssetName("v0.6.0", "linux", "riscv64"); !errors.Is(err, ErrUnsupportedPlatform) {
		t.Errorf("linux/riscv64: want ErrUnsupportedPlatform, got %v", err)
	}
}

func sha256hex(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

// TestVerifyChecksum proves the checksum gate: match -> nil; one flipped byte ->
// ErrChecksumMismatch (never nil); a checksums.txt omitting our filename -> error,
// not a silent pass.
func TestVerifyChecksum(t *testing.T) {
	asset := []byte("the-tarball-bytes")
	name := "dross_0.6.0_darwin_arm64.tar.gz"
	other := "dross_0.6.0_linux_amd64.tar.gz"
	checksums := ParseChecksums([]byte(fmt.Sprintf("%s  %s\n%s  %s\n",
		sha256hex(asset), name, sha256hex([]byte("other")), other)))

	if err := VerifyChecksum(asset, checksums, name); err != nil {
		t.Errorf("matching asset: want nil, got %v", err)
	}

	tampered := append([]byte{}, asset...)
	tampered[0] ^= 0xff
	if err := VerifyChecksum(tampered, checksums, name); !errors.Is(err, ErrChecksumMismatch) {
		t.Errorf("tampered asset: want ErrChecksumMismatch, got %v", err)
	}

	if err := VerifyChecksum(asset, checksums, "dross_0.6.0_windows_amd64.tar.gz"); err == nil {
		t.Error("missing filename: want error, got nil (silent pass)")
	} else if !errors.Is(err, ErrNoChecksumEntry) {
		t.Errorf("missing filename: want ErrNoChecksumEntry, got %v", err)
	}
}

// TestNewerOnly proves the version policy: strictly-newer -> UpdateAvailable;
// equal or older -> UpToDate (no downgrade); a dev/unknown running build -> NeedsConfirm.
func TestNewerOnly(t *testing.T) {
	cases := []struct {
		name                       string
		latest, version, commit    string
		want                       Decision
	}{
		{"strictly newer", "v0.6.1", "0.6.0", "abc1234", UpdateAvailable},
		{"newer minor", "v0.7.0", "0.6.9", "abc1234", UpdateAvailable},
		{"equal", "v0.6.0", "0.6.0", "abc1234", UpToDate},
		{"older latest", "v0.5.0", "0.6.0", "abc1234", UpToDate},
		{"dev commit unknown", "v0.6.1", "0.6.0", "unknown", NeedsConfirm},
		{"four-part dev version", "v0.6.1", "0.1.0.0", "abc1234", NeedsConfirm},
		{"tag with and without v compare equal", "0.6.0", "v0.6.0", "abc1234", UpToDate},
	}
	for _, c := range cases {
		if got := Decide(c.latest, c.version, c.commit); got != c.want {
			t.Errorf("%s: Decide(%q,%q,%q) = %v, want %v", c.name, c.latest, c.version, c.commit, got, c.want)
		}
	}
}

type errReader struct {
	data []byte
	n    int
}

// Read yields data once, then errors — simulating a download that fails mid-stream.
func (e *errReader) Read(p []byte) (int, error) {
	if e.n >= len(e.data) {
		return 0, errors.New("simulated mid-stream failure")
	}
	n := copy(p, e.data[e.n:])
	e.n += n
	return n, nil
}

// TestAtomicReplace proves the swap never bricks the binary: success replaces the
// bytes and leaves an executable file; a staging failure leaves the original
// byte-for-byte intact with no partial temp left behind.
func TestAtomicReplace(t *testing.T) {
	t.Run("success replaces and is executable", func(t *testing.T) {
		dir := t.TempDir()
		target := filepath.Join(dir, "dross")
		if err := os.WriteFile(target, []byte("OLD-BINARY"), 0o755); err != nil {
			t.Fatal(err)
		}
		newBytes := []byte("NEW-BINARY-CONTENT")
		if err := AtomicReplace(target, bytes.NewReader(newBytes)); err != nil {
			t.Fatalf("AtomicReplace: %v", err)
		}
		got, err := os.ReadFile(target)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, newBytes) {
			t.Errorf("target bytes = %q, want %q", got, newBytes)
		}
		info, err := os.Stat(target)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm()&0o111 == 0 {
			t.Errorf("target not executable: mode %v", info.Mode())
		}
		assertNoTempLeft(t, dir)
	})

	t.Run("staging failure preserves original", func(t *testing.T) {
		dir := t.TempDir()
		target := filepath.Join(dir, "dross")
		orig := []byte("ORIGINAL-BINARY")
		if err := os.WriteFile(target, orig, 0o755); err != nil {
			t.Fatal(err)
		}
		err := AtomicReplace(target, &errReader{data: []byte("partial")})
		if err == nil {
			t.Fatal("want error from failed staging, got nil")
		}
		got, err := os.ReadFile(target)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, orig) {
			t.Errorf("target corrupted after failed replace: got %q, want %q", got, orig)
		}
		assertNoTempLeft(t, dir)
	})
}

func assertNoTempLeft(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".dross-update-") {
			t.Errorf("partial temp file left behind: %s", e.Name())
		}
	}
}

// TestClientFetchAndVerify exercises the HTTP path against an httptest server
// standing in for api.github.com + the asset host: LatestRelease parses the tag and
// asset URLs, Download fetches bytes (and 404s error), and the fetched tarball
// verifies against the served checksums.txt.
func TestClientFetchAndVerify(t *testing.T) {
	tarball := []byte("tarball-payload")
	assetName := "dross_0.6.1_linux_amd64.tar.gz"
	checksums := fmt.Sprintf("%s  %s\n", sha256hex(tarball), assetName)

	mux := http.NewServeMux()
	var base string
	mux.HandleFunc("/repos/Rivil/dross/releases/latest", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, `{"tag_name":"v0.6.1","assets":[
			{"name":%q,"browser_download_url":%q},
			{"name":"checksums.txt","browser_download_url":%q}
		]}`, assetName, base+"/dl/"+assetName, base+"/dl/checksums.txt")
	})
	mux.HandleFunc("/dl/"+assetName, func(w http.ResponseWriter, _ *http.Request) { w.Write(tarball) })
	mux.HandleFunc("/dl/checksums.txt", func(w http.ResponseWriter, _ *http.Request) { io.WriteString(w, checksums) })
	srv := httptest.NewServer(mux)
	defer srv.Close()
	base = srv.URL

	c := &Client{APIBase: srv.URL, HTTP: srv.Client()}
	rel, err := c.LatestRelease(context.Background())
	if err != nil {
		t.Fatalf("LatestRelease: %v", err)
	}
	if rel.TagName != "v0.6.1" {
		t.Errorf("tag = %q, want v0.6.1", rel.TagName)
	}
	tbURL := rel.AssetURL(assetName)
	if tbURL == "" {
		t.Fatalf("no asset URL for %s", assetName)
	}

	gotTar, err := c.Download(context.Background(), tbURL)
	if err != nil {
		t.Fatalf("Download tarball: %v", err)
	}
	gotSums, err := c.Download(context.Background(), rel.AssetURL("checksums.txt"))
	if err != nil {
		t.Fatalf("Download checksums: %v", err)
	}
	if err := VerifyChecksum(gotTar, ParseChecksums(gotSums), assetName); err != nil {
		t.Errorf("verify served tarball: %v", err)
	}

	// A 404 must error, never yield empty bytes silently.
	if _, err := c.Download(context.Background(), base+"/dl/missing.tar.gz"); err == nil {
		t.Error("Download of missing asset: want error, got nil")
	}
}
