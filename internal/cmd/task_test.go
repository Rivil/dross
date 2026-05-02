package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// helpers shared with cmd_test.go: chdir, runCmd, captureStdout, mustWrite.

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
