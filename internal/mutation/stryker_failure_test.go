package mutation

import (
	"strings"
	"testing"
)

// The stryker half of c-2: what a failed leg puts in an error, and what it puts
// on the terminal, are now two different things.
//
// The canary is the whole method. It is fed through the tool's stdout, so any
// appearance of it in the error means the tool's own output travelled into a
// string that tests.json and verify.toml will hold.
const canaryA = "CANARY-A-4d71"

// TestStrykerReportlessSplitsTerminalFromError is the cut, asserted from both
// sides at once: the cause is on the terminal TWICE (teed live, then re-printed
// at the failure point) and in the error NOT AT ALL.
func TestStrykerReportlessSplitsTerminalFromError(t *testing.T) {
	s := &Stryker{ProjectRoot: t.TempDir()}
	defer noisyStryker(t, s, "INFO Stryker Starting\n"+canaryA, 3, nil)()

	out, rep, err := captureStderr(t, func() (*Report, error) { return s.Run([]string{"src/a.ts"}) })
	if err == nil {
		t.Fatal("a reportless failure returned nil error")
	}
	if rep != nil {
		t.Errorf("a failed run produced a Report: %+v", rep)
	}

	if strings.Contains(err.Error(), canaryA) {
		t.Errorf("tool output reached the error, which is what gets persisted:\n%v", err)
	}
	if n := strings.Count(out, canaryA); n != 2 {
		t.Errorf("the cause appears %d time(s) on stderr, want 2 — once teed live and once re-printed under the head banner:\n%s", n, out)
	}
	if !strings.Contains(out, "the head of stryker's output") {
		t.Errorf("the head was not re-printed at the failure point:\n%s", out)
	}

	// The facts ABOUT the output, which are what the record carries.
	if !strings.Contains(err.Error(), "exit status 3") {
		t.Errorf("the error does not report the exit status:\n%v", err)
	}
	if !strings.Contains(err.Error(), "observed") {
		t.Errorf("the error does not report how much output went past:\n%v", err)
	}
	if !strings.Contains(err.Error(), "stderr") {
		t.Errorf("the error does not point at where the full output went:\n%v", err)
	}
}

// TestStrykerReportlessErrorKeepsItsDrossAuthoredContext. The record is WRAPPED,
// not returned bare: the expected report path is dross's own knowledge, it is
// the first thing a reader checks, and it is not something the record can carry.
func TestStrykerReportlessErrorKeepsItsDrossAuthoredContext(t *testing.T) {
	s := &Stryker{ProjectRoot: t.TempDir()}
	defer noisyStryker(t, s, canaryA, 1, nil)()

	_, _, err := captureStderr(t, func() (*Report, error) { return s.Run([]string{"src/a.ts"}) })
	if err == nil {
		t.Fatal("a reportless failure returned nil error")
	}
	if !strings.Contains(err.Error(), "did not write a report") {
		t.Errorf("a bare record replaced the dross-authored context:\n%v", err)
	}
	if !strings.Contains(err.Error(), s.reportPath()) {
		t.Errorf("the expected report path is gone from the error:\n%v", err)
	}
}

// TestStrykerTruncationNoteStillGatesOnTheHead. The note is dross-authored fixed
// prose, so it stays in the error — but only when stryker actually printed the
// truncated list, or it claims a truncation that never happened (locked
// decision note_trigger).
func TestStrykerTruncationNoteStillGatesOnTheHead(t *testing.T) {
	// A phrase distinctive to the note itself. Keying on its first word would
	// key on "stryker", which every error says.
	const note = "that list is truncated by design"
	if !strings.Contains(strykerInitialTestTruncationNote, note) {
		t.Fatalf("the note was reworded; this test keys on %q:\n%s", note, strykerInitialTestTruncationNote)
	}

	t.Run("attaches when the head matched", func(t *testing.T) {
		s := &Stryker{ProjectRoot: t.TempDir()}
		defer noisyStryker(t, s, strykerInitialTestFailureText+"\n  ✗ src/a.spec.ts > it works", 1, nil)()

		_, _, err := captureStderr(t, func() (*Report, error) { return s.Run([]string{"src/a.ts"}) })
		if err == nil {
			t.Fatal("a reportless failure returned nil error")
		}
		if !strings.Contains(err.Error(), note) {
			t.Errorf("the truncation note did not attach to an initial-test abort:\n%v", err)
		}
		// The note is dross's own prose; stryker's list is not in the error.
		if strings.Contains(err.Error(), "src/a.spec.ts") {
			t.Errorf("stryker's failing-test list came with the note:\n%v", err)
		}
	})

	t.Run("does not attach otherwise", func(t *testing.T) {
		s := &Stryker{ProjectRoot: t.TempDir()}
		defer noisyStryker(t, s, "ERROR a crash in the instrumenter", 1, nil)()

		_, _, err := captureStderr(t, func() (*Report, error) { return s.Run([]string{"src/a.ts"}) })
		if err == nil {
			t.Fatal("a reportless failure returned nil error")
		}
		if strings.Contains(err.Error(), note) {
			t.Errorf("the truncation note claimed a truncated list on an abort that printed none:\n%v", err)
		}
	})
}

// TestStrykerInstrumentationFailureCarriesNoQuotedOutput. checkInstrumented runs
// after a SUCCESSFUL invocation — the tool wrote a report, it just skipped
// files — so there is no tool failure to record and the dropped-path list is
// the whole diagnostic. The hint stays; the quoted output goes.
func TestStrykerInstrumentationFailureCarriesNoQuotedOutput(t *testing.T) {
	s := &Stryker{ProjectRoot: t.TempDir()}
	warning := `Glob pattern "src/c.ts" ` + strykerDropWarningText + ". " + canaryA
	defer noisyStryker(t, s, "WARN ProjectReader "+warning, 0, func(p string) {
		writeReport(t, p, "src/a.ts")
	})()

	out, _, err := captureStderr(t, func() (*Report, error) {
		return s.Run([]string{"src/a.ts", "src/b.ts", "src/c.ts"})
	})
	if err == nil {
		t.Fatal("a run that silently dropped files returned nil error")
	}

	for _, dropped := range []string{"src/b.ts", "src/c.ts"} {
		if !strings.Contains(err.Error(), dropped) {
			t.Errorf("the error no longer names dropped path %s:\n%v", dropped, err)
		}
	}
	if !strings.Contains(err.Error(), "stryker said so itself") {
		t.Errorf("the hint is gone from an error whose head DID match:\n%v", err)
	}
	if strings.Contains(err.Error(), canaryA) {
		t.Errorf("the tool's output is quoted inside the error:\n%v", err)
	}
	if !strings.Contains(out, canaryA) {
		t.Errorf("the head was not printed to the terminal, so the hint points at nothing:\n%s", out)
	}
}
