package mutation

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A fetched report is reused in place: nothing is spawned (there is no
// stryker, no package manager and no remote in this fixture, and any of the
// three would fail a launch), the report parses through the same path a run
// takes, and the path plus mtime are printed so the reuse cannot be mistaken
// for a fresh measurement.
func TestReuseReportReadsTheFetchedReportWithoutSpawning(t *testing.T) {
	root := t.TempDir()
	reportDir := filepath.Join(root, "web", "reports", "mutation")
	if err := os.MkdirAll(reportDir, 0o755); err != nil {
		t.Fatal(err)
	}
	reportPath := filepath.Join(reportDir, "mutation.json")
	report := `{"files":{"src/a.ts":{"mutants":[{"id":"1","status":"Killed","location":{"start":{"line":3}}},{"id":"2","status":"Survived","location":{"start":{"line":4}}}]}}}`
	if err := os.WriteFile(reportPath, []byte(report), 0o644); err != nil {
		t.Fatal(err)
	}
	written := time.Now().Add(-90 * time.Minute)
	if err := os.Chtimes(reportPath, written, written); err != nil {
		t.Fatal(err)
	}

	s := &Stryker{ProjectRoot: root, Workdir: "web", ReuseReport: true}
	out, got, err := captureStderr(t, func() (*Report, error) {
		return s.Run([]string{"web/src/a.ts", "web/src/app.d.ts"})
	})
	if err != nil {
		t.Fatalf("reuse: %v\n%s", err, out)
	}
	if got.Killed != 1 || got.Survived != 1 {
		t.Errorf("reused report parsed as killed=%d survived=%d, want 1/1", got.Killed, got.Survived)
	}
	if _, ok := got.Files["web/src/a.ts"]; !ok {
		t.Errorf("reused report keys must be re-prefixed to repo-relative, got %v", got.Files)
	}
	if !strings.Contains(out, "REUSING existing report "+reportPath) {
		t.Errorf("stderr must name the reused report path:\n%s", out)
	}
	if !strings.Contains(out, written.UTC().Format(time.RFC3339)) {
		t.Errorf("stderr must carry the report's mtime %s:\n%s", written.UTC().Format(time.RFC3339), out)
	}
	if !strings.Contains(out, "src/app.d.ts") || !strings.Contains(out, "contributed no mutants") {
		t.Errorf("an absent zero-mutant file must be named as contributing nothing, not refused:\n%s", out)
	}
	if strings.Contains(out, strykerDropWarningText) {
		t.Errorf("no run happened, so nothing may claim stryker warned:\n%s", out)
	}
}

// No report is not a stale report: the flag refuses, naming the path it
// looked at, rather than launching the run it was told not to.
func TestReuseReportRefusesWhenNoReportExists(t *testing.T) {
	root := t.TempDir()
	s := &Stryker{ProjectRoot: root, ReuseReport: true}
	_, _, err := captureStderr(t, func() (*Report, error) {
		return s.Run([]string{"src/a.ts"})
	})
	if err == nil {
		t.Fatal("reuse with no report on disk must refuse")
	}
	want := filepath.Join(root, "reports", "mutation", "mutation.json")
	if !strings.Contains(err.Error(), want) || !strings.Contains(err.Error(), "--reuse-report") {
		t.Errorf("refusal must name the flag and the path it looked at: %v", err)
	}
}
