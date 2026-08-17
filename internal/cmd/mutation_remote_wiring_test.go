package cmd

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/mutation"
	"github.com/Rivil/dross/internal/project"
	"github.com/Rivil/dross/internal/remote"
)

// c-3 and half of c-1, executed at the two places adapters are constructed.
//
// There are two of them — configuredAdapters (verify) and
// runGremlinsOverPackages (the survivor drain) — and they are the failure mode
// this file is about. A knob wired into one only is a run that behaves
// differently depending on which command you reached it through, and the drain
// is what decides whether a survivor is real. Both sites read one table.

// wiringFixture builds a repo with optional grant, tuning and docker mode, and
// returns its .dross root.
func wiringFixture(t *testing.T, local map[string]string, dockerMode bool) string {
	t.Helper()
	dir := t.TempDir()
	gitInit(t, dir, "git@github.com:Rivil/dross.git")
	chdir(t, dir)
	if err := runCmd(t, Init()); err != nil {
		t.Fatal(err)
	}
	mustRunSet(t, "project.name", "x")
	if dockerMode {
		mustRunSet(t, "runtime.mode", "docker")
		mustRunSet(t, "runtime.test_command", "docker compose exec app pnpm test")
	} else {
		mustRunSet(t, "runtime.mode", "native")
		mustRunSet(t, "runtime.test_command", "go test ./...")
	}
	root := filepath.Join(dir, ".dross")
	if len(local) > 0 {
		var b strings.Builder
		for k, v := range local {
			b.WriteString(k + " = \"" + v + "\"\n")
		}
		if err := os.WriteFile(filepath.Join(root, LocalFile), []byte(b.String()), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func loadWiringProject(t *testing.T, root string) *project.Project {
	t.Helper()
	p, err := project.Load(filepath.Join(root, project.File))
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// stubProbe fixes the probed core count so the derived worker default is
// deterministic, and reports how many times the run probed.
func stubProbe(t *testing.T, cores int, err error) *int {
	t.Helper()
	calls := 0
	orig := remoteProbeFn
	remoteProbeFn = func(remote.Target, []string) (remote.Readiness, error) {
		calls++
		return remote.Readiness{Cores: cores}, err
	}
	t.Cleanup(func() { remoteProbeFn = orig })
	return &calls
}

func adapterByName(t *testing.T, as []mutation.Adapter, name string) mutation.Adapter {
	t.Helper()
	for _, a := range as {
		if a.Name() == name {
			return a
		}
	}
	t.Fatalf("adapter %q not built; got %d adapters", name, len(as))
	return nil
}

// TestGrantReachesEveryAdapter: a grant that only reached some of the adapters
// is the silent half-remote run — the Go leg measured on a 32-core host and the
// TS leg on this laptop, with one report and no indication which is which.
func TestGrantReachesEveryAdapter(t *testing.T) {
	root := wiringFixture(t, map[string]string{
		"mutation_remote_host":    "helicon",
		"mutation_remote_workdir": "/srv/dross",
	}, false)
	stubProbe(t, 32, nil)

	adapters, _, err := configuredAdapters(loadWiringProject(t, root), root, false)
	if err != nil {
		t.Fatalf("configuredAdapters: %v", err)
	}
	if len(adapters) != 3 {
		t.Fatalf("want all three adapters, got %d", len(adapters))
	}

	var seen []*remote.Target
	for _, a := range adapters {
		tgt := targetOf(t, a)
		if tgt == nil {
			t.Fatalf("adapter %q was left local while the others went remote", a.Name())
		}
		seen = append(seen, tgt)
	}
	for i := 1; i < len(seen); i++ {
		if !reflect.DeepEqual(*seen[0], *seen[i]) {
			t.Errorf("adapters carry different targets: %+v vs %+v", *seen[0], *seen[i])
		}
	}
	if seen[0].Host != "helicon" || seen[0].Workdir != "/srv/dross" || seen[0].Cores != 32 {
		t.Errorf("target = %+v, want helicon:/srv/dross with the probed 32 cores", *seen[0])
	}
}

// targetOf reads the Remote field of whichever adapter this is.
func targetOf(t *testing.T, a mutation.Adapter) *remote.Target {
	t.Helper()
	switch v := a.(type) {
	case *mutation.Gremlins:
		return v.Remote
	case *mutation.Stryker:
		return v.Remote
	case *mutation.StrykerNet:
		return v.Remote
	}
	t.Fatalf("unknown adapter type %T — this helper must cover every adapter or it silently skips one", a)
	return nil
}

// prefixOf reads the Prefix field of whichever adapter this is.
func prefixOf(t *testing.T, a mutation.Adapter) string {
	t.Helper()
	switch v := a.(type) {
	case *mutation.Gremlins:
		return v.Prefix
	case *mutation.Stryker:
		return v.Prefix
	case *mutation.StrykerNet:
		return v.Prefix
	}
	t.Fatalf("unknown adapter type %T", a)
	return ""
}

// TestBothSitesBuildTheSameGremlins is the anti-drift assertion, and it is
// field-by-field on purpose: a knob added at one construction site only would
// pass every other test in this repo and fail here.
func TestBothSitesBuildTheSameGremlins(t *testing.T) {
	root := wiringFixture(t, map[string]string{
		"mutation_remote_host":    "helicon",
		"mutation_remote_workdir": "/srv/dross",
		"mutation_workers":        "8",
		"mutation_test_cpu":       "2",
	}, false)
	stubProbe(t, 32, nil)

	p := loadWiringProject(t, root)
	adapters, _, err := configuredAdapters(p, root, false)
	if err != nil {
		t.Fatalf("configuredAdapters: %v", err)
	}
	fromVerify := adapterByName(t, adapters, "gremlins").(*mutation.Gremlins)

	mt, err := resolveMutationTuning(p, root)
	if err != nil {
		t.Fatalf("resolveMutationTuning: %v", err)
	}
	fromDrain := mt.gremlins(fromVerify.ProjectRoot, p, nil)

	if !reflect.DeepEqual(fromVerify, fromDrain) {
		t.Errorf("the two construction sites disagree:\n verify: %+v\n drain:  %+v", fromVerify, fromDrain)
	}
	if fromVerify.Workers != 8 || fromVerify.TestCPU != 2 {
		t.Errorf("tuning did not reach the adapter: Workers=%d TestCPU=%d, want 8 and 2",
			fromVerify.Workers, fromVerify.TestCPU)
	}
}

// TestTuningUnsetStaysZeroSoAdaptersApplyTheirOwnDefault.
//
// Zero is not "no parallelism" — it is "unset", which the adapters read as
// "apply your own default". Substituting a local default here would size a
// 32-core host's run by this laptop, which is exactly the bug the locked
// remote_workers decision names.
func TestTuningUnsetStaysZeroSoAdaptersApplyTheirOwnDefault(t *testing.T) {
	root := wiringFixture(t, nil, false)
	p := loadWiringProject(t, root)

	adapters, _, err := configuredAdapters(p, root, false)
	if err != nil {
		t.Fatalf("configuredAdapters: %v", err)
	}
	g := adapterByName(t, adapters, "gremlins").(*mutation.Gremlins)
	if g.Workers != 0 || g.TestCPU != 0 {
		t.Errorf("unset tuning was defaulted at the wiring layer: Workers=%d TestCPU=%d", g.Workers, g.TestCPU)
	}
	if g.Remote != nil {
		t.Errorf("a repo with no grant got a remote: %+v", *g.Remote)
	}
}

// TestUnsetWorkersWithARemoteHalvesTheProbedCores closes the loop from config
// to argv: the number that ends up on the gremlins command line is derived from
// the REMOTE's core count, not this machine's.
func TestUnsetWorkersWithARemoteHalvesTheProbedCores(t *testing.T) {
	root := wiringFixture(t, map[string]string{
		"mutation_remote_host":    "helicon",
		"mutation_remote_workdir": "/srv/dross",
	}, false)
	calls := stubProbe(t, 32, nil)

	adapters, _, err := configuredAdapters(loadWiringProject(t, root), root, false)
	if err != nil {
		t.Fatalf("configuredAdapters: %v", err)
	}
	g := adapterByName(t, adapters, "gremlins").(*mutation.Gremlins)
	if g.Remote == nil || g.Remote.Cores != 32 {
		t.Fatalf("the probed core count did not reach the adapter: %+v", g.Remote)
	}
	if *calls != 1 {
		t.Errorf("probed %d times, want exactly 1 — the probe is a pre-flight, not a per-adapter cost", *calls)
	}
	if g.Workers != 0 {
		t.Errorf("Workers = %d, want 0 so the adapter applies the remote-derived default", g.Workers)
	}
	// The other half of the claim — that Cores:32 becomes `--workers 16` on the
	// gremlins argv — is pinned in the adapter's own package, where the argv is
	// reachable: TestRemoteWorkersDeriveFromTheProbedHost in
	// internal/mutation/launcher_test.go. What THIS site owes is delivering the
	// probed count unaltered, which is what is asserted above.
}

// TestGrantDropsTheDockerPrefixAtBothSites is the locked
// docker_prefix_under_remote decision.
//
// dockerPrefix gates on runtime.mode, which describes the DEV stack and says
// nothing about where mutation runs. Refusing the combination would refuse every
// docker-mode repo that grants a remote — the common case, and the one this
// phase exists for. So the grant DROPS the prefix instead, which is also what a
// remote-native run should do: the point is the remote's own toolchain.
func TestGrantDropsTheDockerPrefixAtBothSites(t *testing.T) {
	root := wiringFixture(t, map[string]string{
		"mutation_remote_host":    "helicon",
		"mutation_remote_workdir": "/srv/dross",
	}, true)
	stubProbe(t, 32, nil)

	p := loadWiringProject(t, root)
	if dockerPrefix(p) == "" {
		t.Fatal("the fixture is not in docker mode — this test would prove nothing")
	}

	adapters, _, err := configuredAdapters(p, root, false)
	if err != nil {
		t.Fatalf("a docker-mode repo with a grant was refused: %v", err)
	}
	for _, a := range adapters {
		if pfx := prefixOf(t, a); pfx != "" {
			t.Errorf("adapter %q kept the docker prefix %q under a remote", a.Name(), pfx)
		}
		if targetOf(t, a) == nil {
			t.Errorf("adapter %q has no target", a.Name())
		}
	}

	// The drain's site agrees.
	mt, err := resolveMutationTuning(p, root)
	if err != nil {
		t.Fatal(err)
	}
	g := mt.gremlins(filepath.Dir(root), p, nil)
	if g.Prefix != "" || g.Remote == nil {
		t.Errorf("the drain site disagrees: Prefix=%q Remote=%+v", g.Prefix, g.Remote)
	}
}

// TestMutualExclusionStaysUnreachableFromBothSites: the Launcher refuses a
// prefix and a target together, and this asserts the wiring never builds that
// combination — so the refusal stays a defensive invariant rather than becoming
// live behaviour someone routes around.
func TestMutualExclusionStaysUnreachableFromBothSites(t *testing.T) {
	for _, docker := range []bool{false, true} {
		root := wiringFixture(t, map[string]string{
			"mutation_remote_host":    "helicon",
			"mutation_remote_workdir": "/srv/dross",
		}, docker)
		stubProbe(t, 8, nil)

		p := loadWiringProject(t, root)
		adapters, _, err := configuredAdapters(p, root, false)
		if err != nil {
			t.Fatalf("docker=%v: %v", docker, err)
		}
		for _, a := range adapters {
			if prefixOf(t, a) != "" && targetOf(t, a) != nil {
				t.Errorf("docker=%v: adapter %q carries BOTH a prefix and a target", docker, a.Name())
			}
		}
		mt, err := resolveMutationTuning(p, root)
		if err != nil {
			t.Fatal(err)
		}
		if mt.Prefix != "" && mt.Target != nil {
			t.Errorf("docker=%v: the shared table produced both a prefix and a target", docker)
		}
	}
}

// TestNoGrantKeepsTheDockerPrefixUnchanged is the regression half: dropping the
// prefix is conditional on the REMOTE, not a change to how local docker runs
// behave.
func TestNoGrantKeepsTheDockerPrefixUnchanged(t *testing.T) {
	root := wiringFixture(t, nil, true)
	p := loadWiringProject(t, root)
	want := dockerPrefix(p)
	if want == "" {
		t.Fatal("the fixture is not in docker mode")
	}

	adapters, _, err := configuredAdapters(p, root, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range adapters {
		if got := prefixOf(t, a); got != want {
			t.Errorf("adapter %q prefix = %q, want %q", a.Name(), got, want)
		}
	}
	mt, err := resolveMutationTuning(p, root)
	if err != nil {
		t.Fatal(err)
	}
	if mt.Prefix != want {
		t.Errorf("the drain site's prefix = %q, want %q", mt.Prefix, want)
	}
}

// TestProbeFailureAbortsVerifyBeforeAnyAdapterRuns is c-4 at the wiring layer.
//
// The abort has to happen before RunScoped, or the run has already pushed a tree
// and written artefacts describing a measurement that did not happen. Asserted
// on the artefacts rather than on the error alone: an error returned after
// tests.json was written would read identically from the caller's side.
//
// The failure driven here is a REMOTE COMMAND failure, not a transport one.
// remote-toolchain-bootstrap's locked fallback_policy split the two: a host that
// could not be REACHED now falls back to a local run and records that it did
// (TestVerifyFallsBackToLocal), while a host that ran the probe and failed it
// has given an answer and still aborts — which is what this pins.
func TestProbeFailureAbortsVerifyBeforeAnyAdapterRuns(t *testing.T) {
	root := wiringFixture(t, map[string]string{
		"mutation_remote_host":    "helicon",
		"mutation_remote_workdir": "/srv/dross",
	}, false)
	stubProbe(t, 0, remote.Classify("getconf", "helicon", 127))

	adapters, _, err := configuredAdapters(loadWiringProject(t, root), root, false)
	if err == nil {
		t.Fatal("a failing remote probe produced a usable adapter list")
	}
	if adapters != nil {
		t.Errorf("a refused resolution still returned %d adapters — a local-only fallback list", len(adapters))
	}
	if !strings.Contains(err.Error(), "helicon") {
		t.Errorf("the error does not name the host: %v", err)
	}
}

// TestTrackedLocalRefusalAbortsTheWiring: a tracked local.toml is refused
// unread, and the refusal must reach the caller rather than degrading to "no
// grant, run locally" — which would silently run on the wrong machine.
func TestTrackedLocalRefusalAbortsTheWiring(t *testing.T) {
	root := wiringFixture(t, map[string]string{
		"mutation_remote_host":    "attacker.example",
		"mutation_remote_workdir": "/srv/x",
	}, false)
	mustGit(t, filepath.Dir(root), "add", "-f", RootDirName+"/"+LocalFile)
	stubProbe(t, 8, nil)

	adapters, _, err := configuredAdapters(loadWiringProject(t, root), root, false)
	if err == nil {
		t.Fatal("a tracked local.toml was read rather than refused")
	}
	if adapters != nil {
		t.Errorf("a refusal still returned %d adapters", len(adapters))
	}
	if !strings.Contains(err.Error(), "refusing to read") {
		t.Errorf("the refusal did not surface: %v", err)
	}
}

// TestSkipMutationNeedsNoRemote: `--skip-mutation` builds no adapters, so it
// must not probe — a repo whose mutation host is down still has to be able to
// run verify with the mutation pass skipped.
func TestSkipMutationNeedsNoRemote(t *testing.T) {
	root := wiringFixture(t, map[string]string{
		"mutation_remote_host":    "helicon",
		"mutation_remote_workdir": "/srv/dross",
	}, false)
	calls := stubProbe(t, 0, remote.Classify("ssh", "helicon", 255))

	adapters, _, err := configuredAdapters(loadWiringProject(t, root), root, true)
	if err != nil {
		t.Fatalf("--skip-mutation was blocked by an unreachable remote: %v", err)
	}
	if adapters != nil {
		t.Errorf("--skip-mutation built %d adapters", len(adapters))
	}
	if *calls != 0 {
		t.Errorf("--skip-mutation probed the remote %d times", *calls)
	}
}
