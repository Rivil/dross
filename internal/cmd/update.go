package cmd

import (
	"archive/tar"
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

	"github.com/spf13/cobra"

	"github.com/Rivil/dross/internal/update"
)

// Update registers `dross update` — a thin cobra wrapper over internal/update. It
// fetches the latest GitHub release, verifies the platform tarball's SHA-256 against
// checksums.txt (refusing on mismatch), atomically replaces the running binary when
// the release is strictly newer (or always, with --force), then re-syncs the embedded
// assets by exec'ing the FRESHLY-SWAPPED binary — never the in-process install engine,
// which would re-materialize the OLD binary's embedded assets.
func Update() *cobra.Command {
	var o updateOpts
	var apiBase string
	c := &cobra.Command{
		Use:   "update",
		Short: "Update dross to the latest GitHub release",
		RunE: func(cmd *cobra.Command, _ []string) error {
			o.out = cmd.OutOrStdout()
			o.apiBase = apiBase
			return runUpdate(cmd.Context(), o)
		},
	}
	c.Flags().BoolVar(&o.check, "check", false, "report the available version without updating")
	c.Flags().BoolVar(&o.force, "force", false, "reinstall the latest release even if it is not newer")
	c.Flags().StringVar(&apiBase, "api-base", "", "override the GitHub API base URL (testing)")
	_ = c.Flags().MarkHidden("api-base")
	return c
}

// updateOpts carries the command flags plus injectable seams for tests. Zero-valued
// seams fall back to production defaults (running build version, os.Executable, an
// exec of the new binary).
type updateOpts struct {
	out     io.Writer
	apiBase string
	check   bool
	force   bool

	httpClient *http.Client
	version    string                    // running version; defaults to cmd.Version
	commit     string                    // running commit; defaults to cmd.Commit
	targetPath string                    // binary to replace; defaults to os.Executable()
	resync     func(newBinary string) error // asset re-sync; defaults to `<newBinary> install`
}

func runUpdate(ctx context.Context, o updateOpts) error {
	if ctx == nil {
		ctx = context.Background()
	}
	version := o.version
	if version == "" {
		version = Version
	}
	commit := o.commit
	if commit == "" {
		commit = Commit
	}

	client := update.NewClient()
	if o.apiBase != "" {
		client.APIBase = o.apiBase
	}
	if o.httpClient != nil {
		client.HTTP = o.httpClient
	}

	rel, err := client.LatestRelease(ctx)
	if err != nil {
		return err
	}
	decision := update.Decide(rel.TagName, version, commit)
	fmt.Fprintf(o.out, "current: %s\nlatest:  %s\n", version, rel.TagName)

	if o.check {
		switch decision {
		case update.UpdateAvailable:
			fmt.Fprintln(o.out, "update available — run `dross update` to apply")
		case update.NeedsConfirm:
			fmt.Fprintln(o.out, "running a dev/unknown build — use `dross update --force` to install the latest")
		default:
			fmt.Fprintln(o.out, "up to date")
		}
		return nil
	}

	if !(o.force || decision == update.UpdateAvailable) {
		switch decision {
		case update.NeedsConfirm:
			fmt.Fprintf(o.out, "running a dev/unknown build; not updating. Use --force to install %s.\n", rel.TagName)
		default:
			fmt.Fprintln(o.out, "already up to date.")
		}
		return nil
	}

	assetName, err := update.AssetName(rel.TagName, runtime.GOOS, runtime.GOARCH)
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
		return fmt.Errorf("refusing update: %w", update.ErrNoSignature)
	}
	sig, err := client.Download(ctx, sigURL)
	if err != nil {
		return err
	}
	if err := update.VerifySignature(sums, sig, update.TrustedMinisignKey); err != nil {
		return fmt.Errorf("refusing update: %w", err)
	}
	if err := update.VerifyChecksum(tarball, update.ParseChecksums(sums), assetName); err != nil {
		return fmt.Errorf("refusing update: %w", err)
	}

	binBytes, err := extractBinary(tarball, "dross")
	if err != nil {
		return err
	}

	targetPath := o.targetPath
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
	if err := update.AtomicReplace(targetPath, bytes.NewReader(binBytes)); err != nil {
		return err
	}
	fmt.Fprintf(o.out, "updated %s → %s\n", targetPath, rel.TagName)

	resync := o.resync
	if resync == nil {
		resync = func(newBinary string) error {
			cmd := exec.Command(newBinary, "install")
			cmd.Stdout = o.out
			cmd.Stderr = o.out
			return cmd.Run()
		}
	}
	if err := resync(targetPath); err != nil {
		return fmt.Errorf("binary updated but asset re-sync failed: %w", err)
	}
	fmt.Fprintln(o.out, "re-synced assets from the updated binary.")
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
