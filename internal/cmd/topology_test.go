package cmd

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// topologyFixture builds a dross repo on main with `commits` extra commits on
// milestone/<version>, and leaves HEAD on the milestone branch. With no remote
// wired at all when originURL is empty — branchTopology must be a pure local
// read either way.
func topologyFixture(t *testing.T, version string, commits int, withOrigin bool) string {
	t.Helper()
	dir := t.TempDir()
	if withOrigin {
		remoteDir := t.TempDir()
		mustGit(t, remoteDir, "init", "-q", "--bare", "-b", "main")
		gitInit(t, dir, remoteDir)
	} else {
		// gitInit always adds an origin; init the repo without one by hand.
		for _, args := range [][]string{
			{"init", "-q", "-b", "main"},
			{"config", "user.email", "test@example.com"},
			{"config", "user.name", "Test"},
			{"config", "commit.gpgsign", "false"},
		} {
			c := exec.Command("git", append([]string{"-C", dir}, args...)...)
			if out, err := c.CombinedOutput(); err != nil {
				t.Fatalf("git %v: %v\n%s", args, err, out)
			}
		}
	}
	chdir(t, dir)
	if err := runCmd(t, Init()); err != nil {
		t.Fatalf("init: %v", err)
	}
	mustWrite(t, filepath.Join(dir, "README.md"), "base\n")
	mustGit(t, dir, "add", ".")
	mustGit(t, dir, "commit", "-q", "-m", "chore: baseline")

	if version == "" {
		return dir
	}

	// current_milestone is what resolveNewWorkBase infers from; the branch's
	// existence is the cutover switch.
	if err := runCmd(t, State(), "set", "current_milestone", version); err != nil {
		t.Fatalf("state set current_milestone: %v", err)
	}
	mustGit(t, dir, "checkout", "-q", "-b", "milestone/"+version)
	for i := 0; i < commits; i++ {
		mustWrite(t, filepath.Join(dir, "src.txt"), string(rune('a'+i))+"\n")
		mustGit(t, dir, "add", "src.txt")
		mustGit(t, dir, "commit", "-q", "-m", "feat: work")
	}
	return dir
}

// TestBranchTopologyCountsAheadOfMain (c-5): the count answers "how much work
// is staged on the milestone branch that main hasn't seen?". Counting the
// reverse range answers how far the milestone branch is *behind*, which is 0
// for every freshly-cut branch and would report a full milestone as empty.
func TestBranchTopologyCountsAheadOfMain(t *testing.T) {
	dir := topologyFixture(t, "v1.2", 3, true)

	got, err := branchTopology(dir, filepath.Join(dir, ".dross"), "")
	if err != nil {
		t.Fatalf("branchTopology: %v", err)
	}
	if got.Work != "milestone/v1.2" {
		t.Errorf("Work = %q, want milestone/v1.2", got.Work)
	}
	if got.AheadOfMain != 3 {
		t.Errorf("AheadOfMain = %d, want 3", got.AheadOfMain)
	}
	if got.OnMain {
		t.Error("OnMain should be false when the work branch is a milestone branch")
	}
	if got.Head != "milestone/v1.2" {
		t.Errorf("Head = %q, want milestone/v1.2", got.Head)
	}
	if got.Main != "main" {
		t.Errorf("Main = %q, want main", got.Main)
	}
}

// TestBranchTopologyWorkOverrideWins (c-5): the override is used verbatim and
// the count runs against it. A helper that re-infers would name the milestone
// branch the phase never forked from — the stale-milestone lie the base-truth
// work removed, retold in a completion statement.
func TestBranchTopologyWorkOverrideWins(t *testing.T) {
	dir := topologyFixture(t, "v1.2", 3, true)

	// A branch inference would never pick: two commits off main, unrelated to
	// the active milestone.
	mustGit(t, dir, "checkout", "-q", "-b", "release/legacy", "main")
	mustWrite(t, filepath.Join(dir, "legacy.txt"), "a\n")
	mustGit(t, dir, "add", "legacy.txt")
	mustGit(t, dir, "commit", "-q", "-m", "feat: legacy")
	mustGit(t, dir, "checkout", "-q", "milestone/v1.2")

	inferred, err := branchTopology(dir, filepath.Join(dir, ".dross"), "")
	if err != nil {
		t.Fatal(err)
	}
	if inferred.Work == "release/legacy" {
		t.Fatal("precondition: inference must not already pick release/legacy")
	}

	got, err := branchTopology(dir, filepath.Join(dir, ".dross"), "release/legacy")
	if err != nil {
		t.Fatalf("branchTopology: %v", err)
	}
	if got.Work != "release/legacy" {
		t.Errorf("Work = %q, want the override release/legacy", got.Work)
	}
	if got.AheadOfMain != 1 {
		t.Errorf("AheadOfMain = %d, want 1 — the count must run against the override, not the inferred branch", got.AheadOfMain)
	}
}

// TestBranchTopologyNoMilestoneNoRemote (c-5): status prints this line on every
// run, so a repo with no milestone branch and no origin must produce a partial
// answer, not an error. Erroring on a missing ref would break status in exactly
// the repos that have the least going on.
func TestBranchTopologyNoMilestoneNoRemote(t *testing.T) {
	dir := topologyFixture(t, "", 0, false)

	got, err := branchTopology(dir, filepath.Join(dir, ".dross"), "")
	if err != nil {
		t.Fatalf("branchTopology must degrade, not error: %v", err)
	}
	if got.Work != "main" {
		t.Errorf("Work = %q, want main", got.Work)
	}
	if got.AheadOfMain != 0 {
		t.Errorf("AheadOfMain = %d, want 0", got.AheadOfMain)
	}
	if !got.OnMain {
		t.Error("OnMain should be true when the work branch is main")
	}
}

// TestBranchTopologyDetachedHead (c-5): the same degrade-don't-break rule for a
// detached HEAD — Head is empty, the rest of the answer still holds.
func TestBranchTopologyDetachedHead(t *testing.T) {
	dir := topologyFixture(t, "v1.2", 2, true)
	mustGit(t, dir, "checkout", "-q", "--detach", "HEAD")

	got, err := branchTopology(dir, filepath.Join(dir, ".dross"), "")
	if err != nil {
		t.Fatalf("branchTopology must degrade on a detached HEAD: %v", err)
	}
	if got.Head != "" {
		t.Errorf("Head = %q, want empty on a detached HEAD", got.Head)
	}
	if got.Work != "milestone/v1.2" || got.AheadOfMain != 2 {
		t.Errorf("the rest of the answer should still hold, got %+v", got)
	}
}

// TestRenderTopologyLine (c-5): the "not yet on main" clause is conditional on
// AheadOfMain > 0 and nothing else, and the branch name is always named. A
// hardcoded clause turns a finished milestone into a standing warning; a bare
// count names nothing the user can act on.
func TestRenderTopologyLine(t *testing.T) {
	for _, tc := range []struct {
		name       string
		top        topology
		want       string
		wantClause bool
	}{
		{
			name: "ahead of main from a phase branch",
			top:  topology{Head: "phase/x", Work: "milestone/v1.2", Main: "main", AheadOfMain: 3},
			want: "phase/x · 3 commits on milestone/v1.2, not yet on main",
		},
		{
			name: "ahead of main standing on the work branch",
			top:  topology{Head: "milestone/v1.2", Work: "milestone/v1.2", Main: "main", AheadOfMain: 3},
			want: "3 commits on milestone/v1.2, not yet on main",
		},
		{
			name: "singular",
			top:  topology{Head: "milestone/v1.2", Work: "milestone/v1.2", Main: "main", AheadOfMain: 1},
			want: "1 commit on milestone/v1.2, not yet on main",
		},
		{
			name: "level with main",
			top:  topology{Head: "milestone/v1.2", Work: "milestone/v1.2", Main: "main", AheadOfMain: 0},
			want: "milestone/v1.2",
		},
		{
			name: "on main",
			top:  topology{Head: "main", Work: "main", Main: "main", AheadOfMain: 0, OnMain: true},
			want: "main",
		},
		{
			name: "detached",
			top:  topology{Head: "", Work: "main", Main: "main", AheadOfMain: 0, OnMain: true},
			want: "(detached HEAD) · main",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := renderTopologyLine(tc.top)
			if got != tc.want {
				t.Errorf("renderTopologyLine = %q, want %q", got, tc.want)
			}
			hasClause := strings.Contains(got, "not yet on")
			if want := tc.top.AheadOfMain > 0; hasClause != want {
				t.Errorf("the \"not yet on\" clause must appear exactly when AheadOfMain > 0 (=%d); got %q",
					tc.top.AheadOfMain, got)
			}
			// The branch name is always named, whichever branch that is.
			if !strings.Contains(got, tc.top.Work) {
				t.Errorf("the line must name the work branch %q, got %q", tc.top.Work, got)
			}
		})
	}
}
