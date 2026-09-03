package cmd

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/project"
)

// planFixture loads the lane fixture's own project, so a test can call lanePlan
// against exactly the configuration `dross test --files` would read.
//
// It returns the repo dir rather than the dross root because that is what the
// existence filter resolves paths against — a plan built against the wrong
// directory would drop every path and pass the "scoped to nothing" assertions
// for entirely the wrong reason.
func planFixture(t *testing.T, lanes string) (string, *project.Project) {
	t.Helper()
	filesFixture(t, lanes)
	root, err := FindRoot()
	if err != nil {
		t.Fatal(err)
	}
	p, err := project.Load(filepath.Join(root, project.File))
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Dir(root), p
}

// TestPlanLineIsTheSpawnedLine is c-1's whole guarantee, asserted as an
// identity between two strings rather than each against a literal.
//
// A preview that derived its own line would satisfy any literal assertion and
// still drift from the gate the first time either derivation changed. Comparing
// the RECORDED spawn against the plan's Line is the only form of this test that
// cannot pass while the two disagree.
func TestPlanLineIsTheSpawnedLine(t *testing.T) {
	repoDir, proj := planFixture(t, selectorLanes)
	grantAllLanes(t)
	files := []string{"internal/cmd/test.go", "internal/cmd/lane_plan.go", "internal/project/project.go"}
	for _, f := range files {
		touchFile(t, f)
	}
	rec := installSpawnRecorder(t, nil)

	if err := runCmd(t, Test(), "--files", files[0], "--files", files[1], "--files", files[2]); err != nil {
		t.Fatalf("dross test --files: %v", err)
	}
	if rec.count() != 1 {
		t.Fatalf("want exactly 1 spawn, got %d: %v", rec.count(), rec.lines)
	}

	plan := lanePlan(repoDir, proj, files)
	if len(plan.Lanes) != 1 {
		t.Fatalf("want 1 planned lane, got %d", len(plan.Lanes))
	}
	if plan.Lanes[0].Line != rec.lines[0] {
		t.Errorf("plan line %q is not the line the gate spawned (%q) — preview and gate have diverged",
			plan.Lanes[0].Line, rec.lines[0])
	}
}

// TestLaneRunLineNamesDroppedPaths: the existence filter locked by missing_paths
// must report what it removed, not merely remove it.
//
// The gate can shorten its line silently; preview cannot, because the deleted
// path is the case a user is most likely to be previewing. A filter that
// discards its own findings leaves c-2 with nothing to print.
func TestLaneRunLineNamesDroppedPaths(t *testing.T) {
	repoDir, proj := planFixture(t, selectorLanes)
	touchFile(t, "internal/cmd/here.go")

	plan := lanePlan(repoDir, proj, []string{"internal/cmd/here.go", "internal/gone/away.go"})
	if len(plan.Lanes) != 1 {
		t.Fatalf("want 1 planned lane, got %d", len(plan.Lanes))
	}
	got := plan.Lanes[0]
	if len(got.Dropped) != 1 || got.Dropped[0] != "internal/gone/away.go" {
		t.Errorf("dropped = %v, want [internal/gone/away.go] — the filter removed it without saying so", got.Dropped)
	}
	if !strings.Contains(got.Line, "./internal/cmd/...") {
		t.Errorf("line %q does not carry the surviving path's package", got.Line)
	}
	if strings.Contains(got.Line, "gone") {
		t.Errorf("line %q still carries a path that is not on disk", got.Line)
	}
}

// TestEverySelectorPathGoneScopesToNothing keeps the "nothing to scope to" lane
// distinguishable from a lane with a line.
//
// Both would spawn nothing, but only one of them is a finding: an empty Line
// with no flag beside it is indistinguishable from a lane that simply has not
// been derived yet, and preview would have nothing to name.
func TestEverySelectorPathGoneScopesToNothing(t *testing.T) {
	repoDir, proj := planFixture(t, selectorLanes)

	plan := lanePlan(repoDir, proj, []string{"internal/gone/away.go"})
	if len(plan.Lanes) != 1 {
		t.Fatalf("want 1 planned lane, got %d", len(plan.Lanes))
	}
	if !plan.Lanes[0].ScopedToNothing {
		t.Error("a lane whose every selector path is deleted is not marked ScopedToNothing")
	}
	if plan.Lanes[0].Line != "" {
		t.Errorf("line = %q, want empty — a lane that scoped to nothing has no line to run", plan.Lanes[0].Line)
	}
}

// TestResolveReportsOutOfTreeAsData is what locked preview_exit_status rests on.
//
// The shared resolution is TOTAL: an out-of-tree path comes back as a field on
// the plan, with nothing refused and no lane resolved. The refusal stays at the
// gate, which is why both halves are asserted here — if exitBadFileSet moved
// into the shared function, preview could never exit 0 over the same argv.
func TestResolveReportsOutOfTreeAsData(t *testing.T) {
	repoDir, proj := planFixture(t, selectorLanes)

	plan := lanePlan(repoDir, proj, []string{"/abs/x.go"})
	if len(plan.OutOfTree) != 1 || plan.OutOfTree[0] != "/abs/x.go" {
		t.Errorf("out-of-tree = %v, want [/abs/x.go] carried as data", plan.OutOfTree)
	}
	if len(plan.Lanes) != 0 {
		t.Errorf("want no planned lanes, got %d", len(plan.Lanes))
	}

	rec := installSpawnRecorder(t, nil)
	err := runCmd(t, Test(), "--files", "/abs/x.go")
	if ExitCode(err) != exitBadFileSet {
		t.Errorf("gate exit = %d, want %d — the refusal belongs to the gate, not to resolution",
			ExitCode(err), exitBadFileSet)
	}
	if rec.count() != 0 {
		t.Errorf("the refusal spawned %d command(s)", rec.count())
	}
}

// malformedTemplateLane declares a selector_template carrying no placeholder:
// well-formed TOML, unrunnable line. Honoured at derivation it would append
// nothing and spawn the lane's WHOLE command under a scoped lane's name.
const malformedTemplateLane = `[[runtime.test_lane]]
name = "bad"
match = ["internal/**"]
command = "cargo test"
selector = "dir"
selector_template = "--package"`

// TestMalformedLaneIsFencedBeforeAnySpawn: the fence runs inside resolution,
// before any line is derived, and its verdict is carried rather than returned.
//
// Carried, because preview must be able to NAME a malformed lane without
// inheriting the gate's refusal (locked preview_exit_status). Before derivation,
// because a plan that carried both a FenceErr and a Line would be offering a
// command line the fence had already rejected.
func TestMalformedLaneIsFencedBeforeAnySpawn(t *testing.T) {
	repoDir, proj := planFixture(t, malformedTemplateLane)
	grantAllLanes(t)
	touchFile(t, "internal/cmd/here.go")
	rec := installSpawnRecorder(t, nil)

	plan := lanePlan(repoDir, proj, []string{"internal/cmd/here.go"})
	if len(plan.Lanes) != 1 {
		t.Fatalf("want 1 planned lane, got %d", len(plan.Lanes))
	}
	if plan.Lanes[0].FenceErr == nil {
		t.Fatal("a lane whose selector_template has no placeholder carries no FenceErr")
	}
	if plan.Lanes[0].Line != "" {
		t.Errorf("line = %q — a fenced lane must never carry a derived line", plan.Lanes[0].Line)
	}

	if err := runCmd(t, Test(), "--files", "internal/cmd/here.go"); err == nil {
		t.Error("the gate ran a lane the fence rejected")
	}
	if rec.count() != 0 {
		t.Errorf("the fence spawned %d command(s) — it exists to stop a line reaching a shell", rec.count())
	}
}

// TestPlanCarriesUnmatchedPaths: the partial miss is a field on the plan, not
// something the gate prints and forgets.
//
// The gate reports it inline at the top of a run; preview has to render it
// beside the lanes it did hit. Reading it off the plan is what keeps the two
// naming the same set — a preview that re-selected the file set to find its
// unmatched paths would be a second resolution.
func TestPlanCarriesUnmatchedPaths(t *testing.T) {
	repoDir, proj := planFixture(t, selectorLanes)
	touchFile(t, "internal/cmd/here.go")

	plan := lanePlan(repoDir, proj, []string{"internal/cmd/here.go", "NOTES.txt"})
	if len(plan.Lanes) != 1 {
		t.Fatalf("want 1 planned lane, got %d", len(plan.Lanes))
	}
	if len(plan.Unmatched) != 1 || plan.Unmatched[0] != "NOTES.txt" {
		t.Errorf("unmatched = %v, want [NOTES.txt]", plan.Unmatched)
	}
}
