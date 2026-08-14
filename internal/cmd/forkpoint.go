package cmd

// Fork-point resolution — the commit a phase branched from.
//
// `dross phase create` records it at fork time (changes.BaseCommit), which is
// the only moment it is knowable for free. The ~30 phase dirs that predate the
// field carry a base branch and nothing else, so the fork point is resolved on
// demand from that base plus the phase's own commits and cached back into the
// record on first use — the locked fork_point_backfill decision. There is no
// migration command to write, run and commit, and the check never degrades for
// want of a field.

import (
	"fmt"
	"sort"

	"github.com/Rivil/dross/internal/changes"
)

// phaseForkPoint returns the commit phaseID was forked from.
//
// A recorded fork point is returned as-is and costs no git call — that is what
// makes the cache observable: a second call for the same phase touches git not
// at all. A record with a base but no fork point is resolved and cached. A
// record with neither is an error naming the phase, never a blank SHA: a caller
// that got "" back would go on to ask git about the empty ref and get a
// confusing answer from two layers down instead of the real one here.
func phaseForkPoint(repoDir, root, phaseID string) (string, error) {
	path := changes.FilePath(root, phaseID)
	c, err := changes.Load(path, phaseID)
	if err != nil {
		return "", err
	}
	if c.BaseCommit != "" {
		return c.BaseCommit, nil
	}
	if c.Base == "" {
		return "", fmt.Errorf("phase %s records no base branch, so its fork point cannot be resolved; "+
			"re-record it with `dross phase create` semantics or set base in %s", phaseID, path)
	}
	sha, err := resolveForkPoint(repoDir, phaseID, c)
	if err != nil {
		return "", err
	}
	c.BaseCommit = sha
	if err := c.Save(path); err != nil {
		return "", fmt.Errorf("cache fork point for %s: %w", phaseID, err)
	}
	return sha, nil
}

// resolveForkPoint merge-bases the phase's recorded base against a commit that
// belongs to the phase. The phase branch is the obvious tip, but it is deleted
// on completion, so a completed phase falls back to the newest commit its own
// changes.json recorded — the phase's durable evidence of where it worked.
func resolveForkPoint(repoDir, phaseID string, c *changes.Changes) (string, error) {
	baseRef, err := resolvableRef(repoDir, c.Base, "origin/"+c.Base)
	if err != nil {
		return "", fmt.Errorf("phase %s: base %q resolves to no ref locally or on origin", phaseID, c.Base)
	}
	tips := phaseTipCandidates(phaseID, c)
	tipRef, err := resolvableRef(repoDir, tips...)
	if err != nil {
		return "", fmt.Errorf("phase %s: no commit of its own survives (tried %v), so its fork point off %s cannot be resolved",
			phaseID, tips, c.Base)
	}
	sha, err := gitTrim(repoDir, gitRefArgs("merge-base", nil, baseRef, tipRef)...)
	if err != nil {
		return "", fmt.Errorf("phase %s: merge-base %s %s: %w", phaseID, baseRef, tipRef, err)
	}
	return sha, nil
}

// phaseTipCandidates lists commit-ish names that should belong to the phase,
// most-authoritative first: its branch (local, then origin's copy), then the
// commits it recorded, newest first.
func phaseTipCandidates(phaseID string, c *changes.Changes) []string {
	branch := "phase/" + phaseID
	tips := []string{"refs/heads/" + branch, "refs/remotes/origin/" + branch}

	ids := make([]string, 0, len(c.Tasks))
	for id := range c.Tasks {
		if c.Tasks[id].Commit != "" {
			ids = append(ids, id)
		}
	}
	sort.Slice(ids, func(i, j int) bool {
		return c.Tasks[ids[i]].CompletedAt.After(c.Tasks[ids[j]].CompletedAt)
	})
	for _, id := range ids {
		tips = append(tips, c.Tasks[id].Commit)
	}
	return tips
}

// resolvableRef returns the first candidate that names an existing commit.
// ^{commit} is deliberate: a candidate that resolves to a tag or tree is not a
// commit-ish merge-base can work with, and a partial SHA recorded by an older
// dross still resolves through it.
func resolvableRef(repoDir string, candidates ...string) (string, error) {
	for _, ref := range candidates {
		if ref == "" {
			continue
		}
		if validateGitRef("fork point candidate", ref) != nil {
			continue
		}
		if gitNoOut(repoDir, gitRefArgs("rev-parse", []string{"--verify", "--quiet"}, ref+"^{commit}")...) == nil {
			return ref, nil
		}
	}
	return "", fmt.Errorf("no candidate ref resolves to a commit")
}
