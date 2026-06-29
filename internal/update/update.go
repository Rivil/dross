// Package update is the pure self-update core: it resolves the release asset name
// for the host platform, fetches the latest GitHub release, verifies a downloaded
// tarball's SHA-256 against the release checksums.txt, decides whether a candidate
// release is strictly newer than the running build, and atomically replaces the
// running binary. It deliberately holds no cobra dependency so the load-bearing
// checksum/semver/swap logic tests against an httptest server with no CLI plumbing
// (the `dross update` command in internal/cmd is a thin wrapper over this).
package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/mod/semver"
)

// Repo coordinates — the single trust root for releases.
const (
	RepoOwner = "Rivil"
	RepoName  = "dross"

	// DefaultAPIBase is the GitHub REST API root. Overridable on Client for tests.
	DefaultAPIBase = "https://api.github.com"
)

// Sentinel errors callers branch on.
var (
	// ErrChecksumMismatch is returned when a downloaded asset's SHA-256 does not
	// match its checksums.txt entry. The update path MUST refuse on this.
	ErrChecksumMismatch = errors.New("checksum mismatch")
	// ErrUnsupportedPlatform is returned when the host GOOS/GOARCH has no release build.
	ErrUnsupportedPlatform = errors.New("unsupported platform")
	// ErrNoChecksumEntry is returned when checksums.txt has no row for the asset.
	ErrNoChecksumEntry = errors.New("no checksum entry for asset")
)

// Decision is the outcome of comparing a candidate release against the running build.
type Decision int

const (
	// UpToDate — the latest release is equal to or older than the running build.
	UpToDate Decision = iota
	// UpdateAvailable — the latest release is strictly newer (semver) than the running build.
	UpdateAvailable
	// NeedsConfirm — the running build is a dev/unknown version that cannot be
	// semver-compared; the caller should offer to update anyway behind a prompt/--force.
	NeedsConfirm
)

func (d Decision) String() string {
	switch d {
	case UpdateAvailable:
		return "update-available"
	case NeedsConfirm:
		return "needs-confirm"
	default:
		return "up-to-date"
	}
}

var supportedOS = map[string]bool{"darwin": true, "linux": true}
var supportedArch = map[string]bool{"arm64": true, "amd64": true}

// AssetName returns the release tarball name for the given version and platform,
// matching the .goreleaser name_template exactly:
//
//	dross_{Version}_{Os}_{Arch}.tar.gz
//
// goreleaser's {{.Version}} is the tag without a leading "v", and {{.Os}}/{{.Arch}}
// are the lowercase GOOS/GOARCH. An unsupported platform returns ErrUnsupportedPlatform.
func AssetName(version, goos, goarch string) (string, error) {
	if !supportedOS[goos] || !supportedArch[goarch] {
		return "", fmt.Errorf("%w: %s/%s", ErrUnsupportedPlatform, goos, goarch)
	}
	v := strings.TrimPrefix(version, "v")
	return fmt.Sprintf("%s_%s_%s_%s.tar.gz", RepoName, v, goos, goarch), nil
}

// ParseChecksums parses a goreleaser checksums.txt (sha256sum format:
// "<hex-sha256>  <filename>" per line) into a filename->hex map. Malformed or
// blank lines are skipped.
func ParseChecksums(data []byte) map[string]string {
	out := map[string]string{}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		out[fields[1]] = fields[0]
	}
	return out
}

// VerifyChecksum confirms asset's SHA-256 matches the checksums entry for filename.
// Returns ErrNoChecksumEntry if filename is absent (never a silent pass) and
// ErrChecksumMismatch if the hash differs.
func VerifyChecksum(asset []byte, checksums map[string]string, filename string) error {
	want, ok := checksums[filename]
	if !ok {
		return fmt.Errorf("%w: %s", ErrNoChecksumEntry, filename)
	}
	sum := sha256.Sum256(asset)
	got := hex.EncodeToString(sum[:])
	if !strings.EqualFold(got, want) {
		return fmt.Errorf("%w: %s (got %s, want %s)", ErrChecksumMismatch, filename, got, want)
	}
	return nil
}

// Decide compares the latest release tag against the running build and returns
// whether to update. A dev/unknown build (Commit=="unknown" or a version that is
// not valid semver, e.g. the 4-part "0.1.0.0" source default) cannot be compared
// and yields NeedsConfirm. Otherwise it updates only when latest is STRICTLY newer,
// never on equal or older (no accidental downgrades).
func Decide(latestTag, currentVersion, currentCommit string) Decision {
	cur := canonicalSemver(currentVersion)
	if currentCommit == "unknown" || cur == "" {
		return NeedsConfirm
	}
	lat := canonicalSemver(latestTag)
	if lat == "" {
		return NeedsConfirm
	}
	if semver.Compare(lat, cur) > 0 {
		return UpdateAvailable
	}
	return UpToDate
}

// canonicalSemver normalizes a tag/version to a valid canonical semver ("vX.Y.Z")
// or returns "" if it is not valid semver (so 4-part dev versions are rejected).
func canonicalSemver(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return ""
	}
	if !strings.HasPrefix(v, "v") {
		v = "v" + v
	}
	if !semver.IsValid(v) {
		return ""
	}
	return semver.Canonical(v)
}

// AtomicReplace overwrites targetPath with the bytes from r without ever leaving a
// partial binary in place: it stages into a temp file in the SAME directory (so the
// final rename is atomic on one filesystem), makes it executable, then renames over
// the target. If staging fails (e.g. r errors mid-copy) the temp file is discarded
// and targetPath is left byte-for-byte unchanged.
func AtomicReplace(targetPath string, r io.Reader) (err error) {
	dir := filepath.Dir(targetPath)
	tmp, err := os.CreateTemp(dir, ".dross-update-*")
	if err != nil {
		return fmt.Errorf("stage temp: %w", err)
	}
	tmpName := tmp.Name()
	// On any failure after this point, discard the partial temp.
	defer func() {
		if err != nil {
			_ = os.Remove(tmpName)
		}
	}()

	if _, err = io.Copy(tmp, r); err != nil {
		tmp.Close()
		return fmt.Errorf("write staged binary: %w", err)
	}
	if err = tmp.Close(); err != nil {
		return fmt.Errorf("close staged binary: %w", err)
	}
	if err = os.Chmod(tmpName, 0o755); err != nil {
		return fmt.Errorf("chmod staged binary: %w", err)
	}
	if err = os.Rename(tmpName, targetPath); err != nil {
		return fmt.Errorf("replace binary: %w", err)
	}
	return nil
}

// Release is the subset of the GitHub release payload the updater needs.
type Release struct {
	TagName string  `json:"tag_name"`
	Assets  []Asset `json:"assets"`
}

// Asset is a single release asset (tarball or checksums.txt).
type Asset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
}

// AssetURL returns the download URL for the named asset, or "" if absent.
func (r Release) AssetURL(name string) string {
	for _, a := range r.Assets {
		if a.Name == name {
			return a.URL
		}
	}
	return ""
}

// Client fetches releases and assets. APIBase is injectable so tests point it at an
// httptest server standing in for api.github.com and the asset host.
type Client struct {
	APIBase string
	HTTP    *http.Client
}

// NewClient returns a Client targeting the real GitHub API.
func NewClient() *Client {
	return &Client{APIBase: DefaultAPIBase, HTTP: http.DefaultClient}
}

func (c *Client) httpClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return http.DefaultClient
}

// LatestRelease fetches the latest published release for Rivil/dross.
func (c *Client) LatestRelease(ctx context.Context) (Release, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/releases/latest", strings.TrimRight(c.APIBase, "/"), RepoOwner, RepoName)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return Release{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return Release{}, fmt.Errorf("fetch latest release: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Release{}, fmt.Errorf("fetch latest release: status %d", resp.StatusCode)
	}
	var rel Release
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return Release{}, fmt.Errorf("decode release: %w", err)
	}
	return rel, nil
}

// Download fetches the bytes at url, erroring on any non-200 status so a 404 never
// silently yields an empty/partial asset.
func (c *Client) Download(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download %s: status %d", url, resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}
