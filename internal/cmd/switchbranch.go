package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/Rivil/dross/internal/state"
)

// Every branch switch dross performs goes through this file, so the one failure
// mode the untracking introduces has exactly one place to be handled.
//
// state.json is machine-local and gitignored now (locked state_tracking), but
// branches and tags cut before that migration still track a copy — history was
// deliberately not rewritten (locked migration_scope). Switching to one of those
// replays the tracked copy over the live file.
//
// The guard is a dross-side PRE-CHECK, not a translation of git's own refusal,
// because git does not refuse here. git declines to overwrite an untracked
// working-tree file only when that file is NOT ignored; for an ignored path it
// overwrites silently. The `state_tracking` decision's rationale assumed the
// refusal — the choice it made (untrack + ignore) stands, but nothing in git
// stops the clobber, so dross has to.
//
// The check is scoped to dross's own switches. A bare `git checkout` of a legacy
// branch is still a silent clobber; `dross doctor` reports branches that still
// track the file so they can be cleaned up.

// checkoutBranch switches to branch, refusing first if the switch would replay a
// tracked .dross/state.json over the live machine-local one.
//
// It never moves or deletes the live file. Backing state up and putting it back
// is the user's call: dross relocating machine-local position data on their
// behalf is how a "helpful" tool loses the history it was protecting.
func checkoutBranch(repoDir, branch string) error {
	if err := guardLiveState(repoDir, branch); err != nil {
		return err
	}
	if out, err := gitCombined(repoDir, "checkout", branch); err != nil {
		return fmt.Errorf("git checkout %s: %w\n%s", branch, err, out)
	}
	return nil
}

// checkoutBranchNew is the fork-time counterpart: `git checkout -b <branch>
// <base>`. The clobber fires here too — on every base ref that still tracks the
// file — and this is the call site a user hits first, at `dross phase create`.
func checkoutBranchNew(repoDir, branch, base string) error {
	if err := guardLiveState(repoDir, base); err != nil {
		return err
	}
	if out, err := gitCombined(repoDir, "checkout", "-b", branch, base); err != nil {
		return fmt.Errorf("git checkout -b %s %s: %w\n%s", branch, base, err, out)
	}
	return nil
}

// guardLiveState returns a named error when switching to ref would overwrite the
// live .dross/state.json with a tracked copy carried by that ref. It answers nil
// whenever there is nothing to protect: no live file, or a ref that carries no
// copy of its own — which is every ref cut after the migration, so the common
// path costs one `git cat-file -e`.
func guardLiveState(repoDir, ref string) error {
	rel := RootDirName + "/" + state.File
	live := filepath.Join(repoDir, RootDirName, state.File)
	if _, err := os.Stat(live); err != nil {
		return nil // nothing live to lose
	}
	if gitNoOut(repoDir, "ls-files", "--error-unmatch", "--", rel) == nil {
		return nil // still tracked here: git is managing it, not us
	}
	if gitNoOut(repoDir, "cat-file", "-e", ref+":"+rel) != nil {
		return nil // the target carries no copy — the ordinary case
	}
	return fmt.Errorf(`refusing to switch to %s: it would overwrite your live %s.

%s carries a tracked copy of that file from before it was untracked, and git
overwrites an ignored working-tree file without complaint. The live copy is
machine-local — current phase, version, activity history — so dross stops here
rather than let the switch replace it.

To proceed, move the live copy aside and put it back after the switch:

    cp %s %s.bak
    rm %s
    git checkout %s
    cp %s.bak %s

Or drop the stale copy from that branch for good:

    git checkout %s && git rm --cached %s && git commit -m "chore: untrack state.json"`,
		ref, rel,
		ref,
		rel, rel,
		rel,
		ref,
		rel, rel,
		ref, rel)
}
