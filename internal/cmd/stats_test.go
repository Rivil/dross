package cmd

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

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

// TestStatsCover_parseSince pins the exact value/sign returned by each branch
// of parseSince so the arithmetic/conditional mutants on lines 340-349 change
// observable output. It calls the unexported parseSince directly (same pkg).
func TestStatsCover_parseSince(t *testing.T) {
	now := time.Now()

	// Empty → zero time.
	if got := parseSince(""); !got.IsZero() {
		t.Errorf(`parseSince("") = %v, want zero`, got)
	}

	// Absolute date (line 340 == nil branch): exact value returned. Under the
	// negation mutant this branch is skipped and a zero time falls through.
	got := parseSince("2026-05-01")
	if got.IsZero() {
		t.Fatalf(`parseSince("2026-05-01") returned zero; date branch (line 340) not taken`)
	}
	if got.Year() != 2026 || got.Month() != time.May || got.Day() != 1 {
		t.Errorf(`parseSince("2026-05-01") = %v, want 2026-05-01`, got)
	}

	// Go duration (lines 343-344): now - 24h. Negating line 343 returns zero;
	// inverting the sign on line 344 returns a future time.
	got = parseSince("24h")
	if got.IsZero() {
		t.Fatalf(`parseSince("24h") returned zero; duration branch (line 343) not taken`)
	}
	if !got.Before(now) {
		t.Errorf(`parseSince("24h") = %v, want a past time (< now %v)`, got, now)
	}
	if got.Before(now.Add(-25*time.Hour)) || got.After(now.Add(-23*time.Hour)) {
		t.Errorf(`parseSince("24h") = %v, want ~24h before now`, got)
	}

	// days-style (lines 347-349): now - 7*24h. A zero result means one of the
	// line-347/348 conditionals was negated; a too-recent or future result
	// means the *24 / sign math on line 349 was mutated.
	got = parseSince("7d")
	if got.IsZero() {
		t.Fatalf(`parseSince("7d") returned zero; days branch (lines 347-348) not taken`)
	}
	// Must be OLDER than a day ago: kills `-d*24`→`-d/24` (~-17min),
	// `*`→`+` (~-7h) and the sign invert (future).
	if !got.Before(now.Add(-24 * time.Hour)) {
		t.Errorf(`parseSince("7d") = %v, want older than 24h ago (7 days)`, got)
	}
	// ...but not absurdly old — pins it near now-7d.
	if got.Before(now.Add(-8*24*time.Hour)) || got.After(now.Add(-6*24*time.Hour)) {
		t.Errorf(`parseSince("7d") = %v, want ~7 days before now`, got)
	}

	// Single "d" exercises the len(s) > 1 == false path (line 347): no valid
	// duration, so a zero time is returned.
	if got := parseSince("d"); !got.IsZero() {
		t.Errorf(`parseSince("d") = %v, want zero (len<=1 path)`, got)
	}
}

// TestStatsCover_renderErrorBucketsOrder asserts error classes print in
// descending-count order, so negating the `>` comparator on line 198 (which
// flips to ascending) changes the printed order and fails the test.
func TestStatsCover_renderErrorBucketsOrder(t *testing.T) {
	events := []telemetry.Event{
		{ErrorClass: "alpha"},
		{ErrorClass: "beta"}, {ErrorClass: "beta"}, {ErrorClass: "beta"},
		{ErrorClass: "gamma"}, {ErrorClass: "gamma"},
	}
	out := captureStdout(t, func() { renderErrorBuckets(events) })

	bi := strings.Index(out, "beta")  // count 3
	gi := strings.Index(out, "gamma") // count 2
	ai := strings.Index(out, "alpha") // count 1
	if bi < 0 || gi < 0 || ai < 0 {
		t.Fatalf("missing an error class in output:\n%s", out)
	}
	if !(bi < gi && gi < ai) {
		t.Errorf("error buckets not descending by count (want beta<gamma<alpha):\n%s", out)
	}
}

// TestStatsCover_renderForceFlagsCount pins the printed override count so the
// increment→decrement mutant on line 213 (count-- → prints -2) is caught.
func TestStatsCover_renderForceFlagsCount(t *testing.T) {
	events := []telemetry.Event{
		{Tags: map[string]string{"force": "true"}},
		{Tags: map[string]string{"force": "true"}},
		{Tags: map[string]string{"force": "false"}},
		{Tags: nil},
	}
	out := captureStdout(t, func() { renderForceFlags(events) })
	if !strings.Contains(out, "force-flag invocations: 2") {
		t.Errorf("want 'force-flag invocations: 2' (decrement mutant prints -2):\n%s", out)
	}
}
