package gatestate

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// ShowDir holds the show-once records, under Dir: what `dross rule show` and
// `dross interaction show` last printed in full to each session, so a repeat
// in the same context can print a one-line marker instead.
//
// Three kinds of file, all written through saveBytes:
//
//   - epoch: a repo-wide nonce, replaced by every BumpContext. Any session's
//     start, /clear or compaction bumps it, which retires every record at once
//     — a stale record only costs a reprint, a wrong one would hide the rules.
//   - <key>.seen: the session was announced by its SessionStart hook. A session
//     dross never heard start is never current, so a missing hook prints full.
//   - <key>-<kind>: "<epoch> <sha>", the epoch and content digest of the last
//     full print.
//
// <key> is the session id hashed the way nudgeKey hashes it, so an arbitrary
// id never names a path outside ShowDir.
const ShowDir = "show"

// Show kinds.
const (
	ShowRules    = "rules"
	ShowPlaybook = "playbook"
)

const (
	showEpochFile = "epoch"
	showSeenExt   = ".seen"
	// showTTL is how long a record or seen-mark outlives its last write.
	showTTL = 7 * 24 * time.Hour
)

var showKind = regexp.MustCompile(`^[a-z]+$`)

// showLine is a record's content: the epoch and the digest of the print.
func showLine(epoch, sha string) string { return epoch + " " + sha }

func showName(file string) string { return ShowDir + "/" + file }

func showPath(root string) string {
	return filepath.Join(root, filepath.FromSlash(Dir), ShowDir)
}

// BumpContext starts a new show epoch and marks sessionID seen. The
// SessionStart hook calls it for every source — startup, resume, clear and
// compact — because each one leaves a context that has not seen the last
// full print.
func BumpContext(root, sessionID string) error {
	if sessionID == "" {
		return errors.New("bump show context: empty session id")
	}
	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		return fmt.Errorf("bump show context: %w", err)
	}
	if err := showWrite(root, showEpochFile, []byte(hex.EncodeToString(nonce)+"\n")); err != nil {
		return err
	}
	return showWrite(root, nudgeKey(sessionID)+showSeenExt, nil)
}

// BumpForHook reads a SessionStart hook payload and bumps the context for the
// session it names. Decoded key by key, like gate.Decode: the shape is Claude
// Code's, and a key this version does not know costs nothing. A payload that
// is not a JSON object naming a session_id is an error the hook swallows.
func BumpForHook(root string, payload []byte) error {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(payload, &top); err != nil {
		return fmt.Errorf("unparseable SessionStart payload: %w", err)
	}
	var sid string
	if raw, ok := top["session_id"]; ok {
		_ = json.Unmarshal(raw, &sid)
	}
	if sid == "" {
		return errors.New("SessionStart payload names no session_id")
	}
	return BumpContext(root, sid)
}

// ShownCurrent reports whether sessionID's last full print of kind is still in
// its context: the session was seen, and its record holds the current epoch
// and sha. It only reads; any error reads as false, which prints in full.
func ShownCurrent(root, sessionID, kind, sha string) bool {
	if sessionID == "" || !showKind.MatchString(kind) {
		return false
	}
	dir := showPath(root)
	key := nudgeKey(sessionID)
	if _, err := os.Stat(filepath.Join(dir, key+showSeenExt)); err != nil {
		return false
	}
	epoch, err := os.ReadFile(filepath.Join(dir, showEpochFile))
	if err != nil {
		return false
	}
	b, err := os.ReadFile(filepath.Join(dir, key+"-"+kind))
	if err != nil {
		return false
	}
	got, want := strings.TrimSpace(string(b)), showLine(strings.TrimSpace(string(epoch)), sha)
	return got == want
}

// RecordShown records that kind was printed in full to sessionID, with
// content digest sha, in the current epoch. Call it only after the print
// succeeded: a record for a print that never landed would hide the block.
func RecordShown(root, sessionID, kind, sha string) error {
	if sessionID == "" {
		return errors.New("record show: empty session id")
	}
	if !showKind.MatchString(kind) {
		return fmt.Errorf("record show: invalid kind %q", kind)
	}
	epoch, err := os.ReadFile(filepath.Join(showPath(root), showEpochFile))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("read %s: %w", rel(showName(showEpochFile)), err)
	}
	return showWrite(root, nudgeKey(sessionID)+"-"+kind, []byte(showLine(strings.TrimSpace(string(epoch)), sha)+"\n"))
}

// showWrite writes one file under ShowDir and prunes the directory.
func showWrite(root, file string, b []byte) error {
	if err := ensureDir(root); err != nil {
		return err
	}
	dir := showPath(root)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create %s/%s: %w", Dir, ShowDir, err)
	}
	if err := saveBytes(root, showName(file), b); err != nil {
		return err
	}
	pruneShow(dir, time.Now().Add(-showTTL))
	return nil
}

// pruneShow removes records and seen-marks last written before cutoff — inside
// ShowDir only, and never the epoch. Best effort: one it can't remove is
// retried on the next write.
func pruneShow(dir string, cutoff time.Time) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if !e.Type().IsRegular() || e.Name() == showEpochFile {
			continue
		}
		if info, err := e.Info(); err == nil && info.ModTime().Before(cutoff) {
			os.Remove(filepath.Join(dir, e.Name()))
		}
	}
}
