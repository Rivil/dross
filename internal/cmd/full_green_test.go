package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Rivil/dross/internal/consent"
	"github.com/Rivil/dross/internal/gatestate"
	"github.com/Rivil/dross/internal/mutation"
	"github.com/Rivil/dross/internal/phase"
	"github.com/Rivil/dross/internal/project"
	"github.com/Rivil/dross/internal/remote"
	"github.com/Rivil/dross/internal/treefp"
	"github.com/Rivil/dross/internal/verify"
)

// vouchingLane is one lane trusted to admit a commit on a scoped green.
const vouchingLane = `[[runtime.test_lane]]
name = "web"
match = ["web/**"]
command = "npx vitest run"
selector = "path"
empty_exit = [3]
vouch = true`

const fullGreenPhase = "fullgreen"

// countAdapterCalls wraps the installed seam, so a refusal can be told from a
// run that got as far as building its adapters.
func countAdapterCalls(t *testing.T) *int {
	t.Helper()
	calls := new(int)
	prev := configuredAdaptersFn
	configuredAdaptersFn = func(p *project.Project, root string, skip bool, ph string, w remote.WaitPolicy) ([]mutation.Adapter, mutationTuning, error) {
		*calls++
		return prev(p, root, skip, ph, w)
	}
	t.Cleanup(func() { configuredAdaptersFn = prev })
	return calls
}

// fullGreenRepo is a phase ready to verify on a stub adapter, a.go committed,
// with lanes appended to project.toml. test_command is set and consented
// (scopedVerifyRepo), and nothing has recorded a green.
func fullGreenRepo(t *testing.T, lanes string) (string, *int) {
	t.Helper()
	dir := scopedVerifyRepo(t, fullGreenPhase)
	phaseSpec(t, fullGreenPhase)
	writeScopeFile(t, dir, "a.go", "package x\n\nfunc A() bool { return 1 > 0 }\n")
	mustGit(t, dir, "commit", "-qam", "phase edits a.go")
	if err := runCmd(t, Changes(), "record", fullGreenPhase, "t-1", "--files", "a.go"); err != nil {
		t.Fatal(err)
	}
	mustSetBase(t, fullGreenPhase, "base")
	if lanes != "" {
		appendLanes(t, dir, lanes)
		grantAllLanes(t)
	}
	useStubAdapter(t, &stubMutationAdapter{name: "gremlins", exts: []string{".go"},
		report: goReport(map[string]mutation.FileStat{"a.go": {Killed: 1}})})
	return dir, countAdapterCalls(t)
}

// recordFullGreen runs a real bare `dross test`, so the green under test is the
// one the recorder writes rather than a tree a fixture chose to match.
func recordFullGreen(t *testing.T, dir string) *gatestate.Green {
	t.Helper()
	notes := captureGreen(t)
	installSpawnRecorder(t, nil)
	if err := runCmd(t, Test()); err != nil {
		t.Fatalf("dross test: %v", err)
	}
	g := green(t, dir)
	if g == nil {
		t.Fatalf("a green bare run recorded nothing (notes: %q)", notes.String())
	}
	return g
}

func fullGreenArtefact(dir, name string) string {
	return filepath.Join(phase.Dir(filepath.Join(dir, RootDirName), fullGreenPhase), name)
}

// assertNotMeasured: the refusal came before any adapter, and left no artefact.
func assertNotMeasured(t *testing.T, dir string, calls *int) {
	t.Helper()
	if *calls != 0 {
		t.Errorf("configuredAdapters ran %d time(s) before the refusal", *calls)
	}
	for _, name := range []string{verify.TestsFile, verify.VerifyFile} {
		if _, err := os.Stat(fullGreenArtefact(dir, name)); !os.IsNotExist(err) {
			t.Errorf("a refused verify left %s behind (stat: %v)", name, err)
		}
	}
}

// assertMeasured: the check let the run through to its adapters and artefact.
func assertMeasured(t *testing.T, dir string, calls *int) {
	t.Helper()
	if *calls != 1 {
		t.Errorf("configuredAdapters ran %d time(s), want 1", *calls)
	}
	if _, err := os.Stat(fullGreenArtefact(dir, verify.TestsFile)); err != nil {
		t.Errorf("verify wrote no %s: %v", verify.TestsFile, err)
	}
}

func assertNamesBareTest(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("verify was not refused")
	}
	if !strings.Contains(err.Error(), "bare `dross test`") {
		t.Errorf("the refusal does not name bare `dross test`: %v", err)
	}
}

// TestVerifyRefusesAGreenForAnOlderTree: code committed after the green leaves
// HEAD unmeasured, and the refusal lands before any adapter is built.
func TestVerifyRefusesAGreenForAnOlderTree(t *testing.T) {
	dir, calls := fullGreenRepo(t, vouchingLane)
	recordFullGreen(t, dir)
	writeScopeFile(t, dir, "a.go", "package x\n\nfunc A() bool { return 2 > 0 }\n")
	mustGit(t, dir, "commit", "-qam", "code after the green")

	err := runCmd(t, Verify(), fullGreenPhase)
	assertNamesBareTest(t, err)
	assertNotMeasured(t, dir, calls)
}

// TestVerifyRefusesAScopedGreen: a valid scoped record on HEAD's exact tree
// stands in for nothing, and the refusal is the no-green one.
func TestVerifyRefusesAScopedGreen(t *testing.T) {
	dir, calls := fullGreenRepo(t, vouchingLane)
	head, err := treefp.Base(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := gatestate.SaveScoped(dir, gatestate.Scoped{
		Tree:    head,
		Covered: []string{"a.go"},
		Lanes:   []string{consent.Fingerprint("npx vitest run")},
		At:      time.Now().UTC(),
		Runner:  "local",
	}); err != nil {
		t.Fatal(err)
	}

	err = runCmd(t, Verify(), fullGreenPhase)
	assertNamesBareTest(t, err)
	if !strings.Contains(err.Error(), "scoped green never stands in") {
		t.Errorf("the refusal does not say a scoped green cannot stand in: %v", err)
	}
	assertNotMeasured(t, dir, calls)
}

// TestVerifySkipMutationTakesTheCheck: skipping the mutation legs does not skip
// the question of whether the tree passed its suite.
func TestVerifySkipMutationTakesTheCheck(t *testing.T) {
	dir, calls := fullGreenRepo(t, vouchingLane)

	err := runCmd(t, Verify(), fullGreenPhase, "--skip-mutation")
	assertNamesBareTest(t, err)
	assertNotMeasured(t, dir, calls)
}

// TestVerifyDetachTakesTheCheckBeforeThePush: the refused run touches the host
// not at all, and the same dispatch reaches the push once a full green stands —
// so the silence is the check's, not a fixture that could never dispatch.
func TestVerifyDetachTakesTheCheckBeforeThePush(t *testing.T) {
	const id = "fullgreendetach"
	dir := detachTreeRepo(t, id)
	appendLanes(t, dir, vouchingLane)
	grantAllLanes(t)
	rec := &detachRecorder{}
	rec.install(t)
	calls := countAdapterCalls(t)

	assertNamesBareTest(t, runCmd(t, Verify(), id, "--detach"))
	if *calls != 0 || len(rec.order) != 0 || len(rec.syncs) != 0 || len(rec.scripts) != 0 {
		t.Fatalf("the refused --detach went on: %d adapter calls, order %q, %d syncs, %d spawns",
			*calls, rec.order, len(rec.syncs), len(rec.scripts))
	}

	recordFullGreen(t, dir)
	if err := runCmd(t, Verify(), id, "--detach"); err != nil {
		t.Fatalf("--detach with a full green on HEAD: %v", err)
	}
	if len(rec.syncs) != 1 {
		t.Errorf("--detach with a full green pushed %d time(s), want 1", len(rec.syncs))
	}
}

// TestVerifyDirtyGreenNamesPathsNotARerun: the suite passed on the work tree
// and HEAD has yet to catch up, so another run would land here again. The way
// out is the commit, and the refusal must not send anyone round the loop.
func TestVerifyDirtyGreenNamesPathsNotARerun(t *testing.T) {
	dir, calls := fullGreenRepo(t, vouchingLane)
	// Not the committed a.go's length: this edit stays uncommitted, so only
	// its stat data tells the scratch index it changed.
	writeScopeFile(t, dir, "a.go", "package x\n\nfunc A() bool { return 2 > 0 } // uncommitted\n")
	writeScopeFile(t, dir, "fresh.go", "package x\n")
	recordFullGreen(t, dir)

	err := runCmd(t, Verify(), fullGreenPhase)
	if err == nil {
		t.Fatal("verify was not refused")
	}
	msg := err.Error()
	for _, want := range []string{"a.go", "fresh.go", "Commit or clean"} {
		if !strings.Contains(msg, want) {
			t.Errorf("the refusal is missing %q: %s", want, msg)
		}
	}
	for _, never := range []string{"b.go", "dross test", "re-run", "Re-run"} {
		if strings.Contains(msg, never) {
			t.Errorf("the refusal should not say %q: %s", never, msg)
		}
	}
	assertNotMeasured(t, dir, calls)
}

// TestVerifyAcceptsAGreenOnCleanHead: the recorder fingerprints with
// WorkingTree and the check with Base. On a clean HEAD they must be one tree,
// or every full green would be refused.
func TestVerifyAcceptsAGreenOnCleanHead(t *testing.T) {
	dir, calls := fullGreenRepo(t, vouchingLane)
	g := recordFullGreen(t, dir)
	head, err := treefp.Base(dir)
	if err != nil {
		t.Fatal(err)
	}
	if g.Tree != head {
		t.Fatalf("WorkingTree recorded %s, Base says %s", g.Tree, head)
	}

	if err := runCmd(t, Verify(), fullGreenPhase); err != nil {
		t.Fatalf("verify refused a full green on HEAD: %v", err)
	}
	assertMeasured(t, dir, calls)
}

// TestVerifyAcceptsAGreenAcrossBookkeeping: dross's own commits move HEAD
// between a run and a verify without changing any code.
func TestVerifyAcceptsAGreenAcrossBookkeeping(t *testing.T) {
	dir, calls := fullGreenRepo(t, vouchingLane)
	recordFullGreen(t, dir)
	before := mustGit(t, dir, "rev-parse", "HEAD")
	writeScopeFile(t, dir, ".dross/phases/"+fullGreenPhase+"/note.txt", "bookkeeping\n")
	mustGit(t, dir, "add", ".dross")
	mustGit(t, dir, "commit", "-qm", "chore: bookkeeping")
	if after := mustGit(t, dir, "rev-parse", "HEAD"); after == before {
		t.Fatal("fixture precondition: the bookkeeping commit did not move HEAD")
	}

	if err := runCmd(t, Verify(), fullGreenPhase); err != nil {
		t.Fatalf("verify refused a green after a .dross-only commit: %v", err)
	}
	assertMeasured(t, dir, calls)
}

// TestVerifyLeavesReposWithNoVouchingLaneAlone: test_command is set and no
// green exists, and verify runs as it always has, declared lanes or not.
func TestVerifyLeavesReposWithNoVouchingLaneAlone(t *testing.T) {
	for name, lanes := range map[string]string{
		"no lanes":             "",
		"lanes, none vouching": fullRunLanes,
	} {
		t.Run(name, func(t *testing.T) {
			dir, calls := fullGreenRepo(t, lanes)
			if g := green(t, dir); g != nil {
				t.Fatalf("fixture precondition: a green exists: %+v", g)
			}
			if err := runCmd(t, Verify(), fullGreenPhase); err != nil {
				t.Fatalf("verify refused a repo with no vouching lane: %v", err)
			}
			assertMeasured(t, dir, calls)
		})
	}
}

// TestHasVouchingLane: one vouching lane anywhere in the list is enough.
func TestHasVouchingLane(t *testing.T) {
	cases := []struct {
		name  string
		lanes []project.TestLane
		want  bool
	}{
		{"none declared", nil, false},
		{"none vouching", []project.TestLane{{Name: "a"}, {Name: "b"}}, false},
		{"the first", []project.TestLane{{Name: "a", Vouch: true}, {Name: "b"}}, true},
		{"the last", []project.TestLane{{Name: "a"}, {Name: "b", Vouch: true}}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := &project.Project{}
			p.Runtime.TestLane = c.lanes
			if got := hasVouchingLane(p); got != c.want {
				t.Errorf("hasVouchingLane = %v, want %v", got, c.want)
			}
		})
	}
}

func vouchingProject() *project.Project {
	p := &project.Project{}
	p.Runtime.TestLane = []project.TestLane{{Name: "web", Vouch: true}}
	return p
}

// TestRequireFullGreenOutsideGit: there is no HEAD to judge, and no run could
// ever record a green, so a refusal here could never be cleared.
func TestRequireFullGreenOutsideGit(t *testing.T) {
	if err := requireFullGreen(vouchingProject(), t.TempDir()); err != nil {
		t.Errorf("refused outside a git work tree: %v", err)
	}
}

// TestRequireFullGreenRefusesWhatItCannotRead: an unreadable record and a git
// that cannot name HEAD's tree are refusals, never a pass.
func TestRequireFullGreenRefusesWhatItCannotRead(t *testing.T) {
	t.Run("a green that does not decode", func(t *testing.T) {
		dir := scopeRepo(t)
		writeScopeFile(t, dir, ".dross/gate/green.json", "not json")
		err := requireFullGreen(vouchingProject(), dir)
		if err == nil || !strings.Contains(err.Error(), "green.json") {
			t.Errorf("err = %v, want one naming green.json", err)
		}
	})
	t.Run("a broken repository", func(t *testing.T) {
		dir := scopeRepo(t)
		if err := gatestate.SaveGreen(dir, gatestate.Green{Tree: "abc", At: time.Now().UTC(), Runner: "local"}); err != nil {
			t.Fatal(err)
		}
		writeScopeFile(t, dir, ".git/HEAD", "not a ref\n")
		err := requireFullGreen(vouchingProject(), dir)
		if err == nil || !strings.Contains(err.Error(), "fingerprint HEAD's tree") {
			t.Errorf("err = %v, want one saying HEAD's tree could not be read", err)
		}
	})
}
