package cmd

// `dross remote bootstrap`'s lane coverage (c-2).
//
// The point of every test here is that a host's readiness for LANES and for
// MUTATION is one answer. Two probes, two vocabularies, or a private derivation
// on either side would make them two — and the shape that failure takes is
// doctor and bootstrap both passing on a host the run then falls back from.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/project"
	"github.com/Rivil/dross/internal/remote"
)

// bootstrapLaneFixture is bootstrapFixture with lanes appended to project.toml.
func bootstrapLaneFixture(t *testing.T, lanes string, adapters ...string) string {
	t.Helper()
	root := bootstrapFixture(t, adapters...)
	if lanes == "" {
		return root
	}
	path := filepath.Join(root, project.File)
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(string(b)+"\n"+lanes+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

const bootstrapTwoLanes = `[[runtime.test_lane]]
name = "web"
match = ["web/**"]
command = "pnpm test"

[[runtime.test_lane]]
name = "unit"
match = ["internal/**"]
command = "go test ./..."`

// TestBootstrapProbesOnceForAdaptersAndLanes is c-2 stated as the round trip it
// forbids. The recorder asserts BOTH the count and the argument: one probe
// asking only the adapters' tools would satisfy a count-only test while leaving
// the lanes unplanned.
func TestBootstrapProbesOnceForAdaptersAndLanes(t *testing.T) {
	bootstrapLaneFixture(t, bootstrapTwoLanes, "gremlins")
	var asked [][]string
	fakeProbe(t, func(_ remote.Target, tools []string) (remote.Readiness, error) {
		asked = append(asked, append([]string(nil), tools...))
		return remote.Readiness{Cores: 10}, nil
	})
	rec := &execRecorder{}
	rec.install(t)

	if _, err := runBootstrap(t); err != nil {
		t.Fatal(err)
	}

	if len(asked) != 1 {
		t.Fatalf("bootstrap probed %d times, want exactly 1: %v", len(asked), asked)
	}
	for _, want := range []string{"gremlins", "pnpm", "go"} {
		if !contains(asked[0], want) {
			t.Errorf("the probe did not ask for %q: %v", want, asked[0])
		}
	}
}

// TestBootstrapPlansLaneStepsWithNoAdapterAllowlist: a repo that declares lanes
// and names no adapters must have its lanes planned, or bootstrap answers only
// half the question and the lane half has to be asked somewhere else — which is
// the "two separate answers" c-2 exists to end.
//
// It is NOT a zero-adapter repo: an empty allowlist means every adapter runs,
// which is configuredAdapters' own rule. The zero-tools arm is asserted
// separately below, where it is actually reachable.
func TestBootstrapPlansLaneStepsWithNoAdapterAllowlist(t *testing.T) {
	bootstrapLaneFixture(t, bootstrapTwoLanes)
	probeMissing(t, "pnpm")
	rec := &execRecorder{}
	rec.install(t)

	out, err := runBootstrap(t)
	if err != nil {
		t.Fatalf("bootstrap failed on a repo with lanes: %v\n%s", err, out)
	}
	if strings.Contains(out, "nothing to bootstrap") {
		t.Fatalf("a repo declaring lanes was reported as having nothing to bootstrap:\n%s", out)
	}
	if !strings.Contains(out, "lane web") {
		t.Errorf("the lane step was not planned or not attributed:\n%s", out)
	}
	if !strings.Contains(out, "npm install -g pnpm") {
		t.Errorf("the lane's built-in recipe was not planned:\n%s", out)
	}
}

// TestBootstrapEmptyMessageNamesBothSources: the repo shape with nothing to do
// is now empty for TWO reasons, and a message naming only adapters would tell a
// user with lanes that they have none.
//
// Reached through an allowlist that selects no known adapter — the only way the
// tool set is genuinely empty.
func TestBootstrapEmptyMessageNamesBothSources(t *testing.T) {
	bootstrapLaneFixture(t, "", "no-such-adapter")
	probeMissing(t)
	rec := &execRecorder{}
	rec.install(t)

	out, err := runBootstrap(t)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "nothing to bootstrap") {
		t.Fatalf("a repo with no tools at all did not report an empty plan:\n%s", out)
	}
	if !strings.Contains(out, "test lanes") {
		t.Errorf("the empty message names only adapters:\n%s", out)
	}
}

// TestBootstrapDedupesASharedTool: `go` is wanted by gremlins' recipe and by a
// Go lane, and it is ONE step. Two would ask the host the same question twice
// and print one gap as two.
func TestBootstrapDedupesASharedTool(t *testing.T) {
	bootstrapLaneFixture(t, bootstrapTwoLanes, "gremlins")
	probeMissing(t, "go")
	rec := &execRecorder{}
	rec.install(t)

	out, _ := runBootstrap(t)

	if n := strings.Count(out, "  ✗ go ") + strings.Count(out, "  → go ") + strings.Count(out, "  ✓ go "); n != 1 {
		t.Errorf("go appears as %d steps, want 1:\n%s", n, out)
	}
}

// TestBootstrapAndDoctorShareTheirDerivation: a private derivation in bootstrap
// drifts, and the drift shows up as doctor passing on a host the run then falls
// back from. Asserted by comparing the probe sets the two surfaces actually
// send.
func TestBootstrapAndDoctorShareTheirDerivation(t *testing.T) {
	bootstrapLaneFixture(t, bootstrapTwoLanes, "gremlins")
	var asked [][]string
	fakeProbe(t, func(_ remote.Target, tools []string) (remote.Readiness, error) {
		asked = append(asked, append([]string(nil), tools...))
		return remote.Readiness{Cores: 10}, nil
	})
	rec := &execRecorder{}
	rec.install(t)

	if _, err := runBootstrap(t); err != nil {
		t.Fatal(err)
	}
	bootstrapAsked := asked[0]

	asked = nil
	var out string
	doctorIssues(t, &out)
	if len(asked) == 0 {
		t.Fatal("doctor did not probe")
	}
	doctorAsked := asked[0]

	// Bootstrap probes a superset — it adds the recipes' runtime prerequisites,
	// which doctor has no use for. Every tool doctor asks about must be in it.
	for _, tool := range doctorAsked {
		if !contains(bootstrapAsked, tool) {
			t.Errorf("doctor asks about %q and bootstrap does not: doctor=%v bootstrap=%v", tool, doctorAsked, bootstrapAsked)
		}
	}
	// And the lane attribution both read is the same map.
	root, err := FindRoot()
	if err != nil {
		t.Fatal(err)
	}
	p, err := project.Load(filepath.Join(root, project.File))
	if err != nil {
		t.Fatal(err)
	}
	_, _, laneBy := remoteProbeTools(p)
	if laneBy["pnpm"] != "web" {
		t.Errorf("the shared attribution does not name the lane: %v", laneBy)
	}
}

// TestBootstrapVocabularyIsSharedAcrossOrigins is c-2's other half: a present
// lane tool and a present adapter tool produce the same shape, differing only
// in the tag. Two vocabularies would read as two commands' output interleaved.
func TestBootstrapVocabularyIsSharedAcrossOrigins(t *testing.T) {
	bootstrapLaneFixture(t, bootstrapTwoLanes, "gremlins")
	probeMissing(t) // everything present
	rec := &execRecorder{}
	rec.install(t)

	out, err := runBootstrap(t)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"✓ gremlins (gremlins) already installed",
		"✓ pnpm (lane web) already installed",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q from:\n%s", want, out)
		}
	}
}

// TestBootstrapUndeclaredLaneToolExitsZero is locked undeclared_exit at the
// report layer. The report is the useful part; a non-zero exit here would be
// noise that trains the loop to ignore the command.
func TestBootstrapUndeclaredLaneToolExitsZero(t *testing.T) {
	bootstrapLaneFixture(t, `[[runtime.test_lane]]
name = "web"
match = ["web/**"]
command = "bespoke-runner test"`)
	probeMissing(t, "bespoke-runner")
	rec := &execRecorder{}
	rec.install(t)

	out, err := runBootstrap(t)
	if err != nil {
		t.Fatalf("a lane tool with no recipe and no install line exited non-zero: %v\n%s", err, out)
	}
	if !strings.Contains(out, "no install line") {
		t.Errorf("the gap was not reported:\n%s", out)
	}
	if strings.Contains(out, "refused") {
		t.Errorf("an unreported tool was counted as a refusal:\n%s", out)
	}
}

// TestUnchangedMutationRefusalStillExitsOne: the undeclared arm must not soften
// the ladder around it. A refusal in the same run still exits 1, or adding a
// lane to a repo would quietly turn its adapter refusals green.
func TestUnchangedMutationRefusalStillExitsOne(t *testing.T) {
	bootstrapLaneFixture(t, `[[runtime.test_lane]]
name = "web"
match = ["web/**"]
command = "bespoke-runner test"`, "stryker")
	probeMissing(t, "bespoke-runner", "npx")
	rec := &execRecorder{}
	rec.install(t)

	out, err := runBootstrap(t)
	if err == nil {
		t.Fatalf("a mutation refusal beside an undeclared lane tool exited 0:\n%s", out)
	}
	if !strings.Contains(out, "no install line") {
		t.Errorf("the lane gap stopped being reported:\n%s", out)
	}
	if !strings.Contains(out, "refused") {
		t.Errorf("the adapter refusal stopped being reported:\n%s", out)
	}
}

// TestLaneRuntimeRefusalIsCountedRefusedNotFailed is c-8 on the bootstrap
// surface: only one of the two has a remedy dross can perform.
func TestLaneRuntimeRefusalIsCountedRefusedNotFailed(t *testing.T) {
	bootstrapLaneFixture(t, `[[runtime.test_lane]]
name = "web"
match = ["web/**"]
command = "node run.js"`)
	probeMissing(t, "node")
	rec := &execRecorder{}
	rec.install(t)

	out, err := runBootstrap(t, "--apply")
	if err == nil {
		t.Fatalf("a lane runtime refusal exited 0:\n%s", out)
	}
	if len(rec.argvs) != 0 {
		t.Errorf("a refusal was attempted: %v", rec.argvs)
	}
	if !strings.Contains(out, "0 installed, 1 refused, 0 failed") {
		t.Errorf("the refusal was not counted apart from a failure:\n%s", out)
	}
}

// TestBootstrapIsNotAWayAroundTheInstallGate: a second surface that installed a
// lane's declared line without its grant would make the gate the lane-scoped
// verb enforces worth nothing.
func TestBootstrapIsNotAWayAroundTheInstallGate(t *testing.T) {
	lane := `[[runtime.test_lane]]
name = "web"
match = ["web/**"]
command = "pnpm test"
install = "corepack enable pnpm"`

	t.Run("ungranted is a refusal, not an attempt", func(t *testing.T) {
		bootstrapLaneFixture(t, lane)
		probeMissing(t, "pnpm")
		rec := &execRecorder{}
		rec.install(t)

		out, err := runBootstrap(t, "--apply")
		if err == nil {
			t.Fatalf("an ungranted install line was accepted:\n%s", out)
		}
		if len(rec.argvs) != 0 {
			t.Fatalf("an ungranted install line reached the exec seam: %v", rec.argvs)
		}
		if !strings.Contains(out, "refused") {
			t.Errorf("it was not reported as a refusal:\n%s", out)
		}
		if strings.Contains(out, "0 refused, 1 failed") {
			t.Errorf("an ungranted line was reported as a failed attempt:\n%s", out)
		}
		if !strings.Contains(out, "dross trust --lane-install web") {
			t.Errorf("the refusal does not name the fix:\n%s", out)
		}
	})

	t.Run("granted reaches the seam as sh -c", func(t *testing.T) {
		bootstrapLaneFixture(t, lane)
		probeMissing(t, "pnpm")
		mustGrantLaneInstall(t, "web")
		rec := &execRecorder{}
		rec.install(t)

		if _, err := runBootstrap(t, "--apply"); err != nil {
			t.Fatal(err)
		}
		if len(rec.argvs) != 1 {
			t.Fatalf("the granted line reached the seam %d times, want 1: %v", len(rec.argvs), rec.argvs)
		}
		want := []string{"sh", "-c", "corepack enable pnpm"}
		if len(rec.argvs[0]) != 3 || rec.argvs[0][0] != want[0] || rec.argvs[0][1] != want[1] || rec.argvs[0][2] != want[2] {
			t.Errorf("the declared line reached the seam as %q, want %q", rec.argvs[0], want)
		}
	})
}

// TestBootstrapDryRunNeverInstallsALaneTool is c-5 on this surface. The default
// has to be the safe one for lanes exactly as it is for adapters.
func TestBootstrapDryRunNeverInstallsALaneTool(t *testing.T) {
	bootstrapLaneFixture(t, bootstrapTwoLanes)
	probeMissing(t, "pnpm")
	rec := &execRecorder{}
	rec.install(t)

	out, err := runBootstrap(t)
	if err != nil {
		t.Fatal(err)
	}
	if len(rec.argvs) != 0 {
		t.Errorf("a dry run installed a lane tool: %v", rec.argvs)
	}
	if !strings.Contains(out, "would install") {
		t.Errorf("the dry run does not show what it would do:\n%s", out)
	}
}
