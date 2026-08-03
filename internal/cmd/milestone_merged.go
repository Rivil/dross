package cmd

import (
	"errors"
	"fmt"
)

// This file answers one question and writes nothing: has milestone/<version>
// already landed on the main branch. `milestone create` uses the answer to
// decide whether to stack a new milestone on it, `milestone complete` to decide
// which branch its PR targets, and the delete gate to decide whether a
// dependent still needs it.
//
// The answer comes from git ancestry, never from `milestone.status` (locked
// decision: unmerged_test). Status is user-editable metadata that can lie — a
// milestone left at status="active" after its PR merged would stack the next
// milestone onto a branch that is about to be deleted.

// milestoneMergedIntoMain reports whether branch is already contained in
// mainBranch.
//
// It fetches origin best-effort and prefers origin's view, because a merge
// lands there first: local main can sit days behind the merge that closed the
// milestone. When the fetch fails or origin carries neither ref, it answers
// from this repo's own refs and returns localOnly so the caller can narrate the
// caveat rather than presenting a possibly-stale answer as fact.
//
// A branch that exists nowhere — no origin ref, no local ref — reads as merged.
// That is the ordinary state after `milestone complete --finalize` deleted it,
// and a branch that is gone is nothing to stack on. Erroring here would wedge
// every later `milestone create` until the user hand-cleared current_milestone.
func milestoneMergedIntoMain(repoDir, branch, mainBranch string) (merged, localOnly bool, err error) {
	if branch == "" {
		return false, false, errors.New("merged check: no branch given")
	}
	if mainBranch == "" {
		return false, false, errors.New("merged check: no main branch given")
	}

	// Best-effort: offline is a supported mode, not a failure.
	fetched := gitNoOut(repoDir, "fetch", "-q", "origin") == nil

	remoteRef := "refs/remotes/origin/" + branch
	remoteMain := "refs/remotes/origin/" + mainBranch
	localRef := "refs/heads/" + branch
	localMain := "refs/heads/" + mainBranch

	hasRemote := gitRefExists(repoDir, remoteRef)
	hasLocal := gitRefExists(repoDir, localRef)
	if !hasRemote && !hasLocal {
		return true, !fetched, nil
	}

	if fetched && hasRemote && gitRefExists(repoDir, remoteMain) {
		m, err := isAncestor(repoDir, remoteRef, remoteMain)
		return m, false, err
	}

	// Local answer: either the fetch failed, or origin has never seen one of
	// the two refs. Prefer refs/heads and fill in from the remote-tracking ref
	// only for a side this repo does not carry at all.
	ref, main := localRef, localMain
	if !hasLocal {
		ref = remoteRef
	}
	if !gitRefExists(repoDir, localMain) {
		if !gitRefExists(repoDir, remoteMain) {
			return false, true, fmt.Errorf("merged check: no such branch %q", mainBranch)
		}
		main = remoteMain
	}
	m, err := isAncestor(repoDir, ref, main)
	return m, true, err
}

// gitRefExists reports whether a fully-qualified ref resolves in repoDir.
func gitRefExists(repoDir, ref string) bool {
	return gitNoOut(repoDir, "rev-parse", "--verify", "--quiet", ref) == nil
}
