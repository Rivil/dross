package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/configenum"
	"github.com/Rivil/dross/internal/protect"
	"github.com/Rivil/dross/internal/ship"
)

// milestone/* under the "dross: milestones" ruleset (phase
// main-branch-protection, c-10): a PR is required to change an existing
// milestone branch, but creating and deleting one stay open — milestone create
// cuts one, --finalize and prune delete them. origin plays that ruleset with a
// pre-receive hook that logs every update and rejects only an update to an
// existing milestone ref.

// milestoneOrigin installs the hook; accept lets every update through, as a
// bypass holder's push would, while still logging it.
func milestoneOrigin(t *testing.T, origin string, accept bool) (log string) {
	t.Helper()
	log = filepath.Join(t.TempDir(), "updates.log")
	verdict := "echo \"rejected $ref: a PR is required\" >&2; exit 1"
	if accept {
		verdict = ":"
	}
	hook := "#!/bin/sh\nzero() { [ -z \"$(printf '%s' \"$1\" | tr -d 0)\" ]; }\n" +
		"while read old new ref; do\n  echo \"$ref $old $new\" >> '" + log + "'\n" +
		"  case \"$ref\" in refs/heads/milestone/*) if ! zero \"$old\" && ! zero \"$new\"; then " + verdict + "; fi ;; esac\ndone\n"
	if err := os.WriteFile(filepath.Join(origin, "hooks", "pre-receive"), []byte(hook), 0o755); err != nil {
		t.Fatal(err)
	}
	return log
}

// milestoneUpdates returns the logged updates of an existing milestone ref —
// creations and deletions excluded.
func milestoneUpdates(t *testing.T, log string) []string {
	t.Helper()
	b, err := os.ReadFile(log)
	if err != nil {
		return nil
	}
	isZero := func(s string) bool { return strings.Trim(s, "0") == "" }
	var out []string
	for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		f := strings.Fields(line)
		if len(f) == 3 && strings.HasPrefix(f[0], "refs/heads/milestone/") && !isZero(f[1]) && !isZero(f[2]) {
			out = append(out, f[0])
		}
	}
	return out
}

// msRepo is a dross repo on provider with main pushed and milestone/v1 cut
// from it and pushed, checked out.
func msRepo(t *testing.T, provider string) (dir, origin string) {
	t.Helper()
	dir, origin = setupMilestoneRepo(t)
	setProtectRemote(t, dir, provider, "")
	mustGit(t, dir, "commit", "-q", "--allow-empty", "-am", "remote")
	mustGit(t, dir, "push", "-q", "-u", "origin", "main")
	mustGit(t, dir, "switch", "-q", "-c", "milestone/v1")
	mustGit(t, dir, "push", "-q", "-u", "origin", "milestone/v1")
	return dir, origin
}

func msComplete(t *testing.T, args ...string) (string, error) {
	t.Helper()
	return runCapture(t, nil, func() error {
		return runCmd(t, Milestone(), append([]string{"complete", "v1", "--force-unverified"}, args...)...)
	})
}

// openedHeads returns the head branches of the PRs the stub opened.
func openedHeads(s *choreSeams) []string {
	var out []string
	for _, o := range s.opened {
		out = append(out, o.HeadBranch+"→"+o.BaseBranch)
	}
	return out
}

// landOnMain merges milestone/<v> into origin's main the way its
// integration PR would, through a throwaway branch.
func landOnMain(t *testing.T, dir, v string) {
	t.Helper()
	mustGit(t, dir, "switch", "-q", "-c", "mergetmp", "main")
	mustGit(t, dir, "merge", "--no-ff", "-q", "-m", "Merge milestone "+v, "milestone/"+v)
	mustGit(t, dir, "push", "-q", "origin", "mergetmp:main")
	mustGit(t, dir, "switch", "-q", "main")
	mustGit(t, dir, "branch", "-q", "-D", "mergetmp")
	mustGit(t, dir, "fetch", "-q", "origin")
}

// Create, prune and --finalize never update an existing milestone ref, so a
// PR-only ruleset leaves the branch lifecycle working.
func TestMilestoneRefLifecycleUnderProtection(t *testing.T) {
	dir, origin := setupMilestoneRepo(t)
	setProtectRemote(t, dir, "github", "")
	mustGit(t, dir, "commit", "-q", "--allow-empty", "-am", "remote")
	mustGit(t, dir, "push", "-q", "-u", "origin", "main")
	log := milestoneOrigin(t, origin, false)
	stubBranchRules(t, liveFrom(t, protect.MilestoneRuleset(), 2))
	stubOpenPRs(t, nil, nil)
	stubChoreSeams(t, &choreSeams{})

	for _, v := range []string{"v1", "v2"} {
		if err := runCmd(t, Milestone(), "create", v); err != nil {
			t.Fatalf("create %s under the hook: %v", v, err)
		}
		if originRef(t, origin, "refs/heads/milestone/"+v) == "" {
			t.Fatalf("milestone/%s was not created on origin", v)
		}
	}
	gitCommit(t, dir, "chore(dross): scope v1 and v2")
	mustGit(t, dir, "push", "-q", "origin", "main")

	landOnMain(t, dir, "v1")
	if err := runCmd(t, Milestone(), "complete", "v1", "--finalize"); err != nil {
		t.Fatalf("--finalize under the hook: %v", err)
	}
	if originRef(t, origin, "refs/heads/milestone/v1") != "" {
		t.Error("--finalize did not delete milestone/v1 on origin")
	}

	landOnMain(t, dir, "v2")
	mustGit(t, dir, "merge", "-q", "--ff-only", "origin/main")
	if err := runCmd(t, Milestone(), "set", "v2", "milestone.status", milestoneStatusComplete); err != nil {
		t.Fatal(err)
	}
	if err := runCmd(t, Milestone(), "prune", "--yes"); err != nil {
		t.Fatalf("prune under the hook: %v", err)
	}
	if b := mustGit(t, dir, "branch", "--list", "milestone/v2"); b != "" {
		t.Errorf("prune left milestone/v2: %q", b)
	}
	if got := milestoneUpdates(t, log); len(got) != 0 {
		t.Errorf("a lifecycle step updated an existing milestone ref: %v", got)
	}
}

// .dross chores ahead on a protected milestone branch go through a chore PR
// into it; the integration PR opens only once that PR has merged.
func TestMilestoneChoresRouteThroughChorePR(t *testing.T) {
	t.Run("merged at once: the integration PR opens", func(t *testing.T) {
		dir, origin := msRepo(t, "github")
		choreCommit(t, dir, "scope.md")
		log := milestoneOrigin(t, origin, false)
		stubBranchRules(t, liveFrom(t, protect.MilestoneRuleset(), 2))
		s := &choreSeams{autoRes: ship.AutoMergeResult{Merged: true}}
		stubChoreSeams(t, s)

		out, err := msComplete(t)
		if err != nil {
			t.Fatalf("complete: %v\n%s", err, out)
		}
		heads := openedHeads(s)
		if len(heads) != 2 || heads[0] != "dross-chores/milestone-v1→milestone/v1" || heads[1] != "milestone/v1→main" {
			t.Errorf("opened %v; want the chore PR, then the integration PR", heads)
		}
		if got := milestoneUpdates(t, log); len(got) != 0 {
			t.Errorf("the hook was offered %v", got)
		}
	})
	t.Run("still open: the integration PR waits", func(t *testing.T) {
		dir, origin := msRepo(t, "github")
		choreCommit(t, dir, "scope.md")
		milestoneOrigin(t, origin, false)
		stubBranchRules(t, liveFrom(t, protect.MilestoneRuleset(), 2))
		s := &choreSeams{autoRes: ship.AutoMergeResult{AutoEnabled: true}}
		stubChoreSeams(t, s)

		_, err := msComplete(t)
		if err == nil || !strings.Contains(err.Error(), "https://github.com/o/r/pull/41") {
			t.Fatalf("err = %v; want a wait naming the chore PR", err)
		}
		if heads := openedHeads(s); len(heads) != 1 {
			t.Errorf("opened %v; the integration PR must wait for the chore PR", heads)
		}
	})
}

// A chore PR an earlier run left open blocks the integration PR too.
func TestMilestoneCompleteWaitsForOpenChorePR(t *testing.T) {
	msRepo(t, "github")
	stubBranchRules(t, liveFrom(t, protect.MilestoneRuleset(), 2))
	s := &choreSeams{open: &ship.OpenResult{Number: 9, URL: "https://github.com/o/r/pull/9"}}
	stubChoreSeams(t, s)
	_, err := msComplete(t)
	if err == nil || !strings.Contains(err.Error(), "https://github.com/o/r/pull/9") || !strings.Contains(err.Error(), "milestone/v1") {
		t.Fatalf("err = %v; want a refusal naming chore PR #9 into milestone/v1", err)
	}
	if len(s.opened) != 0 {
		t.Errorf("opened %v while a chore PR was open", openedHeads(s))
	}
}

// Code on a protected milestone branch reaches it through a phase PR.
func TestMilestoneCodeAheadRefuses(t *testing.T) {
	dir, origin := msRepo(t, "github")
	before := originRef(t, origin, "refs/heads/milestone/v1")
	mustWrite(t, filepath.Join(dir, "late.txt"), "code\n")
	mustGit(t, dir, "add", "late.txt")
	mustGit(t, dir, "commit", "-q", "-m", "late work on the milestone branch")
	log := milestoneOrigin(t, origin, false)
	stubBranchRules(t, liveFrom(t, protect.MilestoneRuleset(), 2))
	s := &choreSeams{}
	stubChoreSeams(t, s)

	_, err := msComplete(t)
	if err == nil || !strings.Contains(err.Error(), "phase branch") || !strings.Contains(err.Error(), "dross ship") {
		t.Fatalf("err = %v; want a refusal naming the phase-PR route", err)
	}
	if len(s.opened) != 0 || originRef(t, origin, "refs/heads/milestone/v1") != before {
		t.Error("code ahead still pushed or opened a PR")
	}
	if got := milestoneUpdates(t, log); len(got) != 0 {
		t.Errorf("the hook was offered %v", got)
	}
}

// Every other forge, and an unprotected GitHub branch, keep today's push —
// code included — and only GitHub is ever asked about protection.
func TestMilestoneHeadLegacyTable(t *testing.T) {
	for _, provider := range append(configenum.ShipProviders.Values(), "") {
		probes := 0
		if provider == "github" {
			probes = 1
		}
		t.Run("provider="+provider, func(t *testing.T) {
			dir, origin := msRepo(t, provider)
			mustWrite(t, filepath.Join(dir, "late.txt"), "code\n")
			mustGit(t, dir, "add", "late.txt")
			mustGit(t, dir, "commit", "-q", "-m", "late work")
			calls := stubBranchRules(t, ship.BranchRulesResult{Known: true})
			pushed, commits, err := pushMilestoneHeadIfAhead(dir, "milestone/v1")
			if err != nil || !pushed || commits != 1 {
				t.Fatalf("pushed=%v commits=%d err=%v; want today's push of 1 commit", pushed, commits, err)
			}
			if originRef(t, origin, "refs/heads/milestone/v1") != mustGit(t, dir, "rev-parse", "milestone/v1") {
				t.Error("origin did not take the push")
			}
			if *calls != probes {
				t.Errorf("%d protection probes, want %d", *calls, probes)
			}
		})
	}
}

// Unknown pushes nothing, even to an origin that would accept it.
func TestMilestoneUnknownProbeNeverPushes(t *testing.T) {
	dir, origin := msRepo(t, "github")
	before := originRef(t, origin, "refs/heads/milestone/v1")
	choreCommit(t, dir, "scope.md")
	log := milestoneOrigin(t, origin, true)
	stubBranchRules(t, ship.BranchRulesResult{Reason: "no answer from GitHub within 20s"})
	stubChoreSeams(t, &choreSeams{})

	_, _, err := pushMilestoneHeadIfAhead(dir, "milestone/v1")
	if err == nil || !strings.Contains(err.Error(), "no answer from GitHub within 20s") || !strings.Contains(err.Error(), "gh auth login") {
		t.Fatalf("err = %v; want a refusal naming the reason and the fix", err)
	}
	if got := milestoneUpdates(t, log); len(got) != 0 {
		t.Errorf("an unknown probe offered the hook %v", got)
	}
	if originRef(t, origin, "refs/heads/milestone/v1") != before {
		t.Error("origin's milestone branch moved")
	}
}
