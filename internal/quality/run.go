// Package quality implements the deterministic, testable surface of
// `dross quality`: run-dir/run-id creation, analyzer availability detection,
// the maintainability-risk findings ledger, and the findings→spec.toml scaffold
// writer. The audit orchestration itself (recon, analyzer sweep, fan-out,
// refute-panel, calibrate-only context judging) lives in the quality.md prompt;
// this package is the part with real tests.
package quality

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// runIDTimeFormat is a fixed-width, lexically-sortable timestamp layout, so run
// ids sort chronologically when listed.
const runIDTimeFormat = "20060102T150405"

// RunID returns the run identifier "<timestamp>-<short-sha>". The timestamp is
// formatted from now (pass UTC for ids that are stable across machines); sha is
// the short commit sha, or "nogit" when the repo state is unavailable.
func RunID(now time.Time, sha string) string {
	if sha == "" {
		sha = "nogit"
	}
	return fmt.Sprintf("%s-%s", now.Format(runIDTimeFormat), sha)
}

// ShortSHA returns the short HEAD sha for the repo at repoDir, or "nogit" if it
// can't be read (not a git repo, no commits). Best-effort — it never errors, so
// a missing repo degrades to a stable "nogit" rather than failing a run.
func ShortSHA(repoDir string) string {
	out, err := exec.Command("git", "-C", repoDir, "rev-parse", "--short", "HEAD").Output()
	if err != nil {
		return "nogit"
	}
	return normalizeSHA(string(out))
}

// normalizeSHA trims git's output and falls back to "nogit" when it is empty —
// the empty-but-no-error case, extracted so the fallback branch is unit-testable.
func normalizeSHA(out string) string {
	sha := strings.TrimSpace(out)
	if sha == "" {
		return "nogit"
	}
	return sha
}

// QualityDir is the conventional parent for all quality run artifacts:
// .dross/quality. root is the .dross dir (as returned by cmd.FindRoot), mirroring
// phase.Dir's root-parameter convention.
func QualityDir(root string) string {
	return filepath.Join(root, "quality")
}

// NewRun creates a fresh run directory under .dross/quality/ and returns its
// absolute path. The directory name is RunID(now, sha); if a directory with that
// id already exists (e.g. a second run in the same second on the same commit), a
// numeric suffix ("-2", "-3", …) is appended so an existing run is never
// clobbered. Nothing is written outside the returned run directory.
func NewRun(root string, now time.Time, sha string) (string, error) {
	base := RunID(now, sha)
	dir := filepath.Join(QualityDir(root), base)
	for n := 2; ; n++ {
		_, err := os.Stat(dir)
		if err != nil {
			if os.IsNotExist(err) {
				break
			}
			return "", err
		}
		dir = filepath.Join(QualityDir(root), fmt.Sprintf("%s-%d", base, n))
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}
