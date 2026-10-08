package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/changes"
	"github.com/Rivil/dross/internal/treefp"
	"github.com/Rivil/dross/internal/verify"
)

// stampVerdict records the tree as it is now in phase x's verify.toml —
// what a verify run would have written — and commits it as .dross bookkeeping.
func stampVerdict(t *testing.T, dir string) {
	t.Helper()
	m, err := treefp.MeasuredTree(dir)
	if err != nil {
		t.Fatal(err)
	}
	setRecordedTree(t, dir, m.Commit, m.Tree)
}

// setRecordedTree writes the given commit and tree into phase x's verify.toml
// and commits it.
func setRecordedTree(t *testing.T, dir, commit, tree string) {
	t.Helper()
	path := filepath.Join(dir, ".dross", "phases", "x", verify.VerifyFile)
	v, err := verify.LoadVerify(path)
	if err != nil || v == nil {
		t.Fatalf("load verify.toml: %v", err)
	}
	v.Verify.MeasuredCommit, v.Verify.MeasuredTree = commit, tree
	if err := v.Save(path); err != nil {
		t.Fatal(err)
	}
	gitCommit(t, dir, "chore(dross): record the measured tree")
}

// stampedShipFixture is shipFixture with a verdict that measured its tree.
func stampedShipFixture(t *testing.T) string {
	t.Helper()
	dir := shipFixture(t, "https://forge.example/me/p.git")
	stampVerdict(t, dir)
	return dir
}

func shipErr(t *testing.T, args ...string) error {
	t.Helper()
	return runCmd(t, Ship(), args...)
}

// TestShipRefusesAStalePass: a modified, an added and a deleted file after the
// run each make the pass stale; the refusal names all three and the way out,
// and ship writes nothing — not even the .dross bookkeeping it would commit.
func TestShipRefusesAStalePass(t *testing.T) {
	dir := stampedShipFixture(t)
	mustWrite(t, filepath.Join(dir, "src/tag.ts"), "export const tag = 2\n")
	mustWrite(t, filepath.Join(dir, "src/new.ts"), "export const n = 1\n")
	if err := os.Remove(filepath.Join(dir, "README.md")); err != nil {
		t.Fatal(err)
	}
	gitCommit(t, dir, "fix after verify")
	mustWrite(t, filepath.Join(dir, ".dross/phases/x/notes.md"), "bookkeeping\n")
	head := gitOutT(t, dir, "rev-parse", "HEAD")

	err := shipErr(t, "--no-push", "x")
	if err == nil {
		t.Fatal("ship accepted a pass whose tree changed since the run")
	}
	for _, want := range []string{"stale", "src/tag.ts", "src/new.ts", "README.md", "dross verify x", "--force-unverified"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal does not name %q:\n%v", want, err)
		}
	}
	if got := gitOutT(t, dir, "rev-parse", "HEAD"); got != head {
		t.Error("a stale refusal committed something")
	}
	if !strings.Contains(mustGit(t, dir, "status", "--porcelain"), "notes.md") {
		t.Error("a stale refusal auto-committed the .dross bookkeeping")
	}
}

// TestShipStaleCountsUncommittedEdits: an uncommitted edit is a change to the
// tree that would ship, refused as stale — not left to the dirty-tree check.
func TestShipStaleCountsUncommittedEdits(t *testing.T) {
	dir := stampedShipFixture(t)
	shipMockFlow(t, dir)
	stampVerdict(t, dir)
	mustWrite(t, filepath.Join(dir, "src/tag.ts"), "export const tag = 3 // uncommitted\n")

	err := shipErr(t, "x")
	if err == nil || !strings.Contains(err.Error(), "stale") || !strings.Contains(err.Error(), "src/tag.ts") {
		t.Errorf("an uncommitted edit = %v, want a stale refusal naming src/tag.ts", err)
	}
}

// TestShipStaleRefusalPushesNothing: through the full mock flow, a stale
// refusal leaves origin, the provider and the PR record untouched.
func TestShipStaleRefusalPushesNothing(t *testing.T) {
	dir := stampedShipFixture(t)
	cap, remoteDir := shipMockFlowRemote(t, dir)
	stampVerdict(t, dir)
	mustWrite(t, filepath.Join(dir, "src/tag.ts"), "export const tag = 4\n")
	gitCommit(t, dir, "fix after verify")

	if err := shipErr(t, "x"); err == nil {
		t.Fatal("a stale pass shipped")
	}
	if out, _ := gitOut(remoteDir, "show-ref", "--verify", "refs/heads/phase/x"); strings.TrimSpace(out) != "" {
		t.Errorf("phase/x reached origin on a stale refusal: %s", out)
	}
	if cap.posts != 0 {
		t.Errorf("POST /pulls ran %d time(s) on a stale refusal", cap.posts)
	}
	ch, err := changes.Load(changes.FilePath(filepath.Join(dir, ".dross"), "x"), "x")
	if err != nil {
		t.Fatal(err)
	}
	if ch.PR != 0 {
		t.Errorf("changes.json records PR %d after a stale refusal", ch.PR)
	}
}

// TestShipStaleRefusalLeavesDrossDirt: in the full flow too, the gate runs
// before ship's own .dross auto-commit.
func TestShipStaleRefusalLeavesDrossDirt(t *testing.T) {
	dir := stampedShipFixture(t)
	shipMockFlow(t, dir)
	stampVerdict(t, dir)
	mustWrite(t, filepath.Join(dir, "src/tag.ts"), "export const tag = 5\n")
	gitCommit(t, dir, "fix after verify")
	mustWrite(t, filepath.Join(dir, ".dross/phases/x/notes.md"), "bookkeeping\n")
	head := gitOutT(t, dir, "rev-parse", "HEAD")

	if err := shipErr(t, "x"); err == nil {
		t.Fatal("a stale pass shipped")
	}
	if got := gitOutT(t, dir, "rev-parse", "HEAD"); got != head {
		t.Error("ship committed before refusing a stale pass")
	}
	if !strings.Contains(mustGit(t, dir, "status", "--porcelain"), "notes.md") {
		t.Error("the .dross bookkeeping was auto-committed ahead of the stale refusal")
	}
}

// TestShipExemptChangesStayFresh: ship's own landmark merge into
// ARCHITECTURE.md and .dross bookkeeping commits leave the pass fresh.
func TestShipExemptChangesStayFresh(t *testing.T) {
	dir := stampedShipFixture(t)
	mustWrite(t, filepath.Join(dir, "ARCHITECTURE.md"), "# Architecture\n\nlandmarks merged at ship\n")
	gitCommit(t, dir, "docs: merge phase landmarks into ARCHITECTURE.md")
	mustWrite(t, filepath.Join(dir, ".dross/phases/x/notes.md"), "bookkeeping\n")
	gitCommit(t, dir, "chore(dross): bookkeeping")

	if err := shipErr(t, "--no-push", "x"); err != nil {
		t.Errorf("exempt changes made the pass stale: %v", err)
	}
}

// TestShipExemptLookalikesGoStale: only the root ARCHITECTURE.md and .dross/
// are exempt; files that merely look like them are measured.
func TestShipExemptLookalikesGoStale(t *testing.T) {
	for _, rel := range []string{"docs/ARCHITECTURE.md", ".drossrc"} {
		t.Run(rel, func(t *testing.T) {
			dir := stampedShipFixture(t)
			mustWrite(t, filepath.Join(dir, rel), "lookalike\n")
			gitCommit(t, dir, "add a lookalike")
			err := shipErr(t, "--no-push", "x")
			if err == nil || !strings.Contains(err.Error(), rel) {
				t.Errorf("%s = %v, want a stale refusal naming it", rel, err)
			}
		})
	}
}

// TestShipReRunAfterRecordCommitStaysFresh: ship's own PR-record commit is
// .dross bookkeeping, so the re-ship ship.md requires before merging passes.
func TestShipReRunAfterRecordCommitStaysFresh(t *testing.T) {
	dir := stampedShipFixture(t)
	shipMockFlow(t, dir)
	stampVerdict(t, dir)
	if err := shipErr(t, "x"); err != nil {
		t.Fatalf("first ship: %v", err)
	}
	if err := shipErr(t, "x"); err != nil {
		t.Errorf("re-ship after ship's own record commit: %v", err)
	}
}

// TestShipOffBranchRefusalBeatsStale: off phase/<id>, the branch refusal comes
// first — another branch's tree would name changes that are not this phase's.
func TestShipOffBranchRefusalBeatsStale(t *testing.T) {
	dir := stampedShipFixture(t)
	mustWrite(t, filepath.Join(dir, "src/tag.ts"), "export const tag = 6\n")
	gitCommit(t, dir, "fix after verify")
	mustGit(t, dir, "checkout", "-q", "-b", "elsewhere")

	err := shipErr(t, "--no-push", "x")
	if err == nil || !strings.Contains(err.Error(), "must be on phase/x") || strings.Contains(err.Error(), "stale") {
		t.Errorf("off-branch = %v, want the must-be-on-phase-branch refusal and no staleness list", err)
	}
}

// TestShipForceUnverifiedOverridesStale: the one override passes a stale
// verdict, and says so on stderr with the count.
func TestShipForceUnverifiedOverridesStale(t *testing.T) {
	dir := stampedShipFixture(t)
	mustWrite(t, filepath.Join(dir, "src/tag.ts"), "export const tag = 7\n")
	gitCommit(t, dir, "fix after verify")

	var err error
	stderr := captureStderr(t, func() { err = shipErr(t, "--no-push", "--force-unverified", "x") })
	if err != nil {
		t.Fatalf("--force-unverified on a stale pass: %v", err)
	}
	if !strings.Contains(stderr, "stale") || !strings.Contains(stderr, "1 file(s)") {
		t.Errorf("the override went silent or lost the count:\n%s", stderr)
	}
}

// TestShipLegacyVerifyWarnsAndProceeds: a verdict written before trees were
// kept ships with a warning on stderr — and under --json stdout is still one
// object.
func TestShipLegacyVerifyWarnsAndProceeds(t *testing.T) {
	dir := shipFixture(t, "https://forge.example/me/p.git")
	var err error
	stderr := captureStderr(t, func() { err = shipErr(t, "--no-push", "x") })
	if err != nil {
		t.Fatalf("a legacy verdict was refused: %v", err)
	}
	if !strings.Contains(stderr, "freshness unknown") {
		t.Errorf("no freshness-unknown warning on stderr:\n%s", stderr)
	}

	shipMockFlow(t, dir)
	var stdout string
	_ = captureStderr(t, func() {
		stdout = captureStdout(t, func() {
			if err := shipErr(t, "--json", "x"); err != nil {
				t.Fatalf("ship --json on a legacy verdict: %v", err)
			}
		})
	})
	var obj map[string]any
	dec := json.NewDecoder(strings.NewReader(stdout))
	if err := dec.Decode(&obj); err != nil {
		t.Fatalf("stdout is not a JSON object: %v\n%s", err, stdout)
	}
	if dec.More() {
		t.Errorf("stdout carries more than one JSON value:\n%s", stdout)
	}
}

// TestShipLegacyNeverCaptures: a verdict with no tree is judged without the
// tree being taken, so a capture that would fail cannot refuse it.
func TestShipLegacyNeverCaptures(t *testing.T) {
	shipFixture(t, "https://forge.example/me/p.git")
	prev := measuredTreeFn
	measuredTreeFn = func(string) (treefp.Measured, error) { return treefp.Measured{}, errors.New("must not be called") }
	t.Cleanup(func() { measuredTreeFn = prev })

	var err error
	stderr := captureStderr(t, func() { err = shipErr(t, "--no-push", "x") })
	if err != nil || !strings.Contains(stderr, "freshness unknown") {
		t.Errorf("legacy verdict with a failing capture = %v, stderr %q; want a warning and no refusal", err, stderr)
	}
}

// TestShipRefusesUnmeasurableTree: a recorded tree that cannot be compared
// because the current one cannot be taken refuses, naming why.
func TestShipRefusesUnmeasurableTree(t *testing.T) {
	stampedShipFixture(t)
	prev := measuredTreeFn
	measuredTreeFn = func(string) (treefp.Measured, error) { return treefp.Measured{}, errors.New("index.lock exists") }
	t.Cleanup(func() { measuredTreeFn = prev })

	err := shipErr(t, "--no-push", "x")
	if err == nil || !strings.Contains(err.Error(), "index.lock exists") {
		t.Errorf("a failed capture = %v, want a refusal naming it", err)
	}
}

// TestShipRefusesUnlistableTree: a well-formed recorded tree this clone does
// not have — pruned, or never fetched — refuses: it cannot be shown to be the
// tree being shipped.
func TestShipRefusesUnlistableTree(t *testing.T) {
	dir := shipFixture(t, "https://forge.example/me/p.git")
	setRecordedTree(t, dir, "", "0123456789abcdef0123456789abcdef01234567")

	err := shipErr(t, "--no-push", "x")
	if err == nil || !strings.Contains(err.Error(), "could not be listed") {
		t.Errorf("an absent recorded tree = %v, want a refusal saying the changes could not be listed", err)
	}
}

// TestShipRefusesMalformedTree: a recorded value that is not an object id is
// refused by field name and never handed to git.
func TestShipRefusesMalformedTree(t *testing.T) {
	dir := shipFixture(t, "https://forge.example/me/p.git")
	setRecordedTree(t, dir, "", "HEAD")

	err := shipErr(t, "--no-push", "x")
	if err == nil || !strings.Contains(err.Error(), "measured_tree") {
		t.Errorf("a malformed recorded tree = %v, want a refusal naming measured_tree", err)
	}
}

// TestShipStaleNamesEveryFile: the refusal lists every changed file, uncapped.
func TestShipStaleNamesEveryFile(t *testing.T) {
	dir := stampedShipFixture(t)
	var want []string
	for i := 0; i < 25; i++ {
		rel := fmt.Sprintf("src/gen%02d.ts", i)
		mustWrite(t, filepath.Join(dir, rel), "export const g = 1\n")
		want = append(want, rel)
	}
	gitCommit(t, dir, "generated after verify")

	err := shipErr(t, "--no-push", "x")
	if err == nil {
		t.Fatal("25 new files after the run did not stale the pass")
	}
	for _, rel := range want {
		if !strings.Contains(err.Error(), rel) {
			t.Errorf("the refusal dropped %s", rel)
		}
	}
}

// TestShipPrintBodyRefusesStale: --print-body sits behind the gate too.
func TestShipPrintBodyRefusesStale(t *testing.T) {
	dir := stampedShipFixture(t)
	mustWrite(t, filepath.Join(dir, "src/tag.ts"), "export const tag = 8\n")
	gitCommit(t, dir, "fix after verify")

	if err := shipErr(t, "--print-body", "x"); err == nil || !strings.Contains(err.Error(), "stale") {
		t.Errorf("--print-body on a stale pass = %v, want the stale refusal", err)
	}
}
