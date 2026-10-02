package cmd

import (
	"errors"
	"fmt"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/protect"
	"github.com/Rivil/dross/internal/ship"
)

// choreSeams records what publishChorePR asked of the forge.
type choreSeams struct {
	open    *ship.OpenResult // what the open-PR lookup answers
	opened  []ship.OpenOpts
	armed   []string // merge methods auto-merge was armed with
	autoRes ship.AutoMergeResult
	autoErr error
	lookups int
}

// stubChoreSeams installs recording stubs for the three forge seams the chore
// publisher uses, restoring them when the test ends. A PR it opens is #41.
func stubChoreSeams(t *testing.T, s *choreSeams) {
	t.Helper()
	prevFind, prevOpen, prevAuto := ship.FindOpenPRByHeadFunc, ship.OpenPRFunc, ship.AutoMergePRFunc
	ship.FindOpenPRByHeadFunc = func(_ ship.OpenOpts, head string) (*ship.OpenResult, error) {
		s.lookups++
		return s.open, nil
	}
	ship.OpenPRFunc = func(o ship.OpenOpts) (*ship.OpenResult, error) {
		s.opened = append(s.opened, o)
		return &ship.OpenResult{Number: 41, URL: "https://github.com/o/r/pull/41"}, nil
	}
	ship.AutoMergePRFunc = func(_ ship.OpenOpts, n int, method string) (ship.AutoMergeResult, error) {
		s.armed = append(s.armed, method)
		return s.autoRes, s.autoErr
	}
	t.Cleanup(func() {
		ship.FindOpenPRByHeadFunc, ship.OpenPRFunc, ship.AutoMergePRFunc = prevFind, prevOpen, prevAuto
	})
}

var choreOpts = ship.OpenOpts{Provider: "github", URL: "https://github.com/o/r"}

// choreRepo is a clone of a bare origin whose main carries a project.toml at
// version, pushed. Nothing else: the publisher needs git, not a dross init.
func choreRepo(t *testing.T, version string) (dir, origin string) {
	t.Helper()
	dir, origin = t.TempDir(), t.TempDir()
	mustGit(t, origin, "init", "--bare", "-q", "-b", "main")
	gitInit(t, dir, origin)
	setVersion(t, dir, version)
	mustWrite(t, filepath.Join(dir, "README.md"), "# r\n")
	mustGit(t, dir, "add", "-A")
	mustGit(t, dir, "commit", "-q", "-m", "init")
	mustGit(t, dir, "push", "-q", "-u", "origin", "main")
	return dir, origin
}

func setVersion(t *testing.T, dir, version string) {
	t.Helper()
	mustWrite(t, filepath.Join(dir, ".dross", "project.toml"), "[project]\n  name = \"r\"\n  version = \""+version+"\"\n")
}

// choreCommit commits a .dross-only change on the current branch.
func choreCommit(t *testing.T, dir, name string) {
	t.Helper()
	mustWrite(t, filepath.Join(dir, ".dross", name), name+"\n")
	mustGit(t, dir, "add", ".dross")
	mustGit(t, dir, "commit", "-q", "-m", "chore(dross): "+name)
}

func originRef(t *testing.T, origin, ref string) string {
	t.Helper()
	out, err := gitOut(origin, "rev-parse", "--verify", "--quiet", ref)
	if err != nil {
		return ""
	}
	return out
}

// gitOut runs git in dir and returns its trimmed stdout, failing soft so a
// missing ref is a value, not a fatal.
func gitOut(dir string, args ...string) (string, error) {
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).Output()
	return strings.TrimSpace(string(out)), err
}

// A purely-ahead base reaches origin as dross-chores/main at local main's exact
// tip, through a newly opened PR armed for a merge commit — never a push to
// main.
func TestChorePRPublishesPurelyAhead(t *testing.T) {
	dir, origin := choreRepo(t, "1.7.19.0")
	mainBefore := originRef(t, origin, "refs/heads/main")
	choreCommit(t, dir, "handoff.md")
	mustGit(t, dir, "fetch", "-q", "origin")
	s := &choreSeams{autoRes: ship.AutoMergeResult{AutoEnabled: true}}
	stubChoreSeams(t, s)

	c, err := publishChorePR(dir, choreOpts, "main", "main")
	if err != nil {
		t.Fatal(err)
	}
	if got := originRef(t, origin, "refs/heads/dross-chores/main"); got != mustGit(t, dir, "rev-parse", "main") {
		t.Errorf("origin dross-chores/main = %s, want local main's tip", got)
	}
	if got := originRef(t, origin, "refs/heads/main"); got != mainBefore {
		t.Errorf("origin main moved %s → %s; the chore must not be pushed to the base", mainBefore, got)
	}
	if len(s.opened) != 1 || s.opened[0].HeadBranch != "dross-chores/main" || s.opened[0].BaseBranch != "main" {
		t.Errorf("opened %+v, want one PR dross-chores/main → main", s.opened)
	}
	if !c.Opened || !c.AutoMerge || c.Number != 41 || c.URL == "" {
		t.Errorf("result = %+v", c)
	}
}

// With the chore PR still open, the next batch fast-forwards its branch and
// joins the PR: no second PR.
func TestChorePRJoinsOpenPR(t *testing.T) {
	dir, origin := choreRepo(t, "1.7.19.0")
	choreCommit(t, dir, "a.md")
	mustGit(t, dir, "fetch", "-q", "origin")
	s := &choreSeams{autoRes: ship.AutoMergeResult{AutoEnabled: true}}
	stubChoreSeams(t, s)
	if _, err := publishChorePR(dir, choreOpts, "main", "main"); err != nil {
		t.Fatal(err)
	}
	first := originRef(t, origin, "refs/heads/dross-chores/main")

	s.open = &ship.OpenResult{Number: 41, URL: "https://github.com/o/r/pull/41"}
	s.opened = nil
	choreCommit(t, dir, "b.md")
	mustGit(t, dir, "fetch", "-q", "origin")
	c, err := publishChorePR(dir, choreOpts, "main", "main")
	if err != nil {
		t.Fatal(err)
	}
	second := originRef(t, origin, "refs/heads/dross-chores/main")
	if second != mustGit(t, dir, "rev-parse", "main") {
		t.Errorf("chore branch not at local tip after the second batch")
	}
	if _, err := gitOut(dir, "merge-base", "--is-ancestor", first, second); err != nil {
		t.Errorf("second batch was not a fast-forward of the first (%s → %s)", first, second)
	}
	if len(s.opened) != 0 || c.Opened || c.Number != 41 {
		t.Errorf("joining opened %d PRs, result %+v; want 0 and #41 joined", len(s.opened), c)
	}
}

// pushStrayChore puts a commit local main doesn't reach on origin's chore
// branch, as another machine would; force overwrites whatever is there.
func pushStrayChore(t *testing.T, dir string, force ...bool) string {
	t.Helper()
	mustGit(t, dir, "switch", "-q", "-c", "stray")
	choreCommit(t, dir, "stray.md")
	args := []string{"push", "-q", "origin", "stray:refs/heads/dross-chores/main"}
	if len(force) > 0 && force[0] {
		args = []string{"push", "-q", "-f", "origin", "stray:refs/heads/dross-chores/main"}
	}
	mustGit(t, dir, args...)
	stray := mustGit(t, dir, "rev-parse", "stray")
	mustGit(t, dir, "switch", "-q", "main")
	mustGit(t, dir, "branch", "-q", "-D", "stray")
	return stray
}

func TestChorePRDivergedChoreBranch(t *testing.T) {
	t.Run("open PR: refuse naming it, push nothing", func(t *testing.T) {
		dir, origin := choreRepo(t, "1.7.19.0")
		stray := pushStrayChore(t, dir)
		choreCommit(t, dir, "mine.md")
		mustGit(t, dir, "fetch", "-q", "origin")
		s := &choreSeams{open: &ship.OpenResult{Number: 7, URL: "https://github.com/o/r/pull/7"}}
		stubChoreSeams(t, s)

		_, err := publishChorePR(dir, choreOpts, "main", "main")
		if err == nil || !strings.Contains(err.Error(), "https://github.com/o/r/pull/7") {
			t.Fatalf("err = %v, want a refusal naming the open PR's URL", err)
		}
		if got := originRef(t, origin, "refs/heads/dross-chores/main"); got != stray {
			t.Errorf("origin chore branch moved %s → %s", stray, got)
		}
		if len(s.opened) != 0 || len(s.armed) != 0 {
			t.Errorf("opened %d, armed %d after a refusal", len(s.opened), len(s.armed))
		}
	})

	t.Run("no open PR: recreate with lease and open a PR", func(t *testing.T) {
		dir, origin := choreRepo(t, "1.7.19.0")
		pushStrayChore(t, dir)
		choreCommit(t, dir, "mine.md")
		mustGit(t, dir, "fetch", "-q", "origin")
		s := &choreSeams{autoRes: ship.AutoMergeResult{AutoEnabled: true}}
		stubChoreSeams(t, s)

		c, err := publishChorePR(dir, choreOpts, "main", "main")
		if err != nil {
			t.Fatal(err)
		}
		if got := originRef(t, origin, "refs/heads/dross-chores/main"); got != mustGit(t, dir, "rev-parse", "main") {
			t.Errorf("stale chore branch not recreated at local main's tip")
		}
		if len(s.opened) != 1 || !c.Opened {
			t.Errorf("want a new PR, opened %d", len(s.opened))
		}
	})

	t.Run("lease refuses a push that raced the fetch", func(t *testing.T) {
		dir, origin := choreRepo(t, "1.7.19.0")
		fetched := pushStrayChore(t, dir)
		choreCommit(t, dir, "mine.md")
		mustGit(t, dir, "fetch", "-q", "origin")
		// Someone moves the chore branch again after our fetch; our view of
		// it is still the commit we fetched.
		raced := pushStrayChore(t, dir, true)
		mustGit(t, dir, "update-ref", "refs/remotes/origin/dross-chores/main", fetched)
		stubChoreSeams(t, &choreSeams{})
		if _, err := publishChorePR(dir, choreOpts, "main", "main"); err == nil {
			t.Error("a force push past a lease that no longer holds succeeded")
		}
		if got := originRef(t, origin, "refs/heads/dross-chores/main"); got != raced {
			t.Errorf("origin chore branch = %s, want the raced commit %s untouched", got, raced)
		}
	})
}

// Chore PRs merge as merge commits so local base can fast-forward to the
// result; a squash would leave local's commits off base forever.
func TestChorePRMergesAsMergeCommit(t *testing.T) {
	dir, _ := choreRepo(t, "1.7.19.0")
	choreCommit(t, dir, "a.md")
	mustGit(t, dir, "fetch", "-q", "origin")
	s := &choreSeams{autoRes: ship.AutoMergeResult{AutoEnabled: true}}
	stubChoreSeams(t, s)
	if _, err := publishChorePR(dir, choreOpts, "main", "main"); err != nil {
		t.Fatal(err)
	}
	if len(s.armed) != 1 || s.armed[0] != "merge" {
		t.Errorf("auto-merge armed with %v, want exactly [merge]", s.armed)
	}
}

// Auto-merge off for the repo is not a failure: the PR is open and the user
// is told to merge it.
func TestChorePRDegradesWhenAutoMergeUnavailable(t *testing.T) {
	dir, _ := choreRepo(t, "1.7.19.0")
	choreCommit(t, dir, "a.md")
	mustGit(t, dir, "fetch", "-q", "origin")
	stubChoreSeams(t, &choreSeams{autoErr: fmt.Errorf("%w: off", ship.ErrAutoMergeUnavailable)})

	c, err := publishChorePR(dir, choreOpts, "main", "main")
	if err != nil {
		t.Fatalf("unavailable auto-merge surfaced as an error: %v", err)
	}
	const url = "https://github.com/o/r/pull/41"
	want := chorePR{Base: "main", Branch: "dross-chores/main", Number: 41, URL: url, Opened: true, Manual: true}
	if c != want {
		t.Errorf("result %+v, want %+v", c, want)
	}
	if n := c.narrate(); !strings.Contains(n, "opened chore PR #41 "+url) || !strings.Contains(n, "merge it by hand") {
		t.Errorf("narrates %q; want the opened PR's URL and \"merge it by hand\"", n)
	}

	t.Run("any other auto-merge failure is an error", func(t *testing.T) {
		choreCommit(t, dir, "b.md")
		mustGit(t, dir, "fetch", "-q", "origin")
		stubChoreSeams(t, &choreSeams{open: &ship.OpenResult{Number: 41, URL: "u"}, autoErr: errors.New("network")})
		if _, err := publishChorePR(dir, choreOpts, "main", "main"); err == nil {
			t.Error("a failed arm was swallowed")
		}
	})
}

// A phase squash on origin bumps main's version; a chore batch that leaves the
// version alone must still publish. The guard compares the batch's own change.
func TestReleaseGuardIgnoresOriginBump(t *testing.T) {
	dir, origin := choreRepo(t, "1.7.19.0")
	mustGit(t, dir, "switch", "-q", "-c", "squash")
	setVersion(t, dir, "1.7.20.0")
	mustWrite(t, filepath.Join(dir, "feature.go"), "package f\n")
	mustGit(t, dir, "add", "-A")
	mustGit(t, dir, "commit", "-q", "-m", "phase x: x (#1)")
	mustGit(t, dir, "push", "-q", "origin", "squash:main")
	mustGit(t, dir, "switch", "-q", "main")
	mustGit(t, dir, "branch", "-q", "-D", "squash")

	choreCommit(t, dir, "handoff.md") // ahead and behind, version untouched
	mustGit(t, dir, "fetch", "-q", "origin")
	stubChoreSeams(t, &choreSeams{autoRes: ship.AutoMergeResult{AutoEnabled: true}})

	if _, err := publishChorePR(dir, choreOpts, "main", "main"); err != nil {
		t.Fatalf("a version-neutral batch was refused after origin's own bump: %v", err)
	}
	if got := originRef(t, origin, "refs/heads/dross-chores/main"); got != mustGit(t, dir, "rev-parse", "main") {
		t.Errorf("origin dross-chores/main = %s, want local main's tip", got)
	}
	if left := mustGit(t, dir, "rev-list", "main", "--not", "origin/main", "origin/dross-chores/main"); left != "" {
		t.Errorf("commits of local main reach origin through neither main nor the chore branch: %s", left)
	}
}

// A main batch that changes the projected release tag is refused before any
// ref moves, naming the way out. The milestone branch cuts no release.
func TestReleaseGuardRefusesTagChange(t *testing.T) {
	dir, origin := choreRepo(t, "1.7.19.1")
	setVersion(t, dir, "1.8.0.0")
	mustGit(t, dir, "add", ".dross")
	mustGit(t, dir, "commit", "-q", "-m", "chore(dross): scope v1.8")
	localBefore := mustGit(t, dir, "rev-parse", "main")
	originBefore := originRef(t, origin, "refs/heads/main")
	mustGit(t, dir, "fetch", "-q", "origin")
	s := &choreSeams{}
	stubChoreSeams(t, s)

	_, err := publishChorePR(dir, choreOpts, "main", "main")
	if err == nil {
		t.Fatal("a release-cutting chore batch was published")
	}
	for _, want := range []string{"milestone branch", "reset main to origin/main"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal %q does not name the next step (%q)", err, want)
		}
	}
	if originRef(t, origin, "refs/heads/main") != originBefore || originRef(t, origin, "refs/heads/dross-chores/main") != "" ||
		mustGit(t, dir, "rev-parse", "main") != localBefore {
		t.Error("the refusal moved a ref")
	}
	if s.lookups+len(s.opened)+len(s.armed) != 0 {
		t.Errorf("the refusal reached the forge: %+v", s)
	}

	t.Run("quick bump publishes", func(t *testing.T) {
		dir, _ := choreRepo(t, "1.7.19.0")
		setVersion(t, dir, "1.7.19.1")
		mustGit(t, dir, "add", ".dross")
		mustGit(t, dir, "commit", "-q", "-m", "chore(dross): record quick")
		mustGit(t, dir, "fetch", "-q", "origin")
		stubChoreSeams(t, &choreSeams{autoRes: ship.AutoMergeResult{AutoEnabled: true}})
		if _, err := publishChorePR(dir, choreOpts, "main", "main"); err != nil {
			t.Errorf("an .internal bump cuts no release, yet was refused: %v", err)
		}
	})

	t.Run("milestone base is not guarded", func(t *testing.T) {
		dir, _ := choreRepo(t, "1.7.19.1")
		mustGit(t, dir, "switch", "-q", "-c", "milestone/v1.8")
		mustGit(t, dir, "push", "-q", "-u", "origin", "milestone/v1.8")
		setVersion(t, dir, "1.8.0.0")
		mustGit(t, dir, "add", ".dross")
		mustGit(t, dir, "commit", "-q", "-m", "chore(dross): scope v1.8")
		mustGit(t, dir, "fetch", "-q", "origin")
		s := &choreSeams{autoRes: ship.AutoMergeResult{Merged: true}}
		stubChoreSeams(t, s)
		c, err := publishChorePR(dir, choreOpts, "milestone/v1.8", "main")
		if err != nil {
			t.Fatalf("a milestone batch was refused: %v", err)
		}
		if c.Branch != "dross-chores/milestone-v1.8" || len(s.opened) != 1 || s.opened[0].BaseBranch != "milestone/v1.8" {
			t.Errorf("result %+v, opened %+v", c, s.opened)
		}
	})
}

// A chore branch must stay pushable: it can match neither ruleset's include.
func TestChoreBranchMatchesNoRuleset(t *testing.T) {
	main, err := protect.MainRuleset("main", []string{"test"})
	if err != nil {
		t.Fatal(err)
	}
	includes := append(main.Conditions.RefName.Include, protect.MilestoneRuleset().Conditions.RefName.Include...)
	for _, base := range []string{"main", "milestone/v1.7"} {
		ref := "refs/heads/" + choreBranch(base)
		for _, inc := range includes {
			if ok, _ := path.Match(inc, ref); ok {
				t.Errorf("%s matches ruleset include %s", ref, inc)
			}
		}
	}
	if got := choreBranch("milestone/v1.7"); got != "dross-chores/milestone-v1.7" {
		t.Errorf("choreBranch(milestone/v1.7) = %q", got)
	}
}

func TestChorePRNarration(t *testing.T) {
	base := chorePR{Base: "main", Number: 41, URL: "https://github.com/o/r/pull/41"}
	for _, tc := range []struct {
		c    chorePR
		want []string
	}{
		{func() chorePR { c := base; c.Opened, c.AutoMerge = true, true; return c }(), []string{"opened chore PR #41", base.URL, "auto-merge armed"}},
		{func() chorePR { c := base; c.AutoMerge = true; return c }(), []string{"joined chore PR #41", "auto-merge armed"}},
		{func() chorePR { c := base; c.Merged = true; return c }(), []string{"— merged"}},
		{func() chorePR { c := base; c.Manual = true; return c }(), []string{"merge it by hand"}},
	} {
		n := tc.c.narrate()
		for _, w := range tc.want {
			if !strings.Contains(n, w) {
				t.Errorf("%+v narrates %q, missing %q", tc.c, n, w)
			}
		}
	}
	if got := pullURL("https://github.com/o/r.git/", 7); got != "https://github.com/o/r/pull/7" {
		t.Errorf("pullURL = %q", got)
	}
}
