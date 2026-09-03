package cmd

// previewHost: where lanes WOULD run, answered without committing to a run.
//
// Every test here is about the difference between an answer and an assumption.
// The run only ever reports locality after it has probed and is about to spawn,
// so its two-valued vocabulary — remote or local — is always earned. Preview can
// be asked when nothing has been probed at all, and the assertions below are
// mostly about what it must REFUSE to claim in that state.

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/project"
	"github.com/Rivil/dross/internal/remote"
)

// previewLanes is the matched-lane set for the fixture's declared lanes, in
// declaration order — what runTestLanes hands laneLocality after consent.
func previewLanes(t *testing.T) (string, string, []matchedLane) {
	t.Helper()
	root, err := FindRoot()
	if err != nil {
		t.Fatal(err)
	}
	p, err := project.Load(filepath.Join(root, project.File))
	if err != nil {
		t.Fatal(err)
	}
	var lanes []matchedLane
	for i, lane := range p.Runtime.TestLane {
		lanes = append(lanes, matchedLane{index: i, lane: lane})
	}
	return root, filepath.Dir(root), lanes
}

// appendPoolHost adds a second authorized host after the granted one, so the
// pool walk has somewhere to walk to.
func appendPoolHost(t *testing.T, root, host string) {
	t.Helper()
	path := filepath.Join(root, LocalFile)
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	body = append(body, []byte(fmt.Sprintf("\n[[remote_pool]]\nhost = %q\nworkdir = \"/srv/dross\"\n", host))...)
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
}

// transportProbe stubs the seam as a dead network, counting the calls.
func transportProbe(t *testing.T) *int {
	t.Helper()
	return fakeProbe(t, func(tg remote.Target, _ []string) (remote.Readiness, error) {
		return remote.Readiness{}, fmt.Errorf("dial %s: %w", tg.Host, remote.ErrTransport)
	})
}

// TestUnreachableHostIsUnresolvedNotAFailure is c-6's escape clause, and the
// reason preview can honour locked preview_exit_status at all.
//
// A verdict of `local` here would be the confident wrong answer: nothing probed
// this machine's suitability either, and a dead network is a fact about the
// host rather than about the file set the user asked about.
func TestUnreachableHostIsUnresolvedNotAFailure(t *testing.T) {
	grantedLaneFixture(t, goAndWebLanes)
	installLaneLookPath(t)
	transportProbe(t)
	root, repoDir, lanes := previewLanes(t)

	got, err := previewHost(root, repoDir, lanes, true)
	if err != nil {
		t.Fatalf("an unreachable host turned preview into a failure: %v", err)
	}
	if got.State != hostUnresolved {
		t.Errorf("state = %q, want unresolved", got.State)
	}
	if got.Host != "helicon" {
		t.Errorf("host = %q, want the configured helicon named", got.Host)
	}
}

// TestNoProbeNeverOpensAConnection: --no-probe's whole promise is the instant
// offline read.
//
// A probe "just to name the host" would break it while proving nothing the
// configured name does not already say — and would make the flag a lie on
// exactly the machine that has no network.
func TestNoProbeNeverOpensAConnection(t *testing.T) {
	grantedLaneFixture(t, goAndWebLanes)
	installLaneLookPath(t)
	calls := fakeProbe(t, func(remote.Target, []string) (remote.Readiness, error) {
		return remote.Readiness{Cores: 8}, nil
	})
	root, repoDir, lanes := previewLanes(t)

	got, err := previewHost(root, repoDir, lanes, false)
	if err != nil {
		t.Fatal(err)
	}
	if *calls != 0 {
		t.Errorf("--no-probe opened %d connection(s)", *calls)
	}
	if got.State != hostUnprobed {
		t.Errorf("state = %q, want unprobed", got.State)
	}
	if got.Host != "helicon" {
		t.Errorf("host = %q, want the configured helicon named", got.Host)
	}
}

// TestMissingRemoteToolFallsBackNamingTheBinary: preview reads laneLocality's
// verdicts rather than deriving its own.
//
// The assertion is on the exact laneFallbackLine, because that line carries all
// four things a fallback needs to be actionable — lane, binary, host and remedy
// — and a preview that reconstructed it would be a second answer nothing else
// validates.
func TestMissingRemoteToolFallsBackNamingTheBinary(t *testing.T) {
	grantedLaneFixture(t, goAndWebLanes)
	installLaneLookPath(t)
	fakeProbe(t, func(remote.Target, []string) (remote.Readiness, error) {
		return remote.Readiness{Cores: 8, Missing: []string{"pnpm"}}, nil
	})
	root, repoDir, lanes := previewLanes(t)

	got, err := previewHost(root, repoDir, lanes, true)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != hostProbed {
		t.Fatalf("state = %q, want probed", got.State)
	}
	if len(got.Lanes) != 2 {
		t.Fatalf("want 2 lane answers, got %d", len(got.Lanes))
	}
	if got.Lanes[0].Site != siteRemote {
		t.Errorf("the go lane is not remote — the host has go")
	}
	if got.Lanes[1].Site != siteLocal {
		t.Fatalf("the web lane is not local — the host lacks pnpm")
	}
	want := laneFallbackLine(lanes[1].lane, []string{"helicon"}, []string{"pnpm"})
	if got.Lanes[1].Note != want {
		t.Errorf("note = %q, want the run's own fallback line %q", got.Lanes[1].Note, want)
	}
	if !strings.Contains(want, "dross test lane install web") {
		t.Errorf("the fallback line names no remedy: %q", want)
	}
}

// TestRefusedLaneCarriesItsErrorRatherThanReturningIt: a lane neither machine
// can run is a finding, not a fault.
//
// Returning it would refuse the whole preview over one lane's missing binary,
// and the other lanes' derived lines — the thing the user asked for — would
// never print.
func TestRefusedLaneCarriesItsErrorRatherThanReturningIt(t *testing.T) {
	grantedLaneFixture(t, goAndWebLanes)
	installLaneLookPath(t, "pnpm")
	fakeProbe(t, func(remote.Target, []string) (remote.Readiness, error) {
		return remote.Readiness{Cores: 8, Missing: []string{"pnpm"}}, nil
	})
	root, repoDir, lanes := previewLanes(t)

	got, err := previewHost(root, repoDir, lanes, true)
	if err != nil {
		t.Fatalf("a refused lane was returned as a fault: %v", err)
	}
	if got.Lanes[1].Site != siteRefused {
		t.Fatalf("the web lane is %v, want refused — neither machine has pnpm", got.Lanes[1].Site)
	}
	if ExitCode(got.Lanes[1].Err) != exitToolchainMissing {
		t.Errorf("the refusal carries exit %d, want %d", ExitCode(got.Lanes[1].Err), exitToolchainMissing)
	}
}

// TestPreviewLandsOnTheGrantTheRunWouldUse: preview walks the pool the way the
// run walks it, through the same function.
//
// And it does so SILENTLY. The pool's notices belong to a run's transcript;
// printed from preview they would land on the first line of `--json`, so the
// quiet core is asserted here rather than left to the JSON task to discover.
func TestPreviewLandsOnTheGrantTheRunWouldUse(t *testing.T) {
	grantedLaneFixture(t, goAndWebLanes)
	installLaneLookPath(t)
	root, repoDir, lanes := previewLanes(t)
	appendPoolHost(t, root, "second")
	fakeProbe(t, func(tg remote.Target, _ []string) (remote.Readiness, error) {
		if tg.Host == "helicon" {
			return remote.Readiness{}, fmt.Errorf("dial helicon: %w", remote.ErrTransport)
		}
		return remote.Readiness{Cores: 4}, nil
	})

	var got previewLocality
	var perr error
	out := captureStdout(t, func() {
		got, perr = previewHost(root, repoDir, lanes, true)
	})
	if perr != nil {
		t.Fatal(perr)
	}
	if got.State != hostProbed || got.Host != "second" {
		t.Errorf("landed on %q (%s), want the second grant — the run would have used it", got.Host, got.State)
	}
	if strings.TrimSpace(out) != "" {
		t.Errorf("previewHost wrote to stdout, which would corrupt --json:\n%s", out)
	}
}

// TestUnresolvedHostNeverRefusesALane is the sharpest form of "preview must not
// claim what it has not measured".
//
// exitToolchainMissing means NEITHER machine has the binary. With the host
// unreachable, only this machine has been asked — so the absence is worth
// naming and the conviction is not available.
func TestUnresolvedHostNeverRefusesALane(t *testing.T) {
	grantedLaneFixture(t, goAndWebLanes)
	installLaneLookPath(t, "pnpm")
	transportProbe(t)
	root, repoDir, lanes := previewLanes(t)

	got, err := previewHost(root, repoDir, lanes, true)
	if err != nil {
		t.Fatal(err)
	}
	web := got.Lanes[1]
	if !strings.Contains(web.Text, "unresolved") {
		t.Errorf("text = %q, want the unresolved banner", web.Text)
	}
	if !strings.Contains(web.Text, "helicon") {
		t.Errorf("text = %q, does not name the configured host", web.Text)
	}
	if !strings.Contains(web.Text, "pnpm") {
		t.Errorf("text = %q, does not name pnpm as absent here", web.Text)
	}
	if strings.Contains(web.Text, "refused") {
		t.Errorf("text = %q convicts a host that never answered", web.Text)
	}
	if web.Err != nil {
		t.Errorf("the verdict carries %v — only a probe can convict the second machine", web.Err)
	}
	if errors.Is(web.Err, remote.ErrTransport) {
		t.Error("the transport failure leaked onto a lane verdict")
	}
}

// TestUnprobedHostNeverClaimsLocal is the mirror image: a lane whose every tool
// resolves here would run locally IF the host were out of the picture, and
// under --no-probe preview does not know that it is.
//
// Printing `local` would claim a fallback nothing proved, and the fallback line
// beside it would name a host that was never asked anything.
func TestUnprobedHostNeverClaimsLocal(t *testing.T) {
	grantedLaneFixture(t, goAndWebLanes)
	installLaneLookPath(t)
	root, repoDir, lanes := previewLanes(t)

	got, err := previewHost(root, repoDir, lanes, false)
	if err != nil {
		t.Fatal(err)
	}
	for i, l := range got.Lanes {
		if !strings.Contains(l.Text, "unresolved") {
			t.Errorf("lane %d text = %q, want unresolved", i, l.Text)
		}
		if strings.Contains(l.Text, "local") {
			t.Errorf("lane %d text = %q claims a fallback no probe proved", i, l.Text)
		}
		if l.Note != "" {
			t.Errorf("lane %d printed a fallback line for an unasked host: %q", i, l.Note)
		}
	}
}

// TestProbeFailureIsUnresolvedNotAFailure is the OTHER half of c-6's escape
// clause, and the one nothing reached until now.
//
// preflightRemote converts remote.ErrTransport into Fallback=true with a NIL
// error, so every transport-shaped fake lands on the `chosen == nil` branch. A
// host that ANSWERED and whose answer was a failure — bad workdir, a refused
// key, a probe command that exited non-zero — is the other way a grant fails to
// resolve, and it is the branch where propagating the error is the natural
// instinct. Propagating it is exactly what locked preview_exit_status forbids.
func TestProbeFailureIsUnresolvedNotAFailure(t *testing.T) {
	grantedLaneFixture(t, goAndWebLanes)
	installLaneLookPath(t)
	// Deliberately NOT wrapping remote.ErrTransport: that error is absorbed
	// into a fallback upstream and would never reach the branch under test.
	fakeProbe(t, func(tg remote.Target, _ []string) (remote.Readiness, error) {
		return remote.Readiness{}, fmt.Errorf("probe %s: exit status 127", tg.Host)
	})
	root, repoDir, lanes := previewLanes(t)

	got, err := previewHost(root, repoDir, lanes, true)
	if err != nil {
		t.Fatalf("a probe failure turned preview into a failure: %v", err)
	}
	if got.State != hostUnresolved {
		t.Errorf("state = %q, want unresolved", got.State)
	}
	if !strings.Contains(got.Why, "did not answer") {
		t.Errorf("why = %q, does not say the host did not answer", got.Why)
	}
	if !strings.Contains(got.Why, "exit status 127") {
		t.Errorf("why = %q, drops the underlying probe error", got.Why)
	}
	// The lanes survive the unresolved host: preview's derived lines are what
	// the user asked for, and a probe failure is not a reason to lose them.
	if len(got.Lanes) != 2 {
		t.Fatalf("want 2 lane answers, got %d", len(got.Lanes))
	}
	for i, l := range got.Lanes {
		if l.Resolved {
			t.Errorf("lane %d claims a resolved site over a host that failed", i)
		}
		if l.Err != nil {
			t.Errorf("lane %d carries %v — a probe failure convicts no lane", i, l.Err)
		}
	}
}

// TestProbeFailureNamesTheHostThatFailed pins WHICH machine the banner blames.
//
// pickRemoteTarget bails at the failing index rather than walking on, so the
// failing grant is not generally the last configured one. Naming
// targets[len(targets)-1] here printed a host that was never contacted, above a
// reason quoting a different host's error — the two halves of one line
// disagreeing. Asserted with the failure on the FIRST of two grants, which is
// the only arrangement where the right answer and the last index differ.
func TestProbeFailureNamesTheHostThatFailed(t *testing.T) {
	grantedLaneFixture(t, goAndWebLanes)
	installLaneLookPath(t)
	root, repoDir, lanes := previewLanes(t)
	appendPoolHost(t, root, "second")
	var asked []string
	fakeProbe(t, func(tg remote.Target, _ []string) (remote.Readiness, error) {
		asked = append(asked, tg.Host)
		if tg.Host == "helicon" {
			return remote.Readiness{}, fmt.Errorf("probe helicon: permission denied")
		}
		return remote.Readiness{Cores: 4}, nil
	})

	got, err := previewHost(root, repoDir, lanes, true)
	if err != nil {
		t.Fatalf("a probe failure turned preview into a failure: %v", err)
	}
	if got.Host != "helicon" {
		t.Errorf("host = %q, want helicon — the grant that actually failed", got.Host)
	}
	if !strings.Contains(got.Why, "permission denied") {
		t.Errorf("why = %q, drops helicon's error", got.Why)
	}
	// The walk must STOP at the failure. Reaching `second` would be the
	// laundering pickRemoteTarget's own comment rules out, and would make the
	// banner's host a matter of which machine answered last.
	if len(asked) != 1 || asked[0] != "helicon" {
		t.Errorf("probed %v, want [helicon] only — the walk ran past a real failure", asked)
	}
}
