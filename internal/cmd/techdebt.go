package cmd

import (
	"io/fs"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/Rivil/dross/internal/findings"
	"github.com/Rivil/dross/internal/techdebt"
)

// Techdebt registers `dross techdebt` — the deterministic, dependency-free
// tech-debt scan. It enumerates the repo's tracked files, scans them for debt
// markers and size heuristics, writes a timestamped run + report under
// .dross/techdebt/, and stamps the store-level last_run so `dross status` can
// rank the tech-debt area by staleness alongside security and quality. No agent
// or prompt — the scan is fully deterministic.
func Techdebt() *cobra.Command {
	return &cobra.Command{
		Use:   "techdebt",
		Short: "Scan tracked files for tech-debt markers + size heuristics (.dross/techdebt/<id>)",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			root, err := FindRoot()
			if err != nil {
				return err
			}
			repoDir := filepath.Dir(root)
			now := time.Now().UTC()
			sha := techdebt.ShortSHA(repoDir)

			paths, err := trackedFiles(repoDir)
			if err != nil {
				return err
			}
			runDir, err := techdebt.NewRun(root, now, sha)
			if err != nil {
				return err
			}
			found := techdebt.Scan(paths, techdebt.DefaultThresholds)
			if err := techdebt.WriteReport(runDir, found); err != nil {
				return err
			}
			// Stamp the store-level last_run so the area ranks as "ran" right
			// after this run (prune-proof: state.toml outlives the run dir).
			if err := findings.StampLastRun(techdebt.StatePath(root), now); err != nil {
				return err
			}
			Printf("techdebt run: %s\n", runDir)
			Printf("  findings: %d\n", len(found))
			return nil
		},
	}
}

// trackedFiles returns the absolute paths of the repo's tracked files via
// `git ls-files`, honoring the locked "scan tracked files" decision (untracked,
// ignored, and vendored files are excluded). When repoDir is not a git repo (so
// ls-files fails), it falls back to a tree walk that skips .git and .dross, so
// the scan still runs — the run id will carry the "nogit" sha.
func trackedFiles(repoDir string) ([]string, error) {
	out, err := exec.Command("git", "-C", repoDir, "ls-files", "-z").Output()
	if err == nil {
		var paths []string
		for _, rel := range strings.Split(strings.TrimRight(string(out), "\x00"), "\x00") {
			if rel != "" {
				paths = append(paths, filepath.Join(repoDir, rel))
			}
		}
		return paths, nil
	}
	var paths []string
	walkErr := filepath.WalkDir(repoDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable entries rather than aborting the scan
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", ".dross":
				return filepath.SkipDir
			}
			return nil
		}
		paths = append(paths, p)
		return nil
	})
	return paths, walkErr
}
