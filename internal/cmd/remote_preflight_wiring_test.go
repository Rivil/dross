package cmd

import (
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/remote"
	"github.com/Rivil/dross/internal/verify"
)

// orderRecorder notes the sequence in which the probe seam and the remote-spawn
// seam were reached. Order is the property under test: probing AFTER the sync
// discovers an unreachable host having already paid for the transfer, and a
// transport failure at that point is indistinguishable from the suite dying.
type orderRecorder struct{ events []string }

func (o *orderRecorder) install(t *testing.T, probeErr error) {
	t.Helper()
	origProbe := remoteProbeFn
	remoteProbeFn = func(remote.Target, []string) (remote.Readiness, error) {
		o.events = append(o.events, "probe")
		if probeErr != nil {
			return remote.Readiness{}, probeErr
		}
		return remote.Readiness{Cores: 8}, nil
	}
	t.Cleanup(func() { remoteProbeFn = origProbe })

	origSpawn := spawnRemote
	spawnRemote = func(argv []string, _ string, _, _ io.Writer) error {
		bin := ""
		if len(argv) > 0 {
			bin = argv[0]
		}
		o.events = append(o.events, "remote:"+bin)
		return nil
	}
	t.Cleanup(func() { spawnRemote = origSpawn })

	origLocal := spawnLocal
	spawnLocal = func(string, string, io.Writer, io.Writer) error {
		o.events = append(o.events, "local")
		return nil
	}
	t.Cleanup(func() { spawnLocal = origLocal })
}

func (o *orderRecorder) count(prefix string) int {
	n := 0
	for _, e := range o.events {
		if strings.HasPrefix(e, prefix) {
			n++
		}
	}
	return n
}

// TestTestPreflightsBeforeSync: the probe must come first, ahead of every
// rsync/ssh invocation.
func TestTestPreflightsBeforeSync(t *testing.T) {
	grantedTestFixture(t, "go test ./...")
	rec := &orderRecorder{}
	rec.install(t, nil)

	if err := runCmd(t, Test()); err != nil {
		t.Fatalf("dross test: %v", err)
	}

	if len(rec.events) == 0 {
		t.Fatal("nothing was recorded — the seams are not the ones the run uses")
	}
	if rec.events[0] != "probe" {
		t.Errorf("first event was %q, want the probe — the tree was pushed before the host was checked:\n%v", rec.events[0], rec.events)
	}
	if rec.count("remote:") == 0 {
		t.Errorf("a reachable host never ran anything remotely: %v", rec.events)
	}
	if rec.count("local") != 0 {
		t.Errorf("a reachable host also spawned locally: %v", rec.events)
	}
}

// TestTestFallsBackToLocal: an unreachable host must not fail the run. The
// local machine can still produce an answer, and refusing to is what forced
// `dross remote revoke` as a workaround when helicon was down for hours.
func TestTestFallsBackToLocal(t *testing.T) {
	grantedTestFixture(t, "go test ./...")
	rec := &orderRecorder{}
	rec.install(t, fmt.Errorf("ssh: connect to host helicon port 22: %w", remote.ErrTransport))

	var err error
	out := captureStdout(t, func() { err = runCmd(t, Test()) })
	if err != nil {
		t.Fatalf("an unreachable host failed the run instead of falling back: %v\n%s", err, out)
	}

	if rec.count("local") != 1 {
		t.Errorf("the suite did not run locally after the fallback: %v", rec.events)
	}
	if rec.count("remote:") != 0 {
		t.Errorf("the tree was pushed to a host that could not be reached: %v", rec.events)
	}
}

// TestTestFallbackIsAnnounced: a fallback the output does not mention leaves a
// local result indistinguishable from a remote one — the exact failure c-5
// names.
func TestTestFallbackIsAnnounced(t *testing.T) {
	grantedTestFixture(t, "go test ./...")
	rec := &orderRecorder{}
	rec.install(t, fmt.Errorf("no route to host: %w", remote.ErrTransport))

	var err error
	out := captureStdout(t, func() { err = runCmd(t, Test()) })
	if err != nil {
		t.Fatalf("dross test: %v\n%s", err, out)
	}

	if !strings.Contains(out, "helicon") {
		t.Errorf("the fallback does not name the host it could not reach:\n%s", out)
	}
	if !strings.Contains(strings.ToLower(out), "local") {
		t.Errorf("the fallback does not say the run happened locally:\n%s", out)
	}
	_ = rec
}

// TestTestRemoteCommandFailureIsNotAFallback: the other half of the locked
// fallback_policy. A host that RAN the probe and failed it has given an answer,
// and re-running locally in the hope of a different one launders a real failure.
func TestTestRemoteCommandFailureIsNotAFallback(t *testing.T) {
	grantedTestFixture(t, "go test ./...")
	rec := &orderRecorder{}
	rec.install(t, fmt.Errorf("getconf: %w", remote.ErrRemoteCommand))

	var err error
	out := captureStdout(t, func() { err = runCmd(t, Test()) })
	if err == nil {
		t.Fatalf("a failing remote command was silently retried locally:\n%s", out)
	}
	if rec.count("local") != 0 {
		t.Errorf("the run fell back on a non-transport failure: %v", rec.events)
	}
}

// TestTestLocalFlagSkipsPreflight: --local has no host to preflight, so probing
// would be a round trip bought for nothing — and would fail the run on a host
// the user explicitly said not to use.
func TestTestLocalFlagSkipsPreflight(t *testing.T) {
	grantedTestFixture(t, "go test ./...")
	rec := &orderRecorder{}
	rec.install(t, fmt.Errorf("unreachable: %w", remote.ErrTransport))

	if err := runCmd(t, Test(), "--local"); err != nil {
		t.Fatalf("dross test --local: %v", err)
	}

	if rec.count("probe") != 0 {
		t.Errorf("--local probed the host anyway: %v", rec.events)
	}
	if rec.count("local") != 1 {
		t.Errorf("--local did not run the suite here: %v", rec.events)
	}
}

// TestVerifyPreflightsRemote: the mutation path takes the same route. A
// preflight wired into `dross test` alone would leave the longer, more
// expensive run discovering an unreachable host part-way through.
func TestVerifyPreflightsRemote(t *testing.T) {
	root := grantedTestFixture(t, "go test ./...")
	p := loadWiringProject(t, root)

	calls := fakeProbe(t, func(remote.Target, []string) (remote.Readiness, error) {
		return remote.Readiness{Cores: 8}, nil
	})
	adapters, tuning, err := configuredAdapters(p, root, false)
	if err != nil {
		t.Fatalf("configuredAdapters: %v", err)
	}
	if *calls == 0 {
		t.Error("the mutation path never probed the host")
	}
	if tuning.Target == nil {
		t.Fatal("a reachable host produced a local tuning")
	}
	if got := measuredOnOf(adapters, tuning); got != "helicon" {
		t.Errorf("measured_on = %q, want the host", got)
	}
}

// TestVerifyFallsBackToLocal: the mutation run falls back too, and records that
// it did. A verify that aborted here would leave the phase unmeasurable for the
// duration of an outage.
func TestVerifyFallsBackToLocal(t *testing.T) {
	root := grantedTestFixture(t, "go test ./...")
	p := loadWiringProject(t, root)

	fakeProbe(t, func(remote.Target, []string) (remote.Readiness, error) {
		return remote.Readiness{}, fmt.Errorf("connection refused: %w", remote.ErrTransport)
	})
	adapters, tuning, err := configuredAdapters(p, root, false)
	if err != nil {
		t.Fatalf("an unreachable host aborted the mutation run: %v", err)
	}
	if tuning.Target != nil {
		t.Error("the fallback still pointed the adapters at the host")
	}
	if tuning.FellBackFrom != "helicon" {
		t.Errorf("FellBackFrom = %q, want helicon", tuning.FellBackFrom)
	}
	if tuning.FallbackWhy == "" {
		t.Error("the fallback carries no reason")
	}

	// And the record says so: not a clean local run, not a remote one.
	got := measuredOnOf(adapters, tuning)
	if !strings.Contains(got, "helicon") || !strings.Contains(got, "local") {
		t.Errorf("measured_on = %q, want both the host and local", got)
	}
	if got == verify.MeasuredLocally() {
		t.Error("a fallback was recorded as an ordinary local run")
	}
}

// TestVerifyRemoteCommandFailureStillAborts: the mutation path keeps refusing a
// host that answered and failed. Nothing was measured, and a local re-run would
// hide it.
func TestVerifyRemoteCommandFailureStillAborts(t *testing.T) {
	root := grantedTestFixture(t, "go test ./...")
	p := loadWiringProject(t, root)

	fakeProbe(t, func(remote.Target, []string) (remote.Readiness, error) {
		return remote.Readiness{}, fmt.Errorf("getconf: %w", remote.ErrRemoteCommand)
	})
	if _, _, err := configuredAdapters(p, root, false); err == nil {
		t.Fatal("a failing remote command produced adapters instead of an error")
	}
}
