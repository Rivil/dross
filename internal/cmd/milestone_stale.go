package cmd

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"github.com/Rivil/dross/internal/milestone"
)

// This file answers one question and writes nothing: which milestone/* branches
// are still sitting in the repo after their work already landed on the main
// branch. `dross doctor` reports the answer and `dross milestone prune` acts on
// it (locked decision: prune_surface) — the detector itself never deletes a ref,
// so a diagnostic can never turn into a destructive act by accident.

// staleBranch is one milestone/* branch whose work is already on the main
// branch, together with the evidence that says so.
type staleBranch struct {
	// Name is the branch name ("milestone/v1.0").
	Name string
	// Version is the milestone id the branch carries ("v1.0").
	Version string
	// Reason is "merged" (an ancestor of main) or "squash-merged" (its content
	// is on main as a single commit that main does not descend from).
	Reason string
	// Squash is the resolved squash commit, empty when Reason is "merged".
	Squash string
	// HasRemote reports whether origin still carries the branch.
	HasRemote bool
}

const (
	reasonMerged = "merged"
	reasonSquash = "squash-merged"
)

// staleMilestoneBranches lists every refs/heads/milestone/* branch whose work is
// already on mainBranch.
//
// Ancestry comes first: a branch main descends from is plainly merged, and that
// is the cheap answer. Everything else goes through a squash-aware content check,
// because this repo squash-merges (repo.squash_merge) and `git branch --merged`
// is blind to a squash — the merged content is on main under a commit main's
// history does not reach.
//
// The content check resolves the squash commit rather than guessing at it: it
// walks main's commits that the branch does not carry and compares each one's own
// patch against the branch synthesized as a single commit. The candidate's patch
// is taken against ITS OWN FIRST PARENT, not against the fork-point merge-base —
// with main advancing between the fork and the merge, a diff from the fork point
// also carries those unrelated main commits and never matches. That is the shape
// this repo's own history has.
//
// A branch with no matching commit is simply not stale: an amended or rewritten
// squash is unresolvable, and reporting nothing is the safe answer for something
// `dross milestone prune` will delete.
//
// The comparison is made against ORIGIN's main branch, not this repo's — see
// resolveMainCompareRef. Local main is a working copy that can be ahead of the
// shared branch by commits nobody else has, and a merge measured against it
// reports work as landed that origin has never seen.
//
// A branch is only ever reported once its milestone says it is finished. Merged
// content is not the same claim as a finished milestone: a milestone branch
// whose first phase has already squash-merged into main has all of its current
// work "on main" and is very much still in use. Gating on the milestone's own
// status is what separates the two, and root is where that status is read from.
func staleMilestoneBranches(root, repoDir, mainBranch string) ([]staleBranch, error) {
	if mainBranch == "" {
		return nil, errors.New("stale milestone scan: no main branch given")
	}
	compare, err := resolveMainCompareRef(repoDir, mainBranch)
	if err != nil {
		return nil, err
	}

	listed, err := gitTrim(repoDir, "for-each-ref", "--format=%(refname:short)", "refs/heads/milestone/*")
	if err != nil {
		return nil, fmt.Errorf("list milestone branches: %w", err)
	}
	if listed == "" {
		return nil, nil
	}

	var stale []staleBranch
	for _, name := range strings.Split(listed, "\n") {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		entry := staleBranch{
			Name:      name,
			Version:   strings.TrimPrefix(name, "milestone/"),
			HasRemote: gitNoOut(repoDir, gitRefArgs("rev-parse", []string{"--verify", "--quiet"}, "refs/remotes/origin/"+name)...) == nil,
		}
		if !milestoneIsFinished(root, entry.Version) {
			continue
		}

		merged, err := isAncestor(repoDir, name, compare)
		if err != nil {
			return nil, err
		}
		if merged {
			entry.Reason = reasonMerged
			stale = append(stale, entry)
			continue
		}

		squash, err := resolveSquashCommit(repoDir, name, compare)
		if err != nil {
			return nil, err
		}
		if squash == "" {
			continue
		}
		entry.Reason = reasonSquash
		entry.Squash = squash
		stale = append(stale, entry)
	}
	return stale, nil
}

// milestoneIsFinished reports whether milestones/<version>.toml says this
// milestone is done — the gate a branch has to pass before it can be called
// stale.
//
// Only status="complete" qualifies. Everything else fails closed: "active" is
// obviously live, "planning" is a branch cut before the work started, an empty
// status is a milestone that never said, and a version with no toml at all is
// unknown (locked toml_less_branch_not_stale). The consumer is `dross milestone
// prune`, which deletes local AND remote, so the ambiguous cases have to answer
// "leave it alone".
//
// This is the one place merged-ness is not the whole answer. Under the
// milestone-branch model phases squash-merge into milestone/<version> and the
// milestone merges into main at the end, so a milestone whose early phases have
// landed can look entirely "already on main" while the next phase is still
// being written against it.
func milestoneIsFinished(root, version string) bool {
	m, err := milestone.Load(milestone.FilePath(root, version))
	if err != nil {
		return false
	}
	return m.Milestone.Status == milestoneStatusComplete
}

// resolveMainCompareRef picks the ref every merged-ness question in this file is
// asked against: refs/remotes/origin/<main> when origin carries it, and only
// otherwise refs/heads/<main>.
//
// Origin first because the consumer is a destructive prune and local main is not
// the shared branch. A repo whose main is pushed-ahead of origin — a `dross
// quick` committed but not pushed, a squash landed locally — makes a
// freshly-cut milestone branch look merged into work nobody else has, and the
// branch is then deleted local AND remote on the strength of it.
//
// The local fallback is for a repo with no origin at all, or one whose main has
// never been pushed. There the local ref is the only answer available, and it is
// also the correct one: nothing else has a claim on that history.
func resolveMainCompareRef(repoDir, mainBranch string) (string, error) {
	if remote := "refs/remotes/origin/" + mainBranch; gitRefExists(repoDir, remote) {
		return remote, nil
	}
	if local := "refs/heads/" + mainBranch; gitRefExists(repoDir, local) {
		return local, nil
	}
	return "", fmt.Errorf("stale milestone scan: no such branch %q", mainBranch)
}

// resolveSquashCommit returns the commit on mainBranch whose patch is the
// branch's whole contribution collapsed into one commit, or "" when there is no
// such commit. Errors are reserved for a broken repo, never for "no answer".
func resolveSquashCommit(repoDir, branch, mainBranch string) (string, error) {
	base, err := gitTrim(repoDir, gitRefArgs("merge-base", nil, mainBranch, branch)...)
	if err != nil {
		// Unrelated histories: nothing to compare, so nothing is stale.
		return "", nil
	}
	want, err := patchIDOfDiff(repoDir, base, branch)
	if err != nil {
		return "", err
	}
	if want == "" {
		// The branch contributes no change at all — an empty patch would match
		// every empty commit on main, which is not evidence of anything.
		return "", nil
	}

	// "^<branch>" rather than "--not <branch>": --not is an OPTION, so behind
	// the separator git would read it as a revision. The caret form is exactly
	// equivalent and survives the fence.
	listed, err := gitTrim(repoDir, gitRefArgs("rev-list", nil, mainBranch, "^"+branch)...)
	if err != nil {
		return "", fmt.Errorf("walk %s: %w", mainBranch, err)
	}
	if listed == "" {
		return "", nil
	}

	// rev-list is newest-first and every match is collected, so two commits
	// carrying the same patch give one deterministic answer — the oldest, the
	// one that actually landed the work — instead of whichever the walk hit first.
	var matches []string
	for _, sha := range strings.Split(listed, "\n") {
		sha = strings.TrimSpace(sha)
		if sha == "" {
			continue
		}
		parent, err := gitTrim(repoDir, gitRefArgs("rev-parse", []string{"--verify", "--quiet"}, sha+"^")...)
		if err != nil {
			continue // a root commit has no first parent to diff against
		}
		got, err := patchIDOfDiff(repoDir, parent, sha)
		if err != nil {
			return "", err
		}
		if got != "" && got == want {
			matches = append(matches, sha)
		}
	}
	if len(matches) == 0 {
		return "", nil
	}
	return matches[len(matches)-1], nil
}

// patchIDOfDiff returns the stable patch-id of the diff between two tree-ishes,
// or "" when the diff is empty. --stable so the id does not shift with the order
// git happens to emit the files in.
func patchIDOfDiff(repoDir, from, to string) (string, error) {
	var diff bytes.Buffer
	d := exec.Command("git", append([]string{"-C", repoDir}, gitRefArgs("diff", nil, from, to)...)...)
	d.Stdout = &diff
	if err := d.Run(); err != nil {
		return "", fmt.Errorf("diff %s..%s: %w", from, to, err)
	}
	if diff.Len() == 0 {
		return "", nil
	}
	p := exec.Command("git", "-C", repoDir, "patch-id", "--stable")
	p.Stdin = &diff
	out, err := p.Output()
	if err != nil {
		return "", fmt.Errorf("patch-id %s..%s: %w", from, to, err)
	}
	fields := strings.Fields(string(out))
	if len(fields) == 0 {
		return "", nil
	}
	return fields[0], nil
}

// isAncestor reports whether ref is an ancestor of other. `merge-base
// --is-ancestor` answers with its exit code — 0 yes, 1 no — so only anything
// else is a real failure worth propagating.
func isAncestor(repoDir, ref, other string) (bool, error) {
	err := gitNoOut(repoDir, gitRefArgs("merge-base", []string{"--is-ancestor"}, ref, other)...)
	if err == nil {
		return true, nil
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() == 1 {
		return false, nil
	}
	return false, fmt.Errorf("ancestry check %s..%s: %w", ref, other, err)
}
