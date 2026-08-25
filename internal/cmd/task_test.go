package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/phase"
)

// helpers shared with cmd_test.go: chdir, runCmd, captureStdout, mustWrite,
// mustRead.

// assertPlanUnchanged fails if plan.toml differs from the given snapshot — the
// byte-unchanged guarantee a rejected mutation must uphold (mirrors the
// mustRead byte-compare in TestPhaseMoveNoOp).
func assertPlanUnchanged(t *testing.T, planPath, before string) {
	t.Helper()
	if after := mustRead(t, planPath); after != before {
		t.Errorf("plan.toml was mutated:\n--- before ---\n%s\n--- after ---\n%s", before, after)
	}
}

// twoTaskPlan is a plan.toml body with t-1 (wave 1) and t-2 (wave 1), both
// covering c-1 — the shared fixture for the add/remove/edit wiring tests.
const twoTaskPlan = `[phase]
id = "01-test"
[[task]]
id = "t-1"
wave = 1
title = "first"
files = ["a.go"]
covers = ["c-1"]
[[task]]
id = "t-2"
wave = 1
title = "second"
files = ["b.go"]
covers = ["c-1"]
`

// depPlan is twoTaskPlan with t-2 depending on t-1 — the fixture for the
// dependency-safe remove tests.
const depPlan = `[phase]
id = "01-test"
[[task]]
id = "t-1"
wave = 1
title = "first"
files = ["a.go"]
covers = ["c-1"]
[[task]]
id = "t-2"
wave = 2
title = "second"
files = ["b.go"]
covers = ["c-1"]
depends_on = ["t-1"]
`

// scaffoldPhaseWithPlan creates a phase dir with both spec.toml and plan.toml.
// Tests that don't need spec/plan content can chain mustRunSet + Phase create
// directly; this is for tests that need a runnable plan to exist.
func scaffoldPhaseWithPlan(t *testing.T, phaseID, planTOML string) {
	t.Helper()
	if err := runCmd(t, Init()); err != nil {
		t.Fatal(err)
	}
	mustRunSet(t, "project.name", "x")
	mustRunSet(t, "runtime.mode", "native")
	dir := filepath.Join(".dross", "phases", phaseID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(dir, "spec.toml"), `[phase]
id = "`+phaseID+`"
title = "test"
[[criteria]]
id = "c-1"
text = "x"
`)
	mustWrite(t, filepath.Join(dir, "plan.toml"), planTOML)
	trustFixture(t)
}

func TestTaskNextEmpty(t *testing.T) {
	chdir(t, t.TempDir())
	scaffoldPhaseWithPlan(t, "01-test", `[phase]
id = "01-test"
[[task]]
id = "t-1"
wave = 1
title = "x"
files = ["a.ts"]
covers = ["c-1"]
status = "done"
`)
	// All tasks done → next prints nothing, exits 0
	out := captureStdout(t, func() {
		if err := runCmd(t, Task(), "next", "01-test"); err != nil {
			t.Errorf("next: %v", err)
		}
	})
	if strings.TrimSpace(out) != "" {
		t.Errorf("expected empty stdout when all done; got %q", out)
	}
}

func TestTaskNextRespectsWaveAndDeps(t *testing.T) {
	chdir(t, t.TempDir())
	scaffoldPhaseWithPlan(t, "01-test", `[phase]
id = "01-test"
[[task]]
id = "t-1"
wave = 1
title = "first"
files = ["a.ts"]
covers = ["c-1"]
[[task]]
id = "t-2"
wave = 2
depends_on = ["t-1"]
title = "second"
files = ["b.ts"]
covers = ["c-1"]
[[task]]
id = "t-3"
wave = 1
title = "parallel"
files = ["c.ts"]
covers = ["c-1"]
`)

	// Wave 1: alphabetic first → t-1
	out := captureStdout(t, func() {
		runCmd(t, Task(), "next", "01-test")
	})
	if strings.TrimSpace(out) != "t-1" {
		t.Errorf("first next: got %q want t-1", strings.TrimSpace(out))
	}

	// Mark t-1 done; t-3 still wave 1, lower id than t-2's wave-2-blocked status
	if err := runCmd(t, Task(), "status", "01-test", "t-1", "done"); err != nil {
		t.Fatal(err)
	}
	out = captureStdout(t, func() {
		runCmd(t, Task(), "next", "01-test")
	})
	if strings.TrimSpace(out) != "t-3" {
		t.Errorf("after t-1 done: got %q want t-3 (wave 1)", strings.TrimSpace(out))
	}

	// Mark t-3 done → t-2 unblocked
	if err := runCmd(t, Task(), "status", "01-test", "t-3", "done"); err != nil {
		t.Fatal(err)
	}
	out = captureStdout(t, func() {
		runCmd(t, Task(), "next", "01-test")
	})
	if strings.TrimSpace(out) != "t-2" {
		t.Errorf("after t-3 done: got %q want t-2", strings.TrimSpace(out))
	}
}

func TestTaskShowMissingTask(t *testing.T) {
	chdir(t, t.TempDir())
	scaffoldPhaseWithPlan(t, "01-test", `[phase]
id = "01-test"
[[task]]
id = "t-1"
wave = 1
title = "only"
files = ["a.ts"]
covers = ["c-1"]
`)
	err := runCmd(t, Task(), "show", "01-test", "nope")
	if err == nil {
		t.Fatal("expected error for missing task")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error should mention not found: %v", err)
	}
}

func TestTaskStatusValidatesValue(t *testing.T) {
	chdir(t, t.TempDir())
	scaffoldPhaseWithPlan(t, "01-test", `[phase]
id = "01-test"
[[task]]
id = "t-1"
wave = 1
title = "x"
files = ["a.ts"]
covers = ["c-1"]
`)
	err := runCmd(t, Task(), "status", "01-test", "t-1", "garbage")
	if err == nil {
		t.Fatal("expected error for invalid status value")
	}
	if !strings.Contains(err.Error(), "invalid status") {
		t.Errorf("error message should mention invalid status: %v", err)
	}
}

func TestTaskStatusMissingTask(t *testing.T) {
	chdir(t, t.TempDir())
	scaffoldPhaseWithPlan(t, "01-test", `[phase]
id = "01-test"
[[task]]
id = "t-1"
wave = 1
title = "x"
files = ["a.ts"]
covers = ["c-1"]
`)
	err := runCmd(t, Task(), "status", "01-test", "nope", "done")
	if err == nil {
		t.Fatal("expected error for missing task id")
	}
}

func TestTaskAddTailAppend(t *testing.T) {
	chdir(t, t.TempDir())
	scaffoldPhaseWithPlan(t, "01-test", twoTaskPlan)
	planPath := filepath.Join(".dross", "phases", "01-test", "plan.toml")

	// No placement flag: appends at the tail with the high-water+1 id.
	if err := runCmd(t, Task(), "add", "01-test", "--title", "third", "--covers", "c-1"); err != nil {
		t.Fatalf("add (tail): %v", err)
	}
	plan, err := phase.LoadPlan(planPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Task) != 3 {
		t.Fatalf("want 3 tasks after add, got %d", len(plan.Task))
	}
	last := plan.Task[2]
	if last.ID != "t-3" {
		t.Errorf("new id = %s, want t-3 (high-water+1)", last.ID)
	}
	if last.Title != "third" {
		t.Errorf("new title = %q, want third", last.Title)
	}
	// The default-append path must NOT route through resolveAnchor (which errors
	// when neither --after/--before is set) — success above already proves it.
	if err := runCmd(t, Validate()); err != nil {
		t.Errorf("validate after add: %v", err)
	}
}

func TestTaskAddAfterAnchor(t *testing.T) {
	chdir(t, t.TempDir())
	scaffoldPhaseWithPlan(t, "01-test", twoTaskPlan)
	planPath := filepath.Join(".dross", "phases", "01-test", "plan.toml")

	if err := runCmd(t, Task(), "add", "01-test", "--title", "mid", "--covers", "c-1", "--after", "t-1"); err != nil {
		t.Fatalf("add --after t-1: %v", err)
	}
	plan, err := phase.LoadPlan(planPath)
	if err != nil {
		t.Fatal(err)
	}
	var order []string
	for _, tk := range plan.Task {
		order = append(order, tk.ID)
	}
	if got, want := strings.Join(order, ","), "t-1,t-3,t-2"; got != want {
		t.Errorf("order = %s, want %s", got, want)
	}
	// Existing tasks keep their ids and titles (placement never renumbers).
	if plan.FindTask("t-1").Title != "first" || plan.FindTask("t-2").Title != "second" {
		t.Errorf("placement renumbered/altered existing tasks: %+v", plan.Task)
	}
}

func TestTaskAddRejectsUnknownCriterion(t *testing.T) {
	chdir(t, t.TempDir())
	scaffoldPhaseWithPlan(t, "01-test", twoTaskPlan)
	planPath := filepath.Join(".dross", "phases", "01-test", "plan.toml")
	before := mustRead(t, planPath)

	// --covers c-99 must be rejected pre-write; plan.toml stays byte-unchanged.
	if err := runCmd(t, Task(), "add", "01-test", "--title", "bad", "--covers", "c-99"); err == nil {
		t.Fatal("expected add with unknown criterion to fail")
	}
	assertPlanUnchanged(t, planPath, before)
}

func TestTaskRemoveRefusesDependedOn(t *testing.T) {
	chdir(t, t.TempDir())
	scaffoldPhaseWithPlan(t, "01-test", depPlan)
	planPath := filepath.Join(".dross", "phases", "01-test", "plan.toml")
	before := mustRead(t, planPath)

	err := runCmd(t, Task(), "remove", "01-test", "t-1")
	if err == nil {
		t.Fatal("expected refusal when t-1 is depended on")
	}
	if !strings.Contains(err.Error(), "t-2") {
		t.Errorf("refusal should name the dependent t-2: %v", err)
	}
	assertPlanUnchanged(t, planPath, before)
}

func TestTaskRemoveForceStripsAndKeepsHighWater(t *testing.T) {
	chdir(t, t.TempDir())
	scaffoldPhaseWithPlan(t, "01-test", depPlan)
	planPath := filepath.Join(".dross", "phases", "01-test", "plan.toml")

	if err := runCmd(t, Task(), "remove", "01-test", "t-1", "--force"); err != nil {
		t.Fatalf("remove --force: %v", err)
	}
	plan, err := phase.LoadPlan(planPath)
	if err != nil {
		t.Fatal(err)
	}
	if plan.FindTask("t-1") != nil {
		t.Error("t-1 not removed under --force")
	}
	if slices.Contains(plan.FindTask("t-2").DependsOn, "t-1") {
		t.Error("dangling dep: t-1 must be stripped from t-2.depends_on")
	}
	if err := runCmd(t, Validate()); err != nil {
		t.Errorf("validate after force-remove: %v", err)
	}

	// A subsequent add must NOT reuse the freed id t-1 (high-water).
	if err := runCmd(t, Task(), "add", "01-test", "--title", "new", "--covers", "c-1"); err != nil {
		t.Fatalf("add after remove: %v", err)
	}
	plan, err = phase.LoadPlan(planPath)
	if err != nil {
		t.Fatal(err)
	}
	if plan.FindTask("t-1") != nil {
		t.Error("freed id t-1 was reused by a subsequent add")
	}
}

func TestTaskRemoveUnknownID(t *testing.T) {
	chdir(t, t.TempDir())
	scaffoldPhaseWithPlan(t, "01-test", twoTaskPlan)
	planPath := filepath.Join(".dross", "phases", "01-test", "plan.toml")
	before := mustRead(t, planPath)

	if err := runCmd(t, Task(), "remove", "01-test", "t-99"); err == nil {
		t.Fatal("expected error removing unknown id")
	}
	assertPlanUnchanged(t, planPath, before)
}

func TestTaskEditPartialUpdate(t *testing.T) {
	chdir(t, t.TempDir())
	scaffoldPhaseWithPlan(t, "01-test", depPlan) // t-2: wave 2, covers c-1, depends_on t-1
	planPath := filepath.Join(".dross", "phases", "01-test", "plan.toml")

	if err := runCmd(t, Task(), "edit", "01-test", "t-2", "--title", "New"); err != nil {
		t.Fatalf("edit --title: %v", err)
	}
	plan, err := phase.LoadPlan(planPath)
	if err != nil {
		t.Fatal(err)
	}
	t2 := plan.FindTask("t-2")
	if t2.Title != "New" {
		t.Errorf("title = %q, want New", t2.Title)
	}
	// Every other field preserved.
	if t2.Wave != 2 {
		t.Errorf("wave = %d, want 2 (preserved)", t2.Wave)
	}
	if !slices.Equal(t2.Covers, []string{"c-1"}) {
		t.Errorf("covers = %v, want [c-1] (preserved)", t2.Covers)
	}
	if !slices.Equal(t2.DependsOn, []string{"t-1"}) {
		t.Errorf("depends_on = %v, want [t-1] (preserved)", t2.DependsOn)
	}
}

func TestTaskEditRejectsUnknownCriterion(t *testing.T) {
	chdir(t, t.TempDir())
	scaffoldPhaseWithPlan(t, "01-test", twoTaskPlan)
	planPath := filepath.Join(".dross", "phases", "01-test", "plan.toml")
	before := mustRead(t, planPath)

	if err := runCmd(t, Task(), "edit", "01-test", "t-2", "--covers", "c-99"); err == nil {
		t.Fatal("expected edit with unknown criterion to fail")
	}
	assertPlanUnchanged(t, planPath, before)
}

// TestTaskEditFilesRoundTrip: files was the one plan.toml field with no CLI
// edit surface, so repointing a task meant hand-editing the file this
// subsystem advertises as never needing a hand edit. --files replaces the whole
// list, like --covers and --depends-on, and the proof reads plan.toml back off
// disk rather than the in-memory plan.
func TestTaskEditFilesRoundTrip(t *testing.T) {
	chdir(t, t.TempDir())
	scaffoldPhaseWithPlan(t, "01-test", twoTaskPlan) // t-2: files = ["b.go"]
	planPath := filepath.Join(".dross", "phases", "01-test", "plan.toml")

	if err := runCmd(t, Task(), "edit", "01-test", "t-2", "--files", "x/one.go,x/two.go"); err != nil {
		t.Fatalf("edit --files: %v", err)
	}
	plan, err := phase.LoadPlan(planPath)
	if err != nil {
		t.Fatal(err)
	}
	t2 := plan.FindTask("t-2")
	if !slices.Equal(t2.Files, []string{"x/one.go", "x/two.go"}) {
		t.Errorf("files = %v, want [x/one.go x/two.go] — replaced, not merged", t2.Files)
	}
	// A partial update: nothing else moved.
	if t2.Title != "second" || !slices.Equal(t2.Covers, []string{"c-1"}) {
		t.Errorf("t-2 = %+v, want title/covers preserved", t2)
	}
	// And the other task is untouched.
	if !slices.Equal(plan.FindTask("t-1").Files, []string{"a.go"}) {
		t.Errorf("t-1 files = %v, want [a.go]", plan.FindTask("t-1").Files)
	}
}

// TestTaskEditFilesDistinguishesAbsentFromEmpty: "--files=" is an instruction
// to clear the list, and no --files at all is an instruction to leave it alone.
// Collapsing the two either makes a task unclearable or silently wipes files on
// every unrelated edit.
func TestTaskEditFilesDistinguishesAbsentFromEmpty(t *testing.T) {
	chdir(t, t.TempDir())
	scaffoldPhaseWithPlan(t, "01-test", twoTaskPlan)
	planPath := filepath.Join(".dross", "phases", "01-test", "plan.toml")

	// Flag absent: files preserved.
	if err := runCmd(t, Task(), "edit", "01-test", "t-2", "--title", "renamed"); err != nil {
		t.Fatalf("edit --title: %v", err)
	}
	plan, err := phase.LoadPlan(planPath)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(plan.FindTask("t-2").Files, []string{"b.go"}) {
		t.Errorf("files = %v, want [b.go] — an unset --files must not touch the array", plan.FindTask("t-2").Files)
	}

	// Flag present and empty: files cleared.
	if err := runCmd(t, Task(), "edit", "01-test", "t-2", "--files", ""); err != nil {
		t.Fatalf("edit --files=: %v", err)
	}
	plan, err = phase.LoadPlan(planPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.FindTask("t-2").Files) != 0 {
		t.Errorf("files = %v, want empty — `--files=` clears the list", plan.FindTask("t-2").Files)
	}
}

// TestTaskEditFilesGoesThroughSaveIfValid: the files edit is validated with the
// rest of the command, not written ahead of it. An edit that also breaks the
// plan is refused whole — plan.toml keeps its original files.
func TestTaskEditFilesGoesThroughSaveIfValid(t *testing.T) {
	chdir(t, t.TempDir())
	scaffoldPhaseWithPlan(t, "01-test", twoTaskPlan)
	planPath := filepath.Join(".dross", "phases", "01-test", "plan.toml")
	before := mustRead(t, planPath)

	if err := runCmd(t, Task(), "edit", "01-test", "t-2", "--files", "x/one.go", "--covers", "c-99"); err == nil {
		t.Fatal("expected the unknown criterion to refuse the whole edit")
	}
	assertPlanUnchanged(t, planPath, before)
}

func TestTaskEditHasNoStatusFlag(t *testing.T) {
	chdir(t, t.TempDir())
	scaffoldPhaseWithPlan(t, "01-test", twoTaskPlan)

	// --status must be an unknown flag: `dross task status` stays the sole owner.
	err := runCmd(t, Task(), "edit", "01-test", "t-2", "--status", "done")
	if err == nil {
		t.Fatal("task edit must not accept a --status flag")
	}
	if !strings.Contains(err.Error(), "unknown flag") {
		t.Errorf("want an unknown-flag error, got: %v", err)
	}
}

func TestTaskShowFormatsAllFields(t *testing.T) {
	chdir(t, t.TempDir())
	scaffoldPhaseWithPlan(t, "01-test", `[phase]
id = "01-test"
[[task]]
id = "t-1"
wave = 2
title = "schema"
files = ["db/schema.ts", "db/migrations/0042.sql"]
description = """
Drizzle schema for tags.
Two-line description.
"""
covers = ["c-1"]
depends_on = ["t-0"]
test_contract = ["unique constraint rejects dup", "case-insensitive lookup"]
status = "in_progress"
`)
	out := captureStdout(t, func() {
		runCmd(t, Task(), "show", "01-test", "t-1")
	})
	for _, want := range []string{
		"id:           t-1",
		"title:        schema",
		"wave:         2",
		"status:       in_progress",
		"db/schema.ts",
		"db/migrations/0042.sql",
		"covers:       c-1",
		"depends_on:   t-0",
		"unique constraint rejects dup",
		"case-insensitive lookup",
		"Drizzle schema for tags.",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("show output missing %q\n--- output ---\n%s", want, out)
		}
	}
}

// threeTaskPlan is twoTaskPlan plus a third wave-1 task — the fixture for the
// move wiring tests (all pending, no deps, so any reorder is legal).
const threeTaskPlan = `[phase]
id = "01-test"
[[task]]
id = "t-1"
wave = 1
title = "first"
files = ["a.go"]
covers = ["c-1"]
[[task]]
id = "t-2"
wave = 1
title = "second"
files = ["b.go"]
covers = ["c-1"]
[[task]]
id = "t-3"
wave = 1
title = "third"
files = ["c.go"]
covers = ["c-1"]
`

func TestTaskMoveRepositions(t *testing.T) {
	chdir(t, t.TempDir())
	scaffoldPhaseWithPlan(t, "01-test", threeTaskPlan)
	planPath := filepath.Join(".dross", "phases", "01-test", "plan.toml")

	if err := runCmd(t, Task(), "move", "01-test", "t-3", "--before", "t-1"); err != nil {
		t.Fatalf("move: %v", err)
	}
	plan, err := phase.LoadPlan(planPath)
	if err != nil {
		t.Fatal(err)
	}
	var order []string
	for _, tk := range plan.Task {
		order = append(order, tk.ID)
	}
	if want := []string{"t-3", "t-1", "t-2"}; !slices.Equal(order, want) {
		t.Errorf("reloaded order = %v, want %v", order, want)
	}
}

// TestTaskMoveFlagValidation pins the resolveAnchor contract: both flags or
// neither produce its exact error strings, with no file write either way.
func TestTaskMoveFlagValidation(t *testing.T) {
	chdir(t, t.TempDir())
	scaffoldPhaseWithPlan(t, "01-test", threeTaskPlan)
	planPath := filepath.Join(".dross", "phases", "01-test", "plan.toml")
	pre := mustRead(t, planPath)

	err := runCmd(t, Task(), "move", "01-test", "t-1", "--before", "t-2", "--after", "t-3")
	if err == nil || err.Error() != "pass exactly one of --after or --before, not both" {
		t.Errorf("both flags: got %v, want resolveAnchor's exact both-flags error", err)
	}
	err = runCmd(t, Task(), "move", "01-test", "t-1")
	if err == nil || err.Error() != "pass exactly one of --after or --before" {
		t.Errorf("neither flag: got %v, want resolveAnchor's exact neither-flag error", err)
	}
	assertPlanUnchanged(t, planPath, pre)
}

// TestTaskMoveRejectedLeavesFileUntouched pins the byte-identity guarantee: an
// illegal move (mover after its dependent) errors with plan.toml byte-unchanged.
func TestTaskMoveRejectedLeavesFileUntouched(t *testing.T) {
	chdir(t, t.TempDir())
	scaffoldPhaseWithPlan(t, "01-test", depPlan)
	planPath := filepath.Join(".dross", "phases", "01-test", "plan.toml")
	pre := mustRead(t, planPath)

	err := runCmd(t, Task(), "move", "01-test", "t-1", "--after", "t-2")
	if err == nil {
		t.Fatal("expected rejection: t-1 moved after its dependent t-2")
	}
	assertPlanUnchanged(t, planPath, pre)
}

// TestTaskMoveThenNext proves the move->next chain end to end: after moving
// t-2 before t-1 (both wave 1, pending), `task next` returns t-2.
func TestTaskMoveThenNext(t *testing.T) {
	chdir(t, t.TempDir())
	scaffoldPhaseWithPlan(t, "01-test", twoTaskPlan)

	if err := runCmd(t, Task(), "move", "01-test", "t-2", "--before", "t-1"); err != nil {
		t.Fatalf("move: %v", err)
	}
	out := captureStdout(t, func() {
		if err := runCmd(t, Task(), "next", "01-test"); err != nil {
			t.Errorf("next: %v", err)
		}
	})
	if strings.TrimSpace(out) != "t-2" {
		t.Errorf("next after move = %q, want t-2", strings.TrimSpace(out))
	}
}

// TestTaskMoveKeepsID pins end-to-end id stability: after a move `task show`
// still resolves the mover, and its id appears exactly once in the saved file.
func TestTaskMoveKeepsID(t *testing.T) {
	chdir(t, t.TempDir())
	scaffoldPhaseWithPlan(t, "01-test", threeTaskPlan)
	planPath := filepath.Join(".dross", "phases", "01-test", "plan.toml")

	if err := runCmd(t, Task(), "move", "01-test", "t-3", "--before", "t-1"); err != nil {
		t.Fatalf("move: %v", err)
	}
	out := captureStdout(t, func() {
		if err := runCmd(t, Task(), "show", "01-test", "t-3"); err != nil {
			t.Errorf("show after move: %v", err)
		}
	})
	if !strings.Contains(out, "id:           t-3") {
		t.Errorf("show t-3 after move did not resolve:\n%s", out)
	}
	if got := strings.Count(mustRead(t, planPath), `"t-3"`); got != 1 {
		t.Errorf("saved file contains %d occurrences of id \"t-3\", want exactly 1", got)
	}
}

// TestTaskMoveCrossWaveValidates proves the re-derived waves survive the full
// validator: after a legal cross-wave move, `dross validate` reports no problems.
func TestTaskMoveCrossWaveValidates(t *testing.T) {
	chdir(t, t.TempDir())
	scaffoldPhaseWithPlan(t, "01-test", `[phase]
id = "01-test"
[[task]]
id = "t-1"
wave = 1
title = "first"
files = ["a.go"]
covers = ["c-1"]
[[task]]
id = "t-2"
wave = 2
title = "second"
files = ["b.go"]
covers = ["c-1"]
`)
	planPath := filepath.Join(".dross", "phases", "01-test", "plan.toml")

	if err := runCmd(t, Task(), "move", "01-test", "t-1", "--after", "t-2"); err != nil {
		t.Fatalf("cross-wave move: %v", err)
	}
	plan, err := phase.LoadPlan(planPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := plan.FindTask("t-1").Wave; got != 2 {
		t.Errorf("mover wave = %d, want 2 (adopted from anchor)", got)
	}
	if err := runCmd(t, Validate()); err != nil {
		t.Errorf("dross validate after legal cross-wave move: %v", err)
	}
}

// listPlan is a 3-task/2-wave plan where t-2 carries no `status` field at all —
// the fixture that pins both the row count and the orPending rendering rule.
const listPlan = `[phase]
id = "01-test"
[[task]]
id = "t-1"
wave = 1
title = "first"
files = ["a.go"]
covers = ["c-1"]
status = "done"
[[task]]
id = "t-2"
wave = 1
title = "second"
files = ["b.go"]
covers = ["c-1"]
[[task]]
id = "t-3"
wave = 2
title = "third"
files = ["c.go"]
covers = ["c-1"]
status = "failed"
`

func TestTaskListTable(t *testing.T) {
	chdir(t, t.TempDir())
	scaffoldPhaseWithPlan(t, "01-test", listPlan)

	out := captureStdout(t, func() {
		if err := runCmd(t, Task(), "list", "01-test"); err != nil {
			t.Errorf("list: %v", err)
		}
	})
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 4 {
		t.Fatalf("want header + 3 rows, got %d lines:\n%s", len(lines), out)
	}
	for _, col := range []string{"ID", "WAVE", "STATUS", "TITLE"} {
		if !strings.Contains(lines[0], col) {
			t.Errorf("header %q missing column %s", lines[0], col)
		}
	}
	// Each row carries id, wave, status and title.
	want := [][]string{
		{"t-1", "1", "done", "first"},
		{"t-2", "1", "pending", "second"}, // status absent in plan.toml → pending
		{"t-3", "2", "failed", "third"},
	}
	for i, w := range want {
		for _, field := range w {
			if !strings.Contains(lines[i+1], field) {
				t.Errorf("row %d = %q, missing %q", i+1, lines[i+1], field)
			}
		}
	}
	// A blank status column would leave "second" preceded by whitespace only.
	if strings.Contains(out, "  second") && !strings.Contains(out, "pending") {
		t.Error("absent status rendered blank instead of pending")
	}
}

func TestTaskListJSON(t *testing.T) {
	chdir(t, t.TempDir())
	scaffoldPhaseWithPlan(t, "01-test", listPlan)

	out := captureStdout(t, func() {
		if err := runCmd(t, Task(), "list", "01-test", "--json"); err != nil {
			t.Errorf("list --json: %v", err)
		}
	})
	var rows []struct {
		ID     string `json:"id"`
		Wave   int    `json:"wave"`
		Status string `json:"status"`
		Title  string `json:"title"`
	}
	if err := json.Unmarshal([]byte(out), &rows); err != nil {
		t.Fatalf("--json did not emit a JSON array (table-only regression?): %v\ngot: %q", err, out)
	}
	if len(rows) != 3 {
		t.Fatalf("want 3 rows, got %d", len(rows))
	}
	if rows[0].ID != "t-1" || rows[0].Wave != 1 || rows[0].Status != "done" || rows[0].Title != "first" {
		t.Errorf("row 0 = %+v", rows[0])
	}
	if rows[1].Status != "pending" {
		t.Errorf("absent status in JSON = %q, want pending", rows[1].Status)
	}
	if rows[2].Wave != 2 {
		t.Errorf("row 2 wave = %d, want 2", rows[2].Wave)
	}
}

func TestTaskListJSONEmptyPlanIsEmptyArray(t *testing.T) {
	chdir(t, t.TempDir())
	scaffoldPhaseWithPlan(t, "01-test", `[phase]
id = "01-test"
`)
	out := captureStdout(t, func() {
		if err := runCmd(t, Task(), "list", "01-test", "--json"); err != nil {
			t.Errorf("list --json on empty plan: %v", err)
		}
	})
	if got := strings.TrimSpace(out); got != "[]" {
		t.Errorf("empty plan --json = %q, want []", got)
	}
}

func TestTaskListDefaultsToCurrentPhase(t *testing.T) {
	chdir(t, t.TempDir())
	scaffoldPhaseWithPlan(t, "01-test", listPlan)
	if err := runCmd(t, State(), "set", "current_phase", "01-test"); err != nil {
		t.Fatal(err)
	}

	out := captureStdout(t, func() {
		if err := runCmd(t, Task(), "list"); err != nil {
			t.Errorf("list with no phase-id: %v", err)
		}
	})
	if !strings.Contains(out, "t-1") || !strings.Contains(out, "t-3") {
		t.Errorf("no-arg list did not list current_phase's tasks:\n%s", out)
	}
}

func TestTaskListNoPhaseIDAndNoCurrentPhase(t *testing.T) {
	chdir(t, t.TempDir())
	scaffoldPhaseWithPlan(t, "01-test", listPlan)
	// current_phase is empty out of `dross init`.

	err := runCmd(t, Task(), "list")
	if err == nil {
		t.Fatal("want an error when neither a phase-id nor current_phase is available")
	}
	if !strings.Contains(err.Error(), "current_phase") {
		t.Errorf("error = %q, want it to name current_phase (not a .dross/phases//plan.toml path)", err)
	}
	if strings.Contains(err.Error(), "phases//") {
		t.Errorf("empty id was turned into a path: %q", err)
	}
}

func TestTaskListUnknownPhaseErrorsBeforeHeader(t *testing.T) {
	chdir(t, t.TempDir())
	scaffoldPhaseWithPlan(t, "01-test", listPlan)

	var err error
	out := captureStdout(t, func() {
		err = runCmd(t, Task(), "list", "99-nope")
	})
	if err == nil {
		t.Fatal("want an error for an unknown phase-id")
	}
	if !strings.Contains(err.Error(), "99-nope") {
		t.Errorf("error = %q, want it to name the missing path", err)
	}
	if strings.Contains(out, "ID") || strings.Contains(out, "WAVE") {
		t.Errorf("table header printed before the load failed:\n%s", out)
	}
}

// contractPlan is twoTaskPlan with t-1 already carrying a two-statement
// contract — the fixture for the replace/append tests.
const contractPlan = `[phase]
id = "01-test"
[[task]]
id = "t-1"
wave = 1
title = "first"
files = ["a.go"]
covers = ["c-1"]
test_contract = ["existing one", "existing two"]
[[task]]
id = "t-2"
wave = 1
title = "second"
files = ["b.go"]
covers = ["c-1"]
`

func planTask(t *testing.T, planPath, id string) phase.Task {
	t.Helper()
	plan, err := phase.LoadPlan(planPath)
	if err != nil {
		t.Fatal(err)
	}
	task := plan.FindTask(id)
	if task == nil {
		t.Fatalf("task %s missing from %s", id, planPath)
	}
	return *task
}

// TestTestContractKeepsCommas is the reason --test-contract is a StringArray
// and not a StringSlice. A contract statement is prose — "if X breaks, TestY
// fails" — so it contains commas by construction. Splitting one statement into
// two produces a plan that still parses and still looks plausible in review,
// which is the kind of corruption nobody catches by eye.
func TestTestContractKeepsCommas(t *testing.T) {
	chdir(t, t.TempDir())
	scaffoldPhaseWithPlan(t, "01-test", twoTaskPlan)
	planPath := filepath.Join(".dross", "phases", "01-test", "plan.toml")

	stmt := "if the unique constraint stops rejecting dupes, TestTagsAreUnique fails, which is the whole invariant"
	if err := runCmd(t, Task(), "add", "01-test", "--title", "third", "--covers", "c-1", "--test-contract", stmt); err != nil {
		t.Fatalf("add: %v", err)
	}
	got := planTask(t, planPath, "t-3").TestContract
	if len(got) != 1 {
		t.Fatalf("test_contract = %v (%d entries), want exactly 1 — the commas must not split it", got, len(got))
	}
	if got[0] != stmt {
		t.Errorf("statement = %q, want it verbatim: %q", got[0], stmt)
	}
}

func TestTestContractRepeats(t *testing.T) {
	chdir(t, t.TempDir())
	scaffoldPhaseWithPlan(t, "01-test", twoTaskPlan)
	planPath := filepath.Join(".dross", "phases", "01-test", "plan.toml")

	want := []string{"first statement", "second statement", "third statement"}
	if err := runCmd(t, Task(), "add", "01-test", "--title", "third", "--covers", "c-1",
		"--test-contract", want[0], "--test-contract", want[1], "--test-contract", want[2]); err != nil {
		t.Fatalf("add: %v", err)
	}
	if got := planTask(t, planPath, "t-3").TestContract; !slices.Equal(got, want) {
		t.Errorf("test_contract = %v, want %v in the order given", got, want)
	}
}

func TestEditReplacesTestContract(t *testing.T) {
	chdir(t, t.TempDir())
	scaffoldPhaseWithPlan(t, "01-test", contractPlan)
	planPath := filepath.Join(".dross", "phases", "01-test", "plan.toml")

	if err := runCmd(t, Task(), "edit", "01-test", "t-1", "--test-contract", "only this"); err != nil {
		t.Fatalf("edit: %v", err)
	}
	if got := planTask(t, planPath, "t-1").TestContract; !slices.Equal(got, []string{"only this"}) {
		t.Errorf("test_contract = %v, want the list replaced wholesale", got)
	}
}

// TestAddTestContractAppends covers the case the phase exists for: a plan
// review adds a fourth statement to a task that already has three, and having
// to retype the three is the hand-editing this command was built to remove.
func TestAddTestContractAppends(t *testing.T) {
	chdir(t, t.TempDir())
	scaffoldPhaseWithPlan(t, "01-test", contractPlan)
	planPath := filepath.Join(".dross", "phases", "01-test", "plan.toml")

	if err := runCmd(t, Task(), "edit", "01-test", "t-1", "--add-test-contract", "appended"); err != nil {
		t.Fatalf("edit: %v", err)
	}
	want := []string{"existing one", "existing two", "appended"}
	if got := planTask(t, planPath, "t-1").TestContract; !slices.Equal(got, want) {
		t.Errorf("test_contract = %v, want %v — the existing entries keep their order and come first", got, want)
	}
}

// TestTestContractReplaceAndAppendConflict: the two flags disagree about the
// result and either resolution silently drops something the user asked for, so
// the pair is refused rather than reconciled.
func TestTestContractReplaceAndAppendConflict(t *testing.T) {
	chdir(t, t.TempDir())
	scaffoldPhaseWithPlan(t, "01-test", contractPlan)
	planPath := filepath.Join(".dross", "phases", "01-test", "plan.toml")
	before := mustRead(t, planPath)

	err := runCmd(t, Task(), "edit", "01-test", "t-1",
		"--test-contract", "replacement", "--add-test-contract", "addition")
	if err == nil {
		t.Fatal("expected a refusal when both --test-contract and --add-test-contract are given")
	}
	assertPlanUnchanged(t, planPath, before)
}

// TestEditWithoutContractFlagsLeavesPlanByteIdentical pins the Changed() gate:
// an edit that touches only the title must not rewrite the contract, or every
// unrelated edit becomes a way to lose one.
func TestEditWithoutContractFlagsLeavesPlanByteIdentical(t *testing.T) {
	chdir(t, t.TempDir())
	scaffoldPhaseWithPlan(t, "01-test", contractPlan)
	planPath := filepath.Join(".dross", "phases", "01-test", "plan.toml")

	if err := runCmd(t, Task(), "edit", "01-test", "t-1", "--title", "renamed"); err != nil {
		t.Fatalf("edit: %v", err)
	}
	got := planTask(t, planPath, "t-1")
	if got.Title != "renamed" {
		t.Errorf("title = %q, want renamed", got.Title)
	}
	if want := []string{"existing one", "existing two"}; !slices.Equal(got.TestContract, want) {
		t.Errorf("test_contract = %v, want it untouched (%v)", got.TestContract, want)
	}
}

func TestEditSetsDescriptionFromFlag(t *testing.T) {
	chdir(t, t.TempDir())
	scaffoldPhaseWithPlan(t, "01-test", twoTaskPlan)
	planPath := filepath.Join(".dross", "phases", "01-test", "plan.toml")

	if err := runCmd(t, Task(), "edit", "01-test", "t-1", "--description", "what it does"); err != nil {
		t.Fatalf("edit: %v", err)
	}
	if got := planTask(t, planPath, "t-1").Description; got != "what it does" {
		t.Errorf("description = %q, want %q", got, "what it does")
	}
}
