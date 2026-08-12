package cmd

import (
	"fmt"

	"github.com/Rivil/dross/internal/milestone"
)

// This file classifies what `milestone complete --finalize` is looking at,
// before anything is torn down. It reads; it never writes, never fetches and
// never deletes a ref — milestoneFinalize owns all of that and consumes the
// verdict.
//
// The classification exists because the old inline guard collapsed three
// different situations into one refusal. A milestone that had already been
// finalized, and one whose branch was deleted by hand, both came back as the
// unmerged-branch refusal below — a message that is false in both cases and
// points at a PR that already merged. Separating the states is the whole point:
// each one gets its own message and its own arm.

// milestoneStatusComplete is the [milestone].status a finalized milestone
// carries — one of configenum.MilestoneStatuses. It gets a name here because it
// became load-bearing rather than descriptive: finalize writes it and reads it
// back as the authoritative already-finalized marker.
const milestoneStatusComplete = "complete"

// finalizeState is the verdict. A string rather than an int so a failed test
// prints the state instead of an ordinal.
type finalizeState string

const (
	// finalizeAlreadyDone: the milestone toml already reads status="complete".
	// That marker is authoritative and is checked before any git question is
	// asked (locked already_finalized_evidence) — a successful finalize deletes
	// the branch local and remote, so afterwards git ancestry has nothing left
	// to answer with.
	finalizeAlreadyDone finalizeState = "already-finalized"
	// finalizeBranchGone: origin no longer carries milestone/<version>, and the
	// toml does not say it was finalized. Something removed the branch outside
	// dross. Nothing to merge-check and nothing to delete.
	finalizeBranchGone finalizeState = "branch-gone"
	// finalizeMerged: origin's branch is contained in Target. Teardown is safe.
	finalizeMerged finalizeState = "merged"
	// finalizeUnmerged: the branch is still on origin and has landed nowhere.
	// The one case the original refusal was written for.
	finalizeUnmerged finalizeState = "unmerged"
)

// finalizeClassification is the full verdict: the state, the branch the
// milestone was measured against, whether main specifically contains it, and
// the message to print or return.
type finalizeClassification struct {
	State finalizeState
	// Target is the branch the merge was measured against — the main branch,
	// or the recorded base when this milestone was stacked on a live parent.
	// Set for merged and unmerged; empty for the other two, which never got as
	// far as an ancestry question.
	Target string
	// MergedIntoMain distinguishes a milestone contained in origin/<main> from
	// a stacked child contained only in its parent. Only the former advances
	// local main, so milestoneFinalize needs the two apart even though both
	// classify as merged (locked stacked_child_status).
	MergedIntoMain bool
	// Message is the rendered narration for this state: the error text for the
	// refusing arms, the already-finalized line for the short-circuit.
	Message string
}

// classifyFinalize answers what state milestone <version> is in, without
// changing anything.
//
// Order matters and is not incidental:
//
//  1. status="complete" wins outright. It is the marker a successful finalize
//     wrote, and it survives the branch deletes that erase git's own evidence.
//  2. no origin branch is "gone", not "unmerged". The old code could not tell
//     these apart and reported the wrong one.
//  3. only then ancestry, against the recorded base as well as main.
//
// A milestone toml that will not load is not fatal here: the git-side questions
// still have answers, so classification falls through to them with the main
// branch as its target. Refusing to classify would wedge finalize on a repo
// whose toml is merely absent.
func classifyFinalize(root, repoDir, mainBranch, msBranch, version string) (finalizeClassification, error) {
	target := mainBranch
	if m, err := milestone.Load(milestone.FilePath(root, version)); err == nil {
		if m.Milestone.Status == milestoneStatusComplete {
			return finalizeClassification{
				State:   finalizeAlreadyDone,
				Message: fmt.Sprintf("milestone %s is already finalized (status = %s) — nothing to do", version, milestoneStatusComplete),
			}, nil
		}
		// The branch this milestone was stacked on, when it is still a live
		// target on origin. Everything else resolves to the main branch.
		if b := m.BaseOr(mainBranch); b != mainBranch && b != msBranch && gitRefExists(repoDir, "refs/remotes/origin/"+b) {
			target = b
		}
	}

	if !gitRefExists(repoDir, "refs/remotes/origin/"+msBranch) {
		return finalizeClassification{
			State: finalizeBranchGone,
			Message: fmt.Sprintf("origin/%s is gone — there is no milestone branch left to finalize. "+
				"If %s is finished, record that with `dross milestone set %s status %s`",
				msBranch, version, version, milestoneStatusComplete),
		}, nil
	}

	// An origin that carries no main branch is not an error to raise here — it
	// simply cannot contain the milestone, so the ancestry question against it
	// is skipped rather than asked and failed.
	var mergedIntoMain bool
	var err error
	if gitRefExists(repoDir, "refs/remotes/origin/"+mainBranch) {
		mergedIntoMain, err = isAncestor(repoDir, "refs/remotes/origin/"+msBranch, "refs/remotes/origin/"+mainBranch)
		if err != nil {
			return finalizeClassification{}, err
		}
	}
	mergedIntoTarget := mergedIntoMain
	if !mergedIntoMain && target != mainBranch {
		mergedIntoTarget, err = isAncestor(repoDir, "refs/remotes/origin/"+msBranch, "refs/remotes/origin/"+target)
		if err != nil {
			return finalizeClassification{}, err
		}
	}
	if !mergedIntoTarget {
		return finalizeClassification{
			State:  finalizeUnmerged,
			Target: target,
			Message: fmt.Sprintf("origin/%s is not merged into origin/%s yet — has the milestone PR merged? Refusing so the milestone branch isn't lost",
				msBranch, target),
		}, nil
	}
	return finalizeClassification{
		State:          finalizeMerged,
		Target:         target,
		MergedIntoMain: mergedIntoMain,
		Message:        fmt.Sprintf("origin/%s is merged into origin/%s", msBranch, target),
	}, nil
}
