package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ClobberedFile is a git-tracked .dross/ file detected missing from, or
// diverging from, its last-committed content in the working tree.
type ClobberedFile struct {
	// Path is repo-root-relative (e.g. ".dross/project.toml").
	Path string
	// Missing is true when the file is absent from the working tree; false
	// means it exists but its content differs from HEAD's tracked blob.
	Missing bool
}

// detectModifiedOrMissingTracked scans every git-tracked file under
// .dross/ and reports the ones missing from the working tree or carrying
// uncommitted content changes against HEAD. Tree-diff only (locked decision
// clobber_detection_scope) — a schema-valid file with stale *content* that
// still matches HEAD is dross validate's business, not this detector's.
//
// state.json is never reported: it is gitignored going forward (locked
// decision state_tracking), so `git ls-files` never lists it here.
func detectModifiedOrMissingTracked(repoDir string) ([]ClobberedFile, error) {
	out, err := gitTrim(repoDir, "ls-files", "--", RootDirName)
	if err != nil {
		return nil, fmt.Errorf("git ls-files: %w", err)
	}
	var found []ClobberedFile
	for _, rel := range strings.Split(out, "\n") {
		if rel == "" {
			continue
		}
		if _, statErr := os.Stat(filepath.Join(repoDir, rel)); statErr != nil {
			if os.IsNotExist(statErr) {
				found = append(found, ClobberedFile{Path: rel, Missing: true})
				continue
			}
			return nil, fmt.Errorf("stat %s: %w", rel, statErr)
		}
		// git diff --quiet exits 1 when HEAD's tracked blob differs from the
		// working-tree copy; exit 0 means they match.
		if diffErr := gitNoOut(repoDir, "diff", "--quiet", "HEAD", "--", rel); diffErr != nil {
			found = append(found, ClobberedFile{Path: rel})
		}
	}
	return found, nil
}

// restorePathFromRef checks out path's exact blob from ref into the working
// tree, overwriting whatever local content is there (or recreating the file
// if it's missing). Shared by every repair detector — t-1's clobbered
// tracked files and t-3's checkout-wiped phase dirs both restore through
// this one primitive.
func restorePathFromRef(repoDir, ref, path string) error {
	if out, err := gitCombined(repoDir, "checkout", ref, "--", path); err != nil {
		return fmt.Errorf("git checkout %s -- %s: %w\n%s", ref, path, err, out)
	}
	return nil
}
