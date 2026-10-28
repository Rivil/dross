package cmd

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Rivil/dross/internal/gatestate"
	"github.com/Rivil/dross/internal/pathfence"
	"github.com/Rivil/dross/internal/review"
)

func runDispatch(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var err error
	out := captureStdout(t, func() { err = runCmd(t, Task(), append([]string{"dispatch"}, args...)...) })
	return out, err
}

// commitSized commits a.go at n bytes in the review fixture.
func commitSized(t *testing.T, dir string, n int) {
	t.Helper()
	mustWrite(t, filepath.Join(dir, "a.go"), "package a\n"+strings.Repeat("/", n-len("package a\n")))
	gitCommit(t, dir, "size a.go")
}

// TestDispatchSizeGate pins the dispatch decision: one byte under the gate
// stays inline with no prompt line; at the gate the writer gets the exact
// WriterPromptLine; uncommitted edits never move the surface.
func TestDispatchSizeGate(t *testing.T) {
	dir, _ := reviewFixture(t, "solo")
	commitSized(t, dir, writerSurfaceBytes-1)
	out, err := runDispatch(t, "p", "t-1")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out, "dispatch: inline (") || strings.Contains(out, "prompt:") {
		t.Fatalf("threshold-1 bytes dispatched:\n%s", out)
	}

	mustWrite(t, filepath.Join(dir, "a.go"), strings.Repeat("x", 10*writerSurfaceBytes))
	if out, _ := runDispatch(t, "p", "t-1"); !strings.HasPrefix(out, "dispatch: inline (") {
		t.Fatalf("a dirty working-tree edit moved the surface:\n%s", out)
	}

	commitSized(t, dir, writerSurfaceBytes)
	out, err = runDispatch(t, "p", "t-1")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out, "dispatch: writer\n") || printedField(t, out, "subagent_type") != review.WriterAgent ||
		printedField(t, out, "prompt") != review.WriterPromptLine("p", "t-1") {
		t.Fatalf("threshold bytes did not dispatch the writer with the exact prompt line:\n%s", out)
	}
}

// TestSurfaceFencesPaths: a path escaping the repo is an error, never a size.
func TestSurfaceFencesPaths(t *testing.T) {
	dir, _ := reviewFixture(t, "solo")
	if n, err := taskSurface(dir, []string{"a.go", "new_file.go"}); err != nil || n != int64(len("package a\n")) {
		t.Fatalf("a.go + a new file = %d, %v; want a.go's size, the new file counting 0", n, err)
	}
	for _, bad := range []string{"../x", "/etc/passwd"} {
		if n, err := taskSurface(dir, []string{bad}); err == nil {
			t.Errorf("%q counted as %d bytes instead of erroring", bad, n)
		} else if !errors.Is(err, pathfence.ErrEscapes) && !errors.Is(err, pathfence.ErrAbsolute) {
			t.Errorf("%q: %v, want a pathfence refusal", bad, err)
		}
	}
}

// TestDispatchRefusesOutsideSolo pins c-4: pair mode, a solo quick, or a task
// not in progress exits non-zero with no prompt line.
func TestDispatchRefusesOutsideSolo(t *testing.T) {
	check := func(name string) {
		t.Helper()
		out, err := runDispatch(t, "p", "t-1")
		if err == nil || strings.Contains(out, "prompt:") {
			t.Errorf("%s: dispatch = %q, %v; want a non-zero exit and no prompt line", name, out, err)
		}
	}
	reviewFixture(t, "pair")
	check("pair mode")

	dir, _ := reviewFixture(t, "solo")
	head := strings.TrimSpace(mustGit(t, dir, "rev-parse", "HEAD"))
	if err := gatestate.SaveQuick(dir, gatestate.Quick{Mode: "solo", Description: "x", Head: head, At: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	check("solo quick")

	reviewFixture(t, "solo")
	if err := runCmd(t, Task(), "status", "p", "t-1", "done"); err != nil {
		t.Fatal(err)
	}
	check("task not in progress")
}

// TestDispatchKillSwitchForcesInline: with the kill switch off, no surface is
// big enough for a writer.
func TestDispatchKillSwitchForcesInline(t *testing.T) {
	if w, why := dispatchDecision(10<<20, false); w || !strings.Contains(why, "kill switch") {
		t.Fatalf("a 10 MB task with the kill switch off: writer = %v (%q)", w, why)
	}
	if w, _ := dispatchDecision(10<<20, true); !w {
		t.Fatal("a 10 MB task with the kill switch on stayed inline")
	}
}

// TestDispatchThresholdMatchesBaseline: the gate in code is the gate the
// baseline calibrated.
func TestDispatchThresholdMatchesBaseline(t *testing.T) {
	if ks := loadKillSwitch(t); ks.Calibration.Threshold != writerSurfaceBytes {
		t.Fatalf("writerSurfaceBytes = %d, kill-switch.toml [calibration].threshold = %d", writerSurfaceBytes, ks.Calibration.Threshold)
	}
}

func rows(peakWall ...float64) []killSwitchRow {
	var out []killSwitchRow
	for i := 0; i+1 < len(peakWall); i += 2 {
		out = append(out, killSwitchRow{PeakContext: peakWall[i], WallClockS: peakWall[i+1], SurfaceBytes: writerSurfaceBytes})
	}
	return out
}

// TestCompareThresholds pins the locked thresholds at their edges.
func TestCompareThresholds(t *testing.T) {
	before := rows(1000, 100)
	for _, c := range []struct {
		name   string
		after  []killSwitchRow
		pass   bool
		reason string
	}{
		{"drop 25.0%, rise 25.0%", rows(750, 125), true, ""},
		{"drop 24.9%", rows(751, 100), false, "context"},
		{"rise 25.1%", rows(500, 125.1), false, "wall-clock"},
	} {
		v, err := compareKillSwitch(before, c.after)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if v.Pass != c.pass || !strings.Contains(v.Reason, c.reason) {
			t.Errorf("%s: pass = %v (%q), want %v naming %q", c.name, v.Pass, v.Reason, c.pass, c.reason)
		}
	}
	// Turns never gate: killSwitchRow has no turn count to triple.
	small := []killSwitchRow{{PeakContext: 1, WallClockS: 1, SurfaceBytes: writerSurfaceBytes - 1}}
	if _, err := compareKillSwitch(before, small); err == nil {
		t.Error("an after leg with no above-gate row compared instead of erroring")
	}
	if _, err := compareKillSwitch(small, before); err == nil {
		t.Error("a before leg with no above-gate row compared instead of erroring")
	}
}

// TestCompareUsesAboveGateMedians: medians, of peak context, over above-gate
// rows only.
func TestCompareUsesAboveGateMedians(t *testing.T) {
	before := rows(1000, 100, 1000, 100, 1000, 100)
	// The mean of these peaks (1200) fails a 25% drop; their median (700)
	// passes it.
	after := rows(700, 100, 700, 100, 2200, 100)
	after = append(after, killSwitchRow{PeakContext: 1e9, WallClockS: 1e9, SurfaceBytes: writerSurfaceBytes - 1})
	v, err := compareKillSwitch(before, after)
	if err != nil {
		t.Fatal(err)
	}
	if !v.Pass || v.ContextDrop < 0.29 || v.ContextDrop > 0.31 || v.WallClockRise != 0 {
		t.Fatalf("verdict %+v: want a 30%% median drop and no rise, the below-gate row ignored", v)
	}
}

func killSwitchRows(rs []ksRow) []killSwitchRow {
	out := make([]killSwitchRow, len(rs))
	for i, r := range rs {
		out[i] = killSwitchRow{PeakContext: float64(r.PeakContext), WallClockS: r.WallClockS, SurfaceBytes: int64(r.SurfaceBytes)}
	}
	return out
}

// TestKillSwitchVerdictHolds pins c-6: once the after leg is recorded, the
// kill switch in code is the comparison's verdict.
func TestKillSwitchVerdictHolds(t *testing.T) {
	ks := loadKillSwitch(t)
	if ks.After == nil {
		t.Skip("kill-switch.toml has no [after] leg yet — the trial is pending")
	}
	v, err := compareKillSwitch(killSwitchRows(ks.Before.Task), killSwitchRows(ks.After.Task))
	if err != nil {
		t.Fatal(err)
	}
	if writerEnabled != v.Pass {
		t.Fatalf("writerEnabled = %v but the recorded trial says pass = %v (%s)", writerEnabled, v.Pass, v.Reason)
	}
}

// TestWriterShipsEnabledForTrial: the writer cannot be measured switched off.
func TestWriterShipsEnabledForTrial(t *testing.T) {
	if ks := loadKillSwitch(t); ks.Trial.Status == "pending" && !writerEnabled {
		t.Fatal("writerEnabled is false while the kill-switch trial is pending — the after leg would measure no writer")
	}
}
