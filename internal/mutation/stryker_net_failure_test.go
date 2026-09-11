package mutation

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The stryker.net half of c-1. Two things had to move here that Stryker.JS
// already had: the output was streamed and never teed, so a failure could say
// nothing about what the tool printed, and the no-report case was diagnosed
// inside findReport, which has no exit status to record.
const canaryN = "CANARY-N-2e5c"

// noisyStrykerNet swaps the process seam for one that prints payload, exits
// code, and writes NO report.
//
// The payload is cat-ed from a file so it never appears in the stub's own argv.
func noisyStrykerNet(t *testing.T, out string, code int) func() {
	t.Helper()
	payload := filepath.Join(t.TempDir(), "tool-output.txt")
	if err := os.WriteFile(payload, []byte(out+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	orig := strykerNetBuildCmd
	strykerNetBuildCmd = func(n *StrykerNet, _ []string) *exec.Cmd {
		// The output tree exists but holds no report — a stryker.net that
		// started and died. Without the mkdir the walk fails with
		// fs.ErrNotExist instead, which is the OTHER return entirely.
		return exec.Command("sh", "-c",
			fmt.Sprintf("mkdir -p %s; cat %s; exit %d", n.outputDir(), payload, code))
	}
	return func() { strykerNetBuildCmd = orig }
}

// TestStrykerNetNoReportIsRecordedAtTheCallSite. The no-report case is a TOOL
// failure and is recorded where the exit status lives — Run — not inside
// findReport, which is called directly by tests and by the launcher and would
// have to invent one.
func TestStrykerNetNoReportIsRecordedAtTheCallSite(t *testing.T) {
	root := t.TempDir()
	s := &StrykerNet{ProjectRoot: root, OutputDir: "StrykerOutput"}
	defer noisyStrykerNet(t, "dotnet stryker: "+canaryN, 5)()

	out, rep, err := captureStderr(t, func() (*Report, error) { return s.Run([]string{"src/A.cs"}) })
	if err == nil {
		t.Fatal("a run that wrote no report returned nil error")
	}
	if rep != nil {
		t.Errorf("a failed run produced a Report: %+v", rep)
	}

	var rec *toolFailure
	if !errors.As(err, &rec) {
		t.Fatalf("the no-report path returned a bare error, not a record: %v", err)
	}
	if !strings.Contains(err.Error(), "stryker.net") {
		t.Errorf("the record does not name the tool:\n%v", err)
	}
	// The exit status is the whole reason this is recorded at the call site:
	// findReport cannot see it.
	if !strings.Contains(err.Error(), "exit status 5") {
		t.Errorf("the record does not name the real exit status — which is only available at the call site:\n%v", err)
	}
	// The advice that was wrong in every case it was hit, for the same reason
	// Stryker.JS dropped it: the config was fine and the cause was in the head
	// of the output.
	if strings.Contains(err.Error(), "check stryker config") {
		t.Errorf("the misleading advice survived into the recorded error:\n%v", err)
	}
	if strings.Contains(err.Error(), canaryN) {
		t.Errorf("tool output reached the error, which is what gets persisted:\n%v", err)
	}
	// Teed live, then re-printed at the failure point — the tee stryker.net
	// did not have before this.
	if n := strings.Count(out, canaryN); n != 2 {
		t.Errorf("the cause appears %d time(s) on stderr, want 2 — once teed live and once under the head banner:\n%s", n, out)
	}
	if strings.Contains(err.Error(), " 0 bytes") {
		t.Errorf("the record observed 0 bytes of a run that printed — the output is not being teed:\n%v", err)
	}
}

// TestStrykerNetInvocationFailureIsRecorded: the process never started, so
// there is no status to name.
func TestStrykerNetInvocationFailureIsRecorded(t *testing.T) {
	s := &StrykerNet{ProjectRoot: t.TempDir(), OutputDir: "StrykerOutput"}
	orig := strykerNetBuildCmd
	strykerNetBuildCmd = func(_ *StrykerNet, _ []string) *exec.Cmd {
		return exec.Command("/nonexistent/dross-dotnet-does-not-exist")
	}
	t.Cleanup(func() { strykerNetBuildCmd = orig })

	_, _, err := captureStderr(t, func() (*Report, error) { return s.Run([]string{"src/A.cs"}) })
	if err == nil {
		t.Fatal("a dotnet that could not be started returned nil error")
	}

	var rec *toolFailure
	if !errors.As(err, &rec) {
		t.Fatalf("the invocation-failed path returned a bare error, not a record: %v", err)
	}
	if !strings.Contains(err.Error(), "did not start") {
		t.Errorf("a start failure does not say so:\n%v", err)
	}
	if strings.Contains(err.Error(), "exit status -1") {
		t.Errorf("a start failure reports an exit status stryker.net never chose:\n%v", err)
	}
	if !strings.Contains(err.Error(), "dotnet tool install") {
		t.Errorf("the install hint was swallowed by the record:\n%v", err)
	}
}

// TestFindReportKeepsItsOwnMessagesForItsOwnCallers. findReport's three returns
// are not one thing: only the no-report case is a tool exit. Its wording is
// unchanged because its DIRECT callers pin it, and they get no exit status to
// record.
func TestFindReportKeepsItsOwnMessagesForItsOwnCallers(t *testing.T) {
	t.Run("no report is typed but reads the same", func(t *testing.T) {
		out := t.TempDir()
		_, err := findReport(out)
		if err == nil {
			t.Fatal("findReport accepted a directory with no report")
		}
		if !strings.Contains(err.Error(), "no mutation-report.json") {
			t.Errorf("findReport's own message changed: %v", err)
		}
		if !strings.Contains(err.Error(), "check stryker config") {
			t.Errorf("findReport's own message changed: %v", err)
		}
		var nr *noReportError
		if !errors.As(err, &nr) {
			t.Error("the no-report return is not discriminable, so the call site cannot tell it from a walk failure")
		}
	})

	// The other two returns are dross's own diagnostics about the filesystem,
	// not a tool exit. Converting either into a record would claim the tool
	// failed when the tool may never have been the problem.
	t.Run("dir-not-exist passes through unconverted", func(t *testing.T) {
		missing := filepath.Join(t.TempDir(), "never-created")
		_, err := findReport(missing)
		if err == nil {
			t.Fatal("findReport accepted a missing output dir")
		}
		if !strings.Contains(err.Error(), "does not exist") {
			t.Errorf("the missing-dir diagnostic changed: %v", err)
		}
		var nr *noReportError
		if errors.As(err, &nr) {
			t.Error("a missing output dir was typed as a no-report tool failure")
		}
		if !errors.Is(err, fs.ErrNotExist) && !strings.Contains(err.Error(), missing) {
			t.Errorf("the error no longer names the directory: %v", err)
		}
	})
}

// TestStrykerNetPassesThroughANonToolFindReportFailure is the call-site half of
// the discrimination above: a missing output dir reaches the caller as itself,
// never as a stryker.net exit.
func TestStrykerNetPassesThroughANonToolFindReportFailure(t *testing.T) {
	root := t.TempDir()
	s := &StrykerNet{ProjectRoot: root, OutputDir: "StrykerOutput"}
	// The stub writes nothing AND removes the output dir the launcher created,
	// so findReport's walk hits fs.ErrNotExist rather than an empty tree.
	orig := strykerNetBuildCmd
	strykerNetBuildCmd = func(n *StrykerNet, _ []string) *exec.Cmd {
		return exec.Command("sh", "-c", "rm -rf "+filepath.Join(root, "StrykerOutput")+"; exit 0")
	}
	t.Cleanup(func() { strykerNetBuildCmd = orig })

	_, _, err := captureStderr(t, func() (*Report, error) { return s.Run([]string{"src/A.cs"}) })
	if err == nil {
		t.Fatal("a vanished output dir returned nil error")
	}
	var rec *toolFailure
	if errors.As(err, &rec) {
		t.Errorf("a filesystem diagnostic was recorded as a stryker.net tool failure: %v", err)
	}
	if !strings.Contains(err.Error(), "does not exist") {
		t.Errorf("the missing-dir diagnostic did not reach the caller: %v", err)
	}
}
