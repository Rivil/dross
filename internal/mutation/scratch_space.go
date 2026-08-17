package mutation

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// The free-space floor a mutation run refuses to start below.
//
// This does NOT predict what a run needs — that depends on the package count
// and the mutant yield, and pretending to know it would be a fabricated number
// dressed as a guarantee. It is a floor taken from one measurement: the run
// that motivated this consumed roughly 60 GB and filled its volume at about
// 1.3 GB/min, reaching two minutes from a wedged host. 20 GB is "clearly not
// enough to be safe", not "enough".
//
// A warning was the obvious alternative and is the wrong one. At 1.3 GB/min a
// warning is something you read afterwards. An error costs a re-run; the thing
// it prevents cost a host that was also serving unrelated services.
const (
	// ScratchMinFreeEnv overrides the floor, in whole gigabytes. Setting it to
	// 0 disables the check for an operator who knows their volume better than
	// a default can.
	ScratchMinFreeEnv = "DROSS_SCRATCH_MIN_FREE_GB"

	defaultScratchMinFreeGB = 20
	bytesPerGB              = 1 << 30
)

// scratchMinFreeBytes is the configured floor, or the default.
func scratchMinFreeBytes() uint64 {
	raw := strings.TrimSpace(os.Getenv(ScratchMinFreeEnv))
	if raw == "" {
		return defaultScratchMinFreeGB * bytesPerGB
	}
	gb, err := strconv.ParseUint(raw, 10, 32)
	if err != nil {
		// An unparseable override is not consent to run without a floor. Fall
		// back to the default rather than to nothing — the opposite reading
		// would turn a typo into a disabled guard.
		return defaultScratchMinFreeGB * bytesPerGB
	}
	return gb * bytesPerGB
}

// checkScratchSpace refuses a run whose scratch volume is already too full.
//
// The refusal names three things because all three are needed to act: where the
// scratch would go (which is not obvious — it is derived, not configured), how
// much is actually there, and the one variable that moves it somewhere else.
//
// Returns nil when free space cannot be determined. A guard that failed closed
// on its own inability to measure would turn every unsupported platform into a
// broken tool.
func checkScratchSpace(base string) error {
	floor := scratchMinFreeBytes()
	if floor == 0 {
		return nil
	}
	free, ok := freeBytes(existingAncestor(base))
	if !ok {
		return nil
	}
	if free >= floor {
		return nil
	}
	return fmt.Errorf(
		"mutation: refusing to start — the scratch build cache would land on %s, which has %.1f GB free (floor %.0f GB).\n\n"+
			"A mutation run writes tens of gigabytes there and deletes it at the end; one measured run used ~60 GB at ~1.3 GB/min.\n"+
			"Starting here risks filling the volume rather than failing on it.\n\n"+
			"  put the scratch elsewhere:  %s=/path/on/a/bigger/volume\n"+
			"  lower or disable the floor: %s=<gb>   (0 disables)",
		base, float64(free)/bytesPerGB, float64(floor)/bytesPerGB, ScratchBaseEnv, ScratchMinFreeEnv)
}

// existingAncestor walks up to the nearest path that exists, because the
// scratch base itself is usually created by the run that is about to start and
// statfs on a missing path answers nothing. The volume is the same either way.
func existingAncestor(path string) string {
	for {
		if _, err := os.Stat(path); err == nil {
			return path
		}
		parent := parentOf(path)
		if parent == path {
			return path
		}
		path = parent
	}
}
