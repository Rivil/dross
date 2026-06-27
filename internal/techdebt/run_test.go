package techdebt

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestNewRunNeverClobbers fails if a second run with the same timestamp+sha
// overwrites the first: it must get a "-2" suffix and both dirs must exist.
func TestNewRunNeverClobbers(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 6, 27, 10, 0, 0, 0, time.UTC)
	sha := "deadbee"

	first, err := NewRun(root, now, sha)
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewRun(root, now, sha)
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatalf("second run clobbered the first: both %q", first)
	}
	if !strings.HasSuffix(second, "-2") {
		t.Fatalf("collision suffix missing: second run = %q, want a -2 suffix", second)
	}
	for _, d := range []string{first, second} {
		if fi, err := os.Stat(d); err != nil || !fi.IsDir() {
			t.Fatalf("run dir %q not created: err=%v", d, err)
		}
	}
}

// TestWriteReportRendersFindings fails if WriteReport drops findings: the run
// dir's report file must exist and name the scanned finding.
func TestWriteReportRendersFindings(t *testing.T) {
	root := t.TempDir()
	runDir, err := NewRun(root, time.Date(2026, 6, 27, 11, 0, 0, 0, time.UTC), "abc1234")
	if err != nil {
		t.Fatal(err)
	}
	fs := []Finding{
		{File: "internal/x.go", Line: 42, Class: ClassMarker, Detail: "TODO"},
		{File: "internal/big.go", Line: 0, Class: ClassOversizedFile, Detail: "800 lines"},
	}
	if err := WriteReport(runDir, fs); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(runDir, ReportName))
	if err != nil {
		t.Fatalf("report not written: %v", err)
	}
	s := string(body)
	if !strings.Contains(s, "internal/x.go:42") || !strings.Contains(s, "TODO") {
		t.Fatalf("report omitted the marker finding:\n%s", s)
	}
	if !strings.Contains(s, "internal/big.go") || !strings.Contains(s, "800 lines") {
		t.Fatalf("report omitted the oversized-file finding:\n%s", s)
	}
}

// TestRenderReportEmpty fails if a clean scan doesn't render the explicit
// no-findings line.
func TestRenderReportEmpty(t *testing.T) {
	if got := renderReport(nil); !strings.Contains(got, "No tech-debt findings") {
		t.Fatalf("empty report = %q, want a no-findings line", got)
	}
}
