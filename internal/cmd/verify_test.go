package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/project"
)

// These tests exercise the verify CLI's dispatch + skeleton-write
// paths without invoking real mutation tools (which require Stryker
// and Gremlins to be installed). --skip-mutation forces all files
// into Skipped, but tests.json + verify.toml still get written —
// that's what we assert.

func TestVerifyEmptyChangesIsNoop(t *testing.T) {
	chdir(t, t.TempDir())
	if err := runCmd(t, Init()); err != nil {
		t.Fatal(err)
	}
	mustRunSet(t, "project.name", "x")
	mustRunSet(t, "runtime.mode", "native")
	if err := runCmd(t, Phase(), "create", "tags"); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, ".dross/phases/01-tags/spec.toml", `[phase]
id = "01-tags"
title = "x"
[[criteria]]
id = "c-1"
text = "x"
`)
	// No changes recorded yet → verify exits without writing tests.json
	out := captureStdout(t, func() {
		if err := runCmd(t, Verify(), "01-tags"); err != nil {
			t.Errorf("verify with empty changes should not error: %v", err)
		}
	})
	if !strings.Contains(out, "no changes recorded") {
		t.Errorf("expected 'no changes recorded' message:\n%s", out)
	}
	if _, err := os.Stat(".dross/phases/01-tags/tests.json"); err == nil {
		t.Error("tests.json should not exist when verify is a no-op")
	}
}

func TestVerifyWritesSkeletonWithSkipMutation(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	if err := runCmd(t, Init()); err != nil {
		t.Fatal(err)
	}
	mustRunSet(t, "project.name", "x")
	mustRunSet(t, "runtime.mode", "native")
	if err := runCmd(t, Phase(), "create", "auth"); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, ".dross/phases/01-auth/spec.toml", `[phase]
id = "01-auth"
title = "auth"
[[criteria]]
id = "c-1"
text = "user can sign up"
[[criteria]]
id = "c-2"
text = "session expires"
`)
	mustWrite(t, ".dross/phases/01-auth/plan.toml", `[phase]
id = "01-auth"
[[task]]
id = "t-1"
wave = 1
title = "x"
files = ["src/auth.ts"]
covers = ["c-1", "c-2"]
`)
	if err := runCmd(t, Changes(), "record", "01-auth", "t-1",
		"--files", "src/auth.ts,src/db/users.go,static/login.html",
		"--commit", "abc1234"); err != nil {
		t.Fatal(err)
	}

	if err := runCmd(t, Verify(), "01-auth", "--skip-mutation"); err != nil {
		t.Fatalf("verify --skip-mutation: %v", err)
	}

	// tests.json: all three files should be in Skipped (no adapters with --skip-mutation)
	testsBody := mustRead(t, filepath.Join(dir, ".dross/phases/01-auth/tests.json"))
	for _, want := range []string{
		`"phase": "01-auth"`,
		`"src/auth.ts"`,
		`"src/db/users.go"`,
		`"static/login.html"`,
	} {
		if !strings.Contains(testsBody, want) {
			t.Errorf("tests.json missing %q\n--- body ---\n%s", want, testsBody)
		}
	}

	// verify.toml: skeleton with verdict=pending and 2 criteria seeded as unknown
	verifyBody := mustRead(t, filepath.Join(dir, ".dross/phases/01-auth/verify.toml"))
	for _, want := range []string{
		`phase = "01-auth"`,
		`verdict = "pending"`,
		`criteria_total = 2`,
		`id = "c-1"`,
		`id = "c-2"`,
		`status = "unknown"`,
	} {
		if !strings.Contains(verifyBody, want) {
			t.Errorf("verify.toml missing %q\n--- body ---\n%s", want, verifyBody)
		}
	}
}

func TestVerifyMissingSpecErrors(t *testing.T) {
	chdir(t, t.TempDir())
	if err := runCmd(t, Init()); err != nil {
		t.Fatal(err)
	}
	mustRunSet(t, "project.name", "x")
	mustRunSet(t, "runtime.mode", "native")
	// phase dir without spec.toml
	if err := runCmd(t, Phase(), "create", "naked"); err != nil {
		t.Fatal(err)
	}
	if err := runCmd(t, Changes(), "record", "01-naked", "t-1", "--files", "a.ts"); err != nil {
		t.Fatal(err)
	}

	err := runCmd(t, Verify(), "01-naked", "--skip-mutation")
	if err == nil {
		t.Fatal("expected error when spec.toml is missing")
	}
	if !strings.Contains(err.Error(), "/dross-spec") {
		t.Errorf("error should hint at /dross-spec: %v", err)
	}
}

// TestVerifyFinalizeRecordsResolvedVerdict drives the
// `dross verify finalize` subcommand and asserts a telemetry outcome
// event with the resolved verdict (not pending) hits the log. This
// is the closing half of the verify lifecycle — the mechanical run
// emits verdict=pending; finalize emits the LLM's pass/partial/fail.
func TestVerifyFinalizeRecordsResolvedVerdict(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("DROSS_NO_TELEMETRY", "") // re-enable telemetry recording

	if err := runCmd(t, Init()); err != nil {
		t.Fatal(err)
	}
	mustRunSet(t, "project.name", "x")
	mustRunSet(t, "runtime.mode", "native")
	if err := runCmd(t, Phase(), "create", "auth"); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, ".dross/phases/01-auth/spec.toml", `[phase]
id = "01-auth"
title = "auth"
[[criteria]]
id = "c-1"
text = "x"
`)
	mustWrite(t, ".dross/phases/01-auth/plan.toml", `[phase]
id = "01-auth"
[[task]]
id = "t-1"
wave = 1
title = "x"
files = ["src/auth.ts"]
covers = ["c-1"]
`)
	if err := runCmd(t, Changes(), "record", "01-auth", "t-1",
		"--files", "src/auth.ts", "--commit", "abc1234"); err != nil {
		t.Fatal(err)
	}

	// Mechanical run writes verdict=pending.
	if err := runCmd(t, Verify(), "01-auth", "--skip-mutation"); err != nil {
		t.Fatalf("verify: %v", err)
	}

	// Finalize without filling in the verdict must error.
	if err := runCmd(t, Verify(), "finalize", "01-auth"); err == nil {
		t.Error("finalize should reject a pending verdict")
	}

	// Patch verify.toml as if /dross-verify had filled it in.
	verifyPath := filepath.Join(dir, ".dross/phases/01-auth/verify.toml")
	body := mustRead(t, verifyPath)
	body = strings.Replace(body, `verdict = "pending"`, `verdict = "partial"`, 1)
	mustWrite(t, ".dross/phases/01-auth/verify.toml", body)

	// Finalize with a resolved verdict should succeed and record telemetry.
	out := captureStdout(t, func() {
		if err := runCmd(t, Verify(), "finalize", "01-auth"); err != nil {
			t.Fatalf("finalize: %v", err)
		}
	})
	if !strings.Contains(out, "verdict=partial") {
		t.Errorf("expected confirmation in output:\n%s", out)
	}

	// Telemetry must contain a resolved-verdict outcome event.
	telemBody := mustRead(t, filepath.Join(home, ".claude/dross", "telemetry.jsonl"))
	if !strings.Contains(telemBody, `"verdict":"partial"`) {
		t.Errorf("expected partial verdict in telemetry:\n%s", telemBody)
	}
}

// TestVerifyFinalizeRejectsBogusVerdict asserts the verdict whitelist —
// only pass | partial | fail can be finalized.
func TestVerifyFinalizeRejectsBogusVerdict(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	if err := runCmd(t, Init()); err != nil {
		t.Fatal(err)
	}
	mustRunSet(t, "project.name", "x")
	mustRunSet(t, "runtime.mode", "native")
	if err := runCmd(t, Phase(), "create", "x"); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, ".dross/phases/01-x/verify.toml", `[verify]
phase = "01-x"
verdict = "maybe"
[summary]
mutation_score = 0.0
mutants_killed = 0
mutants_survived = 0
criteria_total = 0
criteria_covered = 0
criteria_uncovered = 0
`)
	err := runCmd(t, Verify(), "finalize", "01-x")
	if err == nil {
		t.Fatal("expected error for bogus verdict")
	}
	if !strings.Contains(err.Error(), "not one of") {
		t.Errorf("error should explain whitelist: %v", err)
	}
}

// TestVerifyFinalizeMissingFile errors clearly when verify.toml has
// not been written yet.
func TestVerifyFinalizeMissingFile(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	if err := runCmd(t, Init()); err != nil {
		t.Fatal(err)
	}
	mustRunSet(t, "project.name", "x")
	mustRunSet(t, "runtime.mode", "native")
	if err := runCmd(t, Phase(), "create", "x"); err != nil {
		t.Fatal(err)
	}
	err := runCmd(t, Verify(), "finalize", "01-x")
	if err == nil {
		t.Fatal("expected error when verify.toml is missing")
	}
	if !strings.Contains(err.Error(), "dross verify 01-x") {
		t.Errorf("error should hint at running verify first: %v", err)
	}
}

// dockerPrefix derives the runtime prefix from project.toml. Pin its
// behaviour so a refactor of TestCommand parsing doesn't silently break
// docker-routed mutation runs.
func TestDockerPrefixDerivation(t *testing.T) {
	cases := []struct {
		mode, testCmd, want string
	}{
		{"native", "pnpm test", ""},
		{"docker", "docker compose exec app pnpm test", "docker compose exec app"},
		{"docker", "docker compose exec app npm test", "docker compose exec app"},
		{"docker", "docker compose exec api yarn test", "docker compose exec api"},
		{"docker", "docker compose exec app bun test", "docker compose exec app"},
		{"docker", "docker compose exec node node test.js", "docker compose exec node"},
		// docker mode but unrecognised runner — falls back to default
		{"docker", "weird invocation", "docker compose exec app"},
		// docker mode with no test_command at all — falls back to default
		{"docker", "", "docker compose exec app"},
	}
	for _, c := range cases {
		p := &project.Project{Runtime: project.Runtime{Mode: c.mode, TestCommand: c.testCmd}}
		if got := dockerPrefix(p); got != c.want {
			t.Errorf("dockerPrefix(mode=%q, test=%q) = %q want %q", c.mode, c.testCmd, got, c.want)
		}
	}
}
