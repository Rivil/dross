package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Rivil/dross/internal/localstore"
	"github.com/Rivil/dross/internal/mutation"
	"github.com/Rivil/dross/internal/remote"
	"github.com/Rivil/dross/internal/treefp"
	"github.com/Rivil/dross/internal/verify"
)

// detachTreeRepo is a git repo ready to dispatch phase id detached, with the
// mutation tool's report directory ignored the way a configured repo has it —
// the fetched reports must not read as a change to the measured tree.
func detachTreeRepo(t *testing.T, id string) string {
	t.Helper()
	dir := detachCmdRepo(t, id, mutationTuning{Target: detachTarget()})
	writeScopeFile(t, dir, ".gitignore", "/reports/\n")
	mustGit(t, dir, "add", ".gitignore")
	mustGit(t, dir, "commit", "-qm", "ignore tool reports")
	return dir
}

// stubFinishedRun makes the host report the run finished and the fetch drop
// the payload where Collect reads it — collectRepo's stand-ins.
func stubFinishedRun(t *testing.T) {
	t.Helper()
	stubStatus(t, remote.RunStatus{DirExists: true, State: "finished", HasExit: true, ExitCode: 0}, nil)
	orig := detachFetchReports
	t.Cleanup(func() { detachFetchReports = orig })
	detachFetchReports = func(_ remote.Target, localRoot string, steps []mutation.PackageStep) error {
		for _, s := range steps {
			p := filepath.Join(localRoot, s.ReportRel)
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				return err
			}
			if err := os.WriteFile(p, []byte(collectPayload), 0o644); err != nil {
				return err
			}
		}
		return nil
	}
}

func dispatchAndRecord(t *testing.T, dir, id string) *localstore.DetachedRun {
	t.Helper()
	if err := runCmd(t, Verify(), id, "--detach"); err != nil {
		t.Fatalf("dross verify --detach: %v", err)
	}
	got, err := findDetachedRun(filepath.Join(dir, RootDirName), dir, id)
	if err != nil || got == nil {
		t.Fatalf("no record after dispatch: %+v, %v", got, err)
	}
	return got
}

func collectCapturing(t *testing.T, id string) string {
	t.Helper()
	return captureStdout(t, func() {
		if err := collectDetached(id); err != nil {
			t.Fatalf("collect: %v", err)
		}
	})
}

// TestDispatchMeasuresBeforeThePush: an edit that races the push is not
// vouched for — the record keeps the pre-push tree, and collect writes that
// tree and names the file.
func TestDispatchMeasuresBeforeThePush(t *testing.T) {
	const id = "racepush"
	dir := detachTreeRepo(t, id)
	before, err := treefp.MeasuredTree(dir)
	if err != nil {
		t.Fatal(err)
	}
	rec := &detachRecorder{onSync: func() {
		writeScopeFile(t, dir, "a.go", "package x\n\nfunc A() bool { return 1 >= 0 } // edited during the push\n")
	}}
	rec.install(t)

	got := dispatchAndRecord(t, dir, id)
	if got.MeasuredTree != before.Tree {
		t.Errorf("the record carries %s, want the pre-push tree %s", got.MeasuredTree, before.Tree)
	}
	stubFinishedRun(t)
	out := collectCapturing(t, id)
	v := loadVerifyToml(t, dir, id)
	if v.Verify.MeasuredTree != before.Tree {
		t.Errorf("verify.toml records %s, want the pre-push tree %s", v.Verify.MeasuredTree, before.Tree)
	}
	if !strings.Contains(out, "changed since the tree was measured") || !strings.Contains(out, "a.go") {
		t.Errorf("collect did not name the file edited during the push:\n%s", out)
	}
}

// TestCollectRecordsTheDispatchTreeNotTheCollectTree is the locked
// detached_baseline: collect stamps the tree the dispatch pushed, names the
// file changed since, and the verdict reads stale against today's tree.
func TestCollectRecordsTheDispatchTreeNotTheCollectTree(t *testing.T) {
	const id = "collecttree"
	dir := detachTreeRepo(t, id)
	(&detachRecorder{}).install(t)
	got := dispatchAndRecord(t, dir, id)

	writeScopeFile(t, dir, "a.go", "package x\n\nfunc A() bool { return 2 > 1 } // after dispatch\n")
	stubFinishedRun(t)
	out := collectCapturing(t, id)

	now, err := treefp.MeasuredTree(dir)
	if err != nil {
		t.Fatal(err)
	}
	v := loadVerifyToml(t, dir, id)
	if v.Verify.MeasuredTree != got.MeasuredTree || v.Verify.MeasuredTree == now.Tree {
		t.Errorf("verify.toml records %s; want the dispatch tree %s, not the collect tree %s", v.Verify.MeasuredTree, got.MeasuredTree, now.Tree)
	}
	if !strings.Contains(out, "a.go") {
		t.Errorf("collect did not name a.go, changed between dispatch and collect:\n%s", out)
	}
	r := verify.ClassifyFreshness(v, nil, verify.TreeRef{Commit: now.Commit, Tree: now.Tree}, treeDiffer{dir: dir})
	if r.State != verify.Stale || len(r.Changed) != 1 || r.Changed[0] != "a.go" {
		t.Errorf("freshness after a post-dispatch edit = %+v, want stale naming a.go", r)
	}
}

// TestCollectUnchangedTreeNamesNothing: with nothing changed since dispatch —
// the fetched reports ignored, as configured — collect names no drift.
func TestCollectUnchangedTreeNamesNothing(t *testing.T) {
	const id = "collectsame"
	dir := detachTreeRepo(t, id)
	(&detachRecorder{}).install(t)
	dispatchAndRecord(t, dir, id)
	stubFinishedRun(t)
	if out := collectCapturing(t, id); strings.Contains(out, "changed since the tree was measured") {
		t.Errorf("collect named drift on an unchanged tree:\n%s", out)
	}
}

// TestCollectStampsTheDispatchCommit: a commit made between dispatch and
// collect does not become the verdict's commit.
func TestCollectStampsTheDispatchCommit(t *testing.T) {
	const id = "collectcommit"
	dir := detachTreeRepo(t, id)
	(&detachRecorder{}).install(t)
	got := dispatchAndRecord(t, dir, id)
	writeScopeFile(t, dir, "b.go", "package x\n\nfunc B() bool { return true } // committed later\n")
	mustGit(t, dir, "commit", "-qam", "after dispatch")

	stubFinishedRun(t)
	collectCapturing(t, id)
	v := loadVerifyToml(t, dir, id)
	if got.MeasuredCommit == "" || v.Verify.MeasuredCommit != got.MeasuredCommit {
		t.Errorf("verify.toml commit = %q, want the dispatch commit %q", v.Verify.MeasuredCommit, got.MeasuredCommit)
	}
}

// TestDispatchRefusesAnUnmeasurableTree: a capture that fails at dispatch
// stops it before the push, the start and the record.
func TestDispatchRefusesAnUnmeasurableTree(t *testing.T) {
	root := chdirDross(t)
	rec := &detachRecorder{measureErr: errors.New("index.lock exists")}
	rec.install(t)

	err := dispatchDetached(root, "dross", "unmeasurable", detachStepsFixture(), detachTarget(), time.Time{})
	if err == nil || !strings.Contains(err.Error(), "index.lock exists") {
		t.Fatalf("err = %v, want the capture failure", err)
	}
	if len(rec.syncs) != 0 || len(rec.scripts) != 0 {
		t.Errorf("an unmeasurable dispatch still pushed or started: %v", rec.order)
	}
	if got, _ := findDetachedRun(root, filepath.Dir(root), "unmeasurable"); got != nil {
		t.Error("an unmeasurable dispatch recorded a run")
	}
}

// TestDispatchSpawnFailureLeavesNoRecord: the record — tree included — is
// still written last, so a start that fails after the capture leaves none.
func TestDispatchSpawnFailureLeavesNoRecord(t *testing.T) {
	root := chdirDross(t)
	rec := &detachRecorder{spawnErr: errors.New("ssh: connection refused")}
	rec.install(t)

	if err := dispatchDetached(root, "dross", "spawnfails", detachStepsFixture(), detachTarget(), time.Time{}); err == nil {
		t.Fatal("a failed start reported success")
	}
	if !strings.Contains(strings.Join(rec.order, " "), "measure") {
		t.Fatalf("the capture never ran: %v", rec.order)
	}
	if got, _ := findDetachedRun(root, filepath.Dir(root), "spawnfails"); got != nil {
		t.Error("a failed start left a record")
	}
}

// TestDetachedRunTreeRoundTrip: the record keeps the tree through local.toml,
// and a record from before trees were kept collects with none — never the
// collect-time tree — and says its freshness is unknown.
func TestDetachedRunTreeRoundTrip(t *testing.T) {
	root := chdirDross(t)
	repoDir := filepath.Dir(root)
	in := localstore.DetachedRun{
		Phase: "rt", RunID: "r-1", Host: "helicon", Workdir: "/srv/x", RunDir: ".dross-runs/r-1",
		State: "running", MeasuredCommit: strings.Repeat("a", 40), MeasuredTree: strings.Repeat("b", 40),
	}
	if err := localstore.RecordDetachedRun(root, repoDir, in); err != nil {
		t.Fatal(err)
	}
	got, err := localstore.FindDetachedRun(root, repoDir, "rt")
	if err != nil || got == nil {
		t.Fatalf("FindDetachedRun = %+v, %v", got, err)
	}
	if got.MeasuredCommit != in.MeasuredCommit || got.MeasuredTree != in.MeasuredTree {
		t.Errorf("round trip lost the tree: %+v", got)
	}

	// collectRepo records a run the way a pre-phase dross did: no tree.
	const id = "legacyrun"
	dir := collectRepo(t, id)
	out := collectCapturing(t, id)
	body := mustRead(t, filepath.Join(dir, RootDirName, "phases", id, verify.VerifyFile))
	if strings.Contains(body, "measured_commit") || strings.Contains(body, "measured_tree") {
		t.Errorf("a tree-less run was given a tree at collect:\n%s", body)
	}
	if !strings.Contains(out, "freshness will read as unknown") {
		t.Errorf("collect did not say the run's freshness is unknown:\n%s", out)
	}
}
