package cmd

// Red-proof reachability — is a pinned commit still there for someone else?
//
// A red proof is only worth the file it is written in if a reader can check out
// the commit it pins and watch the test go red again. The c-5 pin rotted exactly
// the way this file exists to catch: the commit sat on a phase branch that was
// squash-merged and deleted, so it survived on the author's machine (still
// referenced by a local branch, then as a loose object) and was gone for
// everyone else.
//
// Hence the locked reachability_scope decision: reachable means contained in one
// of ORIGIN's remote-tracking refs. Local branches do not count — they are the
// state a fresh clone does not have. `rev-parse --verify` succeeding does not
// count either: an unreferenced loose object answers that probe right up until
// gc collects it.

import (
	"fmt"
	"strings"
)

// reachability is the three-way verdict. Three, not a bool, because "I cannot
// see enough history to tell" is a different statement from "this commit is
// gone" — collapsing them either reddens every shallow CI clone or hides a real
// rot behind a missing fetch.
type reachability string

const (
	// reachReachable: some refs/remotes/origin/* ref contains the commit.
	reachReachable reachability = "reachable"
	// reachUnreachable: the repo can see origin's refs and none contains it —
	// including the case where the object is gone from the database entirely.
	reachUnreachable reachability = "unreachable"
	// reachIndeterminate: this repo cannot answer the question. A shallow clone
	// or a repo with no origin refs is an absence of history, never a verdict.
	reachIndeterminate reachability = "cannot-determine"
)

// originRefGlob is the ref namespace reachability is judged against: origin's
// tracking refs specifically, not every remote a contributor happens to have
// added. A fork's remote reaching a commit says nothing about whether the
// upstream everyone clones does.
const originRefGlob = "refs/remotes/origin/"

// classifyReachability decides whether sha is reachable from origin's
// remote-tracking refs in repoDir, returning the verdict and a one-line why for
// callers to print.
//
// Ordering is load-bearing:
//
//  1. Shallow clone → indeterminate, BEFORE the object probe. In a shallow
//     clone the pinned commit is legitimately absent; calling that unreachable
//     would redden every truncated checkout for a pin that is perfectly sound.
//  2. No origin refs at all → indeterminate, for the same reason: nothing to
//     judge against.
//  3. Object missing from the database → UNREACHABLE, not indeterminate. This
//     is the post-gc case the whole check is about, and git's non-zero exit
//     here must not be folded into the indeterminate arm — that would turn the
//     one outcome that matters into a shrug.
//  4. Otherwise: contained in an origin ref, or not.
func classifyReachability(repoDir, sha string) (reachability, string, error) {
	if strings.TrimSpace(sha) == "" {
		return "", "", fmt.Errorf("no SHA to check reachability for")
	}
	if err := validateGitRef("red-proof commit", sha); err != nil {
		return "", "", err
	}

	if shallow, err := gitTrim(repoDir, "rev-parse", "--is-shallow-repository"); err == nil && shallow == "true" {
		return reachIndeterminate, "shallow clone — history is truncated, so containment cannot be decided here", nil
	}
	refs, err := gitTrim(repoDir, gitRefArgs("for-each-ref", []string{"--format=%(refname)", "--count=1"}, originRefGlob)...)
	if err != nil || strings.TrimSpace(refs) == "" {
		return reachIndeterminate, "no " + originRefGlob + "* refs in this repo — nothing to judge containment against (try `git fetch origin`)", nil
	}

	if err := gitNoOut(repoDir, gitRefArgs("rev-parse", []string{"--verify", "--quiet"}, sha+"^{commit}")...); err != nil {
		return reachUnreachable, "commit is absent from this repo's object database", nil
	}

	// --contains rides in the opts half because it is a flag's ARGUMENT, not a
	// positional; validateGitRef above has already refused an option-shaped
	// value there. The glob after the separator is dross's own literal.
	got, err := gitTrim(repoDir, gitRefArgs("for-each-ref",
		[]string{"--format=%(refname)", "--count=1", "--contains", sha}, originRefGlob)...)
	if err != nil {
		return "", "", fmt.Errorf("for-each-ref --contains %s: %w", short(sha), err)
	}
	if strings.TrimSpace(got) == "" {
		return reachUnreachable, "no " + originRefGlob + "* ref contains it — a fresh clone would not have this commit", nil
	}
	return reachReachable, "contained in " + strings.TrimSpace(got), nil
}
