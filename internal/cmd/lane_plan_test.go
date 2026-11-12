package cmd

import (
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/project"
	"github.com/Rivil/dross/internal/testlane"
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

// relateLane is a vouching lane that passes every fence: the shape barktime's
// lane takes, so a plan over it reaches the relate arm.
const relateLane = `[[runtime.test_lane]]
name = "web"
match = ["src/**"]
command = "npx vitest run"
selector = "path"
vouch = true
relate = "node scripts/relate.mjs"`

// templatedRelateLane adds the placement a batched line must follow and the
// relate line must not.
const templatedRelateLane = relateLane + `
selector_template = "--only {path}"`

// TestVouchingLanePlansRelateAndNoLine: the changed paths go to relate, shell
// quoted, and the lane has no Line, because no line exists until relate answers.
// A Line here would carry the changed paths, which is the run this replaces.
func TestVouchingLanePlansRelateAndNoLine(t *testing.T) {
	repoDir, proj := planFixture(t, relateLane)
	touchFile(t, "src/a b.ts")
	touchFile(t, "src/c.svelte")

	plan := lanePlan(repoDir, proj, []string{"src/c.svelte", "src/a b.ts"})
	if len(plan.Lanes) != 1 {
		t.Fatalf("want 1 planned lane, got %d", len(plan.Lanes))
	}
	got := plan.Lanes[0]
	if want := "node scripts/relate.mjs 'src/a b.ts' 'src/c.svelte'"; got.Relate != want {
		t.Errorf("relate = %q, want %q", got.Relate, want)
	}
	if got.Line != "" {
		t.Errorf("line = %q, want empty — a vouching lane's tests are not known at plan time", got.Line)
	}
	if len(got.Selector) != 0 {
		t.Errorf("selector = %v, want none — it describes a Line this lane does not have", got.Selector)
	}
	if !reflect.DeepEqual(got.Asked, []string{"src/c.svelte", "src/a b.ts"}) {
		t.Errorf("asked = %v, want the live paths", got.Asked)
	}
	if got.ScopedToNothing || got.FenceErr != nil {
		t.Errorf("scoped to nothing = %v, fence = %v, want neither", got.ScopedToNothing, got.FenceErr)
	}
}

// TestDeletedPathStaysOffTheRelateLine: a deleted path is named, and relate is
// never asked about it, so no output of relate's can cover it.
func TestDeletedPathStaysOffTheRelateLine(t *testing.T) {
	repoDir, proj := planFixture(t, relateLane)
	touchFile(t, "src/here.ts")

	got := lanePlan(repoDir, proj, []string{"src/here.ts", "src/gone.ts"}).Lanes[0]
	if !reflect.DeepEqual(got.Dropped, []string{"src/gone.ts"}) {
		t.Errorf("dropped = %v, want [src/gone.ts]", got.Dropped)
	}
	if strings.Contains(got.Relate, "gone") {
		t.Errorf("relate %q carries a path that is not on disk", got.Relate)
	}
	if slices.Contains(got.Asked, "src/gone.ts") {
		t.Errorf("asked = %v still holds the deleted path", got.Asked)
	}
	if rel, err := testlane.ParseRelate(repoDir, got.Asked, "src/gone.ts\tsrc/here.ts\n"); err == nil {
		t.Errorf("relate covered a deleted path: %+v", rel)
	}
}

// TestVouchingLaneWithEveryPathGoneScopesToNothing: the same outcome a plain
// scoped lane has, and no relate line to spawn over nothing.
func TestVouchingLaneWithEveryPathGoneScopesToNothing(t *testing.T) {
	repoDir, proj := planFixture(t, relateLane)

	got := lanePlan(repoDir, proj, []string{"src/gone.ts"}).Lanes[0]
	if !got.ScopedToNothing {
		t.Error("a vouching lane with every path deleted is not ScopedToNothing")
	}
	if got.Relate != "" {
		t.Errorf("relate = %q, want empty — bare, it would ask about nothing", got.Relate)
	}
	if !reflect.DeepEqual(got.Dropped, []string{"src/gone.ts"}) {
		t.Errorf("dropped = %v, want [src/gone.ts]", got.Dropped)
	}
}

// TestBatchedLineQuotesASharedTestOnce: two paths sharing a test run it once.
// Parsed rather than handed in, because the shared test arrives as two lines of
// relate's output, and the order asserted is the stream's, not sorted.
func TestBatchedLineQuotesASharedTestOnce(t *testing.T) {
	repoDir, proj := planFixture(t, relateLane)
	touchFile(t, "src/z.test.ts")
	touchFile(t, "src/a.test.ts")

	rel, err := testlane.ParseRelate(repoDir, []string{"src/a.ts", "src/b.ts"},
		"src/a.ts\tsrc/z.test.ts\nsrc/b.ts\tsrc/z.test.ts\nsrc/b.ts\tsrc/a.test.ts\n")
	if err != nil {
		t.Fatal(err)
	}
	line, args, ok := laneBatchLine(proj.Runtime.TestLane[0], rel.Tests)
	if !ok {
		t.Fatal("no batched line")
	}
	if want := "npx vitest run 'src/z.test.ts' 'src/a.test.ts'"; line != want {
		t.Errorf("line = %q, want %q", line, want)
	}
	if !reflect.DeepEqual(args, []string{"src/z.test.ts", "src/a.test.ts"}) {
		t.Errorf("args = %v", args)
	}

	// The builder holds the rule itself, so a caller with an undeduped list
	// still gets one argument per test.
	repeated, _, _ := laneBatchLine(proj.Runtime.TestLane[0], []string{"a.test.ts", "a.test.ts"})
	if repeated != "npx vitest run 'a.test.ts'" {
		t.Errorf("line = %q, want one argument", repeated)
	}
}

// TestBatchedLineCarriesTestsNeverChangedPaths: the plan's paths and the batched
// line meet nowhere, so the one source file that is not a test stays off it.
func TestBatchedLineCarriesTestsNeverChangedPaths(t *testing.T) {
	repoDir, proj := planFixture(t, relateLane)
	touchFile(t, "src/changed.ts")
	touchFile(t, "src/changed.test.ts")

	pl := lanePlan(repoDir, proj, []string{"src/changed.ts"}).Lanes[0]
	rel, err := testlane.ParseRelate(repoDir, pl.Asked, "src/changed.ts\tsrc/changed.test.ts\n")
	if err != nil {
		t.Fatal(err)
	}
	line, _, ok := laneBatchLine(pl.lane, rel.Tests)
	if !ok {
		t.Fatal("no batched line")
	}
	if !strings.Contains(line, "'src/changed.test.ts'") {
		t.Errorf("line %q does not carry the test relate named", line)
	}
	if strings.Contains(line, "'src/changed.ts'") {
		t.Errorf("line %q carries the changed path", line)
	}
}

// TestBatchedLineWithNoTestsIsNoLine: a lane's command bare is its whole suite,
// so no tests means no line, never that command.
func TestBatchedLineWithNoTestsIsNoLine(t *testing.T) {
	_, proj := planFixture(t, relateLane)
	if line, args, ok := laneBatchLine(proj.Runtime.TestLane[0], nil); ok || line != "" || args != nil {
		t.Errorf("got %q, %v, %v — want no line", line, args, ok)
	}
}

// TestBatchedLineRefusesABrokenTemplate: a template the fence would have caught
// gives no line, rather than the unscoped command.
func TestBatchedLineRefusesABrokenTemplate(t *testing.T) {
	lane := project.TestLane{Command: "npx vitest run", Selector: "path", SelectorTemplate: "--only"}
	if line, _, ok := laneBatchLine(lane, []string{"a.test.ts"}); ok || line != "" {
		t.Errorf("got %q, %v — want no line", line, ok)
	}
}

// TestTemplateShapesTheBatchedLineNotRelate: relate's paths are always appended,
// and the lane's own placement belongs to the line that runs the tests.
func TestTemplateShapesTheBatchedLineNotRelate(t *testing.T) {
	repoDir, proj := planFixture(t, templatedRelateLane)
	touchFile(t, "src/a.ts")

	pl := lanePlan(repoDir, proj, []string{"src/a.ts"}).Lanes[0]
	if want := "node scripts/relate.mjs 'src/a.ts'"; pl.Relate != want {
		t.Errorf("relate = %q, want %q — the template followed it", pl.Relate, want)
	}
	line, _, ok := laneBatchLine(pl.lane, []string{"src/a.test.ts", "src/b.test.ts"})
	if !ok {
		t.Fatal("no batched line")
	}
	if want := "npx vitest run --only 'src/a.test.ts' --only 'src/b.test.ts'"; line != want {
		t.Errorf("line = %q, want %q", line, want)
	}
}

// TestLaneWithoutVouchIgnoresRelate: relate on a non-vouching lane is validate's
// complaint, not the run's. Such a lane plans today's line, and even a relate
// that could not spawn is left alone, since nothing would spawn it.
func TestLaneWithoutVouchIgnoresRelate(t *testing.T) {
	const deadRelate = `[[runtime.test_lane]]
name = "web"
match = ["src/**"]
command = "npx vitest run"
selector = "path"
relate = "-x dead"`
	repoDir, proj := planFixture(t, deadRelate)
	touchFile(t, "src/a.ts")

	got := lanePlan(repoDir, proj, []string{"src/a.ts"}).Lanes[0]
	if got.FenceErr != nil {
		t.Fatalf("fence = %v, want none", got.FenceErr)
	}
	if want := "npx vitest run 'src/a.ts'"; got.Line != want {
		t.Errorf("line = %q, want %q", got.Line, want)
	}
	if got.Relate != "" || got.Asked != nil {
		t.Errorf("relate = %q, asked = %v, want neither", got.Relate, got.Asked)
	}
}

// TestLaneFenceRefusesAHandEditedVouchingLane: validate is a verb nobody has to
// run, so each of its vouching rules holds again at the fence, through the gate
// and through spawns the recorder would have seen.
func TestLaneFenceRefusesAHandEditedVouchingLane(t *testing.T) {
	const head = `[[runtime.test_lane]]
name = "web"
match = ["src/**"]
command = "npx vitest run"
vouch = true
`
	for _, tc := range []struct {
		name, body, want string
	}{
		{"no selector", `relate = "r"`, "no selector"},
		{"dir selector", "selector = \"dir\"\nrelate = \"r\"", `selector = "dir"`},
		{"go-package selector", "selector = \"go-package\"\nrelate = \"r\"", `selector = "go-package"`},
		{"no relate", `selector = "path"`, "no relate"},
		{"blank relate", "selector = \"path\"\nrelate = \"  \"", "no relate"},
		{"empty_exit listing 1", "selector = \"path\"\nrelate = \"r\"\nempty_exit = [1]", "empty_exit listing 1"},
		{"relate with a leading dash", "selector = \"path\"\nrelate = \"-x evil\"", "begins with a dash"},
		{"relate hiding its dash", "selector = \"path\"\nrelate = \"  -x evil\"", "begins with a dash"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repoDir, proj := planFixture(t, head+tc.body)
			touchFile(t, "src/a.ts")
			rec := installSpawnRecorder(t, nil)

			got := lanePlan(repoDir, proj, []string{"src/a.ts"}).Lanes[0]
			if got.FenceErr == nil {
				t.Fatal("the fence admitted the lane")
			}
			for _, want := range []string{tc.want, laneField("web")} {
				if !strings.Contains(got.FenceErr.Error(), want) {
					t.Errorf("the refusal does not say %q: %v", want, got.FenceErr)
				}
			}
			if got.Relate != "" || got.Line != "" {
				t.Errorf("a fenced lane carries relate %q, line %q", got.Relate, got.Line)
			}

			if err := runCmd(t, Test(), "--files", "src/a.ts"); err == nil {
				t.Error("the gate ran a lane the fence refused")
			}
			if rec.count() != 0 {
				t.Errorf("the fence let %d spawn(s) through: %v", rec.count(), rec.lines)
			}
		})
	}
}

// TestLaneFenceRefusesVouchingWithNoTestCommand: with no test_command the lanes
// are the full run, so both a scoped and a bare run must stop at the fence. The
// bare one is fullLanePlan, which is why it is asserted too.
func TestLaneFenceRefusesVouchingWithNoTestCommand(t *testing.T) {
	dir := laneFixture(t)
	appendLanes(t, dir, relateLane)
	installLaneLookPath(t)
	touchFile(t, "src/a.ts")
	rec := installSpawnRecorder(t, nil)

	for _, args := range [][]string{{"--files", "src/a.ts"}, {}} {
		err := runCmd(t, Test(), args...)
		if err == nil {
			t.Fatalf("dross test %v ran a vouching lane with no test_command", args)
		}
		if !strings.Contains(err.Error(), "no runtime.test_command") || !strings.Contains(err.Error(), laneField("web")) {
			t.Errorf("dross test %v: the refusal does not name the rule and the lane: %v", args, err)
		}
	}
	if rec.count() != 0 {
		t.Errorf("the fence let %d spawn(s) through: %v", rec.count(), rec.lines)
	}
}

// grantLaneLines consents to every lane's whole line. grantAllLanes grants
// lane.Command, which a vouching lane's frame reads as stale, and a lane refused
// for consent would prove nothing about the refusal these tests are for.
func grantLaneLines(t *testing.T) {
	t.Helper()
	root, err := FindRoot()
	if err != nil {
		t.Fatal(err)
	}
	p, err := project.Load(filepath.Join(root, project.File))
	if err != nil {
		t.Fatal(err)
	}
	for _, lane := range p.Runtime.TestLane {
		if err := GrantLaneConsent(root, lane.Name, laneConsentLine(lane)); err != nil {
			t.Fatalf("grant lane %s: %v", lane.Name, err)
		}
	}
}

// TestConsentedVouchingLaneMeasuresNothingSoFailsTheRun: past its grant the lane
// has no Line until relate answers, and spawning that is `sh -c ""`, which exits
// 0. A relate that names nothing leaves it no line at all, so the prepare and
// relate are the whole run, and the exit is the nothing-measured one rather than
// consent's.
func TestConsentedVouchingLaneMeasuresNothingSoFailsTheRun(t *testing.T) {
	filesFixture(t, relateLane+"\nprepare = \"npm ci\"")
	grantLaneLines(t)
	touchFile(t, "src/a.ts")
	rec := installSpawnRecorder(t, nil)

	var out string
	err := runCmdCapturing(t, &out, Test(), "--files", "src/a.ts")
	if ExitCode(err) != exitNothingMeasured {
		t.Fatalf("exit = %d (%v), want %d", ExitCode(err), err, exitNothingMeasured)
	}
	if !strings.Contains(out, `selector miss: lane "web"`) {
		t.Errorf("the transcript does not name the lane:\n%s", out)
	}
	if want := []string{"npm ci", relateOverA}; !reflect.DeepEqual(rec.lines, want) {
		t.Errorf("spawned %q, want %q — the lane's command has nothing to run", rec.lines, want)
	}
}

// TestRefusedVouchingLanePaysForNoHost: a lane refused for its grant leaves
// before the probe and the sync, not only before its relate. grantedLaneFixture
// grants lane.Command, which a vouching lane's frame reads as stale.
func TestRefusedVouchingLanePaysForNoHost(t *testing.T) {
	grantedLaneFixture(t, relateLane)
	touchFile(t, "src/a.ts")
	log := &runLog{}
	log.probeSeam(t, nil, nil)
	log.spawnSeam(t)
	rec := installSpawnRecorder(t, nil)

	err := runCmd(t, Test(), "--files", "src/a.ts")
	if ExitCode(err) != exitLaneRefused {
		t.Fatalf("exit = %d (%v), want %d", ExitCode(err), err, exitLaneRefused)
	}
	if len(log.events) != 0 || log.probes != 0 {
		t.Errorf("the refused lane reached the host: %d probe(s), events %v", log.probes, log.events)
	}
	if rec.count() != 0 {
		t.Errorf("the refused lane spawned %d local command(s): %q", rec.count(), rec.lines)
	}
}

// TestVouchingRefusalFailsTheRunBesideAGreenLane: the refusal here is the parser's
// of relate's answer. The neighbour still runs, and its green must not stand for
// the run, because the paths in the refused lane were never measured.
func TestVouchingRefusalFailsTheRunBesideAGreenLane(t *testing.T) {
	filesFixture(t, relateLane+goNeighbourLane)
	grantLaneLines(t)
	touchFile(t, "src/a.ts")
	touchFile(t, "internal/cmd/here.go")
	seam := (&relateSeam{stdout: "RELATE-MALFORMED\n"}).install(t)

	err := runCmd(t, Test(), "--files", "src/a.ts", "--files", "internal/cmd/here.go")
	if ExitCode(err) != exitPrepareFailed {
		t.Fatalf("exit = %d (%v), want %d", ExitCode(err), err, exitPrepareFailed)
	}
	if want := []string{relateOverA, goNeighbourLine}; !reflect.DeepEqual(seam.lines, want) {
		t.Errorf("spawned %q, want relate and the neighbour's line, %q", seam.lines, want)
	}
}

// TestPlanWithholdsBuildConfigFromRelate: the plan is where config leaves the
// lane, so nothing downstream of it can show relate a path the run would not ask
// about. Config matched at any depth goes, and in the order it was named.
func TestPlanWithholdsBuildConfigFromRelate(t *testing.T) {
	repoDir, proj := planFixture(t, relateWideLane)
	touchFile(t, "src/a.ts")
	touchFile(t, "src/vite.config.ts")
	touchFile(t, "package.json")

	got := lanePlan(repoDir, proj, []string{"src/a.ts", "src/vite.config.ts", "package.json"}).Lanes[0]
	if got.Relate != relateOverA {
		t.Errorf("relate = %q, want %q", got.Relate, relateOverA)
	}
	if !reflect.DeepEqual(got.Asked, []string{"src/a.ts"}) {
		t.Errorf("asked = %v, want only the path relate is asked about", got.Asked)
	}
	if want := []string{"src/vite.config.ts", "package.json"}; !reflect.DeepEqual(got.Withheld, want) {
		t.Errorf("withheld = %v, want %v", got.Withheld, want)
	}
	if got.ScopedToNothing {
		t.Error("a lane with a path left to ask about is ScopedToNothing")
	}
}

// TestPlanOfOnlyBuildConfigHasNoRelateLine: a bare relate line would ask about
// nothing, so the lane has none, and Withheld is what tells it from a lane whose
// paths were all deleted.
func TestPlanOfOnlyBuildConfigHasNoRelateLine(t *testing.T) {
	repoDir, proj := planFixture(t, relateWideLane)
	touchFile(t, "package.json")

	got := lanePlan(repoDir, proj, []string{"package.json"}).Lanes[0]
	if got.Relate != "" || got.Asked != nil {
		t.Errorf("relate = %q, asked = %v, want neither", got.Relate, got.Asked)
	}
	if !got.ScopedToNothing {
		t.Error("a lane with nothing to ask about is not ScopedToNothing")
	}
	if !reflect.DeepEqual(got.Withheld, []string{"package.json"}) {
		t.Errorf("withheld = %v, want [package.json]", got.Withheld)
	}
}

// TestDeletedBuildConfigIsWithheldNotDropped: the fixed list reads names, and the
// gate refuses a deleted package.json as it refuses an edited one, so the run
// must name it. Filtered as dropped first, it would vanish from the transcript.
func TestDeletedBuildConfigIsWithheldNotDropped(t *testing.T) {
	repoDir, proj := planFixture(t, relateWideLane)
	touchFile(t, "src/a.ts")

	got := lanePlan(repoDir, proj, []string{"src/a.ts", "package.json"}).Lanes[0]
	if !reflect.DeepEqual(got.Withheld, []string{"package.json"}) {
		t.Errorf("withheld = %v, want [package.json]", got.Withheld)
	}
	if len(got.Dropped) != 0 {
		t.Errorf("dropped = %v, want none", got.Dropped)
	}
}

// TestPlanOfANonVouchingLaneWithholdsNothing: a plain lane's line is today's, so
// its build config stays on it.
func TestPlanOfANonVouchingLaneWithholdsNothing(t *testing.T) {
	const plain = `[[runtime.test_lane]]
name = "web"
match = ["package.json"]
command = "npx vitest run"
selector = "path"`
	repoDir, proj := planFixture(t, plain)
	touchFile(t, "package.json")

	got := lanePlan(repoDir, proj, []string{"package.json"}).Lanes[0]
	if got.Withheld != nil {
		t.Errorf("withheld = %v, want none", got.Withheld)
	}
	if want := "npx vitest run 'package.json'"; got.Line != want {
		t.Errorf("line = %q, want %q", got.Line, want)
	}
}

// TestPreviewRelateLineIsTheSpawnedOne is TestPlanLineIsTheSpawnedLine for a relate
// line carrying withheld config: one string, compared across preview and the run,
// so a filter on either side alone fails here.
func TestPreviewRelateLineIsTheSpawnedOne(t *testing.T) {
	relateFixture(t, relateWideLane, "src/a.ts", "package.json", "src/a.test.ts")
	seam := (&relateSeam{stdout: aToATest}).install(t)

	out := previewOut(t, "--files", "src/a.ts", "--files", "package.json")
	if err := runCmd(t, Test(), "--files", "src/a.ts", "--files", "package.json"); err != nil {
		t.Fatalf("dross test --files: %v", err)
	}
	if len(seam.lines) == 0 {
		t.Fatal("the run spawned nothing")
	}
	if !strings.Contains(out, "lane web relate: "+seam.lines[0]+"\n") {
		t.Errorf("preview does not show the relate line the run spawned (%q):\n%s", seam.lines[0], out)
	}
	if strings.Contains(seam.lines[0], "package.json") {
		t.Errorf("relate was asked about build config: %q", seam.lines[0])
	}
}
