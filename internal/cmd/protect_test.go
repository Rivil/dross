package cmd

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/project"
	"github.com/Rivil/dross/internal/protect"
	"github.com/Rivil/dross/internal/ship"
)

// protectSeams records every forge call `dross protect` makes and plays
// GitHub: a ruleset written through Create/Update is what BranchRules reads
// back, unless readback overrides it.
type protectSeams struct {
	existing  []ship.RulesetSummary
	listErr   error // what listing the existing rulesets fails with
	settings  ship.MergeSettings
	createErr map[string]error
	readback  *ship.BranchRulesResult

	created     []protect.Ruleset
	updated     []int64
	autoMerge   []bool
	branchCalls []string
	calls       int // every seam call, reads included
	applied     *protect.Ruleset
}

func stubProtectSeams(t *testing.T, s *protectSeams) {
	t.Helper()
	prev := [...]any{ship.BranchRulesFunc, ship.RepoMergeSettingsFunc, ship.ListRulesetsFunc, ship.CreateRulesetFunc, ship.UpdateRulesetFunc, ship.SetAllowAutoMergeFunc}
	t.Cleanup(func() {
		ship.BranchRulesFunc = prev[0].(func(ship.OpenOpts, string) ship.BranchRulesResult)
		ship.RepoMergeSettingsFunc = prev[1].(func(ship.OpenOpts) ship.MergeSettings)
		ship.ListRulesetsFunc = prev[2].(func(ship.OpenOpts) ([]ship.RulesetSummary, error))
		ship.CreateRulesetFunc = prev[3].(func(ship.OpenOpts, protect.Ruleset) (int64, error))
		ship.UpdateRulesetFunc = prev[4].(func(ship.OpenOpts, int64, protect.Ruleset) error)
		ship.SetAllowAutoMergeFunc = prev[5].(func(ship.OpenOpts, bool) error)
	})
	record := func(rs protect.Ruleset) {
		if rs.Name == protect.MainRulesetName {
			cp := rs
			s.applied = &cp
		}
	}
	ship.BranchRulesFunc = func(_ ship.OpenOpts, branch string) ship.BranchRulesResult {
		s.calls++
		s.branchCalls = append(s.branchCalls, branch)
		if s.readback != nil {
			return *s.readback
		}
		if s.applied == nil {
			return ship.BranchRulesResult{Known: true}
		}
		return liveFrom(t, *s.applied, 1)
	}
	ship.RepoMergeSettingsFunc = func(ship.OpenOpts) ship.MergeSettings {
		s.calls++
		return s.settings
	}
	ship.ListRulesetsFunc = func(ship.OpenOpts) ([]ship.RulesetSummary, error) {
		s.calls++
		return s.existing, s.listErr
	}
	ship.CreateRulesetFunc = func(_ ship.OpenOpts, rs protect.Ruleset) (int64, error) {
		s.calls++
		if err := s.createErr[rs.Name]; err != nil {
			return 0, err
		}
		s.created = append(s.created, rs)
		record(rs)
		return int64(10 + len(s.created)), nil
	}
	ship.UpdateRulesetFunc = func(_ ship.OpenOpts, id int64, rs protect.Ruleset) error {
		s.calls++
		s.updated = append(s.updated, id)
		record(rs)
		return nil
	}
	ship.SetAllowAutoMergeFunc = func(_ ship.OpenOpts, on bool) error {
		s.calls++
		s.autoMerge = append(s.autoMerge, on)
		return nil
	}
}

// liveFrom is what rules/branches answers for a branch under rs.
func liveFrom(t *testing.T, rs protect.Ruleset, id int64) ship.BranchRulesResult {
	t.Helper()
	b, err := json.Marshal(rs.Rules)
	if err != nil {
		t.Fatal(err)
	}
	var rules []protect.LiveRule
	if err := json.Unmarshal(b, &rules); err != nil {
		t.Fatal(err)
	}
	for i := range rules {
		rules[i].RulesetID = id
	}
	empty := []protect.BypassActor{}
	return ship.BranchRulesResult{Known: true, Rules: rules, Rulesets: []protect.LiveRuleset{
		{ID: id, Name: rs.Name, Enforcement: "active", BypassActors: &empty, CurrentUserCanBypass: "never"},
	}}
}

const protectCI = `name: ci
on:
  pull_request:
  workflow_dispatch:
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: go test ./...
  lint:
    name: Lint
    runs-on: ubuntu-latest
    steps:
      - run: make lint
`

const protectRelease = "on:\n  push:\n    branches: [main]\njobs:\n  release:\n    runs-on: ubuntu-latest\n"
const protectAutomerge = "on: pull_request_target\njobs:\n  automerge:\n    runs-on: ubuntu-latest\n"

// protectRepo is a dross repo whose [remote] names provider, with ci.yml,
// release.yml and an auto-merge workflow pushed to origin's main.
func protectRepo(t *testing.T, provider string) (dir, origin string) {
	t.Helper()
	dir, origin = setupMilestoneRepo(t)
	setProtectRemote(t, dir, provider, "")
	for name, body := range map[string]string{"ci.yml": protectCI, "release.yml": protectRelease, "automerge.yml": protectAutomerge} {
		mustWrite(t, filepath.Join(dir, ".github", "workflows", name), body)
	}
	mustGit(t, dir, "add", "-A")
	mustGit(t, dir, "commit", "-q", "-m", "workflows")
	mustGit(t, dir, "push", "-q", "-u", "origin", "main")
	return dir, origin
}

// setProtectRemote points project.toml at a GitHub-shaped URL on provider
// ("" drops [remote]) and, when mainBranch is set, renames the main branch.
func setProtectRemote(t *testing.T, dir, provider, mainBranch string) {
	t.Helper()
	path := filepath.Join(dir, ".dross", project.File)
	p, err := project.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	p.Remote = project.Remote{}
	if provider != "" {
		p.Remote.Provider = provider
		p.Remote.URL = "https://github.com/o/r"
	}
	if mainBranch != "" {
		p.Repo.GitMainBranch = mainBranch
	}
	if err := p.Save(path); err != nil {
		t.Fatal(err)
	}
}

func runProtect(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var err error
	out := captureStdout(t, func() { err = runCmd(t, Protect(), args...) })
	return out, err
}

// Locked decision apply_verb: without --apply nothing is written.
func TestProtectPreviewWritesNothing(t *testing.T) {
	protectRepo(t, "github")
	s := &protectSeams{settings: ship.MergeSettings{Known: true, AllowMergeCommit: true}}
	stubProtectSeams(t, s)

	out, err := runProtect(t)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.created)+len(s.updated)+len(s.autoMerge) != 0 {
		t.Errorf("preview wrote: created %d, updated %d, auto-merge %v", len(s.created), len(s.updated), s.autoMerge)
	}
	if !strings.Contains(out, "preview") || !strings.Contains(out, "- Lint") || !strings.Contains(out, "- test") {
		t.Errorf("preview output:\n%s", out)
	}
}

// A ruleset list the token can't read still previews: each ruleset's line
// says the list was unreadable and why, rather than claiming --apply would
// create it.
func TestProtectPreviewUnreadableRulesets(t *testing.T) {
	protectRepo(t, "github")
	s := &protectSeams{
		settings: ship.MergeSettings{Known: true, AllowMergeCommit: true},
		listErr:  errors.New("HTTP 403: Must have admin rights to Repository"),
	}
	stubProtectSeams(t, s)

	out, err := runProtect(t)
	if err != nil {
		t.Fatal(err)
	}
	fate := "  (existing rulesets unreadable — HTTP 403: Must have admin rights to Repository)\n"
	for _, line := range []string{
		protect.MainRulesetName + " → refs/heads/main" + fate,
		protect.MilestoneRulesetName + " → refs/heads/milestone/*" + fate,
	} {
		if !strings.Contains(out, line) {
			t.Errorf("preview lacks %q:\n%s", line, out)
		}
	}
	if strings.Contains(out, "(creates it)") {
		t.Errorf("an unreadable list must not read as a ruleset to create:\n%s", out)
	}
	if len(s.created)+len(s.updated)+len(s.autoMerge) != 0 {
		t.Errorf("preview wrote: created %d, updated %d, auto-merge %v", len(s.created), len(s.updated), s.autoMerge)
	}
}

// Only pull_request jobs are required: not release.yml's, not the
// pull_request_target auto-merge job. The main branch is whatever
// project.toml names.
func TestProtectBuildInputs(t *testing.T) {
	dir, _ := protectRepo(t, "github")
	mustGit(t, dir, "push", "-q", "origin", "main:trunk")
	setProtectRemote(t, dir, "github", "trunk")
	s := &protectSeams{settings: ship.MergeSettings{Known: true, AllowMergeCommit: true}}
	stubProtectSeams(t, s)

	out, err := runProtect(t)
	if err != nil {
		t.Fatal(err)
	}
	for _, banned := range []string{"- release", "- automerge"} {
		if strings.Contains(out, banned) {
			t.Errorf("preview requires %q:\n%s", banned, out)
		}
	}
	if !strings.Contains(out, "refs/heads/trunk") {
		t.Errorf("preview does not target refs/heads/trunk:\n%s", out)
	}
	if _, err := runProtect(t, "--apply"); err != nil {
		t.Fatal(err)
	}
	if s.applied == nil || strings.Join(s.applied.Conditions.RefName.Include, ",") != "refs/heads/trunk" {
		t.Errorf("applied main ruleset = %+v, want it on refs/heads/trunk", s.applied)
	}
	if s.branchCalls[len(s.branchCalls)-1] != "trunk" {
		t.Errorf("read back %v, want trunk", s.branchCalls)
	}
}

func TestProtectUpsert(t *testing.T) {
	t.Run("both listed: update in place, create nothing", func(t *testing.T) {
		protectRepo(t, "github")
		s := &protectSeams{settings: ship.MergeSettings{Known: true, AllowMergeCommit: true},
			existing: []ship.RulesetSummary{{ID: 3, Name: protect.MainRulesetName}, {ID: 4, Name: protect.MilestoneRulesetName}}}
		stubProtectSeams(t, s)
		if _, err := runProtect(t, "--apply"); err != nil {
			t.Fatal(err)
		}
		if len(s.created) != 0 || len(s.updated) != 2 {
			t.Errorf("created %d, updated %v; want 0 and [3 4]", len(s.created), s.updated)
		}
	})
	t.Run("neither listed: create both and turn auto-merge on", func(t *testing.T) {
		protectRepo(t, "github")
		s := &protectSeams{settings: ship.MergeSettings{Known: true, AllowMergeCommit: true}}
		stubProtectSeams(t, s)
		out, err := runProtect(t, "--apply")
		if err != nil {
			t.Fatalf("%v\n%s", err, out)
		}
		if len(s.created) != 2 || len(s.updated) != 0 || len(s.autoMerge) != 1 || !s.autoMerge[0] {
			t.Errorf("created %d, updated %v, auto-merge %v; want 2, none, [true]", len(s.created), s.updated, s.autoMerge)
		}
		if !strings.Contains(out, "read back main: protected") {
			t.Errorf("no clean read-back:\n%s", out)
		}
	})
}

func TestProtectApplySafety(t *testing.T) {
	t.Run("read-back missing a check exits non-zero", func(t *testing.T) {
		protectRepo(t, "github")
		partial, err := protect.MainRuleset("main", []string{"test"})
		if err != nil {
			t.Fatal(err)
		}
		rb := liveFrom(t, partial, 1)
		s := &protectSeams{settings: ship.MergeSettings{Known: true, AllowMergeCommit: true}, readback: &rb}
		stubProtectSeams(t, s)
		out, err := runProtect(t, "--apply")
		if err == nil {
			t.Fatalf("a read-back missing Lint exited 0:\n%s", out)
		}
		if !strings.Contains(out, "required check missing: Lint") {
			t.Errorf("the missing check is not named:\n%s", out)
		}
	})
	t.Run("unknown read-back exits non-zero", func(t *testing.T) {
		protectRepo(t, "github")
		rb := ship.BranchRulesResult{Reason: "GitHub refused access (HTTP 403)"}
		stubProtectSeams(t, &protectSeams{readback: &rb})
		if _, err := runProtect(t, "--apply"); err == nil {
			t.Error("an unreadable read-back exited 0")
		}
	})
	t.Run("a matrix job aborts before any write", func(t *testing.T) {
		dir, _ := protectRepo(t, "github")
		mustWrite(t, filepath.Join(dir, ".github", "workflows", "ci.yml"), protectCI+"  build:\n    strategy:\n      matrix:\n        os: [a, b]\n")
		mustGit(t, dir, "commit", "-q", "-am", "matrix")
		mustGit(t, dir, "push", "-q", "origin", "main")
		s := &protectSeams{}
		stubProtectSeams(t, s)
		if _, err := runProtect(t, "--apply"); err == nil || !strings.Contains(err.Error(), "matrix") {
			t.Errorf("err = %v, want a refusal naming the matrix", err)
		}
		if len(s.created)+len(s.updated)+len(s.autoMerge) != 0 {
			t.Error("a refused workflow still wrote something")
		}
	})
}

// A failure after the main ruleset applied names which one did.
func TestProtectPartialApply(t *testing.T) {
	protectRepo(t, "github")
	s := &protectSeams{createErr: map[string]error{protect.MilestoneRulesetName: errors.New("HTTP 422")}}
	stubProtectSeams(t, s)
	_, err := runProtect(t, "--apply")
	if err == nil {
		t.Fatal("a failed milestone ruleset exited 0")
	}
	msg := err.Error()
	if !strings.Contains(msg, protect.MainRulesetName+" is in place") || !strings.Contains(msg, protect.MilestoneRulesetName+" is not") {
		t.Errorf("err %q does not say which ruleset applied", msg)
	}
}

// Required checks come from origin/main: a job only in the working tree is
// named, not required.
func TestProtectSourceIsOriginMain(t *testing.T) {
	dir, _ := protectRepo(t, "github")
	mustWrite(t, filepath.Join(dir, ".github", "workflows", "ci.yml"), protectCI+"  extra:\n    runs-on: ubuntu-latest\n")
	s := &protectSeams{settings: ship.MergeSettings{Known: true, AllowMergeCommit: true}}
	stubProtectSeams(t, s)

	out, err := runProtect(t)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "not on main yet") || !strings.Contains(out, "- extra") {
		t.Errorf("the working-tree job is not named as not on main yet:\n%s", out)
	}
	if _, err := runProtect(t, "--apply"); err != nil {
		t.Fatal(err)
	}
	for _, r := range s.applied.Rules {
		if p, ok := r.Parameters.(protect.StatusCheckParams); ok {
			for _, c := range p.RequiredStatusChecks {
				if c.Context == "extra" {
					t.Error("a job not yet on origin/main was required")
				}
			}
		}
	}
}

func TestProtectWarnsWhenMergeCommitsDisabled(t *testing.T) {
	protectRepo(t, "github")
	s := &protectSeams{settings: ship.MergeSettings{Known: true, AllowAutoMerge: true, AllowMergeCommit: false}}
	stubProtectSeams(t, s)
	for _, args := range [][]string{nil, {"--apply"}} {
		out, err := runProtect(t, args...)
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{"allow_merge_commit is off", "chore PRs", "milestone PRs"} {
			if !strings.Contains(out, want) {
				t.Errorf("%v: output lacks %q:\n%s", args, want, out)
			}
		}
	}
	for _, on := range s.autoMerge {
		if !on {
			t.Error("protect turned a setting off")
		}
	}
}

// doctor_reach: only GitHub is read or written; every other provider, and no
// remote at all, makes no forge call in any mode.
func TestProtectProviderTable(t *testing.T) {
	for _, provider := range []string{"gitlab", "forgejo", "gitea", "bitbucket", ""} {
		t.Run("provider="+provider, func(t *testing.T) {
			protectRepo(t, provider)
			s := &protectSeams{}
			stubProtectSeams(t, s)
			want := "not checked (" + provider + ")"
			if provider == "" {
				want = "not checked (no remote)"
			}
			out, err := runProtect(t, "--check", "main")
			if err != nil || out != want+"\n" {
				t.Errorf("--check = %q (err %v), want %q", out, err, want)
			}
			if out, err := runProtect(t); err != nil || !strings.Contains(out, want) {
				t.Errorf("preview = %q (err %v)", out, err)
			}
			if _, err := runProtect(t, "--apply"); err == nil {
				t.Error("--apply on a non-GitHub remote exited 0")
			}
			if s.calls != 0 {
				t.Errorf("%d forge calls for provider %q", s.calls, provider)
			}
		})
	}
}

func checkWith(t *testing.T, branch string, rb ship.BranchRulesResult) (string, error) {
	t.Helper()
	stubProtectSeams(t, &protectSeams{readback: &rb})
	return runProtect(t, "--check", branch)
}

func liveTypes(types ...string) ship.BranchRulesResult {
	res := ship.BranchRulesResult{Known: true}
	for _, typ := range types {
		res.Rules = append(res.Rules, protect.LiveRule{Type: typ, RulesetID: 1})
	}
	return res
}

// --check asks only whether a direct push is refused: a PR-only milestone
// ruleset is protected, though it requires no checks.
func TestCheckIsPushRefusalOnly(t *testing.T) {
	protectRepo(t, "github")
	out, err := checkWith(t, "milestone/v1.7", liveFrom(t, protect.MilestoneRuleset(), 2))
	if err != nil || out != "protected\n" {
		t.Errorf("--check milestone/v1.7 = %q (err %v), want protected", out, err)
	}
}

func TestProtectCheckOutputTable(t *testing.T) {
	protectRepo(t, "github")
	for _, tc := range []struct {
		name string
		rb   ship.BranchRulesResult
		want string
	}{
		{"zero rules", liveTypes(), "unprotected"},
		{"only non_fast_forward", liveTypes("non_fast_forward"), "unprotected"},
		{"deletion and non_fast_forward", liveTypes("deletion", "non_fast_forward"), "unprotected"},
		{"pull_request", liveTypes("pull_request"), "protected"},
		{"unknown", ship.BranchRulesResult{Reason: "no answer from GitHub within 20s"}, "unknown"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, err := checkWith(t, "main", tc.rb)
			if err != nil {
				t.Errorf("exit non-zero: %v", err)
			}
			if out != tc.want+"\n" {
				t.Errorf("stdout = %q, want exactly %q", out, tc.want)
			}
		})
	}
	if _, err := runProtect(t, "--check", "main", "--apply"); err == nil {
		t.Error("--check with --apply was accepted")
	}
}

func TestProtectFlags(t *testing.T) {
	c := Protect()
	for _, name := range []string{"apply", "check"} {
		if c.Flags().Lookup(name) == nil {
			t.Errorf("no --%s flag", name)
		}
	}
	if !strings.Contains(ProtectFixHint, "dross protect --apply") {
		t.Errorf("ProtectFixHint = %q", ProtectFixHint)
	}
}

// TestReadmeAdvertisesOnlyRealCommands is one-directional: it catches a row
// for a command that doesn't exist, never a command with no row.
func TestReadmeHasProtectRow(t *testing.T) {
	b, err := os.ReadFile(filepath.Join(repoRootFromTest(t), "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "\n| `dross protect") {
		t.Error("README.md has no `dross protect` row in its command table")
	}
}
