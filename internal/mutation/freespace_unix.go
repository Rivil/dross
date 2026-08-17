//go:build unix

package mutation

import "syscall"

// freeBytes reports the space available on the filesystem holding path, and
// whether it could be determined at all.
//
// Available rather than free: Bavail excludes the blocks reserved for root,
// which an ordinary run cannot use. Reporting Bfree would overstate what is
// actually reachable, and overstating headroom is the whole failure this is
// here to prevent.
//
// ok=false rather than an error, at every call site, because "I could not
// measure this" must never become a reason a run does not happen.
func freeBytes(path string) (uint64, bool) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, false
	}
	//nolint:unconvert,gosec // Bsize/Bavail widths differ across unixes
	return uint64(st.Bavail) * uint64(st.Bsize), true
}
