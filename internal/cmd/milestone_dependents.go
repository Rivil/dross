package cmd

import (
	"fmt"
	"sort"

	"github.com/Rivil/dross/internal/milestone"
	"github.com/Rivil/dross/internal/ship"
)

// This file answers one question and deletes nothing: is anything still
// depending on this milestone branch. `milestone prune` and `milestone complete
// --finalize` ask before removing a ref, because a remote branch delete under
// somebody's open PR is irreversible from their side.
//
// The gate reads the fact c-2 already stores — each milestone's recorded base —
// rather than inferring dependence from git topology, so it works offline, on
// every provider, and gives the same answer every time.

// dependentMilestones returns the versions of milestones that record `branch`
// as their base and have not themselves merged yet, in sorted order.
//
// The unmerged test matters: a gate keyed on "a dependent record exists" would
// wedge the repo permanently, since a merged child keeps its base recorded
// forever.
//
// It fails closed. An unreadable milestone toml is indistinguishable from "no
// dependents", so the scan errors rather than shrinking silently and
// authorizing a delete on partial evidence.
func dependentMilestones(root, repoDir, branch, mainBranch string) ([]string, error) {
	all, err := milestone.LoadAll(root)
	if err != nil {
		return nil, fmt.Errorf("scan milestone records: %w", err)
	}
	versions := make([]string, 0, len(all))
	for v := range all {
		versions = append(versions, v)
	}
	sort.Strings(versions)

	var deps []string
	for _, v := range versions {
		own := "milestone/" + v
		if own == branch {
			continue // a milestone naming itself as base does not self-block
		}
		if all[v].BaseOr(mainBranch) != branch {
			continue
		}
		merged, _, err := milestoneMergedIntoMain(repoDir, own, mainBranch)
		if err != nil {
			return nil, fmt.Errorf("merge status of %s: %w", own, err)
		}
		if !merged {
			deps = append(deps, v)
		}
	}
	return deps, nil
}

// guardMilestoneBranchDelete refuses to delete a milestone branch that anything
// still depends on, naming the dependent. Callers run it before touching any ref.
//
// Two layers, in order of authority. The record scan is offline, provider-
// agnostic and reads a fact dross itself wrote, so protection never depends on
// the network. The forge query on top of it catches PRs dross did not record —
// and when it cannot run, it says so out loud rather than passing silently for
// a clean scan (locked dependent_detection).
func guardMilestoneBranchDelete(root, repoDir, branch, mainBranch string) error {
	deps, err := dependentMilestones(root, repoDir, branch, mainBranch)
	if err != nil {
		return err
	}
	if len(deps) > 0 {
		return fmt.Errorf("refusing to delete %s: milestone %s %s stacked on it and not merged yet — finalize or retarget %s first",
			branch, joinVersions(deps), plural(len(deps), "is", "are"), joinVersions(deps))
	}
	return guardOpenPRsTargeting(branch)
}

// guardOpenPRsTargeting refuses when the forge still has an open PR based on
// branch. A provider that is unconfigured, unwired or unreachable is announced
// as a skip and the caller proceeds on the record scan alone — an unannounced
// skip would read exactly like "the forge confirmed nothing depends on this".
func guardOpenPRsTargeting(branch string) error {
	p, _, err := loadProject()
	if err != nil {
		printOpenPRSkip(err.Error())
		return nil
	}
	if p.Remote.Provider == "" || p.Remote.URL == "" {
		printOpenPRSkip("no [remote].provider/.url configured")
		return nil
	}
	prs, err := ship.OpenPRsTargetingFunc(buildOpenOpts(p), branch)
	if err != nil {
		printOpenPRSkip(err.Error())
		return nil
	}
	if len(prs) > 0 {
		return fmt.Errorf("refusing to delete %s: %s still open against it — retarget or close it first",
			branch, describePRs(prs))
	}
	return nil
}

func printOpenPRSkip(reason string) {
	Printf("open-PR check skipped (%s) — relying on the milestone records alone\n", reason)
}

func describePRs(prs []ship.BasePR) string {
	out := ""
	for i, pr := range prs {
		if i > 0 {
			out += ", "
		}
		out += fmt.Sprintf("#%d", pr.Number)
		if pr.URL != "" {
			out += " (" + pr.URL + ")"
		}
	}
	if len(prs) == 1 {
		return "PR " + out
	}
	return "PRs " + out
}

func joinVersions(vs []string) string {
	out := ""
	for i, v := range vs {
		switch {
		case i == 0:
			out = v
		case i == len(vs)-1:
			out += " and " + v
		default:
			out += ", " + v
		}
	}
	return out
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
