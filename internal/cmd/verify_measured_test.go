package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/mutation"
	"github.com/Rivil/dross/internal/treefp"
	"github.com/Rivil/dross/internal/verify"
)

// sideEffectAdapter is a stub adapter that does something to the work tree
// while it "runs" — the edit a long leg can race, or a report a tool writes.
type sideEffectAdapter struct {
	stubMutationAdapter
	during func()
	runs   int
}

func (s *sideEffectAdapter) Run(files []string) (*mutation.Report, error) {
	s.runs++
	if s.during != nil {
		s.during()
	}
	return s.stubMutationAdapter.Run(files)
}

// measuredRepo is lifecycleRepo with its adapter swapped for one that runs
// during, returned with the adapter so a test can count its runs.
func measuredRepo(t *testing.T, slug string, during func()) (string, *sideEffectAdapter) {
	t.Helper()
	dir := lifecycleRepo(t, slug)
	a := &sideEffectAdapter{
		stubMutationAdapter: stubMutationAdapter{name: "gremlins", exts: []string{".go"},
			report: goReport(map[string]mutation.FileStat{"a.go": {Survived: 1}},
				mutation.Mutant{File: "a.go", Line: 3, Op: "CONDITIONALS_BOUNDARY"})},
		during: during,
	}
	useStubAdapter(t, a)
	return dir, a
}

func loadVerifyToml(t *testing.T, dir, phaseID string) *verify.Verify {
	t.Helper()
	v, err := verify.LoadVerify(filepath.Join(dir, ".dross", "phases", phaseID, verify.VerifyFile))
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// TestVerifyRecordsTheTreeAtRunStart: the tree is taken before the run, so an
// edit the run races is not vouched for — verify.toml keeps the pre-run tree
// and the run names the file that moved.
func TestVerifyRecordsTheTreeAtRunStart(t *testing.T) {
	var dir string
	dir, _ = measuredRepo(t, "atstart", func() {
		writeScopeFile(t, dir, "b.go", "package x\n\nfunc B() bool { return 2 > 1 } // edited mid-run\n")
	})
	before, err := treefp.MeasuredTree(dir)
	if err != nil {
		t.Fatal(err)
	}
	out := runVerifyCapturing(t, "01-atstart")
	v := loadVerifyToml(t, dir, "01-atstart")
	if v.Verify.MeasuredTree != before.Tree || v.Verify.MeasuredCommit != before.Commit {
		t.Errorf("verify.toml records %s@%s, want the pre-run tree %s@%s", v.Verify.MeasuredTree, v.Verify.MeasuredCommit, before.Tree, before.Commit)
	}
	if !strings.Contains(out, "changed since the tree was measured") || !strings.Contains(out, "b.go") {
		t.Errorf("the mid-run edit to b.go was not named:\n%s", out)
	}
}

// TestVerifyNamesUnignoredToolOutput: a report a tool writes outside
// .gitignore moves the tree; the run names it with the .gitignore hint.
func TestVerifyNamesUnignoredToolOutput(t *testing.T) {
	var dir string
	dir, _ = measuredRepo(t, "tooloutput", func() {
		writeScopeFile(t, dir, "reports/x.json", "{}\n")
	})
	out := runVerifyCapturing(t, "01-tooloutput")
	if !strings.Contains(out, "reports/x.json") || !strings.Contains(out, ".gitignore") {
		t.Errorf("tool output outside .gitignore was not named with the hint:\n%s", out)
	}
}

// TestVerifySkipMutationRecordsTheMeasuredTree: a --skip-mutation run records
// its tree too — the work tree as it is, uncommitted edit and untracked file
// included, not HEAD's.
func TestVerifySkipMutationRecordsTheMeasuredTree(t *testing.T) {
	dir, _ := measuredRepo(t, "skipmut", nil)
	writeScopeFile(t, dir, "a.go", "package x\n\nfunc A() bool { return 1 >= 0 } // uncommitted\n")
	writeScopeFile(t, dir, "new.go", "package x\n")
	before, err := treefp.MeasuredTree(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := runCmd(t, Verify(), "01-skipmut", "--skip-mutation"); err != nil {
		t.Fatalf("verify --skip-mutation: %v", err)
	}
	v := loadVerifyToml(t, dir, "01-skipmut")
	head := strings.TrimSpace(mustGit(t, dir, "rev-parse", "HEAD"))
	if v.Verify.MeasuredTree != before.Tree || v.Verify.MeasuredCommit != head {
		t.Errorf("verify.toml records %s@%s, want %s@%s", v.Verify.MeasuredTree, v.Verify.MeasuredCommit, before.Tree, head)
	}
	mustGit(t, dir, "checkout", "--", "a.go")
	if err := os.Remove(filepath.Join(dir, "new.go")); err != nil {
		t.Fatal(err)
	}
	clean, err := treefp.MeasuredTree(dir)
	if err != nil {
		t.Fatal(err)
	}
	if v.Verify.MeasuredTree == clean.Tree {
		t.Error("the recorded tree equals HEAD's — the uncommitted edit and untracked file were not measured")
	}
}

// TestVerifyRefusesWhenTheTreeCannotBeMeasured: in a git tree a capture that
// fails refuses before anything runs or is written — a verdict with no tree
// would ship as freshness-unknown.
func TestVerifyRefusesWhenTheTreeCannotBeMeasured(t *testing.T) {
	dir, a := measuredRepo(t, "nocapture", nil)
	prev := measuredTreeFn
	measuredTreeFn = func(string) (treefp.Measured, error) { return treefp.Measured{}, errors.New("index locked") }
	t.Cleanup(func() { measuredTreeFn = prev })

	err := runCmd(t, Verify(), "01-nocapture")
	if err == nil || !strings.Contains(err.Error(), "index locked") {
		t.Fatalf("err = %v, want a refusal naming the capture failure", err)
	}
	if a.runs != 0 {
		t.Errorf("the adapter ran %d time(s) after the capture failed", a.runs)
	}
	for _, f := range []string{verify.TestsFile, verify.VerifyFile} {
		if _, err := os.Stat(filepath.Join(dir, ".dross", "phases", "01-nocapture", f)); err == nil {
			t.Errorf("%s was written although the tree could not be measured", f)
		}
	}
}

// TestVerifyConsentPrecedesCapture: an untrusted tree is refused before its
// tree is even fingerprinted.
func TestVerifyConsentPrecedesCapture(t *testing.T) {
	measuredRepo(t, "consentfirst", nil)
	revokeConsent(t)
	called := false
	prev := measuredTreeFn
	measuredTreeFn = func(dir string) (treefp.Measured, error) { called = true; return prev(dir) }
	t.Cleanup(func() { measuredTreeFn = prev })

	if err := runCmd(t, Verify(), "01-consentfirst"); err == nil {
		t.Fatal("verify ran in an untrusted tree")
	}
	if called {
		t.Error("the tree was fingerprinted before exec consent was checked")
	}
}

// TestVerifyOutsideGitRecordsNoTree: outside a git work tree there is no tree
// to take — the run records none, says so once, and still writes its verdict.
func TestVerifyOutsideGitRecordsNoTree(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	if err := runCmd(t, Init()); err != nil {
		t.Fatal(err)
	}
	trustFixture(t)
	mustRunSet(t, "project.name", "x")
	mustRunSet(t, "runtime.mode", "native")
	if err := runCmd(t, Phase(), "create", "nogit"); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, ".dross/phases/01-nogit/spec.toml", "[phase]\nid = \"01-nogit\"\ntitle = \"x\"\n[[criteria]]\nid = \"c-1\"\ntext = \"x\"\n")
	if err := runCmd(t, Changes(), "record", "01-nogit", "t-1", "--files", "src/x.go", "--commit", "abc1234"); err != nil {
		t.Fatal(err)
	}
	calls := 0
	prev := measuredTreeFn
	measuredTreeFn = func(dir string) (treefp.Measured, error) { calls++; return prev(dir) }
	t.Cleanup(func() { measuredTreeFn = prev })
	out := captureStdout(t, func() {
		if err := runCmd(t, Verify(), "01-nogit", "--skip-mutation"); err != nil {
			t.Fatalf("verify outside git: %v", err)
		}
	})
	if strings.Count(out, "not a git work tree") != 1 {
		t.Errorf("the run must say once that it recorded no tree:\n%s", out)
	}
	// With no tree recorded there is nothing to re-check: finishVerify must
	// not fingerprint again, or complain that it could not.
	if calls != 0 || strings.Contains(out, "re-check the tree") {
		t.Errorf("a run with no recorded tree fingerprinted %d time(s) or re-checked:\n%s", calls, out)
	}
	body := mustRead(t, filepath.Join(dir, ".dross", "phases", "01-nogit", verify.VerifyFile))
	if strings.Contains(body, "measured_commit") || strings.Contains(body, "measured_tree") {
		t.Errorf("a run outside git recorded a tree:\n%s", body)
	}
}

// TestCaptureRefusesWhenGitFailsInsideARepo: "not a git tree" is the absence
// of a .git, never a git command failing. Inside a repository a capture that
// fails — git missing, safe.directory, corruption — refuses.
func TestCaptureRefusesWhenGitFailsInsideARepo(t *testing.T) {
	calls := 0
	prev := measuredTreeFn
	measuredTreeFn = func(string) (treefp.Measured, error) {
		calls++
		return treefp.Measured{}, errors.New("detected dubious ownership")
	}
	t.Cleanup(func() { measuredTreeFn = prev })

	repo := t.TempDir()
	if err := os.Mkdir(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(repo, "svc")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := captureMeasuredTree(sub); err == nil || !strings.Contains(err.Error(), "dubious ownership") {
		t.Errorf("a failed capture under a .git = %v, want a refusal naming it", err)
	}

	calls = 0
	if m, err := captureMeasuredTree(t.TempDir()); err != nil || m.Tree != "" || calls != 0 {
		t.Errorf("no .git anywhere: %+v, %v (captures %d), want no tree, no error, no capture", m, err, calls)
	}
}
