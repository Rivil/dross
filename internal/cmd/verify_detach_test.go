package cmd

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Rivil/dross/internal/mutation"
	"github.com/Rivil/dross/internal/project"
	"github.com/Rivil/dross/internal/remote"
)

// detachRecorder captures what a dispatch would have done to the host, so
// "started and returned" is asserted without an ssh anywhere.
type detachRecorder struct {
	scripts  []string
	syncs    []string
	spawnErr error
	syncErr  error
	// order records the sequence of operations by name, which is what makes
	// "pushed before started" an assertion rather than an assumption.
	order []string
}

func (r *detachRecorder) install(t *testing.T) {
	t.Helper()
	origSpawn, origSync := detachSpawn, detachSync
	t.Cleanup(func() { detachSpawn, detachSync = origSpawn, origSync })
	detachSpawn = func(tg remote.Target, script string) (string, error) {
		r.scripts = append(r.scripts, script)
		r.order = append(r.order, "spawn")
		return "", r.spawnErr
	}
	detachSync = func(tg remote.Target, localRoot string) error {
		r.syncs = append(r.syncs, tg.Host)
		r.order = append(r.order, "sync")
		return r.syncErr
	}
}

func detachTarget() *remote.Target {
	return &remote.Target{Host: "helicon", Workdir: "/var/lib/buildcache/src/dross"}
}

func detachStepsFixture() []mutation.PackageStep {
	return []mutation.PackageStep{
		{Package: "./internal/cmd", ReportRel: "reports/gremlins/internal_cmd.json",
			Argv: []string{"gremlins", "unleash", "--output", "reports/gremlins/internal_cmd.json", "./internal/cmd"}},
		{Package: "./internal/remote", ReportRel: "reports/gremlins/internal_remote.json",
			Argv: []string{"gremlins", "unleash", "--output", "reports/gremlins/internal_remote.json", "./internal/remote"}},
	}
}

// TestDispatchReturnsWithoutWaiting is c-1 at the command level: exactly one
// remote start, and the call returns while the tool is still running.
//
// The seam records rather than executes, so a dispatch that blocked on the run
// would never return here at all — the assertion is that the function comes
// back having spawned once, not that it came back quickly.
func TestDispatchReturnsWithoutWaiting(t *testing.T) {
	root := chdirDross(t)
	rec := &detachRecorder{}
	rec.install(t)

	if err := dispatchDetached(root, "remote-run-detach", detachStepsFixture(), detachTarget(), time.Time{}); err != nil {
		t.Fatalf("dispatchDetached: %v", err)
	}
	if len(rec.scripts) != 1 {
		t.Fatalf("want exactly 1 remote start, got %d", len(rec.scripts))
	}
	// Every package's work must be in the ONE script: a dispatch that started
	// one run per package would leave N detached jobs and one record.
	for _, s := range detachStepsFixture() {
		if !strings.Contains(rec.scripts[0], s.Package) {
			t.Errorf("the dispatched script omits package %s:\n%s", s.Package, rec.scripts[0])
		}
	}
}

// TestDispatchPushesBeforeItStarts pins the order. A run started against an
// unsynced tree measures whatever the host had left from the last dispatch and
// reports it under this phase's name — a wrong answer that looks exactly like a
// right one, discovered only if someone compares it to an attached run.
func TestDispatchPushesBeforeItStarts(t *testing.T) {
	root := chdirDross(t)
	rec := &detachRecorder{}
	rec.install(t)

	if err := dispatchDetached(root, "remote-run-detach", detachStepsFixture(), detachTarget(), time.Time{}); err != nil {
		t.Fatalf("dispatchDetached: %v", err)
	}
	want := []string{"sync", "spawn"}
	if len(rec.order) != len(want) || rec.order[0] != want[0] || rec.order[1] != want[1] {
		t.Errorf("dispatch order = %v, want %v", rec.order, want)
	}
}

// TestDispatchRecordsTheHostItDispatchedTo is c-5's dispatch half. The record
// is what a later fetch reads the report from, so a record naming a different
// host than the one that was started would collect from a machine that measured
// nothing — or worse, one holding a stale report from another run.
func TestDispatchRecordsTheHostItDispatchedTo(t *testing.T) {
	root := chdirDross(t)
	repoDir := filepath.Dir(root)
	rec := &detachRecorder{}
	rec.install(t)
	target := detachTarget()

	if err := dispatchDetached(root, "remote-run-detach", detachStepsFixture(), target, time.Time{}); err != nil {
		t.Fatalf("dispatchDetached: %v", err)
	}
	got, err := findDetachedRun(root, repoDir, "remote-run-detach")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("the dispatch recorded no run")
	}
	if got.Host != target.Host {
		t.Errorf("recorded host %q, dispatched to %q", got.Host, target.Host)
	}
	if got.Workdir != target.Workdir {
		t.Errorf("recorded workdir %q, dispatched to %q", got.Workdir, target.Workdir)
	}
	if got.State != "running" {
		t.Errorf("an immediate dispatch recorded state %q, want running", got.State)
	}
	if !strings.Contains(rec.scripts[0], got.RunDir) {
		t.Errorf("the recorded run dir %q is not the one the script uses:\n%s", got.RunDir, rec.scripts[0])
	}
}

// TestSecondDispatchIsRefusedByName is the in-flight guard at the command
// level, and it must refuse BEFORE the push: rsyncing a tree to a host for a
// run that is then refused is minutes of transfer spent on nothing.
func TestSecondDispatchIsRefusedByName(t *testing.T) {
	root := chdirDross(t)
	rec := &detachRecorder{}
	rec.install(t)

	if err := dispatchDetached(root, "remote-run-detach", detachStepsFixture(), detachTarget(), time.Time{}); err != nil {
		t.Fatalf("first dispatch: %v", err)
	}
	syncsAfterFirst := len(rec.syncs)

	err := dispatchDetached(root, "remote-run-detach", detachStepsFixture(), detachTarget(), time.Time{})
	if err == nil {
		t.Fatal("a second dispatch for a phase with a run in flight was accepted")
	}
	if !strings.Contains(err.Error(), "helicon") || !strings.Contains(err.Error(), "remote-run-detach") {
		t.Errorf("the refusal does not name the run in flight: %v", err)
	}
	if len(rec.syncs) != syncsAfterFirst {
		t.Errorf("the refused dispatch pushed the tree anyway (%d syncs, want %d)", len(rec.syncs), syncsAfterFirst)
	}
	if len(rec.scripts) != 1 {
		t.Errorf("the refused dispatch started a run: %d scripts", len(rec.scripts))
	}
}

// TestAFailedStartRecordsNothing is why the record is written last. A record
// written before the host accepted the script would name a run that never
// started — and the one-run-per-phase guard would then refuse the retry that
// would have fixed it, stranding the phase until someone cancelled by hand.
func TestAFailedStartRecordsNothing(t *testing.T) {
	root := chdirDross(t)
	repoDir := filepath.Dir(root)
	rec := &detachRecorder{spawnErr: errors.New("ssh: connect to host helicon port 22: No route to host")}
	rec.install(t)

	if err := dispatchDetached(root, "remote-run-detach", detachStepsFixture(), detachTarget(), time.Time{}); err == nil {
		t.Fatal("a dispatch whose start failed reported success")
	}
	got, err := findDetachedRun(root, repoDir, "remote-run-detach")
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Errorf("a failed dispatch left a record behind: %+v", got)
	}
}

// TestScheduledDispatchRecordsItsStartTime is c-4's record half: `verify
// status` must be able to say "scheduled for 02:00" without asking the host,
// or a user checking at midnight cannot tell a waiting run from a running one.
func TestScheduledDispatchRecordsItsStartTime(t *testing.T) {
	root := chdirDross(t)
	repoDir := filepath.Dir(root)
	rec := &detachRecorder{}
	rec.install(t)
	at := time.Now().Add(3 * time.Hour).Truncate(time.Second)

	if err := dispatchDetached(root, "remote-run-detach", detachStepsFixture(), detachTarget(), at); err != nil {
		t.Fatalf("dispatchDetached: %v", err)
	}
	got, err := findDetachedRun(root, repoDir, "remote-run-detach")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("no record")
	}
	if !got.Scheduled() || !got.ScheduledFor.Equal(at) {
		t.Errorf("scheduled_for = %v, want %v", got.ScheduledFor, at)
	}
	if got.State != "scheduled" {
		t.Errorf("state = %q, want scheduled", got.State)
	}
}

// TestDetachRefusesWithoutAReachableHost is the no-fallback rule.
//
// Everywhere else in verify, falling back to this machine is right — the
// numbers still get measured. With --detach the user has said the one thing
// they will not accept is holding the session for the leg, and a local run does
// exactly that while reporting success.
func TestDetachRefusesWithoutAReachableHost(t *testing.T) {
	if err := detachRequiresAHost(mutationTuning{}); err == nil {
		t.Error("--detach was accepted with no granted host")
	} else if !strings.Contains(err.Error(), "dross remote grant") {
		t.Errorf("the refusal does not name the fix: %v", err)
	}

	unreachable := mutationTuning{FellBackFrom: "helicon", FallbackWhy: "helicon unreachable: ssh exit 255"}
	err := detachRequiresAHost(unreachable)
	if err == nil {
		t.Fatal("--detach was accepted against a host that could not be reached")
	}
	if !strings.Contains(err.Error(), "helicon") {
		t.Errorf("the refusal does not name the host that failed: %v", err)
	}

	if err := detachRequiresAHost(mutationTuning{Target: detachTarget()}); err != nil {
		t.Errorf("--detach was refused with a reachable host: %v", err)
	}
}

// TestDetachSequenceClearsEachReportBeforeItsPackage pins the per-package
// shape. A report left by a previous dispatch that is not removed first is
// collected as this run's — the remote half of the guarantee the attached loop
// gets from os.Remove, and it cannot be skipped because rsync --delete will not
// clear it (reports/ is gitignored, and the sync filter protects ignored paths).
func TestDetachSequenceClearsEachReportBeforeItsPackage(t *testing.T) {
	seq := detachSequence(detachStepsFixture())
	for _, s := range detachStepsFixture() {
		rm := "rm -f '" + s.ReportRel + "'"
		if !strings.Contains(seq, rm) {
			t.Errorf("the sequence does not clear %s before running it:\n%s", s.ReportRel, seq)
		}
		if strings.Index(seq, rm) > strings.Index(seq, "'"+s.Package+"'") {
			t.Errorf("%s is cleared AFTER its package runs:\n%s", s.ReportRel, seq)
		}
	}
	if !strings.HasPrefix(seq, "mkdir -p reports/gremlins") {
		t.Errorf("the sequence does not create the report directory first:\n%s", seq)
	}
}

// TestDetachSequenceSurvivesAFailingPackage: joined with `;` rather than `&&`,
// mirroring the attached loop. A package whose run fails must not truncate the
// packages after it — `&&` would report the remainder as unmeasured when they
// were simply never attempted.
func TestDetachSequenceSurvivesAFailingPackage(t *testing.T) {
	seq := detachSequence(detachStepsFixture())
	if strings.Contains(seq, "&&") {
		t.Errorf("the sequence short-circuits on the first failing package:\n%s", seq)
	}
}

// TestDetachSequenceQuotesEveryWord is the injection assertion for the layer
// that composes the sequence: these words are joined into a string that is then
// quoted twice on its way to the host, and a word that escaped here escapes
// everywhere downstream.
func TestDetachSequenceQuotesEveryWord(t *testing.T) {
	seq := detachSequence([]mutation.PackageStep{{
		Package:   "./a b;c",
		ReportRel: "reports/gremlins/a b;c.json",
		Argv:      []string{"gremlins", "unleash", "./a b;c"},
	}})
	if !strings.Contains(seq, `'./a b;c'`) {
		t.Errorf("the package is not quoted as one word:\n%s", seq)
	}
	if !strings.Contains(seq, `'reports/gremlins/a b;c.json'`) {
		t.Errorf("the report path is not quoted as one word:\n%s", seq)
	}
}

// TestParseDetachAtResolvesToTheNextOccurrence is the only reading of a bare
// HH:MM that is ever useful. Typing 02:00 at 23:00 means tomorrow morning; a
// parse that resolved it to today would schedule a run 21 hours in the past,
// and the host-side comparison would start it immediately — the exact opposite
// of what was asked, silently.
func TestParseDetachAtResolvesToTheNextOccurrence(t *testing.T) {
	now := time.Date(2026, 8, 30, 23, 0, 0, 0, time.UTC)
	got, err := parseDetachAt("02:00", now)
	if err != nil {
		t.Fatalf("parseDetachAt: %v", err)
	}
	if !got.After(now) {
		t.Errorf("02:00 typed at 23:00 resolved to %v, which is not after now", got)
	}
	if got.Day() != 31 || got.Hour() != 2 {
		t.Errorf("02:00 typed at 23:00 resolved to %v, want the 31st at 02:00", got)
	}

	// Later today stays today rather than jumping a day.
	morning := time.Date(2026, 8, 30, 1, 0, 0, 0, time.UTC)
	got, err = parseDetachAt("02:00", morning)
	if err != nil {
		t.Fatal(err)
	}
	if got.Day() != 30 || got.Hour() != 2 {
		t.Errorf("02:00 typed at 01:00 resolved to %v, want the same day", got)
	}
}

// TestParseDetachAtAcceptsAnInstantAndRefusesNonsense: an explicit RFC3339 is
// the unambiguous form, and anything else must be refused rather than silently
// treated as "now" — a misparse that dispatched immediately would spend the
// host's night on a run the user asked to defer.
func TestParseDetachAtAcceptsAnInstantAndRefusesNonsense(t *testing.T) {
	want := time.Date(2026, 8, 31, 2, 0, 0, 0, time.UTC)
	got, err := parseDetachAt("2026-08-31T02:00:00Z", time.Now())
	if err != nil {
		t.Fatalf("parseDetachAt: %v", err)
	}
	if !got.Equal(want) {
		t.Errorf("parseDetachAt = %v, want %v", got, want)
	}

	for _, bad := range []string{"tonight", "2am", "25:00", "-1"} {
		if _, err := parseDetachAt(bad, time.Now()); err == nil {
			t.Errorf("parseDetachAt accepted %q", bad)
		}
	}

	// Empty is not an error — it is the absence of a schedule.
	zero, err := parseDetachAt("", time.Now())
	if err != nil || !zero.IsZero() {
		t.Errorf("an empty --at should mean no schedule, got %v / %v", zero, err)
	}
}

// TestGremlinsAdapterNamesTheGapRatherThanDispatchingHalfARun: a phase whose
// changes are half TypeScript would otherwise get a detached run measuring only
// its Go half, collected later as though it were the whole thing.
func TestGremlinsAdapterNamesTheGapRatherThanDispatchingHalfARun(t *testing.T) {
	if _, err := gremlinsAdapter(nil); err == nil {
		t.Fatal("a detached dispatch with no gremlins leg was accepted")
	} else if !strings.Contains(err.Error(), "gremlins") {
		t.Errorf("the refusal does not say which leg is supported: %v", err)
	}

	g := &mutation.Gremlins{}
	got, err := gremlinsAdapter([]mutation.Adapter{g})
	if err != nil {
		t.Fatalf("gremlinsAdapter: %v", err)
	}
	if got != g {
		t.Error("gremlinsAdapter returned a different adapter than the one configured")
	}
}

// detachCmdRepo is the fixture the command-level tests need: a real repo with
// a phase, a recorded change set, and a stubbed adapter list whose tuning the
// caller chooses. Returns the repo dir.
func detachCmdRepo(t *testing.T, phaseID string, tuning mutationTuning) string {
	t.Helper()
	dir := scopedVerifyRepo(t, phaseID)
	phaseSpec(t, phaseID)
	writeScopeFile(t, dir, "a.go", "package x\n\nfunc A() bool { return 1 > 0 }\n")
	mustGit(t, dir, "commit", "-qam", "phase edits a.go")
	if err := runCmd(t, Changes(), "record", phaseID, "t-1", "--files", "a.go"); err != nil {
		t.Fatal(err)
	}
	mustSetBase(t, phaseID, "base")

	prev := configuredAdaptersFn
	configuredAdaptersFn = func(_ *project.Project, _ string, _ bool) ([]mutation.Adapter, mutationTuning, error) {
		return []mutation.Adapter{&mutation.Gremlins{ProjectRoot: dir}}, tuning, nil
	}
	t.Cleanup(func() { configuredAdaptersFn = prev })
	return dir
}

// TestVerifyDetachRefusesWithNoHostThroughTheCommand drives Verify's own RunE
// rather than dispatchDetached, which is what every other dispatch test does.
//
// The wiring between the flag and the guards had no test at all — 12 in-hunk
// survivors sat in that branch — so a refactor that dropped the
// detachRequiresAHost call would have gone unnoticed while the helper's own
// test kept passing.
func TestVerifyDetachRefusesWithNoHostThroughTheCommand(t *testing.T) {
	detachCmdRepo(t, "detachcmd", mutationTuning{}) // no Target: nothing granted
	rec := &detachRecorder{}
	rec.install(t)

	err := runCmd(t, Verify(), "detachcmd", "--detach")
	if err == nil {
		t.Fatal("--detach was accepted with no granted host")
	}
	if !strings.Contains(err.Error(), "dross remote grant") {
		t.Errorf("the refusal does not name the fix: %v", err)
	}
	if len(rec.scripts) != 0 || len(rec.syncs) != 0 {
		t.Errorf("a refused --detach still touched the host: %d syncs, %d spawns",
			len(rec.syncs), len(rec.scripts))
	}
}

// TestVerifyDetachRejectsABadAtThroughTheCommand pins the --at call site. A
// misparse that fell through to an immediate dispatch would spend the host's
// night on a run the user asked to defer, and say nothing.
func TestVerifyDetachRejectsABadAtThroughTheCommand(t *testing.T) {
	detachCmdRepo(t, "detachcmd", mutationTuning{Target: detachTarget()})
	rec := &detachRecorder{}
	rec.install(t)

	err := runCmd(t, Verify(), "detachcmd", "--detach", "--at", "tonight")
	if err == nil {
		t.Fatal("--at tonight was accepted")
	}
	if !strings.Contains(err.Error(), "--at") {
		t.Errorf("the refusal does not name the flag: %v", err)
	}
	if len(rec.scripts) != 0 {
		t.Error("a bad --at still started a run")
	}
}

// TestVerifyDetachDispatchesThroughTheCommand is the positive half: the flag
// reaches dispatchDetached with a schedule parsed and an adapter resolved.
func TestVerifyDetachDispatchesThroughTheCommand(t *testing.T) {
	dir := detachCmdRepo(t, "detachcmd", mutationTuning{Target: detachTarget()})
	rec := &detachRecorder{}
	rec.install(t)

	if err := runCmd(t, Verify(), "detachcmd", "--detach", "--at", "02:00"); err != nil {
		t.Fatalf("dross verify --detach: %v", err)
	}
	if len(rec.scripts) != 1 {
		t.Fatalf("want exactly one dispatch, got %d", len(rec.scripts))
	}
	got, err := findDetachedRun(filepath.Join(dir, RootDirName), dir, "detachcmd")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("the command dispatched but recorded nothing")
	}
	if !got.Scheduled() {
		t.Error("--at reached the command but no schedule was recorded")
	}
	if got.State != "scheduled" {
		t.Errorf("state = %q, want scheduled", got.State)
	}
}

// TestVerifyStatusAndCancelThroughTheCommand covers verifyStatus's own RunE,
// including the --cancel branch that chooses between listing and tearing down.
func TestVerifyStatusAndCancelThroughTheCommand(t *testing.T) {
	dir := detachCmdRepo(t, "detachcmd", mutationTuning{Target: detachTarget()})
	root := filepath.Join(dir, RootDirName)
	if err := recordDetachedRun(root, dir, detachedRun{
		Phase: "detachcmd", RunID: "r-1", Host: "helicon", Workdir: "/srv/x",
		RunDir: ".dross-runs/r-1", DispatchedAt: time.Now().UTC(), State: "running",
	}); err != nil {
		t.Fatal(err)
	}
	stubStatus(t, remote.RunStatus{DirExists: true, State: "running"}, nil)

	var out string
	if err := runCmdCapturing(t, &out, Verify(), "status"); err != nil {
		t.Fatalf("verify status: %v", err)
	}
	if !strings.Contains(out, "detachcmd") || !strings.Contains(out, "helicon") {
		t.Errorf("status did not list the run:\n%s", out)
	}

	calls := stubCancel(t, nil)
	if err := runCmdCapturing(t, &out, Verify(), "status", "--cancel", "detachcmd"); err != nil {
		t.Fatalf("verify status --cancel: %v", err)
	}
	if len(*calls) != 1 {
		t.Fatalf("--cancel did not reach the host: %d calls", len(*calls))
	}
	got, err := findDetachedRun(root, dir, "detachcmd")
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Error("--cancel through the command left the record behind")
	}
}
