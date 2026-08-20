package cmd

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/changes"
)

// residueFixture is a git repo with a real origin and an initialized .dross,
// chdir'd. Callers add ship commits, phase directories and roadmaps.
func residueFixture(t *testing.T) string {
	t.Helper()
	dir := backfillRepo(t)
	chdir(t, dir)
	if err := runCmd(t, Init()); err != nil {
		t.Fatal(err)
	}
	return dir
}

func roadmap(t *testing.T, dir, version string, phases ...string) {
	t.Helper()
	quoted := make([]string, len(phases))
	for i, p := range phases {
		quoted[i] = `"` + p + `"`
	}
	mustWrite(t, filepath.Join(dir, ".dross", "milestones", version+".toml"),
		"phases = ["+strings.Join(quoted, ", ")+"]\n\n[milestone]\n  version = \""+version+"\"\n")
}

func residueSlugs(t *testing.T, dir string) []string {
	t.Helper()
	var out []string
	for _, r := range backfillResidue(filepath.Join(dir, ".dross"), dir, "main") {
		out = append(out, r.Slug)
	}
	return out
}

// TestResidueNamesRoadmapPhaseWithNoEvidence is c-5's core case: a phase a
// milestone signed up for, with no completion marker and no ship commit to
// infer one from. Doneness reads changes.json alone now, so it counts not-done
// forever with nothing saying why — indistinguishable at every surface from a
// phase that was never started.
func TestResidueNamesRoadmapPhaseWithNoEvidence(t *testing.T) {
	dir := residueFixture(t)
	shipCommit(t, dir, "phase something-else: something-else (#1)")
	backfillPhaseDir(t, dir, "no-evidence", "")
	roadmap(t, dir, "v0.1", "no-evidence")

	if got := residueSlugs(t, dir); !hasSlug(got, "no-evidence") {
		t.Errorf("residue = %v, want it to name no-evidence", got)
	}
}

// TestResidueIsScopedToRoadmapArrays pins the locked backfill_residue decision.
// A phase directory on no milestone's phases array — this repo's deliberate
// v14-mutation-pass scratch dir — would otherwise nag on every doctor run
// forever, and silencing it would force an accept-mechanism to be invented here.
func TestResidueIsScopedToRoadmapArrays(t *testing.T) {
	dir := residueFixture(t)
	backfillPhaseDir(t, dir, "scratch-dir", "")
	roadmap(t, dir, "v0.1", "on-roadmap")
	backfillPhaseDir(t, dir, "on-roadmap", "")

	got := residueSlugs(t, dir)
	if hasSlug(got, "scratch-dir") {
		t.Errorf("a directory on no roadmap must not be residue: %v", got)
	}
	if !hasSlug(got, "on-roadmap") {
		t.Errorf("the roadmap phase must still be named: %v", got)
	}
}

// TestResidueExcludesBackfillablePhase: a phase the sweep can close is not
// residue. Reporting it would make the section a to-do list of work already
// evidenced, and the one line that matters would be lost in it.
func TestResidueExcludesBackfillablePhase(t *testing.T) {
	dir := residueFixture(t)
	shipCommit(t, dir, "phase closeable: closeable (#1)")
	backfillPhaseDir(t, dir, "closeable", "")
	roadmap(t, dir, "v0.1", "closeable")

	if got := residueSlugs(t, dir); hasSlug(got, "closeable") {
		t.Errorf("a backfillable phase is not residue: %v", got)
	}
}

// TestResidueNamesInFlightAndUnscaffolded applies the locked rule literally. A
// phase with a live branch has not shipped, whatever the base carries; a
// roadmap slug with no directory is work that was listed and never built. Both
// are "listed and not delivered", which is exactly what the section reports.
func TestResidueNamesInFlightAndUnscaffolded(t *testing.T) {
	dir := residueFixture(t)
	// In flight: a ship commit exists AND the branch is still live, so only the
	// liveness half can catch it.
	shipCommit(t, dir, "phase inflight: inflight (#1)")
	backfillPhaseDir(t, dir, "inflight", "")
	mustGit(t, dir, "branch", "phase/inflight")
	roadmap(t, dir, "v0.1", "inflight", "never-built")

	got := residueSlugs(t, dir)
	for _, want := range []string{"inflight", "never-built"} {
		if !hasSlug(got, want) {
			t.Errorf("residue = %v, want it to name %q", got, want)
		}
	}
}

// TestResidueLivenessReadsCachedRemoteRef: doctor is an offline diagnostic and
// must not open a network connection to print an advisory. The liveness half
// reads local refs plus the cached refs/remotes/origin/ ones, so a phase branch
// that exists only on origin is still seen.
func TestResidueLivenessReadsCachedRemoteRef(t *testing.T) {
	dir := residueFixture(t)
	sha := shipCommit(t, dir, "phase remote-inflight: remote-inflight (#1)")
	backfillPhaseDir(t, dir, "remote-inflight", "")
	mustGit(t, dir, "update-ref", "refs/remotes/origin/phase/remote-inflight", sha)
	roadmap(t, dir, "v0.1", "remote-inflight")

	if got := residueSlugs(t, dir); !hasSlug(got, "remote-inflight") {
		t.Errorf("a phase branch cached from origin must read as in flight: %v", got)
	}
}

// TestResidueExcludesMarkedPhases: a phase carrying a marker — observed or
// backfilled — is done and never residue. The backfilled arm is the one that
// matters: after the sweep, 67 records gain markers, and if provenance made
// them look unclosed the section would report the whole history as outstanding.
func TestResidueExcludesMarkedPhases(t *testing.T) {
	dir := residueFixture(t)
	sha := shipCommit(t, dir, "phase swept: swept (#1)")
	backfillPhaseDir(t, dir, "swept", "")
	root := filepath.Join(dir, ".dross")
	if err := changes.SetBackfilled(root, "swept", sha); err != nil {
		t.Fatal(err)
	}
	backfillPhaseDir(t, dir, "observed", changes.StatusComplete)
	roadmap(t, dir, "v0.1", "swept", "observed")

	if got := residueSlugs(t, dir); len(got) != 0 {
		t.Errorf("marked phases must never be residue: %v", got)
	}
}

// TestDoctorPrintsResidueSectionAdvisory: the section renders, names the phase,
// and — following the duplicate-slug precedent — never moves the exit code. A
// residue line that failed doctor would make an unbuilt roadmap entry, a normal
// mid-milestone state, look like a broken repo.
func TestDoctorPrintsResidueSectionAdvisory(t *testing.T) {
	dir := residueFixture(t)
	roadmap(t, dir, "v0.1", "not-delivered")
	backfillPhaseDir(t, dir, "not-delivered", "")

	var control string
	roadmapPath := filepath.Join(dir, ".dross", "milestones", "v0.1.toml")
	mustWrite(t, roadmapPath, "phases = []\n\n[milestone]\n  version = \"v0.1\"\n")
	controlErr := runCmdCapturing(t, &control, Doctor())
	if strings.Contains(control, "Unbackfillable roadmap phases:") {
		t.Fatalf("fixture: the control run must have no residue:\n%s", control)
	}

	roadmap(t, dir, "v0.1", "not-delivered")
	var out string
	err := runCmdCapturing(t, &out, Doctor())
	if !strings.Contains(out, "Unbackfillable roadmap phases:") {
		t.Fatalf("the section did not render:\n%s", out)
	}
	if !strings.Contains(out, "not-delivered") {
		t.Errorf("the section must name the phase:\n%s", out)
	}
	if !strings.Contains(out, "Advisory only") {
		t.Errorf("the section must say it is advisory:\n%s", out)
	}
	if (err == nil) != (controlErr == nil) {
		t.Errorf("residue changed doctor's exit state: control=%v with-residue=%v", controlErr, err)
	}
}
