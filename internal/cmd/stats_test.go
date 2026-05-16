package cmd

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/telemetry"
)

// TestStatsShowAggregatesEvents writes a few events to a temp HOME's
// telemetry log, runs `dross stats show`, and asserts the major
// sections render with the right counts.
func TestStatsShowAggregatesEvents(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("DROSS_NO_TELEMETRY", "") // ensure recording allowed in this test

	path := filepath.Join(home, ".claude", "dross", telemetry.File)
	for _, ev := range []telemetry.Event{
		{Kind: "cli", Command: "init", DurationMS: 10, ExitCode: 0},
		{Kind: "cli", Command: "verify", DurationMS: 50, ExitCode: 0},
		{Kind: "cli", Command: "verify", DurationMS: 60, ExitCode: 1, ErrorClass: "missing"},
		{Kind: "outcome", Command: "verify", Tags: map[string]string{"verdict": "pass"}, Numbers: map[string]float64{"mutation_score": 0.9}},
		{Kind: "outcome", Command: "verify", Tags: map[string]string{"verdict": "fail"}, Numbers: map[string]float64{"mutation_score": 0.5}},
		{Kind: "outcome", Command: "ship", Tags: map[string]string{"provider": "github", "result": "opened"}},
	} {
		if err := telemetry.Append(path, ev); err != nil {
			t.Fatal(err)
		}
	}

	out := captureStdout(t, func() {
		if err := runCmd(t, Stats(), "show"); err != nil {
			t.Fatalf("stats show: %v", err)
		}
	})

	for _, want := range []string{
		"events:  6",
		"## commands",
		"verify",
		"init",
		"## errors",
		"missing",
		"## outcomes",
		"verify verdicts",
		"pass=1",
		"fail=1",
		"mutation score",
		"ship results",
		"opened=1",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("stats show missing %q in output:\n%s", want, out)
		}
	}
}

func TestStatsRendersDoctorOutcomes(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("DROSS_NO_TELEMETRY", "")

	path := filepath.Join(home, ".claude", "dross", telemetry.File)
	for _, ev := range []telemetry.Event{
		{Kind: "outcome", Command: "doctor", Counts: map[string]int{"issues": 0}, Tags: map[string]string{"result": "passed"}},
		{Kind: "outcome", Command: "doctor", Counts: map[string]int{"issues": 3}, Tags: map[string]string{"result": "issues_found"}},
		{Kind: "outcome", Command: "doctor", Counts: map[string]int{"issues": 2}, Tags: map[string]string{"result": "issues_found"}},
	} {
		if err := telemetry.Append(path, ev); err != nil {
			t.Fatal(err)
		}
	}

	out := captureStdout(t, func() {
		if err := runCmd(t, Stats(), "show"); err != nil {
			t.Fatalf("stats show: %v", err)
		}
	})
	for _, want := range []string{
		"doctor runs",
		"passed=1",
		"issues_found=2",
		"cumulative issues",
		"5",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("stats show missing %q in output:\n%s", want, out)
		}
	}
}

func TestStatsShowEmptyLog(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	out := captureStdout(t, func() {
		if err := runCmd(t, Stats(), "show"); err != nil {
			t.Fatalf("stats show on empty: %v", err)
		}
	})
	if !strings.Contains(out, "no telemetry events") {
		t.Errorf("expected 'no telemetry events' message:\n%s", out)
	}
}

func TestStatsOptOutAndOptIn(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	if err := runCmd(t, Stats(), "opt-out"); err != nil {
		t.Fatalf("opt-out: %v", err)
	}
	if telemetryEnabled() {
		t.Error("telemetry should be disabled after opt-out")
	}
	if err := runCmd(t, Stats(), "opt-in"); err != nil {
		t.Fatalf("opt-in: %v", err)
	}
	if !telemetryEnabled() {
		t.Error("telemetry should be enabled after opt-in")
	}
}

func TestStatsPathPrintsResolvedPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	out := captureStdout(t, func() {
		if err := runCmd(t, Stats(), "path"); err != nil {
			t.Fatalf("stats path: %v", err)
		}
	})
	if !strings.Contains(out, telemetry.File) {
		t.Errorf("expected telemetry file path in output:\n%s", out)
	}
}
