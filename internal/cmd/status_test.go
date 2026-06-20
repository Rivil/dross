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

func TestStatusWithProjectAndNoMilestoneSuggestsMilestoneScope(t *testing.T) {
	chdir(t, t.TempDir())
	if err := runCmd(t, Init()); err != nil {
		t.Fatal(err)
	}
	mustRunSet(t, "project.name", "feast")
	mustRunSet(t, "runtime.mode", "docker")
	out := captureStdout(t, func() {
		runCmd(t, Status())
	})
	if !strings.Contains(out, "/dross-milestone") {
		t.Errorf("expected /dross-milestone suggestion when project complete but no milestone:\n%s", out)
	}
	if !strings.Contains(out, "feast") {
		t.Errorf("project name should appear:\n%s", out)
	}
}

func TestStatusWithMilestoneAndNoPhaseSuggestsPhaseCreate(t *testing.T) {
	chdir(t, t.TempDir())
	if err := runCmd(t, Init()); err != nil {
		t.Fatal(err)
	}
	mustRunSet(t, "project.name", "feast")
	mustRunSet(t, "runtime.mode", "docker")
	if err := runCmd(t, State(), "set", "current_milestone", "v0.1"); err != nil {
		t.Fatal(err)
	}
	out := captureStdout(t, func() {
		runCmd(t, Status())
	})
	if !strings.Contains(out, "/dross-spec --new") {
		t.Errorf("expected /dross-spec --new suggestion once milestone is scoped:\n%s", out)
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

func TestStatusSurfacesPendingVerdicts(t *testing.T) {
	chdir(t, t.TempDir())
	scaffoldPhaseWithSpecAndPlan(t, "01-a", `[phase]
id = "01-a"
[[task]]
id = "t-1"
wave = 1
title = "x"
files = ["x.ts"]
covers = ["c-1"]
status = "done"
`)
	// Two phases with verify.toml: one pending, one filled in.
	mustWrite(t, ".dross/phases/01-a/verify.toml", `verdict = ""
`)
	// Create another phase dir with a finalized verdict — should NOT appear in pending list.
	mustWrite(t, ".dross/phases/02-b/spec.toml", `[phase]
id = "02-b"
title = "b"
[[criteria]]
id = "c-1"
text = "x"
`)
	mustWrite(t, ".dross/phases/02-b/verify.toml", `verdict = "pass"
`)
	// And one with explicit "pending" verdict.
	mustWrite(t, ".dross/phases/03-c/spec.toml", `[phase]
id = "03-c"
title = "c"
[[criteria]]
id = "c-1"
text = "x"
`)
	mustWrite(t, ".dross/phases/03-c/verify.toml", `verdict = "pending"
`)

	out := captureStdout(t, func() {
		runCmd(t, Status())
	})
	if !strings.Contains(out, "pending:") {
		t.Errorf("expected pending verdict section in status:\n%s", out)
	}
	if !strings.Contains(out, "01-a") {
		t.Errorf("expected 01-a (empty verdict) flagged as pending:\n%s", out)
	}
	if !strings.Contains(out, "03-c") {
		t.Errorf("expected 03-c (explicit pending) flagged:\n%s", out)
	}
	if strings.Contains(out, "02-b") {
		t.Errorf("02-b is pass and should NOT appear in pending list:\n%s", out)
	}
	if !strings.Contains(out, "dross verify finalize") {
		t.Errorf("expected the remediation command surfaced:\n%s", out)
	}
}

func TestStatusOmitsPendingSectionWhenNoVerifyFiles(t *testing.T) {
	chdir(t, t.TempDir())
	scaffoldPhaseWithSpecOnly(t, "01-clean")
	out := captureStdout(t, func() {
		runCmd(t, Status())
	})
	if strings.Contains(out, "pending:") {
		t.Errorf("no verify files = no pending section:\n%s", out)
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

func TestStatusSurfacesOpenHandoff(t *testing.T) {
	chdir(t, t.TempDir())
	scaffoldPhaseWithSpecOnly(t, "01-h")
	mustWrite(t, ".dross/handoff.md", `# Handoff — paused 2026-06-03 14:33
phase: 01-h · branch: phase/01-h · v0.1.0.0

## Next
- [ ] apply the guard in issue.go:142

## Open loops
- [ ] delete the dead retry loop
`)
	out := captureStdout(t, func() {
		runCmd(t, Status())
	})
	if !strings.Contains(out, "handoff:") {
		t.Errorf("expected handoff nudge in status:\n%s", out)
	}
	if !strings.Contains(out, "2026-06-03 14:33") {
		t.Errorf("expected the header timestamp surfaced, not just mtime:\n%s", out)
	}
	if !strings.Contains(out, "2 item(s) left") {
		t.Errorf("expected the open-item count:\n%s", out)
	}
	if !strings.Contains(out, "/dross-resume") {
		t.Errorf("expected the resume command surfaced:\n%s", out)
	}
}

func TestStatusOmitsHandoffWhenAbsentOrEmpty(t *testing.T) {
	chdir(t, t.TempDir())
	scaffoldPhaseWithSpecOnly(t, "01-clean")
	out := captureStdout(t, func() {
		runCmd(t, Status())
	})
	if strings.Contains(out, "handoff:") {
		t.Errorf("no handoff file = no handoff nudge:\n%s", out)
	}
	// An empty file is treated as no handoff (resume deletes when cleared,
	// but guard against a 0-byte straggler too).
	mustWrite(t, ".dross/handoff.md", "")
	out = captureStdout(t, func() {
		runCmd(t, Status())
	})
	if strings.Contains(out, "handoff:") {
		t.Errorf("empty handoff file should not nudge:\n%s", out)
	}
}

// renderActionAreas is the pure formatter for the non-spine `actions:` block.
// These two tests pin its contract: unavailable areas are never shown as
// runnable, available areas emit their command.

func TestRenderActionAreasUnavailableShowsPlannedNotRunnable(t *testing.T) {
	lines := renderActionAreas([]actionArea{
		{label: "security", command: "/dross-secure", available: false},
	})
	if len(lines) != 1 {
		t.Fatalf("expected 1 line, got %d: %v", len(lines), lines)
	}
	if !strings.Contains(lines[0], "(planned)") {
		t.Errorf("unavailable area must be marked (planned), not presented as runnable:\n%s", lines[0])
	}
}

func TestRenderActionAreasAvailableEmitsCommand(t *testing.T) {
	lines := renderActionAreas([]actionArea{
		{label: "security", command: "/dross-secure", available: true},
	})
	if len(lines) != 1 {
		t.Fatalf("expected 1 line, got %d: %v", len(lines), lines)
	}
	if !strings.Contains(lines[0], "/dross-secure") {
		t.Errorf("available area must emit its command line:\n%s", lines[0])
	}
	if strings.Contains(lines[0], "(planned)") {
		t.Errorf("available area must NOT be marked planned:\n%s", lines[0])
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
	if err := runCmd(t, State(), "set", "current_milestone", "v0.1"); err != nil {
		t.Fatal(err)
	}
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
