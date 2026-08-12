package cmd

import (
	"path/filepath"
	"strings"
	"testing"
)

// staleRepo is a git repo with an origin bare remote, one baseline commit on
// main, and nothing else. Every fixture below grows from it.
func staleRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	remote := t.TempDir()
	mustGit(t, remote, "init", "-q", "--bare", "-b", "main")
	gitInit(t, dir, remote)
	mustWrite(t, filepath.Join(dir, "README.md"), "base\n")
	mustGit(t, dir, "add", ".")
	mustGit(t, dir, "commit", "-q", "-m", "chore: baseline")
	mustGit(t, dir, "push", "-q", "-u", "origin", "main")
	return dir
}

// commitOn checks out branch and adds one commit touching path.
func commitOn(t *testing.T, dir, branch, path, body, msg string) {
	t.Helper()
	mustGit(t, dir, "checkout", "-q", branch)
	mustWrite(t, filepath.Join(dir, path), body)
	mustGit(t, dir, "add", path)
	mustGit(t, dir, "commit", "-q", "-m", msg)
}

// squashOnto collapses branch's whole contribution into one commit on main, the
// way a forge's squash-merge does: main's tree gains the branch's changes, main's
// history never reaches the branch.
func squashOnto(t *testing.T, dir, branch, msg string) string {
	t.Helper()
	mustGit(t, dir, "checkout", "-q", "main")
	mustGit(t, dir, "merge", "-q", "--squash", branch)
	mustGit(t, dir, "commit", "-q", "-m", msg)
	return mustGit(t, dir, "rev-parse", "HEAD")
}

// pushMain publishes local main to origin. The detector measures merged-ness
// against origin/main, so a fixture that lands work on main and stops there has
// not yet reproduced a merge anyone else can see — which is exactly the
// distinction TestStaleIgnoresLocalOnlyMerge pins.
func pushMain(t *testing.T, dir string) {
	t.Helper()
	mustGit(t, dir, "push", "-q", "origin", "main")
}

// byName indexes a result set so assertions read by branch rather than by
// position — the order the detector walks refs in is git's, not the test's.
func byName(got []staleBranch) map[string]staleBranch {
	m := map[string]staleBranch{}
	for _, b := range got {
		m[b.Name] = b
	}
	return m
}

// TestStaleDetectsSquashMergedBranch is the case `git branch --merged` cannot
// see: the work is on main, collapsed into one commit main's history does not
// descend from. An ancestry-only detector reports nothing here.
func TestStaleDetectsSquashMergedBranch(t *testing.T) {
	dir := staleRepo(t)
	mustGit(t, dir, "checkout", "-q", "-b", "milestone/v1.0")
	commitOn(t, dir, "milestone/v1.0", "a.txt", "a\n", "feat: a")
	commitOn(t, dir, "milestone/v1.0", "b.txt", "b\n", "feat: b")
	commitOn(t, dir, "milestone/v1.0", "c.txt", "c\n", "feat: c")
	squash := squashOnto(t, dir, "milestone/v1.0", "feat(squash): v1.0")
	pushMain(t, dir)

	// Precondition: git's own merged-check is blind to this.
	if merged := mustGit(t, dir, "branch", "--merged", "main"); strings.Contains(merged, "milestone/v1.0") {
		t.Fatalf("precondition: `git branch --merged` should not see a squash-merge, got:\n%s", merged)
	}

	got, err := staleMilestoneBranches(dir, "main")
	if err != nil {
		t.Fatal(err)
	}
	b, ok := byName(got)["milestone/v1.0"]
	if !ok {
		t.Fatalf("squash-merged branch not reported stale: %+v", got)
	}
	if b.Reason != reasonSquash {
		t.Errorf("reason = %q, want %q", b.Reason, reasonSquash)
	}
	if b.Squash != squash {
		t.Errorf("resolved squash = %q, want %q", b.Squash, squash)
	}
}

// TestStaleSquashWithMainAdvancing is the shape this repo's own history has: a
// commit lands on main between the fork and the merge, and two more after it.
// Diffing the branch against the fork-point merge-base picks up that first
// commit's changes and never matches — the candidate's patch has to be taken
// against its own first parent.
func TestStaleSquashWithMainAdvancing(t *testing.T) {
	dir := staleRepo(t)
	mustGit(t, dir, "checkout", "-q", "-b", "milestone/v1.0")
	commitOn(t, dir, "milestone/v1.0", "a.txt", "a\n", "feat: a")
	commitOn(t, dir, "milestone/v1.0", "b.txt", "b\n", "feat: b")

	// One unrelated commit on main between the fork and the merge.
	commitOn(t, dir, "main", "unrelated.txt", "u\n", "chore: unrelated")
	squash := squashOnto(t, dir, "milestone/v1.0", "feat(squash): v1.0")
	// …and two more after it.
	commitOn(t, dir, "main", "after1.txt", "1\n", "chore: after 1")
	commitOn(t, dir, "main", "after2.txt", "2\n", "chore: after 2")
	pushMain(t, dir)

	got, err := staleMilestoneBranches(dir, "main")
	if err != nil {
		t.Fatal(err)
	}
	b, ok := byName(got)["milestone/v1.0"]
	if !ok {
		t.Fatalf("branch squash-merged onto an advancing main not reported stale: %+v", got)
	}
	if b.Squash != squash {
		t.Errorf("resolved squash = %q, want the squash commit %q", b.Squash, squash)
	}
}

// TestStaleAmbiguousSquashIsDeterministic: two commits on main carrying the same
// patch must give one answer, the same one every run — not whichever the walk
// reached first.
func TestStaleAmbiguousSquashIsDeterministic(t *testing.T) {
	dir := staleRepo(t)
	mustGit(t, dir, "checkout", "-q", "-b", "milestone/v1.0")
	commitOn(t, dir, "milestone/v1.0", "a.txt", "a\n", "feat: a")

	first := squashOnto(t, dir, "milestone/v1.0", "feat(squash): v1.0")
	// Revert it and land the identical patch a second time: same patch-id,
	// two candidate commits.
	mustGit(t, dir, "revert", "--no-edit", first)
	second := squashOnto(t, dir, "milestone/v1.0", "feat(squash): v1.0 again")
	pushMain(t, dir)

	var seen string
	for i := 0; i < 3; i++ {
		got, err := staleMilestoneBranches(dir, "main")
		if err != nil {
			t.Fatal(err)
		}
		b, ok := byName(got)["milestone/v1.0"]
		if !ok {
			t.Fatalf("run %d: ambiguous branch should still be reported stale once: %+v", i, got)
		}
		if b.Squash != first && b.Squash != second {
			t.Fatalf("run %d: resolved %q, want one of the two identical commits", i, b.Squash)
		}
		if seen == "" {
			seen = b.Squash
		} else if b.Squash != seen {
			t.Fatalf("run %d resolved %q, run 1 resolved %q — the answer is a coin-flip", i, b.Squash, seen)
		}
	}
	// The branch appears once, not once per matching commit.
	got, _ := staleMilestoneBranches(dir, "main")
	if n := len(got); n != 1 {
		t.Errorf("result has %d entries, want 1: %+v", n, got)
	}
}

// TestStaleUnresolvableSquashIsNotStale: a squash that was later amended has no
// patch-id match left on main. That is not an error and not a stale branch —
// prune deletes what this reports, so "no answer" must mean "leave it alone".
func TestStaleUnresolvableSquashIsNotStale(t *testing.T) {
	dir := staleRepo(t)
	mustGit(t, dir, "checkout", "-q", "-b", "milestone/v1.0")
	commitOn(t, dir, "milestone/v1.0", "a.txt", "a\n", "feat: a")
	squashOnto(t, dir, "milestone/v1.0", "feat(squash): v1.0")

	// Amend the squash so its patch no longer matches the branch's.
	mustGit(t, dir, "checkout", "-q", "main")
	mustWrite(t, filepath.Join(dir, "a.txt"), "a-amended\n")
	mustGit(t, dir, "add", "a.txt")
	mustGit(t, dir, "commit", "-q", "--amend", "--no-edit")
	// Force-push, so origin/main carries the amended commit too — otherwise the
	// branch would read not-stale merely because origin never saw the squash,
	// which is a different reason than the one this test is about.
	mustGit(t, dir, "push", "-q", "--force", "origin", "main")

	got, err := staleMilestoneBranches(dir, "main")
	if err != nil {
		t.Fatalf("an unresolvable squash must not be an error: %v", err)
	}
	if _, ok := byName(got)["milestone/v1.0"]; ok {
		t.Errorf("an amended squash leaves no evidence — the branch must not be reported: %+v", got)
	}
}

// TestStaleFastForwardIsMergedNotSquash: the two reasons must not collapse. A
// branch main plainly descends from is "merged", and the cheap ancestry check
// is what says so.
func TestStaleFastForwardIsMergedNotSquash(t *testing.T) {
	dir := staleRepo(t)
	mustGit(t, dir, "checkout", "-q", "-b", "milestone/v1.1")
	commitOn(t, dir, "milestone/v1.1", "a.txt", "a\n", "feat: a")
	mustGit(t, dir, "checkout", "-q", "main")
	mustGit(t, dir, "merge", "-q", "--ff-only", "milestone/v1.1")
	pushMain(t, dir)

	got, err := staleMilestoneBranches(dir, "main")
	if err != nil {
		t.Fatal(err)
	}
	b, ok := byName(got)["milestone/v1.1"]
	if !ok {
		t.Fatalf("a fast-forwarded branch is stale: %+v", got)
	}
	if b.Reason != reasonMerged {
		t.Errorf("reason = %q, want %q", b.Reason, reasonMerged)
	}
	if b.Squash != "" {
		t.Errorf("a plainly-merged branch has no squash commit to name, got %q", b.Squash)
	}
}

// TestStaleUnmergedBranchIsNotReported: one commit absent from main in any form
// is enough to keep the branch alive. Over-firing here deletes real work.
func TestStaleUnmergedBranchIsNotReported(t *testing.T) {
	dir := staleRepo(t)
	mustGit(t, dir, "checkout", "-q", "-b", "milestone/v1.2")
	commitOn(t, dir, "milestone/v1.2", "a.txt", "a\n", "feat: a")
	commitOn(t, dir, "milestone/v1.2", "b.txt", "b\n", "feat: b")
	mustGit(t, dir, "checkout", "-q", "main")

	got, err := staleMilestoneBranches(dir, "main")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := byName(got)["milestone/v1.2"]; ok {
		t.Errorf("an unmerged branch must not be reported stale: %+v", got)
	}
}

// TestStaleResolvesRemotePerBranch: prune deletes on origin only where origin
// has the branch, so the flag has to be per-branch rather than per-repo.
func TestStaleResolvesRemotePerBranch(t *testing.T) {
	dir := staleRepo(t)
	for _, v := range []string{"v1.0", "v1.1"} {
		mustGit(t, dir, "checkout", "-q", "-b", "milestone/"+v, "main")
		commitOn(t, dir, "milestone/"+v, v+".txt", v+"\n", "feat: "+v)
		squashOnto(t, dir, "milestone/"+v, "feat(squash): "+v)
	}
	mustGit(t, dir, "push", "-q", "origin", "milestone/v1.0")
	pushMain(t, dir)

	got, err := staleMilestoneBranches(dir, "main")
	if err != nil {
		t.Fatal(err)
	}
	idx := byName(got)
	if b, ok := idx["milestone/v1.0"]; !ok || !b.HasRemote {
		t.Errorf("the pushed branch should report HasRemote: %+v", idx["milestone/v1.0"])
	}
	if b, ok := idx["milestone/v1.1"]; !ok || b.HasRemote {
		t.Errorf("the local-only branch should not report HasRemote: %+v", b)
	}
}

// TestStaleIgnoresNonMilestoneBranches: the glob is refs/heads/milestone/*, so a
// stale phase branch is somebody else's problem — phase complete deletes those.
func TestStaleIgnoresNonMilestoneBranches(t *testing.T) {
	dir := staleRepo(t)
	mustGit(t, dir, "checkout", "-q", "-b", "phase/foo")
	commitOn(t, dir, "phase/foo", "a.txt", "a\n", "feat: a")
	squashOnto(t, dir, "phase/foo", "feat(squash): foo")

	got, err := staleMilestoneBranches(dir, "main")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("only milestone/* branches are in scope, got: %+v", got)
	}
}

// TestStaleNoMilestoneRefs: a repo that has never scoped a milestone is the
// common case, and it answers with nothing rather than an error.
func TestStaleNoMilestoneRefs(t *testing.T) {
	dir := staleRepo(t)

	got, err := staleMilestoneBranches(dir, "main")
	if err != nil {
		t.Fatalf("no milestone refs is not an error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("want no results, got: %+v", got)
	}
}

// TestStaleIsReadOnly: doctor calls this, and a diagnostic that mutates refs is
// the failure mode prune_surface exists to prevent.
func TestStaleIsReadOnly(t *testing.T) {
	dir := staleRepo(t)
	mustGit(t, dir, "checkout", "-q", "-b", "milestone/v1.0")
	commitOn(t, dir, "milestone/v1.0", "a.txt", "a\n", "feat: a")
	squashOnto(t, dir, "milestone/v1.0", "feat(squash): v1.0")
	mustGit(t, dir, "push", "-q", "origin", "milestone/v1.0")
	pushMain(t, dir)

	before := mustGit(t, dir, "for-each-ref", "--format=%(refname) %(objectname)")
	head := mustGit(t, dir, "rev-parse", "HEAD")

	if _, err := staleMilestoneBranches(dir, "main"); err != nil {
		t.Fatal(err)
	}

	if after := mustGit(t, dir, "for-each-ref", "--format=%(refname) %(objectname)"); after != before {
		t.Errorf("the scan moved a ref:\nbefore:\n%s\nafter:\n%s", before, after)
	}
	if now := mustGit(t, dir, "rev-parse", "HEAD"); now != head {
		t.Errorf("the scan moved HEAD: %s -> %s", head, now)
	}
	if st := mustGit(t, dir, "status", "--porcelain"); st != "" {
		t.Errorf("the scan dirtied the tree: %q", st)
	}
}

// TestStaleUnknownMainBranchErrors: a typo'd main branch must be a loud error,
// not an empty result that reads as "nothing is stale".
func TestStaleUnknownMainBranchErrors(t *testing.T) {
	dir := staleRepo(t)
	if _, err := staleMilestoneBranches(dir, "nope"); err == nil {
		t.Error("an unknown main branch should error rather than report nothing stale")
	}
}

// TestStaleIgnoresLocalOnlyMerge is c-6, and the two halves differ in exactly
// one thing: whether origin/main has caught up.
//
// The failure it pins is not hypothetical — a milestone branch cut while local
// main sat ahead of origin would be reported stale on the day it was created,
// and `dross milestone prune` deletes what this reports, local and remote.
func TestStaleIgnoresLocalOnlyMerge(t *testing.T) {
	dir := staleRepo(t)
	mustGit(t, dir, "checkout", "-q", "-b", "milestone/v1.0")
	commitOn(t, dir, "milestone/v1.0", "a.txt", "a\n", "feat: a")
	squashOnto(t, dir, "milestone/v1.0", "feat(squash): v1.0")

	// Half one: the squash exists on LOCAL main only. Origin has never seen it.
	got, err := staleMilestoneBranches(dir, "main")
	if err != nil {
		t.Fatal(err)
	}
	if b, ok := byName(got)["milestone/v1.0"]; ok {
		t.Fatalf("a merge origin has never seen must not make the branch stale, got %+v", b)
	}

	// Half two: the identical fixture, one push later.
	pushMain(t, dir)

	got, err = staleMilestoneBranches(dir, "main")
	if err != nil {
		t.Fatal(err)
	}
	b, ok := byName(got)["milestone/v1.0"]
	if !ok {
		t.Fatalf("once origin/main carries the squash the branch is stale: %+v", got)
	}
	if b.Reason != reasonSquash {
		t.Errorf("reason = %q, want %q", b.Reason, reasonSquash)
	}
}

// TestStaleAncestryArmFollowsOriginToo: the resolved ref has to reach BOTH arms.
// A fix applied only to the squash walk still reports a fast-forwarded branch as
// merged off the back of a local-only main.
func TestStaleAncestryArmFollowsOriginToo(t *testing.T) {
	dir := staleRepo(t)
	mustGit(t, dir, "checkout", "-q", "-b", "milestone/v1.1")
	commitOn(t, dir, "milestone/v1.1", "a.txt", "a\n", "feat: a")
	mustGit(t, dir, "checkout", "-q", "main")
	mustGit(t, dir, "merge", "-q", "--ff-only", "milestone/v1.1")

	// Precondition: local main really does contain the branch — an ancestry
	// check against refs/heads/main would say merged right now.
	if merged := mustGit(t, dir, "branch", "--merged", "main"); !strings.Contains(merged, "milestone/v1.1") {
		t.Fatalf("precondition: local main should contain the branch, got:\n%s", merged)
	}

	got, err := staleMilestoneBranches(dir, "main")
	if err != nil {
		t.Fatal(err)
	}
	if b, ok := byName(got)["milestone/v1.1"]; ok {
		t.Errorf("the ancestry arm still reads local main: %+v", b)
	}
}

// TestStaleFallsBackToLocalMainWithoutOrigin: preferring origin cannot mean
// requiring it. A repo whose main has never been pushed still gets an answer —
// there the local ref is the only history with a claim on the question.
func TestStaleFallsBackToLocalMainWithoutOrigin(t *testing.T) {
	dir := t.TempDir()
	remote := t.TempDir()
	mustGit(t, remote, "init", "-q", "--bare", "-b", "main")
	gitInit(t, dir, remote)
	mustWrite(t, filepath.Join(dir, "README.md"), "base\n")
	mustGit(t, dir, "add", ".")
	mustGit(t, dir, "commit", "-q", "-m", "chore: baseline")
	// Deliberately no push: origin exists but carries no refs at all.

	mustGit(t, dir, "checkout", "-q", "-b", "milestone/v1.0")
	commitOn(t, dir, "milestone/v1.0", "a.txt", "a\n", "feat: a")
	squashOnto(t, dir, "milestone/v1.0", "feat(squash): v1.0")

	if gitRefExists(dir, "refs/remotes/origin/main") {
		t.Fatal("precondition: this fixture must have no origin/main")
	}

	got, err := staleMilestoneBranches(dir, "main")
	if err != nil {
		t.Fatalf("no origin/main is a fallback, not an error: %v", err)
	}
	if _, ok := byName(got)["milestone/v1.0"]; !ok {
		t.Errorf("with no origin/main the local ref answers the question, got: %+v", got)
	}
}
