package cmd

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Rivil/dross/internal/gatestate"
)

// What a repeat `dross rule show` / `dross interaction show` prints in place of
// a block this session already holds, byte-identical, since its last context
// start. Each names its --full command: the session id cannot tell a subagent
// from its parent, so a context that never saw the block must be able to ask.
const (
	RulesUnchanged    = "rules unchanged since this session's full print — still MUST-FOLLOW; not in your context? run: dross rule show --full"
	PlaybookUnchanged = "playbook unchanged since this session's full print — still MUST-FOLLOW; not in your context? run: dross interaction show --full"
)

// showPrint writes a full block to stdout; a test swaps it to fail.
var showPrint = func(s string) error {
	_, err := fmt.Fprint(os.Stdout, s)
	return err
}

// showOnce prints rendered in full, or kind's one-line marker when this session
// already has exactly these bytes in its current context.
//
// The session is CLAUDE_CODE_SESSION_ID alone. CLAUDE_CODE_CHILD_SESSION is not
// consulted: it is set in a background job's main session and in its subagents
// alike, under the same session id, so it tells them apart no better. The
// marker names --full instead, so a context that lacks the block recovers.
//
// The record is written only after the full print succeeded, and a store that
// cannot be read or written never fails the command: it just prints in full.
func showOnce(kind, rendered string, full bool) error {
	sid := os.Getenv("CLAUDE_CODE_SESSION_ID")
	repo := showRepo()
	sum := sha256.Sum256([]byte(rendered))
	sha := hex.EncodeToString(sum[:])
	if repo != "" && sid != "" && !full && gatestate.ShownCurrent(repo, sid, kind, sha) {
		Print(showMarker(kind))
		return nil
	}
	if err := showPrint(rendered); err != nil {
		return nil // nothing reached the context, so nothing is recorded
	}
	if repo != "" && sid != "" {
		_ = gatestate.RecordShown(repo, sid, kind, sha)
	}
	return nil
}

func showMarker(kind string) string {
	if kind == gatestate.ShowPlaybook {
		return PlaybookUnchanged
	}
	return RulesUnchanged
}

// showRepo is the repo root holding a complete .dross/, or "" when there is
// none: no root, or a half-built one, prints in full. It resolves through
// FindRoot, as `rule show`'s own project-rules load already does.
func showRepo() string {
	root, err := FindRoot()
	if err != nil {
		return ""
	}
	return filepath.Dir(root)
}
