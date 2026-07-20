package cmd

import (
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
