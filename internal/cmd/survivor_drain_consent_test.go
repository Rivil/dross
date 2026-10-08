package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/mutation"
	"github.com/Rivil/dross/internal/project"
	"github.com/Rivil/dross/internal/survivor"
)

// `survivor drain` spawns the repo's own toolchain: `go list -f {{.Dir}} ./...`
// to discover packages, the configured mutation adapter over them, and — under
// --report — `go test -coverprofile` over the repo's packages. Every one of
// those executes code the repo supplied, which is the authority the exec
// consent gate exists to require.
//
// The assertions here ride COUNTERS over the spawn seams, not the refusal text.
// A gate that returned the right error after shelling out would be
// indistinguishable from a real one if only the message were checked, and the
// spawn is the thing being prevented.

// drainSpawnCounters records how often each of the drain's spawn seams was
// reached.
type drainSpawnCounters struct {
	list     int
	coverage int
	runner   int
}

// countDrainSpawns replaces the drain's three spawn seams with counters.
//
// goListDirs delegates to whatever was installed before — drainFixture's fake
// package set — so a run that legitimately gets past the gate still behaves.
// The other two do NOT delegate: reaching them for real runs the mutation
// adapter and the repo's suite, which is exactly what an unconsented run must
// not do even when the test is the one asking.
func countDrainSpawns(t *testing.T) *drainSpawnCounters {
	t.Helper()
	c := &drainSpawnCounters{}

	prevList := goListDirs
	goListDirs = func(repoRoot string) ([]string, error) {
		c.list++
		return prevList(repoRoot)
	}
	prevCoverage := coverageProfileFn
	coverageProfileFn = func(string, []string) *survivor.Profile {
		c.coverage++
		return nil
	}
	prevRunner := drainRunner
	drainRunner = func(string, []string) ([]mutation.Unmeasured, error) {
		c.runner++
		return nil, nil
	}
	t.Cleanup(func() {
		goListDirs = prevList
		coverageProfileFn = prevCoverage
		drainRunner = prevRunner
	})
	return c
}

// revokeConsent removes the grant drainFixture records, leaving the tree in the
// state a fresh clone is in: a real runtime.test_command nobody on this machine
// has agreed to.
func revokeConsent(t *testing.T) {
	t.Helper()
	root, err := FindRoot()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(localPath(root)); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
}

// untrustedDrainFixture is drainFixture with the grant taken back off, so the
// drain runs against a repo this machine has never trusted.
func untrustedDrainFixture(t *testing.T) string {
	t.Helper()
	dir := drainFixture(t, []string{"./internal"})
	revokeConsent(t)
	return dir
}

// TestDrainRefusesWithoutConsent is c-3: the drain runs the repo's own packages
// through the toolchain, and today it did so in a tree nobody had trusted.
func TestDrainRefusesWithoutConsent(t *testing.T) {
	untrustedDrainFixture(t)
	c := countDrainSpawns(t)

	err := runCmd(t, Survivor(), "drain")
	if err == nil {
		t.Fatal("`dross survivor drain` ran without consent")
	}
	if !strings.Contains(err.Error(), "dross trust") {
		t.Errorf("refusal does not name the remedy: %v", err)
	}
	if c.list != 0 {
		t.Errorf("the refusal ran `go list` first (%d calls) — it spawned the toolchain it was declining to authorize", c.list)
	}
	if c.runner != 0 {
		t.Errorf("the refusal reached the mutation adapter (%d calls) — the adapter runs this repo's tests", c.runner)
	}
}

// TestDrainReportRefusesWithoutConsent: --report reads recorded reports rather
// than running the adapter, which reads like the safe path. It is not — the
// coverage pass under it spawns `go test -coverprofile` over the repo's own
// packages, so a gate that only covered the adapter would leave the quieter
// half of the drain ungated.
func TestDrainReportRefusesWithoutConsent(t *testing.T) {
	dir := untrustedDrainFixture(t)
	report := filepath.Join(dir, "gremlins.json")
	mustWrite(t, report, gremlinsReportWith("y.go", mutantJSON(4, "CONDITIONALS_BOUNDARY", "LIVED")))
	c := countDrainSpawns(t)

	err := runCmd(t, Survivor(), "drain", "--report", report)
	if err == nil {
		t.Fatal("`dross survivor drain --report` ran without consent")
	}
	if !strings.Contains(err.Error(), "dross trust") {
		t.Errorf("refusal does not name the remedy: %v", err)
	}
	if c.coverage != 0 {
		t.Errorf("the refusal ran `go test -coverprofile` (%d calls) — it ran this repo's suite while declining to authorize it", c.coverage)
	}
	if c.list != 0 || c.runner != 0 {
		t.Errorf("the refusal reached a spawn seam: list=%d runner=%d", c.list, c.runner)
	}
}

// TestDrainGateOpensWithConsent is the other half. A gate that refused
// unconditionally would pass every assertion above and brick the command, so
// the grant is proven to let the run through — and the seam is proven reached,
// because "failed for some other reason" and "never got started" read alike
// from the error alone.
func TestDrainGateOpensWithConsent(t *testing.T) {
	untrustedDrainFixture(t)
	root, err := FindRoot()
	if err != nil {
		t.Fatal(err)
	}
	proj, err := project.Load(filepath.Join(root, project.File))
	if err != nil {
		t.Fatal(err)
	}
	if err := GrantConsent(root, proj.Runtime.TestCommand); err != nil {
		t.Fatal(err)
	}
	c := countDrainSpawns(t)

	if err := runCmd(t, Survivor(), "drain"); err != nil && strings.Contains(err.Error(), "dross trust") {
		t.Fatalf("the gate never opens: a consented drain was still refused: %v", err)
	}
	if c.list == 0 {
		t.Error("the consented drain never reached `go list` — it stopped before the work the grant authorizes")
	}
}
