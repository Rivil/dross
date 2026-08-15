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
	if err := validateGitRef("branch", branch); err != nil {
		return err
	}
	if err := guardLiveState(repoDir, branch); err != nil {
		return err
	}
	if out, err := gitCombined(repoDir, gitRefArgs("checkout", nil, branch)...); err != nil {
		return fmt.Errorf("git checkout %s: %w\n%s", branch, err, out)
	}
	return nil
}

// checkoutBranchNew is the fork-time counterpart: `git checkout -b <branch>
// <base>`. The clobber fires here too — on every base ref that still tracks the
// file — and this is the call site a user hits first, at `dross phase create`.
func checkoutBranchNew(repoDir, branch, base string) error {
	// Both, not just the base: the new branch name is the one rendered from
	// [repo].branch_pattern, so it is config-derived too.
	if err := validateGitRef("branch", branch); err != nil {
		return err
	}
	if err := validateGitRef("base", base); err != nil {
		return err
	}
	if err := guardLiveState(repoDir, base); err != nil {
		return err
	}
	// branch rides in the opts half because it is `-b`'s ARGUMENT, not a
	// positional — a separator in front of it would become the branch name.
	// git refuses an option-shaped value there on its own, and validateGitRef
	// above refuses it earlier; the separator's job is the base positional.
	if out, err := gitCombined(repoDir, gitRefArgs("checkout", []string{"-b", branch}, base)...); err != nil {
		return fmt.Errorf("git checkout -b %s %s: %w\n%s", branch, base, err, out)
	}
	return nil
}

// guardedFF is `git merge --ff-only <ref>` behind the same pre-check. A
// fast-forward materializes the target's tree exactly like a checkout does, so
// an origin ref that still tracks state.json clobbers the live file just as
// surely — the branch name simply does not change on the way.
//
// Returns git's own output alongside the error so callers that read the merge
// output (the ff-divergence signal `phase complete --recover` keys off) keep it.
func guardedFF(repoDir, ref string) (string, error) {
	if err := validateGitRef("merge ref", ref); err != nil {
		return "", err
	}
	if err := guardLiveState(repoDir, ref); err != nil {
		return "", err
	}
	return gitCombined(repoDir, gitRefArgs("merge", []string{"--ff-only"}, ref)...)
}

// guardedResetHard is `git reset --hard <ref>` behind the same pre-check, for
// the one caller that has one: `ship recover`'s heal. Same reasoning — the
// working tree is replaced from the target's tree either way.
func guardedResetHard(repoDir, ref string) (string, error) {
	if err := validateGitRef("reset ref", ref); err != nil {
		return "", err
	}
	if err := guardLiveState(repoDir, ref); err != nil {
		return "", err
	}
	return gitCombined(repoDir, gitRefArgs("reset", []string{"--hard"}, ref)...)
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
	if gitNoOut(repoDir, gitPathArgs("ls-files", []string{"--error-unmatch"}, rel)...) == nil {
		return nil // still tracked here: git is managing it, not us
	}
	// The whole "<ref>:<path>" object name is one positional, so it goes
	// behind the ref separator — a caller-derived ref is still a flag to git
	// even when a ":path" is glued to it.
	if gitNoOut(repoDir, gitRefArgs("cat-file", []string{"-e"}, ref+":"+rel)...) != nil {
		return nil // the target carries no copy — the ordinary case
	}
	return fmt.Errorf(`refusing to switch to %s: it would overwrite your live %s.%s

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
		fastForwardRemedy(repoDir, ref, rel),
		ref,
		rel, rel,
		rel,
		ref,
		rel, rel,
		ref, rel)
}

// fastForwardRemedy returns the lead remedy — a fast-forward — for the one case
// where it is both cheaper than the two below and actually sufficient: ref is a
// local branch merely BEHIND its remote, and the remote no longer carries the
// tracked copy.
//
// Both conditions, not either (locked ff_lead_requires_a_clean_remote). A
// remedy is only the cheapest correct fix if it is CORRECT: fast-forwarding
// onto a remote that still tracks the file moves the refusal rather than
// clearing it, so the user does the work, re-runs, and is refused again by the
// same guard for the same reason — which teaches them the guidance is guesswork.
//
// Returns "" for every other shape (no remote-tracking ref, diverged, remote
// still carrying the file), leaving the existing remedies exactly as they were.
// It is an addition and a reordering, never a replacement.
func fastForwardRemedy(repoDir, ref, rel string) string {
	// Only a local branch has a remote to catch up with. A ref that is already
	// remote-tracking, a tag or a raw sha has nothing to fast-forward from.
	if gitNoOut(repoDir, gitRefArgs("rev-parse", []string{"--verify", "--quiet"}, "refs/heads/"+ref)...) != nil {
		return ""
	}
	remoteRef := "refs/remotes/origin/" + ref
	if gitNoOut(repoDir, gitRefArgs("rev-parse", []string{"--verify", "--quiet"}, remoteRef)...) != nil {
		return ""
	}
	// Behind, not diverged: the local ref must be an ANCESTOR of the remote, or
	// a fast-forward is not available and the lead line would be an instruction
	// that errors.
	behind, err := isAncestor(repoDir, ref, remoteRef)
	if err != nil || !behind {
		return ""
	}
	// Already up to date is not "behind" — nothing to suggest.
	if same, err := isAncestor(repoDir, remoteRef, ref); err != nil || same {
		return ""
	}
	// The remote must not carry the file itself, or catching up re-introduces
	// exactly what is being refused.
	if gitNoOut(repoDir, gitRefArgs("cat-file", []string{"-e"}, "origin/"+ref+":"+rel)...) == nil {
		return ""
	}
	return fmt.Sprintf(`

%s is only BEHIND origin/%s, and origin/%s no longer carries that file — so
catching up is enough, and is the cheapest fix:

    git fetch origin && git branch --force %s origin/%s

The two remedies below still work, and are what you need if the fast-forward
is not available to you.`, ref, ref, ref, ref, ref)
}
