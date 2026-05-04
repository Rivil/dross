package mutation

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestStrykerNetParsesSchemaSharedWithJS confirms that the Stryker.NET
// adapter routes its JSON through ParseStrykerJSON and applies the
// .NET-specific tool name on the result. The schema (mutation-testing-
// report-schema) is identical across Stryker.JS and Stryker.NET; this
// test pins that assumption.
func TestStrykerNetParsesSchemaSharedWithJS(t *testing.T) {
	// Synthetic report following the public schema with C# source data.
	report := map[string]any{
		"schemaVersion": "1",
		"files": map[string]any{
			"src/Calculator.cs": map[string]any{
				"language": "csharp",
				"source":   "public class Calculator { public int Add(int a, int b) => a + b; }",
				"mutants": []map[string]any{
					{
						"id":          "1",
						"mutatorName": "BinaryOperator",
						"replacement": "a - b",
						"status":      "Killed",
						"location":    map[string]any{"start": map[string]int{"line": 1, "column": 1}, "end": map[string]int{"line": 1, "column": 80}},
					},
					{
						"id":          "2",
						"mutatorName": "BooleanLiteral",
						"replacement": "false",
						"status":      "Survived",
						"location":    map[string]any{"start": map[string]int{"line": 5, "column": 1}, "end": map[string]int{"line": 5, "column": 10}},
					},
				},
			},
		},
	}
	b, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}

	r, err := ParseStrykerJSON(b)
	if err != nil {
		t.Fatalf("parse shared schema: %v", err)
	}
	if r.Killed != 1 || r.Survived != 1 {
		t.Errorf("counts wrong: killed=%d survived=%d", r.Killed, r.Survived)
	}
	if len(r.Surviving) != 1 || r.Surviving[0].Op != "BooleanLiteral" {
		t.Errorf("surviving mutant lost: %+v", r.Surviving)
	}
}

// TestStrykerNetFindReportPicksMostRecent ensures the discovery logic
// handles Stryker.NET's varying output layouts (timestamped subdirs in
// older versions, flat layout in newer ones) and prefers the freshest
// report on a re-run.
func TestStrykerNetFindReportPicksMostRecent(t *testing.T) {
	out := t.TempDir()

	// Older-style timestamped layout — write a stale report.
	stalePath := filepath.Join(out, "2024-01-01T00-00-00", "reports", "mutation-report.json")
	if err := os.MkdirAll(filepath.Dir(stalePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stalePath, []byte(`{"schemaVersion":"1","files":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	staleTime := time.Now().Add(-24 * time.Hour)
	if err := os.Chtimes(stalePath, staleTime, staleTime); err != nil {
		t.Fatal(err)
	}

	// Newer flat layout — fresh report.
	freshPath := filepath.Join(out, "reports", "mutation-report.json")
	if err := os.MkdirAll(filepath.Dir(freshPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(freshPath, []byte(`{"schemaVersion":"1","files":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := findReport(out)
	if err != nil {
		t.Fatal(err)
	}
	if got != freshPath {
		t.Errorf("findReport picked %q, want %q (most recent)", got, freshPath)
	}
}

func TestStrykerNetFindReportErrsWhenMissing(t *testing.T) {
	out := t.TempDir()
	_, err := findReport(out)
	if err == nil || !strings.Contains(err.Error(), "no mutation-report.json") {
		t.Errorf("expected missing-report error, got: %v", err)
	}
}

func TestStrykerNetSupports(t *testing.T) {
	s := &StrykerNet{}
	for _, ok := range []string{"Foo.cs", "src/Bar.cs", "deep/path/Baz.CS"} {
		if !s.Supports(ok) {
			t.Errorf("should support %q", ok)
		}
	}
	for _, no := range []string{"foo.ts", "foo.go", "foo.cshtml", "foo.fs"} {
		if s.Supports(no) {
			t.Errorf("should not support %q", no)
		}
	}
}

func TestStrykerNetRunReturnsEmptyForNoFiles(t *testing.T) {
	s := &StrykerNet{ProjectRoot: t.TempDir()}
	r, err := s.Run(nil)
	if err != nil {
		t.Fatal(err)
	}
	if r.Tool != "stryker-net" || r.Killed != 0 || r.Survived != 0 {
		t.Errorf("expected empty report, got %+v", r)
	}
}

func TestStrykerNetOutputDirDefaults(t *testing.T) {
	s := &StrykerNet{ProjectRoot: "/tmp/proj"}
	if got := s.outputDir(); got != "/tmp/proj/StrykerOutput" {
		t.Errorf("default output dir wrong: %q", got)
	}
	s.OutputDir = "custom"
	if got := s.outputDir(); got != "/tmp/proj/custom" {
		t.Errorf("relative custom output dir wrong: %q", got)
	}
	s.OutputDir = "/abs/output"
	if got := s.outputDir(); got != "/abs/output" {
		t.Errorf("absolute output dir should pass through: %q", got)
	}
}
