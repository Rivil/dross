package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Rivil/dross/internal/findings"
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

// Milestone-level progress: status must surface how many of the milestone's
// phases are verified (N/M phases), distinct from the current phase's task
// count. Pins the bug where only per-phase task progress (2/2) was shown and
// the milestone looked complete when phases remained.
func TestStatusShowsMilestonePhaseProgress(t *testing.T) {
	chdir(t, t.TempDir())
	scaffoldPhaseWithSpecOnly(t, "01-a")
	// A milestone toml listing five phases, two of which are verified.
	mustWrite(t, ".dross/milestones/v0.1.toml", `phases = ["01-a", "02-b", "03-c", "04-d", "05-e"]

[milestone]
  version = "v0.1"
  title = "First release"
`)
	mustWrite(t, ".dross/phases/01-a/verify.toml", "verdict = \"pass\"\n")
	mustWrite(t, ".dross/phases/02-b/verify.toml", "verdict = \"pass\"\n")
	mustWrite(t, ".dross/phases/03-c/verify.toml", "verdict = \"partial\"\n")
	out := captureStdout(t, func() {
		runCmd(t, Status())
	})
	if !strings.Contains(out, "2/5 phases") {
		t.Errorf("expected milestone phase progress '2/5 phases' (only 01-a and 02-b are pass):\n%s", out)
	}
	if !strings.Contains(out, "First release") {
		t.Errorf("expected milestone title surfaced:\n%s", out)
	}
}

// When the milestone toml is absent (current_milestone set but not scoped),
// status falls back to the bare name with no phase bar — no crash.
func TestStatusMilestoneFallsBackToBareNameWhenNoToml(t *testing.T) {
	chdir(t, t.TempDir())
	scaffoldPhaseWithSpecOnly(t, "01-a") // sets current_milestone=v0.1, no toml written
	out := captureStdout(t, func() {
		runCmd(t, Status())
	})
	if !strings.Contains(out, "milestone: v0.1") {
		t.Errorf("expected bare milestone name fallback:\n%s", out)
	}
	if strings.Contains(out, "phases") {
		t.Errorf("no milestone toml = no phase progress bar:\n%s", out)
	}
}

// The actions block is shown only when the spine is idle. These three pin
// c-1 (shown between phases / when verified) and c-3 (suppressed mid-phase).

func TestStatusShowsActionsBetweenPhases(t *testing.T) {
	chdir(t, t.TempDir())
	if err := runCmd(t, Init()); err != nil {
		t.Fatal(err)
	}
	mustRunSet(t, "project.name", "feast")
	mustRunSet(t, "runtime.mode", "native")
	if err := runCmd(t, State(), "set", "current_milestone", "v0.1"); err != nil {
		t.Fatal(err)
	}
	out := captureStdout(t, func() {
		runCmd(t, Status())
	})
	if !strings.Contains(out, "actions:") {
		t.Errorf("expected actions block between phases:\n%s", out)
	}
	// Fresh repo: no area has run, so each shows "never run" — and now that the
	// commands ship, nothing is "(planned)".
	if !strings.Contains(out, "security") || !strings.Contains(out, "never run") {
		t.Errorf("expected non-spine areas with run-signal text:\n%s", out)
	}
	if strings.Contains(out, "(planned)") {
		t.Errorf("areas whose commands ship must not show (planned):\n%s", out)
	}
}

func TestStatusSuppressesActionsMidPhase(t *testing.T) {
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
	if strings.Contains(out, "actions:") {
		t.Errorf("active phase with a runnable task must not show the actions block:\n%s", out)
	}
}

func TestStatusShowsActionsWhenVerifiedPass(t *testing.T) {
	chdir(t, t.TempDir())
	scaffoldPhaseWithSpecAndPlan(t, "01-done", `[phase]
id = "01-done"
[[task]]
id = "t-1"
wave = 1
title = "schema"
files = ["x.ts"]
covers = ["c-1"]
status = "done"
`)
	mustWrite(t, ".dross/phases/01-done/verify.toml", `verdict = "pass"
`)
	out := captureStdout(t, func() {
		runCmd(t, Status())
	})
	if !strings.Contains(out, "actions:") {
		t.Errorf("verified/pass phase with no runnable task must show the actions block:\n%s", out)
	}
}

func TestStatusSuppressesActionsWhenVerifyPending(t *testing.T) {
	chdir(t, t.TempDir())
	scaffoldPhaseWithSpecAndPlan(t, "01-pend", `[phase]
id = "01-pend"
[[task]]
id = "t-1"
wave = 1
title = "schema"
files = ["x.ts"]
covers = ["c-1"]
status = "done"
`)
	// Tasks done but no verify.toml yet → verify is a pending spine step, so the
	// non-spine surface must stay suppressed (c-3).
	out := captureStdout(t, func() {
		runCmd(t, Status())
	})
	if strings.Contains(out, "actions:") {
		t.Errorf("verify-pending phase (tasks done, no verify.toml) must suppress the actions block:\n%s", out)
	}
}

func TestStatusSuppressesActionsWhenVerifyNotPass(t *testing.T) {
	for _, verdict := range []string{"", "pending", "partial", "fail"} {
		t.Run(verdict, func(t *testing.T) {
			chdir(t, t.TempDir())
			scaffoldPhaseWithSpecAndPlan(t, "01-v", `[phase]
id = "01-v"
[[task]]
id = "t-1"
wave = 1
title = "schema"
files = ["x.ts"]
covers = ["c-1"]
status = "done"
`)
			mustWrite(t, ".dross/phases/01-v/verify.toml", "verdict = \""+verdict+"\"\n")
			out := captureStdout(t, func() {
				runCmd(t, Status())
			})
			if strings.Contains(out, "actions:") {
				t.Errorf("verdict %q is not pass → actions block must be suppressed:\n%s", verdict, out)
			}
		})
	}
}

// renderActionAreas is the pure formatter for the non-spine `actions:` block.
// These two tests pin its contract: unavailable areas are never shown as
// runnable, available areas emit their command.

func TestRenderActionAreasUnavailableShowsPlannedNotRunnable(t *testing.T) {
	lines := renderActionAreas([]areaSignal{
		{area: actionArea{label: "future", command: "/dross-future", available: false}},
	}, time.Now())
	if len(lines) != 1 {
		t.Fatalf("expected 1 line, got %d: %v", len(lines), lines)
	}
	if !strings.Contains(lines[0], "(planned)") {
		t.Errorf("unavailable area must be marked (planned), not presented as runnable:\n%s", lines[0])
	}
}

func TestRenderActionAreasAvailableEmitsCommand(t *testing.T) {
	lines := renderActionAreas([]areaSignal{
		{area: actionArea{label: "security", command: "/dross-secure", available: true}},
	}, time.Now())
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

// TestFormatRunSignal pins c-2's rendering: never-run, a relative "<n>d ago" for
// a past run, and a clamp (no negative/absurd age) for a future timestamp.
func TestFormatRunSignal(t *testing.T) {
	now := time.Date(2026, 6, 27, 12, 0, 0, 0, time.UTC)
	if got := formatRunSignal(now, time.Time{}); got != "never run" {
		t.Errorf("zero time = %q, want \"never run\"", got)
	}
	if got := formatRunSignal(now, now.Add(-72*time.Hour)); !strings.Contains(got, "3d ago") {
		t.Errorf("3-days-ago = %q, want it to contain \"3d ago\"", got)
	}
	future := formatRunSignal(now, now.Add(48*time.Hour))
	if strings.Contains(future, "-") || strings.Contains(future, "ago") && strings.Contains(future, "-") {
		t.Errorf("future last_run rendered a negative age: %q", future)
	}
	if future != "last run just now" {
		t.Errorf("future last_run = %q, want the clamped \"last run just now\"", future)
	}
}

// TestRankAreas pins c-1's ordering: never-run first (stable catalog order),
// then ran areas oldest-last-run first, with ties keeping catalog order.
func TestRankAreas(t *testing.T) {
	now := time.Date(2026, 6, 27, 12, 0, 0, 0, time.UTC)
	in := []areaSignal{
		{area: actionArea{label: "security"}, lastRun: now.Add(-5 * 24 * time.Hour)},
		{area: actionArea{label: "quality"}},                                  // never run
		{area: actionArea{label: "tech-debt"}, lastRun: now.Add(-1 * 24 * time.Hour)},
	}
	got := rankAreas(in)
	order := []string{got[0].area.label, got[1].area.label, got[2].area.label}
	want := []string{"quality", "security", "tech-debt"} // never-run, then 5d, then 1d
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("rank order = %v, want %v", order, want)
		}
	}

	// Tie-break: two never-run areas keep catalog order; identical timestamps too.
	tie := []areaSignal{
		{area: actionArea{label: "a"}},
		{area: actionArea{label: "b"}},
	}
	if r := rankAreas(tie); r[0].area.label != "a" || r[1].area.label != "b" {
		t.Errorf("never-run tie did not preserve catalog order: %v", []string{r[0].area.label, r[1].area.label})
	}
}

// TestStatusActionsRankedAndSignalled pins c-1+c-2 end to end: with security=5d,
// quality=never, tech-debt=1d, the idle actions block lists quality, then
// security, then tech-debt, each line carrying its run-signal text.
func TestStatusActionsRankedAndSignalled(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	scaffoldPhaseWithSpecAndPlan(t, "01-done", `[phase]
id = "01-done"
[[task]]
id = "t-1"
wave = 1
title = "schema"
files = ["x.ts"]
covers = ["c-1"]
status = "done"
`)
	mustWrite(t, ".dross/phases/01-done/verify.toml", "verdict = \"pass\"\n")

	root := filepath.Join(dir, ".dross")
	now := time.Now().UTC()
	stampArea(t, root, "security", now.Add(-5*24*time.Hour))
	stampArea(t, root, "techdebt", now.Add(-1*24*time.Hour))
	// quality: leave unstamped → never run.

	out := captureStdout(t, func() { runCmd(t, Status()) })

	qi, si, ti := strings.Index(out, "quality"), strings.Index(out, "security"), strings.Index(out, "tech-debt")
	if qi < 0 || si < 0 || ti < 0 {
		t.Fatalf("an area is missing from the actions block:\n%s", out)
	}
	if !(qi < si && si < ti) {
		t.Fatalf("ranking order wrong (want quality < security < tech-debt):\n%s", out)
	}
	if !strings.Contains(out, "never run") || !strings.Contains(out, "5d ago") || !strings.Contains(out, "1d ago") {
		t.Fatalf("actions block missing run-signal text:\n%s", out)
	}
}

// TestStatusSignalPruneProof pins the prune-proof contract: with no run dirs left
// but state.toml retaining last_run, the area still renders "last run …", never
// "never run".
func TestStatusSignalPruneProof(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	scaffoldPhaseWithSpecAndPlan(t, "01-done", `[phase]
id = "01-done"
[[task]]
id = "t-1"
wave = 1
title = "schema"
files = ["x.ts"]
covers = ["c-1"]
status = "done"
`)
	mustWrite(t, ".dross/phases/01-done/verify.toml", "verdict = \"pass\"\n")

	root := filepath.Join(dir, ".dross")
	// Stamp tech-debt but create NO run dirs under .dross/techdebt — simulating
	// pruned run dirs with the durable state.toml still present.
	stampArea(t, root, "techdebt", time.Now().UTC().Add(-2*24*time.Hour))

	out := captureStdout(t, func() { runCmd(t, Status()) })
	line := actionLine(t, out, "tech-debt")
	if !strings.Contains(line, "last run") || strings.Contains(line, "never run") {
		t.Fatalf("pruned-run-dir tech-debt line should read 'last run', got %q", line)
	}
}

// TestActionAreaCommandsResolve pins c-3's no-dead-command guard: every available
// area's command resolves to a real surface — a slash command's assets file, or a
// CLI command registered on the root in main.go.
func TestActionAreaCommandsResolve(t *testing.T) {
	repo := repoRootFromTest(t)
	mainGo, err := os.ReadFile(filepath.Join(repo, "cmd", "dross", "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range actionCatalog {
		if !a.available {
			continue
		}
		switch {
		case strings.HasPrefix(a.command, "/"):
			name := strings.TrimPrefix(a.command, "/")
			p := filepath.Join(repo, "assets", "commands", name+".md")
			if _, err := os.Stat(p); err != nil {
				t.Errorf("area %q points at %q but %s is missing", a.label, a.command, p)
			}
		case strings.HasPrefix(a.command, "dross "):
			sub := strings.TrimPrefix(a.command, "dross ")
			fn := "cmd." + strings.ToUpper(sub[:1]) + sub[1:] + "()"
			if !strings.Contains(string(mainGo), fn) {
				t.Errorf("area %q points at %q but %s is not registered in main.go", a.label, a.command, fn)
			}
		default:
			t.Errorf("area %q has unrecognized command form %q", a.label, a.command)
		}
	}
}

// stampArea writes a state.toml under .dross/<area>/ with the given last_run.
func stampArea(t *testing.T, root, area string, lr time.Time) {
	t.Helper()
	dir := filepath.Join(root, area)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := findings.SaveStore(filepath.Join(dir, "state.toml"), &findings.Store{LastRun: lr}); err != nil {
		t.Fatal(err)
	}
}

// actionLine returns the first output line containing label, failing if none.
func actionLine(t *testing.T, out, label string) string {
	t.Helper()
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, label) {
			return l
		}
	}
	t.Fatalf("no %q line in:\n%s", label, out)
	return ""
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
