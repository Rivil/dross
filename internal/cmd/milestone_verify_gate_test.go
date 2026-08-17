package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/milestone"
)

// seedPhaseVerdict writes a phase dir carrying a verify.toml with the given
// verdict. An empty verdict writes no verify.toml at all, which is the
// unverified-phase case that let v1.4 close with its last phase unmeasured.
func seedPhaseVerdict(t *testing.T, dir, id, verdict string) {
	t.Helper()
	pdir := filepath.Join(dir, ".dross", "phases", id)
	if err := os.MkdirAll(pdir, 0o755); err != nil {
		t.Fatal(err)
	}
	if verdict == "" {
		return
	}
	body := "[verify]\n  phase = \"" + id + "\"\n  verdict = \"" + verdict + "\"\n"
	mustWrite(t, filepath.Join(pdir, "verify.toml"), body)
}

// setMilestonePhases records the phase list for the version the open-fixture
// opens a PR for. The fixture writes no milestone toml — `milestone complete`
// does not require one — so this creates the record rather than editing it.
func setMilestonePhases(t *testing.T, dir, version string, phases []string) {
	t.Helper()
	root := filepath.Join(dir, ".dross")
	path := milestone.FilePath(root, version)
	m := &milestone.Milestone{}
	if existing, err := milestone.Load(path); err == nil {
		m = existing
	}
	m.Milestone.Version = version
	m.Phases = phases
	if err := m.Save(path); err != nil {
		t.Fatal(err)
	}
}

// The gate's whole purpose: a phase with no verify.toml stops the integration
// PR before it opens. Asserting cap.created is what makes this a gate rather
// than a warning — an error that still opened the PR would leave the unmeasured
// phase on its way to main.
func TestMilestoneCompleteRefusesAPhaseWithNoVerify(t *testing.T) {
	dir, cap := milestoneOpenFixture(t)
	setMilestonePhases(t, dir, "v0.9", []string{"scratch-volume-guard"})
	seedPhaseVerdict(t, dir, "scratch-volume-guard", "")

	err := runCmd(t, Milestone(), "complete", "v0.9")
	if err == nil {
		t.Fatal("an unverified phase reached the integration PR")
	}
	if !strings.Contains(err.Error(), "scratch-volume-guard") {
		t.Errorf("refusal does not name the offending phase: %v", err)
	}
	if !strings.Contains(err.Error(), "no verify.toml") {
		t.Errorf("refusal does not say what is missing: %v", err)
	}
	if cap.created != 0 {
		t.Errorf("PR was opened despite the refusal (created=%d)", cap.created)
	}
}

// A pending verdict is the forget-to-finalize case, and it must be told apart
// from a missing file: the next command differs.
func TestMilestoneCompleteRefusesAPendingVerdict(t *testing.T) {
	dir, cap := milestoneOpenFixture(t)
	setMilestonePhases(t, dir, "v0.9", []string{"p-pending"})
	seedPhaseVerdict(t, dir, "p-pending", "pending")

	err := runCmd(t, Milestone(), "complete", "v0.9")
	if err == nil {
		t.Fatal("a pending verdict reached the integration PR")
	}
	if !strings.Contains(err.Error(), "verify finalize p-pending") {
		t.Errorf("refusal does not name the finalize command: %v", err)
	}
	if cap.created != 0 {
		t.Errorf("PR was opened despite the refusal (created=%d)", cap.created)
	}
}

// Every offending phase is named in one error. Reporting only the first would
// make a three-phase clean-up three run-and-fix cycles, which is what trains a
// user to reach for the override instead.
func TestMilestoneCompleteNamesEveryUnverifiedPhase(t *testing.T) {
	dir, _ := milestoneOpenFixture(t)
	setMilestonePhases(t, dir, "v0.9", []string{"p-ok", "p-missing", "p-pending", "p-partial"})
	seedPhaseVerdict(t, dir, "p-ok", "pass")
	seedPhaseVerdict(t, dir, "p-missing", "")
	seedPhaseVerdict(t, dir, "p-pending", "pending")
	seedPhaseVerdict(t, dir, "p-partial", "partial")

	err := runCmd(t, Milestone(), "complete", "v0.9")
	if err == nil {
		t.Fatal("three unverified phases reached the integration PR")
	}
	for _, want := range []string{"p-missing", "p-pending", "p-partial"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal does not name %s: %v", want, err)
		}
	}
	// The passing phase must NOT be listed — a gate that named every phase
	// regardless would satisfy the assertions above while telling the reader
	// nothing about which one to fix.
	if strings.Contains(err.Error(), "p-ok") {
		t.Errorf("refusal names a phase that passed: %v", err)
	}
}

// The other direction: all-pass opens the PR. Without this, a gate that
// refused unconditionally would satisfy every refusal test above.
func TestMilestoneCompleteOpensWhenEveryPhasePasses(t *testing.T) {
	dir, cap := milestoneOpenFixture(t)
	setMilestonePhases(t, dir, "v0.9", []string{"p-one", "p-two"})
	seedPhaseVerdict(t, dir, "p-one", "pass")
	seedPhaseVerdict(t, dir, "p-two", "pass")

	if err := runCmd(t, Milestone(), "complete", "v0.9"); err != nil {
		t.Fatalf("all-passing milestone was refused: %v", err)
	}
	if cap.created != 1 {
		t.Errorf("PR was not opened (created=%d)", cap.created)
	}
}

// The override exists because a gate with no escape hatch gets worked around
// by deleting the check. It must actually open the PR, not merely change the
// error text.
func TestMilestoneCompleteForceUnverifiedOverrides(t *testing.T) {
	dir, cap := milestoneOpenFixture(t)
	setMilestonePhases(t, dir, "v0.9", []string{"p-missing"})
	seedPhaseVerdict(t, dir, "p-missing", "")

	if err := runCmd(t, Milestone(), "complete", "v0.9", "--force-unverified"); err != nil {
		t.Fatalf("--force-unverified did not override the gate: %v", err)
	}
	if cap.created != 1 {
		t.Errorf("PR was not opened under --force-unverified (created=%d)", cap.created)
	}
}

// An unrecorded milestone opens as it always has. `milestone complete` does
// not require a milestones/<v>.toml, and the gate must not smuggle in that
// requirement — an absent record claims no phases, so it withholds nothing.
func TestMilestoneCompleteAllowsAnUnrecordedMilestone(t *testing.T) {
	_, cap := milestoneOpenFixture(t)
	// Deliberately no setMilestonePhases: the fixture writes no toml.
	if err := runCmd(t, Milestone(), "complete", "v0.9"); err != nil {
		t.Fatalf("unrecorded milestone was refused: %v", err)
	}
	if cap.created != 1 {
		t.Errorf("PR was not opened (created=%d)", cap.created)
	}
}

// A milestone with no phases recorded is not blocked. The gate measures what
// the milestone claims to deliver; claiming nothing is a different problem
// from delivering something unmeasured, and refusing here would block the
// first close of every new milestone.
func TestMilestoneCompleteAllowsAPhaselessMilestone(t *testing.T) {
	dir, cap := milestoneOpenFixture(t)
	setMilestonePhases(t, dir, "v0.9", nil)

	if err := runCmd(t, Milestone(), "complete", "v0.9"); err != nil {
		t.Fatalf("phaseless milestone was refused: %v", err)
	}
	if cap.created != 1 {
		t.Errorf("PR was not opened (created=%d)", cap.created)
	}
	_ = dir
}
