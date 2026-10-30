package memadvise

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Snapshot copies the memory dir's direct-child regular files into
// <root>/.dross/memory/snapshots/<UTC timestamp>/ and returns that dir. Apply
// takes one before it changes anything, so every apply can be undone by hand
// from a copy that never left this machine.
//
// The state dir's own `*` .gitignore is written first: a repo whose root
// .gitignore predates the .dross/memory/ seed still never stages a copy of
// someone's memories. Symlinks and anything that is not a regular file are
// not copied — Apply never touches them either.
func Snapshot(root, memDir string, now time.Time) (string, error) {
	state := filepath.Join(root, filepath.FromSlash(StateDir))
	if err := os.MkdirAll(state, 0o755); err != nil {
		return "", fmt.Errorf("create %s: %w", StateDir, err)
	}
	info, err := os.Lstat(state)
	if err != nil {
		return "", fmt.Errorf("stat %s: %w", StateDir, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%s is not a directory; refusing to snapshot through it", StateDir)
	}
	if err := ensureStateIgnored(state); err != nil {
		return "", err
	}
	snaps := filepath.Join(root, filepath.FromSlash(SnapshotsDir))
	if err := os.MkdirAll(snaps, 0o755); err != nil {
		return "", fmt.Errorf("create %s: %w", SnapshotsDir, err)
	}
	dest := filepath.Join(snaps, now.UTC().Format("20060102T150405.000000000Z"))
	// Mkdir, not MkdirAll: an existing dir means another apply in the same
	// instant, and two applies must never share one snapshot.
	if err := os.Mkdir(dest, 0o755); err != nil {
		return "", fmt.Errorf("create snapshot: %w", err)
	}
	entries, err := os.ReadDir(memDir)
	if err != nil {
		return "", fmt.Errorf("read memory dir: %w", err)
	}
	for _, e := range entries {
		if !e.Type().IsRegular() {
			continue
		}
		data, ok := readRegular(filepath.Join(memDir, e.Name()))
		if !ok {
			return "", fmt.Errorf("snapshot %s: unreadable", e.Name())
		}
		info, err := e.Info()
		if err != nil {
			return "", fmt.Errorf("snapshot %s: %w", e.Name(), err)
		}
		if err := os.WriteFile(filepath.Join(dest, e.Name()), data, info.Mode().Perm()); err != nil {
			return "", fmt.Errorf("snapshot %s: %w", e.Name(), err)
		}
	}
	return dest, nil
}
