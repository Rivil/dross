package cmd

import (
	"io/fs"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/Rivil/dross/internal/findings"
	"github.com/Rivil/dross/internal/gitrun"
	"github.com/Rivil/dross/internal/stack"
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
			sha := gitrun.ShortSHA(repoDir)

			paths, err := trackedFiles(repoDir)
			if err != nil {
				return err
			}
			// [techdebt] exclude from project.toml: the adopter's own exemptions
			// (dross lists internal/techdebt/, whose marker regex and fixtures
			// would otherwise report themselves). A bad pattern is the command's
			// error, surfaced before any run dir exists.
			proj, _, err := loadProject()
			if err != nil {
				return err
			}
			paths, err = techdebt.Filter(repoDir, paths, proj.Techdebt.Exclude)
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
// ignored, and vendored files are excluded). Any path with a directory
// component in the shared skip set (stack.SkipDir) is dropped on both paths —
// the one definition every dross scanner honours, so the tech-debt scan can
// never see a tree language detection or the security manifest scoped out.
// That set is wider than the two fixture names that motivated it: tracked
// build/, dist/, .idea/, .vscode/, node_modules/ and vendor/ content also
// leaves the scan, deliberately — a second, narrower copy here is the drift the
// skip_dir_set decision forbids. `.dross/` bookkeeping is in the set too —
// planning artefacts (milestone prose, plan.toml strings, tests.json dumps) are
// generated workflow state, and scanning them drowned real code debt 5:1 on
// this repo's own v1.0 self-audit. When repoDir is not a git repo (so ls-files
// fails), it falls back to a tree walk that prunes the same set, so the scan
// still runs — the run id will carry the "nogit" sha.
func trackedFiles(repoDir string) ([]string, error) {
	out, err := gitrun.Raw(repoDir, "ls-files", "-z")
	if err == nil {
		var paths []string
		//dross:taint-cleared git ls-files -z prints NUL-separated tracked paths; each becomes a file the scan reads, and nothing else of git's output is kept
		for _, rel := range strings.Split(strings.TrimRight(out, "\x00"), "\x00") {
			if rel == "" || inSkippedDir(rel) {
				continue
			}
			paths = append(paths, filepath.Join(repoDir, rel))
		}
		return paths, nil
	}
	var paths []string
	walkErr := filepath.WalkDir(repoDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable entries rather than aborting the scan
		}
		if d.IsDir() {
			if p != repoDir && stack.SkipDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		paths = append(paths, p)
		return nil
	})
	return paths, walkErr
}

// inSkippedDir reports whether any DIRECTORY component of the slash-form
// relative path rel is in the shared skip set. The final component is the file
// itself and is never tested, so a file named "vendor" or "testdata.py" at the
// root still scans; the match is whole-segment, so "testdata-like/" does not.
func inSkippedDir(rel string) bool {
	segs := strings.Split(rel, "/")
	for _, seg := range segs[:len(segs)-1] {
		if stack.SkipDir(seg) {
			return true
		}
	}
	return false
}
