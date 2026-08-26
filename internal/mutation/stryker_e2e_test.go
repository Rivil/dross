package mutation

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The end-to-end leg.
//
// Every other Stryker test in this repo parses a canned JSON report. Each of
// them asserts one link of the chain in isolation — the argv, the report path,
// the parser — and none of them would notice if two links stopped agreeing:
// a report written where the adapter does not look, a `--mutate` list the tool
// resolves differently, an output format that moved. Running the real tool
// against a real project is the only thing that fails when they drift.
//
// Per the locked toolchain_gate_not_ci_install decision this SKIPS when the
// Node toolchain is absent, naming the tool and the install line. It is
// deliberately not wired into CI: installing Stryker there pulls a large npm
// tree into a workflow hardened against exactly that, and the trade deserves
// its own decision rather than a step appended while doing something else.

// e2eSkipReason returns the reason the end-to-end run cannot happen, or "" when
// it can.
//
// It returns prose rather than a bool because a skip nobody can act on is
// indistinguishable from a run that happened: the message has to name the tool
// AND how to get it, or the check quietly stops existing on every machine that
// lacks it.
func e2eSkipReason() string {
	if os.Getenv("DROSS_SKIP_E2E") != "" {
		return "DROSS_SKIP_E2E is set — skipping the end-to-end mutation run by request"
	}
	if _, err := exec.LookPath("npx"); err != nil {
		return "npx is not on PATH — the end-to-end Stryker run needs Node. Install Node 20+ (https://nodejs.org), then re-run this test"
	}
	if _, err := os.Stat(filepath.Join(tsFixtureDir, "node_modules")); err != nil {
		return "the fixture's dependencies are not installed — run `npm install` in " + tsFixtureDir + " (its devDependencies are pinned exactly, and are deliberately not vendored into the repo)"
	}
	return ""
}

// TestStrykerRunsEndToEnd is c-1: a real TypeScript project, mutated by the
// real tool, through dross's own adapter.
func TestStrykerRunsEndToEnd(t *testing.T) {
	if reason := e2eSkipReason(); reason != "" {
		// DROSS_REQUIRE_E2E is what makes the CI leg mean something. Without
		// it, a runner missing Node skips — and a skipped leg reports the same
		// green as a leg that ran, so the mutation coverage CI is supposed to
		// guarantee would silently stop existing the first time the setup
		// drifted. In CI the absence of the toolchain is a broken workflow, not
		// a machine that happens to lack it.
		if os.Getenv("DROSS_REQUIRE_E2E") != "" {
			t.Fatalf("DROSS_REQUIRE_E2E is set but the end-to-end run cannot proceed: %s", reason)
		}
		t.Skip(reason)
	}

	root, err := filepath.Abs(tsFixtureDir)
	if err != nil {
		t.Fatal(err)
	}
	s := &Stryker{ProjectRoot: root}

	report, err := s.Run([]string{"src/tally.ts"})
	if err != nil {
		t.Fatalf("the adapter could not run stryker end to end: %v", err)
	}

	// Killed > 0 is what proves the run MEASURED something. A run that
	// produced a parseable report of nothing would satisfy err == nil.
	if report.Killed == 0 {
		t.Errorf("no mutants were killed — the fixture's covered functions should kill some, so either the tests did not run or the report is not the run's: %+v", report)
	}
	// And a survivor proves the report distinguishes the two outcomes rather
	// than reporting everything as killed. The fixture keeps one deliberately
	// uncovered function for exactly this assertion.
	if report.Survived == 0 {
		t.Errorf("no mutants survived — the fixture's uncovered function should leave some, so the report may not be distinguishing killed from survived: %+v", report)
	}

	// Attribution: a report whose per-file rows are empty would satisfy the
	// counts above while telling diff scoping nothing, which is the failure
	// that makes a phase score against files it never touched.
	if len(report.Files) == 0 {
		t.Fatalf("the report attributes no mutants to any file — scoping would have nothing to filter on: %+v", report)
	}
	found := false
	for path := range report.Files {
		if strings.Contains(filepath.ToSlash(path), "tally.ts") {
			found = true
		}
	}
	if !found {
		t.Errorf("no mutants are attributed to the file that was mutated; report files = %v", keysOf(report.Files))
	}
}

// TestE2ESkipNamesTheMissingTool: the gate is only honest if the skip is
// actionable. A bare `t.Skip("no toolchain")` on every machine without Node is
// a check that silently stopped existing.
func TestE2ESkipNamesTheMissingTool(t *testing.T) {
	t.Setenv("PATH", t.TempDir()) // nothing on PATH, so npx is definitely absent
	t.Setenv("DROSS_SKIP_E2E", "")

	reason := e2eSkipReason()
	if reason == "" {
		t.Fatal("with an empty PATH the end-to-end run must report a reason it cannot happen")
	}
	if !strings.Contains(reason, "npx") {
		t.Errorf("the skip reason does not name the missing tool: %q", reason)
	}
	if !strings.Contains(strings.ToLower(reason), "install") {
		t.Errorf("the skip reason does not say how to get it: %q", reason)
	}
}

// TestE2ERunsWhenTheToolchainIsPresent guards the other direction: a gate that
// always skipped would pass every assertion above while never running the tool.
func TestE2ERunsWhenTheToolchainIsPresent(t *testing.T) {
	if _, err := exec.LookPath("npx"); err != nil {
		t.Skip("npx absent — nothing to assert about the not-skipped path here")
	}
	if _, err := os.Stat(filepath.Join(tsFixtureDir, "node_modules")); err != nil {
		t.Skip("fixture dependencies not installed — see e2eSkipReason")
	}
	t.Setenv("DROSS_SKIP_E2E", "")
	if reason := e2eSkipReason(); reason != "" {
		t.Errorf("the toolchain is present but the gate still skipped: %q", reason)
	}
}

func keysOf[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// bracketFixturePath is the fixture whose DIRECTORY NAME is the whole point:
// square brackets are glob metacharacters, so an unescaped --mutate entry
// naming it matches nothing and the file is silently dropped from the run.
//
// Written workdir-relative and with forward slashes because that is the form
// that lands in the argv and in Stryker's report keys.
const bracketFixturePath = "src/[id]/handler.ts"

// strykerDropWarning is @stryker-mutator/core's own text when a --mutate glob
// resolves to no file at all (src/fs/project-reader.ts). It is the ONLY signal
// the tool emits for a silently-dropped path — the run then reports a score
// over whatever survived the drop and looks entirely healthy.
const strykerDropWarning = "did not result in any files"

// TestStrykerInstrumentsBracketPaths is c-3, and it is the acceptance signal
// for the escaping fix — not the unit rows next door.
//
// The unit tests assert the argv dross BUILDS. Only this one asserts the argv
// Stryker ACCEPTS: it drives the real adapter against a real bracket-shaped
// path and requires BOTH halves of the criterion — the file appears in the
// report with mutants, AND the run logs no drop warning for it. Either half
// alone is satisfiable by a broken fix (a report can carry the file with zero
// mutants; a warning can be absent because the pattern was never passed).
func TestStrykerInstrumentsBracketPaths(t *testing.T) {
	if reason := e2eSkipReason(); reason != "" {
		// The same fatal branch TestStrykerRunsEndToEnd carries, and for the
		// same reason: this is c-3's only acceptance signal, so a machine that
		// silently skips it reports the same green as one that ran it. The
		// check has to be inline — e2eSkipReason() returns prose and does not
		// itself know whether skipping is allowed.
		if os.Getenv("DROSS_REQUIRE_E2E") != "" {
			t.Fatalf("DROSS_REQUIRE_E2E is set but the end-to-end run cannot proceed: %s", reason)
		}
		t.Skip(reason)
	}

	root, err := filepath.Abs(tsFixtureDir)
	if err != nil {
		t.Fatal(err)
	}
	s := &Stryker{ProjectRoot: root}

	out, report, runErr := captureStderr(t, func() (*Report, error) {
		return s.Run([]string{bracketFixturePath})
	})
	if runErr != nil {
		t.Fatalf("the adapter could not run stryker against a bracket path: %v\n--- tool output ---\n%s", runErr, out)
	}

	// Half one: the file was actually instrumented. A report that parses but
	// attributes nothing to this path is exactly what the unescaped form
	// produces, and it is indistinguishable from success at the counts level.
	var mutants int
	var seen []string
	for path, f := range report.Files {
		seen = append(seen, path)
		if filepath.ToSlash(path) == bracketFixturePath {
			// NotCovered is a SUBSET of Survived (adapter.go), so it is
			// deliberately not added in — the sum would double-count it.
			mutants = f.Killed + f.Survived + f.Timeout + f.Errors
		}
	}
	if mutants == 0 {
		t.Errorf("no mutants are attributed to %q — the bracket path was dropped from the run; report files = %v", bracketFixturePath, seen)
	}

	// Half two: and Stryker did not tell us it was dropping it. c-3 requires
	// both, because a fix that widened the glob (say, to src/**) would satisfy
	// half one while the specific path still matched nothing.
	if strings.Contains(out, strykerDropWarning) {
		t.Errorf("stryker reported dropping a --mutate glob; the escaped form should resolve.\n--- tool output ---\n%s", out)
	}
}

// captureStderr runs fn with os.Stderr redirected to a temp file and returns
// what the tool wrote.
//
// The adapter streams the tool's stdout and stderr straight to os.Stderr and
// captures neither (stryker.go), so reading the process global is the only way
// to see the tool's own output from a test. Restored on the way out.
func captureStderr(t *testing.T, fn func() (*Report, error)) (string, *Report, error) {
	t.Helper()

	path := filepath.Join(t.TempDir(), "stderr.log")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	saved := os.Stderr
	os.Stderr = f
	report, runErr := fn()
	os.Stderr = saved
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b), report, runErr
}
