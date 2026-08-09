package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/changes"
	"github.com/Rivil/dross/internal/mutation"
	"github.com/Rivil/dross/internal/project"
	"github.com/Rivil/dross/internal/telemetry"
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
	trustFixture(t)
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
	trustFixture(t)
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
	trustFixture(t)
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
	trustFixture(t)
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
		// self-audit inj-4a: a binary that merely STARTS WITH "docker"
		// (dockerevil) must NOT be promoted into the exec prefix — the
		// leading field has to be exactly "docker". Falls back to default.
		{"docker", "dockerevil compose exec app pnpm test", "docker compose exec app"},
		{"docker", "docker-malicious run --privileged x", "docker compose exec app"},
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

// TestConfiguredAdaptersAllowlist pins the [mutation] adapters escape hatch:
// empty means all adapters; non-empty filters by Name(), so a polyglot repo
// can run only the adapter that's actually set up.
func TestConfiguredAdaptersAllowlist(t *testing.T) {
	names := func(as []mutation.Adapter) []string {
		var out []string
		for _, a := range as {
			out = append(out, a.Name())
		}
		return out
	}

	p := &project.Project{}
	if got := names(configuredAdapters(p, "", false)); len(got) != 3 {
		t.Fatalf("empty allowlist must return all adapters, got %v", got)
	}

	p.Mutation.Adapters = []string{"gremlins"}
	got := names(configuredAdapters(p, "", false))
	if len(got) != 1 || got[0] != "gremlins" {
		t.Errorf("allowlist [gremlins] must filter to gremlins only, got %v", got)
	}

	p.Mutation.Adapters = []string{"stryker", "gremlins"}
	got = names(configuredAdapters(p, "", false))
	if len(got) != 2 {
		t.Errorf("allowlist [stryker gremlins] must keep both, got %v", got)
	}

	if got := configuredAdapters(p, "", true); got != nil {
		t.Errorf("--skip-mutation must still return nil regardless of allowlist, got %v", names(got))
	}
}

// --- finalize idempotency + phase stamping (verify-auto-finalize c-2) ---

// TestVerifyFinalizeIdempotentAndStampsPhase drives the full finalize
// lifecycle: the mechanical run's pending event and the finalize
// outcome event both carry the phase id; the first finalize writes the
// finalized=true marker back into verify.toml; a second finalize emits
// no duplicate outcome event, exits 0, and says "already recorded".
func TestVerifyFinalizeIdempotentAndStampsPhase(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("DROSS_NO_TELEMETRY", "") // re-enable telemetry recording

	if err := runCmd(t, Init()); err != nil {
		t.Fatal(err)
	}
	trustFixture(t)
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
	if err := runCmd(t, Verify(), "01-auth", "--skip-mutation"); err != nil {
		t.Fatalf("verify: %v", err)
	}

	telemPath := filepath.Join(home, ".claude/dross", "telemetry.jsonl")

	// The mechanical pending event must carry the phase id.
	if body := mustRead(t, telemPath); !strings.Contains(body, `"phase":"01-auth"`) {
		t.Errorf("pending verify event should be phase-stamped:\n%s", body)
	}

	// Resolve the verdict as /dross-verify would.
	verifyPath := filepath.Join(dir, ".dross/phases/01-auth/verify.toml")
	body := mustRead(t, verifyPath)
	mustWrite(t, ".dross/phases/01-auth/verify.toml",
		strings.Replace(body, `verdict = "pending"`, `verdict = "pass"`, 1))

	// First finalize: records, and writes the marker back.
	out := captureStdout(t, func() {
		if err := runCmd(t, Verify(), "finalize", "01-auth"); err != nil {
			t.Fatalf("finalize: %v", err)
		}
	})
	if !strings.Contains(out, "verdict=pass recorded") {
		t.Errorf("first finalize should say recorded:\n%s", out)
	}
	if vbody := mustRead(t, verifyPath); !strings.Contains(vbody, "finalized = true") {
		t.Errorf("finalize must write finalized=true into verify.toml:\n%s", vbody)
	}
	telemBody := mustRead(t, telemPath)
	if got := strings.Count(telemBody, `"verdict":"pass"`); got != 1 {
		t.Fatalf("want exactly 1 pass outcome event after first finalize, got %d:\n%s", got, telemBody)
	}
	if !strings.Contains(telemBody, `"phase":"01-auth"`) {
		t.Errorf("finalize outcome event should be phase-stamped:\n%s", telemBody)
	}

	// Second finalize: no duplicate event, exit 0, "already recorded".
	out = captureStdout(t, func() {
		if err := runCmd(t, Verify(), "finalize", "01-auth"); err != nil {
			t.Fatalf("second finalize must exit 0, got: %v", err)
		}
	})
	if !strings.Contains(out, "already recorded") {
		t.Errorf("second finalize should say already recorded:\n%s", out)
	}
	telemBody = mustRead(t, telemPath)
	if got := strings.Count(telemBody, `"verdict":"pass"`); got != 1 {
		t.Errorf("second finalize emitted a duplicate outcome event (%d):\n%s", got, telemBody)
	}
}

// --- diff scoping wired into `dross verify` ---------------------------------

// stubMutationAdapter returns a fixed report for a set of extensions and
// remembers what it was handed.
type stubMutationAdapter struct {
	name   string
	exts   []string
	report *mutation.Report
	got    []string
}

func (s *stubMutationAdapter) Name() string { return s.name }
func (s *stubMutationAdapter) Supports(file string) bool {
	for _, e := range s.exts {
		if strings.EqualFold(filepath.Ext(file), e) {
			return true
		}
	}
	return false
}
func (s *stubMutationAdapter) Run(files []string) (*mutation.Report, error) {
	s.got = append([]string(nil), files...)
	return s.report, nil
}

// useStubAdapter installs a stub in place of the configured adapters.
func useStubAdapter(t *testing.T, a mutation.Adapter) {
	t.Helper()
	prev := configuredAdaptersFn
	configuredAdaptersFn = func(_ *project.Project, _ string, _ bool) []mutation.Adapter {
		return []mutation.Adapter{a}
	}
	t.Cleanup(func() { configuredAdaptersFn = prev })
}

// goReport builds a gremlins-shaped report whose aggregates and per-file rows
// agree, as every adapter is required to produce.
func goReport(rows map[string]mutation.FileStat, surviving ...mutation.Mutant) *mutation.Report {
	r := &mutation.Report{Tool: "gremlins", Files: rows, Surviving: surviving}
	for _, st := range rows {
		r.Killed += st.Killed
		r.Survived += st.Survived
		r.Timeout += st.Timeout
	}
	if d := r.Killed + r.Survived + r.Timeout; d > 0 {
		r.Score = float64(r.Killed) / float64(d)
	}
	return r
}

// scopedVerifyRepo builds a real git repo with a dross project, a `base`
// branch, and a phase forked from it. Returns the repo dir.
func scopedVerifyRepo(t *testing.T, phaseSlug string) string {
	t.Helper()
	dir := t.TempDir()
	chdir(t, dir)
	gitInit(t, dir, "git@github.com:example/x.git")
	if err := runCmd(t, Init()); err != nil {
		t.Fatal(err)
	}
	trustFixture(t)
	mustRunSet(t, "project.name", "x")
	mustRunSet(t, "runtime.mode", "native")

	writeScopeFile(t, dir, "a.go", "package x\n\nfunc A() bool { return true }\n")
	writeScopeFile(t, dir, "b.go", "package x\n\nfunc B() bool { return false }\n")
	// The dross scaffold goes into the base commit too: `phase create` refuses
	// a dirty tree, and the phase's diff should start from a clean baseline.
	mustGit(t, dir, "add", "-A")
	mustGit(t, dir, "commit", "-qm", "base")
	mustGit(t, dir, "branch", "base")

	if err := runCmd(t, Phase(), "create", phaseSlug); err != nil {
		t.Fatal(err)
	}
	return dir
}

// phaseSpec writes a one-criterion spec for the phase.
func phaseSpec(t *testing.T, phaseID string) {
	t.Helper()
	mustWrite(t, ".dross/phases/"+phaseID+"/spec.toml", `[phase]
id = "`+phaseID+`"
title = "x"
[[criteria]]
id = "c-1"
text = "x"
`)
}

// TestVerifyScopesToPhaseChangesEndToEnd is the whole feature through the CLI
// over a real repo: a.go is edited by the phase, b.go is its untouched sibling
// in the same package, and each has a survivor.
//
// Before scoping, b.go's survivor was this phase's problem — same package, so
// same gremlins report. It must now appear only as a filtered count, and a.go
// must be named as what the run actually scoped to.
func TestVerifyScopesToPhaseChangesEndToEnd(t *testing.T) {
	dir := scopedVerifyRepo(t, "scoped")
	phaseSpec(t, "01-scoped")
	writeScopeFile(t, dir, "a.go", "package x\n\nfunc A() bool { return 1 > 0 }\n")
	mustGit(t, dir, "commit", "-qam", "phase edits a.go")
	if err := runCmd(t, Changes(), "record", "01-scoped", "t-1", "--files", "a.go"); err != nil {
		t.Fatal(err)
	}
	mustSetBase(t, "01-scoped", "base")

	stub := &stubMutationAdapter{name: "gremlins", exts: []string{".go"},
		report: goReport(
			map[string]mutation.FileStat{"a.go": {Survived: 1}, "b.go": {Survived: 1}},
			mutation.Mutant{File: "a.go", Line: 3, Op: "CONDITIONALS_BOUNDARY"},
			mutation.Mutant{File: "b.go", Line: 3, Op: "CONDITIONALS_NEGATION"},
		)}
	useStubAdapter(t, stub)

	out := captureStdout(t, func() {
		if err := runCmd(t, Verify(), "01-scoped"); err != nil {
			t.Fatalf("verify: %v", err)
		}
	})

	if !strings.Contains(out, "a.go") {
		t.Errorf("scope line must name the phase's own file:\n%s", out)
	}
	if !strings.Contains(out, "filtered 1 out-of-scope survivor") {
		t.Errorf("filtered count missing:\n%s", out)
	}

	body := mustRead(t, filepath.Join(dir, ".dross/phases/01-scoped/tests.json"))
	if !strings.Contains(body, `"out_of_scope"`) || !strings.Contains(body, `"b.go"`) {
		t.Errorf("b.go's survivor should be recorded under out_of_scope:\n%s", body)
	}
	vbody := mustRead(t, filepath.Join(dir, ".dross/phases/01-scoped/verify.toml"))
	if strings.Contains(vbody, "b.go") {
		t.Errorf("an untouched sibling's survivor must not be a phase finding:\n%s", vbody)
	}
	if !strings.Contains(vbody, "a.go") {
		t.Errorf("the phase's own survivor must still FLAG:\n%s", vbody)
	}
}

// TestVerifyUnionFeedsTheAdapter: a file git saw change but no task recorded
// is still handed to the adapter. Otherwise it is never mutated, so its
// survivors cannot gate anything — the same escape hatch on the dispatch side
// that filtering closes on the attribution side.
func TestVerifyUnionFeedsTheAdapter(t *testing.T) {
	dir := scopedVerifyRepo(t, "union")
	phaseSpec(t, "01-union")
	writeScopeFile(t, dir, "a.go", "package x\n\nfunc A() bool { return 1 > 0 }\n")
	writeScopeFile(t, dir, "unrecorded.go", "package x\n\nfunc U() bool { return 2 > 1 }\n")
	mustGit(t, dir, "add", ".")
	mustGit(t, dir, "commit", "-qm", "phase edits two files, records one")
	if err := runCmd(t, Changes(), "record", "01-union", "t-1", "--files", "a.go"); err != nil {
		t.Fatal(err)
	}
	mustSetBase(t, "01-union", "base")

	stub := &stubMutationAdapter{name: "gremlins", exts: []string{".go"},
		report: goReport(map[string]mutation.FileStat{"unrecorded.go": {Survived: 1}},
			mutation.Mutant{File: "unrecorded.go", Line: 3, Op: "X"})}
	useStubAdapter(t, stub)

	if err := runCmd(t, Verify(), "01-union"); err != nil {
		t.Fatalf("verify: %v", err)
	}

	var sawUnrecorded bool
	for _, f := range stub.got {
		if f == "unrecorded.go" {
			sawUnrecorded = true
		}
	}
	if !sawUnrecorded {
		t.Errorf("git-changed but unrecorded file never reached the adapter: %v", stub.got)
	}
	vbody := mustRead(t, filepath.Join(dir, ".dross/phases/01-union/verify.toml"))
	if !strings.Contains(vbody, "unrecorded.go") {
		t.Errorf("its survivor must gate — expected a FLAG:\n%s", vbody)
	}
}

// TestVerifyEmptyChangesStillRunsWithGitDiff: an empty changes.json used to
// short-circuit the whole command. Under the scope_source lock the git side is
// sufficient on its own, so the run proceeds.
func TestVerifyEmptyChangesStillRunsWithGitDiff(t *testing.T) {
	dir := scopedVerifyRepo(t, "nochanges")
	phaseSpec(t, "01-nochanges")
	writeScopeFile(t, dir, "a.go", "package x\n\nfunc A() bool { return 1 > 0 }\n")
	mustGit(t, dir, "commit", "-qam", "phase work, nothing recorded")
	mustSetBase(t, "01-nochanges", "base")

	stub := &stubMutationAdapter{name: "gremlins", exts: []string{".go"},
		report: goReport(map[string]mutation.FileStat{"a.go": {Killed: 1}})}
	useStubAdapter(t, stub)

	out := captureStdout(t, func() {
		if err := runCmd(t, Verify(), "01-nochanges"); err != nil {
			t.Fatalf("verify: %v", err)
		}
	})
	if strings.Contains(out, "no changes recorded") {
		t.Errorf("git diff was non-empty; the run must not short-circuit:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(dir, ".dross/phases/01-nochanges/tests.json")); err != nil {
		t.Errorf("tests.json should have been written: %v", err)
	}
	if len(stub.got) == 0 {
		t.Error("the adapter was never invoked")
	}
}

// TestVerifyPrintsOutOfScopeStatusAndSampleSize covers the summary surface: an
// all-filtered run prints its own status line, and the in-scope mutant count
// sits beside the score so a low score on a tiny sample reads as one.
func TestVerifyPrintsOutOfScopeStatusAndSampleSize(t *testing.T) {
	dir := scopedVerifyRepo(t, "allfiltered")
	phaseSpec(t, "01-allfiltered")
	writeScopeFile(t, dir, "a.go", "package x\n\nfunc A() bool { return 1 > 0 }\n")
	mustGit(t, dir, "commit", "-qam", "phase edits a.go")
	mustSetBase(t, "01-allfiltered", "base")

	stub := &stubMutationAdapter{name: "gremlins", exts: []string{".go"},
		report: goReport(map[string]mutation.FileStat{"b.go": {Survived: 2}},
			mutation.Mutant{File: "b.go", Line: 1, Op: "X"},
			mutation.Mutant{File: "b.go", Line: 2, Op: "Y"},
		)}
	useStubAdapter(t, stub)

	out := captureStdout(t, func() {
		if err := runCmd(t, Verify(), "01-allfiltered"); err != nil {
			t.Fatalf("verify: %v", err)
		}
	})
	if !strings.Contains(out, "mutation status: out-of-scope") {
		t.Errorf("out-of-scope needs its own line in the status switch:\n%s", out)
	}
	if !strings.Contains(out, "in-scope mutants: 0") {
		t.Errorf("summary must carry the in-scope count:\n%s", out)
	}
	vbody := mustRead(t, filepath.Join(dir, ".dross/phases/01-allfiltered/verify.toml"))
	if !strings.Contains(vbody, `mutation_status = "out-of-scope"`) {
		t.Errorf("verify.toml status:\n%s", vbody)
	}
	if !strings.Contains(vbody, "mutants_in_scope = 0") {
		t.Errorf("verify.toml sample size:\n%s", vbody)
	}
}

// TestVerifyWithoutBaseStillRuns: an unresolvable base degrades the scope and
// says so, but must not abort the run. A bookkeeping gap is not a reason to
// refuse to verify.
func TestVerifyWithoutBaseStillRuns(t *testing.T) {
	dir := scopedVerifyRepo(t, "nobase")
	phaseSpec(t, "01-nobase")
	if err := runCmd(t, Changes(), "record", "01-nobase", "t-1", "--files", "a.go"); err != nil {
		t.Fatal(err)
	}
	mustSetBase(t, "01-nobase", "no-such-branch")

	stub := &stubMutationAdapter{name: "gremlins", exts: []string{".go"},
		report: goReport(map[string]mutation.FileStat{"a.go": {Killed: 1}})}
	useStubAdapter(t, stub)

	out := captureStdout(t, func() {
		if err := runCmd(t, Verify(), "01-nobase"); err != nil {
			t.Fatalf("an unresolvable base must not abort verify: %v", err)
		}
	})
	if !strings.Contains(out, "scope degraded") {
		t.Errorf("a degraded scope must be visible in the summary:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(dir, ".dross/phases/01-nobase/tests.json")); err != nil {
		t.Errorf("the run must still complete: %v", err)
	}
}

// TestVerifyDeletedPathNeverReachesAdapter: a deleted file stays in the scope
// so its mutants can still be recognised and filtered, but it must not be
// handed to a tool — `stryker --mutate <gone>` fails the whole leg.
func TestVerifyDeletedPathNeverReachesAdapter(t *testing.T) {
	dir := scopedVerifyRepo(t, "deleted")
	phaseSpec(t, "01-deleted")
	mustGit(t, dir, "rm", "-q", "b.go")
	writeScopeFile(t, dir, "a.go", "package x\n\nfunc A() bool { return 1 > 0 }\n")
	mustGit(t, dir, "add", ".")
	mustGit(t, dir, "commit", "-qm", "delete b.go, edit a.go")
	mustSetBase(t, "01-deleted", "base")

	stub := &stubMutationAdapter{name: "gremlins", exts: []string{".go"},
		report: goReport(map[string]mutation.FileStat{"a.go": {Killed: 1}})}
	useStubAdapter(t, stub)

	if err := runCmd(t, Verify(), "01-deleted"); err != nil {
		t.Fatalf("verify: %v", err)
	}
	for _, f := range stub.got {
		if f == "b.go" {
			t.Errorf("deleted path was handed to the adapter: %v", stub.got)
		}
	}
	body := mustRead(t, filepath.Join(dir, ".dross/phases/01-deleted/tests.json"))
	if !strings.Contains(body, `"b.go"`) {
		t.Errorf("the deleted path must still be recorded in the scope:\n%s", body)
	}
}

// TestVerifyBookkeepingPathsRaiseNoNoteFlood: every phase's git diff contains
// its own .dross/ artefacts. Dispatching them would add a "skipped, no
// adapter" NOTE each, putting a standing, undrainable backlog in every
// verify.toml — the thing rule r-02 exists to prevent. The NOTE COUNT is
// asserted, not their absence by substring, so a rename of the reason text
// cannot make this pass hollow.
func TestVerifyBookkeepingPathsRaiseNoNoteFlood(t *testing.T) {
	dir := scopedVerifyRepo(t, "bookkeeping")
	phaseSpec(t, "01-bookkeeping")
	writeScopeFile(t, dir, "a.go", "package x\n\nfunc A() bool { return 1 > 0 }\n")
	mustWrite(t, ".dross/phases/01-bookkeeping/plan.toml", `[phase]
id = "01-bookkeeping"
[[task]]
id = "t-1"
wave = 1
title = "x"
files = ["a.go"]
covers = ["c-1"]
`)
	mustGit(t, dir, "add", "a.go", ".dross")
	mustGit(t, dir, "commit", "-qm", "phase work plus its own bookkeeping")
	mustSetBase(t, "01-bookkeeping", "base")

	stub := &stubMutationAdapter{name: "gremlins", exts: []string{".go"},
		report: goReport(map[string]mutation.FileStat{"a.go": {Killed: 1}})}
	useStubAdapter(t, stub)

	if err := runCmd(t, Verify(), "01-bookkeeping"); err != nil {
		t.Fatalf("verify: %v", err)
	}

	// Premise guard: the .dross artefacts really are in the phase's diff.
	body := mustRead(t, filepath.Join(dir, ".dross/phases/01-bookkeeping/tests.json"))
	if !strings.Contains(body, ".dross/phases/01-bookkeeping/spec.toml") {
		t.Fatalf("expected the phase's own spec.toml in the scope:\n%s", body)
	}

	v, err := verify.LoadVerify(filepath.Join(dir, ".dross/phases/01-bookkeeping/verify.toml"))
	if err != nil {
		t.Fatal(err)
	}
	notes := 0
	for _, f := range v.Findings {
		if f.Severity == "NOTE" {
			notes++
		}
	}
	if notes != 0 {
		t.Errorf("bookkeeping paths produced %d NOTE(s): %+v", notes, v.Findings)
	}
}

// TestVerifyTelemetryAgreesWithVerifyToml pins telemetry against verify.toml
// on a SINGLE-leg, ZERO-timeout run — the range where the two live score
// formulas agree, and enough to catch duplicated arithmetic drifting in the
// new scoping code.
//
// Do NOT widen this fixture to multiple legs or a timeout, and do not "fix"
// combineScore or recordVerifyOutcome to make a wider assertion pass:
// verify.toml means across legs, telemetry pools, and Report.Score puts
// timeouts in the denominator. Picking a normative formula changes the number
// every phase's thresholds are applied to and is deferred to survivor-lifecycle.
func TestVerifyTelemetryAgreesWithVerifyToml(t *testing.T) {
	dir := scopedVerifyRepo(t, "telemetry")
	enableTelemetry(t)
	phaseSpec(t, "01-telemetry")
	writeScopeFile(t, dir, "a.go", "package x\n\nfunc A() bool { return 1 > 0 }\n")
	mustGit(t, dir, "commit", "-qam", "phase edits a.go")
	mustSetBase(t, "01-telemetry", "base")

	stub := &stubMutationAdapter{name: "gremlins", exts: []string{".go"},
		report: goReport(
			map[string]mutation.FileStat{"a.go": {Killed: 1, Survived: 1}, "b.go": {Killed: 8}},
			mutation.Mutant{File: "a.go", Line: 3, Op: "X"},
		)}
	useStubAdapter(t, stub)

	if err := runCmd(t, Verify(), "01-telemetry"); err != nil {
		t.Fatalf("verify: %v", err)
	}

	v, err := verify.LoadVerify(filepath.Join(dir, ".dross/phases/01-telemetry/verify.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if v.Summary.MutationScore != 0.5 || v.Summary.MutantsInScope != 2 {
		t.Fatalf("verify.toml should score the in-scope slice 1/2: score=%v in_scope=%d",
			v.Summary.MutationScore, v.Summary.MutantsInScope)
	}

	ev := lastVerifyOutcome(t)
	if got := ev.Numbers["mutation_score"]; got != v.Summary.MutationScore {
		t.Errorf("telemetry mutation_score=%v disagrees with verify.toml %v",
			got, v.Summary.MutationScore)
	}
	if got := ev.Counts["mutants_in_scope"]; got != v.Summary.MutantsInScope {
		t.Errorf("telemetry mutants_in_scope=%d want %d", got, v.Summary.MutantsInScope)
	}
	if got := ev.Counts["out_of_scope"]; got != 0 {
		t.Errorf("no survivor was filtered here; out_of_scope=%d", got)
	}
}

// TestVerifyTelemetryTagsOutOfScopeStatus: an all-filtered run must be visible
// in aggregate, not only in that phase's verify.toml.
func TestVerifyTelemetryTagsOutOfScopeStatus(t *testing.T) {
	dir := scopedVerifyRepo(t, "telemstatus")
	enableTelemetry(t)
	phaseSpec(t, "01-telemstatus")
	writeScopeFile(t, dir, "a.go", "package x\n\nfunc A() bool { return 1 > 0 }\n")
	mustGit(t, dir, "commit", "-qam", "phase edits a.go")
	mustSetBase(t, "01-telemstatus", "base")

	stub := &stubMutationAdapter{name: "gremlins", exts: []string{".go"},
		report: goReport(map[string]mutation.FileStat{"b.go": {Survived: 1}},
			mutation.Mutant{File: "b.go", Line: 1, Op: "X"})}
	useStubAdapter(t, stub)

	if err := runCmd(t, Verify(), "01-telemstatus"); err != nil {
		t.Fatalf("verify: %v", err)
	}
	ev := lastVerifyOutcome(t)
	if ev.Tags["mutation_status"] != verify.MutationOutOfScope {
		t.Errorf("mutation_status tag = %q want %q",
			ev.Tags["mutation_status"], verify.MutationOutOfScope)
	}
	if ev.Counts["out_of_scope"] != 1 {
		t.Errorf("out_of_scope count = %d want 1", ev.Counts["out_of_scope"])
	}
}

// TestVerifyHasNoScopingOptOut pins the scoping_always_on lock at the CLI
// surface: no flag on `dross verify` turns attribution back off.
func TestVerifyHasNoScopingOptOut(t *testing.T) {
	flags := Verify().Flags()
	for _, name := range []string{"scope", "no-scope", "whole-package", "no-diff-scope", "unscoped"} {
		if flags.Lookup(name) != nil {
			t.Errorf("--%s exists; diff scoping must have no opt-out", name)
		}
	}
}

// enableTelemetry re-enables recording into a throwaway HOME. It must run
// AFTER the repo helper: chdir() sets DROSS_NO_TELEMETRY=1, so anything set
// before it is overwritten.
func enableTelemetry(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("DROSS_NO_TELEMETRY", "")
}

// mustSetBase records the phase's fork point in changes.json.
func mustSetBase(t *testing.T, phaseID, base string) {
	t.Helper()
	root, err := FindRoot()
	if err != nil {
		t.Fatal(err)
	}
	if err := changes.SetBase(root, phaseID, base); err != nil {
		t.Fatal(err)
	}
}

// lastVerifyOutcome returns the most recent verify outcome event.
func lastVerifyOutcome(t *testing.T) telemetry.Event {
	t.Helper()
	evs, err := telemetry.Load(telemetryPath())
	if err != nil {
		t.Fatal(err)
	}
	for i := len(evs) - 1; i >= 0; i-- {
		if evs[i].Kind == "outcome" && evs[i].Command == "verify" {
			return evs[i]
		}
	}
	t.Fatal("no verify outcome event recorded")
	return telemetry.Event{}
}
