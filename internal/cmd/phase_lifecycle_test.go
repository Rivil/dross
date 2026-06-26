package cmd

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/milestone"
	"github.com/Rivil/dross/internal/phase"
	"github.com/Rivil/dross/internal/state"
)

// snapshotPhases fingerprints every phase DIRECTORY under a .dross root: for
// each slug it hashes the sorted (relpath, length, bytes) of every file in
// phases/<slug>/. It deliberately does NOT fold in the milestone array slot —
// move/insert legitimately reorder the array, so array order is asserted
// explicitly per verb (via milestone.Load), while this proves the on-disk phase
// content is byte-for-byte untouched. Returns slug → hex sha256.
func snapshotPhases(t *testing.T, drossRoot string) map[string]string {
	t.Helper()
	slugs, err := phase.List(drossRoot)
	if err != nil {
		t.Fatalf("snapshotPhases: list phases: %v", err)
	}

	out := make(map[string]string, len(slugs))
	for _, slug := range slugs {
		h := sha256.New()
		dir := filepath.Join(drossRoot, "phases", slug)
		var files []string
		_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !d.IsDir() {
				files = append(files, p)
			}
			return nil
		})
		sort.Strings(files)
		for _, f := range files {
			rel, _ := filepath.Rel(dir, f)
			b, _ := os.ReadFile(f)
			fmt.Fprintf(h, "%s\x00%d\x00", rel, len(b))
			h.Write(b)
		}
		out[slug] = hex.EncodeToString(h.Sum(nil))
	}
	return out
}

// diffSnapshots returns the sorted slugs whose fingerprint changed, appeared, or
// disappeared between before and after, ignoring any slug in except. Pure, so it
// is directly testable without a mock *testing.T.
func diffSnapshots(before, after map[string]string, except ...string) []string {
	skip := make(map[string]bool, len(except))
	for _, e := range except {
		skip[e] = true
	}
	changed := map[string]bool{}
	for slug, h := range before {
		if !skip[slug] && after[slug] != h {
			changed[slug] = true
		}
	}
	for slug := range after {
		if skip[slug] {
			continue
		}
		if _, ok := before[slug]; !ok {
			changed[slug] = true
		}
	}
	out := make([]string, 0, len(changed))
	for s := range changed {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

// assertUntouched fails the test if any phase outside except changed between two
// snapshots — the byte-for-byte guarantee recurring across c-1/c-2/c-3.
func assertUntouched(t *testing.T, before, after map[string]string, except ...string) {
	t.Helper()
	if changed := diffSnapshots(before, after, except...); len(changed) > 0 {
		t.Errorf("phases unexpectedly changed (byte-for-byte): %v (excepted: %v)", changed, except)
	}
}

// TestSnapshotHarnessSelfCheck proves the harness is not vacuous: a one-byte
// edit to a bystander spec must flip its fingerprint AND be reported by
// diffSnapshots, while excepting that slug suppresses the report.
func TestSnapshotHarnessSelfCheck(t *testing.T) {
	dir := setupDeferredFixture(t)
	root := filepath.Join(dir, ".dross")

	before := snapshotPhases(t, root)

	specPath := filepath.Join(root, "phases", "beta", "spec.toml")
	b, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(specPath, append(b, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}

	after := snapshotPhases(t, root)

	if before["beta"] == after["beta"] {
		t.Error("snapshotPhases is blind: a one-byte change to beta/spec.toml did not flip its fingerprint")
	}
	if changed := diffSnapshots(before, after); !slices.Contains(changed, "beta") {
		t.Errorf("diffSnapshots missed the bystander change: got %v, want it to include beta", changed)
	}
	if rest := diffSnapshots(before, after, "beta"); len(rest) != 0 {
		t.Errorf("only beta changed, but excepting beta still reports: %v", rest)
	}
}

// setupLifecycleFixture builds a project whose current milestone v1 holds three
// phases [p1,p2,p3], each with a spec.toml. No git — so refuseIfShipped is a
// no-op and lifecycle verbs run on the array alone.
func setupLifecycleFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	chdir(t, dir)
	if err := runCmd(t, Init()); err != nil {
		t.Fatal(err)
	}
	mustRunSet(t, "project.name", "test-app")
	mustRunSet(t, "runtime.mode", "native")
	root := filepath.Join(dir, ".dross")
	mustWrite(t, filepath.Join(root, "milestones", "v1.toml"),
		"phases = [\"p1\", \"p2\", \"p3\"]\n\n[milestone]\n  version = \"v1\"\n  title = \"M\"\n")
	for _, slug := range []string{"p1", "p2", "p3"} {
		mustWrite(t, filepath.Join(root, "phases", slug, "spec.toml"),
			"[phase]\n  id = \""+slug+"\"\n  title = \""+slug+"\"\n\n[[criteria]]\n  id = \"c-1\"\n  text = \"x\"\n")
	}
	sPath := filepath.Join(root, state.File)
	s, err := state.Load(sPath)
	if err != nil {
		t.Fatal(err)
	}
	s.CurrentMilestone = "v1"
	if err := s.Save(sPath); err != nil {
		t.Fatal(err)
	}
	return dir
}

func milestonePhases(t *testing.T, root, version string) []string {
	t.Helper()
	m, err := milestone.Load(filepath.Join(root, "milestones", version+".toml"))
	if err != nil {
		t.Fatal(err)
	}
	return m.Phases
}

func TestPhaseMove(t *testing.T) {
	dir := setupLifecycleFixture(t)
	root := filepath.Join(dir, ".dross")
	before := snapshotPhases(t, root)

	if err := runCmd(t, Phase(), "move", "p3", "--after", "p1"); err != nil {
		t.Fatalf("move p3 --after p1: %v", err)
	}
	if got, want := milestonePhases(t, root, "v1"), []string{"p1", "p3", "p2"}; !reflect.DeepEqual(got, want) {
		t.Errorf("after move: array = %v, want %v", got, want)
	}
	// Only the array changed — every phase directory is byte-for-byte untouched.
	assertUntouched(t, before, snapshotPhases(t, root))

	var num string
	if err := runCmdCapturing(t, &num, Phase(), "number", "p3"); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(num) != "2" {
		t.Errorf("phase number p3 after move = %q, want 2", num)
	}
	if err := runCmd(t, Validate()); err != nil {
		t.Errorf("validate after move: %v", err)
	}
}

func TestPhaseMoveErrors(t *testing.T) {
	setupLifecycleFixture(t)

	if err := runCmd(t, Phase(), "move", "p2", "--after", "p1", "--before", "p3"); err == nil {
		t.Error("both --after and --before should error")
	}
	if err := runCmd(t, Phase(), "move", "p2"); err == nil {
		t.Error("neither --after nor --before should error")
	}
	if err := runCmd(t, Phase(), "move", "p2", "--after", "nonexistent"); err == nil {
		t.Error("an anchor not in the milestone should error")
	}
	if err := runCmd(t, Phase(), "move", "ghost", "--after", "p1"); err == nil {
		t.Error("moving a phase not in the milestone should error")
	}
}

func TestPhaseMoveNoOp(t *testing.T) {
	dir := setupLifecycleFixture(t)
	root := filepath.Join(dir, ".dross")
	mPath := filepath.Join(root, "milestones", "v1.toml")
	beforeBytes := mustRead(t, mPath)

	// p2 already sits immediately after p1 — a genuine no-op.
	var out string
	if err := runCmdCapturing(t, &out, Phase(), "move", "p2", "--after", "p1"); err != nil {
		t.Fatalf("no-op move: %v", err)
	}
	if !strings.Contains(out, "already in place") {
		t.Errorf("no-op move should report 'already in place', got %q", out)
	}
	if after := mustRead(t, mPath); after != beforeBytes {
		t.Errorf("no-op move rewrote the milestone .toml")
	}
}

func TestRefuseIfShipped(t *testing.T) {
	repo := t.TempDir()
	remote := t.TempDir()
	mustGit(t, remote, "init", "-q", "--bare", "-b", "main")
	gitInit(t, repo, remote)
	mustWrite(t, filepath.Join(repo, "README.md"), "x\n")
	mustGit(t, repo, "add", "README.md")
	mustGit(t, repo, "commit", "-q", "-m", "init")
	mustGit(t, repo, "push", "-q", "-u", "origin", "main")

	// Ship a phase: push its branch to origin (the open-PR window).
	mustGit(t, repo, "checkout", "-q", "-b", "phase/shipped")
	mustGit(t, repo, "push", "-q", "-u", "origin", "phase/shipped")
	mustGit(t, repo, "checkout", "-q", "main")

	if err := refuseIfShipped(repo, "shipped"); err == nil {
		t.Error("refuseIfShipped should block a phase with a live origin branch")
	}
	if err := refuseIfShipped(repo, "planning"); err != nil {
		t.Errorf("refuseIfShipped should allow a phase with no origin branch: %v", err)
	}
}

func TestPhaseInsert(t *testing.T) {
	dir := setupLifecycleFixture(t)
	root := filepath.Join(dir, ".dross")
	before := snapshotPhases(t, root)

	if err := runCmd(t, Phase(), "insert", "New Phase", "--after", "p1"); err != nil {
		t.Fatalf("insert: %v", err)
	}
	if got, want := milestonePhases(t, root, "v1"), []string{"p1", "new-phase", "p2", "p3"}; !reflect.DeepEqual(got, want) {
		t.Errorf("after insert: array = %v, want %v", got, want)
	}
	if !isDir(filepath.Join(root, "phases", "new-phase")) {
		t.Error("insert did not create phases/new-phase")
	}
	// Siblings byte-for-byte untouched (only the new phase is excepted).
	assertUntouched(t, before, snapshotPhases(t, root), "new-phase")

	var num string
	if err := runCmdCapturing(t, &num, Phase(), "number", "p2"); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(num) != "3" {
		t.Errorf("phase number p2 after insert = %q, want 3 (shifted +1)", num)
	}
	if err := runCmd(t, Validate()); err != nil {
		t.Errorf("validate after insert: %v", err)
	}
}

func TestPhaseInsertCollisionRefused(t *testing.T) {
	dir := setupLifecycleFixture(t)
	root := filepath.Join(dir, ".dross")
	before := snapshotPhases(t, root)

	// "p2" slugs to an existing phase — must refuse (no auto-suffix) and leave
	// no stray state.
	if err := runCmd(t, Phase(), "insert", "p2", "--after", "p1"); err == nil {
		t.Fatal("inserting a colliding slug should error (no auto-suffix)")
	}
	if isDir(filepath.Join(root, "phases", "p2-2")) {
		t.Error("collision left a stray suffixed directory")
	}
	if got, want := milestonePhases(t, root, "v1"), []string{"p1", "p2", "p3"}; !reflect.DeepEqual(got, want) {
		t.Errorf("collision mutated the array: %v", got)
	}
	assertUntouched(t, before, snapshotPhases(t, root))
}

func TestPhaseInsertErrors(t *testing.T) {
	setupLifecycleFixture(t)

	if err := runCmd(t, Phase(), "insert", "X", "--after", "p1", "--before", "p2"); err == nil {
		t.Error("both --after and --before should error")
	}
	if err := runCmd(t, Phase(), "insert", "X"); err == nil {
		t.Error("neither anchor flag should error")
	}
	if err := runCmd(t, Phase(), "insert", "X", "--after", "nonexistent"); err == nil {
		t.Error("an anchor not in the milestone should error")
	}
}

func TestPhaseRename(t *testing.T) {
	dir := setupLifecycleFixture(t)
	root := filepath.Join(dir, ".dross")
	before := snapshotPhases(t, root)

	if err := runCmd(t, Phase(), "rename", "p2", "beta"); err != nil {
		t.Fatalf("rename p2 beta: %v", err)
	}
	// dir + array + spec.id all move.
	if isDir(filepath.Join(root, "phases", "p2")) {
		t.Error("phases/p2 still exists after rename")
	}
	if !isDir(filepath.Join(root, "phases", "beta")) {
		t.Error("phases/beta was not created")
	}
	if got, want := milestonePhases(t, root, "v1"), []string{"p1", "beta", "p3"}; !reflect.DeepEqual(got, want) {
		t.Errorf("array after rename = %v, want %v (entry swapped in place)", got, want)
	}
	spec, err := phase.LoadSpec(filepath.Join(root, "phases", "beta", "spec.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if spec.Phase.ID != "beta" {
		t.Errorf("spec.phase.id = %q, want beta", spec.Phase.ID)
	}
	// Siblings byte-for-byte untouched (only old/new excepted).
	assertUntouched(t, before, snapshotPhases(t, root), "p2", "beta")
	if err := runCmd(t, Validate()); err != nil {
		t.Errorf("validate after rename: %v", err)
	}
}

func TestPhaseRenameRepointsDeferred(t *testing.T) {
	dir := setupLifecycleFixture(t)
	root := filepath.Join(dir, ".dross")
	// p1 defers two ideas: one routed to p2 (renamed), one to p3 (must stay).
	mustWrite(t, filepath.Join(root, "phases", "p1", "spec.toml"),
		"[phase]\n  id = \"p1\"\n  title = \"p1\"\n\n[[criteria]]\n  id = \"c-1\"\n  text = \"x\"\n\n[[deferred]]\n  text = \"to p2\"\n  target = \"p2\"\n\n[[deferred]]\n  text = \"to p3\"\n  target = \"p3\"\n")

	if err := runCmd(t, Phase(), "rename", "p2", "beta"); err != nil {
		t.Fatalf("rename: %v", err)
	}
	spec, err := phase.LoadSpec(filepath.Join(root, "phases", "p1", "spec.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if spec.Deferred[0].Target != "beta" {
		t.Errorf("deferred target p2 not re-pointed: got %q, want beta", spec.Deferred[0].Target)
	}
	if spec.Deferred[1].Target != "p3" {
		t.Errorf("deferred target p3 wrongly rewritten: got %q, want p3 (other targets must stay)", spec.Deferred[1].Target)
	}
}

func TestPhaseRenameNoPartialMoveOnCollision(t *testing.T) {
	dir := setupLifecycleFixture(t)
	root := filepath.Join(dir, ".dross")

	// p3 already exists — the target-exists check must fire BEFORE the dir move.
	if err := runCmd(t, Phase(), "rename", "p2", "p3"); err == nil {
		t.Fatal("renaming onto an existing slug should error")
	}
	if !isDir(filepath.Join(root, "phases", "p2")) {
		t.Error("collision left phases/p2 already moved — partial rename")
	}
	if !isDir(filepath.Join(root, "phases", "p3")) {
		t.Error("collision clobbered phases/p3")
	}
}

func TestPhaseRenameNoOpAndCurrentPhase(t *testing.T) {
	dir := setupLifecycleFixture(t)
	root := filepath.Join(dir, ".dross")
	sPath := filepath.Join(root, state.File)

	// No-op: rename to the same slug succeeds quietly and writes nothing.
	var out string
	if err := runCmdCapturing(t, &out, Phase(), "rename", "p2", "p2"); err != nil {
		t.Fatalf("self-rename: %v", err)
	}
	if !strings.Contains(out, "nothing to do") {
		t.Errorf("self-rename should be a quiet no-op, got %q", out)
	}

	// current_phase follows a real rename.
	s, _ := state.Load(sPath)
	s.CurrentPhase = "p2"
	if err := s.Save(sPath); err != nil {
		t.Fatal(err)
	}
	if err := runCmd(t, Phase(), "rename", "p2", "beta"); err != nil {
		t.Fatal(err)
	}
	s2, _ := state.Load(sPath)
	if s2.CurrentPhase != "beta" {
		t.Errorf("current_phase after rename = %q, want beta", s2.CurrentPhase)
	}
}

func TestPhaseRenameRenamesLocalBranch(t *testing.T) {
	dir := setupLifecycleFixture(t)
	remote := t.TempDir()
	mustGit(t, remote, "init", "-q", "--bare", "-b", "main")
	gitInit(t, dir, remote)
	mustGit(t, dir, "add", "-A")
	mustGit(t, dir, "commit", "-q", "-m", "init")
	mustGit(t, dir, "checkout", "-q", "-b", "phase/p2")
	mustGit(t, dir, "checkout", "-q", "main")

	if err := runCmd(t, Phase(), "rename", "p2", "beta"); err != nil {
		t.Fatalf("rename: %v", err)
	}
	branches := mustGit(t, dir, "branch", "--format=%(refname:short)")
	if strings.Contains(branches, "phase/p2") {
		t.Errorf("old branch phase/p2 still present: %q", branches)
	}
	if !strings.Contains(branches, "phase/beta") {
		t.Errorf("new branch phase/beta missing: %q", branches)
	}
}
