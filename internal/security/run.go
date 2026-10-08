// Package security implements the deterministic, testable surface of
// `dross security`: run-dir/run-id creation, scanner availability detection,
// the findings ledger, and the findings→spec.toml scaffold writer. The audit
// orchestration itself (recon, tooling sweep, fan-out, refute-panel) lives in
// the secure.md prompt; this package is the part with real tests.
package security

import (
	"fmt"
	"os"
	"path/filepath"
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

// SecurityDir is the conventional parent for all security run artifacts:
// .dross/security. root is the .dross dir (as returned by cmd.FindRoot), mirroring
// phase.Dir's root-parameter convention.
func SecurityDir(root string) string {
	return filepath.Join(root, "security")
}

// NewRun creates a fresh run directory under .dross/security/ and returns its
// absolute path. The directory name is RunID(now, sha); if a directory with that
// id already exists (e.g. a second run in the same second on the same commit), a
// numeric suffix ("-2", "-3", …) is appended so an existing run is never
// clobbered. Nothing is written outside the returned run directory.
func NewRun(root string, now time.Time, sha string) (string, error) {
	base := RunID(now, sha)
	dir := filepath.Join(SecurityDir(root), base)
	for n := 2; ; n++ {
		_, err := os.Stat(dir)
		if err != nil {
			if os.IsNotExist(err) {
				break
			}
			return "", err
		}
		dir = filepath.Join(SecurityDir(root), fmt.Sprintf("%s-%d", base, n))
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}
