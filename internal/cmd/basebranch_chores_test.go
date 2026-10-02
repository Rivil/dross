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

// The base safety net on a protected base (phase main-branch-protection,
// c-5/c-10): origin plays GitHub with a pre-receive hook that logs every ref
// update it is offered and rejects any to main or milestone/*, so "nothing
// was pushed to the base" is read from the hook's log, not inferred.

// protectedOrigin installs that hook on the bare origin. accept makes it log
// but let protected updates through — a bypass holder — so a test can prove
// dross didn't even try.
func protectedOrigin(t *testing.T, origin string, accept bool) (log string) {
	t.Helper()
	log = filepath.Join(t.TempDir(), "pushes.log")
	verdict := "echo \"rejected $ref: protected\" >&2; exit 1"
	if accept {
		verdict = ":"
	}
	hook := "#!/bin/sh\nwhile read old new ref; do\n  echo \"$ref\" >> '" + log + "'\n" +
		"  case \"$ref\" in refs/heads/main|refs/heads/milestone/*) " + verdict + " ;; esac\ndone\n"
	if err := os.WriteFile(filepath.Join(origin, "hooks", "pre-receive"), []byte(hook), 0o755); err != nil {
		t.Fatal(err)
	}
	return log
}

// baseUpdates returns the protected-ref updates the hook was offered.
func baseUpdates(t *testing.T, log string) []string {
	t.Helper()
	b, err := os.ReadFile(log)
	if err != nil {
		return nil
	}
	var out []string
	for _, ref := range strings.Fields(string(b)) {
		if ref == "refs/heads/main" || strings.HasPrefix(ref, "refs/heads/milestone/") {
			out = append(out, ref)
		}
	}
	return out
}

// stubBranchRules answers every BranchRules call with res and counts them.
func stubBranchRules(t *testing.T, res ship.BranchRulesResult) *int {
	t.Helper()
	calls := 0
	prev := ship.BranchRulesFunc
	ship.BranchRulesFunc = func(ship.OpenOpts, string) ship.BranchRulesResult {
		calls++
		return res
	}
	t.Cleanup(func() { ship.BranchRulesFunc = prev })
	return &calls
}

func protectedRules(t *testing.T) ship.BranchRulesResult {
	t.Helper()
	rs, err := protect.MainRuleset("main", []string{"test"})
	if err != nil {
		t.Fatal(err)
	}
	return liveFrom(t, rs, 1)
}

// choreBase is a dross repo on a GitHub remote with main pushed and one
// .dross-only chore committed on top.
func choreBase(t *testing.T, provider string) (dir, origin string) {
	t.Helper()
	dir, origin = setupMilestoneRepo(t)
	setProtectRemote(t, dir, provider, "")
	mustGit(t, dir, "commit", "-q", "--allow-empty", "-am", "remote")
	mustGit(t, dir, "push", "-q", "-u", "origin", "main")
	choreCommit(t, dir, "handoff.md")
	return dir, origin
}

// A protected, purely-ahead base reaches origin through t-9's chore PR, and
// the hook is never offered main.
func TestProtectedBaseRoutesThroughChorePR(t *testing.T) {
	dir, origin := choreBase(t, "github")
	log := protectedOrigin(t, origin, false)
	stubBranchRules(t, protectedRules(t))
	s := &choreSeams{autoRes: ship.AutoMergeResult{AutoEnabled: true}}
	stubChoreSeams(t, s)

	r, err := routeBaseChores(dir, "main")
	if err != nil {
		t.Fatal(err)
	}
	if got := baseUpdates(t, log); len(got) != 0 {
		t.Errorf("the hook was offered %v", got)
	}
	if r.Pushed || r.ChorePR == nil || len(s.opened) != 1 {
		t.Fatalf("result %+v, %d PRs opened; want a chore PR", r, len(s.opened))
	}
	if got := originRef(t, origin, "refs/heads/dross-chores/main"); got != mustGit(t, dir, "rev-parse", "main") {
		t.Errorf("dross-chores/main = %q, want local main's tip", got)
	}
	if pushed, err := pushBaseIfAheadDrossOnly(dir, "main"); err != nil || pushed {
		t.Errorf("the narrow form reports pushed=%v err=%v for a chore-PR route", pushed, err)
	}
}

// Ahead and behind is included: on a protected base the batch still goes
// through a chore PR, where today's policy would leave it.
func TestProtectedBaseAheadAndBehindRoutesThroughChorePR(t *testing.T) {
	dir, origin := choreBase(t, "github")
	mustGit(t, dir, "switch", "-q", "-c", "elsewhere", "main~1")
	mustWrite(t, filepath.Join(dir, "feature.go"), "package f\n")
	mustGit(t, dir, "add", "feature.go")
	mustGit(t, dir, "commit", "-q", "-m", "phase x: x (#1)")
	mustGit(t, dir, "push", "-q", "origin", "elsewhere:main")
	mustGit(t, dir, "switch", "-q", "main")
	log := protectedOrigin(t, origin, false)
	stubBranchRules(t, protectedRules(t))
	s := &choreSeams{autoRes: ship.AutoMergeResult{AutoEnabled: true}}
	stubChoreSeams(t, s)

	r, err := routeBaseChores(dir, "main")
	if err != nil {
		t.Fatal(err)
	}
	if r.ChorePR == nil || len(s.opened) != 1 {
		t.Errorf("result %+v; an ahead-and-behind protected base must open a chore PR", r)
	}
	if got := baseUpdates(t, log); len(got) != 0 {
		t.Errorf("the hook was offered %v", got)
	}
}

// Unknown never pushes — not even to an origin that would take it, which is
// what a bypass holder's push looks like.
func TestUnknownProbeNeverPushes(t *testing.T) {
	dir, origin := choreBase(t, "github")
	log := protectedOrigin(t, origin, true)
	stubBranchRules(t, ship.BranchRulesResult{Reason: "GitHub refused gh's credentials (HTTP 401)"})
	stubChoreSeams(t, &choreSeams{})

	r, err := routeBaseChores(dir, "main")
	if err == nil {
		t.Fatal("an unknown probe did not refuse")
	}
	if r.Pushed || r.ChorePR != nil {
		t.Errorf("result %+v after an unknown probe", r)
	}
	for _, want := range []string{"HTTP 401", "gh auth login", "[remote]"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("err %q does not name %q", err, want)
		}
	}
	if got := baseUpdates(t, log); len(got) != 0 {
		t.Errorf("an unknown probe offered the hook %v", got)
	}
}

// Every other forge, and no remote, keeps today's push without asking
// anyone about protection; so does an unprotected GitHub base.
func TestBaseChoresLegacyTable(t *testing.T) {
	for _, provider := range configenum.ShipProviders.Values() {
		if provider == "github" {
			continue
		}
		testLegacyPush(t, provider, 0)
	}
	testLegacyPush(t, "", 0)
	testLegacyPush(t, "github", 1) // asked, unprotected
}

func testLegacyPush(t *testing.T, provider string, wantProbes int) {
	t.Run("provider="+provider, func(t *testing.T) {
		dir, _ := choreBase(t, provider)
		calls := stubBranchRules(t, ship.BranchRulesResult{Known: true})
		pushed, err := pushBaseIfAheadDrossOnly(dir, "main")
		if err != nil || !pushed {
			t.Fatalf("pushed=%v err=%v; want today's direct push", pushed, err)
		}
		if *calls != wantProbes {
			t.Errorf("%d protection probes, want %d", *calls, wantProbes)
		}
		if ahead := mustGit(t, dir, "rev-list", "origin/main..main"); ahead != "" {
			t.Errorf("origin/main..main = %q after the push", ahead)
		}
	})
}

// Code ahead on a protected base is refused, naming the route that works.
func TestProtectedBaseCodeAheadRefuses(t *testing.T) {
	dir, origin := choreBase(t, "github")
	mustWrite(t, filepath.Join(dir, "src.go"), "package src\n")
	mustGit(t, dir, "add", "src.go")
	mustGit(t, dir, "commit", "-q", "-m", "feat: code on main")
	log := protectedOrigin(t, origin, false)
	stubBranchRules(t, protectedRules(t))
	s := &choreSeams{}
	stubChoreSeams(t, s)

	_, err := routeBaseChores(dir, "main")
	if err == nil {
		t.Fatal("code ahead on a protected base was not refused")
	}
	msg := err.Error()
	if !strings.Contains(msg, "quick/") || !strings.Contains(msg, "PR into main") || strings.Contains(msg, "manually") {
		t.Errorf("refusal %q must name quick/ and a PR into main, not a manual push", msg)
	}
	if got := baseUpdates(t, log); len(got) != 0 {
		t.Errorf("the hook was offered %v", got)
	}
	if originRef(t, origin, "refs/heads/dross-chores/main") != "" || len(s.opened) != 0 {
		t.Error("code ahead still reached a chore branch or PR")
	}
}

// A quick_base on main and a phase base on milestone/v1.7, both protected,
// each get their own chore branch and PR.
func TestTwoProtectedBasesTwoChorePRs(t *testing.T) {
	dir, origin := choreBase(t, "github")
	mustGit(t, dir, "branch", "-q", "milestone/v1.7", "main~1")
	mustGit(t, dir, "push", "-q", "origin", "milestone/v1.7")
	mustGit(t, dir, "switch", "-q", "milestone/v1.7")
	choreCommit(t, dir, "phase-chore.md")
	mustGit(t, dir, "switch", "-q", "main")
	if err := runCmd(t, Local(), "set", "quick_base", "main"); err != nil {
		t.Fatal(err)
	}
	log := protectedOrigin(t, origin, false)
	stubBranchRules(t, protectedRules(t))
	s := &choreSeams{autoRes: ship.AutoMergeResult{AutoEnabled: true}}
	stubChoreSeams(t, s)

	phase, err := routeBaseChores(dir, "milestone/v1.7")
	if err != nil {
		t.Fatal(err)
	}
	quick, branch, err := routeQuickBaseChores(dir, filepath.Join(dir, RootDirName), "milestone/v1.7")
	if err != nil {
		t.Fatal(err)
	}
	if branch != "main" || phase.ChorePR == nil || quick.ChorePR == nil {
		t.Fatalf("phase %+v, quick %+v (%s); want a chore PR each", phase, quick, branch)
	}
	for _, chore := range []string{"dross-chores/main", "dross-chores/milestone-v1.7"} {
		if originRef(t, origin, "refs/heads/"+chore) == "" {
			t.Errorf("%s is not on origin", chore)
		}
	}
	if len(s.opened) != 2 || s.opened[0].BaseBranch == s.opened[1].BaseBranch {
		t.Errorf("opened %+v, want one PR into each base", s.opened)
	}
	if got := baseUpdates(t, log); len(got) != 0 {
		t.Errorf("the hook was offered %v", got)
	}
}
