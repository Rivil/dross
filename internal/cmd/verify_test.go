package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/mutation"
	"github.com/Rivil/dross/internal/project"
	"github.com/Rivil/dross/internal/verify"
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

// --- printVerifySummary coverage (verify.go:248-284) ---
//
// These call printVerifySummary directly with hand-built Tests/Verify so
// each branch of the efficacy-note math runs with an exactly-asserted
// value. Numbers are chosen so that every arithmetic mutant produces a
// different printed figure.

// verifyCovTests wraps a single language run + mutation report.
func verifyCovTests(m *mutation.Report) *verify.Tests {
	return &verify.Tests{
		Phase: "01-cov",
		Languages: []verify.LanguageRun{
			{Name: "go", Tool: "gremlins", Files: []string{"a.go", "b.go"}, Mutation: m},
		},
	}
}

func verifyCovVerify(status string) *verify.Verify {
	v := &verify.Verify{}
	v.Verify.Verdict = "pending"
	v.Summary.MutationStatus = status
	return v
}

// TestVerifyCover_SummaryEfficacyNote exercises the non-nil-mutation
// branch (253), the NotCovered>0 branch (260), efficacyDenom math (265),
// the denom>0 branch (266), and the efficacy + printed sum (267,269).
// Killed=6, Survived=4, NotCovered=2, Timeout=1:
//
//	efficacyDenom = 6 + (4 - 2) = 8
//	efficacy      = 6 / 8       = 0.75
//	printed total = 6 + 4 + 1   = 11
func TestVerifyCover_SummaryEfficacyNote(t *testing.T) {
	m := &mutation.Report{
		Tool: "gremlins", Killed: 6, Survived: 4, NotCovered: 2, Timeout: 1, Errors: 0, Score: 0.60,
	}
	out := captureStdout(t, func() {
		printVerifySummary(verifyCovTests(m), verifyCovVerify(verify.MutationMeasured))
	})
	for _, want := range []string{
		"killed=6 survived=4 (not_covered=2) timeout=1 errors=0 score=0.60",
		"note: 2/11 mutants NOT COVERED",
		"efficacy excluding them = 0.75",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("summary missing %q\n--- out ---\n%s", want, out)
		}
	}
}

// TestVerifyCover_SummaryNoNoteWhenCovered pins the NotCovered==0 side of
// branch 260: no efficacy note is printed. Kills the boundary (>=0 would
// print) and negation mutants on line 260.
func TestVerifyCover_SummaryNoNoteWhenCovered(t *testing.T) {
	m := &mutation.Report{
		Tool: "gremlins", Killed: 6, Survived: 4, NotCovered: 0, Timeout: 1, Score: 0.60,
	}
	out := captureStdout(t, func() {
		printVerifySummary(verifyCovTests(m), verifyCovVerify(verify.MutationMeasured))
	})
	if !strings.Contains(out, "killed=6 survived=4 (not_covered=0)") {
		t.Errorf("expected killed/survived line:\n%s", out)
	}
	if strings.Contains(out, "NOT COVERED") || strings.Contains(out, "efficacy excluding") {
		t.Errorf("no efficacy note expected when NotCovered==0:\n%s", out)
	}
}

// TestVerifyCover_SummaryDenomZeroSkipsEfficacy drives NotCovered>0 (enters
// block 260) but efficacyDenom==0 (branch 266 false), so the inner efficacy
// line is skipped. Killed=0, Survived=2, NotCovered=2 → denom = 0+(2-2)=0.
// Kills the boundary (>=0 would divide 0/0 and print) and negation on 266.
func TestVerifyCover_SummaryDenomZeroSkipsEfficacy(t *testing.T) {
	m := &mutation.Report{
		Tool: "gremlins", Killed: 0, Survived: 2, NotCovered: 2, Timeout: 0, Score: 0.0,
	}
	out := captureStdout(t, func() {
		printVerifySummary(verifyCovTests(m), verifyCovVerify(verify.MutationMeasured))
	})
	if !strings.Contains(out, "(not_covered=2)") {
		t.Errorf("expected not_covered=2 line:\n%s", out)
	}
	if strings.Contains(out, "efficacy excluding") {
		t.Errorf("efficacy line must be skipped when denom==0:\n%s", out)
	}
}

// TestVerifyCover_SummaryNilMutation pins the nil-mutation side of branch
// 253: prints "no mutation report" and skips the detailed line.
func TestVerifyCover_SummaryNilMutation(t *testing.T) {
	tests := &verify.Tests{
		Phase: "01-cov",
		Languages: []verify.LanguageRun{
			{Name: "html", Tool: "none", Files: []string{"x.html"}, Mutation: nil},
		},
	}
	out := captureStdout(t, func() {
		printVerifySummary(tests, verifyCovVerify(verify.MutationMeasured))
	})
	if !strings.Contains(out, "no mutation report") {
		t.Errorf("expected 'no mutation report':\n%s", out)
	}
	if strings.Contains(out, "killed=") {
		t.Errorf("detailed killed line must not print for nil mutation:\n%s", out)
	}
}

// TestVerifyCover_SummaryStatusMessages covers the mutation-status switch
// (lines 277-284) including the unmeasurable message whose text spans the
// string concatenations on 278-279.
func TestVerifyCover_SummaryStatusMessages(t *testing.T) {
	cases := []struct {
		status string
		want   string
	}{
		{verify.MutationUnmeasurable, "mutation status: unmeasurable — adapter ran but instrumented 0 mutants (likely the project's mutation scope excludes"},
		{verify.MutationSkipped, "mutation status: skipped — no adapter ran."},
	}
	for _, c := range cases {
		empty := &verify.Tests{Phase: "01-cov"}
		out := captureStdout(t, func() {
			printVerifySummary(empty, verifyCovVerify(c.status))
		})
		if !strings.Contains(out, c.want) {
			t.Errorf("status %q: missing %q\n--- out ---\n%s", c.status, c.want, out)
		}
	}
}

// --- recordVerifyOutcome coverage (verify.go:211-236) ---
//
// recordVerifyOutcome emits a telemetry "outcome" event. We enable
// telemetry into an isolated HOME and read telemetry.jsonl back to assert
// the exact computed nums/counts, so the arithmetic and conditional
// mutants produce an observable difference.

func verifyCovEnableTelemetry(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("DROSS_NO_TELEMETRY", "") // re-enable (chdir pins it to "1")
	return filepath.Join(home, ".claude", "dross", "telemetry.jsonl")
}

// TestVerifyCover_RecordOutcomeWithTests exercises the t!=nil branch,
// including the per-language accumulation guard (219) and the mutation_score
// division (228). Killed=6, Survived=4 → score = 6/10 = 0.6. If line 219 is
// negated, accumulation is skipped and mutants_killed drops to 0; if 228's
// division becomes multiplication, the score becomes 60 not 0.6.
func TestVerifyCover_RecordOutcomeWithTests(t *testing.T) {
	chdir(t, t.TempDir())
	telemPath := verifyCovEnableTelemetry(t)

	tests := verifyCovTests(&mutation.Report{
		Tool: "gremlins", Killed: 6, Survived: 4, Score: 0.6,
	})
	v := &verify.Verify{}
	v.Verify.Verdict = "pass"

	recordVerifyOutcome(tests, v)

	body := mustRead(t, telemPath)
	for _, want := range []string{
		`"mutants_killed":6`,
		`"mutants_survived":4`,
		`"mutation_score":0.6`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("telemetry missing %q\n--- body ---\n%s", want, body)
		}
	}
}

// TestVerifyCover_RecordOutcomeNilTestsScored drives the t==nil fallback
// with a positive summary score, pinning the true side of branch 233:
// mutation_score is recorded from the summary.
func TestVerifyCover_RecordOutcomeNilTestsScored(t *testing.T) {
	chdir(t, t.TempDir())
	telemPath := verifyCovEnableTelemetry(t)

	v := &verify.Verify{}
	v.Verify.Verdict = "partial"
	v.Summary.MutantsKilled = 3
	v.Summary.MutantsSurvived = 1
	v.Summary.MutationScore = 0.7

	recordVerifyOutcome(nil, v)

	body := mustRead(t, telemPath)
	for _, want := range []string{`"mutants_killed":3`, `"mutation_score":0.7`} {
		if !strings.Contains(body, want) {
			t.Errorf("telemetry missing %q\n--- body ---\n%s", want, body)
		}
	}
}

// TestVerifyCover_RecordOutcomeNilTestsZeroScore pins the false side of
// branch 233 (MutationScore>0): with score==0 no mutation_score number is
// emitted. Kills both the boundary (>=0 would emit 0) and negation mutants.
func TestVerifyCover_RecordOutcomeNilTestsZeroScore(t *testing.T) {
	chdir(t, t.TempDir())
	telemPath := verifyCovEnableTelemetry(t)

	v := &verify.Verify{}
	v.Verify.Verdict = "fail"
	v.Summary.MutationScore = 0.0

	recordVerifyOutcome(nil, v)

	body := mustRead(t, telemPath)
	if strings.Contains(body, "mutation_score") || strings.Contains(body, `"nums"`) {
		t.Errorf("no mutation_score expected when summary score is 0:\n%s", body)
	}
}
