package cmd

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/BurntSushi/toml"
)

// flowLeg is one measured run of a phase's flow: the agent's turns (distinct
// main-agent message ids), the tool each turn called, and its wall-clock with
// the human's waits excluded, on a named binary in a named session.
type flowLeg struct {
	AgentTurns    int      `toml:"agent_turns"`
	TurnTools     []string `toml:"turn_tools"`
	WallClockS    float64  `toml:"wall_clock_s"`
	BinaryVersion string   `toml:"binary_version"`
	SessionID     string   `toml:"session_id"`
	NudgeFired    bool     `toml:"nudge_fired"`
	NudgesSeen    int      `toml:"nudges_seen"`
	MidWave       bool     `toml:"mid_wave"`
	GateLead      string   `toml:"gate_lead"`
	DrossCalls    []string `toml:"dross_calls"`
}

type flowCost struct {
	Scenario map[string]any `toml:"scenario"`
	Before   *flowLeg       `toml:"before"`
	After    *flowLeg       `toml:"after"`
	Control  *flowLeg       `toml:"control"`
}

// TestFlowCostRecords guards milestone v1.8's c-6 record: every phase's
// flow-cost.toml names its scenario well enough to replay, measures a before
// and an after leg completely, and the after leg adds no round-trip: it
// invokes no dross call the before leg did not. Turns and wall-clock stay
// recorded but ungated — the same steps batched into more or fewer tool calls
// move them from run to run (rivil, 2026-10-08). For
// context-boundaries (c-8) it also pins what the after leg exists to show:
// the nudge fired once on the new binary and turned the mid-wave §1g lead into
// checkpoint, while a below-threshold control kept continue.
func TestFlowCostRecords(t *testing.T) {
	root := filepath.Join(repoRootFromTest(t), RootDirName)
	paths, err := filepath.Glob(filepath.Join(root, "phases", "*", "flow-cost.toml"))
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, path := range paths {
		phase := filepath.Base(filepath.Dir(path))
		found = found || phase == "context-boundaries"
		t.Run(phase, func(t *testing.T) {
			var fc flowCost
			if _, err := toml.DecodeFile(path, &fc); err != nil {
				t.Fatalf("decode %s: %v", path, err)
			}
			for _, key := range []string{"repo_shape", "wave_position", "flow_start", "flow_end", "jq_method"} {
				if s, _ := fc.Scenario[key].(string); s == "" {
					t.Errorf("[scenario] has no %s — the flow cannot be replayed identically", key)
				}
			}
			for name, leg := range map[string]*flowLeg{"before": fc.Before, "after": fc.After} {
				if leg == nil {
					t.Errorf("[%s] is missing", name)
					continue
				}
				if leg.AgentTurns <= 0 || leg.WallClockS <= 0 {
					t.Errorf("[%s] agent_turns = %d, wall_clock_s = %v — not a measurement", name, leg.AgentTurns, leg.WallClockS)
				}
				if leg.BinaryVersion == "" || leg.SessionID == "" {
					t.Errorf("[%s] lacks binary_version or session_id", name)
				}
				if len(leg.TurnTools) != leg.AgentTurns {
					t.Errorf("[%s] turn_tools has %d entries for %d turns — a turn-count difference cannot be attributed", name, len(leg.TurnTools), leg.AgentTurns)
				}
				if len(leg.DrossCalls) == 0 {
					t.Errorf("[%s] records no dross_calls — the round-trip gate has nothing to compare", name)
				}
			}
			if fc.Before == nil || fc.After == nil {
				return
			}
			for _, call := range fc.After.DrossCalls {
				if !slices.Contains(fc.Before.DrossCalls, call) {
					t.Errorf("the after leg runs %q, which the before leg did not — the change added a round-trip", call)
				}
			}
			if fc.After.BinaryVersion == fc.Before.BinaryVersion {
				t.Errorf("both legs ran on %q — the after leg did not measure the new binary", fc.After.BinaryVersion)
			}
			if phase != "context-boundaries" {
				return
			}
			a := fc.After
			if !a.NudgeFired || a.NudgesSeen != 1 {
				t.Errorf("after: nudge_fired = %v, nudges_seen = %d; want one nudge, fired once", a.NudgeFired, a.NudgesSeen)
			}
			if !a.MidWave || a.GateLead != "checkpoint" {
				t.Errorf("after: mid_wave = %v, gate_lead = %q; want a mid-wave §1g that led with checkpoint", a.MidWave, a.GateLead)
			}
			if fc.Control == nil {
				t.Error("[control] is missing — nothing shows a below-threshold mid-wave §1g still leads with continue")
			} else if c := fc.Control; c.GateLead != "continue" || !c.MidWave || c.NudgesSeen != 0 || c.NudgeFired {
				t.Errorf("[control] gate_lead = %q, mid_wave = %v, nudges_seen = %d, nudge_fired = %v; want a nudge-free mid-wave §1g that still led with continue",
					c.GateLead, c.MidWave, c.NudgesSeen, c.NudgeFired)
			}
		})
	}
	if !found {
		t.Fatal("context-boundaries has no flow-cost.toml — the c-8 record is missing")
	}
}
