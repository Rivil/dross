package cmd

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Rivil/dross/internal/mutation"
	"github.com/Rivil/dross/internal/project"
	"github.com/Rivil/dross/internal/remote"
	"github.com/Rivil/dross/internal/verify"
)

// resultsFixture records a detached run for phaseID and returns the dross root.
func resultsFixture(t *testing.T, phaseID string) string {
	t.Helper()
	root := chdirDross(t)
	rec := detachedRun{
		Phase:        phaseID,
		RunID:        "r-20260830-2201",
		Host:         "helicon",
		Workdir:      "/var/lib/buildcache/src/dross",
		RunDir:       ".dross-runs/r-20260830-2201",
		DispatchedAt: time.Now().UTC().Add(-time.Hour),
		State:        "running",
	}
	if err := recordDetachedRun(root, filepath.Dir(root), rec); err != nil {
		t.Fatal(err)
	}
	return root
}

// stubStatus swaps the host status read, capturing which host was asked.
func stubStatus(t *testing.T, st remote.RunStatus, err error) *[]string {
	t.Helper()
	var asked []string
	orig := detachStatus
	t.Cleanup(func() { detachStatus = orig })
	detachStatus = func(tg remote.Target, runDir string) (remote.RunStatus, error) {
		asked = append(asked, tg.Host)
		return st, err
	}
	return &asked
}

// exitCodeOf pulls the code out of an ExitCodeError, or -1 when the error is
// not one. The code is the contract — a caller polls on it rather than parsing
// prose — so the assertions below check it rather than the message.
func exitCodeOf(err error) int {
	var ec *ExitCodeError
	if errors.As(err, &ec) {
		return ec.Code
	}
	return -1
}

// assertNoArtefacts is the half that matters most for every non-finished
// state: a verify.toml written from a run that has not finished carries a score
// computed over whichever packages happened to be done, and looks exactly like
// a complete one.
func assertNoArtefacts(t *testing.T, root, phaseID string) {
	t.Helper()
	testsPath, verifyPath := verify.FilePaths(root, phaseID)
	for _, p := range []string{testsPath, verifyPath} {
		if _, err := os.Stat(p); err == nil {
			t.Errorf("%s was written for a run that did not finish", filepath.Base(p))
		}
	}
}

// TestResultsOnAnUnreachableHostIsNotAVerdict is c-6 at the fetch end.
//
// The run may well be finishing perfectly on a machine this laptop cannot
// currently see. Reporting that as a failed or empty run would write a verdict
// about code from an observation about a network.
func TestResultsOnAnUnreachableHostIsNotAVerdict(t *testing.T) {
	const id = "remote-run-detach"
	root := resultsFixture(t, id)
	stubStatus(t, remote.RunStatus{}, errors.New("ssh: connect to host helicon port 22: No route to host"))

	err := collectDetached(id)
	if err == nil {
		t.Fatal("an unreachable host reported success")
	}
	if got := exitCodeOf(err); got != exitResultsUnreachable {
		t.Errorf("exit code = %d, want %d (unreachable)", got, exitResultsUnreachable)
	}
	if !strings.Contains(err.Error(), "not failed") {
		t.Errorf("the message does not distinguish unknown from failed: %v", err)
	}
	assertNoArtefacts(t, root, id)
}

// TestResultsOnAStillRunningRunWritesNothing: the run is alive and will finish.
// Collecting it now would publish a partial score under this phase's name.
func TestResultsOnAStillRunningRunWritesNothing(t *testing.T) {
	const id = "remote-run-detach"
	root := resultsFixture(t, id)
	stubStatus(t, remote.RunStatus{DirExists: true, State: "running", PID: 42}, nil)

	err := collectDetached(id)
	if err == nil {
		t.Fatal("a still-running run reported success")
	}
	if got := exitCodeOf(err); got != exitResultsRunning {
		t.Errorf("exit code = %d, want %d (running)", got, exitResultsRunning)
	}
	assertNoArtefacts(t, root, id)

	// The record must survive: a fetch that cleared it would strand a run
	// still burning an hour of host time with nothing left pointing at it.
	got, err := findDetachedRun(root, filepath.Dir(root), id)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Error("polling a running run cleared its record")
	}
}

// TestResultsOnAScheduledRunSaysScheduled is the c-4 read: a user checking at
// midnight on a run set for 02:00 must be told it is waiting, not that it is
// running — otherwise the only way to tell is to notice it never finishes.
func TestResultsOnAScheduledRunSaysScheduled(t *testing.T) {
	const id = "remote-run-detach"
	root := chdirDross(t)
	at := time.Now().Add(2 * time.Hour).UTC().Truncate(time.Second)
	if err := recordDetachedRun(root, filepath.Dir(root), detachedRun{
		Phase: id, RunID: "r-1", Host: "helicon", Workdir: "/srv/x",
		RunDir: ".dross-runs/r-1", DispatchedAt: time.Now().UTC(),
		ScheduledFor: at, State: "scheduled",
	}); err != nil {
		t.Fatal(err)
	}
	stubStatus(t, remote.RunStatus{DirExists: true, State: "scheduled"}, nil)

	err := collectDetached(id)
	if got := exitCodeOf(err); got != exitResultsScheduled {
		t.Errorf("exit code = %d, want %d (scheduled): %v", got, exitResultsScheduled, err)
	}
	if err == nil || !strings.Contains(err.Error(), "scheduled for") {
		t.Errorf("the message does not say when it starts: %v", err)
	}
	assertNoArtefacts(t, root, id)
}

// TestResultsOnAVanishedRunSaysSo is the other half of c-6: "the run has
// written nothing yet" and "the run directory is gone" are both an absent exit
// file, and only the directory check tells them apart. Reported as gone rather
// than as still-running, or a user polls forever on a run that no longer exists.
func TestResultsOnAVanishedRunSaysSo(t *testing.T) {
	const id = "remote-run-detach"
	root := resultsFixture(t, id)
	stubStatus(t, remote.RunStatus{DirExists: false}, nil)

	err := collectDetached(id)
	if got := exitCodeOf(err); got != exitResultsGone {
		t.Errorf("exit code = %d, want %d (gone): %v", got, exitResultsGone, err)
	}
	if err == nil || !strings.Contains(err.Error(), ".dross-runs/r-20260830-2201") {
		t.Errorf("the message does not name the directory that vanished: %v", err)
	}
	assertNoArtefacts(t, root, id)
}

// TestResultsReadsTheRecordedHostNotTodaysGrant is c-5's fetch half, and the
// reason the record stores a host at all.
//
// A pool reordered, a grant edited, or a host that came back up between
// dispatch and now must not redirect the fetch. Collecting from a machine that
// measured nothing yields an empty report; collecting from one holding another
// run's report yields a wrong one that looks right.
func TestResultsReadsTheRecordedHostNotTodaysGrant(t *testing.T) {
	const id = "remote-run-detach"
	root := chdirDross(t)
	if err := recordDetachedRun(root, filepath.Dir(root), detachedRun{
		Phase: id, RunID: "r-1", Host: "anachryon", Workdir: "/srv/x",
		RunDir: ".dross-runs/r-1", DispatchedAt: time.Now().UTC(), State: "running",
	}); err != nil {
		t.Fatal(err)
	}
	// The grant on disk names a DIFFERENT host, which is what a re-resolution
	// would pick up.
	l, err := loadLocal(localPath(root))
	if err != nil {
		t.Fatal(err)
	}
	l.RemoteHost, l.RemoteWorkdir = "helicon", "/var/lib/buildcache/src/dross"
	if err := l.save(localPath(root)); err != nil {
		t.Fatal(err)
	}

	asked := stubStatus(t, remote.RunStatus{DirExists: true, State: "running"}, nil)
	_ = collectDetached(id)

	if len(*asked) != 1 {
		t.Fatalf("want exactly one host asked, got %v", *asked)
	}
	if (*asked)[0] != "anachryon" {
		t.Errorf("the fetch asked %q, but the run was measured on anachryon — "+
			"re-resolving the grant collects from a machine that measured nothing", (*asked)[0])
	}
}

// TestResultsWithNoRecordedRunSaysHowToStartOne: a phase nobody dispatched is a
// mistake worth naming, not an empty success.
func TestResultsWithNoRecordedRunSaysHowToStartOne(t *testing.T) {
	chdirDross(t)
	err := collectDetached("remote-run-detach")
	if err == nil {
		t.Fatal("collecting a phase with no detached run reported success")
	}
	if !strings.Contains(err.Error(), "--detach") {
		t.Errorf("the error does not say how to start one: %v", err)
	}
}

// TestFetchClearsEachLocalReportBeforePullingIt is the regression for the bug a
// live collection from helicon produced, and it is the more dangerous half of
// that bug by far.
//
// The fetch originally pulled the report DIRECTORY. FetchArgs passes its source
// through Target.In, which cleans the path and so strips the trailing slash
// rsync needs to mean "the contents of" — the reports landed in
// reports/gremlins/gremlins/ and the real ones were never written. Collect then
// read the LOCAL paths, found reports left there nine days earlier, and merged
// them into a perfectly plausible verify.toml: score 0.96 over 274 mutants,
// attributed to a file this phase had since grown by 238 lines. Every line
// number in it pointed at the pre-phase version.
//
// A fetch that silently does nothing must therefore leave NO report behind, so
// the run reads as unmeasured rather than as someone else's answer. That is
// what this pins: each local report is removed before its fetch is attempted.
func TestFetchClearsEachLocalReportBeforePullingIt(t *testing.T) {
	root := chdirDross(t)
	repoDir := filepath.Dir(root)
	reportDir := filepath.Join(repoDir, "reports", "gremlins")
	if err := os.MkdirAll(reportDir, 0o755); err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(reportDir, "internal_remote.json")
	if err := os.WriteFile(stale, []byte(`{"mutants_killed":999}`), 0o644); err != nil {
		t.Fatal(err)
	}

	steps := []mutation.PackageStep{{
		Package:   "./internal/remote",
		ReportRel: filepath.Join("reports", "gremlins", "internal_remote.json"),
	}}

	// A fetch that fails outright — the case that produced the wrong answer.
	// rsync 23 is how "the source is not there" arrives, which is what a
	// package gremlins gathered no covered mutants for leaves behind.
	orig := runDetachArgv
	t.Cleanup(func() { runDetachArgv = orig })
	runDetachArgv = func(argv []string) error { return remote.Classify("rsync", "helicon", 23) }

	if err := detachFetchReports(remote.Target{Host: "helicon", Workdir: "/srv/x"}, repoDir, steps); err != nil {
		t.Fatalf("a package with no report should not fail the fetch: %v", err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Error("a failed fetch left the previous run's report in place — Collect would " +
			"merge it and report someone else's numbers as this phase's")
	}

	// The clear must happen for ANY failed fetch, not just the tolerated one.
	// A dead connection now refuses the collect, but if it ever stopped
	// refusing, the stale report must still not be there to be merged — the
	// two guards protect the same thing from different directions.
	if err := os.WriteFile(stale, []byte(`{"mutants_killed":999}`), 0o644); err != nil {
		t.Fatal(err)
	}
	runDetachArgv = func(argv []string) error { return remote.Classify("rsync", "helicon", 255) }
	if err := detachFetchReports(remote.Target{Host: "helicon", Workdir: "/srv/x"}, repoDir, steps); err == nil {
		t.Error("a dead connection was collected as a package with no report")
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Error("a fetch that died on the connection left the previous run's report in place")
	}
}

// TestFetchPullsPerFileNotTheDirectory pins the shape rather than the symptom.
// A directory fetch nests, because FetchArgs cleans the trailing slash away;
// per-file is also what the attached path does, so the two agree by
// construction rather than by coincidence.
func TestFetchPullsPerFileNotTheDirectory(t *testing.T) {
	root := chdirDross(t)
	repoDir := filepath.Dir(root)

	steps := []mutation.PackageStep{
		{Package: "./internal/cmd", ReportRel: filepath.Join("reports", "gremlins", "internal_cmd.json")},
		{Package: "./internal/remote", ReportRel: filepath.Join("reports", "gremlins", "internal_remote.json")},
	}

	var argvs [][]string
	orig := runDetachArgv
	t.Cleanup(func() { runDetachArgv = orig })
	runDetachArgv = func(argv []string) error {
		argvs = append(argvs, argv)
		return nil
	}

	if err := detachFetchReports(remote.Target{Host: "helicon", Workdir: "/srv/x"}, repoDir, steps); err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if len(argvs) != len(steps) {
		t.Fatalf("want one fetch per package (%d), got %d", len(steps), len(argvs))
	}
	for i, argv := range argvs {
		src := argv[len(argv)-2]
		if strings.HasSuffix(src, "/gremlins") || strings.HasSuffix(src, "/gremlins/") {
			t.Errorf("fetch %d pulls the directory (%q) — rsync nests it into "+
				"reports/gremlins/gremlins and the real reports never land", i, src)
		}
		if !strings.HasSuffix(src, ".json") {
			t.Errorf("fetch %d source %q is not a report file", i, src)
		}
	}
}

// collectPayload is a gremlins report naming two files: one the phase touched
// and one it did not. The untouched file's survivor is what proves the detached
// collect applies the same scope filtering an attached run does — without it,
// a neighbour's survivor would gate this phase.
const collectPayload = `{
  "go_module": "example.com/x",
  "mutants_total": 4,
  "mutants_killed": 2,
  "mutants_lived": 2,
  "files": [
    {"file_name": "a.go", "mutations": [
      {"line": 3, "column": 1, "type": "CONDITIONALS_NEGATION", "status": "KILLED"},
      {"line": 4, "column": 1, "type": "CONDITIONALS_BOUNDARY", "status": "LIVED"}
    ]},
    {"file_name": "b.go", "mutations": [
      {"line": 3, "column": 1, "type": "CONDITIONALS_NEGATION", "status": "KILLED"},
      {"line": 4, "column": 1, "type": "ARITHMETIC_BASE", "status": "LIVED"}
    ]}
  ]
}`

// collectRepo builds the full fixture a successful collect needs, which the
// refusal tests never did: a real repo with a phase, a recorded change set, a
// finished detached run, a real gremlins adapter, and a fetch that drops a
// report where Collect will read it.
func collectRepo(t *testing.T, phaseID string) string {
	t.Helper()
	dir := scopedVerifyRepo(t, phaseID)
	phaseSpec(t, phaseID)
	writeScopeFile(t, dir, "a.go", "package x\n\nfunc A() bool { return 1 > 0 }\n")
	mustGit(t, dir, "commit", "-qam", "phase edits a.go")
	if err := runCmd(t, Changes(), "record", phaseID, "t-1", "--files", "a.go"); err != nil {
		t.Fatal(err)
	}
	mustSetBase(t, phaseID, "base")

	// A REAL Gremlins: gremlinsAdapter type-asserts the concrete type, and
	// Collect only reads files, so nothing is spawned.
	prev := configuredAdaptersFn
	configuredAdaptersFn = func(_ *project.Project, _ string, _ bool) ([]mutation.Adapter, mutationTuning, error) {
		return []mutation.Adapter{&mutation.Gremlins{ProjectRoot: dir}}, mutationTuning{}, nil
	}
	t.Cleanup(func() { configuredAdaptersFn = prev })

	root := filepath.Join(dir, RootDirName)
	if err := recordDetachedRun(root, dir, detachedRun{
		Phase: phaseID, RunID: "r-1", Host: "anachryon", Workdir: "/srv/x",
		RunDir: ".dross-runs/r-1", DispatchedAt: time.Now().UTC(), State: "running",
	}); err != nil {
		t.Fatal(err)
	}
	stubStatus(t, remote.RunStatus{DirExists: true, State: "finished", HasExit: true, ExitCode: 0}, nil)

	// The fetch drops the payload where Collect reads it, standing in for the
	// rsync without needing a host.
	origFetch := detachFetchReports
	t.Cleanup(func() { detachFetchReports = origFetch })
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
	return dir
}

// TestCollectWritesTheSameArtefactsAnAttachedRunWould is c-2's actual claim,
// and until now nothing asserted it.
//
// The refusal branches were covered; the success path — scope rebuild,
// DetachSteps, fetch, Collect, FilterReport, Tests assembly, measuredOn — had
// no test at all, which is why 36 in-hunk survivors landed in collectDetached.
// Every property below is one an attached run guarantees, restated
// independently here rather than read back from the code under test.
func TestCollectWritesTheSameArtefactsAnAttachedRunWould(t *testing.T) {
	const id = "collect"
	dir := collectRepo(t, id)
	root := filepath.Join(dir, RootDirName)

	if err := collectDetached(id); err != nil {
		t.Fatalf("collectDetached: %v", err)
	}

	testsPath, verifyPath := verify.FilePaths(root, id)
	raw, err := os.ReadFile(testsPath)
	if err != nil {
		t.Fatalf("tests.json was not written: %v", err)
	}
	var got verify.Tests
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}

	// Provenance is the recorded host, not the grant on disk.
	if got.MeasuredOn != "anachryon" {
		t.Errorf("measured_on = %q, want the host recorded at dispatch", got.MeasuredOn)
	}
	// The scope is rebuilt from changes.json, so it must name the file the
	// phase touched.
	if got.Scope == nil || len(got.Scope.Files) == 0 {
		t.Fatalf("no scope recorded — the rebuild from changes.json did not happen: %+v", got.Scope)
	}
	var sawA bool
	for _, f := range got.Scope.Files {
		if f == "a.go" {
			sawA = true
		}
	}
	if !sawA {
		t.Errorf("scope does not carry the phase's own file: %v", got.Scope.Files)
	}
	// The leg is assembled and carries the fetched report's numbers.
	if len(got.Languages) != 1 || got.Languages[0].Mutation == nil {
		t.Fatalf("no go leg assembled: %+v", got.Languages)
	}
	m := got.Languages[0].Mutation
	if m.Killed == 0 {
		t.Errorf("the fetched report contributed no kills: %+v", m)
	}
	// b.go is the untouched sibling: its survivor must be filtered out of
	// scope, exactly as the attached path filters it.
	for _, s := range m.Surviving {
		if strings.Contains(s.File, "b.go") {
			t.Errorf("an untouched sibling's survivor stayed in scope: %+v", s)
		}
	}
	if len(got.OutOfScope) == 0 {
		t.Error("nothing was filtered out of scope — b.go's survivor should have been")
	}

	// verify.toml exists and is a skeleton for the spec's criteria.
	vraw, err := os.ReadFile(verifyPath)
	if err != nil {
		t.Fatalf("verify.toml was not written: %v", err)
	}
	body := string(vraw)
	if !strings.Contains(body, `verdict = "pending"`) {
		t.Errorf("verify.toml is not a pending skeleton:\n%s", body)
	}
	if !strings.Contains(body, `"c-1"`) {
		t.Errorf("verify.toml carries no block for the spec's criterion:\n%s", body)
	}
}

// TestCollectRefusesWhenOnePackageFailedBeforeMeasuring is the end-to-end
// version of the false-green, and the shape the run-level guard cannot see.
//
// The run itself exits 0 — because the steps are joined with `;`, the recorded
// code is the LAST package's, and the last package here succeeded. So
// TestCollectRefusesARunThatProducedNothing's guard (every package empty AND a
// non-zero run) is inert: the second package measured fine and the run looks
// clean. Before the per-package exit file, this collected as a tidy score over
// half the phase, with one line of warning output and nothing in the artefacts
// to say a package had failed. That is exactly the incident reportlessExitFatal
// was written for on the attached path.
func TestCollectRefusesWhenOnePackageFailedBeforeMeasuring(t *testing.T) {
	const id = "collect"
	dir := collectRepo(t, id)
	root := filepath.Join(dir, RootDirName)

	// A second package, so the run has one that fails and one that does not.
	writeScopeFile(t, dir, "sub/c.go", "package sub\n\nfunc C() bool { return 2 > 1 }\n")
	mustGit(t, dir, "add", "sub/c.go")
	mustGit(t, dir, "commit", "-qam", "phase edits sub/c.go")
	if err := runCmd(t, Changes(), "record", id, "t-2", "--files", "sub/c.go"); err != nil {
		t.Fatal(err)
	}

	// The run as a whole exited 0: the last package succeeded.
	stubStatus(t, remote.RunStatus{DirExists: true, State: "finished", HasExit: true, ExitCode: 0}, nil)

	var failed string
	detachFetchReports = func(_ remote.Target, localRoot string, steps []mutation.PackageStep) error {
		if len(steps) < 2 {
			t.Fatalf("fixture did not produce two packages: %+v", steps)
		}
		for i, s := range steps {
			write := func(rel, body string) error {
				p := filepath.Join(localRoot, rel)
				if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
					return err
				}
				return os.WriteFile(p, []byte(body), 0o644)
			}
			if i == 0 {
				// Failed before writing anything: a code, no report.
				failed = s.Package
				if err := write(s.ExitRel, "1\n"); err != nil {
					return err
				}
				continue
			}
			if err := write(s.ReportRel, collectPayload); err != nil {
				return err
			}
			if err := write(s.ExitRel, "0\n"); err != nil {
				return err
			}
		}
		return nil
	}

	err := collectDetached(id)
	if err == nil {
		t.Fatal("a package that failed before measuring was collected as a clean partial score")
	}
	// Naming the package is the difference between an actionable refusal and
	// one the user can only respond to by re-running the whole leg.
	if !strings.Contains(err.Error(), failed) {
		t.Errorf("the refusal does not name the package that failed (%s): %v", failed, err)
	}
	// The host must be the one recorded at dispatch, not today's grant — the
	// same pin c-5 makes on the report read.
	if !strings.Contains(err.Error(), "anachryon") {
		t.Errorf("the refusal does not name the host that measured the run: %v", err)
	}
	// And nothing may be written: a partial verify.toml looks exactly like a
	// complete one to everything downstream.
	assertNoArtefacts(t, root, id)
}

// TestCollectAcceptsARunWhereEveryPackageExitedClean is the over-reach guard on
// the test above. The per-package code must refuse a FAILURE, not merely be
// present: a run whose packages all exited 0 collects normally, and a package
// that legitimately produced no report stays a benign skip.
func TestCollectAcceptsARunWhereEveryPackageExitedClean(t *testing.T) {
	const id = "collect"
	dir := collectRepo(t, id)
	root := filepath.Join(dir, RootDirName)

	orig := detachFetchReports
	detachFetchReports = func(tg remote.Target, localRoot string, steps []mutation.PackageStep) error {
		if err := orig(tg, localRoot, steps); err != nil {
			return err
		}
		for _, s := range steps {
			p := filepath.Join(localRoot, s.ExitRel)
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				return err
			}
			if err := os.WriteFile(p, []byte("0\n"), 0o644); err != nil {
				return err
			}
		}
		return nil
	}

	if err := collectDetached(id); err != nil {
		t.Fatalf("a run whose packages all exited clean was refused: %v", err)
	}
	testsPath, _ := verify.FilePaths(root, id)
	if _, err := os.Stat(testsPath); err != nil {
		t.Errorf("tests.json was not written for a clean run: %v", err)
	}
}

// --- the real detachFetchReports ---
//
// Every collect test above swaps detachFetchReports wholesale, which is why the
// exit-file fetch shipped with no coverage at all and mutation found two live
// mutants inside it. Unlike the detachSync/detachStatus/detachCancel seams,
// this body bottoms out in runDetachArgv — a package-level var — so the REAL
// function runs here with only the spawn replaced. No ssh, no rsync: the
// locked tests_spawn_no_ssh decision is untouched.

// fetchSpy runs the real detachFetchReports against a recording spawn, and
// returns every argv it would have executed. fail maps a destination path to
// the rsync exit code its fetch should report — 23 for a source that is not
// there, 255 or 12 for a connection that died. The codes are what the function
// under test must distinguish, so the fixture speaks in them rather than in a
// generic error.
func fetchSpy(t *testing.T, steps []mutation.PackageStep, localRoot string, fail map[string]int) ([][]string, error) {
	t.Helper()
	orig := runDetachArgv
	t.Cleanup(func() { runDetachArgv = orig })
	var got [][]string
	runDetachArgv = func(argv []string) error {
		got = append(got, argv)
		for _, a := range argv {
			if code, ok := fail[a]; ok {
				return remote.Classify("rsync", "helicon", code)
			}
		}
		return nil
	}
	tg := remote.Target{Host: "helicon", Workdir: "/srv/x"}
	return got, detachFetchReports(tg, localRoot, steps)
}

// argvFor returns the recorded argv whose destination is want, or nil.
func argvFor(got [][]string, want string) []string {
	for _, a := range got {
		if len(a) > 0 && a[len(a)-1] == want {
			return a
		}
	}
	return nil
}

// TestFetchReportsFetchesEachExitCodeBesideItsReport: without the code, a
// package that wrote no report is indistinguishable from one that failed before
// writing anything, and ReportlessExit has nothing to refuse on. Fetching the
// report alone would silently restore the false-green t-13 removed.
func TestFetchReportsFetchesEachExitCodeBesideItsReport(t *testing.T) {
	root := t.TempDir()
	steps := []mutation.PackageStep{
		{Package: "./internal/cmd", ReportRel: "reports/gremlins/internal_cmd.json",
			ExitRel: "reports/gremlins/internal_cmd.exit"},
		{Package: "./internal/remote", ReportRel: "reports/gremlins/internal_remote.json",
			ExitRel: "reports/gremlins/internal_remote.exit"},
	}

	got, err := fetchSpy(t, steps, root, nil)
	if err != nil {
		t.Fatalf("detachFetchReports: %v", err)
	}
	for _, s := range steps {
		if argvFor(got, filepath.Join(root, s.ReportRel)) == nil {
			t.Errorf("no fetch issued for %s's report", s.Package)
		}
		exitArgv := argvFor(got, filepath.Join(root, s.ExitRel))
		if exitArgv == nil {
			t.Fatalf("no fetch issued for %s's exit code — a failed package would read as an empty one", s.Package)
		}
		// The remote operand must name this step's exit path under the
		// target's workdir, or the fetch brings back some other file.
		if !strings.Contains(strings.Join(exitArgv, " "), "/srv/x/"+s.ExitRel) {
			t.Errorf("%s's exit fetch does not read the run's own path: %v", s.Package, exitArgv)
		}
	}
}

// TestFetchReportsClearsAStaleExitCode is the same guarantee the report gets,
// and it matters more: the exit code is what decides whether an ABSENT report
// is benign. A code left by a previous collect would answer for a package that
// recorded nothing this time.
func TestFetchReportsClearsAStaleExitCode(t *testing.T) {
	root := t.TempDir()
	steps := []mutation.PackageStep{
		{Package: "./internal/cmd", ReportRel: "reports/gremlins/internal_cmd.json",
			ExitRel: "reports/gremlins/internal_cmd.exit"},
	}
	stale := filepath.Join(root, steps[0].ExitRel)
	if err := os.MkdirAll(filepath.Dir(stale), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stale, []byte("0\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// The fetch is stubbed and writes nothing, so anything still at the path
	// afterwards is the stale file surviving.
	if _, err := fetchSpy(t, steps, root, nil); err != nil {
		t.Fatalf("detachFetchReports: %v", err)
	}
	if _, err := os.Stat(stale); err == nil {
		t.Error("a stale exit code survived the collect — it would answer for a package that recorded none")
	} else if !os.IsNotExist(err) {
		t.Fatal(err)
	}
}

// TestFetchReportsSkipsAStepWithNoExitPath: a step from before exit files
// existed carries no ExitRel, and there is nothing to fetch for it. Deriving a
// path anyway would fetch some other package's code.
func TestFetchReportsSkipsAStepWithNoExitPath(t *testing.T) {
	root := t.TempDir()
	steps := []mutation.PackageStep{
		{Package: "./internal/cmd", ReportRel: "reports/gremlins/internal_cmd.json"},
	}

	got, err := fetchSpy(t, steps, root, nil)
	if err != nil {
		t.Fatalf("detachFetchReports: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("want exactly the report fetch, got %d fetches: %v", len(got), got)
	}
	if argvFor(got, filepath.Join(root, steps[0].ReportRel)) == nil {
		t.Errorf("the report was not fetched: %v", got)
	}
}

// TestFetchReportsToleratesAFailedExitFetch: a step killed before it recorded a
// code leaves nothing to fetch, exactly as a package that gathered no covered
// mutants leaves no report. Neither absence is a collect failure — Collect
// reads an unrecorded code as a skip, and the run-level guard is the backstop.
func TestFetchReportsToleratesAFailedExitFetch(t *testing.T) {
	root := t.TempDir()
	steps := []mutation.PackageStep{
		{Package: "./internal/cmd", ReportRel: "reports/gremlins/internal_cmd.json",
			ExitRel: "reports/gremlins/internal_cmd.exit"},
		{Package: "./internal/remote", ReportRel: "reports/gremlins/internal_remote.json",
			ExitRel: "reports/gremlins/internal_remote.exit"},
	}
	// 23 is rsync's "source not there": the step recorded no code. The second
	// package must still be attempted.
	fail := map[string]int{filepath.Join(root, steps[0].ExitRel): 23}

	got, err := fetchSpy(t, steps, root, fail)
	if err != nil {
		t.Fatalf("a missing exit code failed the whole collect: %v", err)
	}
	if argvFor(got, filepath.Join(root, steps[1].ExitRel)) == nil {
		t.Error("the second package's exit fetch never happened — one absent code truncated the collect")
	}
}

// --- absence versus a dead connection (c-6 at fetch time) ---
//
// "The file is not there" and "the connection died" arrive at this layer as the
// same thing: a non-zero rsync. They mean opposite things. The first is a
// package with nothing to measure; the second is a collect that learned nothing
// about the package and must not score around it. detachFetchReports tolerated
// both until this test existed, so a connection dropping mid-collect produced a
// score over whichever packages happened to arrive first.

// TestFetchReportsRefusesADeadConnection: rsync 255 is the transport, not the
// payload. Continuing past it writes a score over a subset of the run.
func TestFetchReportsRefusesADeadConnection(t *testing.T) {
	root := t.TempDir()
	steps := []mutation.PackageStep{
		{Package: "./internal/cmd", ReportRel: "reports/gremlins/internal_cmd.json",
			ExitRel: "reports/gremlins/internal_cmd.exit"},
		{Package: "./internal/remote", ReportRel: "reports/gremlins/internal_remote.json",
			ExitRel: "reports/gremlins/internal_remote.exit"},
	}
	fail := map[string]int{filepath.Join(root, steps[0].ReportRel): 255}

	got, err := fetchSpy(t, steps, root, fail)
	if err == nil {
		t.Fatal("a dead connection was read as a package with no report")
	}
	if !errors.Is(err, remote.ErrTransport) {
		t.Errorf("the failure is not reported as a transport failure: %v", err)
	}
	if !strings.Contains(err.Error(), steps[0].Package) {
		t.Errorf("the failure does not name the package it happened on: %v", err)
	}
	// It must stop, not carry on collecting the rest — continuing is what
	// produces the partial score.
	if argvFor(got, filepath.Join(root, steps[1].ReportRel)) != nil {
		t.Error("the collect kept fetching after the connection died")
	}
}

// TestFetchReportsRefusesAConnectionThatDiesOnTheExitCode: the exit code is
// what decides whether an absent report is benign, so failing to learn it is
// strictly worse than failing to learn a report. It must refuse too, not fall
// through to the skip that an absent code earns.
func TestFetchReportsRefusesAConnectionThatDiesOnTheExitCode(t *testing.T) {
	root := t.TempDir()
	steps := []mutation.PackageStep{
		{Package: "./internal/cmd", ReportRel: "reports/gremlins/internal_cmd.json",
			ExitRel: "reports/gremlins/internal_cmd.exit"},
	}
	// 12 is rsync's protocol stream error — the connection, not the payload.
	fail := map[string]int{filepath.Join(root, steps[0].ExitRel): 12}

	if _, err := fetchSpy(t, steps, root, fail); err == nil {
		t.Fatal("a connection that died fetching the exit code was ignored")
	} else if !errors.Is(err, remote.ErrTransport) {
		t.Errorf("not reported as a transport failure: %v", err)
	}
}

// TestFetchReportsStillToleratesAnAbsentReport is the over-reach guard: the
// point is to distinguish, not to become strict. rsync 23 is a source that is
// not there, which is exactly what a package gremlins gathered no covered
// mutants for leaves behind, and the collect must carry on.
func TestFetchReportsStillToleratesAnAbsentReport(t *testing.T) {
	root := t.TempDir()
	steps := []mutation.PackageStep{
		{Package: "./internal/cmd", ReportRel: "reports/gremlins/internal_cmd.json",
			ExitRel: "reports/gremlins/internal_cmd.exit"},
		{Package: "./internal/remote", ReportRel: "reports/gremlins/internal_remote.json",
			ExitRel: "reports/gremlins/internal_remote.exit"},
	}
	fail := map[string]int{
		filepath.Join(root, steps[0].ReportRel): 23,
		filepath.Join(root, steps[0].ExitRel):   23,
	}

	got, err := fetchSpy(t, steps, root, fail)
	if err != nil {
		t.Fatalf("an absent report was treated as a fetch failure: %v", err)
	}
	if argvFor(got, filepath.Join(root, steps[1].ReportRel)) == nil {
		t.Error("the collect stopped at a package that simply had no report")
	}
}

// TestClassifyFetchReadsRsyncsVerdict pins the mapping directly, including the
// case that reaches it as a raw process failure rather than a classified one.
func TestClassifyFetchReadsRsyncsVerdict(t *testing.T) {
	if err := classifyFetch("helicon", nil); err != nil {
		t.Errorf("a successful fetch produced an error: %v", err)
	}
	for _, tc := range []struct {
		code int
		want error
	}{
		{23, remote.ErrPartial}, // source not there
		{24, remote.ErrPartial}, // source vanished mid-transfer
		{255, remote.ErrTransport},
		{12, remote.ErrTransport}, // protocol stream error
		{30, remote.ErrTransport}, // I/O timeout
	} {
		got := classifyFetch("helicon", remote.Classify("rsync", "helicon", tc.code))
		if !errors.Is(got, tc.want) {
			t.Errorf("rsync %d classified as %v, want %v", tc.code, got, tc.want)
		}
	}
	// An error that is not an exit status at all means rsync never ran, which
	// is not an absent report either.
	if err := classifyFetch("helicon", errors.New("exec: rsync not found")); err == nil {
		t.Error("a failure to spawn rsync was read as a successful fetch")
	} else if errors.Is(err, remote.ErrPartial) {
		t.Errorf("a failure to spawn rsync was read as an absent report: %v", err)
	}
}

// TestASuccessfulCollectClearsTheRecord: leaving it would report a phase as
// having a run in flight forever, and block every future --detach on it.
func TestASuccessfulCollectClearsTheRecord(t *testing.T) {
	const id = "collect"
	dir := collectRepo(t, id)
	root := filepath.Join(dir, RootDirName)

	if err := collectDetached(id); err != nil {
		t.Fatalf("collectDetached: %v", err)
	}
	rec, err := findDetachedRun(root, dir, id)
	if err != nil {
		t.Fatal(err)
	}
	if rec != nil {
		t.Errorf("the record survived a successful collect: %+v", rec)
	}
	// And a second collect must say there is nothing to collect, rather than
	// re-running against whatever is still on disk.
	if err := collectDetached(id); err == nil {
		t.Error("a second collect of an already-collected phase reported success")
	}
}

// TestCollectRefusesARunThatProducedNothing is the false-green guard on the
// success path: a tool that exited non-zero having written no measurable
// report must not be recorded as a clean run of zero mutants.
func TestCollectRefusesARunThatProducedNothing(t *testing.T) {
	const id = "collect"
	dir := collectRepo(t, id)
	root := filepath.Join(dir, RootDirName)

	// Finished, but non-zero, and the fetch brings back nothing.
	stubStatus(t, remote.RunStatus{DirExists: true, State: "finished", HasExit: true, ExitCode: 2}, nil)
	detachFetchReports = func(_ remote.Target, _ string, _ []mutation.PackageStep) error { return nil }

	err := collectDetached(id)
	if err == nil {
		t.Fatal("a run that exited 2 with no measurable report was collected as clean")
	}
	if got := exitCodeOf(err); got != exitResultsFailed {
		t.Errorf("exit code = %d, want %d (failed)", got, exitResultsFailed)
	}
	assertNoArtefacts(t, root, id)
}
