package update

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// Options is one `dross update` run: the command's flags plus the seams tests
// inject. Zero-valued seams fall back to production defaults — the host's
// GOOS/GOARCH, os.Executable, an exec of the new binary. Version and Commit
// are the running build's; the caller supplies them, since this package does
// not know which binary it was linked into.
type Options struct {
	Out     io.Writer
	APIBase string       // overrides DefaultAPIBase when set
	HTTP    *http.Client // overrides the client's default when set

	Version string // running version
	Commit  string // running commit
	GOOS    string // target OS; defaults to runtime.GOOS
	GOARCH  string // target arch; defaults to runtime.GOARCH

	TargetPath string                       // binary to replace; defaults to os.Executable()
	Check      bool                         // report the available version without updating
	Force      bool                         // reinstall even when the release is not newer
	Resync     func(newBinary string) error // asset re-sync; defaults to `<newBinary> install`
}

// Apply fetches the latest release, verifies the minisign signature over
// checksums.txt and then the archive's SHA-256 against it (refusing on either
// failing), atomically replaces the target binary when the release is strictly
// newer (or always, with Force), then re-syncs the embedded assets by exec'ing
// the FRESHLY-SWAPPED binary — never the in-process install engine, which
// would re-materialize the OLD binary's embedded assets.
func Apply(ctx context.Context, o Options) error {
	if ctx == nil {
		ctx = context.Background()
	}
	goos := o.GOOS
	if goos == "" {
		goos = runtime.GOOS
	}
	goarch := o.GOARCH
	if goarch == "" {
		goarch = runtime.GOARCH
	}

	client := NewClient()
	if o.APIBase != "" {
		client.APIBase = o.APIBase
	}
	if o.HTTP != nil {
		client.HTTP = o.HTTP
	}

	rel, err := client.LatestRelease(ctx)
	if err != nil {
		return err
	}
	decision := Decide(rel.TagName, o.Version, o.Commit)
	fmt.Fprintf(o.Out, "current: %s\nlatest:  %s\n", o.Version, rel.TagName)

	if o.Check {
		switch decision {
		case UpdateAvailable:
			fmt.Fprintln(o.Out, "update available — run `dross update` to apply")
		case NeedsConfirm:
			fmt.Fprintln(o.Out, "running a dev/unknown build — use `dross update --force` to install the latest")
		default:
			fmt.Fprintln(o.Out, "up to date")
		}
		return nil
	}

	if !(o.Force || decision == UpdateAvailable) {
		switch decision {
		case NeedsConfirm:
			fmt.Fprintf(o.Out, "running a dev/unknown build; not updating. Use --force to install %s.\n", rel.TagName)
		default:
			fmt.Fprintln(o.Out, "already up to date.")
		}
		return nil
	}

	assetName, err := AssetName(rel.TagName, goos, goarch)
	if err != nil {
		return err
	}
	tbURL := rel.AssetURL(assetName)
	if tbURL == "" {
		return fmt.Errorf("release %s has no asset %s", rel.TagName, assetName)
	}
	sumsURL := rel.AssetURL("checksums.txt")
	if sumsURL == "" {
		return fmt.Errorf("release %s has no checksums.txt", rel.TagName)
	}

	tarball, err := client.Download(ctx, tbURL)
	if err != nil {
		return err
	}
	sums, err := client.Download(ctx, sumsURL)
	if err != nil {
		return err
	}
	// Outer trust gate: verify the minisign signature over checksums.txt against the
	// embedded public key BEFORE trusting any of its hashes and BEFORE touching any
	// binary. A missing .minisig is fail-closed (every release from this version on is
	// signed), so an absent signature is treated as tampering, not an unsigned release.
	sigURL := rel.AssetURL("checksums.txt.minisig")
	if sigURL == "" {
		return fmt.Errorf("refusing update: %w", ErrNoSignature)
	}
	sig, err := client.Download(ctx, sigURL)
	if err != nil {
		return err
	}
	if err := VerifySignature(sums, sig, TrustedMinisignKey); err != nil {
		return fmt.Errorf("refusing update: %w", err)
	}
	if err := VerifyChecksum(tarball, ParseChecksums(sums), assetName); err != nil {
		return fmt.Errorf("refusing update: %w", err)
	}

	// Dispatch on the archive format goreleaser published for this OS: windows
	// ships a .zip containing dross.exe; every other platform a .tar.gz with dross.
	// Extraction happens only AFTER the signature+checksum trust gate above.
	binName := BinaryName(goos)
	var binBytes []byte
	if strings.HasSuffix(assetName, ".zip") {
		binBytes, err = extractBinaryZip(tarball, binName)
	} else {
		binBytes, err = extractBinary(tarball, binName)
	}
	if err != nil {
		return err
	}

	targetPath := o.TargetPath
	if targetPath == "" {
		exe, err := os.Executable()
		if err != nil {
			return fmt.Errorf("resolve executable: %w", err)
		}
		if resolved, err := filepath.EvalSymlinks(exe); err == nil {
			exe = resolved
		}
		targetPath = exe
	}
	if err := AtomicReplace(targetPath, bytes.NewReader(binBytes)); err != nil {
		return err
	}
	fmt.Fprintf(o.Out, "updated %s → %s\n", targetPath, rel.TagName)

	resync := o.Resync
	if resync == nil {
		resync = func(newBinary string) error {
			//dross:exec-exempt self-exec of the binary just downloaded and minisign-verified above; signature verification is what makes this argv trusted, and "install" is a literal
			cmd := exec.Command(newBinary, "install")
			cmd.Stdout = o.Out
			cmd.Stderr = o.Out
			return cmd.Run()
		}
	}
	if err := resync(targetPath); err != nil {
		return fmt.Errorf("binary updated but asset re-sync failed: %w", err)
	}
	fmt.Fprintln(o.Out, "re-synced assets from the updated binary.")
	return nil
}

// extractBinary pulls the regular file whose base name is `name` out of a gzipped tar.
func extractBinary(targz []byte, name string) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(targz))
	if err != nil {
		return nil, fmt.Errorf("gunzip release archive: %w", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read release archive: %w", err)
		}
		if h.Typeflag == tar.TypeReg && filepath.Base(h.Name) == name {
			return io.ReadAll(tr)
		}
	}
	return nil, fmt.Errorf("binary %q not found in release archive", name)
}

// extractBinaryZip pulls the file whose base name is `name` out of a zip archive
// (the windows release format). It mirrors extractBinary but for archive/zip.
func extractBinaryZip(zipBytes []byte, name string) ([]byte, error) {
	zr, err := zip.NewReader(bytes.NewReader(zipBytes), int64(len(zipBytes)))
	if err != nil {
		return nil, fmt.Errorf("open zip release archive: %w", err)
	}
	for _, f := range zr.File {
		if f.FileInfo().IsDir() || filepath.Base(f.Name) != name {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, fmt.Errorf("open %q in zip release archive: %w", name, err)
		}
		defer rc.Close()
		return io.ReadAll(rc)
	}
	return nil, fmt.Errorf("binary %q not found in zip release archive", name)
}
