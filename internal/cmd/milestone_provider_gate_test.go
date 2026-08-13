package cmd

import (
	"errors"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/ship"
)

// stubOpenPRs swaps the provider seam and records the base branch it was asked
// about, so a gate that queries main (or the milestone version) instead of the
// branch being deleted is caught.
func stubOpenPRs(t *testing.T, prs []ship.BasePR, err error) *struct {
	calls int
	bases []string
} {
	t.Helper()
	rec := &struct {
		calls int
		bases []string
	}{}
	prev := ship.OpenPRsTargetingFunc
	ship.OpenPRsTargetingFunc = func(_ ship.OpenOpts, base string) ([]ship.BasePR, error) {
		rec.calls++
		rec.bases = append(rec.bases, base)
		return prs, err
	}
	t.Cleanup(func() { ship.OpenPRsTargetingFunc = prev })
	return rec
}

// configureRemoteAndCommit points the project at a provider and commits, so
// commands that refuse a dirty tree can still run.
func configureRemoteAndCommit(t *testing.T, dir string) {
	t.Helper()
	for _, set := range [][]string{
		{"set", "remote.provider", "github"},
		{"set", "remote.url", "https://github.com/me/p"},
	} {
		if err := runCmd(t, Project(), set...); err != nil {
			t.Fatalf("project %v: %v", set, err)
		}
	}
	mustGit(t, dir, "add", "-A")
	mustGit(t, dir, "commit", "-q", "-m", "chore: remote config")
}

// An open PR on the forge blocks the delete even when the record scan is clean
// — that is the whole point of the second layer.
func TestPruneRefusesOnOpenProviderPR(t *testing.T) {
	dir := stackedPruneFixture(t)
	// Clear the record-scan dependent so only the provider gate can refuse.
	writeMilestoneWithBase(t, dir, "v1.3", "main")
	configureRemoteAndCommit(t, dir)
	rec := stubOpenPRs(t, []ship.BasePR{{Number: 7, URL: "https://github.com/me/p/pull/7", HeadRefName: "milestone/v1.3"}}, nil)

	err := runCmd(t, Milestone(), "prune")
	if err == nil {
		t.Fatal("prune deleted a branch with an open PR still based on it")
	}
	if !strings.Contains(err.Error(), "#7") {
		t.Errorf("the refusal must name the open PR: %v", err)
	}
	if !branchExists(t, dir, "milestone/v1.2") {
		t.Error("local milestone/v1.2 was deleted despite the refusal")
	}
	if !remoteHas(t, dir, "milestone/v1.2") {
		t.Error("origin/milestone/v1.2 was deleted despite the refusal")
	}
	// The query has to be about the branch being deleted.
	if len(rec.bases) == 0 || rec.bases[0] != "milestone/v1.2" {
		t.Errorf("provider was asked about %v, want milestone/v1.2", rec.bases)
	}
}

// A single stubbed PR never reaches describePRs' plural path — the i>0
// comma-join inside the loop and the len(prs)!=1 -> "PRs " prefix both need
// two or more results to fire.
func TestPruneRefusesNamingAllOpenProviderPRs(t *testing.T) {
	dir := stackedPruneFixture(t)
	writeMilestoneWithBase(t, dir, "v1.3", "main")
	configureRemoteAndCommit(t, dir)
	stubOpenPRs(t, []ship.BasePR{
		{Number: 7, URL: "https://github.com/me/p/pull/7"},
		{Number: 9, URL: "https://github.com/me/p/pull/9"},
	}, nil)

	err := runCmd(t, Milestone(), "prune")
	if err == nil {
		t.Fatal("prune deleted a branch with two open PRs still based on it")
	}
	if !strings.Contains(err.Error(), "PRs #7 (https://github.com/me/p/pull/7), #9 (https://github.com/me/p/pull/9)") {
		t.Errorf("the refusal must name every open PR, comma-joined, plural prefix: %v", err)
	}
	if !branchExists(t, dir, "milestone/v1.2") {
		t.Error("local milestone/v1.2 was deleted despite the refusal")
	}
	if !remoteHas(t, dir, "milestone/v1.2") {
		t.Error("origin/milestone/v1.2 was deleted despite the refusal")
	}
}

// c-6 names both commands: wiring the gate into prune alone would leave the
// destructive finalize path unguarded.
func TestFinalizeRefusesOnOpenProviderPR(t *testing.T) {
	dir := mergedIntoParentFixture(t)
	configureRemoteAndCommit(t, dir)
	rec := stubOpenPRs(t, []ship.BasePR{{Number: 7, URL: "https://github.com/me/p/pull/7"}}, nil)

	err := runCmd(t, Milestone(), "complete", "v1.2", "--finalize")
	if err == nil {
		t.Fatal("finalize deleted a branch with an open PR still based on it")
	}
	if !strings.Contains(err.Error(), "#7") {
		t.Errorf("the refusal must name the open PR: %v", err)
	}
	if !branchExists(t, dir, "milestone/v1.2") {
		t.Error("local milestone/v1.2 was deleted despite the refusal")
	}
	if !remoteHas(t, dir, "milestone/v1.2") {
		t.Error("origin/milestone/v1.2 was deleted despite the refusal")
	}
	if len(rec.bases) == 0 || rec.bases[0] != "milestone/v1.2" {
		t.Errorf("provider was asked about %v, want milestone/v1.2", rec.bases)
	}
}

// A lookup failure is announced and the command proceeds on the record scan —
// a silent swallow would read as "the forge confirmed nothing depends on this".
func TestPruneAnnouncesProviderLookupSkip(t *testing.T) {
	dir := stackedPruneFixture(t)
	writeMilestoneWithBase(t, dir, "v1.3", "main")
	configureRemoteAndCommit(t, dir)
	stubOpenPRs(t, nil, errors.New("gh not installed"))

	var out string
	if err := runCmdCapturing(t, &out, Milestone(), "prune"); err != nil {
		t.Fatalf("a failed lookup must not block a clean record scan: %v", err)
	}
	if !strings.Contains(out, "open-PR check skipped") || !strings.Contains(out, "gh not installed") {
		t.Errorf("the skip must be announced with its reason:\n%s", out)
	}
	if branchExists(t, dir, "milestone/v1.2") {
		t.Error("a clean record scan should still have pruned milestone/v1.2")
	}
}

// No provider configured: same announced skip, and nothing is asked of a forge
// that isn't there.
func TestPruneAnnouncesSkipWithNoProvider(t *testing.T) {
	dir := stackedPruneFixture(t)
	writeMilestoneWithBase(t, dir, "v1.3", "main")
	rec := stubOpenPRs(t, []ship.BasePR{{Number: 7}}, nil)

	var out string
	if err := runCmdCapturing(t, &out, Milestone(), "prune"); err != nil {
		t.Fatalf("prune: %v", err)
	}
	if !strings.Contains(out, "open-PR check skipped") {
		t.Errorf("an unconfigured provider must still announce the skip:\n%s", out)
	}
	if rec.calls != 0 {
		t.Errorf("provider was queried %d times with no [remote] configured", rec.calls)
	}
	if branchExists(t, dir, "milestone/v1.2") {
		t.Error("a clean record scan should still have pruned milestone/v1.2")
	}
}
