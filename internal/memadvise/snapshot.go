package memadvise

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
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
	// Every component is created and checked on its own: a symlinked .dross,
	// .dross/memory or snapshots/ — even one pointing into the memory dir —
	// is refused, never written through.
	state, err := ensureStateDir(root)
	if err != nil {
		return "", err
	}
	if err := ensureStateIgnored(state); err != nil {
		return "", err
	}
	snaps := filepath.Join(root, filepath.FromSlash(SnapshotsDir))
	// Mkdir never follows a symlink: one already there is EEXIST, and the
	// Lstat below refuses it.
	if err := os.Mkdir(snaps, 0o755); err != nil && !errors.Is(err, fs.ErrExist) {
		return "", fmt.Errorf("create %s: %w", SnapshotsDir, err)
	}
	info, err := os.Lstat(snaps)
	if err != nil {
		return "", fmt.Errorf("stat %s: %w", SnapshotsDir, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%s is not a directory; refusing to snapshot through it", SnapshotsDir)
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
		if err := copyRegular(filepath.Join(memDir, e.Name()), filepath.Join(dest, e.Name())); err != nil {
			return "", fmt.Errorf("snapshot %s: %w", e.Name(), err)
		}
	}
	return dest, nil
}

// copyRegular streams src to a new dst with src's permissions, when src is
// still a regular file once opened. There is no size cap: readRegular's cap
// keeps the advisor from reading a dump, but the snapshot is the undo for
// everything in the dir, so a large file is copied, never a reason to abort.
// The open is non-blocking so a FIFO swapped in after the listing returns.
func copyRegular(src, dst string) error {
	in, err := os.OpenFile(src, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return err
	}
	defer in.Close()
	info, err := in.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("not a regular file")
	}
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, info.Mode().Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
