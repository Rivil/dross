package cmd

import (
	"crypto/sha256"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// treeHashes maps every file under dir to its sha256, so a before/after pair
// shows any write — a new file, a removed one or a changed byte.
func treeHashes(t *testing.T, dir string) map[string][32]byte {
	t.Helper()
	out := map[string][32]byte{}
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		out[path] = sha256.Sum256(b)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// reportLines are a close-out render's indented lines: one per item, ack and
// stale ack, plus each item's clear: line.
func reportLines(s string) []string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if strings.HasPrefix(l, "  ") {
			out = append(out, l)
		}
	}
	return out
}

// runCheck runs `milestone complete v0.9 --check` with extra flags and returns
// its stdout and error.
func runCheck(t *testing.T, extra ...string) (string, error) {
	t.Helper()
	var err error
	out := captureStdout(t, func() {
		err = runCmd(t, Milestone(), append([]string{"complete", "v0.9", "--check"}, extra...)...)
	})
	return out, err
}

// c-6: --check prints exactly what the refusal would, plus the acknowledged
// item, fails while anything is unresolved, and touches nothing — no file
// under .dross, no ref here or on origin, no fetch, no push, no PR.
func TestMilestoneCompleteCheckIsSideEffectFree(t *testing.T) {
	dir, cap, branch := milestonePushHeadFixture(t)
	seedEveryCloseoutKind(t, dir)
	if err := runCmd(t, Milestone(), "ack", "deferred:src#0", "v0.9", "--reason", "parked for v2.0"); err != nil {
		t.Fatalf("ack: %v", err)
	}
	mustWrite(t, filepath.Join(dir, "late.txt"), "committed locally, never pushed\n")
	mustGit(t, dir, "add", "late.txt")
	mustGit(t, dir, "commit", "-q", "-m", "late work on the milestone branch")
	local := mustGit(t, dir, "rev-parse", branch)

	refusal := runCmd(t, Milestone(), "complete", "v0.9")
	if refusal == nil {
		t.Fatal("fixture is not set up: complete opened the PR")
	}

	fetchHead := filepath.Join(dir, ".git", "FETCH_HEAD")
	if err := os.Remove(fetchHead); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	drossBefore := treeHashes(t, filepath.Join(dir, ".dross"))
	refsBefore := mustGit(t, dir, "for-each-ref")
	originBefore := mustGit(t, dir, "ls-remote", "origin", "refs/heads/"+branch)

	out, err := runCheck(t)
	if err == nil {
		t.Fatalf("--check exited 0 with every close-out kind unresolved:\n%s", out)
	}
	if got, want := reportLines(out), reportLines(refusal.Error()); !slices.Equal(got, want) {
		t.Errorf("--check item lines differ from the refusal's:\n--check:\n%s\nrefusal:\n%s",
			strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	for _, want := range []string{headUnverified, headNotDone, headSurvivor,
		"unv", "phase:built", "survivor:K", "acknowledged (1):", "deferred:src#0 — parked for v2.0"} {
		if !strings.Contains(out, want) {
			t.Errorf("--check output is missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, headDeferred) {
		t.Errorf("--check lists the acknowledged deferred item as unresolved:\n%s", out)
	}

	if cap.posts != 0 || cap.created != 0 {
		t.Errorf("--check reached the provider (posts=%d created=%d)", cap.posts, cap.created)
	}
	if after := treeHashes(t, filepath.Join(dir, ".dross")); !maps.Equal(after, drossBefore) {
		t.Errorf("--check wrote under .dross")
	}
	if after := mustGit(t, dir, "for-each-ref"); after != refsBefore {
		t.Errorf("--check moved refs:\nbefore:\n%s\nafter:\n%s", refsBefore, after)
	}
	if after := mustGit(t, dir, "ls-remote", "origin", "refs/heads/"+branch); after != originBefore {
		t.Errorf("--check moved origin %s: %q -> %q", branch, originBefore, after)
	}
	if strings.Contains(mustGit(t, dir, "ls-remote", "origin"), local) {
		t.Errorf("--check pushed the unpushed local commit %s", local)
	}
	if _, err := os.Stat(fetchHead); err == nil {
		t.Errorf("--check fetched: %s appeared", fetchHead)
	}
}

// --check needs no [remote]: a project with none still gets its report, never
// the "no [remote]" refusal the open path gives.
func TestMilestoneCompleteCheckNeedsNoRemote(t *testing.T) {
	dir, _ := setupMilestoneRepo(t)
	seedEveryCloseoutKind(t, dir)
	out, err := runCheck(t)
	if err == nil {
		t.Fatalf("--check exited 0 with every close-out kind unresolved:\n%s", out)
	}
	if strings.Contains(err.Error(), "[remote]") {
		t.Errorf("--check refused for a missing [remote]: %v", err)
	}
	for _, want := range []string{headUnverified, headNotDone, headDeferred, headSurvivor} {
		if !strings.Contains(out, want) {
			t.Errorf("--check output is missing %q:\n%s", want, out)
		}
	}
}

// --check previews the open; --finalize runs after the merge. Together they
// are refused before either runs: the milestone is not finalized.
func TestMilestoneCompleteCheckConflictsWithFinalize(t *testing.T) {
	dir, version := finalizeFixture(t, true, "active")
	refsBefore := mustGit(t, dir, "for-each-ref")

	err := runCmd(t, Milestone(), "complete", version, "--check", "--finalize")
	if err == nil {
		t.Fatal("--check --finalize was accepted")
	}
	if !strings.Contains(err.Error(), "--check") || !strings.Contains(err.Error(), "--finalize") {
		t.Errorf("refusal does not name both flags: %v", err)
	}
	if got := milestoneStatusOf(t, dir, version); got != "active" {
		t.Errorf("status = %q after a refused --check --finalize, want active", got)
	}
	if after := mustGit(t, dir, "for-each-ref"); after != refsBefore {
		t.Errorf("refs changed on a refused --check --finalize:\nbefore:\n%s\nafter:\n%s", refsBefore, after)
	}
}

// --check honours --force-unverified exactly as the open does: with only an
// unverified phase, the flag makes the check clean.
func TestMilestoneCompleteCheckHonoursForceUnverified(t *testing.T) {
	dir, _ := setupMilestoneRepo(t)
	setMilestonePhases(t, dir, "v0.9", []string{"unv"})
	ckPhase(t, filepath.Join(dir, ".dross"), "unv", "complete")

	out, err := runCheck(t)
	if err == nil || !strings.Contains(out, headUnverified) {
		t.Fatalf("fixture is not set up: --check without the flag should list unv as unverified (err=%v):\n%s", err, out)
	}
	out, err = runCheck(t, "--force-unverified")
	if err != nil {
		t.Fatalf("--check --force-unverified failed on an unverified-only milestone: %v\n%s", err, out)
	}
	if strings.Contains(out, headUnverified) {
		t.Errorf("--check --force-unverified still lists the unverified kind:\n%s", out)
	}
	if !strings.Contains(out, "nothing unresolved") {
		t.Errorf("--check --force-unverified output = %q, want nothing unresolved", out)
	}
}

// c-8: no milestone record, or one with no phases, has nothing to close out.
func TestMilestoneCompleteCheckEmpty(t *testing.T) {
	for name, seed := range map[string]func(t *testing.T, dir string){
		"no milestone record": func(*testing.T, string) {},
		"empty phases":        func(t *testing.T, dir string) { setMilestonePhases(t, dir, "v0.9", nil) },
	} {
		t.Run(name, func(t *testing.T) {
			dir, _ := setupMilestoneRepo(t)
			seed(t, dir)
			out, err := runCheck(t)
			if err != nil {
				t.Fatalf("--check: %v\n%s", err, out)
			}
			if !strings.Contains(out, "nothing unresolved") {
				t.Errorf("--check output = %q, want nothing unresolved", out)
			}
		})
	}
}

// c-9: after an ack, --check reports the same thing every time it runs.
func TestMilestoneCompleteCheckIsRepeatable(t *testing.T) {
	dir, _ := setupMilestoneRepo(t)
	seedEveryCloseoutKind(t, dir)
	if err := runCmd(t, Milestone(), "ack", "phase:built", "v0.9", "--reason", "moved to v2.0"); err != nil {
		t.Fatalf("ack: %v", err)
	}
	first, err1 := runCheck(t)
	second, err2 := runCheck(t)
	if err1 == nil || err2 == nil {
		t.Fatalf("--check exited 0 with items still unresolved (first=%v second=%v)", err1, err2)
	}
	if !strings.Contains(first, "phase:built — moved to v2.0") {
		t.Fatalf("fixture is not set up: the ack is not listed:\n%s", first)
	}
	if first != second {
		t.Errorf("--check is not repeatable:\nfirst:\n%s\nsecond:\n%s", first, second)
	}
	if err1.Error() != err2.Error() {
		t.Errorf("--check errors differ: %q vs %q", err1, err2)
	}
}

// c-11: an ack whose item has since resolved is listed as stale and does not
// block.
func TestMilestoneCompleteCheckListsStaleAcksAsNonBlocking(t *testing.T) {
	dir, _ := setupMilestoneRepo(t)
	root := filepath.Join(dir, ".dross")
	setMilestonePhases(t, dir, "v0.9", []string{"p"})
	ckVerify(t, root, "p", closeoutT0)
	if err := runCmd(t, Milestone(), "ack", "phase:p", "v0.9", "--reason", "parked"); err != nil {
		t.Fatalf("ack: %v", err)
	}
	ckPhase(t, root, "p", "complete")

	out, err := runCheck(t)
	if err != nil {
		t.Fatalf("a stale ack blocked --check: %v\n%s", err, out)
	}
	_, stale, ok := strings.Cut(out, "stale acknowledgements (1)")
	if !ok {
		t.Fatalf("--check has no stale-acks heading:\n%s", out)
	}
	if !strings.Contains(stale, "phase:p — parked") {
		t.Errorf("phase:p is not under the stale heading:\n%s", out)
	}
	if strings.Contains(out, "acknowledged (") {
		t.Errorf("the stale ack is listed as an applied acknowledgement:\n%s", out)
	}
}

// The flag usages say what the flags do now: --force-unverified skips one
// close-out kind, not the whole gate, and --check promises no side effects.
func TestMilestoneCompleteFlagUsage(t *testing.T) {
	c := milestoneComplete()
	force := c.Flags().Lookup("force-unverified")
	if force == nil {
		t.Fatal("no --force-unverified flag")
	}
	if strings.Contains(force.Usage, "every phase in the milestone must verify pass") {
		t.Errorf("--force-unverified usage still claims it skips the whole verify gate: %q", force.Usage)
	}
	for _, want := range []string{"only", "unverified", "ack"} {
		if !strings.Contains(force.Usage, want) {
			t.Errorf("--force-unverified usage is missing %q: %q", want, force.Usage)
		}
	}
	check := c.Flags().Lookup("check")
	if check == nil {
		t.Fatal("no --check flag")
	}
	for _, want := range []string{"non-zero", "writes", "pushes"} {
		if !strings.Contains(check.Usage, want) {
			t.Errorf("--check usage is missing %q: %q", want, check.Usage)
		}
	}
}
