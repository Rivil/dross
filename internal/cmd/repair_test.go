package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/state"
)

// TestRepairCommandRegistered guards reachability: Repair() must be wired
// into the real root tree in cmd/dross/main.go, or `dross repair` would 404.
func TestRepairCommandRegistered(t *testing.T) {
	root := repoRootFromTest(t)
	b, err := os.ReadFile(filepath.Join(root, "cmd", "dross", "main.go"))
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	if !strings.Contains(string(b), "cmd.Repair()") {
		t.Error("cmd.Repair() is not registered in cmd/dross/main.go root.AddCommand — `dross repair` would be unreachable")
	}
}

// repairFixture builds a healthy dross repo checked out on phase/x, with
// state.json's current_phase already agreeing with the branch — a clean
// baseline every repair test starts from and may clobber further.
func repairFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	remoteDir := t.TempDir()
	mustGit(t, remoteDir, "init", "-q", "--bare", "-b", "main")
	gitInit(t, dir, remoteDir)
	chdir(t, dir)

	if err := runCmd(t, Init()); err != nil {
		t.Fatalf("init: %v", err)
	}
	mustGit(t, dir, "add", ".")
	mustGit(t, dir, "commit", "-q", "-m", "chore: baseline")
	mustGit(t, dir, "push", "-q", "-u", "origin", "main")

	mustGit(t, dir, "checkout", "-q", "-b", "phase/x")
	root := filepath.Join(dir, ".dross")
	mustWrite(t, filepath.Join(root, "phases", "x", "spec.toml"), "[phase]\nid = \"x\"\ntitle = \"X\"\n")
	mustGit(t, dir, "add", ".")
	mustGit(t, dir, "commit", "-q", "-m", "chore: scaffold x")

	statePath := filepath.Join(root, state.File)
	st, err := state.Load(statePath)
	if err != nil {
		t.Fatal(err)
	}
	st.CurrentPhase = "x"
	if err := st.Save(statePath); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestRepairNoFindingsReportsNothingToRepair(t *testing.T) {
	dir := repairFixture(t)

	out := captureStdout(t, func() {
		if err := runCmd(t, Repair()); err != nil {
			t.Fatalf("repair: %v", err)
		}
	})
	if strings.TrimSpace(out) != "nothing to repair" {
		t.Fatalf("output = %q, want \"nothing to repair\"", out)
	}
	if status := mustGit(t, dir, "status", "--porcelain"); status != "" {
		t.Fatalf("expected no writes on a healthy tree, got dirty status:\n%s", status)
	}
}

func TestRepairDryRunReportsButDoesNotRestore(t *testing.T) {
	dir := repairFixture(t)
	specPath := filepath.Join(dir, ".dross", "phases", "x", "spec.toml")
	mustWrite(t, specPath, "clobbered\n")

	out := captureStdout(t, func() {
		if err := runCmd(t, Repair()); err != nil {
			t.Fatalf("repair: %v", err)
		}
	})
	if !strings.Contains(out, ".dross/phases/x/spec.toml") {
		t.Fatalf("dry-run output should name the clobbered file:\n%s", out)
	}
	if !strings.Contains(out, "dry-run") {
		t.Fatalf("dry-run output should say so:\n%s", out)
	}
	body := mustRead(t, specPath)
	if body != "clobbered\n" {
		t.Fatalf("dry-run must not restore the file; got %q", body)
	}
}

// TestCheckStaleStateMissingIsStale covers repair.go's loadErr!=nil branch:
// a statePath that doesn't exist on disk at all is always stale, regardless
// of branch.
func TestCheckStaleStateMissingIsStale(t *testing.T) {
	dir := repairFixture(t)
	root := filepath.Join(dir, ".dross")
	missingPath := filepath.Join(root, "does-not-exist.json")

	stale, reconstructed, err := checkStaleState(dir, root, missingPath, "main")
	if err != nil {
		t.Fatalf("checkStaleState: %v", err)
	}
	if !stale {
		t.Fatal("expected stale=true for a missing state.json")
	}
	if reconstructed == nil {
		t.Fatal("expected a reconstructed state on stale=true")
	}
	if reconstructed.CurrentPhase != "x" {
		t.Errorf("reconstructed.CurrentPhase = %q, want x (from checked-out branch)", reconstructed.CurrentPhase)
	}
}

// TestCheckStaleStateBranchMismatchIsStale covers the branchPhase-mismatch
// half of repair.go:133 — state.json loads cleanly but disagrees with the
// checked-out phase/<id> branch.
func TestCheckStaleStateBranchMismatchIsStale(t *testing.T) {
	dir := repairFixture(t)
	root := filepath.Join(dir, ".dross")
	statePath := filepath.Join(root, state.File)

	st, err := state.Load(statePath)
	if err != nil {
		t.Fatal(err)
	}
	st.CurrentPhase = "some-other-phase"
	if err := st.Save(statePath); err != nil {
		t.Fatal(err)
	}

	stale, reconstructed, err := checkStaleState(dir, root, statePath, "main")
	if err != nil {
		t.Fatalf("checkStaleState: %v", err)
	}
	if !stale {
		t.Fatal("expected stale=true when state.json's current_phase disagrees with the checked-out branch")
	}
	if reconstructed.CurrentPhase != "x" {
		t.Errorf("reconstructed.CurrentPhase = %q, want x", reconstructed.CurrentPhase)
	}
}

// TestCheckStaleStateNoBranchPhaseNotStale covers the other operand of
// repair.go:133's compound condition: when HEAD isn't on a phase/<id>
// branch, a disagreeing current_phase carries no staleness signal by itself.
func TestCheckStaleStateNoBranchPhaseNotStale(t *testing.T) {
	dir := repairFixture(t)
	mustGit(t, dir, "checkout", "-q", "main")
	root := filepath.Join(dir, ".dross")
	statePath := filepath.Join(root, state.File)

	stale, reconstructed, err := checkStaleState(dir, root, statePath, "main")
	if err != nil {
		t.Fatalf("checkStaleState: %v", err)
	}
	if stale {
		t.Fatal("expected stale=false — not on a phase/<id> branch, so no mismatch signal")
	}
	if reconstructed != nil {
		t.Errorf("expected no reconstructed state when not stale, got %+v", reconstructed)
	}
}

// TestCheckStaleStateReconstructErrorPropagates covers repair.go:140 — when
// reconstructState itself fails (here, mainBranch doesn't exist so `git log`
// errors), checkStaleState reports stale=false and surfaces the error rather
// than returning a half-built reconstruction.
func TestCheckStaleStateReconstructErrorPropagates(t *testing.T) {
	dir := repairFixture(t)
	root := filepath.Join(dir, ".dross")
	missingPath := filepath.Join(root, "does-not-exist.json")

	stale, reconstructed, err := checkStaleState(dir, root, missingPath, "no-such-branch")
	if err == nil {
		t.Fatal("expected an error when mainBranch doesn't exist")
	}
	if stale {
		t.Error("expected stale=false alongside a propagated error")
	}
	if reconstructed != nil {
		t.Error("expected no reconstructed state alongside a propagated error")
	}
}

// TestRepairDryRunReportsStaleState covers repair.go:161's report line and
// confirms dry-run leaves state.json untouched.
func TestRepairDryRunReportsStaleState(t *testing.T) {
	dir := repairFixture(t)
	root := filepath.Join(dir, ".dross")
	statePath := filepath.Join(root, state.File)

	st, err := state.Load(statePath)
	if err != nil {
		t.Fatal(err)
	}
	st.CurrentPhase = "some-other-phase"
	if err := st.Save(statePath); err != nil {
		t.Fatal(err)
	}

	out := captureStdout(t, func() {
		if err := runCmd(t, Repair()); err != nil {
			t.Fatalf("repair: %v", err)
		}
	})
	if !strings.Contains(out, state.File) || !strings.Contains(out, "missing or stale") {
		t.Fatalf("dry-run output should flag the stale state.json:\n%s", out)
	}
	if !strings.Contains(out, "dry-run") {
		t.Fatalf("dry-run output should say so:\n%s", out)
	}

	after, err := state.Load(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if after.CurrentPhase != "some-other-phase" {
		t.Fatalf("dry-run must not write state.json; CurrentPhase = %q, want unchanged", after.CurrentPhase)
	}
}

// TestRepairApplyReconstructsStaleState covers repair.go:85's save path: a
// stale state.json is reconstructed and written to disk under --apply. It's
// gitignored so this never stages a diff — the "nothing further to commit"
// branch, distinct from TestRepairApplyRestoresAndCommits which drives an
// actual commit via a tracked-file restore.
func TestRepairApplyReconstructsStaleState(t *testing.T) {
	dir := repairFixture(t)
	root := filepath.Join(dir, ".dross")
	statePath := filepath.Join(root, state.File)

	st, err := state.Load(statePath)
	if err != nil {
		t.Fatal(err)
	}
	st.CurrentPhase = "some-other-phase"
	if err := st.Save(statePath); err != nil {
		t.Fatal(err)
	}

	out := captureStdout(t, func() {
		if err := runCmd(t, Repair(), "--apply"); err != nil {
			t.Fatalf("repair --apply: %v", err)
		}
	})
	if !strings.Contains(out, "nothing further to commit") {
		t.Fatalf("apply output should note state.json's gitignored save produced no diff:\n%s", out)
	}

	after, err := state.Load(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if after.CurrentPhase != "x" {
		t.Fatalf("state.json CurrentPhase = %q after --apply, want x (reconstructed from branch)", after.CurrentPhase)
	}
}

// TestRepairApplyRestoresAndCommits exercises a clobbered tracked file
// together with a phase dir origin knows about but this branch never had.
// Restoring the clobbered file alone can never produce a commit-worthy diff
// — it only brings the working tree back to what HEAD already held — so the
// commit half of this test is driven by the origin-sourced phase-dir
// restore (t-3), same as `dross repair --apply` would combine in practice.
func TestRepairApplyRestoresAndCommits(t *testing.T) {
	dir := repairFixture(t)

	// y was pushed to origin/main after phase/x branched off, so phase/x's
	// own HEAD never had it — restoring it from origin/main is a genuine
	// addition relative to local HEAD, not a no-op.
	mustGit(t, dir, "checkout", "-q", "main")
	root := filepath.Join(dir, ".dross")
	mustWrite(t, filepath.Join(root, "phases", "y", "spec.toml"), "[phase]\nid = \"y\"\n")
	mustGit(t, dir, "add", ".")
	mustGit(t, dir, "commit", "-q", "-m", "chore: scaffold y")
	mustGit(t, dir, "push", "-q", "origin", "main")
	mustGit(t, dir, "checkout", "-q", "phase/x")

	specPath := filepath.Join(root, "phases", "x", "spec.toml")
	mustWrite(t, specPath, "clobbered\n")
	beforeHead := mustGit(t, dir, "rev-parse", "HEAD")

	out := captureStdout(t, func() {
		if err := runCmd(t, Repair(), "--apply"); err != nil {
			t.Fatalf("repair --apply: %v", err)
		}
	})
	if !strings.Contains(out, "repaired and committed") {
		t.Fatalf("apply output should confirm the commit:\n%s", out)
	}
	body := mustRead(t, specPath)
	if body != "[phase]\nid = \"x\"\ntitle = \"X\"\n" {
		t.Fatalf("clobbered file restored content = %q, want original", body)
	}
	if _, err := os.Stat(filepath.Join(root, "phases", "y", "spec.toml")); err != nil {
		t.Fatalf("expected phase dir y to be restored from origin/main: %v", err)
	}
	afterHead := mustGit(t, dir, "rev-parse", "HEAD")
	if afterHead == beforeHead {
		t.Fatal("expected a new commit after --apply, HEAD unchanged")
	}
	if status := mustGit(t, dir, "status", "--porcelain"); status != "" {
		t.Fatalf("expected clean tree after --apply commit, got:\n%s", status)
	}
}
