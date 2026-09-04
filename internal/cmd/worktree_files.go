package cmd

// The file set a bare `dross test lane preview` reads.
//
// Locked bare_preview_default: "what will the gate run for the work I have
// right now" is the question the verb was opened to ask, and making its most
// common invocation an error would cost the surface its ergonomics. It infers
// the INPUT and never the lanes — the milestone's declared-not-inferred
// non-goal is about lane configuration, not about which files are in hand.

import (
	"fmt"
	"os/exec"
	"strings"
)

// worktreeChangedFiles returns the repo-relative paths of everything
// uncommitted in the working tree — staged, unstaged and untracked — deduped,
// in git's own order.
//
// It runs its own `git status` rather than reusing gitStatusRaw, for one
// reason: `--untracked-files=all`. gitStatusRaw takes git's default, which
// COLLAPSES a new directory to a single `dir/` entry, and a directory previews
// nothing — no lane glob written against files matches it, so a whole new
// package would silently resolve to no lane at all. autoCommitDrossDirt wants
// the collapsed form (it asks whether anything outside .dross/ is dirty, and a
// directory answers that), so that function is left exactly as it is.
//
// A clean tree is an empty set and NOT an error: the caller says "0 files" and
// exits 0, which is the honest answer to "what would the gate run" when there
// is nothing in hand.
func worktreeChangedFiles(repoDir string) ([]string, error) {
	//dross:exec-exempt git status --porcelain reads the working tree and runs no repo-authored line; no hook fires for it
	out, err := exec.Command("git", "-C", repoDir, "status", "--porcelain", "--untracked-files=all").Output()
	if err != nil {
		return nil, fmt.Errorf("git status: %w", err)
	}
	return worktreeFilesFromStatus(strings.TrimRight(string(out), "\n")), nil
}

// worktreeFilesFromStatus is the parse, split out from the git call so the
// line shapes that are awkward to produce on demand — a rename, a quoted path —
// can be exercised directly.
//
// Deletions are KEPT. They are exactly the paths preview has to be able to name
// as dropped (c-2): filtering them here would leave the derivation with nothing
// to report, and a deleted file is the case a user is most likely to preview.
//
// A rename contributes only its DESTINATION. porcelainPaths returns both sides,
// which is what autoCommitDrossDirt needs — a rename out of .dross/ is dirt on
// both ends — but here the source is a path that was never deleted by the user's
// work and no longer exists, so keeping it would make every rename report a
// phantom dropped path.
func worktreeFilesFromStatus(status string) []string {
	if strings.TrimSpace(status) == "" {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, line := range strings.Split(status, "\n") {
		paths := porcelainPaths(line)
		if len(paths) == 0 {
			continue
		}
		// The last path is the destination for a rename or copy, and the only
		// path for everything else.
		p := paths[len(paths)-1]
		if p == "" || seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	return out
}
