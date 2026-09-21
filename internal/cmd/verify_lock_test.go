package cmd

import (
	"errors"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/mutation"
	"github.com/Rivil/dross/internal/project"
	"github.com/Rivil/dross/internal/remote"
)

// lockVerifyRepo is a phase with one recorded Go change, ready for `dross
// verify <id>` to run against a stub adapter. Returns the repo dir.
func lockVerifyRepo(t *testing.T, id string) string {
	t.Helper()
	dir := scopedVerifyRepo(t, id)
	phaseSpec(t, id)
	writeScopeFile(t, dir, "a.go", "package x\n\nfunc A() bool { return 1 > 0 }\n")
	mustGit(t, dir, "commit", "-qam", "phase edits a.go")
	if err := runCmd(t, Changes(), "record", id, "t-1", "--files", "a.go"); err != nil {
		t.Fatal(err)
	}
	mustSetBase(t, id, "base")
	return dir
}

// captureAdapterArgs installs an adapter seam that records the phase and
// wait it was handed and returns a local stub run.
func captureAdapterArgs(t *testing.T, a mutation.Adapter) (*string, *remote.WaitPolicy) {
	t.Helper()
	var phase string
	var wait remote.WaitPolicy
	prev := configuredAdaptersFn
	configuredAdaptersFn = func(_ *project.Project, _ string, _ bool, ph string, w remote.WaitPolicy) ([]mutation.Adapter, mutationTuning, error) {
		phase, wait = ph, w
		return []mutation.Adapter{a}, mutationTuning{}, nil
	}
	t.Cleanup(func() { configuredAdaptersFn = prev })
	return &phase, &wait
}

// busyAdapter is a remote leg that finds the host held under --no-wait.
type busyAdapter struct{ holder remote.Holder }

func (b *busyAdapter) Name() string              { return "gremlins" }
func (b *busyAdapter) Supports(file string) bool { return strings.HasSuffix(file, ".go") }
func (b *busyAdapter) Run([]string) (*mutation.Report, error) {
	return nil, &remote.BusyError{Host: "helicon", Holder: b.holder}
}

// TestVerifyStampsTheHolderIdentity: on a granted repo the tuning's target
// carries the project name, the phase and a fresh run id, waiting forever —
// a waiter on the host would otherwise print a blank (c-3).
func TestVerifyStampsTheHolderIdentity(t *testing.T) {
	root := wiringFixture(t, map[string]string{
		"mutation_remote_host":    "helicon",
		"mutation_remote_workdir": "/srv/dross",
	}, false)
	stubProbe(t, 32, nil)
	p := loadWiringProject(t, root)

	mt, err := resolveMutationTuning(p, root, "remote-host-mutex", remote.Forever)
	if err != nil {
		t.Fatalf("resolveMutationTuning: %v", err)
	}
	if mt.Target == nil {
		t.Fatal("no target on a granted repo")
	}
	h := mt.Target.Lock.Holder
	if h.Project != p.Project.Name || h.Project == "" {
		t.Errorf("holder project = %q, want the project.toml name %q", h.Project, p.Project.Name)
	}
	if h.Phase != "remote-host-mutex" {
		t.Errorf("holder phase = %q", h.Phase)
	}
	if !regexp.MustCompile(`^r-\d{8}-\d{6}$`).MatchString(h.RunID) {
		t.Errorf("holder run id = %q, want r-YYYYMMDD-HHMMSS", h.RunID)
	}
	if !mt.Target.Lock.Wait.Forever {
		t.Errorf("wait = %+v, want Forever", mt.Target.Lock.Wait)
	}
	// And the adapters built from it carry the same target.
	adapters, _, err := configuredAdaptersFor(p, root, false, "remote-host-mutex", remote.Forever)
	if err != nil {
		t.Fatal(err)
	}
	g := adapterByName(t, adapters, "gremlins").(*mutation.Gremlins)
	if g.Remote == nil || g.Remote.Lock.Holder.Phase != "remote-host-mutex" || g.Remote.Lock.Holder.RunID == "" {
		t.Errorf("the gremlins adapter's target lacks the holder: %+v", g.Remote)
	}
}

// TestNoWaitSetsZeroWait: --no-wait reaches the adapters as the zero policy;
// without it the run waits forever.
func TestNoWaitSetsZeroWait(t *testing.T) {
	lockVerifyRepo(t, "01-nowait")
	stub := &stubMutationAdapter{name: "gremlins", exts: []string{".go"},
		report: goReport(map[string]mutation.FileStat{"a.go": {Killed: 1}})}
	phase, wait := captureAdapterArgs(t, stub)

	captureStdout(t, func() {
		if err := runCmd(t, Verify(), "01-nowait", "--no-wait"); err != nil {
			t.Fatalf("verify --no-wait: %v", err)
		}
	})
	if *phase != "01-nowait" {
		t.Errorf("phase reached the adapters as %q", *phase)
	}
	if wait.Forever || wait.Max != 0 {
		t.Errorf("--no-wait reached the adapters as %+v, want the zero policy", *wait)
	}

	captureStdout(t, func() {
		if err := runCmd(t, Verify(), "01-nowait"); err != nil {
			t.Fatalf("verify: %v", err)
		}
	})
	if !wait.Forever {
		t.Errorf("without --no-wait the policy is %+v, want Forever", *wait)
	}
}

// TestBusyRefusalExitsFifteenAndWritesNothing: a held host under --no-wait
// is a refusal with its own code — distinct from every test.go code and the
// results band — naming the holder, and no artefact is written.
func TestBusyRefusalExitsFifteenAndWritesNothing(t *testing.T) {
	dir := lockVerifyRepo(t, "01-busy")
	root := filepath.Join(dir, RootDirName)
	holder := remote.Holder{Project: "other", Phase: "phase-b", RunID: "r-20260921-090000", PID: 7}
	useStubAdapter(t, &busyAdapter{holder: holder})

	var err error
	captureStdout(t, func() { err = runCmd(t, Verify(), "01-busy", "--no-wait") })
	if got := exitCodeOf(err); got != exitVerifyHostBusy {
		t.Fatalf("exit code = %d, want %d: %v", got, exitVerifyHostBusy, err)
	}
	if exitVerifyHostBusy == exitResultsGone || exitVerifyHostBusy <= exitToolchainMissing || (exitVerifyHostBusy >= exitResultsScheduled && exitVerifyHostBusy <= exitResultsGone) {
		t.Errorf("exit %d collides with the suite or results codes", exitVerifyHostBusy)
	}
	for _, want := range []string{"other/phase-b", "r-20260921-090000", "since"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal lacks %q: %v", want, err)
		}
	}
	if !errors.Is(err, remote.ErrHostBusy) {
		t.Errorf("the refusal lost its identity: %v", err)
	}
	assertNoArtefacts(t, root, "01-busy")
}

// TestNoWaitWithDetachIsRefused: the two flags have no coherent meaning
// together, and the refusal names both before any host is touched.
func TestNoWaitWithDetachIsRefused(t *testing.T) {
	detachCmdRepo(t, "detachcmd", mutationTuning{Target: detachTarget()})
	rec := &detachRecorder{}
	rec.install(t)

	err := runCmd(t, Verify(), "detachcmd", "--detach", "--no-wait")
	if err == nil {
		t.Fatal("--no-wait --detach was accepted")
	}
	for _, want := range []string{"--no-wait", "--detach"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not name %s: %v", want, err)
		}
	}
	if len(rec.order) != 0 {
		t.Errorf("the refusal still touched the host: %v", rec.order)
	}
}

// TestLocalVerifyNeverTouchesTheLock: an ungranted repo and a transport
// fallback both leave the tuning local — no Target, so no adapter can hold —
// and no --local flag exists to make a third way.
func TestLocalVerifyNeverTouchesTheLock(t *testing.T) {
	root := wiringFixture(t, nil, false)
	p := loadWiringProject(t, root)
	mt, err := resolveMutationTuning(p, root, "ph", remote.Forever)
	if err != nil {
		t.Fatal(err)
	}
	if mt.Target != nil {
		t.Errorf("an ungranted repo produced a target: %+v", mt.Target)
	}
	adapters, _, err := configuredAdaptersFor(p, root, false, "ph", remote.Forever)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range adapters {
		if targetOf(t, a) != nil {
			t.Errorf("ungranted: adapter %q carries a target", a.Name())
		}
	}

	root = wiringFixture(t, map[string]string{
		"mutation_remote_host":    "helicon",
		"mutation_remote_workdir": "/srv/dross",
	}, false)
	stubProbe(t, 0, remote.Classify("ssh", "helicon", 255))
	p = loadWiringProject(t, root)
	mt, err = resolveMutationTuning(p, root, "ph", remote.Forever)
	if err != nil {
		t.Fatal(err)
	}
	if mt.Target != nil || mt.FellBackFrom == "" {
		t.Errorf("a transport fallback kept a target: %+v", mt)
	}
	adapters, _, err = configuredAdaptersFor(p, root, false, "ph", remote.Forever)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range adapters {
		if targetOf(t, a) != nil {
			t.Errorf("fallback: adapter %q carries a target", a.Name())
		}
	}
	if Verify().Flags().Lookup("local") != nil {
		t.Error("`dross verify` grew a --local flag")
	}
}

// TestDrainStampsAHolder: the survivor drain is the second attached remote
// caller and holds the host like verify does — with no phase, but named.
func TestDrainStampsAHolder(t *testing.T) {
	root := wiringFixture(t, map[string]string{
		"mutation_remote_host":    "helicon",
		"mutation_remote_workdir": "/srv/dross",
	}, false)
	stubProbe(t, 32, nil)
	p := loadWiringProject(t, root)
	// The drain's exact call.
	mt, err := resolveMutationTuning(p, root, "", remote.Forever)
	if err != nil {
		t.Fatal(err)
	}
	g := mt.gremlins(filepath.Dir(root), p, nil)
	if g.Remote == nil || g.Remote.Lock.Holder.RunID == "" || g.Remote.Lock.Holder.Project != p.Project.Name {
		t.Errorf("the drain's target has no usable holder: %+v", g.Remote)
	}
	if g.Remote.Lock.Holder.Phase != "" || !g.Remote.Lock.Wait.Forever {
		t.Errorf("the drain's lock = %+v, want no phase and Forever", g.Remote.Lock)
	}
}

// TestNoWaitWithoutAHostIsInert: with nothing granted --no-wait changes
// nothing — a local run exiting 0.
func TestNoWaitWithoutAHostIsInert(t *testing.T) {
	lockVerifyRepo(t, "01-inert")
	stub := &stubMutationAdapter{name: "gremlins", exts: []string{".go"},
		report: goReport(map[string]mutation.FileStat{"a.go": {Killed: 1}})}
	captureAdapterArgs(t, stub)
	captureStdout(t, func() {
		if err := runCmd(t, Verify(), "01-inert", "--no-wait"); err != nil {
			t.Fatalf("verify --no-wait with no host: %v", err)
		}
	})
	if len(stub.got) == 0 {
		t.Error("the local run did not happen")
	}
}

// TestDetachedHolderIsTheRunID: through the command, the recorded run id is
// the run= the host sees, with the project and phase beside it — otherwise
// status could not tell waiting-on-me from waiting-on-another.
func TestDetachedHolderIsTheRunID(t *testing.T) {
	dir := detachCmdRepo(t, "detachcmd", mutationTuning{Target: detachTarget()})
	rec := &detachRecorder{}
	rec.install(t)

	if err := runCmd(t, Verify(), "detachcmd", "--detach"); err != nil {
		t.Fatalf("dross verify --detach: %v", err)
	}
	got, err := findDetachedRun(filepath.Join(dir, RootDirName), dir, "detachcmd")
	if err != nil || got == nil {
		t.Fatalf("no run recorded: %v", err)
	}
	if len(rec.scripts) != 1 {
		t.Fatalf("want one dispatch, got %d", len(rec.scripts))
	}
	script := rec.scripts[0]
	for _, want := range []string{`run='\''` + got.RunID + `'\''`, `project='\''`, `phase='\''detachcmd'\''`} {
		if !strings.Contains(script, want) {
			t.Errorf("the dispatched script lacks %s:\n%s", want, script)
		}
	}
	if rec.targets[0].Lock.Holder.RunID != got.RunID {
		t.Errorf("holder run id %q != recorded %q", rec.targets[0].Lock.Holder.RunID, got.RunID)
	}
}
