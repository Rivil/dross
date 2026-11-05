package gatestate

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// NudgeDir holds the context-nudge claims, under Dir. A claim is an empty file
// named <session key>-<band>: the band of a session's context that has already
// been nudged, so the hook nudges each band once however often it fires.
const NudgeDir = "nudge"

// nudgeClaimTTL is how long a claim outlives its last write. A session older
// than this has long since been cleared, so its claims are only clutter.
const nudgeClaimTTL = 7 * 24 * time.Hour

// nudgeKey is the session's file-name prefix. Hashing keeps an arbitrary
// session id — "../x", "a/b" — from naming a path outside NudgeDir.
func nudgeKey(sessionID string) string {
	sum := sha256.Sum256([]byte(sessionID))
	return hex.EncodeToString(sum[:])[:16]
}

func nudgePath(root string) string {
	return filepath.Join(root, filepath.FromSlash(Dir), NudgeDir)
}

// highestClaim returns the highest band claimed for the session, 0 when none
// (or when the directory can't be read: no record is no record).
func highestClaim(root, sessionID string) int64 {
	entries, err := os.ReadDir(nudgePath(root))
	if err != nil {
		return 0
	}
	prefix := nudgeKey(sessionID) + "-"
	var high int64
	for _, e := range entries {
		rest, ok := strings.CutPrefix(e.Name(), prefix)
		if !ok {
			continue
		}
		if b, err := strconv.ParseInt(rest, 10, 64); err == nil && b > high {
			high = b
		}
	}
	return high
}

// NudgeClaimed reports whether band, or any higher band, is already claimed for
// the session. It only reads: no directory is created, so the hook's
// below-threshold and already-nudged paths leave the tree untouched.
func NudgeClaimed(root, sessionID string, band int64) bool {
	return sessionID != "" && highestClaim(root, sessionID) >= band
}

// ClaimNudge claims band for the session and reports whether this call won it.
// It loses when the band or any higher one is already claimed (a session's
// nudges only climb), and exactly one of any number of concurrent claims of the
// same band wins: the claim is an O_EXCL create. A successful claim prunes
// claims older than nudgeClaimTTL — inside NudgeDir only.
func ClaimNudge(root, sessionID string, band int64) (bool, error) {
	if sessionID == "" {
		return false, errors.New("claim nudge: empty session id")
	}
	if highestClaim(root, sessionID) >= band {
		return false, nil
	}
	if err := ensureDir(root); err != nil {
		return false, err
	}
	dir := nudgePath(root)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return false, fmt.Errorf("create %s/%s: %w", Dir, NudgeDir, err)
	}
	name := nudgeKey(sessionID) + "-" + strconv.FormatInt(band, 10)
	f, err := os.OpenFile(filepath.Join(dir, name), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if errors.Is(err, fs.ErrExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("claim %s/%s/%s: %w", Dir, NudgeDir, name, err)
	}
	f.Close()
	pruneNudgeClaims(dir, time.Now().Add(-nudgeClaimTTL))
	return true, nil
}

// pruneNudgeClaims removes claims last written before cutoff. Best effort: a
// claim it can't remove is retried on the next successful claim.
func pruneNudgeClaims(dir string, cutoff time.Time) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if !e.Type().IsRegular() {
			continue
		}
		if info, err := e.Info(); err == nil && info.ModTime().Before(cutoff) {
			os.Remove(filepath.Join(dir, e.Name()))
		}
	}
}
