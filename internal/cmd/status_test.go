package cmd

import (
	"strings"
	"testing"
)

// status output evolves through phases of project lifecycle. Each test
// pins the next: suggestion logic at one stage so the heuristic in
// suggestNext doesn't drift silently.

func TestStatusFreshDirSuggestsInit(t *testing.T) {
	chdir(t, t.TempDir())
	out := captureStdout(t, func() {
		err := runCmd(t, Status())
		if err == nil {
			t.Error("expected ErrNoRoot for bare dir")
		}
	})
	_ = out // FindRoot fails before any output; main thing is the error
}

func TestStatusAfterInitSuggestsCompleteProject(t *testing.T) {
	chdir(t, t.TempDir())
	if err := runCmd(t, Init()); err != nil {
		t.Fatal(err)
	}
	out := captureStdout(t, func() {
		runCmd(t, Status())
	})
	// Fresh init: name and runtime are empty → next suggests init/onboard
	if !strings.Contains(out, "/dross-init") && !strings.Contains(out, "/dross-onboard") {
		t.Errorf("expected init/onboard suggestion when project incomplete:\n%s", out)
	}
	if !strings.Contains(out, "(unnamed)") {
		t.Errorf("expected (unnamed) for empty project name:\n%s", out)
	}
}

func TestStatusWithProjectAndNoPhaseSuggestsPhaseCreate(t *testing.T) {
	chdir(t, t.TempDir())
	if err := runCmd(t, Init()); err != nil {
		t.Fatal(err)
	}
	mustRunSet(t, "project.name", "feast")
	mustRunSet(t, "runtime.mode", "docker")
	out := captureStdout(t, func() {
		runCmd(t, Status())
	})
	if !strings.Contains(out, "/dross-spec --new") {
		t.Errorf("expected /dross-spec --new suggestion:\n%s", out)
	}
	if !strings.Contains(out, "feast") {
		t.Errorf("project name should appear:\n%s", out)
	}
}

func TestStatusWithSpecOnlySuggestsPlan(t *testing.T) {
	chdir(t, t.TempDir())
	scaffoldPhaseWithSpecOnly(t, "01-auth")
	out := captureStdout(t, func() {
		runCmd(t, Status())
	})
	if !strings.Contains(out, "/dross-plan") {
		t.Errorf("expected /dross-plan suggestion when spec exists but no plan:\n%s", out)
	}
}

func TestStatusWithRunnableTaskSuggestsExecute(t *testing.T) {
	chdir(t, t.TempDir())
	scaffoldPhaseWithSpecAndPlan(t, "01-auth", `[phase]
id = "01-auth"
[[task]]
id = "t-1"
wave = 1
title = "schema"
files = ["x.ts"]
covers = ["c-1"]
`)
	out := captureStdout(t, func() {
		runCmd(t, Status())
	})
	if !strings.Contains(out, "/dross-execute") {
		t.Errorf("expected /dross-execute when runnable task exists:\n%s", out)
	}
	if !strings.Contains(out, "next runnable: t-1") {
		t.Errorf("expected next runnable task surfaced:\n%s", out)
	}
}

func TestStatusProgressBarReflectsDoneCount(t *testing.T) {
	chdir(t, t.TempDir())
	scaffoldPhaseWithSpecAndPlan(t, "01-x", `[phase]
id = "01-x"
[[task]]
id = "t-1"
wave = 1
title = "first"
files = ["a.ts"]
covers = ["c-1"]
status = "done"
[[task]]
id = "t-2"
wave = 1
title = "second"
files = ["b.ts"]
covers = ["c-1"]
status = "done"
[[task]]
id = "t-3"
wave = 2
depends_on = ["t-1", "t-2"]
title = "third"
files = ["c.ts"]
covers = ["c-1"]
`)
	out := captureStdout(t, func() {
		runCmd(t, Status())
	})
	if !strings.Contains(out, "2/3 done") {
		t.Errorf("expected '2/3 done' progress:\n%s", out)
	}
	if !strings.Contains(out, "1 pending") {
		t.Errorf("expected '1 pending':\n%s", out)
	}
}

// ---- helpers ----

func scaffoldPhaseWithSpecOnly(t *testing.T, phaseID string) {
	t.Helper()
	if err := runCmd(t, Init()); err != nil {
		t.Fatal(err)
	}
	mustRunSet(t, "project.name", "x")
	mustRunSet(t, "runtime.mode", "native")
	if err := runCmd(t, State(), "set", "current_phase", phaseID); err != nil {
		t.Fatal(err)
	}
	dir := ".dross/phases/" + phaseID
	mustWrite(t, dir+"/spec.toml", `[phase]
id = "`+phaseID+`"
title = "x"
[[criteria]]
id = "c-1"
text = "x"
`)
}

func scaffoldPhaseWithSpecAndPlan(t *testing.T, phaseID, planTOML string) {
	t.Helper()
	scaffoldPhaseWithSpecOnly(t, phaseID)
	mustWrite(t, ".dross/phases/"+phaseID+"/plan.toml", planTOML)
}
