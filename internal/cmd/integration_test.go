package cmd

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestFullLifecycle walks the entire plan → execute → verify flow that a
// real /dross-* slash command session would drive, asserting state at
// each transition. This is the test that would have caught the
// dockerPrefix substring-match bug, the silent-state-touch bug, and the
// dangling /dross-status reference if it had existed earlier.
//
// What it covers:
//   1. dross init   — bootstrap
//   2. dross project set ... — runtime + identity (as /dross-onboard would)
//   3. dross phase create — phase dir
//   4. spec.toml written (as /dross-spec would)
//   5. plan.toml written (as /dross-plan would)
//   6. dross validate — schema + cross-file checks pass
//   7. simulated execute: task next → status in_progress → done →
//      changes record, looped per task
//   8. dross verify --skip-mutation — tests.json + verify.toml skeleton
//   9. dross status at every phase boundary — suggestNext heuristic
//      points at the right next slash command
//
// What it does NOT cover (intentional, marked clearly):
//   - real mutation runs (Stryker/Gremlins would need installs)
//   - real git commit creation (execute would; we simulate via SHA strings)
//   - the LLM judgement step in /dross-verify (criterion-to-test mapping)
func TestFullLifecycle(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)

	// --- 1. init ---
	if err := runCmd(t, Init()); err != nil {
		t.Fatalf("init: %v", err)
	}
	for _, f := range []string{
		".dross/project.toml",
		".dross/state.json",
		".dross/rules.toml",
	} {
		if !pathExists(filepath.Join(dir, f)) {
			t.Fatalf("init didn't create %s", f)
		}
	}

	// --- status after init: should hint at completing project.toml ---
	out := captureStdout(t, func() {
		runCmd(t, Status())
	})
	if !containsAny(out, "/dross-init", "/dross-onboard") {
		t.Errorf("status after bare init should suggest init/onboard:\n%s", out)
	}

	// --- 2. project setup (as /dross-onboard prompts user) ---
	mustRunSet(t, "project.name", "tagless")
	mustRunSet(t, "project.description", "tag-free meal planning")
	mustRunSet(t, "runtime.mode", "docker")
	mustRunSet(t, "runtime.dev_command", "docker compose up app")
	mustRunSet(t, "runtime.test_command", "docker compose exec app pnpm test")
	mustRunSet(t, "stack.languages", "typescript,go")

	// --- status after project setup: now points at phase create ---
	out = captureStdout(t, func() { runCmd(t, Status()) })
	if !strings.Contains(out, "phase create") {
		t.Errorf("status after project complete should suggest phase create:\n%s", out)
	}

	// --- 3. phase create ---
	if err := runCmd(t, Phase(), "create", "auth middleware"); err != nil {
		t.Fatalf("phase create: %v", err)
	}
	phaseID := "01-auth-middleware"
	phaseDir := filepath.Join(dir, ".dross/phases", phaseID)
	if !pathExists(phaseDir) {
		t.Fatalf("phase dir %s not created", phaseDir)
	}
	// Set as current phase (as the slash command would after create)
	if err := runCmd(t, State(), "set", "current_phase", phaseID); err != nil {
		t.Fatal(err)
	}

	// --- 4. spec.toml (as /dross-spec writes) ---
	mustWrite(t, filepath.Join(phaseDir, "spec.toml"), `[phase]
id = "`+phaseID+`"
title = "Auth middleware"

[[criteria]]
id = "c-1"
text = "Returns 401 on missing bearer token"

[[criteria]]
id = "c-2"
text = "Attaches user to request on valid token"

[[decisions]]
key = "token_lib"
choice = "jose"
why = "well-maintained, no native deps"
locked = true
`)

	// --- status after spec only: should suggest plan ---
	out = captureStdout(t, func() { runCmd(t, Status()) })
	if !strings.Contains(out, "/dross-plan") {
		t.Errorf("status with spec only should suggest /dross-plan:\n%s", out)
	}

	// --- 5. plan.toml (as /dross-plan writes after user accepts decomposition) ---
	mustWrite(t, filepath.Join(phaseDir, "plan.toml"), `[phase]
id = "`+phaseID+`"

[[task]]
id = "t-1"
wave = 1
title = "Add bearer token parser"
files = ["src/auth/parse.ts"]
description = "Extract Bearer prefix, validate JWT shape, return decoded claims or null."
covers = ["c-1"]
test_contract = ["missing token returns null", "malformed token returns null"]
status = "pending"

[[task]]
id = "t-2"
wave = 2
depends_on = ["t-1"]
title = "Wire middleware to request pipeline"
files = ["src/auth/middleware.ts", "src/server.ts"]
description = "Use parser; on null, send 401; on valid, attach user to request."
covers = ["c-1", "c-2"]
test_contract = ["unauthenticated request gets 401", "authenticated request has user attached"]
status = "pending"
`)

	// --- 6. validate — both spec and plan exist, covers references resolve ---
	if err := runCmd(t, Validate()); err != nil {
		t.Fatalf("validate after plan written: %v", err)
	}

	// --- status after plan: should point at execute, surface next runnable ---
	out = captureStdout(t, func() { runCmd(t, Status()) })
	if !strings.Contains(out, "/dross-execute") {
		t.Errorf("status with plan should suggest /dross-execute:\n%s", out)
	}
	if !strings.Contains(out, "next runnable: t-1") {
		t.Errorf("status should surface t-1 as next:\n%s", out)
	}

	// --- 7. simulated execute loop ---
	// Per task: task next → in_progress → simulate work → done + changes record
	executedTasks := []struct {
		id     string
		files  string
		commit string
	}{
		{"t-1", "src/auth/parse.ts", "abc1234"},
		{"t-2", "src/auth/middleware.ts,src/server.ts", "def5678"},
	}
	for _, task := range executedTasks {
		// dross task next → expected id
		nextOut := captureStdout(t, func() {
			runCmd(t, Task(), "next", phaseID)
		})
		gotNext := strings.TrimSpace(nextOut)
		if gotNext != task.id {
			t.Fatalf("task next: got %q want %q", gotNext, task.id)
		}

		// Mark in_progress (execute prompt does this before code)
		if err := runCmd(t, Task(), "status", phaseID, task.id, "in_progress"); err != nil {
			t.Fatal(err)
		}
		// While in_progress, task next should return nothing (or the next-after task if dep-free)
		// In this plan, t-2 depends on t-1, so during t-1 in_progress, task next is empty.
		if task.id == "t-1" {
			emptyOut := captureStdout(t, func() {
				runCmd(t, Task(), "next", phaseID)
			})
			if strings.TrimSpace(emptyOut) != "" {
				t.Errorf("during t-1 in_progress, next should be empty (t-2 blocked); got %q", emptyOut)
			}
		}

		// Simulate work happening… record changes + mark done
		if err := runCmd(t, Changes(), "record", phaseID, task.id,
			"--files", task.files,
			"--commit", task.commit); err != nil {
			t.Fatalf("changes record %s: %v", task.id, err)
		}
		if err := runCmd(t, Task(), "status", phaseID, task.id, "done"); err != nil {
			t.Fatal(err)
		}
	}

	// All tasks done → task next is empty
	emptyOut := captureStdout(t, func() {
		runCmd(t, Task(), "next", phaseID)
	})
	if strings.TrimSpace(emptyOut) != "" {
		t.Errorf("task next after all done should be empty; got %q", emptyOut)
	}

	// --- status after execute: should suggest /dross-verify ---
	out = captureStdout(t, func() { runCmd(t, Status()) })
	if !strings.Contains(out, "/dross-verify") {
		t.Errorf("status after execute should suggest /dross-verify:\n%s", out)
	}
	if !strings.Contains(out, "2/2 done") {
		t.Errorf("status should show 2/2 done:\n%s", out)
	}

	// --- 8. verify --skip-mutation ---
	if err := runCmd(t, Verify(), phaseID, "--skip-mutation"); err != nil {
		t.Fatalf("verify --skip-mutation: %v", err)
	}
	for _, f := range []string{"tests.json", "verify.toml"} {
		if !pathExists(filepath.Join(phaseDir, f)) {
			t.Errorf("verify didn't write %s", f)
		}
	}
	verifyBody := mustRead(t, filepath.Join(phaseDir, "verify.toml"))
	for _, want := range []string{
		`phase = "01-auth-middleware"`,
		`verdict = "pending"`, // LLM step would update this
		`criteria_total = 2`,
		`id = "c-1"`,
		`status = "unknown"`,
	} {
		if !strings.Contains(verifyBody, want) {
			t.Errorf("verify.toml missing %q\n--- body ---\n%s", want, verifyBody)
		}
	}

	// changes.json should record both tasks
	changesBody := mustRead(t, filepath.Join(phaseDir, "changes.json"))
	for _, want := range []string{`"t-1"`, `"t-2"`, `"abc1234"`, `"def5678"`, `"src/auth/parse.ts"`} {
		if !strings.Contains(changesBody, want) {
			t.Errorf("changes.json missing %q\n--- body ---\n%s", want, changesBody)
		}
	}

	// --- 9. final validate: every artefact still consistent ---
	if err := runCmd(t, Validate()); err != nil {
		t.Fatalf("validate at end of lifecycle: %v", err)
	}
}

// TestLifecycleResumeAfterInterruption simulates the case where execute
// pauses partway through (e.g. user hits Ctrl+C or quits). Restarting
// the loop should pick up where it left off, not reset.
func TestLifecycleResumeAfterInterruption(t *testing.T) {
	chdir(t, t.TempDir())
	if err := runCmd(t, Init()); err != nil {
		t.Fatal(err)
	}
	mustRunSet(t, "project.name", "x")
	mustRunSet(t, "runtime.mode", "native")
	if err := runCmd(t, Phase(), "create", "x"); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, ".dross/phases/01-x/spec.toml", `[phase]
id = "01-x"
title = "x"
[[criteria]]
id = "c-1"
text = "x"
`)
	mustWrite(t, ".dross/phases/01-x/plan.toml", `[phase]
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
wave = 2
depends_on = ["t-1"]
title = "interrupted"
files = ["b.ts"]
covers = ["c-1"]
status = "in_progress"
[[task]]
id = "t-3"
wave = 3
depends_on = ["t-2"]
title = "third"
files = ["c.ts"]
covers = ["c-1"]
`)

	// task next: t-2 is in_progress (not pending), t-3 is blocked.
	// NextRunnable returns nothing — execute prompt would prompt user
	// to resume t-2 rather than auto-pick.
	out := captureStdout(t, func() {
		runCmd(t, Task(), "next", "01-x")
	})
	if strings.TrimSpace(out) != "" {
		t.Errorf("with in_progress task and downstream blocked, next should be empty (resume manually); got %q", out)
	}

	// Resume by completing t-2
	if err := runCmd(t, Task(), "status", "01-x", "t-2", "done"); err != nil {
		t.Fatal(err)
	}
	out = captureStdout(t, func() {
		runCmd(t, Task(), "next", "01-x")
	})
	if strings.TrimSpace(out) != "t-3" {
		t.Errorf("after resuming t-2, next should be t-3; got %q", out)
	}
}

// helpers

func pathExists(p string) bool {
	return fileExists(p) // helper in status.go
}

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}
