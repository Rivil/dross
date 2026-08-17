package cmd

import (
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/Rivil/dross/internal/remote"
)

// remoteRecorder replaces the remote-spawn seam and records every argv and
// script it was handed, in order. Order is half of what these tests assert: a
// run that reached the host before the sync measured the previous run's code.
type remoteRecorder struct {
	mu      sync.Mutex
	argvs   [][]string
	scripts []string
	err     error
}

func (r *remoteRecorder) spawn(argv []string, stdin string, _, _ io.Writer) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.argvs = append(r.argvs, append([]string(nil), argv...))
	r.scripts = append(r.scripts, stdin)
	return r.err
}

func (r *remoteRecorder) bins() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, 0, len(r.argvs))
	for _, a := range r.argvs {
		if len(a) > 0 {
			out = append(out, a[0])
		}
	}
	return out
}

func installRemoteRecorder(t *testing.T, err error) *remoteRecorder {
	t.Helper()
	rec := &remoteRecorder{err: err}
	orig := spawnRemote
	t.Cleanup(func() { spawnRemote = orig })
	spawnRemote = rec.spawn
	return rec
}

// grantedTestFixture is a consented repo with a remote granted, which is the
// state every test here is about.
//
// The probe seam is stubbed REACHABLE by default. The run now preflights the
// host before pushing anything, so a fixture that left the seam live would send
// a real ssh at a host named "helicon" and fall back to local — turning every
// test here into a local run. A test that wants an unreachable host replaces
// the seam after calling this.
func grantedTestFixture(t *testing.T, testCmd string) string {
	t.Helper()
	testFixture(t, testCmd)
	trustFixture(t)
	fakeProbe(t, func(remote.Target, []string) (remote.Readiness, error) {
		return remote.Readiness{Cores: 8}, nil
	})
	root, err := FindRoot()
	if err != nil {
		t.Fatal(err)
	}
	if err := runCmd(t, Remote(), "grant", "helicon", "/srv/dross"); err != nil {
		t.Fatalf("dross remote grant: %v", err)
	}
	return root
}

// TestGrantedHostRunsRemotely is c-2: with a grant, the suite runs there and
// nothing spawns here.
//
// The local spawn count is the criterion, stated directly. "It also ran
// remotely" would be satisfied by a run that did both, and doing both is not
// offload.
func TestGrantedHostRunsRemotely(t *testing.T) {
	grantedTestFixture(t, "go test -count=1 ./...")
	local := installSpawnRecorder(t, nil)
	rem := installRemoteRecorder(t, nil)

	if err := runCmd(t, Test()); err != nil {
		t.Fatalf("dross test: %v", err)
	}
	if n := local.count(); n != 0 {
		t.Errorf("%d test process(es) spawned locally — with a grant the laptop must stay free", n)
	}
	bins := rem.bins()
	if len(bins) == 0 {
		t.Fatal("nothing was spawned remotely either — the run vanished")
	}
	if bins[len(bins)-1] != "ssh" {
		t.Errorf("the suite was not run over ssh: %v", bins)
	}
}

// TestLocalFlagForcesLocalRun: the escape hatch. A remote that is down or a
// tree mid-edit must always be answerable with "run it here".
func TestLocalFlagForcesLocalRun(t *testing.T) {
	grantedTestFixture(t, "go test -count=1 ./...")
	local := installSpawnRecorder(t, nil)
	rem := installRemoteRecorder(t, nil)

	if err := runCmd(t, Test(), "--local"); err != nil {
		t.Fatalf("dross test --local: %v", err)
	}
	if local.count() != 1 {
		t.Errorf("--local spawned %d local command(s), want 1", local.count())
	}
	if bins := rem.bins(); len(bins) != 0 {
		t.Errorf("--local still opened a connection: %v", bins)
	}
}

// TestRemoteRunSyncsBeforeExecuting: running before the sync measures whatever
// the remote tree happened to hold, which is the previous run's code — a green
// that says nothing about the change in hand.
func TestRemoteRunSyncsBeforeExecuting(t *testing.T) {
	grantedTestFixture(t, "go test -count=1 ./...")
	installSpawnRecorder(t, nil)
	rem := installRemoteRecorder(t, nil)

	if err := runCmd(t, Test()); err != nil {
		t.Fatalf("dross test: %v", err)
	}
	bins := rem.bins()
	if len(bins) < 2 {
		t.Fatalf("expected a sync and a run, got %v", bins)
	}
	if bins[0] != "rsync" {
		t.Errorf("the first remote step was %q, want rsync — the tree must be pushed before the suite runs", bins[0])
	}
	if bins[1] != "ssh" {
		t.Errorf("the second remote step was %q, want ssh", bins[1])
	}
}

// TestSelectorReachesTheRemoteUnchanged is c-3's other half: the same selector
// must mean the same thing on both transports, or a targeted re-run measures
// different code depending on where it ran.
func TestSelectorReachesTheRemoteUnchanged(t *testing.T) {
	grantedTestFixture(t, "go test -count=1")
	installSpawnRecorder(t, nil)
	rem := installRemoteRecorder(t, nil)

	if err := runCmd(t, Test(), "./internal/cmd/..."); err != nil {
		t.Fatalf("dross test: %v", err)
	}

	rem.mu.Lock()
	defer rem.mu.Unlock()
	var script string
	for i, a := range rem.argvs {
		if len(a) > 0 && a[0] == "ssh" {
			script = rem.scripts[i]
		}
	}
	if script == "" {
		t.Fatal("no ssh script was sent")
	}
	if !strings.Contains(script, "./internal/cmd/...") {
		t.Errorf("the selector did not reach the remote script:\n%s", script)
	}
	if !strings.Contains(script, "go test -count=1") {
		t.Errorf("the consented command did not reach the remote script:\n%s", script)
	}
	// The script must cd into the granted workdir; without it the suite runs in
	// whatever directory the login shell starts in.
	if !strings.Contains(script, "/srv/dross") {
		t.Errorf("the script does not cd into the granted workdir:\n%s", script)
	}
}

// TestUnsafeTargetIsRefusedBeforeSpawn: the argv builders return an error
// INSTEAD of an argv for a target that would not be safe to hand to a shell, so
// an unsafe host cannot reach one.
//
// The grant verb validates too, so this fixture writes the store directly —
// which is also the real path such a value would arrive by, since local.toml is
// hand-editable.
func TestUnsafeTargetIsRefusedBeforeSpawn(t *testing.T) {
	testFixture(t, "go test -count=1")
	trustFixture(t)
	root, err := FindRoot()
	if err != nil {
		t.Fatal(err)
	}
	writeLocalStore(t, root, "remote_host = \"-oProxyCommand=touch /tmp/pwned\"\nremote_workdir = \"/srv/dross\"\n")

	local := installSpawnRecorder(t, nil)
	rem := installRemoteRecorder(t, nil)

	if err := runCmd(t, Test()); err == nil {
		t.Fatal("an unsafe host was accepted")
	}
	if rem.bins() != nil && len(rem.bins()) != 0 {
		t.Errorf("an unsafe host reached a spawn: %v", rem.bins())
	}
	if local.count() != 0 {
		t.Errorf("the refusal fell back to a local run — a refused remote is not a reason to run somewhere else")
	}
}

// TestNoGrantRunsLocally: the ordinary case. Most repos have no remote, and
// that is not a failure.
func TestNoGrantRunsLocally(t *testing.T) {
	testFixture(t, "go test -count=1")
	trustFixture(t)
	local := installSpawnRecorder(t, nil)
	rem := installRemoteRecorder(t, nil)

	if err := runCmd(t, Test()); err != nil {
		t.Fatalf("dross test with no grant: %v", err)
	}
	if local.count() != 1 {
		t.Errorf("spawned %d local command(s), want 1", local.count())
	}
	if len(rem.bins()) != 0 {
		t.Errorf("a repo with no grant opened a connection: %v", rem.bins())
	}
}

// TestRunRemoteCommandPipesTheScript exercises the real spawn function rather
// than the seam every other test here replaces.
//
// Without it the `stdin != ""` guard is never executed by anything: negating it
// would stop piping the script to ssh — the script being the ONLY thing that
// distinguishes one remote invocation from another, since the argv is the same
// four elements every time — and every test above would still pass, because
// they all record the argv instead of running it.
//
// `sh -c cat` stands in for ssh: same shape (a command that reads its work from
// stdin), no network.
func TestRunRemoteCommandPipesTheScript(t *testing.T) {
	var out strings.Builder
	if err := runRemoteCommand([]string{"sh", "-c", "cat"}, "the script\n", &out, &out); err != nil {
		t.Fatalf("runRemoteCommand: %v", err)
	}
	if got := out.String(); !strings.Contains(got, "the script") {
		t.Errorf("the script was not piped to the command's stdin, got %q", got)
	}
}

// TestRunRemoteCommandWithoutScriptReadsNoStdin is the other side of the same
// guard: the sync leg passes no script and must not be left waiting on a
// stdin nobody closes.
func TestRunRemoteCommandWithoutScriptReadsNoStdin(t *testing.T) {
	var out strings.Builder
	if err := runRemoteCommand([]string{"sh", "-c", "cat; echo done"}, "", &out, &out); err != nil {
		t.Fatalf("runRemoteCommand: %v", err)
	}
	if got := out.String(); !strings.Contains(got, "done") {
		t.Errorf("a command with no script did not complete, got %q", got)
	}
}
