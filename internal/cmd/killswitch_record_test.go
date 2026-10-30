package cmd

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/BurntSushi/toml"
)

// ksRow is one task's main-loop cost in a solo run: the peak context it ran
// at, the context it added, its wall-clock and turns, and the bytes of its
// files when it started — the surface the writer size gate reads.
type ksRow struct {
	Phase         string  `toml:"phase"`
	Task          string  `toml:"task"`
	PeakContext   int     `toml:"peak_context"`
	ContextGrowth int     `toml:"context_growth"`
	WallClockS    float64 `toml:"wall_clock_s"`
	Turns         int     `toml:"turns"`
	SurfaceBytes  int     `toml:"surface_bytes"`
}

type ksLeg struct {
	Method string  `toml:"method"`
	Filter string  `toml:"filter"`
	Task   []ksRow `toml:"task"`
}

type ksRecord struct {
	Thresholds  map[string]any `toml:"thresholds"`
	Calibration struct {
		Threshold int    `toml:"threshold"`
		Method    string `toml:"method"`
	} `toml:"calibration"`
	Trial struct {
		Status string `toml:"status"`
	} `toml:"trial"`
	Before *ksLeg `toml:"before"`
	After  *ksLeg `toml:"after"`
}

// v17SoloPhases are the v1.7 phases run under --solo whose transcripts
// make up the kill switch's before leg (the locked baseline decision).
var v17SoloPhases = []string{"solo-task-review", "board-finalize-on-complete", "verify-staleness", "debug-sessions", "review-comment-ingest"}

func killSwitchPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(repoRootFromTest(t), RootDirName, "phases", "solo-task-subagent", "kill-switch.toml")
}

func loadKillSwitch(t *testing.T) ksRecord {
	t.Helper()
	var ks ksRecord
	if _, err := toml.DecodeFile(killSwitchPath(t), &ks); err != nil {
		t.Fatalf("decode kill-switch.toml: %v", err)
	}
	return ks
}

func medianOf(xs []float64) float64 {
	s := slices.Clone(xs)
	slices.Sort(s)
	if len(s) == 0 {
		return 0
	}
	if len(s)%2 == 1 {
		return s[len(s)/2]
	}
	return (s[len(s)/2-1] + s[len(s)/2]) / 2
}

// TestKillSwitchBaselineRecorded guards the before leg: every v1.7 solo phase
// contributes rows, and every row is a measurement. A negative growth means a
// compaction inside the task's window was counted raw.
func TestKillSwitchBaselineRecorded(t *testing.T) {
	ks := loadKillSwitch(t)
	if ks.Before == nil {
		t.Fatal("[before] is missing")
	}
	if ks.Before.Method == "" || ks.Before.Filter == "" {
		t.Error("[before] lacks its method or jq filter — the baseline cannot be re-mined")
	}
	rows := map[string]int{}
	for _, r := range ks.Before.Task {
		rows[r.Phase]++
		if r.PeakContext <= 0 || r.WallClockS <= 0 || r.Turns <= 0 {
			t.Errorf("%s/%s: peak_context %d, wall_clock_s %v, turns %d — not a measurement", r.Phase, r.Task, r.PeakContext, r.WallClockS, r.Turns)
		}
		if r.ContextGrowth < 0 || r.SurfaceBytes < 0 {
			t.Errorf("%s/%s: context_growth %d, surface_bytes %d — a compaction segment counted raw, or a bad surface", r.Phase, r.Task, r.ContextGrowth, r.SurfaceBytes)
		}
	}
	for _, p := range v17SoloPhases {
		if rows[p] == 0 {
			t.Errorf("v1.7 solo phase %s has no [[before.task]] row", p)
		}
	}
}

// TestKillSwitchThresholdsLocked pins the locked thresholds decision: 25%
// context drop, 25% wall-clock rise, and no gate on turn count.
func TestKillSwitchThresholdsLocked(t *testing.T) {
	ks := loadKillSwitch(t)
	want := map[string]float64{"context_drop": 0.25, "wall_clock_rise": 0.25}
	for k, v := range ks.Thresholds {
		w, ok := want[k]
		if !ok {
			t.Errorf("[thresholds] has %s — only context_drop and wall_clock_rise gate; turns are recorded, not gating", k)
			continue
		}
		if f, _ := v.(float64); f != w {
			t.Errorf("[thresholds] %s = %v, want %v", k, v, w)
		}
	}
	for k := range want {
		if _, ok := ks.Thresholds[k]; !ok {
			t.Errorf("[thresholds] lacks %s", k)
		}
	}
}

// TestThresholdCalibrated checks the size gate earns its place on the
// baseline: tasks at or above it grew the main loop's context more than the
// tasks below it — the context a writer would shed.
func TestThresholdCalibrated(t *testing.T) {
	ks := loadKillSwitch(t)
	if ks.Calibration.Threshold <= 0 || ks.Calibration.Method == "" {
		t.Fatalf("[calibration] threshold %d, method %q — not calibrated", ks.Calibration.Threshold, ks.Calibration.Method)
	}
	if ks.Before == nil {
		t.Fatal("[before] is missing")
	}
	var above, below []float64
	for _, r := range ks.Before.Task {
		if r.SurfaceBytes >= ks.Calibration.Threshold {
			above = append(above, float64(r.ContextGrowth))
		} else {
			below = append(below, float64(r.ContextGrowth))
		}
	}
	if len(above) == 0 || len(below) == 0 {
		t.Fatalf("threshold %d splits the baseline %d above / %d below — nothing to compare", ks.Calibration.Threshold, len(above), len(below))
	}
	if a, b := medianOf(above), medianOf(below); a <= b {
		t.Errorf("median context_growth at or above %d bytes is %v, below it %v — the gate does not pick the context-heavy tasks", ks.Calibration.Threshold, a, b)
	}
}

// TestKillSwitchTrialPending keeps the trial's status honest: pending means
// no after leg yet, and any verdict status needs the after leg it rests on.
func TestKillSwitchTrialPending(t *testing.T) {
	ks := loadKillSwitch(t)
	switch {
	case ks.Trial.Status == "":
		t.Error("[trial] has no status")
	case ks.Trial.Status == "pending" && ks.After != nil:
		t.Error("[after] exists while [trial].status is pending — record the verdict")
	case ks.Trial.Status != "pending" && ks.After == nil:
		t.Errorf("[trial].status is %q with no [after] leg to rest on", ks.Trial.Status)
	}
}

// TestKillSwitchCopiesNoContent keeps transcript text out of a tracked file:
// the only string values allowed are names, the method prose and the filter.
func TestKillSwitchCopiesNoContent(t *testing.T) {
	var raw map[string]any
	if _, err := toml.DecodeFile(killSwitchPath(t), &raw); err != nil {
		t.Fatal(err)
	}
	allowed := []string{"phase", "task", "method", "filter", "status"}
	var walk func(path string, v any)
	walk = func(path string, v any) {
		switch x := v.(type) {
		case map[string]any:
			for k, e := range x {
				if s, ok := e.(string); ok {
					if !slices.Contains(allowed, k) {
						t.Errorf("%s.%s is a string (%d bytes) outside %v — transcript text may have leaked", path, k, len(s), allowed)
					}
					continue
				}
				walk(path+"."+k, e)
			}
		case []map[string]any:
			for _, e := range x {
				walk(path+"[]", e)
			}
		case []any:
			for _, e := range x {
				if _, ok := e.(string); ok {
					t.Errorf("%s holds a string array — transcript text may have leaked", path)
					return
				}
				walk(path+"[]", e)
			}
		}
	}
	walk("kill-switch", raw)
}
