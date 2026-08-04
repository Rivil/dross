package cmd

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"strings"
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
func staleMilestoneBranches(repoDir, mainBranch string) ([]staleBranch, error) {
	if mainBranch == "" {
		return nil, errors.New("stale milestone scan: no main branch given")
	}
	if err := gitNoOut(repoDir, gitRefArgs("rev-parse", []string{"--verify", "--quiet"}, mainBranch)...); err != nil {
		return nil, fmt.Errorf("stale milestone scan: no such branch %q", mainBranch)
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
			HasRemote: gitNoOut(repoDir, "rev-parse", "--verify", "--quiet", "refs/remotes/origin/"+name) == nil,
		}

		merged, err := isAncestor(repoDir, name, mainBranch)
		if err != nil {
			return nil, err
		}
		if merged {
			entry.Reason = reasonMerged
			stale = append(stale, entry)
			continue
		}

		squash, err := resolveSquashCommit(repoDir, name, mainBranch)
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
