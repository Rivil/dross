package cmd

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/ship"
)

// ship and complete on a main-based phase whose main refuses direct pushes
// (phase main-branch-protection, c-5). origin is a bare repo whose
// pre-receive hook logs every ref update offered and rejects main; GitHub's
// merges are played by a scratch clone that moves origin's main with
// update-ref, behind the hook's back, so the log holds only what dross
// itself pushed.

// ghPlay plays GitHub for the seams ship and complete reach: which PRs are
// open per head, what opening one returns, how arming auto-merge answers,
// and whether the phase PR has merged.
type ghPlay struct {
	open    map[string]*ship.OpenResult // open PR per head branch
	opened  []ship.OpenOpts
	next    int
	merged  bool // the phase PR's merged state, for complete's gate
	armed   []string
	protect bool
}

func stubForge(t *testing.T, f *ghPlay) {
	t.Helper()
	if f.open == nil {
		f.open = map[string]*ship.OpenResult{}
	}
	prev := [...]any{ship.FindOpenPRByHeadFunc, ship.OpenPRFunc, ship.AutoMergePRFunc, ship.PRStatusFunc}
	t.Cleanup(func() {
		ship.FindOpenPRByHeadFunc = prev[0].(func(ship.OpenOpts, string) (*ship.OpenResult, error))
		ship.OpenPRFunc = prev[1].(func(ship.OpenOpts) (*ship.OpenResult, error))
		ship.AutoMergePRFunc = prev[2].(func(ship.OpenOpts, int, string) (ship.AutoMergeResult, error))
		ship.PRStatusFunc = prev[3].(func(ship.OpenOpts) (ship.PRStatus, error))
	})
	ship.FindOpenPRByHeadFunc = func(_ ship.OpenOpts, head string) (*ship.OpenResult, error) {
		return f.open[head], nil
	}
	ship.OpenPRFunc = func(o ship.OpenOpts) (*ship.OpenResult, error) {
		f.opened = append(f.opened, o)
		f.next++
		res := &ship.OpenResult{Number: 40 + f.next, URL: "https://github.com/o/r/pull/" + itoa(40+f.next)}
		f.open[o.HeadBranch] = res
		return res, nil
	}
	ship.AutoMergePRFunc = func(_ ship.OpenOpts, n int, method string) (ship.AutoMergeResult, error) {
		f.armed = append(f.armed, method)
		return ship.AutoMergeResult{AutoEnabled: true}, nil
	}
	ship.PRStatusFunc = func(ship.OpenOpts) (ship.PRStatus, error) {
		return ship.PRStatus{Merged: f.merged, BaseRef: "main"}, nil
	}
	if f.protect {
		stubBranchRules(t, protectedRules(t))
	} else {
		stubBranchRules(t, ship.BranchRulesResult{Known: true})
	}
}

// protectedFlowRepo is a dross repo on a GitHub [remote] with main pushed to
// a bare origin, a .dross chore committed on local main and not pushed, and a
// verified phase x checked out, ready to ship.
func protectedFlowRepo(t *testing.T) (dir, origin string) {
	t.Helper()
	dir, origin = t.TempDir(), t.TempDir()
	mustGit(t, origin, "init", "--bare", "-q", "-b", "main")
	gitInit(t, dir, origin)
	chdir(t, dir)
	if err := runCmd(t, Init()); err != nil {
		t.Fatalf("init: %v", err)
	}
	for _, set := range [][]string{
		{"set", "remote.provider", "github"},
		{"set", "remote.url", "https://github.com/o/r"},
		{"set", "repo.git_main_branch", "main"},
	} {
		if err := runCmd(t, Project(), set...); err != nil {
			t.Fatalf("project %v: %v", set, err)
		}
	}
	mustWrite(t, filepath.Join(dir, "README.md"), "base\n")
	gitCommit(t, dir, "initial baseline")
	mustGit(t, dir, "push", "-q", "-u", "origin", "main")
	choreCommit(t, dir, "handoff.md") // a pause snapshot, unpushed

	if err := runCmd(t, Phase(), "create", "x"); err != nil {
		t.Fatalf("phase create: %v", err)
	}
	phaseDir := filepath.Join(dir, ".dross", "phases", "x")
	mustWrite(t, filepath.Join(phaseDir, "verify.toml"), "[verify]\nphase = \"x\"\ngenerated_at = 2026-05-02T10:00:00Z\nverdict = \"pass\"\n\n"+
		"[summary]\nmutation_score = 0.85\nmutants_killed = 17\nmutants_survived = 3\ncriteria_total = 1\ncriteria_covered = 1\ncriteria_uncovered = 0\n\n"+
		"[[criterion]]\nid = \"C1\"\nstatus = \"covered\"\ntests = [\"tag.test.ts:42\"]\n")
	mustWrite(t, filepath.Join(dir, "src/tag.ts"), "export const tag = 1\n")
	mustWrite(t, filepath.Join(phaseDir, "spec.toml"), "[phase]\nid = \"x\"\ntitle = \"Tagging\"\n\n[[criteria]]\nid = \"C1\"\ntext = \"Tags can be added\"\n")
	gitCommit(t, dir, "feat(tag): add tagging")
	return dir, origin
}

// githubSim is a clone of origin standing in for GitHub's merge button.
type githubSim struct {
	t           *testing.T
	dir, origin string
}

func newGithubSim(t *testing.T, origin string) *githubSim {
	t.Helper()
	dir := t.TempDir()
	mustGit(t, dir, "clone", "-q", origin, ".")
	for _, kv := range [][2]string{{"user.email", "gh@example.com"}, {"user.name", "GitHub"}, {"commit.gpgsign", "false"}} {
		mustGit(t, dir, "config", kv[0], kv[1])
	}
	return &githubSim{t: t, dir: dir, origin: origin}
}

// land makes origin's main the sim's HEAD without passing the hook.
func (g *githubSim) land() {
	g.t.Helper()
	mustGit(g.t, g.dir, "push", "-q", "-f", "origin", "HEAD:refs/heads/sim")
	mustGit(g.t, g.origin, "update-ref", "refs/heads/main", mustGit(g.t, g.dir, "rev-parse", "HEAD"))
	mustGit(g.t, g.origin, "update-ref", "-d", "refs/heads/sim")
}

// squash lands phase/x's src/ on main as one commit, as a squash-merge does.
func (g *githubSim) squash() {
	g.t.Helper()
	mustGit(g.t, g.dir, "fetch", "-q", "origin")
	mustGit(g.t, g.dir, "checkout", "-q", "-B", "work", "origin/main")
	mustGit(g.t, g.dir, "checkout", "origin/phase/x", "--", "src/")
	mustGit(g.t, g.dir, "add", "-A")
	mustGit(g.t, g.dir, "commit", "-q", "-m", "phase x: Tagging (#41)")
	g.land()
}

// mergeCommit lands head on main as a merge commit, as a chore PR merges.
func (g *githubSim) mergeCommit(head string) {
	g.t.Helper()
	mustGit(g.t, g.dir, "fetch", "-q", "origin")
	mustGit(g.t, g.dir, "checkout", "-q", "-B", "work", "origin/main")
	mustGit(g.t, g.dir, "merge", "-q", "--no-ff", "-m", "Merge "+head, "origin/"+head)
	g.land()
}

func runCapture(t *testing.T, args []string, cmd func() error) (string, error) {
	t.Helper()
	var err error
	out := captureStdout(t, func() { err = cmd() })
	return out, err
}

// ship → GitHub squash-merges the phase PR and merges the ship chore PR →
// complete → GitHub merges the completion-record chore PR: dross never offers
// main to the hook, both commands succeed, and local main ends level.
func TestProtectedBaseShipThenComplete(t *testing.T) {
	dir, origin := protectedFlowRepo(t)
	log := protectedOrigin(t, origin, false)
	f := &ghPlay{protect: true}
	stubForge(t, f)
	gh := newGithubSim(t, origin)

	out, err := runCapture(t, nil, func() error { return runCmd(t, Ship()) })
	if err != nil {
		t.Fatalf("ship: %v\n%s", err, out)
	}
	if !strings.Contains(out, "chore PR #") {
		t.Errorf("ship did not narrate the chore PR:\n%s", out)
	}
	if originRef(t, origin, "refs/heads/dross-chores/main") == "" {
		t.Fatal("ship's .dross chores never reached a chore branch")
	}

	gh.squash()
	gh.mergeCommit("dross-chores/main")
	delete(f.open, "dross-chores/main")
	f.merged = true

	out, err = runCapture(t, nil, func() error { return runCmd(t, Phase(), "complete", "x") })
	if err != nil {
		t.Fatalf("complete: %v\n%s", err, out)
	}
	// TestCompletionRecordChorePRNarrated's half: the completion record
	// went through a chore PR and complete said where.
	if !strings.Contains(out, "completion record:") || !strings.Contains(out, f.open["dross-chores/main"].URL) {
		t.Errorf("complete did not narrate the completion-record chore PR:\n%s", out)
	}

	gh.mergeCommit("dross-chores/main")
	mustGit(t, dir, "fetch", "-q", "origin")
	if ahead := mustGit(t, dir, "rev-list", "origin/main..main"); ahead != "" {
		t.Errorf("local main is ahead of origin after the chore PRs merged: %s", ahead)
	}
	if got := baseUpdates(t, log); len(got) != 0 {
		t.Errorf("dross offered the hook %v", got)
	}
	for _, m := range f.armed {
		if m != "merge" {
			t.Errorf("a chore PR was armed with %q", m)
		}
	}
}

// The completion record's publish names the chore PR it went through.
func TestCompletionRecordChorePRNarrated(t *testing.T) {
	dir, origin := protectedFlowRepo(t)
	protectedOrigin(t, origin, false)
	f := &ghPlay{protect: true}
	stubForge(t, f)
	gh := newGithubSim(t, origin)
	if _, err := runCapture(t, nil, func() error { return runCmd(t, Ship()) }); err != nil {
		t.Fatal(err)
	}
	gh.squash()
	gh.mergeCommit("dross-chores/main")
	f.merged = true
	// The ship chore PR is still listed as open: the record joins it.
	out, err := runCapture(t, nil, func() error { return runCmd(t, Phase(), "complete", "x") })
	if err != nil {
		t.Fatalf("complete: %v\n%s", err, out)
	}
	if !strings.Contains(out, "completion record: ") || !strings.Contains(out, "joined chore PR #") {
		t.Errorf("complete did not narrate joining the chore PR:\n%s", out)
	}
	if mustGit(t, dir, "rev-parse", "main") != originRef(t, origin, "refs/heads/dross-chores/main") {
		t.Error("the completion record is not on the chore branch")
	}
}

// With the ship chore PR still open and the phase squash already on origin,
// complete can't fast-forward main: it says to wait for the PR, offers no
// --recover, and moves nothing.
func TestCompleteWaitsForPendingChorePR(t *testing.T) {
	dir, origin := protectedFlowRepo(t)
	log := protectedOrigin(t, origin, false)
	f := &ghPlay{protect: true}
	stubForge(t, f)
	gh := newGithubSim(t, origin)
	if _, err := runCapture(t, nil, func() error { return runCmd(t, Ship()) }); err != nil {
		t.Fatal(err)
	}
	gh.squash()
	f.merged = true
	mainBefore := mustGit(t, dir, "rev-parse", "main")
	phaseBefore := mustGit(t, dir, "rev-parse", "phase/x")

	_, err := runCapture(t, nil, func() error { return runCmd(t, Phase(), "complete", "x") })
	if err == nil {
		t.Fatal("complete ran past a pending chore PR")
	}
	msg := err.Error()
	if !strings.Contains(msg, "https://github.com/o/r/pull/") || !strings.Contains(msg, "once it merges") || strings.Contains(msg, "--recover") {
		t.Errorf("refusal %q must name the chore PR and the wait, never --recover", msg)
	}
	if mustGit(t, dir, "rev-parse", "main") != mainBefore || mustGit(t, dir, "rev-parse", "phase/x") != phaseBefore {
		t.Error("the refusal moved a local ref")
	}
	if head := mustGit(t, dir, "symbolic-ref", "--short", "HEAD"); head != "phase/x" {
		t.Errorf("HEAD moved to %s", head)
	}
	if got := baseUpdates(t, log); len(got) != 0 {
		t.Errorf("dross offered the hook %v", got)
	}
}

// Unprotected, ship still pushes main's .dross chores straight to origin.
func TestUnprotectedBaseStillPushesDirectly(t *testing.T) {
	dir, origin := protectedFlowRepo(t)
	f := &ghPlay{protect: false}
	stubForge(t, f)
	out, err := runCapture(t, nil, func() error { return runCmd(t, Ship()) })
	if err != nil {
		t.Fatalf("ship: %v\n%s", err, out)
	}
	if originRef(t, origin, "refs/heads/main") != mustGit(t, dir, "rev-parse", "main") {
		t.Error("an unprotected main's chores were not pushed directly")
	}
	if originRef(t, origin, "refs/heads/dross-chores/main") != "" {
		t.Error("an unprotected main got a chore branch")
	}
	if !strings.Contains(out, "pushed unpushed .dross chores on main to origin") {
		t.Errorf("the direct push is not narrated:\n%s", out)
	}
}

// `ship --json` stays one JSON document; with a chore PR it carries it.
func TestShipJSONCarriesChorePR(t *testing.T) {
	protectedFlowRepo(t)
	stubForge(t, &ghPlay{protect: true})
	out, err := runCapture(t, nil, func() error { return runCmd(t, Ship(), "--json") })
	if err != nil {
		t.Fatalf("ship --json: %v\n%s", err, out)
	}
	var doc struct {
		URL     string `json:"url"`
		ChorePR *struct {
			URL    string `json:"url"`
			Number int    `json:"number"`
			State  string `json:"state"`
		} `json:"chore_pr"`
	}
	dec := json.NewDecoder(strings.NewReader(out))
	if err := dec.Decode(&doc); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, out)
	}
	if dec.More() {
		t.Errorf("stdout carries more than one JSON document:\n%s", out)
	}
	if doc.ChorePR == nil || !strings.HasPrefix(doc.ChorePR.URL, "https://github.com/o/r/pull/") || doc.ChorePR.State != "auto-merge" {
		t.Errorf("chore_pr = %+v", doc.ChorePR)
	}
	if doc.URL == doc.ChorePR.URL {
		t.Error("the phase PR and the chore PR are the same")
	}
}
